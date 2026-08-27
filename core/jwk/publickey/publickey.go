// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package publickey

import (
	"crypto"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/rsa"
	"encoding/json"
	"fmt"

	"github.com/MicahParks/jwkset"
)

// Reading a public key supplied by a caller, so that a key rejected at
// registration is rejected at authentication too.
//
// Separate from the rest of sdk/core/jwk because it is the only part needing a
// JWK library, and only a server verifies signatures -- keeping it here leaves
// that dependency out of binaries that merely want a thumbprint.

// jwkPrivateMembers are the JWK members that carry secret key material, across
// every key type we might be handed: RSA private factors, the EC/OKP private
// scalar, and the symmetric key itself.
var jwkPrivateMembers = []string{"d", "p", "q", "dp", "dq", "qi", "oth", "k"}

// FromJWK converts a public JWK into a key usable for verification.
//
// Private key material is refused explicitly, because jwkset with Private:false
// does not reject a JWK carrying "d" -- it parses it and returns the public
// half. A client that mistakenly sent its private key would otherwise succeed
// silently and have that key stored verbatim.
func FromJWK(raw json.RawMessage) (crypto.PublicKey, error) {
	var members map[string]any
	if err := json.Unmarshal(raw, &members); err != nil {
		return nil, fmt.Errorf("malformed JWK: %w", err)
	}
	for _, member := range jwkPrivateMembers {
		if _, present := members[member]; present {
			return nil, fmt.Errorf("JWK carries private key material (%q); register the public key only", member)
		}
	}

	jwk, err := jwkset.NewJWKFromRawJSON(raw, jwkset.JWKMarshalOptions{}, jwkset.JWKValidateOptions{})
	if err != nil {
		return nil, fmt.Errorf("parsing JWK: %w", err)
	}
	key := jwk.Key()
	if key == nil {
		return nil, fmt.Errorf("JWK carries no usable key")
	}
	switch key.(type) {
	case ed25519.PublicKey, *ecdsa.PublicKey, *rsa.PublicKey:
		return key, nil
	default:
		// Anything else is either a symmetric key -- which would make the
		// "public" key a verification secret and defeat the point -- or a
		// private key that slipped through.
		return nil, fmt.Errorf("unsupported key type %T", key)
	}
}
