// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/cockroachdb/errors"
	goclientnew "github.com/confighub/sdk/core/openapi/goclient-new"
	"github.com/spf13/cobra"
)

var unitEditCmd = &cobra.Command{
	Use:   "edit <name>",
	Short: "Edit the config data of a unit in your system's editor",
	Long: getCommandHelp(`This command will pull down the latest revision of this unit and open it the editor specified in the
	        EDITOR environment variable or vi if the variable is not set. When the editor process exits,
					the changes will be saved as a new revision. If the contents were not changed, then no update will be made`, ""),
	Args: cobra.ExactArgs(1),
	RunE: unitEditCmdRun,
}

func init() {
	enableWaitFlag(unitEditCmd)
	unitEditCmd.Flags().StringVar(&changesetSlug, "changeset", "", "changeset to associate the unit with")
	unitCmd.AddCommand(unitEditCmd)
}

func unitEditCmdRun(cmd *cobra.Command, args []string) error {
	currentUnit, err := apiGetUnitFromSlug(args[0], "*") // get all fields for RMW
	if err != nil {
		return err
	}

	spaceID := currentUnit.SpaceID
	currentUnit.LastChangeDescription = "CLI edit"

	params := &goclientnew.UpdateUnitParams{}
	if changesetSlug != "" {
		if changesetSlug == "-" {
			// Special value to remove the changeset (only valid in patch mode)
			return errors.New("edit cannot remove a changeset")
		}
		changesetUUID, err := parseChangeSetSlug(changesetSlug)
		if err != nil {
			return err
		}
		if currentUnit.ChangeSetID != nil && *currentUnit.ChangeSetID != changesetUUID {
			return fmt.Errorf("specified ChangeSet %s does not match unit's current ChangeSet %s", changesetSlug, currentUnit.ChangeSetID.String())
		}
		currentUnit.ChangeSetID = &changesetUUID
		params.ChangeSetId = &changesetUUID
	} else if currentUnit.ChangeSetID != nil {
		return fmt.Errorf("unit is in ChangeSet %s; use --changeset", currentUnit.ChangeSetID.String())
	}

	tmpFile, err := os.CreateTemp("", "*.yaml")
	if err != nil {
		return err
	}
	defer os.Remove(tmpFile.Name())
	currentData, err := fetchUnitData(currentUnit.SpaceID, currentUnit.UnitID)
	if err != nil {
		return err
	}
	currentContent := []byte(currentData)
	_, err = tmpFile.Write(currentContent)
	if err != nil {
		return err
	}
	err = tmpFile.Close()
	if err != nil {
		return err
	}

	editor := "vi"
	if os.Getenv("EDITOR") != "" {
		editor = os.Getenv("EDITOR")
	}
	vargs := strings.Split(editor, " ")
	vargs = append(vargs, tmpFile.Name())
	// Command to run the vi editor with the filename as argument
	c := exec.Command(vargs[0], vargs[1:]...)

	// Set the standard input, output, and error to the same as the current process
	c.Stdin = os.Stdin
	c.Stdout = os.Stdout
	c.Stderr = os.Stderr
	// Start the command and wait for it to exit
	_ = c.Run()
	updatedContent, err := os.ReadFile(tmpFile.Name())
	if err != nil {
		return err
	}

	if bytes.Equal(currentContent, updatedContent) {
		fmt.Println("No changes made")
		return nil
	}
	// Edit is a read+modify+write, so it is not considered an external merge source. The
	// configuration is written through the data endpoint; the Unit itself is unchanged.
	editParams, err := unitDataParams(currentUnit.LastChangeDescription, changeSetIDForDataWrite(currentUnit))
	if err != nil {
		return err
	}
	if _, err := putUnitData(spaceID, currentUnit.UnitID, string(updatedContent), editParams); err != nil {
		return err
	}
	unitDetails, err := apiGetUnitInSpace(currentUnit.UnitID.String(), spaceID.String(), "*")
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
