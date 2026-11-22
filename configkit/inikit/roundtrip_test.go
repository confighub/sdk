// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package inikit

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/confighub/sdk/core/configkit/yamlkit"
	"github.com/confighub/sdk/core/third_party/gaby"
	"sigs.k8s.io/kustomize/kyaml/yaml"
)

func roundTrip(t *testing.T, in string) (yamlText, out string) {
	t.Helper()
	rp := NewINIResourceProvider()
	y, err := rp.NativeToYAML([]byte(in))
	if err != nil {
		t.Fatalf("NativeToYAML(%q): %v", in, err)
	}
	o, err := rp.YAMLToNative(y)
	if err != nil {
		t.Fatalf("YAMLToNative of\n%s\n: %v", y, err)
	}
	return string(y), string(o)
}

// INI is converted to YAML and back whenever a function writes a unit, so
// every value has to come back as the text it was. The input is in the
// layout go-ini writes.
func TestRoundTripIsExact(t *testing.T) {
	in := `name = billing

# Billing settings
[billing]
version   = 1.10
release   = 2.0
zip       = 02134
retries   = 3
ratio     = 1.5
negative  = -0
plus      = +5
exponent  = 1e5
hex       = 0x1F
big       = 12345678901234567890
enabled   = true
Enabled   = True
answer    = yes
nothing   = null
empty     = ` + "\n" + `date      = 2024-01-01
url       = http://example.com/a
unicode   = é 😀
semicolon = ` + "`a ; b`" + `
hash      = ` + "`a # b`" + `
padded    = "  x  "

[billing.nested]
key = value
`
	yamlText, out := roundTrip(t, in)
	if out != in {
		t.Errorf("round trip changed the INI\nwant:\n%s\ngot:\n%s\nYAML:\n%s", in, out, yamlText)
	}
}

// The format has no types, so every value is a string, which YAML quotes
// when it would otherwise read it as another type.
func TestNativeToYAMLValuesAreStrings(t *testing.T) {
	y, _ := roundTrip(t, "[s]\nversion=1.10\nzip=02134\nport=8080\nratio=1.5\nenabled=true\nneg=-0\nplus=+5\nexp=1e5\nbig=12345678901234567890\nyes=yes\nempty=\n")
	doc, err := gaby.ParseYAML([]byte(y))
	if err != nil {
		t.Fatal(err)
	}
	content := doc.Path("s").YNode().Content
	for i := 0; i+1 < len(content); i += 2 {
		if got := content[i+1].ShortTag(); got != yaml.NodeTagString {
			t.Errorf("%s: YAML tag %s, want !!str\n%s", content[i].Value, got, y)
		}
	}
}

// Values a function sets, and YAML forms INI doesn't have, are written as
// their YAML text; null is written as an empty value.
func TestYAMLToNativeOfYAMLForms(t *testing.T) {
	out, err := NewINIResourceProvider().YAMLToNative([]byte("top: ~\ns:\n  b: 1.0\n  c: 0x1F\n  d: 2024-01-01\n  e: \"02134\"\nbase: &b {x: 1}\nderived:\n  <<: *b\n  y: 2\n"))
	if err != nil {
		t.Fatal(err)
	}
	want := "top = \n\n[s]\nb = 1.0\nc = 0x1F\nd = 2024-01-01\ne = 02134\n\n[base]\nx = 1\n\n[derived]\nx = 1\ny = 2\n"
	if string(out) != want {
		t.Errorf("got\n%s\nwant\n%s", out, want)
	}
}

func TestYAMLToNativeRejectsLists(t *testing.T) {
	rp := NewINIResourceProvider()
	if out, err := rp.YAMLToNative([]byte("s:\n  list: [a, b]\n")); err == nil {
		t.Errorf("YAMLToNative wrote a list as\n%s", out)
	}
	if out, err := rp.YAMLToNative([]byte("- a\n")); err == nil {
		t.Errorf("YAMLToNative wrote a sequence document as\n%s", out)
	}
}

// FuzzValueRoundTrip checks that string values a function sets survive
// being written as INI and read back.
func FuzzValueRoundTrip(f *testing.F) {
	for _, seed := range []string{
		"", "1.10", "02134", "a ; b", "a # b", "a;b", "a#b", " leading", "trailing ", `"quoted"`,
		"`backtick`", `back\slash`, "é 😀", "=", "[section]", "${var}", "%(interp)s",
	} {
		f.Add(seed)
	}
	rp := NewINIResourceProvider()
	f.Fuzz(func(t *testing.T, value string) {
		if !utf8.ValidString(value) || !iniCanHold(value) {
			return
		}
		doc := gaby.New()
		if err := yamlkit.SetPath(doc, value, "section", "key"); err != nil {
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
		if len(entries) != 1 || strings.Join(entries[0].Path, ".") != "section.key" || entries[0].Text != value {
			t.Fatalf("%q came back as %#v\nINI: %q\nYAML:\n%s", value, entries, out, y)
		}
	})
}

// iniCanHold reports whether go-ini can write a value so that it reads it
// back. go-ini has no escapes. It writes a value with a line break or a
// backtick between """, one with # or ; between backticks, one with
// surrounding whitespace between double quotes, and any other value as it
// is, and it reads a value that ends with a backslash as continued on the
// next line and removes the quotes around a value.
func iniCanHold(value string) bool {
	switch {
	case strings.ContainsAny(value, "\n\r"):
		return false
	case strings.Contains(value, "`"):
		return !strings.Contains(value, `"""`) && !strings.HasSuffix(value, `"`)
	case strings.ContainsAny(value, "#;"):
		return true
	case strings.TrimSpace(value) != value:
		// It removes the double quotes only if there are no others.
		return !strings.Contains(value, `"`)
	}
	quoted := len(value) >= 2 && (value[0] == '"' || value[0] == '\'') && value[len(value)-1] == value[0]
	return !quoted && !strings.HasSuffix(value, `\`) && !strings.HasPrefix(value, `"""`)
}

// FuzzRoundTrip checks that, for any INI text go-ini accepts, one round trip
// reaches a fixed point that keeps every value.
func FuzzRoundTrip(f *testing.F) {
	for _, seed := range []string{
		"a=1\n[s]\nb=1.10\n",
		"; comment\n[s]\n# another\nk = v ; inline\n",
		"[a.b]\nk=v\n[a]\nj=w\n",
		"k = \"quoted value\"\n",
	} {
		f.Add(seed)
	}
	rp := NewINIResourceProvider()
	f.Fuzz(func(t *testing.T, in string) {
		if !utf8.ValidString(in) {
			return
		}
		y1, err := rp.NativeToYAML([]byte(in))
		if err != nil {
			return
		}
		// go-ini names a key - #1, #2 and so on, which it then reads as
		// comments, and reads values it can't write.
		if strings.Contains(values(t, y1), "#") || !canHoldAll(t, y1) {
			return
		}
		out1, err := rp.YAMLToNative(y1)
		if err != nil {
			// A section and a key with one name, such as a key b in [a]
			// and a section [a.b], can't both be written back.
			return
		}
		y2, err := rp.NativeToYAML(out1)
		if err != nil {
			t.Fatalf("NativeToYAML of the round trip of %q:\n%s\n%v", in, out1, err)
		}
		out2, err := rp.YAMLToNative(y2)
		if err != nil {
			t.Fatal(err)
		}
		if string(out2) != string(out1) {
			t.Fatalf("second round trip of %q changed\n%s\nto\n%s", in, out1, out2)
		}
		if values(t, y1) != values(t, y2) {
			t.Fatalf("round trip of %q changed the values\nfirst:\n%s\nsecond:\n%s", in, y1, y2)
		}
	})
}

// canHoldAll reports whether go-ini can write every value of a YAML document.
func canHoldAll(t *testing.T, y []byte) bool {
	t.Helper()
	doc, err := gaby.ParseYAML(y)
	if err != nil {
		t.Fatal(err)
	}
	entries, err := yamlkit.FlattenToText(doc.YNode())
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if !iniCanHold(e.Text) {
			return false
		}
	}
	return true
}

// values returns the values of a YAML document, without its comments.
func values(t *testing.T, y []byte) string {
	t.Helper()
	doc, err := gaby.ParseYAML(y)
	if err != nil {
		t.Fatal(err)
	}
	entries, err := yamlkit.FlattenToText(doc.YNode())
	if err != nil {
		t.Fatal(err)
	}
	var b strings.Builder
	for _, e := range entries {
		b.WriteString(strings.Join(e.Path, ".") + "=" + e.Text + "\n")
	}
	return b.String()
}
