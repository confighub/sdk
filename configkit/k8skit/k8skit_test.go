// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package k8skit

import (
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/confighub/sdk/core/configkit/yamlkit"
	"github.com/confighub/sdk/core/function/api"
	"github.com/confighub/sdk/core/third_party/gaby"
)

func TestK8SFnResourceMap(t *testing.T) {
	tests := []struct {
		name api.ResourceName
		data string
		want yamlkit.ResourceNameToCategoryTypesMap
	}{
		{
			name: "Single document",
			data: createPod(t, "pod1", ""),
			want: yamlkit.ResourceNameToCategoryTypesMap{
				api.ResourceName("/pod1"): {{ResourceCategory: api.ResourceCategoryResource, ResourceType: testResourceTypePod}},
			},
		},
		{
			name: "Multiple documents",
			data: joinYAMLDocs(
				createPod(t, "pod1", ""),
				createService(t, "svc1", "", "svc1"),
			),
			want: yamlkit.ResourceNameToCategoryTypesMap{
				api.ResourceName("/pod1"): {{ResourceCategory: api.ResourceCategoryResource, ResourceType: testResourceTypePod}},
				api.ResourceName("/svc1"): {{ResourceCategory: api.ResourceCategoryResource, ResourceType: testResourceTypeService}},
			},
		},
		{
			name: "Two resources with the same name",
			data: `kind: Service
apiVersion: v1
metadata:
  name: headlamp
  namespace: confighubplaceholder
spec:
  ports:
    - port: 80
      targetPort: 4466
  selector:
    k8s-app: headlamp
---
kind: Deployment
apiVersion: apps/v1
metadata:
  name: headlamp
  namespace: confighubplaceholder
spec:
  replicas: 1
  selector:
    matchLabels:
      k8s-app: headlamp
  template:
    metadata:
      labels:
        k8s-app: headlamp
    spec:
      containers:
      - name: headlamp
        image: ghcr.io/headlamp-k8s/headlamp:latest
        args:
          - "-in-cluster"
          - "-plugins-dir=/headlamp/plugins"
        ports:
        - containerPort: 4466
        livenessProbe:
          httpGet:
            scheme: HTTP
            path: /
            port: 4466
          initialDelaySeconds: 30
          timeoutSeconds: 30
      nodeSelector:
        'kubernetes.io/os': linux
`,
			want: yamlkit.ResourceNameToCategoryTypesMap{
				api.ResourceName("confighubplaceholder/headlamp"): {
					{ResourceCategory: api.ResourceCategoryResource, ResourceType: testResourceTypeService},
					{ResourceCategory: api.ResourceCategoryResource, ResourceType: testResourceTypeDeployment},
				},
			},
		},
		{
			name: "Missing metadata",
			data: `apiVersion: v1
kind: Pod
`,
			want: yamlkit.ResourceNameToCategoryTypesMap{},
		},
	}

	for _, tt := range tests {
		t.Run(string(tt.name), func(t *testing.T) {
			docs, err := gaby.ParseAll([]byte(tt.data))
			if err != nil {
				t.Fatalf("failed to parse YAML: %v", err)
			}
			got, _, err := NewK8sResourceProvider().ResourceAndCategoryTypeMaps(docs)
			if tt.name == "Missing metadata" {
				assert.Error(t, err, "name not found")
			} else {
				assert.NoError(t, err)
			}
			if len(got) != len(tt.want) {
				t.Errorf("ResourceAndCategoryTypeMaps() rm = %v, want %v", got, tt.want)
			}
			for k, v := range tt.want {
				if !slices.Equal(got[k], v) {
					t.Errorf("ResourceAndCategoryTypeMaps()[%v] rm = %v, want %v", k, got[k], v)
				}
			}
		})
	}
}

func TestK8sFnResourceCategoryTypeMap(t *testing.T) {
	// Multi-doc YAML fixture
	yamlFixture := joinYAMLDocs(
		createPod(t, "pod1", "default"),
		createPod(t, "pod2", "default"),
		createService(t, "svc1", "default", "svc1"),
	)

	// Parse the fixture using gaby.ParseAll
	docs, err := gaby.ParseAll([]byte(yamlFixture))
	assert.NoError(t, err)

	// Call the function
	_, result, err := NewK8sResourceProvider().ResourceAndCategoryTypeMaps(docs)
	assert.NoError(t, err)

	// Expected result
	expected := yamlkit.ResourceCategoryTypeToNamesMap{
		{ResourceCategory: api.ResourceCategoryResource, ResourceType: testResourceTypePod}: {
			api.ResourceName("default/pod1"), api.ResourceName("default/pod2")},
		{ResourceCategory: api.ResourceCategoryResource, ResourceType: testResourceTypeService}: {
			api.ResourceName("default/svc1")},
	}

	// Assert the result
	assert.Equal(t, expected, result)
}

func TestEnsureConfigHubContextOnData_SingleObject(t *testing.T) {
	data := []byte(`apiVersion: v1
kind: ConfigMap
metadata:
  name: test-cm
  namespace: default
data:
  key: value
`)
	result, err := EnsureConfigHubContextOnData(data, "my-unit", "space-abc", 42)
	assert.NoError(t, err)

	container, err := gaby.ParseAll(result)
	assert.NoError(t, err)
	assert.Len(t, container, 1)

	val, _ := container[0].Path(K8sContextPath("UnitSlug")).Data().(string)
	assert.Equal(t, "my-unit", val)
	val, _ = container[0].Path(K8sContextPath("SpaceID")).Data().(string)
	assert.Equal(t, "space-abc", val)
	val, _ = container[0].Path(K8sContextPath("RevisionNum")).Data().(string)
	assert.Equal(t, "42", val)
}

func TestEnsureConfigHubContextOnData_MultipleObjects(t *testing.T) {
	data := []byte(`apiVersion: v1
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
	result, err := EnsureConfigHubContextOnData(data, "slug", "sid", 7)
	assert.NoError(t, err)

	container, err := gaby.ParseAll(result)
	assert.NoError(t, err)
	assert.Len(t, container, 2)

	for _, doc := range container {
		val, _ := doc.Path(K8sContextPath("UnitSlug")).Data().(string)
		assert.Equal(t, "slug", val)
		val, _ = doc.Path(K8sContextPath("SpaceID")).Data().(string)
		assert.Equal(t, "sid", val)
		val, _ = doc.Path(K8sContextPath("RevisionNum")).Data().(string)
		assert.Equal(t, "7", val)
	}
}

func TestEnsureConfigHubContextOnData_PreservesExistingAnnotations(t *testing.T) {
	data := []byte(`apiVersion: v1
kind: ConfigMap
metadata:
  name: test-cm
  namespace: default
  annotations:
    custom.io/owner: alice
data:
  key: value
`)
	result, err := EnsureConfigHubContextOnData(data, "unit", "space", 1)
	assert.NoError(t, err)

	resultStr := string(result)
	assert.Contains(t, resultStr, "custom.io/owner: alice")
	assert.Contains(t, resultStr, "confighub.com/UnitSlug: unit")
}

// TestEnsureConfigHubContextOnData_NullAnnotations covers issue #4504: a unit
// with `annotations:` present but null used to cause OCI bundle generation to
// 500 because gaby's SetP couldn't descend into a null scalar to inject
// confighub.com/UnitSlug. The fix coerces the null scalar into an empty map.
func TestEnsureConfigHubContextOnData_NullAnnotations(t *testing.T) {
	data := []byte(`apiVersion: v1
kind: ConfigMap
metadata:
  name: demo
  namespace: demo
  annotations:
data:
  k: v
`)
	result, err := EnsureConfigHubContextOnData(data, "my-slug", "space-xyz", 3)
	assert.NoError(t, err)

	container, err := gaby.ParseAll(result)
	assert.NoError(t, err)
	assert.Len(t, container, 1)

	val, _ := container[0].Path(K8sContextPath("UnitSlug")).Data().(string)
	assert.Equal(t, "my-slug", val)
	val, _ = container[0].Path(K8sContextPath("SpaceID")).Data().(string)
	assert.Equal(t, "space-xyz", val)
	val, _ = container[0].Path(K8sContextPath("RevisionNum")).Data().(string)
	assert.Equal(t, "3", val)
}

func TestEnsureConfigHubContextOnData_EmptyData(t *testing.T) {
	result, err := EnsureConfigHubContextOnData(nil, "unit", "space", 1)
	assert.NoError(t, err)
	assert.Nil(t, result)

	result, err = EnsureConfigHubContextOnData([]byte{}, "unit", "space", 1)
	assert.NoError(t, err)
	assert.Empty(t, result)
}

func TestEnsureConfigHubContextOnData_YAMLRoundTrip(t *testing.T) {
	data := []byte(`# ConfigMap for app config
apiVersion: v1
kind: ConfigMap
metadata:
  name: app-config
  namespace: default
data:
  # database connection
  db_host: localhost
  db_port: "5432"
---
# Service for the app
apiVersion: v1
kind: Service
metadata:
  name: app-svc
  namespace: default
spec:
  ports:
    - port: 80
      targetPort: 8080
  selector:
    app: myapp
`)
	result, err := EnsureConfigHubContextOnData(data, "my-unit", "space-123", 5)
	assert.NoError(t, err)

	resultStr := string(result)

	// Annotations injected
	assert.Contains(t, resultStr, "confighub.com/UnitSlug: my-unit")
	assert.Contains(t, resultStr, "confighub.com/SpaceID: space-123")

	// Both documents present
	assert.Contains(t, resultStr, "name: app-config")
	assert.Contains(t, resultStr, "name: app-svc")

	// Comments preserved
	assert.Contains(t, resultStr, "# ConfigMap for app config")
	assert.Contains(t, resultStr, "# database connection")
	assert.Contains(t, resultStr, "# Service for the app")

	// Key ordering preserved
	assert.Contains(t, resultStr, "db_host: localhost")
	assert.Contains(t, resultStr, "app: myapp")

	// Document separator preserved
	assert.Contains(t, resultStr, "---")
}

func TestEnsureConfigHubContextOnData_OverwritesExisting(t *testing.T) {
	data := []byte(`apiVersion: v1
kind: ConfigMap
metadata:
  name: test-cm
  namespace: default
  annotations:
    confighub.com/UnitSlug: old-slug
    confighub.com/SpaceID: old-space
data:
  key: value
`)
	result, err := EnsureConfigHubContextOnData(data, "new-slug", "new-space", 99)
	assert.NoError(t, err)

	container, err := gaby.ParseAll(result)
	assert.NoError(t, err)
	assert.Len(t, container, 1)

	val, _ := container[0].Path(K8sContextPath("UnitSlug")).Data().(string)
	assert.Equal(t, "new-slug", val)
	val, _ = container[0].Path(K8sContextPath("SpaceID")).Data().(string)
	assert.Equal(t, "new-space", val)
	val, _ = container[0].Path(K8sContextPath("RevisionNum")).Data().(string)
	assert.Equal(t, "99", val)
}
