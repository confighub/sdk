// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"fmt"
	"io"
	"os"
	"os/exec"

	"github.com/spf13/cobra"
)

var clusterK8sbotInstallArgs struct {
	argobotBase     string
	registry        string
	k8sbotInstaller bool
}

var clusterK8sbotInstallCmd = &cobra.Command{
	Use:   "k8sbot-install <name>",
	Args:  cobra.ExactArgs(1),
	Short: "Install argobot in a cluster wired into ConfigHub",
	Long: `Installs argobot in a cluster attached without it ("cub cluster attach
--no-k8sbot" or "cub cluster up --no-k8sbot"). It is the last part of "cub
cluster attach", which runs it unless --no-k8sbot is given.

argobot is a bot that listens on a Channel and has the controller deploy a
Release the moment it is published, instead of waiting for its poll: it
force-syncs the matching Argo CD Application, redeploys the Sveltos
ClusterProfile, or reconciles the Flux OCIRepository. It also reports each
Release's live status, and stores facts about the cluster on its target.

In ConfigHub:

  argobot-base        argobot's shared base Space, installed from the deployment
                      for the controller that argobot's newest release image
                      carries, or with --k8sbot-installer, with ConfigHub's
                      installer from argobot's installer package
                      ("argobot-sveltos-base" and "argobot-flux-base" for
                      Sveltos and Flux). When it exists already, --k8sbot-base
                      says whether to update it first or clone it as it is.
  argobot-<name>      the cluster's downstream variant of it, released to the
                      cluster's target, whose deployment "cub variant create"
                      adds to the apps Space like any other.
  <name>/argobot      the Channel argobot listens on, in the cluster Space,
                      delivering the release.published events of the Spaces
                      that release to the target. The cluster's service account
                      is granted Claim on it.

argobot runs as the cluster's service account, with the key in the
argobot-secrets Secret that "cub cluster svc-account-bootstrap" applied; one
is issued and applied here when the cluster has none.

The apps Space's Release is published with argobot in it, so the root deploys
it, and with Argo CD the root is asked to refresh at once. A Sveltos or Flux
cluster attached without argobot polls its Releases every minute; with argobot
that is only a fallback, so the root, the target, and every deployment created
from here on move to polling every 10 minutes, as for a cluster attached with
argobot. Deployments that already exist keep polling every minute.

"cub cluster detach" removes argobot along with the rest of the wiring.`,
	RunE: clusterK8sbotInstallCmdRun,
}

func init() {
	clusterK8sbotInstallCmd.Flags().StringVar(&clusterK8sbotInstallArgs.argobotBase, "k8sbot-base", "", clusterK8sbotBaseFlagUsage)
	clusterK8sbotInstallCmd.Flags().StringVar(&clusterK8sbotInstallArgs.registry, "registry", "", clusterRegistryFlagUsage)
	clusterK8sbotInstallCmd.Flags().BoolVar(&clusterK8sbotInstallArgs.k8sbotInstaller, "k8sbot-installer", false, clusterK8sbotInstallerFlagUsage)
	clusterCmd.AddCommand(clusterK8sbotInstallCmd)
}

func clusterK8sbotInstallCmdRun(cmd *cobra.Command, args []string) error {
	a := clusterK8sbotInstallArgs
	return clusterK8sbotInstallRun(cmd.OutOrStdout(), args[0], a.argobotBase, a.registry, a.k8sbotInstaller)
}

// clusterK8sbotInstallRun installs argobot in a cluster attached without it.
// withInstaller installs its base with ConfigHub's installer.
func clusterK8sbotInstallRun(out io.Writer, name, argobotBase, registry string, withInstaller bool) error {
	repository, err := clusterArgobotRepository(registry)
	if err != nil {
		return err
	}
	var installer *PluginCommand
	if withInstaller {
		if installer, err = clusterFindInstaller(); err != nil {
			return err
		}
	}
	if _, err := exec.LookPath("kubectl"); err != nil {
		return fmt.Errorf("kubectl not found on PATH")
	}
	rec, err := clusterReadRecord(name)
	if err != nil {
		return err
	}
	if rec == nil {
		return fmt.Errorf("cluster %q is not wired into ConfigHub from this machine (see 'cub cluster list'); attach it with 'cub cluster attach %s'", name, name)
	}
	if err := clusterUseRecordedContext(out, rec); err != nil {
		return err
	}
	space, err := clusterSpaceByNameInOrg(name)
	if err != nil {
		return fmt.Errorf("look up cluster spaces: %w", err)
	}
	switch {
	case space == nil:
		return fmt.Errorf("no cub-managed Space for %q in %s, where its record says it is wired", name, clusterRecordWhere(rec))
	case space.Stage != "":
		return fmt.Errorf("cluster %q is not attached yet; 'cub cluster attach %s' installs argobot as it finishes", name, name)
	case space.BotSpace != "":
		return fmt.Errorf("cluster %q already has argobot (space %q)", name, space.BotSpace)
	}
	kubeconfigPath := clusterKubeconfigPath(name)
	if _, err := os.Stat(kubeconfigPath); err != nil {
		return fmt.Errorf("cluster %q: no kubeconfig at %s", name, kubeconfigPath)
	}
	argobotSlug := clusterArgobotComponent + "-" + name
	if err := clusterRequireFreshConfigHub(name, "", "", argobotSlug, false); err != nil {
		return err
	}
	target, err := resolveTargetCore(clusterTargetSlug, space.SpaceID.String(), "*")
	if err != nil {
		return fmt.Errorf("cluster space %q: find its OCI target %q: %w", space.SpaceSlug, clusterTargetSlug, err)
	}
	if err := clusterArgobotCheckBaseKind(space.Controller, installer != nil); err != nil {
		return err
	}
	mode, err := clusterChooseArgobotBase(out, space.Controller, argobotBase)
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
	if err := clusterInstallK8sbot(out, clusterK8sbotSpec{
		name:              name,
		spaceSlug:         space.SpaceSlug,
		kubeconfigPath:    kubeconfigPath,
		controller:        space.Controller,
		spaceID:           space.SpaceID,
		targetID:          target.TargetID,
		argobotBase:       mode,
		argobotRepository: repository,
		argobotInstaller:  installer,
		standalone:        true,
	}, func(f func()) { rollback = append(rollback, f) }); err != nil {
		return err
	}
	commit = true

	kind := "Argo app; refreshes on publish"
	switch space.Controller {
	case clusterControllerSveltos:
		kind = "Sveltos ClusterProfile; redeploys on publish"
	case clusterControllerFlux:
		kind = "Flux Kustomization; reconciles on publish"
	}
	fmt.Fprintf(out, "\nargobot installed.\n  cluster:  %s\n  argobot:  %s (%s)\n  channel:  %s/%s\n  identity: service account %s\n",
		name, argobotSlug, kind, space.SpaceSlug, clusterArgobotChannelSlug, clusterServiceAccountSlug(name))
	return nil
}
