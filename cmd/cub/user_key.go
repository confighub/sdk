// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"encoding/json"
	"fmt"
	"time"

	goclientnew "github.com/confighub/sdk/core/openapi/goclient-new"
	"github.com/google/uuid"
	"github.com/spf13/cobra"

	"github.com/confighub/sdk/core/jwk"
)

// The noun is "key" rather than "credential" because a key is what these
// commands manipulate, and the CLI names things for what the operator is
// holding. It follows `gcloud iam service-accounts keys` and `gh ssh-key`.
// The API path stays /user/{id}/credential and the table stays
// user_keys, where the general name leaves room for a credential that
// is not a key; see docs/design/service-account-auth.md §7.1.
var userKeyCmd = &cobra.Command{
	Use:   "key",
	Short: "Manage the public keys registered against an identity",
	Long: getCommandHelp(`Manage the public keys an identity can authenticate with.

The holder of the matching private key authenticates by signing a short-lived
assertion, so ConfigHub never receives or stores a secret. This replaces the
worker secret, which ConfigHub generates, stores, returns through the API, and
receives again on every token refresh.

Identities are named one of two ways. Use --worker to name a worker and have it
resolved to the bot user it runs as, which is what an operator usually wants;
use --user to name an identity directly.

Registering a key hands out an identity: whoever holds the private key
authenticates as that identity from then on. It requires the admin or manager
role, and you cannot register a key for yourself.`, ""),
	PersistentPreRunE: spacePreRunE,
}

var (
	userKeyUser   string
	userKeyWorker string
)

func init() {
	// The space flag is here for --worker: workers are space-scoped, so
	// resolving one needs a space. It is unused when --user is given.
	addSpaceFlags(userKeyCmd)
	userKeyCmd.PersistentFlags().StringVar(&userKeyUser, "user", "",
		"identity to manage keys for, by username or UUID")
	userKeyCmd.PersistentFlags().StringVar(&userKeyWorker, "worker", "",
		"worker whose bot user to manage keys for, by slug or UUID")
	userCmd.AddCommand(userKeyCmd)
}

// resolveKeyTargetUser turns --user or --worker into the identity whose keys
// are being managed.
//
// A worker resolves to BridgeWorker.UserID, its bot user. That indirection is
// the whole reason --worker exists: the API is identity-shaped because a
// credential proves who you are, while an operator thinks in workers, and
// neither side should have to learn the other's model.
// resolveKeyTargetUser finds the identity a key is being registered for, and
// returns the whole record: the uuid addresses it in the API, and the external
// id is what an assertion signed with the key will name as its issuer.
func resolveKeyTargetUser() (*goclientnew.User, error) {
	switch {
	case userKeyUser != "" && userKeyWorker != "":
		return nil, fmt.Errorf("--user and --worker name the same thing two ways; use one")
	case userKeyWorker != "":
		worker, err := apiGetBridgeWorkerFromSlug(userKeyWorker, "*")
		if err != nil {
			return nil, err
		}
		if worker.UserID == nil || *worker.UserID == uuid.Nil {
			// Every worker gets a bot user at creation, so this means the
			// worker predates that or was created outside the normal path.
			// Worth saying plainly rather than reporting a nil UUID lookup.
			return nil, fmt.Errorf("worker %s has no bot user, so it has no identity to hold a key", userKeyWorker)
		}
		return apiGetUser(worker.UserID.String())
	case userKeyUser != "":
		return apiGetUserFromUsername(userKeyUser)
	default:
		return nil, fmt.Errorf("name an identity with --user or a worker with --worker")
	}
}

// generatedKey is a new keypair: the private half to be written somewhere the
// operator controls, the public half to be registered, and the name the server
// will know it by.
//
// Construction and naming both come from sdk/core/jwk, so the name computed here
// is the one the server stores the key under.
type generatedKey = jwk.Pair

func generateEd25519Key(userID string) (*generatedKey, error) {
	return jwk.GenerateEd25519(userID)
}

func jwkThumbprint(publicJWK json.RawMessage) (string, error) {
	return jwk.Thumbprint(publicJWK)
}

// displayKeyList renders keys as a table. PublicJWK is not a column: it is
// several hundred characters and the kid identifies the key. `-o json` returns
// it in full, which is the point of not redacting it server-side.
func displayKeyList(credentials []*goclientnew.UserKey) {
	table := tableView()
	if !noheader {
		table.SetHeader([]string{"Kid", "Description", "Created", "Last-Used"})
	}
	for _, credential := range credentials {
		table.Append([]string{
			credential.Kid,
			credential.Description,
			credential.CreatedAt.Format("2006-01-02"),
			formatLastUsed(credential.LastUsedAt),
		})
	}
	table.Render()
}

// formatLastUsed renders the zero time as "never" rather than as year 1. A key
// nothing has used is a key nothing depends on, which is the fact this column
// exists to report, so it must not read as a date.
func formatLastUsed(lastUsed time.Time) string {
	if lastUsed.IsZero() {
		return "never"
	}
	return lastUsed.Format("2006-01-02 15:04")
}
