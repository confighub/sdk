// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"os"

	"github.com/spf13/cobra"
)

var gitopsCmd = &cobra.Command{
	Use:   "gitops",
	Short: "GitOps commands",
	Long: getCommandHelp(`[EXPERIMENTAL] The gitops subcommands automate the process of discovering GitOps resources
(ArgoCD Applications, Flux HelmReleases/Kustomizations) from a Kubernetes target, rendering
them, and creating the corresponding ConfigHub units and links.

Examples:
`+"```"+`
  # Discover GitOps resources on a target
  cub gitops discover --space my-space my-k8s-target

  # Import discovered resources, rendering them via a render target
  cub gitops import --space my-space my-k8s-target my-render-target

  # Clean up the discover unit for a target
  cub gitops cleanup --space my-space my-k8s-target
`+"```"+`
`, ""),
	PersistentPreRunE: spacePreRunE,
}

// Shared flag for discover and import subcommands
var whereResource string

// Constants for discover unit naming
const (
	discoverTargetLabel = "TargetID"
	discoverSlugPrefix  = "discover-"
)

func init() {
	if os.Getenv("CONFIGHUB_EXPERIMENTAL") != "" {
		addSpaceFlags(gitopsCmd)
		rootCmd.AddCommand(gitopsCmd)
	}
}

// discoverUnitSlug generates the slug for a discover unit based on the target slug and space ID.
func discoverUnitSlug(targetSlug string, spaceID string) string {
	raw := makeSlug(discoverSlugPrefix + targetSlug + "-" + spaceID)
	return truncateWithHash(raw, MaxSlugLength)
}
