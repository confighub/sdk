// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"fmt"

	goclientnew "github.com/confighub/sdk/openapi/goclient-new"
	"github.com/google/uuid"
	"github.com/spf13/cobra"
)

var invocationGetCmd = &cobra.Command{
	Use:   "get <slug or id>",
	Short: "Get details about an invocation",
	Args:  cobra.ExactArgs(1),
	Long: `Get detailed information about an invocation in a space including its ID, slug, display name, toolchain type, function name, and arguments.

Examples:
  # Get details about an invocation that validates replicas
  cub invocation get --space my-space --json validate-replicas

  # Get details about an invocation that enforces low resource usage
  cub invocation get --space my-space --json enforce-low-cost
`,
	RunE: invocationGetCmdRun,
}

func init() {
	addStandardGetFlags(invocationGetCmd)
	invocationCmd.AddCommand(invocationGetCmd)
}

func invocationGetCmdRun(cmd *cobra.Command, args []string) error {
	invocationDetails, err := apiGetInvocationFromSlug(args[0], selectFields)
	if err != nil {
		return err
	}

	displayGetResults(invocationDetails, displayInvocationDetails)
	return nil
}

func displayInvocationDetails(invocationDetails *goclientnew.Invocation) {
	view := tableView()
	view.Append([]string{"ID", invocationDetails.InvocationID.String()})
	view.Append([]string{"Name", invocationDetails.Slug})
	view.Append([]string{"Display Name", invocationDetails.DisplayName})
	view.Append([]string{"Space ID", invocationDetails.SpaceID.String()})
	view.Append([]string{"Created At", invocationDetails.CreatedAt.String()})
	view.Append([]string{"Updated At", invocationDetails.UpdatedAt.String()})
	view.Append([]string{"Labels", labelsToString(invocationDetails.Labels)})
	view.Append([]string{"Annotations", annotationsToString(invocationDetails.Annotations)})
	view.Append([]string{"Organization ID", invocationDetails.OrganizationID.String()})
	if invocationDetails.BridgeWorkerID != nil && *invocationDetails.BridgeWorkerID != uuid.Nil {
		view.Append([]string{"Worker ID", invocationDetails.BridgeWorkerID.String()})
	}
	view.Append([]string{"Toolchain Type", invocationDetails.ToolchainType})
	view.Append([]string{"Function Name", invocationDetails.FunctionName})
	for i := range invocationDetails.Arguments {
		argLabel := fmt.Sprintf("Argument %d", i)
		if invocationDetails.Arguments[i].ParameterName != nil {
			argLabel = fmt.Sprintf("Argument %d (%s)", i, *invocationDetails.Arguments[i].ParameterName)
		}
		view.Append([]string{argLabel, formatFunctionArgumentValue(invocationDetails.Arguments[i].Value)})
	}
	if invocationDetails.Hash != "" {
		view.Append([]string{"Hash", invocationDetails.Hash})
	}
	view.Render()
}

func apiGetInvocation(invocationID string, selectParam string) (*goclientnew.Invocation, error) {
	newParams := &goclientnew.GetInvocationParams{}
	include := "BridgeWorkerID"
	newParams.Include = &include
	selectValue := handleSelectParameter(selectParam, selectFields, nil)
	if selectValue != "" && selectValue != "*" {
		newParams.Select = &selectValue
	}
	invocationRes, err := cubClientNew.GetInvocationWithResponse(ctx, uuid.MustParse(selectedSpaceID), uuid.MustParse(invocationID), newParams)
	if IsAPIError(err, invocationRes) {
		return nil, InterpretErrorGeneric(err, invocationRes)
	}
	return invocationRes.JSON200.Invocation, nil
}

func apiGetInvocationFromSlug(slug string, selectParam string) (*goclientnew.Invocation, error) {
	id, err := uuid.Parse(slug)
	if err == nil {
		return apiGetInvocation(id.String(), selectParam)
	}
	// The default for get is "*" rather than auto-selected list columns
	if selectParam == "" {
		selectParam = "*"
	}
	invocations, err := apiListInvocations(selectedSpaceID, "Slug = '"+slug+"'", selectParam)
	if err != nil {
		return nil, err
	}
	// find invocation by slug
	for _, invocation := range invocations {
		if invocation.Invocation != nil && invocation.Invocation.Slug == slug {
			return invocation.Invocation, nil
		}
	}
	return nil, fmt.Errorf("invocation %s not found in space %s", slug, selectedSpaceSlug)
}