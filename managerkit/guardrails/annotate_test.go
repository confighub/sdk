// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package guardrails

import (
	"bytes"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	goclientnew "github.com/confighub/sdk/core/openapi/goclient-new"
)

// A Unit whose guards withheld the annotation is marked and not counted as annotated, on a
// commit and on a dry run alike.
func TestPrintAnnotateCountsWithheldAsNotAnnotated(t *testing.T) {
	results := []Result{
		{Space: "prod", Unit: "web", Value: "missing", OK: true},
		{Space: "prod", Unit: "api", Value: "missing", Withheld: []goclientnew.MutationConflict{{Reason: "Guarded"}}},
	}
	for _, tc := range []struct {
		dryRun bool
		want   string
	}{
		{false, "Annotated 1 of 2 Unit(s)."},
		{true, "Dry run — 1 of 2 Unit(s) would be annotated."},
	} {
		var out bytes.Buffer
		cmd := &cobra.Command{}
		cmd.SetOut(&out)
		printAnnotate(cmd, results, "Unit", tc.dryRun)
		if !strings.Contains(out.String(), tc.want) {
			t.Errorf("dryRun=%v: missing %q in:\n%s", tc.dryRun, tc.want, out.String())
		}
		if !strings.Contains(out.String(), "api  missing  WITHHELD: 1 path(s)") {
			t.Errorf("dryRun=%v: withheld Unit not marked:\n%s", tc.dryRun, out.String())
		}
	}
}
