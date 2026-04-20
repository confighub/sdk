// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"encoding/json"

	"github.com/confighub/sdk/core/cubapi"
	goclientnew "github.com/confighub/sdk/core/openapi/goclient-new"
	"github.com/google/uuid"
	"github.com/spf13/cobra"
)

var unitImportCmd = &cobra.Command{
	Use:   "import <slug> [config-file]",
	Short: "Import a unit from various sources using unified import filters",
	Long: getCommandHelp(`Import a unit from various sources using unified import filters.

A target must be attached to the unit (cub unit set-target can be used to add one), and the
worker corresponding to the target must be connected and ready (cub worker get can be used to
get the worker status).

	Default mode (with config-file):
`+"```"+`
  cub unit import myunit resources.json
`+"```"+`

Unified import filter mode currently supports Kubernetes resource filtering:

Examples:
`+"```"+`
  # Include custom resources
  cub unit import myunit --where-resource "metadata.namespace = 'import-test-default' AND import.include_custom = true"

  # Combined scenario
  cub unit import myunit --where-resource "metadata.namespace = 'import-test-default' AND import.include_system = true AND import.include_custom = true"

  # Resource type filtering
  cub unit import myunit --where-resource "kind = 'ConfigMap' AND metadata.namespace IN ('import-test-default', 'import-test-production')"

  # Complex path filtering with wildcards
  cub unit import myunit --where-resource "metadata.namespace IN ('import-test-default', 'import-test-production') AND spec.template.spec.containers.*.image = 'nginx:latest'"
`+"```"+`
`, ""),
	Args: cobra.RangeArgs(1, 2),
	RunE: unitImportCmdRun,
}

var unitImportArgs struct {
	targetSlug    string
	dryRun        bool
	whereResource string
}

func init() {
	addStandardDisplayFlags(unitImportCmd)
	enableActionWaitFlag(unitImportCmd)
	// enableQuietFlagForOperation(unitImportCmd)
	unitImportCmd.Flags().StringVar(&unitImportArgs.targetSlug, "target", "", "target slug to import into")
	unitImportCmd.Flags().BoolVar(&unitImportArgs.dryRun, "dry-run", false, "Preview imported results")
	unitImportCmd.Flags().StringVar(&unitImportArgs.whereResource, "where-resource", "", "Resource filter expression using SQL-inspired syntax, similar to the where-filter function. Supports conjunctions with AND. String operators: =, !=, <, >, <=, >=, LIKE, NOT LIKE, ILIKE, ~~, !~~, ~, ~*, !~, !~*. Pattern matching with LIKE/ILIKE uses % and _ wildcards. Regex operators (~, ~*, !~, !~*) support POSIX regular expressions. Kubernetes-specific filters include import.include_system for system namespaces like kube-system, import.include_cluster for cluster-scoped resources like ClusterRole, and import.include_custom for custom resource types.")

	unitCmd.AddCommand(unitImportCmd)
}

func unitImportCmdRun(cmd *cobra.Command, args []string) error {
	configUnit, err := apiGetUnitFromSlug(args[0], "*")
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
		if unitImportArgs.whereResource != "" {
			importRequest.Where = unitImportArgs.whereResource
		}
	}

	params := &goclientnew.ImportUnitParams{}
	if unitImportArgs.dryRun {
		dryRun := unitImportArgs.dryRun
		params.DryRun = &dryRun
	}
	importRes, err := cubClientNew.ImportUnitWithResponse(ctx, uuid.MustParse(selectedSpaceID), configUnit.UnitID, params, importRequest)
	if cubapi.IsAPIError(err, importRes) {
		return cubapi.InterpretErrorGeneric(err, importRes)
	}
	if actionWait {
		// awaitCompletion will print a unit-centric message !quiet && !isAlternativeOutput()
		err = awaitCompletion("import", importRes.JSON200)
		if err != nil {
			return err
		}
	} else if !quiet && !isAlternativeOutput() {
		displayStartedOperation(importRes.JSON200)
		return nil
	}

	renderPayload(importRes.JSON200)

	return nil
}
