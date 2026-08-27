// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package cubapi_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/confighub/sdk/core/cubapi"
)

// A private key file is routinely the only copy of a credential whose public
// half is already registered. The permissions and the refusal to overwrite are
// the point of this function, so they are what is asserted.

func testStore(t *testing.T) *cubapi.Store {
	t.Helper()
	s, err := cubapi.LoadConfig(filepath.Join(t.TempDir(), "config.yaml"))
	require.NoError(t, err)
	return s
}

func TestWritePrivateKeyIsOwnerReadableOnly(t *testing.T) {
	s := testStore(t)

	path, err := s.WritePrivateKey("admin", json.RawMessage(`{"kty":"OKP"}`))
	require.NoError(t, err)
	assert.Equal(t, ".jwk", filepath.Ext(path), "a bare alias should gain the .jwk extension")

	info, err := os.Stat(path)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), info.Mode().Perm(), "private key must not be group or world readable")

	dir, err := os.Stat(filepath.Dir(path))
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o700), dir.Mode().Perm(), "key directory must not be listable by others")
}

func TestWritePrivateKeyRefusesToOverwrite(t *testing.T) {
	s := testStore(t)

	_, err := s.WritePrivateKey("admin", json.RawMessage(`{"first":true}`))
	require.NoError(t, err)

	_, err = s.WritePrivateKey("admin", json.RawMessage(`{"second":true}`))
	require.Error(t, err, "overwriting would destroy the only copy of a live credential")

	// And the original survives intact.
	path := s.KeyPath("admin")
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Contains(t, string(data), "first")
}
