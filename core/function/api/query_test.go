// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package api

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestStandardParsingOperators ensures standard parsing supports intended operators
func TestStandardParsingOperators(t *testing.T) {
	// Test that standard operators work
	validQuery := "name = 'test' AND age > 18"
	expressions, err := ParseAndValidateWhereFilter(validQuery)
	assert.NoError(t, err, "Standard operators should work")
	assert.Len(t, expressions, 2, "Should parse two expressions")

	// Test newer enhanced operators
	enhancedQueries := []string{
		"name IN ('test1', 'test2')",
		"kind NOT IN ('Secret')",
	}

	for _, query := range enhancedQueries {
		_, err := ParseAndValidateWhereFilter(query)
		assert.NoError(t, err, "IN and NOT IN should be supported in standard parsing: %s", query)
	}
}

// TestINClauseRejectsSQLInjection is a regression test for a SQL injection that
// was possible via the IN / NOT IN clause of the metadata `where` filter.
//
// Every other literal path forbids quotes and backslashes
// (safeStringCharsRegexpString = [^'"\\]*), but the IN-clause path validated only
// that there was no ')' inside. A value such as `x' OR 1=1` therefore survived the
// quote-trim in ParseInClauseValues and was re-wrapped, unescaped, into the raw SQL
// WHERE clause. inClauseRegexp now matches only a comma-separated list that is homogeneous in
// kind -- all quoted strings, all ints, or all bools -- so any operator/comment/cast text between
// values, or a mix of value kinds, fails to parse here. (Per-column-type validation, e.g. matching
// the list's kind to the column, is added on top by ValidateInClauseValues in the entity filter
// parser; see TestValidateInClauseValues.)
//
// Why the inputs below contain `OR`, comments, `;`, `||`: the filter grammar does NOT
// support those. Expressions are `path operator value` joined only by `AND`; there is no
// `OR`. These are ATTACKER strings, not valid filter syntax. The parser never inspected
// the contents of `IN (...)`, so it forwarded them into SQL unchecked. Each case asserts
// that such input is now rejected at parse, not passed through.
func TestINClauseRejectsSQLInjection(t *testing.T) {
	injections := []string{
		"Slug IN ('x' OR 1=1 OR Slug = 'y')",
		"Slug IN ('x'--comment)",
		"Slug IN ('a'='a)",
		"Slug NOT IN ('x' OR 1=1)",
		"Slug IN ('x'; DROP TABLE units)",
		"Slug IN ('a', 'b' OR '1'='1')",
		// Mixed value kinds in one list are rejected by the homogeneous-list regex, even on this
		// config-data path (which has no column type and does not call ValidateInClauseValues).
		"Slug IN ('a', 1)",
		"age IN (1, 'x')",
		"flag IN (true, 1)",
	}
	for _, q := range injections {
		t.Run("rejects/"+q, func(t *testing.T) {
			_, err := ParseAndValidateWhereFilter(q)
			assert.Error(t, err, "injection payload must be rejected: %s", q)
		})
	}

	// Legitimate IN clauses contain ONLY what the grammar supports: a comma-separated list
	// of plain literal values (strings, UUIDs, slugs with -, _, .). These must still parse.
	valid := []string{
		"Slug IN ('alpha', 'beta', 'gamma')",
		"kind NOT IN ('Secret')",
		"SpaceID IN ('11111111-1111-1111-1111-111111111111', '22222222-2222-2222-2222-222222222222')",
		"name IN ('foo-bar', 'baz_qux', 'a.b.c')",
	}
	for _, q := range valid {
		t.Run("valid/"+q, func(t *testing.T) {
			_, err := ParseAndValidateWhereFilter(q)
			assert.NoError(t, err, "legitimate IN clause must still parse: %s", q)
		})
	}
}

// TestValidateInClauseValues covers the per-column-type IN/NOT IN value validation that the
// entity filter parser runs before SQL is generated. It validates each raw value token by kind, so
// every value's type -- quoted string vs bare int vs bare bool -- must match the column type. That
// rejects the security-critical case (a quoted value aimed at a numeric/bool column, which the SQL
// generator would strip and emit bare) and also enforces a single consistent type across the list
// (mixed-type lists, bare values on a string column, and quoted values on an int column all fail).
func TestValidateInClauseValues(t *testing.T) {
	cases := []struct {
		literal  string
		dataType DataType
		valid    bool
	}{
		// string columns: every value must be a quoted string token
		{"('alpha', 'beta')", DataTypeString, true},
		{"('a-b', 'c.d_e')", DataTypeString, true},
		{"(5)", DataTypeString, false},      // bare value for a string column
		{"('a', 1)", DataTypeString, false}, // mixed types in one list
		// int columns: every value must be a bare integer
		{"(1, 2, 3)", DataTypeInt, true},
		{"(0)", DataTypeInt, true},
		{"('1 OR 1=1')", DataTypeInt, false}, // quoted value -> stripped -> would be bare -> must reject
		{"('5')", DataTypeInt, false},        // quoted value for an int column
		{"(1, 'x')", DataTypeInt, false},     // mixed types in one list
		// bool columns / bool maps: every value must be a bare boolean
		{"(true)", DataTypeBool, true},
		{"(TRUE, false)", DataTypeStringBoolMap, true},
		{"('true OR 1=1')", DataTypeStringBoolMap, false},
		{"(true, 1)", DataTypeStringBoolMap, false}, // mixed types in one list
		// unsupported type fails closed
		{"('x')", DataTypeStringStringUUIDBoolMap, false},
	}
	for _, c := range cases {
		t.Run(string(c.dataType)+"/"+c.literal, func(t *testing.T) {
			err := ValidateInClauseValues(c.literal, c.dataType)
			if c.valid {
				assert.NoError(t, err, "%s on %v should be valid", c.literal, c.dataType)
			} else {
				assert.Error(t, err, "%s on %v should be rejected", c.literal, c.dataType)
			}
		})
	}
}

// TestNotLikeOperator tests the NOT LIKE operator for config data where filters
func TestNotLikeOperator(t *testing.T) {
	// Test parsing
	expressions, err := ParseAndValidateWhereFilter("name NOT LIKE 'test%'")
	require.NoError(t, err)
	require.Len(t, expressions, 1)
	assert.Equal(t, "NOT LIKE", expressions[0].Operator)
	assert.Equal(t, "'test%'", expressions[0].Literal)

	// Test in conjunction
	expressions, err = ParseAndValidateWhereFilter("name NOT LIKE 'admin%' AND kind = 'Pod'")
	require.NoError(t, err)
	require.Len(t, expressions, 2)
	assert.Equal(t, "NOT LIKE", expressions[0].Operator)
	assert.Equal(t, "=", expressions[1].Operator)

	// Test evaluation - NOT LIKE should negate LIKE
	testCases := []struct {
		name     string
		value    string
		pattern  string
		expected bool
	}{
		{"no match returns true", "hello", "'test%'", true},
		{"match returns false", "test-app", "'test%'", false},
		{"underscore wildcard no match", "ab", "'a_c'", true},
		{"underscore wildcard match", "abc", "'a_c'", false},
		{"exact no match", "world", "'hello'", true},
		{"exact match", "hello", "'hello'", false},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			expr := &RelationalExpression{
				Path:     "name",
				Operator: "NOT LIKE",
				Literal:  tc.pattern,
				DataType: DataTypeString,
			}
			result, err := EvaluateExpression(expr, tc.value, nil, nil)
			assert.NoError(t, err)
			assert.Equal(t, tc.expected, result)
		})
	}
}

// TestEvaluateExpressionUUID covers the cross-entity filter cases that surface
// during bulk operations (e.g. "cub variant promote"), where a UUID-valued field
// is compared against a value supplied at runtime as a string rather than as a
// quoted literal. Before the fix, the DataTypeUUID case rejected a string right
// operand, and the DataTypeString case could not stringify a uuid.UUID left
// operand, so a filter like "SpaceID = '<uuid>'" or "FromUnitID IN (...)" failed
// in-memory with "internal error: expected ... but got uuid.UUID".
func TestEvaluateExpressionUUID(t *testing.T) {
	left := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	same := "11111111-1111-1111-1111-111111111111"
	other := "22222222-2222-2222-2222-222222222222"

	t.Run("DataTypeUUID right value as string matches", func(t *testing.T) {
		expr := &RelationalExpression{Path: "FromUnitID", Operator: "=", DataType: DataTypeUUID}
		result, err := EvaluateExpression(expr, left, same, nil)
		require.NoError(t, err)
		assert.True(t, result)
	})

	t.Run("DataTypeUUID right value as string not equal", func(t *testing.T) {
		expr := &RelationalExpression{Path: "FromUnitID", Operator: "!=", DataType: DataTypeUUID}
		result, err := EvaluateExpression(expr, left, other, nil)
		require.NoError(t, err)
		assert.True(t, result)
	})

	t.Run("DataTypeUUID right value as uuid.UUID matches", func(t *testing.T) {
		expr := &RelationalExpression{Path: "FromUnitID", Operator: "=", DataType: DataTypeUUID}
		result, err := EvaluateExpression(expr, left, uuid.MustParse(same), nil)
		require.NoError(t, err)
		assert.True(t, result)
	})

	t.Run("DataTypeUUID literal right value still works", func(t *testing.T) {
		expr := &RelationalExpression{Path: "FromUnitID", Operator: "=", Literal: "'" + same + "'", DataType: DataTypeUUID}
		result, err := EvaluateExpression(expr, left, nil, nil)
		require.NoError(t, err)
		assert.True(t, result)
	})

	t.Run("DataTypeUUID invalid string right value errors", func(t *testing.T) {
		expr := &RelationalExpression{Path: "FromUnitID", Operator: "=", DataType: DataTypeUUID}
		_, err := EvaluateExpression(expr, left, "not-a-uuid", nil)
		require.Error(t, err)
	})

	// The original "cub variant promote" failure: a UUID-valued field compared as
	// a string ("SpaceID = '<uuid>'"). The left value arrives as a uuid.UUID, and
	// valueToStringWithReflection must stringify it rather than failing.
	t.Run("DataTypeString left value as uuid.UUID matches literal", func(t *testing.T) {
		expr := &RelationalExpression{Path: "SpaceID", Operator: "=", Literal: "'" + same + "'", DataType: DataTypeString}
		result, err := EvaluateExpression(expr, left, nil, nil)
		require.NoError(t, err)
		assert.True(t, result)
	})

	t.Run("DataTypeString left value as uuid.UUID not equal literal", func(t *testing.T) {
		expr := &RelationalExpression{Path: "SpaceID", Operator: "=", Literal: "'" + other + "'", DataType: DataTypeString}
		result, err := EvaluateExpression(expr, left, nil, nil)
		require.NoError(t, err)
		assert.False(t, result)
	})
}

// TestImportParsingOperators validates import-specific operator support
func TestImportParsingOperators(t *testing.T) {
	tests := []struct {
		name    string
		query   string
		isValid bool
	}{
		{
			name:    "Basic equality works",
			query:   "kind = 'Pod'",
			isValid: true,
		},
		{
			name:    "IN operator works",
			query:   "metadata.namespace IN ('default', 'kube-system')",
			isValid: true,
		},
		{
			name:    "NOT IN operator works",
			query:   "kind NOT IN ('Secret', 'ConfigMap')",
			isValid: true,
		},
		{
			name:    "Unsupported operator rejected",
			query:   "metadata.creationTimestamp > '2023-01-01'",
			isValid: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			expressions, err := ParseAndValidateWhereFilterForImport(tt.query)

			if tt.isValid {
				assert.NoError(t, err, "Query should be valid: %s", tt.query)
				assert.NotEmpty(t, expressions, "Should return expressions")
			} else {
				assert.Error(t, err, "Query should be invalid: %s", tt.query)
			}
		})
	}
}

// TestImportFilterConversionPipeline tests the complete business logic pipeline
// from where-filter query string to ImportFilters and ImportOptions
func TestImportFilterConversionPipeline(t *testing.T) {
	tests := []struct {
		name            string
		query           string
		expectedFilters int
		expectedOptions int
		validateResults func(t *testing.T, filters []ImportFilter, options ImportOptions)
	}{
		{
			name:            "Simple filter conversion",
			query:           "metadata.namespace = 'default'",
			expectedFilters: 1,
			expectedOptions: 0,
			validateResults: func(t *testing.T, filters []ImportFilter, options ImportOptions) {
				assert.Equal(t, "metadata.namespace", filters[0].Type)
				assert.Equal(t, "include", filters[0].Operator)
				assert.Equal(t, []string{"default"}, filters[0].Values)
			},
		},
		{
			name:            "IN clause with multiple values",
			query:           "metadata.namespace IN ('default', 'production')",
			expectedFilters: 1,
			expectedOptions: 0,
			validateResults: func(t *testing.T, filters []ImportFilter, options ImportOptions) {
				assert.Equal(t, "metadata.namespace", filters[0].Type)
				assert.Equal(t, "include", filters[0].Operator)
				assert.Equal(t, []string{"default", "production"}, filters[0].Values)
			},
		},
		{
			name:            "NOT IN clause maps to exclude",
			query:           "kind NOT IN ('Secret', 'ConfigMap')",
			expectedFilters: 1,
			expectedOptions: 0,
			validateResults: func(t *testing.T, filters []ImportFilter, options ImportOptions) {
				assert.Equal(t, "kind", filters[0].Type)
				assert.Equal(t, "exclude", filters[0].Operator)
				assert.Equal(t, []string{"Secret", "ConfigMap"}, filters[0].Values)
			},
		},
		{
			name:            "Import options separated from filters",
			query:           "metadata.namespace = 'default' AND import.include_system = true",
			expectedFilters: 1,
			expectedOptions: 1,
			validateResults: func(t *testing.T, filters []ImportFilter, options ImportOptions) {
				// Validate filter
				assert.Equal(t, "metadata.namespace", filters[0].Type)
				assert.Equal(t, "include", filters[0].Operator)

				// Validate option
				assert.Equal(t, true, options["include_system"])
			},
		},
		{
			name:            "Complex mixed query",
			query:           "metadata.namespace IN ('default', 'production') AND kind = 'Pod' AND import.include_system = true AND import.include_custom = false",
			expectedFilters: 2,
			expectedOptions: 2,
			validateResults: func(t *testing.T, filters []ImportFilter, options ImportOptions) {
				// Should have namespace and kind filters
				filterTypes := make(map[string]bool)
				for _, filter := range filters {
					filterTypes[filter.Type] = true
				}
				assert.True(t, filterTypes["metadata.namespace"], "Should have namespace filter")
				assert.True(t, filterTypes["kind"], "Should have kind filter")

				// Should have both options
				assert.Equal(t, true, options["include_system"])
				assert.Equal(t, false, options["include_custom"])
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			filters, options, err := ParseWhereFilterForImport(tt.query)

			require.NoError(t, err, "Query should parse successfully: %s", tt.query)
			assert.Len(t, filters, tt.expectedFilters, "Filter count mismatch")
			assert.Len(t, options, tt.expectedOptions, "Option count mismatch")

			if tt.validateResults != nil {
				tt.validateResults(t, filters, options)
			}
		})
	}
}

// TestWhereResourceWithDataPaths verifies that ParseAndValidateWhereResource
// accepts both ConfigHub.* metadata paths and data field paths.
func TestWhereResourceWithDataPaths(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr bool
		numExpr int
	}{
		{
			name:    "ConfigHub metadata path",
			input:   "ConfigHub.ResourceType = 'apps/v1/Deployment'",
			numExpr: 1,
		},
		{
			name:    "data field path",
			input:   "spec.replicas > 1",
			numExpr: 1,
		},
		{
			name:    "kind data path",
			input:   "kind = 'Deployment'",
			numExpr: 1,
		},
		{
			name:    "mixed ConfigHub and data paths",
			input:   "ConfigHub.ResourceType = 'apps/v1/Deployment' AND spec.replicas > 1",
			numExpr: 2,
		},
		{
			name:    "invalid ConfigHub path",
			input:   "ConfigHub.InvalidPath = 'test'",
			wantErr: true,
		},
		{
			name:    "empty input",
			input:   "",
			numExpr: 0,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			whereExpressions, err := ParseAndValidateWhereResource(tc.input)
			if tc.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				if tc.input == "" {
					assert.Nil(t, whereExpressions)
				} else {
					require.NotNil(t, whereExpressions)
					assert.Len(t, whereExpressions, tc.numExpr)
				}
			}
		})
	}
}

// TestKubernetesPathSupport validates that Kubernetes-specific paths work correctly
func TestKubernetesPathSupport(t *testing.T) {
	kubernetesQueries := []string{
		"metadata.namespace = 'default'",
		"metadata.labels.app = 'nginx'",
		"metadata.annotations.version = 'v1.2.3'",
		"kind = 'Pod'",
		"apiVersion = 'v1'",
		"import.include_system = true",
	}

	for _, query := range kubernetesQueries {
		t.Run(query, func(t *testing.T) {
			expressions, err := ParseAndValidateWhereFilterForImport(query)
			assert.NoError(t, err, "Kubernetes path should work: %s", query)
			assert.Len(t, expressions, 1, "Should parse exactly one expression")
			assert.NotEmpty(t, expressions[0].Path, "Path should not be empty")
		})
	}
}
