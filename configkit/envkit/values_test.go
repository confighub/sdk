// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package envkit

import (
	"strings"
	"testing"
)

func TestParseValue(t *testing.T) {
	tests := []struct {
		raw, value, comment string
	}{
		{`plain`, `plain`, ``},
		{`  padded  `, `padded`, ``},
		{``, ``, ``},
		{`a#b`, `a#b`, ``},
		{`5432 # port`, `5432`, `port`},
		{`"hello world"`, `hello world`, ``},
		{`"a # b"`, `a # b`, ``},
		{`"a # b" # note`, `a # b`, `note`},
		{`"x\"y"`, `x"y`, ``},
		{`"p\\q"`, `p\q`, ``},
		{`"line1\nline2\r"`, "line1\nline2\r", ``},
		{`"C:\temp"`, `C:\temp`, ``},
		{`"{\"kty\":\"OKP\"}"`, `{"kty":"OKP"}`, ``},
		{`'single "quoted" \n'`, `single "quoted" \n`, ``},
		{`'it\'s'`, `it\'s`, ``},
		{`"it\'s"`, `it's`, ``},
		{`"cost \$5"`, `cost $5`, ``},
		{"\"run \\`date\\`\"", "run `date`", ``},
		{`"hi\!"`, `hi!`, ``},
		{`"$HOME"`, `$HOME`, ``},
		{`$HOME`, `$HOME`, ``},
		{`'$HOME'`, `$HOME`, ``},
		{`'a # b' # note`, `a # b`, `note`},
		{`""`, ``, ``},
		{`{"kty":"OKP"}`, `{"kty":"OKP"}`, ``},
		{`a"b'c`, `a"b'c`, ``},
	}
	for _, tt := range tests {
		value, comment, err := parseValue(tt.raw)
		if err != nil || value != tt.value || comment != tt.comment {
			t.Errorf("parseValue(%q) = %q, %q, %v; want %q, %q", tt.raw, value, comment, err, tt.value, tt.comment)
		}
	}
}

// A value that starts with a quote but isn't one closed quoted string,
// optionally followed by a comment, is an error, as it is for godotenv.
func TestParseValueRejectsMalformedQuoting(t *testing.T) {
	for _, raw := range []string{
		`"unclosed`,
		`'unclosed`,
		`'C:\'`,
		`"ends\"`,
		`"a"b`,
		`"a" b`,
		`'a'b`,
		`"a" "b"`,
	} {
		if value, err := ParseValue(raw); err == nil {
			t.Errorf("ParseValue(%q) = %q, want an error", raw, value)
		}
	}
}

func TestParseEnvEntriesReportsMalformedLine(t *testing.T) {
	_, err := ParseEnvEntries([]byte("A=1\nB='C:\\'\n"))
	if err == nil || !strings.Contains(err.Error(), "line 2") {
		t.Errorf("ParseEnvEntries error = %v, want one naming line 2", err)
	}
	_, err = NewEnvResourceProvider().NativeToYAML([]byte("A=1\nB='C:\\'\n"))
	if err == nil || !strings.Contains(err.Error(), "line 2") {
		t.Errorf("NativeToYAML error = %v, want one naming line 2", err)
	}
}

func TestFormatValueRoundTrip(t *testing.T) {
	values := []string{
		``,
		`plain`,
		`8080`,
		`hello world`,
		` leading and trailing `,
		`a#b`,
		`a # b`,
		`x"y`,
		`p\q`,
		`\"`,
		`'single'`,
		`"double"`,
		`$HOME`,
		"`date`",
		`hi!`,
		"line1\nline2",
		"crlf\r\n",
		"tab\there",
		`{"kty":"OKP","x":"abc"}`,
	}
	for _, v := range values {
		formatted := FormatValue(v)
		if got, err := ParseValue(formatted); err != nil || got != v {
			t.Errorf("ParseValue(FormatValue(%q)) = %q, %v (formatted %q)", v, got, err, formatted)
		}
	}
}

func TestFormatValueEscapesDollarAndBacktick(t *testing.T) {
	if got, want := FormatValue("$HOME and ${PATH} `date`"), "\"\\$HOME and \\${PATH} \\`date\\`\""; got != want {
		t.Errorf("FormatValue = %s, want %s", got, want)
	}
}

// The single-line entries of godotenv's quoted.env fixture
// (github.com/joho/godotenv, fixtures/quoted.env), with the values its tests
// expect.
func TestParseEnvEntriesGodotenvQuotedFixture(t *testing.T) {
	env := []byte(`OPTION_A='1'
OPTION_B='2'
OPTION_C=''
OPTION_D='\n'
OPTION_E="1"
OPTION_F="2"
OPTION_G=""
OPTION_H="\n"
OPTION_I = "echo 'asd'"
`)
	want := map[string]string{
		"OPTION_A": "1",
		"OPTION_B": "2",
		"OPTION_C": "",
		"OPTION_D": `\n`,
		"OPTION_E": "1",
		"OPTION_F": "2",
		"OPTION_G": "",
		"OPTION_H": "\n",
		"OPTION_I": "echo 'asd'",
	}
	entries, err := ParseEnvEntries(env)
	if err != nil {
		t.Fatalf("ParseEnvEntries: %v", err)
	}
	if len(entries) != len(want) {
		t.Errorf("got %d entries, want %d", len(entries), len(want))
	}
	for _, e := range entries {
		if e.Value != want[e.Key] {
			t.Errorf("%s = %q, want %q", e.Key, e.Value, want[e.Key])
		}
	}
}

func TestFormatValueLeavesPlainValuesUnquoted(t *testing.T) {
	for _, v := range []string{`plain`, `8080`, `http://example.com/path?q=1`, `a=b`} {
		if got := FormatValue(v); got != v {
			t.Errorf("FormatValue(%q) = %q, want it unquoted", v, got)
		}
	}
}

// Converting an env file to YAML and back, repeatedly, must not change the
// values, as happens each time a function mutates an AppConfig/Env document.
func TestEnvRoundTripPreservesValues(t *testing.T) {
	rp := NewEnvResourceProvider()
	env := []byte(`JSON={"kty":"OKP","x":"abc"}
SPACED="hello world"
HASHED="a # b" # a comment
ESCAPED="x\"y\\z"
MULTILINE="line1\nline2"
PORT=5432 # port
`)
	want := map[string]string{
		"JSON":      `{"kty":"OKP","x":"abc"}`,
		"SPACED":    `hello world`,
		"HASHED":    `a # b`,
		"ESCAPED":   `x"y\z`,
		"MULTILINE": "line1\nline2",
		"PORT":      `5432`,
	}
	for pass := 1; pass <= 3; pass++ {
		yamlData, err := rp.NativeToYAML(env)
		if err != nil {
			t.Fatalf("pass %d: NativeToYAML: %v", pass, err)
		}
		env, err = rp.YAMLToNative(yamlData)
		if err != nil {
			t.Fatalf("pass %d: YAMLToNative: %v", pass, err)
		}
		entries, err := ParseEnvEntries(env)
		if err != nil {
			t.Fatalf("pass %d: ParseEnvEntries: %v", pass, err)
		}
		got := map[string]string{}
		for _, e := range entries {
			got[e.Key] = e.Value
		}
		for k, v := range want {
			if got[k] != v {
				t.Errorf("pass %d: %s = %q, want %q\nenv:\n%s", pass, k, got[k], v, env)
			}
		}
		if len(got) != len(want) {
			t.Errorf("pass %d: got %d entries, want %d: %q", pass, len(got), len(want), got)
		}
	}
}
