// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"github.com/confighub/sdk/core/cubapi"
	goclientnew "github.com/confighub/sdk/core/openapi/goclient-new"
	"github.com/google/uuid"
	"github.com/spf13/cobra"
)

var tagListCmd = &cobra.Command{
	Use:   "list",
	Short: "List tags",
	Long: getCommandHelp(`List tags you have access to in a space or across all spaces.

Examples:
`+"```"+`
  # List all tags in a space with headers
  cub tag list --space my-space

  # List tags across all spaces (requires --space "*")
  cub tag list --space "*" --where "Labels.version = '1.0'"

  # List tags without headers for scripting
  cub tag list --space my-space --no-headers

  # List tags in JSON format
  cub tag list --space my-space -o json

  # List only tag names
  cub tag list --space my-space --no-headers -o name

  # List tags with specific labels
  cub tag list --space my-space --where "Labels.environment = 'production'"

  # List tags created after a specific date
  cub tag list --space my-space --where "CreatedAt > '2024-01-01'"
`+"```"+`
`, ""),
	Args:        cobra.ExactArgs(0),
	RunE:        tagListCmdRun,
	Annotations: map[string]string{"OrgLevel": ""},
}

// Default columns to display when no custom columns are specified
var defaultTagColumns = []string{"Tag.Slug", "Space.Slug", "ChangeSet.Slug", "Tag.DisplayName", "Tag.CreatedAt"}

// tagListInclude is the Include parameter for tag list queries.
const tagListInclude = "SpaceID,ChangeSetID"

// tagBaseSelectFields are the fields always returned by tag list queries.
var tagBaseSelectFields = []string{"Slug", "TagID", "SpaceID", "OrganizationID"}

// Tag-specific aliases
var tagAliases = map[string]string{
	"Name": "Tag.Slug",
	"ID":   "Tag.TagID",
}

// Tag custom column dependencies
var tagCustomColumnDependencies = map[string][]string{}

func init() {
	addStandardListFlags(tagListCmd)
	tagCmd.AddCommand(tagListCmd)
}

func tagListCmdRun(cmd *cobra.Command, args []string) error {
	filterID, err := parseFilterFlag(filter)
	if err != nil {
		return err
	}

	extendedTags, err := apiListTags(selectedSpaceID, where, selectFields, filterID)
	if err != nil {
		return err
	}

	displayListResults(extendedTags, getTagSlug, displayTagList)
	return nil
}

func getTagSlug(tag *goclientnew.ExtendedTag) string {
	space := ""
	if tag.Space != nil {
		space = tag.Space.Slug
	}
	return prefixedSlug(space, tag.Tag.Slug)
}

func displayTagList(tags []*goclientnew.ExtendedTag) {
	table := tableView()
	if !noheader {
		table.SetHeader([]string{"Name", "Space", "ChangeSet", "Display-Name", "Created-At"})
	}
	for _, t := range tags {
		tag := t.Tag
		spaceSlug := t.Tag.SpaceID.String()
		if t.Space != nil {
			spaceSlug = t.Space.Slug
		} else if selectedSpaceID != "*" {
			spaceSlug = selectedSpaceSlug
		}

		// Show ChangeSet slug if available, or ChangeSetID if not nil and not uuid.Nil
		changeSetDisplay := ""
		if t.ChangeSet != nil {
			changeSetDisplay = t.ChangeSet.Slug
		} else if tag.ChangeSetID != nil && *tag.ChangeSetID != uuid.Nil {
			changeSetDisplay = tag.ChangeSetID.String()
		}

		table.Append([]string{
			tag.Slug,
			spaceSlug,
			changeSetDisplay,
			tag.DisplayName,
			tag.CreatedAt.Format("2006-01-02 15:04:05"),
		})
	}
	table.Render()
}

// apiListTags lists tags via the org-level endpoint, scoped to a single space by
// a SpaceID clause unless spaceID is "*" (list across all spaces).
func apiListTags(spaceID string, whereFilter string, selectParam string, filterParam string) ([]*goclientnew.ExtendedTag, error) {
	where := cubapi.NewWhere(whereFilter)
	if spaceID != "*" {
		where = where.SpaceID(goclientnew.UUID(uuid.MustParse(spaceID)))
	}
	return apiListAllTags(where, selectParam, filterParam)
}

func apiListAllTags(where cubapi.Where, selectParam string, filterParam string) ([]*goclientnew.ExtendedTag, error) {
	selectValue := handleSelectParameter(selectParam, selectFields, func() string {
		return buildSelectList("Tag", nil, tagListInclude, defaultTagColumns, tagAliases, tagCustomColumnDependencies, tagBaseSelectFields)
	})
	return cubapi.ListTags(ctx, cubClient, where, cubapi.ListOpts{
		Select:   cubapi.SelectFields(selectValue),
		Include:  tagListInclude,
		Filter:   filterParam,
		Contains: contains,
	})
}
