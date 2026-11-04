// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package kubernetes

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/confighub/sdk/core/function/api"
	"github.com/confighub/sdk/core/third_party/gaby"
)

// appEnvSchema is the kind of schema an AppConfig/Env Unit is validated against: it requires
// a variable no single resource below carries, and allows no others.
const appEnvSchema = `{
	"type": "object",
	"required": ["DATABASE_PASSWORD"],
	"additionalProperties": false,
	"properties": {
		"LOG_LEVEL": {"type": "string", "enum": ["debug", "info"]},
		"PORT": {"type": "string", "pattern": "^[0-9]+$"},
		"FEATURE.FLAG": {"type": "string"},
		"DATABASE_PASSWORD": {"type": "string"}
	}
}`

func vetEnv(t *testing.T, fixture string, args ...any) api.ValidationResult {
	t.Helper()
	return vetEnvWithSchema(t, appEnvSchema, fixture, args...)
}

func vetEnvWithSchema(t *testing.T, schema string, fixture string, args ...any) api.ValidationResult {
	t.Helper()
	docs, err := gaby.ParseAll([]byte(fixture))
	require.NoError(t, err)
	functionArgs := []api.FunctionArgument{{Value: schema}}
	for _, arg := range args {
		functionArgs = append(functionArgs, api.FunctionArgument{Value: arg})
	}
	_, output, err := k8sFnVetEnv(testResourceProvider, &api.FunctionOptions{}, docs, functionArgs)
	require.NoError(t, err)
	result, ok := output.(api.ValidationResult)
	require.True(t, ok)
	return result
}

func failedPaths(t *testing.T, result api.ValidationResult) []string {
	t.Helper()
	paths := []string{}
	for _, failed := range result.FailedAttributes {
		paths = append(paths, string(failed.Path))
		if failed.Path != "data" && !strings.HasSuffix(string(failed.Path), ".env") {
			assert.Equal(t, api.DataTypeString, failed.DataType, "data type at %s", failed.Path)
		}
	}
	return paths
}

func TestVetEnv_ConfigMap(t *testing.T) {
	t.Run("valid data passes without the required variable", func(t *testing.T) {
		result := vetEnv(t, `apiVersion: v1
kind: ConfigMap
metadata:
  name: app-env
data:
  LOG_LEVEL: debug
  PORT: "8080"
  FEATURE.FLAG: "on"
`)
		assert.True(t, result.Passed, "details: %v", result.Details)
	})

	t.Run("invalid and unknown variables fail at their keys", func(t *testing.T) {
		result := vetEnv(t, `apiVersion: v1
kind: ConfigMap
metadata:
  name: app-env
data:
  LOG_LEVEL: verbose
  UNKNOWN.VAR: x
`)
		assert.False(t, result.Passed)
		assert.ElementsMatch(t, []string{"data.LOG_LEVEL", "data.UNKNOWN~1VAR"}, failedPaths(t, result))
		for _, failed := range result.FailedAttributes {
			if failed.Path == "data.LOG_LEVEL" {
				assert.Equal(t, "verbose", failed.Value)
			}
		}
	})

	t.Run("a File ConfigMap is not checked", func(t *testing.T) {
		result := vetEnv(t, `apiVersion: v1
kind: ConfigMap
metadata:
  name: app-config
  annotations:
    confighub.com/ConfigMapFormat: File
data:
  app.env: |
    LOG_LEVEL=verbose
`)
		assert.True(t, result.Passed, "details: %v", result.Details)
	})

	t.Run("a KeyValue ConfigMap is checked", func(t *testing.T) {
		result := vetEnv(t, `apiVersion: v1
kind: ConfigMap
metadata:
  name: app-env
  annotations:
    confighub.com/ConfigMapFormat: KeyValue
data:
  LOG_LEVEL: verbose
`)
		assert.False(t, result.Passed)
		assert.Equal(t, []string{"data.LOG_LEVEL"}, failedPaths(t, result))
	})

	t.Run("binaryData is not checked", func(t *testing.T) {
		result := vetEnv(t, `apiVersion: v1
kind: ConfigMap
metadata:
  name: app-env
binaryData:
  blob: aGVsbG8=
`)
		assert.True(t, result.Passed, "details: %v", result.Details)
	})
}

const envDeployment = `apiVersion: apps/v1
kind: Deployment
metadata:
  name: app
spec:
  template:
    spec:
      initContainers:
      - name: migrate
        env:
        - name: PORT
          value: eighty
      containers:
      - name: app
        env:
        - name: LOG_LEVEL
          value: trace
        - name: DATABASE_PASSWORD
          valueFrom:
            secretKeyRef: {name: app, key: password}
        - name: FEATURE.FLAG
        envFrom:
        - configMapRef: {name: app-env}
      - name: proxy
        image: proxy
`

func TestVetEnv_Deployment(t *testing.T) {
	t.Run("every container with env is checked", func(t *testing.T) {
		result := vetEnv(t, envDeployment)
		assert.False(t, result.Passed)
		assert.ElementsMatch(t, []string{
			"spec.template.spec.initContainers.?name=migrate.env.?name=PORT.value",
			"spec.template.spec.containers.?name=app.env.?name=LOG_LEVEL.value",
		}, failedPaths(t, result))
	})

	t.Run("container-name selects one container", func(t *testing.T) {
		result := vetEnv(t, envDeployment, "app")
		assert.False(t, result.Passed)
		assert.Equal(t, []string{"spec.template.spec.containers.?name=app.env.?name=LOG_LEVEL.value"}, failedPaths(t, result))
		assert.Equal(t, "trace", result.FailedAttributes[0].Value)

		result = vetEnv(t, envDeployment, "proxy")
		assert.True(t, result.Passed, "details: %v", result.Details)
	})

	t.Run("a variable from valueFrom has its name checked but not its value", func(t *testing.T) {
		result := vetEnv(t, `apiVersion: v1
kind: Pod
metadata:
  name: app
spec:
  containers:
  - name: app
    env:
    - name: DATABASE_PASSWORD
      valueFrom:
        secretKeyRef: {name: app, key: password}
    - name: SECRET_NOT_IN_SCHEMA
      valueFrom:
        secretKeyRef: {name: app, key: secret}
    - name: LOG_LEVEL
      value: info
`)
		assert.False(t, result.Passed)
		assert.Equal(t, []string{"spec.containers.?name=app.env.?name=SECRET_NOT_IN_SCHEMA.name"}, failedPaths(t, result))
		assert.Equal(t, "SECRET_NOT_IN_SCHEMA", result.FailedAttributes[0].Value)
	})

	t.Run("a literal value after valueFrom is checked", func(t *testing.T) {
		result := vetEnv(t, `apiVersion: v1
kind: Pod
metadata:
  name: app
spec:
  containers:
  - name: app
    env:
    - name: LOG_LEVEL
      valueFrom:
        configMapKeyRef: {name: app, key: level}
    - name: LOG_LEVEL
      value: trace
`)
		assert.False(t, result.Passed)
		assert.Equal(t, []string{"spec.containers.?name=app.env.?name=LOG_LEVEL.value"}, failedPaths(t, result))
	})

	t.Run("other resource types are not checked", func(t *testing.T) {
		result := vetEnv(t, `apiVersion: v1
kind: Service
metadata:
  name: app
spec:
  ports:
  - port: 80
`)
		assert.True(t, result.Passed, "details: %v", result.Details)
	})
}

func TestVetEnv_SchemaAtTheRoot(t *testing.T) {
	t.Run("a root combinator does not see a valueFrom variable's value", func(t *testing.T) {
		// Either A, if set, has a string value, or B is set. A's value is elsewhere, so it
		// is as if A were absent, and the first branch holds.
		schema := `{"anyOf": [
			{"properties": {"A": {"type": "string"}}},
			{"required": ["B"]}
		]}`
		result := vetEnvWithSchema(t, schema, `apiVersion: v1
kind: Pod
metadata:
  name: app
spec:
  containers:
  - name: app
    env:
    - name: A
      valueFrom:
        secretKeyRef: {name: app, key: a}
    - name: C
      value: c
`)
		assert.True(t, result.Passed, "details: %v", result.Details)
	})

	t.Run("propertyNames reports each bad name once, with why", func(t *testing.T) {
		schema := `{"propertyNames": {"pattern": "^[A-Z_]+$"}}`
		result := vetEnvWithSchema(t, schema, `apiVersion: v1
kind: Pod
metadata:
  name: app
spec:
  containers:
  - name: app
    env:
    - name: GOOD
      value: x
    - name: bad-literal
      value: x
    - name: bad-secret
      valueFrom:
        secretKeyRef: {name: app, key: s}
`)
		assert.False(t, result.Passed)
		assert.ElementsMatch(t, []string{
			"spec.containers.?name=app.env.?name=bad-literal.name",
			"spec.containers.?name=app.env.?name=bad-secret.name",
		}, failedPaths(t, result))
		require.Len(t, result.Details, 2)
		for _, detail := range result.Details {
			assert.Contains(t, detail, "Does not match pattern")
		}
	})
}

func TestVetEnv_InvalidSchema(t *testing.T) {
	docs, err := gaby.ParseAll([]byte(envDeployment))
	require.NoError(t, err)
	_, _, err = k8sFnVetEnv(testResourceProvider, &api.FunctionOptions{}, docs, []api.FunctionArgument{{Value: "{not json"}})
	assert.Error(t, err)
}
