// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package cubapi

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// One instance addressed two ways is one context. A raw string comparison says
// otherwise, which splits it: a login records against one and later commands
// read the other, and the remembered key appears to vanish.
func TestSameServerIgnoresInsignificantDifferences(t *testing.T) {
	same := [][2]string{
		{"https://hub.example.net", "https://hub.example.net/"},
		{"https://hub.example.net/", "https://hub.example.net///"},
		{"https://HUB.example.net", "https://hub.example.net"},
		{"HTTPS://hub.example.net", "https://hub.example.net"},
		{"https://hub.example.net", " https://hub.example.net "},
	}
	for _, pair := range same {
		assert.True(t, SameServer(pair[0], pair[1]), "%q and %q name one server", pair[0], pair[1])
	}
}

func TestSameServerKeepsRealDifferences(t *testing.T) {
	different := [][2]string{
		{"https://hub.example.net", "https://other.example.net"},
		{"https://hub.example.net", "http://hub.example.net"},
		{"https://hub.example.net", "https://hub.example.net:8443"},
		// The path is part of the address, so only a trailing slash is dropped.
		{"https://hub.example.net/api", "https://hub.example.net"},
	}
	for _, pair := range different {
		assert.False(t, SameServer(pair[0], pair[1]), "%q and %q are different servers", pair[0], pair[1])
	}
}

// A coordinate stored with a trailing slash must match a login without one,
// which is the case that sent a remembered private key to the wrong context.
func TestCoordinateEqualsIgnoresATrailingSlash(t *testing.T) {
	stored := Coordinate{User: "a@b.c", OrganizationID: "org", ServerURL: "https://hub.example.net/"}
	login := Coordinate{User: "a@b.c", OrganizationID: "org", ServerURL: "https://hub.example.net"}
	assert.True(t, stored.Equals(login))

	elsewhere := Coordinate{User: "a@b.c", OrganizationID: "org", ServerURL: "https://other.example.net"}
	assert.False(t, stored.Equals(elsewhere))
}
