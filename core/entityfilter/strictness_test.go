// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package entityfilter_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/confighub/sdk/core/configkit/cubkit"
	"github.com/confighub/sdk/core/entityfilter"
	"github.com/confighub/sdk/core/function/api"
)

func TestParseRejects(t *testing.T) {
	for where, why := range map[string]string{
		"Slug = 'a'Slug = 'b'":                        "expected AND",
		"Slug = 'a' Slug = 'b'":                       "expected AND",
		"Slug = 'a' AND":                              "final AND",
		"Slug IS NULL Slug = 'b'":                     "expected AND",
		"LEN(Labels.tier) > 1":                        "expected `)`",
		"LEN(Labels] > 1":                             "",
		"UpstreamUnitID = 'not-a-uuid'":               "expected a UUID",
		"UpstreamUnitID IN ('not-a-uuid')":            "invalid UUID",
		"CreatedAt > 'yesterday'":                     "invalid time literal",
		"Slug ~ '(?i)web'":                            "`(?` constructs",
		"Slug ~ '[[:alpha:]]'":                        "classes are not supported",
		"Slug = 'a\x00b'":                             "control character",
		"Labels.tier = 'a\nb'":                        "control character",
		"Slug IN ('a', 'b\x01')":                      "control character",
		"Slug = 'a' AND " + strings.Repeat("x", 9000): "maximum length",
	} {
		_, err := parserFor("Unit").Parse(where)
		if assert.Error(t, err, "where %q", where) {
			assert.Contains(t, err.Error(), why, "where %q", where)
		}
	}
}

func TestParseAccepts(t *testing.T) {
	for _, where := range []string{
		"Slug IS NULL AND Slug = 'b'",
		"LEN(Labels) > 1 AND Slug = 'a'",
		"Slug = '?slug' AND DisplayName LIKE '%?TableAlias%'",
		"Slug IN ('a,b', '')",
		"CreatedAt > '2025-02-18' AND CreatedAt < '2025-02-18T23:16:34Z'",
		"Slug ~ '^web-[0-9]{1,3}$'",
	} {
		_, err := parserFor("Unit").Parse(where)
		assert.NoError(t, err, "where %q", where)
	}
}

func TestParseLimitsTerms(t *testing.T) {
	terms := make([]string, api.MaxTerms+1)
	for i := range terms {
		terms[i] = "HeadRevisionNum > 1"
	}
	_, err := parserFor("Unit").Parse(strings.Join(terms, " AND "))
	assert.ErrorContains(t, err, "more than")
}

// A substituted entity's attribute is read from the value by reflection, so its name is checked
// against the entity's type, as an attribute on the left is.
func TestParseSubstitutedOperandIsChecked(t *testing.T) {
	table := cubkit.WhereAttributes()
	parser := entityfilter.NewParser(table, "Unit", nil, map[string]string{"Space": "Space"})
	_, err := parser.Parse("Slug = Space.Slug")
	require.NoError(t, err)
	_, err = parser.Parse("Labels.region = Space.Labels.region")
	require.NoError(t, err)
	_, err = parser.Parse("Slug = Space.NotAnAttribute")
	assert.ErrorContains(t, err, "unrecognized attribute name `NotAnAttribute`")
}

// The parser never panics, and every string literal it accepts is UTF-8 without control
// characters.
func FuzzParse(f *testing.F) {
	for _, seed := range []string{
		"Slug = 'web' AND Labels.tier IN ('a', 'b') AND LEN(FromLinkID) > 0",
		"UpstreamUnit.Slug = 'base' AND FromLink.*.Slug = 'upgrade'",
		"LEN(Permissions.Edit.UserIDs) > 0 AND Permissions.View.UserIDs ? '0b6e7d1c-7b7e-4e0b-9a39-6a8f6e1c2d3e'",
		"ValidationErrors.space/trigger/vet-cel:2 = true",
		"Slug ~ '^a.b$' AND DisplayName ILIKE '%x%'",
		"HeadRevisionNum IN (1 OR 1=1)",
		"Slug IN ('x' OR 1=1)",
		"MergeSourceID = '0b6e7d1c-7b7e-4e0b-9a39-6a8f6e1c2d3e' IS NOT FALSE",
	} {
		f.Add(seed)
	}
	parser := parserFor("Unit")
	f.Fuzz(func(t *testing.T, where string) {
		expressions, err := parser.Parse(where)
		if err != nil {
			return
		}
		for _, expression := range expressions {
			if text, ok := strings.CutPrefix(expression.Literal, "'"); ok && expression.OperandEntityPrefix == "" && !strings.HasPrefix(expression.Literal, "('") {
				require.NoError(t, api.CheckStringLiteralText(strings.TrimSuffix(text, "'")), "where %q", where)
			}
		}
	})
}
