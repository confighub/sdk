// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package cubapi

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// This file lets a client authenticate by signing rather than by presenting a
// stored secret. The private key stays with the client; ConfigHub holds only the
// public half and never receives anything it could replay.
//
// It is written against the RFCs with the standard library alone. An Ed25519
// JWS is a header, a payload, and a signature over "header.payload" -- there is
// no algorithm negotiation to get wrong and no key format to guess -- and a
// JOSE dependency in an SDK module is a cost paid by everyone who imports it.

const (
	// DefaultAssertionAudience must match the server's default. The audience
	// pins an assertion to one token endpoint, so a mismatch is a uniform 401
	// with nothing to distinguish it from a bad key. Deployments that set
	// CONFIGHUB_ASSERTION_AUDIENCE on the server must set it here too.
	DefaultAssertionAudience = "confighub-auth"

	// assertionLifetime is how long a signed assertion is valid. The server
	// bounds this at five minutes; two is comfortably inside that and long
	// enough to survive a slow round trip. Nothing is gained by asking for
	// more: a client holding the key can sign another whenever it likes.
	assertionLifetime = 2 * time.Minute

	// ClientAssertionTypeJWTBearer is fixed by RFC 7523 §2.2. The server
	// requires exactly this value.
	ClientAssertionTypeJWTBearer = "urn:ietf:params:oauth:client-assertion-type:jwt-bearer"

	// jwkUserIDMember carries the ConfigHub identity inside the key file.
	//
	// An assertion's iss and sub must name the identity that owns the key, so a
	// signer needs both the key and the identity. Carrying the identity in the
	// key file means one artifact moves to the client host instead of two, and
	// a key file cannot be separated from the identity it authenticates as.
	// Unknown JWK members are ignored by everything else, including the RFC
	// 7638 thumbprint, which uses only the required members.
	jwkUserIDMember = "confighub_user_id"
)

// AssertionSigner signs private_key_jwt assertions for one identity.
type AssertionSigner struct {
	userID   string
	kid      string
	audience string
	key      ed25519.PrivateKey
}

// NewAssertionSigner builds a signer from a private JWK.
//
// userID may be empty when the JWK carries confighub_user_id, which is what
// `cub user key add --generate` writes. An explicit userID wins, so a key
// generated elsewhere can still be used by naming the identity separately.
//
// audience may be empty for DefaultAssertionAudience.
func NewAssertionSigner(privateJWK []byte, userID, audience string) (*AssertionSigner, error) {
	var jwk struct {
		Kty    string `json:"kty"`
		Crv    string `json:"crv"`
		X      string `json:"x"`
		D      string `json:"d"`
		UserID string `json:"confighub_user_id"`
	}
	if err := json.Unmarshal(privateJWK, &jwk); err != nil {
		return nil, fmt.Errorf("private key must be a JWK (JSON): %w", err)
	}
	if jwk.Kty != "OKP" || jwk.Crv != "Ed25519" {
		// Only Ed25519 is supported here even though the server also accepts
		// ES256 and RS256, because this is the key the CLI generates. Saying so
		// is better than failing later with a signature the server rejects.
		return nil, fmt.Errorf("private key must be an Ed25519 JWK (kty OKP, crv Ed25519), got kty %q crv %q", jwk.Kty, jwk.Crv)
	}
	if jwk.D == "" {
		return nil, errors.New("private key JWK has no private member \"d\"; this looks like a public key")
	}

	seed, err := base64.RawURLEncoding.DecodeString(jwk.D)
	if err != nil {
		return nil, fmt.Errorf("private member \"d\" is not base64url: %w", err)
	}
	if len(seed) != ed25519.SeedSize {
		// RFC 8037 §2 defines d as the seed. A 64-byte value is the expanded
		// key some libraries expose, and silently accepting it would produce
		// signatures nothing else can verify.
		return nil, fmt.Errorf("private member \"d\" is %d bytes, want the %d-byte Ed25519 seed", len(seed), ed25519.SeedSize)
	}

	if userID == "" {
		userID = jwk.UserID
	}
	if userID == "" {
		return nil, fmt.Errorf("no ConfigHub identity for this key: the JWK has no %q member and none was configured", jwkUserIDMember)
	}
	if audience == "" {
		audience = DefaultAssertionAudience
	}

	kid, err := ed25519JWKThumbprint(jwk.X)
	if err != nil {
		return nil, err
	}

	return &AssertionSigner{
		userID:   userID,
		kid:      kid,
		audience: audience,
		key:      ed25519.NewKeyFromSeed(seed),
	}, nil
}

// UserID is the identity this signer authenticates as.
func (s *AssertionSigner) UserID() string { return s.userID }

// Kid is the thumbprint the server will look the key up by.
func (s *AssertionSigner) Kid() string { return s.kid }

// Sign produces a assertion valid from now.
//
// iss and sub both carry the identity: RFC 7523 §3 requires both, and for
// client authentication the client is its own subject. The server checks that
// they match the identity that owns the key, so an assertion signed with a real
// key cannot claim to be about somebody else.
func (s *AssertionSigner) Sign() (string, error) {
	jti := make([]byte, 16)
	if _, err := rand.Read(jti); err != nil {
		return "", fmt.Errorf("generating assertion id: %w", err)
	}

	now := time.Now()
	header := map[string]string{
		"alg": "EdDSA",
		"typ": "JWT",
		// The assertion names its own key, which is what lets the server
		// resolve an identity from the assertion alone, with no identity sent
		// alongside it.
		"kid": s.kid,
	}
	claims := map[string]any{
		"iss": s.userID,
		"sub": s.userID,
		"aud": s.audience,
		"jti": base64.RawURLEncoding.EncodeToString(jti),
		"iat": now.Unix(),
		"exp": now.Add(assertionLifetime).Unix(),
	}

	headerJSON, err := json.Marshal(header)
	if err != nil {
		return "", err
	}
	claimsJSON, err := json.Marshal(claims)
	if err != nil {
		return "", err
	}

	signingInput := base64.RawURLEncoding.EncodeToString(headerJSON) +
		"." + base64.RawURLEncoding.EncodeToString(claimsJSON)
	signature := ed25519.Sign(s.key, []byte(signingInput))
	return signingInput + "." + base64.RawURLEncoding.EncodeToString(signature), nil
}

// ed25519JWKThumbprint computes the RFC 7638 thumbprint of an Ed25519 public
// key, which is the kid the server stored when the key was registered.
//
// Deriving it rather than carrying it means the key file cannot disagree with
// itself: a kid that does not match the key would fail server-side lookup with
// the same uniform 401 as every other failure, which is a bad thing to debug.
//
// RFC 7638 §3 fixes the input exactly: required members only, lexicographically
// ordered, no whitespace. For OKP that is crv, kty, x -- already in order.
func ed25519JWKThumbprint(x string) (string, error) {
	if x == "" {
		return "", errors.New("JWK has no public member \"x\"")
	}
	canonical := fmt.Sprintf(`{"crv":"Ed25519","kty":"OKP","x":%q}`, x)
	sum := sha256.Sum256([]byte(canonical))
	return base64.RawURLEncoding.EncodeToString(sum[:]), nil
}
