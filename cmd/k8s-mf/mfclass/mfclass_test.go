// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package mfclass

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func TestClassifyManager(t *testing.T) {
	cases := []struct {
		manager string
		want    Category
	}{
		// Appliers
		{"confighub-bridge-worker", CategoryApplier},
		{"confighub-something-old", CategoryApplier},
		{"kubectl", CategoryApplier},
		{"kubectl-client-side-apply", CategoryApplier},
		{"kubectl-edit", CategoryApplier},
		{"argocd-controller", CategoryApplier},
		{"argocd-application-controller", CategoryApplier},
		{"helm-controller", CategoryApplier},
		{"kustomize-controller", CategoryApplier},
		{"helm", CategoryApplier},
		{"application/apply-patch", CategoryApplier}, // Sveltos / generic controller-runtime SSA
		{"sveltos", CategoryApplier},
		{"tanka", CategoryApplier},
		{"before-first-apply", CategoryApplier},
		// Admission controllers
		{"istiod", CategoryAdmissionController},
		{"linkerd-proxy-injector", CategoryAdmissionController},
		{"vpa-admission-controller", CategoryAdmissionController},
		// Async controllers
		{"horizontal-pod-autoscaler-controller", CategoryAsyncController},
		{"deployment-controller", CategoryAsyncController},
		{"cert-manager-controller", CategoryAsyncController},
		{"kube-controller-manager", CategoryAsyncController},
		// Names that must NOT be caught by the kubectl prefix
		{"kube-scheduler", CategoryAsyncController},
		// Unknown
		{"some-random-operator", CategoryUnknown},
	}
	for _, tc := range cases {
		got, _ := ClassifyManager(tc.manager)
		assert.Equalf(t, tc.want, got, "manager %q", tc.manager)
	}
}

func TestClassifyHeuristics(t *testing.T) {
	// Unknown manager doing SSA -> Applier (heuristic).
	c := Classify(metav1.ManagedFieldsEntry{Manager: "mystery", Operation: metav1.ManagedFieldsOperationApply})
	assert.Equal(t, CategoryApplier, c.Category)
	assert.True(t, c.Heuristic)

	// Unknown manager writing status -> AsyncController (heuristic).
	c = Classify(metav1.ManagedFieldsEntry{Manager: "mystery", Operation: metav1.ManagedFieldsOperationUpdate, Subresource: "status"})
	assert.Equal(t, CategoryAsyncController, c.Category)
	assert.True(t, c.Heuristic)

	// Unknown manager doing a one-off Update -> stays Unknown.
	c = Classify(metav1.ManagedFieldsEntry{Manager: "mystery", Operation: metav1.ManagedFieldsOperationUpdate})
	assert.Equal(t, CategoryUnknown, c.Category)
	assert.False(t, c.Heuristic)

	// Known manager: registry wins, never flagged heuristic.
	c = Classify(metav1.ManagedFieldsEntry{Manager: "kubectl", Operation: metav1.ManagedFieldsOperationUpdate})
	assert.Equal(t, CategoryApplier, c.Category)
	assert.False(t, c.Heuristic)
}

func TestShouldTakeOver(t *testing.T) {
	keeper := ConfigHubFieldManager
	// Other appliers are taken over.
	assert.True(t, ShouldTakeOver("kubectl", keeper))
	assert.True(t, ShouldTakeOver("kubectl-client-side-apply", keeper))
	assert.True(t, ShouldTakeOver("argocd-controller", keeper))
	assert.True(t, ShouldTakeOver("helm-controller", keeper))
	assert.True(t, ShouldTakeOver("application/apply-patch", keeper))
	assert.True(t, ShouldTakeOver("sveltos", keeper))
	// The keeper does not take over itself.
	assert.False(t, ShouldTakeOver(keeper, keeper))
	// Controllers are preserved.
	assert.False(t, ShouldTakeOver("horizontal-pod-autoscaler-controller", keeper))
	assert.False(t, ShouldTakeOver("istiod", keeper))
	// Unknown managers are left alone.
	assert.False(t, ShouldTakeOver("some-random-operator", keeper))
}

func TestIsIgnored(t *testing.T) {
	assert.True(t, IsIgnored("horizontal-pod-autoscaler-controller"))
	assert.True(t, IsIgnored("istiod")) // admission also ignored
	assert.False(t, IsIgnored("kubectl"))
	assert.False(t, IsIgnored("confighub-bridge-worker"))
	assert.False(t, IsIgnored("some-random-operator"))
}

func mfEntry(t *testing.T, manager string, fieldsV1JSON string) metav1.ManagedFieldsEntry {
	t.Helper()
	return metav1.ManagedFieldsEntry{
		Manager:    manager,
		Operation:  metav1.ManagedFieldsOperationApply,
		FieldsType: "FieldsV1",
		FieldsV1:   &metav1.FieldsV1{Raw: []byte(fieldsV1JSON)},
	}
}

func TestParseEntryAndRenderPaths(t *testing.T) {
	e := mfEntry(t, "kubectl", `{
		"f:metadata":{"f:labels":{"f:app":{}}},
		"f:spec":{"f:replicas":{}}
	}`)
	set, err := ParseEntry(e)
	require.NoError(t, err)
	paths := RenderPaths(set)
	assert.Contains(t, paths, ".metadata.labels.app")
	assert.Contains(t, paths, ".spec.replicas")

	// Empty FieldsV1 -> empty set, no error.
	set, err = ParseEntry(metav1.ManagedFieldsEntry{Manager: "x"})
	require.NoError(t, err)
	assert.True(t, set.Empty())
}

func TestProjectAndDefaultFields(t *testing.T) {
	obj := &unstructured.Unstructured{Object: map[string]interface{}{
		"apiVersion": "apps/v1",
		"kind":       "Deployment",
		"metadata": map[string]interface{}{
			"name":   "demo",
			"labels": map[string]interface{}{"app": "demo"},
		},
		"spec": map[string]interface{}{
			"replicas": int64(3),
			// paused was defaulted/never owned by anyone.
			"paused": false,
		},
	}}

	managed := mfEntry(t, "kubectl", `{
		"f:metadata":{"f:labels":{"f:app":{}}},
		"f:spec":{"f:replicas":{}}
	}`)
	set, err := ParseEntry(managed)
	require.NoError(t, err)

	// Project keeps only managed values.
	projected, err := Project(obj, set)
	require.NoError(t, err)
	spec, _, _ := unstructured.NestedMap(projected.Object, "spec")
	assert.Contains(t, spec, "replicas")
	assert.NotContains(t, spec, "paused")
	labels, _, _ := unstructured.NestedMap(projected.Object, "metadata", "labels")
	assert.Contains(t, labels, "app")

	// Default fields = object fields - managed fields. spec.paused should be
	// among them; spec.replicas should not.
	defaults := ObjectFieldSet(obj).Difference(set)
	defaultPaths := RenderPaths(defaults)
	assert.Contains(t, defaultPaths, ".spec.paused")
	assert.NotContains(t, defaultPaths, ".spec.replicas")
}
