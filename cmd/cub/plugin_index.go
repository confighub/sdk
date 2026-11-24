// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"bufio"
	_ "embed"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"

	"golang.org/x/mod/semver"
	"golang.org/x/term"
	"sigs.k8s.io/yaml"
)

// pluginIndexData is the index of known plugins this build of cub carries, used
// until the published index has been downloaded.
//
//go:embed plugin_index.yaml
var pluginIndexData []byte

// pluginAutoInstallEnv overrides, for one invocation or one shell, whether cub
// installs a known plugin when its command is typed.
const pluginAutoInstallEnv = "CUB_PLUGIN_AUTO_INSTALL"

// Values of pluginAutoInstallEnv.
const (
	pluginAutoInstallAsk   = "ask"
	pluginAutoInstallAuto  = "auto"
	pluginAutoInstallNever = "never"
)

// pluginIndex is the list of plugins cub knows how to install by command.
type pluginIndex struct {
	Plugins []pluginIndexEntry `json:"plugins"`
}

// pluginIndexEntry describes one known plugin. It holds nothing specific to a
// plugin version, so releasing a plugin never requires changing the index.
type pluginIndexEntry struct {
	// Name is the name the plugin is installed under, and the name
	// `cub plugin install <name>` accepts.
	Name string `json:"name"`
	// Source is what `cub plugin install` is given: a GitHub owner/repo.
	Source string `json:"source"`
	// Commands are the top-level cub commands, and their aliases, that the
	// plugin provides.
	Commands []string `json:"commands"`
	// MinCubVersion is the oldest cub the plugin works with. Empty means any.
	MinCubVersion string `json:"minCubVersion,omitempty"`
	// Prompt says whether cub asks before installing. Unset means ask.
	Prompt *bool `json:"prompt,omitempty"`
}

// asks reports whether the entry wants a confirmation before it is installed.
func (e *pluginIndexEntry) asks() bool {
	return e.Prompt == nil || *e.Prompt
}

// embeddedPluginIndex parses the index this build carries.
//
// Parsing is deliberately not strict: a newer index may carry fields this cub
// does not know, and an entry it can still use should stay usable.
func embeddedPluginIndex() (*pluginIndex, error) {
	var ix pluginIndex
	if err := yaml.Unmarshal(pluginIndexData, &ix); err != nil {
		return nil, fmt.Errorf("invalid plugin index: %w", err)
	}
	return &ix, nil
}

// byCommand finds the plugin that provides a top-level command.
func (ix *pluginIndex) byCommand(token string) *pluginIndexEntry {
	for i := range ix.Plugins {
		if slices.Contains(ix.Plugins[i].Commands, token) {
			return &ix.Plugins[i]
		}
	}
	return nil
}

// byName finds a plugin by the name it is installed under.
func (ix *pluginIndex) byName(name string) *pluginIndexEntry {
	for i := range ix.Plugins {
		if ix.Plugins[i].Name == name {
			return &ix.Plugins[i]
		}
	}
	return nil
}

// pluginAutoInstallMode reads pluginAutoInstallEnv. Empty means the index
// entry decides.
func pluginAutoInstallMode() (string, error) {
	mode := os.Getenv(pluginAutoInstallEnv)
	switch mode {
	case "", pluginAutoInstallAsk, pluginAutoInstallAuto, pluginAutoInstallNever:
		return mode, nil
	}
	return "", fmt.Errorf("%s is %q; it must be %s, %s or %s", pluginAutoInstallEnv, mode,
		pluginAutoInstallAsk, pluginAutoInstallAuto, pluginAutoInstallNever)
}

// Seams for tests: a test neither downloads a plugin nor has a terminal.
var (
	knownPluginInstall    = installPluginFromSource
	pluginStdinIsTerminal = func() bool { return term.IsTerminal(int(os.Stdin.Fd())) }
	pluginPromptIn        = io.Reader(os.Stdin)
)

// installKnownPlugin installs the known plugin that provides the command
// token. known is false when no known plugin provides it, and the caller should
// report the command as unknown. A nil error means the plugin is now installed
// and provides the command.
//
// Everything it prints goes to stderr: the command the user typed runs next,
// and its stdout may be piped somewhere that expects only that command's output.
func installKnownPlugin(token string) (known bool, err error) {
	ix, err := loadPluginIndex()
	if err != nil {
		return false, err
	}
	entry := ix.byCommand(token)
	if entry == nil {
		return false, nil
	}
	manual := fmt.Sprintf("cub plugin install %s", entry.Source)

	// The plugin is installed and still does not provide the command: it is an
	// older release from before the plugin had it.
	if _, statErr := os.Stat(filepath.Join(pluginDir(), entry.Name)); statErr == nil {
		return true, fmt.Errorf("command %q is provided by the plugin %q, but the installed release does not have it; upgrade it with: cub plugin upgrade %s",
			token, entry.Name, entry.Name)
	}

	// A development build has no version to compare.
	cubVersion := "v" + strings.TrimPrefix(Version, "v")
	if entry.MinCubVersion != "" && semver.IsValid(cubVersion) && semver.Compare(cubVersion, entry.MinCubVersion) < 0 {
		return true, fmt.Errorf("command %q is provided by the plugin %s, which needs cub %s or newer; this is cub %s",
			token, entry.Source, entry.MinCubVersion, cubVersion)
	}

	mode, err := pluginAutoInstallMode()
	if err != nil {
		return true, err
	}
	if mode == pluginAutoInstallNever {
		return true, fmt.Errorf("command %q is provided by the plugin %s, which is not installed; install it with: %s",
			token, entry.Source, manual)
	}
	if mode == pluginAutoInstallAsk || (mode == "" && entry.asks()) {
		if !pluginStdinIsTerminal() {
			return true, fmt.Errorf("command %q is provided by the plugin %s, which is not installed, and there is no terminal to ask on; install it with: %s, or set %s=%s",
				token, entry.Source, manual, pluginAutoInstallEnv, pluginAutoInstallAuto)
		}
		fmt.Fprintf(os.Stderr, "%q is provided by the plugin %s. Install it? [Y/n] ", token, entry.Source)
		answer, _ := bufio.NewReader(pluginPromptIn).ReadString('\n')
		switch strings.ToLower(strings.TrimSpace(answer)) {
		case "", "y", "yes":
		default:
			return true, fmt.Errorf("plugin %s not installed", entry.Source)
		}
	} else {
		fmt.Fprintf(os.Stderr, "%q is provided by the plugin %s; installing it...\n", token, entry.Source)
	}

	stdout := os.Stdout
	os.Stdout = os.Stderr
	err = knownPluginInstall(entry.Source, entry.Name)
	os.Stdout = stdout
	if err != nil {
		return true, fmt.Errorf("install the plugin %s: %w", entry.Source, err)
	}

	// Discovery ran before the install, and found nothing for this command.
	pluginCache = nil
	pluginCacheOnce = sync.Once{}
	if _, ok := resolvePluginCommand(token); !ok {
		return true, fmt.Errorf("installed the plugin %s, but its latest release does not provide the command %q", entry.Source, token)
	}
	return true, nil
}

// handleKnownPluginCommand installs the known plugin that provides args[0] and
// runs the command. found is false when no known plugin provides it.
func handleKnownPluginCommand(args []string) (found bool, err error) {
	if len(args) == 0 {
		return false, nil
	}
	known, err := installKnownPlugin(args[0])
	if !known || err != nil {
		return known, err
	}
	return handlePluginCommand(args)
}

// knownPluginSource resolves what was given to `cub plugin install` as the
// name of a known plugin, with an optional @tag. ok is false when it is not one,
// and it is then a source in its own right.
func knownPluginSource(arg string) (source, name string, ok bool) {
	if strings.ContainsAny(arg, "/:") || isLocalPath(arg) {
		return "", "", false
	}
	bare, tag, pinned := strings.Cut(arg, "@")
	ix, err := loadPluginIndex()
	if err != nil {
		return "", "", false
	}
	entry := ix.byName(bare)
	if entry == nil {
		return "", "", false
	}
	source = entry.Source
	if pinned {
		source += "@" + tag
	}
	return source, entry.Name, true
}

// PluginListing is one row of `cub plugin list --all`: an installed plugin, a
// known plugin that is not installed, or both at once.
type PluginListing struct {
	Name     string   `json:"name" yaml:"name"`
	Commands []string `json:"commands,omitempty" yaml:"commands,omitempty"`
	// Source is where the plugin was installed from, or where cub would
	// install it from.
	Source    string `json:"source,omitempty" yaml:"source,omitempty"`
	Installed bool   `json:"installed" yaml:"installed"`
	// Version is the installed release. Empty for a plugin that is not
	// installed, or was not installed from a release.
	Version  string   `json:"version,omitempty" yaml:"version,omitempty"`
	Warnings []string `json:"warnings,omitempty" yaml:"warnings,omitempty"`
}

// listAllPlugins merges the installed plugins with the index of known ones,
// sorted by name. An installed plugin stands for the index entry of the same
// name, or of the source it was installed from.
func listAllPlugins(installed []*Plugin, ix *pluginIndex) []PluginListing {
	var rows []PluginListing
	listed := map[string]bool{}
	for _, p := range installed {
		row := PluginListing{Name: p.Name, Installed: true, Warnings: p.Warnings}
		for _, c := range p.Commands {
			row.Commands = append(row.Commands, commandTokens(c)...)
		}
		if meta, err := readInstallMetadata(p.Path); err == nil {
			row.Source = meta.Source
			row.Version = meta.Tag
		}
		if entry := ix.byName(p.Name); entry != nil {
			listed[entry.Name] = true
		}
		source, _, _ := strings.Cut(row.Source, "@")
		for i := range ix.Plugins {
			if ix.Plugins[i].Source == source {
				listed[ix.Plugins[i].Name] = true
			}
		}
		rows = append(rows, row)
	}
	for _, e := range ix.Plugins {
		if !listed[e.Name] {
			rows = append(rows, PluginListing{Name: e.Name, Commands: e.Commands, Source: e.Source})
		}
	}
	slices.SortStableFunc(rows, func(a, b PluginListing) int { return strings.Compare(a.Name, b.Name) })
	return rows
}

// pluginListAllRun is `cub plugin list --all`.
func pluginListAllRun() error {
	ix, err := loadPluginIndex()
	if err != nil {
		return err
	}
	rows := listAllPlugins(discoverPlugins(), ix)

	if isAlternativeOutput() {
		renderPayload(rows)
		return nil
	}
	if quiet {
		return nil
	}

	table := tableView()
	if !noheader {
		table.SetHeader([]string{"NAME", "COMMANDS", "SOURCE", "INSTALLED", "STATUS"})
	}
	for _, r := range rows {
		installed, status := "-", "-"
		if r.Installed {
			installed, status = "yes", "ok"
			if r.Version != "" {
				installed = r.Version
			}
			if len(r.Warnings) > 0 {
				status = strings.Join(r.Warnings, "; ")
			}
		}
		source := r.Source
		if source == "" {
			source = "-"
		}
		table.Append([]string{r.Name, strings.Join(r.Commands, ", "), source, installed, status})
	}
	table.Render()
	return nil
}
