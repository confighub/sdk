// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"bytes"
	"encoding/json"
	"fmt"

	"github.com/cockroachdb/errors"
	goclientnew "github.com/confighub/sdk/openapi/goclient-new"
	"github.com/google/uuid"
	"github.com/spf13/cobra"
)

var filterUpdateCmd = &cobra.Command{
	Use:   "update [<slug or id>] [<from>] [options...]",
	Short: "Update a filter or multiple filters",
	Long: `Update a filter or multiple filters using bulk operations.

Single filter update:
  cub filter update my-filter Unit --where "Labels.Environment = 'staging'"

Bulk update with --patch:
Update multiple filters at once based on search criteria. Requires --patch flag with no positional arguments.

Examples:
  # Update From type for all filters
  cub filter update --patch --where "From = 'Space'" --from-stdin < patch.json

  # Update where clause for specific filters
  echo '{"Where": "Labels.Environment = 'production'"}' | cub filter update --patch --filter my-filter,another-filter --from-stdin

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
		whereData    string
		resourceType string
		fromSpace    string
	}
)

func init() {
	addStandardUpdateFlags(filterUpdateCmd)
	filterUpdateCmd.Flags().BoolVar(&filterPatch, "patch", false, "use patch API for individual or bulk operations")
	enableWhereFlag(filterUpdateCmd)
	filterUpdateCmd.Flags().StringSliceVar(&filterIdentifiers, "filter", []string{}, "target specific filters by slug or UUID for bulk patch (can be repeated or comma-separated)")

	// Single update specific flags
	filterUpdateCmd.Flags().StringVar(&filterUpdateArgs.whereData, "where-data", "", "where filter expression for configuration data (valid only for Units)")
	filterUpdateCmd.Flags().StringVar(&filterUpdateArgs.resourceType, "resource-type", "", "resource type to match (e.g., apps/v1/Deployment, valid only for Units)")
	filterUpdateCmd.Flags().StringVar(&filterUpdateArgs.fromSpace, "from-space", "", "space to filter within (slug or UUID, only relevant for spaced entity types)")

	filterCmd.AddCommand(filterUpdateCmd)
}

func checkFilterConflictingArgs(args []string) bool {
	// Check for bulk patch mode (no positional args with --patch)
	isBulkPatchMode := filterPatch && len(args) == 0

	if !isBulkPatchMode && (where != "" || len(filterIdentifiers) > 0) {
		failOnError(fmt.Errorf("--where or --filter can only be specified with --patch and no positional arguments"))
	}

	// Single create mode validation
	if !isBulkPatchMode && len(args) < 2 {
		failOnError(errors.New("single filter update requires: <slug> <from> [options...]"))
	}

	// Check for mutual exclusivity between --filter and --where flags
	if len(filterIdentifiers) > 0 && where != "" {
		failOnError(fmt.Errorf("--filter and --where flags are mutually exclusive"))
	}

	if filterPatch && flagReplace {
		failOnError(fmt.Errorf("only one of --patch and --replace should be specified"))
	}

	if isBulkPatchMode && (where == "" && len(filterIdentifiers) == 0) {
		failOnError(fmt.Errorf("bulk patch mode requires --where or --filter flags"))
	}

	if err := validateSpaceFlag(isBulkPatchMode); err != nil {
		failOnError(err)
	}

	if err := validateStdinFlags(); err != nil {
		failOnError(err)
	}

	return isBulkPatchMode
}

func runBulkFilterUpdate() error {
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

	// Create patch data
	patchData := make(map[string]interface{})

	// Add filter-specific fields
	if filterUpdateArgs.whereData != "" {
		patchData["WhereData"] = filterUpdateArgs.whereData
	}
	if filterUpdateArgs.resourceType != "" {
		patchData["ResourceType"] = filterUpdateArgs.resourceType
	}
	if filterUpdateArgs.fromSpace != "" {
		fromSpace, err := apiGetSpaceFromSlug(filterUpdateArgs.fromSpace, "SpaceID")
		if err != nil {
			return err
		}
		patchData["FromSpaceID"] = fromSpace.SpaceID.String()
	}

	// Merge with stdin data if provided
	if flagPopulateModelFromStdin || flagFilename != "" {
		stdinBytes, err := getBytesFromFlags()
		if err != nil {
			return err
		}
		if len(stdinBytes) > 0 && string(stdinBytes) != "null" {
			var stdinData map[string]interface{}
			if err := json.Unmarshal(stdinBytes, &stdinData); err != nil {
				return fmt.Errorf("failed to parse stdin data: %w", err)
			}
			// Merge stdinData into patchData
			for k, v := range stdinData {
				patchData[k] = v
			}
		}
	}

	// Add labels if specified
	if len(label) > 0 {
		labelMap := make(map[string]string)
		// Preserve existing labels if any
		if existingLabels, ok := patchData["Labels"]; ok {
			if labelMapInterface, ok := existingLabels.(map[string]interface{}); ok {
				for k, v := range labelMapInterface {
					if strVal, ok := v.(string); ok {
						labelMap[k] = strVal
					}
				}
			}
		}
		err := setLabels(&labelMap)
		if err != nil {
			return err
		}
		patchData["Labels"] = labelMap
	}

	// Convert to JSON
	patchJSON, err := json.Marshal(patchData)
	if err != nil {
		return err
	}

	// Build bulk patch parameters
	include := "SpaceID"
	params := &goclientnew.BulkPatchFiltersParams{
		Where:   &effectiveWhere,
		Include: &include,
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
	isBulkPatchMode := checkFilterConflictingArgs(args)

	if isBulkPatchMode {
		return runBulkFilterUpdate()
	}

	// Single filter update logic
	if len(args) < 2 {
		return errors.New("single filter update requires: <slug or id> <from> [options...]")
	}

	currentFilter, err := apiGetFilterFromSlug(args[0], "*") // get all fields for RMW
	if err != nil {
		return err
	}

	spaceID := uuid.MustParse(selectedSpaceID)

	if filterPatch {
		// Single filter patch mode - we'll apply changes directly to the filter object
		// Handle --from-stdin or --filename
		if flagPopulateModelFromStdin || flagFilename != "" {
			existingFilter := currentFilter
			if err := populateModelFromFlags(currentFilter); err != nil {
				return err
			}
			// Ensure essential fields can't be clobbered
			currentFilter.OrganizationID = existingFilter.OrganizationID
			currentFilter.SpaceID = existingFilter.SpaceID
			currentFilter.FilterID = existingFilter.FilterID
		}

		// Add labels if specified
		if len(label) > 0 {
			err := setLabels(&currentFilter.Labels)
			if err != nil {
				return err
			}
		}

		// Add filter details from args
		currentFilter.From = args[1]

		// Set optional fields from flags
		if where != "" {
			currentFilter.Where = where
		}
		if filterUpdateArgs.whereData != "" {
			currentFilter.WhereData = filterUpdateArgs.whereData
		}
		if filterUpdateArgs.resourceType != "" {
			currentFilter.ResourceType = filterUpdateArgs.resourceType
		}
		if filterUpdateArgs.fromSpace != "" {
			fromSpace, err := apiGetSpaceFromSlug(filterUpdateArgs.fromSpace, "SpaceID")
			if err != nil {
				return err
			}
			currentFilter.FromSpaceID = &fromSpace.SpaceID
		}

		// Convert filter to patch data
		patchData, err := json.Marshal(currentFilter)
		if err != nil {
			return fmt.Errorf("failed to marshal patch data: %w", err)
		}

		filterDetails, err := patchFilter(spaceID, currentFilter.FilterID, patchData)
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

	// If this was set from stdin, it will be overridden
	currentFilter.SpaceID = spaceID
	currentFilter.From = args[1]

	// Set optional fields from flags
	if where != "" {
		currentFilter.Where = where
	}
	if filterUpdateArgs.whereData != "" {
		currentFilter.WhereData = filterUpdateArgs.whereData
	}
	if filterUpdateArgs.resourceType != "" {
		currentFilter.ResourceType = filterUpdateArgs.resourceType
	}
	if filterUpdateArgs.fromSpace != "" {
		fromSpace, err := apiGetSpaceFromSlug(filterUpdateArgs.fromSpace, "SpaceID")
		if err != nil {
			return err
		}
		currentFilter.FromSpaceID = &fromSpace.SpaceID
	}

	filterRes, err := cubClientNew.UpdateFilterWithResponse(ctx, spaceID, currentFilter.FilterID, *currentFilter)
	if IsAPIError(err, filterRes) {
		return InterpretErrorGeneric(err, filterRes)
	}

	filterDetails := filterRes.JSON200
	displayUpdateResults(filterDetails, "filter", args[0], filterDetails.FilterID.String(), displayFilterDetails)
	return nil
}

func handleBulkFilterCreateOrUpdateResponse(responses200 *[]goclientnew.FilterCreateOrUpdateResponse, responses207 *[]goclientnew.FilterCreateOrUpdateResponse, statusCode int, operationName, contextInfo string) error {
	var responses *[]goclientnew.FilterCreateOrUpdateResponse
	if statusCode == 200 && responses200 != nil {
		responses = responses200
	} else if statusCode == 207 && responses207 != nil {
		responses = responses207
	} else {
		return fmt.Errorf("unexpected status code %d or no response data", statusCode)
	}

	if responses == nil {
		return fmt.Errorf("no response data received")
	}

	successCount := 0
	failureCount := 0
	var failures []string

	for _, resp := range *responses {
		if resp.Error == nil && resp.Filter != nil {
			successCount++
			if verbose {
				fmt.Printf("Successfully %sd filter: %s (ID: %s)\n", operationName, resp.Filter.Slug, resp.Filter.FilterID)
			}
		} else {
			failureCount++
			errorMsg := "unknown error"
			if resp.Error != nil && resp.Error.Message != "" {
				errorMsg = resp.Error.Message
			}
			if resp.Filter != nil {
				failures = append(failures, fmt.Sprintf("  - %s: %s", resp.Filter.Slug, errorMsg))
			} else {
				failures = append(failures, fmt.Sprintf("  - (unknown filter): %s", errorMsg))
			}
		}
	}

	// Display summary
	if !jsonOutput {
		fmt.Printf("\nBulk %s operation completed:\n", operationName)
		fmt.Printf("  Success: %d filter(s)\n", successCount)
		if failureCount > 0 {
			fmt.Printf("  Failed: %d filter(s)\n", failureCount)
			if verbose && len(failures) > 0 {
				fmt.Println("\nFailures:")
				for _, failure := range failures {
					fmt.Println(failure)
				}
			}
		}
		if contextInfo != "" {
			fmt.Printf("  Context: %s\n", contextInfo)
		}
	}

	// Return success only if all operations succeeded
	if statusCode == 207 || failureCount > 0 {
		return fmt.Errorf("bulk %s partially failed: %d succeeded, %d failed", operationName, successCount, failureCount)
	}

	return nil
}

func patchFilter(spaceID uuid.UUID, filterID uuid.UUID, patchData []byte) (*goclientnew.Filter, error) {
	filterRes, err := cubClientNew.PatchFilterWithBodyWithResponse(
		ctx,
		spaceID,
		filterID,
		"application/merge-patch+json",
		bytes.NewReader(patchData),
	)
	if IsAPIError(err, filterRes) {
		return nil, InterpretErrorGeneric(err, filterRes)
	}

	return filterRes.JSON200, nil
}
