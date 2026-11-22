// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package envkit

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/confighub/sdk/core/configkit/yamlkit"
	"github.com/confighub/sdk/core/third_party/gaby"
)

// Env files are converted to YAML and back whenever a function writes a
// unit, so every value has to come back as the text it was.
func TestRoundTripIsExact(t *testing.T) {
	in := `# Billing settings
VERSION=1.10
ZIP=02134
PORT=8080
RATIO=1.5
NEGATIVE=-0
HEX=0x1F
ENABLED=true
ANSWER=yes
NOTHING=null
EMPTY=
DATE=2024-01-01
URL="http://example.com/a#b"
SPACED="a b"
QUOTED="say \"hi\""
DOLLAR="\$HOME"
MULTI="line1\nline2"
UNICODE="é 😀"
configHub.configName=billing
`
	rp := NewEnvResourceProvider()
	y, err := rp.NativeToYAML([]byte(in))
	if err != nil {
		t.Fatal(err)
	}
	out, err := rp.YAMLToNative(y)
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != in {
		t.Errorf("round trip changed the env file\nwant:\n%s\ngot:\n%s\nYAML:\n%s", in, out, y)
	}
}

// Values a function sets, and YAML forms env files don't have, are written
// as their YAML text; null is written as an empty value.
func TestYAMLToNativeOfYAMLForms(t *testing.T) {
	out, err := NewEnvResourceProvider().YAMLToNative([]byte("A: ~\nB: 1.0\nC: 0x1F\nD: 1e21\nE: 007\nF: 2024-01-01\nG: true\nbase: &b x\nH: *b\n"))
	if err != nil {
		t.Fatal(err)
	}
	want := "A=\nB=1.0\nC=0x1F\nD=1e21\nE=007\nF=2024-01-01\nG=true\nbase=x\nH=x\n"
	if string(out) != want {
		t.Errorf("got\n%s\nwant\n%s", out, want)
	}
	for _, in := range []string{"A:\n  B: 1\n", "A.B: 1\n", "A: [1]\n"} {
		if out, err := NewEnvResourceProvider().YAMLToNative([]byte(in)); err == nil {
			t.Errorf("YAMLToNative(%q) wrote a key with a dot:\n%s", in, out)
		}
	}
}

// FuzzValueRoundTrip checks that any string value a function sets survives
// being written to an env file and read back.
func FuzzValueRoundTrip(f *testing.F) {
	for _, seed := range []string{
		"", "1.10", "02134", "\f", "\u00a0x\u00a0", "\x01", " leading", "trailing ", "a #b", "#start", `"quoted"`, "'single'",
		`back\slash`, `ends\`, "line1\nline2", "\r\n", "\t", "$HOME", "${HOME}", "`cmd`", "é 😀", "\tx\ny",
	} {
		f.Add(seed)
	}
	rp := NewEnvResourceProvider()
	f.Fuzz(func(t *testing.T, value string) {
		if !utf8.ValidString(value) {
			return
		}
		doc := gaby.New()
		if err := yamlkit.SetPath(doc, value, "KEY"); err != nil {
			t.Fatal(err)
		}
		out, err := rp.YAMLToNative(doc.Bytes())
		if err != nil {
			t.Fatalf("YAMLToNative of %q: %v", value, err)
		}
		y, err := rp.NativeToYAML(out)
		if err != nil {
			t.Fatalf("NativeToYAML of %q: %v", out, err)
		}
		reparsed, err := gaby.ParseYAML(y)
		if err != nil {
			t.Fatal(err)
		}
		entries, err := yamlkit.FlattenToText(reparsed.YNode())
		if err != nil {
			t.Fatal(err)
		}
		if len(entries) != 1 || strings.Join(entries[0].Path, ".") != "KEY" || entries[0].Text != value {
			t.Fatalf("%q came back as %#v\nenv: %q\nYAML:\n%s", value, entries, out, y)
		}
	})
}
