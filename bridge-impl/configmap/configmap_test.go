// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package configmap

import (
	"strings"
	"testing"

	"github.com/confighub/sdk/core/worker/api"
	"github.com/confighub/sdk/core/workerapi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const testEnvData = `APP_NAME=MyApplication
DATABASE_HOST=localhost
DATABASE_PORT=5432
configHub.configName=MyApplicationConfig
configHub.configSchema=SimpleApp
`

func makeEnvPayload(targetOptions map[string]string) api.BridgeWorkerPayload {
	return api.BridgeWorkerPayload{
		ToolchainType: workerapi.ToolchainAppConfigEnv,
		UnitSlug:      "my-config",
		SpaceSlug:     "test-space",
		RevisionNum:   1,
		Data:          []byte(testEnvData),
		TargetOptions: targetOptions,
	}
}

func TestTransformAppConfigToConfigMap_EnvFileMode(t *testing.T) {
	initFunctionExecutor()
	payload := makeEnvPayload(nil)
	err := transformAppConfigToConfigMap(&payload)
	require.NoError(t, err)

	output := string(payload.Data)

	// Verify shared metadata
	assert.Contains(t, output, "apiVersion: v1")
	assert.Contains(t, output, "kind: ConfigMap")
	assert.Contains(t, output, "namespace: confighubplaceholder")
	assert.Contains(t, output, "immutable: true")
	assert.Contains(t, output, "confighub.com/UnitSlug: my-config")

	// Verify file-style data entry
	assert.Contains(t, output, "MyApplicationConfig.env: |")

	// configHub metadata should be stripped from the data
	assert.NotContains(t, output, "configHub.configName")
	assert.NotContains(t, output, "configHub.configSchema")
	// App data should be present as raw env lines inside the block scalar
	assert.Contains(t, output, "APP_NAME=MyApplication")
	assert.Contains(t, output, "DATABASE_HOST=localhost")
	assert.Contains(t, output, "DATABASE_PORT=5432")
}

func TestTransformAppConfigToConfigMap_EnvKeyValueMode(t *testing.T) {
	initFunctionExecutor()
	payload := makeEnvPayload(map[string]string{"AsKeyValue": "true"})
	err := transformAppConfigToConfigMap(&payload)
	require.NoError(t, err)

	output := string(payload.Data)

	// Verify shared metadata
	assert.Contains(t, output, "apiVersion: v1")
	assert.Contains(t, output, "kind: ConfigMap")
	assert.Contains(t, output, "namespace: confighubplaceholder")
	assert.Contains(t, output, "immutable: true")
	assert.Contains(t, output, "confighub.com/UnitSlug: my-config")

	// Should NOT have a file-style entry
	assert.NotContains(t, output, "MyApplicationConfig.env: |")

	// configHub metadata should be stripped
	assert.NotContains(t, output, "configHub")
	assert.NotContains(t, output, "configName")
	assert.NotContains(t, output, "configSchema")

	// Should have individual key-value entries under data:
	// Find the data: section and verify entries
	dataIdx := strings.Index(output, "data:\n")
	require.NotEqual(t, -1, dataIdx, "should have data: section")
	dataSection := output[dataIdx:]
	assert.Contains(t, dataSection, "APP_NAME:")
	assert.Contains(t, dataSection, "MyApplication")
	assert.Contains(t, dataSection, "DATABASE_HOST:")
	assert.Contains(t, dataSection, "localhost")
	assert.Contains(t, dataSection, "DATABASE_PORT:")
	assert.Contains(t, dataSection, "5432")

	// Values must be quoted strings (ConfigMap data is map[string]string)
	assert.Contains(t, dataSection, `DATABASE_PORT: "5432"`)
}

func TestTransformAppConfigToConfigMap_EnvPlaceholderNamespace(t *testing.T) {
	initFunctionExecutor()
	// Namespace is always the placeholder — resolved at the ConfigMap unit level via links
	envData := `APP_NAME=MyApp
configHub.configName=TestConfig
configHub.configSchema=SimpleApp
`
	payload := api.BridgeWorkerPayload{
		ToolchainType: workerapi.ToolchainAppConfigEnv,
		UnitSlug:      "my-unit",
		Data:          []byte(envData),
	}
	err := transformAppConfigToConfigMap(&payload)
	require.NoError(t, err)
	assert.Contains(t, string(payload.Data), "namespace: confighubplaceholder")
}

func TestTransformAppConfigToConfigMap_MutableMode(t *testing.T) {
	initFunctionExecutor()
	payload := makeEnvPayload(map[string]string{"RevisionHistoryLimit": "0"})
	err := transformAppConfigToConfigMap(&payload)
	require.NoError(t, err)

	output := string(payload.Data)

	// Verify shared metadata
	assert.Contains(t, output, "apiVersion: v1")
	assert.Contains(t, output, "kind: ConfigMap")
	assert.Contains(t, output, "namespace: confighubplaceholder")
	assert.Contains(t, output, "confighub.com/UnitSlug: my-config")

	// Mutable mode: should NOT be immutable
	assert.NotContains(t, output, "immutable: true")

	// Name should be just the namePrefix (no hash suffix)
	assert.Contains(t, output, "  name: my-config\n")

	// Hash annotation should be present
	assert.Contains(t, output, "confighub.com/Hash:")

	// App data should still be present
	assert.Contains(t, output, "APP_NAME=MyApplication")
	assert.Contains(t, output, "DATABASE_HOST=localhost")
}

func TestTransformAppConfigToConfigMap_MutableKeyValueMode(t *testing.T) {
	initFunctionExecutor()
	payload := makeEnvPayload(map[string]string{"RevisionHistoryLimit": "0", "AsKeyValue": "true"})
	err := transformAppConfigToConfigMap(&payload)
	require.NoError(t, err)

	output := string(payload.Data)

	// Mutable mode: should NOT be immutable
	assert.NotContains(t, output, "immutable: true")

	// Name should be just the namePrefix
	assert.Contains(t, output, "  name: my-config\n")

	// Hash annotation should be present
	assert.Contains(t, output, "confighub.com/Hash:")

	// Should have key-value entries
	dataIdx := strings.Index(output, "data:\n")
	require.NotEqual(t, -1, dataIdx, "should have data: section")
	dataSection := output[dataIdx:]
	assert.Contains(t, dataSection, "APP_NAME:")
	assert.Contains(t, dataSection, "DATABASE_PORT:")
}

func TestTransformAppConfigToConfigMap_ImmutableModeHasHash(t *testing.T) {
	initFunctionExecutor()
	// Default (immutable) mode should also have the Hash annotation
	payload := makeEnvPayload(nil)
	err := transformAppConfigToConfigMap(&payload)
	require.NoError(t, err)

	output := string(payload.Data)
	assert.Contains(t, output, "immutable: true")
	assert.Contains(t, output, "confighub.com/Hash:")

	// Name should include hash suffix (not just "my-config")
	assert.NotContains(t, output, "  name: my-config\n")
}

func TestTransformAppConfigToConfigMap_AsKeyValueIgnoredForNonEnv(t *testing.T) {
	initFunctionExecutor()
	// AsKeyValue=true should be ignored for non-Env toolchains (file mode used)
	yamlData := `app:
  name: MyApplication
configHub:
  configName: MyApplicationConfig
  configSchema: SimpleApp
`
	payload := api.BridgeWorkerPayload{
		ToolchainType: workerapi.ToolchainAppConfigYAML,
		UnitSlug:      "my-yaml",
		Data:          []byte(yamlData),
		TargetOptions: map[string]string{"AsKeyValue": "true"},
	}
	err := transformAppConfigToConfigMap(&payload)
	require.NoError(t, err)

	output := string(payload.Data)
	// Should use file mode with .yaml extension
	assert.Contains(t, output, "MyApplicationConfig.yaml: |")
}
