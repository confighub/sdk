package gaby

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"testing"

	"sigs.k8s.io/kustomize/kyaml/yaml"
)

// jsonRoundTrip converts JSON to YAML text and back, as AppConfig/JSON units
// are converted around every function call.
func jsonRoundTrip(t *testing.T, in string) (yamlText, out string) {
	t.Helper()
	doc, err := ParseJSON([]byte(in))
	if err != nil {
		t.Fatalf("ParseJSON(%q): %v", in, err)
	}
	yamlText = doc.String()
	reparsed, err := ParseYAML([]byte(yamlText))
	if err != nil {
		t.Fatalf("ParseYAML of\n%s\n: %v", yamlText, err)
	}
	outBytes, err := NodeToJSON(reparsed.YNode(), "  ")
	if err != nil {
		t.Fatalf("NodeToJSON of\n%s\n: %v", yamlText, err)
	}
	return yamlText, string(outBytes)
}

func TestJSONRoundTripIsExact(t *testing.T) {
	// Each input is in the layout NodeToJSON writes, so the round trip has
	// to give it back byte for byte.
	cases := map[string]string{
		"key order": `{
  "zeta": 1,
  "alpha": {
    "b": 2,
    "a": 1
  },
  "mid": [
    {
      "z": true,
      "y": false
    }
  ]
}`,
		"number text": `{
  "decimal": 1.10,
  "trailingZeros": 2.500,
  "big": 12345678901234567890,
  "int64max": 9223372036854775807,
  "int64min": -9223372036854775808,
  "beyond64": 123456789012345678901234567890,
  "negativeZero": -0,
  "negativeZeroFloat": -0.0,
  "exponent": 1e5,
  "upperExponent": 1E+5,
  "negativeExponent": 2.5e-3,
  "huge": 1e400,
  "tiny": 4.9e-324,
  "zero": 0
}`,
		"strings": `{
  "plain": "hello",
  "empty": "",
  "yamlBool": "true",
  "yaml11Bool": "yes",
  "yaml11Off": "off",
  "yamlNull": "null",
  "tilde": "~",
  "number": "1.10",
  "leadingZero": "02134",
  "date": "2024-01-01",
  "base60": "1:20",
  "colon": "a: b",
  "hash": "a #b",
  "leadingSpace": " x",
  "trailingSpace": "x ",
  "multiLine": "line1\nline2\n",
  "crlf": "a\r\nb",
  "tab": "a\tb",
  "tabFirstMultiLine": "\tx\ny",
  "quotes": "say \"hi\"",
  "backslash": "C:\\path",
  "controls": "\u0001\u001f",
  "unicode": "é 😀 中文",
  "html": "<a href=\"x\">&</a>",
  "separators": "\u2028\u2029",
  "indicator": "- item",
  "star": "*alias",
  "bang": "!tag",
  "brace": "{not: json}"
}`,
		"null and empty containers": `{
  "n": null,
  "emptyObject": {},
  "emptyArray": [],
  "nested": [
    [],
    {},
    [
      null
    ]
  ]
}`,
		"top-level array":  "[\n  1,\n  \"two\",\n  {\n    \"three\": 3\n  }\n]",
		"top-level string": `"just a string"`,
		"top-level number": `1.10`,
		"top-level null":   `null`,
		"top-level bool":   `false`,
		"empty object":     `{}`,
		"empty array":      `[]`,
		"yaml-like keys": `{
  "true": 1,
  "null": 2,
  "1.5": 3,
  "": 4,
  "a.b": 5,
  "<<": 6,
  "- x": 7
}`,
	}
	for name, in := range cases {
		t.Run(name, func(t *testing.T) {
			yamlText, out := jsonRoundTrip(t, in)
			if out != in {
				t.Errorf("round trip changed the JSON\nwant:\n%s\ngot:\n%s\nYAML:\n%s", in, out, yamlText)
			}
		})
	}
}

func TestParseJSONTypes(t *testing.T) {
	doc, err := ParseJSON([]byte(`{"s": "1.10", "i": 8080, "f": 1.10, "b": true, "n": null, "big": 12345678901234567890, "beyond": 123456789012345678901234567890, "yes": "yes", "date": "2024-01-01"}`))
	if err != nil {
		t.Fatal(err)
	}
	// Check the types a YAML reader sees in the YAML text, not in the
	// nodes ParseJSON built.
	reparsed, err := ParseYAML(doc.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{
		"s":      yaml.NodeTagString,
		"i":      yaml.NodeTagInt,
		"f":      yaml.NodeTagFloat,
		"b":      yaml.NodeTagBool,
		"n":      yaml.NodeTagNull,
		"big":    yaml.NodeTagInt,
		"beyond": yaml.NodeTagFloat,
		"yes":    yaml.NodeTagString,
		"date":   yaml.NodeTagString,
	}
	content := reparsed.YNode().Content
	for i := 0; i < len(content); i += 2 {
		key, value := content[i].Value, content[i+1]
		if got := value.ShortTag(); got != want[key] {
			t.Errorf("%s: YAML tag %s, want %s\nYAML:\n%s", key, got, want[key], doc.Bytes())
		}
	}
	// A string that YAML 1.1 reads as a bool is quoted, so that every YAML
	// reader sees a string.
	if !strings.Contains(doc.String(), `"yes": "yes"`) {
		t.Errorf("yes isn't quoted:\n%s", doc.String())
	}
}

func TestParseJSONAcceptsJSONOnly(t *testing.T) {
	valid := map[string]string{
		"surrogate pair":  `"\ud83d\ude00"`,
		"escaped slash":   `"a\/b"`,
		"tab whitespace":  "{\n\t\"a\": 1\n}",
		"surrounding ws":  " \n {\"a\": 1} \n ",
		"duplicate keys":  `{"a": 1, "a": 2}`,
		"unicode escapes": `"\u00e9\u4e2d"`,
	}
	for name, in := range valid {
		t.Run(name, func(t *testing.T) {
			if _, err := ParseJSON([]byte(in)); err != nil {
				t.Errorf("ParseJSON(%q): %v", in, err)
			}
		})
	}
	invalid := map[string]string{
		"empty":           ``,
		"whitespace":      "  \n",
		"yaml":            "a: 1",
		"unquoted key":    `{a: 1}`,
		"single quotes":   `{'a': 1}`,
		"trailing comma":  `{"a": 1,}`,
		"comment":         `{"a": 1} # c`,
		"two values":      `{"a": 1} {"b": 2}`,
		"leading zero":    `{"a": 01}`,
		"plus sign":       `{"a": +1}`,
		"hex":             `{"a": 0x1F}`,
		"NaN":             `{"a": NaN}`,
		"unterminated":    `{"a": 1`,
		"bad escape":      `"\x41"`,
		"control in text": "\"a\x01b\"",
	}
	for name, in := range invalid {
		t.Run(name, func(t *testing.T) {
			if _, err := ParseJSON([]byte(in)); err == nil {
				t.Errorf("ParseJSON(%q) succeeded", in)
			}
		})
	}
}

func TestSurrogatePairsAndEscapesAreDecoded(t *testing.T) {
	_, out := jsonRoundTrip(t, `{"emoji": "\ud83d\ude00", "slash": "a\/b", "e": "\u00e9", "html": "\u003cb\u003e"}`)
	want := "{\n  \"emoji\": \"😀\",\n  \"slash\": \"a/b\",\n  \"e\": \"é\",\n  \"html\": \"<b>\"\n}"
	if out != want {
		t.Errorf("got\n%s\nwant\n%s", out, want)
	}
}

func TestNodeToJSONFromYAML(t *testing.T) {
	cases := []struct {
		name, yaml, json string
	}{
		{"YAML integer forms", "hex: 0x1F\noctal: 0o17\nunderscore: 1_000\nplus: +5", `{"hex":31,"octal":15,"underscore":1000,"plus":5}`},
		{"YAML float forms", "a: .5\nb: 1.\nc: +1.5\nd: 1e5\ne: 1.0", `{"a":0.5,"b":1,"c":1.5,"d":1e5,"e":1.0}`},
		{"YAML bool forms", "a: True\nb: FALSE", `{"a":true,"b":false}`},
		{"YAML null forms", "a: ~\nb:\nc: Null", `{"a":null,"b":null,"c":null}`},
		{"timestamp", "a: 2024-01-01\nb: 2001-12-14t21:59:43.10-05:00", `{"a":"2024-01-01","b":"2001-12-14t21:59:43.10-05:00"}`},
		{"quoted number", `a: "1.10"`, `{"a":"1.10"}`},
		{"literal block", "a: |\n  x\n  y\n", `{"a":"x\ny\n"}`},
		{"non-string keys", "1: a\ntrue: b\n1.5: c", `{"1":"a","true":"b","1.5":"c"}`},
		{"alias", "base: &b\n  x: 1\ncopy: *b\nlist: [*b]", `{"base":{"x":1},"copy":{"x":1},"list":[{"x":1}]}`},
		{"merge key", "base: &b\n  x: 1\n  y: 2\nderived:\n  <<: *b\n  y: 3\n  z: 4", `{"base":{"x":1,"y":2},"derived":{"x":1,"y":3,"z":4}}`},
		{"merge list", "a: &a {x: 1}\nb: &b {x: 2, y: 2}\nc:\n  <<: [*a, *b]", `{"a":{"x":1},"b":{"x":2,"y":2},"c":{"x":1,"y":2}}`},
		{"quoted merge key is a key", `"<<": 1`, `{"<<":1}`},
		{"custom tag", "a: !foo bar", `{"a":"bar"}`},
		{"flow style", "a: {b: [1, 2]}", `{"a":{"b":[1,2]}}`},
		{"empty document", "", `null`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			doc, err := ParseYAML([]byte(tc.yaml))
			if err != nil {
				t.Fatal(err)
			}
			out, err := doc.MarshalJSON()
			if err != nil {
				t.Fatal(err)
			}
			if string(out) != tc.json {
				t.Errorf("got %s, want %s", out, tc.json)
			}
		})
	}
}

func TestNodeToJSONErrors(t *testing.T) {
	for _, in := range []string{"a: .inf", "a: -.inf", "a: .nan", "? [a]\n: 1", "a:\n  <<: 1"} {
		doc, err := ParseYAML([]byte(in))
		if err != nil {
			t.Fatal(err)
		}
		if out, err := doc.MarshalJSON(); err == nil {
			t.Errorf("MarshalJSON of %q succeeded with %s", in, out)
		}
	}
}

func TestIsJSONNumber(t *testing.T) {
	for _, s := range []string{"0", "-0", "1", "-1", "10", "1.0", "1.10", "0.5", "1e5", "1E5", "1e+5", "1e-5", "-1.5e-10", "12345678901234567890123"} {
		if !IsJSONNumber(s) {
			t.Errorf("IsJSONNumber(%q) = false", s)
		}
	}
	for _, s := range []string{"", "-", "+1", "01", "00", "1.", ".5", "1e", "1e+", "0x1F", "1_000", "inf", ".inf", "NaN", "1.5.5", " 1", "1 "} {
		if IsJSONNumber(s) {
			t.Errorf("IsJSONNumber(%q) = true", s)
		}
	}
}

// jsonTokens returns the token stream of a JSON value, with strings marked
// so that "1" and 1 differ. Two values with the same stream have the same
// members in the same order and the same number text.
func jsonTokens(data []byte) ([]string, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	var tokens []string
	for {
		tok, err := dec.Token()
		if err == io.EOF {
			return tokens, nil
		}
		if err != nil {
			return nil, err
		}
		tokens = append(tokens, fmt.Sprintf("%T:%v", tok, tok))
	}
}

// FuzzJSONRoundTrip checks that converting any valid JSON to YAML and back
// keeps every member in order and every number's text, that YAML readers see
// the JSON types, and that the result is a fixed point.
func FuzzJSONRoundTrip(f *testing.F) {
	for _, seed := range []string{
		`{"zeta": 1, "alpha": {"b": 2, "a": 1}}`,
		`{"ver": 1.10, "big": 12345678901234567890, "neg0": -0, "e": 1e5, "huge": 1e400}`,
		`[1, "two", null, true, {"x": []}]`,
		`"\ud83d\ude00 \u2028 <&>"`,
		`{"yes": "yes", "on": "on", "date": "2024-01-01", "num": "1.10", "t": "true", "n": "null", "b60": "1:20"}`,
		`{"multi": "a\nb\n", "lead": " x", "trail": "x ", "crlf": "a\r\n", "ctl": "\u0000\u007f"}`,
		`{"": "", "a.b": {"<<": 1}, "- x": "*y"}`,
		`{"a": 1, "a": 2}`,
		`-0.0`,
		`[[[[[]]]]]`,
	} {
		f.Add([]byte(seed))
	}
	f.Fuzz(func(t *testing.T, in []byte) {
		inTokens, err := jsonTokens(in)
		if err != nil || !json.Valid(in) {
			if _, parseErr := ParseJSON(in); parseErr == nil {
				t.Fatalf("ParseJSON accepted invalid JSON %q", in)
			}
			return
		}
		doc, err := ParseJSON(in)
		if err != nil {
			t.Fatalf("ParseJSON(%q): %v", in, err)
		}
		yamlText := doc.Bytes()
		reparsed, err := ParseYAML(yamlText)
		if err != nil {
			t.Fatalf("ParseYAML of the YAML of %q:\n%s\n%v", in, yamlText, err)
		}
		if err := checkJSONTypes(in, reparsed.YNode()); err != nil {
			t.Fatalf("YAML of %q:\n%s\n%v", in, yamlText, err)
		}
		out, err := NodeToJSON(reparsed.YNode(), "  ")
		if err != nil {
			t.Fatalf("NodeToJSON of the YAML of %q:\n%s\n%v", in, yamlText, err)
		}
		outTokens, err := jsonTokens(out)
		if err != nil {
			t.Fatalf("NodeToJSON wrote invalid JSON for %q: %s: %v", in, out, err)
		}
		if strings.Join(inTokens, "\x00") != strings.Join(outTokens, "\x00") {
			t.Fatalf("round trip of %q changed the JSON to %s", in, out)
		}
		doc2, err := ParseJSON(out)
		if err != nil {
			t.Fatal(err)
		}
		if string(doc2.Bytes()) != string(yamlText) {
			t.Fatalf("second conversion of %q changed the YAML\nfirst:\n%s\nsecond:\n%s", in, yamlText, doc2.Bytes())
		}
	})
}

// checkJSONTypes checks that each value in node, read from YAML text, has
// the YAML type of the corresponding JSON value.
func checkJSONTypes(in []byte, node *yaml.Node) error {
	dec := json.NewDecoder(bytes.NewReader(in))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		return err
	}
	return compareJSONTypes(v, node, "$")
}

func compareJSONTypes(v any, node *yaml.Node, path string) error {
	tag := node.ShortTag()
	switch v := v.(type) {
	case map[string]any:
		if node.Kind != yaml.MappingNode {
			return fmt.Errorf("%s: YAML kind %v for an object", path, node.Kind)
		}
		// Duplicate keys make the decoded map smaller than the mapping.
		for i := 0; i+1 < len(node.Content); i += 2 {
			key := node.Content[i]
			if key.ShortTag() != yaml.NodeTagString {
				return fmt.Errorf("%s: key %q has YAML tag %s", path, key.Value, key.ShortTag())
			}
			if _, ok := v[key.Value]; !ok {
				return fmt.Errorf("%s: YAML key %q isn't in the JSON", path, key.Value)
			}
		}
		for i := 0; i+1 < len(node.Content); i += 2 {
			key := node.Content[i].Value
			last := true
			for j := i + 2; j+1 < len(node.Content); j += 2 {
				if node.Content[j].Value == key {
					last = false
				}
			}
			if last {
				if err := compareJSONTypes(v[key], node.Content[i+1], path+"."+key); err != nil {
					return err
				}
			}
		}
	case []any:
		if node.Kind != yaml.SequenceNode || len(node.Content) != len(v) {
			return fmt.Errorf("%s: YAML kind %v with %d elements for an array of %d", path, node.Kind, len(node.Content), len(v))
		}
		for i := range v {
			if err := compareJSONTypes(v[i], node.Content[i], fmt.Sprintf("%s[%d]", path, i)); err != nil {
				return err
			}
		}
	case string:
		if tag != yaml.NodeTagString || node.Value != v {
			return fmt.Errorf("%s: YAML %s %q for the string %q", path, tag, node.Value, v)
		}
	case json.Number:
		if (tag != yaml.NodeTagInt && tag != yaml.NodeTagFloat) || node.Value != v.String() {
			return fmt.Errorf("%s: YAML %s %q for the number %s", path, tag, node.Value, v)
		}
	case bool:
		if tag != yaml.NodeTagBool {
			return fmt.Errorf("%s: YAML %s for a bool", path, tag)
		}
	case nil:
		if tag != yaml.NodeTagNull {
			return fmt.Errorf("%s: YAML %s for null", path, tag)
		}
	}
	return nil
}
