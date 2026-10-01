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
  cub group list -o jq='.[].GroupID'
`+"```"+`
`, ""),
	RunE: groupListCmdRun,
}

// Group-specific aliases
var groupAliases = map[string]string{
	"Name": "Group.DisplayName",
	"ID":   "Group.GroupID",
}

func init() {
	addStandardListFlags(groupListCmd)
	groupCmd.AddCommand(groupListCmd)
}

func groupListCmdRun(cmd *cobra.Command, args []string) error {
	groups, err := cubapi.ListGroups(ctx, cubClient, cubapi.NewWhere(where), cubapi.ListOpts{
		Filter:        filter,
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
