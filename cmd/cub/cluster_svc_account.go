// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"bytes"
	"fmt"
	"io"
	"os/exec"

	"github.com/spf13/cobra"
)

var clusterSvcAccountBootstrapArgs struct {
	noArgobot bool
}

var clusterSvcAccountBootstrapCmd = &cobra.Command{
	Use:   "svc-account-bootstrap <name>",
	Args:  cobra.ExactArgs(1),
	Short: "Create a cluster's service account and put its credentials in the cluster",
	Long: `The second part of "cub cluster attach", on its own, for a cluster "cub cluster
controller-install" set up. "cub cluster attach <name>" finishes the job
later, picking the cluster up where this left it.

In ConfigHub it creates the cluster's identity, a service account with no
organization role named argo-<name>, and grants its user View, Edit,
ViewChildren and EditChildren on the cluster's OCI target: what the controller
needs to pull the Releases of every Space that releases to the target, and
argobot to find the target, store the cluster's facts on it and record each
Release's live status. Claim on argobot's Channel is granted where the Channel
is created, when argobot is installed.

In the cluster it puts the service account's credentials:

  a private key       in the argobot-secrets Secret in the argobot namespace,
                      which argobot authenticates with. Skipped with
                      --no-k8sbot, for a cluster that will not run argobot;
                      installing argobot later issues one.
  a pull token        a token that does not expire, as the controller's
                      registry credentials: Argo CD's confighub-oci-creds
                      repo-creds Secret, Sveltos's confighub-gateway Secret, or
                      Flux's confighub-oci Secret.

Neither credential is kept anywhere else: ConfigHub holds the public half of
the key and a revocable record of the token.

"cub cluster detach <name> --delete-config" undoes this with the rest of the
cluster's wiring.

Examples:
  cub cluster create lab
  cub cluster controller-install lab --provider kind
  cub cluster svc-account-bootstrap lab
  cub cluster attach lab`,
	RunE: clusterSvcAccountBootstrapCmdRun,
}

func init() {
	clusterSvcAccountBootstrapCmd.Flags().BoolVar(&clusterSvcAccountBootstrapArgs.noArgobot, "no-k8sbot", false, "skip argobot's private key, for a cluster that will not run argobot (installing argobot later issues one)")
	clusterCmd.AddCommand(clusterSvcAccountBootstrapCmd)
}

func clusterSvcAccountBootstrapCmdRun(cmd *cobra.Command, args []string) error {
	return clusterSvcAccountBootstrapRun(cmd.OutOrStdout(), args[0], clusterSvcAccountBootstrapArgs.noArgobot)
}

// clusterSvcAccountBootstrapRun runs the service account stage of an
// attachment on a cluster `cub cluster controller-install` set up; the stage
// moves its cluster Space on to clusterStageServiceAccount, so `cub cluster
// attach` finishes it.
func clusterSvcAccountBootstrapRun(out io.Writer, name string, noArgobot bool) error {
	if _, err := exec.LookPath("kubectl"); err != nil {
		return fmt.Errorf("kubectl not found on PATH")
	}
	space, err := clusterFindControllerInstalled(out, name)
	if err != nil {
		return err
	}
	switch {
	case space == nil:
		return fmt.Errorf("cluster %q has no controller installed by cub; run 'cub cluster controller-install %s' first", name, name)
	case space.Stage == clusterStageServiceAccount:
		return fmt.Errorf("cluster %q already has its service account (argo-%s); finish with 'cub cluster attach %s'", name, name, name)
	}
	serviceAccountSlug := clusterServiceAccountSlug(name)
	if exists, err := clusterServiceAccountExists(serviceAccountSlug); err != nil {
		return err
	} else if exists {
		return fmt.Errorf("ConfigHub service account %q already exists (leftover cluster identity; delete it with 'cub serviceaccount delete %s')", serviceAccountSlug, serviceAccountSlug)
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
	if err := clusterBootstrapServiceAccount(out, clusterServiceAccountSpec{
		name:           name,
		spaceSlug:      space.SpaceSlug,
		kubeconfigPath: kubeconfigPath,
		controller:     space.Controller,
		noArgobot:      noArgobot,
	}, inst, func(f func()) { rollback = append(rollback, f) }); err != nil {
		return err
	}
	commit = true

	fmt.Fprintf(out, "\nService account bootstrapped.\n  cluster:    %s\n  identity:   service account %s, granted its deployment path on %s/%s\n",
		name, serviceAccountSlug, space.SpaceSlug, clusterTargetSlug)
	ns := clusterControllerNamespace(space.Controller)
	fmt.Fprintf(out, "  pull token: Secret %s/%s\n", ns, clusterControllerCredsSecret(space.Controller))
	if noArgobot {
		fmt.Fprintf(out, "  argobot key: none (--no-k8sbot)\n")
	} else {
		fmt.Fprintf(out, "  argobot key: Secret %s/%s\n", clusterArgobotNamespace, clusterArgobotSecret)
	}
	flag := ""
	if noArgobot {
		flag = " --no-k8sbot"
	}
	fmt.Fprintf(out, "\nFinish wiring it into ConfigHub — the apps space, the root and argobot — with:\n  cub cluster attach %s%s\n", name, flag)
	return nil
}

// clusterServiceAccountSpec is what the service account stage needs.
type clusterServiceAccountSpec struct {
	name           string
	spaceSlug      string
	kubeconfigPath string
	controller     string
	// noArgobot skips argobot's key, which only argobot uses.
	noArgobot bool
}

// clusterBootstrapServiceAccount creates the cluster's identity on the
// ConfigHub side and puts its credentials in the cluster. One service account,
// used two ways: the controller pulls the cluster's Releases from the OCI
// registry with a long-lived token of it, and argobot authenticates as it with
// a key. It has no organization role; the Target, granted here, and argobot's
// Channel, granted where it is created, give its user exactly the cluster's
// deployment path. The service account is org-scoped, so it is rolled back on
// its own rather than with a Space, and the credentials Secrets are rolled back
// too, since the registry one marks the cluster as wired. When it is done it
// moves the cluster Space's stage marker on to clusterStageServiceAccount.
func clusterBootstrapServiceAccount(out io.Writer, spec clusterServiceAccountSpec, inst clusterInstalled, addRollback func(func())) error {
	serviceAccountSlug := clusterServiceAccountSlug(spec.name)
	fmt.Fprintf(out, "Creating service account %q (OrgRole=none; the controller pulls and argobot runs as it)...\n", serviceAccountSlug)
	serviceAccountID, serviceAccountUserID, err := clusterCreateServiceAccount(serviceAccountSlug, serviceAccountSlug,
		map[string]string{clusterAnnotationName: spec.name})
	if err != nil {
		return err
	}
	addRollback(func() {
		fmt.Fprintf(out, "Rolling back: cub serviceaccount delete %q\n", serviceAccountSlug)
		_ = clusterDeleteServiceAccount(serviceAccountSlug)
	})

	fmt.Fprintf(out, "Granting %q its deployment path on OCI target %q...\n", serviceAccountSlug, clusterTargetSlug)
	if _, err := clusterGrantTarget(inst.spaceID, inst.targetID, serviceAccountUserID, nil); err != nil {
		return fmt.Errorf("grant the service account access to the target: %w", err)
	}

	if !spec.noArgobot {
		fmt.Fprintf(out, "Issuing a key for argobot on %q...\n", serviceAccountSlug)
		privateKey, err := clusterCreateServiceAccountKey(serviceAccountID, serviceAccountUserID, "argobot in cluster "+spec.name)
		if err != nil {
			return fmt.Errorf("issue the service account key: %w", err)
		}
		if err := clusterApplyArgobotKey(out, spec.kubeconfigPath, privateKey, addRollback); err != nil {
			return err
		}
	}

	fmt.Fprintf(out, "Issuing a pull token on %q...\n", serviceAccountSlug)
	pullToken, err := clusterCreateServiceAccountToken(serviceAccountID, "cluster "+spec.name+" (OCI pull)")
	if err != nil {
		return fmt.Errorf("issue the service account token: %w", err)
	}
	ociURLForArgo, ociInsecure, err := clusterControllerOCIEndpoint()
	if err != nil {
		return err
	}
	addRollback(func() {
		_ = clusterKubectl(ctx, spec.kubeconfigPath, nil, io.Discard,
			"delete", "secret", clusterControllerCredsSecret(spec.controller), "-n", clusterControllerNamespace(spec.controller), "--ignore-not-found")
	})
	switch spec.controller {
	case clusterControllerSveltos:
		err = clusterSveltosApplyOCICreds(ctx, spec.kubeconfigPath, pullToken, out)
	case clusterControllerFlux:
		err = clusterFluxApplyOCICreds(ctx, spec.kubeconfigPath, ociURLForArgo, pullToken, out)
	default:
		err = clusterArgoApplyOCIRepoCreds(ctx, spec.kubeconfigPath, ociURLForArgo, pullToken, ociInsecure, out)
	}
	if err != nil {
		return err
	}
	// Last, so a run stopped before here is picked up at the controller stage
	// and one stopped after it at this one, whether the stage ran on its own or
	// inside `cub cluster attach`.
	return clusterSetSpaceStage(inst.spaceID, clusterStageServiceAccount)
}

// clusterControllerOCIEndpoint returns the OCI registry URL as the controller,
// running inside the cluster, reaches it, and whether it is plain HTTP. The
// server is asked where its registry is once, for both, so the two cannot
// disagree. A loopback registry is rewritten to the docker host address, which
// only a cluster on this machine can reach anyway; a remote one passes through
// untouched. Plain HTTP comes from the advertised registry's scheme, or failing
// that from the cub server URL's: http:// API → http:// OCI (the local-dev
// case), https:// API → TLS OCI.
func clusterControllerOCIEndpoint() (string, bool, error) {
	ociServerURL, err := clusterOCIServerURL()
	if err != nil {
		return "", false, err
	}
	url, err := clusterOCIEndpointFromContainer(ociServerURL, clusterServerURL())
	if err != nil {
		return "", false, err
	}
	insecure, err := clusterOCIInsecure(ociServerURL, clusterServerURL())
	if err != nil {
		return "", false, err
	}
	return url, insecure, nil
}

// clusterControllerCredsSecret returns the Secret, in the controller's
// namespace, holding the controller's ConfigHub registry credentials.
func clusterControllerCredsSecret(controller string) string {
	switch controller {
	case clusterControllerSveltos:
		return clusterSveltosGatewaySecret
	case clusterControllerFlux:
		return clusterFluxCredsSecret
	}
	return clusterArgoOCICredsSecret
}

// clusterHasArgobotKey reports whether argobot's key Secret is in the cluster,
// which `cub cluster svc-account-bootstrap --no-k8sbot` leaves out.
func clusterHasArgobotKey(kubeconfigPath string) (bool, error) {
	got, err := clusterKubectlOutput(ctx, kubeconfigPath,
		"get", "secret", clusterArgobotSecret, "-n", clusterArgobotNamespace, "--ignore-not-found", "-o", "name")
	if err != nil {
		return false, fmt.Errorf("check for argobot's key Secret: %w", err)
	}
	return len(bytes.TrimSpace(got)) > 0, nil
}
