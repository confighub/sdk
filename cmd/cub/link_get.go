// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"fmt"

	goclientnew "github.com/confighub/sdk/openapi/goclient-new"
	"github.com/google/uuid"
	"github.com/spf13/cobra"
)

var linkGetCmd = &cobra.Command{
	Use:   "get <slug or id>",
	Short: "Get details about a link",
	Args:  cobra.ExactArgs(1),
	Long: `Get detailed information about a link in a space including its ID, slug, display name, and the connected units.

Examples:
  # Get details about a deployment-to-namespace link
  cub link get --space my-space deployment-to-namespace

`,
	RunE: linkGetCmdRun,
}

func init() {
	addStandardGetFlags(linkGetCmd)
	linkCmd.AddCommand(linkGetCmd)
}

func linkGetCmdRun(cmd *cobra.Command, args []string) error {
	linkDetails, err := apiGetLinkFromSlug(args[0])
	if err != nil {
		return err
	}

	// the previous call got the list resource. We want the "detail" resource just in case they're different
	linkDetails, err = apiGetLink(linkDetails.LinkID.String())
	if err != nil {
		return err
	}
	displayGetResults(linkDetails, displayLinkDetails)
	return nil
}

func displayLinkDetails(linkDetails *goclientnew.Link) {
	view := tableView()
	view.Append([]string{"ID", linkDetails.LinkID.String()})
	view.Append([]string{"Name", linkDetails.Slug})
	view.Append([]string{"Space ID", linkDetails.SpaceID.String()})
	view.Append([]string{"Created At", linkDetails.CreatedAt.String()})
	view.Append([]string{"Updated At", linkDetails.UpdatedAt.String()})
	view.Append([]string{"Labels", labelsToString(linkDetails.Labels)})
	view.Append([]string{"Annotations", annotationsToString(linkDetails.Annotations)})
	view.Append([]string{"Organization ID", linkDetails.OrganizationID.String()})
	view.Append([]string{"From Unit ID", linkDetails.FromUnitID.String()})
	view.Append([]string{"To Unit ID", linkDetails.ToUnitID.String()})
	view.Append([]string{"To Space ID", linkDetails.ToSpaceID.String()})
	view.Render()
}

func apiGetLink(linkID string) (*goclientnew.Link, error) {
	newParams := &goclientnew.GetLinkParams{}
	linkRes, err := cubClientNew.GetLinkWithResponse(ctx,
		uuid.MustParse(selectedSpaceID), uuid.MustParse(linkID), newParams)
	if IsAPIError(err, linkRes) {
		return nil, InterpretErrorGeneric(err, linkRes)
	}
	return linkRes.JSON200.Link, nil
}

func apiGetLinkFromSlug(slug string) (*goclientnew.Link, error) {
	id, err := uuid.Parse(slug)
	if err == nil {
		return apiGetLink(id.String())
	}
	links, err := apiListLinks(selectedSpaceID, "Slug = '"+slug+"'")
	if err != nil {
		return nil, err
	}
	for _, extendedLink := range links {
		if extendedLink.Link.Slug == slug {
			return extendedLink.Link, nil
		}
	}
	return nil, fmt.Errorf("link %s not found in space %s", slug, selectedSpaceSlug)
}
