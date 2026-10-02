// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package entityfilter_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/confighub/sdk/core/configkit/cubkit"
	"github.com/confighub/sdk/core/entityfilter"
)

// parserFor parses expressions over an entity type with the attributes the API publishes, allowing
// the prefixes the type's endpoints expand.
func parserFor(entityType string) *entityfilter.Parser {
	table := cubkit.WhereAttributes()
	return entityfilter.NewParser(table, entityType, table.ExpandableFields(entityType), nil)
}

func TestParseTerms(t *testing.T) {
	expressions, err := parserFor("Unit").Parse("Slug = 'web' AND Labels.tier IN ('a', 'b') AND LEN(FromLinkID) > 0")
	require.NoError(t, err)
	require.Len(t, expressions, 3)

	assert.Equal(t, "Slug", expressions[0].Path)
	assert.Equal(t, "=", expressions[0].Operator)
	assert.Equal(t, "'web'", expressions[0].Literal)
	assert.Equal(t, "Unit", expressions[0].AttributeEntityType)

	assert.Equal(t, "Labels", expressions[1].Path)
	assert.Equal(t, "tier", expressions[1].MapKey)
	assert.Equal(t, "IN", expressions[1].Operator)

	assert.True(t, expressions[2].IsLengthExpression)
	assert.Equal(t, "Link", expressions[2].Attribute.References)
}

// A prefix names another entity through a reference the type's endpoints expand, and the term
// names that entity's attribute.
func TestParseEntityPrefixes(t *testing.T) {
	expressions, err := parserFor("Unit").Parse("UpstreamUnit.Slug = 'base' AND FromLink.*.Slug = 'upgrade'")
	require.NoError(t, err)
	require.Len(t, expressions, 2)
	assert.Equal(t, "UpstreamUnit", expressions[0].EntityPrefix)
	assert.Equal(t, "Unit", expressions[0].AttributeEntityType)
	assert.True(t, expressions[0].IsExtendedTerm)
	assert.Equal(t, "Link", expressions[1].AttributeEntityType)
	assert.True(t, expressions[1].IsArrayElement)

	_, err = parserFor("Unit").Parse("FromLink.Slug = 'upgrade'")
	assert.ErrorContains(t, err, "is a list")

	// Without the included field, the prefix is not a prefix.
	table := cubkit.WhereAttributes()
	_, err = entityfilter.NewParser(table, "Unit", nil, nil).Parse("UpstreamUnit.Slug = 'base'")
	assert.Error(t, err)
}

// A name the API nests and the row does not is declared qualified, and the term's Path is the
// field it reads.
func TestParseQualifiedName(t *testing.T) {
	expressions, err := parserFor("Mutation").Parse("FunctionInvocation.FunctionName = 'set-image'")
	require.NoError(t, err)
	require.Len(t, expressions, 1)
	assert.Equal(t, "FunctionName", expressions[0].Path)
	assert.Equal(t, "FunctionInvocation.FunctionName", expressions[0].AttributeName)
}

func TestParseRefusals(t *testing.T) {
	for _, where := range []string{
		"Colour = 'blue'",             // no such attribute
		"Slug ? 'x'",                  // ? is for arrays and maps
		"SpaceID > 'not-compared-so'", // UUIDs compare for equality only
		"Slug IN ('a'', 'b')",         // a quote inside an IN value
		"Labels.tier = 3",             // a label value is a string
		"Slug = 'a' OR Slug = 'b'",    // only AND
		"HeadRevisionNum = 'one'",     // an int takes an int
	} {
		_, err := parserFor("Unit").Parse(where)
		assert.Error(t, err, where)
	}
}

// A JSON attribute is read by path, and a path takes none of the configuration-data constructs that
// have no JSON path equivalent.
func TestParseJSONPath(t *testing.T) {
	expressions, err := parserFor("Revision").Parse("Conflicts.*.Path = 'spec.replicas'")
	require.NoError(t, err)
	require.Len(t, expressions, 1)
	assert.Equal(t, "*.Path", expressions[0].MapKey)

	_, err = parserFor("Revision").Parse("Conflicts.0.Value#tag = 'x'")
	assert.ErrorContains(t, err, "embedded accessors")
}

// A substituted entity's attributes are checked against the type the caller states.
func TestParseSubstitutedPrefix(t *testing.T) {
	table := cubkit.WhereAttributes()
	parser := entityfilter.NewParser(table, "Unit", nil, map[string]string{"Space": "Space"})
	expressions, err := parser.Parse("Labels.env = Space.Labels.env")
	require.NoError(t, err)
	require.Len(t, expressions, 1)
	assert.Equal(t, "Space", expressions[0].OperandEntityPrefix)

	_, err = parser.Parse("Space.Colour = 'blue'")
	assert.Error(t, err)
}

func TestIsKeyedMap(t *testing.T) {
	table := cubkit.WhereAttributes()
	for _, name := range []string{"Labels", "Annotations", "ValidationErrors", "DeleteGates", "Permissions", "Tags"} {
		assert.True(t, table.IsKeyedMap(name), name)
	}
	for _, name := range []string{"Slug", "Unit", "Data"} {
		assert.False(t, table.IsKeyedMap(name), name)
	}
}
