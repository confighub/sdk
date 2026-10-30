// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package write

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/confighub/sdk/core/cubapi"
	goclientnew "github.com/confighub/sdk/core/openapi/goclient-new"
	"github.com/google/uuid"
)

// A malformed target is rejected before the Space lookup, so no request is made
// for an argument that was never a target.
func TestParseUnitRefRejectsMalformedTarget(t *testing.T) {
	for _, bad := range []string{"nospace", "/unit", "space/", ""} {
		if _, err := ParseUnitRef(context.Background(), nil, bad); err == nil {
			t.Errorf("ParseUnitRef(%q) was accepted", bad)
		}
	}
}

func TestUnitRefSelector(t *testing.T) {
	id := goclientnew.UUID(uuid.MustParse("11111111-1111-1111-1111-111111111111"))
	sel := UnitRef{SpaceID: id, SpaceSlug: "prod", UnitSlug: "web"}.Selector()
	want := "SpaceID = '11111111-1111-1111-1111-111111111111' AND Slug = 'web'"
	if sel.Where != want {
		t.Errorf("selector = %q, want %q", sel.Where, want)
	}
}

func TestChangeIsDryRunWhenUndescribed(t *testing.T) {
	// An empty Description is what makes a call a dry run, so a dry run is
	// spelled by not describing one.
	if ch := Change("scale to 3", true); !ch.DryRun() {
		t.Errorf("dry run carried a description: %+v", ch)
	}
	ch := Change("scale to 3", false)
	if ch.DryRun() || ch.Description != "scale to 3" {
		t.Errorf("commit = %+v", ch)
	}
	// Both ask for the diff, which is what lets a dry run show what it would do.
	if !Change("", true).Diff || !ch.Diff {
		t.Error("Change does not ask for the diff")
	}
}

func replicasDiff(from, to string) *goclientnew.ConfigDiff {
	update := goclientnew.Update
	return &goclientnew.ConfigDiff{Resources: []goclientnew.ResourceDiff{{
		Resource:   &goclientnew.ResourceInfo{ResourceType: "apps/v1/Deployment", ResourceName: "prod/web"},
		ChangeType: &update,
		Changes:    []goclientnew.PathChange{{DisplayPath: "spec.replicas", ChangeType: "Update", FromValue: from, ToValue: to}},
	}}}
}

// A Unit's diffs are kept in the order its invocations ran; one that changed
// nothing adds none.
func TestSummarizeKeepsDiffs(t *testing.T) {
	first := &cubapi.Result{Outcomes: []cubapi.UnitOutcome{
		{UnitID: "u-web", SpaceSlug: "prod", UnitSlug: "web", Success: true, HasMutations: true, Diff: replicasDiff("1", "3")},
		{UnitID: "u-api", SpaceSlug: "prod", UnitSlug: "api", Success: true, Diff: &goclientnew.ConfigDiff{}},
	}}
	second := &cubapi.Result{Outcomes: []cubapi.UnitOutcome{
		{UnitID: "u-web", SpaceSlug: "prod", UnitSlug: "web", Success: true, HasMutations: true, Diff: replicasDiff("3", "4")},
	}}
	rep := Summarize("scale", "prod", true, first, second)
	api, web := rep.Outcomes[0], rep.Outcomes[1]
	if len(api.Diffs) != 0 {
		t.Errorf("api diffs = %+v, want none for an unchanged Unit", api.Diffs)
	}
	if len(web.Diffs) != 2 || web.Diffs[0].Resources[0].Changes[0].ToValue != "3" || web.Diffs[1].Resources[0].Changes[0].ToValue != "4" {
		t.Errorf("web diffs = %+v, want both in order", web.Diffs)
	}
}

// A command with a report of its own gets the same diffs, keyed by Unit.
func TestDiffsForBespokeReports(t *testing.T) {
	res := &cubapi.Result{Outcomes: []cubapi.UnitOutcome{
		{UnitID: "u-web", SpaceSlug: "prod", UnitSlug: "web", Success: true, HasMutations: true, Diff: replicasDiff("1", "3")},
		{UnitID: "u-api", SpaceSlug: "prod", UnitSlug: "api", Success: true},
	}}
	diffs := Diffs(res)
	if len(diffs) != 1 || diffs[0].Space != "prod" || diffs[0].Unit != "web" {
		t.Fatalf("diffs = %+v, want only web's", diffs)
	}
	var out bytes.Buffer
	WriteUnitDiffs(&out, "fix", diffs)
	if !strings.Contains(out.String(), "Changes to unit prod/web from fix:") {
		t.Errorf("output = %q", out.String())
	}
}

// The table is followed by each changed Unit's diff, uncolored when not written
// to a terminal.
func TestReportMutationsShowsDiffs(t *testing.T) {
	res := &cubapi.Result{DryRun: true, Outcomes: []cubapi.UnitOutcome{
		{UnitID: "u-web", SpaceSlug: "prod", UnitSlug: "web", Success: true, HasMutations: true, Diff: replicasDiff("1", "3")},
	}}
	var out bytes.Buffer
	cmd := &cobra.Command{}
	cmd.SetOut(&out)
	if err := ReportMutations(cmd, "scale", "prod", true, "table", res); err != nil {
		t.Fatal(err)
	}
	got := out.String()
	for _, want := range []string{
		"scale: 1 of 1 Unit(s) would change",
		"Changes to unit prod/web from scale:\nResource: apps/v1/Deployment prod/web\n  ~ [Update] spec.replicas\n      1 → 3\n",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("output lacks %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "\033[") {
		t.Errorf("output to a buffer is colored:\n%s", got)
	}
}

// A command that runs several functions over the same Unit shows one row,
// mutated if any function changed it.
func TestSummarizeAggregatesPerUnit(t *testing.T) {
	res := &cubapi.Result{Outcomes: []cubapi.UnitOutcome{
		{UnitID: "u-web", SpaceSlug: "prod", UnitSlug: "web", Success: true, HasMutations: false},
		{UnitID: "u-web", SpaceSlug: "prod", UnitSlug: "web", Success: true, HasMutations: true},
		{UnitID: "u-api", SpaceSlug: "prod", UnitSlug: "api", Success: true, HasMutations: true},
	}}
	rep := Summarize("bulk", "prod", false, res)
	if len(rep.Outcomes) != 2 {
		t.Fatalf("outcomes = %+v, want one row per Unit", rep.Outcomes)
	}
	// Sorted by Space then Unit, so api leads.
	if rep.Outcomes[0].Unit != "api" || rep.Outcomes[1].Unit != "web" {
		t.Errorf("not sorted: %+v", rep.Outcomes)
	}
	if !rep.Outcomes[1].Mutated {
		t.Error("web mutated by one of two functions should count as mutated")
	}
	if rep.Mutated != 2 || rep.Committed != 2 {
		t.Errorf("mutated=%d committed=%d, want 2/2", rep.Mutated, rep.Committed)
	}
}

// A slug is unique only within a Space, and a bulk edit spans Spaces. Keying the
// aggregation by slug folded every Unit sharing a name into one row, so the
// count an operator read off a dry run was the number of distinct names rather
// than the number of Units about to change.
func TestSummarizeKeepsSameSlugInDifferentSpaces(t *testing.T) {
	res := &cubapi.Result{Outcomes: []cubapi.UnitOutcome{
		{UnitID: "u-1", SpaceSlug: "prod", UnitSlug: "app", Success: true, HasMutations: true},
		{UnitID: "u-2", SpaceSlug: "dev", UnitSlug: "app", Success: true, HasMutations: true},
		{UnitID: "u-3", SpaceSlug: "staging", UnitSlug: "app", Success: true, HasMutations: true},
	}}
	rep := Summarize("fleet-edit", "", false, res)
	if len(rep.Outcomes) != 3 {
		t.Fatalf("outcomes = %+v, want one row per Unit, not per slug", rep.Outcomes)
	}
	if rep.Mutated != 3 {
		t.Errorf("mutated = %d, want 3", rep.Mutated)
	}
	// Sorted by Space, so the rows are distinguishable in a table.
	for i, want := range []string{"dev", "prod", "staging"} {
		if rep.Outcomes[i].Space != want {
			t.Errorf("outcome %d space = %q, want %q", i, rep.Outcomes[i].Space, want)
		}
	}
}

// Committed follows the server's Success, which it reports separately from
// Error: a failed outcome that carried no message must not count as committed.
func TestSummarizeDoesNotCommitAFailure(t *testing.T) {
	res := &cubapi.Result{Outcomes: []cubapi.UnitOutcome{
		{UnitID: "u-web", SpaceSlug: "prod", UnitSlug: "web", Success: false, HasMutations: true},
	}}
	rep := Summarize("edit", "", false, res)
	if rep.Mutated != 1 {
		t.Errorf("mutated = %d, want 1", rep.Mutated)
	}
	if rep.Committed != 0 {
		t.Errorf("committed = %d, want 0 — the outcome did not succeed", rep.Committed)
	}
}

// On a dry run nothing is committed, however the outcomes read.
func TestSummarizeDryRunCommitsNothing(t *testing.T) {
	res := &cubapi.Result{Outcomes: []cubapi.UnitOutcome{
		{UnitID: "u-web", SpaceSlug: "prod", UnitSlug: "web", Success: true, HasMutations: true},
	}}
	rep := Summarize("edit", "", true, res)
	if !rep.DryRun || rep.Mutated != 1 || rep.Committed != 0 {
		t.Errorf("dry run = %+v", rep)
	}
}

func TestSummarizeTolerantOfNilResults(t *testing.T) {
	rep := Summarize("edit", "", false, nil, nil)
	if len(rep.Outcomes) != 0 || rep.Mutated != 0 {
		t.Errorf("nil results = %+v", rep)
	}
}
