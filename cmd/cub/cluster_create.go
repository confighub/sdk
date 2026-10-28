// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"fmt"
	"io"
	"os/exec"

	"github.com/spf13/cobra"
)

var clusterCreateArgs struct {
	mounts     []string
	noPorts    bool
	kubeconfig string
}

var clusterCreateCmd = &cobra.Command{
	Use:   "create <name>",
	Args:  cobra.ExactArgs(1),
	Short: "Create a local kind cluster, without wiring it into ConfigHub",
	Long: `Creates a local kind cluster: the first step of "cub cluster up", on its own.
Nothing is created in ConfigHub; "cub cluster attach <name> --provider kind"
wires the cluster in afterwards.

The kind node gets a window of host ports mapped through, free for your own
NodePort Services; --no-ports maps none. The window is recorded in the node's
confighub.com/cluster-port-range label. "cub cluster attach --provider kind"
reads it, and when it installs Argo CD or Sveltos it puts the UI on the
window's first port, as "cub cluster up" does.

The kubeconfig goes where kind puts it: merged into $KUBECONFIG or
~/.kube/config as context kind-<name>, or into the file --kubeconfig names.

Use --mount HOST[:CONTAINER] (repeatable) to bind-mount host directories
into the cluster node.

The node is also labeled confighub.com/cluster-origin=kind, so once attached
the cluster is one "cub cluster down" deletes, as if "cub cluster up" had made
it. Before it is attached, delete it with "kind delete cluster --name <name>".

Examples:
  # Create a cluster, then wire it into ConfigHub through Flux
  cub cluster create lab
  cub cluster attach lab --provider kind --controller flux`,
	RunE: clusterCreateCmdRun,
}

func init() {
	clusterCreateCmd.Flags().StringArrayVar(&clusterCreateArgs.mounts, "mount", nil, "host:container bind mount (repeatable; container path defaults to /mnt/<basename>)")
	clusterCreateCmd.Flags().BoolVar(&clusterCreateArgs.noPorts, "no-ports", false, "map no host ports to the node")
	clusterCreateCmd.Flags().StringVar(&clusterCreateArgs.kubeconfig, "kubeconfig", "", "file to write the cluster's kubeconfig to (defaults to kind's: $KUBECONFIG or ~/.kube/config)")
	clusterCmd.AddCommand(clusterCreateCmd)
}

func clusterCreateCmdRun(cmd *cobra.Command, args []string) error {
	mounts, err := clusterParseMounts(clusterCreateArgs.mounts)
	if err != nil {
		return err
	}
	out := cmd.OutOrStdout()
	result, _, err := clusterCreateKind(out, clusterCreateSpec{
		name:           args[0],
		kubeconfigPath: clusterCreateArgs.kubeconfig,
		mounts:         mounts,
		noPorts:        clusterCreateArgs.noPorts,
	})
	if err != nil {
		return err
	}
	fmt.Fprintf(out, "\nCreated kind cluster %q (kube context kind-%s).\n", args[0], args[0])
	if result.windowSize > 0 {
		fmt.Fprintf(out, "User NodePort window: %s (attaching with Argo CD or Sveltos puts its UI on the first)\n", result.portRange())
	}
	clusterPrintMounts(out, mounts)
	fmt.Fprintf(out, "\nWire it into ConfigHub with:\n  cub cluster attach %s --provider kind\n", args[0])
	return nil
}

// clusterCreateSpec is what creating a kind cluster needs.
type clusterCreateSpec struct {
	name string
	// kubeconfigPath is the file kind writes the cluster's kubeconfig to; ""
	// leaves it to kind, which merges into $KUBECONFIG or ~/.kube/config.
	kubeconfigPath string
	mounts         []clusterMount
	noPorts        bool
	// uiPort reserves the window's first port for the controller's UI, which
	// --no-ports keeps.
	uiPort bool
}

// clusterCreateResult is the host port window clusterCreateKind mapped to the
// node.
type clusterCreateResult struct {
	startPort  int
	windowSize int
	// uiPort is the port reserved for the controller's UI, 0 when none was.
	uiPort int
}

// portRange renders the window as "<first>-<last>", or "" when it is empty.
func (r clusterCreateResult) portRange() string {
	if r.windowSize == 0 {
		return ""
	}
	return fmt.Sprintf("%d-%d", r.startPort, r.startPort+r.windowSize-1)
}

// clusterCreateKind creates a kind cluster with a free window of host ports
// mapped to its node. The returned function deletes the cluster again, for a
// caller that fails after this succeeded.
func clusterCreateKind(out io.Writer, spec clusterCreateSpec) (clusterCreateResult, func(), error) {
	var result clusterCreateResult
	if err := clusterKindAvailable(); err != nil {
		return result, nil, err
	}
	if _, err := exec.LookPath("kubectl"); err != nil {
		return result, nil, fmt.Errorf("kubectl not found on PATH")
	}
	if exists, err := clusterKindExists(ctx, spec.name); err != nil {
		return result, nil, err
	} else if exists {
		return result, nil, fmt.Errorf("kind cluster %q already exists", spec.name)
	}

	result.windowSize = clusterPortRangeSize
	if spec.noPorts {
		result.windowSize = 0
		if spec.uiPort {
			result.windowSize = 1
		}
	}
	var ports []clusterPortMapping
	if result.windowSize > 0 {
		bound, err := clusterBoundHostPorts(ctx)
		if err != nil {
			return result, nil, fmt.Errorf("probe docker for bound ports: %w", err)
		}
		result.startPort, err = clusterPickFreePortWindow(bound, clusterPortRangeStart, clusterPortRangeEnd, result.windowSize)
		if err != nil {
			return result, nil, err
		}
		if spec.uiPort {
			result.uiPort = result.startPort
		}
		ports = make([]clusterPortMapping, 0, result.windowSize)
		for p := result.startPort; p < result.startPort+result.windowSize; p++ {
			ports = append(ports, clusterPortMapping{HostPort: p, ContainerPort: p})
		}
	}

	if spec.kubeconfigPath != "" {
		fmt.Fprintf(out, "Creating kind cluster %q (kubeconfig: %s)...\n", spec.name, spec.kubeconfigPath)
	} else {
		fmt.Fprintf(out, "Creating kind cluster %q...\n", spec.name)
	}
	// The window, and that cub created the cluster, are recorded on the node,
	// so attaching the cluster later finds them with nothing but the
	// kubeconfig: the controller's UI goes on the window, and the cluster is
	// one `cub cluster down` deletes, as if `cub cluster up` had made it.
	nodeLabels := map[string]string{clusterAnnotationOrigin: clusterOriginKind}
	if r := result.portRange(); r != "" {
		nodeLabels[clusterAnnotationPortRange] = r
	}
	if _, err := clusterKindCreate(ctx, spec.name, spec.kubeconfigPath, ports, spec.mounts, nodeLabels, out); err != nil {
		return result, nil, err
	}
	undo := func() {
		fmt.Fprintf(out, "Rolling back: kind delete cluster %q\n", spec.name)
		_ = clusterKindDelete(ctx, spec.name, spec.kubeconfigPath, out)
	}
	return result, undo, nil
}

// clusterPrintMounts lists the host mounts a cluster was created with.
func clusterPrintMounts(out io.Writer, mounts []clusterMount) {
	if len(mounts) == 0 {
		return
	}
	fmt.Fprintf(out, "\nHost mounts (host → node, accessible to pods via hostPath):\n")
	for _, m := range mounts {
		fmt.Fprintf(out, "  %s → %s\n", m.HostPath, m.ContainerPath)
	}
}
