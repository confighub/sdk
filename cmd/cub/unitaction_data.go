// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"encoding/base64"
	"fmt"
	"os"
	"strconv"

	goclientnew "github.com/confighub/sdk/core/openapi/goclient-new"
	"github.com/google/uuid"
	"github.com/spf13/cobra"
)

// blobSection identifies which decoded blob field of a UnitAction to emit.
type blobSection string

const (
	blobData        blobSection = "data"
	blobLiveData    blobSection = "livedata"
	blobLiveState   blobSection = "livestate"
	blobBridgeState blobSection = "bridgestate"
)

var (
	unitActionBlobFilename string
	unitActionBlobDecoded  bool
)

func newUnitActionBlobCmd(section blobSection, summary string) *cobra.Command {
	sectionStr := string(section)
	cmd := &cobra.Command{
		Use:   sectionStr + " <unit> <unit-action-id-or-num>",
		Short: summary,
		Long: getCommandHelp(fmt.Sprintf(`Display the %s field of a unit action, decoded from base64.

The unit action is identified by its UUID or its per-unit UnitActionNum.`, sectionStr), ""),
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runUnitActionBlob(section, args[0], args[1])
		},
	}
	cmd.Flags().StringVar(&unitActionBlobFilename, "filename", "", fmt.Sprintf("Write %s to file instead of stdout", sectionStr))
	cmd.Flags().BoolVar(&unitActionBlobDecoded, "decode", true, fmt.Sprintf("Decode base64 %s (default: true)", sectionStr))
	unitActionCmd.AddCommand(cmd)
	return cmd
}

func init() {
	newUnitActionBlobCmd(blobData, "Show the Data of a unit action")
	newUnitActionBlobCmd(blobLiveData, "Show the LiveData of a unit action")
	newUnitActionBlobCmd(blobLiveState, "Show the LiveState of a unit action")
	newUnitActionBlobCmd(blobBridgeState, "Show the BridgeState of a unit action")
}

// resolveUnitAction looks up a UnitAction by its UUID or UnitActionNum.
func resolveUnitAction(spaceID, unitID uuid.UUID, identifier string) (*goclientnew.UnitAction, error) {
	if actionUUID, err := uuid.Parse(identifier); err == nil {
		return apiGetUnitAction(spaceID, unitID, actionUUID)
	}
	num, err := strconv.ParseInt(identifier, 10, 64)
	if err != nil {
		return nil, fmt.Errorf("unit-action identifier must be a UUID or UnitActionNum: %q", identifier)
	}
	return apiGetUnitActionFromNum(spaceID, unitID, num)
}

func runUnitActionBlob(section blobSection, unitSlugOrID, actionIdentifier string) error {
	u, err := apiGetUnitFromSlug(unitSlugOrID, "*")
	if err != nil {
		return err
	}

	spaceUUID := uuid.MustParse(selectedSpaceID)
	action, err := resolveUnitAction(spaceUUID, u.UnitID, actionIdentifier)
	if err != nil {
		return err
	}

	var raw string
	switch section {
	case blobData:
		raw = action.Data
	case blobLiveData:
		raw = action.LiveData
	case blobLiveState:
		raw = action.LiveState
	case blobBridgeState:
		raw = action.BridgeState
	}

	if raw == "" {
		return fmt.Errorf("no %s found for unit action %d on unit %s", section, action.UnitActionNum, u.Slug)
	}

	out := []byte(raw)
	if unitActionBlobDecoded {
		decoded, derr := base64.StdEncoding.DecodeString(raw)
		if derr != nil {
			return fmt.Errorf("failed to decode %s: %w", section, derr)
		}
		out = decoded
	}

	if unitActionBlobFilename != "" {
		if err := os.WriteFile(unitActionBlobFilename, out, 0o644); err != nil {
			return fmt.Errorf("failed to write %s to file: %w", section, err)
		}
		if !quiet {
			tprint("%s written to %s", section, unitActionBlobFilename)
		}
		return nil
	}
	tprintRaw(string(out))
	return nil
}
