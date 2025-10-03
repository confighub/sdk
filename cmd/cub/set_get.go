// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

// Not implemented yet
// import (
// 	"fmt"

// 	"github.com/confighub/sdk/cubapi"
// 	goclientnew "github.com/confighub/sdk/openapi/goclient-new"
// 	"github.com/google/uuid"
// 	"github.com/spf13/cobra"
// )

// var setGetCmd = &cobra.Command{
// 	Use:   "get <slug or id>",
// 	Short: "Get details about a set",
// 	Args:  cobra.ExactArgs(1),
// 	Long: `Get detailed information about a set in a space including its ID, slug, display name, and organization details.

// Examples:
//   # Get details about a set
//   cub set get --space my-space --json my-set

// `,
// 	RunE: setGetCmdRun,
// }

// func init() {
// 	addStandardGetFlags(setGetCmd)
// 	setCmd.AddCommand(setGetCmd)
// }

// func setGetCmdRun(cmd *cobra.Command, args []string) error {
// 	setDetails, err := apiGetExtendedSetFromSlug(args[0], selectFields)
// 	if err != nil {
// 		return err
// 	}

// 	displayGetResults(setDetails, displayExtendedSetDetails)
// 	return nil
// }

// func displaySetDetails(setDetails *goclientnew.Set) {
// 	// Create an ExtendedSet wrapper with just the Set set
// 	extendedSet := &goclientnew.ExtendedSet{
// 		Set: setDetails,
// 		// All other fields (Space, etc.) will be nil, causing Extended display to show IDs
// 	}
// 	displayExtendedSetDetails(extendedSet)
// }

// func displayExtendedSetDetails(extendedSet *goclientnew.ExtendedSet) {
// 	setDetails := extendedSet.Set
// 	view := tableView()
// 	view.Append([]string{"ID", setDetails.SetID.String()})
// 	view.Append([]string{"Name", setDetails.Slug})

// 	// Show Space slug instead of Space ID when available
// 	if extendedSet.Space != nil {
// 		view.Append([]string{"Space", extendedSet.Space.Slug})
// 	} else {
// 		view.Append([]string{"Space ID", setDetails.SpaceID.String()})
// 	}

// 	view.Append([]string{"Created At", setDetails.CreatedAt.String()})
// 	view.Append([]string{"Updated At", setDetails.UpdatedAt.String()})
// 	view.Append([]string{"Labels", labelsToString(setDetails.Labels)})
// 	view.Append([]string{"Delete Gates", deleteGatesToString(setDetails.DeleteGates)})
// 	view.Append([]string{"Annotations", annotationsToString(setDetails.Annotations)})
// 	view.Append([]string{"Organization ID", setDetails.OrganizationID.String()})
// 	view.Render()
// }

// func apiGetSet(setID string, selectParam string) (*goclientnew.Set, error) {
// 	extendedSet, err := apiGetExtendedSet(setID, selectParam)
// 	if err != nil {
// 		return nil, err
// 	}
// 	return extendedSet.Set, nil
// }

// func apiGetExtendedSet(setID string, selectParam string) (*goclientnew.ExtendedSet, error) {
// 	newParams := goclientnew.GetSetParams{}
// 	include := "SpaceID"
// 	newParams.Include = &include
// 	selectValue := handleSelectParameter(selectParam, selectFields, nil)
// 	if selectValue != "" && selectValue != "*" {
// 		newParams.Select = &selectValue
// 	}
// 	setDetails, err := cubClientNew.GetSetWithResponse(ctx, uuid.MustParse(selectedSpaceID), uuid.MustParse(setID), &newParams)
// 	if cubapi.IsAPIError(err, setDetails) {
// 		return nil, cubapi.InterpretErrorGeneric(err, setDetails)
// 	}
// 	return setDetails.JSON200, nil
// }

// func apiGetSetFromSlug(slug string, selectParam string) (*goclientnew.Set, error) {
// 	return apiGetSetFromSlugInSpace(slug, selectedSpaceID, selectParam)
// }

// func apiGetSetFromSlugInSpace(slug string, spaceID string, selectParam string) (*goclientnew.Set, error) {
// 	id, err := uuid.Parse(slug)
// 	if err == nil {
// 		return apiGetSet(id.String(), selectParam)
// 	}
// 	// The default for get is "*" rather than auto-selected list columns
// 	if selectParam == "" {
// 		selectParam = "*"
// 	}
// 	sets, err := apiListSets(spaceID, "Slug = '"+slug+"'", selectParam, "")
// 	if err != nil {
// 		return nil, err
// 	}
// 	for _, set := range sets {
// 		if set.Slug == slug {
// 			return set, nil
// 		}
// 	}

// 	// Get space slug for error message
// 	spaceSlug := spaceID
// 	if spaceUUID, err := uuid.Parse(spaceID); err == nil {
// 		if space, err := apiGetSpace(spaceUUID.String(), "Slug"); err == nil && space != nil {
// 			spaceSlug = space.Slug
// 		}
// 	}
// 	return nil, fmt.Errorf("set %s not found in space %s", slug, spaceSlug)
// }

// func apiGetExtendedSetFromSlug(slug string, selectParam string) (*goclientnew.ExtendedSet, error) {
// 	id, err := uuid.Parse(slug)
// 	if err == nil {
// 		return apiGetExtendedSet(id.String(), selectParam)
// 	}
// 	// The default for get is "*" rather than auto-selected list columns
// 	if selectParam == "" {
// 		selectParam = "*"
// 	}
// 	sets, err := apiListSets(selectedSpaceID, "Slug = '"+slug+"'", selectParam, "")
// 	if err != nil {
// 		return nil, err
// 	}
// 	for _, set := range sets {
// 		if set.Slug == slug {
// 			return apiGetExtendedSet(set.SetID.String(), selectParam)
// 		}
// 	}
// 	return nil, fmt.Errorf("set %s not found in space %s", slug, selectedSpaceID)
// }
