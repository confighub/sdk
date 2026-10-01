// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	goclientnew "github.com/confighub/sdk/core/openapi/goclient-new"
	"github.com/spf13/cobra"
)

var groupGetCmd = &cobra.Command{
	Use:   "get <group>",
	Short: "Get details about a group",
	Args:  cobra.ExactArgs(1),
	Long: getCommandHelp(`Get detailed information about a group you belong to, by slug or ID.

Examples:
`+"```"+`
  # Get details about a group
  cub group get --json my-group
`+"```"+`
`, ""),
	RunE: groupGetCmdRun,
}

func init() {
	addStandardGetFlags(groupGetCmd)
	groupCmd.AddCommand(groupGetCmd)
}

func groupGetCmdRun(cmd *cobra.Command, args []string) error {
	extendedGroup, err := resolveGroup(args[0], "")
	if err != nil {
		return err
	}
	displayGetResults(extendedGroup, displayExtendedGroupDetails)
	return nil
}

// displayExtendedGroupDetails renders what get returns: the Group wrapped the way every other
// get's entity is, so -o json and -o jq read it as .Group, as list does.
func displayExtendedGroupDetails(extendedGroup *goclientnew.ExtendedGroup) {
	displayGroupDetails(extendedGroup.Group)
}

func displayGroupDetails(group *goclientnew.Group) {
	view := tableView()
	view.Append([]string{"Group ID", group.GroupID.String()})
	view.Append([]string{"External ID", group.ExternalID})
	view.Append([]string{"Display Name", group.DisplayName})
	view.Append([]string{"Slug", group.Slug})
	view.Render()
}
