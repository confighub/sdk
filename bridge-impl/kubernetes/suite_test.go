// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package kubernetes

import (
	"testing"

	"github.com/confighub/sdk/core/worker/api"
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
)

// Test helper functions

// setupKubernetesClientFactory swaps in a fake KubernetesClientFactory for the
// duration of a test and returns a restore function the caller defers.
func setupKubernetesClientFactory(t *testing.T, mockClient *MockK8sClient) func() {
	t.Helper()
	originalFunc := KubernetesClientFactory
	KubernetesClientFactory = func(kubeContext string) (KubernetesClient, error) {
		return mockClient, nil
	}
	return func() { KubernetesClientFactory = originalFunc }
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

// Helper functions for import tests
func setupImportStatusMocks(t *testing.T, mockCtx *MockBridgeWorkerContext, expectedCalls int) {
	t.Helper()
	switch expectedCalls {
	case 3: // Standard import flow
		SetupMockSendStatus(t, mockCtx, api.ActionStatusProgressing, api.ActionResultNone, "Fetching resources from Kubernetes cluster...")
		SetupMockSendStatusContains(t, mockCtx, api.ActionStatusProgressing, api.ActionResultNone, "Found")
		SetupMockSendStatus(t, mockCtx, api.ActionStatusProgressing, api.ActionResultNone, "Converting resources to unstructured format...")
		SetupMockSendStatus(t, mockCtx, api.ActionStatusProgressing, api.ActionResultNone, "Converting resources to YAML format...")
		SetupMockSendStatusContains(t, mockCtx, api.ActionStatusCompleted, api.ActionResultImportCompleted, "Imported")
	case 4: // Legacy resource info list flow
		SetupMockSendStatusContains(t, mockCtx, api.ActionStatusProgressing, api.ActionResultNone, "Found")
		SetupMockSendStatus(t, mockCtx, api.ActionStatusProgressing, api.ActionResultNone, "Retrieving live state of resources...")
		SetupMockSendStatus(t, mockCtx, api.ActionStatusProgressing, api.ActionResultNone, "Converting resources to YAML format...")
		SetupMockSendStatusContains(t, mockCtx, api.ActionStatusCompleted, api.ActionResultImportCompleted, "Imported")
	}
}

func setupMockGetResourcesWithParams(t *testing.T, mockClient *MockK8sClient) {
	t.Helper()
	mockClient.On("List", mock.Anything, mock.Anything, mock.Anything).Return(nil).Run(func(args mock.Arguments) {
		list := args.Get(1).(*unstructured.UnstructuredList)
		list.Items = []unstructured.Unstructured{*testConfigMap}
	})
}

func setupMockGetAllClusterResources(t *testing.T, mockClient *MockK8sClient) {
	t.Helper()
	setupMockGetResourcesWithParams(t, mockClient)
}

func setupMockGetLiveObjects(t *testing.T, mockClient *MockK8sClient) {
	t.Helper()
	mockClient.On("Get", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil).Run(func(args mock.Arguments) {
		obj := args.Get(2).(*unstructured.Unstructured)
		obj.Object = testConfigMap.Object
	})
}
