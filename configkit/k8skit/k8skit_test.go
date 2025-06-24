// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package k8skit

import (
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/confighub/sdk/configkit/yamlkit"
	"github.com/confighub/sdk/function/api"
	"github.com/confighub/sdk/third_party/gaby"
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
  namespace: replaceme
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
  namespace: replaceme
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
				api.ResourceName("replaceme/headlamp"): {
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
			got, _, err := K8sResourceProvider.ResourceAndCategoryTypeMaps(docs)
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
	_, result, err := K8sResourceProvider.ResourceAndCategoryTypeMaps(docs)
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
