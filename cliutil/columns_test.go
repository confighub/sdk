// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package cliutil

import (
	"bytes"
	"strings"
	"testing"
)

// Envelope mirrors the ConfigHub Extended<Entity> shape: a named pointer to the
// core record plus related entities.
type ExtendedThing struct {
	Thing *Thing
	Space *Space
}

type Thing struct {
	Slug   string
	Count  int
	Labels map[string]string
}

type Space struct {
	Slug string
}

func TestGetValueDottedAndEnvelope(t *testing.T) {
	p := NewColumnProvider(new(ExtendedThing))
	obj := &ExtendedThing{
		Thing: &Thing{Slug: "checkout", Count: 5, Labels: map[string]string{"env": "prod"}},
		Space: &Space{Slug: "team-a"},
	}

	// Bare field falls back to the embedded Thing.
	if got := p.GetValue(obj, "Slug"); got != "checkout" {
		t.Fatalf("Slug = %q", got)
	}
	if got := p.GetValue(obj, "Count"); got != "5" {
		t.Fatalf("Count = %q", got)
	}
	// Nested envelope field.
	if got := p.GetValue(obj, "Space.Slug"); got != "team-a" {
		t.Fatalf("Space.Slug = %q", got)
	}
	// Labels.<key> resolves against the embedded entity's map.
	if got := p.GetValue(obj, "Labels.env"); got != "prod" {
		t.Fatalf("Labels.env = %q", got)
	}
	// Name aliases to Slug.
	if got := p.GetValue(obj, "Name"); got != "checkout" {
		t.Fatalf("Name = %q", got)
	}
	// Missing field.
	if got := p.GetValue(obj, "Nope"); got != "?" {
		t.Fatalf("Nope = %q, want ?", got)
	}
}

func TestColumnHeader(t *testing.T) {
	cases := map[string]string{
		"Slug":            "SLUG",
		"DisplayName":     "DISPLAY-NAME",
		"Space.Slug":      "SLUG",
		"Labels.env":      "LABEL:env",
		"Annotations.foo": "ANNOTATION:foo",
	}
	for in, want := range cases {
		if got := ColumnHeader(in); got != want {
			t.Fatalf("ColumnHeader(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestRenderTable(t *testing.T) {
	entities := []*ExtendedThing{
		{Thing: &Thing{Slug: "checkout", Count: 5}, Space: &Space{Slug: "team-a"}},
		{Thing: &Thing{Slug: "cart", Count: 2}, Space: &Space{Slug: "team-b"}},
	}
	var b bytes.Buffer
	if err := RenderTable(&b, entities, nil, []string{"Slug", "Count", "Space.Slug"}, TableOptions{}); err != nil {
		t.Fatalf("RenderTable: %v", err)
	}
	out := b.String()
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	if len(lines) != 3 {
		t.Fatalf("expected header + 2 rows, got %d lines:\n%s", len(lines), out)
	}
	if !strings.Contains(lines[0], "SLUG") || !strings.Contains(lines[0], "COUNT") {
		t.Fatalf("header = %q", lines[0])
	}
	if !strings.Contains(out, "checkout") || !strings.Contains(out, "team-b") {
		t.Fatalf("table body missing data:\n%s", out)
	}

	// NoHeader.
	var nb bytes.Buffer
	if err := RenderTable(&nb, entities, []string{"Slug"}, nil, TableOptions{NoHeader: true}); err != nil {
		t.Fatalf("RenderTable NoHeader: %v", err)
	}
	if strings.Contains(nb.String(), "SLUG") {
		t.Fatalf("NoHeader still wrote header: %q", nb.String())
	}
}
