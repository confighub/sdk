// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"encoding/json"

	goclientnew "github.com/confighub/sdk/openapi/goclient-new"
	"github.com/google/uuid"
	"github.com/spf13/cobra"
)

var unitImportCmd = &cobra.Command{
	Use:   "import <slug> <config-file>",
	Short: "Import a unit",
	Args:  cobra.ExactArgs(2),
	RunE:  unitImportCmdRun,
}

var unitImportArgs struct {
	targetSlug string
}

func init() {
	// FIXME: These flags are mostly not implemented
	addStandardCreateFlags(unitImportCmd)
	enableWaitFlag(unitImportCmd)
	// enableQuietFlagForOperation(unitImportCmd)
	unitImportCmd.Flags().StringVar(&unitImportArgs.targetSlug, "target", "", "target slug to import into")
	unitCmd.AddCommand(unitImportCmd)
}

func unitImportCmdRun(cmd *cobra.Command, args []string) error {
	configUnit, err := apiGetUnitFromSlug(args[0])
	if err != nil {
		return err
	}

	filename := args[1]
	var resourceInfoListBytes []byte
	if filename == "-" {
		resourceInfoListBytes, err = readStdin()
		if err != nil {
			return err
		}
	} else {
		resourceInfoListBytes = readFile(args[1])
	}

	importRequest := goclientnew.ImportRequest{}
	if err := json.Unmarshal(resourceInfoListBytes, &importRequest.ResourceInfoList); err != nil {
		return err
	}

	importRes, err := cubClientNew.ImportUnitWithResponse(ctx, uuid.MustParse(selectedSpaceID), configUnit.UnitID, importRequest)
	if IsAPIError(err, importRes) {
		return InterpretErrorGeneric(err, importRes)
	}
	if wait {
		return awaitCompletion("import", importRes.JSON200)
	}

	return nil
}
