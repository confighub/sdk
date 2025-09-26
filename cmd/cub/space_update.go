// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"bytes"
	"errors"
	"fmt"

	"github.com/confighub/sdk/cubapi"
	goclientnew "github.com/confighub/sdk/openapi/goclient-new"
	"github.com/google/uuid"
	"github.com/spf13/cobra"
)

var spaceUpdateArgs struct {
	whereTrigger string
}

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
	spaceUpdateCmd.Flags().StringVar(&spaceUpdateArgs.whereTrigger, "where-trigger", "", "filter expression to identify Triggers that should be invoked on Units within this Space (use '-' to clear)")
	enableWhereFlag(spaceUpdateCmd)
	enableFilterFlag(spaceUpdateCmd)
	spaceCmd.AddCommand(spaceUpdateCmd)
}

func checkSpaceUpdateConflictingArgs(args []string) (bool, error) {
	// Check for bulk patch mode (no positional args with --patch)
	isBulkPatchMode := len(args) == 0

	if isBulkPatchMode {
		if !isPatch {
			failOnError(errors.New("--patch is required in bulk mode"))
		}

		// Check for mutual exclusivity between --space and --where flags
		if len(spaceIdentifiers) > 0 && where != "" {
			return false, fmt.Errorf("--space and --where flags are mutually exclusive")
		}

	} else {
		if len(args) != 1 {
			return false, errors.New("space name is required for single space update")
		}

		if filter != "" || where != "" || len(spaceIdentifiers) > 0 {
			return false, fmt.Errorf("--filter, --where, or --space can only be specified with --patch and no space positional argument")
		}
	}

	if isPatch && flagReplace {
		return false, fmt.Errorf("only one of --patch and --replace should be specified")
	}

	// Validate label removal only works with patch
	if err := ValidateLabelRemoval(label, isPatch); err != nil {
		return false, err
	}
	// Validate delete gate removal only works with patch
	if err := ValidateDeleteGateRemoval(deleteGate, isPatch); err != nil {
		return false, err
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
		// Single space patch mode

		// Build patch data using BuildPatchData with space enhancer
		var spaceEnhancer PatchEnhancer

		// Add WhereTrigger if provided
		if spaceUpdateArgs.whereTrigger == "-" || spaceUpdateArgs.whereTrigger != "" {
			spaceEnhancer = func(patchMap map[string]interface{}) {
				if spaceUpdateArgs.whereTrigger == "-" {
					patchMap["WhereTrigger"] = ""
				} else {
					patchMap["WhereTrigger"] = spaceUpdateArgs.whereTrigger
				}
			}
		}

		patchData, err := BuildPatchData(spaceEnhancer)
		if err != nil {
			return fmt.Errorf("failed to build patch data: %w", err)
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
	err = setDeleteGates(&newBody.DeleteGates)
	if err != nil {
		return err
	}

	// Set WhereTrigger if provided
	if spaceUpdateArgs.whereTrigger == "-" {
		newBody.WhereTrigger = ""
	} else if spaceUpdateArgs.whereTrigger != "" {
		newBody.WhereTrigger = spaceUpdateArgs.whereTrigger
	}

	spaceRes, err := cubClientNew.UpdateSpaceWithResponse(ctx, currentSpaceID, *newBody)
	if cubapi.IsAPIError(err, spaceRes) {
		return cubapi.InterpretErrorGeneric(err, spaceRes)
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
	if cubapi.IsAPIError(err, spaceRes) {
		return nil, cubapi.InterpretErrorGeneric(err, spaceRes)
	}

	return spaceRes.JSON200, nil
}

func runBulkSpaceUpdate() error {
	// Parse filter parameter
	filterID, err := parseFilterFlag(filter)
	if err != nil {
		return err
	}

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

	// Build patch data with space enhancer
	var spaceEnhancer PatchEnhancer

	// Add WhereTrigger if provided
	if spaceUpdateArgs.whereTrigger == "-" || spaceUpdateArgs.whereTrigger != "" {
		spaceEnhancer = func(patchMap map[string]interface{}) {
			if spaceUpdateArgs.whereTrigger == "-" {
				patchMap["WhereTrigger"] = ""
			} else {
				patchMap["WhereTrigger"] = spaceUpdateArgs.whereTrigger
			}
		}
	}

	patchData, err := BuildPatchData(spaceEnhancer)
	if err != nil {
		return err
	}

	// Build bulk patch parameters
	params := &goclientnew.BulkPatchSpacesParams{
		Where: &effectiveWhere,
	}
	if filterID != "" {
		params.Filter = &filterID
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
	if cubapi.IsAPIError(err, bulkRes) {
		return cubapi.InterpretErrorGeneric(err, bulkRes)
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
