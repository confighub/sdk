// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"time"

	goclientnew "github.com/confighub/sdk/core/openapi/goclient-new"
	"github.com/google/uuid"
	"github.com/spf13/cobra"
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
func resolveKeyTargetUser() (uuid.UUID, error) {
	switch {
	case userKeyUser != "" && userKeyWorker != "":
		return uuid.Nil, fmt.Errorf("--user and --worker name the same thing two ways; use one")
	case userKeyWorker != "":
		worker, err := apiGetBridgeWorkerFromSlug(userKeyWorker, "*")
		if err != nil {
			return uuid.Nil, err
		}
		if worker.UserID == nil || *worker.UserID == uuid.Nil {
			// Every worker gets a bot user at creation, so this means the
			// worker predates that or was created outside the normal path.
			// Worth saying plainly rather than reporting a nil UUID lookup.
			return uuid.Nil, fmt.Errorf("worker %s has no bot user, so it has no identity to hold a key", userKeyWorker)
		}
		return *worker.UserID, nil
	case userKeyUser != "":
		user, err := apiGetUserFromUsername(userKeyUser)
		if err != nil {
			return uuid.Nil, err
		}
		return user.UserID, nil
	default:
		return uuid.Nil, fmt.Errorf("name an identity with --user or a worker with --worker")
	}
}

// generatedKey is a new keypair: the private half to be written somewhere the
// client can read it, the public half to be registered.
type generatedKey struct {
	PrivateJWK json.RawMessage
	PublicJWK  json.RawMessage
	Kid        string
}

// jwkUserIDMember carries the ConfigHub identity inside the private key file.
//
// An assertion's iss and sub must name the identity that owns the key, so
// whatever signs needs both the key and the identity. Writing the identity into
// the key file means one artifact moves to the worker host rather than two, and
// a key cannot be separated from the identity it authenticates as. Members
// beyond the required ones are ignored by the RFC 7638 thumbprint, so this
// changes no key's name; the worker reads it back in
// public/core/worker/lib/assertion.go.
const jwkUserIDMember = "confighub_user_id"

// generateEd25519Key makes a keypair and renders both halves as JWKs, with the
// identity recorded in the private half.
//
// Ed25519 because it is on the server's algorithm allowlist (EdDSA, ES256,
// RS256), has no parameters to get wrong, and produces keys short enough to
// paste. Encoding the JWK by hand keeps the CLI free of a JOSE dependency for
// what is, at this size, a well-specified handful of base64: RFC 8037 §2 fixes
// the member names for OKP keys and RFC 8032 fixes the key sizes.
func generateEd25519Key(userID string) (*generatedKey, error) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("generating key: %w", err)
	}

	x := base64.RawURLEncoding.EncodeToString(pub)
	// The private member "d" is the 32-byte seed, not the 64-byte expanded
	// key: RFC 8037 §2 defines d as the seed, and ed25519.PrivateKey stores
	// seed || public. Writing all 64 bytes produces a JWK that other
	// implementations reject.
	d := base64.RawURLEncoding.EncodeToString(priv.Seed())

	publicJWK := json.RawMessage(fmt.Sprintf(
		`{"kty":"OKP","crv":"Ed25519","x":%q}`, x))
	privateJWK := json.RawMessage(fmt.Sprintf(
		`{"kty":"OKP","crv":"Ed25519","x":%q,"d":%q,%q:%q}`, x, d, jwkUserIDMember, userID))

	kid, err := jwkThumbprint(publicJWK)
	if err != nil {
		return nil, err
	}
	return &generatedKey{PrivateJWK: privateJWK, PublicJWK: publicJWK, Kid: kid}, nil
}

// jwkThumbprint computes the RFC 7638 thumbprint of a public JWK, which is the
// kid the server will store. Computing it locally is what lets the CLI report
// the key's name before the server has answered, and lets a client name its own
// key without asking.
//
// RFC 7638 §3 is exact about the input: only the required members for the key
// type, lexicographically ordered, no whitespace. Marshalling a Go map produces
// exactly that ordering, so the construction is the specification rather than a
// re-implementation of it.
func jwkThumbprint(publicJWK json.RawMessage) (string, error) {
	var jwk map[string]any
	if err := json.Unmarshal(publicJWK, &jwk); err != nil {
		return "", fmt.Errorf("parsing JWK: %w", err)
	}

	kty, _ := jwk["kty"].(string)
	var required map[string]any
	switch kty {
	case "OKP":
		required = map[string]any{"crv": jwk["crv"], "kty": jwk["kty"], "x": jwk["x"]}
	case "EC":
		required = map[string]any{"crv": jwk["crv"], "kty": jwk["kty"], "x": jwk["x"], "y": jwk["y"]}
	case "RSA":
		required = map[string]any{"e": jwk["e"], "kty": jwk["kty"], "n": jwk["n"]}
	default:
		return "", fmt.Errorf("unsupported key type %q: expected OKP, EC, or RSA", kty)
	}
	for name, value := range required {
		if s, ok := value.(string); !ok || s == "" {
			return "", fmt.Errorf("JWK is missing the required member %q for a %s key", name, kty)
		}
	}

	canonical, err := json.Marshal(required)
	if err != nil {
		return "", fmt.Errorf("canonicalizing JWK: %w", err)
	}
	sum := sha256.Sum256(canonical)
	return base64.RawURLEncoding.EncodeToString(sum[:]), nil
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
