// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package impl

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/confighub/sdk/bridge-worker/api"
	"github.com/fluxcd/pkg/ssa"
	"github.com/stretchr/testify/mock"
	v1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

// Common test data
const (
	testNamespace = "default"
	testName      = "test-configmap"
)

var (
	// Common test payloads
	testConfigMapYAML = []byte(`
apiVersion: v1
kind: ConfigMap
metadata:
  name: test-configmap
  namespace: default
data:
  key: value
`)

	testTargetParams = []byte(`{"KubeContext":"test-context"}`)

	// Common test objects
	testConfigMap = &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "v1",
			"kind":       "ConfigMap",
			"metadata": map[string]interface{}{
				"name":      testName,
				"namespace": testNamespace,
			},
			"data": map[string]interface{}{
				"key": "value",
			},
		},
	}

	// Common test options
	testWaitOptions = ssa.WaitOptions{
		Interval: 5 * time.Second,
		Timeout:  1 * time.Minute,
	}

	testApplyOptions = ssa.ApplyOptions{
		Force: false,
	}
)

// MockBridgeWorkerContext implements api.BridgeWorkerContext for testing
// It mocks the SendStatus and Context methods.
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

// MockK8sClient is a mock implementation of the Kubernetes client
type MockK8sClient struct {
	mock.Mock
}

func (m *MockK8sClient) IsObjectNamespaced(obj runtime.Object) (bool, error) {
	// Mock implementation, adjust as needed
	return true, nil
}

func (m *MockK8sClient) DeleteAllOf(ctx context.Context, obj client.Object, opts ...client.DeleteAllOfOption) error {
	args := m.Called(ctx, obj, opts)
	return args.Error(0)
}

func (m *MockK8sClient) Get(ctx context.Context, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
	args := m.Called(ctx, key, obj, opts)
	if obj != nil {
		// Simulate setting fields on the object if needed
		unstructuredObj, ok := obj.(*unstructured.Unstructured)
		if ok {
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

// MockResourceManager is a mock implementation of the ResourceManager
type MockResourceManager struct {
	mock.Mock
}

func (m *MockResourceManager) Client() KubernetesClient {
	args := m.Called()
	return args.Get(0).(KubernetesClient)
}

func (m *MockResourceManager) ApplyAllStaged(ctx context.Context, objects []*unstructured.Unstructured, opts ssa.ApplyOptions) (*ssa.ChangeSet, error) {
	args := m.Called(ctx, objects, opts)
	// Ensure a valid *ssa.ChangeSet is returned, even in failure scenarios
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

// Test helper functions

// setupMockContext creates a new MockBridgeWorkerContext with default context
func setupMockContext(t *testing.T) *MockBridgeWorkerContext {
	t.Helper()
	mockCtx := new(MockBridgeWorkerContext)
	mockCtx.On("Context").Return(context.Background())
	return mockCtx
}

// setupMockSendStatus sets up a mock expectation for SendStatus with exact message match
func setupMockSendStatus(t *testing.T, mockCtx *MockBridgeWorkerContext, status api.ActionStatusType, result api.ActionResultType, message string) {
	t.Helper()
	mockCtx.On("SendStatus", mock.MatchedBy(func(r *api.ActionResult) bool {
		return r.Status == status && r.Result == result && r.Message == message
	})).Return(nil).Once()
}

// setupMockSendStatusContains sets up a mock expectation for SendStatus with partial message match
func setupMockSendStatusContains(t *testing.T, mockCtx *MockBridgeWorkerContext, status api.ActionStatusType, result api.ActionResultType, messageContains string) {
	t.Helper()
	mockCtx.On("SendStatus", mock.MatchedBy(func(r *api.ActionResult) bool {
		return r.Status == status && r.Result == result && strings.Contains(r.Message, messageContains)
	})).Return(nil).Once()
}

// setupMockResourceManager creates a new MockResourceManager with a MockK8sClient
func setupMockResourceManager(t *testing.T) (*MockResourceManager, *MockK8sClient) {
	t.Helper()
	mockManager := new(MockResourceManager)
	mockClient := new(MockK8sClient)
	mockManager.On("Client").Return(mockClient)
	return mockManager, mockClient
}

// setupMockApplyAllStaged sets up a mock expectation for ApplyAllStaged
func setupMockApplyAllStaged(t *testing.T, mockManager *MockResourceManager, returnError error) {
	t.Helper()
	mockManager.On("ApplyAllStaged",
		mock.MatchedBy(func(ctx context.Context) bool {
			return ctx != nil
		}),
		mock.MatchedBy(func(objects []*unstructured.Unstructured) bool {
			return len(objects) == 1 && objects[0].GetName() == testName
		}),
		mock.MatchedBy(func(opts ssa.ApplyOptions) bool {
			return opts.Force == testApplyOptions.Force
		}),
	).Return(&ssa.ChangeSet{Entries: []ssa.ChangeSetEntry{}}, returnError)
}

// setupMockWait sets up a mock expectation for Wait
func setupMockWait(t *testing.T, mockManager *MockResourceManager, returnError error) {
	t.Helper()
	mockManager.On("Wait",
		mock.MatchedBy(func(objects []*unstructured.Unstructured) bool {
			return len(objects) == 1 && objects[0].GetName() == testName
		}),
		mock.MatchedBy(func(opts ssa.WaitOptions) bool {
			return opts.Interval == testWaitOptions.Interval &&
				opts.Timeout == testWaitOptions.Timeout
		}),
	).Return(returnError)
}

// setupKubernetesClientFactory sets up the kubernetesClientFactory for testing
func setupKubernetesClientFactory(t *testing.T, mockClient *MockK8sClient, mockManager *MockResourceManager) func() {
	t.Helper()
	originalFunc := kubernetesClientFactory
	kubernetesClientFactory = func(kubeContext string) (KubernetesClient, ResourceManager, error) {
		return mockClient, mockManager, nil
	}
	return func() { kubernetesClientFactory = originalFunc }
}

// setupFakeClient creates a new fake client with the necessary schemes
func setupFakeClient(t *testing.T) client.Client {
	t.Helper()
	scheme := runtime.NewScheme()
	_ = v1.AddToScheme(scheme)
	return fake.NewClientBuilder().WithScheme(scheme).Build()
}

// Helper functions for creating test objects
func createTestObject(t *testing.T, apiVersion, kind string, metadata map[string]interface{}, spec map[string]interface{}, status map[string]interface{}) *unstructured.Unstructured {
	t.Helper()
	return &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": apiVersion,
			"kind":       kind,
			"metadata":   metadata,
			"spec":       spec,
		},
	}
}

func createTestMetadata(t *testing.T, name, namespace string, additionalFields map[string]interface{}) map[string]interface{} {
	t.Helper()
	metadata := map[string]interface{}{
		"name":              name,
		"namespace":         namespace,
		"resourceVersion":   "123",
		"generation":        "1",
		"uid":               "abc-123",
		"creationTimestamp": "2024-01-01T00:00:00Z",
		"managedFields": []interface{}{
			map[string]interface{}{
				"manager":   "kubectl",
				"operation": "Update",
			},
		},
		"ownerReferences": []interface{}{
			map[string]interface{}{
				"apiVersion": "v1",
				"kind":       "ReplicaSet",
				"name":       "test-rs",
				"uid":        "rs-123",
			},
		},
	}
	for k, v := range additionalFields {
		metadata[k] = v
	}
	return metadata
}

func createExpectedObject(t *testing.T, apiVersion, kind string, metadata map[string]interface{}, spec map[string]interface{}) *unstructured.Unstructured {
	t.Helper()
	return &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": apiVersion,
			"kind":       kind,
			"metadata":   metadata,
			"spec":       spec,
		},
	}
}
