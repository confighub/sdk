// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	goclientnew "github.com/confighub/sdk/core/openapi/goclient-new"
	"github.com/spf13/cobra"
)

var userGetCmd = &cobra.Command{
	Use:   "get <user>",
	Short: "Get details about a user",
	Args:  cobra.ExactArgs(1),
	Long: getCommandHelp(`Get detailed information about a user.

Examples:
`+"```"+`
  # Get details about a user
  cub user get --json my-user

  # Get the user's ID
  cub user get my-user -o jq=.User.UserID
`+"```"+`
`, ""),
	RunE: userGetCmdRun,
}

func init() {
	addStandardGetFlags(userGetCmd)
	userCmd.AddCommand(userGetCmd)
}

// TODO: select

func userGetCmdRun(cmd *cobra.Command, args []string) error {
	extendedUser, err := resolveUser(args[0], selectFields)
	if err != nil {
		return err
	}
	displayGetResults(extendedUser, displayExtendedUserDetails)
	return nil
}

// displayExtendedUserDetails renders what get returns: the User wrapped the way every other
// get's entity is, so -o json and -o jq read it as .User, as list does.
func displayExtendedUserDetails(extendedUser *goclientnew.ExtendedUser) {
	displayUserDetails(extendedUser.User)
}

func displayUserDetails(member *goclientnew.User) {
	view := tableView()
	view.Append([]string{"User ID", member.UserID.String()})
	view.Append([]string{"External ID", member.ExternalID})
	view.Append([]string{"Display Name", member.DisplayName})
	view.Append([]string{"Username", member.Username})
	view.Render()
}
