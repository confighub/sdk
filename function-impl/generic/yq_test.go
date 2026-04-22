// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package generic

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/confighub/sdk/configkit/k8skit"
	"github.com/confighub/sdk/core/function/api"
	"github.com/confighub/sdk/core/third_party/gaby"
)

const multiDocK8sYAML = `apiVersion: apps/v1
kind: Deployment
metadata:
  name: web
  namespace: default
spec:
  replicas: 3
  template:
    spec:
      containers:
        - name: nginx
          image: nginx:1.25
---
apiVersion: v1
kind: Service
metadata:
  name: web-svc
  namespace: default
spec:
  type: ClusterIP
  ports:
    - port: 80
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: api
  namespace: default
spec:
  replicas: 2
  template:
    spec:
      containers:
        - name: app
          image: myapp:latest
`

// whereResourceType returns FunctionOptions filtering by ConfigHub.ResourceType.
func whereResourceType(resourceType string) *api.FunctionOptions {
	return &api.FunctionOptions{
		WhereResourceExpressions: []*api.VisitorRelationalExpression{
			{
				RelationalExpression: api.RelationalExpression{
					Path:     "ConfigHub.ResourceType",
					Operator: "=",
					Literal:  "'" + resourceType + "'",
					DataType: api.DataTypeString,
				},
			},
		},
	}
}

// whereMetadataName returns FunctionOptions filtering by metadata.name.
func whereMetadataName(name string) *api.FunctionOptions {
	return &api.FunctionOptions{
		WhereResourceExpressions: []*api.VisitorRelationalExpression{
			{
				RelationalExpression: api.RelationalExpression{
					Path:     "metadata.name",
					Operator: "=",
					Literal:  "'" + name + "'",
					DataType: api.DataTypeString,
				},
			},
		},
	}
}

func TestYQQuery_MultiDoc_NoFilter(t *testing.T) {
	rp := k8skit.NewK8sResourceProvider()
	parsedData, err := gaby.ParseAll([]byte(multiDocK8sYAML))
	require.NoError(t, err)
	require.Len(t, parsedData, 3)

	args := []api.FunctionArgument{{Value: ".metadata.name"}}
	_, output, err := genericFnYQQuery(rp, &api.FunctionContext{}, parsedData, args[0].Value.(string), nil)
	require.NoError(t, err)

	payload, ok := output.(api.YAMLPayload)
	require.True(t, ok)
	// Should contain all three resource names
	assert.Contains(t, payload.Payload, "web")
	assert.Contains(t, payload.Payload, "web-svc")
	assert.Contains(t, payload.Payload, "api")
}

func TestYQQuery_MultiDoc_WhereDeployment(t *testing.T) {
	rp := k8skit.NewK8sResourceProvider()
	parsedData, err := gaby.ParseAll([]byte(multiDocK8sYAML))
	require.NoError(t, err)

	args := []api.FunctionArgument{{Value: ".metadata.name"}}
	_, output, err := genericFnYQQuery(rp, &api.FunctionContext{}, parsedData, args[0].Value.(string), whereResourceType("apps/v1/Deployment"))
	require.NoError(t, err)

	payload := output.(api.YAMLPayload)
	// Should contain only Deployment names
	assert.Contains(t, payload.Payload, "web")
	assert.Contains(t, payload.Payload, "api")
	assert.NotContains(t, payload.Payload, "web-svc")
}

func TestYQQuery_MultiDoc_WhereSpecificResource(t *testing.T) {
	rp := k8skit.NewK8sResourceProvider()
	parsedData, err := gaby.ParseAll([]byte(multiDocK8sYAML))
	require.NoError(t, err)

	args := []api.FunctionArgument{{Value: ".spec.replicas"}}
	_, output, err := genericFnYQQuery(rp, &api.FunctionContext{}, parsedData, args[0].Value.(string), whereMetadataName("api"))
	require.NoError(t, err)

	payload := output.(api.YAMLPayload)
	assert.Contains(t, strings.TrimSpace(payload.Payload), "2")
	assert.NotContains(t, payload.Payload, "3")
}

func TestYQMutating_MultiDoc_NoFilter(t *testing.T) {
	rp := k8skit.NewK8sResourceProvider()
	parsedData, err := gaby.ParseAll([]byte(multiDocK8sYAML))
	require.NoError(t, err)

	args := []api.FunctionArgument{{Value: `.metadata.labels.app = "test"`}}
	result, output, err := genericFnYQMutating(rp, parsedData, args[0].Value.(string), nil)
	require.NoError(t, err)
	assert.Nil(t, output)
	require.Len(t, result, 3)

	// All resources should have the label
	resultYAML := result.String()
	assert.Equal(t, 3, strings.Count(resultYAML, "labels:"), "all three resources should have labels added")
}

func TestYQMutating_MultiDoc_WhereDeployment(t *testing.T) {
	rp := k8skit.NewK8sResourceProvider()
	parsedData, err := gaby.ParseAll([]byte(multiDocK8sYAML))
	require.NoError(t, err)

	args := []api.FunctionArgument{{Value: ".spec.replicas = 5"}}
	result, _, err := genericFnYQMutating(rp, parsedData, args[0].Value.(string), whereResourceType("apps/v1/Deployment"))
	require.NoError(t, err)
	// All 3 documents should be present
	require.Len(t, result, 3)

	resultYAML := result.String()
	// Deployments should have replicas: 5
	assert.Equal(t, 2, strings.Count(resultYAML, "replicas: 5"), "both deployments should have replicas set to 5")
	// Service should still be present unchanged
	assert.Contains(t, resultYAML, "kind: Service")
	assert.Contains(t, resultYAML, "web-svc")
}

func TestYQMutating_MultiDoc_WhereSpecificResource(t *testing.T) {
	rp := k8skit.NewK8sResourceProvider()
	parsedData, err := gaby.ParseAll([]byte(multiDocK8sYAML))
	require.NoError(t, err)

	args := []api.FunctionArgument{{Value: ".spec.replicas = 10"}}
	result, _, err := genericFnYQMutating(rp, parsedData, args[0].Value.(string), whereMetadataName("web"))
	require.NoError(t, err)
	require.Len(t, result, 3)

	resultYAML := result.String()
	// Only the "web" deployment should have replicas: 10
	assert.Contains(t, resultYAML, "replicas: 10")
	// The "api" deployment should still have replicas: 2
	assert.Contains(t, resultYAML, "replicas: 2")
	// Service should be unchanged
	assert.Contains(t, resultYAML, "kind: Service")
}

func TestYQQuery_SingleDoc(t *testing.T) {
	rp := k8skit.NewK8sResourceProvider()
	singleDoc := `apiVersion: v1
kind: ConfigMap
metadata:
  name: my-config
data:
  key1: value1
  key2: value2
`
	parsedData, err := gaby.ParseAll([]byte(singleDoc))
	require.NoError(t, err)

	args := []api.FunctionArgument{{Value: ".data.key1"}}
	_, output, err := genericFnYQQuery(rp, &api.FunctionContext{}, parsedData, args[0].Value.(string), nil)
	require.NoError(t, err)

	payload := output.(api.YAMLPayload)
	assert.Contains(t, strings.TrimSpace(payload.Payload), "value1")
}

func TestYQMutating_PreservesUnmatchedDocs(t *testing.T) {
	rp := k8skit.NewK8sResourceProvider()
	parsedData, err := gaby.ParseAll([]byte(multiDocK8sYAML))
	require.NoError(t, err)
	require.Len(t, parsedData, 3)

	// Filter to only Service, mutate it
	args := []api.FunctionArgument{{Value: ".spec.type = \"NodePort\""}}
	result, _, err := genericFnYQMutating(rp, parsedData, args[0].Value.(string), whereResourceType("v1/Service"))
	require.NoError(t, err)
	// All 3 docs should be present
	require.Len(t, result, 3)

	resultYAML := result.String()
	// Service should be mutated
	assert.Contains(t, resultYAML, "type: NodePort")
	// Deployments should be unchanged
	assert.Contains(t, resultYAML, "replicas: 3")
	assert.Contains(t, resultYAML, "replicas: 2")
}
