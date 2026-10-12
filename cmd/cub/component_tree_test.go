// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"reflect"
	"strings"
	"testing"

	goclientnew "github.com/confighub/sdk/core/openapi/goclient-new"
)

func treeSpace(id byte, slug, variant string, target *goclientnew.UUID) *goclientnew.ExtendedSpace {
	s := &goclientnew.Space{SpaceID: goclientnew.UUID{id}, Slug: slug, ReleaseTargetID: target}
	if variant != "" {
		s.Labels = map[string]string{"Variant": variant}
	}
	return &goclientnew.ExtendedSpace{Space: s}
}

// TestArrangeVariants: a variant hangs under the space most of its units came from, when
// that is in the component; anything else is a root; a cycle is broken rather than lost.
func TestArrangeVariants(t *testing.T) {
	devTarget, prodTarget := goclientnew.UUID{101}, goclientnew.UUID{102}
	targets := map[goclientnew.UUID]string{devTarget: "dev/target", prodTarget: "prod/target"}
	spaces := []*goclientnew.ExtendedSpace{
		treeSpace(1, "web-base", "base", nil),
		treeSpace(2, "web-dev", "dev", &devTarget),
		treeSpace(3, "web-prod", "prod", &prodTarget),
		treeSpace(4, "web-canary", "canary", nil),
		treeSpace(5, "web-scratch", "", nil),
	}
	upstreams := map[goclientnew.UUID]map[goclientnew.UUID]int{
		{2}: {{1}: 7},
		{3}: {{1}: 7},
		// Most of canary's units came from prod; one was cloned from elsewhere.
		{4}: {{3}: 6, {1}: 1},
		// scratch was cloned from a space of another component.
		{5}: {{9}: 3},
	}
	roots := arrangeVariants(spaces, upstreams, targets)
	got := renderComponentTree(componentTree{Slug: "web", Roots: roots})
	// The arrows align on the widest line that has a target; canary has none.
	want := []string{
		"web",
		"├── base",
		"│   ├── dev     → dev/target",
		"│   └── prod    → prod/target",
		"│       └── canary",
		"└── web-scratch",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("tree:\n%s\nwant:\n%s", joinLines(got), joinLines(want))
	}

	// A cycle: each of two variants was cloned from the other. One becomes a root, the other
	// hangs under it, and neither is lost.
	cycle := arrangeVariants(spaces[:3], map[goclientnew.UUID]map[goclientnew.UUID]int{
		{2}: {{3}: 2},
		{3}: {{2}: 2},
	}, targets)
	got = renderComponentTree(componentTree{Slug: "web", Roots: cycle})
	want = []string{
		"web",
		"├── base",
		"└── dev         → dev/target",
		"    └── prod    → prod/target",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("cycle:\n%s\nwant:\n%s", joinLines(got), joinLines(want))
	}
}

func joinLines(lines []string) string {
	return strings.Join(lines, "\n")
}
