// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package api

import (
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidatePortableRegexAccepts(t *testing.T) {
	for _, pattern := range []string{
		"",
		"abc",
		"^a.b$",
		"a*b+c?",
		"a{2}", "a{2,}", "a{2,5}", "a{0,255}",
		"(a|b)+c",
		"(a|)b",
		"()a",
		"(a*)*",
		"[abc]", "[^abc]", "[a-z0-9]", "[]a]", "[^]a]", "[a-]", "[-a]", "[.]", "[{]", "[}]", "[*+?|()^$]",
		"[[]",
		"a^b", "a$b",
		"é+", "^ü[ä-ö]$",
		"nginx:1[.]2[0-9]*",
	} {
		assert.NoError(t, ValidatePortableRegex(pattern), "pattern %q", pattern)
	}
}

func TestValidatePortableRegexRejects(t *testing.T) {
	for pattern, why := range map[string]string{
		"*a":          "nothing to repeat",
		"***=a.c":     "nothing to repeat", // a PostgreSQL director
		"a|*b":        "nothing to repeat",
		"(*a)":        "nothing to repeat",
		"a**":         "follows another quantifier",
		"a*?":         "follows another quantifier",
		"a+{2}":       "follows another quantifier",
		"a{2}{3}":     "follows another quantifier",
		"^*":          "anchor cannot be repeated",
		"$+":          "anchor cannot be repeated",
		"(?i)a":       "`(?` constructs",
		"(?:a)":       "`(?` constructs",
		"a(?=b)":      "`(?` constructs",
		"a{":          "not part of a bound",
		"a{x}":        "not part of a bound",
		"a{256}":      "not part of a bound",
		"a{3,2}":      "not part of a bound",
		"a{,2}":       "not part of a bound",
		"a}":          "not part of a bound or bracket",
		"a]":          "not part of a bound or bracket",
		"(a":          "missing `)`",
		"a)":          "unmatched `)`",
		"[a":          "missing `]`",
		"[[:alpha:]]": "classes are not supported",
		"[[.a.]]":     "classes are not supported",
		"[[=a=]]":     "classes are not supported",
		"[a-c-e]":     "must end a range",
		"[--/]":       "must end a range",
		"[c-a]":       "out of order",
		`a\.b`:        "backslashes",
		`[\d]`:        "backslashes",
		strings.Repeat("(", maxRegexDepth+1) + strings.Repeat(")", maxRegexDepth+1): "nest deeper",
		strings.Repeat("a", MaxRegexLength+1):                                       "maximum length",
	} {
		err := ValidatePortableRegex(pattern)
		if assert.Error(t, err, "pattern %q", pattern) {
			assert.Contains(t, err.Error(), why, "pattern %q", pattern)
		}
	}
}

// Every pattern the subset admits compiles in Go, with the flags the evaluator adds.
func FuzzPortableRegex(f *testing.F) {
	for _, seed := range []string{"^a.b$", "[]a-c]+", "(a|)b{0,255}", "[^]-]", "é|ü*", "a{1,}", "[{}]"} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, pattern string) {
		if ValidatePortableRegex(pattern) != nil {
			return
		}
		_, err := regexp.Compile(regexFlags(true) + pattern)
		require.NoError(t, err, "pattern %q", pattern)
	})
}

func TestPatternOperandsAreValidatedAtParse(t *testing.T) {
	_, err := ParseAndValidateWhereFilter("metadata.name ~ '(?i)web'")
	assert.ErrorContains(t, err, "`(?` constructs")
	_, err = ParseAndValidateWhereFilter("metadata.name ~ '^web-[0-9]+$'")
	assert.NoError(t, err)
	_, err = ParseAndValidateWhereFilter("metadata.name LIKE '" + strings.Repeat("%", MaxRegexLength+1) + "'")
	assert.ErrorContains(t, err, "maximum length")
}

// `.` and LIKE's `_` and `%` match a newline, as they do in PostgreSQL.
func TestPatternsMatchNewlines(t *testing.T) {
	for _, tc := range []struct {
		operator, pattern, value string
		want                     bool
	}{
		{"~", "^a.b$", "a\nb", true},
		{"LIKE", "a_b", "a\nb", true},
		{"LIKE", "%foo%", "first\nfoo\nlast", true},
		{"ILIKE", "%FOO%", "first\nfoo", true},
		{"~", "^ab$", "ab\n", false},
	} {
		expr := &RelationalExpression{Operator: tc.operator, Literal: "'" + tc.pattern + "'", DataType: DataTypeString}
		got, err := EvaluateExpression(expr, tc.value, nil, nil)
		require.NoError(t, err)
		assert.Equal(t, tc.want, got, "%q %s %q", tc.value, tc.operator, tc.pattern)
	}
}
