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

var changesetCreateCmd = &cobra.Command{
	Use:         "create [<slug> [--filter <filter>] [--start-tag <start-tag>] [--end-tag <end-tag>] [--description <description>]]",
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
  # Create a changeset with a filter
  cub changeset create --space my-space release-changeset --filter unit-filter --description "Release 1.0 changes"

  # Create a changeset with start and end tags
  cub changeset create --space my-space hotfix-changeset --start-tag v1.0 --end-tag v1.1 --description "Hotfix changes"

  # Create a changeset with all parameters
  cub changeset create --space my-space full-changeset --filter deployment-filter --start-tag baseline --end-tag current --description "Full deployment changes"

  # Create a changeset from JSON
  cub changeset create --space my-space --json my-changeset --from-stdin < changeset.json

BULK CHANGESET CREATION:
When no positional arguments are provided, bulk create mode is activated. This mode clones existing
changesets based on filters and creates multiple new changesets with optional modifications.

Bulk Create Examples:
  # Clone all changesets matching a pattern with name prefixes
  cub changeset create --where "Description LIKE '%release%'" --name-prefix archive- --dest-space archive-space

  # Clone specific changesets to multiple spaces
  cub changeset create --changeset my-changeset --dest-space dev-space,staging-space

  # Clone changesets using a where expression for destination spaces
  cub changeset create --where "StartTagID IS NOT NULL" --where-space "Labels.Environment IN ('dev', 'staging')"

  # Clone changesets with modifications via JSON patch
  echo '{"Description": "Archived changeset"}' | cub changeset create --where "CreatedAt < '2024-01-01'" --name-prefix old- --from-stdin`

	return baseHelp
}

var changesetCreateArgs struct {
	destSpaces     []string
	whereSpace     string
	namePrefixes   []string
	changesetSlugs []string
	filter         string
	startTag       string
	endTag         string
	description    string
}

func init() {
	addStandardCreateFlags(changesetCreateCmd)
	enableWhereFlag(changesetCreateCmd)

	// Single create specific flags
	changesetCreateCmd.Flags().StringVar(&changesetCreateArgs.filter, "filter", "", "filter to identify units whose revisions are included (slug or UUID)")
	changesetCreateCmd.Flags().StringVar(&changesetCreateArgs.startTag, "start-tag", "", "tag identifying the set of revisions that begin the changeset (slug or UUID)")
	changesetCreateCmd.Flags().StringVar(&changesetCreateArgs.endTag, "end-tag", "", "tag identifying the set of revisions that end the changeset (slug or UUID)")
	changesetCreateCmd.Flags().StringVar(&changesetCreateArgs.description, "description", "", "human-readable description of the change")

	// Bulk create specific flags
	changesetCreateCmd.Flags().StringSliceVar(&changesetCreateArgs.destSpaces, "dest-space", []string{}, "destination spaces for bulk create (can be repeated or comma-separated)")
	changesetCreateCmd.Flags().StringVar(&changesetCreateArgs.whereSpace, "where-space", "", "where expression to select destination spaces for bulk create")
	changesetCreateCmd.Flags().StringSliceVar(&changesetCreateArgs.namePrefixes, "name-prefix", []string{}, "name prefixes for bulk create (can be repeated or comma-separated)")
	changesetCreateCmd.Flags().StringSliceVar(&changesetCreateArgs.changesetSlugs, "changeset", []string{}, "target specific changesets by slug or UUID for bulk create (can be repeated or comma-separated)")

	changesetCmd.AddCommand(changesetCreateCmd)
}

func checkChangeSetCreateConflictingArgs(args []string) (bool, error) {
	// Determine if bulk create mode: no positional args and has bulk-specific flags
	isBulkCreateMode := len(args) == 0 && (where != "" || len(changesetCreateArgs.changesetSlugs) > 0 || len(changesetCreateArgs.destSpaces) > 0 || changesetCreateArgs.whereSpace != "" || len(changesetCreateArgs.namePrefixes) > 0)

	if isBulkCreateMode {
		// Validate bulk create requirements
		if where == "" && len(changesetCreateArgs.changesetSlugs) == 0 {
			return false, errors.New("bulk create mode requires --where or --changeset flags")
		}

		if len(changesetCreateArgs.changesetSlugs) > 0 && where != "" {
			return false, errors.New("--changeset and --where flags are mutually exclusive")
		}

		if len(changesetCreateArgs.destSpaces) > 0 && changesetCreateArgs.whereSpace != "" {
			return false, errors.New("--dest-space and --where-space flags are mutually exclusive")
		}

		if len(changesetCreateArgs.destSpaces) == 0 && changesetCreateArgs.whereSpace == "" && len(changesetCreateArgs.namePrefixes) == 0 {
			return false, errors.New("bulk create mode requires at least one of --dest-space, --where-space, or --name-prefix")
		}
	} else {
		// Single create mode validation
		if len(args) < 1 {
			return false, errors.New("single changeset creation requires: <slug>")
		}

		if where != "" || len(changesetCreateArgs.changesetSlugs) > 0 || len(changesetCreateArgs.destSpaces) > 0 || changesetCreateArgs.whereSpace != "" || len(changesetCreateArgs.namePrefixes) > 0 {
			return false, errors.New("bulk create flags (--where, --changeset, --dest-space, --where-space, --name-prefix) can only be used without positional arguments")
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
	err := setLabels(&newBody.Labels)
	if err != nil {
		return err
	}
	newBody.SpaceID = spaceID
	newBody.Slug = makeSlug(args[0])
	if newBody.DisplayName == "" {
		newBody.DisplayName = args[0]
	}

	// Set filter reference if provided
	if changesetCreateArgs.filter != "" {
		filter, err := apiGetFilterFromSlug(changesetCreateArgs.filter, "FilterID")
		if err != nil {
			return err
		}
		newBody.FilterID = &filter.FilterID
	}

	// Set start tag reference if provided
	if changesetCreateArgs.startTag != "" {
		startTag, err := apiGetTagFromSlug(changesetCreateArgs.startTag, "TagID")
		if err != nil {
			return err
		}
		newBody.StartTagID = &startTag.TagID
	}

	// Set end tag reference if provided
	if changesetCreateArgs.endTag != "" {
		endTag, err := apiGetTagFromSlug(changesetCreateArgs.endTag, "TagID")
		if err != nil {
			return err
		}
		newBody.EndTagID = &endTag.TagID
	}

	// Set description if provided
	if changesetCreateArgs.description != "" {
		newBody.Description = changesetCreateArgs.description
	}

	changesetRes, err := cubClientNew.CreateChangeSetWithResponse(ctx, spaceID, newBody)
	if IsAPIError(err, changesetRes) {
		return InterpretErrorGeneric(err, changesetRes)
	}

	changesetDetails := changesetRes.JSON200
	displayCreateResults(changesetDetails, "changeset", args[0], changesetDetails.ChangeSetID.String(), displayChangeSetDetails)
	return nil
}

func runBulkChangeSetCreate() error {
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

	// Build patch data using consolidated function (no entity-specific fields for changeset)
	patchJSON, err := BuildPatchData(nil)
	if err != nil {
		return err
	}

	// Build bulk create parameters
	include := "SpaceID"
	params := &goclientnew.BulkCreateChangeSetsParams{
		Where:   &effectiveWhere,
		Include: &include,
	}

	// Add name prefixes if specified
	if len(changesetCreateArgs.namePrefixes) > 0 {
		namePrefixesStr := strings.Join(changesetCreateArgs.namePrefixes, ",")
		params.NamePrefixes = &namePrefixesStr
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
