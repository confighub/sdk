// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"fmt"
	"strconv"

	"github.com/google/uuid"
	"github.com/spf13/cobra"

	"github.com/confighub/sdk/core/cubapi"
	goclientnew "github.com/confighub/sdk/core/openapi/goclient-new"
)

var mutationGetCmd = &cobra.Command{
	Use:   "get <unit-slug> <mutation-num>",
	Short: "Get details about a mutation",
	Args:  cobra.ExactArgs(2),
	Long: getCommandHelp(`Get detailed information about a specific mutation of a unit.

Examples:
`+"```"+`
  # Get details about a specific mutation in JSON format
  cub mutation get --space my-space --json my-deployment 3
`+"```"+`
`, ""),
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
	// Create an ExtendedMutation wrapper with just the Mutation set
	extendedMutation := &goclientnew.ExtendedMutation{
		Mutation: mutationDetails,
		// All other fields (Space, etc.) will be nil, causing Extended display to show IDs
	}
	displayExtendedMutationDetails(extendedMutation)
}

func displayExtendedMutationDetails(extendedMutationDetails *goclientnew.ExtendedMutation) {
	mutationDetails := extendedMutationDetails.Mutation
	view := tableView()
	view.Append([]string{"ID", mutationDetails.MutationID.String()})
	view.Append([]string{"Mutation Num", fmt.Sprintf("%d", mutationDetails.MutationNum)})

	// Show Unit slug instead of Unit ID when available
	if extendedMutationDetails.Unit != nil {
		view.Append([]string{"Unit", extendedMutationDetails.Unit.Slug})
	} else if mutationDetails.UnitSlug != "" {
		view.Append([]string{"Unit", mutationDetails.UnitSlug})
	} else {
		view.Append([]string{"Unit ID", mutationDetails.UnitID.String()})
	}

	// Show Space slug instead of Space ID when available
	if extendedMutationDetails.Space != nil {
		view.Append([]string{"Space", extendedMutationDetails.Space.Slug})
	} else if mutationDetails.SpaceSlug != "" {
		view.Append([]string{"Space", mutationDetails.SpaceSlug})
	} else {
		view.Append([]string{"Space ID", mutationDetails.SpaceID.String()})
	}

	// Revisions are addressed by number, so that is what identifies the one this Mutation
	// belongs to. The id is shown only when the number is not there to show.
	if mutationDetails.RevisionNum != 0 {
		view.Append([]string{"Revision Num", fmt.Sprintf("%d", mutationDetails.RevisionNum)})
	} else {
		view.Append([]string{"Revision ID", mutationDetails.RevisionID.String()})
	}

	view.Append([]string{"Created At", mutationDetails.CreatedAt.String()})
	view.Append([]string{"Updated At", mutationDetails.UpdatedAt.String()})
	if mutationDetails.Subgroup != "" {
		view.Append([]string{"Subgroup", mutationDetails.Subgroup})
	}

	if mutationDetails.RestoredRevisionNum != 0 {
		view.Append([]string{"Restored Revision Num", fmt.Sprintf("%d", mutationDetails.RestoredRevisionNum)})
	}
	if mutationDetails.UpgradedFromUpstreamRevisionNum != 0 {
		view.Append([]string{"Upgraded From Upstream Revision Num", fmt.Sprintf("%d", mutationDetails.UpgradedFromUpstreamRevisionNum)})
	}
	if extendedMutationDetails.MergeSource != nil {
		view.Append([]string{"Merge Source", extendedMutationDetails.MergeSource.Slug})
	} else if isSetUUID(mutationDetails.MergeSourceID) {
		view.Append([]string{"Merge Source ID", mutationDetails.MergeSourceID.String()})
	}
	if mutationDetails.MergeBaseRevisionNum != 0 {
		view.Append([]string{"Merge Base Revision Num", fmt.Sprintf("%d", mutationDetails.MergeBaseRevisionNum)})
	}
	if mutationDetails.MergeEndRevisionNum != 0 {
		view.Append([]string{"Merge End Revision Num", fmt.Sprintf("%d", mutationDetails.MergeEndRevisionNum)})
	}

	if extendedMutationDetails.Link != nil {
		view.Append([]string{"Link", extendedMutationDetails.Link.Slug})
	} else if isSetUUID(mutationDetails.LinkID) {
		view.Append([]string{"Link ID", mutationDetails.LinkID.String()})
	}
	// The provided resource and path are what a Link resolution satisfied a need from; a
	// Mutation from anything else carries none of them.
	if provided := mutationDetails.ProvidedResource; provided != nil {
		if provided.ResourceType != "" {
			view.Append([]string{"Provided Resource Type", provided.ResourceType})
		}
		if provided.ResourceName != "" {
			view.Append([]string{"Provided Resource Name", provided.ResourceName})
		}
		if provided.ResourceNameStableCore != "" {
			view.Append([]string{"Provided Resource Stable Name", provided.ResourceNameStableCore})
		}
		if provided.ResourceCategory != "" {
			view.Append([]string{"Provided Resource Category", provided.ResourceCategory})
		}
	}
	if mutationDetails.ProvidedPath != "" {
		view.Append([]string{"Provided Path", mutationDetails.ProvidedPath})
	}

	// Show Trigger slug when available
	if extendedMutationDetails.Trigger != nil {
		view.Append([]string{"Trigger", extendedMutationDetails.Trigger.Slug})
	} else if isSetUUID(mutationDetails.TriggerID) {
		view.Append([]string{"Trigger ID", mutationDetails.TriggerID.String()})
	}

	// Show Invocation slug when available
	if extendedMutationDetails.Invocation != nil {
		view.Append([]string{"Invocation", extendedMutationDetails.Invocation.Slug})
	} else if isSetUUID(mutationDetails.InvocationID) {
		view.Append([]string{"Invocation ID", mutationDetails.InvocationID.String()})
	}

	if mutationDetails.FunctionInvocation != nil && mutationDetails.FunctionInvocation.FunctionName != "" {
		view.Append([]string{"Function Name", mutationDetails.FunctionInvocation.FunctionName})
		for i := range mutationDetails.FunctionInvocation.Arguments {
			argument := mutationDetails.FunctionInvocation.Arguments[i]
			argLabel := fmt.Sprintf("Argument %d", i)
			if argument.ParameterName != nil && *argument.ParameterName != "" {
				argLabel = fmt.Sprintf("Argument %d (%s)", i, *argument.ParameterName)
			}
			view.Append([]string{argLabel, formatFunctionArgumentValue(argument.Value)})
		}
	}
	// The values a parameterized Invocation ran with. They are recorded separately from the
	// arguments above because those are the Invocation's own, shared by every caller.
	if len(mutationDetails.InvocationParams) != 0 {
		view.Append([]string{"Invocation Params", jsonOneLine(mutationDetails.InvocationParams)})
	}

	// Which executor ran the function. Nil means the builtin one, which is the common case and
	// says nothing worth a row; a worker is what makes the Mutation's replay worker-dependent.
	if isSetUUID(mutationDetails.BridgeWorkerID) {
		view.Append([]string{"Bridge Worker", resolveBridgeWorkerName(*mutationDetails.BridgeWorkerID, mutationDetails.SpaceSlug)})
	}

	// What a merge did with this Mutation, and why. Empty on a Mutation no merge produced.
	if mutationDetails.ReplayOutcome != "" {
		view.Append([]string{"Replay Outcome", mutationDetails.ReplayOutcome})
	}
	if mutationDetails.ReplayReason != "" {
		view.Append([]string{"Replay Reason", mutationDetails.ReplayReason})
	}

	view.Append([]string{"Organization ID", mutationDetails.OrganizationID.String()})
	view.Render()

	// Display Link details if present
	if extendedMutationDetails.Link != nil {
		tprintRaw("")
		tprintRaw("Link Details:")
		tprintRaw("-------------")
		displayLinkDetails(extendedMutationDetails.Link)
	}

	// Display Trigger details if present
	if extendedMutationDetails.Trigger != nil {
		tprintRaw("")
		tprintRaw("Trigger Details:")
		tprintRaw("----------------")
		displayTriggerDetails(extendedMutationDetails.Trigger)
	}

	// Display Invocation details if present
	if extendedMutationDetails.Invocation != nil {
		tprintRaw("")
		tprintRaw("Invocation Details:")
		tprintRaw("-------------------")
		// Create an ExtendedInvocation wrapper
		extendedInvocation := &goclientnew.ExtendedInvocation{
			Invocation: extendedMutationDetails.Invocation,
		}
		displayExtendedInvocationDetails(extendedInvocation)
	}
}

// resolveBridgeWorkerName names a worker recorded as provenance on a Mutation. The worker may
// have been deleted, or live in another Space, since the id is a record of what ran rather than
// a live pointer; the id itself is the answer when it cannot be resolved.
func resolveBridgeWorkerName(workerID uuid.UUID, mutationSpaceSlug string) string {
	workers, err := apiListAllBridgeWorkers(cubapi.NewWhere("BridgeWorkerID = '"+workerID.String()+"'"), "*", "")
	if err != nil || len(workers) == 0 || workers[0].BridgeWorker == nil || workers[0].BridgeWorker.Slug == "" {
		return workerID.String()
	}
	workerSpace := ""
	if workers[0].Space != nil {
		workerSpace = workers[0].Space.Slug
	}
	return qualifySlug(workers[0].BridgeWorker.Slug, workerSpace, mutationSpaceSlug)
}

func apiGetMutation(mutationID string, unitID string, selectParam string) (*goclientnew.ExtendedMutation, error) {
	newParams := &goclientnew.GetExtendedMutationParams{}
	include := "SpaceID,RevisionID,MergeSourceID,LinkID,TriggerID,InvocationID"
	newParams.Include = &include
	selectValue := handleSelectParameter(selectParam, selectFields, nil)
	if selectValue != "" && selectValue != "*" {
		newParams.Select = &selectValue
	}
	muteRes, err := cubClientNew.GetExtendedMutationWithResponse(ctx,
		uuid.MustParse(selectedSpaceID),
		uuid.MustParse(unitID),
		uuid.MustParse(mutationID), newParams)
	if cubapi.IsAPIError(err, muteRes) {
		return nil, cubapi.InterpretErrorGeneric(err, muteRes)
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
	extendedMutations, err := apiListMutations(selectedSpaceID, unitID, fmt.Sprintf("MutationNum = %d", mutationNum), selectParam, "")
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
