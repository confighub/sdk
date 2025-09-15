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

var viewUpdateCmd = &cobra.Command{
	Use:   "update [<slug or id>] [options...]",
	Short: "Update a view or multiple views",
	Long: `Update a view or multiple views using bulk operations.

Single view update:
  cub view update my-view --column Unit.Slug --column Unit.Status --order-by Unit.CreatedAt --order-by-direction DESC

Bulk update with --patch:
Update multiple views at once based on search criteria. Requires --patch flag with no positional arguments.

Examples:
  # Update columns for all views matching a pattern
  cub view update --patch --where "FilterID IS NOT NULL" --column Unit.Slug --column Unit.DisplayName

  # Update ordering for specific views
  cub view update --patch --view view1,view2 --order-by Unit.UpdatedAt --order-by-direction ASC

  # Update views using JSON patch
  echo '{"OrderByDirection": "DESC"}' | cub view update --patch --where "OrderBy IS NOT NULL" --from-stdin

  # Clear ordering from views
  echo '{"OrderBy": "", "OrderByDirection": ""}' | cub view update --patch --where "OrderBy IS NOT NULL" --from-stdin`,
	Args:        cobra.MinimumNArgs(0), // Allow 0 args for bulk mode
	RunE:        viewUpdateCmdRun,
	Annotations: map[string]string{"OrgLevel": ""},
}

var (
	viewPatch       bool
	viewIdentifiers []string
	viewUpdateArgs  struct {
		filter           string
		columns          []string
		groupBy          string
		orderBy          string
		orderByDirection string
	}
)

func init() {
	addStandardUpdateFlags(viewUpdateCmd)
	viewUpdateCmd.Flags().BoolVar(&viewPatch, "patch", false, "use patch API for individual or bulk operations")
	enableWhereFlag(viewUpdateCmd)
	enableFilterFlag(viewUpdateCmd)
	viewUpdateCmd.Flags().StringSliceVar(&viewIdentifiers, "view", []string{}, "target specific views by slug or UUID for bulk patch (can be repeated or comma-separated)")

	// Single update specific flags
	viewUpdateCmd.Flags().StringVar(&viewUpdateArgs.filter, "filter-field", "", "filter to identify entities to include in the view (slug or UUID)")
	viewUpdateCmd.Flags().StringSliceVar(&viewUpdateArgs.columns, "column", []string{}, "column names to display in the view (can be repeated or comma-separated)")
	viewUpdateCmd.Flags().StringVar(&viewUpdateArgs.groupBy, "group-by", "", "column name to group by")
	viewUpdateCmd.Flags().StringVar(&viewUpdateArgs.orderBy, "order-by", "", "column name to sort by")
	viewUpdateCmd.Flags().StringVar(&viewUpdateArgs.orderByDirection, "order-by-direction", "", "sort direction (ASC or DESC, only valid with --order-by)")

	viewCmd.AddCommand(viewUpdateCmd)
}

func checkViewConflictingArgs(args []string) bool {
	// Check for bulk patch mode (no positional args)
	isBulkPatchMode := len(args) == 0

	if isBulkPatchMode {
		if !viewPatch {
			failOnError(errors.New("--patch is required in bulk mode"))
		}

		// Check for mutual exclusivity between --view and --where flags
		if len(viewIdentifiers) > 0 && where != "" {
			failOnError(fmt.Errorf("--view and --where flags are mutually exclusive"))
		}

	} else {
		// Single create mode validation
		if len(args) != 1 {
			failOnError(errors.New("single view update requires exactly one argument: <slug or id>"))
		}

		if filter != "" || where != "" || len(viewIdentifiers) > 0 {
			failOnError(fmt.Errorf("--filter, --where, or --view can only be specified with --patch and no positional arguments"))
		}
	}

	if viewPatch && flagReplace {
		failOnError(fmt.Errorf("only one of --patch and --replace should be specified"))
	}

	// Validate order-by-direction is only used with order-by
	if viewUpdateArgs.orderByDirection != "" && viewUpdateArgs.orderBy == "" {
		failOnError(errors.New("--order-by-direction can only be specified with --order-by"))
	}

	// Validate order-by-direction values
	if viewUpdateArgs.orderByDirection != "" && viewUpdateArgs.orderByDirection != "ASC" && viewUpdateArgs.orderByDirection != "DESC" {
		failOnError(errors.New("--order-by-direction must be ASC or DESC"))
	}

	if err := validateSpaceFlag(isBulkPatchMode); err != nil {
		failOnError(err)
	}

	if err := validateStdinFlags(); err != nil {
		failOnError(err)
	}

	// Validate label removal only works with patch
	if err := ValidateLabelRemoval(label, viewPatch); err != nil {
		failOnError(err)
	}
	// Validate delete gate removal only works with patch
	if err := ValidateDeleteGateRemoval(deleteGate, viewPatch); err != nil {
		failOnError(err)
	}

	return isBulkPatchMode
}

func runBulkViewUpdate() error {
	// Parse filter parameter
	filterID, err := parseFilterFlag(filter)
	if err != nil {
		return err
	}

	// Build WHERE clause from view identifiers or use provided where clause
	var effectiveWhere string
	if len(viewIdentifiers) > 0 {
		whereClause, err := buildWhereClauseFromViews(viewIdentifiers)
		if err != nil {
			return err
		}
		effectiveWhere = whereClause
	} else {
		effectiveWhere = where
	}

	// Add space constraint to the where clause only if not org level
	effectiveWhere = addSpaceIDToWhereClause(effectiveWhere, selectedSpaceID)

	// Handle view-specific field parsing that can fail
	var filterField string
	if viewUpdateArgs.filter != "" {
		var err error
		filterField, err = parseFilterFlag(viewUpdateArgs.filter)
		if err != nil {
			return err
		}
	}

	// Build patch data using BuildPatchData with view enhancer
	viewEnhancer := func(patchData map[string]interface{}) {
		// Add view-specific fields
		if filterField != "" {
			patchData["FilterID"] = filterField
		}

		if len(viewUpdateArgs.columns) > 0 {
			columns := make([]map[string]interface{}, 0, len(viewUpdateArgs.columns))
			for _, columnName := range viewUpdateArgs.columns {
				columns = append(columns, map[string]interface{}{
					"Name": columnName,
				})
			}
			patchData["Columns"] = columns
		}

		if viewUpdateArgs.groupBy != "" {
			patchData["GroupBy"] = viewUpdateArgs.groupBy
		}

		if viewUpdateArgs.orderBy != "" {
			patchData["OrderBy"] = viewUpdateArgs.orderBy
		}

		if viewUpdateArgs.orderByDirection != "" {
			patchData["OrderByDirection"] = viewUpdateArgs.orderByDirection
		}
	}

	patchJSON, err := BuildPatchData(viewEnhancer)
	if err != nil {
		return err
	}

	// Build bulk patch parameters
	include := "SpaceID"
	params := &goclientnew.BulkPatchViewsParams{
		Where:   &effectiveWhere,
		Include: &include,
	}
	if filterID != "" {
		params.Filter = &filterID
	}

	// Call the bulk patch API
	bulkRes, err := cubClientNew.BulkPatchViewsWithBodyWithResponse(
		ctx,
		params,
		"application/merge-patch+json",
		bytes.NewReader(patchJSON),
	)
	if err != nil {
		return err
	}

	// Handle the response
	return handleBulkViewCreateOrUpdateResponse(bulkRes.JSON200, bulkRes.JSON207, bulkRes.StatusCode(), "update", effectiveWhere)
}

func viewUpdateCmdRun(cmd *cobra.Command, args []string) error {
	isBulkPatchMode := checkViewConflictingArgs(args)

	if isBulkPatchMode {
		return runBulkViewUpdate()
	}

	// Single view update logic
	if len(args) != 1 {
		return errors.New("single view update requires exactly one argument: <slug or id>")
	}

	currentView, err := apiGetViewFromSlug(args[0], "*") // get all fields for RMW
	if err != nil {
		return err
	}

	spaceID := uuid.MustParse(selectedSpaceID)

	if viewPatch {
		// Single view patch mode

		// Add labels if specified
		if len(label) > 0 {
			err := setLabels(&currentView.Labels)
			if err != nil {
				return err
			}
		}

		// Handle view-specific field parsing that can fail
		var filterID *uuid.UUID
		if viewUpdateArgs.filter != "" {
			filter, err := apiGetFilterFromSlug(viewUpdateArgs.filter, "FilterID", selectedSpaceID)
			if err != nil {
				return err
			}
			filterID = &filter.FilterID
		}

		// Build patch data using BuildPatchData with view enhancer
		viewEnhancer := func(patchData map[string]interface{}) {
			// Add view-specific fields from flags
			if filterID != nil {
				patchData["FilterID"] = *filterID
			}

			if len(viewUpdateArgs.columns) > 0 {
				columns := make([]map[string]interface{}, 0, len(viewUpdateArgs.columns))
				for _, columnName := range viewUpdateArgs.columns {
					columns = append(columns, map[string]interface{}{
						"Name": columnName,
					})
				}
				patchData["Columns"] = columns
			}

			if viewUpdateArgs.groupBy != "" {
				patchData["GroupBy"] = viewUpdateArgs.groupBy
			}

			if viewUpdateArgs.orderBy != "" {
				patchData["OrderBy"] = viewUpdateArgs.orderBy
			}

			if viewUpdateArgs.orderByDirection != "" {
				patchData["OrderByDirection"] = viewUpdateArgs.orderByDirection
			}
		}

		patchData, err := BuildPatchData(viewEnhancer)
		if err != nil {
			return fmt.Errorf("failed to build patch data: %w", err)
		}

		viewDetails, err := patchView(spaceID, currentView.ViewID, patchData)
		if err != nil {
			return err
		}

		displayUpdateResults(viewDetails, "view", args[0], viewDetails.ViewID.String(), displayViewDetails)
		return nil
	}

	// Traditional update mode
	// Handle --from-stdin or --filename with optional --replace
	if flagPopulateModelFromStdin || flagFilename != "" {
		existingView := currentView
		if flagReplace {
			// Replace mode - create new entity, allow Version to be overwritten
			currentView = new(goclientnew.View)
			currentView.Version = existingView.Version
		}

		if err := populateModelFromFlags(currentView); err != nil {
			return err
		}

		// Ensure essential fields can't be clobbered
		currentView.OrganizationID = existingView.OrganizationID
		currentView.SpaceID = existingView.SpaceID
		currentView.ViewID = existingView.ViewID
	}
	err = setLabels(&currentView.Labels)
	if err != nil {
		return err
	}

	// If this was set from stdin, it will be overridden
	currentView.SpaceID = spaceID

	// Set view-specific fields from flags
	if viewUpdateArgs.filter != "" {
		filter, err := apiGetFilterFromSlug(viewUpdateArgs.filter, "FilterID", selectedSpaceID)
		if err != nil {
			return err
		}
		currentView.FilterID = filter.FilterID
	}

	if len(viewUpdateArgs.columns) > 0 {
		columns := make([]goclientnew.Column, 0, len(viewUpdateArgs.columns))
		for _, columnName := range viewUpdateArgs.columns {
			columns = append(columns, goclientnew.Column{
				Name: columnName,
			})
		}
		currentView.Columns = columns
	}

	if viewUpdateArgs.groupBy != "" {
		currentView.GroupBy = viewUpdateArgs.groupBy
	}

	if viewUpdateArgs.orderBy != "" {
		currentView.OrderBy = viewUpdateArgs.orderBy
	}

	if viewUpdateArgs.orderByDirection != "" {
		currentView.OrderByDirection = viewUpdateArgs.orderByDirection
	}

	viewRes, err := cubClientNew.UpdateViewWithResponse(ctx, spaceID, currentView.ViewID, *currentView)
	if cubapi.IsAPIError(err, viewRes) {
		return cubapi.InterpretErrorGeneric(err, viewRes)
	}

	viewDetails := viewRes.JSON200
	displayUpdateResults(viewDetails, "view", args[0], viewDetails.ViewID.String(), displayViewDetails)
	return nil
}

func handleBulkViewCreateOrUpdateResponse(responses200 *[]goclientnew.ViewCreateOrUpdateResponse, responses207 *[]goclientnew.ViewCreateOrUpdateResponse, statusCode int, operationName, contextInfo string) error {
	return displayBulkGenericCreateOrUpdateResults(
		responses200, responses207, statusCode, "view", operationName, contextInfo,
		func(r *goclientnew.ViewCreateOrUpdateResponse) *goclientnew.ResponseError { return r.Error },
		func(r *goclientnew.ViewCreateOrUpdateResponse) string {
			if r.View != nil {
				return fmt.Sprintf("%s (ID: %s)", r.View.Slug, r.View.ViewID)
			}
			return ""
		},
	)
}

func patchView(spaceID uuid.UUID, viewID uuid.UUID, patchData []byte) (*goclientnew.View, error) {
	viewRes, err := cubClientNew.PatchViewWithBodyWithResponse(
		ctx,
		spaceID,
		viewID,
		"application/merge-patch+json",
		bytes.NewReader(patchData),
	)
	if cubapi.IsAPIError(err, viewRes) {
		return nil, cubapi.InterpretErrorGeneric(err, viewRes)
	}

	return viewRes.JSON200, nil
}
