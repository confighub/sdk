// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package generic

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/confighub/sdk/configkit/k8skit"
	"github.com/confighub/sdk/core/function/api"
	"github.com/confighub/sdk/core/third_party/gaby"
)

// Two resources of the same type in one Unit, the case in which an unscoped
// attribute set fans out to both.
const twoDeployments = `apiVersion: apps/v1
kind: Deployment
metadata:
  name: web
  namespace: default
spec:
  replicas: 1
  template:
    spec:
      containers:
      - name: app
        image: ghcr.io/example/web:v1.2.3
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: worker
  namespace: default
spec:
  replicas: 1
  template:
    spec:
      containers:
      - name: app
        image: ghcr.io/example/worker:v9.9.9
`

// invokeSetAttributes runs set-attributes over the input config with the given
// JSON-encoded attribute list and returns the mutated config.
func invokeSetAttributes(t *testing.T, input, attributeListJSON string) gaby.Container {
	t.Helper()
	rp := k8skit.NewK8sResourceProvider()
	parsedData, err := gaby.ParseAll([]byte(input))
	require.NoError(t, err)

	args := []api.FunctionArgument{{ParameterName: "attribute-list", Value: attributeListJSON}}
	result, _, err := genericFnSetAttributes(rp, nil, parsedData, args, nil)
	require.NoError(t, err)
	return result
}

// valueAt returns the value at path in the resource named name.
func valueAt(t *testing.T, docs gaby.Container, name, path string) any {
	t.Helper()
	for _, doc := range docs {
		if n := doc.Path("metadata.name"); n != nil && n.Data() == name {
			p := doc.Path(path)
			require.NotNil(t, p, "path %s should exist in resource %s", path, name)
			return p.Data()
		}
	}
	t.Fatalf("resource %s not found", name)
	return nil
}

// An attribute that names a resource is set only on that resource, not on every
// resource of its type.
func TestSetAttributes_ScopedToNamedResource(t *testing.T) {
	out := invokeSetAttributes(t, twoDeployments, `[{
		"ResourceType": "apps/v1/Deployment",
		"ResourceName": "default/web",
		"ResourceNameWithoutScope": "web",
		"AttributeName": "spec.replicas",
		"Path": "spec.replicas",
		"DataType": "int",
		"Value": 5
	}]`)

	assert.Equal(t, 5, valueAt(t, out, "web", "spec.replicas"))
	assert.Equal(t, 1, valueAt(t, out, "worker", "spec.replicas"), "the resource the attribute didn't name is untouched")
}

// A string attribute naming a resource likewise doesn't reach its same-typed siblings.
func TestSetAttributes_ScopedToNamedResource_String(t *testing.T) {
	out := invokeSetAttributes(t, twoDeployments, `[{
		"ResourceType": "apps/v1/Deployment",
		"ResourceName": "default/worker",
		"ResourceNameWithoutScope": "worker",
		"AttributeName": "image",
		"Path": "spec.template.spec.containers.0.image",
		"DataType": "string",
		"Value": "ghcr.io/example/worker:v10.0.0"
	}]`)

	assert.Equal(t, "ghcr.io/example/worker:v10.0.0", valueAt(t, out, "worker", "spec.template.spec.containers.0.image"))
	assert.Equal(t, "ghcr.io/example/web:v1.2.3", valueAt(t, out, "web", "spec.template.spec.containers.0.image"))
}

// Each attribute is scoped independently, so one call can set the same path to
// different values in two resources of the same type.
func TestSetAttributes_PerAttributeScoping(t *testing.T) {
	out := invokeSetAttributes(t, twoDeployments, `[
		{
			"ResourceType": "apps/v1/Deployment",
			"ResourceName": "default/web",
			"Path": "spec.replicas",
			"DataType": "int",
			"Value": 5
		},
		{
			"ResourceType": "apps/v1/Deployment",
			"ResourceName": "default/worker",
			"Path": "spec.replicas",
			"DataType": "int",
			"Value": 3
		}
	]`)

	assert.Equal(t, 5, valueAt(t, out, "web", "spec.replicas"))
	assert.Equal(t, 3, valueAt(t, out, "worker", "spec.replicas"))
}

// An attribute that names no resource still applies to every resource of its type.
func TestSetAttributes_WithoutResourceNameAppliesToType(t *testing.T) {
	out := invokeSetAttributes(t, twoDeployments, `[{
		"ResourceType": "apps/v1/Deployment",
		"Path": "spec.replicas",
		"DataType": "int",
		"Value": 5
	}]`)

	assert.Equal(t, 5, valueAt(t, out, "web", "spec.replicas"))
	assert.Equal(t, 5, valueAt(t, out, "worker", "spec.replicas"))
}

// A Deployment and a Service that share the name default/web, with a path in common.
const sameNameDifferentTypes = `apiVersion: apps/v1
kind: Deployment
metadata:
  name: web
  namespace: default
  labels:
    tier: old
---
apiVersion: v1
kind: Service
metadata:
  name: web
  namespace: default
  labels:
    tier: old
`

// valueAtKind returns the value at path in the resource of the given kind.
func valueAtKind(t *testing.T, docs gaby.Container, kind, path string) any {
	t.Helper()
	for _, doc := range docs {
		if k := doc.Path("kind"); k != nil && k.Data() == kind {
			p := doc.Path(path)
			require.NotNil(t, p, "path %s should exist in %s", path, kind)
			return p.Data()
		}
	}
	t.Fatalf("kind %s not found", kind)
	return nil
}

// A typed attribute reaches only the same-named resource of its own type.
func TestSetAttributes_SharedNameDifferentTypes(t *testing.T) {
	out := invokeSetAttributes(t, sameNameDifferentTypes, `[{
		"ResourceType": "v1/Service",
		"ResourceName": "default/web",
		"Path": "metadata.labels.tier",
		"DataType": "string",
		"Value": "new"
	}]`)

	assert.Equal(t, "new", valueAtKind(t, out, "Service", "metadata.labels.tier"))
	assert.Equal(t, "old", valueAtKind(t, out, "Deployment", "metadata.labels.tier"),
		"the same-named resource of another type is untouched")
}

// An attribute typed ResourceTypeAny names a resource of any type, so it reaches every
// resource carrying the name — but still no resource that doesn't carry it.
func TestSetAttributes_SharedNameAnyType(t *testing.T) {
	out := invokeSetAttributes(t, sameNameDifferentTypes, `[{
		"ResourceType": "*",
		"ResourceName": "default/web",
		"Path": "metadata.labels.tier",
		"DataType": "string",
		"Value": "new"
	}]`)

	assert.Equal(t, "new", valueAtKind(t, out, "Service", "metadata.labels.tier"))
	assert.Equal(t, "new", valueAtKind(t, out, "Deployment", "metadata.labels.tier"))
}

// An attribute naming a resource the Unit doesn't have writes nothing, rather than
// falling back to every resource of its type.
func TestSetAttributes_UnknownResourceNameWritesNothing(t *testing.T) {
	out := invokeSetAttributes(t, twoDeployments, `[{
		"ResourceType": "apps/v1/Deployment",
		"ResourceName": "default/absent",
		"Path": "spec.replicas",
		"DataType": "int",
		"Value": 5
	}]`)

	assert.Equal(t, 1, valueAt(t, out, "web", "spec.replicas"))
	assert.Equal(t, 1, valueAt(t, out, "worker", "spec.replicas"))
}

// Resource names are resolved before any attribute is applied, so an attribute that
// renames a resource doesn't strand the attributes that follow it. This is the shape
// search-replace produces when the value it rewrites also appears in a name.
func TestSetAttributes_RenameDoesNotStrandLaterAttributes(t *testing.T) {
	out := invokeSetAttributes(t, twoDeployments, `[
		{
			"ResourceType": "apps/v1/Deployment",
			"ResourceName": "default/web",
			"Path": "metadata.name",
			"DataType": "string",
			"Value": "web-renamed"
		},
		{
			"ResourceType": "apps/v1/Deployment",
			"ResourceName": "default/web",
			"Path": "spec.replicas",
			"DataType": "int",
			"Value": 5
		}
	]`)

	assert.Equal(t, 5, valueAt(t, out, "web-renamed", "spec.replicas"),
		"the attribute after the rename still reaches the resource it names")
	assert.Equal(t, 1, valueAt(t, out, "worker", "spec.replicas"))
}

// The attribute's resource scope is AND-ed with the caller's WhereResource filter
// rather than replacing it: a filter that excludes the named resource wins.
func TestSetAttributes_CombinesWithWhereResource(t *testing.T) {
	rp := k8skit.NewK8sResourceProvider()
	parsedData, err := gaby.ParseAll([]byte(twoDeployments))
	require.NoError(t, err)

	whereExpressions, err := api.ParseAndValidateWhereResource("ConfigHub.ResourceNameWithoutScope='worker'")
	require.NoError(t, err)
	options := &api.FunctionOptions{WhereResourceExpressions: whereExpressions}

	args := []api.FunctionArgument{{ParameterName: "attribute-list", Value: `[{
		"ResourceType": "apps/v1/Deployment",
		"ResourceName": "default/web",
		"Path": "spec.replicas",
		"DataType": "int",
		"Value": 5
	}]`}}
	out, _, err := genericFnSetAttributes(rp, nil, parsedData, args, options)
	require.NoError(t, err)

	assert.Equal(t, 1, valueAt(t, out, "web", "spec.replicas"), "excluded by the WhereResource filter")
	assert.Equal(t, 1, valueAt(t, out, "worker", "spec.replicas"), "not named by the attribute")

	// The caller's options are not mutated by the per-attribute scoping.
	assert.Equal(t, whereExpressions, options.WhereResourceExpressions)
}

// search-replace shares set-attributes' code path: the attributes it derives carry
// the name of the resource each match was found in, so a replacement lands only in
// the resource that matched.
func TestSearchReplace_DoesNotCrossResources(t *testing.T) {
	rp := k8skit.NewK8sResourceProvider()
	parsedData, err := gaby.ParseAll([]byte(twoDeployments))
	require.NoError(t, err)

	out, _, err := genericFnSearchReplace(rp, nil, parsedData, []api.FunctionArgument{
		{ParameterName: "search-value", Value: "v1.2.3"},
		{ParameterName: "replace-value", Value: "v2.0.0"},
	}, nil)
	require.NoError(t, err)

	assert.Equal(t, "ghcr.io/example/web:v2.0.0", valueAt(t, out, "web", "spec.template.spec.containers.0.image"))
	assert.Equal(t, "ghcr.io/example/worker:v9.9.9", valueAt(t, out, "worker", "spec.template.spec.containers.0.image"),
		"the resource with no match keeps its own image")
}
