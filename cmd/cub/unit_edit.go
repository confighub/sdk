// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"os"
	"os/exec"
	"strings"

	goclientnew "github.com/confighub/sdk/openapi/goclient-new"
	"github.com/spf13/cobra"
)

var unitEditCmd = &cobra.Command{
	Use:   "edit <name>",
	Short: "Edit the config data of a unit in your system's editor",
	Long: `This command will pull down the latest revision of this unit and open it the editor specified in the
	        EDITOR environment variable or vi if the variable is not set. When the editor process exits,
					the changes will be saved as a new revision. If the contents were not changed, then no update will be made`,
	Args: cobra.ExactArgs(1),
	RunE: unitEditCmdRun,
}

func init() {
	enableWaitFlag(unitEditCmd)
	unitCmd.AddCommand(unitEditCmd)
}

func unitEditCmdRun(cmd *cobra.Command, args []string) error {
	currentUnit, err := apiGetUnitFromSlug(args[0], "*") // get all fields for RMW
	if err != nil {
		return err
	}

	spaceID := currentUnit.SpaceID
	currentUnit.LastChangeDescription = "CLI edit"

	tmpFile, err := os.CreateTemp("", "*.yaml")
	if err != nil {
		return err
	}
	defer os.Remove(tmpFile.Name())
	currentContent, err := base64.StdEncoding.DecodeString(currentUnit.Data)
	if err != nil {
		return err
	}
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
	updatedData := base64.StdEncoding.EncodeToString(updatedContent)
	currentUnit.Data = updatedData
	unitDetails, err := updateUnit(spaceID, currentUnit, &goclientnew.UpdateUnitParams{})
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
