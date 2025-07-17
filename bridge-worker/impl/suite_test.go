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
	"github.com/stretchr/testify/assert"
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

func createTestConfigMap(name, namespace, env string) *unstructured.Unstructured {
	cm := &unstructured.Unstructured{}
	cm.SetAPIVersion("v1")
	cm.SetKind("ConfigMap")
	cm.SetName(name)
	cm.SetNamespace(namespace)
	cm.Object["data"] = map[string]interface{}{
		"env": env,
	}
	return cm
}

func createTestPod(name, namespace string) *unstructured.Unstructured {
	pod := &unstructured.Unstructured{}
	pod.SetAPIVersion("v1")
	pod.SetKind("Pod")
	pod.SetName(name)
	pod.SetNamespace(namespace)
	pod.Object["spec"] = map[string]interface{}{
		"containers": []interface{}{
			map[string]interface{}{
				"name":  "test",
				"image": "nginx",
			},
		},
	}
	return pod
}

func createTestSecret(name, namespace string) *unstructured.Unstructured {
	secret := &unstructured.Unstructured{}
	secret.SetAPIVersion("v1")
	secret.SetKind("Secret")
	secret.SetName(name)
	secret.SetNamespace(namespace)
	secret.Object["data"] = map[string]interface{}{
		"key": "dmFsdWU=", // base64 encoded "value"
	}
	return secret
}

func createTestNode(name string) *unstructured.Unstructured {
	node := &unstructured.Unstructured{}
	node.SetAPIVersion("v1")
	node.SetKind("Node")
	node.SetName(name)
	// Nodes are cluster-scoped, no namespace
	return node
}

func createTestClusterRole(name string) *unstructured.Unstructured {
	cr := &unstructured.Unstructured{}
	cr.SetAPIVersion("rbac.authorization.k8s.io/v1")
	cr.SetKind("ClusterRole")
	cr.SetName(name)
	// ClusterRoles are cluster-scoped, no namespace
	return cr
}

func createTestDeployment(name, namespace string, replicas int64) *unstructured.Unstructured {
	deployment := &unstructured.Unstructured{}
	deployment.SetAPIVersion("apps/v1")
	deployment.SetKind("Deployment")
	deployment.SetName(name)
	deployment.SetNamespace(namespace)
	deployment.Object["spec"] = map[string]interface{}{
		"replicas": replicas,
		"template": map[string]interface{}{
			"spec": map[string]interface{}{
				"containers": []interface{}{
					map[string]interface{}{
						"name":  "test",
						"image": "nginx",
					},
				},
			},
		},
	}
	return deployment
}

func createTestService(name, namespace, serviceType string, port int64) *unstructured.Unstructured {
	service := &unstructured.Unstructured{}
	service.SetAPIVersion("v1")
	service.SetKind("Service")
	service.SetName(name)
	service.SetNamespace(namespace)
	service.Object["spec"] = map[string]interface{}{
		"type": serviceType,
		"ports": []interface{}{
			map[string]interface{}{
				"port": port,
			},
		},
	}
	return service
}

// Helper functions for creating test specs
func createTestDeploymentSpec(replicas int64, containerName, image string) map[string]interface{} {
	return map[string]interface{}{
		"replicas": replicas,
		"template": map[string]interface{}{
			"spec": map[string]interface{}{
				"containers": []interface{}{
					map[string]interface{}{
						"name":  containerName,
						"image": image,
					},
				},
			},
		},
	}
}

func createTestServiceSpec(serviceType string, port int64) map[string]interface{} {
	return map[string]interface{}{
		"type": serviceType,
		"ports": []interface{}{
			map[string]interface{}{
				"port": port,
			},
		},
		"clusterIP": "10.0.0.1",
	}
}

// Helper functions for creating virtual and administrative resources that should be excluded

func createTestSubjectAccessReview(name, namespace string) *unstructured.Unstructured {
	sar := &unstructured.Unstructured{}
	sar.SetAPIVersion("authorization.k8s.io/v1")
	sar.SetKind("SubjectAccessReview")
	sar.SetName(name)
	sar.SetNamespace(namespace)
	return sar
}

func createTestTokenReview(name string) *unstructured.Unstructured {
	tr := &unstructured.Unstructured{}
	tr.SetAPIVersion("authentication.k8s.io/v1")
	tr.SetKind("TokenReview")
	tr.SetName(name)
	// TokenReview is cluster-scoped
	return tr
}

func createTestBinding(name, namespace string) *unstructured.Unstructured {
	binding := &unstructured.Unstructured{}
	binding.SetAPIVersion("v1")
	binding.SetKind("Binding")
	binding.SetName(name)
	binding.SetNamespace(namespace)
	return binding
}

func createTestAPIService(name string) *unstructured.Unstructured {
	apiService := &unstructured.Unstructured{}
	apiService.SetAPIVersion("apiregistration.k8s.io/v1")
	apiService.SetKind("APIService")
	apiService.SetName(name)
	// APIService is cluster-scoped
	return apiService
}

func createTestCertificateSigningRequest(name string) *unstructured.Unstructured {
	csr := &unstructured.Unstructured{}
	csr.SetAPIVersion("certificates.k8s.io/v1")
	csr.SetKind("CertificateSigningRequest")
	csr.SetName(name)
	// CSR is cluster-scoped
	return csr
}

func createStandardTestPayload(targetParams, data []byte) api.BridgeWorkerPayload {
	return api.BridgeWorkerPayload{
		TargetParams: targetParams,
		Data:         data,
	}
}

// Helper functions for mock setup
func setupApplyOperationMocks(t *testing.T, mockCtx *MockBridgeWorkerContext, mockManager *MockResourceManager, applyErr, waitErr error) {
	t.Helper()
	setupMockSendStatus(t, mockCtx, api.ActionStatusProgressing, api.ActionResultNone, "Starting to apply resources...")
	setupMockSendStatus(t, mockCtx, api.ActionStatusProgressing, api.ActionResultNone, "Applying resources...")

	if applyErr != nil {
		setupMockSendStatusContains(t, mockCtx, api.ActionStatusFailed, api.ActionResultApplyFailed, "Failed to apply resources")
	} else {
		setupMockSendStatusContains(t, mockCtx, api.ActionStatusCompleted, api.ActionResultApplyCompleted, "resources successfully")
	}

	setupMockApplyAllStaged(t, mockManager, applyErr)
	if waitErr != nil {
		setupMockWait(t, mockManager, waitErr)
	}
}

func setupWatchOperationMocks(t *testing.T, mockCtx *MockBridgeWorkerContext, mockManager *MockResourceManager, waitErr error) {
	t.Helper()
	setupMockSendStatus(t, mockCtx, api.ActionStatusProgressing, api.ActionResultNone, "Waiting for the applied resources...")

	if waitErr != nil {
		if waitErr == context.DeadlineExceeded {
			setupMockSendStatusContains(t, mockCtx, api.ActionStatusProgressing, api.ActionResultNone, "Failed to wait for resources")
		} else {
			setupMockSendStatusContains(t, mockCtx, api.ActionStatusFailed, api.ActionResultApplyWaitFailed, "Failed to wait for resources")
		}
	} else {
		setupMockSendStatusContains(t, mockCtx, api.ActionStatusCompleted, api.ActionResultApplyCompleted, "resources successfully")
	}

	setupMockWait(t, mockManager, waitErr)
}

func setupMockClientGet(t *testing.T, mockClient *MockK8sClient, namespace, name string, returnErr error) {
	t.Helper()
	mockClient.On("Get",
		mock.MatchedBy(func(ctx context.Context) bool { return ctx != nil }),
		client.ObjectKey{Namespace: namespace, Name: name},
		mock.MatchedBy(func(obj client.Object) bool {
			u, ok := obj.(*unstructured.Unstructured)
			return ok && u.GetName() == name && u.GetNamespace() == namespace
		}), mock.Anything,
	).Return(returnErr)
}

func setupFullApplyTest(t *testing.T, applyErr, waitErr error) (*MockBridgeWorkerContext, *MockResourceManager, *MockK8sClient, func()) {
	t.Helper()
	mockCtx := setupMockContext(t)
	mockManager, mockClient := setupMockResourceManager(t)

	setupApplyOperationMocks(t, mockCtx, mockManager, applyErr, waitErr)
	setupMockClientGet(t, mockClient, testNamespace, testName, nil)
	mockManager.On("Client").Return(mockClient)

	restoreFunc := setupKubernetesClientFactory(t, mockClient, mockManager)
	return mockCtx, mockManager, mockClient, restoreFunc
}

func assertStandardApplyResults(t *testing.T, err error, mockCtx *MockBridgeWorkerContext, mockManager *MockResourceManager, expectedError bool, expectedStatusCalls, expectedApplyCalls int) {
	t.Helper()
	if expectedError {
		assert.Error(t, err)
	} else {
		assert.NoError(t, err)
	}
	mockCtx.AssertNumberOfCalls(t, "SendStatus", expectedStatusCalls)
	mockManager.AssertNumberOfCalls(t, "ApplyAllStaged", expectedApplyCalls)
}

// Helper functions for import tests
func setupImportStatusMocks(t *testing.T, mockCtx *MockBridgeWorkerContext, expectedCalls int) {
	t.Helper()
	switch expectedCalls {
	case 3: // Standard import flow
		setupMockSendStatus(t, mockCtx, api.ActionStatusProgressing, api.ActionResultNone, "Fetching resources from Kubernetes cluster...")
		setupMockSendStatusContains(t, mockCtx, api.ActionStatusProgressing, api.ActionResultNone, "Found")
		setupMockSendStatus(t, mockCtx, api.ActionStatusProgressing, api.ActionResultNone, "Converting resources to unstructured format...")
		setupMockSendStatus(t, mockCtx, api.ActionStatusProgressing, api.ActionResultNone, "Converting resources to YAML format...")
		setupMockSendStatusContains(t, mockCtx, api.ActionStatusCompleted, api.ActionResultImportCompleted, "Imported")
	case 6: // Legacy resource info list flow
		setupMockSendStatus(t, mockCtx, api.ActionStatusProgressing, api.ActionResultNone, "Parsing provided resource information...")
		setupMockSendStatusContains(t, mockCtx, api.ActionStatusProgressing, api.ActionResultNone, "Found")
		setupMockSendStatus(t, mockCtx, api.ActionStatusProgressing, api.ActionResultNone, "Converting resources to unstructured format...")
		setupMockSendStatus(t, mockCtx, api.ActionStatusProgressing, api.ActionResultNone, "Retrieving live state of resources...")
		setupMockSendStatus(t, mockCtx, api.ActionStatusProgressing, api.ActionResultNone, "Converting resources to YAML format...")
		setupMockSendStatusContains(t, mockCtx, api.ActionStatusCompleted, api.ActionResultImportCompleted, "Imported")
	}
}

func setupMockGetResourcesWithParams(t *testing.T, mockClient *MockK8sClient, mockManager *MockResourceManager) {
	t.Helper()
	mockClient.On("List", mock.Anything, mock.Anything, mock.Anything).Return(nil).Run(func(args mock.Arguments) {
		list := args.Get(1).(*unstructured.UnstructuredList)
		list.Items = []unstructured.Unstructured{*testConfigMap}
	})
}

func setupMockGetAllClusterResources(t *testing.T, mockClient *MockK8sClient, mockManager *MockResourceManager) {
	t.Helper()
	setupMockGetResourcesWithParams(t, mockClient, mockManager)
}

func setupMockGetLiveObjects(t *testing.T, mockClient *MockK8sClient, mockManager *MockResourceManager) {
	t.Helper()
	mockClient.On("Get", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil).Run(func(args mock.Arguments) {
		obj := args.Get(2).(*unstructured.Unstructured)
		obj.Object = testConfigMap.Object
	})
	mockManager.On("Client").Return(mockClient)
}
