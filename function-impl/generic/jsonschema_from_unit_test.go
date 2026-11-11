// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package generic

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/confighub/sdk/core/function/api"
	"github.com/confighub/sdk/core/third_party/gaby"
)

// replicasSchemaYAML is a schema Unit's data, as YAML with a comment, which reading the Unit turns
// into a $comment$ key the schema must not see.
const replicasSchemaYAML = `# The shape a Deployment's replicas take.
type: object
required: [definitelyNotPresent]
properties:
  spec:
    type: object
    properties:
      replicas:
        type: %s
`

func vetFromUnit(t *testing.T, schemaYAML string, ignoreRequired bool) api.ValidationResult {
	t.Helper()
	docs, err := gaby.ParseAll([]byte(deploymentFixture))
	require.NoError(t, err)
	schema, err := gaby.ParseAll([]byte(schemaYAML))
	require.NoError(t, err)
	for _, doc := range schema {
		require.NoError(t, doc.ExtractCommentsToKeys())
	}
	_, output, err := GenericFnVetJSONSchemaFromUnit(testResourceProvider, &api.FunctionOptions{}, nil, docs, schema, ignoreRequired)
	require.NoError(t, err)
	result, ok := output.(api.ValidationResult)
	require.True(t, ok)
	return result
}

func TestVetJSONSchemaFromUnit(t *testing.T) {
	result := vetFromUnit(t, fmt.Sprintf(replicasSchemaYAML, "integer"), false)
	assert.False(t, result.Passed, "the schema requires a property the Deployment lacks")

	result = vetFromUnit(t, fmt.Sprintf(replicasSchemaYAML, "integer"), true)
	assert.True(t, result.Passed, "with ignore-required only the type is checked: %v", result.Details)

	result = vetFromUnit(t, fmt.Sprintf(replicasSchemaYAML, "string"), true)
	assert.False(t, result.Passed)
	require.NotEmpty(t, result.FailedAttributes)
	assert.Equal(t, api.ResolvedPath("spec.replicas"), result.FailedAttributes[0].Path)
}

func TestVetJSONSchemaFromUnitNeedsOneSchema(t *testing.T) {
	docs, err := gaby.ParseAll([]byte(deploymentFixture))
	require.NoError(t, err)

	_, _, err = GenericFnVetJSONSchemaFromUnit(testResourceProvider, &api.FunctionOptions{}, nil, docs, nil, false)
	assert.ErrorContains(t, err, "pass a Unit holding a JSON Schema as OtherData schema")

	two, err := gaby.ParseAll([]byte("type: object\n---\ntype: object\n"))
	require.NoError(t, err)
	_, _, err = GenericFnVetJSONSchemaFromUnit(testResourceProvider, &api.FunctionOptions{}, nil, docs, two, false)
	assert.ErrorContains(t, err, "holds 2 documents")
}
