// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"fmt"

	"github.com/confighub/sdk/core/cubapi"
	goclientnew "github.com/confighub/sdk/core/openapi/goclient-new"
	"github.com/spf13/cobra"
)

var spaceListCmd = &cobra.Command{
	Use:   "list",
	Short: "List spaces",
	Long:  getSpaceListHelp(),
	RunE:  spaceListCmdRun,
}

func getSpaceListHelp() string {
	baseHelp := `List spaces you have access to in this organization. The output includes slugs, the standard space labels, and the number of units. Use --output=wide to also show the counts of links, tags, changesets, filters, views, invocations, triggers, workers, targets, and attributes.

Examples:
` + "```" + `
  # List all spaces with headers
  cub space list

  # List spaces with all summary counts
  cub space list --output=wide

  # List spaces without headers for scripting
  cub space list --no-headers

  # List spaces in JSON format
  cub space list -o json

  # List spaces with custom JQ filter
  cub space list -o jq='.[].Slug'

  # List spaces matching a specific criteria
  cub space list --where "Labels.Environment = 'prod'"
 ` + "```" + `
`

	agentContext := `Essential first step for discovering available spaces and setting up context.

Agent discovery workflow:
1. Run 'space list' immediately after authentication to see available spaces
2. Choose appropriate space for your operations
3. Set default context or use --space flag for subsequent commands

Common agent patterns:

Initial setup:
  # Discover available spaces
  cub space list -o jq='.[].Space.Slug'
  
  # Set default space context
  cub context set --space CHOSEN_SPACE

Environment-specific operations:
  # Find production spaces
  cub space list --where "Labels.Environment = 'prod'" -o name
  
  # Find staging spaces
  cub space list --where "Labels.Environment = 'staging'" -o name

Key information provided:
- Space slugs: Used for --space flag and context setting
- Standard labels: Component, Owner, Variant, Environment, Region, Layer
- Unit count: Number of units in each space; -o wide adds the remaining summary counts
- Organization context: Which org these spaces belong to

Important flags for agents:
- -o name: Get just space identifiers for automation
- -o wide: Show all summary counts (links, tags, changesets, filters, views, invocations, triggers, workers, targets, attributes)
- -o jq=<expr>: Extract specific fields for further processing
- -o json: Full JSON payload
- --where: Filter spaces by display name or other attributes
- --no-headers: Suppress table headers for clean output

Next steps after listing spaces:
1. Use 'context set --space SPACE_SLUG' to set default context
2. Use 'unit list --space SPACE_SLUG' to explore units in the space
3. Use 'function list --space SPACE_SLUG' to see available functions`

	return getCommandHelp(baseHelp, agentContext)
}

// Default columns to display when no custom columns are specified
var defaultSpaceColumns = []string{"Space.Slug", "Space.Labels", "Space.WhereTrigger", "TotalUnitCount", "TotalLinkCount", "TotalFilterCount", "TotalViewCount", "TotalTagCount", "TotalChangeSetCount", "TotalInvocationCount", "TriggerCountByEventType", "TotalBridgeWorkerCount", "TargetCountByToolchainType", "TotalAttributeCount"}

// spaceBaseSelectFields are the fields always returned by space list queries,
// regardless of the requested columns.
var spaceBaseSelectFields = []string{"Slug", "SpaceID", "OrganizationID"}

// Space-specific aliases
var spaceAliases = map[string]string{
	"Name": "Space.Slug",
	"ID":   "Space.SpaceID",
}

// spaceLabelColumns are the standard space labels, in the order they are
// displayed as columns by the space list command.
var spaceLabelColumns = []string{"Component", "Owner", "Variant", "Environment", "Region", "Layer"}

// Space custom column dependencies (e.g. Environment comes from Labels.Environment)
var spaceCustomColumnDependencies = func() map[string][]string {
	deps := make(map[string][]string, len(spaceLabelColumns))
	for _, label := range spaceLabelColumns {
		deps[label] = []string{"Labels"}
	}
	return deps
}()

func init() {
	addStandardListFlags(spaceListCmd)
	spaceCmd.AddCommand(spaceListCmd)
}

func spaceListCmdRun(cmd *cobra.Command, args []string) error {
	filterID, err := parseFilterFlag(filter)
	if err != nil {
		return err
	}
	extendedSpaces, err := apiListExtendedSpaces(where, selectFields, filterID, true)
	if err != nil {
		return err
	}
	displayListResults(extendedSpaces, getExtendedSpaceSlug, displayExtendedSpaceList)
	return nil
}

func getExtendedSpaceSlug(extendedSpace *goclientnew.ExtendedSpace) string {
	return extendedSpace.Space.Slug
}

// wideSpaceCountColumns are the count columns shown only with --output=wide.
// #Units is always shown, so it isn't listed here.
var wideSpaceCountColumns = []string{"#Links", "#Tags", "#ChangeSets", "#Filters", "#Views", "#Invocations", "#Triggers", "#Workers", "#Targets", "#Attributes"}

func wideSpaceCounts(extendedSpace *goclientnew.ExtendedSpace) []string {
	return []string{
		fmt.Sprintf("%d", extendedSpace.TotalLinkCount),
		fmt.Sprintf("%d", extendedSpace.TotalTagCount),
		fmt.Sprintf("%d", extendedSpace.TotalChangeSetCount),
		fmt.Sprintf("%d", extendedSpace.TotalFilterCount),
		fmt.Sprintf("%d", extendedSpace.TotalViewCount),
		fmt.Sprintf("%d", extendedSpace.TotalInvocationCount),
		fmt.Sprintf("%d", totalCountMap(extendedSpace.TriggerCountByEventType)),
		fmt.Sprintf("%d", extendedSpace.TotalBridgeWorkerCount),
		fmt.Sprintf("%d", totalCountMap(extendedSpace.TargetCountByToolchainType)),
		fmt.Sprintf("%d", extendedSpace.TotalAttributeCount),
	}
}

func displayExtendedSpaceList(extendedSpaces []*goclientnew.ExtendedSpace) {
	wide := effectiveOutput().Kind == OutputWide
	table := tableView()
	if !noheader {
		header := append([]string{"Name"}, spaceLabelColumns...)
		header = append(header, "#Units")
		if wide {
			header = append(header, wideSpaceCountColumns...)
		}
		table.SetHeader(header)
	}
	for _, extendedSpace := range extendedSpaces {
		row := []string{extendedSpace.Space.Slug}
		for _, label := range spaceLabelColumns {
			row = append(row, extendedSpace.Space.Labels[label])
		}
		row = append(row, fmt.Sprintf("%d", extendedSpace.TotalUnitCount))
		if wide {
			row = append(row, wideSpaceCounts(extendedSpace)...)
		}
		table.Append(row)
	}
	table.Render()
}

// apiListSpaces lists spaces and returns just their core records, delegating to
// apiListExtendedSpaces.
func apiListSpaces(whereFilter string, selectParam string) ([]*goclientnew.Space, error) {
	extendedSpaces, err := apiListExtendedSpaces(whereFilter, selectParam, "", false)
	if err != nil {
		return nil, err
	}
	spaces := make([]*goclientnew.Space, 0, len(extendedSpaces))
	for _, extendedSpace := range extendedSpaces {
		if extendedSpace.Space != nil {
			spaces = append(spaces, extendedSpace.Space)
		}
	}
	return spaces, nil
}

// apiListExtendedSpaces lists spaces. When summary is true the per-space counts
// (units, links, targets, triggers, …) are computed and returned, as displayed
// by the space list command; callers that only need the core Space records pass
// false to avoid that work.
func apiListExtendedSpaces(whereFilter string, selectParam string, filterParam string, summary bool) ([]*goclientnew.ExtendedSpace, error) {
	selectValue := handleSelectParameter(selectParam, selectFields, func() string {
		return buildSelectList("Space", nil, "", defaultSpaceColumns, spaceAliases, spaceCustomColumnDependencies, spaceBaseSelectFields)
	})
	var with []func(*goclientnew.ListSpacesParams)
	if summary {
		with = append(with, func(p *goclientnew.ListSpacesParams) {
			s := true
			p.Summary = &s
		})
	}
	return cubapi.ListSpaces(ctx, cubClient, cubapi.NewWhere(whereFilter), cubapi.ListOpts{
		Select:   cubapi.SelectFields(selectValue),
		Filter:   filterParam,
		Contains: contains,
	}, with...)
}
