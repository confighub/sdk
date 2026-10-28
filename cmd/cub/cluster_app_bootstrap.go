// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"fmt"
	"io"
	"os/exec"

	"github.com/spf13/cobra"
)

var clusterAppBootstrapArgs struct {
	prune bool
}

var clusterAppBootstrapCmd = &cobra.Command{
	Use:   "app-bootstrap <name>",
	Args:  cobra.ExactArgs(1),
	Short: "Create a cluster's apps Space and bootstrap its root in the controller",
	Long: `The third part of "cub cluster attach", on its own, for a cluster "cub cluster
svc-account-bootstrap" set up. It finishes the attachment without argobot, as
"cub cluster attach --no-k8sbot" does; "cub cluster k8sbot-install" adds
argobot afterwards.

In ConfigHub it creates the apps Space, <name>-argo-apps (<name>-sveltos-apps
or <name>-flux-apps with Sveltos or Flux), releasing to the cluster's OCI
target, and in it the root Unit: an Argo CD Application, a Sveltos
ClusterProfile, or a Flux OCIRepository and Kustomization, that pulls the apps
Space's own Release from ConfigHub's OCI registry with the pull token
"svc-account-bootstrap" put in the cluster. Its first Release is published.

In the cluster the root is applied once with kubectl; from then on the
controller pulls everything else from ConfigHub, so each Unit "cub variant
create --target" adds to the apps Space becomes a deployment the controller
runs. Without argobot, a Sveltos or Flux root polls its Release every minute.

--prune lets the controller delete what a removed deployment deployed; see
"cub cluster attach --help".

Examples:
  cub cluster create lab
  cub cluster controller-install lab --provider kind
  cub cluster svc-account-bootstrap lab
  cub cluster app-bootstrap lab
  cub cluster k8sbot-install lab`,
	RunE: clusterAppBootstrapCmdRun,
}

func init() {
	clusterAppBootstrapCmd.Flags().BoolVar(&clusterAppBootstrapArgs.prune, "prune", false, "let the controller delete what a removed deployment deployed: the root prunes deployments whose Units leave the apps space (Sveltos's always does), and deployments created later prune what they deployed")
	clusterCmd.AddCommand(clusterAppBootstrapCmd)
}

func clusterAppBootstrapCmdRun(cmd *cobra.Command, args []string) error {
	return clusterAppBootstrapRun(cmd.OutOrStdout(), args[0], clusterAppBootstrapArgs.prune)
}

// clusterAppBootstrapRun runs the apps stage of an attachment, without
// argobot, on a cluster `cub cluster svc-account-bootstrap` set up.
func clusterAppBootstrapRun(out io.Writer, name string, prune bool) error {
	if _, err := exec.LookPath("kubectl"); err != nil {
		return fmt.Errorf("kubectl not found on PATH")
	}
	space, err := clusterFindControllerInstalled(out, name)
	if err != nil {
		return err
	}
	switch {
	case space == nil:
		return fmt.Errorf("cluster %q has no controller installed by cub; run 'cub cluster controller-install %s' and 'cub cluster svc-account-bootstrap %s' first", name, name, name)
	case space.Stage == clusterStageController:
		return fmt.Errorf("cluster %q has no service account yet; run 'cub cluster svc-account-bootstrap %s' first", name, name)
	}
	appsSlug := space.SpaceSlug + clusterAppsSuffix(space.Controller)
	if err := clusterRequireFreshConfigHub(name, "", appsSlug, "", false); err != nil {
		return err
	}
	kubeconfigPath := clusterKubeconfigPath(name)
	inst, err := clusterLoadInstalled(space, kubeconfigPath)
	if err != nil {
		return err
	}

	var rollback []func()
	commit := false
	defer func() {
		if commit {
			return
		}
		for i := len(rollback) - 1; i >= 0; i-- {
			rollback[i]()
		}
	}()
	spec := clusterAttachSpec{
		clusterControllerSpec: clusterControllerSpec{
			name:              name,
			spaceSlug:         space.SpaceSlug,
			kubeconfigPath:    kubeconfigPath,
			origin:            space.Origin,
			controller:        space.Controller,
			noPlaceholderGate: inst.noPlaceholderGate,
		},
		noArgobot: true,
		prune:     prune,
	}
	result, err := clusterBootstrapApps(out, spec, inst, nil, func(f func()) { rollback = append(rollback, f) })
	if err != nil {
		return err
	}
	commit = true

	envFilePath := clusterEnvFilePath(name)
	clusterPrintSummaryHead(out, "Attached", result, kubeconfigPath, envFilePath)
	clusterPrintSummaryTail(out, result, envFilePath)
	fmt.Fprintf(out, "\nInstall argobot, which has the controller deploy each Release the moment it is\npublished, with:\n  cub cluster k8sbot-install %s\n", name)
	return nil
}
