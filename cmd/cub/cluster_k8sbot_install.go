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
	k8sbotBase      string
	registry        string
	k8sbotInstaller bool
	secretsDir      string
}

var clusterK8sbotInstallCmd = &cobra.Command{
	Use:   "k8sbot-install <name>",
	Args:  cobra.ExactArgs(1),
	Short: "Install k8sbot in a cluster wired into ConfigHub",
	Long: `Installs k8sbot in a cluster attached without it ("cub cluster attach
--no-k8sbot" or "cub cluster up --no-k8sbot"). It is the last part of "cub
cluster attach", which runs it unless --no-k8sbot is given.

k8sbot is a bot that listens on a Channel and has the controller deploy a
Release the moment it is published, instead of waiting for its poll: it
force-syncs the matching Argo CD Application, redeploys the Sveltos
ClusterProfile, or reconciles the Flux OCIRepository. It also reports each
Release's live status, and stores facts about the cluster on its target.

In ConfigHub:

  k8sbot-base         k8sbot's shared base Space, installed from the deployment
                      for the controller that k8sbot's newest release image
                      carries, or with --k8sbot-installer, with ConfigHub's
                      installer from k8sbot's installer package
                      ("k8sbot-sveltos-base" and "k8sbot-flux-base" for
                      Sveltos and Flux). When it exists already, --k8sbot-base
                      says whether to update it first or clone it as it is.
  k8sbot-<name>       the cluster's downstream variant of it, released to the
                      cluster's target, whose deployment "cub variant create"
                      adds to the apps Space like any other.
  <name>/k8sbot       the Channel k8sbot listens on, in the cluster Space,
                      delivering the release.published events of the Spaces
                      that release to the target. The cluster's service account
                      is granted Claim on it.

k8sbot runs as the cluster's service account, with the key in the
k8sbot-secrets Secret that "cub cluster svc-account-bootstrap" applied; one
is issued and applied here when the cluster has none. A Secret already there,
such as one a secret store keeps, is used as it is, and has to hold the key as
CONFIGHUB_AUTH_PRIVATE_KEY. With --secrets-dir <dir> the Kubernetes manifest of
the Secret holding an issued key (with the k8sbot Namespace, which is
applied) is written to <dir>/<name>/k8sbot_k8sbot-secrets.yaml instead of
being applied, for the store to put in the cluster, and a manifest written
there before is used rather than another key issued; k8sbot cannot start until the Secret is in the cluster.

The apps Space's Release is published with k8sbot in it, so the root deploys
it, and with Argo CD the root is asked to refresh at once. A Sveltos or Flux
cluster attached without k8sbot polls its Releases every minute; with k8sbot
that is only a fallback, so the root, the target, and every deployment created
from here on move to polling every 10 minutes, as for a cluster attached with
k8sbot. Deployments that already exist keep polling every minute.

"cub cluster detach" removes k8sbot along with the rest of the wiring.`,
	RunE: clusterK8sbotInstallCmdRun,
}

func init() {
	clusterK8sbotInstallCmd.Flags().StringVar(&clusterK8sbotInstallArgs.k8sbotBase, "k8sbot-base", "", clusterK8sbotBaseFlagUsage)
	clusterK8sbotInstallCmd.Flags().StringVar(&clusterK8sbotInstallArgs.registry, "registry", "", clusterRegistryFlagUsage)
	clusterK8sbotInstallCmd.Flags().BoolVar(&clusterK8sbotInstallArgs.k8sbotInstaller, "k8sbot-installer", false, clusterK8sbotInstallerFlagUsage)
	clusterK8sbotInstallCmd.Flags().StringVar(&clusterK8sbotInstallArgs.secretsDir, "secrets-dir", "", clusterSecretsDirFlagUsage)
	clusterCmd.AddCommand(clusterK8sbotInstallCmd)
}

func clusterK8sbotInstallCmdRun(cmd *cobra.Command, args []string) error {
	a := clusterK8sbotInstallArgs
	return clusterK8sbotInstallRun(cmd.OutOrStdout(), args[0], a.k8sbotBase, a.registry, a.k8sbotInstaller, a.secretsDir)
}

// clusterK8sbotInstallRun installs k8sbot in a cluster attached without it.
// withInstaller installs its base with ConfigHub's installer. secretsDirFlag is
// --secrets-dir.
func clusterK8sbotInstallRun(out io.Writer, name, k8sbotBase, registry string, withInstaller bool, secretsDirFlag string) (err error) {
	repository, err := clusterK8sbotRepository(registry)
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
		return fmt.Errorf("cluster %q is not attached yet; 'cub cluster attach %s' installs k8sbot as it finishes", name, name)
	case space.BotSpace != "":
		return fmt.Errorf("cluster %q already has k8sbot (space %q)", name, space.BotSpace)
	}
	kubeconfigPath := clusterKubeconfigPath(name)
	if _, err := os.Stat(kubeconfigPath); err != nil {
		return fmt.Errorf("cluster %q: no kubeconfig at %s", name, kubeconfigPath)
	}
	k8sbotSlug := clusterK8sbotComponent + "-" + name
	if err := clusterRequireFreshConfigHub(name, "", "", k8sbotSlug, false); err != nil {
		return err
	}
	// An older cub installed argobot, k8sbot's earlier name, without recording
	// it on the cluster Space.
	argobotSlug := clusterArgobotComponent + "-" + name
	if exists, err := clusterSpaceExists(argobotSlug); err != nil {
		return err
	} else if exists {
		return fmt.Errorf("cluster %q already has argobot, k8sbot's earlier name (space %q)", name, argobotSlug)
	}
	target, err := resolveTargetCore(clusterTargetSlug, space.SpaceID.String(), "*")
	if err != nil {
		return fmt.Errorf("cluster space %q: find its OCI target %q: %w", space.SpaceSlug, clusterTargetSlug, err)
	}
	if err := clusterK8sbotCheckBaseKind(space.Controller, installer != nil); err != nil {
		return err
	}
	mode, err := clusterChooseK8sbotBase(out, space.Controller, k8sbotBase)
	if err != nil {
		return err
	}

	rollback := clusterBeginRollback(out)
	commit := false
	defer func() { rollback.finish(commit, &err) }()
	if err := clusterInstallK8sbot(out, clusterK8sbotSpec{
		name:             name,
		spaceSlug:        space.SpaceSlug,
		kubeconfigPath:   kubeconfigPath,
		controller:       space.Controller,
		spaceID:          space.SpaceID,
		targetID:         target.TargetID,
		k8sbotBase:       mode,
		k8sbotRepository: repository,
		k8sbotInstaller:  installer,
		standalone:       true,
		secretsDir:       clusterSecretsDirFor(secretsDirFlag, name),
	}, rollback.add); err != nil {
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
	fmt.Fprintf(out, "\nk8sbot installed.\n  cluster:  %s\n  k8sbot:   %s (%s)\n  channel:  %s/%s\n  identity: service account %s\n",
		name, k8sbotSlug, kind, space.SpaceSlug, clusterK8sbotChannelSlug, clusterServiceAccountSlug(name))
	return nil
}
