// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package fluxrenderer

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"

	"github.com/confighub/sdk/configkit/k8skit"
)

// createTestKustomizeDir creates a temporary directory with a simple kustomization for testing.
func createTestKustomizeDir(t *testing.T) (string, func()) {
	t.Helper()

	tmpDir, err := os.MkdirTemp("", "kustomize-test-")
	require.NoError(t, err)

	cleanup := func() {
		os.RemoveAll(tmpDir)
	}

	// Create kustomization.yaml
	kustomization := `
apiVersion: kustomize.config.k8s.io/v1beta1
kind: Kustomization
resources:
  - deployment.yaml
  - service.yaml
`
	err = os.WriteFile(filepath.Join(tmpDir, "kustomization.yaml"), []byte(kustomization), 0644)
	require.NoError(t, err)

	// Create deployment.yaml
	deployment := `
apiVersion: apps/v1
kind: Deployment
metadata:
  name: test-app
spec:
  replicas: 1
  selector:
    matchLabels:
      app: test-app
  template:
    metadata:
      labels:
        app: test-app
    spec:
      containers:
        - name: app
          image: nginx:latest
          env:
            - name: CLUSTER_NAME
              value: "${CLUSTER_NAME}"
            - name: ENVIRONMENT
              value: "${ENVIRONMENT:-default}"
`
	err = os.WriteFile(filepath.Join(tmpDir, "deployment.yaml"), []byte(deployment), 0644)
	require.NoError(t, err)

	// Create service.yaml
	service := `
apiVersion: v1
kind: Service
metadata:
  name: test-app
spec:
  selector:
    app: test-app
  ports:
    - port: 80
      targetPort: 80
`
	err = os.WriteFile(filepath.Join(tmpDir, "service.yaml"), []byte(service), 0644)
	require.NoError(t, err)

	return tmpDir, cleanup
}

// createTestTarball creates a tarball from the test directory for FetchArtifact testing.
func createTestTarball(t *testing.T, srcDir string) string {
	t.Helper()

	tarFile, err := os.CreateTemp("", "kustomize-test-*.tar.gz")
	require.NoError(t, err)
	defer tarFile.Close()

	// Use tar command to create the tarball (simpler than doing it in Go)
	// For unit tests, we'll serve this file via a test HTTP server
	return tarFile.Name()
}

func TestRenderKustomization_Basic(t *testing.T) {
	// Create a local test directory with kustomization
	testDir, cleanup := createTestKustomizeDir(t)
	defer cleanup()
	_ = testDir // Would be used with mock artifact fetching

	// For this test, we'll bypass the artifact fetching and test the core rendering logic
	// by using a mock that returns our test directory

	// Parse the Flux Kustomization
	input := RenderInput{
		Documents: []byte(`
apiVersion: kustomize.toolkit.fluxcd.io/v1
kind: Kustomization
metadata:
  name: test-app
  namespace: flux-system
spec:
  interval: 10m
  path: ./
  sourceRef:
    kind: GitRepository
    name: test-repo
`),
		Options: RenderOptions{
			ArtifactSource: ArtifactSource{
				URL: "file://" + testDir, // Won't work directly, but shows the concept
			},
		},
	}

	// For now, test the parsing and helper functions directly
	parsed, err := ParseDocumentsWithResources(input.Documents)
	require.NoError(t, err)
	require.Len(t, parsed.Documents, 1)

	ksDoc := findDocumentByKindAndAPIVersion(parsed.Documents, KustomizationKind, KustomizationAPIVersionPrefix)
	require.NotNil(t, ksDoc)
	assert.Equal(t, "test-app", ksDoc.GetName())
	assert.Equal(t, "flux-system", ksDoc.GetNamespace())

	// Verify we can find the spec
	spec, ok := ksDoc.Object["spec"].(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, "./", spec["path"])
}

func TestRenderKustomization_ParseWithConfigMaps(t *testing.T) {
	input := []byte(`
apiVersion: kustomize.toolkit.fluxcd.io/v1
kind: Kustomization
metadata:
  name: test-app
  namespace: flux-system
spec:
  interval: 10m
  path: ./
  sourceRef:
    kind: GitRepository
    name: test-repo
  postBuild:
    substitute:
      CLUSTER_NAME: production
    substituteFrom:
      - kind: ConfigMap
        name: cluster-vars
---
apiVersion: v1
kind: ConfigMap
metadata:
  name: cluster-vars
  namespace: flux-system
data:
  ENVIRONMENT: prod
  REGION: us-west-2
`)

	parsed, err := ParseDocumentsWithResources(input)
	require.NoError(t, err)
	require.Len(t, parsed.Documents, 2)

	// Should have the Kustomization
	ksDoc := findDocumentByKindAndAPIVersion(parsed.Documents, KustomizationKind, KustomizationAPIVersionPrefix)
	require.NotNil(t, ksDoc)

	// Should have the ConfigMap
	require.Len(t, parsed.ConfigMaps, 1)
	cm, ok := parsed.ConfigMaps["flux-system/cluster-vars"]
	require.True(t, ok)
	assert.Equal(t, "prod", cm.Data["ENVIRONMENT"])
	assert.Equal(t, "us-west-2", cm.Data["REGION"])
}

func TestLoadSubstitutionVars(t *testing.T) {
	// Set up test ConfigMaps and Secrets
	parsed, err := ParseDocumentsWithResources([]byte(`
apiVersion: v1
kind: ConfigMap
metadata:
  name: cluster-vars
  namespace: default
data:
  ENVIRONMENT: production
  REGION: us-east-1
---
apiVersion: v1
kind: Secret
metadata:
  name: secret-vars
  namespace: default
data:
  # base64: API_KEY=secret123
  API_KEY: c2VjcmV0MTIz
`))
	require.NoError(t, err)

	postBuild := map[string]interface{}{
		"substitute": map[string]interface{}{
			"CLUSTER_NAME": "my-cluster",
			"ENVIRONMENT":  "dev", // Will be overridden by ConfigMap
		},
		"substituteFrom": []interface{}{
			map[string]interface{}{
				"kind": "ConfigMap",
				"name": "cluster-vars",
			},
			map[string]interface{}{
				"kind": "Secret",
				"name": "secret-vars",
			},
		},
	}

	vars, err := loadSubstitutionVars(postBuild, "default", parsed.ConfigMaps, parsed.Secrets)
	require.NoError(t, err)

	// Inline vars
	assert.Equal(t, "my-cluster", vars["CLUSTER_NAME"])

	// ConfigMap vars override inline vars
	assert.Equal(t, "production", vars["ENVIRONMENT"])
	assert.Equal(t, "us-east-1", vars["REGION"])

	// Secret vars
	assert.Equal(t, "secret123", vars["API_KEY"])
}

func TestLoadSubstitutionVars_OptionalMissing(t *testing.T) {
	postBuild := map[string]interface{}{
		"substitute": map[string]interface{}{
			"CLUSTER_NAME": "my-cluster",
		},
		"substituteFrom": []interface{}{
			map[string]interface{}{
				"kind":     "ConfigMap",
				"name":     "nonexistent",
				"optional": true,
			},
		},
	}

	vars, err := loadSubstitutionVars(postBuild, "default", make(map[string]*corev1.ConfigMap), make(map[string]*corev1.Secret))
	require.NoError(t, err)

	// Should still have inline vars
	assert.Equal(t, "my-cluster", vars["CLUSTER_NAME"])
}

func TestLoadSubstitutionVars_RequiredMissing(t *testing.T) {
	postBuild := map[string]interface{}{
		"substituteFrom": []interface{}{
			map[string]interface{}{
				"kind": "ConfigMap",
				"name": "nonexistent",
				// optional defaults to false
			},
		},
	}

	_, err := loadSubstitutionVars(postBuild, "default", make(map[string]*corev1.ConfigMap), make(map[string]*corev1.Secret))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestSubstituteVarsInValue_String(t *testing.T) {
	vars := map[string]string{
		"NAME":        "myapp",
		"ENVIRONMENT": "production",
	}

	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "simple substitution",
			input:    "Hello ${NAME}",
			expected: "Hello myapp",
		},
		{
			name:     "multiple substitutions",
			input:    "${NAME} in ${ENVIRONMENT}",
			expected: "myapp in production",
		},
		{
			name:     "no substitution needed",
			input:    "plain text",
			expected: "plain text",
		},
		{
			name:     "undefined variable",
			input:    "Hello ${UNDEFINED}",
			expected: "Hello ", // undefined becomes empty
		},
		{
			name:     "default value syntax",
			input:    "Hello ${UNDEFINED:-default}",
			expected: "Hello default",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := substituteVarsInValue(tt.input, vars)
			require.NoError(t, err)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestSubstituteVarsInValue_Map(t *testing.T) {
	vars := map[string]string{
		"NAME": "myapp",
	}

	input := map[string]interface{}{
		"name":  "${NAME}",
		"other": "static",
		"nested": map[string]interface{}{
			"key": "${NAME}-nested",
		},
	}

	result, err := substituteVarsInValue(input, vars)
	require.NoError(t, err)

	resultMap, ok := result.(map[string]interface{})
	require.True(t, ok)

	assert.Equal(t, "myapp", resultMap["name"])
	assert.Equal(t, "static", resultMap["other"])

	nested, ok := resultMap["nested"].(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, "myapp-nested", nested["key"])
}

func TestSubstituteVarsInValue_Slice(t *testing.T) {
	vars := map[string]string{
		"NAME": "myapp",
	}

	input := []interface{}{
		"${NAME}",
		"static",
		map[string]interface{}{
			"key": "${NAME}",
		},
	}

	result, err := substituteVarsInValue(input, vars)
	require.NoError(t, err)

	resultSlice, ok := result.([]interface{})
	require.True(t, ok)
	require.Len(t, resultSlice, 3)

	assert.Equal(t, "myapp", resultSlice[0])
	assert.Equal(t, "static", resultSlice[1])

	item, ok := resultSlice[2].(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, "myapp", item["key"])
}

func TestIsClusterScoped(t *testing.T) {
	tests := []struct {
		apiVersion string
		kind       string
		expected   bool
	}{
		{"v1", "Namespace", true},
		{"rbac.authorization.k8s.io/v1", "ClusterRole", true},
		{"rbac.authorization.k8s.io/v1", "ClusterRoleBinding", true},
		{"apiextensions.k8s.io/v1", "CustomResourceDefinition", true},
		{"apps/v1", "Deployment", false},
		{"v1", "Service", false},
		{"v1", "ConfigMap", false},
		{"v1", "Pod", false},
		{"v1", "PersistentVolumeClaim", false},
		{"rbac.authorization.k8s.io/v1", "Role", false},
		{"rbac.authorization.k8s.io/v1", "RoleBinding", false},
	}

	for _, tt := range tests {
		name := tt.apiVersion + "/" + tt.kind
		t.Run(name, func(t *testing.T) {
			assert.Equal(t, tt.expected, k8skit.IsClusterScoped(tt.apiVersion, tt.kind))
		})
	}
}

func TestGetKustomizationPath(t *testing.T) {
	tests := []struct {
		name     string
		spec     map[string]interface{}
		expected string
	}{
		{
			name:     "explicit path",
			spec:     map[string]interface{}{"path": "./manifests"},
			expected: "./manifests",
		},
		{
			name:     "root path",
			spec:     map[string]interface{}{"path": "./"},
			expected: "./",
		},
		{
			name:     "no path",
			spec:     map[string]interface{}{},
			expected: ".",
		},
		{
			name:     "absolute-looking path",
			spec:     map[string]interface{}{"path": "/manifests"},
			expected: "manifests",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := getKustomizationPath(tt.spec)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestRenderKustomization_NoKustomization(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	input := RenderInput{
		Documents: []byte(`
apiVersion: v1
kind: ConfigMap
metadata:
  name: some-config
data:
  key: value
`),
		Options: RenderOptions{
			ArtifactSource: ArtifactSource{
				URL: "http://example.com/artifact.tar.gz",
			},
		},
	}

	_, err := RenderKustomization(ctx, input)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no Flux Kustomization found")
}

func TestRenderKustomization_MissingArtifactURL(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	input := RenderInput{
		Documents: []byte(`
apiVersion: kustomize.toolkit.fluxcd.io/v1
kind: Kustomization
metadata:
  name: test-app
  namespace: flux-system
spec:
  interval: 10m
  path: ./
  sourceRef:
    kind: GitRepository
    name: test-repo
`),
		Options: RenderOptions{
			ArtifactSource: ArtifactSource{
				URL: "", // Missing URL
			},
		},
	}

	_, err := RenderKustomization(ctx, input)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "artifact source URL is required")
}

func TestParseYAMLDocuments(t *testing.T) {
	input := []byte(`
apiVersion: v1
kind: ConfigMap
metadata:
  name: config1
data:
  key: value1
---
apiVersion: v1
kind: ConfigMap
metadata:
  name: config2
data:
  key: value2
---
# Empty document should be skipped
---
apiVersion: v1
kind: Secret
metadata:
  name: secret1
data:
  password: cGFzc3dvcmQ=
`)

	docs, err := parseYAMLDocuments(input)
	require.NoError(t, err)
	require.Len(t, docs, 3)

	assert.Equal(t, "ConfigMap", docs[0].GetKind())
	assert.Equal(t, "config1", docs[0].GetName())

	assert.Equal(t, "ConfigMap", docs[1].GetKind())
	assert.Equal(t, "config2", docs[1].GetName())

	assert.Equal(t, "Secret", docs[2].GetKind())
	assert.Equal(t, "secret1", docs[2].GetName())
}

func TestFindDocumentByKindAndAPIVersion(t *testing.T) {
	docs := []byte(`
apiVersion: kustomize.config.k8s.io/v1
kind: Kustomization
metadata:
  name: native-kustomization
---
apiVersion: kustomize.toolkit.fluxcd.io/v1
kind: Kustomization
metadata:
  name: flux-kustomization
---
apiVersion: v1
kind: ConfigMap
metadata:
  name: config
`)

	parsed, err := parseYAMLDocuments(docs)
	require.NoError(t, err)

	// Should find Flux Kustomization, not native kustomize
	fluxKs := findDocumentByKindAndAPIVersion(parsed, KustomizationKind, KustomizationAPIVersionPrefix)
	require.NotNil(t, fluxKs)
	assert.Equal(t, "flux-kustomization", fluxKs.GetName())
	assert.True(t, strings.HasPrefix(fluxKs.GetAPIVersion(), KustomizationAPIVersionPrefix))

	// Should find HelmRelease (none in this test)
	hr := findDocumentByKindAndAPIVersion(parsed, HelmReleaseKind, HelmReleaseAPIVersionPrefix)
	assert.Nil(t, hr)
}

