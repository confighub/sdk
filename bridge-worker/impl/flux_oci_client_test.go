// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package impl

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/fluxcd/pkg/oci"
	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/crane"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

// Mock structures for testing
type MockClient struct {
	mock.Mock
}

func (m *MockClient) LoginWithCredentials(cred string) error {
	args := m.Called(cred)
	return args.Error(0)
}

func (m *MockClient) LoginWithProvider(ctx context.Context, url string, provider oci.Provider) error {
	args := m.Called(ctx, url, provider)
	return args.Error(0)
}

func (m *MockClient) Delete(ctx context.Context, url string) error {
	args := m.Called(ctx, url)
	return args.Error(0)
}

func (m *MockClient) GetOptions() []crane.Option {
	args := m.Called()
	return args.Get(0).([]crane.Option)
}

type MockKeychainHelper struct{}

func (m *MockKeychainHelper) Get(image string) (string, string, error) {
	return "user", "pass", nil
}

type MockKeychain struct {
	FailForInvalidRepo bool
}

func (m *MockKeychain) Resolve(target authn.Resource) (authn.Authenticator, error) {
	if m.FailForInvalidRepo && strings.Contains(target.String(), "invalid-repo") {
		return nil, fmt.Errorf("failed to resolve keychain for invalid repo")
	}
	return authn.FromConfig(authn.AuthConfig{
		Username: "user",
		Password: "pass",
	}), nil
}

// Test GetDockerConfigCredentials
func TestGetDockerConfigCredentials(t *testing.T) {
	// Setup a temporary Docker config file
	tempDir := t.TempDir()
	configPath := tempDir + "/config.json"
	os.Setenv("DOCKER_CONFIG", tempDir)

	dockerConfig := DockerConfig{
		Auths: map[string]DockerAuth{
			"my-registry.com": {Auth: base64.StdEncoding.EncodeToString([]byte("user:pass"))},
		},
	}
	data, _ := json.Marshal(dockerConfig)
	_ = os.WriteFile(configPath, data, 0644)

	// Test valid registry
	cred := GetDockerConfigCredentials("my-registry.com/repo/image:tag")
	assert.Equal(t, "user:pass", cred)

	// Test invalid registry
	cred = GetDockerConfigCredentials("unknown-registry.com/repo/image:tag")
	assert.Equal(t, "", cred)
}

func TestGetDockerConfigCredentials_Invalid(t *testing.T) {
	os.Setenv("DOCKER_CONFIG", "/path/to/invalid/config")
	defer os.Unsetenv("DOCKER_CONFIG")

	cred := GetDockerConfigCredentials("my-registry.com/repo/image:tag")
	assert.Equal(t, "", cred)
}

// Test TryAuth
func TestTryAuth(t *testing.T) {
	auths := map[string]DockerAuth{
		"my-registry.com": {Auth: base64.StdEncoding.EncodeToString([]byte("user:pass"))},
	}

	// Test valid key
	cred := TryAuth(auths, "my-registry.com")
	assert.Equal(t, "user:pass", cred)

	// Test invalid key
	cred = TryAuth(auths, "unknown-registry.com")
	assert.Equal(t, "", cred)
}

// Test GetDefaultKeychainCredentials
func TestGetDefaultKeychainCredentials(t *testing.T) {
	params := &FluxOCIParams{
		Repository: "my-registry.com/repo",
		Tag:        "latest",
	}

	// Use the mock keychain
	mockKeychain := &MockKeychain{}

	cred := GetDefaultKeychainCredentials(params, mockKeychain)
	assert.Equal(t, "user:pass", cred)
}

func TestGetDefaultKeychainCredentials_Invalid(t *testing.T) {
	params := &FluxOCIParams{
		Repository: "invalid-repo",
		Tag:        "latest",
	}

	mockKeychain := &MockKeychain{FailForInvalidRepo: true}

	cred := GetDefaultKeychainCredentials(params, mockKeychain)
	assert.Equal(t, "", cred)
}

// Test GetCloudProvider
func TestGetCloudProvider(t *testing.T) {
	assert.Equal(t, oci.ProviderAWS, GetCloudProvider("AWS"))
	assert.Equal(t, oci.ProviderAzure, GetCloudProvider("Azure"))
	assert.Equal(t, oci.ProviderGCP, GetCloudProvider("GCP"))
	assert.Equal(t, oci.ProviderGeneric, GetCloudProvider("Generic"))
}

// Test GetK8sSecretCredentials
func TestGetK8sSecretCredentials(t *testing.T) {
	// Mock Kubernetes client
	ctx := context.Background()
	params := &FluxOCIParams{
		KubernetesSecretName:      "test-secret",
		KubernetesSecretNamespace: "default",
	}

	// Simulate missing secret
	cred := GetK8sSecretCredentials(ctx, params)
	assert.Equal(t, "", cred)
}

func TestLoginToRegistry_K8sSecret(t *testing.T) {
	workerConfig := &FluxOCIWorkerConfig{
		AuthMethod:                  AuthMethodKubernetes,
		KubernetesSecretCredentials: "user:pass",
	}
	params := &FluxOCIParams{
		Repository: "my-registry.com/repo",
		Tag:        "latest",
	}

	mockClient := new(MockClient)
	mockClient.On("LoginWithCredentials", "user:pass").Return(nil)
	newFunc := func() OCIClient {
		return mockClient
	}
	client, err := LoginToRegistry(context.Background(), workerConfig, params, newFunc)
	assert.NoError(t, err)
	assert.NotNil(t, client)
	mockClient.AssertCalled(t, "LoginWithCredentials", "user:pass")
}

func TestLoginToRegistry_CloudProvider(t *testing.T) {
	workerConfig := &FluxOCIWorkerConfig{
		AuthMethod: AuthMethodCloud,
	}
	params := &FluxOCIParams{
		Repository: "my-registry.com/repo",
		Tag:        "latest",
		Provider:   ProviderAWS,
	}

	mockClient := new(MockClient)
	mockClient.On("LoginWithProvider", mock.Anything, "my-registry.com/repo:latest", oci.ProviderAWS).Return(nil)
	newFunc := func() OCIClient {
		return mockClient
	}
	client, err := LoginToRegistry(context.Background(), workerConfig, params, newFunc)
	assert.NoError(t, err)
	assert.NotNil(t, client)
	mockClient.AssertCalled(t, "LoginWithProvider", mock.Anything, "my-registry.com/repo:latest", oci.ProviderAWS)
}

func TestExtractCredentialsFromSecret_DockerConfigJSON(t *testing.T) {
	// Mock Kubernetes secret with `.dockerconfigjson`
	secret := corev1.Secret{
		Data: map[string][]byte{
			".dockerconfigjson": []byte(base64.StdEncoding.EncodeToString([]byte(`{
                "auths": {
                    "ghcr.io": {
                        "username": "user",
                        "password": "password",
                        "auth": "dXNlcjpwYXNzd29yZAo="
                    }
                }
            }`))),
		},
	}

	creds := ExtractCredentialsFromSecret(secret)
	assert.Equal(t, "user:password", creds)
}

func TestExtractCredentialsFromSecret_UsernamePassword(t *testing.T) {
	// Mock Kubernetes secret with `username` and `password`
	secret := corev1.Secret{
		Data: map[string][]byte{
			"username": []byte("user"),
			"password": []byte("password"),
		},
	}

	creds := ExtractCredentialsFromSecret(secret)
	assert.Equal(t, "user:password", creds)
}

func TestExtractCredentialsFromSecret_NoCredentials(t *testing.T) {
	// Mock Kubernetes secret with no credentials
	secret := corev1.Secret{
		Data: map[string][]byte{},
	}

	creds := ExtractCredentialsFromSecret(secret)
	assert.Equal(t, "", creds)
}

// TODO: Dependencies on DefaultKeychainCredentials and GetDockerConfigCredentials
// make these tests hard to isolate. commenting out for now.
//
// func TestLoginToRegistry_DockerConfig(t *testing.T) {
// 	workerConfig := &FluxOCIWorkerConfig{
// 		AuthMethod: AuthMethodDockerConfig,
// 	}
// 	params := &FluxOCIParams{
// 		Repository: "my-registry.com/repo",
// 		Tag:        "latest",
// 	}
// 	mockClient := new(MockClient)
// 	mockClient.On("LoginWithCredentials", "user:pass").Return(nil)
// 	newFunc := func() OCIClient {
// 		return mockClient
// 	}
// 	client, err := LoginToRegistry(context.Background(), workerConfig, params, newFunc)
// 	assert.NoError(t, err)
// 	assert.NotNil(t, client)
// 	mockClient.AssertCalled(t, "LoginWithCredentials", "user:pass")
// }
// func TestLoginToRegistry_DefaultKeychain(t *testing.T) {
// 	workerConfig := &FluxOCIWorkerConfig{
// 		AuthMethod: AuthMethodKeychain,
// 	}
// 	params := &FluxOCIParams{
// 		Repository: "my-registry.com/repo",
// 		Tag:        "latest",
// 	}
// 	mockClient := new(MockClient)
// 	mockClient.On("LoginWithCredentials", "user:pass").Return(nil)
// 	newFunc := func() OCIClient {
// 		return mockClient
// 	}
// 	client, err := LoginToRegistry(context.Background(), workerConfig, params, newFunc)
// 	assert.NoError(t, err)
// 	assert.NotNil(t, client)
// 	mockClient.AssertCalled(t, "LoginWithCredentials", "user:pass")
// }

// Define shared constants and helper functions to reduce duplicate code
const (
	namespace           = "default"
	podName             = "test-pod"
	serviceAccountName  = "test-service-account"
	imagePullSecretName = "test-pull-secret"
)

func createFakePod() *corev1.Pod {
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      podName,
			Namespace: namespace,
		},
		Spec: corev1.PodSpec{
			ServiceAccountName: serviceAccountName,
		},
	}
}

func createFakeServiceAccount(imagePullSecrets []corev1.LocalObjectReference) *corev1.ServiceAccount {
	return &corev1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{
			Name:      serviceAccountName,
			Namespace: namespace,
		},
		ImagePullSecrets: imagePullSecrets,
	}
}

func createFakeSecret(data map[string][]byte) *corev1.Secret {
	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      imagePullSecretName,
			Namespace: namespace,
		},
		Data: data,
	}
}

// Refactor tests to use shared constants and helper functions
func TestGetCredentialsFromImagePullSecrets(t *testing.T) {
	// Setup the fake Kubernetes client
	s := scheme.Scheme
	_ = corev1.AddToScheme(s)

	pod := createFakePod()
	serviceAccount := createFakeServiceAccount([]corev1.LocalObjectReference{
		{Name: imagePullSecretName},
	})
	dockerConfigJSON := `{
		"auths": {
			"ghcr.io": {
				"username": "user",
				"password": "pass",
				"auth": "dXNlcjpwYXNz"
			}
		}
	}`
	imagePullSecret := createFakeSecret(map[string][]byte{
		".dockerconfigjson": []byte(dockerConfigJSON),
	})

	client := fake.NewClientBuilder().
		WithScheme(s).
		WithObjects(pod, serviceAccount, imagePullSecret).
		Build()

	t.Setenv("POD_NAMESPACE", namespace)
	t.Setenv("POD_NAME", podName)

	ctx := context.Background()
	cred := GetCredentialsFromImagePullSecrets(ctx, client)

	assert.Equal(t, "user:pass", cred, "Expected credentials to be extracted from imagePullSecrets")
}

func TestGetCredentialsFromImagePullSecrets_NoSecrets(t *testing.T) {
	s := scheme.Scheme
	_ = corev1.AddToScheme(s)

	pod := createFakePod()
	serviceAccount := createFakeServiceAccount(nil)

	client := fake.NewClientBuilder().
		WithScheme(s).
		WithObjects(pod, serviceAccount).
		Build()

	t.Setenv("POD_NAMESPACE", namespace)
	t.Setenv("POD_NAME", podName)

	ctx := context.Background()
	cred := GetCredentialsFromImagePullSecrets(ctx, client)

	assert.Equal(t, "", cred, "Expected no credentials to be extracted when no imagePullSecrets are present")
}

func TestGetCredentialsFromImagePullSecrets_InvalidSecret(t *testing.T) {
	s := scheme.Scheme
	_ = corev1.AddToScheme(s)

	pod := createFakePod()
	serviceAccount := createFakeServiceAccount([]corev1.LocalObjectReference{
		{Name: imagePullSecretName},
	})
	imagePullSecret := createFakeSecret(map[string][]byte{
		".dockerconfigjson": []byte("invalid-json"),
	})

	client := fake.NewClientBuilder().
		WithScheme(s).
		WithObjects(pod, serviceAccount, imagePullSecret).
		Build()

	t.Setenv("POD_NAMESPACE", namespace)
	t.Setenv("POD_NAME", podName)

	ctx := context.Background()
	cred := GetCredentialsFromImagePullSecrets(ctx, client)

	assert.Equal(t, "", cred, "Expected no credentials to be extracted from invalid imagePullSecrets")
}
