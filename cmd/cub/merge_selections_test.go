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
// overrode and per pending revision it went ahead of.
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
	} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("output %q does not contain %q", out.String(), want)
		}
	}
}
