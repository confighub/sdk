// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"fmt"
	"time"

	"github.com/confighub/sdk/core/cubapi"
	goclientnew "github.com/confighub/sdk/core/openapi/goclient-new"
	"github.com/spf13/cobra"
)

var serviceAccountTokenCmd = &cobra.Command{
	Use:   "token",
	Short: "Manage the long-lived tokens a service account authenticates with",
	Long: getCommandHelp(`Manage the long-lived tokens issued to a service account.

A long-lived token is a ConfigHub token with no expiry, for a consumer that can
hold only a static credential: a username/password field in a third-party
system, such as an Argo CD repository Secret. Anything that can sign an
assertion ("cub auth login --private-key") should use a key instead.

The token is shown once, when it is issued, and ConfigHub does not keep it.
What it keeps is a record per token, so a token can be revoked on its own, and
so an operator can see which tokens exist, who issued them, and when each was
last used.

Issuing or revoking a token is acting as the service account: whoever holds
the token authenticates as it until the token is revoked. It requires
Impersonate permission on the service account, which Manage does not include.
A service account cannot issue a token for itself. Listing tokens requires
View.`, ""),
}

func init() {
	serviceAccountCmd.AddCommand(serviceAccountTokenCmd)
}

var (
	serviceAccountTokenDescription string
	serviceAccountTokenExpiresIn   time.Duration
)

var serviceAccountTokenCreateCmd = &cobra.Command{
	Use:   "create <service account>",
	Short: "Issue a long-lived token for a service account",
	Long: getCommandHelp(`Issue a long-lived token that authenticates as a service account.

The token is printed once and cannot be shown again; store it where the
consumer will read it. By default it never expires. --expires-in sets an
expiry, which a server may require by policy.

The token is a bearer token for the API, and a registry client uses it as a
password with the username "token":

    docker login <oci host> --username token --password-stdin

Examples:
`+"```"+`
  # Issue a token for the service account Argo CD pulls as
  cub serviceaccount token create argo-prod --description "Argo CD, prod cluster"

  # A token that expires in a year
  cub serviceaccount token create argo-prod --expires-in 8760h

  # Just the token, for a script
  cub serviceaccount token create argo-prod -o jq=.Token
`+"```"+`
`, ""),
	Args: cobra.ExactArgs(1),
	RunE: func(_ *cobra.Command, args []string) error {
		if serviceAccountTokenExpiresIn < 0 {
			return fmt.Errorf("--expires-in must be a positive duration")
		}
		serviceAccount, err := resolveServiceAccount(args[0], "ServiceAccountID,Slug")
		if err != nil {
			return err
		}
		body := goclientnew.CreateServiceAccountTokenJSONRequestBody{Description: serviceAccountTokenDescription}
		if serviceAccountTokenExpiresIn > 0 {
			body.ExpiresAt = time.Now().Add(serviceAccountTokenExpiresIn)
		}
		issuedRes, err := cubClientNew.CreateServiceAccountTokenWithResponse(ctx, serviceAccount.ServiceAccount.ServiceAccountID.String(), &goclientnew.CreateServiceAccountTokenParams{DryRun: dryRunParam()}, body)
		if cubapi.IsAPIError(err, issuedRes) {
			return cubapi.InterpretErrorGeneric(err, issuedRes)
		}
		displayGetResults(issuedRes.JSON200, displayIssuedToken)
		return nil
	},
}

var serviceAccountTokenListCmd = &cobra.Command{
	Use:   "list <service account>",
	Short: "List the long-lived tokens issued to a service account",
	Long: getCommandHelp(`List the long-lived tokens issued to a service account, revoked ones
included.

The tokens themselves are not shown; ConfigHub does not have them. Each entry
carries the token's id (its jti), who issued it, when it expires, when it was
last used, and whether it has been revoked.

Last-Used is what makes a token safe to revoke: a token nothing has used is a
token nothing depends on.

Examples:
`+"```"+`
  cub serviceaccount token list argo-prod

  # Just the ids, for scripting
  cub serviceaccount token list argo-prod -o jq='.[].Jti'
`+"```"+`
`, ""),
	Args: cobra.ExactArgs(1),
	RunE: func(_ *cobra.Command, args []string) error {
		serviceAccount, err := resolveServiceAccount(args[0], "ServiceAccountID,Slug")
		if err != nil {
			return err
		}
		tokensRes, err := cubClientNew.ListServiceAccountTokensWithResponse(ctx, serviceAccount.ServiceAccount.ServiceAccountID.String())
		if cubapi.IsAPIError(err, tokensRes) {
			return cubapi.InterpretErrorGeneric(err, tokensRes)
		}
		tokens := make([]*goclientnew.UserToken, 0, len(*tokensRes.JSON200))
		for i := range *tokensRes.JSON200 {
			tokens = append(tokens, &(*tokensRes.JSON200)[i])
		}
		displayListResults(tokens, getJtiForToken, displayTokenList)
		return nil
	},
}

var serviceAccountTokenRevokeCmd = &cobra.Command{
	Use:   "revoke <service account> <jti>",
	Short: "Revoke a service account's long-lived token",
	Long: getCommandHelp(`Revoke one long-lived token, by its id.

The token stops working within a minute on every server. Its record stays,
marked revoked, so the list still shows it was issued. The service account's
other credentials keep working.

To rotate without an outage, issue the new token first, move the consumer
over, confirm the new token's Last-Used is moving, and only then revoke the
old one.

Examples:
`+"```"+`
  cub serviceaccount token revoke argo-prod 6f1c2a9e-3b7d-4e0a-9c11-2d5f8b7a4c3e
`+"```"+`
`, ""),
	Args: cobra.ExactArgs(2),
	RunE: func(_ *cobra.Command, args []string) error {
		serviceAccount, err := resolveServiceAccount(args[0], "ServiceAccountID,Slug")
		if err != nil {
			return err
		}
		jti := args[1]
		revokeRes, err := cubClientNew.RevokeServiceAccountTokenWithResponse(ctx, serviceAccount.ServiceAccount.ServiceAccountID.String(), jti)
		if cubapi.IsAPIError(err, revokeRes) {
			return cubapi.InterpretErrorGeneric(err, revokeRes)
		}
		if !quiet && !isAlternativeOutput() {
			tprint("Revoked token %s", jti)
		}
		renderPayload(revokeRes.JSON200)
		return nil
	},
}

func init() {
	serviceAccountTokenCreateCmd.Flags().StringVar(&serviceAccountTokenDescription, "description", "",
		"note recording which system holds the token")
	serviceAccountTokenCreateCmd.Flags().DurationVar(&serviceAccountTokenExpiresIn, "expires-in", 0,
		"how long the token stays valid, such as 720h; unset means it does not expire")
	addStandardDisplayFlags(serviceAccountTokenCreateCmd)
	enableDryRunFlag(serviceAccountTokenCreateCmd)
	serviceAccountTokenCmd.AddCommand(serviceAccountTokenCreateCmd)

	addStandardListFlags(serviceAccountTokenListCmd)
	serviceAccountTokenCmd.AddCommand(serviceAccountTokenListCmd)

	addStandardDisplayFlags(serviceAccountTokenRevokeCmd)
	serviceAccountTokenCmd.AddCommand(serviceAccountTokenRevokeCmd)
}

func getJtiForToken(token *goclientnew.UserToken) string {
	return token.Jti
}

func displayIssuedToken(issued *goclientnew.IssuedUserToken) {
	if issued.UserToken != nil {
		displayTokenDetails(issued.UserToken)
	}
	fmt.Println()
	fmt.Println("Token (shown once; ConfigHub does not keep it):")
	fmt.Println(issued.Token)
}

func displayTokenDetails(token *goclientnew.UserToken) {
	view := tableView()
	view.Append([]string{"Jti", token.Jti})
	view.Append([]string{"User ID", token.UserID.String()})
	view.Append([]string{"Description", token.Description})
	view.Append([]string{"Issued By", token.CreatedByUserID.String()})
	view.Append([]string{"Created At", token.CreatedAt.String()})
	view.Append([]string{"Expires At", formatExpiry(token.ExpiresAt)})
	view.Append([]string{"Last Used At", formatLastUsed(token.LastUsedAt)})
	view.Append([]string{"Status", tokenStatus(token)})
	view.Render()
}

func displayTokenList(tokens []*goclientnew.UserToken) {
	if displayRequestedColumns(tokens, nil, nil) {
		return
	}
	table := tableView()
	if !noheader {
		table.SetHeader([]string{"Jti", "Description", "Created", "Expires", "Last-Used", "Status"})
	}
	for _, token := range tokens {
		table.Append([]string{
			token.Jti,
			token.Description,
			token.CreatedAt.Format("2006-01-02"),
			formatExpiry(token.ExpiresAt),
			formatLastUsed(token.LastUsedAt),
			tokenStatus(token),
		})
	}
	table.Render()
}

func formatExpiry(expires time.Time) string {
	if expires.IsZero() {
		return "never"
	}
	return expires.Format("2006-01-02")
}

func tokenStatus(token *goclientnew.UserToken) string {
	switch {
	case !token.RevokedAt.IsZero():
		return "revoked " + token.RevokedAt.Format("2006-01-02")
	case !token.ExpiresAt.IsZero() && !token.ExpiresAt.After(time.Now()):
		return "expired"
	default:
		return "active"
	}
}
