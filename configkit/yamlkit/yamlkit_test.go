// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package yamlkit

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/confighub/sdk/function/api"
	"github.com/confighub/sdk/third_party/gaby"
)

// testResourceProvider is a minimal ResourceProvider for testing purposes only.
type testResourceProvider struct{}

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

var testProvider = testResourceProvider{}

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
	results, err := ResolveAssociativePaths(docs[0], api.UnresolvedPath("spec.template.spec.containers.?name=example-container.env.?name=EXAMPLE_ENV.value"), "", false)
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
	results, err := ResolveAssociativePaths(docs[0], api.UnresolvedPath("spec.template.spec.containers.?name=container-two.image"), "", false)
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
	results, err := ResolveAssociativePaths(docs[0], api.UnresolvedPath("spec.ports.?name=http.port"), "", false)
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
	results, err := ResolveAssociativePaths(docs[0], api.UnresolvedPath("items.?name=duplicate-item.value"), "", false)
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
	results, err := ResolveAssociativePaths(docs[0], api.UnresolvedPath("data.?key=nonexistent"), "", false)
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
	results, err := ResolveAssociativePaths(c, api.UnresolvedPath("?name=container-two.image"), "", false)
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
	results, err := ResolveAssociativePaths(docs[0], api.UnresolvedPath("subjects.*.namespace"), "", false)
	assert.NoError(t, err)
	assert.Equal(t, len(results), 2)
	assert.Equal(t, api.ResolvedPath("subjects.0.namespace"), results[0].Path)
	assert.Equal(t, api.ResolvedPath("subjects.1.namespace"), results[1].Path)
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
	results, err := ResolveAssociativePaths(docs[0], api.UnresolvedPath("spec.template.spec.containers.?name:containerName=container-two.image"), "", false)
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
	results, err := ResolveAssociativePaths(docs[0], api.UnresolvedPath("spec.template.spec.containers.*?name:containerName.image"), "", false)
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
	results, err := ResolveAssociativePaths(docs[0], api.UnresolvedPath("spec.template.spec.containers.*.securityContext.runAsNonRoot"), "", true)
	assert.NoError(t, err)
	assert.Equal(t, 2, len(results))
	assert.Equal(t, api.ResolvedPath("spec.template.spec.containers.0.securityContext.runAsNonRoot"), results[0].Path)
	assert.Equal(t, api.ResolvedPath("spec.template.spec.containers.1.securityContext.runAsNonRoot"), results[1].Path)

	// Test 2: Upsert with associative match should resolve path even when target doesn't exist  
	results, err = ResolveAssociativePaths(docs[0], api.UnresolvedPath("spec.template.spec.containers.?name=nginx.securityContext.runAsNonRoot"), "", true)
	assert.NoError(t, err)
	assert.Equal(t, 1, len(results))
	assert.Equal(t, api.ResolvedPath("spec.template.spec.containers.0.securityContext.runAsNonRoot"), results[0].Path)

	// Test 3: Non-upsert mode should not resolve non-existent paths
	results, err = ResolveAssociativePaths(docs[0], api.UnresolvedPath("spec.template.spec.containers.*.securityContext.runAsNonRoot"), "", false)
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
	results, err := ResolveAssociativePaths(docs[0], api.UnresolvedPath("spec.template.spec.containers.0.|newField"), "", true)
	assert.NoError(t, err)
	assert.Equal(t, 1, len(results))
	assert.Equal(t, api.ResolvedPath("spec.template.spec.containers.0.newField"), results[0].Path)

	// Test 2: "|" syntax should also work when current segment exists
	results, err = ResolveAssociativePaths(docs[0], api.UnresolvedPath("spec.template.spec.containers.0.|securityContext"), "", true)
	assert.NoError(t, err)
	assert.Equal(t, 1, len(results))
	assert.Equal(t, api.ResolvedPath("spec.template.spec.containers.0.securityContext"), results[0].Path)

	// Test 3: "|" syntax should not resolve when preceding path doesn't exist
	results, err = ResolveAssociativePaths(docs[0], api.UnresolvedPath("spec.template.spec.nonexistent.0.|field"), "", true)
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
	results, err := ResolveAssociativePaths(docs[0], api.UnresolvedPath("spec.template.spec.containers.*.securityContext.runAsNonRoot"), "", true)
	assert.NoError(t, err)
	assert.Equal(t, 1, len(results))
	assert.Equal(t, api.ResolvedPath("spec.template.spec.containers.0.securityContext.runAsNonRoot"), results[0].Path)
}
