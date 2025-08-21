// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"

	goclientnew "github.com/confighub/sdk/openapi/goclient-new"
	"github.com/google/uuid"
	"github.com/spf13/cobra"
)

var spaceUpdateCmd = &cobra.Command{
	Use:   "update [slug or id]",
	Short: "Update a space",
	Long: `Update a space.

Single space update examples:
  # Update a space by slug
  cub space update my-space --from-stdin

  # Update a space with patch mode
  cub space update --patch my-space --label "Environment=prod"

Bulk update examples:
  # Bulk patch spaces by filter
  cub space update --patch --where "Labels.Environment = 'dev'" --label "updated=true"

  # Patch specific spaces by identifier
  cub space update --patch --space "space1,space2" --from-stdin`,
	Args: cobra.RangeArgs(0, 1),
	RunE: spaceUpdateCmdRun,
}

func init() {
	addStandardUpdateFlags(spaceUpdateCmd)
	spaceUpdateCmd.Flags().StringSliceVar(&spaceIdentifiers, "space", []string{}, "target specific spaces by slug or UUID for bulk patch (can be repeated or comma-separated)")
	spaceUpdateCmd.Flags().BoolVar(&isPatch, "patch", false, "use patch API for individual or bulk operations")
	enableWhereFlag(spaceUpdateCmd)
	spaceCmd.AddCommand(spaceUpdateCmd)
}

func checkSpaceUpdateConflictingArgs(args []string) (bool, error) {
	// Check for bulk patch mode (no positional args with --patch)
	isBulkPatchMode := isPatch && len(args) == 0

	// Validate label removal only works with patch
	if err := ValidateLabelRemoval(label, isPatch); err != nil {
		return false, err
	}

	if !isBulkPatchMode && (where != "" || len(spaceIdentifiers) > 0) {
		return false, fmt.Errorf("--where or --space can only be specified with --patch and no space positional argument")
	}

	// Check for mutual exclusivity between --space and --where flags
	if len(spaceIdentifiers) > 0 && where != "" {
		return false, fmt.Errorf("--space and --where flags are mutually exclusive")
	}

	if isPatch && flagReplace {
		return false, fmt.Errorf("only one of --patch and --replace should be specified")
	}

	if isBulkPatchMode && (where == "" && len(spaceIdentifiers) == 0) {
		return false, fmt.Errorf("bulk patch mode requires --where or --space flags")
	}

	if err := validateStdinFlags(); err != nil {
		return isBulkPatchMode, err
	}
	return isBulkPatchMode, nil
}

func spaceUpdateCmdRun(cmd *cobra.Command, args []string) error {
	isBulkPatchMode, err := checkSpaceUpdateConflictingArgs(args)
	if err != nil {
		return err
	}

	if isBulkPatchMode {
		return runBulkSpaceUpdate()
	}

	if len(args) == 0 {
		return errors.New("space identifier is required for single space update")
	}

	return runSingleSpaceUpdate(args)
}

func runSingleSpaceUpdate(args []string) error {
	currentSpace, err := apiGetSpaceFromSlug(args[0], "*") // get all fields for RMW
	if err != nil {
		return err
	}

	currentSpaceID := currentSpace.SpaceID

	if isPatch {
		// Single space patch mode - we'll apply changes directly to the space object
		// Handle --from-stdin or --filename
		if flagPopulateModelFromStdin || flagFilename != "" {
			existingSpace := currentSpace
			if err := populateModelFromFlags(currentSpace); err != nil {
				return err
			}
			// Ensure essential fields can't be clobbered
			currentSpace.OrganizationID = existingSpace.OrganizationID
			currentSpace.SpaceID = existingSpace.SpaceID
		}

		// Add labels if specified
		if len(label) > 0 {
			err := setLabels(&currentSpace.Labels)
			if err != nil {
				return err
			}
		}

		// Convert space to patch data
		patchData, err := json.Marshal(currentSpace)
		if err != nil {
			return fmt.Errorf("failed to marshal patch data: %w", err)
		}

		spaceDetails, err := patchSpace(currentSpaceID, patchData)
		if err != nil {
			return err
		}

		displayUpdateResults(spaceDetails, "space", args[0], spaceDetails.SpaceID.String(), displaySpaceDetails)
		return nil
	}

	// Traditional update mode
	newBody := currentSpace

	// Handle --from-stdin or --filename with optional --replace
	if flagPopulateModelFromStdin || flagFilename != "" {
		if flagReplace {
			// Replace mode - create new entity, allow Version to be overwritten
			newBody = new(goclientnew.Space)
			newBody.Version = currentSpace.Version
		}

		if err := populateModelFromFlags(newBody); err != nil {
			return err
		}

		// Ensure essential fields can't be clobbered
		newBody.OrganizationID = currentSpace.OrganizationID
		newBody.SpaceID = currentSpace.SpaceID
	}
	err = setLabels(&newBody.Labels)
	if err != nil {
		return err
	}

	spaceRes, err := cubClientNew.UpdateSpaceWithResponse(ctx, currentSpaceID, *newBody)
	if IsAPIError(err, spaceRes) {
		return InterpretErrorGeneric(err, spaceRes)
	}

	spaceDetails := spaceRes.JSON200
	displayUpdateResults(spaceDetails, "space", args[0], spaceDetails.SpaceID.String(), displaySpaceDetails)

	return nil
}

func patchSpace(spaceID uuid.UUID, patchData []byte) (*goclientnew.Space, error) {
	spaceRes, err := cubClientNew.PatchSpaceWithBodyWithResponse(
		ctx,
		spaceID,
		"application/merge-patch+json",
		bytes.NewReader(patchData),
	)
	if IsAPIError(err, spaceRes) {
		return nil, InterpretErrorGeneric(err, spaceRes)
	}

	return spaceRes.JSON200, nil
}

func createSpacePatchData(currentSpace, newSpace *goclientnew.Space) ([]byte, error) {
	// Build patch data using consolidated function (no entity-specific fields for space)
	patchData, err := BuildPatchData(nil)
	if err != nil {
		return nil, err
	}

	// Parse the patch data to filter out protected fields
	var patchMap map[string]interface{}
	if err := json.Unmarshal(patchData, &patchMap); err != nil {
		return nil, fmt.Errorf("failed to parse patch data: %w", err)
	}

	// Don't allow changing IDs
	delete(patchMap, "SpaceID")
	delete(patchMap, "OrganizationID")

	return json.Marshal(patchMap)
}

func runBulkSpaceUpdate() error {
	// Build the where clause
	var effectiveWhere string
	if len(spaceIdentifiers) > 0 {
		// Convert space identifiers to where clause
		whereClause, err := buildWhereClauseFromIdentifiers(spaceIdentifiers, "SpaceID", "Slug")
		if err != nil {
			return fmt.Errorf("error building where clause from space identifiers: %w", err)
		}
		effectiveWhere = whereClause
	} else {
		effectiveWhere = where
	}

	// Build patch data using consolidated function (no entity-specific fields for space)
	patchData, err := BuildPatchData(nil)
	if err != nil {
		return err
	}

	// Parse the patch data to filter out protected fields
	var patchMap map[string]interface{}
	if err := json.Unmarshal(patchData, &patchMap); err != nil {
		return fmt.Errorf("failed to parse patch data: %w", err)
	}

	// Don't allow changing IDs
	delete(patchMap, "SpaceID")
	delete(patchMap, "OrganizationID")

	patchData, err = json.Marshal(patchMap)
	if err != nil {
		return fmt.Errorf("error marshaling patch data: %w", err)
	}

	// Build bulk patch parameters
	params := &goclientnew.BulkPatchSpacesParams{
		Where: &effectiveWhere,
	}

	// Set include parameter to expand OrganizationID if needed
	include := "OrganizationID"
	params.Include = &include

	// Call the bulk patch API (organization-level API)
	bulkRes, err := cubClientNew.BulkPatchSpacesWithBodyWithResponse(
		ctx,
		params,
		"application/merge-patch+json",
		bytes.NewReader(patchData),
	)
	if IsAPIError(err, bulkRes) {
		return InterpretErrorGeneric(err, bulkRes)
	}

	// Handle response based on status code
	var responses []goclientnew.SpaceCreateOrUpdateResponse
	var statusCode int

	if bulkRes.JSON200 != nil {
		responses = *bulkRes.JSON200
		statusCode = 200
	} else if bulkRes.JSON207 != nil {
		responses = *bulkRes.JSON207
		statusCode = 207
	} else {
		return fmt.Errorf("unexpected response from bulk patch API")
	}

	return handleBulkSpaceCreateOrUpdateResponse(responses, statusCode, "patch", "")
}
