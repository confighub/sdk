// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"github.com/confighub/sdk/core/cubapi"
	goclientnew "github.com/confighub/sdk/core/openapi/goclient-new"
	"github.com/spf13/cobra"
)

var userListCmd = &cobra.Command{
	Use:   "list",
	Short: "List users",
	Long: getCommandHelp(`List users you have access to in organizations to which you belong.

Examples:
`+"```"+`
  # List all users with headers
  cub user list

  # List user without headers for scripting
  cub user list --no-headers

  # List user in JSON format
  cub user list -o json

  # List user with custom JQ filter
  cub user list -o jq='.[].User.UserID'
`+"```"+`
`, ""),
	RunE: userListCmdRun,
}

// Default columns to display when no custom columns are specified
var defaultUserColumns = []string{"User.UserID", "User.ExternalID", "User.DisplayName", "User.Username"}

// userBaseSelectFields are the fields always returned by user list queries. Username is among
// them because it is what names a user, in -o name and to user get.
var userBaseSelectFields = []string{"Slug", "UserID", "Username"}

// User-specific aliases
var userAliases = map[string]string{
	"Name": "User.DisplayName",
	"ID":   "User.UserID",
}

// User custom column dependencies
var userCustomColumnDependencies = map[string][]string{}

func init() {
	addStandardListFlags(userListCmd)
	userCmd.AddCommand(userListCmd)
}

func userListCmdRun(cmd *cobra.Command, args []string) error {
	filterID, err := parseFilterFlag(filter)
	if err != nil {
		return err
	}

	selectValue := handleSelectParameter(selectFields, selectFields, func() string {
		return buildSelectList("User", listColumnsFor("cub user list"), "", defaultUserColumns, userAliases, userCustomColumnDependencies, userBaseSelectFields)
	})
	users, err := cubapi.ListUsers(ctx, cubClient, cubapi.NewWhere(where), cubapi.ListOpts{
		Select:        cubapi.SelectFields(selectValue),
		Filter:        filterID,
		Contains:      contains,
		IncludeHidden: includeHidden,
	})
	if err != nil {
		return err
	}
	displayListResults(users, getSlugForUser, displayUserList)
	return nil
}

func getSlugForUser(userDetails *goclientnew.ExtendedUser) string {
	// Return the username because get expects the username
	return userDetails.User.Username
}

func displayUserList(users []*goclientnew.ExtendedUser) {
	if displayRequestedColumns(users, userAliases, nil) {
		return
	}
	table := tableView()
	if !noheader {
		table.SetHeader([]string{"User-ID", "External-ID", "Name", "Username"})
	}
	for _, extendedUser := range users {
		user := extendedUser.User
		table.Append([]string{
			user.UserID.String(),
			user.ExternalID,
			user.DisplayName,
			user.Username,
		})
	}
	table.Render()
}
