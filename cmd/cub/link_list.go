// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	goclientnew "github.com/confighub/sdk/openapi/goclient-new"
	"github.com/google/uuid"
	"github.com/spf13/cobra"
)

var linkListCmd = &cobra.Command{
	Use:   "list",
	Short: "List links",
	Long: `List links you have access to in a space or across all spaces. The output includes slugs, source unit slugs (From-Unit), target unit slugs (To-Unit), and target space slugs (To-Space).

Examples:
  # List all links in a space
  cub link list --space my-space

  # List links across all spaces (requires --space "*")
  cub link list --space "*" --where "DisplayName = 'app-to-db'"

  # List links without headers for scripting
  cub link list --space my-space --no-header

  # List only link names
  cub link list --space my-space --no-header --names

  # List links in JSON format
  cub link list --space my-space --json

  # List links with custom JQ filter
  cub link list --space my-space --quiet --jq ".[].Slug"

  # List links to a specific unit
  cub link list --space my-space --where "ToUnitID = 'c871ca3a-d9ca-4eeb-a576-79c3b5a2ca97'"

  # List links from a specific unit
  cub link list --space my-space --where "FromUnitID = 'c871ca3a-d9ca-4eeb-a576-79c3b5a2ca97'"

  # List cross-space links across all spaces  
  cub link list --space "*" --where "ToSpaceID != SpaceID"`,
	Args:        cobra.ExactArgs(0),
	RunE:        linkListCmdRun,
	Annotations: map[string]string{"OrgLevel": ""},
}

// Default columns to display when no custom columns are specified
var defaultLinkColumns = []string{"Link.Slug", "Space.Slug", "FromUnit.Slug", "ToUnit.Slug", "ToSpace.Slug"}

// Link-specific aliases
var linkAliases = map[string]string{
	"Name": "Link.Slug",
	"ID":   "Link.LinkID",
}

// Link custom column dependencies
var linkCustomColumnDependencies = map[string][]string{}

func init() {
	addStandardListFlags(linkListCmd)
	linkCmd.AddCommand(linkListCmd)
}

func linkListCmdRun(cmd *cobra.Command, args []string) error {
	var links []*goclientnew.ExtendedLink
	var err error

	if selectedSpaceID == "*" {
		links, err = apiSearchLinks(where, selectFields)
		if err != nil {
			return err
		}
	} else {
		links, err = apiListLinks(selectedSpaceID, where, selectFields)
		if err != nil {
			return err
		}
	}

	displayListResults(links, getLinkSlug, displayLinkList)
	return nil
}

func getLinkSlug(extendedLink *goclientnew.ExtendedLink) string {
	return extendedLink.Link.Slug
}

func displayLinkList(extendedLinks []*goclientnew.ExtendedLink) {
	table := tableView()
	if !noheader {
		table.SetHeader([]string{"Name", "Space", "From-Unit", "To-Unit", "To-Space"})
	}
	for _, extendedLink := range extendedLinks {
		link := extendedLink.Link
		space := ""
		if extendedLink.Space != nil {
			space = extendedLink.Space.Slug
		}
		fromUnitSlug := ""
		if extendedLink.FromUnit != nil {
			fromUnitSlug = extendedLink.FromUnit.Slug
		}
		toUnitSlug := ""
		if extendedLink.ToUnit != nil {
			toUnitSlug = extendedLink.ToUnit.Slug
		}
		toSpaceSlug := ""
		if extendedLink.ToSpace != nil {
			toSpaceSlug = extendedLink.ToSpace.Slug
		} else if link.ToSpaceID.String() == selectedSpaceID {
			toSpaceSlug = selectedSpaceSlug
		}
		table.Append([]string{
			link.Slug,
			space,
			fromUnitSlug,
			toUnitSlug,
			toSpaceSlug,
		})
	}
	table.Render()
}

func apiSearchLinks(whereFilter string, selectParam string) ([]*goclientnew.ExtendedLink, error) {
	params := &goclientnew.SearchListLinksParams{}
	if whereFilter != "" {
		params.Where = &whereFilter
	}
	if contains != "" {
		params.Contains = &contains
	}

	include := "SpaceID,FromUnitID,ToUnitID,ToSpaceID"
	params.Include = &include

	selectValue := handleSelectParameter(selectParam, selectFields, func() string {
		baseFields := []string{"Slug", "LinkID", "SpaceID", "OrganizationID"}
		return buildSelectList("Link", "", include, defaultLinkColumns, linkAliases, linkCustomColumnDependencies, baseFields)
	})
	if selectValue != "" && selectValue != "*" {
		params.Select = &selectValue
	}

	res, err := cubClientNew.SearchListLinks(ctx, params)
	if err != nil {
		return nil, err
	}
	linkRes, err := goclientnew.ParseSearchListLinksResponse(res)
	if IsAPIError(err, linkRes) {
		return nil, InterpretErrorGeneric(err, linkRes)
	}

	extendedLinks := make([]*goclientnew.ExtendedLink, 0, len(*linkRes.JSON200))
	for _, extendedLink := range *linkRes.JSON200 {
		extendedLinks = append(extendedLinks, &extendedLink)
	}

	return extendedLinks, nil
}

func apiListLinks(spaceID string, whereFilter string, selectParam string) ([]*goclientnew.ExtendedLink, error) {
	// TODO: update List APIs to allow where filter
	newParams := &goclientnew.ListLinksParams{}
	if whereFilter != "" {
		newParams.Where = &whereFilter
	}
	if contains != "" {
		newParams.Contains = &contains
	}
	include := "SpaceID,FromUnitID,ToUnitID,ToSpaceID"
	newParams.Include = &include
	selectValue := handleSelectParameter(selectParam, selectFields, func() string {
		baseFields := []string{"Slug", "LinkID", "SpaceID", "OrganizationID"}
		return buildSelectList("Link", "", include, defaultLinkColumns, linkAliases, linkCustomColumnDependencies, baseFields)
	})
	if selectValue != "" && selectValue != "*" {
		newParams.Select = &selectValue
	}
	linkRes, err := cubClientNew.ListLinksWithResponse(ctx, uuid.MustParse(spaceID), newParams)
	if IsAPIError(err, linkRes) {
		return nil, InterpretErrorGeneric(err, linkRes)
	}

	links := make([]*goclientnew.ExtendedLink, 0, len(*linkRes.JSON200))
	for _, extendedLink := range *linkRes.JSON200 {
		if extendedLink.Link == nil {
			continue
		}
		links = append(links, &extendedLink)
	}
	return links, nil
}
