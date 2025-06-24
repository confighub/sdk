// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"log"
	"net/url"

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
  cub user list --noheader

  # List user in JSON format
  cub user list --json

  # List user with custom JQ filter
  cub user list --jq '.[].UserID'

`,
	// TODO: where filter
	// # List user matching a specific criteria
	// cub user list --where "DisplayName contains 'prod'"
	RunE: userListCmdRun,
}

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
		whereFilter = url.QueryEscape(whereFilter)
		log.Printf("where filter: %s", whereFilter)
		newParams.Where = &whereFilter
	}
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
