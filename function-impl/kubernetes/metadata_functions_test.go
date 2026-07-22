// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package kubernetes

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/confighub/sdk/core/function/api"
	"github.com/confighub/sdk/core/third_party/gaby"
)

func TestK8sFnSetNamespace(t *testing.T) {
	cases := []struct {
		name     string
		input    string
		args     []string
		expected string
	}{
		{
			name: "Namespace resource gets renamed",
			input: `
apiVersion: v1
kind: Namespace
metadata:
  name: old-ns
`,
			args: []string{"new-ns"},
			expected: `apiVersion: v1
kind: Namespace
metadata:
  name: new-ns
`,
		},
		{
			name: "Deployment without metadata.namespace gets it added; cluster-scoped untouched",
			input: `
apiVersion: apps/v1
kind: Deployment
metadata:
  name: web
spec:
  template:
    spec:
      containers:
      - name: main
        image: nginx
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: viewer
rules:
- apiGroups: [""]
  resources: ["pods"]
  verbs: ["get"]
`,
			args: []string{"new-ns"},
			expected: `apiVersion: apps/v1
kind: Deployment
metadata:
  name: web
  namespace: new-ns
spec:
  template:
    spec:
      containers:
        - name: main
          image: nginx
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: viewer
rules:
  - apiGroups: [""]
    resources: ["pods"]
    verbs: ["get"]
`,
		},
		{
			name: "RoleBinding: SA subjects get namespace upserted; Group/User left alone",
			input: `
apiVersion: rbac.authorization.k8s.io/v1
kind: RoleBinding
metadata:
  name: rb
subjects:
- kind: ServiceAccount
  name: sa1
- kind: Group
  name: admins
- kind: ServiceAccount
  name: sa2
  namespace: existing
- kind: User
  name: alice
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: Role
  name: r
`,
			args: []string{"new-ns"},
			expected: `apiVersion: rbac.authorization.k8s.io/v1
kind: RoleBinding
metadata:
  name: rb
  namespace: new-ns
subjects:
  - kind: ServiceAccount
    name: sa1
    namespace: new-ns
  - kind: Group
    name: admins
  - kind: ServiceAccount
    name: sa2
    namespace: new-ns
  - kind: User
    name: alice
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: Role
  name: r
`,
		},
		{
			name: "ClusterRoleBinding: SA subjects get namespace upserted; CRB itself stays cluster-scoped",
			input: `
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRoleBinding
metadata:
  name: crb
subjects:
- kind: ServiceAccount
  name: sa1
- kind: Group
  name: admins
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: ClusterRole
  name: cr
`,
			args: []string{"new-ns"},
			expected: `apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRoleBinding
metadata:
  name: crb
subjects:
  - kind: ServiceAccount
    name: sa1
    namespace: new-ns
  - kind: Group
    name: admins
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: ClusterRole
  name: cr
`,
		},
		{
			name: "CRD with conversion webhook gets namespace rewritten",
			input: `
apiVersion: apiextensions.k8s.io/v1
kind: CustomResourceDefinition
metadata:
  name: widgets.example.com
spec:
  group: example.com
  names:
    kind: Widget
    plural: widgets
  scope: Namespaced
  conversion:
    strategy: Webhook
    webhook:
      clientConfig:
        service:
          name: conv
          namespace: old-ns
          path: /convert
  versions:
  - name: v1
    served: true
    storage: true
`,
			args: []string{"new-ns"},
			expected: `apiVersion: apiextensions.k8s.io/v1
kind: CustomResourceDefinition
metadata:
  name: widgets.example.com
spec:
  group: example.com
  names:
    kind: Widget
    plural: widgets
  scope: Namespaced
  conversion:
    strategy: Webhook
    webhook:
      clientConfig:
        service:
          name: conv
          namespace: new-ns
          path: /convert
  versions:
    - name: v1
      served: true
      storage: true
`,
		},
		{
			name: "CRD without spec.conversion is left untouched (|namespace gates upsert)",
			input: `
apiVersion: apiextensions.k8s.io/v1
kind: CustomResourceDefinition
metadata:
  name: widgets.example.com
spec:
  group: example.com
  names:
    kind: Widget
    plural: widgets
  scope: Namespaced
  versions:
  - name: v1
    served: true
    storage: true
`,
			args: []string{"new-ns"},
			expected: `apiVersion: apiextensions.k8s.io/v1
kind: CustomResourceDefinition
metadata:
  name: widgets.example.com
spec:
  group: example.com
  names:
    kind: Widget
    plural: widgets
  scope: Namespaced
  versions:
    - name: v1
      served: true
      storage: true
`,
		},
		{
			name: "MutatingWebhookConfiguration webhooks[].clientConfig.service.namespace rewritten",
			input: `
apiVersion: admissionregistration.k8s.io/v1
kind: MutatingWebhookConfiguration
metadata:
  name: mwc
webhooks:
- name: a.example.com
  clientConfig:
    service:
      name: svc
      namespace: old-ns
      path: /mutate
  rules: []
  sideEffects: None
  admissionReviewVersions: ["v1"]
`,
			args: []string{"new-ns"},
			expected: `apiVersion: admissionregistration.k8s.io/v1
kind: MutatingWebhookConfiguration
metadata:
  name: mwc
webhooks:
  - name: a.example.com
    clientConfig:
      service:
        name: svc
        namespace: new-ns
        path: /mutate
    rules: []
    sideEffects: None
    admissionReviewVersions: ["v1"]
`,
		},
		{
			name: "Pod-spec command/args/env DNS rewritten when matching old namespace",
			input: `
apiVersion: apps/v1
kind: Deployment
metadata:
  name: web
  namespace: old-ns
spec:
  template:
    spec:
      containers:
      - name: main
        image: nginx
        command: ["curl", "http://db.old-ns.svc.cluster.local/health"]
        args: ["--remote", "cache.old-ns.svc:6379", "--other", "thing.kube-system.svc.cluster.local"]
        env:
        - name: URL
          value: "redis://cache.old-ns.svc:6379"
        - name: LITERAL
          value: "no-svc-here"
`,
			args: []string{"new-ns"},
			expected: `apiVersion: apps/v1
kind: Deployment
metadata:
  name: web
  namespace: new-ns
spec:
  template:
    spec:
      containers:
        - name: main
          image: nginx
          command:
            - curl
            - http://db.new-ns.svc.cluster.local/health
          args:
            - --remote
            - cache.new-ns.svc:6379
            - --other
            - thing.kube-system.svc.cluster.local
          env:
            - name: URL
              value: redis://cache.new-ns.svc:6379
            - name: LITERAL
              value: no-svc-here
`,
		},
		{
			name: "Explicit old-namespace argument is used for DNS rewrite even when metadata.namespace differs",
			input: `
apiVersion: apps/v1
kind: Deployment
metadata:
  name: web
spec:
  template:
    spec:
      containers:
      - name: main
        image: nginx
        env:
        - name: URL
          value: "redis://cache.elsewhere.svc:6379"
`,
			args: []string{"new-ns", "elsewhere"},
			expected: `apiVersion: apps/v1
kind: Deployment
metadata:
  name: web
  namespace: new-ns
spec:
  template:
    spec:
      containers:
        - name: main
          image: nginx
          env:
            - name: URL
              value: redis://cache.new-ns.svc:6379
`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			parsed, err := gaby.ParseAll([]byte(tc.input))
			require.NoError(t, err)

			args := []api.FunctionArgument{{ParameterName: "namespace-name", Value: tc.args[0]}}
			if len(tc.args) > 1 {
				args = append(args, api.FunctionArgument{ParameterName: "old-namespace", Value: tc.args[1]})
			}
			out, _, err := k8sFnSetNamespace(testResourceProvider, nil, parsed, args)
			require.NoError(t, err)
			assert.YAMLEq(t, tc.expected, out.String())
		})
	}
}

func TestK8sFnSetWorkloadLabels(t *testing.T) {
	cases := []struct {
		name     string
		input    string
		args     []string
		expected string
	}{
		{
			name: "Deployment updates pod template labels and selector matchLabels",
			input: `
apiVersion: apps/v1
kind: Deployment
metadata:
  name: web
spec:
  selector:
    matchLabels:
      app: web
  template:
    metadata:
      labels:
        app: web
    spec:
      containers:
      - name: main
        image: nginx
`,
			args: []string{"app=frontend", "tier=ui"},
			expected: `apiVersion: apps/v1
kind: Deployment
metadata:
  name: web
spec:
  selector:
    matchLabels:
      app: frontend
      tier: ui
  template:
    metadata:
      labels:
        app: frontend
        tier: ui
    spec:
      containers:
        - name: main
          image: nginx
`,
		},
		{
			name: "CronJob updates jobTemplate selector and pod-template labels",
			input: `
apiVersion: batch/v1
kind: CronJob
metadata:
  name: report
spec:
  schedule: "*/5 * * * *"
  jobTemplate:
    spec:
      selector:
        matchLabels:
          app: report
      template:
        metadata:
          labels:
            app: report
        spec:
          containers:
          - name: main
            image: busybox
`,
			args: []string{"app=billing"},
			expected: `apiVersion: batch/v1
kind: CronJob
metadata:
  name: report
spec:
  schedule: "*/5 * * * *"
  jobTemplate:
    spec:
      selector:
        matchLabels:
          app: billing
      template:
        metadata:
          labels:
            app: billing
        spec:
          containers:
            - name: main
              image: busybox
`,
		},
		{
			name: "Service uses flat spec.selector (not matchLabels)",
			input: `
apiVersion: v1
kind: Service
metadata:
  name: web
spec:
  selector:
    app: web
  ports:
  - port: 80
`,
			args: []string{"app=frontend", "tier=ui"},
			expected: `apiVersion: v1
kind: Service
metadata:
  name: web
spec:
  selector:
    app: frontend
    tier: ui
  ports:
    - port: 80
`,
		},
		{
			name: "PodDisruptionBudget updates spec.selector.matchLabels",
			input: `
apiVersion: policy/v1
kind: PodDisruptionBudget
metadata:
  name: web
spec:
  minAvailable: 1
  selector:
    matchLabels:
      app: web
`,
			args: []string{"app=frontend"},
			expected: `apiVersion: policy/v1
kind: PodDisruptionBudget
metadata:
  name: web
spec:
  minAvailable: 1
  selector:
    matchLabels:
      app: frontend
`,
		},
		{
			name: "NetworkPolicy updates spec.podSelector but leaves ingress/egress alone",
			input: `
apiVersion: networking.k8s.io/v1
kind: NetworkPolicy
metadata:
  name: web
spec:
  podSelector:
    matchLabels:
      app: web
  ingress:
  - from:
    - podSelector:
        matchLabels:
          app: web
  egress:
  - to:
    - podSelector:
        matchLabels:
          app: web
`,
			args: []string{"app=frontend"},
			expected: `apiVersion: networking.k8s.io/v1
kind: NetworkPolicy
metadata:
  name: web
spec:
  podSelector:
    matchLabels:
      app: frontend
  ingress:
    - from:
        - podSelector:
            matchLabels:
              app: web
  egress:
    - to:
        - podSelector:
            matchLabels:
              app: web
`,
		},
		{
			name: "Pod updates only metadata.labels",
			input: `
apiVersion: v1
kind: Pod
metadata:
  name: web
  labels:
    app: web
spec:
  containers:
  - name: main
    image: nginx
`,
			args: []string{"app=frontend", "tier=ui"},
			expected: `apiVersion: v1
kind: Pod
metadata:
  name: web
  labels:
    app: frontend
    tier: ui
spec:
  containers:
    - name: main
      image: nginx
`,
		},
		{
			name: "Dotted label key written with escaping",
			input: `
apiVersion: apps/v1
kind: Deployment
metadata:
  name: web
spec:
  selector:
    matchLabels:
      app: web
  template:
    metadata:
      labels:
        app: web
    spec:
      containers:
      - name: main
        image: nginx
`,
			args: []string{"app.kubernetes.io/name=web", "app.kubernetes.io/instance=prod"},
			expected: `apiVersion: apps/v1
kind: Deployment
metadata:
  name: web
spec:
  selector:
    matchLabels:
      app: web
      app.kubernetes.io/name: web
      app.kubernetes.io/instance: prod
  template:
    metadata:
      labels:
        app: web
        app.kubernetes.io/name: web
        app.kubernetes.io/instance: prod
    spec:
      containers:
        - name: main
          image: nginx
`,
		},
		{
			name: "Empty value removes label from both template and selector",
			input: `
apiVersion: apps/v1
kind: Deployment
metadata:
  name: web
spec:
  selector:
    matchLabels:
      app: web
      tier: ui
  template:
    metadata:
      labels:
        app: web
        tier: ui
    spec:
      containers:
      - name: main
        image: nginx
`,
			args: []string{"tier="},
			expected: `apiVersion: apps/v1
kind: Deployment
metadata:
  name: web
spec:
  selector:
    matchLabels:
      app: web
  template:
    metadata:
      labels:
        app: web
    spec:
      containers:
        - name: main
          image: nginx
`,
		},
		{
			name: "Resource type without label/selector paths is untouched",
			input: `
apiVersion: v1
kind: ConfigMap
metadata:
  name: cfg
data:
  key: value
`,
			args: []string{"app=frontend"},
			expected: `apiVersion: v1
kind: ConfigMap
metadata:
  name: cfg
data:
  key: value
`,
		},
		{
			name: "PodMonitor spec.selector.matchLabels updated",
			input: `
apiVersion: monitoring.coreos.com/v1
kind: PodMonitor
metadata:
  name: web
spec:
  selector:
    matchLabels:
      app: web
  podMetricsEndpoints:
  - port: metrics
`,
			args: []string{"app=frontend"},
			expected: `apiVersion: monitoring.coreos.com/v1
kind: PodMonitor
metadata:
  name: web
spec:
  selector:
    matchLabels:
      app: frontend
  podMetricsEndpoints:
    - port: metrics
`,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			parsed, err := gaby.ParseAll([]byte(tc.input))
			require.NoError(t, err)

			out, _, err := k8sFnSetWorkloadLabels(testResourceProvider, nil, parsed, stringArgsToFunctionArgs(tc.args))
			require.NoError(t, err)

			assert.YAMLEq(t, tc.expected, out.String())
		})
	}
}

func TestK8sFnGetWorkloadLabels(t *testing.T) {
	cases := []struct {
		name string
		// each entry: resourceName -> path -> expected YAML of the labels map
		expected map[string]map[string]string
		input    string
	}{
		{
			name: "Deployment exposes template labels and selector matchLabels",
			input: `
apiVersion: apps/v1
kind: Deployment
metadata:
  name: web
spec:
  selector:
    matchLabels:
      app: web
      tier: ui
  template:
    metadata:
      labels:
        app: web
        tier: ui
    spec:
      containers:
      - name: main
        image: nginx
`,
			expected: map[string]map[string]string{
				"/web": {
					"spec.template.metadata.labels": "app: web\ntier: ui",
					"spec.selector.matchLabels":     "app: web\ntier: ui",
				},
			},
		},
		{
			name: "Service exposes flat spec.selector",
			input: `
apiVersion: v1
kind: Service
metadata:
  name: web
spec:
  selector:
    app: web
  ports:
  - port: 80
`,
			expected: map[string]map[string]string{
				"/web": {
					"spec.selector": "app: web",
				},
			},
		},
		{
			name: "CronJob exposes jobTemplate selector and pod-template labels",
			input: `
apiVersion: batch/v1
kind: CronJob
metadata:
  name: report
spec:
  schedule: "*/5 * * * *"
  jobTemplate:
    spec:
      selector:
        matchLabels:
          app: report
      template:
        metadata:
          labels:
            app: report
        spec:
          containers:
          - name: main
            image: busybox
`,
			expected: map[string]map[string]string{
				"/report": {
					"spec.jobTemplate.spec.template.metadata.labels": "app: report",
					"spec.jobTemplate.spec.selector.matchLabels":     "app: report",
				},
			},
		},
		{
			name: "NetworkPolicy exposes spec.podSelector.matchLabels but skips ingress/egress",
			input: `
apiVersion: networking.k8s.io/v1
kind: NetworkPolicy
metadata:
  name: web
spec:
  podSelector:
    matchLabels:
      app: web
  ingress:
  - from:
    - podSelector:
        matchLabels:
          app: other
`,
			expected: map[string]map[string]string{
				"/web": {
					"spec.podSelector.matchLabels": "app: web",
				},
			},
		},
		{
			name: "Pod exposes only metadata.labels",
			input: `
apiVersion: v1
kind: Pod
metadata:
  name: web
  labels:
    app: web
spec:
  containers:
  - name: main
    image: nginx
`,
			expected: map[string]map[string]string{
				"/web": {
					"metadata.labels": "app: web",
				},
			},
		},
		{
			name: "Resource type without label/selector paths returns nothing",
			input: `
apiVersion: v1
kind: ConfigMap
metadata:
  name: cfg
data:
  key: value
`,
			expected: map[string]map[string]string{},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			parsed, err := gaby.ParseAll([]byte(tc.input))
			require.NoError(t, err)

			_, raw, err := k8sFnGetWorkloadLabels(testResourceProvider, nil, parsed)
			require.NoError(t, err)

			// k8sFnGetWorkloadLabels returns an api.AttributeValueList.
			values, ok := raw.(api.AttributeValueList)
			require.True(t, ok, "expected AttributeValueList, got %T", raw)

			// Build a lookup map of what we got.
			got := map[string]map[string]string{}
			for _, v := range values {
				rn := string(v.ResourceName)
				p := string(v.Path)
				if got[rn] == nil {
					got[rn] = map[string]string{}
				}
				assert.Equal(t, api.DataTypeYAML, v.DataType, "expected DataTypeYAML at %s/%s", rn, p)
				strVal, ok := v.Value.(string)
				require.True(t, ok, "expected string value, got %T at %s/%s", v.Value, rn, p)
				got[rn][p] = strVal
			}

			// Each expected entry must be present.
			for rn, paths := range tc.expected {
				require.Contains(t, got, rn, "expected resource %s in output", rn)
				for path, expectedYAML := range paths {
					require.Contains(t, got[rn], path, "expected path %s on %s", path, rn)
					assert.YAMLEq(t, expectedYAML, got[rn][path],
						"map at %s/%s did not match expected YAML", rn, path)
				}
			}

			// No unexpected entries should appear.
			for rn, paths := range got {
				require.Contains(t, tc.expected, rn, "unexpected resource %s in output", rn)
				for path := range paths {
					require.Contains(t, tc.expected[rn], path, "unexpected path %s on %s", path, rn)
				}
			}
		})
	}
}

func TestK8sFnSetWorkloadLabels_InvalidArgs(t *testing.T) {
	parsed, err := gaby.ParseAll([]byte(`
apiVersion: apps/v1
kind: Deployment
metadata:
  name: web
spec:
  selector:
    matchLabels:
      app: web
  template:
    metadata:
      labels:
        app: web
    spec:
      containers:
      - name: main
        image: nginx
`))
	require.NoError(t, err)

	// No '=' sign in the pair, and missing key — both invalid, no valid pairs remain.
	_, _, err = k8sFnSetWorkloadLabels(testResourceProvider, nil, parsed,
		stringArgsToFunctionArgs([]string{"noequalssign", "=novalue"}))
	require.Error(t, err)
}

// Crossplane managed resources are cluster-scoped, but there are thousands of them across a
// provider family, so they are recognized by the group-suffix rule rather than enumerated. Before
// that rule existed, set-namespace wrote metadata.namespace into every one of them.
func TestK8sFnSetNamespace_CrossplaneClusterScoped(t *testing.T) {
	input := `
apiVersion: eks.aws.upbound.io/v1beta2
kind: Cluster
metadata:
  name: prod
spec:
  forProvider:
    region: us-east-1
---
apiVersion: ec2.aws.upbound.io/v1beta1
kind: Subnet
metadata:
  name: prod-private-a
spec:
  forProvider:
    region: us-east-1
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: web
spec:
  template:
    spec:
      containers:
        - name: main
          image: nginx
`
	parsed, err := gaby.ParseAll([]byte(input))
	require.NoError(t, err)

	out, _, err := k8sFnSetNamespace(testResourceProvider, nil,
		parsed, []api.FunctionArgument{{ParameterName: "namespace-name", Value: "new-ns"}})
	require.NoError(t, err)

	got := out.String()
	// The workload is namespaced and must be updated.
	assert.Contains(t, got, "namespace: new-ns")
	// Exactly one resource may carry a namespace: the Deployment.
	assert.Equal(t, 1, strings.Count(got, "namespace: new-ns"),
		"a namespace was written into a cluster-scoped Crossplane managed resource:\n%s", got)
}

// The Crossplane v2 namespaced variants carry a ".m." infix and ARE namespaced, so the rule must
// not sweep them up along with their cluster-scoped twins.
func TestK8sFnSetNamespace_CrossplaneNamespacedVariant(t *testing.T) {
	input := `
apiVersion: eks.aws.m.upbound.io/v1beta1
kind: Cluster
metadata:
  name: prod
spec:
  forProvider:
    region: us-east-1
`
	parsed, err := gaby.ParseAll([]byte(input))
	require.NoError(t, err)

	out, _, err := k8sFnSetNamespace(testResourceProvider, nil,
		parsed, []api.FunctionArgument{{ParameterName: "namespace-name", Value: "new-ns"}})
	require.NoError(t, err)
	assert.Contains(t, out.String(), "namespace: new-ns",
		"the namespaced (.m.) Crossplane variant should have received a namespace")
}

// cluster-scoped-types is the escape hatch for CRDs no rule covers — user-defined composite
// resources, whose API groups are arbitrary.
func TestK8sFnSetNamespace_ClusterScopedTypesParameter(t *testing.T) {
	input := `
apiVersion: platform.acme.io/v1alpha1
kind: XCluster
metadata:
  name: xc
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: web
spec:
  template:
    spec:
      containers:
        - name: main
          image: nginx
`
	parsed, err := gaby.ParseAll([]byte(input))
	require.NoError(t, err)

	// Without the parameter the composite resource looks namespaced.
	out, _, err := k8sFnSetNamespace(testResourceProvider, nil,
		parsed, []api.FunctionArgument{{ParameterName: "namespace-name", Value: "new-ns"}})
	require.NoError(t, err)
	assert.Equal(t, 2, strings.Count(out.String(), "namespace: new-ns"))

	// With it, the composite resource is left alone.
	parsed, err = gaby.ParseAll([]byte(input))
	require.NoError(t, err)
	out, _, err = k8sFnSetNamespace(testResourceProvider, nil, parsed, []api.FunctionArgument{
		{ParameterName: "namespace-name", Value: "new-ns"},
		{ParameterName: "cluster-scoped-types", Value: "platform.acme.io/v1alpha1/XCluster"},
	})
	require.NoError(t, err)
	got := out.String()
	assert.Equal(t, 1, strings.Count(got, "namespace: new-ns"),
		"cluster-scoped-types did not exclude the composite resource:\n%s", got)
	assert.Contains(t, got, "kind: XCluster")
}
