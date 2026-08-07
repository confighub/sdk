// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"bytes"
	"strings"

	"github.com/cockroachdb/errors"
	"github.com/confighub/sdk/core/cubapi"
	goclientnew "github.com/confighub/sdk/core/openapi/goclient-new"
	"github.com/google/uuid"
	"github.com/spf13/cobra"
)

var changeorderCreateCmd = &cobra.Command{
	Use:         "create [<slug> [--description <description>]]",
	Short:       "Create a new changeorder or bulk create changeorders",
	Long:        getChangeOrderCreateHelp(),
	Args:        cobra.MinimumNArgs(0), // Allow 0 args for bulk mode
	RunE:        changeorderCreateCmdRun,
	Annotations: map[string]string{"OrgLevel": ""},
}

func getChangeOrderCreateHelp() string {
	baseHelp := `Create a new changeorder or bulk create multiple changeorders by cloning existing ones.

SINGLE CHANGEORDER CREATION:

Create a new changeorder to record an entity changeorder specification.

Examples:
` + "```" + `
  # Create a change order
  cub changeorder create --space my-space bump-base-image --description "Bump the base image to 1.42"

  # Create one that says where it is headed: a Filter over Spaces
  cub changeorder create --space my-space bump-base-image --description "Bump the base image" \
    --space-filter platform/prod-spaces

  # End it at an existing boundary rather than at each unit's head
  cub changeorder create --space my-space bump-base-image --end-tag my-space/release-42-end

  # Create a changeorder from JSON
  cub changeorder create --space my-space -o json my-changeorder --from-stdin < changeorder.json
` + "```" + `

BULK CHANGEORDER CREATION:

When no positional arguments are provided, bulk create mode is activated. This mode clones existing
changeorders based on filters and creates multiple new changeorders with optional modifications.

Bulk Create Examples:
` + "```" + `
  # Clone all changeorders matching a pattern with name prefixes
  cub changeorder create --where "Description LIKE '%release%'" --name-prefix archive- --dest-space archive-space

  # Clone specific changeorders to multiple spaces
  cub changeorder create --changeorder my-changeorder --dest-space dev-space,staging-space

  # Clone changeorders using a where expression for destination spaces
  cub changeorder create --where "Description LIKE 'Release%'" --where-space "Labels.Environment IN ('dev', 'staging')"

  # Clone changeorders with modifications via JSON patch
  echo '{"Description": "Archived changeorder"}' | cub changeorder create --where "CreatedAt < '2024-01-01'" --name-prefix old- --from-stdin
` + "```" + `
`

	return getCommandHelp(baseHelp, "")
}

var changeorderCreateArgs struct {
	destSpaces     []string
	whereSpace     string
	namePrefixes   []string
	changeorderSlugs []string
	description    string
	spaceFilter    string
	endTag         string
	updateType     string
	filterSpace    string
	variantLabels  []string
	namePattern    string
}

func init() {
	addStandardCreateFlags(changeorderCreateCmd)
	enableWhereFlag(changeorderCreateCmd)
	enableFilterFlag(changeorderCreateCmd)

	// Single create specific flags
	changeorderCreateCmd.Flags().StringVar(&changeorderCreateArgs.description, "description", "", "human-readable description of the change")
	changeorderCreateCmd.Flags().StringVar(&changeorderCreateArgs.spaceFilter, "space-filter", "", "filter over Spaces (slug, space/slug, or UUID) selecting the Spaces this change order propagates into")
	changeorderCreateCmd.Flags().StringVar(&changeorderCreateArgs.updateType, "update-type", "", "link update type to follow when propagating: UpgradeUnit (the clone lineage, the default) or MergeUnits")
	changeorderCreateCmd.Flags().StringVar(&changeorderCreateArgs.endTag, "end-tag", "", "tag (slug, space/slug, or UUID) marking the last revision of each unit to promote; without one, the change order tags each unit's head revision")

	// Bulk create specific flags
	changeorderCreateCmd.Flags().StringSliceVar(&changeorderCreateArgs.destSpaces, "dest-space", []string{}, "destination spaces for bulk create (can be repeated or comma-separated)")
	changeorderCreateCmd.Flags().StringVar(&changeorderCreateArgs.whereSpace, "where-space", "", "where expression to select destination spaces for bulk create")
	changeorderCreateCmd.Flags().StringSliceVar(&changeorderCreateArgs.namePrefixes, "name-prefix", []string{}, "name prefixes for bulk create (can be repeated or comma-separated)")
	changeorderCreateCmd.Flags().StringSliceVar(&changeorderCreateArgs.variantLabels, "variant-labels", []string{}, "labels for bulk create in the format of key1=value1|value2,key2=value1|value2|value3")
	changeorderCreateCmd.Flags().StringVar(&changeorderCreateArgs.namePattern, "name-pattern", "", "a pattern string for name generation of clones, prefix 'template:' to use a Go template with .SourceEntitySlug to access the original ChangeOrder and .Labels to access variant labels, example: 'template:{{.SourceEntitySlug}}-{{.Labels.env}}'")
	changeorderCreateCmd.Flags().StringSliceVar(&changeorderCreateArgs.changeorderSlugs, "changeorder", []string{}, "target specific changeorders by slug or UUID for bulk create (can be repeated or comma-separated)")
	changeorderCreateCmd.Flags().StringVar(&changeorderCreateArgs.filterSpace, "filter-space", "", "filter entity containing WHERE expression to select destination spaces for bulk create (slug or UUID)")

	changeorderCmd.AddCommand(changeorderCreateCmd)
}

func checkChangeOrderCreateConflictingArgs(args []string) (bool, error) {
	// Determine if bulk create mode: no positional args
	isBulkCreateMode := len(args) == 0

	if isBulkCreateMode {
		// Validate bulk create requirements
		if len(changeorderCreateArgs.changeorderSlugs) > 0 && where != "" {
			return false, errors.New("--changeorder and --where flags are mutually exclusive")
		}

		if len(changeorderCreateArgs.destSpaces) > 0 && changeorderCreateArgs.whereSpace != "" {
			return false, errors.New("--dest-space and --where-space flags are mutually exclusive")
		}

		if len(changeorderCreateArgs.destSpaces) == 0 && changeorderCreateArgs.whereSpace == "" && len(changeorderCreateArgs.namePrefixes) == 0 && len(changeorderCreateArgs.variantLabels) == 0 {
			return false, errors.New("bulk create mode requires at least one of --dest-space, --where-space, --name-prefix, or --variant-labels")
		}

		if len(changeorderCreateArgs.namePrefixes) > 0 && len(changeorderCreateArgs.variantLabels) > 0 {
			return false, errors.New("--name-prefix and --variant-labels cannot be used together")
		}

		if changeorderCreateArgs.namePattern != "" && len(changeorderCreateArgs.namePrefixes) > 0 {
			return false, errors.New("--name-pattern and --name-prefix cannot be used together")
		}

		if changeorderCreateArgs.namePattern != "" && len(changeorderCreateArgs.variantLabels) == 0 {
			return false, errors.New("--name-pattern requires --variant-labels to be set")
		}
	} else {
		// Single create mode validation
		if len(args) != 1 {
			return false, errors.New("single changeorder creation requires: <slug>")
		}

		if filter != "" || where != "" || changeorderCreateArgs.namePattern != "" ||
			len(changeorderCreateArgs.changeorderSlugs) > 0 || len(changeorderCreateArgs.destSpaces) > 0 ||
			changeorderCreateArgs.whereSpace != "" || len(changeorderCreateArgs.namePrefixes) > 0 ||
			len(changeorderCreateArgs.variantLabels) > 0 {
			return false, errors.New(
				"bulk create flags (--filter, --where, --changeorder, --dest-space, --where-space, --name-prefix, --variant-labels, --name-pattern) can only be used without positional arguments",
			)
		}
	}

	if err := validateSpaceFlag(isBulkCreateMode); err != nil {
		return isBulkCreateMode, err
	}

	if err := validateStdinFlags(); err != nil {
		return isBulkCreateMode, err
	}

	// Validate no label removal
	if err := ValidateLabelRemoval(label, false); err != nil {
		return isBulkCreateMode, err
	}
	// Validate no delete gate removal
	if err := ValidateDeleteGateRemoval(deleteGate, false); err != nil {
		return isBulkCreateMode, err
	}

	return isBulkCreateMode, nil
}

func changeorderCreateCmdRun(cmd *cobra.Command, args []string) error {
	isBulkCreateMode, err := checkChangeOrderCreateConflictingArgs(args)
	if err != nil {
		return err
	}

	if isBulkCreateMode {
		return runBulkChangeOrderCreate()
	}

	return runSingleChangeOrderCreate(args)
}

func runSingleChangeOrderCreate(args []string) error {
	spaceID := uuid.MustParse(selectedSpaceID)
	newBody := goclientnew.ChangeOrder{}
	if flagPopulateModelFromStdin || flagFilename != "" {
		if err := populateModelFromFlags(&newBody); err != nil {
			return err
		}
	}
	err := setAnnotations(&newBody.Annotations)
	if err != nil {
		return err
	}
	err = setLabels(&newBody.Labels)
	if err != nil {
		return err
	}
	err = setDeleteGates(&newBody.DeleteGates)
	if err != nil {
		return err
	}
	newBody.SpaceID = spaceID
	newBody.Slug = makeSlug(args[0])
	if newBody.DisplayName == "" {
		newBody.DisplayName = args[0]
	}

	// Set description if provided
	if changeorderCreateArgs.description != "" {
		newBody.Description = changeorderCreateArgs.description
	}
	if changeorderCreateArgs.updateType != "" {
		newBody.UpdateType = changeorderCreateArgs.updateType
	}
	if changeorderCreateArgs.endTag != "" {
		endTagID, err := parseTagSlug(changeorderCreateArgs.endTag)
		if err != nil {
			return errors.Wrap(err, "failed to parse end-tag")
		}
		newBody.EndTagID = endTagID
	}
	if changeorderCreateArgs.spaceFilter != "" {
		filterIDStr, err := parseFilterFlag(changeorderCreateArgs.spaceFilter)
		if err != nil {
			return errors.Wrap(err, "failed to parse space-filter")
		}
		filterID, err := uuid.Parse(filterIDStr)
		if err != nil {
			return errors.Wrap(err, "invalid space-filter")
		}
		newBody.SpaceFilterID = &filterID
	}

	// Create params with AllowExists if needed
	params := &goclientnew.CreateChangeOrderParams{}
	if allowExists {
		allowExistsStr := "true"
		params.AllowExists = &allowExistsStr
	}

	changeorderRes, err := cubClientNew.CreateChangeOrderWithResponse(ctx, spaceID, params, newBody)
	if cubapi.IsAPIError(err, changeorderRes) {
		return cubapi.InterpretErrorGeneric(err, changeorderRes)
	}

	changeorderDetails := changeorderRes.JSON200
	displayCreateResults(changeorderDetails, "changeorder", args[0], changeorderDetails.ChangeOrderID.String(), displayChangeOrderDetails)
	return nil
}

func runBulkChangeOrderCreate() error {
	// Parse filter parameter
	filterID, err := parseFilterFlag(filter)
	if err != nil {
		return err
	}

	// Build WHERE clause from changeorder identifiers or use provided where clause
	var effectiveWhere string
	if len(changeorderCreateArgs.changeorderSlugs) > 0 {
		whereClause, err := buildWhereClauseFromChangeOrders(changeorderCreateArgs.changeorderSlugs)
		if err != nil {
			return err
		}
		effectiveWhere = whereClause
	} else {
		effectiveWhere = where
	}

	// Add space constraint to the where clause only if not org level
	effectiveWhere = addSpaceIDToWhereClause(effectiveWhere, selectedSpaceID)

	// Create enhancer function for changeorder-specific fields
	enhancer := func(patchMap map[string]interface{}) {
		// Add changeorder-specific fields
		if changeorderCreateArgs.description != "" {
			patchMap["Description"] = changeorderCreateArgs.description
		}
	}

	// Build patch data using consolidated function
	patchJSON, err := BuildPatchData(enhancer)
	if err != nil {
		return err
	}

	// Build bulk create parameters
	include := "SpaceID"
	params := &goclientnew.BulkCreateChangeOrdersParams{
		Where:   &effectiveWhere,
		Include: &include,
	}
	if filterID != "" {
		params.Filter = &filterID
	}

	// Set allow_exists parameter if flag is set
	if allowExists {
		allowExistsStr := "true"
		params.AllowExists = &allowExistsStr
	}

	// Add name prefixes if specified
	if len(changeorderCreateArgs.namePrefixes) > 0 {
		namePrefixesStr := strings.Join(changeorderCreateArgs.namePrefixes, ",")
		params.NamePrefixes = &namePrefixesStr
	}

	// Add variant labels if specified
	if len(changeorderCreateArgs.variantLabels) > 0 {
		variantLabelsStr := strings.Join(changeorderCreateArgs.variantLabels, ",")
		params.VariantLabels = &variantLabelsStr
	}

	// Add name pattern if specified
	if changeorderCreateArgs.namePattern != "" {
		params.NamePattern = &changeorderCreateArgs.namePattern
	}

	// Set where_space parameter - either from direct where-space flag or converted from dest-space
	var whereSpaceExpr string
	if changeorderCreateArgs.whereSpace != "" {
		whereSpaceExpr = changeorderCreateArgs.whereSpace
	} else if len(changeorderCreateArgs.destSpaces) > 0 {
		// Convert dest-space identifiers to a where expression
		whereSpaceExpr, err = buildWhereClauseForSpaces(changeorderCreateArgs.destSpaces)
		if err != nil {
			return errors.Wrapf(err, "error converting destination spaces to where expression")
		}
	}

	if whereSpaceExpr != "" {
		params.WhereSpace = &whereSpaceExpr
	}

	// Parse and set filter_space parameter if specified
	if changeorderCreateArgs.filterSpace != "" {
		filterSpaceID, err := parseFilterFlag(changeorderCreateArgs.filterSpace)
		if err != nil {
			return errors.Wrapf(err, "error parsing filter-space")
		}
		params.FilterSpace = &filterSpaceID
	}

	// Call the bulk create API
	bulkRes, err := cubClientNew.BulkCreateChangeOrdersWithBodyWithResponse(
		ctx,
		params,
		"application/merge-patch+json",
		bytes.NewReader(patchJSON),
	)
	if err != nil {
		return err
	}

	// Handle the response
	return handleBulkChangeOrderCreateOrUpdateResponse(bulkRes.JSON200, bulkRes.JSON207, bulkRes.StatusCode(), "create", effectiveWhere)
}
