// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"reflect"
	"testing"

	goclientnew "github.com/confighub/sdk/core/openapi/goclient-new"
)

// TestVariantNames: the VARIANTS column names variants as --variant takes them, by Variant
// label, and falls back to the slug for a space that carries none.
func TestVariantNames(t *testing.T) {
	space := func(slug, variant string) *goclientnew.ExtendedSpace {
		s := &goclientnew.Space{Slug: slug}
		if variant != "" {
			s.Labels = map[string]string{"Variant": variant}
		}
		return &goclientnew.ExtendedSpace{Space: s}
	}
	got := variantNames([]*goclientnew.ExtendedSpace{
		space("web-prod", "prod"),
		space("web-base", "base"),
		space("web-scratch", ""),
		nil,
		space("web-dev", "dev"),
	})
	want := []string{"base", "dev", "prod", "web-scratch"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("variantNames = %v, want %v", got, want)
	}
}

// TestLastColumnWidth: the last column gets what the other columns and their padding leave
// of the terminal, never less than minWrapWidth, and nothing when there is no terminal.
func TestLastColumnWidth(t *testing.T) {
	header := []string{"Name", "Owner", "Variants"}
	rows := [][]string{{"web", "platform-team", "base, dev"}, {"api", "", "base"}}
	// "Name" is 4 wide, "platform-team" 13: 17 plus two paddings of 4.
	for _, tc := range []struct{ terminal, want int }{
		{120, 95},
		{40, minWrapWidth},
		{0, 0},
	} {
		if got := lastColumnWidth(rows, header, tc.terminal); got != tc.want {
			t.Errorf("terminal %d: width %d, want %d", tc.terminal, got, tc.want)
		}
	}
}

func TestWrapList(t *testing.T) {
	list := "base, nonprod, prod, use1-cards-nonprod1, use1-cards-prod1"
	if got := wrapList(list, 0); got != list {
		t.Errorf("width 0 wrapped: %q", got)
	}
	want := "base, nonprod, prod,\nuse1-cards-nonprod1,\nuse1-cards-prod1"
	if got := wrapList(list, 22); got != want {
		t.Errorf("width 22: %q, want %q", got, want)
	}
}
