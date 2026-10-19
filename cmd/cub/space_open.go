// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"errors"

	"github.com/confighub/sdk/core/cubapi"
	"github.com/spf13/cobra"
)

var spaceOpenArgs struct {
	inCluster bool
}

var spaceOpenCmd = &cobra.Command{
	Use:   "open [<name or id>]",
	Short: "Open spaces in the web UI",
	Args:  cobra.MaximumNArgs(1),
	Long: getCommandHelp(`Open the web UI on a space, or on the space list when no space is named.

Examples:
`+"```"+`
  # Open the space list
  cub space open

  # Open a specific space
  cub space open my-space

  # Print the URL instead of opening a browser
  cub space open my-space --print-url

  # Open the Argo CD Application or Sveltos ClusterProfile that deploys a space
  cub space open my-space --in-cluster
`+"```"+`
`, ""),
	RunE: spaceOpenCmdRun,
}

func init() {
	enableOpenFlags(spaceOpenCmd)
	spaceOpenCmd.Flags().BoolVar(&spaceOpenArgs.inCluster, "in-cluster", false,
		"Open the space's page in the UI of the cluster controller that deploys it, the URL-TargetUI of the unit its ClusterUnitID annotation names")
	spaceCmd.AddCommand(spaceOpenCmd)
}

func spaceOpenCmdRun(_ *cobra.Command, args []string) error {
	if len(args) == 0 {
		if spaceOpenArgs.inCluster {
			return errors.New("--in-cluster requires a space")
		}
		return openWebUI(cubapi.GetSpaceListURL(webUIServerURL()))
	}
	space, err := resolveSpace(args[0], "SpaceID,Slug,Annotations")
	if err != nil {
		return err
	}
	if spaceOpenArgs.inCluster {
		url, err := clusterControllerUIOfSpace(space.Space.Slug, space.Space.Annotations)
		if err != nil {
			return err
		}
		return openControllerUI(url)
	}
	return openWebUI(cubapi.GetSpaceDetailURL(webUIServerURL(), space.Space.SpaceID.String()))
}
