// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"bytes"
	"fmt"

	"github.com/cockroachdb/errors"
	"github.com/confighub/sdk/cubapi"
	goclientnew "github.com/confighub/sdk/openapi/goclient-new"
	"github.com/google/uuid"
	"github.com/spf13/cobra"
)

var filterUpdateCmd = &cobra.Command{
	Use:   "update [<slug or id>] [<from>] [options...]",
	Short: "Update a filter or multiple filters",
	Long: `Update a filter or multiple filters using bulk operations.

Single filter update:
  cub filter update my-filter Unit --where-field "Labels.Environment = 'staging'"

Bulk update with --patch:
Update multiple filters at once based on search criteria. Requires --patch flag with no positional arguments.

Examples:
  # Update From type for all filters
  cub filter update --patch --where "From = 'Space'" --from-stdin < patch.json

  # Update where clause for specific filters
  echo '{"Where": "Labels.Environment = 'production'"}' | cub filter update --patch --filter-entity my-filter,another-filter --from-stdin

  # Update resource type for Unit filters
  echo '{"ResourceType": "apps/v1/StatefulSet"}' | cub filter update --patch --where "From = 'Unit'" --from-stdin`,
	Args:        cobra.MinimumNArgs(0), // Allow 0 args for bulk mode
	RunE:        filterUpdateCmdRun,
	Annotations: map[string]string{"OrgLevel": ""},
}

var (
	filterPatch       bool
	filterIdentifiers []string
	filterUpdateArgs  struct {
		whereField   string
		whereData    string
		resourceType string
		fromSpace    string
	}
)

func init() {
	addStandardUpdateFlags(filterUpdateCmd)
	filterUpdateCmd.Flags().BoolVar(&filterPatch, "patch", false, "use patch API for individual or bulk operations")
	enableWhereFlag(filterUpdateCmd)
	enableFilterFlag(filterUpdateCmd)
	filterUpdateCmd.Flags().StringSliceVar(&filterIdentifiers, "filter-entity", []string{}, "target specific filters by slug or UUID for bulk patch (can be repeated or comma-separated)")

	// Single update specific flags
	filterUpdateCmd.Flags().StringVar(&filterUpdateArgs.whereField, "where-field", "", "where expression for filter entity")
	filterUpdateCmd.Flags().StringVar(&filterUpdateArgs.whereData, "where-data", "", "where filter expression for configuration data (valid only for Units)")
	filterUpdateCmd.Flags().StringVar(&filterUpdateArgs.resourceType, "resource-type", "", "resource type to match (e.g., apps/v1/Deployment, valid only for Units)")
	filterUpdateCmd.Flags().StringVar(&filterUpdateArgs.fromSpace, "from-space", "", "space to filter within (slug or UUID, only relevant for spaced entity types)")

	filterCmd.AddCommand(filterUpdateCmd)
}

func checkFilterUpdateConflictingArgs(args []string) bool {
	// Check for bulk patch mode: no positional args
	isBulkPatchMode := len(args) == 0

	if isBulkPatchMode {
		if !filterPatch {
			failOnError(errors.New("--patch is required in bulk mode"))
		}

		// Check for mutual exclusivity between --filter-entity and --where flags
		if len(filterIdentifiers) > 0 && where != "" {
			failOnError(fmt.Errorf("--filter-entity and --where flags are mutually exclusive"))
		}

	} else {
		// Single create mode validation
		if len(args) != 2 {
			failOnError(errors.New("single filter update requires: <slug> <from> [options...]"))
		}

		if filter != "" || where != "" || len(filterIdentifiers) > 0 {
			failOnError(fmt.Errorf("--where or --filter-entity can only be specified with --patch and no positional arguments"))
		}
	}

	if filterPatch && flagReplace {
		failOnError(fmt.Errorf("only one of --patch and --replace should be specified"))
	}

	if err := validateSpaceFlag(isBulkPatchMode); err != nil {
		failOnError(err)
	}

	if err := validateStdinFlags(); err != nil {
		failOnError(err)
	}

	// Validate label removal only works with patch
	if err := ValidateLabelRemoval(label, filterPatch); err != nil {
		failOnError(err)
	}
	// Validate delete gate removal only works with patch
	if err := ValidateDeleteGateRemoval(deleteGate, filterPatch); err != nil {
		failOnError(err)
	}

	return isBulkPatchMode
}

func runBulkFilterUpdate() error {
	// Parse filter parameter
	filterID, err := parseFilterFlag(filter)
	if err != nil {
		return err
	}

	// Build WHERE clause from filter identifiers or use provided where clause
	var effectiveWhere string
	if len(filterIdentifiers) > 0 {
		whereClause, err := buildWhereClauseFromFilters(filterIdentifiers)
		if err != nil {
			return err
		}
		effectiveWhere = whereClause
	} else {
		effectiveWhere = where
	}

	// Add space constraint to the where clause only if not org level
	effectiveWhere = addSpaceIDToWhereClause(effectiveWhere, selectedSpaceID)

	// Validate and resolve fromSpace early if needed
	var fromSpaceID uuid.UUID
	if filterUpdateArgs.fromSpace != "" {
		fromSpace, err := apiGetSpaceFromSlug(filterUpdateArgs.fromSpace, "SpaceID")
		if err != nil {
			return err
		}
		fromSpaceID = fromSpace.SpaceID
	}

	// Create enhancer function for filter-specific fields
	enhancer := func(patchMap map[string]interface{}) {
		// Add filter-specific fields
		if filterUpdateArgs.whereField != "" {
			patchMap["Where"] = filterUpdateArgs.whereField
		}
		if filterUpdateArgs.whereData != "" {
			patchMap["WhereData"] = filterUpdateArgs.whereData
		}
		if filterUpdateArgs.resourceType != "" {
			patchMap["ResourceType"] = filterUpdateArgs.resourceType
		}
		if filterUpdateArgs.fromSpace != "" {
			patchMap["FromSpaceID"] = fromSpaceID
		}
	}

	// Build patch data using consolidated function
	patchJSON, err := BuildPatchData(enhancer)
	if err != nil {
		return err
	}

	// Build bulk patch parameters
	include := "SpaceID"
	params := &goclientnew.BulkPatchFiltersParams{
		Where:   &effectiveWhere,
		Include: &include,
	}
	if filterID != "" {
		params.Filter = &filterID
	}

	// Call the bulk patch API
	bulkRes, err := cubClientNew.BulkPatchFiltersWithBodyWithResponse(
		ctx,
		params,
		"application/merge-patch+json",
		bytes.NewReader(patchJSON),
	)
	if err != nil {
		return err
	}

	// Handle the response
	return handleBulkFilterCreateOrUpdateResponse(bulkRes.JSON200, bulkRes.JSON207, bulkRes.StatusCode(), "update", effectiveWhere)
}

func filterUpdateCmdRun(cmd *cobra.Command, args []string) error {
	isBulkPatchMode := checkFilterUpdateConflictingArgs(args)

	if isBulkPatchMode {
		return runBulkFilterUpdate()
	}

	// Single filter update logic
	if len(args) < 2 {
		return errors.New("single filter update requires: <slug or id> <from> [options...]")
	}

	currentFilter, err := apiGetFilterFromSlug(args[0], "*", selectedSpaceID) // get all fields for RMW
	if err != nil {
		return err
	}
	spaceID := uuid.MustParse(selectedSpaceID)

	// Validate and resolve fromSpace early if needed
	var fromSpaceID uuid.UUID
	if filterUpdateArgs.fromSpace != "" {
		fromSpace, err := apiGetSpaceFromSlug(filterUpdateArgs.fromSpace, "SpaceID")
		if err != nil {
			return err
		}
		fromSpaceID = fromSpace.SpaceID
	}

	if filterPatch {

		// Create enhancer function for filter-specific fields
		enhancer := func(patchMap map[string]interface{}) {
			// Add filter-specific fields
			patchMap["From"] = args[1]
			if filterUpdateArgs.whereField != "" {
				patchMap["Where"] = filterUpdateArgs.whereField
			}
			if filterUpdateArgs.whereData != "" {
				patchMap["WhereData"] = filterUpdateArgs.whereData
			}
			if filterUpdateArgs.resourceType != "" {
				patchMap["ResourceType"] = filterUpdateArgs.resourceType
			}
			if filterUpdateArgs.fromSpace != "" {
				patchMap["FromSpaceID"] = fromSpaceID
			}
		}

		// Build patch data using consolidated function
		patchJSON, err := BuildPatchData(enhancer)
		if err != nil {
			return err
		}

		filterDetails, err := patchFilter(spaceID, currentFilter.FilterID, patchJSON)
		if err != nil {
			return err
		}

		displayUpdateResults(filterDetails, "filter", args[0], filterDetails.FilterID.String(), displayFilterDetails)
		return nil
	}

	// Traditional update mode
	// Handle --from-stdin or --filename with optional --replace
	if flagPopulateModelFromStdin || flagFilename != "" {
		existingFilter := currentFilter
		if flagReplace {
			// Replace mode - create new entity, allow Version to be overwritten
			currentFilter = new(goclientnew.Filter)
			currentFilter.Version = existingFilter.Version
		}

		if err := populateModelFromFlags(currentFilter); err != nil {
			return err
		}

		// Ensure essential fields can't be clobbered
		currentFilter.OrganizationID = existingFilter.OrganizationID
		currentFilter.SpaceID = existingFilter.SpaceID
		currentFilter.FilterID = existingFilter.FilterID
	}
	err = setLabels(&currentFilter.Labels)
	if err != nil {
		return err
	}
	err = setDeleteGates(&currentFilter.DeleteGates)
	if err != nil {
		return err
	}

	// If this was set from stdin, it will be overridden
	currentFilter.SpaceID = spaceID
	currentFilter.From = args[1]

	// Set optional fields from flags
	if filterUpdateArgs.whereField != "" {
		currentFilter.Where = filterUpdateArgs.whereField
	}
	if filterUpdateArgs.whereData != "" {
		currentFilter.WhereData = filterUpdateArgs.whereData
	}
	if filterUpdateArgs.resourceType != "" {
		currentFilter.ResourceType = filterUpdateArgs.resourceType
	}
	if filterUpdateArgs.fromSpace != "" {
		currentFilter.FromSpaceID = &fromSpaceID
	}

	filterRes, err := cubClientNew.UpdateFilterWithResponse(ctx, spaceID, currentFilter.FilterID, *currentFilter)
	if cubapi.IsAPIError(err, filterRes) {
		return cubapi.InterpretErrorGeneric(err, filterRes)
	}

	filterDetails := filterRes.JSON200
	displayUpdateResults(filterDetails, "filter", args[0], filterDetails.FilterID.String(), displayFilterDetails)
	return nil
}

func handleBulkFilterCreateOrUpdateResponse(responses200 *[]goclientnew.FilterCreateOrUpdateResponse, responses207 *[]goclientnew.FilterCreateOrUpdateResponse, statusCode int, operationName, contextInfo string) error {
	return displayBulkGenericCreateOrUpdateResults(
		responses200, responses207, statusCode, "filter", operationName, contextInfo,
		func(r *goclientnew.FilterCreateOrUpdateResponse) *goclientnew.ResponseError { return r.Error },
		func(r *goclientnew.FilterCreateOrUpdateResponse) string {
			if r.Filter != nil {
				return fmt.Sprintf("%s (ID: %s)", r.Filter.Slug, r.Filter.FilterID)
			}
			return ""
		},
	)
}

func patchFilter(spaceID uuid.UUID, filterID uuid.UUID, patchData []byte) (*goclientnew.Filter, error) {
	filterRes, err := cubClientNew.PatchFilterWithBodyWithResponse(
		ctx,
		spaceID,
		filterID,
		"application/merge-patch+json",
		bytes.NewReader(patchData),
	)
	if cubapi.IsAPIError(err, filterRes) {
		return nil, cubapi.InterpretErrorGeneric(err, filterRes)
	}

	return filterRes.JSON200, nil
}
