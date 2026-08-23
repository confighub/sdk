// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"github.com/confighub/sdk/core/cubapi"
	goclientnew "github.com/confighub/sdk/core/openapi/goclient-new"
	"github.com/google/uuid"
	"github.com/spf13/cobra"
)

var userKeyListCmd = &cobra.Command{
	Use:   "list",
	Short: "List the public keys registered against an identity",
	Long: getCommandHelp(`List the public keys an identity can authenticate with.

Keys are shown in full with -o json rather than redacted. They are public
material, and a key nobody registered is only noticeable if it is visible.

The Last-Used column is what makes a key safe to retire: a key nothing has used
is a key nothing depends on. It is also how an injected key gives itself away,
by starting to be used.

Examples:
`+"```"+`
  # List the keys a worker can authenticate with
  cub user key list --worker my-worker

  # Full key material, for comparing against what a client holds
  cub user key list --worker my-worker -o json

  # Just the thumbprints, for scripting
  cub user key list --worker my-worker --no-headers -o jq='.[].Kid'
`+"```"+`
`, ""),
	Args: cobra.NoArgs,
	RunE: userKeyListCmdRun,
}

func init() {
	addStandardListFlags(userKeyListCmd)
	userKeyCmd.AddCommand(userKeyListCmd)
}

func userKeyListCmdRun(cmd *cobra.Command, args []string) error {
	targetUserID, err := resolveKeyTargetUser()
	if err != nil {
		return err
	}

	keys, err := apiListUserKeys(targetUserID)
	if err != nil {
		return err
	}
	displayListResults(keys, getKidForCredential, displayKeyList)
	return nil
}

func getKidForCredential(key *goclientnew.UserKey) string {
	return key.Kid
}

func apiListUserKeys(userID uuid.UUID) ([]*goclientnew.UserKey, error) {
	keysRes, err := cubClientNew.ListUserKeysWithResponse(ctx, userID.String())
	if cubapi.IsAPIError(err, keysRes) {
		return nil, cubapi.InterpretErrorGeneric(err, keysRes)
	}

	keys := make([]*goclientnew.UserKey, 0, len(*keysRes.JSON200))
	for i := range *keysRes.JSON200 {
		keys = append(keys, &(*keysRes.JSON200)[i])
	}
	return keys, nil
}
