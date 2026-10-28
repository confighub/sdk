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
	Long: `The inverse of "cub cluster attach". The cluster is left running and its
controller — Argo CD, Sveltos or Flux — left installed; what goes is the
wiring:

  - everything that came from the apps Space — the root and the children: Argo
    Applications, Sveltos ClusterProfiles, or Flux OCIRepositories and
    Kustomizations — is deleted WITHOUT cascading, so the workloads they
    deployed stay (Applications lose their finalizers, ClusterProfiles are set
    to LeavePolicies, Kustomizations are suspended first);
  - argobot, cub's own bot, is deleted with its resources;
  - the registry credentials Secret is deleted;
  - the local kubeconfig, env file and record are removed.

By default the ConfigHub side is left in place — the cluster Space with its OCI
target and argobot's Channel, the apps Space holding the controller's Units,
any deployment Spaces bound to the target, and the cluster's service
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

	if err := clusterDetach(ctx, out, name, clusterKubeconfigPath(name), match, clusterControllerFrom(rec, match)); err != nil {
		return err
	}
	clusterRemoveLocalFiles(out, name)
	return clusterFinishConfig(out, name, match, deleteConfig)
}

// clusterDetach removes the ConfigHub wiring from an attached cluster and
// nothing else: the cluster keeps running, the controller stays installed (it
// may predate the attachment), and the workloads it deployed stay deployed.
//
// What came from the apps Space is deleted so the controller orphans what it
// deployed instead of deleting it. With Argo CD, the Applications' finalizers
// are stripped: the root Application carries resources-finalizer, and a plain
// delete of it would CASCADE through every child Application and everything
// they deployed — stripping it first is the single most consequential step
// here. With Sveltos, each ClusterProfile is set to LeavePolicies; with Flux,
// each Kustomization is suspended, which also keeps a pruning root from
// deleting the children. argobot is the exception: it is cub's own bot, its
// identity is the cluster's service account, and a bot left behind without a
// working identity is noise, so it is deleted WITH cascade.
//
// Without the kubeconfig, or with an unreachable cluster, the in-cluster
// cleanup is skipped with a note rather than failed: the ConfigHub side can
// still be handled, and the user is told what remains.
func clusterDetach(ctx context.Context, out io.Writer, name, kubeconfigPath string, match *clusterSpace, controller string) error {
	fmt.Fprintf(out, "Detaching cluster %q (the cluster itself is left running)...\n", name)
	if _, err := os.Stat(kubeconfigPath); err != nil {
		fmt.Fprintf(out, "  no kubeconfig at %s; skipping in-cluster cleanup\n", kubeconfigPath)
		return nil
	}
	controllerNamespace := clusterControllerNamespace(controller)
	ns, err := clusterKubectlOutput(ctx, kubeconfigPath, "get", "namespace", controllerNamespace, "--ignore-not-found", "-o", "name")
	if err != nil {
		fmt.Fprintf(out, "  cannot reach the cluster; skipping in-cluster cleanup: %v\n", err)
		return nil
	}
	if len(bytes.TrimSpace(ns)) == 0 {
		fmt.Fprintf(out, "  no %q namespace in the cluster; nothing to detach\n", controllerNamespace)
		return nil
	}

	// The children to remove: whatever the apps Space manifests besides the
	// root. With no Space to ask (--force), only the root is known, by
	// convention.
	appsSlug := name + clusterAppsSuffix(controller)
	if match != nil {
		appsSlug = match.SpaceSlug + clusterAppsSuffix(controller)
	}
	var children []string
	if match != nil {
		if appsSpace, err := resolveSpace(appsSlug, "SpaceID"); err == nil {
			slugs, err := clusterListUnitSlugs(appsSpace.Space.SpaceID)
			if err != nil {
				fmt.Fprintf(out, "  warning: could not list the apps space's Units, removing only the root: %v\n", err)
			}
			for _, s := range slugs {
				if s != clusterRootUnitSlug && !slices.Contains(children, s) {
					children = append(children, s)
				}
			}
		}
	}

	argobotName := clusterBotSpaceOf(match)
	if match == nil {
		argobotName = clusterArgobotComponent + "-" + name
	}
	var argobotRemoved bool
	var credsSecret, left string
	switch controller {
	case clusterControllerSveltos:
		argobotRemoved, err = clusterDetachSveltos(ctx, out, kubeconfigPath, children, argobotName)
		credsSecret = clusterSveltosGatewaySecret
		left = "Sveltos left installed in %q; workloads deployed by the removed ClusterProfiles left running\n"
	case clusterControllerFlux:
		argobotRemoved, err = clusterDetachFlux(ctx, out, kubeconfigPath, children, argobotName)
		credsSecret = clusterFluxCredsSecret
		left = "Flux left installed in %q; workloads applied by the removed Kustomizations left running\n"
	default:
		argobotRemoved, err = clusterDetachArgo(ctx, out, kubeconfigPath, appsSlug, children, argobotName)
		credsSecret = clusterArgoOCICredsSecret
		left = "Argo CD left installed in %q; workloads deployed by the removed Applications left running\n"
	}
	if err != nil {
		return err
	}
	if argobotRemoved {
		// argobot's Namespace holds the key Secret cub applied out of band; the
		// controller does not track either, so they go here.
		if err := clusterKubectl(ctx, kubeconfigPath, nil, out,
			"delete", "namespace", clusterArgobotNamespace, "--ignore-not-found", "--wait=false"); err != nil {
			fmt.Fprintf(out, "  warning: %v\n", err)
		}
	} else if hasKey, err := clusterHasArgobotKey(kubeconfigPath); err == nil && hasKey {
		// `cub cluster svc-account-bootstrap` applies the key before argobot is
		// installed, so a cluster detached in between has the key and no bot.
		if err := clusterKubectl(ctx, kubeconfigPath, nil, out,
			"delete", "secret", clusterArgobotSecret, "-n", clusterArgobotNamespace, "--ignore-not-found"); err != nil {
			fmt.Fprintf(out, "  warning: %v\n", err)
		}
	}
	if err := clusterKubectl(ctx, kubeconfigPath, nil, out,
		"delete", "secret", credsSecret, "-n", controllerNamespace, "--ignore-not-found"); err != nil {
		fmt.Fprintf(out, "  warning: %v\n", err)
	}
	fmt.Fprintf(out, "  "+left, controllerNamespace)
	return nil
}

// clusterDetachArgo deletes the root Application, named for the apps Space,
// then every child, orphaning what they deployed, except argobot's, which goes
// with its resources. It reports whether argobot's Application was removed.
func clusterDetachArgo(ctx context.Context, out io.Writer, kubeconfigPath, root string, children []string, argobotName string) (bool, error) {
	argobotRemoved := false
	for _, app := range append([]string{root}, children...) {
		cascade := app == argobotName
		removed, err := clusterArgoDeleteApplication(ctx, kubeconfigPath, app, cascade, out)
		if err != nil {
			return argobotRemoved, err
		}
		if removed && cascade {
			argobotRemoved = true
		}
	}
	return argobotRemoved, nil
}

// clusterDetachSveltos deletes the root ClusterProfile, whose LeavePolicies
// leaves the child profiles in place, then every child with LeavePolicies set,
// so what they deployed stays, except argobot's, which is withdrawn. It
// reports whether argobot's profile was removed.
func clusterDetachSveltos(ctx context.Context, out io.Writer, kubeconfigPath string, children []string, argobotName string) (bool, error) {
	argobotRemoved := false
	for _, profile := range append([]string{clusterSveltosRootProfile}, children...) {
		cascade := profile == argobotName
		removed, err := clusterSveltosDeleteProfile(ctx, kubeconfigPath, profile, cascade, out)
		if err != nil {
			return argobotRemoved, err
		}
		if removed && cascade {
			argobotRemoved = true
		}
	}
	return argobotRemoved, nil
}

// clusterDetachFlux deletes the root Kustomization and OCIRepository, suspended
// first so Flux neither prunes the children nor re-applies them, then every
// child, suspended so what it applied stays, except argobot's, which is deleted
// with what it applied. It reports whether argobot's Kustomization was
// removed.
func clusterDetachFlux(ctx context.Context, out io.Writer, kubeconfigPath string, children []string, argobotName string) (bool, error) {
	argobotRemoved := false
	for _, kustomization := range append([]string{clusterFluxRootName}, children...) {
		cascade := kustomization == argobotName
		removed, err := clusterFluxDeleteDeployment(ctx, kubeconfigPath, kustomization, cascade, out)
		if err != nil {
			return argobotRemoved, err
		}
		if removed && cascade {
			argobotRemoved = true
		}
	}
	return argobotRemoved, nil
}

// clusterControllerNamespace returns the namespace controller runs in, and
// keeps its registry credentials in.
func clusterControllerNamespace(controller string) string {
	switch controller {
	case clusterControllerSveltos:
		return clusterSveltosNamespace
	case clusterControllerFlux:
		return clusterFluxNamespace
	}
	return clusterArgoNamespace
}
