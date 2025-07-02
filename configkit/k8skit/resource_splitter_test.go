// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package k8skit

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSplitResources(t *testing.T) {
	tests := []struct {
		name                string
		renderedResources   map[string]string
		sourceName          string
		expectedCRDsOrder   []string // Expected resource names in CRDs output
		expectedResOrder    []string // Expected resource names in Resources output
		expectedCRDsContain []string // Strings that should be in CRDs output
		expectedResContain  []string // Strings that should be in Resources output
		wantErr             bool
	}{
		{
			name:       "Basic resource splitting with proper ordering",
			sourceName: "test-chart",
			renderedResources: map[string]string{
				"templates/deployment.yaml": `
apiVersion: apps/v1
kind: Deployment
metadata:
  name: my-app
  namespace: my-namespace
spec:
  replicas: 1
`,
				"templates/service.yaml": `
apiVersion: v1
kind: Service
metadata:
  name: my-service
  namespace: my-namespace
spec:
  ports:
  - port: 80
`,
				"templates/namespace.yaml": `
apiVersion: v1
kind: Namespace
metadata:
  name: my-namespace
`,
				"templates/crd.yaml": `
apiVersion: apiextensions.k8s.io/v1
kind: CustomResourceDefinition
metadata:
  name: myresources.example.com
spec:
  group: example.com
  versions:
  - name: v1
    served: true
    storage: true
  scope: Namespaced
  names:
    plural: myresources
    singular: myresource
    kind: MyResource
`,
			},
			expectedCRDsOrder: []string{"myresources.example.com"},
			expectedResOrder:  []string{"my-namespace", "my-service", "my-app"}, // Namespace first, then Service, then Deployment
			expectedCRDsContain: []string{
				"# Source: test-chart/templates/crd.yaml",
				"kind: CustomResourceDefinition",
				"name: myresources.example.com",
			},
			expectedResContain: []string{
				"# Source: test-chart/templates/namespace.yaml",
				"# Source: test-chart/templates/service.yaml",
				"# Source: test-chart/templates/deployment.yaml",
				"kind: Namespace",
				"kind: Service",
				"kind: Deployment",
			},
		},
		{
			name:       "Complex resource ordering test",
			sourceName: "complex-chart",
			renderedResources: map[string]string{
				"templates/rbac.yaml": `
apiVersion: v1
kind: ServiceAccount
metadata:
  name: my-sa
  namespace: my-namespace
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: my-cluster-role
rules:
- apiGroups: [""]
  resources: ["pods"]
  verbs: ["get", "list"]
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRoleBinding
metadata:
  name: my-cluster-role-binding
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: ClusterRole
  name: my-cluster-role
subjects:
- kind: ServiceAccount
  name: my-sa
  namespace: my-namespace
`,
				"templates/config.yaml": `
apiVersion: v1
kind: ConfigMap
metadata:
  name: my-config
  namespace: my-namespace
data:
  key: value
---
apiVersion: v1
kind: Secret
metadata:
  name: my-secret
  namespace: my-namespace
type: Opaque
data:
  password: cGFzc3dvcmQ=
`,
				"templates/workload.yaml": `
apiVersion: apps/v1
kind: StatefulSet
metadata:
  name: my-statefulset
  namespace: my-namespace
spec:
  serviceName: my-service
  replicas: 1
---
apiVersion: batch/v1
kind: Job
metadata:
  name: my-job
  namespace: my-namespace
spec:
  template:
    spec:
      containers:
      - name: job
        image: busybox
      restartPolicy: Never
`,
			},
			expectedCRDsOrder: []string{}, // No CRDs
			expectedResOrder: []string{
				"my-sa",                   // ServiceAccount
				"my-secret",               // Secret
				"my-config",               // ConfigMap
				"my-cluster-role-binding", // ClusterRoleBinding
				"my-cluster-role",         // ClusterRole
				"my-job",                  // Job
				"my-statefulset",          // StatefulSet
			},
			expectedCRDsContain: []string{}, // No CRDs
			expectedResContain: []string{
				"kind: ServiceAccount",
				"kind: ClusterRole",
				"kind: ClusterRoleBinding",
				"kind: Secret",
				"kind: ConfigMap",
				"kind: StatefulSet",
				"kind: Job",
			},
		},
		{
			name:       "CRDs from crds directory",
			sourceName: "helm-with-crds",
			renderedResources: map[string]string{
				"templates/deployment.yaml": `
apiVersion: apps/v1
kind: Deployment
metadata:
  name: my-app
  namespace: default
spec:
  replicas: 1
`,
				"crds/custom-crd.yaml": `
apiVersion: apiextensions.k8s.io/v1
kind: CustomResourceDefinition
metadata:
  name: customs.example.com
spec:
  group: example.com
  versions:
  - name: v1
    served: true
    storage: true
  scope: Namespaced
  names:
    plural: customs
    singular: custom
    kind: Custom
`,
			},
			expectedCRDsOrder:   []string{"customs.example.com"},
			expectedResOrder:    []string{"my-app"},
			expectedCRDsContain: []string{"customs.example.com"},
			expectedResContain:  []string{"my-app"},
		},
		{
			name:       "Skip empty files and NOTES.txt",
			sourceName: "chart-with-notes",
			renderedResources: map[string]string{
				"templates/NOTES.txt":    "This is a notes file that should be skipped",
				"templates/empty.yaml":   "   \n\n   ",
				"templates/_helpers.tpl": "{{ define \"chart.name\" }}my-chart{{ end }}",
				"templates/service.yaml": `
apiVersion: v1
kind: Service
metadata:
  name: my-service
  namespace: default
`,
			},
			expectedCRDsOrder:   []string{},
			expectedResOrder:    []string{"my-service"},
			expectedCRDsContain: []string{},
			expectedResContain:  []string{"my-service"},
		},
		{
			name:       "Loki Helm chart resources - real world example",
			sourceName: "loki",
			renderedResources: map[string]string{
				"templates/serviceaccount.yaml": `
apiVersion: v1
kind: ServiceAccount
metadata:
  name: loki
  namespace: loki
`,
				"templates/clusterrole.yaml": `
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: loki-clusterrole
rules:
- apiGroups: [""]
  resources: ["configmaps", "secrets"]
  verbs: ["get", "watch", "list"]
`,
				"templates/clusterrolebinding.yaml": `
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRoleBinding
metadata:
  name: loki-clusterrolebinding
subjects:
- kind: ServiceAccount
  name: loki
  namespace: loki
roleRef:
  kind: ClusterRole
  name: loki-clusterrole
  apiGroup: rbac.authorization.k8s.io
`,
				"templates/configmap.yaml": `
apiVersion: v1
kind: ConfigMap
metadata:
  name: loki-config
  namespace: loki
data:
  config.yaml: |
    auth_enabled: true
`,
				"templates/service.yaml": `
apiVersion: v1
kind: Service
metadata:
  name: loki
  namespace: loki
spec:
  ports:
  - port: 3100
`,
				"templates/deployment.yaml": `
apiVersion: apps/v1
kind: Deployment
metadata:
  name: loki-gateway
  namespace: loki
spec:
  replicas: 1
`,
				"templates/statefulset.yaml": `
apiVersion: apps/v1
kind: StatefulSet
metadata:
  name: loki
  namespace: loki
spec:
  replicas: 1
  serviceName: loki-headless
`,
				"templates/daemonset.yaml": `
apiVersion: apps/v1
kind: DaemonSet
metadata:
  name: loki-canary
  namespace: loki
spec:
  selector:
    matchLabels:
      app: loki-canary
`,
				"templates/job.yaml": `
apiVersion: batch/v1
kind: Job
metadata:
  name: loki-minio-post-job
  namespace: loki
spec:
  template:
    spec:
      restartPolicy: OnFailure
`,
				"templates/pdb.yaml": `
apiVersion: policy/v1
kind: PodDisruptionBudget
metadata:
  name: loki-pdb
  namespace: loki
spec:
  maxUnavailable: 1
`,
			},
			expectedCRDsOrder: []string{}, // No CRDs
			expectedResOrder: []string{
				"loki",                    // ServiceAccount
				"loki",                    // Service
				"loki-config",             // ConfigMap
				"loki-clusterrolebinding", // ClusterRoleBinding
				"loki-clusterrole",        // ClusterRole
				"loki-pdb",                // PodDisruptionBudget
				"loki-minio-post-job",     // Job
				"loki",                    // StatefulSet
				"loki-gateway",            // Deployment
				"loki-canary",             // DaemonSet
			},
			expectedCRDsContain: []string{},
			expectedResContain: []string{
				"kind: ServiceAccount",
				"kind: ClusterRole",
				"kind: ClusterRoleBinding",
				"kind: ConfigMap",
				"kind: Service",
				"kind: Deployment",
				"kind: StatefulSet",
				"kind: DaemonSet",
				"kind: Job",
				"kind: PodDisruptionBudget",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := SplitResources(tt.renderedResources, tt.sourceName)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.NotNil(t, result)

			// Verify CRDs output
			if len(tt.expectedCRDsOrder) > 0 {
				assert.NotEmpty(t, result.CRDs, "CRDs should not be empty")
				// Check order of resources in CRDs
				crdLines := strings.Split(result.CRDs, "\n")
				foundIndex := 0
				for _, line := range crdLines {
					if foundIndex < len(tt.expectedCRDsOrder) && strings.Contains(line, "name: "+tt.expectedCRDsOrder[foundIndex]) {
						foundIndex++
					}
				}
				assert.Equal(t, len(tt.expectedCRDsOrder), foundIndex, "All expected CRDs should be found in order")
			} else {
				assert.Empty(t, result.CRDs, "CRDs should be empty")
			}

			// Verify regular resources output
			if len(tt.expectedResOrder) > 0 {
				assert.NotEmpty(t, result.Resources, "Resources should not be empty")

				// Check order of resources - look for exact matches in metadata name fields
				resLines := strings.Split(result.Resources, "\n")
				foundIndex := 0
				for _, line := range resLines {
					trimmedLine := strings.TrimSpace(line)
					if foundIndex < len(tt.expectedResOrder) && trimmedLine == "name: "+tt.expectedResOrder[foundIndex] {
						foundIndex++
					}
				}
				assert.Equal(t, len(tt.expectedResOrder), foundIndex, "All expected resources should be found in order. Expected: %v", tt.expectedResOrder)
			}

			// Check that expected strings are contained
			for _, expected := range tt.expectedCRDsContain {
				assert.Contains(t, result.CRDs, expected, "CRDs output should contain: %s", expected)
			}
			for _, expected := range tt.expectedResContain {
				assert.Contains(t, result.Resources, expected, "Resources output should contain: %s", expected)
			}

			// Verify source comments are present
			if result.CRDs != "" {
				assert.Contains(t, result.CRDs, "# Source: "+tt.sourceName)
			}
			if result.Resources != "" {
				assert.Contains(t, result.Resources, "# Source: "+tt.sourceName)
			}
		})
	}
}

func TestSplitResources_InvalidYAML(t *testing.T) {
	renderedResources := map[string]string{
		"templates/invalid.yaml": `
this is not valid yaml
  indentation: is wrong
    - list items
  without proper structure
`,
	}

	result, err := SplitResources(renderedResources, "invalid-chart")
	assert.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "failed to parse YAML")
}

func TestSplitResources_MultipleDocumentsPerFile(t *testing.T) {
	renderedResources := map[string]string{
		"templates/multi.yaml": `
apiVersion: v1
kind: Namespace
metadata:
  name: ns1
---
apiVersion: v1
kind: Namespace
metadata:
  name: ns2
---
apiVersion: v1
kind: Service
metadata:
  name: svc1
  namespace: ns1
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: deploy1
  namespace: ns1
`,
	}

	result, err := SplitResources(renderedResources, "multi-chart")
	require.NoError(t, err)
	require.NotNil(t, result)

	// Verify all resources are present and in correct order
	assert.Contains(t, result.Resources, "ns1")
	assert.Contains(t, result.Resources, "ns2")
	assert.Contains(t, result.Resources, "svc1")
	assert.Contains(t, result.Resources, "deploy1")

	// Verify order: specific dependencies should be respected
	// ns1 should come before svc1 (since svc1 is in namespace ns1)
	// svc1 should come before deploy1 (since deploy1 may reference svc1)
	resContent := result.Resources
	ns1Index := strings.Index(resContent, "name: ns1")
	svcIndex := strings.Index(resContent, "name: svc1")
	deployIndex := strings.Index(resContent, "name: deploy1")

	// Only test the specific dependency that should be enforced:
	// ns1 should come before svc1 because svc1 is in namespace ns1
	assert.True(t, ns1Index < svcIndex, "Namespace ns1 should come before service svc1 that is in ns1")

	// Note: ns2 doesn't need to come before svc1 since svc1 is not in ns2
	// Services should generally come before deployments (priority-wise)
	assert.True(t, svcIndex < deployIndex, "Service should come before deployment")
}
