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
	Long:  getUnitUpdateHelp(),
	Args:  cobra.RangeArgs(1, 2),
	RunE:  unitUpdateCmdRun,
}

func getUnitUpdateHelp() string {
	baseHelp := `Update an existing unit in a space. Units can be updated with new configuration data, restored to previous revisions, or upgraded from upstream units.

Like other ConfigHub entities, Units have metadata, which can be partly set on the command line
and otherwise read from stdin using the flag --from-stdin or --replace-from-stdin.

Unit configuration data can be provided in multiple ways:
  1. From a local or remote configuration file, or from stdin (by specifying "-")
  2. By restoring to a previous revision (using --restore)
  3. By upgrading from the upstream unit (using --upgrade)

Examples:
  # Update a unit from a local YAML file
  cub unit update --space my-space myunit config.yaml

  # Update a unit from a file:// URL
  cub unit update --space my-space myunit file:///path/to/config.yaml

  # Update a unit from a remote HTTPS URL
  cub unit update --space my-space myunit https://example.com/config.yaml

  # Update a unit with config from stdin
  cub unit update --space my-space myunit -

  # Combine Unit JSON metadata from stdin with config data from file
  cub unit update --space my-space myunit config.yaml --from-stdin

  # Restore a unit to revision 5
  cub unit update --space my-space myunit --restore 5

  # Restore a unit to 2 revisions ago (relative to head)
  cub unit update --space my-space myunit --restore -2

  # Upgrade a unit to match its upstream unit
  cub unit update --space my-space myunit --upgrade

  # Update with a change description
  cub unit update --space my-space myunit config.yaml --change-desc "Updated database configuration"`

	agentContext := `Essential for maintaining and evolving configuration in ConfigHub.

Agent update workflow:
1. Identify the unit to update by slug or ID
2. Choose update method: new config, restore, or upgrade
3. Update unit and wait for triggers to complete validation
4. Check for any validation issues or apply gates

Update methods:

From local file:
  cub unit update --space SPACE my-unit config.yaml --wait

From stdin (useful for programmatic updates):
  cat config.yaml | cub unit update --space SPACE my-unit - --wait

Restore to previous revision:
  cub unit update --space SPACE my-unit --restore 3 --wait

Upgrade from upstream:
  cub unit update --space SPACE my-unit --upgrade --wait

Key flags for agents:
- --wait: Wait for triggers and validation to complete (recommended)
- --json: Get structured response with unit ID and details
- --verbose: Show detailed update information
- --from-stdin: Read additional metadata from stdin
- --replace-from-stdin: Replace entire metadata from stdin
- --restore: Restore to a specific revision number (positive) or relative to head (negative)
- --upgrade: Upgrade to match the latest version of upstream unit
- --change-desc: Add a description for this change
- --label: Update labels for organization and filtering

Post-update workflow:
1. Use 'function do get-placeholders' to check for placeholder values
2. Use 'function do' commands to modify configuration as needed
3. Use 'unit approve' if approval is required
4. Use 'unit apply' to deploy to live infrastructure

Important: Only one of config-file, --restore, or --upgrade should be specified per update operation.`

	return getCommandHelp(baseHelp, agentContext)
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
		if args[1] == "-" && flagPopulateModelFromStdin {
			return errors.New("can't read both entity attributes and config data from stdin")
		}
		content, err := fetchContent(args[1])
		if err != nil {
			return fmt.Errorf("failed to read config: %w", err)
		}
		var base64Content strfmt.Base64 = content
		currentUnit.Data = base64Content.String()
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
