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
		keysRes, err := cubClientNew.ListServiceAccountKeysWithResponse(ctx, serviceAccount.ServiceAccount.ServiceAccountID.String())
		if cubapi.IsAPIError(err, keysRes) {
			return cubapi.InterpretErrorGeneric(err, keysRes)
		}
		keys := make([]*goclientnew.UserKey, 0, len(*keysRes.JSON200))
		for i := range *keysRes.JSON200 {
			keys = append(keys, &(*keysRes.JSON200)[i])
		}
		displayListResults(keys, getKidForCredential, displayKeyList)
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
		kid := args[1]
		deleteRes, err := cubClientNew.DeleteServiceAccountKeyWithResponse(ctx, serviceAccount.ServiceAccount.ServiceAccountID.String(), kid)
		if cubapi.IsAPIError(err, deleteRes) {
			return cubapi.InterpretErrorGeneric(err, deleteRes)
		}
		displayDeleteResults("key", kid, kid, deleteRes.JSON200)
		return nil
	},
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

	addStandardListFlags(serviceAccountKeyListCmd)
	serviceAccountKeyCmd.AddCommand(serviceAccountKeyListCmd)

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
	body := goclientnew.CreateServiceAccountKeyJSONRequestBody{
		PublicJWK:   jwk,
		Description: description,
	}
	keyRes, err := cubClientNew.CreateServiceAccountKeyWithResponse(ctx, serviceAccountID, &goclientnew.CreateServiceAccountKeyParams{DryRun: dryRunParam()}, body)
	if cubapi.IsAPIError(err, keyRes) {
		return nil, cubapi.InterpretErrorGeneric(err, keyRes)
	}
	return keyRes.JSON200, nil
}
