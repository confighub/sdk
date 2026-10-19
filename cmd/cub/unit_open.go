// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"errors"

	"github.com/confighub/sdk/core/cubapi"
	goclientnew "github.com/confighub/sdk/core/openapi/goclient-new"
	"github.com/spf13/cobra"
)

var unitOpenArgs struct {
	edit      bool
	revisions bool
	inCluster bool
}

var unitOpenCmd = &cobra.Command{
	Use:         "open [<name or id>]",
	Short:       "Open units in the web UI",
	Args:        cobra.MaximumNArgs(1),
	Annotations: map[string]string{"OrgLevel": ""},
	Long: getCommandHelp(`Open the web UI on a unit, or on the unit list when no unit is named.

Examples:
`+"```"+`
  # Open the unit list
  cub unit open

  # Open a specific unit
  cub unit open my-unit

  # Open a unit's config data in the web editor
  cub unit open my-unit --edit

  # Open a unit's revision history
  cub unit open my-unit --revisions

  # Print the URL instead of opening a browser
  cub unit open my-unit --print-url

  # Open the Argo CD Application or Sveltos ClusterProfile that deploys a unit
  cub unit open my-unit --in-cluster
`+"```"+`
`, ""),
	RunE: unitOpenCmdRun,
}

func init() {
	enableOpenFlags(unitOpenCmd)
	unitOpenCmd.Flags().BoolVar(&unitOpenArgs.edit, "edit", false, "Open the unit's edit tab")
	unitOpenCmd.Flags().BoolVar(&unitOpenArgs.revisions, "revisions", false, "Open the unit's revisions tab")
	unitOpenCmd.Flags().BoolVar(&unitOpenArgs.inCluster, "in-cluster", false,
		"Open the unit's page in the UI of the cluster controller that deploys it: the unit's own URL-TargetUI for an Argo CD Application or Sveltos ClusterProfile unit, else that of the unit its space's ClusterUnitID annotation names")
	unitOpenCmd.MarkFlagsMutuallyExclusive("edit", "revisions", "in-cluster")
	unitCmd.AddCommand(unitOpenCmd)
}

func unitOpenCmdRun(_ *cobra.Command, args []string) error {
	if len(args) == 0 {
		if unitOpenArgs.edit || unitOpenArgs.revisions || unitOpenArgs.inCluster {
			return errors.New("--edit, --revisions and --in-cluster require a unit")
		}
		return openWebUI(cubapi.GetUnitListURL(webUIServerURL()))
	}
	// Without --space the lookup is organization-wide: "space/slug" names the
	// space, and a bare slug found in more than one space is refused by the resolver.
	unit, err := resolveUnit(args[0], selectedSpaceID, "UnitID,SpaceID,Annotations")
	if err != nil {
		return err
	}
	if unitOpenArgs.inCluster {
		return unitOpenInCluster(unit.Unit)
	}
	serverURL := webUIServerURL()
	spaceID := unit.Unit.SpaceID.String()
	unitID := unit.Unit.UnitID.String()
	switch {
	case unitOpenArgs.edit:
		return openWebUI(cubapi.GetUnitEditURL(serverURL, spaceID, unitID))
	case unitOpenArgs.revisions:
		return openWebUI(cubapi.GetUnitRevisionsURL(serverURL, spaceID, unitID))
	default:
		return openWebUI(cubapi.GetUnitDetailURL(serverURL, spaceID, unitID))
	}
}

// unitOpenInCluster opens the controller UI page of what deploys the unit. A
// controller-app unit in a cluster's apps Space carries that link itself; any
// other unit is deployed with its Space, so the link is the Space's.
func unitOpenInCluster(unit *goclientnew.Unit) error {
	if url := unit.Annotations[clusterAnnotationTargetUI]; url != "" {
		return openControllerUI(url)
	}
	space, err := resolveSpace(unit.SpaceID.String(), "SpaceID,Slug,Annotations")
	if err != nil {
		return err
	}
	url, err := clusterControllerUIOfSpace(space.Space.Slug, space.Space.Annotations)
	if err != nil {
		return err
	}
	return openControllerUI(url)
}
