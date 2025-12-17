// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package impl

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/cenkalti/backoff/v5"
	"github.com/confighub/sdk/bridge-worker/api"
	goclientnew "github.com/confighub/sdk/openapi/goclient-new"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func TestKubernetesBridgeWorker_Apply_Success(t *testing.T) {
	mockCtx := setupMockContext(t)
	mockApplier := new(MockK8sApplier)

	// Setup mock expectations
	setupMockSendStatus(t, mockCtx, api.ActionStatusProgressing, api.ActionResultNone, "Starting to apply resources...")
	setupMockSendStatus(t, mockCtx, api.ActionStatusProgressing, api.ActionResultNone, "Applying resources...")
	// After successful apply, expect ApplySynced status (config pushed, waiting for ready)
	mockCtx.On("SendStatus", mock.MatchedBy(func(status *api.ActionResult) bool {
		return status.Status == api.ActionStatusCompleted &&
			status.Result == api.ActionResultApplySynced &&
			status.Message == "Resources applied successfully, waiting for ready state"
	})).Return(nil).Once()

	// Mock the applier factory to return our mock applier
	originalFactory := k8sApplierFactory
	k8sApplierFactory = func(name ApplierName, config ApplierConfig) (K8sApplier, error) {
		return mockApplier, nil
	}
	defer func() { k8sApplierFactory = originalFactory }()

	// Setup mock applier behavior
	resourceSet := &SimpleResourceSet{
		Entries: []SimpleResourceSetEntry{
			{
				Name:      "test-configmap",
				Namespace: "default",
				Kind:      "ConfigMap",
				Action:    "configured",
			},
		},
	}
	mockApplier.On("Apply", mock.Anything, mock.Anything).Return(ApplyResult{
		ResourceSet: resourceSet,
		LiveObjects: []*unstructured.Unstructured{testConfigMap},
		LiveData:    []byte("mock-live-data"),
		Error:       nil,
	})

	worker := NewKubernetesBridgeWorker()
	worker.applierType = CLIUtilsSSA
	payload := createStandardTestPayload(testTargetParams, testConfigMapYAML)

	err := worker.Apply(mockCtx, payload)
	assert.NoError(t, err)
	mockCtx.AssertNumberOfCalls(t, "SendStatus", 3)
	mockApplier.AssertNumberOfCalls(t, "Apply", 1)
}

func TestKubernetesBridgeWorker_Apply_Failure(t *testing.T) {
	mockCtx := setupMockContext(t)
	mockApplier := new(MockK8sApplier)

	// Setup mock expectations
	setupMockSendStatus(t, mockCtx, api.ActionStatusProgressing, api.ActionResultNone, "Starting to apply resources...")
	setupMockSendStatus(t, mockCtx, api.ActionStatusProgressing, api.ActionResultNone, "Applying resources...")
	setupMockSendStatusContains(t, mockCtx, api.ActionStatusFailed, api.ActionResultApplyFailed, "mock apply error")

	// Mock the applier factory to return our mock applier
	originalFactory := k8sApplierFactory
	k8sApplierFactory = func(name ApplierName, config ApplierConfig) (K8sApplier, error) {
		return mockApplier, nil
	}
	defer func() { k8sApplierFactory = originalFactory }()

	// Setup mock applier behavior to return an error
	mockApplier.On("Apply", mock.Anything, mock.Anything).Return(ApplyResult{
		ResourceSet: nil,
		LiveObjects: nil,
		LiveData:    nil,
		Error:       errors.New("mock apply error"),
	})

	worker := NewKubernetesBridgeWorker()
	worker.applierType = CLIUtilsSSA
	payload := api.BridgeWorkerPayload{
		TargetParams: testTargetParams,
		Data:         testConfigMapYAML,
	}

	err := worker.Apply(mockCtx, payload)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "mock apply error")
	mockCtx.AssertNumberOfCalls(t, "SendStatus", 3)
	mockApplier.AssertNumberOfCalls(t, "Apply", 1)
}

func TestKubernetesBridgeWorker_Apply_InvalidTargetParams(t *testing.T) {
	mockCtx := setupMockContext(t)
	setupMockSendStatusContains(t, mockCtx, api.ActionStatusFailed, api.ActionResultApplyFailed, "failed to parse target params")

	worker := NewKubernetesBridgeWorker()
	payload := api.BridgeWorkerPayload{
		TargetParams: []byte("invalid-json"),
	}

	err := worker.Apply(mockCtx, payload)
	assert.Error(t, err)
	mockCtx.AssertNumberOfCalls(t, "SendStatus", 1)
}

func TestKubernetesBridgeWorker_Apply_ParseObjectsError(t *testing.T) {
	mockCtx := setupMockContext(t)
	mockApplier := new(MockK8sApplier)

	// The error should happen before reaching the applier, during parseObjects
	setupMockSendStatusContains(t, mockCtx, api.ActionStatusFailed, api.ActionResultApplyFailed, "failed to parse YAML resources")

	// Mock the applier factory (even though it shouldn't be called)
	originalFactory := k8sApplierFactory
	k8sApplierFactory = func(name ApplierName, config ApplierConfig) (K8sApplier, error) {
		return mockApplier, nil
	}
	defer func() { k8sApplierFactory = originalFactory }()

	worker := NewKubernetesBridgeWorker()
	worker.applierType = CLIUtilsSSA
	payload := api.BridgeWorkerPayload{
		TargetParams: testTargetParams,
		Data:         []byte("invalid-yaml"),
	}

	err := worker.Apply(mockCtx, payload)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to parse YAML resources")
	mockCtx.AssertNumberOfCalls(t, "SendStatus", 1)
	// Apply should not be called because parsing failed
	mockApplier.AssertNotCalled(t, "Apply")
}

func TestKubernetesBridgeWorker_Apply_EmptyPayload(t *testing.T) {
	mockCtx := setupMockContext(t)
	mockApplier := new(MockK8sApplier)

	setupMockSendStatus(t, mockCtx, api.ActionStatusProgressing, api.ActionResultNone, "Starting to apply resources...")
	setupMockSendStatus(t, mockCtx, api.ActionStatusProgressing, api.ActionResultNone, "Applying resources...")
	// After successful apply, expect ApplySynced status (config pushed, waiting for ready)
	mockCtx.On("SendStatus", mock.MatchedBy(func(status *api.ActionResult) bool {
		return status.Status == api.ActionStatusCompleted &&
			status.Result == api.ActionResultApplySynced &&
			status.Message == "Resources applied successfully, waiting for ready state"
	})).Return(nil).Once()

	// Mock the applier factory to return our mock applier
	originalFactory := k8sApplierFactory
	k8sApplierFactory = func(name ApplierName, config ApplierConfig) (K8sApplier, error) {
		return mockApplier, nil
	}
	defer func() { k8sApplierFactory = originalFactory }()

	// Setup mock applier behavior for empty payload
	mockApplier.On("Apply", mock.Anything, mock.Anything).Return(ApplyResult{
		ResourceSet: &SimpleResourceSet{Entries: []SimpleResourceSetEntry{}},
		LiveObjects: []*unstructured.Unstructured{},
		LiveData:    []byte(""),
		Error:       nil,
	})

	worker := NewKubernetesBridgeWorker()
	worker.applierType = CLIUtilsSSA
	payload := api.BridgeWorkerPayload{
		TargetParams: testTargetParams,
		Data:         []byte(""),
	}

	err := worker.Apply(mockCtx, payload)
	assert.NoError(t, err)
	mockCtx.AssertNumberOfCalls(t, "SendStatus", 3)
}

func TestKubernetesBridgeWorker_WatchForApply_Success(t *testing.T) {
	mockCtx := setupMockContext(t)
	mockApplier := new(MockK8sApplier)

	// Setup mock expectations
	setupMockSendStatus(t, mockCtx, api.ActionStatusProgressing, api.ActionResultNone, "Waiting for the applied resources...")
	setupMockSendStatusContains(t, mockCtx, api.ActionStatusCompleted, api.ActionResultApplyCompleted, "Applied 1 resources successfully")

	// Mock the applier factory to return our mock applier
	originalFactory := k8sApplierFactory
	k8sApplierFactory = func(name ApplierName, config ApplierConfig) (K8sApplier, error) {
		return mockApplier, nil
	}
	defer func() { k8sApplierFactory = originalFactory }()

	// Setup mock applier behavior
	mockApplier.On("WaitForApply", mock.Anything, mock.Anything, mock.Anything).Return(WaitResult{
		LiveObjects: []*unstructured.Unstructured{testConfigMap},
		ResourceSet: &SimpleResourceSet{
			Entries: []SimpleResourceSetEntry{
				{
					Name:      "test-configmap",
					Namespace: "default",
					Kind:      "ConfigMap",
					Action:    "configured",
				},
			},
		},
		Error: nil,
	})

	worker := NewKubernetesBridgeWorker()
	worker.applierType = CLIUtilsSSA
	payload := createStandardTestPayload(testTargetParams, testConfigMapYAML)

	err := worker.WatchForApply(mockCtx, payload)
	assert.NoError(t, err)
	mockCtx.AssertNumberOfCalls(t, "SendStatus", 2)
	mockApplier.AssertNumberOfCalls(t, "WaitForApply", 1)
}

func TestKubernetesBridgeWorker_WatchForApply_Failure(t *testing.T) {
	mockCtx := setupMockContext(t)
	mockCtx.On("SendStatus", mock.Anything).Return(errors.New("mock send status error"))

	worker := NewKubernetesBridgeWorker()
	payload := api.BridgeWorkerPayload{
		Data: testConfigMapYAML,
	}

	err := worker.WatchForApply(mockCtx, payload)
	assert.Error(t, err)
	mockCtx.AssertCalled(t, "SendStatus", mock.Anything)
}

func TestKubernetesBridgeWorker_WatchForApply_InvalidWaitTimeout(t *testing.T) {
	mockCtx := setupMockContext(t)
	mockApplier := new(MockK8sApplier)

	setupMockSendStatus(t, mockCtx, api.ActionStatusProgressing, api.ActionResultNone, "Waiting for the applied resources...")
	setupMockSendStatusContains(t, mockCtx, api.ActionStatusFailed, api.ActionResultApplyWaitFailed, "mock wait error")

	// Mock the applier factory to return our mock applier
	originalFactory := k8sApplierFactory
	k8sApplierFactory = func(name ApplierName, config ApplierConfig) (K8sApplier, error) {
		return mockApplier, nil
	}
	defer func() { k8sApplierFactory = originalFactory }()

	// Setup mock applier behavior to return an error
	mockApplier.On("WaitForApply", mock.Anything, mock.Anything, mock.Anything).Return(WaitResult{
		LiveObjects: nil,
		ResourceSet: nil,
		Error:       errors.New("mock wait error"),
	})

	worker := NewKubernetesBridgeWorker()
	worker.applierType = CLIUtilsSSA
	payload := api.BridgeWorkerPayload{
		TargetParams: []byte(`{"KubeContext":"test-context","WaitTimeout":"invalid-duration"}`), // Invalid WaitTimeout
		Data:         testConfigMapYAML,
	}

	err := worker.WatchForApply(mockCtx, payload)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "mock wait error")
	mockCtx.AssertNumberOfCalls(t, "SendStatus", 2)
	mockApplier.AssertNumberOfCalls(t, "WaitForApply", 1)
}

func TestKubernetesBridgeWorker_WatchForApply_ContextDeadlineExceeded(t *testing.T) {
	mockCtx := setupMockContext(t)
	mockApplier := new(MockK8sApplier)

	setupMockSendStatus(t, mockCtx, api.ActionStatusProgressing, api.ActionResultNone, "Waiting for the applied resources...")
	// When deadline is exceeded, it sends a retry message with the error (with jitter in timing)
	setupMockSendStatusContains(t, mockCtx, api.ActionStatusProgressing, api.ActionResultNone, "Resources not ready, retrying in")

	// Mock the applier factory to return our mock applier
	originalFactory := k8sApplierFactory
	k8sApplierFactory = func(name ApplierName, config ApplierConfig) (K8sApplier, error) {
		return mockApplier, nil
	}
	defer func() { k8sApplierFactory = originalFactory }()

	// Setup mock applier behavior to return context deadline exceeded
	mockApplier.On("WaitForApply", mock.Anything, mock.Anything, mock.Anything).Return(WaitResult{
		LiveObjects: nil,
		ResourceSet: nil,
		Error:       context.DeadlineExceeded,
	})

	worker := NewKubernetesBridgeWorker()
	worker.applierType = CLIUtilsSSA
	payload := api.BridgeWorkerPayload{
		TargetParams: testTargetParams,
		Data:         testConfigMapYAML,
	}

	err := worker.WatchForApply(mockCtx, payload)
	assert.Error(t, err)
	var retryErr *backoff.RetryAfterError
	assert.ErrorAs(t, err, &retryErr, "error should be of type *backoff.RetryAfterError")
	assert.Contains(t, err.Error(), "retry after")
	mockCtx.AssertNumberOfCalls(t, "SendStatus", 2)
	mockApplier.AssertNumberOfCalls(t, "WaitForApply", 1)
}

// Import operation test cases
func TestKubernetesBridgeWorker_Import(t *testing.T) {
	tests := []struct {
		name                string
		payload             api.BridgeWorkerPayload
		setupMockFunc       func(*testing.T, *MockK8sClient, *MockResourceManager)
		expectedError       bool
		expectedStatusCalls int
	}{
		{
			name: "with import params",
			payload: func() api.BridgeWorkerPayload {
				importRequest := &goclientnew.ImportRequest{
					Filters: []goclientnew.ImportFilter{
						{Type: "namespace", Operator: "include", Values: []string{"default", "production"}},
					},
					Options: &goclientnew.ImportOptions{
						"include_system": false,
						"include_custom": true,
					},
				}
				extraParamsBytes, _ := json.Marshal(importRequest)
				return api.BridgeWorkerPayload{
					TargetParams: testTargetParams,
					ExtraParams:  extraParamsBytes,
				}
			}(),
			setupMockFunc:       setupMockGetResourcesWithParams,
			expectedError:       false,
			expectedStatusCalls: 3,
		},
		{
			name: "legacy resource info list",
			payload: func() api.BridgeWorkerPayload {
				resourceInfoList := goclientnew.ResourceInfoList{
					{ResourceType: "v1/ConfigMap", ResourceName: "default/test-configmap"},
				}
				importRequest := &goclientnew.ImportRequest{
					ResourceInfoList: &resourceInfoList,
				}
				extraParamsBytes, _ := json.Marshal(importRequest)
				return api.BridgeWorkerPayload{
					TargetParams: testTargetParams,
					ExtraParams:  extraParamsBytes,
				}
			}(),
			setupMockFunc:       setupMockGetLiveObjects,
			expectedError:       false,
			expectedStatusCalls: 4,
		},
		{
			name: "default behavior",
			payload: api.BridgeWorkerPayload{
				TargetParams: testTargetParams,
			},
			setupMockFunc:       setupMockGetAllClusterResources,
			expectedError:       false,
			expectedStatusCalls: 3,
		},
		{
			name: "invalid json falls back to default",
			payload: api.BridgeWorkerPayload{
				TargetParams: testTargetParams,
				ExtraParams:  []byte("invalid-json"),
			},
			setupMockFunc:       setupMockGetAllClusterResources,
			expectedError:       false,
			expectedStatusCalls: 3,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockCtx := setupMockContext(t)
			mockManager, mockClient := setupMockResourceManager(t)

			// Set up expected status calls
			setupImportStatusMocks(t, mockCtx, tt.expectedStatusCalls)

			// Set up specific mock behaviors
			tt.setupMockFunc(t, mockClient, mockManager)

			restoreFunc := setupKubernetesClientFactory(t, mockClient, mockManager)
			defer restoreFunc()

			worker := NewKubernetesBridgeWorker()
			err := worker.Import(mockCtx, tt.payload)

			if tt.expectedError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
			mockCtx.AssertNumberOfCalls(t, "SendStatus", tt.expectedStatusCalls)
		})
	}
}
