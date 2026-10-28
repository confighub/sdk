// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"fmt"
	"io"
	"os/exec"

	"github.com/spf13/cobra"
)

var clusterControllerInstallArgs struct {
	spaceSlug         string
	provider          string
	cluster           string
	kubeconfig        string
	kubeContext       string
	awsRegion         string
	awsProfile        string
	grantAccess       bool
	controller        string
	controllerVersion string
	noInstall         bool
	noPlaceholderGate bool
}

var clusterControllerInstallCmd = &cobra.Command{
	Use:   "controller-install <name>",
	Args:  cobra.ExactArgs(1),
	Short: "Install Argo CD, Sveltos or Flux in a cluster and create its target in ConfigHub",
	Long: `The first part of "cub cluster attach", on its own: installs the controller in
the cluster and creates the ConfigHub side that represents the cluster.
"cub cluster attach <name>" finishes the job later, picking the cluster up
where this left it.

In the cluster, the controller — Argo CD, Sveltos or Flux, chosen with
--controller — is installed from the upstream manifest when it is absent, and
an existing one is left as found (--no-install fails instead of installing).
On a kind cluster "cub cluster create" made, Argo CD's UI or the Sveltos
dashboard goes on the first port of the node's host port window.

In ConfigHub:

  <name>              the cluster Space, which holds the OCI target and, once
                      attached, argobot's Channel.
  target              the OCI target that represents the cluster, gated by a
                      "no-placeholders" Trigger in the cluster Space unless
                      --no-placeholder-gate is given. No one is granted access
                      to it yet: attaching creates the cluster's service
                      account and grants it.

The cluster's credentials come from --provider, as for "cub cluster attach",
and a copy is written to <config-dir>/clusters/<name>.kubeconfig beside the
cluster's record, so "cub cluster list" shows the cluster and "cub cluster
detach <name> --delete-config" undoes this.

Examples:
  # Install Argo CD in a kind cluster, then finish attaching it
  cub cluster create lab
  cub cluster controller-install lab --provider kind
  cub cluster attach lab

  # Install Flux in the cluster your current kube context points at
  cub cluster controller-install staging --controller flux`,
	RunE: clusterControllerInstallCmdRun,
}

func init() {
	f := clusterControllerInstallCmd.Flags()
	f.StringVar(&clusterControllerInstallArgs.spaceSlug, "space", "", "ConfigHub space slug for the cluster's target (the \"target prefix\"; defaults to <name>)")
	f.StringVar(&clusterControllerInstallArgs.provider, "provider", clusterProviderKubeconfig, "where the cluster's credentials come from: kubeconfig, eks or kind")
	f.StringVar(&clusterControllerInstallArgs.cluster, "cluster", "", "the cluster's name at the provider (--provider eks or kind; defaults to the cluster name)")
	f.StringVar(&clusterControllerInstallArgs.kubeconfig, "kubeconfig", "", "--provider kubeconfig: the kubeconfig to read the cluster from (defaults to $KUBECONFIG or ~/.kube/config)")
	f.StringVar(&clusterControllerInstallArgs.kubeContext, "kube-context", "", "--provider kubeconfig: the kube context to use (defaults to the kubeconfig's current context)")
	f.StringVar(&clusterControllerInstallArgs.awsRegion, "aws-region", "", "--provider eks: the cluster's AWS region (defaults to the AWS CLI's: AWS_REGION or the profile's)")
	f.StringVar(&clusterControllerInstallArgs.awsProfile, "aws-profile", "", "--provider eks: the AWS CLI profile (defaults to AWS_PROFILE)")
	f.BoolVar(&clusterControllerInstallArgs.grantAccess, "grant-access", false, "--provider eks: if your AWS identity has no access entry on the cluster, add one with cluster-admin")
	f.StringVar(&clusterControllerInstallArgs.controller, "controller", clusterControllerArgo, "controller to install: argo (Argo CD), sveltos (Sveltos) or flux (Flux)")
	f.StringVar(&clusterControllerInstallArgs.controllerVersion, "controller-version", "", "controller release to install when absent (default: argo "+clusterArgoDefaultVersion+", sveltos "+clusterSveltosVersion+", flux "+clusterFluxVersion+"; Argo CD must be v3.1 or later, which oci:// sources need)")
	f.BoolVar(&clusterControllerInstallArgs.noInstall, "no-install", false, "never install the controller; fail if the cluster has none")
	f.BoolVar(&clusterControllerInstallArgs.noPlaceholderGate, "no-placeholder-gate", false, "skip the default vet-placeholders Trigger, allowing Releases with unfilled confighubplaceholder values to be published to this cluster")
	clusterCmd.AddCommand(clusterControllerInstallCmd)
}

func clusterControllerInstallCmdRun(cmd *cobra.Command, args []string) error {
	a := clusterControllerInstallArgs
	return clusterControllerInstallRun(cmd.OutOrStdout(), clusterAttachOptions{
		name:              args[0],
		spaceSlug:         a.spaceSlug,
		provider:          a.provider,
		cluster:           a.cluster,
		kubeconfig:        a.kubeconfig,
		kubeContext:       a.kubeContext,
		awsRegion:         a.awsRegion,
		awsProfile:        a.awsProfile,
		grantAccess:       a.grantAccess,
		controller:        a.controller,
		controllerVersion: a.controllerVersion,
		noInstall:         a.noInstall,
		noPlaceholderGate: a.noPlaceholderGate,
	})
}

// clusterControllerInstallRun runs the controller stage of an attachment on
// its own and records the cluster, leaving its cluster Space marked so `cub
// cluster attach` finishes it. Of opts it reads the name, the credentials, the
// controller and the gate; the rest belongs to attach.
func clusterControllerInstallRun(out io.Writer, opts clusterAttachOptions) error {
	if err := clusterCheckController(opts.controller); err != nil {
		return err
	}
	if _, err := exec.LookPath("kubectl"); err != nil {
		return fmt.Errorf("kubectl not found on PATH")
	}
	if opts.origin == "" {
		opts.origin = clusterOriginAttach
	}
	if installed, err := clusterFindControllerInstalled(out, opts.name); err != nil {
		return err
	} else if installed != nil {
		return fmt.Errorf("cluster %q already has its controller installed (space %q); finish with 'cub cluster attach %s'", opts.name, installed.SpaceSlug, opts.name)
	}
	if err := clusterAttachCheckProviderFlags(&opts); err != nil {
		return err
	}
	if opts.spaceSlug == "" {
		opts.spaceSlug = opts.name
	}
	// The apps Space is attach's, but one already there would stop attach later.
	if err := clusterRequireFreshConfigHub(opts.name, opts.spaceSlug, opts.spaceSlug+clusterAppsSuffix(opts.controller), "", true); err != nil {
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
	inst, err := clusterControllerInstallStage(out, &opts, func(f func()) { rollback = append(rollback, f) })
	if err != nil {
		return err
	}
	if err := clusterWriteRecord(clusterNewRecord(opts.name, opts.spaceSlug, opts.origin, opts.controller)); err != nil {
		return fmt.Errorf("write cluster record: %w", err)
	}
	if err := clusterWriteEnvFile(opts.name, opts.origin, opts.controller, inst); err != nil {
		return err
	}
	commit = true

	kubeconfigPath, envFilePath := clusterKubeconfigPath(opts.name), clusterEnvFilePath(opts.name)
	fmt.Fprintf(out, "\nController installed.\n  cluster:    %s\n  context:    %s\n  kubeconfig: %s\n  env file:   %s\n  space:      %s\n  target:     %s/%s (no one granted access yet)\n",
		opts.name, contextManager.ActiveContextName(), kubeconfigPath, envFilePath, opts.spaceSlug, opts.spaceSlug, clusterTargetSlug)
	if inst.noPlaceholderGate {
		fmt.Fprintf(out, "  gates:      none (--no-placeholder-gate)\n")
	} else {
		fmt.Fprintf(out, "  gates:      %s/%s (vet-placeholders)\n", opts.spaceSlug, clusterGateTriggerSlug)
	}
	clusterPrintControllerSummary(out, opts.name, opts.controller, kubeconfigPath, inst)
	fmt.Fprintf(out, "\nFinish wiring it into ConfigHub — the service account, the apps space, the\nroot and argobot — with:\n  cub cluster attach %s\n", opts.name)
	return nil
}
