// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The thumbprint is the name the server stores and the name a client uses to
// address its own key, so an implementation that is merely self-consistent is
// not good enough: it has to agree with everyone else's. These tests check it
// against the RFCs rather than against itself.

// RFC 7638 §3.1 carries a worked example: this RSA key has this thumbprint.
func TestJWKThumbprintMatchesRFC7638(t *testing.T) {
	const rfcKey = `{
      "kty": "RSA",
      "n": "0vx7agoebGcQSuuPiLJXZptN9nndrQmbXEps2aiAFbWhM78LhWx4cbbfAAtVT86zwu1RK7aPFFxuhDR1L6tSoc_BJECPebWKRXjBZCiFV4n3oknjhMstn64tZ_2W-5JsGY4Hc5n9yBXArwl93lqt7_RN5w6Cf0h4QyQ5v-65YGjQR0_FDW2QvzqY368QQMicAtaSqzs8KJZgnYb9c7d0zgdAZHzu6qMQvRL5hajrn1n91CbOpbISD08qNLyrdkt-bFTWhAI4vMQFh6WeZu0fM4lFd2NcRwr3XPksINHaQ-G_xBniIqbw0Ls1jF44-csFCur-kEgU8awapJzKnqDKgw",
      "e": "AQAB",
      "alg": "RS256",
      "kid": "2011-04-29"
    }`
	const want = "NzbLsXh8uDCcd-6MNwXF4W_7noWXFZAfHkxZsRGC9Xs"

	got, err := jwkThumbprint(json.RawMessage(rfcKey))
	if err != nil {
		t.Fatalf("jwkThumbprint: %v", err)
	}
	if got != want {
		t.Errorf("thumbprint = %q, want the RFC 7638 §3.1 value %q", got, want)
	}
}

// RFC 8037 §A.3 gives the thumbprint of the Ed25519 key in §A.1, which is the
// key type the CLI generates.
func TestJWKThumbprintMatchesRFC8037(t *testing.T) {
	const rfcKey = `{"kty":"OKP","crv":"Ed25519","x":"11qYAYKxCrfVS_7TyWQHOg7hcvPapiMlrwIaaPcHURo"}`
	const want = "kPrK_qmxVWaYVA9wwBF6Iuo3vVzz7TxHCTwXBygrS4k"

	got, err := jwkThumbprint(json.RawMessage(rfcKey))
	if err != nil {
		t.Fatalf("jwkThumbprint: %v", err)
	}
	if got != want {
		t.Errorf("thumbprint = %q, want the RFC 8037 §A.3 value %q", got, want)
	}
}

// Members that are not required by RFC 7638 must not affect the result, or two
// descriptions of the same key would produce two names for it.
func TestJWKThumbprintIgnoresOptionalMembers(t *testing.T) {
	const bare = `{"kty":"OKP","crv":"Ed25519","x":"11qYAYKxCrfVS_7TyWQHOg7hcvPapiMlrwIaaPcHURo"}`
	const decorated = `{"use":"sig","kid":"something-else","alg":"EdDSA","kty":"OKP","crv":"Ed25519","x":"11qYAYKxCrfVS_7TyWQHOg7hcvPapiMlrwIaaPcHURo"}`

	bareThumbprint, err := jwkThumbprint(json.RawMessage(bare))
	if err != nil {
		t.Fatalf("jwkThumbprint(bare): %v", err)
	}
	decoratedThumbprint, err := jwkThumbprint(json.RawMessage(decorated))
	if err != nil {
		t.Fatalf("jwkThumbprint(decorated): %v", err)
	}
	if bareThumbprint != decoratedThumbprint {
		t.Errorf("optional members changed the thumbprint: %q vs %q", bareThumbprint, decoratedThumbprint)
	}
}

func TestJWKThumbprintRejectsIncompleteKeys(t *testing.T) {
	for name, jwk := range map[string]string{
		"missing x":          `{"kty":"OKP","crv":"Ed25519"}`,
		"missing crv":        `{"kty":"OKP","x":"11qYAYKxCrfVS_7TyWQHOg7hcvPapiMlrwIaaPcHURo"}`,
		"unknown key type":   `{"kty":"oct","k":"c2VjcmV0"}`,
		"not an object":      `["kty","OKP"]`,
		"empty required set": `{"kty":"EC","crv":"P-256","x":"","y":""}`,
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := jwkThumbprint(json.RawMessage(jwk)); err == nil {
				t.Errorf("jwkThumbprint(%s) succeeded, want an error", jwk)
			}
		})
	}
}

// The generated pair has to actually be a pair, and the private half has to be
// in the form other implementations expect. A JWK whose "d" is the 64-byte
// expanded key rather than the 32-byte seed round-trips through our own code
// and fails everywhere else, which is the kind of bug that surfaces only when
// something other than cub tries to sign.
func TestGenerateEd25519KeyProducesAUsablePair(t *testing.T) {
	generated, err := generateEd25519Key("2f1c1b7e-0000-4000-8000-00000000abcd")
	if err != nil {
		t.Fatalf("generateEd25519Key: %v", err)
	}

	var public struct {
		Kty string `json:"kty"`
		Crv string `json:"crv"`
		X   string `json:"x"`
	}
	if err := json.Unmarshal(generated.PublicJWK, &public); err != nil {
		t.Fatalf("public JWK is not JSON: %v", err)
	}
	if public.Kty != "OKP" || public.Crv != "Ed25519" {
		t.Errorf("public JWK = kty %q crv %q, want OKP/Ed25519", public.Kty, public.Crv)
	}

	var private struct {
		X string `json:"x"`
		D string `json:"d"`
	}
	if err := json.Unmarshal(generated.PrivateJWK, &private); err != nil {
		t.Fatalf("private JWK is not JSON: %v", err)
	}
	if private.X != public.X {
		t.Errorf("private JWK carries a different public half: %q vs %q", private.X, public.X)
	}

	seed, err := base64.RawURLEncoding.DecodeString(private.D)
	if err != nil {
		t.Fatalf("private member d is not base64url: %v", err)
	}
	if len(seed) != ed25519.SeedSize {
		t.Fatalf("d is %d bytes, want the %d-byte seed", len(seed), ed25519.SeedSize)
	}
	pub, err := base64.RawURLEncoding.DecodeString(public.X)
	if err != nil {
		t.Fatalf("public member x is not base64url: %v", err)
	}

	// Signing with the private half must verify under the public half, which is
	// the only check that proves the two describe one keypair.
	message := []byte("assertion payload")
	signature := ed25519.Sign(ed25519.NewKeyFromSeed(seed), message)
	if !ed25519.Verify(ed25519.PublicKey(pub), message, signature) {
		t.Error("signature from the private JWK does not verify under the public JWK")
	}

	// The reported kid must be the thumbprint of the public half, since that is
	// what the server will independently compute and store.
	want, err := jwkThumbprint(generated.PublicJWK)
	if err != nil {
		t.Fatalf("jwkThumbprint: %v", err)
	}
	if generated.Kid != want {
		t.Errorf("Kid = %q, want the public key's thumbprint %q", generated.Kid, want)
	}
}

// The public JWK must not carry private members: it is sent to the server, and
// registering a key that contains its own private half would hand the identity
// to anyone who can read the table.
func TestGenerateEd25519KeyPublicHalfCarriesNoPrivateMembers(t *testing.T) {
	generated, err := generateEd25519Key("2f1c1b7e-0000-4000-8000-00000000abcd")
	if err != nil {
		t.Fatalf("generateEd25519Key: %v", err)
	}
	var jwk map[string]any
	if err := json.Unmarshal(generated.PublicJWK, &jwk); err != nil {
		t.Fatalf("public JWK is not JSON: %v", err)
	}
	for _, private := range []string{"d", "p", "q", "dp", "dq", "qi", "k"} {
		if _, found := jwk[private]; found {
			t.Errorf("public JWK carries the private member %q", private)
		}
	}
}

// A generated key is written before it is registered, so that a key the server
// trusts can never have a lost private half. The cost is that a failed
// registration leaves a file behind, and writePrivateKey refuses to overwrite --
// so without cleanup the alias is poisoned and the retry fails on a file the
// caller never knowingly created.
func TestWritePrivateKeyRefusesToOverwrite(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ci.jwk")
	if err := writePrivateKey(path, json.RawMessage(`{"kty":"OKP"}`)); err != nil {
		t.Fatalf("first write: %v", err)
	}

	err := writePrivateKey(path, json.RawMessage(`{"kty":"OKP"}`))
	if err == nil {
		t.Fatal("overwriting an existing key must be refused")
	}
	if !strings.Contains(err.Error(), "already exists") {
		t.Errorf("error does not explain the refusal: %v", err)
	}

	// Removing it makes the name usable again, which is what the failed
	// registration path relies on.
	if err := os.Remove(path); err != nil {
		t.Fatalf("remove: %v", err)
	}
	if err := writePrivateKey(path, json.RawMessage(`{"kty":"OKP"}`)); err != nil {
		t.Fatalf("write after removal: %v", err)
	}
}

// The key is owner-only. Anything that can read it can authenticate as the
// identity it belongs to.
func TestWritePrivateKeyIsOwnerOnly(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sub", "ci.jwk")
	if err := writePrivateKey(path, json.RawMessage(`{"kty":"OKP"}`)); err != nil {
		t.Fatalf("write: %v", err)
	}

	fi, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if perm := fi.Mode().Perm(); perm != 0o600 {
		t.Errorf("key file mode = %o, want 600", perm)
	}
	di, err := os.Stat(filepath.Dir(path))
	if err != nil {
		t.Fatalf("stat dir: %v", err)
	}
	if perm := di.Mode().Perm(); perm != 0o700 {
		t.Errorf("key directory mode = %o, want 700", perm)
	}
}
