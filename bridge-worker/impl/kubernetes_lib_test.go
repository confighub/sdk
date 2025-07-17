// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package impl

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
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
	result := extraCleanupObjects(objects)

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
	result := extraCleanupObjects(objects)

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
