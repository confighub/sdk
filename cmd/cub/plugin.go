// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/spf13/cobra"
)

// Plugin represents a discovered plugin.
type Plugin struct {
	Name     string   `json:"name" yaml:"name"`
	Path     string   `json:"path" yaml:"path"`
	Warnings []string `json:"warnings,omitempty" yaml:"warnings,omitempty"`
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
		if os.IsNotExist(err) {
			return nil
		}
		return nil
	}

	builtins := getBuiltinCommandNames()
	var plugins []*Plugin

	for _, entry := range entries {
		name := entry.Name()

		// Skip hidden files (e.g. .DS_Store)
		if strings.HasPrefix(name, ".") {
			continue
		}

		p := &Plugin{
			Name: name,
		}

		fullPath := filepath.Join(dir, name)

		if entry.IsDir() {
			// Directory plugin: look for executable "main" inside
			mainPath := filepath.Join(fullPath, "main")
			info, err := os.Stat(mainPath)
			if err != nil {
				p.Path = fullPath
				p.Warnings = append(p.Warnings, "directory plugin missing executable 'main'")
				plugins = append(plugins, p)
				continue
			}
			if !isExecutable(info) {
				p.Path = mainPath
				p.Warnings = append(p.Warnings, "'main' is not executable")
				plugins = append(plugins, p)
				continue
			}
			p.Path = mainPath
		} else {
			// Single file plugin: check executability
			info, err := entry.Info()
			if err != nil {
				p.Path = fullPath
				p.Warnings = append(p.Warnings, fmt.Sprintf("cannot stat: %v", err))
				plugins = append(plugins, p)
				continue
			}
			if !isExecutable(info) {
				p.Path = fullPath
				p.Warnings = append(p.Warnings, "not executable")
				plugins = append(plugins, p)
				continue
			}
			p.Path = fullPath
		}

		// Check if plugin shadows a built-in command
		if builtins[name] {
			p.Warnings = append(p.Warnings, fmt.Sprintf("shadows built-in command %q", name))
		}

		plugins = append(plugins, p)
	}

	return plugins
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

	if hasAlternativeOutput() {
		if jsonOutput {
			displayJSON(plugins)
		}
		if jq != "" {
			outJQ := jq
			jq = outJQ
			displayJQ(plugins)
		}
		if yamlOutput {
			displayYAML(plugins)
		}
		if yq != "" {
			displayYQ(plugins)
		}
		return nil
	}

	if quiet {
		return nil
	}

	if len(plugins) == 0 {
		tprint("No plugins installed.")
		tprint("Install plugins by placing executables in %s", pluginDir())
		return nil
	}

	table := tableView()
	if !noheader {
		table.SetHeader([]string{"NAME", "PATH", "STATUS"})
	}

	for _, p := range plugins {
		status := "ok"
		if len(p.Warnings) > 0 {
			status = strings.Join(p.Warnings, "; ")
		}
		table.Append([]string{p.Name, p.Path, status})
	}

	table.Render()
	return nil
}
