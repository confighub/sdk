// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"fmt"
	"sort"
	"strings"

	"github.com/google/uuid"
	"github.com/spf13/cobra"

	"github.com/confighub/sdk/core/cubapi"
	goclientnew "github.com/confighub/sdk/core/openapi/goclient-new"
)

var revisionListCmd = &cobra.Command{
	Use:   "list [unit]",
	Short: "List revisions",
	Long: getCommandHelp(`List revisions for a unit in a space, or across all spaces when selectedSpaceID is "*". The output includes revision numbers, timestamps, usernames, sources, descriptions, apply gates, and tags. Revisions track the history of changes made to a unit's configuration.

Examples:
`+"```"+`
  # List all revisions for a unit
  cub revision list --space my-space my-ns

  # List revisions without headers
  cub revision list --space my-space --no-header my-ns

  # List revisions in JSON format
  cub revision list --space my-space --json my-ns

  # List revisions using unit ID instead of slug
  cub revision list --space my-space --by-unit-id 61f26b06-3c34-4363-8b9d-7d0a7c2b5f1c

  # List revisions with custom JQ filter
  cub revision list --space my-space --jq '.[].RevisionNum' my-ns

  # List revisions with specific criteria
  cub revision list --space my-space --where 'RevisionNum > 1' my-ns

  # List revisions across all spaces (organization-wide search, at most one revision per unit)
  cub revision list --space '*' --where 'Tags.my-tag-id = "some-value"'
`+"```"+`
`, ""),
	Args:        cobra.RangeArgs(0, 1),
	RunE:        revisionListCmdRun,
	Annotations: map[string]string{"OrgLevel": ""},
}

// Default columns to display when no custom columns are specified
var defaultRevisionColumns = []string{"Revision.RevisionNum", "Unit.Slug", "ChangeSet.Slug", "Revision.CreatedAt", "User.Username", "Revision.Source", "Revision.Description", "Revision.ApplyGates", "Revision.Tags"}

// Revision-specific aliases
var revisionAliases = map[string]string{
	"Name": "RevisionNum",
	"ID":   "RevisionID",
}

// Revision custom column dependencies
var revisionCustomColumnDependencies = map[string][]string{}

// Flags for tag-based filtering
var (
	revisionTagSlug           string
	revisionChangeSetStartTag string
	revisionChangeSetEndTag   string
)

func init() {
	addStandardListFlags(revisionListCmd)
	enableWebFlag(revisionListCmd)
	revisionListCmd.Flags().StringVar(&revisionTagSlug, "tag", "", "filter revisions by tag slug or UUID")
	revisionListCmd.Flags().StringVar(&revisionChangeSetStartTag, "changeset-starttag", "", "filter revisions by changeset start tag slug or UUID")
	revisionListCmd.Flags().StringVar(&revisionChangeSetEndTag, "changeset-endtag", "", "filter revisions by changeset end tag slug or UUID")
	revisionCmd.AddCommand(revisionListCmd)
}

func revisionListCmdRun(cmd *cobra.Command, args []string) error {
	filterID, err := parseFilterFlag(filter)
	if err != nil {
		return err
	}

	// Build where clause from tag flags
	effectiveWhere, err := buildRevisionWhereClause()
	if err != nil {
		return err
	}

	// Determine if we're in bulk mode (no positional argument)
	isBulkMode := len(args) == 0

	if isBulkMode {
		// Add space constraint if not wildcard
		if selectedSpaceID != "*" {
			effectiveWhere = addSpaceIDToWhereClause(effectiveWhere, selectedSpaceID)
		}

		// Use the SearchList API for bulk revision search
		revisions, err := apiSearchListRevisions(effectiveWhere, selectFields, filterID)
		if err != nil {
			return err
		}
		displayListResults(revisions, getRevisionSlug, displayRevisionList)
		return nil
	}

	// Regular unit-specific revision listing
	unit, err := apiGetUnitFromSlug(args[0], "*") // get all fields for now
	if err != nil {
		return err
	}

	if webFlag {
		ctx := contextManager.ActiveContext()
		url := cubapi.GetUnitRevisionsURL(ctx.Coordinate.ServerURL, unit.SpaceID.String(), unit.UnitID.String())
		return openWebUI(url)
	}

	revisions, err := apiListRevisions(selectedSpaceID, unit.UnitID.String(), effectiveWhere, selectFields, filterID)
	if err != nil {
		return err
	}
	displayListResults(revisions, getRevisionSlug, displayRevisionList)
	return nil
}

// buildRevisionWhereClause constructs the where clause from tag flags and user-provided where expression
func buildRevisionWhereClause() (string, error) {
	var clauses []string

	// Handle --tag flag
	if revisionTagSlug != "" {
		tag, err := parseEntityIdentifierSingleAsEntity[goclientnew.Tag](
			revisionTagSlug,
			EntityTypeTag,
			"*", // selectParam to get all fields
			apiGetTagFromSlugInSpace,
			func(t *goclientnew.Tag) string { return t.TagID.String() },
		)
		if err != nil {
			return "", fmt.Errorf("failed to parse tag '%s': %w", revisionTagSlug, err)
		}
		clauses = append(clauses, fmt.Sprintf("Tags ? '%s'", tag.TagID.String()))
	}

	// Handle --changeset-starttag flag
	if revisionChangeSetStartTag != "" {
		changeset, err := parseEntityIdentifierSingleAsEntity[goclientnew.ChangeSet](
			revisionChangeSetStartTag,
			EntityTypeChangeSet,
			"*", // selectParam to get all fields
			apiGetChangeSetFromSlugInSpace,
			func(cs *goclientnew.ChangeSet) string { return cs.ChangeSetID.String() },
		)
		if err != nil {
			return "", fmt.Errorf("failed to parse changeset start tag '%s': %w", revisionChangeSetStartTag, err)
		}
		if changeset.State != "New" {
			clauses = append(clauses, fmt.Sprintf("Tags ? '%s'", changeset.StartTagID.String()))
		} else {
			return "", fmt.Errorf("changeset '%s' has not been added to any units yet", revisionChangeSetStartTag)
		}
	}

	// Handle --changeset-endtag flag
	if revisionChangeSetEndTag != "" {
		changeset, err := parseEntityIdentifierSingleAsEntity[goclientnew.ChangeSet](
			revisionChangeSetEndTag,
			EntityTypeChangeSet,
			"*", // selectParam to get all fields
			apiGetChangeSetFromSlugInSpace,
			func(cs *goclientnew.ChangeSet) string { return cs.ChangeSetID.String() },
		)
		if err != nil {
			return "", fmt.Errorf("failed to parse changeset end tag '%s': %w", revisionChangeSetEndTag, err)
		}
		if changeset.State == "Closed" {
			clauses = append(clauses, fmt.Sprintf("Tags ? '%s'", changeset.EndTagID.String()))
		} else {
			return "", fmt.Errorf("changeset '%s' is not closed", revisionChangeSetEndTag)
		}
	}

	// Add user-provided where clause
	if where != "" {
		clauses = append(clauses, where)
	}

	// Combine all clauses with AND
	if len(clauses) == 0 {
		return "", nil
	}
	return strings.Join(clauses, " AND "), nil
}

func getRevisionSlug(extendedRevision *goclientnew.ExtendedRevision) string {
	// Use number
	return fmt.Sprintf("%d", extendedRevision.Revision.RevisionNum)
}

func displayRevisionList(extendedRevisions []*goclientnew.ExtendedRevision) {
	table := tableView()
	if !noheader {
		table.SetHeader([]string{"Num", "Unit", "ChangeSet", "Time", "User", "Source", "Description", "Apply-Gates", "Tags"})
	}
	for _, extendedRev := range extendedRevisions {
		rev := extendedRev.Revision
		applyGates := "None"
		if rev.ApplyGates != nil {
			if len(rev.ApplyGates) > 1 {
				applyGates = "Multiple"
			} else {
				for key := range rev.ApplyGates {
					applyGates = key
				}
			}
		}
		username := ""
		if extendedRev.User != nil {
			username = extendedRev.User.Username
		}
		unit := ""
		if extendedRev.Unit != nil {
			unit = extendedRev.Unit.Slug
		}

		// Show ChangeSet slug if available, otherwise ChangeSetID if not nil and not uuid.Nil
		changeSet := ""
		if extendedRev.ChangeSet != nil {
			changeSet = extendedRev.ChangeSet.Slug
		} else if rev.ChangeSetID != nil && *rev.ChangeSetID != uuid.Nil {
			changeSet = rev.ChangeSetID.String()
		}

		// Resolve tags to slugs
		tags := "None"
		if extendedRev.Tags != nil && len(extendedRev.Tags) > 0 {
			var tagSlugs []string
			for _, tag := range extendedRev.Tags {
				if tag.Slug != "" {
					tagSlugs = append(tagSlugs, tag.Slug)
				}
			}
			if len(tagSlugs) > 0 {
				tags = strings.Join(tagSlugs, ", ")
			}
		} else if rev.Tags != nil && len(rev.Tags) > 0 {
			tagSlugs := resolveTagSlugs(rev.Tags, rev.SpaceID.String())
			if len(tagSlugs) > 0 {
				tags = strings.Join(tagSlugs, ", ")
			}
		}

		table.Append([]string{
			fmt.Sprintf("%d", rev.RevisionNum),
			unit,
			changeSet,
			rev.CreatedAt.String(),
			username,
			rev.Source,
			rev.Description,
			applyGates,
			tags,
		})
	}
	table.Render()
}

func apiListRevisions(spaceID string, unitID string, whereFilter string, selectParam string, filterParam string) ([]*goclientnew.ExtendedRevision, error) {
	newParams := &goclientnew.ListExtendedRevisionsParams{}
	if whereFilter != "" {
		newParams.Where = &whereFilter
	}
	if filterParam != "" {
		newParams.Filter = &filterParam
	}
	if contains != "" {
		newParams.Contains = &contains
	}
	include := "UserID,UnitID,ChangeSetID,Tags"
	newParams.Include = &include
	selectValue := handleSelectParameter(selectParam, selectFields, func() string {
		baseFields := []string{"RevisionNum", "RevisionID", "UnitID", "SpaceID", "OrganizationID"}
		return buildSelectList("Revision", "", include, defaultRevisionColumns, revisionAliases, revisionCustomColumnDependencies, baseFields)
	})
	if selectValue != "" && selectValue != "*" {
		newParams.Select = &selectValue
	}
	revsRes, err := cubClientNew.ListExtendedRevisionsWithResponse(ctx,
		uuid.MustParse(spaceID),
		uuid.MustParse(unitID),
		newParams,
	)
	if cubapi.IsAPIError(err, revsRes) {
		return nil, cubapi.InterpretErrorGeneric(err, revsRes)
	}

	revisions := make([]*goclientnew.ExtendedRevision, len(*revsRes.JSON200))
	for i, er := range *revsRes.JSON200 {
		revisions[i] = &er
	}

	// Sort by RevisionNum descending
	sort.Slice(revisions, func(i, j int) bool {
		return revisions[i].Revision.RevisionNum > revisions[j].Revision.RevisionNum
	})

	return revisions, nil
}

func apiSearchListRevisions(whereFilter string, selectParam string, filterParam string) ([]*goclientnew.ExtendedRevision, error) {
	newParams := &goclientnew.ListAllRevisionsParams{}
	if whereFilter != "" {
		newParams.Where = &whereFilter
	}
	if filterParam != "" {
		newParams.Filter = &filterParam
	}
	if contains != "" {
		newParams.Contains = &contains
	}
	include := "UserID,UnitID,ChangeSetID,Tags"
	newParams.Include = &include
	selectValue := handleSelectParameter(selectParam, selectFields, func() string {
		baseFields := []string{"RevisionNum", "RevisionID", "UnitID", "SpaceID", "OrganizationID"}
		return buildSelectList("Revision", "", include, defaultRevisionColumns, revisionAliases, revisionCustomColumnDependencies, baseFields)
	})
	if selectValue != "" && selectValue != "*" {
		newParams.Select = &selectValue
	}
	revsRes, err := cubClientNew.ListAllRevisionsWithResponse(ctx, newParams)
	if cubapi.IsAPIError(err, revsRes) {
		return nil, cubapi.InterpretErrorGeneric(err, revsRes)
	}

	revisions := make([]*goclientnew.ExtendedRevision, len(*revsRes.JSON200))
	for i, er := range *revsRes.JSON200 {
		revisions[i] = &er
	}

	// Sort by RevisionNum descending
	sort.Slice(revisions, func(i, j int) bool {
		return revisions[i].Revision.RevisionNum > revisions[j].Revision.RevisionNum
	})

	return revisions, nil
}
