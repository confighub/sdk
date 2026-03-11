// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"errors"
	"fmt"

	"github.com/confighub/sdk/cubapi"
	goclientnew "github.com/confighub/sdk/openapi/goclient-new"
	"github.com/google/uuid"
	"github.com/spf13/cobra"
)

var unitRefreshArgs struct {
	dryRun    bool
	driftMode string
}

var unitRefreshCmd = &cobra.Command{
	Use:   "refresh [unit-slug]",
	Args:  cobra.MaximumNArgs(1),
	Short: "Refresh a configuration unit from the target",
	Long: getCommandHelp(`Refresh a configuration unit from the target. If no unit is specified, performs bulk refresh based on filter criteria.

A target must be attached to the unit (cub unit set-target can be used to add one), and the
worker corresponding to the target must be connected and ready (cub worker get can be used to
get the worker status).

Single unit refresh:
`+"```"+`
  cub unit refresh my-unit
`+"```"+`

Bulk refresh with --where:

Refresh multiple units at once based on search criteria.

Examples:
`+"```"+`
  # Refresh all units with specific label
  cub unit refresh --where "Labels.Tier = 'backend'"

  # Refresh units across all spaces (requires --space "*")
  cub unit refresh --space "*" --where "Slug = 'backend'"

  # Refresh specific units by slug
  cub unit refresh --unit my-unit,another-unit

  # Dry run to preview which units would be refreshed
  cub unit refresh --where "Labels.Environment = 'test'" --dry-run
`+"```"+`
`, ""),
	RunE:        unitRefreshCmdRun,
	Annotations: map[string]string{"OrgLevel": ""},
}

func init() {
	enableActionWaitFlag(unitRefreshCmd)
	addStandardDisplayFlags(unitRefreshCmd)
	enableWhereFlag(unitRefreshCmd)
	enableFilterFlag(unitRefreshCmd)
	enableDisplayMutationsFlag(unitRefreshCmd)

	// Bulk operation flags
	unitRefreshCmd.Flags().StringSliceVar(&unitIdentifiers, "unit", []string{}, "target specific units by slug or UUID for bulk refresh (can be repeated or comma-separated)")
	unitRefreshCmd.Flags().BoolVar(&unitRefreshArgs.dryRun, "dry-run", false, "Preview refresh results")
	unitRefreshCmd.Flags().StringVar(&unitRefreshArgs.driftMode, "drift-mode", "", "Drift reconciliation mode (OnDemand, ContinuousApply, ContinuousRefresh)")

	unitCmd.AddCommand(unitRefreshCmd)
}

func checkUnitRefreshConflictingArgs(args []string) bool {
	// Check for bulk refresh mode: no positional args
	isBulkRefreshMode := len(args) == 0

	if isBulkRefreshMode {
		// Check for mutual exclusivity between --unit and --where flags
		if len(unitIdentifiers) > 0 && where != "" {
			failOnError(fmt.Errorf("--unit and --where flags are mutually exclusive"))
		}

	} else {
		// Single refresh mode validation
		if len(args) != 1 {
			failOnError(fmt.Errorf("single unit refresh requires exactly one argument: <slug or id>"))
		}

		if filter != "" || where != "" || len(unitIdentifiers) > 0 {
			failOnError(fmt.Errorf("--filter, --where, or --unit can only be specified with no positional arguments"))
		}
	}

	if err := validateSpaceFlag(isBulkRefreshMode); err != nil {
		failOnError(err)
	}

	return isBulkRefreshMode
}

func unitRefreshCmdRun(_ *cobra.Command, args []string) error {
	isBulkRefreshMode := checkUnitRefreshConflictingArgs(args)

	if isBulkRefreshMode {
		return runBulkUnitRefresh()
	}

	return runSingleUnitRefresh(args[0])
}

func runSingleUnitRefresh(unitSlug string) error {
	configUnit, err := apiGetUnitFromSlug(unitSlug, "*") // get all fields for now
	if err != nil {
		return err
	}

	// Save prior HeadMutationNum for mutation display
	priorHeadMutationNum := configUnit.HeadMutationNum
	priorRevisionNum := configUnit.HeadRevisionNum
	refreshUnitSlug := configUnit.Slug
	priorData := configUnit.Data

	params := &goclientnew.RefreshUnitParams{}
	if unitRefreshArgs.dryRun {
		dryRun := unitRefreshArgs.dryRun
		params.DryRun = &dryRun
	}
	if unitRefreshArgs.driftMode != "" {
		params.DriftMode = &unitRefreshArgs.driftMode
	}
	refreshRes, err := cubClientNew.RefreshUnitWithResponse(ctx, uuid.MustParse(selectedSpaceID), configUnit.UnitID, params)
	if cubapi.IsAPIError(err, refreshRes) {
		return cubapi.InterpretErrorGeneric(err, refreshRes)
	}

	// Handle wait flag
	if actionWait || displayMutations {
		// awaitCompletion will print a unit-centric message !quiet && !hasAlternativeOutput()
		err = awaitCompletion("refresh", refreshRes.JSON200)
		if err != nil {
			return err
		}
	} else if !quiet && !hasAlternativeOutput() {
		displayStartedOperation(refreshRes.JSON200)
		return nil
	}

	if jsonOutput {
		displayJSON(refreshRes.JSON200)
	}
	if jq != "" {
		displayJQ(refreshRes.JSON200)
	}
	if yamlOutput {
		displayYAML(refreshRes.JSON200)
	}
	if yq != "" {
		displayYQ(refreshRes.JSON200)
	}

	// Display mutations if requested
	if displayMutations {
		tprintRaw("")
		if unitRefreshArgs.dryRun {
			// For dry-run, get the refreshed config data and compute mutations
			refreshedUnit, err := apiGetUnitInSpace(configUnit.UnitID.String(), configUnit.SpaceID.String(), "*")
			if err != nil {
				return err
			}
			lookupMutationsUnitID = configUnit.UnitID.String()
			displayMutationsFromDryRun(priorData, refreshedUnit.Data, configUnit.SpaceID.String(), "refresh")
		} else {
			// For non-dry-run, get the updated unit and display mutations
			updatedUnit, err := apiGetUnitInSpace(configUnit.UnitID.String(), configUnit.SpaceID.String(), "*")
			if err != nil {
				return err
			}
			priorRevision := fmt.Sprintf("%s/%d", refreshUnitSlug, priorRevisionNum)
			displayMutationsForUnit(updatedUnit, priorHeadMutationNum, "refresh", priorRevision)
		}
	}

	return nil
}

func runBulkUnitRefresh() error {
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

	// Build bulk refresh parameters
	params := &goclientnew.BulkRefreshUnitsParams{
		Where: effectiveWhere,
	}
	if filterID != "" {
		params.Filter = &filterID
	}
	if unitRefreshArgs.dryRun {
		dryRun := unitRefreshArgs.dryRun
		params.DryRun = &dryRun
	}
	if unitRefreshArgs.driftMode != "" {
		params.DriftMode = &unitRefreshArgs.driftMode
	}

	// Execute bulk refresh
	resp, err := cubClientNew.BulkRefreshUnitsWithResponse(ctx, params)
	if err != nil {
		return fmt.Errorf("failed to execute bulk refresh: %w", err)
	}

	// Handle different response codes
	var responses *[]goclientnew.UnitActionResponse
	if resp.JSON200 != nil {
		responses = resp.JSON200
	} else if resp.JSON207 != nil {
		responses = resp.JSON207
	} else {
		return errors.New("unexpected response from bulk refresh API")
	}

	return handleBulkUnitActionResponse(responses, "refresh", unitRefreshArgs.dryRun)
}
