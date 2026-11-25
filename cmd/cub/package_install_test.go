// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/confighub/sdk/core/ocibundle"
)

// A package argument is an oci:// reference, a path on this machine, or the
// name of a package ConfigHub publishes, which alone is read anonymously.
func TestResolvePackageRef(t *testing.T) {
	orig := packageNewestRelease
	t.Cleanup(func() { packageNewestRelease = orig })
	var asked string
	packageNewestRelease = func(_ context.Context, repository string, _ ocibundle.Options) (string, error) {
		asked = repository
		return "0.2.0", nil
	}

	dir := t.TempDir()
	tgz := filepath.Join(dir, "app-0.1.0.tgz")
	if err := os.WriteFile(tgz, nil, 0o644); err != nil {
		t.Fatal(err)
	}

	for _, tc := range []struct {
		arg, want string
		public    bool
	}{
		{"oci://example.com/packages/app:1.0.0", "oci://example.com/packages/app:1.0.0", false},
		{dir, dir, false},
		{tgz, tgz, false},
		{"k8sbot:0.1.6", "oci://ghcr.io/confighub/packages/k8sbot:0.1.6", true},
		{"k8sbot", "oci://ghcr.io/confighub/packages/k8sbot:0.2.0", true},
	} {
		asked = ""
		got, public, err := resolvePackageRef(context.Background(), tc.arg)
		if err != nil {
			t.Errorf("%s: %v", tc.arg, err)
			continue
		}
		if got != tc.want || public != tc.public {
			t.Errorf("%s = %q, public %v; want %q, public %v", tc.arg, got, public, tc.want, tc.public)
		}
		if wantAsked := tc.arg == "k8sbot"; wantAsked != (asked != "") {
			t.Errorf("%s: asked for the newest release of %q", tc.arg, asked)
		}
	}

	for _, arg := range []string{"App", "k8sbot@0.1.6"} {
		if _, _, err := resolvePackageRef(context.Background(), arg); err == nil || !strings.Contains(err.Error(), "is not a package") {
			t.Errorf("%s: err = %v, want a refusal naming the forms a package takes", arg, err)
		}
	}
	for _, arg := range []string{"./no-such-dir", "ghcr.io/example/app:1.0.0", "missing.tgz"} {
		if _, _, err := resolvePackageRef(context.Background(), arg); err == nil || !os.IsNotExist(errors.Unwrap(err)) {
			t.Errorf("%s: err = %v, want the path not found", arg, err)
		}
	}

	// A directory named like a published package is not taken for it.
	t.Chdir(dir)
	if err := os.Mkdir("k8sbot", 0o755); err != nil {
		t.Fatal(err)
	}
	if got, public, err := resolvePackageRef(context.Background(), "k8sbot:0.1.6"); err != nil || got != "oci://ghcr.io/confighub/packages/k8sbot:0.1.6" || !public {
		t.Errorf("k8sbot:0.1.6 beside a k8sbot directory = %q, public %v, err %v; want the published package", got, public, err)
	}
	if got, _, err := resolvePackageRef(context.Background(), "k8sbot"); err != nil || got != "oci://ghcr.io/confighub/packages/k8sbot:0.2.0" {
		t.Errorf("k8sbot beside a k8sbot directory = %q, err %v; want the published package", got, err)
	}
	if got, public, err := resolvePackageRef(context.Background(), "./k8sbot"); err != nil || got != filepath.Join(dir, "k8sbot") || public {
		t.Errorf("./k8sbot = %q, public %v, err %v; want the directory", got, public, err)
	}
}

func TestKeyValueFlags(t *testing.T) {
	got, err := keyValueFlags("--input", []string{"a=1", "b=x=y", "c="})
	if err != nil {
		t.Fatal(err)
	}
	if got["a"] != "1" || got["b"] != "x=y" || got["c"] != "" || len(got) != 3 {
		t.Errorf("got %v", got)
	}
	for _, bad := range []string{"novalue", "=1"} {
		if _, err := keyValueFlags("--input", []string{bad}); err == nil || !strings.Contains(err.Error(), "--input") {
			t.Errorf("%q: err = %v, want a refusal naming the flag", bad, err)
		}
	}
}
