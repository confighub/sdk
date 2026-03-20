// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package kubernetes

import (
	"context"
	"strings"
	"testing"

	"time"

	"github.com/confighub/sdk/core/worker/api"
	"github.com/fluxcd/pkg/ssa"
	"github.com/stretchr/testify/mock"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// MockBridgeWorkerContext implements api.BridgeWorkerContext for testing.
type MockBridgeWorkerContext struct {
	mock.Mock
}

func (m *MockBridgeWorkerContext) Context() context.Context {
	args := m.Called()
	return args.Get(0).(context.Context)
}

func (m *MockBridgeWorkerContext) SendStatus(result *api.ActionResult) error {
	args := m.Called(result)
	return args.Error(0)
}

func (m *MockBridgeWorkerContext) GetServerURL() string {
	args := m.Called()
	return args.String(0)
}

func (m *MockBridgeWorkerContext) GetWorkerID() string {
	args := m.Called()
	return args.String(0)
}

// MockK8sClient is a mock implementation of the KubernetesClient interface.
type MockK8sClient struct {
	mock.Mock
}

func (m *MockK8sClient) IsObjectNamespaced(obj runtime.Object) (bool, error) {
	return true, nil
}

func (m *MockK8sClient) DeleteAllOf(ctx context.Context, obj client.Object, opts ...client.DeleteAllOfOption) error {
	args := m.Called(ctx, obj, opts)
	return args.Error(0)
}

func (m *MockK8sClient) Get(ctx context.Context, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
	args := m.Called(ctx, key, obj, opts)
	if obj != nil {
		if unstructuredObj, ok := obj.(*unstructured.Unstructured); ok {
			unstructuredObj.SetName(key.Name)
			unstructuredObj.SetNamespace(key.Namespace)
		}
	}
	return args.Error(0)
}

func (m *MockK8sClient) List(ctx context.Context, list client.ObjectList, opts ...client.ListOption) error {
	args := m.Called(ctx, list, opts)
	return args.Error(0)
}

func (m *MockK8sClient) Create(ctx context.Context, obj client.Object, opts ...client.CreateOption) error {
	args := m.Called(ctx, obj, opts)
	return args.Error(0)
}

func (m *MockK8sClient) Update(ctx context.Context, obj client.Object, opts ...client.UpdateOption) error {
	args := m.Called(ctx, obj, opts)
	return args.Error(0)
}

func (m *MockK8sClient) Delete(ctx context.Context, obj client.Object, opts ...client.DeleteOption) error {
	args := m.Called(ctx, obj, opts)
	return args.Error(0)
}

func (m *MockK8sClient) Patch(ctx context.Context, obj client.Object, patch client.Patch, opts ...client.PatchOption) error {
	args := m.Called(ctx, obj, patch, opts)
	return args.Error(0)
}

var _ KubernetesClient = (*MockK8sClient)(nil)

// MockK8sApplier is a mock implementation of the K8sApplier interface.
type MockK8sApplier struct {
	mock.Mock
}

func (m *MockK8sApplier) Apply(ctx context.Context, objects []*unstructured.Unstructured) ApplyResult {
	args := m.Called(ctx, objects)
	return args.Get(0).(ApplyResult)
}

func (m *MockK8sApplier) WaitForApply(ctx context.Context, objects []*unstructured.Unstructured, timeout time.Duration) WaitResult {
	args := m.Called(ctx, objects, timeout)
	return args.Get(0).(WaitResult)
}

func (m *MockK8sApplier) Refresh(ctx context.Context, objects []*unstructured.Unstructured) ([]*unstructured.Unstructured, error) {
	args := m.Called(ctx, objects)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]*unstructured.Unstructured), args.Error(1)
}

func (m *MockK8sApplier) Destroy(ctx context.Context, objects []*unstructured.Unstructured) DestroyResult {
	args := m.Called(ctx, objects)
	return args.Get(0).(DestroyResult)
}

func (m *MockK8sApplier) WaitForDestroy(ctx context.Context, objects []*unstructured.Unstructured, timeout time.Duration) WaitResult {
	args := m.Called(ctx, objects, timeout)
	return args.Get(0).(WaitResult)
}

func (m *MockK8sApplier) BuildLiveState(ctx context.Context) ([]*unstructured.Unstructured, error) {
	args := m.Called(ctx)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]*unstructured.Unstructured), args.Error(1)
}

// MockResourceManager is a mock implementation of the ResourceManager interface.
type MockResourceManager struct {
	mock.Mock
}

func (m *MockResourceManager) Client() KubernetesClient {
	args := m.Called()
	return args.Get(0).(KubernetesClient)
}

func (m *MockResourceManager) ApplyAllStaged(ctx context.Context, objects []*unstructured.Unstructured, opts ssa.ApplyOptions) (*ssa.ChangeSet, error) {
	args := m.Called(ctx, objects, opts)
	changeSet := args.Get(0)
	if changeSet == nil {
		changeSet = &ssa.ChangeSet{Entries: []ssa.ChangeSetEntry{}}
	}
	return changeSet.(*ssa.ChangeSet), args.Error(1)
}

func (m *MockResourceManager) Wait(objects []*unstructured.Unstructured, opts ssa.WaitOptions) error {
	args := m.Called(objects, opts)
	return args.Error(0)
}

func (m *MockResourceManager) WaitForTermination(objects []*unstructured.Unstructured, opts ssa.WaitOptions) error {
	args := m.Called(objects, opts)
	return args.Error(0)
}

func (m *MockResourceManager) DeleteAll(ctx context.Context, objects []*unstructured.Unstructured, opts ssa.DeleteOptions) (*ssa.ChangeSet, error) {
	args := m.Called(ctx, objects, opts)
	return args.Get(0).(*ssa.ChangeSet), args.Error(1)
}

// SetupMockContext creates a new MockBridgeWorkerContext with default context.
func SetupMockContext(t *testing.T) *MockBridgeWorkerContext {
	t.Helper()
	mockCtx := new(MockBridgeWorkerContext)
	mockCtx.On("Context").Return(context.Background())
	return mockCtx
}

// SetupMockSendStatus sets up a mock expectation for SendStatus with exact message match.
func SetupMockSendStatus(t *testing.T, mockCtx *MockBridgeWorkerContext, status api.ActionStatusType, result api.ActionResultType, message string) {
	t.Helper()
	mockCtx.On("SendStatus", mock.MatchedBy(func(r *api.ActionResult) bool {
		return r.Status == status && r.Result == result && r.Message == message
	})).Return(nil).Once()
}

// SetupMockSendStatusContains sets up a mock expectation for SendStatus with partial message match.
func SetupMockSendStatusContains(t *testing.T, mockCtx *MockBridgeWorkerContext, status api.ActionStatusType, result api.ActionResultType, messageContains string) {
	t.Helper()
	mockCtx.On("SendStatus", mock.MatchedBy(func(r *api.ActionResult) bool {
		return r.Status == status && r.Result == result && strings.Contains(r.Message, messageContains)
	})).Return(nil).Once()
}
