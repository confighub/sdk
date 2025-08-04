// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"github.com/confighub/sdk/configkit/k8skit"
	"github.com/spf13/cobra"
)

// helmCmd is the top-level command group for Helm-related operations.
var helmCmd = &cobra.Command{
	Use:               "helm",
	Short:             "Helm commands",
	Long:              "Interact with Helm charts from the ConfigHub CLI.",
	PersistentPreRunE: spacePreRunE, // Re-use the space selection mechanism used elsewhere
}

// Helm label constants
const (
	HelmChartLabel   = "HelmChart"
	HelmReleaseLabel = "HelmRelease"
	AbstractLabel    = "Abstract"
)

func init() {
	addSpaceFlags(helmCmd)
	rootCmd.AddCommand(helmCmd) // helmCmd here refers to the package-level variable
}

// splitHelmResources separates rendered Helm resources into CRDs and regular resources
func splitHelmResources(renderedResources map[string]string, chartName string) (*k8skit.SplitResourcesResult, error) {
	return k8skit.SplitResources(renderedResources, chartName)
}
