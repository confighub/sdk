// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package k8skit_test

import (
	"fmt"
	"testing"

	"github.com/confighub/sdk/configkit/k8skit"
	"github.com/confighub/sdk/core/configkit/yamlkit"
	"github.com/confighub/sdk/core/function/api"
	"github.com/confighub/sdk/core/third_party/gaby"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
				assert.Contains(t, mutations[0].PathMutationMap, api.ResolvedPath("spec.template.spec.containers.?name=nginx;@0.image"))
				assert.Equal(t, api.MutationTypeUpdate, mutations[0].PathMutationMap[api.ResolvedPath("spec.template.spec.containers.?name=nginx;@0.image")].MutationType)
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
				assert.Contains(t, mutations[0].PathMutationMap, api.ResolvedPath("spec.template.spec.containers.?name=sidecar;@1"))
				assert.Equal(t, api.MutationTypeAdd, mutations[0].PathMutationMap[api.ResolvedPath("spec.template.spec.containers.?name=sidecar;@1")].MutationType)
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
				assert.Contains(t, mutations[0].PathMutationMap, api.ResolvedPath("spec.template.spec.containers.?name=sidecar;@1"))
				assert.Equal(t, api.MutationTypeDelete, mutations[0].PathMutationMap[api.ResolvedPath("spec.template.spec.containers.?name=sidecar;@1")].MutationType)
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
				assert.Contains(t, pathMap, api.ResolvedPath("spec.template.spec.containers.?name=nginx;@0.image"))
				assert.Contains(t, pathMap, api.ResolvedPath("spec.template.spec.containers.?name=nginx;@0.resources.limits.cpu"))
				assert.Contains(t, pathMap, api.ResolvedPath("spec.template.spec.containers.?name=nginx;@0.resources.limits.memory"))

				assert.Equal(t, api.MutationTypeUpdate, pathMap[api.ResolvedPath("spec.replicas")].MutationType)
				assert.Equal(t, api.MutationTypeUpdate, pathMap[api.ResolvedPath("spec.template.spec.containers.?name=nginx;@0.image")].MutationType)
				assert.Equal(t, api.MutationTypeUpdate, pathMap[api.ResolvedPath("spec.template.spec.containers.?name=nginx;@0.resources.limits.cpu")].MutationType)
				assert.Equal(t, api.MutationTypeAdd, pathMap[api.ResolvedPath("spec.template.spec.containers.?name=nginx;@0.resources.limits.memory")].MutationType)
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
  name: confighubplaceholder
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
				assert.Contains(t, mutations[0].Aliases, api.ResourceName("/confighubplaceholder"))
				assert.Contains(t, mutations[0].AliasesWithoutScopes, api.ResourceName("myapp"))
				assert.Contains(t, mutations[0].AliasesWithoutScopes, api.ResourceName("confighubplaceholder"))
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
			mutations, err := yamlkit.ComputeMutations(previousParsedData, modifiedParsedData, int64(tt.functionIndex), k8skit.NewK8sResourceProvider())

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
			yamlkit.ComputeMutationsForDocs(tt.path, previousDoc, modifiedDoc, int64(tt.functionIndex), pathMutationMap, nil, nil, nil)

			// Verify the mutation map. Elements of an array with no declared merge key
			// are named by an anchor carrying the index; reduce those back to the index
			// so the cases can state the path they are about.
			stripped := api.MutationMap{}
			for path, info := range pathMutationMap {
				stripped[api.ResolvedPath(yamlkit.StripAssociativeSegments(string(path)))] = info
			}
			pathMutationMap = stripped
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

// TestStrategicMergeEnvVars tests associative matching with Kubernetes env vars,
// verifying that array elements are matched by merge key (name) rather than position.
func TestStrategicMergeEnvVars(t *testing.T) {
	k8sProvider := k8skit.NewK8sResourceProvider()

	tests := []struct {
		name           string
		previous       string
		modified       string
		validateResult func(t *testing.T, mutations api.ResourceMutationList)
	}{
		{
			name: "Modify env var value",
			previous: `apiVersion: apps/v1
kind: Deployment
metadata:
  name: myapp
  namespace: default
spec:
  template:
    spec:
      containers:
      - name: nginx
        image: nginx:1.19
        env:
        - name: DATABASE_URL
          value: postgres://old-host:5432/db
        - name: LOG_LEVEL
          value: info
`,
			modified: `apiVersion: apps/v1
kind: Deployment
metadata:
  name: myapp
  namespace: default
spec:
  template:
    spec:
      containers:
      - name: nginx
        image: nginx:1.19
        env:
        - name: DATABASE_URL
          value: postgres://new-host:5432/db
        - name: LOG_LEVEL
          value: info
`,
			validateResult: func(t *testing.T, mutations api.ResourceMutationList) {
				assert.Len(t, mutations, 1)
				pm := mutations[0].PathMutationMap
				dbPath := api.ResolvedPath("spec.template.spec.containers.?name=nginx;@0.env.?name=DATABASE_URL;@0.value")
				assert.Contains(t, pm, dbPath)
				assert.Equal(t, api.MutationTypeUpdate, pm[dbPath].MutationType)
				// LOG_LEVEL unchanged, should not appear
				for p := range pm {
					assert.NotContains(t, string(p), "LOG_LEVEL")
				}
			},
		},
		{
			name: "Add env var",
			previous: `apiVersion: apps/v1
kind: Deployment
metadata:
  name: myapp
  namespace: default
spec:
  template:
    spec:
      containers:
      - name: nginx
        image: nginx:1.19
        env:
        - name: DATABASE_URL
          value: postgres://host:5432/db
`,
			modified: `apiVersion: apps/v1
kind: Deployment
metadata:
  name: myapp
  namespace: default
spec:
  template:
    spec:
      containers:
      - name: nginx
        image: nginx:1.19
        env:
        - name: DATABASE_URL
          value: postgres://host:5432/db
        - name: NEW_VAR
          value: new-value
`,
			validateResult: func(t *testing.T, mutations api.ResourceMutationList) {
				assert.Len(t, mutations, 1)
				pm := mutations[0].PathMutationMap
				newVarPath := api.ResolvedPath("spec.template.spec.containers.?name=nginx;@0.env.?name=NEW_VAR;@1")
				assert.Contains(t, pm, newVarPath)
				assert.Equal(t, api.MutationTypeAdd, pm[newVarPath].MutationType)
			},
		},
		{
			name: "Remove env var",
			previous: `apiVersion: apps/v1
kind: Deployment
metadata:
  name: myapp
  namespace: default
spec:
  template:
    spec:
      containers:
      - name: nginx
        image: nginx:1.19
        env:
        - name: DATABASE_URL
          value: postgres://host:5432/db
        - name: DEPRECATED_VAR
          value: old-value
`,
			modified: `apiVersion: apps/v1
kind: Deployment
metadata:
  name: myapp
  namespace: default
spec:
  template:
    spec:
      containers:
      - name: nginx
        image: nginx:1.19
        env:
        - name: DATABASE_URL
          value: postgres://host:5432/db
`,
			validateResult: func(t *testing.T, mutations api.ResourceMutationList) {
				assert.Len(t, mutations, 1)
				pm := mutations[0].PathMutationMap
				removedPath := api.ResolvedPath("spec.template.spec.containers.?name=nginx;@0.env.?name=DEPRECATED_VAR;@1")
				assert.Contains(t, pm, removedPath)
				assert.Equal(t, api.MutationTypeDelete, pm[removedPath].MutationType)
			},
		},
		{
			name: "Reorder env vars - no mutations",
			previous: `apiVersion: apps/v1
kind: Deployment
metadata:
  name: myapp
  namespace: default
spec:
  template:
    spec:
      containers:
      - name: nginx
        image: nginx:1.19
        env:
        - name: VAR_A
          value: a
        - name: VAR_B
          value: b
        - name: VAR_C
          value: c
`,
			modified: `apiVersion: apps/v1
kind: Deployment
metadata:
  name: myapp
  namespace: default
spec:
  template:
    spec:
      containers:
      - name: nginx
        image: nginx:1.19
        env:
        - name: VAR_C
          value: c
        - name: VAR_A
          value: a
        - name: VAR_B
          value: b
`,
			validateResult: func(t *testing.T, mutations api.ResourceMutationList) {
				assert.Len(t, mutations, 1)
				// Path-level: no mutations, since elements match by merge key.
				// Resource-level: Update, because the array's element order
				// changed and was recorded in ArrayOrders so PatchMutations
				// can reorder the target accordingly.
				assert.Len(t, mutations[0].PathMutationMap, 0)
				envPath := api.ResolvedPath("spec.template.spec.containers.?name=nginx;@0.env")
				require.Contains(t, mutations[0].ArrayOrders, envPath)
				assert.Equal(t, []string{"VAR_C", "VAR_A", "VAR_B"}, mutations[0].ArrayOrders[envPath])
				assert.Equal(t, api.MutationTypeUpdate, mutations[0].ResourceMutationInfo.MutationType)
			},
		},
		{
			name: "Reorder and modify env vars",
			previous: `apiVersion: apps/v1
kind: Deployment
metadata:
  name: myapp
  namespace: default
spec:
  template:
    spec:
      containers:
      - name: nginx
        image: nginx:1.19
        env:
        - name: VAR_A
          value: a
        - name: VAR_B
          value: b
`,
			modified: `apiVersion: apps/v1
kind: Deployment
metadata:
  name: myapp
  namespace: default
spec:
  template:
    spec:
      containers:
      - name: nginx
        image: nginx:1.19
        env:
        - name: VAR_B
          value: b-modified
        - name: VAR_A
          value: a
`,
			validateResult: func(t *testing.T, mutations api.ResourceMutationList) {
				assert.Len(t, mutations, 1)
				pm := mutations[0].PathMutationMap
				// Only VAR_B's value changed - the reordering is invisible
				assert.Len(t, pm, 1)
				// VAR_B was at index 1 in previous, now at index 0 in modified
				// The path should use the modified index since the merge key matches
				varBPath := api.ResolvedPath("spec.template.spec.containers.?name=nginx;@0.env.?name=VAR_B;@0.value")
				assert.Contains(t, pm, varBPath)
				assert.Equal(t, api.MutationTypeUpdate, pm[varBPath].MutationType)
			},
		},
		{
			name: "Multiple containers with env changes",
			previous: `apiVersion: apps/v1
kind: Deployment
metadata:
  name: myapp
  namespace: default
spec:
  template:
    spec:
      containers:
      - name: nginx
        image: nginx:1.19
        env:
        - name: PORT
          value: "80"
      - name: sidecar
        image: sidecar:v1
        env:
        - name: LOG_LEVEL
          value: info
`,
			modified: `apiVersion: apps/v1
kind: Deployment
metadata:
  name: myapp
  namespace: default
spec:
  template:
    spec:
      containers:
      - name: nginx
        image: nginx:1.19
        env:
        - name: PORT
          value: "8080"
      - name: sidecar
        image: sidecar:v1
        env:
        - name: LOG_LEVEL
          value: debug
`,
			validateResult: func(t *testing.T, mutations api.ResourceMutationList) {
				assert.Len(t, mutations, 1)
				pm := mutations[0].PathMutationMap
				// Both containers have changes, paths should use merge keys
				nginxPortPath := api.ResolvedPath("spec.template.spec.containers.?name=nginx;@0.env.?name=PORT;@0.value")
				sidecarLogPath := api.ResolvedPath("spec.template.spec.containers.?name=sidecar;@1.env.?name=LOG_LEVEL;@0.value")
				assert.Contains(t, pm, nginxPortPath)
				assert.Contains(t, pm, sidecarLogPath)
				assert.Equal(t, api.MutationTypeUpdate, pm[nginxPortPath].MutationType)
				assert.Equal(t, api.MutationTypeUpdate, pm[sidecarLogPath].MutationType)
			},
		},
		{
			name: "Add container with env vars",
			previous: `apiVersion: apps/v1
kind: Deployment
metadata:
  name: myapp
  namespace: default
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
  namespace: default
spec:
  template:
    spec:
      containers:
      - name: nginx
        image: nginx:1.19
      - name: sidecar
        image: sidecar:v1
        env:
        - name: LOG_LEVEL
          value: debug
`,
			validateResult: func(t *testing.T, mutations api.ResourceMutationList) {
				assert.Len(t, mutations, 1)
				pm := mutations[0].PathMutationMap
				// New container is an add at the container level
				sidecarPath := api.ResolvedPath("spec.template.spec.containers.?name=sidecar;@1")
				assert.Contains(t, pm, sidecarPath)
				assert.Equal(t, api.MutationTypeAdd, pm[sidecarPath].MutationType)
			},
		},
		{
			name: "Env var changes survive PatchMutations round-trip",
			previous: `apiVersion: apps/v1
kind: Deployment
metadata:
  name: myapp
  namespace: default
spec:
  template:
    spec:
      containers:
      - name: nginx
        image: nginx:1.19
        env:
        - name: DATABASE_URL
          value: postgres://old:5432/db
        - name: LOG_LEVEL
          value: info
`,
			modified: `apiVersion: apps/v1
kind: Deployment
metadata:
  name: myapp
  namespace: default
spec:
  template:
    spec:
      containers:
      - name: nginx
        image: nginx:1.19
        env:
        - name: DATABASE_URL
          value: postgres://new:5432/db
        - name: LOG_LEVEL
          value: debug
`,
			validateResult: func(t *testing.T, mutations api.ResourceMutationList) {
				assert.Len(t, mutations, 1)
				// Apply the mutations to the previous data via PatchMutations
				previousYAML := `apiVersion: apps/v1
kind: Deployment
metadata:
  name: myapp
  namespace: default
spec:
  template:
    spec:
      containers:
      - name: nginx
        image: nginx:1.19
        env:
        - name: DATABASE_URL
          value: postgres://old:5432/db
        - name: LOG_LEVEL
          value: info
`
				targetParsed, err := gaby.ParseAll([]byte(previousYAML))
				assert.NoError(t, err)

				patched, _, err := yamlkit.PatchMutations(targetParsed, nil, mutations, nil, k8sProvider, nil)
				assert.NoError(t, err)
				assert.Len(t, patched, 1)

				// Verify patched values
				dbURL, found, err := yamlkit.YamlSafePathGetValue[string](patched[0], "spec.template.spec.containers.0.env.0.value", false)
				assert.NoError(t, err)
				assert.True(t, found)
				assert.Equal(t, "postgres://new:5432/db", dbURL)

				logLevel, found, err := yamlkit.YamlSafePathGetValue[string](patched[0], "spec.template.spec.containers.0.env.1.value", false)
				assert.NoError(t, err)
				assert.True(t, found)
				assert.Equal(t, "debug", logLevel)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			previousParsed, err := gaby.ParseAll([]byte(tt.previous))
			assert.NoError(t, err)

			modifiedParsed, err := gaby.ParseAll([]byte(tt.modified))
			assert.NoError(t, err)

			mutations, err := yamlkit.ComputeMutations(previousParsed, modifiedParsed, 1, k8sProvider)
			assert.NoError(t, err)

			tt.validateResult(t, mutations)
		})
	}
}
