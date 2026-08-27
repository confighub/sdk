// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/confighub/sdk/core/cubapi"
	goclientnew "github.com/confighub/sdk/core/openapi/goclient-new"
	"github.com/google/uuid"
	"github.com/spf13/cobra"
)

var userKeyAddCmd = &cobra.Command{
	Use:   "add",
	Short: "Register a public key for an identity",
	Long: getCommandHelp(`Register a public key against an identity.

Either generate a keypair here with --generate, which stores the private key
locally and registers the public half, or register a public key you already have
with --public-key.

--generate takes an alias or a path. A bare name is an alias stored under
~/.confighub/keys, which is what "cub auth login --private-key=<alias>" looks
up; anything containing a path separator is written there instead and has no
alias.

The private key never reaches ConfigHub in either case. --generate creates it
locally and only the public half is sent.

Examples:
`+"```"+`
  # Generate a keypair, storing it under ~/.confighub/keys as the alias "my-worker"
  cub user key add --worker my-worker --generate my-worker

  # ...then authenticate with it
  cub auth login --private-key=my-worker

  # Generate to an explicit path instead of an alias
  cub user key add --worker my-worker --generate ./my-worker.jwk

  # Register a public key you already have
  cub user key add --worker my-worker --public-key ./worker.pub.jwk

  # Register a public key from stdin, with a note about who holds it
  cat worker.pub.jwk | cub user key add --worker my-worker --public-key - \
      --description "CI runner, us-east"
`+"```"+`
`, ""),
	Args: cobra.NoArgs,
	RunE: userKeyAddCmdRun,
}

var (
	userKeyPublicKey   string
	userKeyGenerate    string
	userKeyDescription string
)

func init() {
	userKeyAddCmd.Flags().StringVar(&userKeyPublicKey, "public-key", "",
		"file holding the public key as a JWK, or - for stdin")
	userKeyAddCmd.Flags().StringVar(&userKeyGenerate, "generate", "",
		"generate an Ed25519 keypair, store the private key under this alias (or path), and register the public half")
	userKeyAddCmd.Flags().StringVar(&userKeyDescription, "description", "",
		"note recording which host or pipeline holds the private key")
	userKeyCmd.AddCommand(userKeyAddCmd)
}

func userKeyAddCmdRun(cmd *cobra.Command, args []string) error {
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

	targetUser, err := resolveKeyTargetUser()
	if err != nil {
		return err
	}

	if userKeyGenerate != "" {
		// Generated after resolution because the identity is written into the
		// private key, which is what lets a worker be configured with the key
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
		if err := writePrivateKey(generatedPath, generated.PrivateJWK); err != nil {
			return err
		}
		publicJWK = generated.PublicJWK
	}

	// Computed locally so a failure to register can still report which key was
	// meant, and so the value shown matches what a client computes for itself.
	kid, err := jwkThumbprint(publicJWK)
	if err != nil {
		return err
	}

	key, err := apiCreateUserKey(targetUser.UserID, publicJWK, userKeyDescription)
	if err != nil {
		// Registration failed, so the key we just wrote authenticates nothing.
		// Leaving it behind would poison the alias: writePrivateKey refuses to
		// overwrite, so retrying the same name fails on a file the caller never
		// knowingly created. Removing it is safe precisely because nothing was
		// registered -- and it can only be a file we made ourselves, since
		// writePrivateKey creates exclusively.
		if generatedPath != "" {
			if rmErr := os.Remove(generatedPath); rmErr != nil && !os.IsNotExist(rmErr) {
				return fmt.Errorf("%w (and %s could not be removed: %v)", err, generatedPath, rmErr)
			}
		}
		return err
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
func writePrivateKey(path string, privateJWK json.RawMessage) error {
	if dir := filepath.Dir(path); dir != "" {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return fmt.Errorf("creating %s: %w", dir, err)
		}
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		if os.IsExist(err) {
			return fmt.Errorf("%s already exists; remove it or choose another path", path)
		}
		return fmt.Errorf("writing private key: %w", err)
	}
	defer file.Close()
	if _, err := file.Write(append(privateJWK, '\n')); err != nil {
		return fmt.Errorf("writing private key: %w", err)
	}
	return nil
}

func apiCreateUserKey(userID uuid.UUID, publicJWK json.RawMessage, description string) (*goclientnew.UserKey, error) {
	var jwk any
	if err := json.Unmarshal(publicJWK, &jwk); err != nil {
		return nil, fmt.Errorf("public key must be a JWK (JSON): %w", err)
	}
	body := goclientnew.CreateUserKeyJSONRequestBody{
		PublicJWK:   jwk,
		Description: description,
	}
	credentialRes, err := cubClientNew.CreateUserKeyWithResponse(ctx, userID.String(), body)
	if cubapi.IsAPIError(err, credentialRes) {
		return nil, cubapi.InterpretErrorGeneric(err, credentialRes)
	}
	return credentialRes.JSON200, nil
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
