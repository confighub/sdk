// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"errors"

	"github.com/confighub/sdk/core/cubapi"
	"github.com/spf13/cobra"
)

var unitOpenArgs struct {
	edit      bool
	revisions bool
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
`+"```"+`
`, ""),
	RunE: unitOpenCmdRun,
}

func init() {
	enableOpenFlags(unitOpenCmd)
	unitOpenCmd.Flags().BoolVar(&unitOpenArgs.edit, "edit", false, "Open the unit's edit tab")
	unitOpenCmd.Flags().BoolVar(&unitOpenArgs.revisions, "revisions", false, "Open the unit's revisions tab")
	unitOpenCmd.MarkFlagsMutuallyExclusive("edit", "revisions")
	unitCmd.AddCommand(unitOpenCmd)
}

func unitOpenCmdRun(_ *cobra.Command, args []string) error {
	if len(args) == 0 {
		if unitOpenArgs.edit || unitOpenArgs.revisions {
			return errors.New("--edit and --revisions require a unit")
		}
		return openWebUI(cubapi.GetUnitListURL(webUIServerURL()))
	}
	// The unit list page is org-wide, so this command is OrgLevel, but resolving
	// a unit slug still needs one concrete space.
	if selectedSpaceID == "*" {
		return errors.New("space is required to open a specific unit. Set with --space option or set in context with the context sub-command")
	}
	unit, err := apiGetUnitFromSlug(args[0], "UnitID,SpaceID")
	if err != nil {
		return err
	}
	serverURL := webUIServerURL()
	spaceID := unit.SpaceID.String()
	unitID := unit.UnitID.String()
	switch {
	case unitOpenArgs.edit:
		return openWebUI(cubapi.GetUnitEditURL(serverURL, spaceID, unitID))
	case unitOpenArgs.revisions:
		return openWebUI(cubapi.GetUnitRevisionsURL(serverURL, spaceID, unitID))
	default:
		return openWebUI(cubapi.GetUnitDetailURL(serverURL, spaceID, unitID))
	}
}
