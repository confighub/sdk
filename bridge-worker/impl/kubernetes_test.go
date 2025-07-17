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
	"github.com/fluxcd/pkg/ssa"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

func TestKubernetesBridgeWorker_Apply_Success(t *testing.T) {
	mockCtx, mockManager, _, restoreFunc := setupFullApplyTest(t, nil, nil)
	defer restoreFunc()

	worker := &KubernetesBridgeWorker{}
	payload := createStandardTestPayload(testTargetParams, testConfigMapYAML)

	err := worker.Apply(mockCtx, payload)
	assertStandardApplyResults(t, err, mockCtx, mockManager, false, 2, 1)
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
	mockManager, mockClient := setupMockResourceManager(t)

	setupWatchOperationMocks(t, mockCtx, mockManager, nil)
	setupMockApplyAllStaged(t, mockManager, nil)
	setupMockClientGet(t, mockClient, testNamespace, testName, nil)
	mockManager.On("Client").Return(mockClient)

	restoreFunc := setupKubernetesClientFactory(t, mockClient, mockManager)
	defer restoreFunc()

	worker := &KubernetesBridgeWorker{}
	payload := createStandardTestPayload(testTargetParams, testConfigMapYAML)

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
	assert.Error(t, err)
	var retryErr *backoff.RetryAfterError
	assert.ErrorAs(t, err, &retryErr, "error should be of type *backoff.RetryAfterError")
	assert.Contains(t, err.Error(), "retry after 30s")
	mockCtx.AssertNumberOfCalls(t, "SendStatus", 2)
	mockManager.AssertNumberOfCalls(t, "Wait", 1)
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
				resourceInfoList := []api.ResourceInfo{
					{ResourceType: "v1/ConfigMap", ResourceName: "default/test-configmap"},
				}
				resourceInfoListBytes, _ := json.Marshal(resourceInfoList)
				return api.BridgeWorkerPayload{
					TargetParams: testTargetParams,
					Data:         resourceInfoListBytes,
				}
			}(),
			setupMockFunc:       setupMockGetLiveObjects,
			expectedError:       false,
			expectedStatusCalls: 6,
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

			worker := &KubernetesBridgeWorker{}
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
