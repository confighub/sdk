// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// OS clipboard support, for commands that hand the user a secret to paste rather
// than echo it to the terminal — today `cub cluster open`, which puts the Argo CD
// admin password on the clipboard.
//
// We shell out to the platform's clipboard tool instead of taking a clipboard
// library dependency: the cub cluster commands already shell out to
// kind/kubectl/docker, and the X11/Wayland paths would otherwise pull cgo into
// the CLI build. Plenty of environments have no clipboard at all (containers,
// ssh sessions, CI), so copying is best-effort and every caller must handle the
// error by falling back to printing the value.

// clipboardCopyTimeout bounds the shell-out so a misbehaving clipboard tool
// cannot wedge the command.
const clipboardCopyTimeout = 5 * time.Second

// clipboardCopy writes s to the OS clipboard.
func clipboardCopy(ctx context.Context, s string) error {
	argv, tried := clipboardCommand(runtime.GOOS, os.Getenv)
	if argv == nil {
		return fmt.Errorf("no clipboard tool found on PATH (tried %s)", strings.Join(tried, ", "))
	}
	ctx, cancel := context.WithTimeout(ctx, clipboardCopyTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
	cmd.Stdin = strings.NewReader(s)
	// Stdout/Stderr are deliberately left unset (so they go to /dev/null): xclip
	// and wl-copy fork a process that stays alive holding the selection, and
	// capturing their output would make Wait block on that child's lifetime
	// instead of the parent's exit.
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%s: %w", filepath.Base(argv[0]), err)
	}
	return nil
}

// clipboardCommand resolves the first clipboard candidate present on PATH,
// returning its full argv (with argv[0] resolved to an absolute path) or nil,
// plus the names it tried in order.
func clipboardCommand(goos string, env func(string) string) (argv []string, tried []string) {
	for _, c := range clipboardCandidates(goos, env) {
		tried = append(tried, c[0])
		if path, err := exec.LookPath(c[0]); err == nil {
			return append([]string{path}, c[1:]...), tried
		}
	}
	return nil, tried
}

// clipboardCandidates returns the clipboard-writing commands to try, in
// preference order, for a GOOS and environment. Each reads the new clipboard
// contents from stdin. Split out from clipboardCommand so the ordering is
// testable without the tools installed.
func clipboardCandidates(goos string, env func(string) string) [][]string {
	switch goos {
	case "darwin":
		return [][]string{{"pbcopy"}}
	case "windows":
		return [][]string{{"clip"}}
	default:
		x11 := [][]string{
			{"xclip", "-selection", "clipboard"},
			{"xsel", "--clipboard", "--input"},
		}
		// wl-copy is the only one of these that talks to a Wayland compositor,
		// so prefer it when the session is Wayland; keep it as a fallback
		// otherwise, since XWayland can make DISPLAY misleading. clip.exe covers
		// WSL, where no Linux clipboard tool is installed but the Windows one is
		// on PATH.
		wayland := [][]string{{"wl-copy"}}
		wsl := [][]string{{"clip.exe"}}
		if env("WAYLAND_DISPLAY") != "" {
			return append(append(wayland, x11...), wsl...)
		}
		return append(append(x11, wayland...), wsl...)
	}
}
