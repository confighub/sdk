// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package impl

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// TestConfigProvider always returns an error to force static fallback
type TestConfigProvider struct{}

func (t TestConfigProvider) GetConfig() (*rest.Config, error) {
	return nil, fmt.Errorf("test config provider - force static fallback")
}

// TestGetResourceTypesWithFallback tests the static fallback behavior
func TestGetResourceTypesWithFallback(t *testing.T) {
	SetConfigProvider(TestConfigProvider{})
	tests := []struct {
		name                  string
		includeClusterScoped  bool
		includeNamespaced     bool
		expectedResourceTypes int
	}{
		{
			name:                  "cluster scoped only",
			includeClusterScoped:  true,
			includeNamespaced:     false,
			expectedResourceTypes: 29, // Based on K8sClusterScopedResourceTypes count
		},
		{
			name:                  "namespaced only",
			includeClusterScoped:  false,
			includeNamespaced:     true,
			expectedResourceTypes: 14, // Based on K8sNamespacedResourceTypes count
		},
		{
			name:                  "both scoped types",
			includeClusterScoped:  true,
			includeNamespaced:     true,
			expectedResourceTypes: 43, // Sum of both
		},
		{
			name:                  "neither scoped type",
			includeClusterScoped:  false,
			includeNamespaced:     false,
			expectedResourceTypes: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resourceTypes, err := getResourceTypesWithFallback(tt.includeClusterScoped, tt.includeNamespaced)
			require.NoError(t, err)
			assert.Equal(t, tt.expectedResourceTypes, len(resourceTypes))
		})
	}
}

func TestGetAllClusterResources(t *testing.T) {
	SetConfigProvider(TestConfigProvider{})
	tests := []struct {
		name              string
		client            *MockK8sClient
		includeKubeSystem bool
		expectedCount     int
		expectError       bool
	}{
		{
			name: "success with kube-system included",
			client: func() *MockK8sClient {
				mockedClient := new(MockK8sClient)
				mockedClient.On("List",
					mock.MatchedBy(func(ctx context.Context) bool { return ctx != nil }),
					mock.MatchedBy(func(list client.ObjectList) bool {
						ul, ok := list.(*unstructured.UnstructuredList)
						return ok && ul != nil
					}),
					mock.Anything,
				).Run(func(args mock.Arguments) {
					list := args.Get(1).(*unstructured.UnstructuredList)
					gvk := list.GetObjectKind().GroupVersionKind()

					// Only return items for Pod resources to make test predictable
					if gvk.Kind == "PodList" {
						list.Items = []unstructured.Unstructured{
							{
								Object: map[string]interface{}{
									"kind":       "Pod",
									"apiVersion": "v1",
									"metadata": map[string]interface{}{
										"name":      "test-pod",
										"namespace": "default",
									},
								},
							},
							{
								Object: map[string]interface{}{
									"kind":       "Pod",
									"apiVersion": "v1",
									"metadata": map[string]interface{}{
										"name":      "kube-system-pod",
										"namespace": "kube-system",
									},
								},
							},
						}
					}
					// For other resource types, return empty list (no resources found)
				}).Return(nil)
				return mockedClient
			}(),
			includeKubeSystem: true,
			expectedCount:     2,
			expectError:       false,
		},
		{
			name: "success with kube-system excluded",
			client: func() *MockK8sClient {
				mockedClient := new(MockK8sClient)
				mockedClient.On("List",
					mock.MatchedBy(func(ctx interface{}) bool { return ctx != nil }),
					mock.MatchedBy(func(list interface{}) bool {
						ul, ok := list.(*unstructured.UnstructuredList)
						return ok && ul != nil
					}),
					mock.Anything,
				).Run(func(args mock.Arguments) {
					list := args.Get(1).(*unstructured.UnstructuredList)
					gvk := list.GetObjectKind().GroupVersionKind()

					// Only return items for Pod resources to make test predictable
					if gvk.Kind == "PodList" {
						list.Items = []unstructured.Unstructured{
							{
								Object: map[string]interface{}{
									"kind":       "Pod",
									"apiVersion": "v1",
									"metadata": map[string]interface{}{
										"name":      "test-pod",
										"namespace": "default",
									},
								},
							},
							{
								Object: map[string]interface{}{
									"kind":       "Pod",
									"apiVersion": "v1",
									"metadata": map[string]interface{}{
										"name":      "kube-system-pod",
										"namespace": "kube-system",
									},
								},
							},
						}
					}
					// For other resource types, return empty list (no resources found)
				}).Return(nil)
				return mockedClient
			}(),
			includeKubeSystem: false,
			expectedCount:     1,
			expectError:       false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resources, err := GetAllClusterResources(tt.client, tt.includeKubeSystem)
			if tt.expectError {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.expectedCount, len(resources))
		})
	}
}

func TestGetCustomResourceDefinitions(t *testing.T) {
	SetConfigProvider(TestConfigProvider{})
	tests := []struct {
		name          string
		client        *MockK8sClient
		expectedCount int
		expectError   bool
	}{
		{
			name: "success with CRDs",
			client: func() *MockK8sClient {
				mockedClient := new(MockK8sClient)
				mockedClient.On("List",
					mock.MatchedBy(func(ctx interface{}) bool { return ctx != nil }),
					mock.MatchedBy(func(list interface{}) bool {
						ul, ok := list.(*unstructured.UnstructuredList)
						return ok && ul != nil
					}),
					mock.Anything,
				).Run(func(args mock.Arguments) {
					list := args.Get(1).(*unstructured.UnstructuredList)
					list.Items = []unstructured.Unstructured{
						{
							Object: map[string]interface{}{
								"kind":       "CustomResourceDefinition",
								"apiVersion": "apiextensions.k8s.io/v1",
								"metadata": map[string]interface{}{
									"name": "test.crd.io",
								},
							},
						},
					}
				}).Return(nil)
				return mockedClient
			}(),
			expectedCount: 1,
			expectError:   false,
		},
		{
			name: "empty CRD list",
			client: func() *MockK8sClient {
				mockedClient := new(MockK8sClient)
				mockedClient.On("List",
					mock.MatchedBy(func(ctx interface{}) bool { return ctx != nil }),
					mock.MatchedBy(func(list interface{}) bool {
						ul, ok := list.(*unstructured.UnstructuredList)
						return ok && ul != nil
					}),
					mock.Anything,
				).Run(func(args mock.Arguments) {
					list := args.Get(1).(*unstructured.UnstructuredList)
					list.Items = []unstructured.Unstructured{}
				}).Return(nil)
				return mockedClient
			}(),
			expectedCount: 0,
			expectError:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			crds, err := GetCustomResourceDefinitions(tt.client)
			if tt.expectError {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.expectedCount, len(crds))
		})
	}
}
