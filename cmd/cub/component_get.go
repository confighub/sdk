// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"sort"
	"strings"

	goclientnew "github.com/confighub/sdk/core/openapi/goclient-new"
	"github.com/spf13/cobra"
)

var componentGetCmd = &cobra.Command{
	Use:   "get <name or id>",
	Short: "Get details about a component",
	Args:  cobra.ExactArgs(1),
	Long: getCommandHelp(`Get detailed information about a Component entity, including which ChangeWorkflows its promotions and releases may use and whether one is required.

Examples:
`+"```"+`
  # Get component details in table format
  cub component get my-app

  # Get component details in JSON format
  cub component get --json my-app
`+"```"+`
`, ""),
	RunE: componentGetCmdRun,
}

func init() {
	addStandardGetFlags(componentGetCmd)
	componentCmd.AddCommand(componentGetCmd)
}

func componentGetCmdRun(cmd *cobra.Command, args []string) error {
	extendedComponent, err := resolveComponent(args[0], selectFields)
	if err != nil {
		return err
	}
	displayGetResults(extendedComponent.Component, displayComponentEntityDetails)
	return nil
}

func allowedChangeWorkflowIDsToString(allowed []goclientnew.UUID) string {
	ids := make([]string, 0, len(allowed))
	for _, id := range allowed {
		ids = append(ids, id.String())
	}
	sort.Strings(ids)
	return strings.Join(ids, ", ")
}

func displayComponentEntityDetails(component *goclientnew.Component) {
	view := tableView()
	view.Append([]string{"ID", component.ComponentID.String()})
	view.Append([]string{"Name", component.Slug})
	view.Append([]string{"Created At", component.CreatedAt.String()})
	view.Append([]string{"Updated At", component.UpdatedAt.String()})
	view.Append([]string{"Labels", labelsToString(component.Labels)})
	view.Append([]string{"Delete Gates", deleteGatesToString(component.DeleteGates)})
	view.Append([]string{"Annotations", annotationsToString(component.Annotations)})
	view.Append([]string{"Permissions", permissionsToString(component.Permissions)})
	view.Append([]string{"Allowed ChangeWorkflows", allowedChangeWorkflowIDsToString(component.AllowedChangeWorkflowIDs)})
	changeWorkflowRequired := "false"
	if component.ChangeWorkflowRequired {
		changeWorkflowRequired = "true"
	}
	view.Append([]string{"ChangeWorkflow Required", changeWorkflowRequired})
	view.Append([]string{"Organization ID", component.OrganizationID.String()})
	view.Render()
}
