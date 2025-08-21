// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"fmt"
	"strconv"

	"github.com/google/uuid"
	"github.com/spf13/cobra"

	goclientnew "github.com/confighub/sdk/openapi/goclient-new"
)

var mutationGetCmd = &cobra.Command{
	Use:   "get <unit-slug> <mutation-num>",
	Short: "Get details about a mutation",
	Args:  cobra.ExactArgs(2),
	Long: `Get detailed information about a specific mutation of a unit.

Examples:
  # Get details about a specific mutation in JSON format
  cub mutation get --space my-space --json my-deployment 3

`,
	RunE: mutationGetCmdRun,
}

func init() {
	addStandardGetFlags(mutationGetCmd)
	mutationCmd.AddCommand(mutationGetCmd)
}

func mutationGetCmdRun(cmd *cobra.Command, args []string) error {
	unit, err := apiGetUnitFromSlug(args[0], "*") // get all fields for now
	if err != nil {
		return err
	}
	num, err := strconv.ParseInt(args[1], 10, 64)
	if err != nil {
		return err
	}
	extendedMutationDetails, err := apiGetMutationFromNumber(num, unit.UnitID.String(), selectFields)
	if err != nil {
		return err
	}

	displayGetResults(extendedMutationDetails, displayExtendedMutationDetails)
	return nil
}

func displayMutationDetails(mutationDetails *goclientnew.Mutation) {
	view := tableView()
	view.Append([]string{"ID", mutationDetails.MutationID.String()})
	view.Append([]string{"Unit ID", mutationDetails.UnitID.String()})
	view.Append([]string{"Revision ID", mutationDetails.RevisionID.String()})
	view.Append([]string{"Mutation Num", fmt.Sprintf("%d", mutationDetails.MutationNum)})
	if mutationDetails.LinkID != nil {
		view.Append([]string{"Link ID", mutationDetails.LinkID.String()})
	}
	view.Append([]string{"Provided Path", mutationDetails.ProvidedPath})
	if mutationDetails.TriggerID != nil {
		view.Append([]string{"Trigger ID", mutationDetails.TriggerID.String()})
	}
	if mutationDetails.FunctionInvocation.FunctionName != "" {
		view.Append([]string{"Function Name", mutationDetails.FunctionInvocation.FunctionName})
		for i := range mutationDetails.FunctionInvocation.Arguments {
			view.Append([]string{fmt.Sprintf("Argument %d", i), fmt.Sprintf("%v", (mutationDetails.FunctionInvocation.Arguments)[i].Value)})
		}
	}
	view.Append([]string{"Space ID", mutationDetails.SpaceID.String()})
	view.Append([]string{"Organization ID", mutationDetails.OrganizationID.String()})
	view.Render()
}

func displayExtendedMutationDetails(extendedMutationDetails *goclientnew.ExtendedMutation) {
	displayMutationDetails(extendedMutationDetails.Mutation)
	// TODO
	// if extendedMutationDetails.Link != nil {
	// 	displayLinkDetails(extendedMutationDetails.Link)
	// }
	if extendedMutationDetails.Trigger != nil {
		displayTriggerDetails(extendedMutationDetails.Trigger)
	}
}

func apiGetMutation(mutationID string, unitID string, selectParam string) (*goclientnew.ExtendedMutation, error) {
	newParams := &goclientnew.GetExtendedMutationParams{}
	include := "RevisionID,LinkID,TargetID"
	newParams.Include = &include
	selectValue := handleSelectParameter(selectParam, selectFields, nil)
	if selectValue != "" && selectValue != "*" {
		newParams.Select = &selectValue
	}
	muteRes, err := cubClientNew.GetExtendedMutationWithResponse(ctx,
		uuid.MustParse(selectedSpaceID),
		uuid.MustParse(unitID),
		uuid.MustParse(mutationID), newParams)
	if IsAPIError(err, muteRes) {
		return nil, InterpretErrorGeneric(err, muteRes)
	}

	mutation := muteRes.JSON200
	if mutation.Mutation.SpaceID.String() != selectedSpaceID {
		return nil, fmt.Errorf("SERVER DIDN'T CHECK: mutation %s not found", mutationID)
	}
	return mutation, nil
}

func apiGetMutationFromNumber(mutationNum int64, unitID string, selectParam string) (*goclientnew.ExtendedMutation, error) {
	// The default for get is "*" rather than auto-selected list columns
	if selectParam == "" {
		selectParam = "*"
	}
	extendedMutations, err := apiListMutations(selectedSpaceID, unitID, fmt.Sprintf("MutationNum = %d", mutationNum), selectParam)
	if err != nil {
		return nil, err
	}
	for _, extendedMutation := range extendedMutations {
		// FIXME: This shouldn't be an int
		if int64(extendedMutation.Mutation.MutationNum) == mutationNum {
			return extendedMutation, nil
		}
	}
	return nil, fmt.Errorf("mutation %d of unit %s not found in space %s", mutationNum, unitID, selectedSpaceSlug)
}
