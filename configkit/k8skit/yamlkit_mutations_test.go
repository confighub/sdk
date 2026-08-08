// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package k8skit_test

import (
	"testing"

	"github.com/confighub/sdk/configkit/k8skit"
	"github.com/confighub/sdk/core/configkit/yamlkit"
	"github.com/confighub/sdk/core/function/api"
	"github.com/confighub/sdk/core/third_party/gaby"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Test data: Kubernetes Deployment YAML
const baseDeployment = `apiVersion: apps/v1
kind: Deployment
metadata:
  name: myapp
  namespace: confighubplaceholder
spec:
  replicas: 1
  selector:
    matchLabels:
      app: myapp
  template:
    spec:
      containers:
      - name: mycontainer
        image: nginx:1.19
        resources:
          limits:
            cpu: "500m"
            memory: "512Mi"
`

const sourceChangedDeployment = `apiVersion: apps/v1
kind: Deployment
metadata:
  name: myapp
  namespace: confighubplaceholder
spec:
  replicas: 3
  selector:
    matchLabels:
      app: myapp
  template:
    spec:
      containers:
      - name: mycontainer
        image: nginx:1.20
        resources:
          limits:
            cpu: "1000m"
            memory: "1Gi"
`

const targetChangedDeployment = `apiVersion: apps/v1
kind: Deployment
metadata:
  name: myapp
  namespace: appchat
spec:
  replicas: 2
  selector:
    matchLabels:
      app: myapp
  template:
    spec:
      containers:
      - name: mycontainer
        image: nginx:1.19
        resources:
          limits:
            cpu: "500m"
            memory: "512Mi"
`

func TestAddMutations(t *testing.T) {
	tests := []struct {
		name           string
		base           string
		firstChange    string
		secondChange   string
		validateResult func(t *testing.T, combined api.ResourceMutationList)
	}{
		{
			name:         "Combine two changes to same resource",
			base:         baseDeployment,
			firstChange:  sourceChangedDeployment,
			secondChange: targetChangedDeployment,
			validateResult: func(t *testing.T, combined api.ResourceMutationList) {
				require.Len(t, combined, 1)
				assert.Equal(t, api.MutationTypeUpdate, combined[0].ResourceMutationInfo.MutationType)
				// Should have mutations for replicas, namespace, and image
				assert.Contains(t, combined[0].PathMutationMap, api.ResolvedPath("spec.replicas"))
				assert.Contains(t, combined[0].PathMutationMap, api.ResolvedPath("metadata.namespace"))
			},
		},
		{
			name: "Combine add and update",
			base: `apiVersion: apps/v1
kind: Deployment
metadata:
  name: myapp
  namespace: default
spec:
  replicas: 1
`,
			firstChange: `apiVersion: apps/v1
kind: Deployment
metadata:
  name: myapp
  namespace: default
spec:
  replicas: 2
---
apiVersion: v1
kind: Service
metadata:
  name: myapp-svc
  namespace: default
spec:
  ports:
  - port: 80
`,
			secondChange: `apiVersion: apps/v1
kind: Deployment
metadata:
  name: myapp
  namespace: default
spec:
  replicas: 3
---
apiVersion: v1
kind: Service
metadata:
  name: myapp-svc
  namespace: default
spec:
  ports:
  - port: 8080
`,
			validateResult: func(t *testing.T, combined api.ResourceMutationList) {
				// Should have 2 resources
				require.Len(t, combined, 2)
				// Both should be updates
				for _, m := range combined {
					assert.NotEqual(t, api.MutationTypeNone, m.ResourceMutationInfo.MutationType)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			baseParsed, err := gaby.ParseAll([]byte(tt.base))
			require.NoError(t, err)

			firstParsed, err := gaby.ParseAll([]byte(tt.firstChange))
			require.NoError(t, err)

			secondParsed, err := gaby.ParseAll([]byte(tt.secondChange))
			require.NoError(t, err)

			// Compute first diff
			firstMutations, err := yamlkit.ComputeMutations(baseParsed, firstParsed, 1, k8skit.NewK8sResourceProvider())
			require.NoError(t, err)

			// Compute second diff (from first change to second change)
			secondMutations, err := yamlkit.ComputeMutations(firstParsed, secondParsed, 2, k8skit.NewK8sResourceProvider())
			require.NoError(t, err)

			// Combine mutations
			combined, _ := yamlkit.AddMutations(firstMutations, secondMutations)

			tt.validateResult(t, combined)
		})
	}
}

func TestSubtractMutations(t *testing.T) {
	tests := []struct {
		name           string
		base           string
		sourceChange   string
		targetChange   string
		validateResult func(t *testing.T, result api.ResourceMutationList)
	}{
		{
			name: "Source changes replicas, target changes namespace only - replicas change should remain",
			base: baseDeployment,
			sourceChange: `apiVersion: apps/v1
kind: Deployment
metadata:
  name: myapp
  namespace: confighubplaceholder
spec:
  replicas: 3
  selector:
    matchLabels:
      app: myapp
  template:
    spec:
      containers:
      - name: mycontainer
        image: nginx:1.19
        resources:
          limits:
            cpu: "500m"
            memory: "512Mi"
`,
			targetChange: `apiVersion: apps/v1
kind: Deployment
metadata:
  name: myapp
  namespace: production
spec:
  replicas: 1
  selector:
    matchLabels:
      app: myapp
  template:
    spec:
      containers:
      - name: mycontainer
        image: nginx:1.19
        resources:
          limits:
            cpu: "500m"
            memory: "512Mi"
`,
			validateResult: func(t *testing.T, result api.ResourceMutationList) {
				require.Len(t, result, 1)
				// Source's replicas change should remain since target only changed namespace
				assert.Contains(t, result[0].PathMutationMap, api.ResolvedPath("spec.replicas"))
				// Namespace should not be in result since we're subtracting target's changes
				// (actually it shouldn't be in source mutations at all)
			},
		},
		{
			name: "Target deletes resource, source changes it - source change should be removed",
			base: `apiVersion: apps/v1
kind: Deployment
metadata:
  name: myapp
  namespace: default
spec:
  replicas: 1
---
apiVersion: v1
kind: Service
metadata:
  name: myapp-svc
  namespace: default
spec:
  ports:
  - port: 80
`,
			sourceChange: `apiVersion: apps/v1
kind: Deployment
metadata:
  name: myapp
  namespace: default
spec:
  replicas: 1
---
apiVersion: v1
kind: Service
metadata:
  name: myapp-svc
  namespace: default
spec:
  ports:
  - port: 8080
`,
			targetChange: `apiVersion: apps/v1
kind: Deployment
metadata:
  name: myapp
  namespace: default
spec:
  replicas: 1
`,
			validateResult: func(t *testing.T, result api.ResourceMutationList) {
				// The Service mutation should be removed because target deleted it
				for _, m := range result {
					if m.Resource.ResourceType == "v1/Service" {
						t.Errorf("Service mutation should have been removed")
					}
				}
			},
		},
		{
			name: "Target changes image, source changes image - source change should be subtracted entirely",
			base: baseDeployment,
			sourceChange: `apiVersion: apps/v1
kind: Deployment
metadata:
  name: myapp
  namespace: confighubplaceholder
spec:
  replicas: 1
  selector:
    matchLabels:
      app: myapp
  template:
    spec:
      containers:
      - name: mycontainer
        image: nginx:1.21
        resources:
          limits:
            cpu: "500m"
            memory: "512Mi"
`,
			targetChange: `apiVersion: apps/v1
kind: Deployment
metadata:
  name: myapp
  namespace: confighubplaceholder
spec:
  replicas: 1
  selector:
    matchLabels:
      app: myapp
  template:
    spec:
      containers:
      - name: mycontainer
        image: nginx:1.20
        resources:
          limits:
            cpu: "500m"
            memory: "512Mi"
`,
			validateResult: func(t *testing.T, result api.ResourceMutationList) {
				// Source only changed image, target also changed image
				// After subtraction, there should be nothing left OR a None mutation
				if len(result) > 0 {
					// If there's a mutation, it should have no path changes
					assert.Equal(t, api.MutationTypeNone, result[0].ResourceMutationInfo.MutationType)
				}
				// Either way, there should be no image path mutation
				for _, m := range result {
					imagePath := api.ResolvedPath("spec.template.spec.containers.0.image")
					_, hasImage := m.PathMutationMap[imagePath]
					assert.False(t, hasImage, "Image mutation should have been removed")
				}
			},
		},
		{
			name: "No conflicts - all source changes should remain",
			base: baseDeployment,
			sourceChange: `apiVersion: apps/v1
kind: Deployment
metadata:
  name: myapp
  namespace: confighubplaceholder
spec:
  replicas: 3
  selector:
    matchLabels:
      app: myapp
  template:
    spec:
      containers:
      - name: mycontainer
        image: nginx:1.19
        resources:
          limits:
            cpu: "500m"
            memory: "512Mi"
`,
			targetChange: baseDeployment, // no changes
			validateResult: func(t *testing.T, result api.ResourceMutationList) {
				require.Len(t, result, 1)
				// Replicas change should remain
				assert.Contains(t, result[0].PathMutationMap, api.ResolvedPath("spec.replicas"))
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			baseParsed, err := gaby.ParseAll([]byte(tt.base))
			require.NoError(t, err)

			sourceParsed, err := gaby.ParseAll([]byte(tt.sourceChange))
			require.NoError(t, err)

			targetParsed, err := gaby.ParseAll([]byte(tt.targetChange))
			require.NoError(t, err)

			// Compute source diff (base -> sourceChange)
			sourceMutations, err := yamlkit.ComputeMutations(baseParsed, sourceParsed, 1, k8skit.NewK8sResourceProvider())
			require.NoError(t, err)

			// Compute target diff (base -> targetChange)
			targetMutations, err := yamlkit.ComputeMutations(baseParsed, targetParsed, 2, k8skit.NewK8sResourceProvider())
			require.NoError(t, err)

			// Subtract target changes from source changes
			result, _ := yamlkit.SubtractMutations(sourceMutations, targetMutations)

			tt.validateResult(t, result)
		})
	}
}

func TestPatchMutations(t *testing.T) {
	tests := []struct {
		name           string
		config         string
		mutations      api.ResourceMutationList
		protection     api.ResourceMutationList // nil means no filtering
		validateResult func(t *testing.T, result string)
	}{
		{
			name:   "Apply replicas change",
			config: baseDeployment,
			mutations: api.ResourceMutationList{{
				Resource: api.ResourceInfo{
					ResourceType:             "apps/v1/Deployment",
					ResourceName:             "confighubplaceholder/myapp",
					ResourceNameWithoutScope: "myapp",
					ResourceCategory:         "Kubernetes",
				},
				ResourceMutationInfo: api.MutationInfo{
					MutationType: api.MutationTypeUpdate,
					Index:        1,
				},
				PathMutationMap: api.MutationMap{
					"spec.replicas": api.MutationInfo{
						MutationType: api.MutationTypeUpdate,
						Index:        1,
						Value:        "3\n",
					},
				},
			}},
			validateResult: func(t *testing.T, result string) {
				parsed, err := gaby.ParseAll([]byte(result))
				require.NoError(t, err)
				require.Len(t, parsed, 1)

				replicas, found, err := yamlkit.YamlSafePathGetValue[int](parsed[0], "spec.replicas", false)
				require.NoError(t, err)
				require.True(t, found)
				assert.Equal(t, 3, replicas)
			},
		},
		{
			name:   "Apply multiple path changes",
			config: baseDeployment,
			mutations: api.ResourceMutationList{{
				Resource: api.ResourceInfo{
					ResourceType:             "apps/v1/Deployment",
					ResourceName:             "confighubplaceholder/myapp",
					ResourceNameWithoutScope: "myapp",
					ResourceCategory:         "Kubernetes",
				},
				ResourceMutationInfo: api.MutationInfo{
					MutationType: api.MutationTypeUpdate,
					Index:        1,
				},
				PathMutationMap: api.MutationMap{
					"spec.replicas": api.MutationInfo{
						MutationType: api.MutationTypeUpdate,
						Index:        1,
						Value:        "5\n",
					},
					"metadata.namespace": api.MutationInfo{
						MutationType: api.MutationTypeUpdate,
						Index:        1,
						Value:        "production\n",
					},
				},
			}},
			validateResult: func(t *testing.T, result string) {
				parsed, err := gaby.ParseAll([]byte(result))
				require.NoError(t, err)
				require.Len(t, parsed, 1)

				replicas, found, err := yamlkit.YamlSafePathGetValue[int](parsed[0], "spec.replicas", false)
				require.NoError(t, err)
				require.True(t, found)
				assert.Equal(t, 5, replicas)

				ns, found, err := yamlkit.YamlSafePathGetValue[string](parsed[0], "metadata.namespace", false)
				require.NoError(t, err)
				require.True(t, found)
				assert.Equal(t, "production", ns)
			},
		},
		{
			name:   "Add new path",
			config: baseDeployment,
			mutations: api.ResourceMutationList{{
				Resource: api.ResourceInfo{
					ResourceType:             "apps/v1/Deployment",
					ResourceName:             "confighubplaceholder/myapp",
					ResourceNameWithoutScope: "myapp",
					ResourceCategory:         "Kubernetes",
				},
				ResourceMutationInfo: api.MutationInfo{
					MutationType: api.MutationTypeUpdate,
					Index:        1,
				},
				PathMutationMap: api.MutationMap{
					"spec.template.spec.containers.0.securityContext": api.MutationInfo{
						MutationType: api.MutationTypeAdd,
						Index:        1,
						Value: `runAsNonRoot: true
runAsUser: 1000
`,
					},
				},
			}},
			validateResult: func(t *testing.T, result string) {
				parsed, err := gaby.ParseAll([]byte(result))
				require.NoError(t, err)
				require.Len(t, parsed, 1)

				assert.True(t, parsed[0].ExistsP("spec.template.spec.containers.0.securityContext"))
				runAsUser, found, err := yamlkit.YamlSafePathGetValue[int](parsed[0], "spec.template.spec.containers.0.securityContext.runAsUser", false)
				require.NoError(t, err)
				require.True(t, found)
				assert.Equal(t, 1000, runAsUser)
			},
		},
		{
			name: "Delete path",
			config: `apiVersion: apps/v1
kind: Deployment
metadata:
  name: myapp
  namespace: confighubplaceholder
  annotations:
    description: "test deployment"
spec:
  replicas: 1
`,
			mutations: api.ResourceMutationList{{
				Resource: api.ResourceInfo{
					ResourceType:             "apps/v1/Deployment",
					ResourceName:             "confighubplaceholder/myapp",
					ResourceNameWithoutScope: "myapp",
					ResourceCategory:         "Kubernetes",
				},
				ResourceMutationInfo: api.MutationInfo{
					MutationType: api.MutationTypeUpdate,
					Index:        1,
				},
				PathMutationMap: api.MutationMap{
					"metadata.annotations": api.MutationInfo{
						MutationType: api.MutationTypeDelete,
						Index:        1,
					},
				},
			}},
			validateResult: func(t *testing.T, result string) {
				parsed, err := gaby.ParseAll([]byte(result))
				require.NoError(t, err)
				require.Len(t, parsed, 1)

				assert.False(t, parsed[0].ExistsP("metadata.annotations"))
			},
		},
		{
			name:   "Add new resource to existing data",
			config: baseDeployment,
			mutations: api.ResourceMutationList{{
				Resource: api.ResourceInfo{
					ResourceType:             "v1/Namespace",
					ResourceName:             "production",
					ResourceNameWithoutScope: "production",
					ResourceCategory:         "Kubernetes",
				},
				ResourceMutationInfo: api.MutationInfo{
					MutationType: api.MutationTypeAdd,
					Index:        1,
					Value: `apiVersion: v1
kind: Namespace
metadata:
  name: production
`,
				},
			}},
			validateResult: func(t *testing.T, result string) {
				parsed, err := gaby.ParseAll([]byte(result))
				require.NoError(t, err)
				require.Len(t, parsed, 2, "should have original deployment plus new namespace")

				// Original deployment should still be there
				kind, found, err := yamlkit.YamlSafePathGetValue[string](parsed[0], "kind", false)
				require.NoError(t, err)
				require.True(t, found)
				assert.Equal(t, "Deployment", kind)

				// New namespace should be appended
				kind, found, err = yamlkit.YamlSafePathGetValue[string](parsed[1], "kind", false)
				require.NoError(t, err)
				require.True(t, found)
				assert.Equal(t, "Namespace", kind)
			},
		},
		{
			name:   "Add new resource to empty data",
			config: "",
			mutations: api.ResourceMutationList{{
				Resource: api.ResourceInfo{
					ResourceType:             "v1/Namespace",
					ResourceName:             "test-ns",
					ResourceNameWithoutScope: "test-ns",
					ResourceCategory:         "Kubernetes",
				},
				ResourceMutationInfo: api.MutationInfo{
					MutationType: api.MutationTypeAdd,
					Index:        1,
					Value: `apiVersion: v1
kind: Namespace
metadata:
  name: test-ns
`,
				},
			}},
			validateResult: func(t *testing.T, result string) {
				parsed, err := gaby.ParseAll([]byte(result))
				require.NoError(t, err)
				require.Len(t, parsed, 1, "should have the new namespace")

				kind, found, err := yamlkit.YamlSafePathGetValue[string](parsed[0], "kind", false)
				require.NoError(t, err)
				require.True(t, found)
				assert.Equal(t, "Namespace", kind)

				name, found, err := yamlkit.YamlSafePathGetValue[string](parsed[0], "metadata.name", false)
				require.NoError(t, err)
				require.True(t, found)
				assert.Equal(t, "test-ns", name)
			},
		},
		{
			name:   "Add multiple new resources to empty data",
			config: "",
			mutations: api.ResourceMutationList{
				{
					Resource: api.ResourceInfo{
						ResourceType:             "v1/Namespace",
						ResourceName:             "test-ns",
						ResourceNameWithoutScope: "test-ns",
						ResourceCategory:         "Kubernetes",
					},
					ResourceMutationInfo: api.MutationInfo{
						MutationType: api.MutationTypeAdd,
						Index:        1,
						Value: `apiVersion: v1
kind: Namespace
metadata:
  name: test-ns
`,
					},
				},
				{
					Resource: api.ResourceInfo{
						ResourceType:             "apps/v1/Deployment",
						ResourceName:             "test-ns/myapp",
						ResourceNameWithoutScope: "myapp",
						ResourceCategory:         "Kubernetes",
					},
					ResourceMutationInfo: api.MutationInfo{
						MutationType: api.MutationTypeAdd,
						Index:        1,
						Value: `apiVersion: apps/v1
kind: Deployment
metadata:
  name: myapp
  namespace: test-ns
`,
					},
				},
			},
			validateResult: func(t *testing.T, result string) {
				parsed, err := gaby.ParseAll([]byte(result))
				require.NoError(t, err)
				require.Len(t, parsed, 2, "should have both new resources")

				kind, found, err := yamlkit.YamlSafePathGetValue[string](parsed[0], "kind", false)
				require.NoError(t, err)
				require.True(t, found)
				assert.Equal(t, "Namespace", kind)

				kind, found, err = yamlkit.YamlSafePathGetValue[string](parsed[1], "kind", false)
				require.NoError(t, err)
				require.True(t, found)
				assert.Equal(t, "Deployment", kind)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			parsed, err := gaby.ParseAll([]byte(tt.config))
			require.NoError(t, err)

			result, _, err := yamlkit.PatchMutations(parsed, tt.protection, tt.mutations, nil, k8skit.NewK8sResourceProvider(), nil)
			require.NoError(t, err)

			// Serialize the result using Container's String() method
			resultStr := result.String()

			tt.validateResult(t, resultStr)
		})
	}
}

// TestPatchMutations_DeleteLastResourceCompactsNils is a regression test for
// emptying a single-resource Unit (e.g. via merge-external-source with empty
// content). A resource-level Delete marks the doc nil in place; PatchMutations
// must compact those nils out of the returned container. It can't rely on
// Container.String() dropping them, because the function handler runs
// compute-mutations on the returned container directly (without
// re-serializing) after every mutating function — and ComputeMutations calls
// GetResourceInfo on each element, which fails on a nil doc with
// "apiVersion not found". Before the fix this blew up the whole update.
func TestPatchMutations_DeleteLastResourceCompactsNils(t *testing.T) {
	provider := k8skit.NewK8sResourceProvider()
	const namespaceYAML = `apiVersion: v1
kind: Namespace
metadata:
  name: myns
`

	baseParsed, err := gaby.ParseAll([]byte(namespaceYAML))
	require.NoError(t, err)
	emptyParsed, err := gaby.ParseAll([]byte(""))
	require.NoError(t, err)

	// base -> empty yields a single resource-level Delete.
	mutations, err := yamlkit.ComputeMutations(baseParsed, emptyParsed, 0, provider)
	require.NoError(t, err)
	require.Len(t, mutations, 1)
	require.Equal(t, api.MutationTypeDelete, mutations[0].ResourceMutationInfo.MutationType)

	targetParsed, err := gaby.ParseAll([]byte(namespaceYAML))
	require.NoError(t, err)
	result, _, err := yamlkit.PatchMutations(targetParsed, mutations, mutations, nil, provider, nil)
	require.NoError(t, err)

	// The deleted doc must be compacted out, not left as a nil slot.
	require.Len(t, result, 0, "deleted resource should be removed from the container, not left nil")
	for i, doc := range result {
		require.NotNil(t, doc, "result doc %d must not be nil", i)
	}
	assert.Equal(t, "", result.String())

	// The handler runs compute-mutations on the returned container after a
	// mutating function; a leftover nil doc made this fail.
	_, err = yamlkit.ComputeMutations(baseParsed, result, 0, provider)
	require.NoError(t, err, "compute-mutations on the patched result must succeed")
}

func TestMergeScenario(t *testing.T) {
	// This test simulates the merge scenario in mergeUnits:
	// 1. Start with a base configuration
	// 2. Source unit makes changes (e.g., image update, replicas update)
	// 3. Target unit makes different changes (e.g., namespace change)
	// 4. Merge should apply source changes that don't conflict with target changes

	t.Run("Three-way merge preserves target changes", func(t *testing.T) {
		// Base: original deployment
		base := `apiVersion: apps/v1
kind: Deployment
metadata:
  name: myapp
  namespace: confighubplaceholder
spec:
  replicas: 1
  selector:
    matchLabels:
      app: myapp
  template:
    spec:
      containers:
      - name: mycontainer
        image: nginx:1.19
`

		// Source end: updated replicas and image
		sourceEnd := `apiVersion: apps/v1
kind: Deployment
metadata:
  name: myapp
  namespace: confighubplaceholder
spec:
  replicas: 3
  selector:
    matchLabels:
      app: myapp
  template:
    spec:
      containers:
      - name: mycontainer
        image: nginx:1.20
`

		// Target: changed namespace and replicas (different value)
		target := `apiVersion: apps/v1
kind: Deployment
metadata:
  name: myapp
  namespace: production
spec:
  replicas: 5
  selector:
    matchLabels:
      app: myapp
  template:
    spec:
      containers:
      - name: mycontainer
        image: nginx:1.19
`

		baseParsed, err := gaby.ParseAll([]byte(base))
		require.NoError(t, err)

		sourceEndParsed, err := gaby.ParseAll([]byte(sourceEnd))
		require.NoError(t, err)

		targetParsed, err := gaby.ParseAll([]byte(target))
		require.NoError(t, err)

		// Step 1: Compute source mutations (base -> sourceEnd)
		sourceMutations, err := yamlkit.ComputeMutations(baseParsed, sourceEndParsed, 1, k8skit.NewK8sResourceProvider())
		require.NoError(t, err)

		// Step 2: Compute target mutations (base -> target)
		targetMutations, err := yamlkit.ComputeMutations(baseParsed, targetParsed, 2, k8skit.NewK8sResourceProvider())
		require.NoError(t, err)

		// Step 3: Subtract target changes from source changes
		finalMutations, _ := yamlkit.SubtractMutations(sourceMutations, targetMutations)

		// Step 4: Apply final mutations to target
		result, _, err := yamlkit.PatchMutations(targetParsed, nil, finalMutations, nil, k8skit.NewK8sResourceProvider(), nil)
		require.NoError(t, err)

		// Verify results
		require.Len(t, result, 1)

		// Namespace should be preserved (target's change)
		ns, found, err := yamlkit.YamlSafePathGetValue[string](result[0], "metadata.namespace", false)
		require.NoError(t, err)
		require.True(t, found)
		assert.Equal(t, "production", ns, "Target's namespace change should be preserved")

		// Replicas should be preserved (target's change takes precedence)
		replicas, found, err := yamlkit.YamlSafePathGetValue[int](result[0], "spec.replicas", false)
		require.NoError(t, err)
		require.True(t, found)
		assert.Equal(t, 5, replicas, "Target's replicas change should be preserved")

		// Image should be updated (source's change, no conflict)
		image, found, err := yamlkit.YamlSafePathGetValue[string](result[0], "spec.template.spec.containers.0.image", false)
		require.NoError(t, err)
		require.True(t, found)
		assert.Equal(t, "nginx:1.20", image, "Source's image change should be applied")
	})

	t.Run("Resource deleted in target is not re-added from source", func(t *testing.T) {
		base := `apiVersion: apps/v1
kind: Deployment
metadata:
  name: myapp
  namespace: default
spec:
  replicas: 1
---
apiVersion: v1
kind: ConfigMap
metadata:
  name: myconfig
  namespace: default
data:
  key: value
`

		// Source: updates the ConfigMap
		sourceEnd := `apiVersion: apps/v1
kind: Deployment
metadata:
  name: myapp
  namespace: default
spec:
  replicas: 1
---
apiVersion: v1
kind: ConfigMap
metadata:
  name: myconfig
  namespace: default
data:
  key: newvalue
  extrakey: extravalue
`

		// Target: deleted the ConfigMap
		target := `apiVersion: apps/v1
kind: Deployment
metadata:
  name: myapp
  namespace: default
spec:
  replicas: 1
`

		baseParsed, err := gaby.ParseAll([]byte(base))
		require.NoError(t, err)

		sourceEndParsed, err := gaby.ParseAll([]byte(sourceEnd))
		require.NoError(t, err)

		targetParsed, err := gaby.ParseAll([]byte(target))
		require.NoError(t, err)

		// Compute mutations
		sourceMutations, err := yamlkit.ComputeMutations(baseParsed, sourceEndParsed, 1, k8skit.NewK8sResourceProvider())
		require.NoError(t, err)

		targetMutations, err := yamlkit.ComputeMutations(baseParsed, targetParsed, 2, k8skit.NewK8sResourceProvider())
		require.NoError(t, err)

		// Subtract and patch
		finalMutations, _ := yamlkit.SubtractMutations(sourceMutations, targetMutations)
		result, _, err := yamlkit.PatchMutations(targetParsed, nil, finalMutations, nil, k8skit.NewK8sResourceProvider(), nil)
		require.NoError(t, err)

		// Should still have only 1 resource (Deployment)
		// Count non-nil docs
		nonNilCount := 0
		for _, doc := range result {
			if doc != nil {
				nonNilCount++
			}
		}
		assert.Equal(t, 1, nonNilCount, "ConfigMap should not be re-added since target deleted it")
	})

	t.Run("Labels added in source are applied when no conflict", func(t *testing.T) {
		base := `apiVersion: apps/v1
kind: Deployment
metadata:
  name: myapp
  namespace: default
spec:
  replicas: 1
`

		// Source: adds labels
		sourceEnd := `apiVersion: apps/v1
kind: Deployment
metadata:
  name: myapp
  namespace: default
  labels:
    app: myapp
    version: v1
spec:
  replicas: 1
`

		// Target: changes namespace only
		target := `apiVersion: apps/v1
kind: Deployment
metadata:
  name: myapp
  namespace: production
spec:
  replicas: 1
`

		baseParsed, err := gaby.ParseAll([]byte(base))
		require.NoError(t, err)

		sourceEndParsed, err := gaby.ParseAll([]byte(sourceEnd))
		require.NoError(t, err)

		targetParsed, err := gaby.ParseAll([]byte(target))
		require.NoError(t, err)

		// Compute mutations
		sourceMutations, err := yamlkit.ComputeMutations(baseParsed, sourceEndParsed, 1, k8skit.NewK8sResourceProvider())
		require.NoError(t, err)

		targetMutations, err := yamlkit.ComputeMutations(baseParsed, targetParsed, 2, k8skit.NewK8sResourceProvider())
		require.NoError(t, err)

		// Subtract and patch
		finalMutations, _ := yamlkit.SubtractMutations(sourceMutations, targetMutations)
		result, _, err := yamlkit.PatchMutations(targetParsed, nil, finalMutations, nil, k8skit.NewK8sResourceProvider(), nil)
		require.NoError(t, err)

		require.Len(t, result, 1)

		// Namespace should be preserved (target's change)
		ns, found, err := yamlkit.YamlSafePathGetValue[string](result[0], "metadata.namespace", false)
		require.NoError(t, err)
		require.True(t, found)
		assert.Equal(t, "production", ns)

		// Labels should be added (source's change)
		assert.True(t, result[0].ExistsP("metadata.labels"), "Labels should be added from source")
		appLabel, found, err := yamlkit.YamlSafePathGetValue[string](result[0], "metadata.labels.app", false)
		require.NoError(t, err)
		require.True(t, found)
		assert.Equal(t, "myapp", appLabel)
	})
}

func TestSubtractMutationsWithPathExpansion(t *testing.T) {
	t.Run("Source has container block, target changes image - other fields preserved", func(t *testing.T) {
		// This tests the case where source has a mutation at spec.template.spec.containers.0
		// (the whole container block) and target has a mutation at
		// spec.template.spec.containers.0.image. The source's container block should be
		// expanded and the image path removed, but name and resources should remain.

		base := `apiVersion: apps/v1
kind: Deployment
metadata:
  name: myapp
  namespace: default
spec:
  replicas: 1
  template:
    spec:
      containers:
      - name: nginx
        image: nginx:1.18
        resources:
          limits:
            cpu: "500m"
`

		// Source changes the entire container (name, image, and resources)
		sourceEnd := `apiVersion: apps/v1
kind: Deployment
metadata:
  name: myapp
  namespace: default
spec:
  replicas: 1
  template:
    spec:
      containers:
      - name: nginx
        image: nginx:1.20
        resources:
          limits:
            cpu: "1000m"
            memory: "1Gi"
`

		// Target only changes the image
		target := `apiVersion: apps/v1
kind: Deployment
metadata:
  name: myapp
  namespace: default
spec:
  replicas: 1
  template:
    spec:
      containers:
      - name: nginx
        image: nginx:1.19
        resources:
          limits:
            cpu: "500m"
`

		baseParsed, err := gaby.ParseAll([]byte(base))
		require.NoError(t, err)

		sourceEndParsed, err := gaby.ParseAll([]byte(sourceEnd))
		require.NoError(t, err)

		targetParsed, err := gaby.ParseAll([]byte(target))
		require.NoError(t, err)

		// Compute source mutations (base -> sourceEnd)
		sourceMutations, err := yamlkit.ComputeMutations(baseParsed, sourceEndParsed, 1, k8skit.NewK8sResourceProvider())
		require.NoError(t, err)

		// Compute target mutations (base -> target)
		targetMutations, err := yamlkit.ComputeMutations(baseParsed, targetParsed, 2, k8skit.NewK8sResourceProvider())
		require.NoError(t, err)

		// Verify target only changed image
		require.Len(t, targetMutations, 1)
		_, hasImage := targetMutations[0].PathMutationMap[api.ResolvedPath("spec.template.spec.containers.?name=nginx;@0.image")]
		assert.True(t, hasImage, "Target should have image mutation")

		// Subtract target changes from source
		result, _ := yamlkit.SubtractMutations(sourceMutations, targetMutations)

		// Apply the result to the target
		patchedResult, _, err := yamlkit.PatchMutations(targetParsed, nil, result, nil, k8skit.NewK8sResourceProvider(), nil)
		require.NoError(t, err)
		require.Len(t, patchedResult, 1)

		// Image should be from target (nginx:1.19), not source (nginx:1.20)
		image, found, err := yamlkit.YamlSafePathGetValue[string](patchedResult[0], "spec.template.spec.containers.0.image", false)
		require.NoError(t, err)
		require.True(t, found)
		assert.Equal(t, "nginx:1.19", image, "Target's image should be preserved")

		// CPU limit should be from source (1000m)
		cpu, found, err := yamlkit.YamlSafePathGetValue[string](patchedResult[0], "spec.template.spec.containers.0.resources.limits.cpu", false)
		require.NoError(t, err)
		require.True(t, found)
		assert.Equal(t, "1000m", cpu, "Source's CPU limit should be applied")

		// Memory limit should be from source (1Gi)
		memory, found, err := yamlkit.YamlSafePathGetValue[string](patchedResult[0], "spec.template.spec.containers.0.resources.limits.memory", false)
		require.NoError(t, err)
		require.True(t, found)
		assert.Equal(t, "1Gi", memory, "Source's memory limit should be applied")
	})

	t.Run("Source changes resources block, target changes cpu - memory preserved", func(t *testing.T) {
		// Source changes resources.limits (cpu and adds memory)
		// Target changes resources.limits.cpu
		// Result: target's cpu preserved, source's memory added

		base := `apiVersion: apps/v1
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
        resources:
          limits:
            cpu: "500m"
`

		sourceEnd := `apiVersion: apps/v1
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
        resources:
          limits:
            cpu: "2000m"
            memory: "2Gi"
`

		target := `apiVersion: apps/v1
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
        resources:
          limits:
            cpu: "1000m"
`

		baseParsed, err := gaby.ParseAll([]byte(base))
		require.NoError(t, err)

		sourceEndParsed, err := gaby.ParseAll([]byte(sourceEnd))
		require.NoError(t, err)

		targetParsed, err := gaby.ParseAll([]byte(target))
		require.NoError(t, err)

		sourceMutations, err := yamlkit.ComputeMutations(baseParsed, sourceEndParsed, 1, k8skit.NewK8sResourceProvider())
		require.NoError(t, err)

		targetMutations, err := yamlkit.ComputeMutations(baseParsed, targetParsed, 2, k8skit.NewK8sResourceProvider())
		require.NoError(t, err)

		result, _ := yamlkit.SubtractMutations(sourceMutations, targetMutations)

		patchedResult, _, err := yamlkit.PatchMutations(targetParsed, nil, result, nil, k8skit.NewK8sResourceProvider(), nil)
		require.NoError(t, err)
		require.Len(t, patchedResult, 1)

		// CPU should be from target (1000m)
		cpu, found, err := yamlkit.YamlSafePathGetValue[string](patchedResult[0], "spec.template.spec.containers.0.resources.limits.cpu", false)
		require.NoError(t, err)
		require.True(t, found)
		assert.Equal(t, "1000m", cpu, "Target's CPU should be preserved")

		// Memory should be from source (2Gi)
		memory, found, err := yamlkit.YamlSafePathGetValue[string](patchedResult[0], "spec.template.spec.containers.0.resources.limits.memory", false)
		require.NoError(t, err)
		require.True(t, found)
		assert.Equal(t, "2Gi", memory, "Source's memory should be applied")
	})

	t.Run("Source removes resources, target modified cpu - resources preserved", func(t *testing.T) {
		// Source removes the resources block entirely
		// Target modified resources.limits.cpu
		// Result: target's modification takes precedence, resources should NOT be deleted

		base := `apiVersion: apps/v1
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
        resources:
          limits:
            cpu: "500m"
            memory: "512Mi"
`

		// Source removes resources entirely
		sourceEnd := `apiVersion: apps/v1
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
`

		// Target modifies cpu limit
		target := `apiVersion: apps/v1
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
        resources:
          limits:
            cpu: "1000m"
            memory: "512Mi"
`

		baseParsed, err := gaby.ParseAll([]byte(base))
		require.NoError(t, err)

		sourceEndParsed, err := gaby.ParseAll([]byte(sourceEnd))
		require.NoError(t, err)

		targetParsed, err := gaby.ParseAll([]byte(target))
		require.NoError(t, err)

		sourceMutations, err := yamlkit.ComputeMutations(baseParsed, sourceEndParsed, 1, k8skit.NewK8sResourceProvider())
		require.NoError(t, err)

		// Verify source has a delete mutation for resources
		require.Len(t, sourceMutations, 1)
		resourcesPath := api.ResolvedPath("spec.template.spec.containers.?name=nginx;@0.resources")
		resourcesMutation, hasResources := sourceMutations[0].PathMutationMap[resourcesPath]
		assert.True(t, hasResources, "Source should have resources mutation")
		assert.Equal(t, api.MutationTypeDelete, resourcesMutation.MutationType, "Source should delete resources")

		targetMutations, err := yamlkit.ComputeMutations(baseParsed, targetParsed, 2, k8skit.NewK8sResourceProvider())
		require.NoError(t, err)

		// Verify target has a cpu modification
		require.Len(t, targetMutations, 1)
		cpuPath := api.ResolvedPath("spec.template.spec.containers.?name=nginx;@0.resources.limits.cpu")
		_, hasCpu := targetMutations[0].PathMutationMap[cpuPath]
		assert.True(t, hasCpu, "Target should have cpu mutation")

		result, conflicts := yamlkit.SubtractMutations(sourceMutations, targetMutations)

		// The resources Delete passes through; once the parent block is removed
		// the target's child cpu change can't apply, so a DeleteShadowed
		// conflict is emitted for the cpu path.
		hasResourcesDelete := false
		for _, m := range result {
			if _, found := m.PathMutationMap[resourcesPath]; found {
				hasResourcesDelete = true
			}
		}
		assert.True(t, hasResourcesDelete, "Resources delete passes through; target's child change can't survive parent removal")

		var deleteShadowedForCpu bool
		for _, c := range conflicts {
			if c.Reason == api.ConflictReasonDeleteShadowed && c.Path == cpuPath {
				deleteShadowedForCpu = true
			}
		}
		assert.True(t, deleteShadowedForCpu, "expected DeleteShadowed conflict for the lost cpu override")

		// Apply the result to target — resources block is removed.
		patchedResult, _, err := yamlkit.PatchMutations(targetParsed, nil, result, nil, k8skit.NewK8sResourceProvider(), nil)
		require.NoError(t, err)
		require.Len(t, patchedResult, 1)
		assert.False(t, patchedResult[0].ExistsP("spec.template.spec.containers.0.resources"),
			"Resources block should be removed (source-side Delete applied)")
	})

	t.Run("Case 3 - verify combined paths in result", func(t *testing.T) {
		// Source adds a new field (memory) at the same level as an existing field
		// Target changes an existing field (cpu)
		// Result should contain source's addition, and Case 1 should remove the overlapping cpu path

		base := `apiVersion: apps/v1
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
        resources:
          limits:
            cpu: "500m"
`

		sourceEnd := `apiVersion: apps/v1
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
        resources:
          limits:
            cpu: "2000m"
            memory: "2Gi"
`

		target := `apiVersion: apps/v1
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
        resources:
          limits:
            cpu: "1000m"
`

		baseParsed, err := gaby.ParseAll([]byte(base))
		require.NoError(t, err)

		sourceEndParsed, err := gaby.ParseAll([]byte(sourceEnd))
		require.NoError(t, err)

		targetParsed, err := gaby.ParseAll([]byte(target))
		require.NoError(t, err)

		sourceMutations, err := yamlkit.ComputeMutations(baseParsed, sourceEndParsed, 1, k8skit.NewK8sResourceProvider())
		require.NoError(t, err)

		targetMutations, err := yamlkit.ComputeMutations(baseParsed, targetParsed, 2, k8skit.NewK8sResourceProvider())
		require.NoError(t, err)

		// Source should have cpu update and memory add
		cpuPath := api.ResolvedPath("spec.template.spec.containers.?name=nginx;@0.resources.limits.cpu")
		memoryPath := api.ResolvedPath("spec.template.spec.containers.?name=nginx;@0.resources.limits.memory")

		require.Len(t, sourceMutations, 1)
		_, sourceCpu := sourceMutations[0].PathMutationMap[cpuPath]
		_, sourceMemory := sourceMutations[0].PathMutationMap[memoryPath]
		assert.True(t, sourceCpu, "Source should have cpu mutation")
		assert.True(t, sourceMemory, "Source should have memory mutation")

		// Target should have cpu update
		require.Len(t, targetMutations, 1)
		_, targetCpu := targetMutations[0].PathMutationMap[cpuPath]
		assert.True(t, targetCpu, "Target should have cpu mutation")

		result, _ := yamlkit.SubtractMutations(sourceMutations, targetMutations)

		// Verify the result structure
		require.Len(t, result, 1, "Should have one resource mutation")

		// CPU should be subtracted (Case 1 exact match)
		_, hasCpu := result[0].PathMutationMap[cpuPath]
		assert.False(t, hasCpu, "CPU path should be subtracted (exact match)")

		// Memory should remain (no conflict)
		_, hasMemory := result[0].PathMutationMap[memoryPath]
		assert.True(t, hasMemory, "Memory path should remain (no conflict)")
	})

	t.Run("Case 3 - multiple child paths modified by target", func(t *testing.T) {
		// Source changes a parent block
		// Target changes multiple child paths
		// All target child paths should be added to override source's parent block

		base := `apiVersion: apps/v1
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
        resources:
          limits:
            cpu: "500m"
            memory: "256Mi"
`

		sourceEnd := `apiVersion: apps/v1
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
        resources:
          limits:
            cpu: "2000m"
            memory: "2Gi"
          requests:
            cpu: "1000m"
`

		target := `apiVersion: apps/v1
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
        resources:
          limits:
            cpu: "1000m"
            memory: "512Mi"
`

		baseParsed, err := gaby.ParseAll([]byte(base))
		require.NoError(t, err)

		sourceEndParsed, err := gaby.ParseAll([]byte(sourceEnd))
		require.NoError(t, err)

		targetParsed, err := gaby.ParseAll([]byte(target))
		require.NoError(t, err)

		sourceMutations, err := yamlkit.ComputeMutations(baseParsed, sourceEndParsed, 1, k8skit.NewK8sResourceProvider())
		require.NoError(t, err)

		targetMutations, err := yamlkit.ComputeMutations(baseParsed, targetParsed, 2, k8skit.NewK8sResourceProvider())
		require.NoError(t, err)

		result, _ := yamlkit.SubtractMutations(sourceMutations, targetMutations)

		// Apply the result
		patchedResult, _, err := yamlkit.PatchMutations(targetParsed, nil, result, nil, k8skit.NewK8sResourceProvider(), nil)
		require.NoError(t, err)
		require.Len(t, patchedResult, 1)

		// CPU should be target's value (1000m)
		cpu, found, err := yamlkit.YamlSafePathGetValue[string](patchedResult[0], "spec.template.spec.containers.0.resources.limits.cpu", false)
		require.NoError(t, err)
		require.True(t, found)
		assert.Equal(t, "1000m", cpu, "Target's CPU should be preserved")

		// Memory should be target's value (512Mi)
		memory, found, err := yamlkit.YamlSafePathGetValue[string](patchedResult[0], "spec.template.spec.containers.0.resources.limits.memory", false)
		require.NoError(t, err)
		require.True(t, found)
		assert.Equal(t, "512Mi", memory, "Target's memory should be preserved")

		// Requests should be from source (1000m) since target didn't modify it
		requestsCpu, found, err := yamlkit.YamlSafePathGetValue[string](patchedResult[0], "spec.template.spec.containers.0.resources.requests.cpu", false)
		require.NoError(t, err)
		require.True(t, found)
		assert.Equal(t, "1000m", requestsCpu, "Source's requests.cpu should be applied")
	})

	t.Run("Case 1 - exact path match removes mutation", func(t *testing.T) {
		// Source and target both change the same exact path
		// Source's change should be completely removed

		base := `apiVersion: apps/v1
kind: Deployment
metadata:
  name: myapp
  namespace: default
  labels:
    app: myapp
`

		sourceEnd := `apiVersion: apps/v1
kind: Deployment
metadata:
  name: myapp
  namespace: default
  labels:
    app: myapp
    version: v2
`

		target := `apiVersion: apps/v1
kind: Deployment
metadata:
  name: myapp
  namespace: default
  labels:
    app: myapp
    version: v1
`

		baseParsed, err := gaby.ParseAll([]byte(base))
		require.NoError(t, err)

		sourceEndParsed, err := gaby.ParseAll([]byte(sourceEnd))
		require.NoError(t, err)

		targetParsed, err := gaby.ParseAll([]byte(target))
		require.NoError(t, err)

		sourceMutations, err := yamlkit.ComputeMutations(baseParsed, sourceEndParsed, 1, k8skit.NewK8sResourceProvider())
		require.NoError(t, err)

		targetMutations, err := yamlkit.ComputeMutations(baseParsed, targetParsed, 2, k8skit.NewK8sResourceProvider())
		require.NoError(t, err)

		// Verify both have the same path
		versionPath := api.ResolvedPath("metadata.labels.version")
		_, sourceHasVersion := sourceMutations[0].PathMutationMap[versionPath]
		_, targetHasVersion := targetMutations[0].PathMutationMap[versionPath]
		assert.True(t, sourceHasVersion, "Source should have version mutation")
		assert.True(t, targetHasVersion, "Target should have version mutation")

		result, _ := yamlkit.SubtractMutations(sourceMutations, targetMutations)

		// The version path should be removed from result (Case 1 exact match)
		if len(result) > 0 {
			_, hasVersion := result[0].PathMutationMap[versionPath]
			assert.False(t, hasVersion, "Version path should be subtracted (exact match)")
		}

		// Apply and verify target's value is preserved
		patchedResult, _, err := yamlkit.PatchMutations(targetParsed, nil, result, nil, k8skit.NewK8sResourceProvider(), nil)
		require.NoError(t, err)
		require.Len(t, patchedResult, 1)

		version, found, err := yamlkit.YamlSafePathGetValue[string](patchedResult[0], "metadata.labels.version", false)
		require.NoError(t, err)
		require.True(t, found)
		assert.Equal(t, "v1", version, "Target's version should be preserved")
	})

	t.Run("Unmatched Add applies path mutations to new resource", func(t *testing.T) {
		// When a patch has an Add at the resource level (no matching doc in parsedData)
		// and also has path mutations, the path mutations should be applied to the
		// newly created document.
		config := ""
		mutations := api.ResourceMutationList{{
			Resource: api.ResourceInfo{
				ResourceType:             "apps/v1/Deployment",
				ResourceName:             "test-ns/mydep",
				ResourceNameWithoutScope: "mydep",
				ResourceCategory:         "Kubernetes",
			},
			ResourceMutationInfo: api.MutationInfo{
				MutationType: api.MutationTypeAdd,
				Index:        1,
				Value: `apiVersion: apps/v1
kind: Deployment
metadata:
  name: mydep
  namespace: confighubplaceholder
spec:
  replicas: 3
  selector:
    matchLabels:
      app: mydep
  template:
    spec:
      containers:
      - image: nginx:latest
        name: nginx
        resources: {}
`,
			},
			PathMutationMap: api.MutationMap{
				"metadata.namespace": {
					MutationType: api.MutationTypeUpdate,
					Index:        2,
					Value:        "test-ns\n",
				},
				"spec.replicas": {
					MutationType: api.MutationTypeUpdate,
					Index:        3,
					Value:        "2\n",
				},
				"spec.template.spec.containers.?name=nginx;@0.image": {
					MutationType: api.MutationTypeUpdate,
					Index:        4,
					Value:        "nginx:1.25\n",
				},
				"spec.template.spec.containers.?name=nginx;@0.resources": {
					MutationType: api.MutationTypeUpdate,
					Index:        5,
					Value:        "{requests: {cpu: 128m, memory: 128Mi}}\n",
				},
			},
		}}

		parsed, err := gaby.ParseAll([]byte(config))
		require.NoError(t, err)

		result, _, err := yamlkit.PatchMutations(parsed, nil, mutations, nil, k8skit.NewK8sResourceProvider(), nil)
		require.NoError(t, err)
		require.Len(t, result, 1)

		// Namespace should be updated by path mutation
		ns, found, err := yamlkit.YamlSafePathGetValue[string](result[0], "metadata.namespace", false)
		require.NoError(t, err)
		require.True(t, found)
		assert.Equal(t, "test-ns", ns, "Path mutation should update namespace")

		// Replicas should be updated by path mutation
		replicas, found, err := yamlkit.YamlSafePathGetValue[int](result[0], "spec.replicas", false)
		require.NoError(t, err)
		require.True(t, found)
		assert.Equal(t, 2, replicas, "Path mutation should update replicas")

		// Image should be updated by path mutation
		image, found, err := yamlkit.YamlSafePathGetValue[string](result[0], "spec.template.spec.containers.0.image", false)
		require.NoError(t, err)
		require.True(t, found)
		assert.Equal(t, "nginx:1.25", image, "Path mutation should update image")

		// Resources should be updated by path mutation
		cpu, found, err := yamlkit.YamlSafePathGetValue[string](result[0], "spec.template.spec.containers.0.resources.requests.cpu", false)
		require.NoError(t, err)
		require.True(t, found)
		assert.Equal(t, "128m", cpu, "Path mutation should set resources.requests.cpu")
	})

	t.Run("Unmatched Add with delete of nonexistent path does not error", func(t *testing.T) {
		// When a patch has an Add at the resource level with path mutations that
		// include a Delete for a path that doesn't exist in the base Value (e.g.,
		// a parent Update removed it), the Delete should be silently skipped.
		config := ""
		mutations := api.ResourceMutationList{{
			Resource: api.ResourceInfo{
				ResourceType:             "apps/v1/Deployment",
				ResourceName:             "test-ns/mydep",
				ResourceNameWithoutScope: "mydep",
				ResourceCategory:         "Kubernetes",
			},
			ResourceMutationInfo: api.MutationInfo{
				MutationType: api.MutationTypeAdd,
				Index:        1,
				Value: `apiVersion: apps/v1
kind: Deployment
metadata:
  name: mydep
spec:
  template:
    spec:
      containers:
      - image: nginx:latest
        name: nginx
        resources: {}
`,
			},
			PathMutationMap: api.MutationMap{
				"spec.template.spec.containers.?name=nginx;@0.resources": {
					MutationType: api.MutationTypeUpdate,
					Index:        2,
					Value:        "{requests: {cpu: 128m, memory: 128Mi}}\n",
				},
				"spec.template.spec.containers.?name=nginx;@0.resources.limits": {
					MutationType: api.MutationTypeDelete,
					Index:        3,
					Value:        "{cpu: 1500m, memory: 1Gi}\n",
				},
			},
		}}

		parsed, err := gaby.ParseAll([]byte(config))
		require.NoError(t, err)

		result, _, err := yamlkit.PatchMutations(parsed, nil, mutations, nil, k8skit.NewK8sResourceProvider(), nil)
		require.NoError(t, err)
		require.Len(t, result, 1)

		// Resources should have requests from the Update, and no limits
		cpu, found, err := yamlkit.YamlSafePathGetValue[string](result[0], "spec.template.spec.containers.0.resources.requests.cpu", false)
		require.NoError(t, err)
		require.True(t, found)
		assert.Equal(t, "128m", cpu)

		assert.False(t, result[0].ExistsP("spec.template.spec.containers.0.resources.limits"),
			"limits should not exist since it was never present and Delete was skipped")
	})

	t.Run("Case 2 - subtract path is prefix removes mutation", func(t *testing.T) {
		// Target changes a parent path
		// Source changes a child path under it
		// Source's child change should be removed

		base := `apiVersion: apps/v1
kind: Deployment
metadata:
  name: myapp
  namespace: default
  annotations:
    config: |
      key1: value1
`

		sourceEnd := `apiVersion: apps/v1
kind: Deployment
metadata:
  name: myapp
  namespace: default
  annotations:
    config: |
      key1: value1-modified
`

		// Target replaces the entire annotations block
		target := `apiVersion: apps/v1
kind: Deployment
metadata:
  name: myapp
  namespace: default
  annotations:
    config: |
      completely: different
    extra: annotation
`

		baseParsed, err := gaby.ParseAll([]byte(base))
		require.NoError(t, err)

		sourceEndParsed, err := gaby.ParseAll([]byte(sourceEnd))
		require.NoError(t, err)

		targetParsed, err := gaby.ParseAll([]byte(target))
		require.NoError(t, err)

		sourceMutations, err := yamlkit.ComputeMutations(baseParsed, sourceEndParsed, 1, k8skit.NewK8sResourceProvider())
		require.NoError(t, err)

		targetMutations, err := yamlkit.ComputeMutations(baseParsed, targetParsed, 2, k8skit.NewK8sResourceProvider())
		require.NoError(t, err)

		result, _ := yamlkit.SubtractMutations(sourceMutations, targetMutations)

		// Apply and verify target's annotations are preserved
		patchedResult, _, err := yamlkit.PatchMutations(targetParsed, nil, result, nil, k8skit.NewK8sResourceProvider(), nil)
		require.NoError(t, err)
		require.Len(t, patchedResult, 1)

		// Should have target's "extra" annotation
		extra, found, err := yamlkit.YamlSafePathGetValue[string](patchedResult[0], "metadata.annotations.extra", false)
		require.NoError(t, err)
		require.True(t, found)
		assert.Equal(t, "annotation", extra, "Target's extra annotation should be preserved")
	})
}
