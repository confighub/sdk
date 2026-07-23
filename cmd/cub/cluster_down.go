// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"fmt"
	"io"
	"os"

	"github.com/spf13/cobra"
)

var clusterDownArgs struct {
	name  string
	force bool
}

var clusterDownCmd = &cobra.Command{
	Use:   "down",
	Short: "Tear down a kind cluster, leaving its ConfigHub config in place",
	Long: `Tears down the live cluster: deletes the kind cluster and removes the local
kubeconfig/env file. The ConfigHub side is deliberately left in place — the
cluster Space with its worker and OCI target, and any deployment Spaces bound
to it. That config outlives the cluster and can be re-applied to a new one, so
tearing down the live cluster does not throw it away.

Deleting the config is a separate, explicit act (e.g. cub space delete
--recursive), not something down does for you. A future flag may fold it in
once the use case is clearer.

Looks up the cluster's Space in the current cub context (via the
confighub.com/cluster-name annotation) to confirm the cluster is cub-managed
and to report what it leaves behind. If no matching Space is found, fails with
an "are you in the right context?" hint; pass --force to delete the local kind
cluster + kubeconfig anyway.`,
	RunE: clusterDownCmdRun,
}

func init() {
	clusterDownCmd.Flags().StringVar(&clusterDownArgs.name, "name", "", "cluster name (required)")
	clusterDownCmd.Flags().BoolVar(&clusterDownArgs.force, "force", false, "delete the local kind cluster + kubeconfig even if no matching ConfigHub Space is found in the current context")
	clusterCmd.AddCommand(clusterDownCmd)
}

func clusterDownCmdRun(cmd *cobra.Command, args []string) error {
	if clusterDownArgs.name == "" {
		return fmt.Errorf("--name is required")
	}
	return clusterDownRun(cmd.OutOrStdout(), clusterDownArgs.name, clusterDownArgs.force)
}

func clusterDownRun(out io.Writer, name string, force bool) error {
	hostname, _ := os.Hostname()
	kubeconfigPath := clusterKubeconfigPath(name)

	// Confirm the cluster is cub-managed in the current context, and remember its
	// Space so we can point at the config left behind. We only read here: down
	// tears down the live cluster, never the ConfigHub side.
	spaces, err := clusterListSpacesForHost(hostname)
	if err != nil {
		return fmt.Errorf("look up cluster spaces: %w", err)
	}
	var match *clusterSpace
	for i := range spaces {
		if spaces[i].ClusterName == name {
			match = &spaces[i]
			break
		}
	}
	if match == nil && !force {
		return fmt.Errorf("no cub-managed Space for %q in the current cub context (are you in the right context?). Pass --force to delete the local kind cluster + kubeconfig anyway", name)
	}

	fmt.Fprintf(out, "Deleting kind cluster %q...\n", name)
	if err := clusterKindDelete(ctx, name, kubeconfigPath, out); err != nil {
		fmt.Fprintf(out, "  warning: %v\n", err)
	}

	if err := os.Remove(kubeconfigPath); err != nil && !os.IsNotExist(err) {
		fmt.Fprintf(out, "  warning: removing kubeconfig: %v\n", err)
	}
	envFilePath := clusterEnvFilePath(name)
	if err := os.Remove(envFilePath); err != nil && !os.IsNotExist(err) {
		fmt.Fprintf(out, "  warning: removing env file: %v\n", err)
	}

	if match != nil {
		fmt.Fprintf(out, "\nLeft the ConfigHub config in place: space %q (worker, OCI target,\n"+
			"and any deployment spaces bound to it). Delete it explicitly when you want\n"+
			"it gone, e.g.:\n  cub space delete %s --recursive\n", match.SpaceSlug, match.SpaceSlug)
	}
	fmt.Fprintf(out, "\nDone.\n")
	return nil
}
