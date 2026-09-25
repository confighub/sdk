// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"fmt"
	"sort"
	"strings"

	"github.com/confighub/sdk/core/cubapi"
	goclientnew "github.com/confighub/sdk/core/openapi/goclient-new"
	"github.com/spf13/cobra"
)

var componentListCmd = &cobra.Command{
	Use:   "list",
	Short: "List components",
	Args:  cobra.NoArgs,
	Long:  getComponentListHelp(),
	RunE:  componentListCmdRun,
}

func getComponentListHelp() string {
	baseHelp := `List the Component entities in this organization, with whether a ChangeWorkflow is required to promote and release, how many ChangeWorkflows they allow, and their variants: the spaces naming them with ComponentID.

Examples:
` + "```" + `
  # List all components
  cub component list

  # List just the component names
  cub component list -o name

  # List components that require a ChangeWorkflow
  cub component list --where "ChangeWorkflowRequired = true"

  # List components labeled with an owner
  cub component list --where "Labels.Owner = 'platform'"
` + "```" + `
`

	agentContext := `Use this to discover which applications exist before drilling into their spaces and units.

Follow-up workflow:
1. 'component list' to find the component name
2. 'component get NAME' to see its allowed ChangeWorkflows and permissions
3. 'space list --where "ComponentID = 'COMPONENT_ID'"' to see its variants as spaces
4. 'unit list --space SPACE_SLUG' to see the units in one variant

--where filters components, not their spaces.`

	return getCommandHelp(baseHelp, agentContext)
}

// Default columns to display when no custom columns are specified
var defaultComponentColumns = []string{"Component.Slug", "Component.ChangeWorkflowRequired", "Component.AllowedChangeWorkflowIDs"}

// componentBaseSelectFields are the fields always returned by component list queries.
var componentBaseSelectFields = []string{"Slug", "ComponentID", "OrganizationID"}

var componentAliases = map[string]string{
	"Name": "Component.Slug",
	"ID":   "Component.ComponentID",
}

var componentCustomColumnDependencies = map[string][]string{}

func init() {
	enableWhereFlag(componentListCmd)
	enableFilterFlag(componentListCmd)
	enableContainsFlag(componentListCmd)
	addStandardListDisplayFlags(componentListCmd)
	componentCmd.AddCommand(componentListCmd)
}

func componentListCmdRun(_ *cobra.Command, _ []string) error {
	filterID, err := parseFilterFlag(filter)
	if err != nil {
		return err
	}
	selectValue := handleSelectParameter(selectFields, selectFields, func() string {
		return buildSelectList("Component", listColumnsFor("cub component list"), "", defaultComponentColumns, componentAliases, componentCustomColumnDependencies, componentBaseSelectFields)
	})
	components, err := cubapi.ListComponents(ctx, cubClient, cubapi.NewWhere(where), cubapi.ListOpts{
		Select:   cubapi.SelectFields(selectValue),
		Filter:   filterID,
		Contains: contains,
	})
	if err != nil {
		return err
	}
	displayListResults(components, func(c *goclientnew.ExtendedComponent) string { return c.Component.Slug }, displayComponentEntityList)
	return nil
}

// componentVariants returns the slugs of the spaces naming each Component, sorted, by ComponentID.
func componentVariants() (map[goclientnew.UUID][]string, error) {
	spaces, err := cubapi.ListSpaces(ctx, cubClient, cubapi.NewWhere("ComponentID IS NOT NULL"), cubapi.ListOpts{
		Select: "SpaceID,Slug,ComponentID,OrganizationID",
	})
	if err != nil {
		return nil, err
	}
	variants := map[goclientnew.UUID][]string{}
	for _, extendedSpace := range spaces {
		if extendedSpace.Space == nil || extendedSpace.Space.ComponentID == nil {
			continue
		}
		componentID := *extendedSpace.Space.ComponentID
		variants[componentID] = append(variants[componentID], extendedSpace.Space.Slug)
	}
	for _, slugs := range variants {
		sort.Strings(slugs)
	}
	return variants, nil
}

func displayComponentEntityList(components []*goclientnew.ExtendedComponent) {
	if displayRequestedColumns(components, componentAliases, nil) {
		return
	}
	variants, err := componentVariants()
	failOnError(err)
	table := tableView()
	if !noheader {
		table.SetHeader([]string{"Name", "Workflow-Required", "#Allowed-Workflows", "Variants"})
	}
	for _, c := range components {
		component := c.Component
		table.Append([]string{
			component.Slug,
			fmt.Sprintf("%t", component.ChangeWorkflowRequired),
			fmt.Sprintf("%d", len(component.AllowedChangeWorkflowIDs)),
			strings.Join(variants[component.ComponentID], ", "),
		})
	}
	table.Render()
}
