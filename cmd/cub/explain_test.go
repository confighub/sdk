// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"bytes"
	"io"
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/confighub/sdk/core/openapi"
)

func TestExplainFieldPaths(t *testing.T) {
	link, err := openapi.LookupSchema("Link")
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		path        string
		wantType    string
		wantElement string // the named type whose fields explain lists, if any
		wantErr     string
	}{
		{path: "ManualBindings", wantType: "[]Binding", wantElement: "Binding"},
		// A field whose $ref has a description of its own is written as an allOf of it.
		{path: "Bindings", wantType: "[]Binding", wantElement: "Binding"},
		{path: "ManualBindings.NeededResource", wantType: "ResourceInfo", wantElement: "ResourceInfo"},
		{path: "ManualBindings.NeededResource.ResourceType", wantType: "string"},
		{path: "UpstreamPaths", wantType: "[]NamedPath", wantElement: "NamedPath"},
		{path: "Permissions", wantType: "map[string]Subjects", wantElement: "Subjects"},
		{path: "Permissions.UserIDs", wantType: "map[string]boolean"},
		{path: "UpdateType", wantType: "string"},
		{path: "Labels", wantType: "map[string]string"},
		{path: "ManualBinding", wantErr: `Link has no field "ManualBinding"; did you mean "ManualBindings"?`},
		{path: "ManualBindings.AutoUpdate", wantErr: `Binding has no field "AutoUpdate"`},
		{path: "Labels.app", wantErr: `Labels holds map[string]string, which has no fields`},
	}
	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			field, err := lookupFieldPath("Link", link, tt.path)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("error %v, want it to contain %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got := schemaType(field); got != tt.wantType {
				t.Errorf("type %q, want %q", got, tt.wantType)
			}
			if name, _ := elementSchema(field); name != tt.wantElement {
				t.Errorf("element %q, want %q", name, tt.wantElement)
			}
		})
	}
}

func TestFirstSentence(t *testing.T) {
	tests := map[string]string{
		"One. Two.":                           "One",
		"Ends with a period.":                 "Ends with a period",
		"Names <metadata.namespace>/x. Then.": "Names <metadata.namespace>/x",
		"First line\nsecond line":             "First line",
		"First line.\nsecond line":            "First line",
		"No period":                           "No period",
		"Version 1.2.3 is the one. X":         "Version 1.2.3 is the one",
	}
	for in, want := range tests {
		if got := firstSentence(in); got != want {
			t.Errorf("firstSentence(%q) = %q, want %q", in, got, want)
		}
	}
}

// runExplainCommand runs `cub <entity> explain [field]` as registered, and returns what it printed.
func runExplainCommand(t *testing.T, entity string, args ...string) (string, error) {
	t.Helper()
	cmd, _, err := rootCmd.Find([]string{entity, "explain"})
	if err != nil || cmd.Name() != "explain" {
		t.Fatalf("cub %s explain: %v", entity, err)
	}
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	stdout := os.Stdout
	os.Stdout = w
	output := make(chan string)
	go func() {
		var b bytes.Buffer
		_, _ = io.Copy(&b, r)
		output <- b.String()
	}()
	runErr := cmd.RunE(cmd, args)
	os.Stdout = stdout
	_ = w.Close()
	return <-output, runErr
}

// hasRow reports whether output has a table row with these cells, in order, separated by
// the table's padding.
func hasRow(output string, cells ...string) bool {
	quoted := make([]string, len(cells))
	for i, cell := range cells {
		quoted[i] = regexp.QuoteMeta(cell)
	}
	return regexp.MustCompile(`(?m)^\s*` + strings.Join(quoted, `\s+`) + `(\s|$)`).MatchString(output)
}

func TestExplainCommandOutput(t *testing.T) {
	t.Run("summary", func(t *testing.T) {
		out, err := runExplainCommand(t, "link")
		if err != nil {
			t.Fatal(err)
		}
		for _, row := range [][]string{
			{"Bindings", "[]Binding", "The needs/provides attribute bindings resolution found"},
			{"ManualBindings", "[]Binding"},
			{"UpstreamPaths", "[]NamedPath", "Values to read from the upstream Unit"},
			{"Permissions", "map[string]Subjects"},
		} {
			if !hasRow(out, row...) {
				t.Errorf("no row %q in:\n%s", row, out)
			}
		}
	})

	t.Run("a list of objects lists the object's fields", func(t *testing.T) {
		out, err := runExplainCommand(t, "link", "ManualBindings")
		if err != nil {
			t.Fatal(err)
		}
		for _, row := range [][]string{
			{"Field", "ManualBindings"},
			{"Type", "[]Binding"},
			{"AttributeName", "string", "Shared attribute name"},
			{"NeededResource", "ResourceInfo"},
			{"NeededPath", "string", "Resolved path within the needed resource"},
		} {
			if !hasRow(out, row...) {
				t.Errorf("no row %q in:\n%s", row, out)
			}
		}
		if want := "Fields of Binding (explain one with `cub link explain ManualBindings.<field>`):"; !strings.Contains(out, want) {
			t.Errorf("no %q in:\n%s", want, out)
		}
	})

	t.Run("a path to a nested object", func(t *testing.T) {
		out, err := runExplainCommand(t, "link", "ManualBindings.NeededResource")
		if err != nil {
			t.Fatal(err)
		}
		if !hasRow(out, "Type", "ResourceInfo") || !hasRow(out, "ResourceType", "string") {
			t.Errorf("ResourceInfo and its fields missing from:\n%s", out)
		}
		// A description is cut at the end of its first sentence, not at the dot in a path.
		if !strings.Contains(out, "represented in the form <metadata.namespace>/<metadata.name>") {
			t.Errorf("ResourceName's description is cut short in:\n%s", out)
		}
		if want := "explain one with `cub link explain ManualBindings.NeededResource.<field>`"; !strings.Contains(out, want) {
			t.Errorf("no %q in:\n%s", want, out)
		}
	})

	t.Run("a field that holds a plain value lists nothing more", func(t *testing.T) {
		out, err := runExplainCommand(t, "link", "ManualBindings.NeededPath")
		if err != nil {
			t.Fatal(err)
		}
		if !hasRow(out, "Type", "string") || strings.Contains(out, "Fields") {
			t.Errorf("unexpected output:\n%s", out)
		}
	})

	t.Run("a misspelled field", func(t *testing.T) {
		_, err := runExplainCommand(t, "link", "ManualBindings.NeededResource.ResourceTyp")
		want := "ResourceInfo has no field \"ResourceTyp\"; did you mean \"ResourceType\"? (run `cub link explain` to list fields)"
		if err == nil || err.Error() != want {
			t.Errorf("error %v, want %q", err, want)
		}
	})
}
