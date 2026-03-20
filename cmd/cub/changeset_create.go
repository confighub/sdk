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

var changesetCreateCmd = &cobra.Command{
	Use:         "create [<slug> [--description <description>]]",
	Short:       "Create a new changeset or bulk create changesets",
	Long:        getChangeSetCreateHelp(),
	Args:        cobra.MinimumNArgs(0), // Allow 0 args for bulk mode
	RunE:        changesetCreateCmdRun,
	Annotations: map[string]string{"OrgLevel": ""},
}

func getChangeSetCreateHelp() string {
	baseHelp := `Create a new changeset or bulk create multiple changesets by cloning existing ones.

SINGLE CHANGESET CREATION:

Create a new changeset to record an entity changeset specification.

Examples:
` + "```" + `
  # Create a changeset
  cub changeset create --space my-space hotfix-changeset --description "Hotfix changes"

  # Create a changeset from JSON
  cub changeset create --space my-space --json my-changeset --from-stdin < changeset.json
` + "```" + `

BULK CHANGESET CREATION:

When no positional arguments are provided, bulk create mode is activated. This mode clones existing
changesets based on filters and creates multiple new changesets with optional modifications.

Bulk Create Examples:
` + "```" + `
  # Clone all changesets matching a pattern with name prefixes
  cub changeset create --where "Description LIKE '%release%'" --name-prefix archive- --dest-space archive-space

  # Clone specific changesets to multiple spaces
  cub changeset create --changeset my-changeset --dest-space dev-space,staging-space

  # Clone changesets using a where expression for destination spaces
  cub changeset create --where "Description LIKE 'Release%'" --where-space "Labels.Environment IN ('dev', 'staging')"

  # Clone changesets with modifications via JSON patch
  echo '{"Description": "Archived changeset"}' | cub changeset create --where "CreatedAt < '2024-01-01'" --name-prefix old- --from-stdin
` + "```" + `
`

	return getCommandHelp(baseHelp, "")
}

var changesetCreateArgs struct {
	destSpaces     []string
	whereSpace     string
	namePrefixes   []string
	changesetSlugs []string
	description    string
	filterSpace    string
	variantLabels  []string
	namePattern    string
}

func init() {
	addStandardCreateFlags(changesetCreateCmd)
	enableWhereFlag(changesetCreateCmd)
	enableFilterFlag(changesetCreateCmd)

	// Single create specific flags
	changesetCreateCmd.Flags().StringVar(&changesetCreateArgs.description, "description", "", "human-readable description of the change")

	// Bulk create specific flags
	changesetCreateCmd.Flags().StringSliceVar(&changesetCreateArgs.destSpaces, "dest-space", []string{}, "destination spaces for bulk create (can be repeated or comma-separated)")
	changesetCreateCmd.Flags().StringVar(&changesetCreateArgs.whereSpace, "where-space", "", "where expression to select destination spaces for bulk create")
	changesetCreateCmd.Flags().StringSliceVar(&changesetCreateArgs.namePrefixes, "name-prefix", []string{}, "name prefixes for bulk create (can be repeated or comma-separated)")
	changesetCreateCmd.Flags().StringSliceVar(&changesetCreateArgs.variantLabels, "variant-labels", []string{}, "labels for bulk create in the format of key1=value1|value2,key2=value1|value2|value3")
	changesetCreateCmd.Flags().StringVar(&changesetCreateArgs.namePattern, "name-pattern", "", "a pattern string for name generation of clones, prefix 'template:' to use a Go template with .SourceEntitySlug to access the original ChangeSet and .Labels to access variant labels, example: 'template:{{.SourceEntitySlug}}-{{.Labels.env}}'")
	changesetCreateCmd.Flags().StringSliceVar(&changesetCreateArgs.changesetSlugs, "changeset", []string{}, "target specific changesets by slug or UUID for bulk create (can be repeated or comma-separated)")
	changesetCreateCmd.Flags().StringVar(&changesetCreateArgs.filterSpace, "filter-space", "", "filter entity containing WHERE expression to select destination spaces for bulk create (slug or UUID)")

	changesetCmd.AddCommand(changesetCreateCmd)
}

func checkChangeSetCreateConflictingArgs(args []string) (bool, error) {
	// Determine if bulk create mode: no positional args
	isBulkCreateMode := len(args) == 0

	if isBulkCreateMode {
		// Validate bulk create requirements
		if len(changesetCreateArgs.changesetSlugs) > 0 && where != "" {
			return false, errors.New("--changeset and --where flags are mutually exclusive")
		}

		if len(changesetCreateArgs.destSpaces) > 0 && changesetCreateArgs.whereSpace != "" {
			return false, errors.New("--dest-space and --where-space flags are mutually exclusive")
		}

		if len(changesetCreateArgs.destSpaces) == 0 && changesetCreateArgs.whereSpace == "" && len(changesetCreateArgs.namePrefixes) == 0 && len(changesetCreateArgs.variantLabels) == 0 {
			return false, errors.New("bulk create mode requires at least one of --dest-space, --where-space, --name-prefix, or --variant-labels")
		}

		if len(changesetCreateArgs.namePrefixes) > 0 && len(changesetCreateArgs.variantLabels) > 0 {
			return false, errors.New("--name-prefix and --variant-labels cannot be used together")
		}

		if changesetCreateArgs.namePattern != "" && len(changesetCreateArgs.namePrefixes) > 0 {
			return false, errors.New("--name-pattern and --name-prefix cannot be used together")
		}

		if changesetCreateArgs.namePattern != "" && len(changesetCreateArgs.variantLabels) == 0 {
			return false, errors.New("--name-pattern requires --variant-labels to be set")
		}
	} else {
		// Single create mode validation
		if len(args) != 1 {
			return false, errors.New("single changeset creation requires: <slug>")
		}

		if filter != "" || where != "" || changesetCreateArgs.namePattern != "" ||
			len(changesetCreateArgs.changesetSlugs) > 0 || len(changesetCreateArgs.destSpaces) > 0 ||
			changesetCreateArgs.whereSpace != "" || len(changesetCreateArgs.namePrefixes) > 0 ||
			len(changesetCreateArgs.variantLabels) > 0 {
			return false, errors.New(
				"bulk create flags (--filter, --where, --changeset, --dest-space, --where-space, --name-prefix, --variant-labels, --name-pattern) can only be used without positional arguments",
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

func changesetCreateCmdRun(cmd *cobra.Command, args []string) error {
	isBulkCreateMode, err := checkChangeSetCreateConflictingArgs(args)
	if err != nil {
		return err
	}

	if isBulkCreateMode {
		return runBulkChangeSetCreate()
	}

	return runSingleChangeSetCreate(args)
}

func runSingleChangeSetCreate(args []string) error {
	spaceID := uuid.MustParse(selectedSpaceID)
	newBody := goclientnew.ChangeSet{}
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
	if changesetCreateArgs.description != "" {
		newBody.Description = changesetCreateArgs.description
	}

	// Create params with AllowExists if needed
	params := &goclientnew.CreateChangeSetParams{}
	if allowExists {
		allowExistsStr := "true"
		params.AllowExists = &allowExistsStr
	}

	changesetRes, err := cubClientNew.CreateChangeSetWithResponse(ctx, spaceID, params, newBody)
	if cubapi.IsAPIError(err, changesetRes) {
		return cubapi.InterpretErrorGeneric(err, changesetRes)
	}

	changesetDetails := changesetRes.JSON200
	displayCreateResults(changesetDetails, "changeset", args[0], changesetDetails.ChangeSetID.String(), displayChangeSetDetails)
	return nil
}

func runBulkChangeSetCreate() error {
	// Parse filter parameter
	filterID, err := parseFilterFlag(filter)
	if err != nil {
		return err
	}

	// Build WHERE clause from changeset identifiers or use provided where clause
	var effectiveWhere string
	if len(changesetCreateArgs.changesetSlugs) > 0 {
		whereClause, err := buildWhereClauseFromChangeSets(changesetCreateArgs.changesetSlugs)
		if err != nil {
			return err
		}
		effectiveWhere = whereClause
	} else {
		effectiveWhere = where
	}

	// Add space constraint to the where clause only if not org level
	effectiveWhere = addSpaceIDToWhereClause(effectiveWhere, selectedSpaceID)

	// Create enhancer function for changeset-specific fields
	enhancer := func(patchMap map[string]interface{}) {
		// Add changeset-specific fields
		if changesetCreateArgs.description != "" {
			patchMap["Description"] = changesetCreateArgs.description
		}
	}

	// Build patch data using consolidated function
	patchJSON, err := BuildPatchData(enhancer)
	if err != nil {
		return err
	}

	// Build bulk create parameters
	include := "SpaceID"
	params := &goclientnew.BulkCreateChangeSetsParams{
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
	if len(changesetCreateArgs.namePrefixes) > 0 {
		namePrefixesStr := strings.Join(changesetCreateArgs.namePrefixes, ",")
		params.NamePrefixes = &namePrefixesStr
	}

	// Add variant labels if specified
	if len(changesetCreateArgs.variantLabels) > 0 {
		variantLabelsStr := strings.Join(changesetCreateArgs.variantLabels, ",")
		params.VariantLabels = &variantLabelsStr
	}

	// Add name pattern if specified
	if changesetCreateArgs.namePattern != "" {
		params.NamePattern = &changesetCreateArgs.namePattern
	}

	// Set where_space parameter - either from direct where-space flag or converted from dest-space
	var whereSpaceExpr string
	if changesetCreateArgs.whereSpace != "" {
		whereSpaceExpr = changesetCreateArgs.whereSpace
	} else if len(changesetCreateArgs.destSpaces) > 0 {
		// Convert dest-space identifiers to a where expression
		whereSpaceExpr, err = buildWhereClauseForSpaces(changesetCreateArgs.destSpaces)
		if err != nil {
			return errors.Wrapf(err, "error converting destination spaces to where expression")
		}
	}

	if whereSpaceExpr != "" {
		params.WhereSpace = &whereSpaceExpr
	}

	// Parse and set filter_space parameter if specified
	if changesetCreateArgs.filterSpace != "" {
		filterSpaceID, err := parseFilterFlag(changesetCreateArgs.filterSpace)
		if err != nil {
			return errors.Wrapf(err, "error parsing filter-space")
		}
		params.FilterSpace = &filterSpaceID
	}

	// Call the bulk create API
	bulkRes, err := cubClientNew.BulkCreateChangeSetsWithBodyWithResponse(
		ctx,
		params,
		"application/merge-patch+json",
		bytes.NewReader(patchJSON),
	)
	if err != nil {
		return err
	}

	// Handle the response
	return handleBulkChangeSetCreateOrUpdateResponse(bulkRes.JSON200, bulkRes.JSON207, bulkRes.StatusCode(), "create", effectiveWhere)
}
