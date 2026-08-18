// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"slices"
	"testing"
)

// contextListCell returns the cell under column for the row naming ctx.
func contextListCell(t *testing.T, header []string, rows [][]string, ctx, column string) string {
	t.Helper()
	col := slices.Index(header, column)
	if col < 0 {
		t.Fatalf("no %q column in header %q", column, header)
	}
	nameCol := slices.Index(header, "NAME")
	for _, row := range rows {
		if row[nameCol] == ctx {
			return row[col]
		}
	}
	t.Fatalf("no row for context %q in %q", ctx, rows)
	return ""
}

// TestContextListMarksEffectiveContext is the regression test for #4957. With
// an override in effect, the marker fell on the persisted context while every
// command ran against the override, so a listing consulted before a destructive
// operation said the opposite of the truth.
func TestContextListMarksEffectiveContext(t *testing.T) {
	cm := setupContextOverrideTest(t)
	globalContextFlag = "prod"
	if err := applyContextOverride(); err != nil {
		t.Fatal(err)
	}

	header, rows := contextListTable(cm.ListContexts())

	if got, want := contextListCell(t, header, rows, "prod", "SELECTED"), "* "+contextSourceFlag; got != want {
		t.Errorf("SELECTED for the overriding context = %q, want %q", got, want)
	}
	// The context config.yaml records is still named, unstarred: a reader has to
	// be able to tell an obeyed override from one that was outranked.
	if got, want := contextListCell(t, header, rows, "dev", "SELECTED"), "  "+contextSourceConfig; got != want {
		t.Errorf("SELECTED for the persisted context = %q, want %q", got, want)
	}
}

// TestContextListMarksEnvOverride covers $CUB_CONTEXT, the override a shell
// carries invisibly from command to command.
func TestContextListMarksEnvOverride(t *testing.T) {
	cm := setupContextOverrideTest(t)
	t.Setenv("CUB_CONTEXT", "prod")
	if err := applyContextOverride(); err != nil {
		t.Fatal(err)
	}

	header, rows := contextListTable(cm.ListContexts())

	if got, want := contextListCell(t, header, rows, "prod", "SELECTED"), "* "+contextSourceEnv; got != want {
		t.Errorf("SELECTED for the overriding context = %q, want %q", got, want)
	}
	if got, want := contextListCell(t, header, rows, "dev", "SELECTED"), "  "+contextSourceConfig; got != want {
		t.Errorf("SELECTED for the persisted context = %q, want %q", got, want)
	}
}

// TestContextListShowsOutrankedEnvOverride covers a $CUB_CONTEXT that lost to
// --context. All three sources asked for something; the listing has to show all
// three, or a shadowed environment variable stays invisible.
func TestContextListShowsOutrankedEnvOverride(t *testing.T) {
	cm := setupContextOverrideTest(t)
	addContext(t, cm, "staging", Coordinate{ServerURL: "https://staging.example.com"})
	t.Setenv("CUB_CONTEXT", "staging")
	globalContextFlag = "prod"
	if err := applyContextOverride(); err != nil {
		t.Fatal(err)
	}

	header, rows := contextListTable(cm.ListContexts())

	if got, want := contextListCell(t, header, rows, "prod", "SELECTED"), "* "+contextSourceFlag; got != want {
		t.Errorf("SELECTED for the flag's context = %q, want %q", got, want)
	}
	if got, want := contextListCell(t, header, rows, "staging", "SELECTED"), "  "+contextSourceEnv; got != want {
		t.Errorf("SELECTED for the outranked environment variable = %q, want %q", got, want)
	}
	if got, want := contextListCell(t, header, rows, "dev", "SELECTED"), "  "+contextSourceConfig; got != want {
		t.Errorf("SELECTED for the persisted context = %q, want %q", got, want)
	}
}

// TestContextListWithoutOverride covers the common case: the current context is
// starred, and the mark says it came from the config file.
func TestContextListWithoutOverride(t *testing.T) {
	cm := setupContextOverrideTest(t)
	if err := applyContextOverride(); err != nil {
		t.Fatal(err)
	}

	header, rows := contextListTable(cm.ListContexts())

	if got, want := contextListCell(t, header, rows, "dev", "SELECTED"), "* "+contextSourceConfig; got != want {
		t.Errorf("SELECTED for the current context = %q, want %q", got, want)
	}
	if got := contextListCell(t, header, rows, "prod", "SELECTED"); got != "" {
		t.Errorf("SELECTED for a context nothing asked for = %q, want it empty", got)
	}
}

// TestContextListOverrideOfCurrentContext covers an override naming the context
// that is already current: both sources asked for the same row, so both appear
// on it.
func TestContextListOverrideOfCurrentContext(t *testing.T) {
	cm := setupContextOverrideTest(t)
	globalContextFlag = "dev"
	if err := applyContextOverride(); err != nil {
		t.Fatal(err)
	}

	header, rows := contextListTable(cm.ListContexts())

	want := "* " + contextSourceFlag + ", " + contextSourceConfig
	if got := contextListCell(t, header, rows, "dev", "SELECTED"); got != want {
		t.Errorf("SELECTED for the active context = %q, want %q", got, want)
	}
	if got := contextListCell(t, header, rows, "prod", "SELECTED"); got != "" {
		t.Errorf("SELECTED for a context nothing asked for = %q, want it empty", got)
	}
}

// TestContextSelectionSummaryNamesOutrankedSources covers the "Selected By" row
// of `cub context get`: it names what won and what it beat, so someone who
// wonders why their $CUB_CONTEXT was ignored can see that it was.
func TestContextSelectionSummaryNamesOutrankedSources(t *testing.T) {
	setupContextOverrideTest(t)
	t.Setenv("CUB_CONTEXT", "prod")
	globalContextFlag = "prod"
	if err := applyContextOverride(); err != nil {
		t.Fatal(err)
	}

	want := `--context flag (CUB_CONTEXT selects "prod"; config.yaml records "dev")`
	if got := contextSelectionSummary(); got != want {
		t.Errorf("contextSelectionSummary() = %q, want %q", got, want)
	}
}

// TestContextSelectionSummaryWithoutOverride keeps the plain case readable: the
// config file selected the context and there is nothing it outranked.
func TestContextSelectionSummaryWithoutOverride(t *testing.T) {
	setupContextOverrideTest(t)
	if err := applyContextOverride(); err != nil {
		t.Fatal(err)
	}

	if got := contextSelectionSummary(); got != contextSourceConfig {
		t.Errorf("contextSelectionSummary() = %q, want %q", got, contextSourceConfig)
	}
}

// TestContextSelectorsKeepEnvAfterExport guards the recording order:
// applyContextOverride rewrites $CUB_CONTEXT for child processes, so selectors
// read from the environment afterwards would report the flag's context as the
// environment's and hide that the two disagreed.
func TestContextSelectorsKeepEnvAfterExport(t *testing.T) {
	cm := setupContextOverrideTest(t)
	addContext(t, cm, "staging", Coordinate{ServerURL: "https://staging.example.com"})
	t.Setenv("CUB_CONTEXT", "staging")
	globalContextFlag = "prod"
	if err := applyContextOverride(); err != nil {
		t.Fatal(err)
	}

	want := []contextSelector{
		{contextSourceFlag, "prod"},
		{contextSourceEnv, "staging"},
		{contextSourceConfig, "dev"},
	}
	if !slices.Equal(activeContextSelectors, want) {
		t.Errorf("activeContextSelectors = %v, want %v", activeContextSelectors, want)
	}
}
