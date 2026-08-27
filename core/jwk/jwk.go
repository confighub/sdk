// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

// Package jwk constructs and names the keys ConfigHub authenticates with.
//
// A key's name is its RFC 7638 thumbprint, and that name is how the server
// resolves a credential: an assertion identifies its key in the JWT header and
// nothing else is consulted. Client and server must therefore derive the same
// name from the same key, or authentication fails with the same uniform 401 as a
// forged credential.
//
// Whether a key is acceptable -- algorithms, expiry, who may register it -- is
// the server's decision and is not here.
package jwk

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
)

// UserExternalIDMember records, inside a private key file, which identity holds
// the key, so a holder can sign without being told separately who it is.
//
// A convenience, not a control: the holder can edit it and a caller can override
// it at signing time. What binds a key to an identity is the server, where the
// key is registered against exactly one.
//
// Private JWKs only. A registered public JWK is attached to an identity by the
// row holding it.
const UserExternalIDMember = "confighub_user_external_id"

// Pair is a generated keypair, with the name the server will know it by.
// PublicJWK is meant to be visible; PrivateJWK must never leave the holder.
type Pair struct {
	PublicJWK  json.RawMessage
	PrivateJWK json.RawMessage
	Kid        string
}

// GenerateEd25519 mints a keypair and renders both halves as JWKs. Member names
// and sizes are fixed by RFC 8037 §2 and RFC 8032.
//
// externalID may be empty, producing a key that records no identity; a caller
// doing that supplies one at signing time instead.
func GenerateEd25519(externalID string) (*Pair, error) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("generating key: %w", err)
	}

	x := base64.RawURLEncoding.EncodeToString(pub)

	// RFC 8037 §2 defines the private member "d" as the 32-byte seed.
	// ed25519.PrivateKey stores seed || public, so writing the whole 64 bytes
	// produces a JWK that other implementations reject.
	d := base64.RawURLEncoding.EncodeToString(priv.Seed())

	publicJWK := json.RawMessage(fmt.Sprintf(`{"kty":"OKP","crv":"Ed25519","x":%q}`, x))

	var privateJWK json.RawMessage
	if externalID == "" {
		privateJWK = json.RawMessage(fmt.Sprintf(`{"kty":"OKP","crv":"Ed25519","x":%q,"d":%q}`, x, d))
	} else {
		privateJWK = json.RawMessage(fmt.Sprintf(`{"kty":"OKP","crv":"Ed25519","x":%q,"d":%q,%q:%q}`,
			x, d, UserExternalIDMember, externalID))
	}

	kid, err := Thumbprint(publicJWK)
	if err != nil {
		return nil, err
	}

	return &Pair{PublicJWK: publicJWK, PrivateJWK: privateJWK, Kid: kid}, nil
}

// requiredMembers lists, per key type, the members RFC 7638 §3.2 includes in a
// thumbprint. Nothing else participates -- not "alg", "use", or "kid" -- so the
// thumbprint is a property of the key rather than of how it was described.
var requiredMembers = map[string][]string{
	"RSA": {"e", "kty", "n"},
	"EC":  {"crv", "kty", "x", "y"},
	"OKP": {"crv", "kty", "x"},
}

// Thumbprint computes the RFC 7638 thumbprint of a JWK.
//
// Either half of a keypair gives the same answer, since private members are not
// required members, so a client can name its own key without asking the server.
//
// RFC 7638 §3 requires the required members only, lexicographically ordered,
// without whitespace; marshalling a map[string]string produces exactly that.
func Thumbprint(key json.RawMessage) (string, error) {
	var parsed map[string]any
	if err := json.Unmarshal(key, &parsed); err != nil {
		return "", fmt.Errorf("malformed JWK: %w", err)
	}

	kty, _ := parsed["kty"].(string)
	required, ok := requiredMembers[kty]
	if !ok {
		return "", fmt.Errorf("unsupported key type %q: expected OKP, EC, or RSA", kty)
	}

	canonical := make(map[string]string, len(required))
	for _, member := range required {
		v, ok := parsed[member].(string)
		if !ok || v == "" {
			return "", fmt.Errorf("JWK is missing the required member %q for a %s key", member, kty)
		}
		canonical[member] = v
	}

	encoded, err := json.Marshal(canonical)
	if err != nil {
		return "", fmt.Errorf("canonicalizing JWK: %w", err)
	}

	sum := sha256.Sum256(encoded)
	return base64.RawURLEncoding.EncodeToString(sum[:]), nil
}
