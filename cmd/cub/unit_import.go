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
	Use:   "import <slug> [config-file]",
	Short: "Import a unit from Kubernetes cluster or resource file",
	Long: `Import a unit from various sources using unified import filters.

Default mode (with config-file):
  cub unit import myunit resources.json

Unified import filter mode:
  cub unit import myunit --where "namespace IN ('default', 'production') AND resource_type != 'secrets'"
  cub unit import myunit --where "workspace = 'prod' AND IMPORT_OPTIONS(include_system=false)"
  cub unit import myunit --where "
    namespace IN ('default', 'production')
    AND resource_type NOT IN ('secrets', 'configmaps')
    AND labels.env = 'prod'
    AND IMPORT_OPTIONS(
      include_system=false,
      include_custom=true,
      dry_run=true,
      timeout='10m'
    )
  "`,
	Args: cobra.RangeArgs(1, 2),
	RunE: unitImportCmdRun,
}

var unitImportArgs struct {
	targetSlug string
}

func init() {
	addStandardCreateFlags(unitImportCmd)
	enableWaitFlag(unitImportCmd)
	enableWhereFlag(unitImportCmd)
	// enableQuietFlagForOperation(unitImportCmd)
	unitImportCmd.Flags().StringVar(&unitImportArgs.targetSlug, "target", "", "target slug to import into")

	unitCmd.AddCommand(unitImportCmd)
}

func unitImportCmdRun(cmd *cobra.Command, args []string) error {
	configUnit, err := apiGetUnitFromSlug(args[0])
	if err != nil {
		return err
	}

	importRequest := goclientnew.ImportRequest{}

	if len(args) == 2 {
		// Legacy mode: import from resource file
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

		if err := json.Unmarshal(resourceInfoListBytes, &importRequest.ResourceInfoList); err != nil {
			return err
		}
	} else {
		// New unified mode - set the Where field from the --where flag
		if where != "" {
			importRequest.Where = where
		}
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
