// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	coreplugin "github.com/confighub/sdk/core/plugin"
	"github.com/spf13/cobra"
)

// PluginCommand is a single command contributed by a plugin, with its
// entrypoint resolved to an absolute path.
type PluginCommand struct {
	Name       string   `json:"name" yaml:"name"`
	Aliases    []string `json:"aliases,omitempty" yaml:"aliases,omitempty"`
	Summary    string   `json:"summary,omitempty" yaml:"summary,omitempty"`
	Entrypoint string   `json:"entrypoint" yaml:"entrypoint"`
	Args       []string `json:"args,omitempty" yaml:"args,omitempty"`
}

// Plugin represents a discovered plugin — one entry in the plugin directory.
type Plugin struct {
	Name     string          `json:"name" yaml:"name"` // install slot (directory or file name)
	Path     string          `json:"path" yaml:"path"` // directory or file path
	Version  string          `json:"version,omitempty" yaml:"version,omitempty"`
	Commands []PluginCommand `json:"commands,omitempty" yaml:"commands,omitempty"`
	Warnings []string        `json:"warnings,omitempty" yaml:"warnings,omitempty"`
}

var (
	pluginCache     []*Plugin
	pluginCacheOnce sync.Once
)

// pluginDir returns the plugin directory path derived from the context manager's config path.
func pluginDir() string {
	configDir := filepath.Dir(contextManager.configPath)
	return filepath.Join(configDir, "plugins")
}

// discoverPlugins scans the plugin directory and returns discovered plugins.
// Results are cached after the first call.
func discoverPlugins() []*Plugin {
	pluginCacheOnce.Do(func() {
		pluginCache = scanPlugins()
	})
	return pluginCache
}

func scanPlugins() []*Plugin {
	dir := pluginDir()
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}

	builtins := getBuiltinCommandNames()
	var plugins []*Plugin

	for _, entry := range entries {
		name := entry.Name()

		// Skip hidden files (e.g. .DS_Store) and cub-owned metadata.
		if strings.HasPrefix(name, ".") {
			continue
		}

		fullPath := filepath.Join(dir, name)
		p := &Plugin{Name: name, Path: fullPath}

		if entry.IsDir() {
			scanDirectoryPlugin(p, fullPath)
		} else {
			scanFilePlugin(p, fullPath, entry)
		}

		// Warn about commands that shadow a built-in.
		for _, c := range p.Commands {
			for _, tok := range commandTokens(c) {
				if builtins[tok] {
					p.Warnings = append(p.Warnings, fmt.Sprintf("command %q shadows built-in command %q", tok, tok))
				}
			}
		}

		plugins = append(plugins, p)
	}

	return plugins
}

// scanDirectoryPlugin populates p from a directory plugin: a manifest with one
// or more commands, or the legacy single-command form.
func scanDirectoryPlugin(p *Plugin, dirPath string) {
	m, err := coreplugin.Read(dirPath)
	if err != nil {
		p.Warnings = append(p.Warnings, err.Error())
		return
	}

	if m != nil {
		p.Version = m.Version
	}

	if m != nil && len(m.Commands) > 0 {
		for _, c := range m.Commands {
			p.Commands = append(p.Commands, resolveCommand(p, dirPath, c))
		}
		return
	}

	// Legacy single-command directory plugin: entrypoint from manifest or "main".
	ep := "main"
	if m != nil && m.Entrypoint != "" {
		ep = m.Entrypoint
	}
	p.Commands = append(p.Commands, resolveCommand(p, dirPath, coreplugin.Command{
		Name:       p.Name,
		Entrypoint: ep,
	}))
}

// scanFilePlugin populates p from a single-file plugin: one command named after
// the file, whose entrypoint is the file itself.
func scanFilePlugin(p *Plugin, fullPath string, entry os.DirEntry) {
	info, err := entry.Info()
	if err != nil {
		p.Warnings = append(p.Warnings, fmt.Sprintf("cannot stat: %v", err))
		return
	}
	cmd := PluginCommand{Name: p.Name, Entrypoint: fullPath}
	if !isExecutable(info) {
		p.Warnings = append(p.Warnings, fmt.Sprintf("command %q: %q is not executable", p.Name, p.Name))
	}
	p.Commands = append(p.Commands, cmd)
}

// resolveCommand turns a manifest command into a PluginCommand with an absolute
// entrypoint path, recording warnings on the plugin if it cannot be run.
func resolveCommand(p *Plugin, dirPath string, c coreplugin.Command) PluginCommand {
	ep := c.Entrypoint
	if ep == "" {
		ep = "main"
	}
	epPath := filepath.Join(dirPath, ep)
	out := PluginCommand{
		Name:       c.Name,
		Aliases:    c.Aliases,
		Summary:    c.Summary,
		Entrypoint: epPath,
		Args:       c.Args,
	}
	info, err := os.Stat(epPath)
	if err != nil {
		p.Warnings = append(p.Warnings, fmt.Sprintf("command %q: missing executable %q", c.Name, ep))
		out.Entrypoint = ""
		return out
	}
	if info.IsDir() || !isExecutable(info) {
		p.Warnings = append(p.Warnings, fmt.Sprintf("command %q: %q is not executable", c.Name, ep))
		out.Entrypoint = ""
	}
	return out
}

// commandTokens returns the command's name plus aliases.
func commandTokens(c PluginCommand) []string {
	tokens := make([]string, 0, 1+len(c.Aliases))
	if c.Name != "" {
		tokens = append(tokens, c.Name)
	}
	tokens = append(tokens, c.Aliases...)
	return tokens
}

func isExecutable(info os.FileInfo) bool {
	return info.Mode()&0111 != 0
}

// getBuiltinCommandNames returns a set of all top-level command names and aliases on rootCmd.
func getBuiltinCommandNames() map[string]bool {
	names := make(map[string]bool)
	for _, cmd := range rootCmd.Commands() {
		names[cmd.Name()] = true
		for _, alias := range cmd.Aliases {
			names[alias] = true
		}
	}
	return names
}

// installedCommandOwners maps each command token (name or alias) already claimed
// by an installed plugin to the slot of the owning plugin. The given slot is
// excluded, so an upgrade does not collide with its own previous commands.
// It reads fresh from disk rather than the discovery cache.
func installedCommandOwners(excludeSlot string) map[string]string {
	owners := make(map[string]string)
	for _, p := range scanPlugins() {
		if p.Name == excludeSlot {
			continue
		}
		for _, c := range p.Commands {
			for _, tok := range commandTokens(c) {
				owners[tok] = p.Name
			}
		}
	}
	return owners
}

// checkCommandCollisions fails if any of the manifest's command tokens collide
// with a built-in command, another installed plugin's command, or each other.
// excludeSlot is the slot being installed/replaced, excluded from the namespace.
func checkCommandCollisions(excludeSlot string, cmds []coreplugin.Command) error {
	builtins := getBuiltinCommandNames()
	owners := installedCommandOwners(excludeSlot)
	seen := make(map[string]bool)

	for _, c := range cmds {
		tokens := append([]string{c.Name}, c.Aliases...)
		for _, tok := range tokens {
			if tok == "" {
				continue
			}
			if seen[tok] {
				return fmt.Errorf("plugin declares command %q more than once", tok)
			}
			seen[tok] = true
			if builtins[tok] {
				return fmt.Errorf("command %q conflicts with a built-in cub command", tok)
			}
			if owner, ok := owners[tok]; ok {
				return fmt.Errorf("command %q is already provided by plugin %q; uninstall it first or choose a different name", tok, owner)
			}
		}
	}
	return nil
}

// resolvePluginCommand finds the runnable command matching token (name or alias)
// across all installed plugins. When more than one plugin claims a token, the
// match is deterministic by plugin slot name.
func resolvePluginCommand(token string) (*PluginCommand, bool) {
	plugins := discoverPlugins()
	sorted := make([]*Plugin, len(plugins))
	copy(sorted, plugins)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Name < sorted[j].Name })

	for _, p := range sorted {
		for i := range p.Commands {
			c := &p.Commands[i]
			if c.Entrypoint == "" {
				continue
			}
			for _, tok := range commandTokens(*c) {
				if tok == token {
					return c, true
				}
			}
		}
	}
	return nil, false
}

// pluginCmd is the "cub plugin" command group.
var pluginCmd = &cobra.Command{
	Use:   "plugin",
	Short: "Plugin commands",
	Long:  getCommandHelp("Plugin commands", ""),
}

// pluginListCmd is the "cub plugin list" command.
var pluginListCmd = &cobra.Command{
	Use:   "list",
	Short: "List installed plugins",
	Long:  getCommandHelp("List installed plugins", ""),
	RunE:  pluginListCmdRun,
}

func init() {
	addStandardDisplayFlags(pluginListCmd)
	pluginCmd.AddCommand(pluginListCmd)
	rootCmd.AddCommand(pluginCmd)
}

func pluginListCmdRun(cmd *cobra.Command, args []string) error {
	plugins := discoverPlugins()

	if isAlternativeOutput() {
		renderPayload(plugins)
		return nil
	}

	if quiet {
		return nil
	}

	if len(plugins) == 0 {
		tprint("No plugins installed.")
		tprint("Install plugins with 'cub plugin install <source>' or place them in %s", pluginDir())
		return nil
	}

	table := tableView()
	if !noheader {
		table.SetHeader([]string{"NAME", "COMMANDS", "STATUS"})
	}

	for _, p := range plugins {
		var tokens []string
		for _, c := range p.Commands {
			tokens = append(tokens, commandTokens(c)...)
		}
		status := "ok"
		if len(p.Warnings) > 0 {
			status = strings.Join(p.Warnings, "; ")
		}
		table.Append([]string{p.Name, strings.Join(tokens, ", "), status})
	}

	table.Render()
	return nil
}
