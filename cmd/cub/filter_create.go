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

var filterCreateCmd = &cobra.Command{
	Use:         "create [<slug> <from> [--where <where>] [--where-data <where-data>] [--resource-type <resource-type>] [--from-space <from-space>]]",
	Short:       "Create a new filter or bulk create filters",
	Long:        getFilterCreateHelp(),
	Args:        cobra.MinimumNArgs(0), // Allow 0 args for bulk mode
	RunE:        filterCreateCmdRun,
	Annotations: map[string]string{"OrgLevel": ""},
}

func getFilterCreateHelp() string {
	baseHelp := `Create a new filter or bulk create multiple filters by cloning existing ones.

SINGLE FILTER CREATION:
Create a new filter to define entity filter expressions.

From Types:
  - Unit: Filter units
  - Space: Filter spaces
  - Trigger: Filter triggers
  - Worker: Filter workers
  - Target: Filter targets

Examples:
  # Create a filter for Units with specific labels
  cub filter create --space my-space --json unit-filter Unit --where "Labels.Environment = 'production'"

  # Create a filter for Units with specific resource type
  cub filter create --space my-space --json deployment-filter Unit --resource-type "apps/v1/Deployment"

  # Create a filter for Units with data filters
  cub filter create --space my-space --json replicas-filter Unit --where-data "spec.replicas > 2"

  # Create a filter for Spaces with specific criteria
  cub filter create --space my-space --json dev-spaces Space --where "Labels.Environment = 'dev'"

  # Create a filter with from-space for filtering within a specific space
  cub filter create --space my-space --json cross-space-filter Unit --from-space other-space --where "DisplayName LIKE 'app-%'"

BULK FILTER CREATION:
When no positional arguments are provided, bulk create mode is activated. This mode clones existing
filters based on filters and creates multiple new filters with optional modifications.

Bulk Create Examples:
  # Clone all filters matching a pattern with name prefixes
  cub filter create --where "From = 'Unit'" --name-prefix dev-,staging- --dest-space dev-space

  # Clone specific filters to multiple spaces
  cub filter create --filter my-filter --dest-space dev-space,staging-space

  # Clone filters using a where expression for destination spaces
  cub filter create --where "From = 'Space'" --where-space "Labels.Environment IN ('dev', 'staging')"

  # Clone filters with modifications via JSON patch
  echo '{"From": "Unit"}' | cub filter create --where "From = 'Space'" --name-prefix unit- --from-stdin`

	return baseHelp
}

var filterCreateArgs struct {
	destSpaces   []string
	whereSpace   string
	namePrefixes []string
	filterSlugs  []string
	whereData    string
	resourceType string
	fromSpace    string
}

func init() {
	addStandardCreateFlags(filterCreateCmd)
	enableWhereFlag(filterCreateCmd)

	// Single create specific flags
	filterCreateCmd.Flags().StringVar(&filterCreateArgs.whereData, "where-data", "", "where filter expression for configuration data (valid only for Units)")
	filterCreateCmd.Flags().StringVar(&filterCreateArgs.resourceType, "resource-type", "", "resource type to match (e.g., apps/v1/Deployment, valid only for Units)")
	filterCreateCmd.Flags().StringVar(&filterCreateArgs.fromSpace, "from-space", "", "space to filter within (slug or UUID, only relevant for spaced entity types)")

	// Bulk create specific flags
	filterCreateCmd.Flags().StringSliceVar(&filterCreateArgs.destSpaces, "dest-space", []string{}, "destination spaces for bulk create (can be repeated or comma-separated)")
	filterCreateCmd.Flags().StringVar(&filterCreateArgs.whereSpace, "where-space", "", "where expression to select destination spaces for bulk create")
	filterCreateCmd.Flags().StringSliceVar(&filterCreateArgs.namePrefixes, "name-prefix", []string{}, "name prefixes for bulk create (can be repeated or comma-separated)")
	filterCreateCmd.Flags().StringSliceVar(&filterCreateArgs.filterSlugs, "filter", []string{}, "target specific filters by slug or UUID for bulk create (can be repeated or comma-separated)")

	filterCmd.AddCommand(filterCreateCmd)
}

func checkFilterCreateConflictingArgs(args []string) (bool, error) {
	// Determine if bulk create mode: no positional args and has bulk-specific flags
	isBulkCreateMode := len(args) == 0 && (len(filterCreateArgs.filterSlugs) > 0 || len(filterCreateArgs.destSpaces) > 0 || filterCreateArgs.whereSpace != "" || len(filterCreateArgs.namePrefixes) > 0)

	if isBulkCreateMode {
		// Validate bulk create requirements
		if where == "" && len(filterCreateArgs.filterSlugs) == 0 {
			return false, errors.New("bulk create mode requires --where or --filter flags")
		}

		if len(filterCreateArgs.filterSlugs) > 0 && where != "" {
			return false, errors.New("--filter and --where flags are mutually exclusive")
		}

		if len(filterCreateArgs.destSpaces) > 0 && filterCreateArgs.whereSpace != "" {
			return false, errors.New("--dest-space and --where-space flags are mutually exclusive")
		}

		if len(filterCreateArgs.destSpaces) == 0 && filterCreateArgs.whereSpace == "" && len(filterCreateArgs.namePrefixes) == 0 {
			return false, errors.New("bulk create mode requires at least one of --dest-space, --where-space, or --name-prefix")
		}
	} else {
		// Single create mode validation
		if len(args) < 2 {
			return false, errors.New("single filter creation requires: <slug> <from> [options...]")
		}

		if len(filterCreateArgs.filterSlugs) > 0 || len(filterCreateArgs.destSpaces) > 0 || filterCreateArgs.whereSpace != "" || len(filterCreateArgs.namePrefixes) > 0 {
			return false, errors.New("bulk create flags (--filter, --dest-space, --where-space, --name-prefix) can only be used without positional arguments")
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

func filterCreateCmdRun(cmd *cobra.Command, args []string) error {
	isBulkCreateMode, err := checkFilterCreateConflictingArgs(args)
	if err != nil {
		return err
	}

	if isBulkCreateMode {
		return runBulkFilterCreate()
	}

	return runSingleFilterCreate(args)
}

func runSingleFilterCreate(args []string) error {
	spaceID := uuid.MustParse(selectedSpaceID)
	newBody := goclientnew.Filter{}
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

	// Set From field
	newBody.From = args[1]

	// Set optional fields from flags
	if where != "" {
		newBody.Where = where
	}
	if filterCreateArgs.whereData != "" {
		newBody.WhereData = filterCreateArgs.whereData
	}
	if filterCreateArgs.resourceType != "" {
		newBody.ResourceType = filterCreateArgs.resourceType
	}
	if filterCreateArgs.fromSpace != "" {
		fromSpace, err := apiGetSpaceFromSlug(filterCreateArgs.fromSpace, "SpaceID")
		if err != nil {
			return err
		}
		newBody.FromSpaceID = &fromSpace.SpaceID
	}

	filterRes, err := cubClientNew.CreateFilterWithResponse(ctx, spaceID, newBody)
	if IsAPIError(err, filterRes) {
		return InterpretErrorGeneric(err, filterRes)
	}

	filterDetails := filterRes.JSON200
	displayCreateResults(filterDetails, "filter", args[0], filterDetails.FilterID.String(), displayFilterDetails)
	return nil
}

func runBulkFilterCreate() error {
	// Build WHERE clause from filter identifiers or use provided where clause
	var effectiveWhere string
	if len(filterCreateArgs.filterSlugs) > 0 {
		whereClause, err := buildWhereClauseFromFilters(filterCreateArgs.filterSlugs)
		if err != nil {
			return err
		}
		effectiveWhere = whereClause
	} else {
		effectiveWhere = where
	}

	// Add space constraint to the where clause only if not org level
	effectiveWhere = addSpaceIDToWhereClause(effectiveWhere, selectedSpaceID)

	// Build patch data using consolidated function (no entity-specific fields for filter)
	patchJSON, err := BuildPatchData(nil)
	if err != nil {
		return err
	}

	// Build bulk create parameters
	include := "SpaceID"
	params := &goclientnew.BulkCreateFiltersParams{
		Where:   &effectiveWhere,
		Include: &include,
	}

	// Add name prefixes if specified
	if len(filterCreateArgs.namePrefixes) > 0 {
		namePrefixesStr := strings.Join(filterCreateArgs.namePrefixes, ",")
		params.NamePrefixes = &namePrefixesStr
	}

	// Set where_space parameter - either from direct where-space flag or converted from dest-space
	var whereSpaceExpr string
	if filterCreateArgs.whereSpace != "" {
		whereSpaceExpr = filterCreateArgs.whereSpace
	} else if len(filterCreateArgs.destSpaces) > 0 {
		// Convert dest-space identifiers to a where expression
		whereSpaceExpr, err = buildWhereClauseForSpaces(filterCreateArgs.destSpaces)
		if err != nil {
			return errors.Wrapf(err, "error converting destination spaces to where expression")
		}
	}

	if whereSpaceExpr != "" {
		params.WhereSpace = &whereSpaceExpr
	}

	// Call the bulk create API
	bulkRes, err := cubClientNew.BulkCreateFiltersWithBodyWithResponse(
		ctx,
		params,
		"application/merge-patch+json",
		bytes.NewReader(patchJSON),
	)
	if err != nil {
		return err
	}

	// Handle the response
	return handleBulkFilterCreateOrUpdateResponse(bulkRes.JSON200, bulkRes.JSON207, bulkRes.StatusCode(), "create", effectiveWhere)
}
