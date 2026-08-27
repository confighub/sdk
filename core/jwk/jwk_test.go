// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package jwk_test

import (
	"encoding/base64"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/confighub/sdk/core/jwk"
)

// The thumbprint is the name the server stores a key under and the only thing an
// assertion presents to find it. A disagreement about how it is computed is
// invisible until it shows up as a uniform 401, so it is pinned here against the
// RFC's own vector rather than against our own output.

func TestThumbprintMatchesRFC7638Vector(t *testing.T) {
	// RFC 7638 §3.1, whose stated result is the whole point of the example.
	key := json.RawMessage(`{
		"kty": "RSA",
		"n": "0vx7agoebGcQSuuPiLJXZptN9nndrQmbXEps2aiAFbWhM78LhWx4cbbfAAtVT86zwu1RK7aPFFxuhDR1L6tSoc_BJECPebWKRXjBZCiFV4n3oknjhMstn64tZ_2W-5JsGY4Hc5n9yBXArwl93lqt7_RN5w6Cf0h4QyQ5v-65YGjQR0_FDW2QvzqY368QQMicAtaSqzs8KJZgnYb9c7d0zgdAZHzu6qMQvRL5hajrn1n91CbOpbISD08qNLyrdkt-bFTWhAI4vMQFh6WeZu0fM4lFd2NcRwr3XPksINHaQ-G_xBniIqbw0Ls1jF44-csFCur-kEgU8awapJzKnqDKgw",
		"e": "AQAB",
		"alg": "RS256",
		"kid": "2011-04-29"
	}`)

	got, err := jwk.Thumbprint(key)
	require.NoError(t, err)
	assert.Equal(t, "NzbLsXh8uDCcd-6MNwXF4W_7noWXFZAfHkxZsRGC9Xs", got)
}

// alg, use, and kid are not required members, so they cannot change a key's
// name. If they could, the same key described two ways would be two keys.
func TestThumbprintIgnoresNonRequiredMembers(t *testing.T) {
	bare := json.RawMessage(`{"kty":"OKP","crv":"Ed25519","x":"11qYAYKxCrfVS_7TyWQHOg7hcvPapiMlrwIaaPcHURo"}`)
	adorned := json.RawMessage(`{"use":"sig","alg":"EdDSA","kid":"anything","kty":"OKP","crv":"Ed25519","x":"11qYAYKxCrfVS_7TyWQHOg7hcvPapiMlrwIaaPcHURo"}`)

	a, err := jwk.Thumbprint(bare)
	require.NoError(t, err)
	b, err := jwk.Thumbprint(adorned)
	require.NoError(t, err)
	assert.Equal(t, a, b)
}

// The private members are not required members either, which is what lets a
// holder name its own key from the private half alone.
func TestThumbprintOfBothHalvesAgrees(t *testing.T) {
	pair, err := jwk.GenerateEd25519("")
	require.NoError(t, err)

	fromPrivate, err := jwk.Thumbprint(pair.PrivateJWK)
	require.NoError(t, err)
	assert.Equal(t, pair.Kid, fromPrivate)
}

func TestThumbprintRejectsIncompleteAndUnknownKeys(t *testing.T) {
	for name, key := range map[string]string{
		"unknown key type": `{"kty":"oct","k":"c2VjcmV0"}`,
		"missing x":        `{"kty":"OKP","crv":"Ed25519"}`,
		"empty x":          `{"kty":"OKP","crv":"Ed25519","x":""}`,
		"missing n":        `{"kty":"RSA","e":"AQAB"}`,
		"not json":         `not a jwk`,
	} {
		t.Run(name, func(t *testing.T) {
			_, err := jwk.Thumbprint(json.RawMessage(key))
			assert.Error(t, err)
		})
	}
}

func TestGenerateEd25519ProducesAUsableKeypair(t *testing.T) {
	pair, err := jwk.GenerateEd25519("confighub:test:alice")
	require.NoError(t, err)

	var pub map[string]string
	require.NoError(t, json.Unmarshal(pair.PublicJWK, &pub))
	assert.Equal(t, "OKP", pub["kty"])
	assert.Equal(t, "Ed25519", pub["crv"])
	assert.NotEmpty(t, pub["x"])
	assert.NotContains(t, pub, "d", "the public half must never carry private material")

	var priv map[string]string
	require.NoError(t, json.Unmarshal(pair.PrivateJWK, &priv))
	assert.Equal(t, pub["x"], priv["x"])
	assert.NotEmpty(t, priv["d"])
	assert.Equal(t, "confighub:test:alice", priv[jwk.UserExternalIDMember])
}

// RFC 8037 §2 defines d as the 32-byte seed. ed25519.PrivateKey stores
// seed || public, and emitting all 64 bytes produces a key other
// implementations reject -- including our own assertion signer.
func TestGeneratedPrivateMemberIsTheSeedNotTheExpandedKey(t *testing.T) {
	pair, err := jwk.GenerateEd25519("")
	require.NoError(t, err)

	var priv struct {
		D string `json:"d"`
	}
	require.NoError(t, json.Unmarshal(pair.PrivateJWK, &priv))

	decoded, err := base64.RawURLEncoding.DecodeString(priv.D)
	require.NoError(t, err)
	assert.Len(t, decoded, 32, "d must be the seed, not the expanded key")
}

func TestGenerateEd25519OmitsTheIdentityMemberWhenNoneIsGiven(t *testing.T) {
	pair, err := jwk.GenerateEd25519("")
	require.NoError(t, err)

	var priv map[string]string
	require.NoError(t, json.Unmarshal(pair.PrivateJWK, &priv))
	assert.NotContains(t, priv, jwk.UserExternalIDMember)
}

func TestGeneratedKeysAreDistinct(t *testing.T) {
	a, err := jwk.GenerateEd25519("")
	require.NoError(t, err)
	b, err := jwk.GenerateEd25519("")
	require.NoError(t, err)
	assert.NotEqual(t, a.Kid, b.Kid)
}
