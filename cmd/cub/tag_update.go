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

var tagUpdateCmd = &cobra.Command{
	Use:   "update [<slug or id>]",
	Short: "Update a tag or multiple tags",
	Long: `Update a tag or multiple tags using bulk operations.

Single tag update:
  cub tag update my-tag --label environment=production

Bulk update with --patch:
Update multiple tags at once based on search criteria. Requires --patch flag with no positional arguments.

Examples:
  # Update labels for all tags matching a pattern
  echo '{"Labels": {"archived": "true"}}' | cub tag update --patch --where "CreatedAt < '2024-01-01'" --from-stdin

  # Update display names with a prefix for specific tags
  echo '{"DisplayName": "archived-v1.0"}' | cub tag update --patch --tag release-v1.0,release-v1.1 --from-stdin

  # Update labels for tags in multiple spaces
  cub tag update --patch --where "Labels.version = '1.0'" --label status=deprecated`,
	Args:        cobra.MinimumNArgs(0), // Allow 0 args for bulk mode
	RunE:        tagUpdateCmdRun,
	Annotations: map[string]string{"OrgLevel": ""},
}

var (
	tagPatch       bool
	tagIdentifiers []string
)

func init() {
	addStandardUpdateFlags(tagUpdateCmd)
	tagUpdateCmd.Flags().BoolVar(&tagPatch, "patch", false, "use patch API for individual or bulk operations")
	enableWhereFlag(tagUpdateCmd)
	enableFilterFlag(tagUpdateCmd)
	tagUpdateCmd.Flags().StringSliceVar(&tagIdentifiers, "tag", []string{}, "target specific tags by slug or UUID for bulk patch (can be repeated or comma-separated)")
	tagCmd.AddCommand(tagUpdateCmd)
}

func checkTagConflictingArgs(args []string) bool {
	// Check for bulk patch mode: no positional args
	isBulkPatchMode := len(args) == 0

	if isBulkPatchMode {
		if !tagPatch {
			failOnError(errors.New("--patch is required in bulk mode"))
		}

		// Check for mutual exclusivity between --tag and --where flags
		if len(tagIdentifiers) > 0 && where != "" {
			failOnError(fmt.Errorf("--tag and --where flags are mutually exclusive"))
		}

	} else {
		// Single create mode validation
		if len(args) != 1 {
			failOnError(errors.New("single tag update requires exactly one argument: <slug or id>"))
		}

		if filter != "" || where != "" || len(tagIdentifiers) > 0 {
			failOnError(fmt.Errorf("--filter, --where, or --tag can only be specified with --patch and no positional arguments"))
		}
	}

	if tagPatch && flagReplace {
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

func runBulkTagUpdate() error {
	// Parse filter parameter
	filterID, err := parseFilterFlag(filter)
	if err != nil {
		return err
	}

	// Build WHERE clause from tag identifiers or use provided where clause
	var effectiveWhere string
	if len(tagIdentifiers) > 0 {
		whereClause, err := buildWhereClauseFromTags(tagIdentifiers)
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
	params := &goclientnew.BulkPatchTagsParams{
		Where:   &effectiveWhere,
		Include: &include,
	}
	if filterID != "" {
		params.Filter = &filterID
	}

	// Call the bulk patch API
	bulkRes, err := cubClientNew.BulkPatchTagsWithBodyWithResponse(
		ctx,
		params,
		"application/merge-patch+json",
		bytes.NewReader(patchJSON),
	)
	if err != nil {
		return err
	}

	// Handle the response
	return handleBulkTagCreateOrUpdateResponse(bulkRes.JSON200, bulkRes.JSON207, bulkRes.StatusCode(), "update", effectiveWhere)
}

func tagUpdateCmdRun(cmd *cobra.Command, args []string) error {
	isBulkPatchMode := checkTagConflictingArgs(args)

	if isBulkPatchMode {
		return runBulkTagUpdate()
	}

	// Single tag update logic
	if len(args) != 1 {
		return errors.New("single tag update requires exactly one argument: <slug or id>")
	}

	currentTag, err := apiGetTagFromSlug(args[0], "*") // get all fields for RMW
	if err != nil {
		return err
	}

	spaceID := uuid.MustParse(selectedSpaceID)

	if tagPatch {
		// Single tag patch mode - we'll apply changes directly to the tag object
		// Handle --from-stdin or --filename
		if flagPopulateModelFromStdin || flagFilename != "" {
			existingTag := currentTag
			if err := populateModelFromFlags(currentTag); err != nil {
				return err
			}
			// Ensure essential fields can't be clobbered
			currentTag.OrganizationID = existingTag.OrganizationID
			currentTag.SpaceID = existingTag.SpaceID
			currentTag.TagID = existingTag.TagID
		}

		// Add labels if specified
		if len(label) > 0 {
			err := setLabels(&currentTag.Labels)
			if err != nil {
				return err
			}
		}

		// Convert tag to patch data
		patchData, err := json.Marshal(currentTag)
		if err != nil {
			return fmt.Errorf("failed to marshal patch data: %w", err)
		}

		tagDetails, err := patchTag(spaceID, currentTag.TagID, patchData)
		if err != nil {
			return err
		}

		displayUpdateResults(tagDetails, "tag", args[0], tagDetails.TagID.String(), displayTagDetails)
		return nil
	}

	// Traditional update mode
	// Handle --from-stdin or --filename with optional --replace
	if flagPopulateModelFromStdin || flagFilename != "" {
		existingTag := currentTag
		if flagReplace {
			// Replace mode - create new entity, allow Version to be overwritten
			currentTag = new(goclientnew.Tag)
			currentTag.Version = existingTag.Version
		}

		if err := populateModelFromFlags(currentTag); err != nil {
			return err
		}

		// Ensure essential fields can't be clobbered
		currentTag.OrganizationID = existingTag.OrganizationID
		currentTag.SpaceID = existingTag.SpaceID
		currentTag.TagID = existingTag.TagID
	}
	err = setLabels(&currentTag.Labels)
	if err != nil {
		return err
	}

	// If this was set from stdin, it will be overridden
	currentTag.SpaceID = spaceID

	tagRes, err := cubClientNew.UpdateTagWithResponse(ctx, spaceID, currentTag.TagID, *currentTag)
	if IsAPIError(err, tagRes) {
		return InterpretErrorGeneric(err, tagRes)
	}

	tagDetails := tagRes.JSON200
	displayUpdateResults(tagDetails, "tag", args[0], tagDetails.TagID.String(), displayTagDetails)
	return nil
}

func handleBulkTagCreateOrUpdateResponse(responses200 *[]goclientnew.TagCreateOrUpdateResponse, responses207 *[]goclientnew.TagCreateOrUpdateResponse, statusCode int, operationName, contextInfo string) error {
	return displayBulkGenericCreateOrUpdateResults(
		responses200, responses207, statusCode, "tag", operationName, contextInfo,
		func(r *goclientnew.TagCreateOrUpdateResponse) *goclientnew.ResponseError { return r.Error },
		func(r *goclientnew.TagCreateOrUpdateResponse) string {
			if r.Tag != nil {
				return fmt.Sprintf("%s (ID: %s)", r.Tag.Slug, r.Tag.TagID)
			}
			return ""
		},
	)
}

func patchTag(spaceID uuid.UUID, tagID uuid.UUID, patchData []byte) (*goclientnew.Tag, error) {
	tagRes, err := cubClientNew.PatchTagWithBodyWithResponse(
		ctx,
		spaceID,
		tagID,
		"application/merge-patch+json",
		bytes.NewReader(patchData),
	)
	if IsAPIError(err, tagRes) {
		return nil, InterpretErrorGeneric(err, tagRes)
	}

	return tagRes.JSON200, nil
}
