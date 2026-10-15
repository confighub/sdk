// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"slices"

	"github.com/spf13/cobra"
)

var clusterDetachArgs struct {
	name         string
	force        bool
	deleteConfig bool
}

var clusterDetachCmd = &cobra.Command{
	Use:   "detach <name>",
	Args:  cobra.MaximumNArgs(1),
	Short: "Remove a cluster's ConfigHub wiring and leave it running",
	Long: `The inverse of "cub cluster attach". The cluster is left running and Argo CD
left installed; what goes is the wiring:

  - every Argo Application that came from the apps Space — the root app-of-apps
    and the child Applications — is deleted WITHOUT cascading, so the workloads
    they deployed stay;
  - argobot, cub's own bot, is deleted with its resources;
  - the registry credentials Secret is deleted;
  - the local kubeconfig, env file and record are removed.

By default the ConfigHub side is left in place — the cluster Space with its OCI
target and argobot's Channel, the apps Space holding the Argo Application
Units, any deployment Spaces bound to the target, and the cluster's service
account. That config outlives the attachment and can be re-attached to a
cluster later, so detaching does not throw it away.

Pass --delete-config to also delete that config: detach removes every Space
whose release target is the cluster's OCI target (the apps Space, the argobot
variant Space, and any deployment variant Spaces), then the cluster Space
itself, then the service account. This is destructive and includes your
deployment variant Spaces — it deletes their config, not just the wiring.

The cluster's record (written beside its kubeconfig by attach) says which
ConfigHub server and organization it is wired to, and detach acts there,
whichever cub context is current. Without the kubeconfig — on a machine other
than the one that attached the cluster — the in-cluster cleanup is skipped and
reported, and the ConfigHub side is still handled. For a kind cluster that
"cub cluster up" created, use "cub cluster down", which also deletes it.`,
	RunE: clusterDetachCmdRun,
}

func init() {
	clusterDetachCmd.Flags().StringVar(&clusterDetachArgs.name, "name", "", "cluster name (the argument; kept as a flag for older scripts)")
	_ = clusterDetachCmd.Flags().MarkHidden("name")
	clusterDetachCmd.Flags().BoolVar(&clusterDetachArgs.force, "force", false, "proceed even if no matching ConfigHub Space (or context for the cluster's record) is found")
	clusterDetachCmd.Flags().BoolVar(&clusterDetachArgs.deleteConfig, "delete-config", false, "also delete the ConfigHub config: the cluster's spaces (target, channel, apps, argobot, and deployment variants bound to the target) and its service account. Destructive.")
	clusterCmd.AddCommand(clusterDetachCmd)
}

func clusterDetachCmdRun(cmd *cobra.Command, args []string) error {
	name, err := clusterNameArg(args, clusterDetachArgs.name, "detach", true)
	if err != nil {
		return err
	}
	return clusterDetachRun(cmd.OutOrStdout(), name, clusterDetachArgs.force, clusterDetachArgs.deleteConfig)
}

func clusterDetachRun(out io.Writer, name string, force, deleteConfig bool) error {
	rec, match, err := clusterLocate(out, name, force)
	if err != nil {
		return err
	}
	// A kind cluster cub created has a lifecycle of its own; down owns it.
	if origin := clusterOriginFrom(rec, match); origin == clusterOriginKind {
		return fmt.Errorf("cluster %q was created by 'cub cluster up'; 'cub cluster down %s' detaches and deletes it", name, name)
	}

	if err := clusterDetach(ctx, out, name, clusterKubeconfigPath(name), match); err != nil {
		return err
	}
	clusterRemoveLocalFiles(out, name)
	return clusterFinishConfig(out, name, match, deleteConfig)
}

// clusterDetach removes the ConfigHub wiring from an attached cluster and
// nothing else: the cluster keeps running, Argo CD stays installed (it may
// predate the attachment), and the workloads Argo deployed stay deployed.
//
// The Applications that came from the apps Space are deleted with their
// finalizers stripped, so Argo orphans their resources instead of deleting
// them. The root Application carries resources-finalizer, and a plain delete
// of it would CASCADE through every child Application and everything they
// deployed — stripping it first is the single most consequential step here.
// argobot is the exception: it is cub's own bot, its identity is the cluster's
// service account, and a bot left behind without a working identity is noise,
// so its Application is deleted WITH cascade.
//
// Without the kubeconfig, or with an unreachable cluster, the in-cluster
// cleanup is skipped with a note rather than failed: the ConfigHub side can
// still be handled, and the user is told what remains.
func clusterDetach(ctx context.Context, out io.Writer, name, kubeconfigPath string, match *clusterSpace) error {
	fmt.Fprintf(out, "Detaching cluster %q (the cluster itself is left running)...\n", name)
	if _, err := os.Stat(kubeconfigPath); err != nil {
		fmt.Fprintf(out, "  no kubeconfig at %s; skipping in-cluster cleanup\n", kubeconfigPath)
		return nil
	}
	ns, err := clusterKubectlOutput(ctx, kubeconfigPath, "get", "namespace", clusterArgoNamespace, "--ignore-not-found", "-o", "name")
	if err != nil {
		fmt.Fprintf(out, "  cannot reach the cluster; skipping in-cluster cleanup: %v\n", err)
		return nil
	}
	if len(bytes.TrimSpace(ns)) == 0 {
		fmt.Fprintf(out, "  no %q namespace in the cluster; nothing to detach\n", clusterArgoNamespace)
		return nil
	}

	// The Applications to remove: whatever the apps Space manifests, which is
	// the root app-of-apps plus every child. With no Space to ask (--force), the
	// root is still known by convention.
	appsSlug := name + clusterArgoAppsSuffix
	if match != nil {
		appsSlug = match.SpaceSlug + clusterArgoAppsSuffix
	}
	apps := []string{appsSlug}
	if match != nil {
		if appsSpace, err := resolveSpace(appsSlug, "SpaceID"); err == nil {
			slugs, err := clusterListUnitSlugs(appsSpace.Space.SpaceID)
			if err != nil {
				fmt.Fprintf(out, "  warning: could not list the apps space's Units, removing only the root Application: %v\n", err)
			}
			for _, s := range slugs {
				if !slices.Contains(apps, s) {
					apps = append(apps, s)
				}
			}
		}
	}

	argobotApp := clusterBotSpaceOf(match)
	if match == nil {
		argobotApp = clusterArgobotComponent + "-" + name
	}
	argobotRemoved := false
	for _, app := range apps {
		cascade := app == argobotApp
		removed, err := clusterArgoDeleteApplication(ctx, kubeconfigPath, app, cascade, out)
		if err != nil {
			return err
		}
		if removed && cascade {
			argobotRemoved = true
		}
	}
	if argobotRemoved {
		// argobot's Namespace holds the key Secret cub applied out of band;
		// Argo does not track either, so they go here.
		if err := clusterKubectl(ctx, kubeconfigPath, nil, out,
			"delete", "namespace", clusterArgobotNamespace, "--ignore-not-found", "--wait=false"); err != nil {
			fmt.Fprintf(out, "  warning: %v\n", err)
		}
	}
	if err := clusterKubectl(ctx, kubeconfigPath, nil, out,
		"delete", "secret", clusterArgoOCICredsSecret, "-n", clusterArgoNamespace, "--ignore-not-found"); err != nil {
		fmt.Fprintf(out, "  warning: %v\n", err)
	}
	fmt.Fprintf(out, "  Argo CD left installed in %q; workloads deployed by the removed Applications left running\n", clusterArgoNamespace)
	return nil
}
