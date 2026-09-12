// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// Several commands do part of their work by running cub again rather than
// re-implementing what another command already does: "cub variant upload",
// "cub variant create", "cub variant promote", and "cub cluster up" all do.
// Self-invocation goes through the two helpers here.
//
// The context travels with the environment. The parent exports the context it
// resolved as $CUB_CONTEXT (applyContextOverride, main.go) and the child reads
// it as an implicit --context, so parent and child always agree on the server,
// organization, and user.

// cubBinaryPath returns the binary that runCub should invoke: this same running
// executable, wherever it lives and whatever it is called.
//
// It deliberately never consults $PATH for a binary named "cub". Resolving by
// name means a locally built or renamed binary delegates half its work to some
// other cub that happens to be installed — a `bin/cub-dev` doing its own space
// creation but handing unit creation to last month's release. The resulting
// version skew is invisible at the call site and surfaces as behaviour that
// looks like the server's.
//
// The bare "cub" it returns when os.Executable fails is the sole exception, and
// the only case where no self-reference is available at all: that call fails
// only where the OS cannot report the running binary, and resolving through
// $PATH there beats refusing to run. This function always returns something
// runnable, so callers never have to decide what an unresolved binary means.
func cubBinaryPath() string {
	bin, err := os.Executable()
	if err != nil || bin == "" {
		return "cub"
	}
	return bin
}

// runCub executes the cub binary (this same CLI) as a subprocess, streaming its
// output. Side effects go through cub so the caller reuses its create/link
// semantics without re-implementing them.
func runCub(args ...string) error {
	c := exec.Command(cubBinaryPath(), args...)
	c.Stdout = os.Stdout
	c.Stderr = os.Stderr
	if err := c.Run(); err != nil {
		return fmt.Errorf("cub %s: %w", strings.Join(args, " "), err)
	}
	return nil
}
