// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"fmt"
	"strings"

	"github.com/confighub/sdk/core/cubapi"
	goclientnew "github.com/confighub/sdk/core/openapi/goclient-new"
	"github.com/spf13/cobra"
)

var oauthClientGetCmd = &cobra.Command{
	Use:   "get <name or client id>",
	Short: "Get an OAuth client",
	Long:  getCommandHelp(`Get details for an OAuth client registered in your organization, including its client_id and redirect URIs.`, ""),
	Args:  cobra.ExactArgs(1),
	RunE:  oauthClientGetCmdRun,
}

func init() {
	addStandardDisplayFlags(oauthClientGetCmd)
	oauthClientCmd.AddCommand(oauthClientGetCmd)
}

func oauthClientGetCmdRun(cmd *cobra.Command, args []string) error {
	client, err := apiGetOAuthClient(args[0])
	if err != nil {
		return err
	}
	displayGetResults(client, displayOAuthClientDetails)
	return nil
}

// apiGetOAuthClient resolves an OAuth client by its client_id, or by its human-friendly
// name (the API addresses clients by client_id, so a name is resolved via list).
func apiGetOAuthClient(nameOrClientID string) (*goclientnew.OAuthClient, error) {
	res, err := cubClientNew.GetOAuthClientWithResponse(ctx, nameOrClientID)
	if err == nil && res.JSON200 != nil {
		return res.JSON200, nil
	}

	listRes, listErr := cubClientNew.ListOAuthClientsWithResponse(ctx)
	if cubapi.IsAPIError(listErr, listRes) {
		// Surface the original get error if the list also fails.
		if cubapi.IsAPIError(err, res) {
			return nil, cubapi.InterpretErrorGeneric(err, res)
		}
		return nil, cubapi.InterpretErrorGeneric(listErr, listRes)
	}
	for i := range *listRes.JSON200 {
		client := &(*listRes.JSON200)[i]
		if client.Name == nameOrClientID || client.ClientID == nameOrClientID {
			return client, nil
		}
	}
	return nil, fmt.Errorf("no OAuth client found matching %q", nameOrClientID)
}

func displayOAuthClientDetails(client *goclientnew.OAuthClient) {
	scope := "owning org only"
	if client.AllowAllOrgs {
		scope = "all organizations"
	}
	view := tableView()
	view.Append([]string{"Name", client.Name})
	view.Append([]string{"Client ID", client.ClientID})
	view.Append([]string{"Organization", client.OrganizationID})
	view.Append([]string{"Available to", scope})
	view.Append([]string{"Redirect URIs", strings.Join(client.RedirectURIs, "\n")})
	view.Render()
}
