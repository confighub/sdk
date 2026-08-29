// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Per-cluster kubeconfig path conventions for `cub cluster`. There is no state
// file: "cub knows about this cluster" is implied by the existence of
// <config-dir>/clusters/<name>.kubeconfig (created by `cub cluster up`, removed
// by `cub cluster down`). All other per-cluster metadata comes from the
// corresponding ConfigHub Space's annotations.

// clusterConfigDir resolves the ConfigHub config directory: CUB_CONFIG if set,
// else ~/.confighub. CUB_CONFIG names the directory, so there is nothing to
// work out from the filesystem -- this used to stat it to tell a directory from
// a config.yaml file, which gave a different answer from the CLI whenever the
// path did not exist yet.
func clusterConfigDir() string {
	if v := os.Getenv("CUB_CONFIG"); v != "" {
		return v
	}
	home, err := os.UserHomeDir()
	if err != nil {
		home = "."
	}
	return filepath.Join(home, ".confighub")
}

// clusterKubeconfigDir returns the directory where cub stores per-cluster kubeconfigs.
func clusterKubeconfigDir() string {
	return filepath.Join(clusterConfigDir(), "clusters")
}

// clusterKubeconfigPath returns the per-cluster kubeconfig path for a name.
func clusterKubeconfigPath(name string) string {
	return filepath.Join(clusterKubeconfigDir(), name+".kubeconfig")
}

// clusterEnvFilePath returns the convenience env file path for a cluster:
// <config-dir>/clusters/<name>.env. Sourced by the user to populate
// KUBECONFIG, ARGOCD_SERVER, and Argo admin credentials.
func clusterEnvFilePath(name string) string {
	return filepath.Join(clusterKubeconfigDir(), name+".env")
}

// clusterLocalNames returns the cluster names cub has tracked locally based on
// kubeconfig files at <config-dir>/clusters/*.kubeconfig.
func clusterLocalNames() ([]string, error) {
	entries, err := os.ReadDir(clusterKubeconfigDir())
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read cluster kubeconfig dir: %w", err)
	}
	var names []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name, ok := strings.CutSuffix(e.Name(), ".kubeconfig")
		if !ok {
			continue
		}
		names = append(names, name)
	}
	return names, nil
}

// clusterEnsureKubeconfigDir creates the kubeconfig directory if missing.
func clusterEnsureKubeconfigDir() error {
	return os.MkdirAll(clusterKubeconfigDir(), 0o755)
}
