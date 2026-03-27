// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"fmt"
	"os"
	"path/filepath"
	"syscall"
)

// findPlugin resolves the first arg to a plugin by name.
// It returns the resolved executable path and the remaining arguments to pass to the plugin.
func findPlugin(args []string) (path string, remainingArgs []string, err error) {
	if len(args) == 0 {
		return "", nil, fmt.Errorf("no command specified")
	}

	dir := pluginDir()
	name := args[0]

	// Check for single-file plugin
	filePath := filepath.Join(dir, name)
	if info, err := os.Stat(filePath); err == nil && !info.IsDir() {
		if isExecutable(info) {
			return filePath, args[1:], nil
		}
	}

	// Check for directory plugin with "main" entry point
	mainPath := filepath.Join(dir, name, "main")
	if info, err := os.Stat(mainPath); err == nil && !info.IsDir() {
		if isExecutable(info) {
			return mainPath, args[1:], nil
		}
	}

	return "", nil, fmt.Errorf("unknown command %q for \"cub\"", name)
}

// pluginEnv constructs the CUB_* environment variables for a plugin process.
func pluginEnv() []string {
	env := os.Environ()

	env = append(env, "CUB_PLUGIN=1")
	env = append(env, fmt.Sprintf("CUB_CONFIG=%s", filepath.Dir(contextManager.configPath)))

	activeCtx := contextManager.ActiveContext()
	env = append(env, fmt.Sprintf("CUB_CONTEXT=%s", activeCtx.Name))
	env = append(env, fmt.Sprintf("CUB_SERVER=%s", activeCtx.Coordinate.ServerURL))

	if activeCtx.Settings.DefaultSpace != "" {
		env = append(env, fmt.Sprintf("CUB_SPACE=%s", activeCtx.Settings.DefaultSpace))
	}

	tokenData, err := contextManager.LoadTokenData(activeCtx)
	if err == nil && tokenData.AccessToken != "" {
		env = append(env, fmt.Sprintf("CUB_TOKEN=%s", tokenData.AccessToken))
	}

	return env
}

// handlePluginCommand attempts to resolve and execute a plugin from the given args.
// It is called from main() when Cobra reports an unknown command.
func handlePluginCommand(args []string) error {
	pluginPath, remainingArgs, err := findPlugin(args)
	if err != nil {
		return err
	}

	env := pluginEnv()
	return execPlugin(pluginPath, remainingArgs, env)
}

// execPlugin replaces the current process with the plugin executable via syscall.Exec.
func execPlugin(path string, args, env []string) error {
	argv := append([]string{path}, args...)
	return syscall.Exec(path, argv, env)
}
