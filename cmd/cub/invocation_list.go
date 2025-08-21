// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"fmt"

	goclientnew "github.com/confighub/sdk/openapi/goclient-new"
	"github.com/google/uuid"
	"github.com/spf13/cobra"
)

var invocationListCmd = &cobra.Command{
	Use:   "list",
	Short: "List invocations",
	Long: `List invocations you have access to in a space or across all spaces. The output includes slugs, worker slugs, toolchain types, function names, and the number of arguments.

Examples:
  # List all invocations in a space with headers
  cub invocation list --space my-space

  # List invocations across all spaces (requires --space "*")
  cub invocation list --space "*" --where "FunctionName = 'cel-validate'"

  # List invocations without headers for scripting
  cub invocation list --space my-space --no-header

  # List invocations in JSON format
  cub invocation list --space my-space --json

  # List only invocation names
  cub invocation list --space my-space --no-header --names

  # List invocations for a specific toolchain
  cub invocation list --space my-space --where "ToolchainType = 'Kubernetes/YAML'"

  # List invocations using a specific function
  cub invocation list --space my-space --where "FunctionName = 'cel-validate'"`,
	Args:        cobra.ExactArgs(0),
	RunE:        invocationListCmdRun,
	Annotations: map[string]string{"OrgLevel": ""},
}

// Default columns to display when no custom columns are specified
var defaultInvocationColumns = []string{"Invocation.Slug", "Space.Slug", "BridgeWorker.Slug", "Invocation.ToolchainType", "Invocation.FunctionName", "Invocation.Arguments"}

// Invocation-specific aliases
var invocationAliases = map[string]string{
	"Name": "Invocation.Slug",
	"ID":   "Invocation.InvocationID",
}

// Invocation custom column dependencies
var invocationCustomColumnDependencies = map[string][]string{}

func init() {
	addStandardListFlags(invocationListCmd)
	invocationCmd.AddCommand(invocationListCmd)
}

func invocationListCmdRun(cmd *cobra.Command, args []string) error {
	var extendedInvocations []*goclientnew.ExtendedInvocation
	var err error

	if selectedSpaceID == "*" {
		extendedInvocations, err = apiSearchInvocations(where, selectFields)
		if err != nil {
			return err
		}
	} else {
		extendedInvocations, err = apiListInvocations(selectedSpaceID, where, selectFields)
		if err != nil {
			return err
		}
	}

	displayListResults(extendedInvocations, getInvocationSlug, displayInvocationList)
	return nil
}

func getInvocationSlug(invocation *goclientnew.ExtendedInvocation) string {
	return invocation.Invocation.Slug
}

func displayInvocationList(invocations []*goclientnew.ExtendedInvocation) {
	table := tableView()
	if !noheader {
		table.SetHeader([]string{"Name", "Space", "Worker", "Toolchain-Type", "Function-Name", "Num-Args"})
	}
	for _, i := range invocations {
		invocation := i.Invocation
		workerSlug := ""
		if i.BridgeWorker != nil {
			workerSlug = i.BridgeWorker.Slug
		}
		spaceSlug := i.Invocation.InvocationID.String()
		if i.Space != nil {
			spaceSlug = i.Space.Slug
		} else if selectedSpaceID != "*" {
			spaceSlug = selectedSpaceSlug
		}
		table.Append([]string{
			invocation.Slug,
			spaceSlug,
			workerSlug,
			invocation.ToolchainType,
			invocation.FunctionName,
			fmt.Sprintf("%d", len(invocation.Arguments)),
		})
	}
	table.Render()
}

func apiListInvocations(spaceID string, whereFilter string, selectParam string) ([]*goclientnew.ExtendedInvocation, error) {
	newParams := &goclientnew.ListInvocationsParams{}
	include := "SpaceID,BridgeWorkerID"
	newParams.Include = &include
	if whereFilter != "" {
		newParams.Where = &whereFilter
	}
	if contains != "" {
		newParams.Contains = &contains
	}
	selectValue := handleSelectParameter(selectParam, selectFields, func() string {
		baseFields := []string{"Slug", "InvocationID", "SpaceID", "OrganizationID"}
		return buildSelectList("Invocation", "", include, defaultInvocationColumns, invocationAliases, invocationCustomColumnDependencies, baseFields)
	})
	if selectValue != "" && selectValue != "*" {
		newParams.Select = &selectValue
	}
	invocationsRes, err := cubClientNew.ListInvocationsWithResponse(ctx, uuid.MustParse(spaceID), newParams)
	if IsAPIError(err, invocationsRes) {
		return nil, InterpretErrorGeneric(err, invocationsRes)
	}

	invocations := make([]*goclientnew.ExtendedInvocation, 0, len(*invocationsRes.JSON200))
	for _, invocation := range *invocationsRes.JSON200 {
		invocations = append(invocations, &invocation)
	}

	return invocations, nil
}

func apiSearchInvocations(whereFilter string, selectParam string) ([]*goclientnew.ExtendedInvocation, error) {
	newParams := &goclientnew.ListAllInvocationsParams{}
	if whereFilter != "" {
		newParams.Where = &whereFilter
	}
	if contains != "" {
		newParams.Contains = &contains
	}

	include := "SpaceID,BridgeWorkerID"
	newParams.Include = &include

	selectValue := handleSelectParameter(selectParam, selectFields, func() string {
		baseFields := []string{"Slug", "InvocationID", "SpaceID", "OrganizationID"}
		return buildSelectList("Invocation", "", include, defaultInvocationColumns, invocationAliases, invocationCustomColumnDependencies, baseFields)
	})
	if selectValue != "" && selectValue != "*" {
		newParams.Select = &selectValue
	}

	res, err := cubClientNew.ListAllInvocations(ctx, newParams)
	if err != nil {
		return nil, err
	}
	invocationsRes, err := goclientnew.ParseListAllInvocationsResponse(res)
	if IsAPIError(err, invocationsRes) {
		return nil, InterpretErrorGeneric(err, invocationsRes)
	}

	extendedInvocations := make([]*goclientnew.ExtendedInvocation, 0, len(*invocationsRes.JSON200))
	for _, invocation := range *invocationsRes.JSON200 {
		extendedInvocations = append(extendedInvocations, &invocation)
	}

	return extendedInvocations, nil
}