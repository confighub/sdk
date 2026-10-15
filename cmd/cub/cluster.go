// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"fmt"

	"github.com/spf13/cobra"
)

// Annotation/label keys cub writes onto every cluster Space `cub cluster up`
// or `cub cluster attach` creates.
const (
	clusterAnnotationName      = "confighub.com/cluster-name"
	clusterAnnotationPortRange = "confighub.com/cluster-port-range"
	clusterAnnotationHost      = "confighub.com/cluster-host"
	clusterAnnotationArgoPort  = "confighub.com/cluster-argo-port"
	// clusterAnnotationOrigin records how the cluster came to be cub-managed:
	// clusterOriginKind for a kind cluster `cub cluster up` created and owns the
	// lifecycle of, clusterOriginAttach for a pre-existing cluster `cub cluster
	// attach` wired in, which cub never creates or destroys. `cub cluster down`
	// and `cub cluster detach` read it to refuse each other's clusters. Absent
	// on Spaces from before attach existed, which are all kind clusters.
	clusterAnnotationOrigin = "confighub.com/cluster-origin"
	clusterOriginKind       = "kind"
	clusterOriginAttach     = "attach"
	// clusterAnnotationBotSpace and clusterAnnotationBotChannel record where the
	// cluster's bot (argobot) lives: its variant Space slug and the Channel slug
	// in the cluster Space. `cub cluster detach` reads them rather than deriving
	// the names from the cluster name, so the bot can be renamed or moved
	// without the teardown losing track of it. Absent when the cluster was
	// brought up with --no-argobot, or before they were recorded.
	clusterAnnotationBotSpace   = "confighub.com/cluster-bot-space"
	clusterAnnotationBotChannel = "confighub.com/cluster-bot-channel"
	// clusterAnnotationProvider records where `cub cluster attach` got the
	// cluster's credentials (clusterProviderKubeconfig, clusterProviderEKS,
	// clusterProviderKind), and clusterAnnotationProviderCluster the cluster's
	// name at that provider when the provider names clusters itself. `cub
	// cluster up` writes neither: its origin says kind already.
	clusterAnnotationProvider        = "confighub.com/cluster-provider"
	clusterAnnotationProviderCluster = "confighub.com/cluster-provider-cluster"
	// clusterAnnotationController names the controller `cub cluster up`
	// installed, clusterControllerArgo, clusterControllerSveltos or
	// clusterControllerFlux, on the cluster Space and its OCI target.
	clusterAnnotationController = "confighub.com/cluster-controller"
	// clusterAnnotationSveltosPort is the Sveltos dashboard's NodePort, the
	// counterpart of clusterAnnotationArgoPort.
	clusterAnnotationSveltosPort = "confighub.com/cluster-sveltos-port"
	// clusterLabel is the queryable marker label on every cluster Space.
	clusterLabel = "confighub.com/cluster"

	// clusterAnnotationTargetUI is read by the ConfigHub UI to render a deep
	// link to the Target's external UI. The literal "{slug}" token in the
	// value is substituted by the UI with the Space slug. `cub cluster up`
	// sets it on the OCI target so a Unit links straight to its Argo CD
	// Application.
	clusterAnnotationTargetUI = "URL-TargetUI"

	// clusterAnnotationArgoAppsSpace is stamped on the OCI target and names the
	// Space that holds the cluster's Argo CD Application Units (the app-of-apps
	// and every child app). API clients resolve that Space from the target
	// through this annotation. `cub cluster up` keeps those Units in a dedicated
	// apps Space (the target-prefix Space slug + clusterArgoAppsSuffix), separate
	// from the Space that holds the target, so the value is that apps
	// Space slug rather than the target's own Space.
	clusterAnnotationArgoAppsSpace = "confighub.com/argo-apps-space"

	// clusterAnnotationSveltosAppsSpace is the Sveltos counterpart of
	// clusterAnnotationArgoAppsSpace: stamped on a Sveltos cluster's OCI target,
	// it names the apps Space holding the root and child ClusterProfile Units,
	// where `cub variant create --target` adds a deployment's ClusterProfile.
	clusterAnnotationSveltosAppsSpace = "confighub.com/sveltos-apps-space"

	// clusterAnnotationSveltosPollInterval records, on a Sveltos cluster's OCI
	// target, Sveltos's own poll interval (remoteURL.interval) for the cluster's
	// profiles, so `cub variant create --target` gives a deployment's profile the
	// same one as the root.
	clusterAnnotationSveltosPollInterval = "confighub.com/sveltos-poll-interval"

	// clusterAnnotationFluxAppsSpace is the Flux counterpart of
	// clusterAnnotationArgoAppsSpace: stamped on a Flux cluster's OCI target, it
	// names the apps Space holding the root and child Flux Units, where `cub
	// variant create --target` adds a deployment's OCIRepository and
	// Kustomization.
	clusterAnnotationFluxAppsSpace = "confighub.com/flux-apps-space"

	// clusterAnnotationFluxPollInterval records, on a Flux cluster's OCI target,
	// the OCIRepositories' poll interval (spec.interval), so `cub variant create
	// --target` gives a deployment's OCIRepository the same one as the root.
	clusterAnnotationFluxPollInterval = "confighub.com/flux-poll-interval"

	// clusterAnnotationArgobot is "true" on the OCI target of a cluster `cub
	// cluster up` installed argobot in. With Flux it decides when a deployment's
	// Units are released (see createVariantFluxApp).
	clusterAnnotationArgobot = "confighub.com/cluster-argobot"
)

// Slugs of the ConfigHub entities an attachment creates. The target lives in
// the cluster (target-prefix) Space; the root Unit lives in the dedicated apps
// Space; the cluster's service account is org-scoped and named by
// clusterServiceAccountSlug. (argobot-specific constants live in
// cluster_argobot.go.)
const (
	clusterTargetSlug   = "target"
	clusterRootUnitSlug = "root"

	// clusterGateTriggerSlug is the default gate Trigger `cub cluster up`
	// creates in the cluster Space: vet-placeholders on every Mutation, so a
	// Release carrying an unfilled confighubplaceholder cannot be published to
	// the cluster. Skipped with --no-placeholder-gate.
	clusterGateTriggerSlug = "no-placeholders"

	// clusterArgoAppsSuffix is appended to the cluster (target-prefix) Space
	// slug to form the slug of the dedicated Space holding the Argo Application
	// Units. Keeping the target out of that Space means the apps Space's
	// ReleaseTargetID points at a target in a different Space, so it is never
	// self-referential — the cluster Space is a pure namespace, the apps Space is
	// the config bundle.
	clusterArgoAppsSuffix = "-argo-apps"
)

// The providers `cub cluster attach --provider` reads a cluster's credentials
// from: the user's kubeconfig, the AWS CLI for EKS, or kind for a kind cluster
// made by hand.
const (
	clusterProviderKubeconfig = "kubeconfig"
	clusterProviderEKS        = "eks"
	clusterProviderKind       = "kind"
)

// The controllers `cub cluster up --controller` installs.
const (
	clusterControllerArgo    = "argo"
	clusterControllerSveltos = "sveltos"
	clusterControllerFlux    = "flux"
)

// Default search range and window size for NodePort allocation. The window
// covers Argo's NodePort + a handful of user-allocatable NodePorts.
const (
	clusterPortRangeStart = 30000
	clusterPortRangeEnd   = 30099
	clusterPortRangeSize  = 10
)

var clusterCmd = &cobra.Command{
	Use:   "cluster",
	Short: "Manage clusters wired into ConfigHub via Argo CD or Sveltos",
	Long: `The cluster subcommands wire a Kubernetes cluster into ConfigHub: a controller
in the cluster (Argo CD, or Sveltos) pulls its config from ConfigHub's OCI
registry as the cluster's service account, so adding an Argo Application is a
single cub unit create and no worker runs anywhere.

  cub cluster attach   wire a cluster you already have into ConfigHub
  cub cluster detach   the inverse: remove the wiring, leave the cluster running
  cub cluster up       create a local kind cluster, then attach it
  cub cluster down     the inverse: detach, then delete the kind cluster
  cub cluster list     every cluster this machine holds credentials for, and the
                       ones in the current context's organization

A cluster is wired into exactly one organization on one server. cub records
which one beside the cluster's kubeconfig, so list, open and down find the
right context on their own rather than needing it to be the current one.`,
	PersistentPreRunE: globalPreRun,
}

func init() {
	rootCmd.AddCommand(clusterCmd)
}

// clusterNameArg resolves a command's cluster name from its positional
// argument, or from the --name flag kept as an alias for the scripts and docs
// written against it. Both given and disagreeing is an error rather than a
// guess. required says whether an empty result is an error: up generates a
// name when none is given, the others act on one cluster and need to know which.
func clusterNameArg(args []string, flagName, command string, required bool) (string, error) {
	name := flagName
	if len(args) == 1 {
		if name != "" && name != args[0] {
			return "", fmt.Errorf("cluster named twice: %q as an argument and %q via --name", args[0], name)
		}
		name = args[0]
	}
	if name == "" && required {
		return "", fmt.Errorf("a cluster name is required: cub cluster %s <name>", command)
	}
	return name, nil
}
