// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"bytes"
	"encoding/json"
	"fmt"

	goclientnew "github.com/confighub/sdk/openapi/goclient-new"
	"github.com/google/uuid"
	"github.com/spf13/cobra"
)

var unitSetTargetCmd = &cobra.Command{
	Use:   "set-target <unit-slug> <target-slug> | set-target <target-slug>",
	Short: "Set target for unit(s)",
	Long: `Set target for unit(s). Supports two modes:

Single unit mode:
  cub unit set-target <unit-slug> <target-slug>

Bulk mode:
  cub unit set-target <target-slug> --where "Slug LIKE 'app-%'"
  cub unit set-target <target-slug> --unit unit1,unit2,unit3
  
Use "-" as target-slug to unset/clear the target.`,
	Args: cobra.RangeArgs(1, 2),
	RunE: unitSetTargetCmdRun,
}

func init() {
	enableVerboseFlag(unitSetTargetCmd)
	enableJsonFlag(unitSetTargetCmd)
	enableJqFlag(unitSetTargetCmd)
	enableWhereFlag(unitSetTargetCmd)
	unitSetTargetCmd.Flags().StringSliceVar(&unitIdentifiers, "unit", []string{}, "target specific units by slug or UUID (can be repeated or comma-separated)")
	unitCmd.AddCommand(unitSetTargetCmd)
}

func unitSetTargetCmdRun(cmd *cobra.Command, args []string) error {
	// Determine operation mode based on number of arguments
	if len(args) == 2 {
		// Single unit mode (backward compatibility)
		return runSingleUnitSetTarget(args[0], args[1])
	} else {
		// Bulk mode
		return runBulkUnitSetTarget(args[0])
	}
}

// createTargetPatch creates a JSON patch for setting a target on a unit
func createTargetPatch(targetSlug string) ([]byte, error) {
	var targetID uuid.UUID
	if targetSlug == "-" {
		targetID = uuid.Nil
	} else {
		exTarget, err := apiGetTargetFromSlug(targetSlug, selectedSpaceID)
		if err != nil {
			return nil, err
		}
		targetID = exTarget.Target.TargetID
	}

	// Create JSON patch with only the TargetID field
	patchData := map[string]interface{}{
		"TargetID": targetID,
	}
	patchJSON, err := json.Marshal(patchData)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal patch data: %w", err)
	}
	return patchJSON, nil
}

func runSingleUnitSetTarget(unitSlug, targetSlug string) error {
	newParams := &goclientnew.PatchUnitParams{}
	configUnit, err := apiGetUnitFromSlug(unitSlug)
	if err != nil {
		return err
	}

	patchJSON, err := createTargetPatch(targetSlug)
	if err != nil {
		return err
	}

	unitRes, err := cubClientNew.PatchUnitWithBodyWithResponse(
		ctx,
		uuid.MustParse(selectedSpaceID),
		configUnit.UnitID,
		newParams,
		"application/merge-patch+json",
		bytes.NewReader(patchJSON),
	)
	if IsAPIError(err, unitRes) {
		return InterpretErrorGeneric(err, unitRes)
	}

	unitDetails := unitRes.JSON200
	tprint("Successfully set target of unit %s (%s)", unitSlug, unitDetails.UnitID)
	if verbose {
		displayUnitDetails(unitDetails)
	}
	if jsonOutput {
		displayJSON(unitDetails)
	}
	if jq != "" {
		displayJQ(unitDetails)
	}
	return nil
}

func runBulkUnitSetTarget(targetSlug string) error {
	// Check for mutual exclusivity between --unit and --where flags
	if len(unitIdentifiers) > 0 && where != "" {
		return fmt.Errorf("--unit and --where flags are mutually exclusive")
	}

	// Build WHERE clause from unit identifiers if provided
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

	// Append space constraint to the where clause
	effectiveWhere = addSpaceIDToWhereClause(effectiveWhere, selectedSpaceID)

	patchJSON, err := createTargetPatch(targetSlug)
	if err != nil {
		return err
	}

	// Build bulk patch parameters
	params := &goclientnew.BulkPatchUnitsParams{
		Where: &effectiveWhere,
	}

	// Set include parameter to expand UpstreamUnitID
	include := "UpstreamUnitID"
	params.Include = &include

	// Call the bulk patch API
	bulkRes, err := cubClientNew.BulkPatchUnitsWithBodyWithResponse(
		ctx,
		params,
		"application/merge-patch+json",
		bytes.NewReader(patchJSON),
	)
	if IsAPIError(err, bulkRes) {
		return InterpretErrorGeneric(err, bulkRes)
	}

	// Handle response based on status code
	var responses *[]goclientnew.UnitCreateOrUpdateResponse
	var statusCode int

	if bulkRes.JSON200 != nil {
		responses = bulkRes.JSON200
		statusCode = 200
	} else if bulkRes.JSON207 != nil {
		responses = bulkRes.JSON207
		statusCode = 207
	} else {
		return fmt.Errorf("unexpected response from bulk patch API")
	}

	return handleBulkSetTargetResponse(responses, statusCode, targetSlug)
}

func handleBulkSetTargetResponse(responses *[]goclientnew.UnitCreateOrUpdateResponse, statusCode int, targetSlug string) error {
	if responses == nil {
		return fmt.Errorf("no response data received")
	}

	successCount := 0
	errorCount := 0

	for _, resp := range *responses {
		if resp.Error != nil {
			errorCount++
			if !quiet {
				if resp.Unit != nil {
					tprint("ERROR: Failed to set target for unit %s (%s): %s", resp.Unit.Slug, resp.Unit.UnitID, resp.Error.Message)
				} else {
					tprint("ERROR: %s", resp.Error.Message)
				}
			}
		} else {
			successCount++
			if resp.Unit != nil && !quiet {
				tprint("Successfully set target of unit %s (%s)", resp.Unit.Slug, resp.Unit.UnitID)
				if verbose {
					displayUnitDetails(resp.Unit)
				}
			}
		}
	}

	// Summary message
	if !quiet {
		totalCount := len(*responses)
		if statusCode == 207 {
			tprint("Bulk operation completed with mixed results: %d succeeded, %d failed out of %d total",
				successCount, errorCount, totalCount)
		} else if statusCode == 200 {
			tprint("Bulk operation completed successfully: %d units updated", successCount)
		}
	}

	// Output JSON if requested
	if jsonOutput {
		displayJSON(responses)
	}
	if jq != "" {
		displayJQ(responses)
	}

	// Return error if any operations failed and it was a complete failure
	if errorCount > 0 && successCount == 0 {
		return fmt.Errorf("all bulk operations failed")
	}

	return nil
}
