// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/confighub/sdk/cmd/k8s-mf/mfclass"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/client-go/openapi"
	"k8s.io/client-go/openapi/openapitest"
	"sigs.k8s.io/yaml"
)

// The fixtures in testdata/ were captured from a live kind cluster with
// `kubectl get -o yaml --show-managed-fields` after applying
// public/cmd/fctl/test-data/{deployment,service}.yaml server-side as the field
// manager "confighub-bridge-worker". They let these tests exercise the
// managed-field analysis — including the schema-aware path — without a cluster:
// the schema comes from client-go's embedded OpenAPI (openapitest), not the
// cluster.

func loadFixture(t *testing.T, name string) *unstructured.Unstructured {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", name))
	require.NoError(t, err)
	var m map[string]interface{}
	require.NoError(t, yaml.Unmarshal(data, &m))
	return &unstructured.Unstructured{Object: m}
}

// schemaProjector builds a schema-aware projector from client-go's embedded
// OpenAPI (covers apps/v1, core v1, …) — the offline equivalent of fetching the
// cluster's OpenAPI.
func schemaProjector(t *testing.T) *mfclass.Projector {
	t.Helper()
	tc, err := openapi.NewTypeConverter(openapitest.NewEmbeddedFileClient(), false)
	require.NoError(t, err)
	p := mfclass.NewProjector(tc)
	require.True(t, p.SchemaAware())
	return p
}

func managerCategories(a *analysis) map[string]string {
	out := map[string]string{}
	for _, c := range a.byCategory() {
		for _, m := range c.Managers {
			out[m.Manager] = c.Category
		}
	}
	return out
}

func anyContains(paths []string, sub string) bool {
	for _, p := range paths {
		if strings.Contains(p, sub) {
			return true
		}
	}
	return false
}

// Categorization is name-based, so it is identical with or without a schema.
func TestDeploymentCategorization(t *testing.T) {
	obj := loadFixture(t, "deployment-live.yaml")
	for _, proj := range []*mfclass.Projector{schemaProjector(t), mfclass.NewProjector(nil)} {
		a, err := analyze(obj, proj)
		require.NoError(t, err)
		cats := managerCategories(a)
		assert.Equal(t, string(mfclass.CategoryApplier), cats["confighub-bridge-worker"])
		assert.Equal(t, string(mfclass.CategoryAsyncController), cats["kube-controller-manager"])
	}
}

// Schema-aware defaults are exact: owned atomic fields (spec.selector) and
// controller-owned status are not misreported, while genuine API-server
// defaults are.
func TestDeploymentDefaults_SchemaAware(t *testing.T) {
	obj := loadFixture(t, "deployment-live.yaml")
	a, err := analyze(obj, schemaProjector(t))
	require.NoError(t, err)
	defs := a.defaultFields()

	assert.False(t, anyContains(defs, "selector.matchLabels"),
		"atomic spec.selector is owned, not a default: %v", defs)
	assert.False(t, anyContains(defs, "status.conditions"),
		"status conditions are owned by kube-controller-manager: %v", defs)
	// strategy was applied as "{}" so its contents are server-defaulted.
	assert.True(t, anyContains(defs, ".spec.strategy"),
		"server-defaulted strategy.* should be a default: %v", defs)
}

// Without a schema, the projector can't tell an atomic "f:selector: {}" from a
// granular key-only ownership, so spec.selector.matchLabels leaks into defaults.
// This pins that documented limitation (and the schema-aware fix above).
func TestDeploymentDefaults_SchemalessLimitation(t *testing.T) {
	obj := loadFixture(t, "deployment-live.yaml")
	a, err := analyze(obj, mfclass.NewProjector(nil))
	require.NoError(t, err)
	assert.True(t, anyContains(a.defaultFields(), "selector.matchLabels"),
		"schemaless path is expected to leak the atomic selector's subfields")
}

// values projects the owned fields into a faithful, complete manifest.
func TestValuesDeployment_SchemaAware(t *testing.T) {
	obj := loadFixture(t, "deployment-live.yaml")
	a, err := analyze(obj, schemaProjector(t))
	require.NoError(t, err)

	out, err := buildValues(a, "", string(mfclass.CategoryApplier), false)
	require.NoError(t, err)
	o := out.Object

	assert.Equal(t, "apps/v1", o["apiVersion"])
	assert.Equal(t, "Deployment", o["kind"])

	// Atomic selector comes back whole.
	labels, found, err := unstructured.NestedStringMap(o, "spec", "selector", "matchLabels")
	require.NoError(t, err)
	require.True(t, found, "spec.selector.matchLabels should be present")
	assert.Equal(t, "mydep", labels["app"])

	// No nil placeholders left in associative lists.
	containers, found, err := unstructured.NestedSlice(o, "spec", "template", "spec", "containers")
	require.NoError(t, err)
	require.True(t, found)
	require.NotEmpty(t, containers)
	for _, c := range containers {
		assert.NotNil(t, c, "projected container list must not contain nil entries")
	}
}

// A Service applied via SSA: the applier owns its config, while cluster-allocated
// fields (clusterIP, …) are defaults.
func TestServiceDefaults_SchemaAware(t *testing.T) {
	obj := loadFixture(t, "service-live.yaml")
	a, err := analyze(obj, schemaProjector(t))
	require.NoError(t, err)

	assert.Equal(t, string(mfclass.CategoryApplier), managerCategories(a)["confighub-bridge-worker"])
	assert.True(t, anyContains(a.defaultFields(), ".spec.clusterIP"),
		"server-allocated clusterIP should be a default: %v", a.defaultFields())
}
