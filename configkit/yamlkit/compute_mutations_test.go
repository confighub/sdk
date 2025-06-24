// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package yamlkit_test

import (
	"fmt"
	"testing"

	"github.com/confighub/sdk/configkit/k8skit"
	"github.com/confighub/sdk/configkit/yamlkit"
	"github.com/confighub/sdk/function/api"
	"github.com/confighub/sdk/third_party/gaby"
	"github.com/stretchr/testify/assert"
)

func TestK8sFnComputeMutations(t *testing.T) {
	tests := []struct {
		name           string
		previous       string
		modified       string
		functionIndex  int
		expected       api.ResourceMutationList
		expectedError  bool
		validateResult func(t *testing.T, mutations api.ResourceMutationList)
	}{
		{
			name: "No changes",
			previous: `apiVersion: apps/v1
kind: Deployment
metadata:
  name: myapp
  namespace: example
spec:
  replicas: 3
  selector:
    matchLabels:
      app: myapp
  template:
    spec:
      containers:
      - name: mycontainer
        image: nginx:latest
`,
			modified: `apiVersion: apps/v1
kind: Deployment
metadata:
  name: myapp
  namespace: example
spec:
  replicas: 3
  selector:
    matchLabels:
      app: myapp
  template:
    spec:
      containers:
      - name: mycontainer
        image: nginx:latest
`,
			functionIndex: 1,
			validateResult: func(t *testing.T, mutations api.ResourceMutationList) {
				assert.Len(t, mutations, 1)
				assert.Equal(t, api.MutationTypeNone, mutations[0].ResourceMutationInfo.MutationType)
				assert.Empty(t, mutations[0].PathMutationMap)
			},
		},
		{
			name: "Add new resource",
			previous: `apiVersion: apps/v1
kind: Deployment
metadata:
  name: myapp
  namespace: example
spec:
  replicas: 3
`,
			modified: `apiVersion: apps/v1
kind: Deployment
metadata:
  name: myapp
  namespace: example
spec:
  replicas: 3
---
apiVersion: v1
kind: Service
metadata:
  name: myapp-svc
  namespace: example
spec:
  selector:
    app: myapp
  ports:
  - port: 80
`,
			functionIndex: 2,
			validateResult: func(t *testing.T, mutations api.ResourceMutationList) {
				assert.Len(t, mutations, 2)
				assert.Equal(t, api.MutationTypeNone, mutations[0].ResourceMutationInfo.MutationType)
				assert.Equal(t, api.MutationTypeAdd, mutations[1].ResourceMutationInfo.MutationType)
				assert.Equal(t, api.ResourceType("v1/Service"), mutations[1].Resource.ResourceType)
				assert.Equal(t, api.ResourceName("example/myapp-svc"), mutations[1].Resource.ResourceName)
			},
		},
		{
			name: "Delete resource",
			previous: `apiVersion: apps/v1
kind: Deployment
metadata:
  name: myapp
  namespace: example
spec:
  replicas: 3
---
apiVersion: v1
kind: Service
metadata:
  name: myapp-svc
  namespace: example
spec:
  selector:
    app: myapp
  ports:
  - port: 80
`,
			modified: `apiVersion: apps/v1
kind: Deployment
metadata:
  name: myapp
  namespace: example
spec:
  replicas: 3
`,
			functionIndex: 3,
			validateResult: func(t *testing.T, mutations api.ResourceMutationList) {
				assert.Len(t, mutations, 2)
				assert.Equal(t, api.MutationTypeNone, mutations[0].ResourceMutationInfo.MutationType)
				assert.Equal(t, api.MutationTypeDelete, mutations[1].ResourceMutationInfo.MutationType)
				assert.Equal(t, api.ResourceType("v1/Service"), mutations[1].Resource.ResourceType)
				assert.Equal(t, api.ResourceName("example/myapp-svc"), mutations[1].Resource.ResourceName)
			},
		},
		{
			name: "Update map value",
			previous: `apiVersion: apps/v1
kind: Deployment
metadata:
  name: myapp
  namespace: example
spec:
  replicas: 2
  template:
    spec:
      containers:
      - name: nginx
        resources:
          limits:
            cpu: "1"
            memory: "1Gi"
`,
			modified: `apiVersion: apps/v1
kind: Deployment
metadata:
  name: myapp
  namespace: example
spec:
  replicas: 3
  template:
    spec:
      containers:
      - name: nginx
        resources:
          limits:
            cpu: "1"
            memory: "1Gi"
`,
			functionIndex: 3,
			validateResult: func(t *testing.T, mutations api.ResourceMutationList) {
				assert.Len(t, mutations, 1)
				assert.Equal(t, api.MutationTypeUpdate, mutations[0].ResourceMutationInfo.MutationType)
				assert.Len(t, mutations[0].PathMutationMap, 1)
				assert.Contains(t, mutations[0].PathMutationMap, api.ResolvedPath("spec.replicas"))
				assert.Equal(t, api.MutationTypeUpdate, mutations[0].PathMutationMap[api.ResolvedPath("spec.replicas")].MutationType)
			},
		},
		{
			name: "Update array element",
			previous: `apiVersion: apps/v1
kind: Deployment
metadata:
  name: myapp
  namespace: example
spec:
  template:
    spec:
      containers:
      - name: nginx
        image: nginx:1.19
      - name: sidecar
        image: sidecar:v1
`,
			modified: `apiVersion: apps/v1
kind: Deployment
metadata:
  name: myapp
  namespace: example
spec:
  template:
    spec:
      containers:
      - name: nginx
        image: nginx:1.20
      - name: sidecar
        image: sidecar:v1
`,
			functionIndex: 4,
			validateResult: func(t *testing.T, mutations api.ResourceMutationList) {
				assert.Len(t, mutations, 1)
				assert.Equal(t, api.MutationTypeUpdate, mutations[0].ResourceMutationInfo.MutationType)
				assert.Contains(t, mutations[0].PathMutationMap, api.ResolvedPath("spec.template.spec.containers.0.image"))
				assert.Equal(t, api.MutationTypeUpdate, mutations[0].PathMutationMap[api.ResolvedPath("spec.template.spec.containers.0.image")].MutationType)
			},
		},
		{
			name: "Add array element",
			previous: `apiVersion: apps/v1
kind: Deployment
metadata:
  name: myapp
  namespace: example
spec:
  template:
    spec:
      containers:
      - name: nginx
        image: nginx:1.19
`,
			modified: `apiVersion: apps/v1
kind: Deployment
metadata:
  name: myapp
  namespace: example
spec:
  template:
    spec:
      containers:
      - name: nginx
        image: nginx:1.19
      - name: sidecar
        image: sidecar:v1
`,
			functionIndex: 5,
			validateResult: func(t *testing.T, mutations api.ResourceMutationList) {
				assert.Len(t, mutations, 1)
				assert.Equal(t, api.MutationTypeUpdate, mutations[0].ResourceMutationInfo.MutationType)
				assert.Contains(t, mutations[0].PathMutationMap, api.ResolvedPath("spec.template.spec.containers.1"))
				assert.Equal(t, api.MutationTypeAdd, mutations[0].PathMutationMap[api.ResolvedPath("spec.template.spec.containers.1")].MutationType)
			},
		},
		{
			name: "Remove array element",
			previous: `apiVersion: apps/v1
kind: Deployment
metadata:
  name: myapp
  namespace: example
spec:
  template:
    spec:
      containers:
      - name: nginx
        image: nginx:1.19
      - name: sidecar
        image: sidecar:v1
`,
			modified: `apiVersion: apps/v1
kind: Deployment
metadata:
  name: myapp
  namespace: example
spec:
  template:
    spec:
      containers:
      - name: nginx
        image: nginx:1.19
`,
			functionIndex: 6,
			validateResult: func(t *testing.T, mutations api.ResourceMutationList) {
				assert.Len(t, mutations, 1)
				assert.Equal(t, api.MutationTypeUpdate, mutations[0].ResourceMutationInfo.MutationType)
				assert.Contains(t, mutations[0].PathMutationMap, api.ResolvedPath("spec.template.spec.containers.1"))
				assert.Equal(t, api.MutationTypeDelete, mutations[0].PathMutationMap[api.ResolvedPath("spec.template.spec.containers.1")].MutationType)
			},
		},
		{
			name: "Multiple changes in single resource",
			previous: `apiVersion: apps/v1
kind: Deployment
metadata:
  name: myapp
  namespace: example
spec:
  replicas: 2
  template:
    spec:
      containers:
      - name: nginx
        image: nginx:1.19
        resources:
          limits:
            cpu: "1"
`,
			modified: `apiVersion: apps/v1
kind: Deployment
metadata:
  name: myapp
  namespace: example
spec:
  replicas: 3
  template:
    spec:
      containers:
      - name: nginx
        image: nginx:1.20
        resources:
          limits:
            cpu: "2"
            memory: "1Gi"
`,
			functionIndex: 7,
			validateResult: func(t *testing.T, mutations api.ResourceMutationList) {
				assert.Len(t, mutations, 1)
				assert.Equal(t, api.MutationTypeUpdate, mutations[0].ResourceMutationInfo.MutationType)

				// Check specific path changes
				pathMap := mutations[0].PathMutationMap
				assert.Contains(t, pathMap, api.ResolvedPath("spec.replicas"))
				assert.Contains(t, pathMap, api.ResolvedPath("spec.template.spec.containers.0.image"))
				assert.Contains(t, pathMap, api.ResolvedPath("spec.template.spec.containers.0.resources.limits.cpu"))
				assert.Contains(t, pathMap, api.ResolvedPath("spec.template.spec.containers.0.resources.limits.memory"))

				assert.Equal(t, api.MutationTypeUpdate, pathMap[api.ResolvedPath("spec.replicas")].MutationType)
				assert.Equal(t, api.MutationTypeUpdate, pathMap[api.ResolvedPath("spec.template.spec.containers.0.image")].MutationType)
				assert.Equal(t, api.MutationTypeUpdate, pathMap[api.ResolvedPath("spec.template.spec.containers.0.resources.limits.cpu")].MutationType)
				assert.Equal(t, api.MutationTypeAdd, pathMap[api.ResolvedPath("spec.template.spec.containers.0.resources.limits.memory")].MutationType)
			},
		},
		{
			name: "Change resource type and structure",
			previous: `apiVersion: apps/v1
kind: Deployment
metadata:
  name: myapp
  namespace: example
spec:
  replicas: 2
`,
			modified: `apiVersion: apps/v1
kind: StatefulSet
metadata:
  name: myapp
  namespace: example
spec:
  replicas: 2
  serviceName: mydep-svc
`,
			functionIndex: 8,
			validateResult: func(t *testing.T, mutations api.ResourceMutationList) {
				assert.Len(t, mutations, 1)

				// The mutation should be a change from Deployment to StatefulSet
				assert.Equal(t, api.MutationTypeUpdate, mutations[0].ResourceMutationInfo.MutationType)
				assert.Equal(t, api.ResourceType("apps/v1/StatefulSet"), mutations[0].Resource.ResourceType)
			},
		},
		{
			name: "Change resource name",
			previous: `apiVersion: v1
kind: Namespace
metadata:
  name: replaceme
spec: {}
`,
			modified: `apiVersion: v1
kind: Namespace
metadata:
  name: myapp
  labels:
    environment: prod
spec: {}
`,
			functionIndex: 9,
			validateResult: func(t *testing.T, mutations api.ResourceMutationList) {
				assert.Len(t, mutations, 1)

				// The mutation should record the new resource name and both the old and new names as aliases.
				assert.Equal(t, api.MutationTypeUpdate, mutations[0].ResourceMutationInfo.MutationType)
				assert.Equal(t, api.ResourceName("/myapp"), mutations[0].Resource.ResourceName)
				assert.Equal(t, api.ResourceName("myapp"), mutations[0].Resource.ResourceNameWithoutScope)
				assert.Len(t, mutations[0].Aliases, 2)
				assert.Len(t, mutations[0].AliasesWithoutScopes, 2)
				assert.Contains(t, mutations[0].Aliases, api.ResourceName("/myapp"))
				assert.Contains(t, mutations[0].Aliases, api.ResourceName("/replaceme"))
				assert.Contains(t, mutations[0].AliasesWithoutScopes, api.ResourceName("myapp"))
				assert.Contains(t, mutations[0].AliasesWithoutScopes, api.ResourceName("replaceme"))
			},
		},
		// ComputeMutations can only be called with valid parsed YAML
		// 		{
		// 			name: "Invalid previous YAML",
		// 			previous: `apiVersion: apps/v1
		// kind: Deployment
		// metadata
		//   name: invalid-yaml
		// `,
		// 			modified: `apiVersion: apps/v1
		// kind: Deployment
		// metadata:
		//   name: example/mydep
		// `,
		// 			functionIndex: 9,
		// 			expectedError: true,
		// 		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Parse the previous YAML
			previousParsedData, err := gaby.ParseAll([]byte(tt.previous))
			if err != nil {
				t.Fatalf("failed to parse previous YAML: %v", err)
			}

			// Parse the modified YAML
			modifiedParsedData, err := gaby.ParseAll([]byte(tt.modified))
			if err != nil {
				t.Fatalf("failed to parse modified YAML: %v", err)
			}

			// Call the function
			mutations, err := yamlkit.ComputeMutations(previousParsedData, modifiedParsedData, int64(tt.functionIndex), k8skit.K8sResourceProvider)

			// Check for expected errors
			if tt.expectedError {
				assert.Error(t, err)
				return
			}

			assert.NoError(t, err)

			// Run the custom validation function if provided
			if tt.validateResult != nil {
				tt.validateResult(t, mutations)
			}
		})
	}
}

func TestK8sComputeMutationsForDocs(t *testing.T) {
	tests := []struct {
		name          string
		path          string
		previous      string
		modified      string
		functionIndex int
		expected      api.MutationMap
	}{
		{
			name: "Map update",
			path: "",
			previous: `
metadata:
  labels:
    app: oldlabel
spec:
  replicas: 1
`,
			modified: `
metadata:
  labels:
    app: newlabel
spec:
  replicas: 1
`,
			functionIndex: 1,
			expected: api.MutationMap{
				"metadata.labels.app": api.MutationInfo{
					MutationType: api.MutationTypeUpdate,
					Index:        1,
				},
			},
		},
		{
			name: "Array add element",
			path: "spec.containers",
			previous: `
- name: container1
  image: image1
`,
			modified: `
- name: container1
  image: image1
- name: container2
  image: image2
`,
			functionIndex: 2,
			expected: api.MutationMap{
				"spec.containers.1": api.MutationInfo{
					MutationType: api.MutationTypeAdd,
					Index:        2,
				},
			},
		},
		{
			name: "Array delete element",
			path: "spec.containers",
			previous: `
- name: container1
  image: image1
- name: container2
  image: image2
`,
			modified: `
- name: container1
  image: image1
`,
			functionIndex: 3,
			expected: api.MutationMap{
				"spec.containers.1": api.MutationInfo{
					MutationType: api.MutationTypeDelete,
					Index:        3,
				},
			},
		},
		{
			name:          "Scalar value change",
			path:          "spec.replicas",
			previous:      `2`,
			modified:      `3`,
			functionIndex: 4,
			expected: api.MutationMap{
				"spec.replicas": api.MutationInfo{
					MutationType: api.MutationTypeUpdate,
					Index:        4,
				},
			},
		},
		{
			name:     "Type change (scalar to map)",
			path:     "metadata.annotations",
			previous: `test`,
			modified: `
key1: value1
key2: value2
`,
			functionIndex: 5,
			expected: api.MutationMap{
				"metadata.annotations": api.MutationInfo{
					MutationType: api.MutationTypeUpdate,
					Index:        5,
				},
			},
		},
		{
			name: "Type change (map to array)",
			path: "spec.strategy",
			previous: `
type: RollingUpdate
rollingUpdate:
  maxSurge: 1
`,
			modified: `
- step1
- step2
`,
			functionIndex: 6,
			expected: api.MutationMap{
				"spec.strategy": api.MutationInfo{
					MutationType: api.MutationTypeUpdate,
					Index:        6,
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Parse the previous and modified docs
			previousDocs, err := gaby.ParseAll([]byte(tt.previous))
			if err != nil {
				t.Fatalf("failed to parse previous YAML: %v", err)
			}
			if len(previousDocs) == 0 {
				t.Fatalf("no docs parsed from previous YAML")
			}
			previousDoc := previousDocs[0]

			modifiedDocs, err := gaby.ParseAll([]byte(tt.modified))
			if err != nil {
				t.Fatalf("failed to parse modified YAML: %v", err)
			}
			if len(modifiedDocs) == 0 {
				t.Fatalf("no docs parsed from modified YAML")
			}
			modifiedDoc := modifiedDocs[0]

			// Create the mutation map and call the function
			pathMutationMap := api.MutationMap{}
			yamlkit.ComputeMutationsForDocs(tt.path, previousDoc, modifiedDoc, int64(tt.functionIndex), pathMutationMap)

			// Verify the mutation map
			for path, expectedInfo := range tt.expected {
				actualInfo, exists := pathMutationMap[api.ResolvedPath(path)]
				assert.True(t, exists, fmt.Sprintf("Expected path %s not found in mutation map", path))
				assert.Equal(t, expectedInfo.MutationType, actualInfo.MutationType,
					fmt.Sprintf("Incorrect mutation type for path %s", path))
				assert.Equal(t, expectedInfo.Index, actualInfo.Index,
					fmt.Sprintf("Incorrect function index for path %s", path))
			}

			// Ensure there aren't extra mutations we didn't expect
			assert.Equal(t, len(tt.expected), len(pathMutationMap),
				"Unexpected number of mutations in the map")
		})
	}
}
