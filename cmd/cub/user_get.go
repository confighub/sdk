// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"fmt"

	goclientnew "github.com/confighub/sdk/openapi/goclient-new"
	"github.com/google/uuid"
	"github.com/spf13/cobra"
)

var userGetCmd = &cobra.Command{
	Use:   "get <user>",
	Short: "Get details about a user",
	Args:  cobra.ExactArgs(1),
	Long: `Get detailed information about a user.

Examples:
  # Get details about a user
  cub user get --json my-user

`,
	RunE: userGetCmdRun,
}

func init() {
	addStandardGetFlags(userGetCmd)
	userCmd.AddCommand(userGetCmd)
}

func userGetCmdRun(cmd *cobra.Command, args []string) error {
	userDetails, err := apiGetUserFromUsername(args[0])
	if err != nil {
		return err
	}

	// the previous call got the list resource. We want the "detail" resource just in case they're different
	exUserDetails, err := apiGetUser(userDetails.UserID.String())
	if err != nil {
		return err
	}
	displayGetResults(exUserDetails, displayUserDetails)
	return nil
}

func displayUserDetails(member *goclientnew.User) {
	view := tableView()
	view.Append([]string{"User ID", member.UserID.String()})
	view.Append([]string{"External ID", member.ExternalID})
	view.Append([]string{"Display Name", member.DisplayName})
	view.Append([]string{"Username", member.Username})
	view.Render()
}

func apiGetUser(userID string) (*goclientnew.User, error) {
	// No params currently
	// newParams := &goclientnew.GetUserParams{}
	orgMemberRes, err := cubClientNew.GetUserWithResponse(ctx, uuid.MustParse(userID) /*, newParams*/)
	if IsAPIError(err, orgMemberRes) {
		return nil, InterpretErrorGeneric(err, orgMemberRes)
	}
	return orgMemberRes.JSON200, nil
}

func apiGetUserFromUsername(username string) (*goclientnew.User, error) {
	id, err := uuid.Parse(username)
	if err == nil {
		return apiGetUser(id.String())
	}
	users, err := apiListUsers("Username = '" + username + "'")
	if err != nil {
		return nil, err
	}
	// find member by username
	for _, userDetails := range users {
		if userDetails.Username == username {
			return userDetails, nil
		}
	}
	return nil, fmt.Errorf("user %s not found", username)
}
