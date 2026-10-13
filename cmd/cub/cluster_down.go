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
	name         string
	force        bool
	deleteConfig bool
}

var clusterDownCmd = &cobra.Command{
	Use:   "down",
	Short: "Tear down a kind cluster (optionally its ConfigHub config too)",
	Long: `Tears down the live cluster: deletes the kind cluster and removes the local
kubeconfig/env file. By default the ConfigHub side is left in place — the
cluster Space with its OCI target and argobot's Channel, the apps Space holding
the Argo Application Units (or, for a --controller sveltos cluster, the Sveltos
ClusterProfile Units), any deployment Spaces bound to the target, and the
cluster's service account. That config outlives the cluster and can be
re-applied to a new one, so tearing down the live cluster does not throw it
away.

Pass --delete-config to also delete that config: down removes every Space whose
release target is the cluster's OCI target (the apps Space, the argobot variant
Space, and any deployment variant Spaces), then the cluster Space itself, then
the service account. The cluster Space is deleted after the others because ON
DELETE RESTRICT blocks deleting its target while those Spaces still reference
it. This is destructive and includes your deployment variant Spaces — it
deletes their config, not just the live cluster.

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
	clusterDownCmd.Flags().BoolVar(&clusterDownArgs.deleteConfig, "delete-config", false, "also delete the ConfigHub config: the cluster's spaces (target, channel, apps, argobot, and deployment variants bound to the target) and its service account. Destructive.")
	clusterCmd.AddCommand(clusterDownCmd)
}

func clusterDownCmdRun(cmd *cobra.Command, args []string) error {
	if clusterDownArgs.name == "" {
		return fmt.Errorf("--name is required")
	}
	return clusterDownRun(cmd.OutOrStdout(), clusterDownArgs.name, clusterDownArgs.force, clusterDownArgs.deleteConfig)
}

func clusterDownRun(out io.Writer, name string, force, deleteConfig bool) error {
	hostname, _ := os.Hostname()
	kubeconfigPath := clusterKubeconfigPath(name)

	// Confirm the cluster is cub-managed in the current context, and remember its
	// Space so we can report (or, with --delete-config, delete) the config. Absent
	// --delete-config we only read here: down tears down the live cluster and
	// leaves the ConfigHub side in place.
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

	switch {
	case deleteConfig && match == nil:
		fmt.Fprintf(out, "\n--delete-config: no cub-managed Space for %q in the current context; no config to delete.\n", name)
	case deleteConfig:
		if err := clusterDeleteClusterConfig(out, match); err != nil {
			return err
		}
	case match != nil:
		appsSlug, appsKind := match.SpaceSlug+clusterArgoAppsSuffix, "Argo Application Units"
		if match.Controller == clusterControllerSveltos {
			appsSlug, appsKind = match.SpaceSlug+clusterSveltosAppsSuffix, "Sveltos ClusterProfile Units"
		}
		argobotSlug := clusterArgobotComponent + "-" + match.ClusterName
		serviceAccountSlug := clusterServiceAccountSlug(match.ClusterName)
		fmt.Fprintf(out, "\nLeft the ConfigHub config in place: space %q (OCI target + argobot channel),\n"+
			"space %q (%s), the argobot variant space %q (if\n"+
			"installed), any deployment variant spaces you created, and the service\n"+
			"account %q. Delete it all in one step with\n"+
			"'cub cluster down --name %s --delete-config', or by hand — the cluster space\n"+
			"after every other space, since their release targets point cross-space at\n"+
			"its OCI target (deleting the cluster space first is blocked by those\n"+
			"references):\n"+
			"  cub space delete %s --recursive\n"+
			"  cub space delete %s --recursive   # if argobot was installed\n"+
			"  # ...plus any deployment variant spaces bound to %s/%s...\n"+
			"  cub space delete %s --recursive\n"+
			"  cub serviceaccount delete %s\n",
			match.SpaceSlug, appsSlug, appsKind, argobotSlug, serviceAccountSlug, name,
			appsSlug, argobotSlug, match.SpaceSlug, clusterTargetSlug, match.SpaceSlug, serviceAccountSlug)
	}
	fmt.Fprintf(out, "\nDone.\n")
	return nil
}

// clusterDeleteClusterConfig deletes the ConfigHub config a cluster's `cub
// cluster up` created. It deletes the Spaces that reference the cluster's OCI
// target cross-Space (the apps Space, the argobot variant Space, and any
// deployment variant Spaces) first, then the cluster Space that holds the target
// and target — that order is required because ON DELETE RESTRICT blocks deleting
// the target while any Space still references it.
func clusterDeleteClusterConfig(out io.Writer, c *clusterSpace) error {
	deps, err := clusterDependentSpaceSlugs(c.SpaceID)
	if err != nil {
		return err
	}
	fmt.Fprintf(out, "\nDeleting the ConfigHub config for %q...\n", c.SpaceSlug)
	for _, slug := range deps {
		fmt.Fprintf(out, "  deleting space %q...\n", slug)
		if err := clusterDeleteSpace(slug, true); err != nil {
			return fmt.Errorf("delete space %q: %w", slug, err)
		}
	}
	fmt.Fprintf(out, "  deleting cluster space %q (target + channel)...\n", c.SpaceSlug)
	if err := clusterDeleteSpace(c.SpaceSlug, true); err != nil {
		return fmt.Errorf("delete cluster space %q: %w", c.SpaceSlug, err)
	}
	// The service account is org-scoped, so no Space takes it along. Its keys
	// and tokens go with it; a cluster from before it existed has none to delete.
	serviceAccountSlug := clusterServiceAccountSlug(c.ClusterName)
	fmt.Fprintf(out, "  deleting service account %q (if any)...\n", serviceAccountSlug)
	if err := clusterDeleteServiceAccount(serviceAccountSlug); err != nil {
		return fmt.Errorf("delete service account %q: %w", serviceAccountSlug, err)
	}
	return nil
}
