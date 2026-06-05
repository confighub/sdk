// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"slices"
	"strings"
)

// kind + kubectl shell-outs for `cub cluster`. We shell out because kind has
// no Go library API designed for external consumers, and kubectl is the
// simplest way to apply the handful of bootstrap manifests.

// clusterPortMapping is a host→container port pair on the kind control-plane node.
type clusterPortMapping struct {
	HostPort      int
	ContainerPort int
	Protocol      string // "TCP" or "UDP"; empty = TCP
}

// clusterMount is a host→node bind mount (kind extraMounts). Pods can then
// reach the host path via a hostPath volume on ContainerPath.
type clusterMount struct {
	HostPath      string
	ContainerPath string
}

// clusterKindAvailable returns an error if kind is not on PATH.
func clusterKindAvailable() error {
	if _, err := exec.LookPath("kind"); err != nil {
		return fmt.Errorf("kind not found on PATH; install from https://kind.sigs.k8s.io")
	}
	return nil
}

// clusterKindCreate runs `kind create cluster` with a generated config that
// opens the given port mappings and host mounts on the control-plane node.
// kind writes the cluster's kubeconfig to the given path (rather than merging
// into ~/.kube/config) so each cluster's credentials stay isolated. Returns
// the kube context name kind wires up (`kind-<name>`).
func clusterKindCreate(ctx context.Context, name, kubeconfigPath string, ports []clusterPortMapping, mounts []clusterMount, out io.Writer) (string, error) {
	configPath, err := clusterWriteKindConfig(name, ports, mounts)
	if err != nil {
		return "", err
	}
	defer os.Remove(configPath)

	cmd := exec.CommandContext(ctx, "kind", "create", "cluster",
		"--name", name,
		"--kubeconfig", kubeconfigPath,
		"--config", configPath,
	)
	cmd.Stdout = out
	cmd.Stderr = out
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("kind create cluster: %w", err)
	}
	return "kind-" + name, nil
}

// clusterWriteKindConfig generates a kind v1alpha4 cluster config to a temp
// file and returns its path. The caller removes the file.
func clusterWriteKindConfig(name string, ports []clusterPortMapping, mounts []clusterMount) (string, error) {
	var b strings.Builder
	b.WriteString("kind: Cluster\n")
	b.WriteString("apiVersion: kind.x-k8s.io/v1alpha4\n")
	b.WriteString("name: ")
	b.WriteString(name)
	b.WriteString("\n")
	b.WriteString("nodes:\n")
	b.WriteString("- role: control-plane\n")
	if len(ports) > 0 {
		b.WriteString("  extraPortMappings:\n")
		for _, p := range ports {
			proto := p.Protocol
			if proto == "" {
				proto = "TCP"
			}
			fmt.Fprintf(&b, "  - hostPort: %d\n    containerPort: %d\n    protocol: %s\n", p.HostPort, p.ContainerPort, proto)
		}
	}
	if len(mounts) > 0 {
		b.WriteString("  extraMounts:\n")
		for _, m := range mounts {
			fmt.Fprintf(&b, "  - hostPath: %s\n    containerPath: %s\n", m.HostPath, m.ContainerPath)
		}
	}

	f, err := os.CreateTemp("", "cluster-kind-config-*.yaml")
	if err != nil {
		return "", err
	}
	if _, err := f.WriteString(b.String()); err != nil {
		f.Close()
		os.Remove(f.Name())
		return "", err
	}
	if err := f.Close(); err != nil {
		os.Remove(f.Name())
		return "", err
	}
	return f.Name(), nil
}

// clusterKindDelete runs `kind delete cluster --name <name>`. kubeconfigPath,
// if set, scopes the credential cleanup to the dedicated file.
func clusterKindDelete(ctx context.Context, name, kubeconfigPath string, out io.Writer) error {
	args := []string{"delete", "cluster", "--name", name}
	if kubeconfigPath != "" {
		args = append(args, "--kubeconfig", kubeconfigPath)
	}
	cmd := exec.CommandContext(ctx, "kind", args...)
	cmd.Stdout = out
	cmd.Stderr = out
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("kind delete cluster: %w", err)
	}
	return nil
}

// clusterKindList returns the names of all kind clusters on the host (any
// source — cub-managed or not).
func clusterKindList(ctx context.Context) ([]string, error) {
	out, err := exec.CommandContext(ctx, "kind", "get", "clusters").Output()
	if err != nil {
		return nil, fmt.Errorf("kind get clusters: %w", err)
	}
	var names []string
	for line := range strings.SplitSeq(strings.TrimSpace(string(out)), "\n") {
		l := strings.TrimSpace(line)
		if l == "" || l == "No kind clusters found." {
			continue
		}
		names = append(names, l)
	}
	return names, nil
}

// clusterKindExists reports whether a kind cluster with the given name exists.
func clusterKindExists(ctx context.Context, name string) (bool, error) {
	clusters, err := clusterKindList(ctx)
	if err != nil {
		return false, err
	}
	return slices.Contains(clusters, name), nil
}

// clusterKubectlApply pipes the manifest to `kubectl --kubeconfig <path> apply -f -`.
func clusterKubectlApply(ctx context.Context, kubeconfigPath string, manifest []byte, out io.Writer) error {
	return clusterKubectl(ctx, kubeconfigPath, manifest, out, "apply", "-f", "-")
}

// clusterKubectl runs `kubectl --kubeconfig <path> <args...>` with optional
// stdin and pipes stdout+stderr to out.
func clusterKubectl(ctx context.Context, kubeconfigPath string, stdin []byte, out io.Writer, args ...string) error {
	if _, err := exec.LookPath("kubectl"); err != nil {
		return fmt.Errorf("kubectl not found on PATH")
	}
	full := append([]string{"--kubeconfig", kubeconfigPath}, args...)
	cmd := exec.CommandContext(ctx, "kubectl", full...)
	if stdin != nil {
		cmd.Stdin = bytes.NewReader(stdin)
	}
	cmd.Stdout = out
	cmd.Stderr = out
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("kubectl %s: %w", strings.Join(args, " "), err)
	}
	return nil
}

// clusterKubectlOutput runs kubectl and returns stdout. stderr goes nowhere on
// success; on failure it's included in the error.
func clusterKubectlOutput(ctx context.Context, kubeconfigPath string, args ...string) ([]byte, error) {
	if _, err := exec.LookPath("kubectl"); err != nil {
		return nil, fmt.Errorf("kubectl not found on PATH")
	}
	full := append([]string{"--kubeconfig", kubeconfigPath}, args...)
	var stdout, stderr bytes.Buffer
	cmd := exec.CommandContext(ctx, "kubectl", full...)
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("kubectl %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	return stdout.Bytes(), nil
}
