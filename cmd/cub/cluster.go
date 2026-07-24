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
	// from the Space that holds the worker and target, so the value is that apps
	// Space slug rather than the target's own Space.
	clusterAnnotationArgoAppsSpace = "confighub.com/argo-apps-space"
)

// Slugs of the ConfigHub entities `cub cluster up` creates. The worker and
// target live in the cluster (target-prefix) Space; the root Unit lives in the
// dedicated apps Space. (argobot-specific constants live in cluster_argobot.go.)
const (
	clusterWorkerSlug   = "worker"
	clusterTargetSlug   = "target"
	clusterRootUnitSlug = "root"

	// clusterArgoAppsSuffix is appended to the cluster (target-prefix) Space
	// slug to form the slug of the dedicated Space holding the Argo Application
	// Units. Keeping the worker/target out of that Space means the apps Space's
	// ReleaseTargetID points at a target in a different Space, so it is never
	// self-referential — the cluster Space is a pure namespace, the apps Space is
	// the config bundle.
	clusterArgoAppsSuffix = "-argo-apps"
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
	Long: `The cluster subcommands bring up a local kind cluster, install Argo CD into
it, and wire it into ConfigHub so that adding a new Argo Application is a
single cub unit create. Argo pulls its config from ConfigHub's OCI registry —
no separate worker pod runs in your cluster.`,
	PersistentPreRunE: globalPreRun,
}

func init() {
	rootCmd.AddCommand(clusterCmd)
}
