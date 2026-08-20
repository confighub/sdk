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
	Long: getCommandHelp(`List revisions for a unit in a space, or across all spaces when selectedSpaceID is "*". Revisions track the history of changes made to a unit's configuration.

The default output identifies each revision and shows what change management put on it: its
ChangeSet, the ChangeOrders it belongs to, and its Tags, with the change description last and
truncated. -o wide adds the timestamp, the user, and the apply gates, and shows the whole
description.

Examples:
`+"```"+`
  # List all revisions for a unit
  cub revision list --space my-space my-ns

  # Include the timestamp, user, and apply gates, with untruncated descriptions
  cub revision list --space my-space -o wide my-ns

  # List the revisions a change order landed on, across every space it reached
  cub revision list --space '*' --change-order my-base/release-42

  # List revisions without headers
  cub revision list --space my-space --no-headers my-ns

  # List revisions in JSON format
  cub revision list --space my-space -o json my-ns

  # List revisions using unit ID instead of slug
  cub revision list --space my-space --by-unit-id 61f26b06-3c34-4363-8b9d-7d0a7c2b5f1c

  # List revisions with custom JQ filter
  cub revision list --space my-space -o jq='.[].RevisionNum' my-ns

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

// Default columns to display when no custom columns are specified. This is also what drives the
// select list, so it names every field either layout shows -- the wide-only ones included.
var defaultRevisionColumns = []string{"Revision.RevisionNum", "Unit.Slug", "Revision.Source", "ChangeSet.Slug", "Revision.ChangeOrders", "Revision.Tags", "Revision.Description", "Revision.CreatedAt", "User.Username", "Revision.ApplyGates"}

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
	revisionChangeOrderSlug   string
	revisionChangeSetStartTag string
	revisionChangeSetEndTag   string
)

func init() {
	addStandardListFlags(revisionListCmd)
	revisionListCmd.Flags().StringVar(&revisionTagSlug, "tag", "", "filter revisions by tag slug or UUID")
	revisionListCmd.Flags().StringVar(&revisionChangeOrderSlug, "change-order", "", "filter revisions by change order slug, space/slug, or UUID: the revisions the change landed on, in whichever space they are in")
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

	// Handle --change-order flag. A ChangeOrder names a change wherever it went, so this is the
	// query that answers "where has it landed?" -- across every Space when --space is '*'.
	if revisionChangeOrderSlug != "" {
		changeOrder, err := parseEntityIdentifierSingleAsEntity[goclientnew.ChangeOrder](
			revisionChangeOrderSlug,
			EntityTypeChangeOrder,
			"*", // selectParam to get all fields
			apiGetChangeOrderFromSlugInSpace,
			func(co *goclientnew.ChangeOrder) string { return co.ChangeOrderID.String() },
		)
		if err != nil {
			return "", fmt.Errorf("failed to parse change order '%s': %w", revisionChangeOrderSlug, err)
		}
		clauses = append(clauses, fmt.Sprintf("ChangeOrders ? '%s'", changeOrder.ChangeOrderID.String()))
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

// revisionSpaceSlug is the Space a Revision is in, which is what decides whether the entities it
// references need qualifying.
func revisionSpaceSlug(extendedRev *goclientnew.ExtendedRevision) string {
	if extendedRev.Revision != nil && extendedRev.Revision.SpaceSlug != "" {
		return extendedRev.Revision.SpaceSlug
	}
	if extendedRev.Space != nil {
		return extendedRev.Space.Slug
	}
	return ""
}

// qualifySlug prefixes an entity's slug with its Space when that is not the Revision's own Space.
//
// A ChangeOrder's Tags live in the Space the change came from and are placed on the Revisions it
// lands on everywhere else, so a variant's Revision routinely carries a Tag whose name means
// nothing in that variant -- there is no Tag by that name there to look up. Same-Space references
// are left bare, so the qualified ones are the ones worth noticing, and the form is the space/slug
// the CLI already takes on input.
func qualifySlug(slug, spaceSlug, revisionSpaceSlug string) string {
	if slug == "" || spaceSlug == "" || spaceSlug == revisionSpaceSlug {
		return slug
	}
	return spaceSlug + "/" + slug
}

// maxRevisionDescription is where the default layout cuts a change description. Descriptions are
// free text and routinely longer than the rest of the row put together, so the narrow layout shows
// the beginning and -o wide shows all of it.
const maxRevisionDescription = 50

// displayRevisionList renders the table. The narrow layout carries what identifies a Revision and
// what change management put on it -- ChangeSet, ChangeOrders and Tags kept together, since they
// are read as a group -- and leaves the description last, where a long one runs off the end
// without pushing anything else off. -o wide adds when, who, and the gate state, and stops
// truncating the description.
func displayRevisionList(extendedRevisions []*goclientnew.ExtendedRevision) {
	wide := effectiveOutput().Kind == OutputWide
	crossSpace := selectedSpaceID == "*"
	table := tableView()
	if !noheader {
		header := []string{"Num", "Unit"}
		if wide {
			header = append(header, "Time", "User")
		}
		header = append(header, "Source")
		if wide {
			header = append(header, "Apply-Gates")
		}
		header = append(header, "ChangeSet", "ChangeOrders", "Tags", "Description")
		table.SetHeader(header)
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
		// Across Spaces the Unit slug alone does not identify the row: the same Unit exists in the
		// base and in every variant cloned from it, which is exactly what a change order query
		// returns. Qualify it the way the CLI takes it on input.
		revSpace := revisionSpaceSlug(extendedRev)
		if crossSpace && revSpace != "" {
			unit = revSpace + "/" + unit
		}

		// Show ChangeSet slug if available, otherwise ChangeSetID if not nil and not uuid.Nil
		changeSet := ""
		if extendedRev.ChangeSet != nil {
			changeSet = qualifySlug(extendedRev.ChangeSet.Slug, extendedRev.ChangeSet.SpaceSlug, revSpace)
		} else if rev.ChangeSetID != nil && *rev.ChangeSetID != uuid.Nil {
			changeSet = rev.ChangeSetID.String()
		}

		// A Revision can belong to more than one ChangeOrder, since promotion runs at different
		// rates into different Spaces.
		changeOrders := "None"
		if len(extendedRev.ChangeOrders) > 0 {
			var slugs []string
			for _, changeOrder := range extendedRev.ChangeOrders {
				if changeOrder.Slug != "" {
					slugs = append(slugs, qualifySlug(changeOrder.Slug, changeOrder.SpaceSlug, revSpace))
				}
			}
			if len(slugs) > 0 {
				sort.Strings(slugs)
				changeOrders = strings.Join(slugs, ", ")
			}
		} else if len(rev.ChangeOrders) > 0 {
			// Unexpanded, so the IDs are all there is to show.
			var ids []string
			for changeOrderID := range rev.ChangeOrders {
				ids = append(ids, changeOrderID)
			}
			sort.Strings(ids)
			changeOrders = strings.Join(ids, ", ")
		}

		// Resolve tags to slugs
		tags := "None"
		if extendedRev.Tags != nil && len(extendedRev.Tags) > 0 {
			var tagSlugs []string
			for _, tag := range extendedRev.Tags {
				if tag.Slug != "" {
					tagSlugs = append(tagSlugs, qualifySlug(tag.Slug, tag.SpaceSlug, revSpace))
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

		description := rev.Description
		if !wide {
			description = truncateWithEllipsis(description, maxRevisionDescription)
		}

		row := []string{fmt.Sprintf("%d", rev.RevisionNum), unit}
		if wide {
			// The same short form the other list commands use; time.Time.String() carries
			// microseconds and a zone name, which is most of a column for no gain.
			row = append(row, rev.CreatedAt.Format("2006-01-02 15:04:05"), username)
		}
		row = append(row, rev.Source)
		if wide {
			row = append(row, applyGates)
		}
		row = append(row, changeSet, changeOrders, tags, description)
		table.Append(row)
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
	include := "UserID,UnitID,ChangeSetID,Tags,ChangeOrders"
	newParams.Include = &include
	selectValue := handleSelectParameter(selectParam, selectFields, func() string {
		baseFields := []string{"RevisionNum", "RevisionID", "UnitID", "SpaceID", "SpaceSlug", "OrganizationID"}
		return buildSelectList("Revision", nil, include, defaultRevisionColumns, revisionAliases, revisionCustomColumnDependencies, baseFields)
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
	include := "UserID,UnitID,ChangeSetID,Tags,ChangeOrders"
	newParams.Include = &include
	selectValue := handleSelectParameter(selectParam, selectFields, func() string {
		baseFields := []string{"RevisionNum", "RevisionID", "UnitID", "SpaceID", "SpaceSlug", "OrganizationID"}
		return buildSelectList("Revision", nil, include, defaultRevisionColumns, revisionAliases, revisionCustomColumnDependencies, baseFields)
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
