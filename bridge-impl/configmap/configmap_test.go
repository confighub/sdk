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
configHub.kubernetes.namespace=test-ns
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
	assert.Contains(t, output, "namespace: test-ns")
	assert.Contains(t, output, "immutable: true")
	assert.Contains(t, output, "confighub.com/UnitSlug: my-config")

	// Verify file-style data entry
	assert.Contains(t, output, "MyApplicationConfig.env: |")

	// configHub metadata should be stripped from the data
	assert.NotContains(t, output, "configHub.configName")
	assert.NotContains(t, output, "configHub.configSchema")
	assert.NotContains(t, output, "configHub.kubernetes.namespace")

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
	assert.Contains(t, output, "namespace: test-ns")
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

func TestTransformAppConfigToConfigMap_EnvDefaultNamespace(t *testing.T) {
	initFunctionExecutor()
	// Env data without a namespace should default to "default"
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
	assert.Contains(t, string(payload.Data), "namespace: default")
}

func TestTransformAppConfigToConfigMap_AsKeyValueIgnoredForNonEnv(t *testing.T) {
	initFunctionExecutor()
	// AsKeyValue=true should be ignored for non-Env toolchains (file mode used)
	yamlData := `app:
  name: MyApplication
configHub:
  configName: MyApplicationConfig
  configSchema: SimpleApp
  kubernetes:
    namespace: test-ns
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
