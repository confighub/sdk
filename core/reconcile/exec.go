// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package reconcile

import (
	"bytes"
	"context"
	"io"
	"os"
	"os/exec"
)

// BinaryRunner is a CubRunner that forks the cub binary. Bin defaults to
// "cub" resolved from $PATH. This is the runner the installer uses; a CLI
// that embeds the engine can instead supply an in-process runner.
type BinaryRunner struct {
	Bin string
}

// Run executes cub with args, streaming to stdout/stderr (nil discards).
func (r BinaryRunner) Run(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	bin := r.Bin
	if bin == "" {
		bin = "cub"
	}
	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	return cmd.Run()
}

// capture runs cub with args, returning captured stdout and stderr.
func (e *Engine) capture(ctx context.Context, args []string) (stdout, stderr string, err error) {
	var o, er bytes.Buffer
	err = e.Cub.Run(ctx, args, &o, &er)
	return o.String(), er.String(), err
}

// bodyPath returns a filesystem path holding the Unit body for u, and a
// cleanup func the caller must invoke. When Content is set it is staged to a
// temp file; otherwise Path is returned as-is with a no-op cleanup.
func bodyPath(slug, path string, content []byte) (string, func(), error) {
	if content == nil {
		return path, func() {}, nil
	}
	tmp, err := os.CreateTemp("", "reconcile-body-*-"+slug)
	if err != nil {
		return "", func() {}, err
	}
	cleanup := func() { os.Remove(tmp.Name()) }
	if _, err := tmp.Write(content); err != nil {
		tmp.Close()
		cleanup()
		return "", func() {}, err
	}
	if err := tmp.Close(); err != nil {
		cleanup()
		return "", func() {}, err
	}
	return tmp.Name(), cleanup, nil
}
