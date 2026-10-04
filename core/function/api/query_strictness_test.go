// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package api

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Terms are joined by AND with whitespace on both sides, and by nothing else, so the terms of an
// expression are exactly those a reader sees.
func TestWhereFilterRequiresConjunction(t *testing.T) {
	for _, query := range []string{
		"a = 'x'b = 'y'",
		"a = 'x' b = 'y'",
		"a = 'x'AND b = 'y'",
		"a = 'x' ANDb = 'y'",
		"a = 'x' AND",
		"a = 'x' AND ",
		"a = 'x' OR b = 'y'",
		"a = 1 2",
	} {
		_, err := ParseAndValidateWhereFilter(query)
		assert.Error(t, err, "query %q", query)
	}
	expressions, err := ParseAndValidateWhereFilter("  a = 'x'  AND\tb = 'y'  ")
	require.NoError(t, err)
	assert.Len(t, expressions, 2)
}

func TestWhereFilterLimits(t *testing.T) {
	terms := make([]string, MaxTerms+1)
	for i := range terms {
		terms[i] = "a = 1"
	}
	_, err := ParseAndValidateWhereFilter(strings.Join(terms, " AND "))
	assert.ErrorContains(t, err, "more than")
	_, err = ParseAndValidateWhereFilter(strings.Join(terms[:MaxTerms], " AND "))
	assert.NoError(t, err)

	values := make([]string, MaxInValues+1)
	for i := range values {
		values[i] = "1"
	}
	_, err = ParseAndValidateWhereFilter("a IN (" + strings.Join(values, ",") + ")")
	assert.ErrorContains(t, err, "more than")
}

func TestStringLiteralText(t *testing.T) {
	for _, literal := range []string{"'a\x00b'", "'a\nb'", "'a\tb'", "'a\x7fb'", "'\xff'"} {
		_, _, _, err := ParseLiteral(literal)
		assert.Error(t, err, "literal %q", literal)
	}
	for _, literal := range []string{"'é ü ✓'", "'a b'", "'?TableAlias'", "''"} {
		_, _, _, err := ParseLiteral(literal)
		assert.NoError(t, err, "literal %q", literal)
	}
}

// An IN list's values are read with the expression that validated it: a comma inside a quoted
// value stays there, and the empty string is a value.
func TestParseInClauseValues(t *testing.T) {
	assert.Equal(t, []string{"a,b", "c"}, ParseInClauseValues("('a,b', 'c')"))
	assert.Equal(t, []string{""}, ParseInClauseValues("('')"))
	assert.Equal(t, []string{"", "x"}, ParseInClauseValues("('' , 'x')"))
	assert.Equal(t, []string{"1", "22"}, ParseInClauseValues("(1,22)"))
	assert.Equal(t, []string{"true"}, ParseInClauseValues("( true )"))

	got, err := EvaluateExpression(&RelationalExpression{Operator: "IN", Literal: "('a,b')", DataType: DataTypeString}, "a,b", nil, nil)
	require.NoError(t, err)
	assert.True(t, got)
}

func TestValidateInClauseValuesTyped(t *testing.T) {
	assert.Error(t, ValidateInClauseValues("('not-a-uuid')", DataTypeUUID))
	assert.NoError(t, ValidateInClauseValues("('0b6e7d1c-7b7e-4e0b-9a39-6a8f6e1c2d3e')", DataTypeUUID))
	assert.Error(t, ValidateInClauseValues("('yesterday')", DataTypeTime))
	assert.NoError(t, ValidateInClauseValues("('2025-02-18', '2025-02-18T23:16:34Z')", DataTypeTime))
}

// Every expression the where-filter parser accepts parses again from its terms, and no accepted
// string literal carries a control character.
func FuzzParseWhereFilter(f *testing.F) {
	for _, seed := range []string{
		"a = 'x' AND b > 2",
		"spec.containers.*.image ~ '^nginx:[0-9]+'",
		"metadata.labels.app IN ('a,b', '')",
		"a.|b != 'x'",
		"a = 'x'b = 'y'",
		"a ~ '(?i)x'",
		"a IN ('x' OR 1=1)",
	} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, query string) {
		expressions, err := ParseAndValidateWhereFilter(query)
		if err != nil {
			return
		}
		terms := make([]string, 0, len(expressions))
		for _, expression := range expressions {
			if expression.DataType == DataTypeString && strings.HasPrefix(expression.Literal, "'") {
				require.NoError(t, CheckStringLiteralText(strings.Trim(expression.Literal, "'")))
			}
			terms = append(terms, expression.Path+" "+expression.Operator+" "+expression.Literal)
		}
		again, err := ParseAndValidateWhereFilter(strings.Join(terms, " AND "))
		require.NoError(t, err, "query %q", query)
		require.Len(t, again, len(expressions))
	})
}
