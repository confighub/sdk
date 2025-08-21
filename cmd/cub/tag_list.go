// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	goclientnew "github.com/confighub/sdk/openapi/goclient-new"
	"github.com/google/uuid"
	"github.com/spf13/cobra"
)

var tagListCmd = &cobra.Command{
	Use:   "list",
	Short: "List tags",
	Long: `List tags you have access to in a space or across all spaces.

Examples:
  # List all tags in a space with headers
  cub tag list --space my-space

  # List tags across all spaces (requires --space "*")
  cub tag list --space "*" --where "Labels.version = '1.0'"

  # List tags without headers for scripting
  cub tag list --space my-space --no-header

  # List tags in JSON format
  cub tag list --space my-space --json

  # List only tag names
  cub tag list --space my-space --no-header --names

  # List tags with specific labels
  cub tag list --space my-space --where "Labels.environment = 'production'"

  # List tags created after a specific date
  cub tag list --space my-space --where "CreatedAt > '2024-01-01'"`,
	Args:        cobra.ExactArgs(0),
	RunE:        tagListCmdRun,
	Annotations: map[string]string{"OrgLevel": ""},
}

// Default columns to display when no custom columns are specified
var defaultTagColumns = []string{"Tag.Slug", "Space.Slug", "Tag.DisplayName", "Tag.CreatedAt"}

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
	var extendedTags []*goclientnew.ExtendedTag
	var err error

	if selectedSpaceID == "*" {
		extendedTags, err = apiSearchTags(where, selectFields)
		if err != nil {
			return err
		}
	} else {
		extendedTags, err = apiListTags(selectedSpaceID, where, selectFields)
		if err != nil {
			return err
		}
	}

	displayListResults(extendedTags, getTagSlug, displayTagList)
	return nil
}

func getTagSlug(tag *goclientnew.ExtendedTag) string {
	return tag.Tag.Slug
}

func displayTagList(tags []*goclientnew.ExtendedTag) {
	table := tableView()
	if !noheader {
		table.SetHeader([]string{"Name", "Space", "Display-Name", "Created-At"})
	}
	for _, t := range tags {
		tag := t.Tag
		spaceSlug := t.Tag.TagID.String()
		if t.Space != nil {
			spaceSlug = t.Space.Slug
		} else if selectedSpaceID != "*" {
			spaceSlug = selectedSpaceSlug
		}
		table.Append([]string{
			tag.Slug,
			spaceSlug,
			tag.DisplayName,
			tag.CreatedAt.Format("2006-01-02 15:04:05"),
		})
	}
	table.Render()
}

func apiListTags(spaceID string, whereFilter string, selectParam string) ([]*goclientnew.ExtendedTag, error) {
	newParams := &goclientnew.ListTagsParams{}
	include := "SpaceID"
	newParams.Include = &include
	if whereFilter != "" {
		newParams.Where = &whereFilter
	}
	if contains != "" {
		newParams.Contains = &contains
	}
	selectValue := handleSelectParameter(selectParam, selectFields, func() string {
		baseFields := []string{"Slug", "TagID", "SpaceID", "OrganizationID"}
		return buildSelectList("Tag", "", include, defaultTagColumns, tagAliases, tagCustomColumnDependencies, baseFields)
	})
	if selectValue != "" && selectValue != "*" {
		newParams.Select = &selectValue
	}
	tagsRes, err := cubClientNew.ListTagsWithResponse(ctx, uuid.MustParse(spaceID), newParams)
	if IsAPIError(err, tagsRes) {
		return nil, InterpretErrorGeneric(err, tagsRes)
	}

	tags := make([]*goclientnew.ExtendedTag, 0, len(*tagsRes.JSON200))
	for _, tag := range *tagsRes.JSON200 {
		tags = append(tags, &tag)
	}

	return tags, nil
}

func apiSearchTags(whereFilter string, selectParam string) ([]*goclientnew.ExtendedTag, error) {
	newParams := &goclientnew.ListAllTagsParams{}
	if whereFilter != "" {
		newParams.Where = &whereFilter
	}
	if contains != "" {
		newParams.Contains = &contains
	}

	include := "SpaceID"
	newParams.Include = &include

	selectValue := handleSelectParameter(selectParam, selectFields, func() string {
		baseFields := []string{"Slug", "TagID", "SpaceID", "OrganizationID"}
		return buildSelectList("Tag", "", include, defaultTagColumns, tagAliases, tagCustomColumnDependencies, baseFields)
	})
	if selectValue != "" && selectValue != "*" {
		newParams.Select = &selectValue
	}

	res, err := cubClientNew.ListAllTags(ctx, newParams)
	if err != nil {
		return nil, err
	}
	tagsRes, err := goclientnew.ParseListAllTagsResponse(res)
	if IsAPIError(err, tagsRes) {
		return nil, InterpretErrorGeneric(err, tagsRes)
	}

	extendedTags := make([]*goclientnew.ExtendedTag, 0, len(*tagsRes.JSON200))
	for _, tag := range *tagsRes.JSON200 {
		extendedTags = append(extendedTags, &tag)
	}

	return extendedTags, nil
}