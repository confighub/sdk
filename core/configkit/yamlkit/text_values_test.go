// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package yamlkit

import (
	"reflect"
	"strings"
	"testing"

	"github.com/confighub/sdk/core/third_party/gaby"
)

func TestTextAs(t *testing.T) {
	for _, tc := range []struct {
		text string
		want int
		ok   bool
	}{
		{"8080", 8080, true}, {"-12", -12, true}, {"0", 0, true},
		{"1.5", 0, false}, {"0x1F", 0, false}, {"", 0, false}, {" 1", 0, false}, {"1_000", 0, false},
	} {
		got, ok := TextAs[int](tc.text)
		if got != tc.want || ok != tc.ok {
			t.Errorf("TextAs[int](%q) = %v, %v; want %v, %v", tc.text, got, ok, tc.want, tc.ok)
		}
	}
	for _, tc := range []struct {
		text     string
		want, ok bool
	}{
		{"true", true, true}, {"false", false, true}, {"True", false, false}, {"yes", false, false}, {"1", false, false},
	} {
		got, ok := TextAs[bool](tc.text)
		if got != tc.want || ok != tc.ok {
			t.Errorf("TextAs[bool](%q) = %v, %v; want %v, %v", tc.text, got, ok, tc.want, tc.ok)
		}
	}
	if got, ok := TextAs[string]("02134"); got != "02134" || !ok {
		t.Errorf("TextAs[string] = %q, %v", got, ok)
	}
	if TextOf(5433) != "5433" || TextOf(false) != "false" || TextOf("x") != "x" {
		t.Errorf("TextOf = %q %q %q", TextOf(5433), TextOf(false), TextOf("x"))
	}
}

// Whatever string a format's value is, YAML has to give back the same text.
func TestStringValuesKeepTheirText(t *testing.T) {
	texts := []string{"true", "false", "0", "8080", "-12", "9223372036854775807", "1.5", "1.10", "02134",
		"-0", "+5", "1e5", "yes", "on", "null", "~", "", "2024-01-01", "1:20", "0x1F", "a: b", "- x", "#x",
		"*x", "&x", "!x", "@x", "`x", "'x'", "\"x\"", "x\ny", " x", "x ", "%x", "{x}", "[x]", ".inf", ".nan", "<<"}
	for _, text := range texts {
		doc := gaby.New()
		if err := SetPath(doc, text, "k"); err != nil {
			t.Fatal(err)
		}
		reparsed, err := gaby.ParseYAML(doc.Bytes())
		if err != nil {
			t.Fatalf("%q: %v\n%s", text, err, doc.Bytes())
		}
		entries, err := FlattenToText(reparsed.YNode())
		if err != nil {
			t.Fatal(err)
		}
		if len(entries) != 1 || entries[0].Text != text {
			t.Errorf("%q came back as %#v from YAML\n%s", text, entries, doc.Bytes())
		}
	}
}

func TestFlattenToText(t *testing.T) {
	doc, err := gaby.ParseYAML([]byte(`zeta: 1
alpha:
  $comment$head$b: a comment
  b: 1.10
  a: ~
list:
- x
- nested:
    deep: 0x1F
empty: {}
none: []
quoted: "02134"
when: 2024-01-01
base: &b
  x: 1
derived:
  <<: *b
  y: 2
alias: *b
`))
	if err != nil {
		t.Fatal(err)
	}
	entries, err := FlattenToText(doc.YNode())
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, e := range entries {
		got = append(got, strings.Join(e.Path, ".")+"="+e.Text)
	}
	want := []string{
		"zeta=1",
		"alpha.b=1.10",
		"alpha.a=",
		"list.0=x",
		"list.1.nested.deep=0x1F",
		"quoted=02134",
		"when=2024-01-01",
		"base.x=1",
		"derived.x=1",
		"derived.y=2",
		"alias.x=1",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}

	empty, err := gaby.ParseYAML([]byte(""))
	if err != nil {
		t.Fatal(err)
	}
	if entries, err := FlattenToText(empty.YNode()); err != nil || len(entries) != 0 {
		t.Errorf("FlattenToText of an empty document = %v, %v", entries, err)
	}
	scalar, err := gaby.ParseYAML([]byte("just text"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := FlattenToText(scalar.YNode()); err == nil {
		t.Error("FlattenToText of a scalar document succeeded")
	}
}
