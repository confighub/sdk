// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"bytes"
	"encoding/json"
	"fmt"

	"github.com/confighub/sdk/core/cubapi"
	goclientnew "github.com/confighub/sdk/core/openapi/goclient-new"
	"github.com/google/uuid"
	"github.com/spf13/cobra"
)

var unitSetTargetCmd = &cobra.Command{
	Use:   "set-target <unit-slug> <target-slug> | set-target <target-slug>",
	Short: "Set target for unit(s)",
	Long: getCommandHelp(`Set target for unit(s). 

Supports two modes:

Single unit mode:
`+"```"+`
  cub unit set-target <unit-slug> <target-slug>
`+"```"+`

Bulk mode:
`+"```"+`
  cub unit set-target <target-slug> --where "Slug LIKE 'app-%'"
  cub unit set-target <target-slug> --unit unit1,unit2,unit3
`+"```"+`

Use "-" as target-slug to unset/clear the target.

Targets typically are created by workers, but may also be created using cub target create.`, ""),
	Args:        cobra.RangeArgs(1, 2),
	Annotations: map[string]string{"OrgLevel": ""},
	RunE:        unitSetTargetCmdRun,
}

func init() {
	addStandardDisplayFlags(unitSetTargetCmd)
	enableWhereFlag(unitSetTargetCmd)
	enableFilterFlag(unitSetTargetCmd)
	enableWaitFlag(unitSetTargetCmd)
	unitSetTargetCmd.Flags().StringSliceVar(&unitIdentifiers, "unit", []string{}, "target specific units by slug or UUID (can be repeated or comma-separated)")
	unitCmd.AddCommand(unitSetTargetCmd)
}

// TODO: Check arguments

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
		// Use parseEntityIdentifierSingle to support cross-space target lookup
		id, err := parseEntityIdentifierSingle[goclientnew.Target](
			targetSlug,
			EntityTypeTarget,
			apiGetTargetFromSlugInSpaceCore,
			func(t *goclientnew.Target) string { return t.TargetID.String() },
		)
		if err != nil {
			return nil, err
		}
		targetID = id
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
	configUnit, err := apiGetUnitFromSlug(unitSlug, "*") // get all fields for RMW
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
	if cubapi.IsAPIError(err, unitRes) {
		return cubapi.InterpretErrorGeneric(err, unitRes)
	}

	unitDetails, err := unitFromWrite(unitRes.JSON200)
	if err != nil {
		return err
	}
	displayUpdateResults(unitDetails, EntityTypeUnit, unitSlug, unitDetails.UnitID.String(), displayUnitDetails)
	if wait {
		return awaitTriggersRemoval(unitDetails)
	}
	return nil
}

func runBulkUnitSetTarget(targetSlug string) error {
	// Parse filter parameter
	filterID, err := parseFilterFlag(filter)
	if err != nil {
		return err
	}

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
	if filterID != "" {
		params.Filter = &filterID
	}

	// Set include parameter to expand UpstreamUnitID
	include := "UnitEventID,TargetID,UpstreamUnitID,SpaceID"
	params.Include = &include

	// Call the bulk patch API
	bulkRes, err := cubClientNew.BulkPatchUnitsWithBodyWithResponse(
		ctx,
		params,
		"application/merge-patch+json",
		bytes.NewReader(patchJSON),
	)
	if cubapi.IsAPIError(err, bulkRes) {
		return cubapi.InterpretErrorGeneric(err, bulkRes)
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

	err = handleBulkSetTargetResponse(responses, statusCode, targetSlug)
	if err != nil {
		return err
	}
	if wait && responses != nil {
		for _, resp := range *responses {
			if resp.Unit != nil {
				if awaitErr := awaitTriggersRemoval(resp.Unit); awaitErr != nil {
					return awaitErr
				}
			}
		}
	}
	return nil
}

func handleBulkSetTargetResponse(responses *[]goclientnew.UnitCreateOrUpdateResponse, statusCode int, targetSlug string) error {
	// Convert responses to the format expected by the generic function
	var responses200 *[]goclientnew.UnitCreateOrUpdateResponse
	var responses207 *[]goclientnew.UnitCreateOrUpdateResponse
	if statusCode == 200 {
		responses200 = responses
	} else if statusCode == 207 {
		responses207 = responses
	}

	return displayBulkGenericCreateOrUpdateResults(
		responses200, responses207, statusCode, "unit", "set-target", fmt.Sprintf("target %s", targetSlug),
		func(r *goclientnew.UnitCreateOrUpdateResponse) *goclientnew.ResponseError { return r.Error },
		func(r *goclientnew.UnitCreateOrUpdateResponse) string {
			if r.Unit != nil {
				return fmt.Sprintf("%s (%s)", r.Unit.Slug, r.Unit.UnitID)
			}
			return ""
		},
	)
}
