// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"github.com/spf13/cobra"
)

// Annotation/label keys cub writes onto every Space `cub cluster up` creates.
const (
	clusterAnnotationName      = "confighub.com/cluster-name"
	clusterAnnotationPortRange = "confighub.com/cluster-port-range"
	clusterAnnotationHost      = "confighub.com/cluster-host"
	clusterAnnotationArgoPort  = "confighub.com/cluster-argo-port"
	// clusterAnnotationController names the controller `cub cluster up`
	// installed, clusterControllerArgo or clusterControllerSveltos, on the
	// cluster Space and its OCI target.
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
)

// Slugs of the ConfigHub entities `cub cluster up` creates. The target lives in
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

// The controllers `cub cluster up --controller` installs.
const (
	clusterControllerArgo    = "argo"
	clusterControllerSveltos = "sveltos"
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
	Short: "Manage local kind clusters wired into ConfigHub",
	Long: `The cluster subcommands bring up a local kind cluster, install Argo CD (or
Sveltos) into it, and wire it into ConfigHub so that adding a new Argo
Application is a single cub unit create. The controller pulls its config from
ConfigHub's OCI registry as the cluster's service account — no worker runs
anywhere.`,
	PersistentPreRunE: globalPreRun,
}

func init() {
	rootCmd.AddCommand(clusterCmd)
}
