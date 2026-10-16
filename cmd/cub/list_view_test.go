// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"os"
	"path/filepath"
	"regexp"
	"testing"

	"github.com/spf13/cobra"

	goclientnew "github.com/confighub/sdk/core/openapi/goclient-new"
)

var listViewForCall = regexp.MustCompile(`listViewFor\("([^"]+)"\)`)

// TestListViewForNamesItsOwnCommand pins each listViewFor path to a command that takes --view,
// and each command that takes --view to a listViewFor call. A path that matches no command is
// never the running one, so the list would not send the View, and a --view that no list sends
// would print a table of empty columns.
func TestListViewForNamesItsOwnCommand(t *testing.T) {
	files, err := filepath.Glob("*_list.go")
	if err != nil {
		t.Fatal(err)
	}
	sent := map[string]bool{}
	for _, file := range files {
		src, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		for _, m := range listViewForCall.FindAllStringSubmatch(string(src), -1) {
			sent[m[1]] = true
			cmd := findCommand(t, m[1][len("cub "):])
			if cmd.CommandPath() != m[1] {
				t.Errorf("%s: listViewFor(%q) resolves to %q", file, m[1], cmd.CommandPath())
			}
			if cmd.Flags().Lookup("view") == nil {
				t.Errorf("%s: %q has no --view flag", file, m[1])
			}
		}
	}
	if len(sent) < 25 {
		t.Fatalf("found %d listViewFor paths; the pattern no longer matches the source", len(sent))
	}

	var walk func(cmd *cobra.Command)
	walk = func(cmd *cobra.Command) {
		if cmd.Flags().Lookup("view") != nil && cmd.Name() == "list" && !sent[cmd.CommandPath()] {
			t.Errorf("%q takes --view but no list helper sends it", cmd.CommandPath())
		}
		for _, child := range cmd.Commands() {
			walk(child)
		}
	}
	walk(rootCmd)
}

func TestListViewForOnlyServesTheRunningCommand(t *testing.T) {
	defer func(v *goclientnew.View, p string) { listView, runningCommandPath = v, p }(listView, runningCommandPath)
	listView = &goclientnew.View{}
	listView.ViewID = goclientnew.UUID{1}
	runningCommandPath = "cub unit list"
	if got := listViewFor("cub space list"); got != "" {
		t.Errorf("the View of another command leaked into space list: %s", got)
	}
	if got := listViewFor("cub unit list"); got != listView.ViewID.String() {
		t.Errorf("listViewFor(running command) = %q", got)
	}
}

// TestViewTableRows covers the values and order of the rows a View's table prints.
func TestViewTableRows(t *testing.T) {
	entity := func(slug, replicas string, images ...string) *goclientnew.ExtendedUnit {
		columns := []goclientnew.ViewColumn{{Name: "Unit", Value: slug}, {Name: "Replicas", Value: replicas}}
		for _, image := range images {
			columns = append(columns, goclientnew.ViewColumn{Name: "Image", Value: image})
		}
		return &goclientnew.ExtendedUnit{ViewColumns: columns}
	}
	units := []*goclientnew.ExtendedUnit{entity("b", "10", "app:1"), entity("a", "9", "app:2", "sidecar:1"), entity("c", "10")}
	rows := make([]map[string]string, len(units))
	for i, unit := range units {
		rows[i] = viewColumnValues(unit)
	}
	if got := rows[1]["Image"]; got != "app:2, sidecar:1" {
		t.Errorf("a column with several values is joined: got %q", got)
	}

	view := &goclientnew.View{
		OrderBy:          "Replicas",
		OrderByDirection: "DESC",
		Columns: []goclientnew.Column{
			{Name: "Unit", OrderByDirection: "ASC"},
			{Name: "Replicas", DataType: "int"},
			{Name: "Image"},
		},
	}
	sortViewRows(rows, view)
	var order []string
	for _, row := range rows {
		order = append(order, row["Unit"])
	}
	// Replicas descending, numerically, then Unit ascending.
	if want := []string{"b", "c", "a"}; !equalStrings(order, want) {
		t.Errorf("order = %v, want %v", order, want)
	}

	if got := viewColumnValues(&goclientnew.CommandRun{}); got != nil {
		t.Errorf("an entity with no ViewColumns has no values: %v", got)
	}
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
