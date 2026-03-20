// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"errors"

	"github.com/confighub/sdk/core/cubapi"
	goclientnew "github.com/confighub/sdk/core/openapi/goclient-new"
	"github.com/google/uuid"
	"github.com/spf13/cobra"
)

var unitDestroyArgs struct {
	dryRun          bool
	unitIdentifiers []string
}

var unitDestroyCmd = &cobra.Command{
	Use:   "destroy [<unit-slug>]",
	Args:  cobra.MaximumNArgs(1),
	Short: "Destroy configuration units from the target",
	Long: getCommandHelp(`Destroy configuration units from the target.

A target must be attached to the unit (cub unit set-target can be used to add one), and the
worker corresponding to the target must be connected and ready (cub worker get can be used to
get the worker status).

Examples:
`+"```"+`
  # Destroy a single unit by slug
  cub unit destroy my-unit

  # Destroy multiple specific units
  cub unit destroy --space my-space --unit unit1,unit2,unit3
  cub unit destroy --space my-space --unit unit1 --unit unit2 --unit unit3

  # Bulk destroy units using a WHERE clause with labels
  cub unit destroy --space my-space --where "Labels.Tier = 'backend'"

  # Destroy units with multiple label conditions
  cub unit destroy --space my-space --where "Labels.App = 'api' AND Labels.Tier = 'backend'"

  # Dry run to see what would be destroyed
  cub unit destroy --space my-space --unit unit1,unit2 --dry-run

  # Destroy all applied units in a space (use with caution!)
  cub unit destroy --space my-space --where "LiveRevisionNum > 0"

  # Destroy units across all spaces (requires --space "*")
  cub unit destroy --space "*" --where "Space.Labels.Environment = 'test'"
`+"```"+`
`, ""),
	RunE:        unitDestroyCmdRun,
	Annotations: map[string]string{"OrgLevel": ""},
}

func init() {
	enableActionWaitFlag(unitDestroyCmd)
	addStandardDisplayFlags(unitDestroyCmd)
	enableWhereFlag(unitDestroyCmd)
	enableFilterFlag(unitDestroyCmd)
	unitDestroyCmd.Flags().BoolVar(&unitDestroyArgs.dryRun, "dry-run", false, "Perform a dry run without actually destroying")
	unitDestroyCmd.Flags().StringSliceVar(&unitDestroyArgs.unitIdentifiers, "unit", []string{}, "target specific units by slug or UUID (can be repeated or comma-separated)")
	unitCmd.AddCommand(unitDestroyCmd)
}

func unitDestroyCmdRun(_ *cobra.Command, args []string) error {
	if len(args) == 0 {
		return runBulkUnitDestroy()
	}

	return runSingleUnitDestroy(args[0])
}

func runSingleUnitDestroy(unitSlug string) error {
	configUnit, err := apiGetUnitFromSlug(unitSlug, "*")
	if err != nil {
		return err
	}

	params := &goclientnew.DestroyUnitParams{}
	if unitDestroyArgs.dryRun {
		dryRun := unitDestroyArgs.dryRun
		params.DryRun = &dryRun
	}
	destroyRes, err := cubClientNew.DestroyUnitWithResponse(ctx, uuid.MustParse(selectedSpaceID), configUnit.UnitID, params)
	if cubapi.IsAPIError(err, destroyRes) {
		return cubapi.InterpretErrorGeneric(err, destroyRes)
	}

	// Handle wait flag
	if actionWait {
		err = awaitCompletion("destroy", destroyRes.JSON200)
		if err != nil {
			return err
		}
	} else if !quiet && !hasAlternativeOutput() {
		displayStartedOperation(destroyRes.JSON200)
		return nil
	}

	// Output JSON if requested
	if jsonOutput {
		displayJSON(destroyRes.JSON200)
	}
	if jq != "" {
		displayJQ(destroyRes.JSON200)
	}
	if yamlOutput {
		displayYAML(destroyRes.JSON200)
	}
	if yq != "" {
		displayYQ(destroyRes.JSON200)
	}

	return nil
}

func runBulkUnitDestroy() error {
	// Check for mutual exclusivity between --unit and --where flags
	if len(unitDestroyArgs.unitIdentifiers) > 0 && where != "" {
		return errors.New("--unit and --where flags are mutually exclusive")
	}

	// Build WHERE clause from unit identifiers if provided
	var effectiveWhere string
	if len(unitDestroyArgs.unitIdentifiers) > 0 {
		whereClause, err := buildWhereClauseFromUnits(unitDestroyArgs.unitIdentifiers)
		if err != nil {
			return err
		}
		effectiveWhere = whereClause
	} else {
		effectiveWhere = where
	}

	// Add space constraint to the where clause if not org level
	effectiveWhere = addSpaceIDToWhereClause(effectiveWhere, selectedSpaceID)

	// Parse filter parameter
	filterID, err := parseFilterFlag(filter)
	if err != nil {
		return err
	}

	// Build query parameters
	include := "UnitEventID,TargetID,UpstreamUnitID,SpaceID"
	params := &goclientnew.BulkDestroyUnitsParams{
		Where:   effectiveWhere,
		Include: &include,
	}
	if filterID != "" {
		params.Filter = &filterID
	}
	if unitDestroyArgs.dryRun {
		params.DryRun = &unitDestroyArgs.dryRun
	}

	// Call the bulk destroy endpoint
	resp, err := cubClientNew.BulkDestroyUnitsWithResponse(ctx, params)
	if cubapi.IsAPIError(err, resp) {
		return cubapi.InterpretErrorGeneric(err, resp)
	}

	// Handle the response - could be 200 (all success) or 207 (mixed results)
	var responses *[]goclientnew.UnitActionResponse
	if resp.JSON200 != nil {
		responses = resp.JSON200
	} else if resp.JSON207 != nil {
		responses = resp.JSON207
	} else {
		return errors.New("unexpected response from bulk destroy API")
	}

	return handleBulkUnitActionResponse(responses, "destroy", unitDestroyArgs.dryRun)
}
