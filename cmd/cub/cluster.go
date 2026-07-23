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
	// through this annotation. `cub cluster up` keeps those Units in the cluster
	// Space itself, so the value is the cluster Space slug — the annotation
	// stays a stable contract even if the Units move to a dedicated Space later.
	clusterAnnotationArgoAppsSpace = "confighub.com/argo-apps-space"
)

// Slugs of the ConfigHub entities `cub cluster up` creates in each Space.
// (argobot-specific constants live in cluster_argobot.go.)
const (
	clusterOCIWorkerSlug = "oci-worker"
	clusterOCITargetSlug = "oci"
	clusterRootUnitSlug  = "root"
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
