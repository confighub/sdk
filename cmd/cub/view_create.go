// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"bytes"
	"strings"

	"github.com/cockroachdb/errors"
	goclientnew "github.com/confighub/sdk/openapi/goclient-new"
	"github.com/google/uuid"
	"github.com/spf13/cobra"
)

var viewCreateCmd = &cobra.Command{
	Use:         "create [<slug> <filter> [--column <column>...] [--group-by <column>] [--order-by <column>] [--order-by-direction <ASC|DESC>]]",
	Short:       "Create a new view or bulk create views",
	Long:        getViewCreateHelp(),
	Args:        cobra.MinimumNArgs(0), // Allow 0 args for bulk mode
	RunE:        viewCreateCmdRun,
	Annotations: map[string]string{"OrgLevel": ""},
}

func getViewCreateHelp() string {
	baseHelp := `Create a new view or bulk create multiple views by cloning existing ones.

SINGLE VIEW CREATION:
Create a new view to define an entity view specification.

Examples:
  # Create a view with a filter and specific columns
  cub view create --space my-space unit-view unit-filter --column Unit.Slug --column Unit.DisplayName --column Space.Slug

  # Create a view with grouping and ordering
  cub view create --space my-space summary-view deployment-filter --column Unit.Labels.Environment --column Unit.Status --group-by Unit.Labels.Environment --order-by Unit.CreatedAt --order-by-direction DESC

  # Create a view with custom columns and sorting
  cub view create --space my-space detailed-view my-filter --column Unit.Slug --column Unit.HeadRevisionNum --column UpstreamUnit.HeadRevisionNum --order-by Unit.UpdatedAt

  # Create a view from JSON
  cub view create --space my-space --json my-view --from-stdin < view.json

BULK VIEW CREATION:
When no positional arguments are provided, bulk create mode is activated. This mode clones existing
views based on filters and creates multiple new views with optional modifications.

Bulk Create Examples:
  # Clone all views matching a pattern with name prefixes
  cub view create --where "FilterID IS NOT NULL" --name-prefix backup- --dest-space backup-space

  # Clone specific views to multiple spaces
  cub view create --view my-view --dest-space dev-space,staging-space

  # Clone views using a where expression for destination spaces
  cub view create --where "GroupBy IS NOT NULL" --where-space "Labels.Environment IN ('dev', 'staging')"

  # Clone views with modifications via JSON patch
  echo '{"OrderByDirection": "DESC"}' | cub view create --where "OrderBy IS NOT NULL" --name-prefix sorted- --from-stdin

Column Names:
Columns should be specified in the format used by list commands, such as:
  - Unit.Slug, Unit.DisplayName, Unit.Status
  - Space.Slug, Space.DisplayName
  - Labels.Environment, Labels.Version
  - UpstreamUnit.HeadRevisionNum, UpstreamUnit.Slug
  - Target.Slug, Target.ToolchainType`

	return baseHelp
}

var viewCreateArgs struct {
	destSpaces       []string
	whereSpace       string
	namePrefixes     []string
	viewSlugs        []string
	filter           string
	columns          []string
	groupBy          string
	orderBy          string
	orderByDirection string
}

func init() {
	addStandardCreateFlags(viewCreateCmd)
	enableWhereFlag(viewCreateCmd)

	// Single create specific flags
	viewCreateCmd.Flags().StringVar(&viewCreateArgs.filter, "filter", "", "filter to identify entities to include in the view (slug or UUID, required for single create)")
	viewCreateCmd.Flags().StringSliceVar(&viewCreateArgs.columns, "column", []string{}, "column names to display in the view (can be repeated or comma-separated)")
	viewCreateCmd.Flags().StringVar(&viewCreateArgs.groupBy, "group-by", "", "column name to group by")
	viewCreateCmd.Flags().StringVar(&viewCreateArgs.orderBy, "order-by", "", "column name to sort by")
	viewCreateCmd.Flags().StringVar(&viewCreateArgs.orderByDirection, "order-by-direction", "", "sort direction (ASC or DESC, only valid with --order-by)")

	// Bulk create specific flags
	viewCreateCmd.Flags().StringSliceVar(&viewCreateArgs.destSpaces, "dest-space", []string{}, "destination spaces for bulk create (can be repeated or comma-separated)")
	viewCreateCmd.Flags().StringVar(&viewCreateArgs.whereSpace, "where-space", "", "where expression to select destination spaces for bulk create")
	viewCreateCmd.Flags().StringSliceVar(&viewCreateArgs.namePrefixes, "name-prefix", []string{}, "name prefixes for bulk create (can be repeated or comma-separated)")
	viewCreateCmd.Flags().StringSliceVar(&viewCreateArgs.viewSlugs, "view", []string{}, "target specific views by slug or UUID for bulk create (can be repeated or comma-separated)")

	viewCmd.AddCommand(viewCreateCmd)
}

func checkViewCreateConflictingArgs(args []string) (bool, error) {
	// Determine if bulk create mode: no positional args and has bulk-specific flags
	isBulkCreateMode := len(args) == 0 && (where != "" || len(viewCreateArgs.viewSlugs) > 0 || len(viewCreateArgs.destSpaces) > 0 || viewCreateArgs.whereSpace != "" || len(viewCreateArgs.namePrefixes) > 0)

	if isBulkCreateMode {
		// Validate bulk create requirements
		if where == "" && len(viewCreateArgs.viewSlugs) == 0 {
			return false, errors.New("bulk create mode requires --where or --view flags")
		}

		if len(viewCreateArgs.viewSlugs) > 0 && where != "" {
			return false, errors.New("--view and --where flags are mutually exclusive")
		}

		if len(viewCreateArgs.destSpaces) > 0 && viewCreateArgs.whereSpace != "" {
			return false, errors.New("--dest-space and --where-space flags are mutually exclusive")
		}

		if len(viewCreateArgs.destSpaces) == 0 && viewCreateArgs.whereSpace == "" && len(viewCreateArgs.namePrefixes) == 0 {
			return false, errors.New("bulk create mode requires at least one of --dest-space, --where-space, or --name-prefix")
		}
	} else {
		// Single create mode validation
		if len(args) < 2 {
			return false, errors.New("single view creation requires: <slug> <filter>")
		}

		if where != "" || len(viewCreateArgs.viewSlugs) > 0 || len(viewCreateArgs.destSpaces) > 0 || viewCreateArgs.whereSpace != "" || len(viewCreateArgs.namePrefixes) > 0 {
			return false, errors.New("bulk create flags (--where, --view, --dest-space, --where-space, --name-prefix) can only be used without positional arguments")
		}

		// Validate order-by-direction is only used with order-by
		if viewCreateArgs.orderByDirection != "" && viewCreateArgs.orderBy == "" {
			return false, errors.New("--order-by-direction can only be specified with --order-by")
		}

		// Validate order-by-direction values
		if viewCreateArgs.orderByDirection != "" && viewCreateArgs.orderByDirection != "ASC" && viewCreateArgs.orderByDirection != "DESC" {
			return false, errors.New("--order-by-direction must be ASC or DESC")
		}
	}

	if err := validateSpaceFlag(isBulkCreateMode); err != nil {
		failOnError(err)
	}

	if err := validateStdinFlags(); err != nil {
		return isBulkCreateMode, err
	}

	return isBulkCreateMode, nil
}

func viewCreateCmdRun(cmd *cobra.Command, args []string) error {
	isBulkCreateMode, err := checkViewCreateConflictingArgs(args)
	if err != nil {
		return err
	}

	if isBulkCreateMode {
		return runBulkViewCreate()
	}

	return runSingleViewCreate(args)
}

func runSingleViewCreate(args []string) error {
	spaceID := uuid.MustParse(selectedSpaceID)
	newBody := goclientnew.View{}
	if flagPopulateModelFromStdin || flagFilename != "" {
		if err := populateModelFromFlags(&newBody); err != nil {
			return err
		}
	}
	err := setLabels(&newBody.Labels)
	if err != nil {
		return err
	}
	newBody.SpaceID = spaceID
	newBody.Slug = makeSlug(args[0])
	if newBody.DisplayName == "" {
		newBody.DisplayName = args[0]
	}

	// Set filter reference (required)
	filterSlug := args[1]
	if viewCreateArgs.filter != "" {
		filterSlug = viewCreateArgs.filter
	}
	filter, err := apiGetFilterFromSlug(filterSlug, "FilterID")
	if err != nil {
		return err
	}
	newBody.FilterID = filter.FilterID

	// Set columns if provided
	if len(viewCreateArgs.columns) > 0 {
		columns := make([]goclientnew.Column, 0, len(viewCreateArgs.columns))
		for _, columnName := range viewCreateArgs.columns {
			columns = append(columns, goclientnew.Column{
				Name: columnName,
			})
		}
		newBody.Columns = columns
	}

	// Set grouping and ordering if provided
	if viewCreateArgs.groupBy != "" {
		newBody.GroupBy = viewCreateArgs.groupBy
	}

	if viewCreateArgs.orderBy != "" {
		newBody.OrderBy = viewCreateArgs.orderBy
	}

	if viewCreateArgs.orderByDirection != "" {
		newBody.OrderByDirection = viewCreateArgs.orderByDirection
	}

	viewRes, err := cubClientNew.CreateViewWithResponse(ctx, spaceID, newBody)
	if IsAPIError(err, viewRes) {
		return InterpretErrorGeneric(err, viewRes)
	}

	viewDetails := viewRes.JSON200
	displayCreateResults(viewDetails, "view", args[0], viewDetails.ViewID.String(), displayViewDetails)
	return nil
}

func runBulkViewCreate() error {
	// Build WHERE clause from view identifiers or use provided where clause
	var effectiveWhere string
	if len(viewCreateArgs.viewSlugs) > 0 {
		whereClause, err := buildWhereClauseFromViews(viewCreateArgs.viewSlugs)
		if err != nil {
			return err
		}
		effectiveWhere = whereClause
	} else {
		effectiveWhere = where
	}

	// Add space constraint to the where clause only if not org level
	effectiveWhere = addSpaceIDToWhereClause(effectiveWhere, selectedSpaceID)

	// Build patch data using consolidated function (no entity-specific fields for view)
	patchJSON, err := BuildPatchData(nil)
	if err != nil {
		return err
	}

	// Build bulk create parameters
	include := "SpaceID"
	params := &goclientnew.BulkCreateViewsParams{
		Where:   &effectiveWhere,
		Include: &include,
	}

	// Add name prefixes if specified
	if len(viewCreateArgs.namePrefixes) > 0 {
		namePrefixesStr := strings.Join(viewCreateArgs.namePrefixes, ",")
		params.NamePrefixes = &namePrefixesStr
	}

	// Set where_space parameter - either from direct where-space flag or converted from dest-space
	var whereSpaceExpr string
	if viewCreateArgs.whereSpace != "" {
		whereSpaceExpr = viewCreateArgs.whereSpace
	} else if len(viewCreateArgs.destSpaces) > 0 {
		// Convert dest-space identifiers to a where expression
		whereSpaceExpr, err = buildWhereClauseForSpaces(viewCreateArgs.destSpaces)
		if err != nil {
			return errors.Wrapf(err, "error converting destination spaces to where expression")
		}
	}

	if whereSpaceExpr != "" {
		params.WhereSpace = &whereSpaceExpr
	}

	// Call the bulk create API
	bulkRes, err := cubClientNew.BulkCreateViewsWithBodyWithResponse(
		ctx,
		params,
		"application/merge-patch+json",
		bytes.NewReader(patchJSON),
	)
	if err != nil {
		return err
	}

	// Handle the response
	return handleBulkViewCreateOrUpdateResponse(bulkRes.JSON200, bulkRes.JSON207, bulkRes.StatusCode(), "create", effectiveWhere)
}
