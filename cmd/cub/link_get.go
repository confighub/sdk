// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"fmt"

	"github.com/confighub/sdk/core/cubapi"
	goclientnew "github.com/confighub/sdk/core/openapi/goclient-new"
	"github.com/google/uuid"
	"github.com/spf13/cobra"
)

var linkGetCmd = &cobra.Command{
	Use:   "get <slug or id>",
	Short: "Get details about a link",
	Args:  cobra.ExactArgs(1),
	Long: getCommandHelp(`Get detailed information about a link in a space including its ID, slug, display name, and the connected units.

Examples:
`+"```"+`
  # Get details about a deployment-to-namespace link
  cub link get --space my-space deployment-to-namespace
`+"```"+`
`, ""),
	RunE: linkGetCmdRun,
}

func init() {
	addStandardGetFlags(linkGetCmd)
	linkCmd.AddCommand(linkGetCmd)
}

func linkGetCmdRun(cmd *cobra.Command, args []string) error {
	linkDetails, err := apiGetExtendedLinkFromSlug(args[0], selectFields) // use select flag
	if err != nil {
		return err
	}

	displayGetResults(linkDetails, displayExtendedLinkDetails)
	return nil
}

func displayLinkDetails(linkDetails *goclientnew.Link) {
	// Create an ExtendedLink wrapper with just the Link set
	extendedLink := &goclientnew.ExtendedLink{
		Link: linkDetails,
		// All other fields (Space, ToSpace, FromUnit, ToUnit, etc.) will be nil, causing Extended display to show IDs
	}
	displayExtendedLinkDetails(extendedLink)
}

func displayExtendedLinkDetails(extendedLink *goclientnew.ExtendedLink) {
	linkDetails := extendedLink.Link
	view := tableView()
	view.Append([]string{"ID", linkDetails.LinkID.String()})
	view.Append([]string{"Name", linkDetails.Slug})

	// Show Space slug instead of Space ID when available
	if extendedLink.Space != nil {
		view.Append([]string{"Space", extendedLink.Space.Slug})
	} else {
		view.Append([]string{"Space ID", linkDetails.SpaceID.String()})
	}

	view.Append([]string{"Created At", linkDetails.CreatedAt.String()})
	view.Append([]string{"Updated At", linkDetails.UpdatedAt.String()})
	view.Append([]string{"Labels", labelsToString(linkDetails.Labels)})
	view.Append([]string{"Delete Gates", deleteGatesToString(linkDetails.DeleteGates)})
	view.Append([]string{"Annotations", annotationsToString(linkDetails.Annotations)})
	view.Append([]string{"Organization ID", linkDetails.OrganizationID.String()})

	// Show related units by slug when available
	if extendedLink.FromUnit != nil {
		view.Append([]string{"From Unit", extendedLink.FromUnit.Slug})
	} else {
		view.Append([]string{"From Unit ID", linkDetails.FromUnitID.String()})
	}

	if extendedLink.ToUnit != nil {
		view.Append([]string{"To Unit", extendedLink.ToUnit.Slug})
	} else {
		view.Append([]string{"To Unit ID", linkDetails.ToUnitID.String()})
	}

	// Show To Space slug when available
	if extendedLink.ToSpace != nil {
		view.Append([]string{"To Space", extendedLink.ToSpace.Slug})
	} else {
		view.Append([]string{"To Space ID", linkDetails.ToSpaceID.String()})
	}

	if linkDetails.UpdateType != "" {
		view.Append([]string{"Update Type", linkDetails.UpdateType})
	}
	if linkDetails.AutoUpdate {
		view.Append([]string{"Auto Update", fmt.Sprintf("%t", linkDetails.AutoUpdate)})
	}
	if linkDetails.UseLiveState {
		view.Append([]string{"Use Live State", fmt.Sprintf("%t", linkDetails.UseLiveState)})
	}
	if linkDetails.WhereMutation != "" {
		view.Append([]string{"Where Mutation", linkDetails.WhereMutation})
	}
	if linkDetails.WhereResource != "" {
		view.Append([]string{"Where Resource", linkDetails.WhereResource})
	}
	if linkDetails.UpstreamLastMergedRevisionNum != 0 {
		view.Append([]string{"Upstream Last Merged Rev", fmt.Sprintf("%d", linkDetails.UpstreamLastMergedRevisionNum)})
	}
	if linkDetails.DownstreamLastMergedRevisionNum != 0 {
		view.Append([]string{"Downstream Last Merged Rev", fmt.Sprintf("%d", linkDetails.DownstreamLastMergedRevisionNum)})
	}
	if linkDetails.UpstreamSpaceID != nil {
		view.Append([]string{"Upstream Space ID", linkDetails.UpstreamSpaceID.String()})
	}
	if linkDetails.UpstreamLinkID != nil {
		view.Append([]string{"Upstream Link ID", linkDetails.UpstreamLinkID.String()})
	}
	if linkDetails.Bindings != nil && len(*linkDetails.Bindings) > 0 {
		view.Append([]string{"Bindings", fmt.Sprintf("%d bindings", len(*linkDetails.Bindings))})
	}

	view.Render()

	if linkDetails.Bindings != nil && len(*linkDetails.Bindings) > 0 && verbose {
		tprintRaw("")
		tprintRaw("Bindings:")
		tprintRaw("---------")
		displayJSON(linkDetails.Bindings)
	}
}

func apiGetLink(linkID string, selectParam string) (*goclientnew.Link, error) {
	extendedLink, err := apiGetExtendedLink(linkID, selectParam)
	if err != nil {
		return nil, err
	}
	return extendedLink.Link, nil
}

func apiGetExtendedLink(linkID string, selectParam string) (*goclientnew.ExtendedLink, error) {
	newParams := &goclientnew.GetLinkParams{}
	include := "SpaceID,ToSpaceID,FromUnitID,ToUnitID"
	newParams.Include = &include
	selectValue := handleSelectParameter(selectParam, selectFields, nil)
	if selectValue != "" && selectValue != "*" {
		newParams.Select = &selectValue
	}
	linkRes, err := cubClientNew.GetLinkWithResponse(ctx,
		uuid.MustParse(selectedSpaceID), uuid.MustParse(linkID), newParams)
	if cubapi.IsAPIError(err, linkRes) {
		return nil, cubapi.InterpretErrorGeneric(err, linkRes)
	}
	return linkRes.JSON200, nil
}

func apiGetLinkFromSlug(slug string, selectParam string) (*goclientnew.Link, error) {
	return apiGetLinkFromSlugInSpace(slug, selectedSpaceID, selectParam)
}

func apiGetLinkFromSlugInSpace(slug string, spaceID string, selectParam string) (*goclientnew.Link, error) {
	id, err := uuid.Parse(slug)
	if err == nil {
		return apiGetLink(id.String(), selectParam)
	}
	// The default for get is "*" rather than auto-selected list columns
	if selectParam == "" {
		selectParam = "*"
	}
	links, err := apiListLinks(spaceID, "Slug = '"+slug+"'", selectParam, "")
	if err != nil {
		return nil, err
	}
	for _, extendedLink := range links {
		if extendedLink.Link.Slug == slug {
			return extendedLink.Link, nil
		}
	}

	// Get space slug for error message
	spaceSlug := spaceID
	if spaceUUID, err := uuid.Parse(spaceID); err == nil {
		if space, err := apiGetSpace(spaceUUID.String(), "Slug"); err == nil && space != nil {
			spaceSlug = space.Slug
		}
	}
	return nil, fmt.Errorf("link %s not found in space %s", slug, spaceSlug)
}

func apiGetExtendedLinkFromSlug(slug string, selectParam string) (*goclientnew.ExtendedLink, error) {
	id, err := uuid.Parse(slug)
	if err == nil {
		return apiGetExtendedLink(id.String(), selectParam)
	}
	// The default for get is "*" rather than auto-selected list columns
	if selectParam == "" {
		selectParam = "*"
	}
	links, err := apiListLinks(selectedSpaceID, "Slug = '"+slug+"'", selectParam, "")
	if err != nil {
		return nil, err
	}
	for _, extendedLink := range links {
		if extendedLink.Link.Slug == slug {
			return extendedLink, nil
		}
	}
	return nil, fmt.Errorf("link %s not found in space %s", slug, selectedSpaceID)
}
