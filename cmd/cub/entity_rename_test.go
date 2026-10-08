// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"reflect"
	"testing"
)

// A display name that follows the slug is renamed with it; one chosen separately is kept unless
// --display-name says otherwise.
func TestRenamePatch(t *testing.T) {
	following := &renamedEntity{slug: "old", displayName: "old"}
	chosen := &renamedEntity{slug: "old", displayName: "Old Name"}

	for _, test := range []struct {
		entity           *renamedEntity
		newSlug          string
		displayName      string
		displayNameGiven bool
		want             map[string]any
	}{
		{following, "new", "", false, map[string]any{"Slug": "new", "DisplayName": "new"}},
		{chosen, "new", "", false, map[string]any{"Slug": "new"}},
		{chosen, "new", "New Name", true, map[string]any{"Slug": "new", "DisplayName": "New Name"}},
		{following, "old", "Old", true, map[string]any{"DisplayName": "Old"}},
		{following, "old", "", false, map[string]any{}},
		{chosen, "old", "Old Name", true, map[string]any{}},
	} {
		got := renamePatch(test.entity, test.newSlug, test.displayName, test.displayNameGiven)
		if !reflect.DeepEqual(got, test.want) {
			t.Errorf("renaming %+v to %q with display name %q (given %v): got %v, want %v",
				*test.entity, test.newSlug, test.displayName, test.displayNameGiven, got, test.want)
		}
	}
}
