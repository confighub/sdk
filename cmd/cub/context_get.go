// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"fmt"

	"github.com/spf13/cobra"
)

var contextGetCmd = &cobra.Command{
	Use:   "get",
	Short: "Gets the current local context",
	Long: `Get information about the current local context including user details, organization, and space settings.

Examples:
  # Get current context information
  cub context get`,
	Args: cobra.ExactArgs(0),
	RunE: contextGetCmdRun,
}

var spaceSlugOnly bool

func init() {
	contextGetCmd.Flags().BoolVar(&spaceSlugOnly, "space-slug-only", false, "just print the space slug")
	contextCmd.AddCommand(contextGetCmd)
}

func contextGetCmdRun(_ *cobra.Command, _ []string) error {
	if spaceSlugOnly {
		tprint(cubContext.Space)
		return nil
	}
	view := tableView()
	view.Append([]string{"User Email", authSession.User.Email})
	view.Append([]string{"IDP User ID", authSession.User.ID})
	view.Append([]string{"IDP Organization ID", authSession.OrganizationID})
	view.Append([]string{"ConfigHub URL", cubContext.ConfigHubURL})
	view.Append([]string{"Space", fmt.Sprintf("%s (%s)", cubContext.Space, cubContext.SpaceID)})
	view.Append([]string{"Organization", fmt.Sprintf("%s", cubContext.OrganizationID)})
	view.Render()
	return nil
}
