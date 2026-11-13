// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"fmt"
	"slices"
	"sort"
	"strings"

	"github.com/cockroachdb/errors"
	"github.com/confighub/sdk/core/cubapi"
	goclientnew "github.com/confighub/sdk/core/openapi/goclient-new"
	"github.com/google/uuid"
	"github.com/spf13/cobra"
)

var componentGetCmd = &cobra.Command{
	Use:   "get <name or id>",
	Short: "Get details about a component",
	Args:  cobra.ExactArgs(1),
	Long: getCommandHelp(`Get detailed information about a Component entity, including which ChangeWorkflows its promotions and releases may use, whether one is required, and whether changes to its variants must be tracked by a ChangeSet or ChangeOrder.

The Owner row is the Component's own Owner label when it has one. Otherwise it is the Owner label
all of the Component's spaces share with one value, and it is empty when they disagree, when one of
them has no Owner label, or when the Component has no spaces. -o json and -o jq return the
Component as stored, so .Component.Labels.Owner is its own label only.

Examples:
`+"```"+`
  # Get component details in table format
  cub component get my-app

  # Get component details in JSON format
  cub component get -o json my-app

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
	allowed, err := allowedChangeWorkflowNames(component.AllowedChangeWorkflowIDs, extendedComponent.AllowedChangeWorkflows)
	failOnError(err)
	renderComponentEntityDetails(component, &owner, extendedComponent.Permissions, allowed)
}

// allowedChangeWorkflowNames names the ChangeWorkflows a Component allows as <space>/<slug>, the
// form --allowed-change-workflow takes, sorted. expanded are the ChangeWorkflows the server
// returned for the IDs; an ID none of them is stays an ID.
func allowedChangeWorkflowNames(allowed []goclientnew.UUID, expanded []goclientnew.ChangeWorkflow) (string, error) {
	byID := make(map[uuid.UUID]*goclientnew.ChangeWorkflow, len(expanded))
	var spaceIDs []uuid.UUID
	for i := range expanded {
		byID[expanded[i].ChangeWorkflowID] = &expanded[i]
		if !slices.Contains(spaceIDs, expanded[i].SpaceID) {
			spaceIDs = append(spaceIDs, expanded[i].SpaceID)
		}
	}
	spaceSlugs := make(map[uuid.UUID]string, len(spaceIDs))
	if len(spaceIDs) > 0 {
		spaces, err := cubapi.ListSpaces(ctx, cubClient, cubapi.NewWhere("").In("SpaceID", spaceIDs),
			cubapi.ListOpts{Select: "SpaceID,Slug"})
		if err != nil {
			return "", errors.Wrap(err, "failed to list the spaces of the allowed changeworkflows")
		}
		for _, space := range spaces {
			spaceSlugs[space.Space.SpaceID] = space.Space.Slug
		}
	}
	names := make([]string, 0, len(allowed))
	for _, id := range allowed {
		changeWorkflow, ok := byID[id]
		if !ok {
			names = append(names, id.String())
			continue
		}
		if spaceSlug, ok := spaceSlugs[changeWorkflow.SpaceID]; ok {
			names = append(names, spaceSlug+"/"+changeWorkflow.Slug)
		} else {
			names = append(names, changeWorkflow.SpaceID.String()+"/"+changeWorkflow.Slug)
		}
	}
	sort.Strings(names)
	return strings.Join(names, ", "), nil
}

// displayComponentEntityDetails renders the Component a create, update or edit returns. That
// carries only the IDs of the ChangeWorkflows it allows, so the Component is read back for them.
func displayComponentEntityDetails(component *goclientnew.Component) {
	var expanded []goclientnew.ChangeWorkflow
	if len(component.AllowedChangeWorkflowIDs) > 0 {
		extendedComponent, err := resolveComponent(component.ComponentID.String(), "ComponentID,AllowedChangeWorkflowIDs")
		failOnError(err)
		expanded = extendedComponent.AllowedChangeWorkflows
	}
	allowed, err := allowedChangeWorkflowNames(component.AllowedChangeWorkflowIDs, expanded)
	failOnError(err)
	renderComponentEntityDetails(component, nil, nil, allowed)
}

// renderComponentEntityDetails prints the Component, with an Owner row when owner is given:
// get reads the Component's spaces to work it out, and create and update do not.
func renderComponentEntityDetails(component *goclientnew.Component, owner *string, expanded *goclientnew.ExtendedPermissions, allowedChangeWorkflows string) {
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
	view.Append([]string{"Permissions", permissionsToString(component.Permissions, expanded)})
	view.Append([]string{"Allowed ChangeWorkflows", allowedChangeWorkflows})
	view.Append([]string{"Require ChangeWorkflow", fmt.Sprintf("%t", component.RequireChangeWorkflow)})
	view.Append([]string{"Require Change Tracking", fmt.Sprintf("%t", component.RequireChangeTracking)})
	view.Append([]string{"Organization ID", component.OrganizationID.String()})
	view.Render()
}
