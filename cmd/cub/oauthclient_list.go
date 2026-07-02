// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"strings"

	"github.com/confighub/sdk/core/cubapi"
	goclientnew "github.com/confighub/sdk/core/openapi/goclient-new"
	"github.com/spf13/cobra"
)

var oauthClientListCmd = &cobra.Command{
	Use:   "list",
	Short: "List OAuth clients",
	Long: getCommandHelp(`List the OAuth clients registered for your organization.

Examples:
`+"```"+`
  # List all OAuth clients
  cub oauthclient list

  # List without headers for scripting
  cub oauthclient list --no-headers

  # List in JSON format
  cub oauthclient list -o json
`+"```"+`
`, ""),
	Args: cobra.NoArgs,
	RunE: oauthClientListCmdRun,
}

func init() {
	addStandardListDisplayFlags(oauthClientListCmd)
	oauthClientCmd.AddCommand(oauthClientListCmd)
}

func oauthClientListCmdRun(cmd *cobra.Command, args []string) error {
	res, err := cubClientNew.ListOAuthClientsWithResponse(ctx)
	if cubapi.IsAPIError(err, res) {
		return cubapi.InterpretErrorGeneric(err, res)
	}
	clients := make([]*goclientnew.OAuthClient, 0, len(*res.JSON200))
	for i := range *res.JSON200 {
		clients = append(clients, &(*res.JSON200)[i])
	}
	displayListResults(clients, getNameForOAuthClient, displayOAuthClientList)
	return nil
}

func getNameForOAuthClient(client *goclientnew.OAuthClient) string {
	return client.ClientID
}

func displayOAuthClientList(clients []*goclientnew.OAuthClient) {
	table := tableView()
	if !noheader {
		table.SetHeader([]string{"Name", "Client-ID", "Available-To", "Redirect-URIs"})
	}
	for _, client := range clients {
		scope := "owning org"
		if client.AllowAllOrgs {
			scope = "all orgs"
		}
		table.Append([]string{
			client.Name,
			client.ClientID,
			scope,
			strings.Join(client.RedirectURIs, ", "),
		})
	}
	table.Render()
}
