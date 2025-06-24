// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"errors"
	"fmt"
	"time"

	goclientnew "github.com/confighub/sdk/openapi/goclient-new"
	"github.com/go-openapi/strfmt"
	"github.com/google/uuid"
	"github.com/spf13/cobra"
)

var unitUpdateCmd = &cobra.Command{
	Use:   "update <slug or id> [config-file]",
	Short: "Update a unit",
	Long:  `Update a unit. Use '-' as file name to read from stdin if not reading metadata from stdin`,
	Args:  cobra.RangeArgs(1, 2),
	RunE:  unitUpdateCmdRun,
}

var (
	changeDescription string
	revisionNum       int64
	isUpgrade         bool
)

func init() {
	addStandardUpdateFlags(unitUpdateCmd)
	unitUpdateCmd.Flags().StringVar(&changeDescription, "change-desc", "", "change description")
	unitUpdateCmd.Flags().Int64Var(&revisionNum, "restore", 0, "revision number to restore")
	unitUpdateCmd.Flags().BoolVar(&isUpgrade, "upgrade", false, "upgrade the unit to the latest version of its upstream unit")
	enableWaitFlag(unitUpdateCmd)
	unitCmd.AddCommand(unitUpdateCmd)
}

func checkConflictingArgs(args []string) {
	if revisionNum != 0 && (isUpgrade || len(args) > 1) {
		failOnError(fmt.Errorf("only one of --restore, --upgrade, or config-file should be specified"))
	}
}

func unitUpdateCmdRun(cmd *cobra.Command, args []string) error {
	checkConflictingArgs(args)
	newParams := &goclientnew.UpdateUnitParams{}
	currentUnit, err := apiGetUnitFromSlug(args[0])
	if err != nil {
		return err
	}

	if flagPopulateModelFromStdin {
		// TODO: this could clobber a lot of fields
		if err := populateNewModelFromStdin(currentUnit); err != nil {
			return err
		}
	} else if flagReplaceModelFromStdin {
		// TODO: this could clobber a lot of fields
		existingUnit := currentUnit
		currentUnit = new(goclientnew.Unit)
		// Before reading from stdin so it can be overridden by stdin
		currentUnit.Version = existingUnit.Version
		if err := populateNewModelFromStdin(currentUnit); err != nil {
			return err
		}
		// After reading from stdin so it can't be clobbered by stdin
		currentUnit.OrganizationID = existingUnit.OrganizationID
		currentUnit.SpaceID = existingUnit.SpaceID
		currentUnit.UnitID = existingUnit.UnitID
	}
	err = setLabels(&currentUnit.Labels)
	if err != nil {
		return err
	}

	spaceID := uuid.MustParse(selectedSpaceID)
	// If this was set from stdin, it will be overridden
	currentUnit.SpaceID = spaceID

	if revisionNum != 0 {
		if revisionNum < 0 {
			// a negative value means it's relative to head revision num
			revisionNum = int64(currentUnit.HeadRevisionNum) + revisionNum
		}
		rev, err := apiGetRevisionFromNumber(revisionNum, currentUnit.UnitID.String())
		failOnError(err)
		// TODO: this should read RevisionID, but stays revision_id in the query parameter call
		newParams.RevisionId = &rev.RevisionID
	}
	if changeDescription != "" {
		currentUnit.LastChangeDescription = changeDescription
	}

	// Read test payload
	if len(args) > 1 {
		var content strfmt.Base64
		if args[1] == "-" {
			if flagPopulateModelFromStdin {
				failOnError(errors.New("can't read both entity attributes and config data from stdin"))
			}
			content, err = readStdin()
			if err != nil {
				return err
			}
		} else {
			content = readFile(args[1])
		}
		currentUnit.Data = content.String()
	}
	if isUpgrade {
		newParams.Upgrade = &isUpgrade
	}

	unitDetails, err := updateUnit(spaceID, currentUnit, newParams)
	if err != nil {
		return err
	}
	if wait {
		err = awaitTriggersRemoval(unitDetails)
		if err != nil {
			return err
		}
	}
	displayUpdateResults(unitDetails, "unit", args[0], unitDetails.UnitID.String(), displayUnitDetails)
	return nil
}

func updateUnit(spaceID uuid.UUID, currentUnit *goclientnew.Unit, params *goclientnew.UpdateUnitParams) (*goclientnew.Unit, error) {
	updatedRes, err := cubClientNew.UpdateUnitWithResponse(ctx, spaceID, currentUnit.UnitID, params, *currentUnit)
	if IsAPIError(err, updatedRes) {
		return nil, InterpretErrorGeneric(err, updatedRes)
	}

	return updatedRes.JSON200, nil
}

func awaitTriggersRemoval(unitDetails *goclientnew.Unit) error {
	// TODO: Implement configurable timeout, similar to awaitCompletion
	var err error
	unitID := unitDetails.UnitID
	tries := 0
	numTries := 100
	ms := 25
	maxMs := 250
	done := false
	for tries < numTries {
		if unitDetails.ApplyGates == nil {
			done = true
			break
		}
		_, awaitingTriggers := unitDetails.ApplyGates["awaiting/triggers"]
		if !awaitingTriggers {
			done = true
			break
		}
		time.Sleep(time.Duration(ms) * time.Millisecond)
		ms *= 2
		if ms > maxMs {
			ms = maxMs
		}
		tries++
		unitDetails, err = apiGetUnitInSpace(unitID.String(), unitDetails.SpaceID.String())
		if err != nil {
			return err
		}
	}
	if !done {
		return errors.New("triggers didn't execute on unit " + unitDetails.Slug)
	}
	return nil
}
