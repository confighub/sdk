// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"github.com/confighub/sdk/core/cubapi"
	goclientnew "github.com/confighub/sdk/core/openapi/goclient-new"
	"github.com/spf13/cobra"
)

var oauthClientRedirectURIs []string
var oauthClientAllowAllOrgs bool

var oauthClientCreateCmd = &cobra.Command{
	Use:   "create <name>",
	Short: "Register an OAuth client",
	Long: getCommandHelp(`Register a per-app OAuth public client for your organization (RFC 7591). The client is a browser app: it holds no secret, enforces PKCE (S256), and is restricted to the exact redirect URIs you supply.

By default the app is restricted to your organization — only your members can log in through it. --allow-all-orgs makes it usable by members of any organization (each gets their own org's session); this is permitted only for trusted (first-party) organizations.

Examples:
`+"```"+`
  # Register an app with a single redirect URI
  cub oauthclient create my-dashboard --redirect-uri https://dash.example.com/callback

  # Register an app with multiple redirect URIs
  cub oauthclient create my-dashboard \
    --redirect-uri https://dash.example.com/callback \
    --redirect-uri http://localhost:5173/callback

  # Register a first-party app available to all organizations
  cub oauthclient create shared-console --redirect-uri https://console.confighub.com/callback --allow-all-orgs
`+"```"+`
`, ""),
	Args: cobra.ExactArgs(1),
	RunE: oauthClientCreateCmdRun,
}

func init() {
	oauthClientCreateCmd.Flags().StringArrayVar(&oauthClientRedirectURIs, "redirect-uri", nil, "Exact redirect URI allowed at login (repeatable). Required.")
	oauthClientCreateCmd.Flags().BoolVar(&oauthClientAllowAllOrgs, "allow-all-orgs", false, "Allow members of any organization to use the app (trusted orgs only). Default: restricted to your org.")
	addStandardDisplayFlags(oauthClientCreateCmd)
	oauthClientCmd.AddCommand(oauthClientCreateCmd)
}

func oauthClientCreateCmdRun(cmd *cobra.Command, args []string) error {
	body := goclientnew.OAuthClient{
		Name:         args[0],
		RedirectURIs: oauthClientRedirectURIs,
		AllowAllOrgs: oauthClientAllowAllOrgs,
	}
	res, err := cubClientNew.CreateOAuthClientWithResponse(ctx, body)
	if cubapi.IsAPIError(err, res) {
		return cubapi.InterpretErrorGeneric(err, res)
	}
	displayCreateResults(res.JSON200, "oauthclient", args[0], res.JSON200.ClientID, displayOAuthClientDetails)
	return nil
}
