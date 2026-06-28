// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package cliutil

import (
	"bytes"
	"strings"
	"testing"
)

func TestParseOutput(t *testing.T) {
	tests := []struct {
		in      string
		kind    OutputKind
		arg     string
		wantErr bool
	}{
		{"", OutputDefault, "", false},
		{"json", OutputJSON, "", false},
		{"yaml", OutputYAML, "", false},
		{"name", OutputName, "", false},
		{"table", OutputTable, "", false},
		{"wide", OutputWide, "", false},
		{"jq=.[].Slug", OutputJQ, ".[].Slug", false},
		{"yq=.metadata.name", OutputYQ, ".metadata.name", false},
		{"custom-columns=A:.a", OutputCustomColumns, "A:.a", false},
		{"json=x", 0, "", true},   // json takes no arg
		{"jq", 0, "", true},       // jq requires arg
		{"bogus", 0, "", true},    // unknown
	}
	for _, tc := range tests {
		t.Run(tc.in, func(t *testing.T) {
			spec, err := ParseOutput(tc.in)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("ParseOutput(%q) = nil error, want error", tc.in)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseOutput(%q): %v", tc.in, err)
			}
			if spec.Kind != tc.kind || spec.Arg != tc.arg {
				t.Fatalf("ParseOutput(%q) = {%v,%q}, want {%v,%q}", tc.in, spec.Kind, spec.Arg, tc.kind, tc.arg)
			}
		})
	}
}

type sample struct {
	Slug string `json:"Slug" yaml:"Slug"`
	N    int    `json:"N" yaml:"N"`
}

func TestRenderJSONAndYAML(t *testing.T) {
	v := sample{Slug: "checkout", N: 3}

	var jb bytes.Buffer
	handled, err := OutputSpec{Kind: OutputJSON}.Render(&jb, v)
	if !handled || err != nil {
		t.Fatalf("json render handled=%v err=%v", handled, err)
	}
	if !strings.Contains(jb.String(), `"Slug": "checkout"`) {
		t.Fatalf("json = %s", jb.String())
	}

	var yb bytes.Buffer
	handled, err = OutputSpec{Kind: OutputYAML}.Render(&yb, v)
	if !handled || err != nil {
		t.Fatalf("yaml render handled=%v err=%v", handled, err)
	}
	if !strings.Contains(yb.String(), "Slug: checkout") {
		t.Fatalf("yaml = %s", yb.String())
	}
}

func TestRenderTableReturnsUnhandled(t *testing.T) {
	var b bytes.Buffer
	handled, err := OutputSpec{Kind: OutputTable}.Render(&b, sample{})
	if handled || err != nil {
		t.Fatalf("table render handled=%v err=%v, want false/nil", handled, err)
	}
	if b.Len() != 0 {
		t.Fatalf("table render wrote %q, want nothing", b.String())
	}
}

func TestRenderJQ(t *testing.T) {
	var b bytes.Buffer
	list := []sample{{Slug: "a"}, {Slug: "b"}}
	if err := RenderJQ(&b, list, ".[].Slug"); err != nil {
		t.Fatalf("RenderJQ: %v", err)
	}
	got := strings.Fields(b.String())
	if len(got) != 2 || got[0] != "a" || got[1] != "b" {
		t.Fatalf("jq output = %q", b.String())
	}
}

func TestRenderYQ(t *testing.T) {
	var b bytes.Buffer
	if err := RenderYQ(&b, sample{Slug: "checkout", N: 3}, ".Slug"); err != nil {
		t.Fatalf("RenderYQ: %v", err)
	}
	if strings.TrimSpace(b.String()) != "checkout" {
		t.Fatalf("yq output = %q", b.String())
	}
}
