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
	Short: "Import a unit from various sources using unified import filters",
	Long: `Import a unit from various sources using unified import filters.

Default mode (with config-file):
  cub unit import myunit resources.json

Unified import filter mode currently supports Kubernetes resource filtering:

Include custom resources:
  cub unit import myunit --where "metadata.namespace = 'import-test-default' AND import.include_custom = true"

Combined scenario:
  cub unit import myunit --where "metadata.namespace = 'import-test-default' AND import.include_system = true AND import.include_custom = true"

Resource type filtering:
  cub unit import myunit --where "kind = 'ConfigMap' AND metadata.namespace IN ('import-test-default', 'import-test-production')"

Complex path filtering with wildcards:
  cub unit import myunit --where "metadata.namespace IN ('import-test-default', 'import-test-production') AND spec.template.spec.containers.*.image = 'nginx:latest'"`,
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
