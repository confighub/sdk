// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package kubernetes

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/confighub/sdk/configkit/k8skit"
	"github.com/confighub/sdk/core/configkit/yamlkit"
	"github.com/confighub/sdk/core/function/api"
)

// providesMetadataName reports whether metadata.name is registered as a Provides for
// the given resource type with ProvidedProperties[ResourceType] equal to that type.
// This is what lets a resource of that type satisfy a name reference to it.
func providesMetadataName(resourceType api.ResourceType) bool {
	provided := yamlkit.GetRegisteredProvidedPaths(testResourceProvider)
	pathMap, ok := provided[resourceType]
	if !ok {
		return false
	}
	info, ok := pathMap["metadata.name"]
	if !ok || info.Details == nil {
		return false
	}
	return info.Details.ProvidedProperties["ResourceType"] == string(resourceType)
}

// TestReferenceProvides_DeclaredTargetsProvideMetadataName verifies that every resource type
// the specs declare a reference for -- both the declaring types and the targets -- registers
// metadata.name as a Provides. Before this was added, references to targets that are not
// kustomize NameReferenceFieldSpecs targets (e.g. Traefik Middleware) could never resolve
// because nothing provided their name.
func TestReferenceProvides_DeclaredTargetsProvideMetadataName(t *testing.T) {
	targets := map[api.ResourceType]struct{}{}
	for _, reference := range k8skit.DeclaredReferences() {
		targets[reference.ResourceType] = struct{}{}
		targets[reference.Target] = struct{}{}
	}
	for target := range targets {
		assert.True(t, providesMetadataName(target),
			"expected metadata.name to be provided for %s", target)
	}
}

// TestReferenceProvides_MiddlewareUnblocksIngressRoute is the concrete regression: the
// IngressRoute spec.routes.*.middlewares.*.name reference targets a Middleware, and the
// Middleware must provide its own metadata.name for that reference to resolve.
func TestReferenceProvides_MiddlewareUnblocksIngressRoute(t *testing.T) {
	const middleware = api.ResourceType("traefik.io/v1alpha1/Middleware")
	assert.True(t, providesMetadataName(middleware),
		"Middleware must provide metadata.name so IngressRoute middleware references resolve")

	// The reciprocal Needs must carry the same ResourceType property, which is what the
	// resolver matches on.
	needed := yamlkit.GetPathRegistryForAttributeNameByProperty(
		testResourceProvider, api.AttributeNameResourceName, api.PropertyKeyResourceType, string(middleware))
	irPaths, ok := needed[api.ResourceType("traefik.io/v1alpha1/IngressRoute")]
	assert.True(t, ok, "IngressRoute should have paths needing a %s", middleware)
	info, ok := irPaths["spec.routes.*.middlewares.*.name"]
	assert.True(t, ok, "IngressRoute should need spec.routes.*.middlewares.*.name")
	if ok && info.Details != nil {
		assert.Equal(t, string(middleware), info.Details.NeededRequired[api.PropertyKeyResourceType])
	}
}

// TestReferenceProvides_BuiltinServiceStillProvides guards against regressing the kustomize
// NameReferenceFieldSpecs path: v1/Service (and other built-ins) must still provide their name.
func TestReferenceProvides_BuiltinServiceStillProvides(t *testing.T) {
	for _, rt := range []api.ResourceType{"v1/Service", "v1/ConfigMap", "v1/Secret", "v1/ServiceAccount"} {
		assert.True(t, providesMetadataName(rt), "expected %s to provide metadata.name", rt)
	}
}

// TestNeededNamespacePathsKeepTheirRequiredType covers a path registered under two attribute
// names: the namespace fields are registered once under namespace-name-reference, which is how
// get-namespace and set-namespace find them, and again under resource-name with the
// ResourceType property that makes them match a Namespace. Only the second registration states
// the property, so the requirement survives only if the needed-path view merges the two
// registrations. A needed path that reaches the resolver with no required properties matches
// any provided value at all, so losing it does not fail loudly -- it binds the wrong resource.
func TestNeededNamespacePathsKeepTheirRequiredType(t *testing.T) {
	needed := yamlkit.GetRegisteredNeededPaths(testResourceProvider)
	for _, testCase := range []struct {
		resourceType api.ResourceType
		path         api.UnresolvedPath
	}{
		{api.ResourceTypeAny, "metadata.namespace"},
		{"rbac.authorization.k8s.io/v1/RoleBinding", "subjects.*.|namespace"},
		{"rbac.authorization.k8s.io/v1/ClusterRoleBinding", "subjects.*.|namespace"},
		{"apiregistration.k8s.io/v1/APIService", "spec.service.|namespace"},
		{"apiextensions.k8s.io/v1/CustomResourceDefinition", "spec.conversion.webhook.clientConfig.service.|namespace"},
	} {
		info := needed[testCase.resourceType][testCase.path]
		if !assert.NotNil(t, info, "%s %s should be a needed path", testCase.resourceType, testCase.path) ||
			!assert.NotNil(t, info.Details, "%s %s has no details", testCase.resourceType, testCase.path) {
			continue
		}
		assert.Equal(t, "v1/Namespace", info.Details.NeededRequired[api.PropertyKeyResourceType],
			"%s %s must require a Namespace", testCase.resourceType, testCase.path)
	}
}

// TestGetPathsTakesPropertiesFromTheRegistry covers what a stored NeededPath contributes to a
// resolve. NeededPaths and ProvidedPaths were stored (#3785) before the properties matching reads
// existed (#4063), so a record stored between the two states no requirement at all -- and a
// needed path that requires nothing matches every provided value of its attribute. get-paths
// therefore takes the properties from the registry and carries across only what the record alone
// knows: which Link bound it, and what that value offered.
func TestGetPathsTakesPropertiesFromTheRegistry(t *testing.T) {
	const deployment = `apiVersion: apps/v1
kind: Deployment
metadata:
  name: mydep
  namespace: confighubplaceholder
spec:
  template:
    spec:
      containers:
        - name: app
          image: nginx
`
	boundLink := uuid.New()
	// A record as a pre-#4063 version stored it: no NeededRequired, and binding fields that
	// exist nowhere but here.
	stored := []api.AttributeValue{{
		AttributeInfo: api.AttributeInfo{
			AttributeIdentifier: api.AttributeIdentifier{
				ResourceInfo: api.ResourceInfo{
					ResourceName: "confighubplaceholder/mydep",
					ResourceType: "apps/v1/Deployment",
				},
				Path: "metadata.namespace",
			},
			AttributeMetadata: api.AttributeMetadata{
				AttributeName: api.AttributeNameResourceName,
				DataType:      api.DataTypeString,
				Details: &api.AttributeDetails{AttributeNeedsProvidesDetails: api.AttributeNeedsProvidesDetails{
					IsNeeded:                true,
					BoundLinkID:             boundLink,
					BoundProvidedProperties: map[string]string{"ResourceType": "v1/Namespace"},
				}},
			},
		},
		Value: "confighubplaceholder",
	}}
	storedJSON, err := json.Marshal(stored)
	require.NoError(t, err)

	resp, err := testFunctionHandler.InvokeCore(context.Background(), &api.FunctionInvocationRequest{
		ConfigData: deployment,
		FunctionInvocations: []api.FunctionInvocation{{
			FunctionName: "get-paths",
			Arguments:    []api.FunctionArgument{{Value: string(storedJSON)}},
		}},
	})
	require.NoError(t, err)
	require.True(t, resp.Success, "get-paths should succeed; errors: %v", resp.ErrorMessages)

	out, ok := resp.Outputs[api.OutputTypeAttributeValueList]
	require.True(t, ok, "expected an AttributeValueList output")
	var values api.AttributeValueList
	require.NoError(t, json.Unmarshal(out, &values))
	require.Len(t, values, 1)

	require.NotNil(t, values[0].Details)
	assert.Equal(t, "v1/Namespace", values[0].Details.NeededRequired[api.PropertyKeyResourceType],
		"the requirement should come from the registry, not from the record that lacks it")
	assert.Equal(t, boundLink, values[0].Details.BoundLinkID,
		"which Link bound the path lives only in the stored record")
	assert.Equal(t, map[string]string{"ResourceType": "v1/Namespace"}, values[0].Details.BoundProvidedProperties,
		"what the bound value offered lives only in the stored record")
}

// A ClusterRoleBinding's subject names its ServiceAccount's namespace beside its name. The
// reference requires that namespace, and a ServiceAccount's name offers its own, so matching can
// tell two same-named ServiceAccounts apart and a cluster-scoped binding can match at all. A
// placeholder namespace requires nothing, and a subject that is not a ServiceAccount has none.
func TestSubjectReferencesRequireTheirServiceAccountsNamespace(t *testing.T) {
	const bundle = `apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRoleBinding
metadata:
  name: worker-admin
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: ClusterRole
  name: cluster-admin
subjects:
  - kind: ServiceAccount
    name: worker
    namespace: workers
  - kind: ServiceAccount
    name: undecided
    namespace: confighubplaceholder
---
apiVersion: v1
kind: ServiceAccount
metadata:
  name: worker
  namespace: workers
`
	invoke := func(fn string) api.AttributeValueList {
		t.Helper()
		resp, err := testFunctionHandler.InvokeCore(context.Background(), &api.FunctionInvocationRequest{
			ConfigData:          bundle,
			FunctionInvocations: []api.FunctionInvocation{{FunctionName: fn}},
		})
		require.NoError(t, err)
		require.True(t, resp.Success, "%s should succeed; errors: %v", fn, resp.ErrorMessages)
		var values api.AttributeValueList
		require.NoError(t, json.Unmarshal(resp.Outputs[api.OutputTypeAttributeValueList], &values))
		return values
	}

	required := map[string]map[string]string{}
	for _, v := range invoke("get-references") {
		if v.ResourceType == "rbac.authorization.k8s.io/v1/ClusterRoleBinding" && v.Details != nil {
			required[string(v.Path)] = v.Details.NeededRequired
		}
	}
	require.Contains(t, required, "subjects.0.name")
	assert.Equal(t, "v1/ServiceAccount", required["subjects.0.name"][api.PropertyKeyResourceType])
	assert.Equal(t, "workers", required["subjects.0.name"][api.PropertyKeyNamespace])
	require.Contains(t, required, "subjects.1.name")
	assert.NotContains(t, required["subjects.1.name"], api.PropertyKeyNamespace,
		"a placeholder namespace is not decided yet and requires nothing")

	var offered map[string]string
	for _, v := range invoke("get-provided") {
		if v.ResourceType == "v1/ServiceAccount" && v.Path == "metadata.name" && v.Details != nil {
			offered = v.Details.ProvidedProperties
		}
	}
	require.NotNil(t, offered, "a ServiceAccount should provide its name")
	assert.Equal(t, "workers", offered[api.PropertyKeyNamespace])
}
