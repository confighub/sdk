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

var tagCreateCmd = &cobra.Command{
	Use:         "create [<slug>]",
	Short:       "Create a new tag or bulk create tags",
	Long:        getTagCreateHelp(),
	Args:        cobra.MinimumNArgs(0), // Allow 0 args for bulk mode
	RunE:        tagCreateCmdRun,
	Annotations: map[string]string{"OrgLevel": ""},
}

func getTagCreateHelp() string {
	baseHelp := `Create a new tag or bulk create multiple tags by cloning existing ones.

SINGLE TAG CREATION:
Create a new tag to identify a set of revisions across units.

Examples:
  # Create a tag for a release
  cub tag create --space my-space release-v1.0

  # Create a tag with labels
  cub tag create --space my-space production-deploy --label version=1.0 --label environment=prod

  # Create a tag with a display name
  cub tag create --space my-space --json my-tag --from-stdin < tag.json

BULK TAG CREATION:
When no positional arguments are provided, bulk create mode is activated. This mode clones existing
tags based on filters and creates multiple new tags with optional modifications.

Bulk Create Examples:
  # Clone all tags matching a pattern with name prefixes
  cub tag create --where "Slug LIKE 'release-%'" --name-prefix archive- --dest-space archive-space

  # Clone specific tags to multiple spaces
  cub tag create --tag release-v1.0 --dest-space dev-space,staging-space

  # Clone tags using a where expression for destination spaces
  cub tag create --where "Labels.version = '1.0'" --where-space "Labels.Environment IN ('dev', 'staging')"

  # Clone tags with modifications via JSON patch
  echo '{"Labels": {"archived": "true"}}' | cub tag create --where "CreatedAt < '2024-01-01'" --name-prefix old- --from-stdin`

	return baseHelp
}

var tagCreateArgs struct {
	destSpaces   []string
	whereSpace   string
	namePrefixes []string
	tagSlugs     []string
}

func init() {
	addStandardCreateFlags(tagCreateCmd)
	enableWhereFlag(tagCreateCmd)

	// Bulk create specific flags
	tagCreateCmd.Flags().StringSliceVar(&tagCreateArgs.destSpaces, "dest-space", []string{}, "destination spaces for bulk create (can be repeated or comma-separated)")
	tagCreateCmd.Flags().StringVar(&tagCreateArgs.whereSpace, "where-space", "", "where expression to select destination spaces for bulk create")
	tagCreateCmd.Flags().StringSliceVar(&tagCreateArgs.namePrefixes, "name-prefix", []string{}, "name prefixes for bulk create (can be repeated or comma-separated)")
	tagCreateCmd.Flags().StringSliceVar(&tagCreateArgs.tagSlugs, "tag", []string{}, "target specific tags by slug or UUID for bulk create (can be repeated or comma-separated)")

	tagCmd.AddCommand(tagCreateCmd)
}

func checkTagCreateConflictingArgs(args []string) (bool, error) {
	// Determine if bulk create mode: no positional args and has bulk-specific flags
	isBulkCreateMode := len(args) == 0 && (where != "" || len(tagCreateArgs.tagSlugs) > 0 || len(tagCreateArgs.destSpaces) > 0 || tagCreateArgs.whereSpace != "" || len(tagCreateArgs.namePrefixes) > 0)

	if isBulkCreateMode {
		// Validate bulk create requirements
		if where == "" && len(tagCreateArgs.tagSlugs) == 0 {
			return false, errors.New("bulk create mode requires --where or --tag flags")
		}

		if len(tagCreateArgs.tagSlugs) > 0 && where != "" {
			return false, errors.New("--tag and --where flags are mutually exclusive")
		}

		if len(tagCreateArgs.destSpaces) > 0 && tagCreateArgs.whereSpace != "" {
			return false, errors.New("--dest-space and --where-space flags are mutually exclusive")
		}

		if len(tagCreateArgs.destSpaces) == 0 && tagCreateArgs.whereSpace == "" && len(tagCreateArgs.namePrefixes) == 0 {
			return false, errors.New("bulk create mode requires at least one of --dest-space, --where-space, or --name-prefix")
		}
	} else {
		// Single create mode validation
		if len(args) < 1 {
			return false, errors.New("single tag creation requires: <slug>")
		}

		if where != "" || len(tagCreateArgs.tagSlugs) > 0 || len(tagCreateArgs.destSpaces) > 0 || tagCreateArgs.whereSpace != "" || len(tagCreateArgs.namePrefixes) > 0 {
			return false, errors.New("bulk create flags (--where, --tag, --dest-space, --where-space, --name-prefix) can only be used without positional arguments")
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

func tagCreateCmdRun(cmd *cobra.Command, args []string) error {
	isBulkCreateMode, err := checkTagCreateConflictingArgs(args)
	if err != nil {
		return err
	}

	if isBulkCreateMode {
		return runBulkTagCreate()
	}

	return runSingleTagCreate(args)
}

func runSingleTagCreate(args []string) error {
	spaceID := uuid.MustParse(selectedSpaceID)
	newBody := goclientnew.Tag{}
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

	tagRes, err := cubClientNew.CreateTagWithResponse(ctx, spaceID, newBody)
	if IsAPIError(err, tagRes) {
		return InterpretErrorGeneric(err, tagRes)
	}

	tagDetails := tagRes.JSON200
	displayCreateResults(tagDetails, "tag", args[0], tagDetails.TagID.String(), displayTagDetails)
	return nil
}

func runBulkTagCreate() error {
	// Build WHERE clause from tag identifiers or use provided where clause
	var effectiveWhere string
	if len(tagCreateArgs.tagSlugs) > 0 {
		whereClause, err := buildWhereClauseFromTags(tagCreateArgs.tagSlugs)
		if err != nil {
			return err
		}
		effectiveWhere = whereClause
	} else {
		effectiveWhere = where
	}

	// Add space constraint to the where clause only if not org level
	effectiveWhere = addSpaceIDToWhereClause(effectiveWhere, selectedSpaceID)

	// Build patch data using consolidated function (no entity-specific fields for tag)
	patchJSON, err := BuildPatchData(nil)
	if err != nil {
		return err
	}

	// Build bulk create parameters
	include := "SpaceID"
	params := &goclientnew.BulkCreateTagsParams{
		Where:   &effectiveWhere,
		Include: &include,
	}

	// Add name prefixes if specified
	if len(tagCreateArgs.namePrefixes) > 0 {
		namePrefixesStr := strings.Join(tagCreateArgs.namePrefixes, ",")
		params.NamePrefixes = &namePrefixesStr
	}

	// Set where_space parameter - either from direct where-space flag or converted from dest-space
	var whereSpaceExpr string
	if tagCreateArgs.whereSpace != "" {
		whereSpaceExpr = tagCreateArgs.whereSpace
	} else if len(tagCreateArgs.destSpaces) > 0 {
		// Convert dest-space identifiers to a where expression
		whereSpaceExpr, err = buildWhereClauseForSpaces(tagCreateArgs.destSpaces)
		if err != nil {
			return errors.Wrapf(err, "error converting destination spaces to where expression")
		}
	}

	if whereSpaceExpr != "" {
		params.WhereSpace = &whereSpaceExpr
	}

	// Call the bulk create API
	bulkRes, err := cubClientNew.BulkCreateTagsWithBodyWithResponse(
		ctx,
		params,
		"application/merge-patch+json",
		bytes.NewReader(patchJSON),
	)
	if err != nil {
		return err
	}

	// Handle the response
	return handleBulkTagCreateOrUpdateResponse(bulkRes.JSON200, bulkRes.JSON207, bulkRes.StatusCode(), "create", effectiveWhere)
}
