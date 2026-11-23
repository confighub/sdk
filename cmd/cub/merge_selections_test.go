// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"bytes"
	"io"
	"os"
	"strings"
	"testing"

	goclientnew "github.com/confighub/sdk/core/openapi/goclient-new"
)

// What a merge naming its revisions took reads as one line per link, then one per skip it
// overrode, per declined skip it held to, and per pending revision it went ahead of.
func TestDisplayMergeSelections(t *testing.T) {
	saved := os.Stdout
	read, write, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = write
	displayMergeSelections("app", []goclientnew.MergeSelection{{
		Taken:           []int64{3, 6, 7},
		SkipsOverridden: []goclientnew.MergeSkipOverridden{{RevisionNum: 3, Reason: "not in prod"}},
		Overlaps:        []goclientnew.MergeOverlap{{RevisionNum: 6, PendingRevisionNum: 5, ResourceName: "ns/web", Paths: []string{"spec.replicas"}}},
		DeclinedHeld:    []goclientnew.MergeSkipOverridden{{RevisionNum: 4, Reason: "never in prod"}},
	}}, true)
	_ = write.Close()
	os.Stdout = saved
	var out bytes.Buffer
	if _, err := io.Copy(&out, read); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"Unit app would take upstream revisions 3, 6-7",
		"revision 3 was skipped: not in prod; taking it removes the skip",
		"revision 6 would be taken ahead of pending revision 5, which also wrote spec.replicas of ns/web",
		"revision 4 was declined: never in prod; it is not taken until its skip is made an ordinary one",
	} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("output %q does not contain %q", out.String(), want)
		}
	}
}

// A default upgrade's selection lists only what it passes over because the unit made the same
// change itself, with how.
func TestDisplayMergeSelectionsAlreadyHere(t *testing.T) {
	saved := os.Stdout
	read, write, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = write
	displayMergeSelections("app", []goclientnew.MergeSelection{{
		AlreadyHere: []goclientnew.MergeAlreadyHere{
			{RevisionNum: 4, DownstreamRevisionNum: 9, Kind: goclientnew.MergeAlreadyHereKindResolve, SourceRevisionNum: 60},
			{RevisionNum: 5, DownstreamRevisionNum: 10, Kind: goclientnew.MergeAlreadyHereKindChangeOrder},
		},
	}}, true)
	_ = write.Close()
	os.Stdout = saved
	var out bytes.Buffer
	if _, err := io.Copy(&out, read); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"Unit app would pass over upstream revisions 4-5: it made the same changes itself",
		"revision 4 is already here: the unit's revision 9 made the same change, resolving the same source unit at revision 60 or later",
		"revision 5 is already here: the unit's revision 10 made the same change, under the same change order",
	} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("output %q does not contain %q", out.String(), want)
		}
	}
	if strings.Contains(out.String(), "merged them all already") {
		t.Errorf("output %q should not say the unit merged everything already", out.String())
	}
}
