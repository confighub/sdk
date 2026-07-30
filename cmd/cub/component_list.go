// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"fmt"

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
	baseHelp := `List the components in this organization, with their owner, their variants, and the number of spaces and units they span.

Components are derived from the "Component" label on spaces, so --where filters spaces, not components: a component is listed if any of its spaces match.

Examples:
` + "```" + `
  # List all components
  cub component list

  # List just the component names
  cub component list -o name

  # List components owned by a team
  cub component list --where "Labels.Owner = 'platform'"

  # List components that have a prod variant
  cub component list --where "Labels.Variant = 'prod'"
` + "```" + `
`

	agentContext := `Use this to discover which applications exist before drilling into their spaces and units.

Follow-up workflow:
1. 'component list' to find the component name
2. 'space list --where "Labels.Component = 'NAME'"' to see its variants as spaces
3. 'unit list --space SPACE_SLUG' to see the units in one variant

Note that --where filters the underlying spaces. A component whose prod space matches is listed
in full, including its other variants; the counts always cover all of the component's spaces.`

	return getCommandHelp(baseHelp, agentContext)
}

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
	components, err := apiListComponents(where, filterID)
	if err != nil {
		return err
	}
	displayListResults(components, func(c *Component) string { return c.Name }, displayComponentList)
	return nil
}

func displayComponentList(components []*Component) {
	table := tableView()
	if !noheader {
		table.SetHeader([]string{"Name", "Owner", "Variants", "#Spaces", "#Units"})
	}
	for _, component := range components {
		table.Append([]string{
			component.Name,
			component.Owner,
			joinVariants(component.Variants),
			fmt.Sprintf("%d", len(component.Spaces)),
			fmt.Sprintf("%d", component.UnitCount),
		})
	}
	table.Render()
}
