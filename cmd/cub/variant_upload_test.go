// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestCubBinaryPathIgnoresPATH is the regression test for runCub resolving its
// child binary by name. It used to fall back to a "cub" on $PATH whenever the
// running binary's basename was not exactly "cub", so a build at bin/cub-dev
// would do its own work but hand every delegated step to whatever release was
// installed. This test runs in precisely that shape: under `go test` the running
// binary is cub.test.
func TestCubBinaryPathIgnoresPATH(t *testing.T) {
	self, err := os.Executable()
	if err != nil {
		t.Skip("os.Executable unavailable on this platform")
	}
	if filepath.Base(self) == "cub" {
		t.Skip("running binary is itself named cub; the $PATH fallback is unobservable here")
	}

	// A decoy cub, first on $PATH. The old resolution would pick this over the
	// running binary; the current one must not look for it at all.
	dir := t.TempDir()
	decoy := filepath.Join(dir, "cub")
	if err := os.WriteFile(decoy, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))

	got := cubBinaryPath()
	if got != self {
		t.Errorf("cubBinaryPath() = %q, want the running binary %q", got, self)
	}
	if got == "cub" || got == decoy {
		t.Errorf("cubBinaryPath() = %q: resolved via $PATH, so delegated work would run a different build", got)
	}
}

func TestUploadSourceDescription(t *testing.T) {
	tests := []struct {
		name   string
		inputs []string
		want   string
	}{
		{
			name:   "oci ref is kept verbatim",
			inputs: []string{"oci://ghcr.io/confighub/configs/cubbychat"},
			want:   "oci://ghcr.io/confighub/configs/cubbychat",
		},
		{
			name:   "stdin is named rather than dashed",
			inputs: []string{"-"},
			want:   "stdin",
		},
		{
			name:   "file and directory paths are kept as given",
			inputs: []string{"./rendered/", "extra.yaml"},
			want:   "./rendered/, extra.yaml",
		},
		{
			name:   "mixed inputs are joined",
			inputs: []string{"oci://ghcr.io/org/bundle", "-"},
			want:   "oci://ghcr.io/org/bundle, stdin",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := uploadSourceDescription(tt.inputs); got != tt.want {
				t.Errorf("uploadSourceDescription(%q) = %q, want %q", tt.inputs, got, tt.want)
			}
		})
	}
}

func TestUploadSourceDescriptionTruncates(t *testing.T) {
	inputs := make([]string, 40)
	for i := range inputs {
		inputs[i] = "oci://ghcr.io/confighub/configs/a-rather-long-bundle-name"
	}
	got := uploadSourceDescription(inputs)
	if len(got) > maxUploadSourceDescription {
		t.Errorf("length = %d, want at most %d", len(got), maxUploadSourceDescription)
	}
	if !strings.HasSuffix(got, "...") {
		t.Errorf("truncated description %q should end in an ellipsis", got)
	}
}
