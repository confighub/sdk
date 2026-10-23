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
		body := goclientnew.CreateServiceAccountUserTokenJSONRequestBody{Description: serviceAccountTokenDescription}
		if serviceAccountTokenExpiresIn > 0 {
			body.ExpiresAt = time.Now().Add(serviceAccountTokenExpiresIn)
		}
		issuedRes, err := cubClientNew.CreateServiceAccountUserTokenWithResponse(ctx, serviceAccount.ServiceAccount.ServiceAccountID.String(), &goclientnew.CreateServiceAccountUserTokenParams{DryRun: dryRunParam()}, body)
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
  cub serviceaccount token list argo-prod -o jq='.[].UserToken.Jti'
`+"```"+`
`, ""),
	Args: cobra.ExactArgs(1),
	RunE: func(_ *cobra.Command, args []string) error {
		serviceAccount, err := resolveServiceAccount(args[0], "ServiceAccountID,Slug")
		if err != nil {
			return err
		}
		tokens, err := cubapi.ListServiceAccountUserTokens(ctx, cubClient, serviceAccount.ServiceAccount.ServiceAccountID.String(),
			cubapi.NewWhere(where), credentialListOpts())
		if err != nil {
			return err
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
		serviceAccountID := serviceAccount.ServiceAccount.ServiceAccountID.String()
		jti := args[1]
		token, err := resolveServiceAccountToken(serviceAccountID, jti)
		if err != nil {
			return err
		}
		revokeRes, err := cubClientNew.RevokeServiceAccountUserTokenWithResponse(ctx, serviceAccountID, token.UserTokenID.String())
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

var serviceAccountTokenGetCmd = &cobra.Command{
	Use:   "get <service account> <jti>",
	Short: "Show one of a service account's long-lived tokens",
	Long: getCommandHelp(`Show the record of one long-lived token, by its id.

The token itself is not shown; ConfigHub does not have it.

Examples:
`+"```"+`
  cub serviceaccount token get argo-prod 6f1c2a9e-3b7d-4e0a-9c11-2d5f8b7a4c3e
`+"```"+`
`, ""),
	Args: cobra.ExactArgs(2),
	RunE: func(_ *cobra.Command, args []string) error {
		serviceAccount, err := resolveServiceAccount(args[0], "ServiceAccountID,Slug")
		if err != nil {
			return err
		}
		serviceAccountID := serviceAccount.ServiceAccount.ServiceAccountID.String()
		token, err := resolveServiceAccountToken(serviceAccountID, args[1])
		if err != nil {
			return err
		}
		tokenRes, err := cubClientNew.GetServiceAccountUserTokenWithResponse(ctx, serviceAccountID, token.UserTokenID.String(), &goclientnew.GetServiceAccountUserTokenParams{})
		if cubapi.IsAPIError(err, tokenRes) {
			return cubapi.InterpretErrorGeneric(err, tokenRes)
		}
		displayGetResults(tokenRes.JSON200, func(extended *goclientnew.ExtendedUserToken) {
			displayTokenDetails(extended.UserToken)
		})
		return nil
	},
}

var serviceAccountTokenDeleteCmd = &cobra.Command{
	Use:   "delete <service account> <jti>",
	Short: "Delete a service account's long-lived token",
	Long: getCommandHelp(`Delete the record of one long-lived token, by its id, to tidy the list.

A token that is still live stops working within a minute on every server, as a
revoked one does, and is refused afterwards as unknown. Revoke it instead to
keep the record that it was issued.

Examples:
`+"```"+`
  cub serviceaccount token delete argo-prod 6f1c2a9e-3b7d-4e0a-9c11-2d5f8b7a4c3e
`+"```"+`
`, ""),
	Args: cobra.ExactArgs(2),
	RunE: func(_ *cobra.Command, args []string) error {
		serviceAccount, err := resolveServiceAccount(args[0], "ServiceAccountID,Slug")
		if err != nil {
			return err
		}
		serviceAccountID := serviceAccount.ServiceAccount.ServiceAccountID.String()
		jti := args[1]
		token, err := resolveServiceAccountToken(serviceAccountID, jti)
		if err != nil {
			return err
		}
		deleteRes, err := cubClientNew.DeleteServiceAccountUserTokenWithResponse(ctx, serviceAccountID, token.UserTokenID.String())
		if cubapi.IsAPIError(err, deleteRes) {
			return cubapi.InterpretErrorGeneric(err, deleteRes)
		}
		displayDeleteResults("token", jti, jti, deleteRes.JSON200)
		return nil
	},
}

// resolveServiceAccountToken finds the service account's token with the jti, which is how a token
// is named on the command line and in its own claims; the API addresses it by its ID.
func resolveServiceAccountToken(serviceAccountID, jti string) (*goclientnew.UserToken, error) {
	tokens, err := cubapi.ListServiceAccountUserTokens(ctx, cubClient, serviceAccountID,
		cubapi.Where{}.Eq("Jti", jti), cubapi.ListOpts{Select: "UserTokenID,Jti"})
	if err != nil {
		return nil, err
	}
	if len(tokens) == 0 || tokens[0].UserToken == nil {
		return nil, fmt.Errorf("the service account has no token %s", jti)
	}
	return tokens[0].UserToken, nil
}

func init() {
	serviceAccountTokenCreateCmd.Flags().StringVar(&serviceAccountTokenDescription, "description", "",
		"note recording which system holds the token")
	serviceAccountTokenCreateCmd.Flags().DurationVar(&serviceAccountTokenExpiresIn, "expires-in", 0,
		"how long the token stays valid, such as 720h; unset means it does not expire")
	addStandardDisplayFlags(serviceAccountTokenCreateCmd)
	enableDryRunFlag(serviceAccountTokenCreateCmd)
	serviceAccountTokenCmd.AddCommand(serviceAccountTokenCreateCmd)

	// The endpoint takes no filter or view.
	enableWhereFlag(serviceAccountTokenListCmd)
	enableContainsFlag(serviceAccountTokenListCmd)
	enableSelectFlag(serviceAccountTokenListCmd)
	addStandardListDisplayFlags(serviceAccountTokenListCmd)
	enableListPagingFlags(serviceAccountTokenListCmd)
	serviceAccountTokenCmd.AddCommand(serviceAccountTokenListCmd)

	addStandardDisplayFlags(serviceAccountTokenRevokeCmd)
	serviceAccountTokenCmd.AddCommand(serviceAccountTokenRevokeCmd)

	addStandardDisplayFlags(serviceAccountTokenGetCmd)
	serviceAccountTokenCmd.AddCommand(serviceAccountTokenGetCmd)

	addStandardDisplayFlags(serviceAccountTokenDeleteCmd)
	serviceAccountTokenCmd.AddCommand(serviceAccountTokenDeleteCmd)
}

// tokenAliases name a listed token's columns as a User's are named.
var tokenAliases = map[string]string{
	"Name": "UserToken.DisplayName",
	"ID":   "UserToken.UserTokenID",
}

func getJtiForToken(token *goclientnew.ExtendedUserToken) string {
	return token.UserToken.Jti
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

func displayTokenList(tokens []*goclientnew.ExtendedUserToken) {
	if displayRequestedColumns(tokens, tokenAliases, nil) {
		return
	}
	table := tableView()
	if !noheader {
		table.SetHeader([]string{"Jti", "Description", "Created", "Expires", "Last-Used", "Status"})
	}
	for _, extended := range tokens {
		token := extended.UserToken
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
