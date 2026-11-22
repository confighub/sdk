// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package jsonkit

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/confighub/sdk/core/configkit/yamlkit"
	"github.com/confighub/sdk/core/third_party/gaby"
)

func roundTrip(t *testing.T, in string) (yamlText, out string) {
	t.Helper()
	rp := NewJSONResourceProvider()
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

// A unit's JSON goes through NativeToYAML and YAMLToNative whenever a
// function writes it, so the two have to give back what they were given.
func TestRoundTripIsExact(t *testing.T) {
	cases := map[string]string{
		"application config": `{
  "configHub": {
    "configSchema": "registrybot.confighub.com/v1alpha3",
    "configName": "registry"
  },
  "server": {
    "port": 8080,
    "host": "0.0.0.0",
    "timeoutSeconds": 30.5,
    "tls": false
  },
  "version": "1.10",
  "zip": "02134",
  "ratio": 1.10,
  "maxBytes": 12345678901234567890,
  "int64max": 9223372036854775807,
  "negativeZero": -0,
  "exponent": 1e5,
  "features": [
    "a",
    "b"
  ],
  "limits": {},
  "tags": [],
  "owner": null,
  "message": "line1\nline2",
  "unicode": "é 😀",
  "html": "<b>&amp;</b>"
}
`,
		"top-level array": `[
  {
    "name": "b"
  },
  {
    "name": "a"
  }
]
`,
		"top-level scalar": "\"text\"\n",
	}
	for name, in := range cases {
		t.Run(name, func(t *testing.T) {
			yamlText, out := roundTrip(t, in)
			if out != in {
				t.Errorf("round trip changed the JSON\nwant:\n%s\ngot:\n%s\nYAML:\n%s", in, out, yamlText)
			}
		})
	}
}

func TestNativeToYAMLKeepsOrderAndText(t *testing.T) {
	yamlText, _ := roundTrip(t, `{"zeta": 1, "alpha": {"b": 2, "a": 1}, "ver": 1.10, "big": 12345678901234567890, "neg0": -0, "on": "on"}`)
	want := `zeta: 1
alpha:
  b: 2
  a: 1
ver: 1.10
big: 12345678901234567890
neg0: -0
"on": "on"
`
	if yamlText != want {
		t.Errorf("got\n%s\nwant\n%s", yamlText, want)
	}
}

// A function edits the YAML; the JSON written back keeps the order of the
// keys and the text of the values the function didn't touch.
func TestYAMLToNativeAfterAnEdit(t *testing.T) {
	rp := NewJSONResourceProvider()
	y, err := rp.NativeToYAML([]byte(`{"zeta": 1.10, "alpha": {"b": 2, "a": 1}, "port": 80}`))
	if err != nil {
		t.Fatal(err)
	}
	doc, err := gaby.ParseYAML(y)
	if err != nil {
		t.Fatal(err)
	}
	if err := yamlkit.SetPath(doc, 8080, "port"); err != nil {
		t.Fatal(err)
	}
	if err := yamlkit.SetPath(doc, "new", "alpha", "c"); err != nil {
		t.Fatal(err)
	}
	out, err := rp.YAMLToNative(doc.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	want := `{
  "zeta": 1.10,
  "alpha": {
    "b": 2,
    "a": 1,
    "c": "new"
  },
  "port": 8080
}
`
	if string(out) != want {
		t.Errorf("got\n%s\nwant\n%s", out, want)
	}
}

func TestYAMLToNativeOfYAMLForms(t *testing.T) {
	rp := NewJSONResourceProvider()
	out, err := rp.YAMLToNative([]byte("hex: 0x1F\nunderscore: 1_000\nhalf: .5\nnothing: ~\nyes: yes\nwhen: 2024-01-01\nbase: &b {x: 1}\nderived:\n  <<: *b\n  y: 2\n"))
	if err != nil {
		t.Fatal(err)
	}
	want := `{
  "hex": 31,
  "underscore": 1000,
  "half": 0.5,
  "nothing": null,
  "yes": "yes",
  "when": "2024-01-01",
  "base": {
    "x": 1
  },
  "derived": {
    "x": 1,
    "y": 2
  }
}
`
	if string(out) != want {
		t.Errorf("got\n%s\nwant\n%s", out, want)
	}
	if _, err := rp.YAMLToNative([]byte("a: .inf\n")); err == nil {
		t.Error("YAMLToNative wrote infinity as JSON")
	}
}

func TestEmptyAndInvalidJSON(t *testing.T) {
	rp := NewJSONResourceProvider()
	for _, in := range []string{"", "  \n"} {
		y, err := rp.NativeToYAML([]byte(in))
		if err != nil || len(y) != 0 {
			t.Errorf("NativeToYAML(%q) = %q, %v; want empty", in, y, err)
		}
	}
	if out, err := rp.YAMLToNative(nil); err != nil || len(out) != 0 {
		t.Errorf("YAMLToNative(nil) = %q, %v; want empty", out, err)
	}
	for _, in := range []string{"a: 1", "{a: 1}", `{"a": 1,}`, `{"a": 1} {"b": 2}`, `{"a": 01}`} {
		if y, err := rp.NativeToYAML([]byte(in)); err == nil {
			t.Errorf("NativeToYAML(%q) accepted invalid JSON, giving\n%s", in, y)
		}
	}
}

func TestResourceNameAndType(t *testing.T) {
	rp := NewJSONResourceProvider()
	y, err := rp.NativeToYAML([]byte(`{"configHub": {"configSchema": "app/v1", "configName": "my-app"}, "a": 1}`))
	if err != nil {
		t.Fatal(err)
	}
	doc, err := gaby.ParseYAML(y)
	if err != nil {
		t.Fatal(err)
	}
	name, err := rp.ResourceNameGetter(doc)
	if err != nil || name != "my-app" {
		t.Errorf("ResourceNameGetter = %q, %v", name, err)
	}
	typ, err := rp.ResourceTypeGetter(doc)
	if err != nil || typ != "app/v1" {
		t.Errorf("ResourceTypeGetter = %q, %v", typ, err)
	}
}

// FuzzRoundTrip checks that converting valid JSON to YAML and back keeps its
// value, and that the output converts to itself.
func FuzzRoundTrip(f *testing.F) {
	for _, seed := range []string{
		`{"zeta": 1, "alpha": {"b": 2, "a": 1}, "ver": 1.10, "big": 12345678901234567890}`,
		`[1, "2", null, true, {"x": [{}]}]`,
		`{"yes": "yes", "date": "2024-01-01", "multi": "a\n b\n", "<<": {"<<": 1}}`,
		`"😀"`,
		`-0`,
	} {
		f.Add(seed)
	}
	rp := NewJSONResourceProvider()
	f.Fuzz(func(t *testing.T, in string) {
		if !json.Valid([]byte(in)) || strings.TrimSpace(in) == "" {
			return
		}
		y, err := rp.NativeToYAML([]byte(in))
		if err != nil {
			t.Fatalf("NativeToYAML(%q): %v", in, err)
		}
		out, err := rp.YAMLToNative(y)
		if err != nil {
			t.Fatalf("YAMLToNative of\n%s\n: %v", y, err)
		}
		var want, got any
		if err := json.Unmarshal([]byte(in), &want); err != nil {
			// A number too large for a float64.
			return
		}
		if err := json.Unmarshal(out, &got); err != nil {
			t.Fatalf("YAMLToNative wrote invalid JSON for %q: %s", in, out)
		}
		wantJSON, _ := json.Marshal(want)
		gotJSON, _ := json.Marshal(got)
		if string(wantJSON) != string(gotJSON) {
			t.Fatalf("round trip of %q changed the value to %s", in, out)
		}
		out2, err := rp.YAMLToNative(mustNativeToYAML(t, rp, out))
		if err != nil {
			t.Fatal(err)
		}
		if string(out2) != string(out) {
			t.Fatalf("second round trip of %q changed\n%s\nto\n%s", in, out, out2)
		}
	})
}

func mustNativeToYAML(t *testing.T, rp *JSONResourceProviderType, data []byte) []byte {
	t.Helper()
	y, err := rp.NativeToYAML(data)
	if err != nil {
		t.Fatalf("NativeToYAML(%q): %v", data, err)
	}
	return y
}
