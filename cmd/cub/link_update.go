// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	goclientnew "github.com/confighub/sdk/openapi/goclient-new"
	"github.com/google/uuid"
	"github.com/spf13/cobra"
)

var linkUpdateCmd = &cobra.Command{
	Use:   "update <link slug or id> <from unit slug> <to unit slug> [<to space slug>]",
	Short: "Update a link",
	Long:  `Update a link.`,
	Args:  cobra.RangeArgs(3, 4),
	RunE:  linkUpdateCmdRun,
}

func init() {
	addStandardUpdateFlags(linkUpdateCmd)
	enableWaitFlag(linkUpdateCmd)
	linkCmd.AddCommand(linkUpdateCmd)
}

func linkUpdateCmdRun(cmd *cobra.Command, args []string) error {
	currentLink, err := apiGetLinkFromSlug(args[0])
	if err != nil {
		return err
	}

	spaceID := uuid.MustParse(selectedSpaceID)
	currentLink.SpaceID = spaceID
	if flagPopulateModelFromStdin {
		// TODO: this could clobber a lot of fields
		if err := populateNewModelFromStdin(currentLink); err != nil {
			return err
		}
	} else if flagReplaceModelFromStdin {
		// TODO: this could clobber a lot of fields
		existingLink := currentLink
		currentLink = new(goclientnew.Link)
		// Before reading from stdin so it can be overridden by stdin
		currentLink.Version = existingLink.Version
		if err := populateNewModelFromStdin(currentLink); err != nil {
			return err
		}
		// After reading from stdin so it can't be clobbered by stdin
		currentLink.OrganizationID = existingLink.OrganizationID
		currentLink.SpaceID = existingLink.SpaceID
		currentLink.LinkID = existingLink.LinkID
	}
	err = setLabels(&currentLink.Labels)
	if err != nil {
		return err
	}

	// If this was set from stdin, it will be overridden
	currentLink.SpaceID = spaceID

	fromUnit, err := apiGetUnitFromSlug(args[1])
	if err != nil {
		return err
	}
	fromUnitID := fromUnit.UnitID
	toSpaceID := selectedSpaceID
	if len(args) == 4 {
		toSpace, err := apiGetSpaceFromSlug(args[3])
		if err != nil {
			return err
		}
		toSpaceID = toSpace.SpaceID.String()
	}
	toUnit, err := apiGetUnitFromSlugInSpace(args[2], toSpaceID)
	if err != nil {
		return err
	}
	toUnitID := toUnit.UnitID

	currentLink.FromUnitID = fromUnitID
	currentLink.ToUnitID = toUnitID
	currentLink.ToSpaceID = uuid.MustParse(toSpaceID)

	linkRes, err := cubClientNew.UpdateLinkWithResponse(ctx, spaceID, currentLink.LinkID, *currentLink)
	if IsAPIError(err, linkRes) {
		return InterpretErrorGeneric(err, linkRes)
	}

	linkDetails := linkRes.JSON200
	displayUpdateResults(linkDetails, "link", args[0], linkDetails.LinkID.String(), displayLinkDetails)
	if wait {
		if !quiet {
			tprint("Awaiting triggers...")
		}
		unitDetails, err := apiGetUnit(fromUnitID.String())
		if err != nil {
			return err
		}
		err = awaitTriggersRemoval(unitDetails)
		if err != nil {
			return err
		}
	}
	return nil
}
