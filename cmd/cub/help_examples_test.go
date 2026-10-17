// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// Help text is what people, and agents told to read it first, copy commands from, so a
// command it shows has to be one cub still accepts. This checks every `cub ...` command in
// each command's help and in the two overviews against the command tree: each subcommand
// it names has to exist, and each flag has to be one the command takes and does not
// deprecate. Arguments are not checked.
func TestHelpExamplesNameCurrentCommandsAndFlags(t *testing.T) {
	if os.Getenv("CONFIGHUB_AGENT") == "" {
		// Help text is built when the package is initialized, and includes the sections
		// written for agents only when CONFIGHUB_AGENT is set by then, so the help an agent
		// reads is checked in a child process that starts with it set.
		child := exec.Command(os.Args[0], "-test.run=^TestHelpExamplesNameCurrentCommandsAndFlags$")
		child.Env = append(os.Environ(), "CONFIGHUB_AGENT=1")
		if out, err := child.CombinedOutput(); err != nil {
			t.Errorf("with CONFIGHUB_AGENT=1: %v\n%s", err, out)
		}
	}

	var problems []string
	check := func(where, text string) {
		for _, example := range helpExampleCommands(text) {
			if problem := checkHelpExample(example); problem != "" {
				problems = append(problems, fmt.Sprintf("%s: %s\n        %s", where, problem, example))
			}
		}
	}
	var walk func(*cobra.Command)
	walk = func(c *cobra.Command) {
		check(c.CommandPath(), c.Long+"\n"+c.Example)
		c.LocalFlags().VisitAll(func(flag *pflag.Flag) {
			check(c.CommandPath()+" --"+flag.Name, flag.Usage)
		})
		for _, sub := range c.Commands() {
			walk(sub)
		}
	}
	walk(rootCmd)
	for _, overview := range []struct {
		name string
		fs   interface{ ReadFile(string) ([]byte, error) }
	}{{"cub-overview.md", overviewFS}, {"cub-agent-overview.md", agentsFS}} {
		content, err := overview.fs.ReadFile(overview.name)
		if err != nil {
			t.Fatal(err)
		}
		check(overview.name, string(content))
	}
	if len(problems) > 0 {
		t.Errorf("%d help examples name a command or flag cub does not take:\n    %s",
			len(problems), strings.Join(problems, "\n    "))
	}
}

func TestHelpExampleChecks(t *testing.T) {
	tests := []struct {
		example string
		want    string
	}{
		{example: "cub unit list --space my-space -o json"},
		{example: "cub unit update --space my-space my-unit --patch --restore 2"},
		{example: "cub trigger create --space s t Mutation Kubernetes/YAML -- vet-cel --param=min=2"},
		{example: "cub unit get --space s u --json", want: "--json is deprecated"},
		{example: "cub unit list --frobnicate", want: "cub unit list has no flag --frobnicate"},
		{example: "cub unit list -Z", want: "cub unit list has no flag -Z"},
		{example: "cub unit frobnicate", want: `cub unit has no subcommand "frobnicate"`},
		{example: "cub unit <verb> --help"},
		{example: "cub frobnicate", want: `cub has no subcommand "frobnicate"`},
		{example: "cub helm install my-release my-chart"},
	}
	for _, test := range tests {
		got := checkHelpExample(test.example)
		if test.want == "" && got != "" || test.want != "" && !strings.Contains(got, test.want) {
			t.Errorf("checkHelpExample(%q) = %q, want %q", test.example, got, test.want)
		}
	}

	text := "Examples:\n```\n  # one\n  cub unit list \\\n    --space s\n  $ cub space list | head\n" +
		"  cub mutation list --where 'MutationNum > 1' my-ns --frobnicate\n```\n" +
		"Then run `cub unit data <unit>` to read it, or \"cub unit get <unit>\" or 'cub unit list'.\n"
	got := helpExampleCommands(text)
	want := []string{"cub unit list --space s", "cub space list", "cub mutation list --where 'MutationNum > 1' my-ns --frobnicate",
		"cub unit data <unit>", "cub unit get <unit>", "cub unit list"}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("helpExampleCommands = %q, want %q", got, want)
	}
}

// Commands cub has that are not in its command tree under test: those of plugins it
// installs, and the one cobra adds when cub runs.
var helpExampleUntreedCommands = map[string]bool{"helm": true, "installer": true, "completion": true}

// Flags every command has when cub runs, which cobra or main adds then.
var helpExampleUntreedFlags = map[string]bool{"help": true, "help-overview": true}

var (
	helpExampleLine      = regexp.MustCompile(`^\s*(?:\$\s+)?(cub\s.*)$`)
	helpExampleInline    = regexp.MustCompile("`(cub\\s[^`]+)`|\"(cub\\s[^\"]+)\"|'(cub\\s[^']+)'")
	helpExamplePosition  = regexp.MustCompile(`^(<.*>|\[.*\]|\{.*\}|[A-Z][A-Z0-9_]*|\.\.\.)$`)
	helpExampleShellStop = regexp.MustCompile(`^\s(\||&&|;|>|2>|#)\s`)
)

// helpExampleCommands returns the cub commands a help text shows: lines that begin with
// "cub", joined across trailing backslashes, and inline code spans that begin with it. A
// command ends where a pipe, redirection, list operator or comment does.
func helpExampleCommands(text string) []string {
	var commands []string
	lines := strings.Split(text, "\n")
	for i := 0; i < len(lines); i++ {
		if m := helpExampleLine.FindStringSubmatch(lines[i]); m != nil {
			command := m[1]
			for strings.HasSuffix(command, "\\") && i+1 < len(lines) {
				i++
				command = strings.TrimSuffix(command, "\\") + " " + strings.TrimSpace(lines[i])
			}
			commands = append(commands, helpExampleCut(command))
			continue
		}
		for _, m := range helpExampleInline.FindAllStringSubmatch(lines[i], -1) {
			commands = append(commands, helpExampleCut(m[1]+m[2]+m[3]))
		}
	}
	return commands
}

func helpExampleCut(command string) string {
	var quote rune
	for i, r := range command {
		switch {
		case quote != 0:
			if r == quote {
				quote = 0
			}
		case r == '\'' || r == '"':
			quote = r
		case helpExampleShellStop.MatchString(command[i:]):
			return strings.Join(strings.Fields(command[:i]), " ")
		}
	}
	return strings.Join(strings.Fields(command), " ")
}

// checkHelpExample reports what is wrong with one example command, or "" when nothing is.
func checkHelpExample(example string) string {
	words := helpExampleWords(example)
	if len(words) == 0 || words[0] != "cub" {
		return ""
	}
	words = words[1:]
	c := rootCmd
	for len(words) > 0 {
		var next *cobra.Command
		for _, sub := range c.Commands() {
			if sub.Name() == words[0] || sub.HasAlias(words[0]) {
				next = sub
				break
			}
		}
		if next == nil {
			break
		}
		c, words = next, words[1:]
	}
	if c == rootCmd && len(words) > 0 && helpExampleUntreedCommands[words[0]] {
		return ""
	}
	if len(words) > 0 && !strings.HasPrefix(words[0], "-") && !helpExamplePosition.MatchString(words[0]) &&
		(c == rootCmd || !c.Runnable()) {
		return fmt.Sprintf("%s has no subcommand %q", c.CommandPath(), words[0])
	}
	flags := pflag.NewFlagSet(c.CommandPath(), pflag.ContinueOnError)
	flags.AddFlagSet(c.LocalFlags())
	flags.AddFlagSet(c.InheritedFlags())
	for _, word := range words {
		if word == "--" {
			break
		}
		var flag *pflag.Flag
		var name string
		switch {
		case strings.HasPrefix(word, "--") && len(word) > 2:
			name = strings.SplitN(word[2:], "=", 2)[0]
			if !regexp.MustCompile(`^[A-Za-z][\w-]*$`).MatchString(name) {
				continue
			}
			if helpExampleUntreedFlags[name] {
				continue
			}
			flag = flags.Lookup(name)
			name = "--" + name
		case regexp.MustCompile(`^-[A-Za-z]$`).MatchString(word):
			flag = flags.ShorthandLookup(word[1:])
			name = word
		default:
			continue
		}
		if flag == nil {
			return fmt.Sprintf("%s has no flag %s", c.CommandPath(), name)
		}
		if flag.Deprecated != "" {
			return fmt.Sprintf("%s is deprecated on %s: %s", name, c.CommandPath(), flag.Deprecated)
		}
	}
	return ""
}

// helpExampleWords splits a command into words as a shell would, honoring single and
// double quotes, which is all the examples use.
func helpExampleWords(command string) []string {
	var words []string
	var word strings.Builder
	inWord := false
	var quote rune
	for _, r := range command {
		switch {
		case quote != 0:
			if r == quote {
				quote = 0
			} else {
				word.WriteRune(r)
			}
		case r == '\'' || r == '"':
			quote, inWord = r, true
		case r == ' ' || r == '\t':
			if inWord {
				words = append(words, word.String())
				word.Reset()
				inWord = false
			}
		default:
			word.WriteRune(r)
			inWord = true
		}
	}
	if inWord {
		words = append(words, word.String())
	}
	return words
}
