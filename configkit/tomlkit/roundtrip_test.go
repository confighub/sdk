// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package tomlkit

import (
	"fmt"
	"math"
	"reflect"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/BurntSushi/toml"
	"github.com/confighub/sdk/core/configkit/yamlkit"
	"github.com/confighub/sdk/core/third_party/gaby"
	gotoml "github.com/pelletier/go-toml/v2"
	"sigs.k8s.io/kustomize/kyaml/yaml"
)

func roundTrip(t *testing.T, in string) (yamlText, out string) {
	t.Helper()
	rp := NewTOMLResourceProvider()
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

// TOML is converted to YAML and back whenever a function writes a unit, so
// keys have to keep their order and values their text. The input is in the
// layout YAMLToNative writes.
func TestRoundTripIsExact(t *testing.T) {
	in := `# The service
zeta = 1
alpha = "a" # the first letter
version = "1.10"
zip = "02134"
ratio = 1.10
hex = 0xFF
octal = 0o17
binary = 0b101
underscored = 1_000
float_underscored = 224_617.445_991
plus = +5
negative_zero = -0
exponent = 5e+22
whole_float = 3.0
infinity = inf
negative_infinity = -inf
not_a_number = nan
enabled = true
offset_datetime = 1979-05-27T07:32:00Z
offset_datetime_fraction = 1979-05-27T00:32:00.999999-07:00
offset_datetime_space = 1979-05-27 07:32:00Z
local_datetime = 1979-05-27T07:32:00
local_date = 1979-05-27
local_time = 07:32:00.5
yes = "yes"
empty = ""
escapes = "tab\tquote\"backslash\\bell\u0007"
unicode = "é 😀"
multi_line = """
line1
"quoted"
line2"""
ints = [3, 1, 2]
mixed = [1, "a", 1.5, true, 1979-05-27]
nested = [[1, 2], ["a"]]
inline_tables = [{ b = 1, a = 2 }, 3]
empty_array = []
"key with space" = 1
"" = "empty key"

# Servers
[servers]
region = "us"

[servers.beta]
ip = "10.0.0.2"

[servers.alpha]
ip = "10.0.0.1"

[empty_table]

# The first product
[[products]]
name = "Hammer"

[products.dimensions]
width = 1.10

# The second product
[[products]]
name = "Nail"

[[products]]
# Foot of the document
`
	yamlText, out := roundTrip(t, in)
	if out != in {
		t.Errorf("round trip changed the TOML\nwant:\n%s\ngot:\n%s\nYAML:\n%s", in, out, yamlText)
	}
}

// Comments go with the element of an array of tables they are in, including
// those of a table nested in an element.
func TestCommentsInArraysOfTables(t *testing.T) {
	in := `[[a]]
b = 1

# about c
[a.c]
# about d
d = 1 # about d's value

# the second element
[[a]]
# about b
b = 2
`
	yamlText, out := roundTrip(t, in)
	if out != in {
		t.Errorf("round trip changed the TOML\nwant:\n%s\ngot:\n%s\nYAML:\n%s", in, out, yamlText)
	}
}

func TestNativeToYAMLTypes(t *testing.T) {
	y, _ := roundTrip(t, `s = "1.10"
i = 0xFF
f = 1.10
inf = -inf
b = true
odt = 1979-05-27T07:32:00Z
odt_space = 1979-05-27 07:32:00Z
ldt = 1979-05-27T07:32:00
ld = 1979-05-27
lt = 07:32:00
yes = "yes"
`)
	doc, err := gaby.ParseYAML([]byte(y))
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{
		"s":         yaml.NodeTagString,
		"i":         yaml.NodeTagInt,
		"f":         yaml.NodeTagFloat,
		"inf":       yaml.NodeTagFloat,
		"b":         yaml.NodeTagBool,
		"odt":       timestampTag,
		"odt_space": TagDateTime,
		"ldt":       TagLocalDateTime,
		"ld":        timestampTag,
		"lt":        TagLocalTime,
		"yes":       yaml.NodeTagString,
	}
	content := doc.YNode().Content
	for i := 0; i+1 < len(content); i += 2 {
		if got := content[i+1].ShortTag(); got != want[content[i].Value] {
			t.Errorf("%s: YAML tag %s, want %s\n%s", content[i].Value, got, want[content[i].Value], y)
		}
	}
	// Functions read numbers as numbers, and date-times YAML has no type for
	// as strings.
	data, ok := doc.Data().(map[string]any)
	if !ok {
		t.Fatalf("Data() = %T", doc.Data())
	}
	for key, value := range map[string]any{"i": 255, "f": 1.1, "inf": math.Inf(-1), "lt": "07:32:00", "ldt": "1979-05-27T07:32:00"} {
		if !reflect.DeepEqual(data[key], value) {
			t.Errorf("%s reads as %#v, want %#v", key, data[key], value)
		}
	}
}

// Each [[array]] header adds one element, and date-times convert at all.
func TestArraysOfTablesAndDateTimes(t *testing.T) {
	y, _ := roundTrip(t, "when = 1979-05-27T07:32:00Z\n[[pts]]\nx = 1\n[[pts]]\nx = 2\n")
	want := "when: 1979-05-27T07:32:00Z\npts:\n- x: 1\n- x: 2\n"
	if y != want {
		t.Errorf("got\n%s\nwant\n%s", y, want)
	}
}

func TestYAMLToNativeOfYAMLForms(t *testing.T) {
	out, err := NewTOMLResourceProvider().YAMLToNative([]byte(`table:
  b: 1
zeta: 0x1F
alpha: 1_000
half: .5
one: 1.
whole: !!float 3
yaml_inf: .Inf
when: 2001-12-14t21:59:43.10-05:00
spaced: 2001-12-14 21:59:43.10
short_date: 2001-1-2
short_time: 2001-1-2T3:04:05Z
date: 2024-01-01
yes: yes
tilde_string: "~"
true_bool: True
base: &b {x: 1}
derived:
  <<: *b
  y: 2
list: [{a: 1}, {a: 2}]
`))
	if err != nil {
		t.Fatal(err)
	}
	want := `zeta = 0x1F
alpha = 1_000
half = 0.5
one = 1.0
whole = 3.0
yaml_inf = inf
when = 2001-12-14t21:59:43.10-05:00
spaced = 2001-12-14 21:59:43.10
short_date = 2001-01-02
short_time = 2001-01-02T03:04:05Z
date = 2024-01-01
yes = "yes"
tilde_string = "~"
true_bool = true

[table]
b = 1

[base]
x = 1

[derived]
x = 1
y = 2

[[list]]
a = 1

[[list]]
a = 2
`
	if string(out) != want {
		t.Errorf("got\n%s\nwant\n%s", out, want)
	}
}

func TestYAMLToNativeErrors(t *testing.T) {
	for _, in := range []string{
		"a: ~\n",
		"a:\n  b: null\n",
		"a: [1, ~]\n",
		"- a\n",
		"a: !toml/local-time not-a-time\n",
		"a: 18446744073709551615\n",
	} {
		if out, err := NewTOMLResourceProvider().YAMLToNative([]byte(in)); err == nil {
			t.Errorf("YAMLToNative(%q) succeeded with\n%s", in, out)
		}
	}
}

func TestEmptyDocuments(t *testing.T) {
	rp := NewTOMLResourceProvider()
	for _, in := range []string{"", "# only a comment\n"} {
		y, err := rp.NativeToYAML([]byte(in))
		if err != nil {
			t.Fatalf("NativeToYAML(%q): %v", in, err)
		}
		if _, err := rp.YAMLToNative(y); err != nil {
			t.Fatalf("YAMLToNative of\n%s\n: %v", y, err)
		}
	}
	if out, err := rp.YAMLToNative([]byte("{}\n")); err != nil || len(out) != 0 {
		t.Errorf("YAMLToNative({}) = %q, %v", out, err)
	}
}

// FuzzRoundTrip checks that, for any valid TOML, converting it to YAML and
// back keeps its value, that YAML readers see its types, and that one round
// trip reaches a fixed point.
func FuzzRoundTrip(f *testing.F) {
	for _, seed := range []string{
		"a = 1\nb = 1.10\nc = 0xFF\n[t]\nd = 1979-05-27\n",
		"x.y.z = 1\n[x]\nw = 2\n",
		"[[a]]\nb = 1\n[a.c]\nd = 1\n[[a]]\nb = 2\n",
		"a = [{b = 1}, {b = 2}]\nc = [[1], []]\n",
		"t = {a.b = 1, c = {}}\n",
		"# head\nk = \"v\" # line\n# foot\n",
		"[[a]]\nb = 1\n# c\n[a.c]\n# d\nd = 1 # dl\n# e\n[[a]]\n# f\nb = 2\n",
		"s = '''\nraw\\n'''\nm = \"\"\"\\\n  folded \\\n  line\"\"\"\n",
		"lt = 07:32:00.999\nldt = 1979-05-27 07:32:00\nodt = 1979-05-27t07:32:00z\n",
		"inf = +inf\nnan = -nan\nz = -0.0\n",
		"\"quoted.key\" = 1\n'literal key' = 2\n",
		"x.A.0=0\n[x]\nA=0",
	} {
		f.Add(seed)
	}
	rp := NewTOMLResourceProvider()
	f.Fuzz(func(t *testing.T, in string) {
		if !utf8.ValidString(in) {
			return
		}
		var want any
		if _, err := toml.Decode(in, &want); err != nil {
			return
		}
		y1, err := rp.NativeToYAML([]byte(in))
		if err != nil {
			// BurntSushi accepts some invalid documents, which go-toml
			// rejects.
			var strict any
			if gotoml.Unmarshal([]byte(in), &strict) == nil {
				t.Fatalf("NativeToYAML(%q): %v", in, err)
			}
			return
		}
		doc, err := gaby.ParseYAML(y1)
		if err != nil {
			t.Fatalf("YAML of %q doesn't parse:\n%s\n%v", in, y1, err)
		}
		if err := compareTypes(want, doc.YNode(), "$"); err != nil {
			t.Fatalf("YAML of %q:\n%s\n%v", in, y1, err)
		}
		out1, err := rp.YAMLToNative(y1)
		if err != nil {
			t.Fatalf("YAMLToNative of the YAML of %q:\n%s\n%v", in, y1, err)
		}
		var got any
		if _, err := toml.Decode(string(out1), &got); err != nil {
			t.Fatalf("YAMLToNative wrote invalid TOML for %q:\n%s\n%v", in, out1, err)
		}
		if !sameTOML(want, got) {
			t.Fatalf("round trip of %q changed the value\nTOML:\n%s\nwant %#v\ngot  %#v", in, out1, want, got)
		}
		y2, err := rp.NativeToYAML(out1)
		if err != nil {
			t.Fatal(err)
		}
		out2, err := rp.YAMLToNative(y2)
		if err != nil {
			t.Fatal(err)
		}
		if string(out2) != string(out1) {
			t.Fatalf("second round trip of %q changed\n%s\nto\n%s", in, out1, out2)
		}
	})
}

// sameTOML compares decoded TOML, with NaN equal to itself and arrays of
// tables equal to arrays of inline tables, which decode to different types.
func sameTOML(a, b any) bool {
	return reflect.DeepEqual(normalize(a), normalize(b))
}

func normalize(v any) any {
	switch v := v.(type) {
	case map[string]any:
		m := make(map[string]any, len(v))
		for k, e := range v {
			m[k] = normalize(e)
		}
		return m
	case []map[string]any:
		s := make([]any, len(v))
		for i, e := range v {
			s[i] = normalize(e)
		}
		return s
	case []any:
		s := make([]any, len(v))
		for i, e := range v {
			s[i] = normalize(e)
		}
		return s
	case float64:
		if math.IsNaN(v) {
			return "NaN"
		}
		// -0.0 and 0.0 are the same TOML value written differently.
		if v == 0 {
			return 0.0
		}
		return v
	case time.Time:
		return fmt.Sprintf("%s %s", v.Format(time.RFC3339Nano), v.Location())
	}
	return v
}

// compareTypes checks that each value in node has the YAML type of the
// corresponding decoded TOML value.
func compareTypes(v any, node *yaml.Node, path string) error {
	tag := node.ShortTag()
	switch v := v.(type) {
	case map[string]any:
		if len(v) == 0 && tag == yaml.NodeTagNull {
			// An empty document.
			return nil
		}
		if node.Kind != yaml.MappingNode {
			return fmt.Errorf("%s: YAML kind %v for a table", path, node.Kind)
		}
		n := 0
		for i := 0; i+1 < len(node.Content); i += 2 {
			key := node.Content[i].Value
			if yamlkit.IsCommentKey(key) {
				continue
			}
			n++
			if node.Content[i].ShortTag() != yaml.NodeTagString {
				return fmt.Errorf("%s: key %q has YAML tag %s", path, key, node.Content[i].ShortTag())
			}
			value, ok := v[key]
			if !ok {
				return fmt.Errorf("%s: YAML key %q isn't in the TOML", path, key)
			}
			if err := compareTypes(value, node.Content[i+1], path+"."+key); err != nil {
				return err
			}
		}
		if n != len(v) {
			return fmt.Errorf("%s: %d YAML keys for %d TOML keys", path, n, len(v))
		}
	case []map[string]any:
		elements := make([]any, len(v))
		for i := range v {
			elements[i] = v[i]
		}
		return compareTypes(elements, node, path)
	case []any:
		if node.Kind != yaml.SequenceNode || len(node.Content) != len(v) {
			return fmt.Errorf("%s: YAML kind %v with %d elements for an array of %d", path, node.Kind, len(node.Content), len(v))
		}
		for i := range v {
			if err := compareTypes(v[i], node.Content[i], fmt.Sprintf("%s[%d]", path, i)); err != nil {
				return err
			}
		}
	case string:
		if tag != yaml.NodeTagString || node.Value != v {
			return fmt.Errorf("%s: YAML %s %q for the string %q", path, tag, node.Value, v)
		}
	case int64:
		var got int64
		if tag != yaml.NodeTagInt || node.Decode(&got) != nil || got != v {
			return fmt.Errorf("%s: YAML %s %q for the integer %d", path, tag, node.Value, v)
		}
	case float64:
		var got float64
		if tag != yaml.NodeTagFloat || node.Decode(&got) != nil || !(got == v || (math.IsNaN(got) && math.IsNaN(v))) {
			return fmt.Errorf("%s: YAML %s %q for the float %v", path, tag, node.Value, v)
		}
	case bool:
		if tag != yaml.NodeTagBool || node.Value != fmt.Sprint(v) {
			return fmt.Errorf("%s: YAML %s %q for the bool %v", path, tag, node.Value, v)
		}
	case time.Time:
		switch tag {
		case timestampTag, TagDateTime, TagLocalDateTime, TagLocalDate, TagLocalTime:
		default:
			return fmt.Errorf("%s: YAML %s %q for the date-time %v", path, tag, node.Value, v)
		}
	default:
		return fmt.Errorf("%s: unexpected TOML value %T", path, v)
	}
	return nil
}
