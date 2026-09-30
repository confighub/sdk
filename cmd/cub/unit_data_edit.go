// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"bytes"
	"fmt"
	"os"

	"github.com/cockroachdb/errors"
	goclientnew "github.com/confighub/sdk/core/openapi/goclient-new"
	"github.com/spf13/cobra"
)

var unitDataEditCmd = &cobra.Command{
	Use:   "data-edit <unit>",
	Short: "Edit the config data of a unit in your system's editor",
	Long: getCommandHelp(`Open the config data of a unit, as 'cub unit data' shows it, in the editor named by
the EDITOR environment variable, or vi if it is not set. When the editor exits, the changes are
saved as a new revision. If the data was not changed, no update is made.`, ""),
	Args: cobra.ExactArgs(1),
	RunE: unitDataEditCmdRun,
}

func init() {
	enableWaitFlag(unitDataEditCmd)
	unitDataEditCmd.Flags().StringVar(&changesetSlug, "changeset", "", "changeset to associate the unit with")
	enableOptionalSpace(unitDataEditCmd)
	unitCmd.AddCommand(unitDataEditCmd)
}

func unitDataEditCmdRun(cmd *cobra.Command, args []string) error {
	currentUnit, err := resolveUnit(args[0], selectedSpaceID, "*") // get all fields for RMW
	if err != nil {
		return err
	}

	spaceID := currentUnit.Unit.SpaceID
	currentUnit.Unit.LastChangeDescription = "CLI edit"

	params := &goclientnew.UpdateUnitParams{}
	if changesetSlug != "" {
		if changesetSlug == "-" {
			// Special value to remove the changeset (only valid in patch mode)
			return errors.New("data-edit cannot remove a changeset")
		}
		changesetUUID, err := resolveChangeSetID(changesetSlug)
		if err != nil {
			return err
		}
		if currentUnit.Unit.ChangeSetID != nil && *currentUnit.Unit.ChangeSetID != changesetUUID {
			return fmt.Errorf("specified ChangeSet %s does not match unit's current ChangeSet %s", changesetSlug, currentUnit.Unit.ChangeSetID.String())
		}
		currentUnit.Unit.ChangeSetID = &changesetUUID
		params.ChangeSetId = &changesetUUID
	} else if currentUnit.Unit.ChangeSetID != nil {
		return fmt.Errorf("unit is in ChangeSet %s; use --changeset", currentUnit.Unit.ChangeSetID.String())
	}

	currentData, err := fetchUnitData(currentUnit.Unit.SpaceID, currentUnit.Unit.UnitID)
	if err != nil {
		return err
	}
	currentContent := []byte(currentData)
	updatedContent, path, err := editInEditor(currentContent, "*.yaml")
	if err != nil {
		return err
	}
	defer os.Remove(path)

	if bytes.Equal(currentContent, updatedContent) {
		fmt.Println("No changes made")
		return nil
	}
	// Edit is a read+modify+write, so it is not considered an external merge source. The
	// configuration is written through the data endpoint; the Unit itself is unchanged.
	editParams, err := unitDataParams(currentUnit.Unit.LastChangeDescription, changeSetIDForDataWrite(currentUnit.Unit))
	if err != nil {
		return err
	}
	if _, err := putUnitData(spaceID, currentUnit.Unit.UnitID, string(updatedContent), editParams); err != nil {
		return err
	}
	unitDetails, err := resolveUnit(currentUnit.Unit.UnitID.String(), spaceID.String(), "*")
	if err != nil {
		return err
	}
	if wait {
		err = awaitTriggersRemoval(unitDetails.Unit)
		if err != nil {
			return err
		}
	}
	displayUpdateResults(unitDetails, "unit", args[0], unitDetails.Unit.UnitID.String(), displayExtendedUnitDetails)
	return nil
}
