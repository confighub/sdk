// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package api

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestOperatorSeparation is the key regression test - validates that standard vs import parsing
// have different operator support, protecting GenericFnResourceWhereMatchWithComparators
func TestOperatorSeparation(t *testing.T) {
	tests := []struct {
		name        string
		query       string
		description string
	}{
		{
			name:        "IN operator separation",
			query:       "name IN ('test1', 'test2')",
			description: "IN operator should be rejected by standard parsing but accepted by import parsing",
		},
		{
			name:        "NOT IN operator separation",
			query:       "kind NOT IN ('Secret', 'ConfigMap')",
			description: "NOT IN operator should be rejected by standard parsing but accepted by import parsing",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Standard parsing should reject IN/NOT IN
			_, err := ParseAndValidateWhereFilter(tt.query)
			assert.Error(t, err, "Standard parsing should reject query: %s", tt.query)

			// Import parsing should accept IN/NOT IN
			expressions, err := ParseAndValidateWhereFilterForImport(tt.query)
			assert.NoError(t, err, "Import parsing should accept query: %s", tt.query)
			assert.NotEmpty(t, expressions, "Import parsing should return expressions")
		})
	}
}

// TestStandardParsingOperatorLimits ensures standard parsing only supports basic operators
func TestStandardParsingOperatorLimits(t *testing.T) {
	// Test that standard operators work
	validQuery := "name = 'test' AND age > 18"
	expressions, err := ParseAndValidateWhereFilter(validQuery)
	assert.NoError(t, err, "Standard operators should work")
	assert.Len(t, expressions, 2, "Should parse two expressions")

	// Test that enhanced operators are rejected
	enhancedQueries := []string{
		"name IN ('test1', 'test2')",
		"kind NOT IN ('Secret')",
	}

	for _, query := range enhancedQueries {
		_, err := ParseAndValidateWhereFilter(query)
		assert.Error(t, err, "Enhanced operator should be rejected in standard parsing: %s", query)
	}
}

// TestImportParsingEnhancedOperators validates import-specific operator support
func TestImportParsingEnhancedOperators(t *testing.T) {
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
