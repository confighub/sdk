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

The Owner row is the Component's own Owner label when it has one. Otherwise it is the Owner label
all of the Component's spaces share with one value, and it is empty when they disagree, when one of
them has no Owner label, or when the Component has no spaces. -o json and -o jq return the
Component as stored, so .Component.Labels.Owner is its own label only.

Examples:
`+"```"+`
  # Get component details in table format
  cub component get my-app

  # Get component details in JSON format
  cub component get --json my-app

  # Get the component's ID
  cub component get my-app -o jq=.Component.ComponentID
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
	displayGetResults(extendedComponent, displayExtendedComponentDetails)
	return nil
}

// displayExtendedComponentDetails renders what get returns: the Component wrapped the way every
// other get's entity is, so -o json and -o jq read it as .Component, as list does.
func displayExtendedComponentDetails(extendedComponent *goclientnew.ExtendedComponent) {
	component := extendedComponent.Component
	spaces, err := componentSpaces(component.ComponentID)
	failOnError(err)
	owner := componentOwner(component.Labels, spaces)
	renderComponentEntityDetails(component, &owner)
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
	renderComponentEntityDetails(component, nil)
}

// renderComponentEntityDetails prints the Component, with an Owner row when owner is given:
// get reads the Component's spaces to work it out, and create and update do not.
func renderComponentEntityDetails(component *goclientnew.Component, owner *string) {
	view := tableView()
	view.Append([]string{"ID", component.ComponentID.String()})
	view.Append([]string{"Name", component.Slug})
	view.Append([]string{"Created At", component.CreatedAt.String()})
	view.Append([]string{"Updated At", component.UpdatedAt.String()})
	view.Append([]string{"Labels", labelsToString(component.Labels)})
	if owner != nil {
		view.Append([]string{"Owner", *owner})
	}
	appendBackingUnitRow(view, component.BackingUnitID)
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
