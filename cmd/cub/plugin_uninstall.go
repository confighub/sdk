// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
)

var pluginUninstallCmd = &cobra.Command{
	Use:   "uninstall <name>",
	Short: "Uninstall a plugin",
	Long:  getCommandHelp("Uninstall a plugin", "Removes the plugin file or directory from the plugins directory."),
	Args:  cobra.ExactArgs(1),
	RunE:  pluginUninstallCmdRun,
}

func init() {
	pluginCmd.AddCommand(pluginUninstallCmd)
}

func pluginUninstallCmdRun(cmd *cobra.Command, args []string) error {
	name := args[0]

	dir := pluginDir()
	pluginPath := filepath.Join(dir, name)

	info, err := os.Stat(pluginPath)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("plugin %q is not installed", name)
		}
		return fmt.Errorf("failed to stat plugin: %w", err)
	}

	if info.IsDir() {
		if err := os.RemoveAll(pluginPath); err != nil {
			return fmt.Errorf("failed to remove plugin directory: %w", err)
		}
	} else {
		if err := os.Remove(pluginPath); err != nil {
			return fmt.Errorf("failed to remove plugin: %w", err)
		}
	}

	tprint("Uninstalled plugin %q", name)
	return nil
}
