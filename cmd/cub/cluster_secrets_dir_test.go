// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"io"
	"os"
	"path/filepath"
	"testing"
)

// With --secrets-dir the Secret goes to a file only its owner can read, the
// kubeconfig is never used, and a rollback removes the file, whose credential
// the rollback revokes.
func TestClusterPutSecretWritesTheFileInsteadOfApplying(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "secrets", "lab")
	s := clusterSecret{namespace: clusterArgobotNamespace, name: clusterArgobotSecret, manifest: []byte("kind: Secret\n")}
	var undos []clusterUndo
	if err := clusterPutSecret(io.Discard, "/nonexistent/kubeconfig", dir, s, func(u clusterUndo) { undos = append(undos, u) }); err != nil {
		t.Fatalf("clusterPutSecret: %v", err)
	}
	path := filepath.Join(dir, "argobot_argobot-secrets.yaml")
	if got := clusterSecretFilePath(dir, s); got != path {
		t.Fatalf("clusterSecretFilePath = %q, want %q", got, path)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("the Secret file: %v", err)
	}
	if mode := info.Mode().Perm(); mode != 0o600 {
		t.Errorf("mode = %o, want 600", mode)
	}
	if got, _ := os.ReadFile(path); string(got) != "kind: Secret\n" {
		t.Errorf("contents = %q, want the manifest", got)
	}
	if len(undos) != 1 || undos[0].usesKubeconfig {
		t.Fatalf("rollbacks = %+v, want one that removes the file", undos)
	}
	if err := undos[0].run(); err != nil {
		t.Fatalf("rollback: %v", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("after rollback, stat = %v, want the file gone", err)
	}
}

// A file written by hand with a looser mode is tightened when it is replaced.
func TestClusterPutSecretTightensAnExistingFile(t *testing.T) {
	dir := t.TempDir()
	s := clusterSecret{namespace: "argocd", name: "confighub-oci-creds", manifest: []byte("new\n")}
	path := clusterSecretFilePath(dir, s)
	if err := os.WriteFile(path, []byte("old\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := clusterPutSecret(io.Discard, "", dir, s, func(clusterUndo) {}); err != nil {
		t.Fatalf("clusterPutSecret: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if mode := info.Mode().Perm(); mode != 0o600 {
		t.Errorf("mode = %o, want 600", mode)
	}
}

// Every cluster's Secrets have the same names, so each cluster has its own
// directory, and no flag means no directory.
func TestClusterSecretsDirFor(t *testing.T) {
	if got := clusterSecretsDirFor("", "lab"); got != "" {
		t.Errorf(`clusterSecretsDirFor("", "lab") = %q, want ""`, got)
	}
	if got, want := clusterSecretsDirFor("/s", "lab"), filepath.Join("/s", "lab"); got != want {
		t.Errorf("clusterSecretsDirFor = %q, want %q", got, want)
	}
}
