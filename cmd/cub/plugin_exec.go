// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"time"

	coreplugin "github.com/confighub/sdk/core/plugin"
)

// pluginHookTimeout bounds how long an install/upgrade hook may run. Exceeding
// it kills the hook; the install then proceeds with the default manifest.
const pluginHookTimeout = 30 * time.Second

// findPlugin resolves the first arg to a plugin command by name or alias.
// It returns the resolved executable path and the arguments to pass to it
// (the command's args prefix followed by the remaining user arguments).
func findPlugin(args []string) (path string, remainingArgs []string, err error) {
	if len(args) == 0 {
		return "", nil, fmt.Errorf("no command specified")
	}

	token := args[0]
	cmd, ok := resolvePluginCommand(token)
	if !ok {
		return "", nil, fmt.Errorf("unknown command %q for \"cub\"", token)
	}

	remaining := make([]string, 0, len(cmd.Args)+len(args)-1)
	remaining = append(remaining, cmd.Args...)
	remaining = append(remaining, args[1:]...)
	return cmd.Entrypoint, remaining, nil
}

// pluginEnv constructs the CUB_* environment variables for a plugin process.
func pluginEnv() []string {
	env := os.Environ()

	env = append(env, "CUB_PLUGIN=1")
	env = append(env, fmt.Sprintf("CUB_CONFIG=%s", filepath.Dir(contextManager.ConfigPath())))

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

// runHook invokes binPath as an install/upgrade hook in dir. It is best-effort:
// the exit code is ignored (success is determined by whether a manifest results),
// and a hook that hangs is killed after pluginHookTimeout. The hook's output is
// surfaced on stderr but never parsed.
//
// A hook gets the same environment as a normal command invocation, plus the
// hook-specific variables.
func runHook(binPath, dir, phase, prevVersion string) {
	ctx, cancel := context.WithTimeout(context.Background(), pluginHookTimeout)
	defer cancel()

	env := append(pluginEnv(),
		coreplugin.EnvHook+"="+phase,
		coreplugin.EnvDir+"="+dir,
	)
	if prevVersion != "" {
		env = append(env, coreplugin.EnvPreviousVersion+"="+prevVersion)
	}

	cmd := exec.CommandContext(ctx, binPath)
	cmd.Dir = dir
	cmd.Env = env
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr
	_ = cmd.Run()
}

// handlePluginCommand attempts to resolve and execute a plugin from the given
// args. The returned found flag distinguishes "no such command" (caller should
// show the original error) from a command that resolved but failed to exec.
func handlePluginCommand(args []string) (found bool, err error) {
	pluginPath, remainingArgs, ferr := findPlugin(args)
	if ferr != nil {
		return false, ferr
	}

	env := pluginEnv()
	execErr := execPlugin(pluginPath, remainingArgs, env)
	// execPlugin only returns when the exec itself failed.
	if errors.Is(execErr, syscall.ENOEXEC) {
		return true, fmt.Errorf("plugin command %q: its binary cannot execute on this platform (wrong architecture or not a valid executable)", args[0])
	}
	return true, fmt.Errorf("plugin command %q: %w", args[0], execErr)
}

// execPlugin replaces the current process with the plugin executable via syscall.Exec.
func execPlugin(path string, args, env []string) error {
	argv := append([]string{path}, args...)
	return syscall.Exec(path, argv, env)
}
