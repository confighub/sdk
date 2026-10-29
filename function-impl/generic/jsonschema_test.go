// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package generic

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/confighub/sdk/configkit/envkit"
	"github.com/confighub/sdk/core/function/api"
	"github.com/confighub/sdk/core/third_party/gaby"
	"github.com/confighub/sdk/core/workerapi"
)

// schemaMapWithRequired wraps a Deployment-shaped JSON Schema, keyed on the
// Kubernetes resource type so K8sResourceProvider's ResourceTypeGetter
// matches. Requires a property the fixture doesn't carry so the no-flag and
// ignore-required=true cases produce different outcomes.
const schemaMapWithRequired = `{
	"apps/v1/Deployment": {
		"type": "object",
		"required": ["definitelyNotPresent"],
		"properties": {
			"spec": {
				"type": "object",
				"properties": {
					"replicas": {"type": "integer"}
				}
			}
		}
	}
}`

func TestVetJSONSchema_RequiredEnforcedByDefault(t *testing.T) {
	docs, err := gaby.ParseAll([]byte(deploymentFixture))
	require.NoError(t, err)

	args := []api.FunctionArgument{{Value: schemaMapWithRequired}}
	_, output, err := GenericFnVetJSONSchema(testResourceProvider, &api.FunctionOptions{}, nil, docs, args)
	require.NoError(t, err)

	vr, ok := output.(api.ValidationResult)
	require.True(t, ok)
	assert.False(t, vr.Passed, "expected validation to fail on missing required field")
	// Spot-check the failure mentions the missing required field.
	found := false
	for _, d := range vr.Details {
		if contains(d, "definitelyNotPresent") {
			found = true
			break
		}
	}
	assert.True(t, found, "expected detail mentioning the missing required field, got: %v", vr.Details)
}

func TestVetJSONSchema_IgnoreRequiredTrue_PassesWhenOnlyRequiredFails(t *testing.T) {
	docs, err := gaby.ParseAll([]byte(deploymentFixture))
	require.NoError(t, err)

	args := []api.FunctionArgument{
		{Value: schemaMapWithRequired},
		{Value: true}, // ignore-required
	}
	_, output, err := GenericFnVetJSONSchema(testResourceProvider, &api.FunctionOptions{}, nil, docs, args)
	require.NoError(t, err)

	vr, ok := output.(api.ValidationResult)
	require.True(t, ok)
	assert.True(t, vr.Passed,
		"expected validation to pass with ignore-required=true; got details: %v", vr.Details)
}

func TestVetJSONSchema_IgnoreRequiredTrue_StillFlagsTypeMismatches(t *testing.T) {
	// Tightening the schema to require an integer where the fixture supplies one
	// still passes; flipping the type expectation to string flags the mismatch
	// even with ignore-required=true. Demonstrates that ignore-required only
	// drops the required-keyword check, not other constraints.
	schemaMap := `{
		"apps/v1/Deployment": {
			"type": "object",
			"required": ["definitelyNotPresent"],
			"properties": {
				"spec": {
					"type": "object",
					"properties": {
						"replicas": {"type": "string"}
					}
				}
			}
		}
	}`
	docs, err := gaby.ParseAll([]byte(deploymentFixture))
	require.NoError(t, err)

	args := []api.FunctionArgument{
		{Value: schemaMap},
		{Value: true},
	}
	_, output, err := GenericFnVetJSONSchema(testResourceProvider, &api.FunctionOptions{}, nil, docs, args)
	require.NoError(t, err)

	vr, ok := output.(api.ValidationResult)
	require.True(t, ok)
	assert.False(t, vr.Passed,
		"expected validation to fail on the type mismatch (replicas is int, schema says string)")
}

// contains is strings.Contains spelled out to avoid an import shuffle in this
// small test file. Tiny enough to inline.
func contains(haystack, needle string) bool {
	if len(needle) == 0 {
		return true
	}
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return true
		}
	}
	return false
}

// envFixture is an AppConfig/Env Unit as envkit parses it: the configHub.* lines
// become a configHub object beside the variables.
const envFixture = `configHub:
  configName: app-env
  configSchema: App
LOG_LEVEL: debug
`

// strictEnvSchemaMap allows only the application's variables.
const strictEnvSchemaMap = `{
	"App": {
		"type": "object",
		"additionalProperties": false,
		"properties": {
			"LOG_LEVEL": {"type": "string", "enum": ["debug", "info"]}
		}
	}
}`

func vetEnv(t *testing.T, fixture string, functionContext *api.FunctionContext) api.ValidationResult {
	t.Helper()
	docs, err := gaby.ParseAll([]byte(fixture))
	require.NoError(t, err)
	args := []api.FunctionArgument{{Value: strictEnvSchemaMap}}
	_, output, err := GenericFnVetJSONSchema(envkit.NewEnvResourceProvider(), &api.FunctionOptions{}, functionContext, docs, args)
	require.NoError(t, err)
	vr, ok := output.(api.ValidationResult)
	require.True(t, ok)
	return vr
}

func TestVetJSONSchema_AppConfigMetadataIsNotValidated(t *testing.T) {
	appConfigEnv := &api.FunctionContext{ToolchainType: workerapi.ToolchainAppConfigEnv}

	vr := vetEnv(t, envFixture, appConfigEnv)
	assert.True(t, vr.Passed, "the configHub metadata is not one of the schema's properties, and must not fail it; got: %v", vr.Details)

	vr = vetEnv(t, envFixture+"LOG_LEVL: info\n", appConfigEnv)
	assert.False(t, vr.Passed, "a property the schema doesn't allow still fails")

	// Without an AppConfig toolchain, the metadata is validated like any other field.
	vr = vetEnv(t, envFixture, nil)
	assert.False(t, vr.Passed, "only an AppConfig toolchain leaves the metadata out")
}
