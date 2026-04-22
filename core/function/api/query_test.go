// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package api

import (
	"testing"

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
