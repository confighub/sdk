// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"encoding/json"
	"fmt"

	"github.com/confighub/sdk/core/cubapi"
	goclientnew "github.com/confighub/sdk/core/openapi/goclient-new"
	"github.com/spf13/cobra"
)

var serviceAccountKeyCmd = &cobra.Command{
	Use:   "key",
	Short: "Manage the public keys a service account authenticates with",
	Long: getCommandHelp(`Manage the public keys a service account authenticates with.

The holder of the matching private key authenticates as the service account by
signing a short-lived assertion, so ConfigHub never receives or stores a secret.

Registering or deleting a key is acting as the service account: whoever holds
the private key authenticates as it from then on. It requires Impersonate
permission on the service account, which Manage does not include. Whoever
creates a service account is given Impersonate on it, and only someone who has
Impersonate can grant it to another user. A service account cannot register a
key for itself. Listing keys requires View.`, ""),
}

func init() {
	serviceAccountCmd.AddCommand(serviceAccountKeyCmd)
}

var serviceAccountKeyAddCmd = &cobra.Command{
	Use:   "add <service account>",
	Short: "Register a public key for a service account",
	Long: getCommandHelp(`Register a public key for a service account.

Either generate a keypair here with --generate, which stores the private key
locally and registers the public half, or register a public key you already have
with --public-key.

--generate takes an alias or a path. A bare name is an alias stored under
~/.confighub/keys, which is what "cub auth login --private-key=<alias>" looks
up; anything containing a path separator is written there instead and has no
alias.

Examples:
`+"```"+`
  # Generate a keypair, storing it under ~/.confighub/keys as the alias "deploy-bot"
  cub serviceaccount key add deploy-bot --generate deploy-bot

  # ...then authenticate as the service account with it
  cub auth login --private-key=deploy-bot

  # Register a public key you already have, with a note about who holds it
  cub serviceaccount key add deploy-bot --public-key ./deploy-bot.pub.jwk \
      --description "CI runner, us-east"
`+"```"+`
`, ""),
	Args: cobra.ExactArgs(1),
	RunE: func(_ *cobra.Command, args []string) error {
		serviceAccount, err := resolveServiceAccount(args[0], "ServiceAccountID,Slug,UserID")
		if err != nil {
			return err
		}
		return addKey(
			func() (*goclientnew.User, error) { return serviceAccountUser(serviceAccount) },
			func(_ *goclientnew.User, publicJWK json.RawMessage, description string) (*goclientnew.UserKey, error) {
				return apiCreateServiceAccountKey(serviceAccount.ServiceAccount.ServiceAccountID.String(), publicJWK, description)
			})
	},
}

var serviceAccountKeyListCmd = &cobra.Command{
	Use:   "list <service account>",
	Short: "List the public keys registered for a service account",
	Long: getCommandHelp(`List the public keys a service account can authenticate with.

Keys are shown in full with -o json rather than redacted. They are public
material, and a key nobody registered is only noticeable if it is visible. The
Last-Used column is what makes a key safe to retire.

Examples:
`+"```"+`
  cub serviceaccount key list deploy-bot
`+"```"+`
`, ""),
	Args: cobra.ExactArgs(1),
	RunE: func(_ *cobra.Command, args []string) error {
		serviceAccount, err := resolveServiceAccount(args[0], "ServiceAccountID,Slug")
		if err != nil {
			return err
		}
		keys, err := cubapi.ListServiceAccountUserKeys(ctx, cubClient, serviceAccount.ServiceAccount.ServiceAccountID.String(),
			cubapi.NewWhere(where), credentialListOpts())
		if err != nil {
			return err
		}
		displayListResults(keys, getKidForServiceAccountKey, displayServiceAccountKeyList)
		return nil
	},
}

var serviceAccountKeyDeleteCmd = &cobra.Command{
	Use:   "delete <service account> <kid>",
	Short: "Remove a public key from a service account",
	Long: getCommandHelp(`Remove one of a service account's public keys, by its thumbprint.

Whatever holds that private key stops being able to authenticate; the service
account's other keys keep working. To rotate without an outage, add the new key
first, move whatever uses the old one over, confirm the new key's Last-Used is
moving, and only then delete the old one.

Examples:
`+"```"+`
  cub serviceaccount key delete deploy-bot NzbLsXh8uDCcd-6MNwXF4W_7noWXFZAfHkxZsRGC9Xs
`+"```"+`
`, ""),
	Args: cobra.ExactArgs(2),
	RunE: func(_ *cobra.Command, args []string) error {
		serviceAccount, err := resolveServiceAccount(args[0], "ServiceAccountID,Slug")
		if err != nil {
			return err
		}
		serviceAccountID := serviceAccount.ServiceAccount.ServiceAccountID.String()
		kid := args[1]
		key, err := resolveServiceAccountKey(serviceAccountID, kid)
		if err != nil {
			return err
		}
		deleteRes, err := cubClientNew.DeleteServiceAccountUserKeyWithResponse(ctx, serviceAccountID, key.UserKeyID.String())
		if cubapi.IsAPIError(err, deleteRes) {
			return cubapi.InterpretErrorGeneric(err, deleteRes)
		}
		displayDeleteResults("key", kid, kid, deleteRes.JSON200)
		return nil
	},
}

var serviceAccountKeyGetCmd = &cobra.Command{
	Use:   "get <service account> <kid>",
	Short: "Show one of a service account's public keys",
	Long: getCommandHelp(`Show one of a service account's public keys, by its thumbprint.

The key is shown in full with -o json rather than redacted, as in the list.

Examples:
`+"```"+`
  cub serviceaccount key get deploy-bot NzbLsXh8uDCcd-6MNwXF4W_7noWXFZAfHkxZsRGC9Xs -o json
`+"```"+`
`, ""),
	Args: cobra.ExactArgs(2),
	RunE: func(_ *cobra.Command, args []string) error {
		serviceAccount, err := resolveServiceAccount(args[0], "ServiceAccountID,Slug")
		if err != nil {
			return err
		}
		serviceAccountID := serviceAccount.ServiceAccount.ServiceAccountID.String()
		key, err := resolveServiceAccountKey(serviceAccountID, args[1])
		if err != nil {
			return err
		}
		keyRes, err := cubClientNew.GetServiceAccountUserKeyWithResponse(ctx, serviceAccountID, key.UserKeyID.String(), &goclientnew.GetServiceAccountUserKeyParams{})
		if cubapi.IsAPIError(err, keyRes) {
			return cubapi.InterpretErrorGeneric(err, keyRes)
		}
		displayGetResults(keyRes.JSON200, func(extended *goclientnew.ExtendedUserKey) {
			displayKeyList([]*goclientnew.UserKey{extended.UserKey})
		})
		return nil
	},
}

// resolveServiceAccountKey finds the service account's key with the thumbprint, which is how a
// key is named on the command line; the API addresses it by its ID.
func resolveServiceAccountKey(serviceAccountID, kid string) (*goclientnew.UserKey, error) {
	keys, err := cubapi.ListServiceAccountUserKeys(ctx, cubClient, serviceAccountID,
		cubapi.Where{}.Eq("Kid", kid), cubapi.ListOpts{Select: "UserKeyID,Kid"})
	if err != nil {
		return nil, err
	}
	if len(keys) == 0 || keys[0].UserKey == nil {
		return nil, fmt.Errorf("the service account has no key %s", kid)
	}
	return keys[0].UserKey, nil
}

func init() {
	serviceAccountKeyAddCmd.Flags().StringVar(&userKeyPublicKey, "public-key", "",
		"file holding the public key as a JWK, or - for stdin")
	serviceAccountKeyAddCmd.Flags().StringVar(&userKeyGenerate, "generate", "",
		"generate an Ed25519 keypair, store the private key under this alias (or path), and register the public half")
	serviceAccountKeyAddCmd.Flags().StringVar(&userKeyDescription, "description", "",
		"note recording which host or pipeline holds the private key")
	enableDryRunFlag(serviceAccountKeyAddCmd)
	serviceAccountKeyCmd.AddCommand(serviceAccountKeyAddCmd)

	// The endpoint takes no filter or view.
	enableWhereFlag(serviceAccountKeyListCmd)
	enableContainsFlag(serviceAccountKeyListCmd)
	enableSelectFlag(serviceAccountKeyListCmd)
	addStandardListDisplayFlags(serviceAccountKeyListCmd)
	enableListPagingFlags(serviceAccountKeyListCmd)
	serviceAccountKeyCmd.AddCommand(serviceAccountKeyListCmd)

	addStandardDisplayFlags(serviceAccountKeyGetCmd)
	serviceAccountKeyCmd.AddCommand(serviceAccountKeyGetCmd)

	serviceAccountKeyCmd.AddCommand(serviceAccountKeyDeleteCmd)
}

// serviceAccountUser is the User a service account acts as. A key generated for it names the
// User's external ID, which is what an assertion signed with the key carries as its issuer.
func serviceAccountUser(serviceAccount *goclientnew.ExtendedServiceAccount) (*goclientnew.User, error) {
	if serviceAccount.ServiceAccount == nil {
		return nil, fmt.Errorf("the response has no service account")
	}
	return resolveUserCore(serviceAccount.ServiceAccount.UserID.String())
}

func apiCreateServiceAccountKey(serviceAccountID string, publicJWK json.RawMessage, description string) (*goclientnew.UserKey, error) {
	var jwk any
	if err := json.Unmarshal(publicJWK, &jwk); err != nil {
		return nil, fmt.Errorf("public key must be a JWK (JSON): %w", err)
	}
	body := goclientnew.CreateServiceAccountUserKeyJSONRequestBody{
		PublicJWK:   jwk,
		Description: description,
	}
	keyRes, err := cubClientNew.CreateServiceAccountUserKeyWithResponse(ctx, serviceAccountID, &goclientnew.CreateServiceAccountUserKeyParams{DryRun: dryRunParam()}, body)
	if cubapi.IsAPIError(err, keyRes) {
		return nil, cubapi.InterpretErrorGeneric(err, keyRes)
	}
	return keyRes.JSON200, nil
}

// credentialListOpts are a key or token list's --select, --contains, --limit and --order-by.
// Credentials are displayed oldest first, so a --limit keeps the oldest unless --order-by says
// otherwise.
func credentialListOpts() cubapi.ListOpts {
	opts := listPageOpts("")
	opts.Select = selectFields
	opts.Contains = contains
	return opts
}

// serviceAccountKeyAliases name a listed key's columns as a User's are named.
var serviceAccountKeyAliases = map[string]string{
	"Name": "UserKey.DisplayName",
	"ID":   "UserKey.UserKeyID",
}

func getKidForServiceAccountKey(key *goclientnew.ExtendedUserKey) string {
	return key.UserKey.Kid
}

func displayServiceAccountKeyList(keys []*goclientnew.ExtendedUserKey) {
	if displayRequestedColumns(keys, serviceAccountKeyAliases, nil) {
		return
	}
	credentials := make([]*goclientnew.UserKey, 0, len(keys))
	for _, key := range keys {
		credentials = append(credentials, key.UserKey)
	}
	displayKeyList(credentials)
}
