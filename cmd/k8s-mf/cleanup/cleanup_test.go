// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package cleanup

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/yaml"
)

// setupTestObjectWithSystemFields adds common system fields to a test object
func setupTestObjectWithSystemFields(obj *unstructured.Unstructured, status map[string]interface{}) {
	if status != nil {
		obj.Object["status"] = status
	}

	metadata := obj.Object["metadata"].(map[string]interface{})
	metadata["resourceVersion"] = "123"
	metadata["generation"] = "1"
	metadata["uid"] = "abc-123"
	metadata["creationTimestamp"] = "2024-01-01T00:00:00Z"
	metadata["managedFields"] = []interface{}{
		map[string]interface{}{
			"manager":   "kubectl",
			"operation": "Update",
		},
	}
}

// verifySystemFieldsRemoved verifies that common system fields are removed
func verifySystemFieldsRemoved(t *testing.T, obj *unstructured.Unstructured) {
	t.Helper()
	assert.NotContains(t, obj.Object, "status", "status should be removed")

	metadata := obj.Object["metadata"].(map[string]interface{})
	assert.NotContains(t, metadata, "resourceVersion", "resourceVersion should be removed")
	assert.NotContains(t, metadata, "generation", "generation should be removed")
	assert.NotContains(t, metadata, "uid", "uid should be removed")
	assert.NotContains(t, metadata, "creationTimestamp", "creationTimestamp should be removed")
	assert.NotContains(t, metadata, "managedFields", "managedFields should be removed")
}

// TestDeploymentCleanup tests deployment-specific cleanup functionality
func TestDeploymentCleanup(t *testing.T) {
	deployment := createTestDeployment("test-deployment", "default", 3)

	setupTestObjectWithSystemFields(deployment, map[string]interface{}{
		"availableReplicas": int64(3),
		"readyReplicas":     int64(3),
	})

	// Add annotations and labels to test cleanup
	metadata := deployment.Object["metadata"].(map[string]interface{})
	metadata["annotations"] = map[string]interface{}{
		// Should be removed by prefix match
		"kubectl.kubernetes.io/last-applied-configuration": "prefix-match",
		"deployment.kubernetes.io/revision":                "1",
		// Should be removed by specific key match
		"kubernetes.io/change-cause": "specific-key-match",
		// Should be preserved
		"custom.annotation":      "should-be-preserved",
		"app.kubernetes.io/name": "my-app",
	}
	metadata["labels"] = map[string]interface{}{
		// Should be removed
		"controller-uid":    "abc-123",
		"pod-template-hash": "def-456",
		// Should be preserved
		"custom.label": "should-be-preserved",
		"app":          "test",
	}

	objects := []*unstructured.Unstructured{deployment}
	result := ExtraCleanupObjects(objects)

	assert.Equal(t, len(objects), len(result), "number of objects should match")
	deploymentResult := result[0]
	assert.Equal(t, "test-deployment", deploymentResult.GetName())

	verifySystemFieldsRemoved(t, deploymentResult)

	// Verify annotations cleanup
	deploymentAnnotations, found, err := unstructured.NestedStringMap(deploymentResult.Object, "metadata", "annotations")
	assert.NoError(t, err)
	assert.True(t, found)
	assert.NotContains(t, deploymentAnnotations, "kubectl.kubernetes.io/last-applied-configuration", "prefix-matched annotation should be removed")
	assert.NotContains(t, deploymentAnnotations, "deployment.kubernetes.io/revision", "prefix-matched annotation should be removed")
	assert.NotContains(t, deploymentAnnotations, "kubernetes.io/change-cause", "specific-key-matched annotation should be removed")
	assert.Equal(t, "should-be-preserved", deploymentAnnotations["custom.annotation"], "custom annotation should be preserved")
	assert.Equal(t, "my-app", deploymentAnnotations["app.kubernetes.io/name"], "app.kubernetes.io annotation should be preserved")

	// Verify labels cleanup
	deploymentLabels, found, err := unstructured.NestedStringMap(deploymentResult.Object, "metadata", "labels")
	assert.NoError(t, err)
	assert.True(t, found)
	assert.NotContains(t, deploymentLabels, "controller-uid", "internal label should be removed")
	assert.NotContains(t, deploymentLabels, "pod-template-hash", "internal label should be removed")
	assert.Equal(t, "should-be-preserved", deploymentLabels["custom.label"], "custom label should be preserved")
	assert.Equal(t, "test", deploymentLabels["app"], "app label should be preserved")
}

// TestServiceCleanup tests service-specific cleanup functionality
func TestServiceCleanup(t *testing.T) {
	service := createTestService("test-service", "default", "ClusterIP", 80)

	setupTestObjectWithSystemFields(service, map[string]interface{}{
		"loadBalancer": map[string]interface{}{
			"ingress": []interface{}{
				map[string]interface{}{"ip": "10.0.0.1"},
			},
		},
	})

	// Add annotations to test cleanup
	metadata := service.Object["metadata"].(map[string]interface{})
	metadata["annotations"] = map[string]interface{}{
		"kubectl.kubernetes.io/restartedAt": "2024-01-01T00:00:00Z",
		"service.annotation":                "preserved",
	}

	objects := []*unstructured.Unstructured{service}
	result := ExtraCleanupObjects(objects)

	assert.Equal(t, len(objects), len(result), "number of objects should match")
	serviceResult := result[0]
	assert.Equal(t, "test-service", serviceResult.GetName())

	verifySystemFieldsRemoved(t, serviceResult)

	// Verify annotations cleanup
	serviceAnnotations, found, err := unstructured.NestedStringMap(serviceResult.Object, "metadata", "annotations")
	assert.NoError(t, err)
	assert.True(t, found)
	assert.NotContains(t, serviceAnnotations, "kubectl.kubernetes.io/restartedAt", "kubectl annotation should be removed")
	assert.Equal(t, "preserved", serviceAnnotations["service.annotation"], "service annotation should be preserved")
}

// TestRemoveUnmanagedFields tests that removeUnmanagedFields correctly preserves
// fields that are managed atomically (f:rules: {}) while removing unmanaged fields.
func TestRemoveUnmanagedFields(t *testing.T) {
	// This is the ClusterRole from the cluster with managedFields
	// The rules field is managed atomically (f:rules: {})
	clusterRoleYAML := `
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  annotations:
    config.k8s.io/owning-inventory: 33c25a2f-d9ea-409f-8fe7-718540f38ab4-traefik
    meta.helm.sh/release-name: traefik
    meta.helm.sh/release-namespace: traefik
    testannotation: testvalue
  creationTimestamp: "2025-04-29T20:15:08Z"
  labels:
    app.kubernetes.io/instance: traefik-traefik
    app.kubernetes.io/managed-by: Helm
    app.kubernetes.io/name: traefik
    helm.sh/chart: traefik-38.0.1
    helm.toolkit.fluxcd.io/name: traefik
    helm.toolkit.fluxcd.io/namespace: traefik
  managedFields:
  - apiVersion: rbac.authorization.k8s.io/v1
    fieldsType: FieldsV1
    fieldsV1:
      f:metadata:
        f:annotations:
          f:config.k8s.io/owning-inventory: {}
          f:testannotation: {}
        f:labels:
          f:app.kubernetes.io/instance: {}
          f:app.kubernetes.io/managed-by: {}
          f:app.kubernetes.io/name: {}
          f:helm.sh/chart: {}
      f:rules: {}
    manager: confighub-bridge-worker
    operation: Apply
    time: "2025-12-19T20:40:58Z"
  name: traefik-traefik
  resourceVersion: "48708535"
  uid: f6e4345a-e309-44a1-97cd-2da46d6b28f6
rules:
- apiGroups:
  - ""
  resources:
  - configmaps
  - nodes
  - services
  verbs:
  - get
  - list
  - watch
- apiGroups:
  - discovery.k8s.io
  resources:
  - endpointslices
  verbs:
  - list
  - watch
- apiGroups:
  - ""
  resources:
  - pods
  verbs:
  - get
- apiGroups:
  - ""
  resources:
  - secrets
  verbs:
  - get
  - list
  - watch
- apiGroups:
  - extensions
  - networking.k8s.io
  resources:
  - ingressclasses
  - ingresses
  verbs:
  - get
  - list
  - watch
- apiGroups:
  - extensions
  - networking.k8s.io
  resources:
  - ingresses/status
  verbs:
  - update
- apiGroups:
  - ""
  resources:
  - namespaces
  verbs:
  - list
  - watch
- apiGroups:
  - traefik.io
  resources:
  - ingressroutes
  - ingressroutetcps
  - ingressrouteudps
  - middlewares
  - middlewaretcps
  - serverstransports
  - serverstransporttcps
  - tlsoptions
  - tlsstores
  - traefikservices
  verbs:
  - get
  - list
  - watch
`

	// Parse the YAML into an unstructured object
	var objMap map[string]interface{}
	err := yaml.Unmarshal([]byte(clusterRoleYAML), &objMap)
	require.NoError(t, err)

	obj := &unstructured.Unstructured{Object: objMap}

	// Debug: Check what GetManagedFields returns
	managedFields := obj.GetManagedFields()
	t.Logf("managedFields count: %d", len(managedFields))
	for i, mf := range managedFields {
		t.Logf("managedFields[%d]: manager=%s, raw=%s", i, mf.Manager, string(mf.FieldsV1.Raw))
	}

	// Debug: Trace through the logic
	if len(managedFields) > 0 {
		mf := managedFields[0]
		var fieldsMap map[string]interface{}
		if err := json.Unmarshal(mf.FieldsV1.Raw, &fieldsMap); err != nil {
			t.Logf("Error parsing FieldsV1: %v", err)
		} else {
			if rulesVal, ok := fieldsMap["f:rules"]; ok {
				t.Logf("f:rules value: %+v (type: %T)", rulesVal, rulesVal)
				if rulesMap, ok := rulesVal.(map[string]interface{}); ok {
					t.Logf("f:rules is a map with len=%d", len(rulesMap))
				}
			}
		}
	}

	// Verify the rules exist before
	rules, found, err := unstructured.NestedSlice(obj.Object, "rules")
	require.NoError(t, err)
	require.True(t, found, "rules should exist before ExtraCleanupObjects")
	require.Len(t, rules, 8, "should have 8 rules before")

	// Call the function under test - this is what's called in production
	result := ExtraCleanupObjects([]*unstructured.Unstructured{obj})
	require.Len(t, result, 1)
	obj = result[0]

	// Verify the rules still exist after - this is the bug we're testing
	rules, found, err = unstructured.NestedSlice(obj.Object, "rules")
	require.NoError(t, err)
	assert.True(t, found, "rules should still exist after ExtraCleanupObjects")
	assert.Len(t, rules, 8, "should still have 8 rules after")

	// Verify managed labels are kept
	labels := obj.GetLabels()
	assert.Equal(t, "traefik-traefik", labels["app.kubernetes.io/instance"])
	assert.Equal(t, "Helm", labels["app.kubernetes.io/managed-by"])
	assert.Equal(t, "traefik", labels["app.kubernetes.io/name"])
	assert.Equal(t, "traefik-38.0.1", labels["helm.sh/chart"])

	// Verify unmanaged labels are removed
	assert.Empty(t, labels["helm.toolkit.fluxcd.io/name"], "unmanaged label should be removed")
	assert.Empty(t, labels["helm.toolkit.fluxcd.io/namespace"], "unmanaged label should be removed")

	// Verify managed annotations are kept
	annotations := obj.GetAnnotations()
	assert.Equal(t, "testvalue", annotations["testannotation"])

	// Verify unmanaged annotations are removed
	assert.Empty(t, annotations["meta.helm.sh/release-name"], "unmanaged annotation should be removed")
	assert.Empty(t, annotations["meta.helm.sh/release-namespace"], "unmanaged annotation should be removed")
}

// TestServicePortsPreserved tests that Service ports with keyed list items are preserved.
// This tests the fix for numeric type differences between JSON (float64) and YAML (int/int64).
func TestServicePortsPreserved(t *testing.T) {
	// This Service has managedFields with keyed ports like k:{"port":80,"protocol":"TCP"}
	// When parsed from JSON, port is float64(80), but from YAML it's int(80)
	serviceYAML := `
apiVersion: v1
kind: Service
metadata:
  annotations:
    config.k8s.io/owning-inventory: test-inventory
  creationTimestamp: "2025-04-29T20:15:08Z"
  labels:
    app.kubernetes.io/instance: traefik-traefik
    app.kubernetes.io/managed-by: Helm
    app.kubernetes.io/name: traefik
    helm.sh/chart: traefik-38.0.1
  managedFields:
  - apiVersion: v1
    fieldsType: FieldsV1
    fieldsV1:
      f:metadata:
        f:annotations:
          f:config.k8s.io/owning-inventory: {}
        f:labels:
          f:app.kubernetes.io/instance: {}
          f:app.kubernetes.io/managed-by: {}
          f:app.kubernetes.io/name: {}
          f:helm.sh/chart: {}
      f:spec:
        f:ports:
          k:{"port":80,"protocol":"TCP"}:
            .: {}
            f:name: {}
            f:port: {}
            f:protocol: {}
            f:targetPort: {}
          k:{"port":443,"protocol":"TCP"}:
            .: {}
            f:name: {}
            f:port: {}
            f:protocol: {}
            f:targetPort: {}
        f:selector: {}
        f:type: {}
    manager: confighub-bridge-worker
    operation: Apply
    time: "2025-12-19T20:40:58Z"
  name: traefik
  namespace: traefik
  resourceVersion: "48708532"
  uid: 09dc702f-b85e-4940-845e-73338a87fb9f
spec:
  clusterIP: 10.96.82.128
  clusterIPs:
  - 10.96.82.128
  internalTrafficPolicy: Cluster
  ipFamilies:
  - IPv4
  ipFamilyPolicy: SingleStack
  ports:
  - name: web
    port: 80
    protocol: TCP
    targetPort: web
  - name: websecure
    port: 443
    protocol: TCP
    targetPort: websecure
  selector:
    app.kubernetes.io/instance: traefik-traefik
    app.kubernetes.io/name: traefik
  sessionAffinity: None
  type: ClusterIP
status:
  loadBalancer: {}
`

	// Parse the YAML into an unstructured object
	var objMap map[string]interface{}
	err := yaml.Unmarshal([]byte(serviceYAML), &objMap)
	require.NoError(t, err)

	obj := &unstructured.Unstructured{Object: objMap}

	// Verify ports exist before
	ports, found, err := unstructured.NestedSlice(obj.Object, "spec", "ports")
	require.NoError(t, err)
	require.True(t, found, "ports should exist before ExtraCleanupObjects")
	require.Len(t, ports, 2, "should have 2 ports before")

	// Call the function under test
	result := ExtraCleanupObjects([]*unstructured.Unstructured{obj})
	require.Len(t, result, 1)
	obj = result[0]

	// Verify ports still exist after - this is the bug we fixed
	ports, found, err = unstructured.NestedSlice(obj.Object, "spec", "ports")
	require.NoError(t, err)
	assert.True(t, found, "ports should still exist after ExtraCleanupObjects")
	assert.Len(t, ports, 2, "should still have 2 ports after")

	// Verify port details are preserved
	if len(ports) >= 2 {
		port0 := ports[0].(map[string]interface{})
		assert.Equal(t, "web", port0["name"])
		// Port values may be int, int64, or float64 depending on marshaling
		assert.True(t, valuesEqual(80, port0["port"]), "port 0 should be 80")
		assert.Equal(t, "TCP", port0["protocol"])

		port1 := ports[1].(map[string]interface{})
		assert.Equal(t, "websecure", port1["name"])
		assert.True(t, valuesEqual(443, port1["port"]), "port 1 should be 443")
		assert.Equal(t, "TCP", port1["protocol"])
	}

	// Verify unmanaged spec fields are removed
	_, found, _ = unstructured.NestedString(obj.Object, "spec", "clusterIP")
	assert.False(t, found, "clusterIP should be removed (not managed)")

	_, found, _ = unstructured.NestedSlice(obj.Object, "spec", "clusterIPs")
	assert.False(t, found, "clusterIPs should be removed (not managed)")

	_, found, _ = unstructured.NestedString(obj.Object, "spec", "internalTrafficPolicy")
	assert.False(t, found, "internalTrafficPolicy should be removed (not managed)")

	// Verify managed spec fields are kept
	serviceType, found, err := unstructured.NestedString(obj.Object, "spec", "type")
	assert.NoError(t, err)
	assert.True(t, found, "type should still exist")
	assert.Equal(t, "ClusterIP", serviceType)

	selector, found, err := unstructured.NestedStringMap(obj.Object, "spec", "selector")
	assert.NoError(t, err)
	assert.True(t, found, "selector should still exist")
	assert.Equal(t, "traefik-traefik", selector["app.kubernetes.io/instance"])
}

// TestNamespaceFinalizersRemoved tests that Namespace spec.finalizers is removed when not managed.
func TestNamespaceFinalizersRemoved(t *testing.T) {
	// Namespace with spec.finalizers that is NOT managed
	namespaceYAML := `
apiVersion: v1
kind: Namespace
metadata:
  annotations:
    custom.annotation/test: test-value
  creationTimestamp: "2025-04-29T20:15:04Z"
  labels:
    kubernetes.io/metadata.name: traefik
  managedFields:
  - apiVersion: v1
    fieldsType: FieldsV1
    fieldsV1:
      f:metadata:
        f:annotations:
          f:custom.annotation/test: {}
    manager: confighub-bridge-worker
    operation: Apply
    time: "2025-12-19T20:40:58Z"
  name: traefik
  resourceVersion: "48692601"
  uid: 32969381-02e3-4138-9702-5f8f1947fd26
spec:
  finalizers:
  - kubernetes
status:
  phase: Active
`

	// Parse the YAML into an unstructured object
	var objMap map[string]interface{}
	err := yaml.Unmarshal([]byte(namespaceYAML), &objMap)
	require.NoError(t, err)

	obj := &unstructured.Unstructured{Object: objMap}

	// Verify spec exists before
	spec, found, err := unstructured.NestedMap(obj.Object, "spec")
	require.NoError(t, err)
	require.True(t, found, "spec should exist before ExtraCleanupObjects")
	require.NotNil(t, spec["finalizers"], "finalizers should exist before")

	// Call the function under test
	result := ExtraCleanupObjects([]*unstructured.Unstructured{obj})
	require.Len(t, result, 1)
	obj = result[0]

	// Verify spec is removed (since it's not managed)
	_, found, _ = unstructured.NestedMap(obj.Object, "spec")
	assert.False(t, found, "spec should be removed since it's not managed")

	// Verify essential fields are kept
	assert.Equal(t, "v1", obj.GetAPIVersion())
	assert.Equal(t, "Namespace", obj.GetKind())
	assert.Equal(t, "traefik", obj.GetName())

	// Verify managed annotation is kept
	annotations := obj.GetAnnotations()
	assert.Equal(t, "test-value", annotations["custom.annotation/test"])

	// Verify unmanaged label is removed
	labels := obj.GetLabels()
	assert.Empty(t, labels["kubernetes.io/metadata.name"], "unmanaged label should be removed")
}
