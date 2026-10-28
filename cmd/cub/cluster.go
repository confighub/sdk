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
	clusterAnnotationName = "confighub.com/cluster-name"
	// clusterAnnotationPortRange is also the label `cub cluster create` puts on
	// a kind cluster's control-plane node, which is how `cub cluster attach
	// --provider kind` learns the window later.
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
	// brought up with --no-k8sbot, or before they were recorded.
	clusterAnnotationBotSpace   = "confighub.com/cluster-bot-space"
	clusterAnnotationBotChannel = "confighub.com/cluster-bot-channel"
	// clusterAnnotationProvider records where `cub cluster attach` got the
	// cluster's credentials (clusterProviderKubeconfig, clusterProviderEKS,
	// clusterProviderKind), and clusterAnnotationProviderCluster the cluster's
	// name at that provider when the provider names clusters itself. `cub
	// cluster up` attaches through the kind provider, so it writes both.
	clusterAnnotationProvider        = "confighub.com/cluster-provider"
	clusterAnnotationProviderCluster = "confighub.com/cluster-provider-cluster"
	// clusterAnnotationController names the controller `cub cluster up`
	// installed, clusterControllerArgo, clusterControllerSveltos or
	// clusterControllerFlux, on the cluster Space and its OCI target.
	clusterAnnotationController = "confighub.com/cluster-controller"
	// clusterAnnotationSveltosPort is the Sveltos dashboard's NodePort, the
	// counterpart of clusterAnnotationArgoPort.
	clusterAnnotationSveltosPort = "confighub.com/cluster-sveltos-port"
	// clusterAnnotationStage is on the cluster Space of a cluster that is partly
	// set up and nothing has attached yet: clusterStageController after `cub
	// cluster controller-install` (the controller, the Space and its OCI
	// target), clusterStageServiceAccount after `cub cluster
	// svc-account-bootstrap` (the cluster's service account too, with its key
	// and token in the cluster). `cub cluster attach` picks such a cluster up
	// instead of refusing it as already cub-managed, and removes the annotation
	// when done.
	clusterAnnotationStage     = "confighub.com/cluster-stage"
	clusterStageController     = "controller"
	clusterStageServiceAccount = "service-account"
	// clusterLabel is the queryable marker label on every cluster Space.
	clusterLabel = "confighub.com/cluster"

	// clusterAnnotationTargetUI is read by the ConfigHub UI to render a deep
	// link to the Target's external UI. The literal "{slug}" token in the
	// value is substituted by the UI with the Space slug. `cub cluster up`
	// sets it on the OCI target so a Unit links straight to its Argo CD
	// Application. Each controller-app Unit in the apps Space carries the same
	// key with the name already substituted, linking it to its own Application
	// or ClusterProfile (see clusterUnitTargetUIAnnotations).
	clusterAnnotationTargetUI = "URL-TargetUI"

	// clusterAnnotationClusterUnitID is set on each Space a cluster's controller
	// deploys (the apps Space and every deployment variant): the UnitID of the
	// Unit in the apps Space holding the Argo CD Application, Sveltos
	// ClusterProfile or Flux Kustomization that deploys it. Like UpstreamSpaceID,
	// it is an ID, so a client finds the Unit without knowing its Space.
	clusterAnnotationClusterUnitID = "ClusterUnitID"

	// clusterAnnotationAppsSpace is stamped on the OCI target and names the
	// Space that holds the cluster's controller Units: the root and every child
	// Argo CD Application, Sveltos ClusterProfile or Flux OCIRepository and
	// Kustomization, where `cub variant create --target` adds a deployment's.
	// API clients resolve that Space from the target through this annotation.
	// `cub cluster up` keeps those Units in a dedicated apps Space, separate from
	// the Space that holds the target, so the value is that apps Space slug
	// rather than the target's own Space. The key is the same for every
	// controller; clusterAnnotationController on the same target says which.
	clusterAnnotationAppsSpace = "confighub.com/apps-space"

	// clusterAnnotationPrune is "true" on the OCI target of a cluster brought up
	// or attached with --prune: removing a deployment's Unit from the apps Space
	// removes what it deployed, and `cub variant create --target` makes each
	// deployment prune what it deployed. With Argo CD the root Application
	// prunes, and a deployment's Application gets prune and the resources
	// finalizer; with Flux the root and each deployment's Kustomization prune;
	// with Sveltos a deployment's ClusterProfile withdraws what it deployed when
	// it is deleted (Sveltos's root always deletes it, and a profile always
	// deletes a resource that leaves its Release). The key is the same for every
	// controller, like clusterAnnotationAppsSpace.
	clusterAnnotationPrune = "confighub.com/cluster-prune"

	// clusterAnnotationSveltosPollInterval records, on a Sveltos cluster's OCI
	// target, Sveltos's own poll interval (remoteURL.interval) for the cluster's
	// profiles, so `cub variant create --target` gives a deployment's profile the
	// same one as the root.
	clusterAnnotationSveltosPollInterval = "confighub.com/sveltos-poll-interval"

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
	Short: "Manage clusters wired into ConfigHub via Argo CD, Sveltos or Flux",
	Long: `The cluster subcommands wire a Kubernetes cluster into ConfigHub: a controller
in the cluster (Argo CD, Sveltos or Flux) pulls its config from ConfigHub's OCI
registry as the cluster's service account, so adding an Argo Application is a
single cub unit create and no worker runs anywhere.

  cub cluster attach   wire a cluster you already have into ConfigHub (Argo CD only)
  cub cluster detach   the inverse: remove the wiring, leave the cluster running
  cub cluster create   create a local kind cluster, without attaching it
  cub cluster controller-install
                       install the controller and create the cluster's target,
                       the first part of attach
  cub cluster svc-account-bootstrap
                       create the cluster's service account and put its
                       credentials in the cluster, the second part
  cub cluster app-bootstrap
                       create the apps space and bootstrap the root, the third
                       part, which finishes attaching without argobot
  cub cluster k8sbot-install
                       install argobot in a cluster attached without it, the
                       last part
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
