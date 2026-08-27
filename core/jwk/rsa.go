// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package jwk

import (
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math/big"

	"github.com/google/uuid"
)

// The RSA signing key a ConfigHub server mints its tokens with.
//
// Written by whatever stands a server up and read back from JWT_PRIVATE_KEY_JWK
// at startup, so the encoding is a contract between two programs. A server that
// cannot use the key it was given generates its own and carries on, so a
// disagreement shows up as every session ending at the next restart rather than
// as an error at boot.

// rsaSigningJWK is the wire form; member names are RFC 7518 §6.3's.
//
// p and q are required -- a reader reconstructs the key from the primes. The CRT
// values dp, dq and qi are optional and recomputable, but are emitted so a key
// is fully specified by its encoding.
type rsaSigningJWK struct {
	Kty string `json:"kty"`
	Use string `json:"use"`
	Alg string `json:"alg"`
	Kid string `json:"kid"`
	N   string `json:"n"`
	E   string `json:"e"`
	D   string `json:"d,omitempty"`
	P   string `json:"p,omitempty"`
	Q   string `json:"q,omitempty"`
	Dp  string `json:"dp,omitempty"`
	Dq  string `json:"dq,omitempty"`
	Qi  string `json:"qi,omitempty"`
}

// RSASigningKeyBits is the modulus size for a generated signing key.
const RSASigningKeyBits = 2048

// GenerateRSASigningKey mints a signing key and returns it as a private JWK,
// along with the key id embedded in it.
//
// The key id is informational -- the server reads the key it is given rather
// than selecting among several -- so a key can be named in logs and told apart
// from its successor after a rotation.
func GenerateRSASigningKey() (privateJWK string, kid string, err error) {
	key, err := rsa.GenerateKey(rand.Reader, RSASigningKeyBits)
	if err != nil {
		return "", "", fmt.Errorf("generating an RSA signing key: %w", err)
	}
	kid = fmt.Sprintf("confighub_key_%s", uuid.New().String()[:8])

	privateJWK, err = RSAPrivateKeyToJWK(key, kid)
	if err != nil {
		return "", "", err
	}
	return privateJWK, kid, nil
}

// RSAPrivateKeyToJWK renders an RSA private key as a JWK JSON string.
//
// Precompute runs first so the CRT values are present; without it, a generated
// key and a loaded key serialise differently.
func RSAPrivateKeyToJWK(key *rsa.PrivateKey, kid string) (string, error) {
	if key == nil {
		return "", fmt.Errorf("no key to encode")
	}
	key.Precompute()

	b64 := base64.RawURLEncoding.EncodeToString
	out := rsaSigningJWK{
		Kty: "RSA",
		Use: "sig",
		Alg: "RS256",
		Kid: kid,
		N:   b64(key.PublicKey.N.Bytes()),
		E:   b64(big.NewInt(int64(key.PublicKey.E)).Bytes()),
		D:   b64(key.D.Bytes()),
	}
	if len(key.Primes) >= 2 {
		out.P = b64(key.Primes[0].Bytes())
		out.Q = b64(key.Primes[1].Bytes())
	}
	if key.Precomputed.Dp != nil {
		out.Dp = b64(key.Precomputed.Dp.Bytes())
		out.Dq = b64(key.Precomputed.Dq.Bytes())
		out.Qi = b64(key.Precomputed.Qinv.Bytes())
	}

	encoded, err := json.Marshal(out)
	if err != nil {
		return "", fmt.Errorf("encoding the signing key: %w", err)
	}
	return string(encoded), nil
}
