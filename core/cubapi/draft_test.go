// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package cubapi

import (
	"testing"

	goclientnew "github.com/confighub/sdk/core/openapi/goclient-new"
)

func TestLinkHasPending(t *testing.T) {
	for _, tc := range []struct {
		name string
		link goclientnew.Link
		head int64
		want bool
	}{
		{"level", goclientnew.Link{UpstreamLastMergedRevisionNum: 3}, 3, false},
		{"behind", goclientnew.Link{UpstreamLastMergedRevisionNum: 3}, 5, true},
		{"taken above", goclientnew.Link{UpstreamLastMergedRevisionNum: 3, UpstreamMergedAbove: []int64{4, 5}}, 5, false},
		{"skipped above", goclientnew.Link{UpstreamLastMergedRevisionNum: 3,
			UpstreamSkipped: []goclientnew.LinkSkippedRevisions{{FromRevisionNum: 4, ToRevisionNum: 5}}}, 5, false},
		{"one left", goclientnew.Link{UpstreamLastMergedRevisionNum: 3, UpstreamMergedAbove: []int64{5}}, 5, true},
	} {
		if got := linkHasPending(&tc.link, tc.head); got != tc.want {
			t.Errorf("%s: linkHasPending = %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestValidateDraftName(t *testing.T) {
	for _, name := range []string{"memory", "raise-web-memory", "v2"} {
		if err := ValidateDraftName(name); err != nil {
			t.Errorf("%q: %v", name, err)
		}
	}
	for _, name := range []string{"", "-x", "x-", "Upper", "a/b", "a_b"} {
		if err := ValidateDraftName(name); err == nil {
			t.Errorf("%q: want an error", name)
		}
	}
}

func TestDraftNames(t *testing.T) {
	if got := DraftSpaceSlug("acme-base", "fix"); got != "acme-base-draft-fix" {
		t.Errorf("DraftSpaceSlug = %q", got)
	}
	if got := draftVariantLabel("base", "fix"); got != "base-draft-fix" {
		t.Errorf("draftVariantLabel = %q", got)
	}
	if got := draftVariantLabel("", "fix"); got != "draft-fix" {
		t.Errorf("draftVariantLabel with no variant = %q", got)
	}
}
