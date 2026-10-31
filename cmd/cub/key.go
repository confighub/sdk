// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

// Keys registered against an identity, which `cub serviceaccount key` manages.

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	goclientnew "github.com/confighub/sdk/core/openapi/goclient-new"

	"github.com/confighub/sdk/core/jwk"
)

var (
	userKeyPublicKey   string
	userKeyGenerate    string
	userKeyDescription string
)

// addKey registers the key --public-key or --generate names for the identity resolve finds,
// with register, which calls whichever API the identity's keys are managed through.
func addKey(resolve func() (*goclientnew.User, error),
	register func(target *goclientnew.User, publicJWK json.RawMessage, description string) (*goclientnew.UserKey, error)) error {
	if (userKeyPublicKey == "") == (userKeyGenerate == "") {
		return fmt.Errorf("give either --public-key to register a key you have, or --generate to make one")
	}

	// A supplied key is read and checked before anything is looked up, so that
	// handing us a private key by mistake is refused locally rather than after
	// a round trip. Nothing sends the key during resolution either way, but a
	// local mistake should not need the network to be diagnosed.
	var publicJWK json.RawMessage
	var generatedPath string
	if userKeyPublicKey != "" {
		supplied, err := readPublicKey(userKeyPublicKey)
		if err != nil {
			return err
		}
		publicJWK = supplied
	}

	targetUser, err := resolve()
	if err != nil {
		return err
	}

	if userKeyGenerate != "" {
		// Generated after resolution because the identity is written into the
		// private key, which is what lets a bot be configured with the key
		// alone.
		// The external id, not the uuid: it is what the assertion will carry as
		// iss and sub, and what a minted session names as its subject.
		generated, gerr := generateEd25519Key(targetUser.ExternalID)
		if gerr != nil {
			return gerr
		}
		// A bare name is an alias under ~/.confighub/keys, which is what
		// `cub auth login --private-key=<alias>` looks up; anything with a
		// separator stays a path. Resolved here rather than in writePrivateKey
		// so that what gets reported is the file that was actually written.
		generatedPath = contextManager.KeyPath(userKeyGenerate)
		// The private key is written before registration, not after. If the
		// write fails there is nothing registered to clean up; the other order
		// would leave a key trusted by the server whose private half was lost.
		// A dry run registers nothing, so it keeps nothing either.
		if !dryRun {
			if err := writePrivateKey(generatedPath, generated.PrivateJWK); err != nil {
				return err
			}
		}
		publicJWK = generated.PublicJWK
	}

	// Computed locally so a failure to register can still report which key was
	// meant, and so the value shown matches what a client computes for itself.
	kid, err := jwkThumbprint(publicJWK)
	if err != nil {
		return err
	}

	key, err := register(targetUser, publicJWK, userKeyDescription)
	if err != nil {
		// Registration failed, so the key we just wrote authenticates nothing.
		// Leaving it behind would poison the alias: writePrivateKey refuses to
		// overwrite, so retrying the same name fails on a file the caller never
		// knowingly created. Removing it is safe precisely because nothing was
		// registered -- and it can only be a file we made ourselves, since
		// writePrivateKey creates exclusively.
		if generatedPath != "" && !dryRun {
			if rmErr := os.Remove(generatedPath); rmErr != nil && !os.IsNotExist(rmErr) {
				return fmt.Errorf("%w (and %s could not be removed: %v)", err, generatedPath, rmErr)
			}
		}
		return err
	}

	if dryRun {
		if userKeyGenerate != "" {
			fmt.Printf("Dry run: would write the private key to %s\n", generatedPath)
		}
		fmt.Printf("Dry run: would register key %s\n", kid)
		displayGetResults(key, displayKeyDetails)
		return nil
	}
	if userKeyGenerate != "" {
		fmt.Printf("Private key written to %s\n", generatedPath)
		// Printing the login line is not decoration: the alias is the only part
		// of this a caller has to know afterwards, and the path it expanded to
		// is not what they pass back.
		if alias, ok := keyAlias(generatedPath); ok {
			fmt.Printf("Authenticate with it using: cub auth login --private-key=%s\n", alias)
		}
	}
	fmt.Printf("Registered key %s\n", kid)
	displayGetResults(key, displayKeyDetails)
	return nil
}

// keyAlias reports the alias a generated key can be named by, which exists only
// when the key was written into the key directory. A key written elsewhere is
// addressed by path and has no alias.
func keyAlias(path string) (string, bool) {
	dir := contextManager.KeyDir()
	if dir == "" || filepath.Dir(path) != dir {
		return "", false
	}
	base := filepath.Base(path)
	return strings.TrimSuffix(base, filepath.Ext(base)), true
}

// readPublicKey reads a JWK from a file or stdin, and refuses one that carries
// private members.
func readPublicKey(path string) (json.RawMessage, error) {
	var (
		data []byte
		err  error
	)
	if path == "-" {
		data, err = io.ReadAll(os.Stdin)
	} else {
		data, err = os.ReadFile(path)
	}
	if err != nil {
		return nil, fmt.Errorf("reading public key: %w", err)
	}

	var jwk map[string]any
	if err := json.Unmarshal(data, &jwk); err != nil {
		return nil, fmt.Errorf("public key must be a JWK (JSON): %w", err)
	}

	// The server rejects these too, and rejecting here as well is not
	// redundant: this one happens before the key leaves the machine. Sending a
	// private key and being told no still sent it.
	for _, private := range []string{"d", "p", "q", "dp", "dq", "qi", "k"} {
		if _, found := jwk[private]; found {
			return nil, fmt.Errorf(
				"%s holds a private key (it has a %q member); register the public half only", path, private)
		}
	}
	return json.RawMessage(data), nil
}

// writePrivateKey writes a generated private key readable only by its owner,
// and refuses to overwrite an existing file. Overwriting would destroy the only
// copy of a key that something may currently be authenticating with.
//
// The Store owns this so that anything else writing a key into the same
// directory produces an identical file.
func writePrivateKey(path string, privateJWK json.RawMessage) error {
	_, err := contextManager.Store().WritePrivateKey(path, privateJWK)
	return err
}

func displayKeyDetails(key *goclientnew.UserKey) {
	view := tableView()
	view.Append([]string{"Kid", key.Kid})
	view.Append([]string{"User ID", key.UserID.String()})
	view.Append([]string{"Description", key.Description})
	view.Append([]string{"Created At", key.CreatedAt.String()})
	view.Append([]string{"Last Used At", formatLastUsed(key.LastUsedAt)})
	view.Render()
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
	if displayRequestedColumns(credentials, nil, nil) {
		return
	}
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
