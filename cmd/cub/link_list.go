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
	Long: `List links you have access to in a space. The output includes slugs, source unit slugs (From-Unit), target unit slugs (To-Unit), and target space slugs (To-Space).

Examples:
  # List all links in a space
  cub link list --space my-space

  # List links without headers for scripting
  cub link list --space my-space --no-header

  # List only link slugs
  cub link list --space my-space --no-header --slugs

  # List links in JSON format
  cub link list --space my-space --json

  # List links with custom JQ filter
  cub link list --space my-space --quiet --jq ".[].Slug"

  # List links to a specific unit
  cub link list --space my-space --where "ToUnitID = 'c871ca3a-d9ca-4eeb-a576-79c3b5a2ca97'"

  # List links from a specific unit
  cub link list --space my-space --where "FromUnitID = 'c871ca3a-d9ca-4eeb-a576-79c3b5a2ca97'"

  # List cross-space links
  cub link list --space my-space --where "ToSpaceID != SpaceID"`,
	Args: cobra.ExactArgs(0),
	RunE: linkListCmdRun,
}

func init() {
	addStandardListFlags(linkListCmd)
	linkCmd.AddCommand(linkListCmd)
}

func linkListCmdRun(cmd *cobra.Command, args []string) error {
	links, err := apiListLinks(selectedSpaceID, where)
	if err != nil {
		return err
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
		table.SetHeader([]string{"Slug", "From-Unit", "To-Unit", "To-Space"})
	}
	for _, extendedLink := range extendedLinks {
		link := extendedLink.Link
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
			fromUnitSlug,
			toUnitSlug,
			toSpaceSlug,
		})
	}
	table.Render()
}

func apiListLinks(spaceID string, whereFilter string) ([]*goclientnew.ExtendedLink, error) {
	// TODO: update List APIs to allow where filter
	newParams := &goclientnew.ListLinksParams{}
	if whereFilter != "" {
		newParams.Where = &whereFilter
	}
	include := "FromUnitID,ToUnitID,ToSpaceID"
	newParams.Include = &include
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
