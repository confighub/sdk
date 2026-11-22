// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package propkit

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/confighub/sdk/core/configkit/yamlkit"
	"github.com/confighub/sdk/core/function/api"
	"github.com/confighub/sdk/core/third_party/gaby"
	"sigs.k8s.io/kustomize/kyaml/yaml"
)

func roundTrip(t *testing.T, in string) (yamlText, out string) {
	t.Helper()
	rp := NewPropertiesResourceProvider()
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

// Properties are converted to YAML and back whenever a function writes a
// unit, so every value has to come back as the text it was.
func TestRoundTripIsExact(t *testing.T) {
	in := `# Application settings
app.name=billing
app.version=1.10
app.release=2.0
billing.default-zip=02134
billing.retries=3
billing.ratio=1.5
billing.negative=-0
billing.plus=+5
billing.exponent=1e5
billing.hex=0x1F
billing.big=12345678901234567890
billing.enabled=true
billing.Enabled=True
billing.answer=yes
billing.nothing=null
billing.tilde=~
billing.empty=
billing.date=2024-01-01
billing.url=http://example.com/a#b
billing.hash=a \#b
billing.colon=a: b
billing.equals=a=b
billing.quoted="x"
billing.path=C:\\temp\\
billing.tab=a\tb
billing.newline=line1\nline2
billing.formfeed=a\fb
billing.leading=\  x
billing.trailing=x\u0020\u0020
billing.unicode=é 😀
billing.control=\u0001
billing.list.0=a
billing.list.1=b
key\ with\ spaces=v
`
	yamlText, out := roundTrip(t, in)
	if out != in {
		t.Errorf("round trip changed the properties\nwant:\n%s\ngot:\n%s\nYAML:\n%s", in, out, yamlText)
	}
}

// The format has no types, so every value is a string, which YAML quotes
// when it would otherwise read it as another type.
func TestNativeToYAMLValuesAreStrings(t *testing.T) {
	y, _ := roundTrip(t, "version=1.10\nzip=02134\nport=8080\nratio=1.5\nenabled=true\nneg=-0\nplus=+5\nexp=1e5\nbig=12345678901234567890\nyes=yes\nempty=\n")
	doc, err := gaby.ParseYAML([]byte(y))
	if err != nil {
		t.Fatal(err)
	}
	content := doc.YNode().Content
	for i := 0; i+1 < len(content); i += 2 {
		if got := content[i+1].ShortTag(); got != yaml.NodeTagString {
			t.Errorf("%s: YAML tag %s, want !!str\n%s", content[i].Value, got, y)
		}
	}
}

// Values a function sets, and YAML forms properties don't have, are written
// as their YAML text; null is written as an empty value.
func TestYAMLToNativeOfYAMLForms(t *testing.T) {
	out, err := NewPropertiesResourceProvider().YAMLToNative([]byte("a: ~\nb: 1.0\nc: 0x1F\nd: 2024-01-01\ne: \"02134\"\nf:\n  g: [x, y]\nbase: &b {x: 1}\nderived:\n  <<: *b\n  y: 2\n"))
	if err != nil {
		t.Fatal(err)
	}
	want := "a=\nb=1.0\nc=0x1F\nd=2024-01-01\ne=02134\nf.g.0=x\nf.g.1=y\nbase.x=1\nderived.x=1\nderived.y=2\n"
	if string(out) != want {
		t.Errorf("got\n%s\nwant\n%s", out, want)
	}
}

func TestContinuationLines(t *testing.T) {
	y, err := NewPropertiesResourceProvider().NativeToYAML([]byte("a=one \\\n    two\nb=ends with backslash\\\\\nc=3\n"))
	if err != nil {
		t.Fatal(err)
	}
	doc, err := gaby.ParseYAML(y)
	if err != nil {
		t.Fatal(err)
	}
	for path, want := range map[string]any{"a": "one two", "b": `ends with backslash\`, "c": "3"} {
		got := doc.Path(path).Data()
		if got != want {
			t.Errorf("%s = %#v, want %#v\n%s", path, got, want, y)
		}
	}
}

// FuzzValueRoundTrip checks that any string value a function sets survives
// being written as properties and read back.
func FuzzValueRoundTrip(f *testing.F) {
	for _, seed := range []string{
		"", "1.10", "02134", " leading", "trailing ", "  ", "a #b", "a\t#b", "#start", "!bang",
		`back\slash`, `ends\`, "line1\nline2", "\r\n", "\f\b\x00\x7f", "é 😀", "\U000E0001", "\uFFFE",
		`"quoted"`, "'single'", "a=b:c", "\\u0041", "x \\", "\t", "${var}", " ", "\t\n", "\tx\ny",
	} {
		f.Add(seed)
	}
	rp := NewPropertiesResourceProvider()
	f.Fuzz(func(t *testing.T, value string) {
		if !utf8.ValidString(value) {
			return
		}
		doc := gaby.New()
		if err := yamlkit.SetPath(doc, value, "app", "value"); err != nil {
			t.Fatal(err)
		}
		props, err := rp.YAMLToNative(doc.Bytes())
		if err != nil {
			t.Fatalf("YAMLToNative of %q: %v", value, err)
		}
		y, err := rp.NativeToYAML(props)
		if err != nil {
			t.Fatalf("NativeToYAML of %q: %v", props, err)
		}
		reparsed, err := gaby.ParseYAML(y)
		if err != nil {
			t.Fatal(err)
		}
		entries, err := yamlkit.FlattenToText(reparsed.YNode())
		if err != nil {
			t.Fatal(err)
		}
		if len(entries) != 1 || strings.Join(entries[0].Path, ".") != "app.value" || entries[0].Text != value {
			t.Fatalf("%q came back as %#v\nproperties: %q\nYAML:\n%s", value, entries, props, y)
		}
	})
}

// FuzzRoundTrip checks that, for any properties text the parser accepts,
// one round trip reaches a fixed point that keeps every value.
func FuzzRoundTrip(f *testing.F) {
	for _, seed := range []string{
		"a=1\nb.c=1.10\n",
		"# comment\na=1 # inline\n! bang comment\nb:2\n",
		"a=one \\\n  two\n",
		"k\\=ey=v\\:al\n",
		"a.0=x\na.1=y\n",
		"a=\\u00e9\\uD83D\\uDE00\n",
	} {
		f.Add(seed)
	}
	rp := NewPropertiesResourceProvider()
	f.Fuzz(func(t *testing.T, in string) {
		if !utf8.ValidString(in) {
			return
		}
		y1, err := rp.NativeToYAML([]byte(in))
		if err != nil {
			return
		}
		out1, err := rp.YAMLToNative(y1)
		if err != nil {
			// Keys that make one path both a value and a map, such as a=1
			// and a.b=2, can't be written back.
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

// Functions that get and set ints and bools read and write a property's text,
// and the YAML stays all strings.
func TestTypedFunctionsReadAndWriteText(t *testing.T) {
	rp := NewPropertiesResourceProvider()
	y, err := rp.NativeToYAML([]byte("configHub.configSchema=SimpleApp\ndatabase.port=5432\ndatabase.ssl=true\ndatabase.version=1.10\n"))
	if err != nil {
		t.Fatal(err)
	}
	doc, err := gaby.ParseYAML(y)
	if err != nil {
		t.Fatal(err)
	}
	docs := gaby.Container{doc}
	paths := func(path string) api.ResourceTypeToPathToVisitorInfoType {
		return yamlkit.GetVisitorMapForPath(rp, "SimpleApp", api.UnresolvedPath(path))
	}

	ports, err := yamlkit.GetPaths[int](docs, paths("database.port"), nil, rp, nil)
	if err != nil || len(ports) != 1 || ports[0].Value != 5432 || ports[0].DataType != api.DataTypeInt {
		t.Errorf("GetPaths[int] = %+v, %v", ports, err)
	}
	ssl, err := yamlkit.GetPaths[bool](docs, paths("database.ssl"), nil, rp, nil)
	if err != nil || len(ssl) != 1 || ssl[0].Value != true {
		t.Errorf("GetPaths[bool] = %+v, %v", ssl, err)
	}
	// Text that isn't an int isn't one.
	versions, err := yamlkit.GetPaths[int](docs, paths("database.version"), nil, rp, nil)
	if err != nil || len(versions) != 0 {
		t.Errorf("GetPaths[int] of 1.10 = %+v, %v", versions, err)
	}

	if err := yamlkit.UpdatePathsValue[int](docs, paths("database.port"), nil, rp, 5433, false, nil); err != nil {
		t.Fatal(err)
	}
	if err := yamlkit.UpdatePathsValue[bool](docs, paths("database.ssl"), nil, rp, false, false, nil); err != nil {
		t.Fatal(err)
	}
	for path, want := range map[string]string{"database.port": "5433", "database.ssl": "false"} {
		if got := doc.Path(path).Data(); got != want {
			t.Errorf("%s = %#v, want the string %q", path, got, want)
		}
	}
	out, err := rp.YAMLToNative(doc.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	if want := "configHub.configSchema=SimpleApp\ndatabase.port=5433\ndatabase.ssl=false\ndatabase.version=1.10\n"; string(out) != want {
		t.Errorf("got\n%s\nwant\n%s", out, want)
	}
}
