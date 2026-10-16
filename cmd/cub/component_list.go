// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/confighub/sdk/core/cubapi"
	goclientnew "github.com/confighub/sdk/core/openapi/goclient-new"
	"github.com/olekukonko/tablewriter"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

var componentListCmd = &cobra.Command{
	Use:   "list",
	Short: "List components",
	Args:  cobra.NoArgs,
	Long:  getComponentListHelp(),
	RunE:  componentListCmdRun,
}

func getComponentListHelp() string {
	baseHelp := `List the Component entities in this organization, with whether a ChangeWorkflow is required to promote and release, how many ChangeWorkflows they allow, their owner, and their variants: the Variant label of each space naming them with ComponentID, which is the name --variant <component>/<variant> takes.

The Owner column is the Component's own Owner label when it has one. Otherwise it is the Owner
label all of the Component's spaces share with one value, and it is empty when they disagree,
when one of them has no Owner label, or when the Component has no spaces.

Examples:
` + "```" + `
  # List all components
  cub component list

  # List just the component names
  cub component list -o name

  # List components that require a ChangeWorkflow
  cub component list --where "ChangeWorkflowRequired = true"

  # List components whose own Owner label is "platform". This matches only the
  # Component's label, not an owner the Owner column reads from its spaces.
  cub component list --where "Labels.Owner = 'platform'"
` + "```" + `
`

	agentContext := `Use this to discover which applications exist before drilling into their spaces and units.

Follow-up workflow:
1. 'component list' to find the component name
2. 'component get NAME' to see its allowed ChangeWorkflows and permissions
3. 'space list --where "ComponentID = 'COMPONENT_ID'"' to see its variants as spaces
4. 'unit list --space SPACE_SLUG' to see the units in one variant

--where filters components, not their spaces, so --where "Labels.Owner = '...'" matches only a
Component's own Owner label. 'cub variant create --owner' and 'cub variant upload --owner' set that
label.`

	return getCommandHelp(baseHelp, agentContext)
}

// Default columns to display when no custom columns are specified
var defaultComponentColumns = []string{"Component.Slug", "Component.ChangeWorkflowRequired", "Component.AllowedChangeWorkflowIDs", "Component.Labels"}

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
	enableListPagingFlags(componentListCmd)
	addStandardListDisplayFlags(componentListCmd)
	enableViewFlag(componentListCmd)
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
		Limit:         listLimit,
		OrderBy:       listOrderBy,
		Select:        cubapi.SelectFields(selectValue),
		Filter:        filterID,
		Contains:      contains,
		IncludeHidden: includeHidden,
		View:          listViewFor("cub component list"),
	})
	if err != nil {
		return err
	}
	displayListResults(components, func(c *goclientnew.ExtendedComponent) string { return c.Component.Slug }, displayComponentEntityList)
	return nil
}

// spacesByComponent returns the spaces naming each Component, sorted by slug, by ComponentID.
func spacesByComponent() (map[goclientnew.UUID][]*goclientnew.ExtendedSpace, error) {
	spaces, err := cubapi.ListSpaces(ctx, cubClient, cubapi.NewWhere("ComponentID IS NOT NULL"), cubapi.ListOpts{
		Select: "SpaceID,Slug,ComponentID,Labels,OrganizationID",
	})
	if err != nil {
		return nil, err
	}
	byComponent := map[goclientnew.UUID][]*goclientnew.ExtendedSpace{}
	for _, extendedSpace := range spaces {
		if extendedSpace.Space == nil || extendedSpace.Space.ComponentID == nil {
			continue
		}
		componentID := *extendedSpace.Space.ComponentID
		byComponent[componentID] = append(byComponent[componentID], extendedSpace)
	}
	for _, componentSpaces := range byComponent {
		sort.Slice(componentSpaces, func(i, j int) bool {
			return componentSpaces[i].Space.Slug < componentSpaces[j].Space.Slug
		})
	}
	return byComponent, nil
}

// variantNames names a Component's variants as --variant takes them: by each space's Variant
// label, sorted. A space naming the Component without that label is shown by its slug, so
// that it is not left out of the row.
func variantNames(spaces []*goclientnew.ExtendedSpace) []string {
	names := make([]string, 0, len(spaces))
	for _, extendedSpace := range spaces {
		if extendedSpace == nil || extendedSpace.Space == nil {
			continue
		}
		if variant := extendedSpace.Space.Labels["Variant"]; variant != "" {
			names = append(names, variant)
		} else {
			names = append(names, extendedSpace.Space.Slug)
		}
	}
	sort.Strings(names)
	return names
}

func displayComponentEntityList(components []*goclientnew.ExtendedComponent) {
	if displayRequestedColumns(components, componentAliases, nil) {
		return
	}
	byComponent, err := spacesByComponent()
	failOnError(err)
	table := tableView()
	if !noheader {
		table.SetHeader([]string{"Name", "Workflow-Required", "#Allowed-Workflows", "Owner", "Variants"})
	}
	rows := make([][]string, 0, len(components))
	for _, c := range components {
		component := c.Component
		spaces := byComponent[component.ComponentID]
		rows = append(rows, []string{
			component.Slug,
			fmt.Sprintf("%t", component.ChangeWorkflowRequired),
			fmt.Sprintf("%d", len(component.AllowedChangeWorkflowIDs)),
			componentOwner(component.Labels, spaces),
			strings.Join(variantNames(spaces), ", "),
		})
	}
	width := lastColumnWidth(rows, []string{"Name", "Workflow-Required", "#Allowed-Workflows", "Owner", "Variants"}, terminalWidth())
	for _, row := range rows {
		row[len(row)-1] = wrapList(row[len(row)-1], width)
		table.Append(row)
	}
	table.Render()
}

// terminalWidth is the width of the terminal stdout is, or 0 when stdout is not a terminal:
// a pipeline reads one row per line, so nothing is wrapped for it.
func terminalWidth() int {
	if !term.IsTerminal(int(os.Stdout.Fd())) {
		return 0
	}
	width, _, err := term.GetSize(int(os.Stdout.Fd()))
	if err != nil {
		return 0
	}
	return width
}

// lastColumnWidth is how wide a table's last column may be before its text wraps: what is
// left of a terminal of the given width after the other columns and the padding between
// them, and at least minWrapWidth so that a narrow window still gets a readable list. A
// width of 0 means the output is not a terminal, and nothing wraps.
func lastColumnWidth(rows [][]string, header []string, terminal int) int {
	if terminal <= 0 {
		return 0
	}
	used := 0
	for column := 0; column < len(header)-1; column++ {
		widest := len(header[column])
		for _, row := range rows {
			widest = max(widest, len(row[column]))
		}
		used += widest + len(tablePadding)
	}
	return max(terminal-used, minWrapWidth)
}

// tablePadding is what tableView puts between columns, and minWrapWidth the narrowest a
// wrapped column is allowed to be.
const (
	tablePadding = "    "
	minWrapWidth = 30
)

// wrapList breaks a comma-separated list onto lines no wider than width, at the spaces
// after the commas, so that a cell holding many names reads down rather than off the
// screen. A width of 0 leaves it on one line.
func wrapList(list string, width int) string {
	if width <= 0 {
		return list
	}
	lines, _ := tablewriter.WrapString(list, width)
	return strings.Join(lines, "\n")
}
