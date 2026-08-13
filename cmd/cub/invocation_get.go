// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"fmt"
	"strings"

	"github.com/olekukonko/tablewriter"

	"github.com/confighub/sdk/core/cubapi"
	goclientnew "github.com/confighub/sdk/core/openapi/goclient-new"
	"github.com/google/uuid"
	"github.com/spf13/cobra"
)

var invocationGetCmd = &cobra.Command{
	Use:   "get <slug or id>",
	Short: "Get details about an invocation",
	Args:  cobra.ExactArgs(1),
	Long: getCommandHelp(`Get detailed information about an invocation in a space including its ID, slug, display name, toolchain type, function name, and arguments.

Examples:
`+"```"+`
  # Get details about an invocation that validates replicas
  cub invocation get --space my-space -o json validate-replicas

  # Get details about an invocation that enforces low resource usage
  cub invocation get --space my-space -o json enforce-low-cost
`+"```"+`
`, ""),
	RunE: invocationGetCmdRun,
}

func init() {
	addStandardGetFlags(invocationGetCmd)
	invocationCmd.AddCommand(invocationGetCmd)
}

func invocationGetCmdRun(cmd *cobra.Command, args []string) error {
	invocationDetails, err := apiGetExtendedInvocationFromSlug(args[0], selectFields)
	if err != nil {
		return err
	}

	displayGetResults(invocationDetails, displayExtendedInvocationDetails)
	return nil
}

func displayInvocationDetails(invocationDetails *goclientnew.Invocation) {
	// Create an ExtendedInvocation wrapper with just the Invocation set
	extendedInvocation := &goclientnew.ExtendedInvocation{
		Invocation: invocationDetails,
		// All other fields (Space, BridgeWorker, etc.) will be nil, causing Extended display to show IDs
	}
	displayExtendedInvocationDetails(extendedInvocation)
}

func displayExtendedInvocationDetails(extendedInvocation *goclientnew.ExtendedInvocation) {
	invocationDetails := extendedInvocation.Invocation
	view := tableView()
	view.Append([]string{"ID", invocationDetails.InvocationID.String()})
	view.Append([]string{"Name", invocationDetails.Slug})

	// Show Space slug instead of Space ID when available
	if extendedInvocation.Space != nil {
		view.Append([]string{"Space", extendedInvocation.Space.Slug})
	} else {
		view.Append([]string{"Space ID", invocationDetails.SpaceID.String()})
	}
	view.Append([]string{"Created At", invocationDetails.CreatedAt.String()})
	view.Append([]string{"Updated At", invocationDetails.UpdatedAt.String()})
	view.Append([]string{"Labels", labelsToString(invocationDetails.Labels)})
	view.Append([]string{"Delete Gates", deleteGatesToString(invocationDetails.DeleteGates)})
	view.Append([]string{"Annotations", annotationsToString(invocationDetails.Annotations)})
	view.Append([]string{"Organization ID", invocationDetails.OrganizationID.String()})

	// Show BridgeWorker slug instead of BridgeWorkerID when available
	if extendedInvocation.BridgeWorker != nil {
		view.Append([]string{"Worker", extendedInvocation.BridgeWorker.Slug})
	} else if invocationDetails.BridgeWorkerID != nil && *invocationDetails.BridgeWorkerID != uuid.Nil {
		view.Append([]string{"Worker ID", invocationDetails.BridgeWorkerID.String()})
	}

	view.Append([]string{"Toolchain Type", invocationDetails.ToolchainType})
	for i := range invocationDetails.Parameters {
		p := invocationDetails.Parameters[i]
		req := "optional"
		if p.Required {
			req = "required"
		}
		detail := req
		if p.DataType != "" {
			detail = fmt.Sprintf("%s, %s", p.DataType, req)
		}
		view.Append([]string{fmt.Sprintf("Parameter %d (%s)", i, p.ParameterName), detail})
	}
	appendFunctionInvocationRows(view, invocationDetails.FunctionInvocations)
	if invocationDetails.Hash != "" {
		view.Append([]string{"Hash", invocationDetails.Hash})
	}
	view.Render()
}

// appendFunctionInvocationRows lists the functions an Invocation calls, in the order they are
// executed, one row per function plus one per argument. The functions are numbered only when
// there is more than one, so a single-function Invocation reads as it always has.
func appendFunctionInvocationRows(view *tablewriter.Table, functionInvocations *goclientnew.FunctionInvocationList) {
	if functionInvocations == nil {
		return
	}
	functions := *functionInvocations
	for f := range functions {
		prefix := ""
		if len(functions) > 1 {
			prefix = fmt.Sprintf("%d. ", f)
		}
		view.Append([]string{prefix + "Function Name", functions[f].FunctionName})
		if functions[f].WhereResource != "" {
			view.Append([]string{prefix + "Where Resource", functions[f].WhereResource})
		}
		for i, argument := range functions[f].Arguments {
			argLabel := fmt.Sprintf("%sArgument %d", prefix, i)
			if argument.ParameterName != nil {
				argLabel = fmt.Sprintf("%sArgument %d (%s)", prefix, i, *argument.ParameterName)
			}
			view.Append([]string{argLabel, formatFunctionArgumentValue(argument.Value)})
		}
	}
}

// functionInvocationsSummary names the functions an Invocation calls, for one table cell.
func functionInvocationsSummary(functionInvocations *goclientnew.FunctionInvocationList) string {
	if functionInvocations == nil {
		return ""
	}
	names := make([]string, 0, len(*functionInvocations))
	for _, function := range *functionInvocations {
		names = append(names, function.FunctionName)
	}
	return strings.Join(names, ", ")
}

func apiGetInvocation(invocationID string, selectParam string) (*goclientnew.Invocation, error) {
	extendedInvocation, err := apiGetExtendedInvocation(invocationID, selectParam)
	if err != nil {
		return nil, err
	}
	return extendedInvocation.Invocation, nil
}

func apiGetExtendedInvocation(invocationID string, selectParam string) (*goclientnew.ExtendedInvocation, error) {
	newParams := &goclientnew.GetInvocationParams{}
	include := "SpaceID,BridgeWorkerID"
	newParams.Include = &include
	selectValue := handleSelectParameter(selectParam, selectFields, nil)
	if selectValue != "" && selectValue != "*" {
		newParams.Select = &selectValue
	}
	invocationRes, err := cubClientNew.GetInvocationWithResponse(ctx, uuid.MustParse(selectedSpaceID), uuid.MustParse(invocationID), newParams)
	if cubapi.IsAPIError(err, invocationRes) {
		return nil, cubapi.InterpretErrorGeneric(err, invocationRes)
	}
	return invocationRes.JSON200, nil
}

func apiGetInvocationFromSlug(slug string, selectParam string) (*goclientnew.Invocation, error) {
	return apiGetInvocationFromSlugInSpace(slug, selectedSpaceID, selectParam)
}

func apiGetInvocationFromSlugInSpace(slug string, spaceID string, selectParam string) (*goclientnew.Invocation, error) {
	id, err := uuid.Parse(slug)
	if err == nil {
		return apiGetInvocation(id.String(), selectParam)
	}
	// The default for get is "*" rather than auto-selected list columns
	if selectParam == "" {
		selectParam = "*"
	}
	invocations, err := apiListInvocations(spaceID, "Slug = '"+slug+"'", selectParam, "")
	if err != nil {
		return nil, err
	}
	// find invocation by slug
	for _, invocation := range invocations {
		if invocation.Invocation != nil && invocation.Invocation.Slug == slug {
			return invocation.Invocation, nil
		}
	}
	return nil, fmt.Errorf("invocation %s not found in space %s", slug, spaceID)
}

func apiGetExtendedInvocationFromSlug(slug string, selectParam string) (*goclientnew.ExtendedInvocation, error) {
	id, err := uuid.Parse(slug)
	if err == nil {
		return apiGetExtendedInvocation(id.String(), selectParam)
	}
	// The default for get is "*" rather than auto-selected list columns
	if selectParam == "" {
		selectParam = "*"
	}
	invocations, err := apiListInvocations(selectedSpaceID, "Slug = '"+slug+"'", selectParam, "")
	if err != nil {
		return nil, err
	}
	// find invocation by slug
	for _, invocation := range invocations {
		if invocation.Invocation != nil && invocation.Invocation.Slug == slug {
			return invocation, nil
		}
	}
	return nil, fmt.Errorf("invocation %s not found in space %s", slug, selectedSpaceID)
}
