// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package impl

import (
	"context"
	"errors"
	"testing"

	"github.com/cenkalti/backoff/v5"
	"github.com/confighub/sdk/bridge-worker/api"
	"github.com/fluxcd/pkg/ssa"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func TestKubernetesBridgeWorker_Apply_Success(t *testing.T) {
	mockCtx := setupMockContext(t)
	setupMockSendStatus(t, mockCtx, api.ActionStatusProgressing, api.ActionResultNone, "Starting to apply resources...")
	setupMockSendStatus(t, mockCtx, api.ActionStatusProgressing, api.ActionResultNone, "Applying resources...")
	setupMockSendStatusContains(t, mockCtx, api.ActionStatusCompleted, api.ActionResultApplyCompleted, "resources successfully")

	mockManager, mockClient := setupMockResourceManager(t)
	setupMockApplyAllStaged(t, mockManager, nil)
	setupMockWait(t, mockManager, nil)

	// Verify the Get call
	mockClient.On("Get",
		mock.MatchedBy(func(ctx context.Context) bool { return ctx != nil }),
		client.ObjectKey{Namespace: testNamespace, Name: testName},
		mock.MatchedBy(func(obj client.Object) bool {
			u, ok := obj.(*unstructured.Unstructured)
			return ok && u.GetName() == testName && u.GetNamespace() == testNamespace
		}), mock.Anything,
	).Return(nil)
	mockManager.On("Client").Return(mockClient)

	// Override the kubernetesClientFactory to return the mock manager
	restoreFunc := setupKubernetesClientFactory(t, mockClient, mockManager)
	defer restoreFunc()

	worker := &KubernetesBridgeWorker{}
	payload := api.BridgeWorkerPayload{
		TargetParams: testTargetParams,
		Data:         testConfigMapYAML,
	}

	err := worker.Apply(mockCtx, payload)

	assert.NoError(t, err)
	mockCtx.AssertNumberOfCalls(t, "SendStatus", 2)
	mockManager.AssertNumberOfCalls(t, "ApplyAllStaged", 1)
}

func TestKubernetesBridgeWorker_Apply_Failure(t *testing.T) {
	mockCtx := setupMockContext(t)
	setupMockSendStatus(t, mockCtx, api.ActionStatusProgressing, api.ActionResultNone, "Starting to apply resources...")
	setupMockSendStatusContains(t, mockCtx, api.ActionStatusFailed, api.ActionResultApplyFailed, "Failed to apply resources")

	mockManager, mockClient := setupMockResourceManager(t)
	setupMockApplyAllStaged(t, mockManager, errors.New("mock apply error"))

	restoreFunc := setupKubernetesClientFactory(t, mockClient, mockManager)
	defer restoreFunc()

	worker := &KubernetesBridgeWorker{}
	payload := api.BridgeWorkerPayload{
		Data: testConfigMapYAML,
	}

	err := worker.Apply(mockCtx, payload)

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "mock apply error")
	mockCtx.AssertNumberOfCalls(t, "SendStatus", 2)
	mockManager.AssertNumberOfCalls(t, "ApplyAllStaged", 1)
}

func TestKubernetesBridgeWorker_Apply_InvalidTargetParams(t *testing.T) {
	mockCtx := setupMockContext(t)
	setupMockSendStatusContains(t, mockCtx, api.ActionStatusFailed, api.ActionResultApplyFailed, "failed to parse target params")

	worker := &KubernetesBridgeWorker{}
	payload := api.BridgeWorkerPayload{
		TargetParams: []byte("invalid-json"),
	}

	err := worker.Apply(mockCtx, payload)

	assert.Error(t, err)
	mockCtx.AssertNumberOfCalls(t, "SendStatus", 1)
}

func TestKubernetesBridgeWorker_Apply_ParseObjectsError(t *testing.T) {
	mockCtx := setupMockContext(t)
	setupMockSendStatusContains(t, mockCtx, api.ActionStatusFailed, api.ActionResultApplyFailed, "failed to parse YAML resources")

	mockManager, _ := setupMockResourceManager(t)
	mockManager.On("Wait", mock.Anything, mock.Anything).
		Return(errors.New("mock wait error"))

	restoreFunc := setupKubernetesClientFactory(t, new(MockK8sClient), mockManager)
	defer restoreFunc()

	worker := &KubernetesBridgeWorker{}
	payload := api.BridgeWorkerPayload{
		Data: []byte("invalid-yaml"),
	}

	err := worker.Apply(mockCtx, payload)

	assert.Error(t, err)
	mockCtx.AssertNumberOfCalls(t, "SendStatus", 1)
}

func TestKubernetesBridgeWorker_Apply_EmptyPayload(t *testing.T) {
	mockCtx := setupMockContext(t)
	setupMockSendStatus(t, mockCtx, api.ActionStatusProgressing, api.ActionResultNone, "Starting to apply resources...")
	setupMockSendStatus(t, mockCtx, api.ActionStatusProgressing, api.ActionResultNone, "Applying resources...")

	mockManager, mockK8sClient := setupMockResourceManager(t)

	mockManager.On("ApplyAllStaged", mock.Anything, mock.Anything, mock.Anything).
		Return(&ssa.ChangeSet{Entries: []ssa.ChangeSetEntry{}}, nil)
	mockManager.On("Client").Return(mockK8sClient)
	restoreFunc := setupKubernetesClientFactory(t, mockK8sClient, mockManager)
	defer restoreFunc()

	worker := &KubernetesBridgeWorker{}
	payload := api.BridgeWorkerPayload{
		Data: []byte(""),
	}

	err := worker.Apply(mockCtx, payload)

	assert.NoError(t, err)
	mockCtx.AssertNumberOfCalls(t, "SendStatus", 2)
}

func TestKubernetesBridgeWorker_WatchForApply_Success(t *testing.T) {
	mockCtx := setupMockContext(t)
	setupMockSendStatus(t, mockCtx, api.ActionStatusProgressing, api.ActionResultNone, "Waiting for the applied resources...")
	setupMockSendStatusContains(t, mockCtx, api.ActionStatusCompleted, api.ActionResultApplyCompleted, "resources successfully")

	mockManager, mockClient := setupMockResourceManager(t)
	setupMockApplyAllStaged(t, mockManager, nil)
	setupMockWait(t, mockManager, nil)

	// Verify the Get call
	mockClient.On("Get",
		mock.MatchedBy(func(ctx context.Context) bool { return ctx != nil }),
		client.ObjectKey{Namespace: testNamespace, Name: testName},
		mock.MatchedBy(func(obj client.Object) bool {
			u, ok := obj.(*unstructured.Unstructured)
			return ok && u.GetName() == testName && u.GetNamespace() == testNamespace
		}), mock.Anything,
	).Return(nil)
	mockManager.On("Client").Return(mockClient)

	restoreFunc := setupKubernetesClientFactory(t, mockClient, mockManager)
	defer restoreFunc()

	worker := &KubernetesBridgeWorker{}
	payload := api.BridgeWorkerPayload{
		TargetParams: testTargetParams,
		Data:         testConfigMapYAML,
	}

	err := worker.WatchForApply(mockCtx, payload)

	assert.NoError(t, err)
	mockCtx.AssertNumberOfCalls(t, "SendStatus", 2)
	mockManager.AssertNumberOfCalls(t, "Wait", 1)
	mockClient.AssertNumberOfCalls(t, "Get", 1)
}

func TestKubernetesBridgeWorker_WatchForApply_Failure(t *testing.T) {
	mockCtx := setupMockContext(t)
	mockCtx.On("SendStatus", mock.Anything).Return(errors.New("mock send status error"))

	worker := &KubernetesBridgeWorker{}
	payload := api.BridgeWorkerPayload{
		Data: testConfigMapYAML,
	}

	err := worker.WatchForApply(mockCtx, payload)

	assert.Error(t, err)
	mockCtx.AssertCalled(t, "SendStatus", mock.Anything)
}

func TestKubernetesBridgeWorker_WatchForApply_InvalidWaitTimeout(t *testing.T) {
	mockCtx := setupMockContext(t)
	setupMockSendStatus(t, mockCtx, api.ActionStatusProgressing, api.ActionResultNone, "Waiting for the applied resources...")
	setupMockSendStatusContains(t, mockCtx, api.ActionStatusFailed, api.ActionResultApplyWaitFailed, "Failed to wait for resources")

	mockManager, _ := setupMockResourceManager(t)
	mockManager.On("Wait", mock.Anything, mock.Anything).
		Return(errors.New("mock wait error"))

	restoreFunc := setupKubernetesClientFactory(t, new(MockK8sClient), mockManager)
	defer restoreFunc()

	worker := &KubernetesBridgeWorker{}
	payload := api.BridgeWorkerPayload{
		TargetParams: []byte(`{"WaitTimeout":"invalid-duration"}`), // Invalid WaitTimeout
		Data:         testConfigMapYAML,
	}

	err := worker.WatchForApply(mockCtx, payload)

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "mock wait error")
	mockCtx.AssertNumberOfCalls(t, "SendStatus", 2)
	mockManager.AssertNumberOfCalls(t, "Wait", 1)
}

func TestKubernetesBridgeWorker_WatchForApply_ContextDeadlineExceeded(t *testing.T) {
	mockCtx := setupMockContext(t)
	setupMockSendStatus(t, mockCtx, api.ActionStatusProgressing, api.ActionResultNone, "Waiting for the applied resources...")
	setupMockSendStatusContains(t, mockCtx, api.ActionStatusProgressing, api.ActionResultNone, "Failed to wait for resources")

	mockManager, mockClient := setupMockResourceManager(t)
	setupMockApplyAllStaged(t, mockManager, nil)
	setupMockWait(t, mockManager, context.DeadlineExceeded)

	restoreFunc := setupKubernetesClientFactory(t, mockClient, mockManager)
	defer restoreFunc()

	worker := &KubernetesBridgeWorker{}
	payload := api.BridgeWorkerPayload{
		TargetParams: testTargetParams,
		Data:         testConfigMapYAML,
	}

	err := worker.WatchForApply(mockCtx, payload)

	// Should return a backoff.RetryAfter error
	assert.Error(t, err)
	var retryErr *backoff.RetryAfterError
	assert.ErrorAs(t, err, &retryErr, "error should be of type *backoff.RetryAfterError")
	assert.Contains(t, err.Error(), "retry after 30s")
	mockCtx.AssertNumberOfCalls(t, "SendStatus", 2)
	mockManager.AssertNumberOfCalls(t, "Wait", 1)
}

func TestCleanup(t *testing.T) {
	tests := []struct {
		name     string
		input    *unstructured.Unstructured
		expected *unstructured.Unstructured
	}{
		{
			name: "cleanup deployment",
			input: createTestObject(t,
				"apps/v1",
				"Deployment",
				createTestMetadata(t, "test-deployment", "default", nil),
				map[string]interface{}{
					"replicas": int64(3),
				},
				map[string]interface{}{
					"availableReplicas": int64(3),
					"readyReplicas":     int64(3),
				},
			),
			expected: createExpectedObject(t,
				"apps/v1",
				"Deployment",
				map[string]interface{}{
					"name":      "test-deployment",
					"namespace": "default",
				},
				map[string]interface{}{
					"replicas": int64(3),
				},
			),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cleanup(tt.input)

			// Verify metadata cleanup
			metadata, exists := tt.input.Object["metadata"].(map[string]interface{})
			assert.True(t, exists, "metadata should exist")
			assert.Equal(t, tt.expected.Object["metadata"].(map[string]interface{})["name"], metadata["name"], "name should be preserved")
			assert.Equal(t, tt.expected.Object["metadata"].(map[string]interface{})["namespace"], metadata["namespace"], "namespace should be preserved")
			assert.NotContains(t, metadata, "resourceVersion", "resourceVersion should be removed")
			assert.NotContains(t, metadata, "generation", "generation should be removed")
			assert.NotContains(t, metadata, "uid", "uid should be removed")
			assert.NotContains(t, metadata, "creationTimestamp", "creationTimestamp should be removed")
			assert.NotContains(t, metadata, "managedFields", "managedFields should be removed")
			assert.NotContains(t, metadata, "ownerReferences", "ownerReferences should be removed")

			// Verify status removal
			assert.NotContains(t, tt.input.Object, "status", "status should be removed")

			// Verify spec preservation
			assert.Equal(t, tt.expected.Object["spec"], tt.input.Object["spec"], "spec should be preserved")
		})
	}
}

func TestExtraCleanupObjects(t *testing.T) {
	tests := []struct {
		name     string
		input    []*unstructured.Unstructured
		expected []*unstructured.Unstructured
	}{
		{
			name: "cleanup deployment with annotations and labels",
			input: []*unstructured.Unstructured{
				createTestObject(t,
					"apps/v1",
					"Deployment",
					createTestMetadata(t, "test-deployment", "default", map[string]interface{}{
						"annotations": map[string]interface{}{
							"kubectl.kubernetes.io/last-applied-configuration": "{}",
							"custom.annotation": "value",
						},
						"labels": map[string]interface{}{
							"kubernetes.io/name": "test",
							"custom.label":       "value",
						},
					}),
					map[string]interface{}{
						"replicas": int64(3),
						"template": map[string]interface{}{
							"spec": map[string]interface{}{
								"dnsPolicy": "ClusterFirst",
								"containers": []interface{}{
									map[string]interface{}{
										"name":  "test",
										"image": "test:latest",
									},
								},
							},
						},
					},
					map[string]interface{}{
						"availableReplicas": int64(3),
						"readyReplicas":     int64(3),
					},
				),
			},
			expected: []*unstructured.Unstructured{
				createExpectedObject(t,
					"apps/v1",
					"Deployment",
					map[string]interface{}{
						"name":      "test-deployment",
						"namespace": "default",
						"annotations": map[string]interface{}{
							"custom.annotation": "value",
						},
						"labels": map[string]interface{}{
							"custom.label": "value",
						},
					},
					map[string]interface{}{
						"replicas": int64(3),
						"template": map[string]interface{}{
							"spec": map[string]interface{}{
								"containers": []interface{}{
									map[string]interface{}{
										"name":  "test",
										"image": "test:latest",
									},
								},
							},
						},
					},
				),
			},
		},
		{
			name: "cleanup service",
			input: []*unstructured.Unstructured{
				createTestObject(t,
					"v1",
					"Service",
					createTestMetadata(t, "test-service", "default", nil),
					map[string]interface{}{
						"type":      "ClusterIP",
						"clusterIP": "10.0.0.1",
						"ports": []interface{}{
							map[string]interface{}{
								"port": int64(80),
							},
						},
					},
					map[string]interface{}{
						"loadBalancer": map[string]interface{}{
							"ingress": []interface{}{
								map[string]interface{}{
									"ip": "10.0.0.1",
								},
							},
						},
					},
				),
			},
			expected: []*unstructured.Unstructured{
				createExpectedObject(t,
					"v1",
					"Service",
					map[string]interface{}{
						"name":      "test-service",
						"namespace": "default",
					},
					map[string]interface{}{
						"type": "ClusterIP",
						"ports": []interface{}{
							map[string]interface{}{
								"port": int64(80),
							},
						},
					},
				),
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := extraCleanupObjects(tt.input)
			assert.Equal(t, len(tt.expected), len(result), "number of objects should match")

			for i, obj := range result {
				expected := tt.expected[i]

				// Verify metadata cleanup
				metadata, exists := obj.Object["metadata"].(map[string]interface{})
				assert.True(t, exists, "metadata should exist")
				assert.Equal(t, expected.Object["metadata"].(map[string]interface{})["name"], metadata["name"], "name should be preserved")
				assert.Equal(t, expected.Object["metadata"].(map[string]interface{})["namespace"], metadata["namespace"], "namespace should be preserved")
				assert.NotContains(t, metadata, "resourceVersion", "resourceVersion should be removed")
				assert.NotContains(t, metadata, "generation", "generation should be removed")
				assert.NotContains(t, metadata, "uid", "uid should be removed")
				assert.NotContains(t, metadata, "creationTimestamp", "creationTimestamp should be removed")
				assert.NotContains(t, metadata, "managedFields", "managedFields should be removed")
				assert.NotContains(t, metadata, "ownerReferences", "ownerReferences should be removed")

				// Verify annotations and labels filtering
				if expectedAnnotations, ok := expected.Object["metadata"].(map[string]interface{})["annotations"].(map[string]interface{}); ok {
					actualAnnotations, exists := metadata["annotations"].(map[string]interface{})
					assert.True(t, exists, "annotations should exist")
					assert.Equal(t, expectedAnnotations, actualAnnotations, "annotations should match")
				}

				if expectedLabels, ok := expected.Object["metadata"].(map[string]interface{})["labels"].(map[string]interface{}); ok {
					actualLabels, exists := metadata["labels"].(map[string]interface{})
					assert.True(t, exists, "labels should exist")
					assert.Equal(t, expectedLabels, actualLabels, "labels should match")
				}

				// Verify status removal
				assert.NotContains(t, obj.Object, "status", "status should be removed")

				// Verify spec preservation and cleanup
				assert.Equal(t, expected.Object["spec"], obj.Object["spec"], "spec should be preserved and cleaned up")
			}
		})
	}
}
