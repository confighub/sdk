// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"net/url"

	goclientnew "github.com/confighub/sdk/openapi/goclient-new"
	"github.com/google/uuid"
	"github.com/spf13/cobra"
)

var linkListCmd = &cobra.Command{
	Use:   "list",
	Short: "List links",
	Long: `List links you have access to in a space. The output includes display names, slugs, link IDs, source unit IDs (From-ID), target unit IDs (To-ID), and target space IDs.

Examples:
  # List all links in a space
  cub link list --space my-space

  # List links without headers for scripting
  cub link list --space my-space --noheader

  # List only link slugs
  cub link list --space my-space --noheader --slugs-only

  # List links in JSON format
  cub link list --space my-space --json

  # List links with custom JQ filter
  cub link list --space my-space --quiet --jq ".[].LinkID"

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

func getLinkSlug(link *goclientnew.Link) string {
	return link.Slug
}

func displayLinkList(links []*goclientnew.Link) {
	table := tableView()
	if !noheader {
		table.SetHeader([]string{"Display-Name", "Slug", "ID", "From-ID", "To-ID", "To-Space-ID"})
	}
	for _, link := range links {
		table.Append([]string{
			link.DisplayName,
			link.Slug,
			link.LinkID.String(),
			link.FromUnitID.String(),
			link.ToUnitID.String(),
			link.ToSpaceID.String(),
		})
	}
	table.Render()
}

func apiListLinks(spaceID string, whereFilter string) ([]*goclientnew.Link, error) {
	// TODO: update List APIs to allow where filter
	newParams := &goclientnew.ListLinksParams{}
	if whereFilter != "" {
		whereFilter = url.QueryEscape(whereFilter)
		newParams.Where = &whereFilter
	}
	linkRes, err := cubClientNew.ListLinksWithResponse(ctx, uuid.MustParse(spaceID), newParams)
	if IsAPIError(err, linkRes) {
		return nil, InterpretErrorGeneric(err, linkRes)
	}

	links := make([]*goclientnew.Link, 0, len(*linkRes.JSON200))
	for _, link := range *linkRes.JSON200 {
		if link.Link == nil {
			continue
		}
		links = append(links, link.Link)
	}
	return links, nil
}
