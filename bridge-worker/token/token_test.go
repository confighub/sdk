// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package token

import (
	"crypto/rand"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDefaultSpec(t *testing.T) {
	// Set environment variable for test
	originalSecret := os.Getenv("WORKER_MASTER_SECRET")
	os.Setenv("WORKER_MASTER_SECRET", "test-secret")
	defer os.Setenv("WORKER_MASTER_SECRET", originalSecret)

	spec := DefaultSpec()
	assert.Equal(t, "ch_", spec.Prefix, "unexpected prefix")
	assert.Equal(t, 4, spec.RandomLength, "unexpected random length")
	assert.Equal(t, []byte("test-secret"), spec.SecretKey, "unexpected secret key")
	assert.Equal(t, 90*24*time.Hour, spec.TTL, "unexpected TTL")
}

func TestGenerate(t *testing.T) {
	tests := []struct {
		name    string
		spec    *Spec
		wantErr bool
	}{
		{
			name: "valid spec",
			spec: DefaultSpec().
				WithPrefix("test_").
				WithRandomLength(4).
				WithSecretKey([]byte("secret")).
				WithTTL(time.Hour).
				WithRandReader(rand.Reader),
			wantErr: false,
		},
		{
			name: "zero random length",
			spec: DefaultSpec().
				WithPrefix("test_").
				WithRandomLength(0).
				WithSecretKey([]byte("secret")).
				WithTTL(time.Hour).
				WithRandReader(rand.Reader),
			wantErr: true,
		},
		{
			name: "empty secret key",
			spec: DefaultSpec().
				WithPrefix("test_").
				WithRandomLength(4).
				WithSecretKey([]byte{}).
				WithTTL(time.Hour).
				WithRandReader(rand.Reader),
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			token, err := Generate(tt.spec)
			if tt.wantErr {
				assert.Error(t, err)
				return
			}

			assert.NoError(t, err)
			assert.True(t, strings.HasPrefix(token, tt.spec.Prefix), "token should have correct prefix")
		})
	}
}

func TestVerify(t *testing.T) {
	validSpec := DefaultSpec().
		WithPrefix("test_").
		WithRandomLength(4).
		WithSecretKey([]byte("secret")).
		WithTTL(time.Hour).
		WithRandReader(rand.Reader)

	// Generate a valid token for testing
	validToken, err := Generate(validSpec)
	require.NoError(t, err, "Failed to generate valid token")

	// Create an expired token
	expiredSpec := DefaultSpec().
		WithPrefix("test_").
		WithRandomLength(4).
		WithSecretKey([]byte("secret")).
		WithTTL(-(time.Hour + time.Minute)).
		WithRandReader(rand.Reader)
	expiredToken, err := Generate(expiredSpec)
	require.NoError(t, err, "Failed to generate expired token")

	tests := []struct {
		name    string
		spec    *Spec
		token   string
		wantErr bool
	}{
		{
			name:    "valid token",
			spec:    validSpec,
			token:   validToken,
			wantErr: false,
		},
		{
			name:    "invalid prefix",
			spec:    validSpec,
			token:   "invalid_" + strings.TrimPrefix(validToken, validSpec.Prefix),
			wantErr: true,
		},
		{
			name:    "invalid base32",
			spec:    validSpec,
			token:   validSpec.Prefix + "invalid!@#$",
			wantErr: true,
		},
		{
			name:    "invalid length",
			spec:    validSpec,
			token:   validSpec.Prefix + strings.ToLower(base32Encoding.EncodeToString([]byte("short"))),
			wantErr: true,
		},
		{
			name:    "expired token",
			spec:    validSpec,
			token:   expiredToken,
			wantErr: true,
		},
		{
			name: "invalid signature",
			spec: DefaultSpec().
				WithPrefix(validSpec.Prefix).
				WithRandomLength(validSpec.RandomLength).
				WithSecretKey([]byte("different-secret")).
				WithTTL(validSpec.TTL).
				WithRandReader(rand.Reader),
			token:   validToken,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := Verify(tt.spec, tt.token)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestTokenRoundTrip(t *testing.T) {
	spec := DefaultSpec().
		WithPrefix("test_").
		WithRandomLength(16).
		WithSecretKey([]byte("test-secret-key")).
		WithTTL(time.Hour).
		WithRandReader(rand.Reader)

	// Generate token
	token, err := Generate(spec)
	require.NoError(t, err, "Generate() failed")

	// Verify the generated token
	err = Verify(spec, token)
	assert.NoError(t, err, "Verify() failed for valid token")
}

// errorReader is a mock reader that always returns an error
type errorReader struct{}

func (r errorReader) Read(p []byte) (n int, err error) {
	return 0, errors.New("mock random reader error")
}

func TestGenerateRandomError(t *testing.T) {
	spec := DefaultSpec().
		WithPrefix("test_").
		WithRandomLength(4).
		WithSecretKey([]byte("secret")).
		WithTTL(time.Hour).
		WithRandReader(errorReader{})

	token, err := Generate(spec)
	assert.Error(t, err, "Generate should fail when random reader fails")
	assert.Equal(t, "", token, "Token should be empty on error")
	assert.Contains(t, err.Error(), "failed to generate random bytes", "Error should indicate random generation failure")
}
