// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"log"

	goclientnew "github.com/confighub/sdk/openapi/goclient-new"
	"github.com/spf13/cobra"
)

var userListCmd = &cobra.Command{
	Use:   "list",
	Short: "List users",
	Long: `List users you have access to in organizations to which you belong.

Examples:
  # List all users with headers
  cub user list

  # List user without headers for scripting
  cub user list --no-header

  # List user in JSON format
  cub user list --json

  # List user with custom JQ filter
  cub user list --jq '.[].UserID'

`,
	RunE: userListCmdRun,
}

// Default columns to display when no custom columns are specified
var defaultUserColumns = []string{"UserID", "ExternalID", "DisplayName", "Username"}

// User-specific aliases
var userAliases = map[string]string{
	"Name": "DisplayName",
	"ID":   "UserID",
}

// User custom column dependencies
var userCustomColumnDependencies = map[string][]string{}

func init() {
	addStandardListFlags(userListCmd)
	userCmd.AddCommand(userListCmd)
}

func userListCmdRun(cmd *cobra.Command, args []string) error {
	users, err := apiListUsers(where)
	if err != nil {
		return err
	}
	displayListResults(users, getSlugForUser, displayUserList)
	return nil
}

func getSlugForUser(userDetails *goclientnew.User) string {
	return userDetails.Slug
}

func displayUserList(users []*goclientnew.User) {
	table := tableView()
	if !noheader {
		table.SetHeader([]string{"User-ID", "External-ID", "Name", "Username"})
	}
	for _, orgMember := range users {
		table.Append([]string{
			orgMember.UserID.String(),
			orgMember.ExternalID,
			orgMember.DisplayName,
			orgMember.Username,
		})
	}
	table.Render()
}

// apiListUsers
func apiListUsers(whereFilter string) ([]*goclientnew.User, error) {
	newParams := &goclientnew.ListUsersParams{}
	if whereFilter != "" {
		log.Printf("where filter: %s", whereFilter)
		newParams.Where = &whereFilter
	}
	if contains != "" {
		newParams.Contains = &contains
	}
	// TODO: Add select parameter support when backend endpoint supports it
	// Auto-select fields based on default display if no custom output format is specified
	// if selectFields == "" {
	//     baseFields := []string{"Slug", "UserID"}
	//     autoSelect := buildSelectList("User", "", "", defaultUserColumns, userAliases, userCustomColumnDependencies, baseFields)
	//     newParams.Select = &autoSelect
	// } else if selectFields != "" {
	//     newParams.Select = &selectFields
	// }
	membersRes, err := cubClientNew.ListUsersWithResponse(ctx, newParams)
	if IsAPIError(err, membersRes) {
		return nil, InterpretErrorGeneric(err, membersRes)
	}

	users := make([]*goclientnew.User, 0, len(*membersRes.JSON200))
	for _, member := range *membersRes.JSON200 {
		users = append(users, &member)
	}
	return users, nil
}
