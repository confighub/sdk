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

	// Create patch data
	patchData := make(map[string]interface{})

	// Add view-specific fields
	if viewUpdateArgs.filter != "" {
		filterField, err := parseFilterFlag(viewUpdateArgs.filter)
		if err != nil {
			return err
		}
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
		// Single view patch mode - we'll apply changes directly to the view object
		// Handle --from-stdin or --filename
		if flagPopulateModelFromStdin || flagFilename != "" {
			existingView := currentView
			if err := populateModelFromFlags(currentView); err != nil {
				return err
			}
			// Ensure essential fields can't be clobbered
			currentView.OrganizationID = existingView.OrganizationID
			currentView.SpaceID = existingView.SpaceID
			currentView.ViewID = existingView.ViewID
		}

		// Add labels if specified
		if len(label) > 0 {
			err := setLabels(&currentView.Labels)
			if err != nil {
				return err
			}
		}

		// Add view details from flags
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

		// Convert view to patch data
		patchData, err := json.Marshal(currentView)
		if err != nil {
			return fmt.Errorf("failed to marshal patch data: %w", err)
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
	if IsAPIError(err, viewRes) {
		return InterpretErrorGeneric(err, viewRes)
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
	if IsAPIError(err, viewRes) {
		return nil, InterpretErrorGeneric(err, viewRes)
	}

	return viewRes.JSON200, nil
}
