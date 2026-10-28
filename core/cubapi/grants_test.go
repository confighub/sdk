// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package cubapi

import (
	"sort"
	"testing"

	goclientnew "github.com/confighub/sdk/core/openapi/goclient-new"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
)

func TestGrantedActions(t *testing.T) {
	user, groupA, groupB, other := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	permissions := goclientnew.Permissions{
		"View":   {UserIDs: map[string]bool{user.String(): true}, GroupIDs: map[string]bool{groupA.String(): true}},
		"Edit":   {GroupIDs: map[string]bool{groupA.String(): true, groupB.String(): true}},
		"Manage": {UserIDs: map[string]bool{other.String(): true}},
		"Use":    {UserIDs: map[string]bool{user.String(): false}},
	}

	got := grantedActions(&permissions, user, []goclientnew.UUID{groupA, groupB})
	assert.Equal(t, []GrantedAction{
		{Action: "Edit", GroupIDs: []goclientnew.UUID{groupA, groupB}},
		{Action: "View", Direct: true, GroupIDs: []goclientnew.UUID{groupA}},
	}, got)

	assert.Empty(t, grantedActions(nil, user, nil))
	assert.Equal(t, []GrantedAction{{Action: "Manage", Direct: true}}, grantedActions(&permissions, other, nil))
	assert.Empty(t, grantedActions(&permissions, uuid.New(), nil), "a User named nowhere is granted nothing")
}

func TestNaturalLess(t *testing.T) {
	names := []string{"release-10", "release-9", "release-100", "beta", "release-2", "alpha-01", "alpha-1x"}
	sort.Slice(names, func(i, j int) bool { return naturalLess(names[i], names[j]) })
	assert.Equal(t, []string{"alpha-01", "alpha-1x", "beta", "release-2", "release-9", "release-10", "release-100"}, names)
}
