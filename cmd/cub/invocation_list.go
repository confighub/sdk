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

var invocationListCmd = &cobra.Command{
	Use:   "list",
	Short: "List invocations",
	Long: getCommandHelp(`List invocations you have access to in a space or across all spaces. The output includes slugs, worker slugs, toolchain types, function names, and the number of arguments.

Examples:
`+"```"+`
  # List all invocations in a space with headers
  cub invocation list --space my-space

  # List invocations across all spaces (requires --space "*")
  cub invocation list --space "*" --where "FunctionName = 'cel-validate'"

  # List invocations without headers for scripting
  cub invocation list --space my-space --no-headers

  # List invocations in JSON format
  cub invocation list --space my-space -o json

  # List only invocation names
  cub invocation list --space my-space --no-headers -o name

  # List invocations for a specific toolchain
  cub invocation list --space my-space --where "ToolchainType = 'Kubernetes/YAML'"

  # List invocations using a specific function
  cub invocation list --space my-space --where "FunctionName = 'cel-validate'"
`+"```"+`
`, ""),
	Args:        cobra.ExactArgs(0),
	RunE:        invocationListCmdRun,
	Annotations: map[string]string{"OrgLevel": ""},
}

// Default columns to display when no custom columns are specified
var defaultInvocationColumns = []string{"Invocation.Slug", "Space.Slug", "BridgeWorker.Slug", "Invocation.ToolchainType", "Invocation.FunctionName", "Invocation.Arguments"}

// invocationListInclude is the Include parameter for invocation list queries (the
// related entities expanded into each ExtendedInvocation).
const invocationListInclude = "SpaceID,BridgeWorkerID"

// invocationBaseSelectFields are the fields always returned by invocation list
// queries, regardless of the requested columns.
var invocationBaseSelectFields = []string{"Slug", "InvocationID", "SpaceID", "OrganizationID"}

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
	filterID, err := parseFilterFlag(filter)
	if err != nil {
		return err
	}

	extendedInvocations, err := apiListInvocations(selectedSpaceID, where, selectFields, filterID)
	if err != nil {
		return err
	}

	displayListResults(extendedInvocations, getInvocationSlug, displayInvocationList)
	return nil
}

func getInvocationSlug(invocation *goclientnew.ExtendedInvocation) string {
	space := ""
	if invocation.Space != nil {
		space = invocation.Space.Slug
	}
	return prefixedSlug(space, invocation.Invocation.Slug)
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

// apiListInvocations lists invocations via the org-level endpoint, scoped to a
// single space by a SpaceID clause unless spaceID is "*" (list across all spaces).
func apiListInvocations(spaceID string, whereFilter string, selectParam string, filterParam string) ([]*goclientnew.ExtendedInvocation, error) {
	where := cubapi.NewWhere(whereFilter)
	if spaceID != "*" {
		where = where.SpaceID(goclientnew.UUID(uuid.MustParse(spaceID)))
	}
	return apiListAllInvocations(where, selectParam, filterParam)
}

func apiListAllInvocations(where cubapi.Where, selectParam string, filterParam string) ([]*goclientnew.ExtendedInvocation, error) {
	selectValue := handleSelectParameter(selectParam, selectFields, func() string {
		return buildSelectList("Invocation", nil, invocationListInclude, defaultInvocationColumns, invocationAliases, invocationCustomColumnDependencies, invocationBaseSelectFields)
	})
	return cubapi.ListInvocations(ctx, cubClient, where, cubapi.ListOpts{
		Select:   cubapi.SelectFields(selectValue),
		Include:  invocationListInclude,
		Filter:   filterParam,
		Contains: contains,
	})
}
