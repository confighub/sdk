// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package token

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base32"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/cockroachdb/errors"
)

type Spec struct {
	Prefix       string
	RandomLength int           // number of random bytes
	SecretKey    []byte        // secret key for HMAC
	TTL          time.Duration // token time-to-live
	RandReader   io.Reader     // random number generator
}

// Base32 encoding without padding (still produces uppercase).
var base32Encoding = base32.StdEncoding.WithPadding(base32.NoPadding)

func DefaultSpec() *Spec {
	return &Spec{
		Prefix:       "ch_",
		RandomLength: 4,
		SecretKey:    []byte(os.Getenv("WORKER_MASTER_SECRET")),
		TTL:          90 * 24 * time.Hour,
		RandReader:   rand.Reader,
	}
}

// WithPrefix sets a custom prefix for the spec
func (s *Spec) WithPrefix(prefix string) *Spec {
	s.Prefix = prefix
	return s
}

// WithRandomLength sets the length of random bytes
func (s *Spec) WithRandomLength(length int) *Spec {
	s.RandomLength = length
	return s
}

// WithSecretKey sets a custom secret key
func (s *Spec) WithSecretKey(key []byte) *Spec {
	s.SecretKey = key
	return s
}

// WithTTL sets a custom time-to-live duration
func (s *Spec) WithTTL(ttl time.Duration) *Spec {
	s.TTL = ttl
	return s
}

// WithRandReader sets a custom random number generator
func (s *Spec) WithRandReader(reader io.Reader) *Spec {
	s.RandReader = reader
	return s
}

func Generate(spec *Spec) (string, error) {
	if spec.RandomLength <= 0 {
		return "", errors.New("RandomLength must be greater than 0")
	}
	if len(spec.SecretKey) == 0 {
		return "", errors.New("SecretKey must not be empty")
	}

	// Generate random bytes
	rnd := make([]byte, spec.RandomLength)
	if _, err := io.ReadFull(spec.RandReader, rnd); err != nil {
		return "", fmt.Errorf("failed to generate random bytes: %w", err)
	}

	// Compute expiration timestamp
	timestamp := time.Now().Add(spec.TTL).Unix()
	timestampBytes := make([]byte, 8)
	tmp := timestamp
	for i := 7; i >= 0; i-- {
		timestampBytes[i] = byte(tmp & 0xff)
		tmp >>= 8
	}

	// Prepare data for HMAC
	data := rnd
	data = append(data, timestampBytes...)

	mac := hmac.New(sha256.New, spec.SecretKey)
	mac.Write(data)
	hmacBytes := mac.Sum(nil)

	// finalData is random|timestamp|hmac.
	finalData := data
	finalData = append(finalData, hmacBytes...)

	// Encode with Base32 (uppercase).
	encoded := base32Encoding.EncodeToString(finalData)

	// Convert to lowercase for the final token.
	lowerEncoded := strings.ToLower(encoded)

	return spec.Prefix + lowerEncoded, nil
}

func Verify(spec *Spec, token string) error {
	if !strings.HasPrefix(token, spec.Prefix) {
		return errors.New("invalid token prefix")
	}

	encoded := strings.TrimPrefix(token, spec.Prefix)

	// Convert to uppercase before decoding since base32 expects uppercase
	upperEncoded := strings.ToUpper(encoded)

	tokenBytes, err := base32Encoding.DecodeString(upperEncoded)
	if err != nil {
		return errors.Wrap(err, "invalid token format")
	}

	// Calculate expected length: random + timestamp + hmac
	expectedLength := spec.RandomLength + 8 + sha256.Size
	if len(tokenBytes) != expectedLength {
		return errors.New("invalid token length")
	}

	randomBytes := tokenBytes[:spec.RandomLength]
	timestampBytes := tokenBytes[spec.RandomLength : spec.RandomLength+8]
	receivedHMAC := tokenBytes[spec.RandomLength+8:]

	// Convert timestamp bytes to int64
	var timestamp int64
	for _, b := range timestampBytes {
		timestamp = (timestamp << 8) | int64(b)
	}

	// Check expiration
	expirationTime := time.Unix(timestamp, 0)
	if time.Now().After(expirationTime) {
		return fmt.Errorf("token expired at %v", expirationTime)
	}

	// Verify HMAC
	data := randomBytes
	data = append(data, timestampBytes...)
	mac := hmac.New(sha256.New, spec.SecretKey)
	mac.Write(data)
	expectedHMAC := mac.Sum(nil)

	if !hmac.Equal(receivedHMAC, expectedHMAC) {
		return errors.New("invalid token signature")
	}

	return nil
}
