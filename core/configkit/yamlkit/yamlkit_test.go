// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package yamlkit

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/confighub/sdk/core/function/api"
	"github.com/confighub/sdk/core/third_party/gaby"
	"github.com/confighub/sdk/core/workerapi"
)

// testResourceProvider is a minimal ResourceProvider for testing purposes only.
type testResourceProvider struct {
	registry ResourceProviderRegistry
}

func (testResourceProvider) ResourceTypeGetter(doc *gaby.YamlDoc) (api.ResourceType, error) {
	apiVersion, _, _ := YamlSafePathGetValue[string](doc, api.ResolvedPath("apiVersion"), false)
	kind, _, _ := YamlSafePathGetValue[string](doc, api.ResolvedPath("kind"), false)
	return api.ResourceType(apiVersion + "/" + kind), nil
}

func (testResourceProvider) ResourceNameGetter(doc *gaby.YamlDoc) (api.ResourceName, error) {
	namespace, _, _ := YamlSafePathGetValue[string](doc, api.ResolvedPath("metadata.namespace"), true)
	name, _, _ := YamlSafePathGetValue[string](doc, api.ResolvedPath("metadata.name"), false)
	return api.ResourceName(namespace + "/" + name), nil
}

func (testResourceProvider) DefaultResourceCategory() api.ResourceCategory {
	return api.ResourceCategoryResource
}
func (testResourceProvider) ResourceCategoryGetter(_ *gaby.YamlDoc) (api.ResourceCategory, error) {
	return api.ResourceCategoryResource, nil
}
func (testResourceProvider) ResourceNameStableCoreGetter(_ *gaby.YamlDoc) (api.ResourceName, error) { return "", nil }
func (testResourceProvider) RemoveScopeFromResourceName(name api.ResourceName) api.ResourceName {
	return name
}
func (testResourceProvider) ScopelessResourceNamePath() api.ResolvedPath {
	return "metadata.name"
}
func (testResourceProvider) SetResourceName(doc *gaby.YamlDoc, name string) error {
	_, err := doc.SetP(name, "metadata.name")
	return err
}
func (testResourceProvider) ResourceTypesAreSimilar(a, b api.ResourceType) bool { return a == b }
func (testResourceProvider) TypeDescription() string                          { return "Kind" }
func (testResourceProvider) NormalizeName(name string) string                 { return name }
func (testResourceProvider) NameSeparator() string                            { return "/" }
func (testResourceProvider) ContextPath(field string) string                  { return "metadata." + field }
func (t testResourceProvider) GetPathRegistry() api.AttributeNameToResourceTypeToPathToVisitorInfoType {
	return t.registry.PathRegistry
}
func (t testResourceProvider) GetAttributeRegistry() api.AttributeNameToAttributeDescriptor {
	return t.registry.AttributeRegistry
}
func (t *testResourceProvider) GetRegistry() *ResourceProviderRegistry {
	return &t.registry
}
func (testResourceProvider) MergeKeyForPath(_ api.ResourceType, _ string) (string, bool) {
	return "", false
}

func (testResourceProvider) IsMapKeyPath(_ api.ResourceType, _ string) bool {
	return false
}

func (testResourceProvider) GetToolchainType() workerapi.ToolchainType {
	return workerapi.ToolchainKubernetesYAML
}

var testProvider = &testResourceProvider{
	registry: NewResourceProviderRegistry(),
}

func TestResolveAssociation(t *testing.T) {
	// YAML fixture
	yamlFixture := `apiVersion: apps/v1
kind: Deployment
metadata:
  name: example-deployment
spec:
  replicas: 3
  selector:
    matchLabels:
      app: example
  template:
    metadata:
      labels:
        app: example
    spec:
      containers:
      - name: example-container
        image: nginx:1.14.2
        env:
        - name: EXAMPLE_ENV
          value: example-value
`
	docs, err := gaby.ParseAll([]byte(yamlFixture))
	assert.NoError(t, err)
	results, err := ResolveAssociativePaths(docs[0], api.UnresolvedPath("spec.template.spec.containers.?name=example-container.env.?name=EXAMPLE_ENV.value"), "", false, nil)
	assert.NoError(t, err)
	assert.Equal(t, len(results), 1)
	assert.Equal(t, api.ResolvedPath("spec.template.spec.containers.0.env.0.value"), results[0].Path)
}

func TestResolveAssociation_MultipleContainers(t *testing.T) {
	// YAML fixture with multiple containers
	yamlFixture := `apiVersion: apps/v1
kind: Deployment
metadata:
  name: multi-container-deployment
spec:
  template:
    spec:
      containers:
      - name: container-one
        image: nginx:1.14.2
      - name: container-two
        image: redis:5.0
`
	docs, err := gaby.ParseAll([]byte(yamlFixture))
	assert.NoError(t, err)
	results, err := ResolveAssociativePaths(docs[0], api.UnresolvedPath("spec.template.spec.containers.?name=container-two.image"), "", false, nil)
	assert.NoError(t, err)
	assert.Equal(t, len(results), 1)
	assert.Equal(t, api.ResolvedPath("spec.template.spec.containers.1.image"), results[0].Path)
}

func TestResolveAssociation_MissingKeys(t *testing.T) {
	// YAML fixture with missing keys
	yamlFixture := `apiVersion: v1
kind: Service
metadata:
  name: example-service
spec:
  ports:
  - port: 80
    targetPort: 80
`
	docs, err := gaby.ParseAll([]byte(yamlFixture))
	assert.NoError(t, err)
	results, err := ResolveAssociativePaths(docs[0], api.UnresolvedPath("spec.ports.?name=http.port"), "", false, nil)
	assert.NoError(t, err)
	assert.Empty(t, results)
}

func TestResolveAssociation_DuplicateKeys(t *testing.T) {
	// YAML fixture with duplicate keys in an array
	yamlFixture := `apiVersion: v1
kind: List
items:
- name: duplicate-item
  value: first
- name: duplicate-item
  value: second
`
	docs, err := gaby.ParseAll([]byte(yamlFixture))
	assert.NoError(t, err)
	results, err := ResolveAssociativePaths(docs[0], api.UnresolvedPath("items.?name=duplicate-item.value"), "", false, nil)
	assert.NoError(t, err)
	// Expecting the first occurrence
	assert.Equal(t, len(results), 1)
	assert.Equal(t, api.ResolvedPath("items.0.value"), results[0].Path)
}

func TestResolveAssociation_EmptyArray(t *testing.T) {
	// YAML fixture with an empty array
	yamlFixture := `apiVersion: v1
kind: ConfigMap
metadata:
  name: empty-array-configmap
data: {}
`
	docs, err := gaby.ParseAll([]byte(yamlFixture))
	assert.NoError(t, err)
	results, err := ResolveAssociativePaths(docs[0], api.UnresolvedPath("data.?key=nonexistent"), "", false, nil)
	assert.NoError(t, err)
	assert.Empty(t, results)
}

func TestResolveAssociation_SubPath_MultipleContainers(t *testing.T) {
	// YAML fixture with multiple containers
	yamlFixture := `apiVersion: apps/v1
kind: Deployment
metadata:
  name: multi-container-deployment
spec:
  template:
    spec:
      containers:
      - name: container-one
        image: nginx:1.14.2
      - name: container-two
        image: redis:5.0
`
	docs, err := gaby.ParseAll([]byte(yamlFixture))
	assert.NoError(t, err)
	c := docs[0].Path("spec.template.spec.containers")
	results, err := ResolveAssociativePaths(c, api.UnresolvedPath("?name=container-two.image"), "", false, nil)
	assert.NoError(t, err)
	assert.Equal(t, len(results), 1)
	assert.Equal(t, api.ResolvedPath("1.image"), results[0].Path)
	image := c.Path(string(results[0].Path)).Data().(string)
	assert.Equal(t, "redis:5.0", image)
}

func TestResolveWildcard(t *testing.T) {
	// YAML fixture
	yamlFixture := `apiVersion: rbac.authorization.k8s.io/v1
kind: RoleBinding
metadata:
  name: myrb
  namespace: example
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: Role
  name: robot-role
subjects:
- kind: ServiceAccount
  name: robot-sa
  namespace: somens
- kind: ServiceAccount
  name: my-sa
  namespace: somens
`
	docs, err := gaby.ParseAll([]byte(yamlFixture))
	assert.NoError(t, err)
	results, err := ResolveAssociativePaths(docs[0], api.UnresolvedPath("subjects.*.namespace"), "", false, nil)
	assert.NoError(t, err)
	assert.Equal(t, len(results), 2)
	assert.Equal(t, api.ResolvedPath("subjects.0.namespace"), results[0].Path)
	assert.Equal(t, api.ResolvedPath("subjects.1.namespace"), results[1].Path)
}

func TestResolveWildcardFilter(t *testing.T) {
	yamlFixture := `apiVersion: rbac.authorization.k8s.io/v1
kind: RoleBinding
metadata:
  name: myrb
subjects:
- kind: ServiceAccount
  name: robot-sa
- kind: Group
  name: admins
- kind: ServiceAccount
  name: my-sa
  namespace: existing
- kind: User
  name: alice
`
	docs, err := gaby.ParseAll([]byte(yamlFixture))
	assert.NoError(t, err)

	// Filter: only ServiceAccount subjects. namespace doesn't exist on the first SA.
	// Without "|" on namespace, non-upsert mode skips paths that don't exist, so we
	// only get the one SA that already has namespace.
	results, err := ResolveAssociativePaths(docs[0], api.UnresolvedPath("subjects.*?kind=ServiceAccount.namespace"), "", false, nil)
	assert.NoError(t, err)
	assert.Equal(t, 1, len(results))
	assert.Equal(t, api.ResolvedPath("subjects.2.namespace"), results[0].Path)

	// With "|", upsert mode resolves both SA subjects (creates namespace on the one missing it).
	results, err = ResolveAssociativePaths(docs[0], api.UnresolvedPath("subjects.*?kind=ServiceAccount.|namespace"), "", true, nil)
	assert.NoError(t, err)
	assert.Equal(t, 2, len(results))
	resolvedPaths := []api.ResolvedPath{results[0].Path, results[1].Path}
	assert.Contains(t, resolvedPaths, api.ResolvedPath("subjects.0.namespace"))
	assert.Contains(t, resolvedPaths, api.ResolvedPath("subjects.2.namespace"))

	// Filter with parameter binding: *?kind:k=ServiceAccount.|namespace binds kind to k.
	results, err = ResolveAssociativePaths(docs[0], api.UnresolvedPath("subjects.*?kind:k=ServiceAccount.|namespace"), "", true, nil)
	assert.NoError(t, err)
	assert.Equal(t, 2, len(results))
	for _, r := range results {
		assert.Equal(t, 1, len(r.PathArguments))
		assert.Equal(t, "k", r.PathArguments[0].ParameterName)
		assert.Equal(t, "ServiceAccount", r.PathArguments[0].Value)
	}

	// No matches when the filter value doesn't appear.
	results, err = ResolveAssociativePaths(docs[0], api.UnresolvedPath("subjects.*?kind=Robot.|namespace"), "", true, nil)
	assert.NoError(t, err)
	assert.Equal(t, 0, len(results))
}

// TestResolveWildcard_SiblingDisjointChildren is the regression for a
// slice-sharing bug in the `*` wildcard expansion. When the parent's
// ResolvedSegments had spare capacity (cap > len), every for-loop iteration
// over arrayChildren wrote its index string to the same backing-array slot,
// so all enqueued positions ended up with the LAST index baked into their
// path. The visible failure: for an envFrom array whose siblings have
// disjoint child fields (envFrom[0].configMapRef vs envFrom[1].secretRef),
// only the LAST sibling's path resolved cleanly — the wrong index made
// the earlier sibling's full-path lookup miss in the live document.
//
// The fix clamps the parent slice's capacity to its length so each
// `append` allocates a fresh backing array.
//
// The earlier TestResolveWildcard didn't catch this because its parent
// ResolvedSegments had cap == len at the wildcard, so each iteration
// forced a fresh allocation regardless.
func TestResolveWildcard_SiblingDisjointChildren(t *testing.T) {
	// Real-shaped fixture — the bug triggers at the second wildcard in
	// `containers.*.envFrom.*.X`, where the parent ResolvedSegments
	// already has spare capacity from the first wildcard's allocation.
	yamlFixture := `apiVersion: apps/v1
kind: Deployment
metadata:
  name: dep
spec:
  template:
    spec:
      containers:
        - name: app
          envFrom:
            - configMapRef:
                name: cm-x
            - secretRef:
                name: secret-y
`
	docs, err := gaby.ParseAll([]byte(yamlFixture))
	assert.NoError(t, err)

	// configMapRef.name lives at envFrom[0] only. Pre-fix, the resolution
	// produced "envFrom.1.configMapRef.name" (wrong index), which then
	// failed the live-doc lookup in VisitPathsDoc and got dropped.
	results, err := ResolveAssociativePaths(docs[0],
		api.UnresolvedPath("spec.template.spec.containers.*.envFrom.*.configMapRef.name"),
		"", false, nil)
	assert.NoError(t, err)
	assert.Equal(t, 1, len(results), "configMapRef resolution should return one path (envFrom[0])")
	if len(results) == 1 {
		assert.Equal(t, api.ResolvedPath("spec.template.spec.containers.0.envFrom.0.configMapRef.name"), results[0].Path)
	}

	// secretRef.name lives at envFrom[1] only. This one used to resolve
	// even pre-fix because the wrong-index happened to land on the
	// secretRef sibling.
	results, err = ResolveAssociativePaths(docs[0],
		api.UnresolvedPath("spec.template.spec.containers.*.envFrom.*.secretRef.name"),
		"", false, nil)
	assert.NoError(t, err)
	assert.Equal(t, 1, len(results), "secretRef resolution should return one path (envFrom[1])")
	if len(results) == 1 {
		assert.Equal(t, api.ResolvedPath("spec.template.spec.containers.0.envFrom.1.secretRef.name"), results[0].Path)
	}
}

func TestResolveAssociation_NamedAssociation(t *testing.T) {
	// YAML fixture with multiple containers
	yamlFixture := `apiVersion: apps/v1
kind: Deployment
metadata:
  name: multi-container-deployment
spec:
  template:
    spec:
      containers:
      - name: container-one
        image: nginx:1.14.2
      - name: container-two
        image: redis:5.0
`
	docs, err := gaby.ParseAll([]byte(yamlFixture))
	assert.NoError(t, err)
	results, err := ResolveAssociativePaths(docs[0], api.UnresolvedPath("spec.template.spec.containers.?name:containerName=container-two.image"), "", false, nil)
	assert.NoError(t, err)
	assert.Equal(t, len(results), 1)
	assert.Equal(t, api.ResolvedPath("spec.template.spec.containers.1.image"), results[0].Path)
	assert.Equal(t, len(results[0].PathArguments), 1)
	assert.Equal(t, results[0].PathArguments[0].ParameterName, "containerName")
	stringValue, ok := results[0].PathArguments[0].Value.(string)
	assert.True(t, ok)
	assert.Equal(t, stringValue, "container-two")
}

func TestResolveNamedWildcard(t *testing.T) {
	// YAML fixture with multiple containers
	yamlFixture := `apiVersion: apps/v1
kind: Deployment
metadata:
  name: multi-container-deployment
spec:
  template:
    spec:
      containers:
      - name: container-one
        image: nginx:1.14.2
      - name: container-two
        image: redis:5.0
`
	docs, err := gaby.ParseAll([]byte(yamlFixture))
	assert.NoError(t, err)
	results, err := ResolveAssociativePaths(docs[0], api.UnresolvedPath("spec.template.spec.containers.*?name:containerName.image"), "", false, nil)
	assert.NoError(t, err)
	assert.Equal(t, len(results), 2)
	assert.Equal(t, api.ResolvedPath("spec.template.spec.containers.0.image"), results[0].Path)
	assert.Equal(t, len(results[0].PathArguments), 1)
	assert.Equal(t, results[0].PathArguments[0].ParameterName, "containerName")
	stringValue, ok := results[0].PathArguments[0].Value.(string)
	assert.True(t, ok)
	assert.Equal(t, stringValue, "container-one")
	assert.Equal(t, api.ResolvedPath("spec.template.spec.containers.1.image"), results[1].Path)
	assert.Equal(t, len(results[1].PathArguments), 1)
	assert.Equal(t, results[1].PathArguments[0].ParameterName, "containerName")
	stringValue, ok = results[1].PathArguments[0].Value.(string)
	assert.True(t, ok)
	assert.Equal(t, stringValue, "container-two")
}

func TestResolveAssociativePaths_UpsertMode(t *testing.T) {
	// YAML fixture without securityContext
	yamlFixture := `apiVersion: apps/v1
kind: Deployment
metadata:
  name: example-deployment
spec:
  template:
    spec:
      containers:
      - name: nginx
        image: nginx:1.14.2
      - name: redis  
        image: redis:5.0
`
	docs, err := gaby.ParseAll([]byte(yamlFixture))
	assert.NoError(t, err)

	// Test 1: Upsert with wildcard should resolve paths even when target doesn't exist
	results, err := ResolveAssociativePaths(docs[0], api.UnresolvedPath("spec.template.spec.containers.*.securityContext.runAsNonRoot"), "", true, nil)
	assert.NoError(t, err)
	assert.Equal(t, 2, len(results))
	assert.Equal(t, api.ResolvedPath("spec.template.spec.containers.0.securityContext.runAsNonRoot"), results[0].Path)
	assert.Equal(t, api.ResolvedPath("spec.template.spec.containers.1.securityContext.runAsNonRoot"), results[1].Path)

	// Test 2: Upsert with associative match should resolve path even when target doesn't exist  
	results, err = ResolveAssociativePaths(docs[0], api.UnresolvedPath("spec.template.spec.containers.?name=nginx.securityContext.runAsNonRoot"), "", true, nil)
	assert.NoError(t, err)
	assert.Equal(t, 1, len(results))
	assert.Equal(t, api.ResolvedPath("spec.template.spec.containers.0.securityContext.runAsNonRoot"), results[0].Path)

	// Test 3: Non-upsert mode should not resolve non-existent paths
	results, err = ResolveAssociativePaths(docs[0], api.UnresolvedPath("spec.template.spec.containers.*.securityContext.runAsNonRoot"), "", false, nil)
	assert.NoError(t, err)
	assert.Equal(t, 0, len(results))
}

func TestResolveAssociativePaths_PrecedingPathExistenceCheck(t *testing.T) {
	// YAML fixture with existing securityContext
	yamlFixture := `apiVersion: apps/v1
kind: Deployment
metadata:
  name: example-deployment
spec:
  template:
    spec:
      containers:
      - name: nginx
        image: nginx:1.14.2
        securityContext:
          runAsUser: 1000
      - name: redis  
        image: redis:5.0
`
	docs, err := gaby.ParseAll([]byte(yamlFixture))
	assert.NoError(t, err)

	// Test 1: "|" syntax should resolve when preceding path exists, even if current segment doesn't exist
	results, err := ResolveAssociativePaths(docs[0], api.UnresolvedPath("spec.template.spec.containers.0.|newField"), "", true, nil)
	assert.NoError(t, err)
	assert.Equal(t, 1, len(results))
	assert.Equal(t, api.ResolvedPath("spec.template.spec.containers.0.newField"), results[0].Path)

	// Test 2: "|" syntax should also work when current segment exists
	results, err = ResolveAssociativePaths(docs[0], api.UnresolvedPath("spec.template.spec.containers.0.|securityContext"), "", true, nil)
	assert.NoError(t, err)
	assert.Equal(t, 1, len(results))
	assert.Equal(t, api.ResolvedPath("spec.template.spec.containers.0.securityContext"), results[0].Path)

	// Test 3: "|" syntax should not resolve when preceding path doesn't exist
	results, err = ResolveAssociativePaths(docs[0], api.UnresolvedPath("spec.template.spec.nonexistent.0.|field"), "", true, nil)
	assert.NoError(t, err)
	assert.Equal(t, 0, len(results))
}

func TestResolveAssociativePaths_UpsertWithResolvedSegments(t *testing.T) {
	// YAML fixture
	yamlFixture := `apiVersion: apps/v1
kind: Deployment
metadata:
  name: example-deployment
spec:
  template:
    spec:
      containers:
      - name: nginx
        image: nginx:1.14.2
`
	docs, err := gaby.ParseAll([]byte(yamlFixture))
	assert.NoError(t, err)

	// Test: Upsert should handle paths that have both search expressions and resolved segments
	results, err := ResolveAssociativePaths(docs[0], api.UnresolvedPath("spec.template.spec.containers.*.securityContext.runAsNonRoot"), "", true, nil)
	assert.NoError(t, err)
	assert.Equal(t, 1, len(results))
	assert.Equal(t, api.ResolvedPath("spec.template.spec.containers.0.securityContext.runAsNonRoot"), results[0].Path)
}

func TestResolveAssociativePaths_ParameterSegmentUpsert(t *testing.T) {
	// YAML fixture for testing @ parameter segments - matches the manual test deployment.yaml
	yamlFixture := `apiVersion: apps/v1
kind: Deployment
metadata:
  labels:
    app: mydep
  annotations:
    confighub.com/key: something
  name: mydep
  namespace: example
spec:
  replicas: 3
  paused: false
  template:
    spec:
      containers:
      - name: nginx
        image: nginx:1.14.2
`
	docs, err := gaby.ParseAll([]byte(yamlFixture))
	assert.NoError(t, err)

	// Test 1: Check if annotations exists first
	annotationsNode := docs[0].S("metadata", "annotations")
	assert.NotNil(t, annotationsNode, "annotations should exist initially")

	// Test 1: @ parameter segment should be resolved and upserted
	results1, err := ResolveAssociativePaths(docs[0], api.UnresolvedPath("metadata.annotations.@confighub~1com/test:annotation-key"), "", true, nil)
	assert.NoError(t, err)
	assert.Equal(t, 1, len(results1), "Should resolve one path even when annotations doesn't exist")
	assert.Equal(t, api.ResolvedPath("metadata.annotations.confighub~1com/test"), results1[0].Path)
	assert.Equal(t, 1, len(results1[0].PathArguments))
	assert.Equal(t, "annotation-key", results1[0].PathArguments[0].ParameterName)
	assert.Equal(t, "confighub.com/test", results1[0].PathArguments[0].Value)

	// Test 2: @ parameter segment without parameter name should still work
	results2, err := ResolveAssociativePaths(docs[0], api.UnresolvedPath("metadata.annotations.@confighub~1com/simple"), "", true, nil)
	assert.NoError(t, err)
	assert.Equal(t, 1, len(results2))
	assert.Equal(t, api.ResolvedPath("metadata.annotations.confighub~1com/simple"), results2[0].Path)
	assert.Equal(t, 0, len(results2[0].PathArguments))

	// Test 3: @ parameter segment in non-upsert mode should not work when path doesn't exist
	results3, err := ResolveAssociativePaths(docs[0], api.UnresolvedPath("metadata.annotations.@confighub~1com/test:annotation-key"), "", false, nil)
	assert.NoError(t, err)
	assert.Equal(t, 0, len(results3))

	// Test 4: Verify gaby can actually set the value at the resolved path
	resolvedPath := string(results1[0].Path) // from Test 1
	_, err = docs[0].SetP("test-value", resolvedPath)
	assert.NoError(t, err)
	
	// Verify the value was set
	result := docs[0].S("metadata", "annotations", "confighub.com/test")
	assert.NotNil(t, result)
	assert.Equal(t, "test-value", result.Data())
}

func TestResolveAssociativePaths_WildcardRewriting(t *testing.T) {
	// YAML fixture with multiple containers
	yamlFixture := `apiVersion: apps/v1
kind: Deployment
metadata:
  name: multi-container-deployment
spec:
  template:
    spec:
      containers:
      - name: container-one
        image: nginx:1.14.2
      - name: container-two
        image: redis:5.0
      - name: container-three
        image: mysql:8.0
`
	docs, err := gaby.ParseAll([]byte(yamlFixture))
	assert.NoError(t, err)
	
	// Test wildcard rewriting: ?name:container-name=* should be transformed to *?name:container-name
	results, err := ResolveAssociativePaths(docs[0], api.UnresolvedPath("spec.template.spec.containers.?name:container-name=*.image"), "", false, nil)
	assert.NoError(t, err)
	assert.Equal(t, 3, len(results))
	
	// Verify all containers are matched
	assert.Equal(t, api.ResolvedPath("spec.template.spec.containers.0.image"), results[0].Path)
	assert.Equal(t, api.ResolvedPath("spec.template.spec.containers.1.image"), results[1].Path)
	assert.Equal(t, api.ResolvedPath("spec.template.spec.containers.2.image"), results[2].Path)
	
	// Verify parameter arguments are correctly captured
	assert.Equal(t, 1, len(results[0].PathArguments))
	assert.Equal(t, "container-name", results[0].PathArguments[0].ParameterName)
	assert.Equal(t, "container-one", results[0].PathArguments[0].Value)
	
	assert.Equal(t, 1, len(results[1].PathArguments))
	assert.Equal(t, "container-name", results[1].PathArguments[0].ParameterName)
	assert.Equal(t, "container-two", results[1].PathArguments[0].Value)
	
	assert.Equal(t, 1, len(results[2].PathArguments))
	assert.Equal(t, "container-name", results[2].PathArguments[0].ParameterName)
	assert.Equal(t, "container-three", results[2].PathArguments[0].Value)
}

func TestResolveAssociativePaths_AssociativeWithIndex(t *testing.T) {
	yamlFixture := `apiVersion: apps/v1
kind: Deployment
metadata:
  name: test
spec:
  template:
    spec:
      containers:
      - name: nginx
        image: nginx:1.19
      - name: sidecar
        image: sidecar:v1
`
	docs, err := gaby.ParseAll([]byte(yamlFixture))
	assert.NoError(t, err)

	// Test 1: ?key=value;@index resolves by key match
	results, err := ResolveAssociativePaths(docs[0], api.UnresolvedPath("spec.template.spec.containers.?name=sidecar;@1.image"), "", false, nil)
	assert.NoError(t, err)
	assert.Equal(t, 1, len(results))
	assert.Equal(t, api.ResolvedPath("spec.template.spec.containers.1.image"), results[0].Path)

	// Test 2: ?key=value;@index where key matches a different index than the fallback
	results, err = ResolveAssociativePaths(docs[0], api.UnresolvedPath("spec.template.spec.containers.?name=nginx;@1.image"), "", false, nil)
	assert.NoError(t, err)
	assert.Equal(t, 1, len(results))
	// Should match by key (index 0), not by fallback index (1)
	assert.Equal(t, api.ResolvedPath("spec.template.spec.containers.0.image"), results[0].Path)

	// Test 3: ?key=value;@index where key doesn't match but index does (fallback)
	results, err = ResolveAssociativePaths(docs[0], api.UnresolvedPath("spec.template.spec.containers.?name=missing;@0.image"), "", false, nil)
	assert.NoError(t, err)
	assert.Equal(t, 1, len(results))
	// Falls back to positional index 0
	assert.Equal(t, api.ResolvedPath("spec.template.spec.containers.0.image"), results[0].Path)

	// Test 4: ?key=value;@index where neither matches
	results, err = ResolveAssociativePaths(docs[0], api.UnresolvedPath("spec.template.spec.containers.?name=missing;@5.image"), "", false, nil)
	assert.NoError(t, err)
	assert.Equal(t, 0, len(results))

	// Test 5: Nested ?key=value;@index
	yamlFixtureWithEnv := `apiVersion: apps/v1
kind: Deployment
metadata:
  name: test
spec:
  template:
    spec:
      containers:
      - name: nginx
        image: nginx:1.19
        env:
        - name: FOO
          value: bar
        - name: BAZ
          value: qux
`
	docs2, err := gaby.ParseAll([]byte(yamlFixtureWithEnv))
	assert.NoError(t, err)

	results, err = ResolveAssociativePaths(docs2[0], api.UnresolvedPath("spec.template.spec.containers.?name=nginx;@0.env.?name=BAZ;@1.value"), "", false, nil)
	assert.NoError(t, err)
	assert.Equal(t, 1, len(results))
	assert.Equal(t, api.ResolvedPath("spec.template.spec.containers.0.env.1.value"), results[0].Path)
}

func TestResolveAssociativePaths_WithAccessor(t *testing.T) {
	yamlFixture := `args:
  - "--port=8080"
  - "--debug"
  - "--host=localhost"
`
	docs, err := gaby.ParseAll([]byte(yamlFixture))
	assert.NoError(t, err)

	accessor, err := newRegexpAccessor(`^--(?P<flag>[a-zA-Z0-9][-a-zA-Z0-9]*)=(?P<value>.+)$`)
	assert.NoError(t, err)

	// Match by flag name
	results, err := ResolveAssociativePaths(docs[0], api.UnresolvedPath("args.?flag=port"), "", false, accessor)
	assert.NoError(t, err)
	assert.Equal(t, 1, len(results))
	assert.Equal(t, api.ResolvedPath("args.0"), results[0].Path)

	// Match by flag name with parameter binding
	results, err = ResolveAssociativePaths(docs[0], api.UnresolvedPath("args.?flag:container-flag=host"), "", false, accessor)
	assert.NoError(t, err)
	assert.Equal(t, 1, len(results))
	assert.Equal(t, api.ResolvedPath("args.2"), results[0].Path)
	assert.Equal(t, 1, len(results[0].PathArguments))
	assert.Equal(t, "container-flag", results[0].PathArguments[0].ParameterName)
	assert.Equal(t, "host", results[0].PathArguments[0].Value)

	// Non-matching element (--debug has no =value)
	results, err = ResolveAssociativePaths(docs[0], api.UnresolvedPath("args.?flag=debug"), "", false, accessor)
	assert.NoError(t, err)
	assert.Equal(t, 0, len(results))

	// Non-existent flag
	results, err = ResolveAssociativePaths(docs[0], api.UnresolvedPath("args.?flag=nonexistent"), "", false, accessor)
	assert.NoError(t, err)
	assert.Equal(t, 0, len(results))

	// Wildcard with accessor - iterates all elements, but only extracts flag for matching ones
	results, err = ResolveAssociativePaths(docs[0], api.UnresolvedPath("args.*?flag:container-flag"), "", false, accessor)
	assert.NoError(t, err)
	// All 3 array elements are visited, but only 2 have extractable flag arguments
	assert.Equal(t, 3, len(results))
	flagArgs := 0
	for _, r := range results {
		if len(r.PathArguments) > 0 {
			flagArgs++
		}
	}
	assert.Equal(t, 2, flagArgs)
}

func TestResolveAssociativeSegments(t *testing.T) {
	yamlFixture := `spec:
  containers:
  - name: nginx
    image: nginx:1.19
  - name: sidecar
    image: sidecar:v1
  legacy:
  - image: legacy:v1
  - image: legacy:v2
`
	docs, err := gaby.ParseAll([]byte(yamlFixture))
	assert.NoError(t, err)

	tests := []struct {
		name             string
		path             string
		expectedPath     string
		expectedResolved bool
	}{
		{
			name:             "no associative segments",
			path:             "spec.containers.0.image",
			expectedPath:     "spec.containers.0.image",
			expectedResolved: true,
		},
		{
			name:             "?key=value;@index resolved by key",
			path:             "spec.containers.?name=sidecar;@1.image",
			expectedPath:     "spec.containers.1.image",
			expectedResolved: true,
		},
		{
			name:             "?key=value;@index key matches different index",
			path:             "spec.containers.?name=nginx;@1.image",
			expectedPath:     "spec.containers.0.image",
			expectedResolved: true,
		},
		{
			name:             "?key=value;@index does not fall back when fallback element has different merge-key value",
			path:             "spec.containers.?name=missing;@0.image",
			expectedPath:     "spec.containers.?name=missing;@0.image",
			expectedResolved: false,
		},
		{
			name:             "?key=value;@index falls back to legacy element without merge-key field",
			path:             "spec.legacy.?name=anything;@0.image",
			expectedPath:     "spec.legacy.0.image",
			expectedResolved: true,
		},
		{
			name:             "?key=value without index",
			path:             "spec.containers.?name=nginx.image",
			expectedPath:     "spec.containers.0.image",
			expectedResolved: true,
		},
		{
			name:             "?key=value no match no fallback",
			path:             "spec.containers.?name=missing.image",
			expectedPath:     "spec.containers.?name=missing.image",
			expectedResolved: false,
		},
		{
			name:             "?key=value;@index out of bounds falls back (append semantics)",
			path:             "spec.containers.?name=missing;@99.image",
			expectedPath:     "spec.containers.99.image",
			expectedResolved: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, resolved := ResolveAssociativeSegments(docs[0], tt.path)
			assert.Equal(t, tt.expectedPath, result)
			assert.Equal(t, tt.expectedResolved, resolved)
		})
	}
}

func TestStripAssociativeSegments(t *testing.T) {
	tests := []struct {
		name     string
		path     string
		expected string
	}{
		{
			name:     "no associative segments",
			path:     "spec.containers.0.image",
			expected: "spec.containers.0.image",
		},
		{
			name:     "?key=value;@index strips to index",
			path:     "spec.containers.?name=nginx;@0.image",
			expected: "spec.containers.0.image",
		},
		{
			name:     "?key=@index strips to index",
			path:     "spec.containers.?name=@1.image",
			expected: "spec.containers.1.image",
		},
		{
			name:     "multiple associative segments",
			path:     "spec.containers.?name=nginx;@0.env.?name=FOO;@2.value",
			expected: "spec.containers.0.env.2.value",
		},
		{
			name:     "mixed segments",
			path:     "spec.template.spec.containers.?name=nginx;@0.resources.limits.cpu",
			expected: "spec.template.spec.containers.0.resources.limits.cpu",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := StripAssociativeSegments(tt.path)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestAssociativePathSegment(t *testing.T) {
	tests := []struct {
		name          string
		mergeKey      string
		mergeKeyValue string
		index         int
		expected      string
	}{
		{
			name:          "simple key and value",
			mergeKey:      "name",
			mergeKeyValue: "nginx",
			index:         0,
			expected:      "?name=nginx;@0",
		},
		{
			name:          "value with dots gets escaped",
			mergeKey:      "name",
			mergeKeyValue: "my.container",
			index:         1,
			expected:      "?name=my~1container;@1",
		},
		{
			name:          "key with dots gets escaped",
			mergeKey:      "container.port",
			mergeKeyValue: "8080",
			index:         2,
			expected:      "?container~1port=8080;@2",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := AssociativePathSegment(tt.mergeKey, tt.mergeKeyValue, tt.index)
			assert.Equal(t, tt.expected, result)
		})
	}
}
