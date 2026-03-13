// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package noop

import (
	"context"
	"testing"

	"github.com/confighub/sdk/worker/api"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

// mockBridgeWorkerContext is a mock for api.BridgeWorkerContext.
type mockBridgeWorkerContext struct {
	mock.Mock
}

func (m *mockBridgeWorkerContext) Context() context.Context {
	args := m.Called()
	return args.Get(0).(context.Context)
}

func (m *mockBridgeWorkerContext) GetServerURL() string {
	args := m.Called()
	return args.String(0)
}

func (m *mockBridgeWorkerContext) GetWorkerID() string {
	args := m.Called()
	return args.String(0)
}

func (m *mockBridgeWorkerContext) SendStatus(result *api.ActionResult) error {
	args := m.Called(result)
	return args.Error(0)
}

var testConfigMapYAML = []byte(`apiVersion: v1
kind: ConfigMap
metadata:
  name: test-configmap
  namespace: default
data:
  key: value
`)

func TestNoopBridge_Apply(t *testing.T) {
	worker := NewNoopBridge()
	mockCtx := new(mockBridgeWorkerContext)

	payload := api.BridgeWorkerPayload{
		Data: testConfigMapYAML,
	}

	mockCtx.On("SendStatus", mock.MatchedBy(func(r *api.ActionResult) bool {
		return r.Status == api.ActionStatusCompleted &&
			r.Result == api.ActionResultApplyCompleted &&
			r.LiveData != nil &&
			r.LiveState != nil &&
			r.ResourceStatuses != nil
	})).Return(nil).Once()

	err := worker.Apply(mockCtx, payload)
	assert.NoError(t, err)
	mockCtx.AssertExpectations(t)
}

func TestNoopBridge_Apply_MultiDoc(t *testing.T) {
	worker := NewNoopBridge()
	mockCtx := new(mockBridgeWorkerContext)

	multiDocYAML := []byte(`apiVersion: v1
kind: ConfigMap
metadata:
  name: cm1
  namespace: default
data:
  key: value
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: deploy1
  namespace: default
spec:
  replicas: 1
`)

	payload := api.BridgeWorkerPayload{
		Data: multiDocYAML,
	}

	mockCtx.On("SendStatus", mock.MatchedBy(func(r *api.ActionResult) bool {
		if r.Status != api.ActionStatusCompleted || r.Result != api.ActionResultApplyCompleted {
			return false
		}
		return len(r.ResourceStatuses) == 2
	})).Return(nil).Once()

	err := worker.Apply(mockCtx, payload)
	assert.NoError(t, err)
	mockCtx.AssertExpectations(t)
}

func TestNoopBridge_Apply_EmptyData(t *testing.T) {
	worker := NewNoopBridge()
	mockCtx := new(mockBridgeWorkerContext)

	payload := api.BridgeWorkerPayload{
		Data: nil,
	}

	mockCtx.On("SendStatus", mock.MatchedBy(func(r *api.ActionResult) bool {
		return r.Status == api.ActionStatusCompleted &&
			r.Result == api.ActionResultApplyCompleted &&
			r.ResourceStatuses == nil
	})).Return(nil).Once()

	err := worker.Apply(mockCtx, payload)
	assert.NoError(t, err)
	mockCtx.AssertExpectations(t)
}

func TestNoopBridge_Refresh(t *testing.T) {
	worker := NewNoopBridge()
	mockCtx := new(mockBridgeWorkerContext)

	payload := api.BridgeWorkerPayload{
		Data:      testConfigMapYAML,
		LiveState: []byte("existing-live-state"),
	}

	mockCtx.On("SendStatus", mock.MatchedBy(func(r *api.ActionResult) bool {
		return r.Status == api.ActionStatusCompleted &&
			r.Result == api.ActionResultRefreshAndNoDrift &&
			assert.Equal(t, payload.Data, r.LiveData) &&
			assert.Equal(t, payload.LiveState, r.LiveState)
	})).Return(nil).Once()

	err := worker.Refresh(mockCtx, payload)
	assert.NoError(t, err)
	mockCtx.AssertExpectations(t)
}

func TestNoopBridge_Refresh_NoLiveState(t *testing.T) {
	worker := NewNoopBridge()
	mockCtx := new(mockBridgeWorkerContext)

	payload := api.BridgeWorkerPayload{
		Data: testConfigMapYAML,
	}

	mockCtx.On("SendStatus", mock.MatchedBy(func(r *api.ActionResult) bool {
		return r.Status == api.ActionStatusCompleted &&
			r.Result == api.ActionResultRefreshAndNoDrift &&
			assert.Equal(t, payload.Data, r.LiveData) &&
			assert.Equal(t, payload.Data, r.LiveState)
	})).Return(nil).Once()

	err := worker.Refresh(mockCtx, payload)
	assert.NoError(t, err)
	mockCtx.AssertExpectations(t)
}

func TestNoopBridge_Destroy(t *testing.T) {
	worker := NewNoopBridge()
	mockCtx := new(mockBridgeWorkerContext)

	payload := api.BridgeWorkerPayload{
		Data: testConfigMapYAML,
	}

	mockCtx.On("SendStatus", mock.MatchedBy(func(r *api.ActionResult) bool {
		return r.Status == api.ActionStatusCompleted &&
			r.Result == api.ActionResultDestroyCompleted
	})).Return(nil).Once()

	err := worker.Destroy(mockCtx, payload)
	assert.NoError(t, err)
	mockCtx.AssertExpectations(t)
}

func TestNoopBridge_Import(t *testing.T) {
	worker := NewNoopBridge()
	mockCtx := new(mockBridgeWorkerContext)

	payload := api.BridgeWorkerPayload{}

	mockCtx.On("SendStatus", mock.MatchedBy(func(r *api.ActionResult) bool {
		return r.Status == api.ActionStatusFailed &&
			r.Result == api.ActionResultImportFailed
	})).Return(nil).Once()

	err := worker.Import(mockCtx, payload)
	assert.NoError(t, err)
	mockCtx.AssertExpectations(t)
}

func TestNoopBridge_Finalize(t *testing.T) {
	worker := NewNoopBridge()

	err := worker.Finalize(nil, api.BridgeWorkerPayload{})
	assert.NoError(t, err)
}

func TestBuildNoopResourceStatuses(t *testing.T) {
	multiDocYAML := []byte(`apiVersion: v1
kind: ConfigMap
metadata:
  name: test-cm
  namespace: default
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: test-deploy
  namespace: kube-system
`)

	statuses := buildNoopResourceStatuses(multiDocYAML)
	assert.Len(t, statuses, 2)

	cmFound := false
	deployFound := false
	for key, status := range statuses {
		if string(key) == "v1/ConfigMap#default/test-cm" {
			cmFound = true
			assert.Equal(t, api.ResourceSyncStatusSynced, status.SyncStatus)
			assert.Equal(t, api.ResourceReadinessReady, status.Readiness)
		}
		if string(key) == "apps/v1/Deployment#kube-system/test-deploy" {
			deployFound = true
			assert.Equal(t, api.ResourceSyncStatusSynced, status.SyncStatus)
			assert.Equal(t, api.ResourceReadinessReady, status.Readiness)
		}
	}
	assert.True(t, cmFound, "ConfigMap resource status not found")
	assert.True(t, deployFound, "Deployment resource status not found")
}

func TestBuildNoopResourceStatuses_EmptyData(t *testing.T) {
	statuses := buildNoopResourceStatuses(nil)
	assert.Nil(t, statuses)

	statuses = buildNoopResourceStatuses([]byte(""))
	assert.Nil(t, statuses)
}

func TestBuildNoopResourceStatuses_ClusterScoped(t *testing.T) {
	yaml := []byte(`apiVersion: v1
kind: Namespace
metadata:
  name: test-ns
`)

	statuses := buildNoopResourceStatuses(yaml)
	assert.Len(t, statuses, 1)

	for key := range statuses {
		assert.Equal(t, "v1/Namespace#/test-ns", string(key))
	}
}
