// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"fmt"

	"github.com/confighub/sdk/cubapi"
	goclientnew "github.com/confighub/sdk/openapi/goclient-new"
	"github.com/google/uuid"
	"github.com/spf13/cobra"
)

var unitDeleteCmd = &cobra.Command{
	Use:   "delete [<slug or id>]",
	Short: "Delete a unit or multiple units",
	Long: getCommandHelp(`Delete a unit or multiple units using bulk operations.

Single unit delete:
`+"```"+`
  cub unit delete my-unit
`+"```"+`

Bulk delete with --where:

Delete multiple units at once based on search criteria.

Examples:
`+"```"+`
  # Delete all units with specific label
  cub unit delete --where "Labels.Tier = 'backend'"

  # Delete units across all spaces (requires --space "*")
  cub unit delete --space "*" --where "Labels.cleanup = 'true'"

  # Delete specific units by slug
  cub unit delete --unit my-unit,another-unit
`+"```"+`
`, ""),
	Args:        cobra.MaximumNArgs(1), // Allow 0 or 1 args (0 for bulk mode)
	RunE:        unitDeleteCmdRun,
	Annotations: map[string]string{"OrgLevel": ""},
}

func init() {
	addStandardDeleteFlags(unitDeleteCmd)
	enableWhereFlag(unitDeleteCmd)
	enableFilterFlag(unitDeleteCmd)
	unitDeleteCmd.Flags().StringSliceVar(&unitIdentifiers, "unit", []string{}, "target specific units by slug or UUID for bulk delete (can be repeated or comma-separated)")
	unitCmd.AddCommand(unitDeleteCmd)
}

func checkUnitDeleteConflictingArgs(args []string) bool {
	// Check for bulk delete mode (no positional args)
	isBulkDeleteMode := len(args) == 0

	if isBulkDeleteMode {
		// Check for mutual exclusivity between --unit and --where flags
		if len(unitIdentifiers) > 0 && where != "" {
			failOnError(fmt.Errorf("--unit and --where flags are mutually exclusive"))
		}

	} else {
		// Single delete mode validation
		if len(args) != 1 {
			failOnError(fmt.Errorf("single unit delete requires exactly one argument: <slug or id>"))
		}

		if filter != "" || where != "" || len(unitIdentifiers) > 0 {
			failOnError(fmt.Errorf("--filter, --where, or --unit can only be specified with no positional arguments"))
		}
	}

	if err := validateSpaceFlag(isBulkDeleteMode); err != nil {
		failOnError(err)
	}

	return isBulkDeleteMode
}

func runBulkUnitDelete() error {
	// Parse filter parameter
	filterID, err := parseFilterFlag(filter)
	if err != nil {
		return err
	}

	// Build WHERE clause from unit identifiers or use provided where clause
	var effectiveWhere string
	if len(unitIdentifiers) > 0 {
		whereClause, err := buildWhereClauseFromUnits(unitIdentifiers)
		if err != nil {
			return err
		}
		effectiveWhere = whereClause
	} else {
		effectiveWhere = where
	}

	// Add space constraint to the where clause only if not org level
	effectiveWhere = addSpaceIDToWhereClause(effectiveWhere, selectedSpaceID)

	// Build bulk delete parameters
	include := "UnitEventID,TargetID,UpstreamUnitID,SpaceID"
	params := &goclientnew.BulkDeleteUnitsParams{
		Where:   &effectiveWhere,
		Include: &include,
	}
	if filterID != "" {
		params.Filter = &filterID
	}

	// Call the bulk delete API
	bulkRes, err := cubClientNew.BulkDeleteUnitsWithResponse(ctx, params)
	if cubapi.IsAPIError(err, bulkRes) {
		return cubapi.InterpretErrorGeneric(err, bulkRes)
	}

	// Handle the response
	return handleBulkUnitDeleteResponse(bulkRes.JSON200, bulkRes.JSON207, bulkRes.StatusCode(), "delete", effectiveWhere)
}

func unitDeleteCmdRun(cmd *cobra.Command, args []string) error {
	isBulkDeleteMode := checkUnitDeleteConflictingArgs(args)

	if isBulkDeleteMode {
		return runBulkUnitDelete()
	}

	// Single unit delete logic
	unitDetails, err := apiGetUnitFromSlug(args[0], "*") // get all fields for now
	if err != nil {
		return err
	}
	deleteRes, err := cubClientNew.DeleteUnitWithResponse(ctx, uuid.MustParse(selectedSpaceID), unitDetails.UnitID)
	if cubapi.IsAPIError(err, deleteRes) {
		return cubapi.InterpretErrorGeneric(err, deleteRes)
	}

	displayDeleteResults("unit", args[0], unitDetails.UnitID.String(), deleteRes)
	return nil
}

func handleBulkUnitDeleteResponse(responses200 *[]goclientnew.DeleteResponse, responses207 *[]goclientnew.DeleteResponse, statusCode int, operationName, contextInfo string) error {
	return displayBulkDeleteResults(responses200, responses207, statusCode, "unit", operationName, contextInfo)
}
