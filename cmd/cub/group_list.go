// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"github.com/confighub/sdk/core/cubapi"
	goclientnew "github.com/confighub/sdk/core/openapi/goclient-new"
	"github.com/spf13/cobra"
)

var groupListCmd = &cobra.Command{
	Use:   "list",
	Short: "List groups",
	Long: getCommandHelp(`List the groups you belong to.

Examples:
`+"```"+`
  # List your groups with headers
  cub group list

  # List your groups without headers for scripting
  cub group list --no-headers

  # List your groups in JSON format
  cub group list -o json

  # List your groups with custom JQ filter
  cub group list -o jq='.[].Group.GroupID'
`+"```"+`
`, ""),
	RunE: groupListCmdRun,
}

// Default columns to display when no custom columns are specified
var defaultGroupColumns = []string{"Group.GroupID", "Group.ExternalID", "Group.DisplayName", "Group.Slug"}

// groupBaseSelectFields are the fields always returned by group list queries. Slug is among
// them because it is what names a group, in -o name and to group get.
var groupBaseSelectFields = []string{"Slug", "GroupID"}

// Group-specific aliases
var groupAliases = map[string]string{
	"Name": "Group.DisplayName",
	"ID":   "Group.GroupID",
}

// Group custom column dependencies
var groupCustomColumnDependencies = map[string][]string{}

func init() {
	addStandardListFlags(groupListCmd)
	enableListPagingFlags(groupListCmd)
	groupCmd.AddCommand(groupListCmd)
}

func groupListCmdRun(cmd *cobra.Command, args []string) error {
	filterID, err := parseFilterFlag(filter)
	if err != nil {
		return err
	}

	selectValue := handleSelectParameter(selectFields, selectFields, func() string {
		return buildSelectList("Group", listColumnsFor("cub group list"), "", defaultGroupColumns, groupAliases, groupCustomColumnDependencies, groupBaseSelectFields)
	})
	groups, err := cubapi.ListGroups(ctx, cubClient, cubapi.NewWhere(where), cubapi.ListOpts{
		Limit:         listLimit,
		OrderBy:       listOrderBy,
		Select:        cubapi.SelectFields(selectValue),
		Filter:        filterID,
		Contains:      contains,
		IncludeHidden: includeHidden,
	})
	if err != nil {
		return err
	}
	displayListResults(groups, getSlugForGroup, displayGroupList)
	return nil
}

func getSlugForGroup(extendedGroup *goclientnew.ExtendedGroup) string {
	return extendedGroup.Group.Slug
}

func displayGroupList(groups []*goclientnew.ExtendedGroup) {
	if displayRequestedColumns(groups, groupAliases, nil) {
		return
	}
	table := tableView()
	if !noheader {
		table.SetHeader([]string{"Group-ID", "External-ID", "Name", "Slug"})
	}
	for _, extendedGroup := range groups {
		group := extendedGroup.Group
		table.Append([]string{
			group.GroupID.String(),
			group.ExternalID,
			group.DisplayName,
			group.Slug,
		})
	}
	table.Render()
}
