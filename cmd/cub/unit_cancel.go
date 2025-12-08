// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"errors"
	"fmt"

	goclientnew "github.com/confighub/sdk/openapi/goclient-new"
	"github.com/spf13/cobra"
)

var unitCancelCmd = &cobra.Command{
	Use:   "cancel [<unit-slug>]",
	Short: "Cancel operations for units",
	Long: getCommandHelp(`Cancel operations for units.

Examples:
`+"```"+`
  # Cancel a single unit by slug
  cub unit cancel my-unit

  # Cancel operations for all units with specific label
  cub unit cancel --where "Labels.Tier = 'backend'"

  # Cancel operations for units in a specific space
  cub unit cancel --space my-space --where "Status = 'Progressing'"

  # Cancel operations with multiple label conditions
  cub unit cancel --space my-space --where "Labels.App = 'api' AND Labels.Tier = 'backend'"

  # Cancel operations for specific units by slug
  cub unit cancel --unit my-unit,another-unit

  # Cancel operations across all spaces (requires --space "*")
  cub unit cancel --space "*" --where "Slug = 'backend'"
`+"```"+`
`, ""),
	Args:        cobra.MaximumNArgs(1), // Allow 0 or 1 args (0 for bulk mode)
	RunE:        unitCancelCmdRun,
	Annotations: map[string]string{"OrgLevel": ""},
}

func init() {
	enableWhereFlag(unitCancelCmd)
	enableFilterFlag(unitCancelCmd)
	unitCancelCmd.Flags().StringSliceVar(&unitIdentifiers, "unit", []string{}, "target specific units by slug or UUID for bulk cancel (can be repeated or comma-separated)")
	unitCmd.AddCommand(unitCancelCmd)
}

func checkUnitCancelConflictingArgs(args []string) bool {
	// Check for bulk cancel mode: no positional args
	isBulkCancelMode := len(args) == 0

	if isBulkCancelMode {
		// Check for mutual exclusivity between --unit and --where flags
		if len(unitIdentifiers) > 0 && where != "" {
			failOnError(fmt.Errorf("--unit and --where flags are mutually exclusive"))
		}
	} else {
		// Single cancel mode validation
		if len(args) != 1 {
			failOnError(fmt.Errorf("single unit cancel requires exactly one argument: <slug or id>"))
		}

		if filter != "" || where != "" || len(unitIdentifiers) > 0 {
			failOnError(fmt.Errorf("--filter, --where, or --unit can only be specified with no positional arguments"))
		}
	}

	if err := validateSpaceFlag(isBulkCancelMode); err != nil {
		failOnError(err)
	}

	return isBulkCancelMode
}

func runBulkUnitCancel() error {
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

	// Build bulk cancel parameters
	params := &goclientnew.BulkCancelUnitsParams{
		Where: &effectiveWhere,
	}
	if filterID != "" {
		params.Filter = &filterID
	}

	// Call the bulk cancel API
	resp, err := cubClientNew.BulkCancelUnitsWithResponse(ctx, params)
	if err != nil {
		return err
	}

	// Handle different response codes
	var responses *[]goclientnew.UnitActionResponse
	if resp.JSON200 != nil {
		responses = resp.JSON200
	} else if resp.JSON207 != nil {
		responses = resp.JSON207
	} else {
		return errors.New("unexpected response from bulk cancel API")
	}

	return handleBulkUnitActionResponse(responses, "cancel", false)
}

func unitCancelCmdRun(cmd *cobra.Command, args []string) error {
	isBulkCancelMode := checkUnitCancelConflictingArgs(args)

	if isBulkCancelMode {
		return runBulkUnitCancel()
	}

	// Single unit cancel logic
	// For single unit, we use the bulk API with a specific where clause
	unitSlugOrID := args[0]

	// Build where clause for single unit
	effectiveWhere, err := buildWhereClauseFromUnits([]string{unitSlugOrID})
	if err != nil {
		return err
	}

	// Add space constraint
	effectiveWhere = addSpaceIDToWhereClause(effectiveWhere, selectedSpaceID)

	// Build parameters
	params := &goclientnew.BulkCancelUnitsParams{
		Where: &effectiveWhere,
	}

	// Call API
	resp, err := cubClientNew.BulkCancelUnitsWithResponse(ctx, params)
	if err != nil {
		return err
	}

	// Handle different response codes
	var responses *[]goclientnew.UnitActionResponse
	if resp.JSON200 != nil {
		responses = resp.JSON200
	} else if resp.JSON207 != nil {
		responses = resp.JSON207
	} else {
		return errors.New("unexpected response from bulk cancel API")
	}

	return handleBulkUnitActionResponse(responses, "cancel", false)
}
