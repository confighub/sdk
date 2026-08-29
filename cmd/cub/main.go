// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:   "cub",
	Short: "ConfigHub CLI",
	Long:  getSimpleHelp(),
}

// Global context flag
var globalContextFlag string

// What can select the context a command runs against, named as `cub context
// list` prints it in its SELECTED column.
const (
	contextSourceConfig = "config.yaml"
	contextSourceEnv    = "CUB_CONTEXT"
	contextSourceFlag   = "--context"
)

// activeContextOverrideSource is the source that overrode the active context for
// this invocation (contextSourceFlag or contextSourceEnv), or is empty when the
// persisted current context is in effect. Set by applyContextOverride and read by
// the context display commands so the override is never invisible.
var activeContextOverrideSource string

// contextSelector is a source that named a context for this invocation, paired
// with the context it named.
type contextSelector struct {
	Source  string // contextSourceFlag, contextSourceEnv or contextSourceConfig
	Context string
}

// activeContextSelectors lists every source that named a context for this
// invocation, highest precedence first, so the display commands can show all of
// them and mark the one that won. Recorded by applyContextOverride before it
// rewrites $CUB_CONTEXT for child processes, which would otherwise erase what
// the environment originally asked for.
var activeContextSelectors []contextSelector

// recordContextSelectors collects the sources that named a context, highest
// precedence first: the --context flag, then $CUB_CONTEXT, then the current
// context config.yaml records. The first entry is the one in effect.
func recordContextSelectors() {
	activeContextSelectors = nil
	if globalContextFlag != "" {
		activeContextSelectors = append(activeContextSelectors,
			contextSelector{contextSourceFlag, globalContextFlag})
	}
	if env := os.Getenv(contextSourceEnv); env != "" {
		activeContextSelectors = append(activeContextSelectors,
			contextSelector{contextSourceEnv, env})
	}
	if stored := contextManager.CurrentContextName(); stored != "" {
		activeContextSelectors = append(activeContextSelectors,
			contextSelector{contextSourceConfig, stored})
	}
}

// contextSelectionSummary describes what selected the context in effect, naming
// the sources it outranked and what they asked for, so a --context flag or a
// $CUB_CONTEXT that lost is never invisible. It is empty when nothing named a
// context, which means there is none to run against.
func contextSelectionSummary() string {
	if len(activeContextSelectors) == 0 {
		return ""
	}
	var outranked []string
	for _, sel := range activeContextSelectors[1:] {
		verb := "selects"
		if sel.Source == contextSourceConfig {
			verb = "records"
		}
		outranked = append(outranked, fmt.Sprintf("%s %s %q", sel.Source, verb, sel.Context))
	}
	summary := contextSourcePhrase(activeContextSelectors[0].Source)
	if len(outranked) > 0 {
		summary += " (" + strings.Join(outranked, "; ") + ")"
	}
	return summary
}

// contextSourcePhrase spells a source out for prose, where the bare name of a
// flag or an environment variable reads as a fragment.
func contextSourcePhrase(source string) string {
	switch source {
	case contextSourceFlag:
		return "--context flag"
	case contextSourceEnv:
		return "CUB_CONTEXT environment variable"
	default:
		return source
	}
}

// resolveContextOverride determines the context override for this invocation and
// its source, from the selectors recorded by recordContextSelectors. An empty
// name means no override (the persisted current context is used).
func resolveContextOverride() (name, source string) {
	if len(activeContextSelectors) == 0 || activeContextSelectors[0].Source == contextSourceConfig {
		return "", ""
	}
	return activeContextSelectors[0].Context, activeContextSelectors[0].Source
}

// applyContextOverride applies the resolved context override (if any) to the
// context manager, recording its source for display. A named-but-missing context
// is a hard error rather than a silent fallback, so commands never run against an
// unexpected context.
//
// The override is also exported as $CUB_CONTEXT so that child cub processes
// resolve the same context. Several commands delegate work by running cub again
// (runCub: `cub variant upload`, `cub variant create`, `cub cluster up`), and a
// child is a fresh process that re-resolves the context from scratch. --context
// lives only in this process's memory, so without exporting it the child falls
// back to the persisted current context and silently operates somewhere else —
// which for a command that mixes delegated work with its own API calls means
// writing to two contexts at once. Exporting mirrors what --debug already does
// with $CONFIGHUB_DEBUG above, and covers plugins for free.
//
// Only an actual override is exported. With none, parent and child both resolve
// the persisted current context and already agree.
func applyContextOverride() error {
	recordContextSelectors()
	name, source := resolveContextOverride()
	if name == "" {
		return nil
	}
	if err := contextManager.OverrideCurrentContext(name); err != nil {
		return fmt.Errorf("%s: %w", contextSourcePhrase(source), err)
	}
	activeContextOverrideSource = source
	// recordContextSelectors read $CUB_CONTEXT above, so the selectors keep what
	// the environment originally asked for even though this replaces it.
	if err := os.Setenv(contextSourceEnv, name); err != nil {
		return fmt.Errorf("export CUB_CONTEXT for child cub processes: %w", err)
	}
	return nil
}

func globalPreRun(cmd *cobra.Command, args []string) error {
	if debug {
		err := os.Setenv("CONFIGHUB_DEBUG", "1")
		if err != nil {
			return err
		}
	} else if os.Getenv("CONFIGHUB_DEBUG") == "1" {
		// Required for the new goclientnew.Client Debug mode
		fmt.Printf("cub Debug mode enabled. version: %s, buildDate: %s\n", BuildTag, BuildDate)
		debug = true
	}
	if err := applyContextOverride(); err != nil {
		return err
	}

	var err error
	cubClientNew, err = InitializeClient(contextManager.ActiveContext())
	if err != nil {
		return err
	}

	return nil
}

func main() {
	var err error
	// An empty path means "work it out", which cubapi.DefaultConfigPath does by
	// joining config.yaml onto CUB_CONFIG. Deciding it here as well only made
	// the two disagree: this stat'd the value and treated a path that did not
	// exist yet as the file, while cubapi took it verbatim as the file always.
	contextManager, err = NewContextManagerWithPath("")
	if err != nil {
		failOnError(err)
	}
	rootCmd.PersistentFlags().BoolVar(&debug, "debug", false, "Debug output")
	rootCmd.PersistentFlags().StringVar(&globalContextFlag, "context", "", "The context to use for this command")

	// Add --help-overview flag
	var helpOverview bool
	rootCmd.Flags().BoolVar(&helpOverview, "help-overview", false, "Show detailed overview instead of standard help")

	// Store original help function before overriding
	originalHelpFunc := rootCmd.HelpFunc()

	// Override the help function to handle --help-overview
	rootCmd.SetHelpFunc(func(cmd *cobra.Command, args []string) {
		if helpOverview {
			fmt.Print(getFormattedOverview())
			return
		}
		// Use the original help function
		originalHelpFunc(cmd, args)
	})

	// This turns off printing Usage after an error
	rootCmd.SilenceUsage = true
	// We don't want root command to print errors. We'll do it ourselves.
	rootCmd.SilenceErrors = true

	rootCmd.PersistentPreRunE = globalPreRun

	// Include plugin command names in shell tab-completion (canonical names only).
	rootCmd.ValidArgsFunction = func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		var completions []string
		for _, p := range discoverPlugins() {
			for _, c := range p.Commands {
				if c.Entrypoint == "" || c.Name == "" {
					continue
				}
				if strings.HasPrefix(c.Name, toComplete) {
					completions = append(completions, c.Name)
				}
			}
		}
		return completions, cobra.ShellCompDirectiveNoFileComp
	}

	cmd, err := rootCmd.ExecuteC()
	if err != nil {
		// If the error is "unknown command" at the root level, try plugin resolution.
		if cmd == rootCmd && isUnknownCommandError(err) {
			// Apply context override manually since PersistentPreRunE may not have run.
			if overrideErr := applyContextOverride(); overrideErr != nil {
				failOnError(overrideErr)
			}
			pluginArgs := extractPluginArgs(os.Args[1:])
			found, pluginErr := handlePluginCommand(pluginArgs)
			if found {
				// The command resolved to a plugin but failed to exec — show why.
				failOnError(pluginErr)
			}
			// No such plugin command — show the original Cobra error.
			failOnError(err)
		}
		failOnError(err)
	}
}

// isUnknownCommandError checks whether a Cobra error is an "unknown command" error.
func isUnknownCommandError(err error) bool {
	return strings.Contains(err.Error(), "unknown command")
}

// extractPluginArgs returns the arguments intended for plugin resolution by
// stripping known persistent flags (--context, --debug) from os.Args.
func extractPluginArgs(args []string) []string {
	var result []string
	skip := false
	for _, arg := range args {
		if skip {
			skip = false
			continue
		}
		if arg == "--debug" {
			continue
		}
		if arg == "--context" {
			skip = true // skip next arg (the value)
			continue
		}
		if strings.HasPrefix(arg, "--context=") {
			continue
		}
		result = append(result, arg)
	}
	return result
}
