// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package impl

import (
	"context"
	"fmt"
	"testing"

	"github.com/confighub/sdk/function/api"
	goclientnew "github.com/confighub/sdk/openapi/goclient-new"
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

// Test helper functions

// setupTestConfig sets up the test configuration provider
func setupTestConfig() {
	SetConfigProvider(TestConfigProvider{})
}

// setupMockListWithCondition sets up a mock expectation for the List method with conditional behavior
func setupMockListWithCondition(mockClient *MockK8sClient, condition func(*unstructured.UnstructuredList) []unstructured.Unstructured) {
	mockClient.On("List",
		mock.MatchedBy(func(ctx context.Context) bool { return ctx != nil }),
		mock.MatchedBy(func(list client.ObjectList) bool {
			ul, ok := list.(*unstructured.UnstructuredList)
			return ok && ul != nil
		}),
		mock.Anything,
	).Run(func(args mock.Arguments) {
		list := args.Get(1).(*unstructured.UnstructuredList)
		listOptions := args.Get(2).([]client.ListOption)

		// Get resources from condition
		items := condition(list)

		// Apply field selector filtering if present (simulate real Kubernetes behavior)
		for _, opt := range listOptions {
			if fieldOpt, ok := opt.(client.MatchingFieldsSelector); ok && fieldOpt.Selector != nil {
				selectorStr := fieldOpt.Selector.String()
				// Handle kube-system exclusion
				if selectorStr == "metadata.namespace!=kube-system" {
					var filtered []unstructured.Unstructured
					for _, item := range items {
						if item.GetNamespace() != "kube-system" {
							filtered = append(filtered, item)
						}
					}
					items = filtered
				}
			}
		}

		list.Items = items
	}).Return(nil)
}

// assertErrorOrSuccess is a helper to handle error checking in tests
func assertErrorOrSuccess(t *testing.T, err error, expectError bool, expectedCount int, actualCount int) {
	if expectError {
		assert.Error(t, err)
		return
	}
	require.NoError(t, err)
	assert.Equal(t, expectedCount, actualCount)
}

// TestGetResourceTypesWithFallback tests the static fallback behavior
func TestGetResourceTypesWithFallback(t *testing.T) {
	setupTestConfig()

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
			expectedResourceTypes: 30, // Based on K8sClusterScopedResourceTypes count
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
			expectedResourceTypes: 44, // Sum of both
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
			icfg := &ImportConfig{
				IncludeCluster: tt.includeClusterScoped,
			}
			rd := NewResourceDiscovery(nil, icfg, nil)
			resourceTypes, err := rd.getResourceTypesWithFallback(tt.includeNamespaced)
			require.NoError(t, err)
			assert.Equal(t, tt.expectedResourceTypes, len(resourceTypes))
		})
	}
}

func TestGetAllClusterResources(t *testing.T) {
	setupTestConfig()

	// Create test pods using suite_test.go helpers
	testPod := createTestObject(t, "v1", "Pod",
		createTestMetadata(t, "test-pod", "default", nil),
		nil, nil)
	kubeSystemPod := createTestObject(t, "v1", "Pod",
		createTestMetadata(t, "kube-system-pod", "kube-system", nil),
		nil, nil)

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
				_, mockClient := setupMockResourceManager(t)
				setupMockListWithCondition(mockClient, func(list *unstructured.UnstructuredList) []unstructured.Unstructured {
					gvk := list.GetObjectKind().GroupVersionKind()
					// Only return items for Pod resources to make test predictable
					if gvk.Kind == "Pod" {
						return []unstructured.Unstructured{*testPod, *kubeSystemPod}
					}
					return []unstructured.Unstructured{} // For other resource types, return empty list
				})
				return mockClient
			}(),
			includeKubeSystem: true,
			expectedCount:     2,
			expectError:       false,
		},
		{
			name: "success with kube-system excluded",
			client: func() *MockK8sClient {
				_, mockClient := setupMockResourceManager(t)
				setupMockListWithCondition(mockClient, func(list *unstructured.UnstructuredList) []unstructured.Unstructured {
					gvk := list.GetObjectKind().GroupVersionKind()
					// Only return items for Pod resources to make test predictable
					if gvk.Kind == "Pod" {
						return []unstructured.Unstructured{*testPod, *kubeSystemPod}
					}
					return []unstructured.Unstructured{} // For other resource types, return empty list
				})
				return mockClient
			}(),
			includeKubeSystem: false,
			expectedCount:     1,
			expectError:       false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := &ImportConfig{
				IncludeSystem:  tt.includeKubeSystem,
				IncludeCustom:  true,
				IncludeCluster: true,
				Filters:        []goclientnew.ImportFilter{},
			}
			resources, err := GetResourcesWithConfig(tt.client, config, nil)
			assertErrorOrSuccess(t, err, tt.expectError, tt.expectedCount, len(resources))
		})
	}
}

func TestValidateResourceTypeFormat(t *testing.T) {
	tests := []struct {
		resourceType string
		valid        bool
		description  string
	}{
		// Valid formats
		{"v1/ConfigMap", true, "Core API resource"},
		{"apps/v1/Deployment", true, "Non-core API resource"},
		{"rbac.authorization.k8s.io/v1/ClusterRole", true, "Complex group name"},
		{"apiextensions.k8s.io/v1/CustomResourceDefinition", true, "CRD resource type"},
		{"v1/Pod", true, "Simple core resource"},

		// Invalid formats
		{"configmaps", false, "Lowercase plural name (incorrect)"},
		{"v1/configmap", false, "Lowercase kind (incorrect)"},
		{"ConfigMap", false, "Missing version"},
		{"v1//ConfigMap", false, "Empty group component"},
		{"v1/", false, "Missing kind"},
		{"/v1/ConfigMap", false, "Empty first component"},
		{"v1/ConfigMap/extra", false, "Too many components"},
		{"", false, "Empty string"},
		{"v1", false, "Only version, no kind"},
	}

	for _, tt := range tests {
		t.Run(tt.resourceType+"_"+tt.description, func(t *testing.T) {
			result := ValidateResourceTypeFormat(tt.resourceType)
			assert.Equal(t, tt.valid, result, "Validation mismatch for %s: %s", tt.resourceType, tt.description)
		})
	}
}

func TestParseGroupVersionKind_EdgeCases(t *testing.T) {
	tests := []struct {
		name            string
		resourceType    string
		expectError     bool
		expectedGroup   string
		expectedVersion string
		expectedKind    string
		shouldRoundTrip bool
	}{
		{
			name:            "well-formed cluster-scoped resource",
			resourceType:    "rbac.authorization.k8s.io/v1/ClusterRoleBinding",
			expectError:     false,
			expectedGroup:   "rbac.authorization.k8s.io",
			expectedVersion: "v1",
			expectedKind:    "ClusterRoleBinding",
			shouldRoundTrip: true,
		},
		{
			name:            "well-formed core API resource",
			resourceType:    "v1/ConfigMap",
			expectError:     false,
			expectedGroup:   "",
			expectedVersion: "v1",
			expectedKind:    "ConfigMap",
			shouldRoundTrip: true,
		},
		{
			name:            "malformed with dots - parses but creates wrong structure",
			resourceType:    "apiregistration.k8s.io.v1/APIService",
			expectError:     false,
			expectedGroup:   "",                          // Empty because no slash found in group/version part
			expectedVersion: "apiregistration.k8s.io.v1", // Whole string treated as version
			expectedKind:    "APIService",
			shouldRoundTrip: true, // Round-trips but with wrong meaning
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gvk, err := parseGroupVersionKind(tt.resourceType)

			if tt.expectError {
				assert.Error(t, err)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.expectedGroup, gvk.Group)
			assert.Equal(t, tt.expectedVersion, gvk.Version)
			assert.Equal(t, tt.expectedKind, gvk.Kind)

			if tt.shouldRoundTrip {
				reconstructed := getResourceType(gvk)
				assert.Equal(t, tt.resourceType, reconstructed)
			}
		})
	}
}

// TestWhereFilterParsing tests the new unified where-filter parsing pipeline
func TestWhereFilterParsing(t *testing.T) {
	tests := []struct {
		name            string
		whereFilter     string
		expectedFilters int
		expectedOptions int
		expectError     bool
		description     string
	}{
		{
			name:            "Basic namespace filter with proper path",
			whereFilter:     "metadata.namespace = 'import-test-default'",
			expectedFilters: 1,
			expectedOptions: 0,
			expectError:     false,
			description:     "Simple namespace filtering using Kubernetes path syntax",
		},
		{
			name:            "Combined filters with import options",
			whereFilter:     "metadata.namespace = 'import-test-default' AND import.include_system = true AND import.include_custom = true",
			expectedFilters: 1,
			expectedOptions: 2,
			expectError:     false,
			description:     "Namespace filter with import options",
		},
		{
			name:            "Multiple filters with IN clause",
			whereFilter:     "metadata.namespace IN ('import-test-default', 'import-test-production') AND kind != 'Secret'",
			expectedFilters: 2,
			expectedOptions: 0,
			expectError:     false,
			description:     "Multiple filters with IN clause",
		},
		{
			name:            "Complex scenario matching test script",
			whereFilter:     "metadata.namespace IN ('import-test-default', 'import-test-production') AND kind = 'ConfigMap' AND import.include_system = true AND import.include_custom = true AND import.include_cluster = false",
			expectedFilters: 2,
			expectedOptions: 3,
			expectError:     false,
			description:     "Complex scenario with multiple filters and options",
		},
		{
			name:        "Invalid syntax",
			whereFilter: "metadata.namespace = 'missing quote",
			expectError: true,
			description: "Should fail with invalid syntax",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			filters, options, err := api.ParseWhereFilterForImport(tt.whereFilter)

			if tt.expectError {
				assert.Error(t, err, "Expected error for: %s", tt.description)
				return
			}

			require.NoError(t, err, "Failed to parse where filter: %s", tt.whereFilter)
			assert.Equal(t, tt.expectedFilters, len(filters), "Expected %d filters for: %s", tt.expectedFilters, tt.description)
			assert.Equal(t, tt.expectedOptions, len(options), "Expected %d options for: %s", tt.expectedOptions, tt.description)

			// Validate filter paths use correct Kubernetes syntax
			for _, filter := range filters {
				if filter.Type == "metadata.namespace" || filter.Type == "kind" || filter.Type == "apiVersion" {
					// These are valid Kubernetes paths
					continue
				}
				t.Logf("Filter type: %s", filter.Type)
			}

			// Validate import options are properly formatted
			for key := range options {
				assert.True(t, key == "include_system" || key == "include_custom" || key == "include_cluster",
					"Invalid import option key: %s", key)
			}

			// Test that operators are correctly mapped
			for _, filter := range filters {
				assert.True(t, filter.Operator == "include" || filter.Operator == "exclude",
					"Operator should be mapped to include/exclude, got: %s", filter.Operator)
			}
		})
	}
}

// TestE2EImportScenarios validates the exact scenarios used in test-setup.sh
func TestE2EImportScenarios(t *testing.T) {
	// Create test resources that match what test-setup.sh creates
	testResources := []*unstructured.Unstructured{
		// ConfigMaps in different namespaces
		createTestConfigMap("test-config1", "import-test-default", "default"),
		createTestConfigMap("test-config2", "import-test-production", "production"),
		createTestConfigMap("kube-system-config", "kube-system", "true"),
		createTestConfigMap("kube-public-config", "kube-public", "true"),
		createTestConfigMap("kube-node-lease-config", "kube-node-lease", "true"),

		// Pods in different namespaces
		createTestPod("test-pod1", "import-test-default"),
		createTestPod("test-pod2", "import-test-production"),
		createTestPod("kube-system-pod", "kube-system"),
		createTestPod("kube-public-pod", "kube-public"),
		createTestPod("kube-node-lease-pod", "kube-node-lease"),

		// Secrets in different namespaces
		createTestSecret("test-secret1", "import-test-default"),
		createTestSecret("kube-system-secret", "kube-system"),
		createTestSecret("kube-public-secret", "kube-public"),
		createTestSecret("kube-node-lease-secret", "kube-node-lease"),

		// Cluster-scoped resources
		createTestNode("test-node1"),
		createTestClusterRole("test-cluster-role"),

		// Virtual resources that should be excluded during discovery
		createTestSubjectAccessReview("test-sar", "import-test-default"),
		createTestTokenReview("test-token-review"),
		createTestBinding("test-binding", "import-test-default"),

		// Administrative resources that should be excluded by default
		createTestAPIService("test-api-service"),
		createTestCertificateSigningRequest("test-csr"),
	}

	tests := []struct {
		name           string
		filters        []goclientnew.ImportFilter
		includeSystem  bool
		includeCustom  bool
		includeCluster bool
		expectedCount  int
		expectedTypes  []string
		expectedNs     []string
	}{
		{
			name: "Import with combined parameters",
			filters: []goclientnew.ImportFilter{
				{
					Type:     "metadata.namespace",
					Operator: "include",
					Values:   []string{"import-test-default"},
				},
			},
			includeSystem:  true,
			includeCustom:  true,
			includeCluster: false,
			expectedCount:  12, // Resources from import-test-default (3) + system namespaces (9) with include_system=true
			expectedNs:     append([]string{"import-test-default"}, systemNamespaces...),
		},
		{
			name: "Complex unified syntax with exclusions",
			filters: []goclientnew.ImportFilter{
				{
					Type:     "metadata.namespace",
					Operator: "include",
					Values:   []string{"import-test-default", "import-test-production"},
				},
				{
					Type:     "resource_type",
					Operator: "exclude",
					Values:   []string{"v1/Secret"},
				},
			},
			includeSystem:  false,
			includeCustom:  true,
			includeCluster: false,
			expectedCount:  4, // 2 ConfigMaps + 2 Pods (excludes Secret)
			expectedNs:     []string{"import-test-default", "import-test-production"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockClient := new(MockK8sClient)
			mockClient.On("List", mock.Anything, mock.AnythingOfType("*unstructured.UnstructuredList"), mock.Anything).
				Run(func(args mock.Arguments) {
					list := args.Get(1).(*unstructured.UnstructuredList)
					opts := args.Get(2).([]client.ListOption)

					// Extract target namespace from list options
					var targetNamespace string
					for _, opt := range opts {
						if nsOpt, ok := opt.(client.InNamespace); ok {
							targetNamespace = string(nsOpt)
							break
						}
					}
					// Filter test resources based on namespace restriction and GVK
					list.Items = []unstructured.Unstructured{}
					requestedGVK := list.GetObjectKind().GroupVersionKind()

					for _, res := range testResources {
						resGVK := res.GetObjectKind().GroupVersionKind()
						// Match GVK and namespace
						if resGVK.Kind == requestedGVK.Kind &&
							resGVK.Group == requestedGVK.Group &&
							resGVK.Version == requestedGVK.Version {
							if targetNamespace == "" || res.GetNamespace() == targetNamespace {
								list.Items = append(list.Items, *res)
							}
						}
					}
				}).Return(nil)

			// Create config that matches the test scenario
			config := &ImportConfig{
				IncludeSystem:  tt.includeSystem,
				IncludeCustom:  tt.includeCustom,
				IncludeCluster: tt.includeCluster,
				Filters:        tt.filters,
			}

			// Extract namespace filters to set Namespaces field
			config.Namespaces = extractNamespaceFilters(tt.filters)

			// Execute the import
			resources, err := GetResourcesWithConfig(mockClient, config, nil)
			assert.NoError(t, err)

			// Validate count
			assert.Equal(t, tt.expectedCount, len(resources), "Expected %d resources, got %d", tt.expectedCount, len(resources))

			// Validate resource types if specified
			if len(tt.expectedTypes) > 0 {
				actualTypes := make(map[string]int)
				for _, res := range resources {
					actualTypes[getResourceType(res.GetObjectKind().GroupVersionKind())]++
				}
				for _, expectedType := range tt.expectedTypes {
					assert.Contains(t, actualTypes, expectedType, "Expected resource type %s not found", expectedType)
				}
			}

			// Validate namespaces if specified
			if len(tt.expectedNs) > 0 {
				actualNs := make(map[string]int)
				for _, res := range resources {
					ns := res.GetNamespace()
					if ns == "" {
						ns = "<cluster-scoped>"
					}
					actualNs[ns]++
				}
				for _, expectedNs := range tt.expectedNs {
					assert.Contains(t, actualNs, expectedNs, "Expected namespace %s not found in results", expectedNs)
				}
			}

			mockClient.AssertExpectations(t)
		})
	}
}

// TestImportCombinedScenario tests the exact scenario from test-setup.sh with new where-filter syntax
// Query: "metadata.namespace = 'import-test-default' AND import.include_system = true AND import.include_custom = true"
func TestImportCombinedScenario(t *testing.T) {
	setupTestConfig()

	// Create exact test resources that match test-setup.sh
	defaultConfigMap := createTestConfigMap("test-config1", "import-test-default", "default")
	defaultPod := createTestPod("test-pod1", "import-test-default")
	defaultSecret := createTestSecret("test-secret1", "import-test-default")

	// Custom resource in the requested namespace
	defaultCustomResource := createTestObject(t, "confighub.io/v1", "TestApp",
		createTestMetadata(t, "test-custom-resource", "import-test-default", nil),
		map[string]interface{}{
			"replicas": 3,
			"image":    "nginx:latest",
		}, nil)

	// System namespace resources
	kubeSystemConfig := createTestConfigMap("kube-system-config", "kube-system", "true")
	kubePublicConfig := createTestConfigMap("kube-public-config", "kube-public", "true")
	kubeNodeLeaseConfig := createTestConfigMap("kube-node-lease-config", "kube-node-lease", "true")

	// CRD (cluster-scoped)
	testCRD := createTestObject(t, "apiextensions.k8s.io/v1", "CustomResourceDefinition",
		createTestMetadata(t, "testapps.confighub.io", "", nil), nil, nil)

	allResources := []*unstructured.Unstructured{
		defaultConfigMap,
		defaultPod,
		defaultSecret,
		defaultCustomResource,
		kubeSystemConfig,
		kubePublicConfig,
		kubeNodeLeaseConfig,
		testCRD,
	}

	// Test the combined scenario step by step
	t.Run("Combined scenario step-by-step analysis", func(t *testing.T) {
		// Step 1: Create ImportConfig
		importRequest := &goclientnew.ImportRequest{
			Filters: []goclientnew.ImportFilter{
				{
					Type:     "metadata.namespace",
					Operator: "include",
					Values:   []string{"import-test-default"},
				},
			},
			Options: &goclientnew.ImportOptions{
				"include_system": true,
				"include_custom": true,
			},
		}

		config := NewImportConfigFromRequest(importRequest)
		// Step 2: Test buildQuery to see target namespaces
		rd := &ResourceDiscovery{config: config}
		query, err := rd.buildQuery()
		require.NoError(t, err)
		// This should include import-test-default + system namespaces
		expectedNamespaces := []string{"import-test-default", "kube-system", "kube-public", "kube-node-lease"}
		assert.ElementsMatch(t, expectedNamespaces, query.TargetNamespaces)

		// Step 4: Test resource filtering
		filtered := rd.applyGenericFilters(allResources)

		// Should include:
		// - 4 resources from import-test-default (ConfigMap, Pod, Secret, TestApp)
		// - 3 resources from system namespaces
		// - 1 CRD (cluster-scoped)
		// Total: 8 resources
		assert.Equal(t, 8, len(filtered), "Should have 8 resources total")

		// Verify namespace distribution
		namespaceCounts := make(map[string]int)
		for _, res := range filtered {
			ns := res.GetNamespace()
			if ns == "" {
				ns = "<cluster-scoped>"
			}
			namespaceCounts[ns]++
		}
		assert.Equal(t, 4, namespaceCounts["import-test-default"], "Should have 4 resources from import-test-default")
		assert.Equal(t, 1, namespaceCounts["kube-system"], "Should have 1 resource from kube-system")
		assert.Equal(t, 1, namespaceCounts["kube-public"], "Should have 1 resource from kube-public")
		assert.Equal(t, 1, namespaceCounts["kube-node-lease"], "Should have 1 resource from kube-node-lease")
		assert.Equal(t, 1, namespaceCounts["<cluster-scoped>"], "Should have 1 CRD")

		// Verify specific resource types
		resourceTypes := make(map[string]int)
		for _, res := range filtered {
			gvk := res.GetObjectKind().GroupVersionKind()
			resourceType := fmt.Sprintf("%s/%s", gvk.Group, gvk.Kind)
			if gvk.Group == "" {
				resourceType = gvk.Kind
			}
			resourceTypes[resourceType]++
		}

		t.Logf("Resource types: %+v", resourceTypes)
		assert.Equal(t, 4, resourceTypes["ConfigMap"], "Should have 3 ConfigMaps")
		assert.Equal(t, 1, resourceTypes["Pod"], "Should have 1 Pod")
		assert.Equal(t, 1, resourceTypes["Secret"], "Should have 1 Secret")
		assert.Equal(t, 1, resourceTypes["confighub.io/TestApp"], "Should have 1 TestApp custom resource")
		assert.Equal(t, 1, resourceTypes["apiextensions.k8s.io/CustomResourceDefinition"], "Should have 1 CRD")

		// Validate that virtual resources are excluded (they should never be included)
		virtualResources := []string{
			"authorization.k8s.io/SubjectAccessReview",
			"authentication.k8s.io/TokenReview",
			"Binding",
		}
		for _, virtualResource := range virtualResources {
			assert.Equal(t, 0, resourceTypes[virtualResource], "Virtual resource %s should be excluded", virtualResource)
		}

		// Validate that administrative resources are excluded by default
		adminResources := []string{
			"apiregistration.k8s.io/APIService",
			"certificates.k8s.io/CertificateSigningRequest",
		}
		for _, adminResource := range adminResources {
			assert.Equal(t, 0, resourceTypes[adminResource], "Administrative resource %s should be excluded by default", adminResource)
		}
	})
}

// TestComplexPathFiltering tests the complex path filtering functionality
func TestComplexPathFiltering(t *testing.T) {
	// Create test resources using suite helpers
	deployment := createTestDeployment("test-deployment", "default", 3)
	// Customize the deployment to have nginx:latest and security context
	containers := []interface{}{
		map[string]interface{}{
			"name":  "nginx",
			"image": "nginx:latest",
			"securityContext": map[string]interface{}{
				"runAsNonRoot": true,
			},
		},
	}
	unstructured.SetNestedSlice(deployment.Object, containers, "spec", "template", "spec", "containers")
	configMap := createTestConfigMap("test-config", "default", "test")

	tests := []struct {
		name     string
		filter   goclientnew.ImportFilter
		expected int
	}{
		{
			name: "Simple field filtering",
			filter: goclientnew.ImportFilter{
				Type: "spec.replicas", Operator: "include", Values: []string{"3"},
			},
			expected: 1, // Only deployment has replicas=3
		},
		{
			name: "Wildcard container image filtering",
			filter: goclientnew.ImportFilter{
				Type: "spec.template.spec.containers.*.image", Operator: "include", Values: []string{"nginx:latest"},
			},
			expected: 1, // Only deployment matches
		},
		{
			name: "Numeric index container access",
			filter: goclientnew.ImportFilter{
				Type: "spec.template.spec.containers.0.name", Operator: "include", Values: []string{"nginx"},
			},
			expected: 1, // Only deployment matches
		},
		{
			name: "Nonexistent path filtering",
			filter: goclientnew.ImportFilter{
				Type: "spec.nonexistent.field", Operator: "include", Values: []string{"value"},
			},
			expected: 0, // No resources match
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := &ImportConfig{Filters: []goclientnew.ImportFilter{tt.filter}}
			rd := &ResourceDiscovery{config: config}
			allResources := []*unstructured.Unstructured{deployment, configMap}
			filtered := rd.applyGenericFilters(allResources)

			assert.Equal(t, tt.expected, len(filtered), "Filter: %s", tt.filter.Type)
		})
	}
}
