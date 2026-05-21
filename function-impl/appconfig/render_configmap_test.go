// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package appconfig

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/confighub/sdk/configkit/envkit"
	"github.com/confighub/sdk/configkit/jsonkit"
	"github.com/confighub/sdk/configkit/propkit"
	"github.com/confighub/sdk/core/configkit"
	"github.com/confighub/sdk/core/configkit/yamlkit"
	"github.com/confighub/sdk/core/function/api"
	"github.com/confighub/sdk/core/third_party/gaby"
	"github.com/confighub/sdk/core/workerapi"
	"github.com/google/uuid"
)

func parseNative(t *testing.T, converter configkit.ConfigConverter, native string) gaby.Container {
	t.Helper()
	yamlBytes, err := converter.NativeToYAML([]byte(native))
	require.NoError(t, err)
	parsed, err := gaby.ParseAll(yamlBytes)
	require.NoError(t, err)
	return parsed
}

func newFunctionContext(toolchain workerapi.ToolchainType, slug string, revisionNum int64) *api.FunctionContext {
	return &api.FunctionContext{
		ToolchainType: toolchain,
		UnitSlug:      slug,
		UnitID:        uuid.New(),
		SpaceID:       uuid.New(),
		RevisionNum:   revisionNum,
	}
}

func TestRenderConfigMap_PropertiesImmutable(t *testing.T) {
	rp := propkit.NewPropertiesResourceProvider()
	var converter configkit.ConfigConverter = rp
	var resourceProvider yamlkit.ResourceProvider = rp

	native := "configHub.configName=MyApp\nconfigHub.configSchema=SimpleApp\ndatabase.host=localhost\ndatabase.port=5432\n"
	parsed := parseNative(t, converter, native)
	fc := newFunctionContext(workerapi.ToolchainAppConfigProperties, "my-app", 7)
	args := []api.FunctionArgument{
		{ParameterName: "immutable", Value: true},
	}

	out, output, err := fnRenderConfigMap(converter, resourceProvider, fc, parsed, args)
	require.NoError(t, err)
	require.NotNil(t, output)
	payload, ok := output.(api.YAMLPayload)
	require.True(t, ok)
	require.NotEmpty(t, payload.Payload)

	// Input must be unchanged (non-mutating).
	assert.Equal(t, string(parseNative(t, converter, native).String()), string(out.String()))

	// Parse the rendered ConfigMap as YAML.
	cm, err := gaby.ParseAll([]byte(payload.Payload))
	require.NoError(t, err)
	require.Equal(t, 1, len(cm))

	kind, _ := cm[0].Path("kind").Data().(string)
	assert.Equal(t, "ConfigMap", kind)

	name, _ := cm[0].Path("metadata.name").Data().(string)
	assert.True(t, strings.HasPrefix(name, "my-app-"), "immutable name should have hash suffix, got %q", name)
	assert.NotEqual(t, "my-app", name)

	immutable := cm[0].Path("immutable")
	require.NotNil(t, immutable)
	imVal, _ := immutable.Data().(bool)
	assert.True(t, imVal)

	rev, _ := cm[0].Path("metadata.annotations.confighub~1com/RevisionNum").Data().(string)
	assert.Equal(t, "7", rev)

	stableCore, _ := cm[0].Path("metadata.annotations.confighub~1com/ResourceNameStableCore").Data().(string)
	assert.Equal(t, "my-app", stableCore)

	dataKey, _ := cm[0].Path("metadata.annotations.confighub~1com/RenderRevision").Data().(string)
	assert.Equal(t, "Latest", dataKey)

	// data field should have my-app.properties with the original config minus configHub.
	dataField := cm[0].Path("data")
	require.NotNil(t, dataField)
	dataMap, _ := dataField.Data().(map[string]any)
	require.NotNil(t, dataMap)
	body, _ := dataMap["MyApp.properties"].(string)
	assert.Contains(t, body, "database.host=localhost")
	assert.Contains(t, body, "database.port=5432")
	assert.NotContains(t, body, "configHub.configName")
}

func TestRenderConfigMap_PropertiesMutableStableName(t *testing.T) {
	rp := propkit.NewPropertiesResourceProvider()
	var converter configkit.ConfigConverter = rp
	var resourceProvider yamlkit.ResourceProvider = rp

	native := "configHub.configName=Cfg\nfoo=bar\n"
	parsed := parseNative(t, converter, native)
	fc := newFunctionContext(workerapi.ToolchainAppConfigProperties, "stable-app", 1)
	args := []api.FunctionArgument{{ParameterName: "immutable", Value: false}}

	_, output, err := fnRenderConfigMap(converter, resourceProvider, fc, parsed, args)
	require.NoError(t, err)
	payload := output.(api.YAMLPayload)

	cm, err := gaby.ParseAll([]byte(payload.Payload))
	require.NoError(t, err)
	require.Equal(t, 1, len(cm))

	name, _ := cm[0].Path("metadata.name").Data().(string)
	assert.Equal(t, "stable-app", name, "mutable ConfigMap should use the unit slug directly with no hash suffix")

	immutable := cm[0].Path("immutable")
	assert.Nil(t, immutable, "mutable ConfigMap should not have an immutable field")

	// Hash annotation is still present even in mutable mode (used to trigger rolling restart).
	hash, _ := cm[0].Path("metadata.annotations.confighub~1com/Hash").Data().(string)
	assert.NotEmpty(t, hash)
}

func TestRenderConfigMap_EnvAsKeyValue(t *testing.T) {
	rp := envkit.NewEnvResourceProvider()
	var converter configkit.ConfigConverter = rp
	var resourceProvider yamlkit.ResourceProvider = rp

	native := "configHub.configName=Cfg\nDATABASE_HOST=localhost\nDATABASE_PORT=5432\n"
	parsed := parseNative(t, converter, native)
	fc := newFunctionContext(workerapi.ToolchainAppConfigEnv, "envcfg", 1)
	args := []api.FunctionArgument{
		{ParameterName: "immutable", Value: true},
		{ParameterName: "as-key-value", Value: true},
	}

	_, output, err := fnRenderConfigMap(converter, resourceProvider, fc, parsed, args)
	require.NoError(t, err)
	payload := output.(api.YAMLPayload)

	cm, err := gaby.ParseAll([]byte(payload.Payload))
	require.NoError(t, err)
	require.Equal(t, 1, len(cm))

	dataField := cm[0].Path("data")
	require.NotNil(t, dataField)
	dataMap, _ := dataField.Data().(map[string]any)
	require.NotNil(t, dataMap)
	// Key-value mode: each env var becomes its own key in data.
	host, _ := dataMap["DATABASE_HOST"].(string)
	port, _ := dataMap["DATABASE_PORT"].(string)
	assert.Equal(t, "localhost", host)
	assert.Equal(t, "5432", port)
	// configHub.configName should not appear as a data key.
	_, present := dataMap["configHub.configName"]
	assert.False(t, present)
}

func TestRenderConfigMap_JSONUsesConfigName(t *testing.T) {
	rp := jsonkit.NewJSONResourceProvider()
	var converter configkit.ConfigConverter = rp
	var resourceProvider yamlkit.ResourceProvider = rp

	native := `{"configHub":{"configName":"app","configSchema":"S"},"foo":"bar"}`
	parsed := parseNative(t, converter, native)
	fc := newFunctionContext(workerapi.ToolchainAppConfigJSON, "json-app", 1)
	args := []api.FunctionArgument{{ParameterName: "immutable", Value: true}}

	_, output, err := fnRenderConfigMap(converter, resourceProvider, fc, parsed, args)
	require.NoError(t, err)
	payload := output.(api.YAMLPayload)

	cm, err := gaby.ParseAll([]byte(payload.Payload))
	require.NoError(t, err)
	require.Equal(t, 1, len(cm))

	dataField := cm[0].Path("data")
	require.NotNil(t, dataField)
	dataMap, _ := dataField.Data().(map[string]any)
	require.NotNil(t, dataMap)
	body, _ := dataMap["app.json"].(string)
	require.NotEmpty(t, body, "expected app.json (configName + extension) as data key")
	assert.Contains(t, body, `"foo"`)
	assert.NotContains(t, body, `configName`)
}
