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

var changesetUpdateCmd = &cobra.Command{
	Use:   "update [<slug or id>] [options...]",
	Short: "Update a changeset or multiple changesets",
	Long: `Update a changeset or multiple changesets using bulk operations.

Single changeset update:
  cub changeset update my-changeset --description "Updated description"

Bulk update with --patch:
Update multiple changesets at once based on search criteria. Requires --patch flag with no positional arguments.

Examples:
  # Update description for all changesets matching a pattern
  echo '{"Description": "Archived changeset"}' | cub changeset update --patch --where "CreatedAt < '2024-01-01'" --from-stdin

  # Update description for specific changesets
  cub changeset update --patch --changeset cs1,cs2 --description "Updated description"

  # Update tags for changesets using JSON patch
  echo '{"StartTagID": "new-tag-uuid", "EndTagID": "another-tag-uuid"}' | cub changeset update --patch --where "Description LIKE 'Release%'" --from-stdin`,
	Args:        cobra.MinimumNArgs(0), // Allow 0 args for bulk mode
	RunE:        changesetUpdateCmdRun,
	Annotations: map[string]string{"OrgLevel": ""},
}

var (
	changesetPatch       bool
	changesetIdentifiers []string
	changesetUpdateArgs  struct {
		description string
	}
)

func init() {
	addStandardUpdateFlags(changesetUpdateCmd)
	changesetUpdateCmd.Flags().BoolVar(&changesetPatch, "patch", false, "use patch API for individual or bulk operations")
	enableWhereFlag(changesetUpdateCmd)
	enableFilterFlag(changesetUpdateCmd)
	changesetUpdateCmd.Flags().StringSliceVar(&changesetIdentifiers, "changeset", []string{}, "target specific changesets by slug or UUID for bulk patch (can be repeated or comma-separated)")

	// Single update specific flags
	changesetUpdateCmd.Flags().StringVar(&changesetUpdateArgs.description, "description", "", "human-readable description of the change")

	changesetCmd.AddCommand(changesetUpdateCmd)
}

func checkChangeSetConflictingArgs(args []string) bool {
	// Check for bulk patch mode: no positional args
	isBulkPatchMode := len(args) == 0

	if isBulkPatchMode {
		if !changesetPatch {
			failOnError(errors.New("--patch is required in bulk mode"))
		}

		// Check for mutual exclusivity between --changeset and --where flags
		if len(changesetIdentifiers) > 0 && where != "" {
			failOnError(fmt.Errorf("--changeset and --where flags are mutually exclusive"))
		}

	} else {
		// Single create mode validation
		if len(args) != 1 {
			failOnError(errors.New("single changeset update requires exactly one argument: <slug or id>"))
		}

		if filter != "" || where != "" || len(changesetIdentifiers) > 0 {
			failOnError(fmt.Errorf("--filter, --where, or --changeset can only be specified with --patch and no positional arguments"))
		}
	}

	if changesetPatch && flagReplace {
		failOnError(fmt.Errorf("only one of --patch and --replace should be specified"))
	}

	if err := validateSpaceFlag(isBulkPatchMode); err != nil {
		failOnError(err)
	}

	if err := validateStdinFlags(); err != nil {
		failOnError(err)
	}

	return isBulkPatchMode
}

func runBulkChangeSetUpdate() error {
	// Parse filter parameter
	filterID, err := parseFilterFlag(filter)
	if err != nil {
		return err
	}

	// Build WHERE clause from changeset identifiers or use provided where clause
	var effectiveWhere string
	if len(changesetIdentifiers) > 0 {
		whereClause, err := buildWhereClauseFromChangeSets(changesetIdentifiers)
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

	// Add changeset-specific fields
	if changesetUpdateArgs.description != "" {
		patchData["Description"] = changesetUpdateArgs.description
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
	params := &goclientnew.BulkPatchChangeSetsParams{
		Where:   &effectiveWhere,
		Include: &include,
	}
	if filterID != "" {
		params.Filter = &filterID
	}

	// Call the bulk patch API
	bulkRes, err := cubClientNew.BulkPatchChangeSetsWithBodyWithResponse(
		ctx,
		params,
		"application/merge-patch+json",
		bytes.NewReader(patchJSON),
	)
	if err != nil {
		return err
	}

	// Handle the response
	return handleBulkChangeSetCreateOrUpdateResponse(bulkRes.JSON200, bulkRes.JSON207, bulkRes.StatusCode(), "update", effectiveWhere)
}

func changesetUpdateCmdRun(cmd *cobra.Command, args []string) error {
	isBulkPatchMode := checkChangeSetConflictingArgs(args)

	if isBulkPatchMode {
		return runBulkChangeSetUpdate()
	}

	// Single changeset update logic
	if len(args) != 1 {
		return errors.New("single changeset update requires exactly one argument: <slug or id>")
	}

	currentChangeSet, err := apiGetChangeSetFromSlug(args[0], "*") // get all fields for RMW
	if err != nil {
		return err
	}

	spaceID := uuid.MustParse(selectedSpaceID)

	if changesetPatch {
		// Single changeset patch mode - we'll apply changes directly to the changeset object
		// Handle --from-stdin or --filename
		if flagPopulateModelFromStdin || flagFilename != "" {
			existingChangeSet := currentChangeSet
			if err := populateModelFromFlags(currentChangeSet); err != nil {
				return err
			}
			// Ensure essential fields can't be clobbered
			currentChangeSet.OrganizationID = existingChangeSet.OrganizationID
			currentChangeSet.SpaceID = existingChangeSet.SpaceID
			currentChangeSet.ChangeSetID = existingChangeSet.ChangeSetID
		}

		// Add labels if specified
		if len(label) > 0 {
			err := setLabels(&currentChangeSet.Labels)
			if err != nil {
				return err
			}
		}

		// Add changeset details from flags
		if changesetUpdateArgs.description != "" {
			currentChangeSet.Description = changesetUpdateArgs.description
		}

		// Convert changeset to patch data
		patchData, err := json.Marshal(currentChangeSet)
		if err != nil {
			return fmt.Errorf("failed to marshal patch data: %w", err)
		}

		changesetDetails, err := patchChangeSet(spaceID, currentChangeSet.ChangeSetID, patchData)
		if err != nil {
			return err
		}

		displayUpdateResults(changesetDetails, "changeset", args[0], changesetDetails.ChangeSetID.String(), displayChangeSetDetails)
		return nil
	}

	// Traditional update mode
	// Handle --from-stdin or --filename with optional --replace
	if flagPopulateModelFromStdin || flagFilename != "" {
		existingChangeSet := currentChangeSet
		if flagReplace {
			// Replace mode - create new entity, allow Version to be overwritten
			currentChangeSet = new(goclientnew.ChangeSet)
			currentChangeSet.Version = existingChangeSet.Version
		}

		if err := populateModelFromFlags(currentChangeSet); err != nil {
			return err
		}

		// Ensure essential fields can't be clobbered
		currentChangeSet.OrganizationID = existingChangeSet.OrganizationID
		currentChangeSet.SpaceID = existingChangeSet.SpaceID
		currentChangeSet.ChangeSetID = existingChangeSet.ChangeSetID
	}
	err = setLabels(&currentChangeSet.Labels)
	if err != nil {
		return err
	}

	// If this was set from stdin, it will be overridden
	currentChangeSet.SpaceID = spaceID

	// Set changeset-specific fields from flags
	if changesetUpdateArgs.description != "" {
		currentChangeSet.Description = changesetUpdateArgs.description
	}

	changesetRes, err := cubClientNew.UpdateChangeSetWithResponse(ctx, spaceID, currentChangeSet.ChangeSetID, *currentChangeSet)
	if IsAPIError(err, changesetRes) {
		return InterpretErrorGeneric(err, changesetRes)
	}

	changesetDetails := changesetRes.JSON200
	displayUpdateResults(changesetDetails, "changeset", args[0], changesetDetails.ChangeSetID.String(), displayChangeSetDetails)
	return nil
}

func handleBulkChangeSetCreateOrUpdateResponse(responses200 *[]goclientnew.ChangeSetCreateOrUpdateResponse, responses207 *[]goclientnew.ChangeSetCreateOrUpdateResponse, statusCode int, operationName, contextInfo string) error {
	return displayBulkGenericCreateOrUpdateResults(
		responses200, responses207, statusCode, "changeset", operationName, contextInfo,
		func(r *goclientnew.ChangeSetCreateOrUpdateResponse) *goclientnew.ResponseError { return r.Error },
		func(r *goclientnew.ChangeSetCreateOrUpdateResponse) string {
			if r.ChangeSet != nil {
				return fmt.Sprintf("%s (ID: %s)", r.ChangeSet.Slug, r.ChangeSet.ChangeSetID)
			}
			return ""
		},
	)
}

func patchChangeSet(spaceID uuid.UUID, changesetID uuid.UUID, patchData []byte) (*goclientnew.ChangeSet, error) {
	changesetRes, err := cubClientNew.PatchChangeSetWithBodyWithResponse(
		ctx,
		spaceID,
		changesetID,
		"application/merge-patch+json",
		bytes.NewReader(patchData),
	)
	if IsAPIError(err, changesetRes) {
		return nil, InterpretErrorGeneric(err, changesetRes)
	}

	return changesetRes.JSON200, nil
}
