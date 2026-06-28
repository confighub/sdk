// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"github.com/confighub/sdk/core/cubapi"
	goclientnew "github.com/confighub/sdk/core/openapi/goclient-new"
	"github.com/google/uuid"
	"github.com/spf13/cobra"
)

var linkListCmd = &cobra.Command{
	Use:   "list",
	Short: "List links",
	Long: getCommandHelp(`List links you have access to in a space or across all spaces. The output includes slugs, source unit slugs (From-Unit), target unit slugs (To-Unit), and target space slugs (To-Space).

Examples:
`+"```"+`
  # List all links in a space
  cub link list --space my-space

  # List links across all spaces (requires --space "*")
  cub link list --space "*" --where "DisplayName = 'app-to-db'"

  # List links without headers for scripting
  cub link list --space my-space --no-headers

  # List only link names
  cub link list --space my-space --no-headers -o name

  # List links in JSON format
  cub link list --space my-space -o json

  # List links with custom JQ filter
  cub link list --space my-space --quiet -o jq=".[].Slug"

  # List links to a specific unit
  cub link list --space my-space --where "ToUnitID = 'c871ca3a-d9ca-4eeb-a576-79c3b5a2ca97'"

  # List links from a specific unit
  cub link list --space my-space --where "FromUnitID = 'c871ca3a-d9ca-4eeb-a576-79c3b5a2ca97'"

  # List cross-space links across all spaces
  cub link list --space "*" --where "ToSpaceID != SpaceID"
`+"```"+`
`, ""),
	Args:        cobra.ExactArgs(0),
	RunE:        linkListCmdRun,
	Annotations: map[string]string{"OrgLevel": ""},
}

// Default columns to display when no custom columns are specified
var defaultLinkColumns = []string{"Link.Slug", "Space.Slug", "FromUnit.Slug", "ToUnit.Slug", "ToSpace.Slug", "Link.UpdateType", "Link.AutoUpdate", "Link.UseLiveState", "Link.UpstreamLinkID"}

// linkListInclude is the Include parameter for link list queries.
const linkListInclude = "SpaceID,FromUnitID,ToUnitID,ToSpaceID"

// linkBaseSelectFields are the fields always returned by link list queries.
var linkBaseSelectFields = []string{"Slug", "LinkID", "SpaceID", "OrganizationID"}

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
	filterID, err := parseFilterFlag(filter)
	if err != nil {
		return err
	}

	links, err := apiListLinks(selectedSpaceID, where, selectFields, filterID)
	if err != nil {
		return err
	}

	displayListResults(links, getLinkSlug, displayLinkList)
	return nil
}

func getLinkSlug(extendedLink *goclientnew.ExtendedLink) string {
	space := ""
	if extendedLink.Space != nil {
		space = extendedLink.Space.Slug
	}
	return prefixedSlug(space, extendedLink.Link.Slug)
}

func displayLinkList(extendedLinks []*goclientnew.ExtendedLink) {
	table := tableView()
	if !noheader {
		table.SetHeader([]string{"Name", "Space", "From-Unit", "To-Unit", "To-Space", "Update-Type", "Auto-Update", "Use-Live-State", "Upstream-Link-ID"})
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
		autoUpdate := ""
		if link.AutoUpdate {
			autoUpdate = "true"
		}
		useLiveState := ""
		if link.UseLiveState {
			useLiveState = "true"
		}
		upstreamLinkID := ""
		if link.UpstreamLinkID != nil {
			upstreamLinkID = link.UpstreamLinkID.String()
		}
		table.Append([]string{
			link.Slug,
			space,
			fromUnitSlug,
			toUnitSlug,
			toSpaceSlug,
			link.UpdateType,
			autoUpdate,
			useLiveState,
			upstreamLinkID,
		})
	}
	table.Render()
}

// apiListLinks lists links via the org-level endpoint, scoped to a single space
// by a SpaceID clause unless spaceID is "*" (list across all spaces).
func apiListLinks(spaceID string, whereFilter string, selectParam string, filterParam string) ([]*goclientnew.ExtendedLink, error) {
	where := cubapi.NewWhere(whereFilter)
	if spaceID != "*" {
		where = where.SpaceID(goclientnew.UUID(uuid.MustParse(spaceID)))
	}
	return apiListAllLinks(where, selectParam, filterParam)
}

func apiListAllLinks(where cubapi.Where, selectParam string, filterParam string) ([]*goclientnew.ExtendedLink, error) {
	selectValue := handleSelectParameter(selectParam, selectFields, func() string {
		return buildSelectList("Link", nil, linkListInclude, defaultLinkColumns, linkAliases, linkCustomColumnDependencies, linkBaseSelectFields)
	})
	return cubapi.ListLinks(ctx, cubClient, where, cubapi.ListOpts{
		Select:   cubapi.SelectFields(selectValue),
		Include:  linkListInclude,
		Filter:   filterParam,
		Contains: contains,
	})
}
