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
	"gopkg.in/yaml.v3"
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
			// Directory plugin: resolve entry point from manifest or fall back to "main"
			ep, epErr := resolveEntrypoint(fullPath)
			if epErr != nil {
				p.Path = fullPath
				p.Warnings = append(p.Warnings, epErr.Error())
				plugins = append(plugins, p)
				continue
			}
			epPath := filepath.Join(fullPath, ep)
			info, err := os.Stat(epPath)
			if err != nil {
				p.Path = fullPath
				p.Warnings = append(p.Warnings, fmt.Sprintf("directory plugin missing executable %q", ep))
				plugins = append(plugins, p)
				continue
			}
			if !isExecutable(info) {
				p.Path = epPath
				p.Warnings = append(p.Warnings, fmt.Sprintf("%q is not executable", ep))
				plugins = append(plugins, p)
				continue
			}
			p.Path = epPath
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

const pluginManifestFile = "cub-plugin.yaml"

// pluginManifest represents the contents of a cub-plugin.yaml file.
type pluginManifest struct {
	Entrypoint string `yaml:"entrypoint"`
}

// readPluginManifest reads and parses a cub-plugin.yaml from a directory.
// Returns nil if the file does not exist.
func readPluginManifest(dir string) (*pluginManifest, error) {
	data, err := os.ReadFile(filepath.Join(dir, pluginManifestFile))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var m pluginManifest
	if err := yaml.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("invalid %s: %w", pluginManifestFile, err)
	}
	return &m, nil
}

// resolveEntrypoint determines the entry point for a directory plugin.
// It checks cub-plugin.yaml first, then falls back to "main".
// Returns an error if the manifest exists but is malformed.
func resolveEntrypoint(dir string) (string, error) {
	m, err := readPluginManifest(dir)
	if err != nil {
		return "", err
	}
	if m != nil && m.Entrypoint != "" {
		return m.Entrypoint, nil
	}
	return "main", nil
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

	if isAlternativeOutput() {
		renderPayload(plugins)
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
