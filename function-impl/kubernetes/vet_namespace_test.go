// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package kubernetes

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/confighub/sdk/core/configkit/yamlkit"
	"github.com/confighub/sdk/core/function/api"
	"github.com/confighub/sdk/core/third_party/gaby"
)

// namespaceFixture holds a resource of each kind vet-namespace distinguishes.
const namespaceFixture = `apiVersion: apps/v1
kind: Deployment
metadata:
  name: no-namespace
---
apiVersion: v1
kind: ConfigMap
metadata:
  name: empty-namespace
  namespace: ""
---
apiVersion: v1
kind: Service
metadata:
  name: namespaced
  namespace: shop
---
apiVersion: v1
kind: Secret
metadata:
  name: placeholder
  namespace: confighubplaceholder
---
apiVersion: v1
kind: Namespace
metadata:
  name: shop
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: reader
---
apiVersion: example.com/v1
kind: Widget
metadata:
  name: undeclared
`

func vetNamespace(t *testing.T, fixture string, args ...api.FunctionArgument) api.ValidationResult {
	t.Helper()
	docs, err := gaby.ParseAll([]byte(fixture))
	require.NoError(t, err)
	_, output, err := k8sFnVetNamespace(testResourceProvider, &api.FunctionOptions{}, docs, args)
	require.NoError(t, err)
	result, ok := output.(api.ValidationResult)
	require.True(t, ok)
	return result
}

// failedResources are the resources of a result's failed attributes, as type and name.
func failedResources(result api.ValidationResult) []string {
	resources := []string{}
	for _, failed := range result.FailedAttributes {
		resources = append(resources, string(failed.ResourceType)+" "+string(failed.ResourceName))
	}
	return resources
}

// A resource that is not known to be cluster-scoped fails at metadata.namespace when it sets none,
// whether the field is missing or empty. A placeholder is a namespace, which vet-placeholders reports.
func TestVetNamespace(t *testing.T) {
	result := vetNamespace(t, namespaceFixture)
	assert.False(t, result.Passed)
	assert.Equal(t, []string{
		"apps/v1/Deployment /no-namespace",
		"v1/ConfigMap /empty-namespace",
		"example.com/v1/Widget /undeclared",
	}, failedResources(result))
	assert.Equal(t, []string{
		"apps/v1/Deployment no-namespace sets no metadata.namespace",
		"v1/ConfigMap empty-namespace sets no metadata.namespace",
		"example.com/v1/Widget undeclared sets no metadata.namespace",
	}, result.Details)
	for _, failed := range result.FailedAttributes {
		assert.Equal(t, api.ResolvedPath("metadata.namespace"), failed.Path)
		assert.Equal(t, AttributeNameNamespaceNameReference, failed.AttributeName)
		assert.Equal(t, api.DataTypeString, failed.DataType)
	}
	assert.Nil(t, result.FailedAttributes[0].Value, "a missing namespace has no value")
	assert.Equal(t, "", result.FailedAttributes[1].Value, "an empty one is reported as it is")
}

// cluster-scoped-types adds to the types known to be cluster-scoped, in any API version.
func TestVetNamespaceClusterScopedTypes(t *testing.T) {
	result := vetNamespace(t, namespaceFixture, api.FunctionArgument{
		ParameterName: "cluster-scoped-types", Value: "example.com/v2/Widget,apps/v1/Deployment",
	})
	assert.Equal(t, []string{"v1/ConfigMap /empty-namespace"}, failedResources(result))

	result = vetNamespace(t, "apiVersion: v1\nkind: Namespace\nmetadata:\n  name: shop\n")
	assert.True(t, result.Passed)
	assert.Empty(t, result.FailedAttributes)
}

// vet-namespace fails exactly the resources ensure-namespaces gives a namespace, which then pass.
func TestVetNamespaceMatchesEnsureNamespaces(t *testing.T) {
	docs, err := gaby.ParseAll([]byte(namespaceFixture))
	require.NoError(t, err)
	before := vetNamespace(t, namespaceFixture)

	_, _, err = k8sFnEnsureNamespaces(testResourceProvider, &api.FunctionOptions{}, docs, nil)
	require.NoError(t, err)
	added := []string{}
	for _, doc := range docs {
		namespace, _, _ := yamlkit.YamlSafePathGetValue[string](doc, "metadata.namespace", true)
		if namespace == yamlkit.PlaceHolderBlockApplyString {
			kind, _, _ := yamlkit.YamlSafePathGetValue[string](doc, "kind", true)
			name, _, _ := yamlkit.YamlSafePathGetValue[string](doc, "metadata.name", true)
			added = append(added, kind+"/"+name)
		}
	}
	assert.Equal(t, []string{"Deployment/no-namespace", "ConfigMap/empty-namespace", "Secret/placeholder", "Widget/undeclared"}, added,
		"ensure-namespaces adds the placeholder where vet-namespace fails; the Secret already had it")
	assert.Len(t, before.FailedAttributes, 3)

	after := vetNamespace(t, docs.String())
	assert.True(t, after.Passed)
}
