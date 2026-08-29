// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package cliutil

import (
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func TestOutputFlagDefault(t *testing.T) {
	cmd := &cobra.Command{Use: "x"}
	var out string
	OutputFlag(cmd, &out, "")
	if got, _ := cmd.Flags().GetString("output"); got != "json" {
		t.Fatalf("default output = %q, want json", got)
	}
}

func TestQueryFlagsBind(t *testing.T) {
	cmd := &cobra.Command{Use: "x"}
	var q QueryFlags
	q.Bind(cmd)
	if err := cmd.Flags().Parse([]string{"--where", "A = 1", "--select", "Slug"}); err != nil {
		t.Fatalf("parse: %v", err)
	}
	if q.Where != "A = 1" || q.Select != "Slug" {
		t.Fatalf("query flags = %+v", q)
	}
}

func TestQueryFlagsPredicate(t *testing.T) {
	for _, tc := range []struct {
		name string
		q    QueryFlags
		want string
	}{
		{"empty", QueryFlags{}, ""},
		{"one label", QueryFlags{Component: "checkout"}, "Space.Labels.Component = 'checkout'"},
		{"raw only", QueryFlags{Where: "Slug LIKE 'x%'"}, "Slug LIKE 'x%'"},
		{
			"raw and labels are ANDed, raw first",
			QueryFlags{Where: "Slug LIKE 'x%'", Environment: "prod"},
			"Slug LIKE 'x%' AND Space.Labels.Environment = 'prod'",
		},
		{
			"every label, in a stable order",
			QueryFlags{Component: "c", Environment: "e", Region: "r", Owner: "o", Layer: "l", Variant: "v"},
			"Space.Labels.Component = 'c' AND Space.Labels.Environment = 'e' AND " +
				"Space.Labels.Region = 'r' AND Space.Labels.Owner = 'o' AND " +
				"Space.Labels.Layer = 'l' AND Space.Labels.Variant = 'v'",
		},
		// Filter, Contains and Select are query parameters, not clauses.
		{"stored-query fields are not clauses", QueryFlags{Filter: "ops/f", Contains: "redis", Select: "Slug"}, ""},
	} {
		got, err := tc.q.Predicate()
		if err != nil {
			t.Errorf("%s: %v", tc.name, err)
			continue
		}
		if got != tc.want {
			t.Errorf("%s:\n got %q\nwant %q", tc.name, got, tc.want)
		}
	}
}

// A filter string literal has no escape sequence, so a value carrying a quote or
// a backslash is rejected rather than rewritten into something that would parse
// as a different value. The error names the flag the operator typed.
func TestQueryFlagsPredicateRejectsUnsendableValue(t *testing.T) {
	if _, err := (QueryFlags{Owner: "o'brien"}).Predicate(); err == nil {
		t.Error("a quoted owner was accepted")
	} else if !strings.Contains(err.Error(), "--owner") {
		t.Errorf("error does not name the flag: %v", err)
	}
	if _, err := (QueryFlags{Component: `back\slash`}).Predicate(); err == nil {
		t.Error("a backslashed component was accepted")
	}
}

// A command-specific label scope joins the predicate like any other, so a
// command can scope by a label the others have no use for without the flag
// spreading to all of them.
func TestQueryFlagsLabel(t *testing.T) {
	cmd := &cobra.Command{Use: "x"}
	var q QueryFlags
	q.Label("cluster", "Cluster", "select Units whose Space has Labels.Cluster = <value>")
	q.BindSpaceLabels(cmd)

	if got, err := q.Predicate(); err != nil || got != "" {
		t.Errorf("unset extra label contributed %q, %v", got, err)
	}
	if err := cmd.Flags().Parse([]string{"--cluster", "prod-use1", "--environment", "prod"}); err != nil {
		t.Fatalf("parse: %v", err)
	}
	want := "Space.Labels.Environment = 'prod' AND Space.Labels.Cluster = 'prod-use1'"
	got, err := q.Predicate()
	if err != nil || got != want {
		t.Errorf("got %q, %v; want %q", got, err, want)
	}
}

// Each group is bindable on its own, so a command registers the flags it
// supports and no others: one whose query pins its own field selection must not
// offer --select.
func TestQueryFlagsBindGroups(t *testing.T) {
	cmd := &cobra.Command{Use: "x"}
	var q QueryFlags
	q.BindWhere(cmd)
	q.BindSpaceLabels(cmd)
	for _, name := range []string{"where", "component", "environment", "region", "owner", "layer", "variant"} {
		if cmd.Flags().Lookup(name) == nil {
			t.Errorf("--%s not registered", name)
		}
	}
	for _, name := range []string{"select", "filter", "contains"} {
		if cmd.Flags().Lookup(name) != nil {
			t.Errorf("--%s registered by a command that did not ask for it", name)
		}
	}
}

func TestProfilesSpaceFlag(t *testing.T) {
	cmd := &cobra.Command{Use: "x"}
	var space string
	ProfilesSpaceFlag(cmd, &space, "common")
	if got, _ := cmd.Flags().GetString("profiles-space"); got != "common" {
		t.Fatalf("default profiles-space = %q, want common", got)
	}
}

func TestCommitFlagsValidate(t *testing.T) {
	// Dry-run by default.
	desc, dryRun, err := (CommitFlags{}).Validate("scale checkout")
	if err != nil || !dryRun || desc != "" {
		t.Fatalf("dry-run: desc=%q dryRun=%v err=%v", desc, dryRun, err)
	}
	// Commit without description -> error.
	if _, _, err := (CommitFlags{Commit: true}).Validate("scale checkout"); err == nil {
		t.Fatal("commit without --change-desc = nil error, want error")
	}
	// Commit with description.
	desc, dryRun, err = (CommitFlags{Commit: true, ChangeDesc: "scale to 3"}).Validate("scale checkout")
	if err != nil || dryRun || desc != "scale to 3" {
		t.Fatalf("commit: desc=%q dryRun=%v err=%v", desc, dryRun, err)
	}
}
