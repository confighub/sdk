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

// getReferencesFor parses the input YAML and returns all references found by
// scanning the K8s path registry for paths whose NeededRequired contains "ResourceType".
// This mirrors the get-references function in the generic package.
func getReferencesFor(t *testing.T, input string) api.AttributeValueList {
	t.Helper()
	parsed, err := gaby.ParseAll([]byte(input))
	require.NoError(t, err)

	opts := &api.FunctionOptions{IncludeDetails: true}
	resourceTypeToReferencePaths := yamlkit.GetRegisteredNeededPathsByProperty(testResourceProvider, []string{"ResourceType"})
	values, err := yamlkit.GetPathsAnyType(parsed, resourceTypeToReferencePaths, []any{}, testResourceProvider, api.DataTypeString, false, false, opts)
	require.NoError(t, err)
	return values
}

// referenceTarget returns the "ResourceType" entry in NeededRequired for the
// given AttributeValue, or "" if the attribute does not declare one.
func referenceTarget(av api.AttributeValue) string {
	if av.Details == nil {
		return ""
	}
	return av.Details.NeededRequired["ResourceType"]
}

// findReferencesAtPath returns all AttributeValues whose ResourceName and Path
// match. There may be more than one — the same path can be registered under
// multiple resource-type-specific attribute names.
func findReferencesAtPath(values api.AttributeValueList, resourceName, path string) []api.AttributeValue {
	var found []api.AttributeValue
	for _, v := range values {
		if string(v.ResourceName) == resourceName && string(v.Path) == path {
			found = append(found, v)
		}
	}
	return found
}

func TestGetReferences_DeploymentWithCommonReferences(t *testing.T) {
	input := `
apiVersion: apps/v1
kind: Deployment
metadata:
  name: web
  namespace: prod
spec:
  template:
    spec:
      serviceAccountName: web-sa
      imagePullSecrets:
      - name: regcred
      volumes:
      - name: cfg
        configMap:
          name: web-config
      - name: tls
        secret:
          secretName: web-tls
      containers:
      - name: main
        image: nginx
        envFrom:
        - secretRef:
            name: web-secret
`
	values := getReferencesFor(t, input)

	// Each entry below describes a reference we expect get-references to surface.
	expected := []struct {
		path           string
		expectedTarget string
	}{
		{"metadata.namespace", "v1/Namespace"},
		{"spec.template.spec.serviceAccountName", "v1/ServiceAccount"},
		{"spec.template.spec.imagePullSecrets.0.name", "v1/Secret"},
		{"spec.template.spec.volumes.0.configMap.name", "v1/ConfigMap"},
		{"spec.template.spec.volumes.1.secret.secretName", "v1/Secret"},
		{"spec.template.spec.containers.0.envFrom.0.secretRef.name", "v1/Secret"},
	}
	for _, exp := range expected {
		matches := findReferencesAtPath(values, "prod/web", exp.path)
		require.NotEmpty(t, matches, "expected reference at path %s", exp.path)
		// At least one of the registered attribute names should carry the expected target.
		foundTarget := false
		for _, m := range matches {
			if referenceTarget(m) == exp.expectedTarget {
				foundTarget = true
				break
			}
		}
		assert.True(t, foundTarget, "expected NeededRequired[ResourceType]=%q at %s; got %+v",
			exp.expectedTarget, exp.path, matches)
	}

	// Every returned reference must declare a ResourceType target.
	for _, v := range values {
		assert.NotEmpty(t, referenceTarget(v),
			"reference at %s/%s lacks NeededRequired[ResourceType]; details=%+v",
			v.ResourceName, v.Path, v.Details)
	}
}

func TestGetReferences_RoleBindingSubjects(t *testing.T) {
	input := `
apiVersion: rbac.authorization.k8s.io/v1
kind: RoleBinding
metadata:
  name: web-rb
  namespace: prod
subjects:
- kind: ServiceAccount
  name: web-sa
  namespace: prod
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: Role
  name: web-role
`
	values := getReferencesFor(t, input)

	// metadata.namespace on the RoleBinding itself targets v1/Namespace.
	nsRefs := findReferencesAtPath(values, "prod/web-rb", "metadata.namespace")
	require.NotEmpty(t, nsRefs)
	foundNs := false
	for _, m := range nsRefs {
		if referenceTarget(m) == "v1/Namespace" {
			foundNs = true
			break
		}
	}
	assert.True(t, foundNs, "expected RoleBinding metadata.namespace to target v1/Namespace")

	// subjects.0.namespace targets v1/Namespace.
	subjNs := findReferencesAtPath(values, "prod/web-rb", "subjects.0.namespace")
	require.NotEmpty(t, subjNs)
	foundSubjNs := false
	for _, m := range subjNs {
		if referenceTarget(m) == "v1/Namespace" {
			foundSubjNs = true
			break
		}
	}
	assert.True(t, foundSubjNs, "expected RoleBinding subjects[0].namespace to target v1/Namespace")

	// subjects.0.name targets v1/ServiceAccount (kustomize-style backref).
	subjName := findReferencesAtPath(values, "prod/web-rb", "subjects.0.name")
	require.NotEmpty(t, subjName)
	foundSubjName := false
	for _, m := range subjName {
		if referenceTarget(m) == "v1/ServiceAccount" {
			foundSubjName = true
			break
		}
	}
	assert.True(t, foundSubjName, "expected RoleBinding subjects[0].name to target v1/ServiceAccount")
}

func TestGetReferences_NonPlaceholderValuesAreIncluded(t *testing.T) {
	// get-needed would skip these because the values are not placeholders.
	// get-references must include them.
	input := `
apiVersion: v1
kind: Pod
metadata:
  name: web
  namespace: prod
spec:
  serviceAccountName: real-sa
`
	values := getReferencesFor(t, input)

	saRefs := findReferencesAtPath(values, "prod/web", "spec.serviceAccountName")
	require.NotEmpty(t, saRefs, "spec.serviceAccountName with a concrete value must still be returned")
	for _, v := range saRefs {
		assert.Equal(t, "real-sa", v.Value)
	}

	nsRefs := findReferencesAtPath(values, "prod/web", "metadata.namespace")
	require.NotEmpty(t, nsRefs)
	for _, v := range nsRefs {
		assert.Equal(t, "prod", v.Value)
	}
}

func TestGetReferences_OnlyResourceTypeBearingPathsReturned(t *testing.T) {
	// metadata.name is a needed path on Namespaces (for the AttributeNameResourceName
	// attribute), but its NeededRequired does NOT include "ResourceType" — it's the
	// definition of the resource itself, not a reference. get-references must omit it.
	input := `
apiVersion: v1
kind: Namespace
metadata:
  name: prod
`
	values := getReferencesFor(t, input)
	nameRefs := findReferencesAtPath(values, "/prod", "metadata.name")
	assert.Empty(t, nameRefs, "Namespace metadata.name should not appear as a reference")
}
