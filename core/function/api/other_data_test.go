// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package api

import (
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOtherDataUnitRoundTrip(t *testing.T) {
	unitID := uuid.New()
	source := FormatOtherDataUnit("policy", unitID)
	assert.Equal(t, "Unit:policy:"+unitID.String(), source)
	key, gotID, ok, err := ParseOtherDataUnit(source)
	require.NoError(t, err)
	assert.True(t, ok)
	assert.Equal(t, "policy", key)
	assert.Equal(t, unitID, gotID)

	// A revision specifier is not a Unit, and not an error either.
	_, _, ok, err = ParseOtherDataUnit("Before:HeadRevisionNum")
	require.NoError(t, err)
	assert.False(t, ok)

	for _, bad := range []string{"Unit:" + unitID.String(), "Unit:policy:", "Unit::" + unitID.String(), "Unit:policy:" + uuid.Nil.String()} {
		_, _, ok, err := ParseOtherDataUnit(bad)
		assert.True(t, ok, bad)
		assert.Error(t, err, bad)
	}
}

func TestValidateOtherDataKey(t *testing.T) {
	for _, key := range []string{"schema", "policy", "LastReleasedRevisionNum", "my-key_2"} {
		assert.NoError(t, ValidateOtherDataKey(key), key)
	}
	for _, key := range []string{"", "Before:HeadRevisionNum", "pol icy", "a.b", strings.Repeat("k", MaxOtherDataKeyLength+1)} {
		assert.Error(t, ValidateOtherDataKey(key), key)
	}
}
