// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"bytes"
	"fmt"

	"github.com/cockroachdb/errors"
	"github.com/confighub/sdk/core/cubapi"
	goclientnew "github.com/confighub/sdk/core/openapi/goclient-new"
	"github.com/google/uuid"
	"github.com/spf13/cobra"
)

var changeorderUpdateCmd = &cobra.Command{
	Use:   "update [<slug or id>] [options...]",
	Short: "Update a changeorder or multiple changeorders",
	Long: getCommandHelp(`Update a changeorder or multiple changeorders using bulk operations.

Single changeorder update:
`+"```"+`
  cub changeorder update my-changeorder --description "Updated description"
`+"```"+`

Bulk update with --patch:

Update multiple changeorders at once based on search criteria. Requires --patch flag with no positional arguments.

Examples:
`+"```"+`
  # Update description for all changeorders matching a pattern
  echo '{"Description": "Archived changeorder"}' | cub changeorder update --patch --where "CreatedAt < '2024-01-01'" --from-stdin

  # Update description for specific changeorders
  cub changeorder update --patch --changeorder cs1,cs2 --description "Updated description"

  # Abort a changeorder, so that it reads as Aborted rather than still on its way
  cub changeorder update my-changeorder --aborted-reason "superseded by the 1.43 rollout"

  # Put an aborted changeorder back on its way
  cub changeorder update my-changeorder --aborted-reason ""

  # Narrow where a changeorder is headed, ANDed with its space filter
  cub changeorder update my-changeorder --where-space-field "Labels.Region = 'use2'"

  # Clear the expression, leaving the space filter (if any) to say where it is headed
  cub changeorder update my-changeorder --where-space-field "-"

  # Point a changeorder at a different Filter over Spaces
  cub changeorder update my-changeorder --space-filter platform/prod-spaces

  # Clear the filter, leaving the expression (if any) to say where it is headed
  cub changeorder update my-changeorder --space-filter "-"

  # Take in the spaces that have come to match since, which a changeorder does not do on its own
  cub changeorder update my-changeorder --refresh-spaces

  # Update tags for changeorders using JSON patch
  echo '{"StartTagID": "new-tag-uuid", "EndTagID": "another-tag-uuid"}' | cub changeorder update --patch --where "Description LIKE 'Release%'" --from-stdin
`+"```"+`
`, ""),
	Args:        cobra.MinimumNArgs(0), // Allow 0 args for bulk mode
	RunE:        changeorderUpdateCmdRun,
	Annotations: map[string]string{"OrgLevel": ""},
}

var (
	changeorderPatch       bool
	changeorderIdentifiers []string
	changeorderUpdateArgs  struct {
		description     string
		spaceFilter     string
		whereSpaceField string
		abortedReason   string
		// abortedReasonSet is whether --aborted-reason was given. The empty value is the one
		// that un-aborts a change order, so "was it passed" is the question rather than "is it
		// non-empty", which is what the other update flags ask.
		abortedReasonSet bool
		refreshSpaces    bool
	}
)

func init() {
	addStandardUpdateFlags(changeorderUpdateCmd)
	changeorderUpdateCmd.Flags().BoolVar(&changeorderPatch, "patch", false, "use patch API for individual or bulk operations")
	enableWhereFlag(changeorderUpdateCmd)
	enableFilterFlag(changeorderUpdateCmd)
	changeorderUpdateCmd.Flags().StringSliceVar(&changeorderIdentifiers, "changeorder", []string{}, "target specific changeorders by slug or UUID for bulk patch (can be repeated or comma-separated)")

	// Single update specific flags
	changeorderUpdateCmd.Flags().StringVar(&changeorderUpdateArgs.description, "description", "", "human-readable description of the change")
	changeorderUpdateCmd.Flags().StringVar(&changeorderUpdateArgs.spaceFilter, "space-filter", "", "filter over Spaces (slug, space/slug, or UUID) selecting the Spaces this change order propagates into, ANDed with its where expression (use '-' to clear)")
	changeorderUpdateCmd.Flags().StringVar(&changeorderUpdateArgs.whereSpaceField, "where-space-field", "", "where expression over Spaces selecting the Spaces this change order propagates into, stored on it as WhereSpace and ANDed with its space filter (use '-' to clear)")
	changeorderUpdateCmd.Flags().StringVar(&changeorderUpdateArgs.abortedReason, "aborted-reason", "", "why the change order was given up on; setting it aborts the change order, and passing an empty value puts it back on its way")
	changeorderUpdateCmd.Flags().BoolVar(&changeorderUpdateArgs.refreshSpaces, "refresh-spaces", false, "re-evaluate the where expression and/or space filter into InScopeSpaceIDs, and re-derive what the change order covers against the spaces they now select, even if neither has changed")

	changeorderCmd.AddCommand(changeorderUpdateCmd)
}

// addChangeOrderWhereSpaceToPatch follows the convention the other where expressions use: "-"
// clears it, since an empty value is how a flag reads when it was not given at all.
func addChangeOrderWhereSpaceToPatch(patchData map[string]interface{}) {
	if changeorderUpdateArgs.whereSpaceField == "-" {
		patchData["WhereSpace"] = ""
	} else if changeorderUpdateArgs.whereSpaceField != "" {
		patchData["WhereSpace"] = changeorderUpdateArgs.whereSpaceField
	}
}

// resolveChangeOrderSpaceFilter resolves --space-filter to a Filter ID. It resolves to no ID when
// the flag was not given and when it was given as "-", which clears the field; the callers tell
// those two apart by the flag value, the same way the where expression does.
func resolveChangeOrderSpaceFilter() (*uuid.UUID, error) {
	if changeorderUpdateArgs.spaceFilter == "" || changeorderUpdateArgs.spaceFilter == "-" {
		return nil, nil
	}
	filterIDStr, err := parseFilterFlag(changeorderUpdateArgs.spaceFilter)
	if err != nil {
		return nil, errors.Wrap(err, "failed to parse space-filter")
	}
	filterID, err := uuid.Parse(filterIDStr)
	if err != nil {
		return nil, errors.Wrap(err, "invalid space-filter")
	}
	return &filterID, nil
}

func addChangeOrderSpaceFilterToPatch(patchData map[string]interface{}, spaceFilterID *uuid.UUID) {
	if changeorderUpdateArgs.spaceFilter == "-" {
		patchData["SpaceFilterID"] = nil
	} else if spaceFilterID != nil {
		patchData["SpaceFilterID"] = spaceFilterID.String()
	}
}

func addChangeOrderAbortedReasonToPatch(patchData map[string]interface{}) {
	if changeorderUpdateArgs.abortedReasonSet {
		patchData["AbortedReason"] = changeorderUpdateArgs.abortedReason
	}
}

func checkChangeOrderUpdateConflictingArgs(args []string) bool {
	// Check for bulk patch mode: no positional args
	isBulkPatchMode := len(args) == 0

	if isBulkPatchMode {
		if !changeorderPatch {
			failOnError(errors.New("--patch is required in bulk mode"))
		}

		// Check for mutual exclusivity between --changeorder and --where flags
		if len(changeorderIdentifiers) > 0 && where != "" {
			failOnError(fmt.Errorf("--changeorder and --where flags are mutually exclusive"))
		}

	} else {
		// Single create mode validation
		if len(args) != 1 {
			failOnError(errors.New("single changeorder update requires exactly one argument: <slug or id>"))
		}

		if filter != "" || where != "" || len(changeorderIdentifiers) > 0 {
			failOnError(fmt.Errorf("--filter, --where, or --changeorder can only be specified with --patch and no positional arguments"))
		}
	}

	if changeorderPatch && flagReplace {
		failOnError(fmt.Errorf("only one of --patch and --replace should be specified"))
	}

	if err := validateSpaceFlag(isBulkPatchMode); err != nil {
		failOnError(err)
	}

	if err := validateStdinFlags(); err != nil {
		failOnError(err)
	}

	// Validate label removal only works with patch
	if err := ValidateLabelRemoval(label, changeorderPatch); err != nil {
		failOnError(err)
	}
	// Validate delete gate removal only works with patch
	if err := ValidateDeleteGateRemoval(deleteGate, changeorderPatch); err != nil {
		failOnError(err)
	}

	return isBulkPatchMode
}

func runBulkChangeOrderUpdate() error {
	// Parse filter parameter
	filterID, err := parseFilterFlag(filter)
	if err != nil {
		return err
	}

	// Build WHERE clause from changeorder identifiers or use provided where clause
	var effectiveWhere string
	if len(changeorderIdentifiers) > 0 {
		whereClause, err := buildWhereClauseFromChangeOrders(changeorderIdentifiers)
		if err != nil {
			return err
		}
		effectiveWhere = whereClause
	} else {
		effectiveWhere = where
	}

	// Add space constraint to the where clause only if not org level
	effectiveWhere = addSpaceIDToWhereClause(effectiveWhere, selectedSpaceID)

	spaceFilterID, err := resolveChangeOrderSpaceFilter()
	if err != nil {
		return err
	}

	// Create enhancer function for changeorder-specific fields
	enhancer := func(patchMap map[string]interface{}) {
		// Add changeorder-specific fields
		if changeorderUpdateArgs.description != "" {
			patchMap["Description"] = changeorderUpdateArgs.description
		}
		addChangeOrderSpaceFilterToPatch(patchMap, spaceFilterID)
		addChangeOrderWhereSpaceToPatch(patchMap)
		addChangeOrderAbortedReasonToPatch(patchMap)
	}

	// Build patch data using consolidated function
	patchJSON, err := BuildPatchData(enhancer)
	if err != nil {
		return err
	}

	// Build bulk patch parameters
	include := "SpaceID"
	params := &goclientnew.BulkPatchChangeOrdersParams{
		Where:   &effectiveWhere,
		Include: &include,
	}
	if filterID != "" {
		params.Filter = &filterID
	}
	if changeorderUpdateArgs.refreshSpaces {
		params.RefreshSpaces = &changeorderUpdateArgs.refreshSpaces
	}

	// Call the bulk patch API
	bulkRes, err := cubClientNew.BulkPatchChangeOrdersWithBodyWithResponse(
		ctx,
		params,
		"application/merge-patch+json",
		bytes.NewReader(patchJSON),
	)
	if err != nil {
		return err
	}

	// Handle the response
	return handleBulkChangeOrderCreateOrUpdateResponse(bulkRes.JSON200, bulkRes.JSON207, bulkRes.StatusCode(), "update", effectiveWhere)
}

func changeorderUpdateCmdRun(cmd *cobra.Command, args []string) error {
	changeorderUpdateArgs.abortedReasonSet = cmd.Flags().Changed("aborted-reason")
	isBulkPatchMode := checkChangeOrderUpdateConflictingArgs(args)

	if isBulkPatchMode {
		return runBulkChangeOrderUpdate()
	}

	// Single changeorder update logic
	if len(args) != 1 {
		return errors.New("single changeorder update requires exactly one argument: <slug or id>")
	}

	currentChangeOrder, err := apiGetChangeOrderFromSlug(args[0], "*") // get all fields for RMW
	if err != nil {
		return err
	}

	spaceID := uuid.MustParse(selectedSpaceID)

	if changeorderPatch {
		// Single changeorder patch mode

		spaceFilterID, err := resolveChangeOrderSpaceFilter()
		if err != nil {
			return err
		}

		// Build patch data using BuildPatchData with changeorder enhancer
		changeorderEnhancer := func(patchData map[string]interface{}) {
			// Add changeorder-specific fields
			if changeorderUpdateArgs.description != "" {
				patchData["Description"] = changeorderUpdateArgs.description
			}
			addChangeOrderSpaceFilterToPatch(patchData, spaceFilterID)
			addChangeOrderWhereSpaceToPatch(patchData)
			addChangeOrderAbortedReasonToPatch(patchData)
		}

		patchData, err := BuildPatchData(changeorderEnhancer)
		if err != nil {
			return fmt.Errorf("failed to build patch data: %w", err)
		}

		changeorderDetails, err := patchChangeOrder(spaceID, currentChangeOrder.ChangeOrderID, patchData)
		if err != nil {
			return err
		}

		displayUpdateResults(changeorderDetails, "changeorder", args[0], changeorderDetails.ChangeOrderID.String(), displayChangeOrderDetails)
		return nil
	}

	// Traditional update mode
	// Handle --from-stdin or --filename with optional --replace
	if flagPopulateModelFromStdin || flagFilename != "" {
		existingChangeOrder := currentChangeOrder
		if flagReplace {
			// Replace mode - create new entity, allow Version to be overwritten
			currentChangeOrder = new(goclientnew.ChangeOrder)
			currentChangeOrder.Version = existingChangeOrder.Version
		}

		if err := populateModelFromFlags(currentChangeOrder); err != nil {
			return err
		}

		// Ensure essential fields can't be clobbered
		currentChangeOrder.OrganizationID = existingChangeOrder.OrganizationID
		currentChangeOrder.SpaceID = existingChangeOrder.SpaceID
		currentChangeOrder.ChangeOrderID = existingChangeOrder.ChangeOrderID
	}
	err = setAnnotations(&currentChangeOrder.Annotations)
	if err != nil {
		return err
	}
	err = setLabels(&currentChangeOrder.Labels)
	if err != nil {
		return err
	}

	// If this was set from stdin, it will be overridden
	currentChangeOrder.SpaceID = spaceID

	// Set changeorder-specific fields from flags
	if changeorderUpdateArgs.description != "" {
		currentChangeOrder.Description = changeorderUpdateArgs.description
	}
	if changeorderUpdateArgs.spaceFilter == "-" {
		currentChangeOrder.SpaceFilterID = nil
	} else if changeorderUpdateArgs.spaceFilter != "" {
		spaceFilterID, err := resolveChangeOrderSpaceFilter()
		if err != nil {
			return err
		}
		currentChangeOrder.SpaceFilterID = spaceFilterID
	}
	if changeorderUpdateArgs.whereSpaceField == "-" {
		currentChangeOrder.WhereSpace = ""
	} else if changeorderUpdateArgs.whereSpaceField != "" {
		currentChangeOrder.WhereSpace = changeorderUpdateArgs.whereSpaceField
	}
	if changeorderUpdateArgs.abortedReasonSet {
		currentChangeOrder.AbortedReason = changeorderUpdateArgs.abortedReason
	}

	updateParams := &goclientnew.UpdateChangeOrderParams{}
	if changeorderUpdateArgs.refreshSpaces {
		updateParams.RefreshSpaces = &changeorderUpdateArgs.refreshSpaces
	}
	changeorderRes, err := cubClientNew.UpdateChangeOrderWithResponse(ctx, spaceID, currentChangeOrder.ChangeOrderID, updateParams, *currentChangeOrder)
	if cubapi.IsAPIError(err, changeorderRes) {
		return cubapi.InterpretErrorGeneric(err, changeorderRes)
	}

	changeorderDetails := changeorderRes.JSON200
	displayUpdateResults(changeorderDetails, "changeorder", args[0], changeorderDetails.ChangeOrderID.String(), displayChangeOrderDetails)
	return nil
}

func handleBulkChangeOrderCreateOrUpdateResponse(responses200 *[]goclientnew.ChangeOrderCreateOrUpdateResponse, responses207 *[]goclientnew.ChangeOrderCreateOrUpdateResponse, statusCode int, operationName, contextInfo string) error {
	return displayBulkGenericCreateOrUpdateResults(
		responses200, responses207, statusCode, "changeorder", operationName, contextInfo,
		func(r *goclientnew.ChangeOrderCreateOrUpdateResponse) *goclientnew.ResponseError { return r.Error },
		func(r *goclientnew.ChangeOrderCreateOrUpdateResponse) string {
			if r.ChangeOrder != nil {
				return fmt.Sprintf("%s (ID: %s)", r.ChangeOrder.Slug, r.ChangeOrder.ChangeOrderID)
			}
			return ""
		},
	)
}

func patchChangeOrder(spaceID uuid.UUID, changeorderID uuid.UUID, patchData []byte) (*goclientnew.ChangeOrder, error) {
	patchParams := &goclientnew.PatchChangeOrderParams{}
	if changeorderUpdateArgs.refreshSpaces {
		patchParams.RefreshSpaces = &changeorderUpdateArgs.refreshSpaces
	}
	changeorderRes, err := cubClientNew.PatchChangeOrderWithBodyWithResponse(
		ctx,
		spaceID,
		changeorderID,
		patchParams,
		"application/merge-patch+json",
		bytes.NewReader(patchData),
	)
	if cubapi.IsAPIError(err, changeorderRes) {
		return nil, cubapi.InterpretErrorGeneric(err, changeorderRes)
	}

	return changeorderRes.JSON200, nil
}
