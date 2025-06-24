// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"fmt"

	goclientnew "github.com/confighub/sdk/openapi/goclient-new"
	"github.com/google/uuid"
	"github.com/spf13/cobra"
)

var setGetCmd = &cobra.Command{
	Use:   "get <slug or id>",
	Short: "Get details about a set",
	Args:  cobra.ExactArgs(1),
	Long: `Get detailed information about a set in a space including its ID, slug, display name, and organization details.

Examples:
  # Get details about a set
  cub set get --space my-space --json my-set

  # Get extended information about a set
  cub set get --space my-space --json --extended my-set`,
	RunE: setGetCmdRun,
}

func init() {
	addStandardGetFlags(setGetCmd)
	setCmd.AddCommand(setGetCmd)
}

func setGetCmdRun(cmd *cobra.Command, args []string) error {
	setDetails, err := apiGetSetFromSlug(args[0])
	if err != nil {
		return err
	}
	if extended {
		setExtended, err := apiGetSetExtended(setDetails.SetID.String())
		if err != nil {
			return err
		}
		displayGetResults(setExtended, displaySetExtendedDetails)
		return nil
	}

	// the previous call got the list resource. We want the "detail" resource just in case they're different
	setDetails, err = apiGetSet(setDetails.SetID.String())
	if err != nil {
		return err
	}
	displayGetResults(setDetails, displaySetDetails)
	return nil
}

func displaySetDetails(setDetails *goclientnew.Set) {
	view := tableView()
	view.Append([]string{"ID", setDetails.SetID.String()})
	view.Append([]string{"Slug", setDetails.Slug})
	view.Append([]string{"Display Name", setDetails.DisplayName})
	view.Append([]string{"Space ID", setDetails.SpaceID.String()})
	view.Append([]string{"Created At", setDetails.CreatedAt.String()})
	view.Append([]string{"Updated At", setDetails.UpdatedAt.String()})
	view.Append([]string{"Labels", labelsToString(setDetails.Labels)})
	view.Append([]string{"Annotations", annotationsToString(setDetails.Annotations)})
	view.Append([]string{"Organization ID", setDetails.OrganizationID.String()})
	view.Render()
}

func displaySetExtendedDetails(setExtendedDetails *goclientnew.SetExtended) {
	displaySetDetails(setExtendedDetails.Set)
}

func apiGetSet(setID string) (*goclientnew.Set, error) {
	newParams := goclientnew.GetSetParams{}
	// if whereFilter != "" {
	// 	whereFilter = url.QueryEscape(whereFilter)
	// 	newParams.Where = &whereFilter
	// }
	setDetails, err := cubClientNew.GetSetWithResponse(ctx, uuid.MustParse(selectedSpaceID), uuid.MustParse(setID), &newParams)
	if IsAPIError(err, setDetails) {
		return nil, InterpretErrorGeneric(err, setDetails)
	}

	if setDetails.JSON200 == nil || setDetails.JSON200.Set == nil {
		return nil, fmt.Errorf("unexpected response: %v", setDetails)
	}
	return setDetails.JSON200.Set, nil
}

func apiGetSetExtended(setID string) (*goclientnew.SetExtended, error) {
	res, err := cubClientNew.GetSetExtended(ctx, uuid.MustParse(selectedSpaceID), uuid.MustParse(setID))
	if err != nil {
		return nil, err
	}
	setDetails, err := goclientnew.ParseGetSetExtendedResponse(res)
	if IsAPIError(err, setDetails) {
		return nil, InterpretErrorGeneric(err, setDetails)
	}
	return setDetails.JSON200, nil
}

func apiGetSetFromSlug(slug string) (*goclientnew.Set, error) {
	id, err := uuid.Parse(slug)
	if err == nil {
		return apiGetSet(id.String())
	}
	sets, err := apiListSets(selectedSpaceID, "Slug = '"+slug+"'")
	if err != nil {
		return nil, err
	}
	for _, set := range sets {
		if set.Slug == slug {
			return set, nil
		}
	}
	return nil, fmt.Errorf("set %s not found in space %s", slug, selectedSpaceSlug)
}
