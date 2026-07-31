// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package k8sdescribe

import (
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// describeYAML parses a manifest and describes it, which is what the CLI does
// with each resource body returned by get-resources.
func describeYAML(t *testing.T, manifest string) []*Section {
	t.Helper()
	var doc map[string]any
	if err := yaml.Unmarshal([]byte(manifest), &doc); err != nil {
		t.Fatalf("failed to parse test manifest: %v", err)
	}
	return Describe(doc)
}

// field looks up a field value by section title and label.
func field(sections []*Section, sectionTitle, label string) (string, bool) {
	for _, section := range sections {
		if section.Title != sectionTitle {
			continue
		}
		for _, item := range section.Items {
			if item.Field != nil && item.Field.Label == label {
				return item.Field.Value, true
			}
		}
	}
	return "", false
}

// table returns the first table in a section.
func table(sections []*Section, sectionTitle string) *Table {
	for _, section := range sections {
		if section.Title != sectionTitle {
			continue
		}
		for _, item := range section.Items {
			if item.Table != nil {
				return item.Table
			}
		}
	}
	return nil
}

func titles(sections []*Section) []string {
	out := make([]string, 0, len(sections))
	for _, section := range sections {
		out = append(out, section.Title)
	}
	return out
}

func requireField(t *testing.T, sections []*Section, sectionTitle, label, want string) {
	t.Helper()
	got, found := field(sections, sectionTitle, label)
	if !found {
		t.Errorf("%s / %s: missing; sections are %v", sectionTitle, label, titles(sections))
		return
	}
	if got != want {
		t.Errorf("%s / %s = %q, want %q", sectionTitle, label, got, want)
	}
}

func TestDescribeDeployment(t *testing.T) {
	sections := describeYAML(t, `
apiVersion: apps/v1
kind: Deployment
metadata:
  name: web
  namespace: shop
  labels:
    app: web
  annotations:
    confighub.com/ResourceMergeID: ignored
    owner: platform
spec:
  replicas: 3
  strategy:
    type: RollingUpdate
    rollingUpdate:
      maxSurge: 25%
      maxUnavailable: 0
  selector:
    matchLabels:
      app: web
      tier: frontend
  template:
    spec:
      serviceAccountName: web
      imagePullSecrets:
        - name: ghcr
      initContainers:
        - name: migrate
          image: migrate:1
      containers:
        - name: web
          image: nginx:1.27
          ports:
            - name: http
              containerPort: 8080
            - containerPort: 9090
          resources:
            requests:
              cpu: 100m
              memory: 128Mi
            limits:
              memory: 256Mi
      volumes:
        - name: config
          configMap:
            name: web-config
        - name: scratch
          emptyDir: {}
`)

	requireField(t, sections, "Metadata", "Name", "web")
	requireField(t, sections, "Metadata", "Namespace", "shop")
	requireField(t, sections, "Labels", "app", "web")
	requireField(t, sections, "Annotations", "owner", "platform")
	if _, found := field(sections, "Annotations", "confighub.com/ResourceMergeID"); found {
		t.Error("ConfigHub bookkeeping annotations should not be described")
	}
	requireField(t, sections, "Deployment", "Replicas", "3")
	requireField(t, sections, "Deployment", "Max Surge", "25%")
	requireField(t, sections, "Deployment", "Max Unavailable", "0")
	// Selectors render in sorted key order so output is stable.
	requireField(t, sections, "Deployment", "Selector", "app=web,tier=frontend")
	requireField(t, sections, "Pod Settings", "Service Account", "web")
	requireField(t, sections, "Pod Settings", "Image Pull Secrets", "ghcr")

	containers := table(sections, "Containers")
	if containers == nil {
		t.Fatalf("missing Containers table; sections are %v", titles(sections))
	}
	want := []string{"web", "nginx:1.27", "http:8080, 9090", "100m (req)", "128Mi -> 256Mi"}
	if len(containers.Rows) != 1 || !equalRow(containers.Rows[0], want) {
		t.Errorf("Containers row = %v, want %v", containers.Rows, want)
	}
	if initContainers := table(sections, "Init Containers"); initContainers == nil {
		t.Error("missing Init Containers table")
	}

	volumes := table(sections, "Volumes")
	if volumes == nil || len(volumes.Rows) != 2 {
		t.Fatalf("Volumes table = %v, want 2 rows", volumes)
	}
	if volumes.Rows[0][1] != "ConfigMap web-config" || volumes.Rows[1][1] != "EmptyDir" {
		t.Errorf("volume sources = %q, %q", volumes.Rows[0][1], volumes.Rows[1][1])
	}
}

func TestDescribeCronJob(t *testing.T) {
	sections := describeYAML(t, `
apiVersion: batch/v1
kind: CronJob
metadata:
  name: nightly
spec:
  schedule: "0 2 * * *"
  suspend: false
  successfulJobsHistoryLimit: 3
  jobTemplate:
    spec:
      backoffLimit: 2
      template:
        spec:
          restartPolicy: OnFailure
          containers:
            - name: run
              image: batch:2
`)
	requireField(t, sections, "CronJob", "Schedule", "0 2 * * *")
	requireField(t, sections, "CronJob", "Suspend", "false")
	requireField(t, sections, "CronJob", "History Limits", "3 successful / default failed")
	requireField(t, sections, "Job Template", "Backoff Limit", "2")
	requireField(t, sections, "Pod Settings", "Restart Policy", "OnFailure")
}

func TestDescribeService(t *testing.T) {
	sections := describeYAML(t, `
apiVersion: v1
kind: Service
metadata:
  name: web
spec:
  selector:
    app: web
  ports:
    - name: http
      port: 80
      targetPort: 8080
`)
	// The Kubernetes default is implied rather than written, so it is supplied.
	requireField(t, sections, "Service", "Type", "ClusterIP")
	requireField(t, sections, "Service", "Selector", "app=web")
	ports := table(sections, "Ports")
	if ports == nil || len(ports.Rows) != 1 {
		t.Fatalf("Ports table = %v, want 1 row", ports)
	}
	if !equalRow(ports.Rows[0], []string{"http", "80", "8080", "TCP", ""}) {
		t.Errorf("Ports row = %v", ports.Rows[0])
	}
}

func TestDescribeConfigMapSplitsShortAndLongValues(t *testing.T) {
	sections := describeYAML(t, `
apiVersion: v1
kind: ConfigMap
metadata:
  name: settings
data:
  LOG_LEVEL: debug
  nginx.conf: |
    server {
      listen 80;
    }
`)
	requireField(t, sections, "Data", "LOG_LEVEL", "debug")
	var blockTitles []string
	for _, section := range sections {
		if section.Title != "Data" {
			continue
		}
		for _, item := range section.Items {
			if item.Block != nil {
				blockTitles = append(blockTitles, item.Block.Title)
			}
		}
	}
	if len(blockTitles) != 1 || blockTitles[0] != "nginx.conf" {
		t.Errorf("multi-line entries rendered as blocks = %v, want [nginx.conf]", blockTitles)
	}
}

// Secret values live in the Unit's data, but a summary reports only their
// shape; the raw configuration is a separate, explicit request.
func TestDescribeSecretReportsSizesNotValues(t *testing.T) {
	sections := describeYAML(t, `
apiVersion: v1
kind: Secret
metadata:
  name: creds
type: Opaque
data:
  password: c3VwZXJzZWNyZXQ=
stringData:
  username: admin
`)
	keys := table(sections, "Keys")
	if keys == nil || len(keys.Rows) != 2 {
		t.Fatalf("Keys table = %v, want 2 rows", keys)
	}
	if !equalRow(keys.Rows[0], []string{"password", "11 bytes"}) {
		t.Errorf("decoded size row = %v", keys.Rows[0])
	}
	if !equalRow(keys.Rows[1], []string{"username", "5 bytes (stringData)"}) {
		t.Errorf("stringData row = %v", keys.Rows[1])
	}
	for _, section := range sections {
		for _, item := range section.Items {
			if item.Field != nil && strings.Contains(item.Field.Value, "supersecret") {
				t.Error("secret values must not appear in a described summary")
			}
		}
	}
}

func TestDescribeIngressFlattensHostAndPath(t *testing.T) {
	sections := describeYAML(t, `
apiVersion: networking.k8s.io/v1
kind: Ingress
metadata:
  name: shop
spec:
  ingressClassName: nginx
  rules:
    - host: shop.example.com
      http:
        paths:
          - path: /
            pathType: Prefix
            backend:
              service:
                name: web
                port:
                  number: 80
          - path: /api
            pathType: Prefix
            backend:
              service:
                name: api
                port:
                  name: http
    - http:
        paths:
          - backend:
              service:
                name: fallback
  tls:
    - hosts: [shop.example.com]
      secretName: shop-tls
`)
	requireField(t, sections, "Ingress", "Ingress Class", "nginx")
	routing := table(sections, "Routing")
	if routing == nil || len(routing.Rows) != 3 {
		t.Fatalf("Routing table = %v, want 3 rows", routing)
	}
	if !equalRow(routing.Rows[0], []string{"shop.example.com", "/", "Prefix", "web:80"}) {
		t.Errorf("first route = %v", routing.Rows[0])
	}
	if routing.Rows[1][3] != "api:http" {
		t.Errorf("named backend port = %q", routing.Rows[1][3])
	}
	// A rule without a host applies to every host, and a path defaults to "/".
	if !equalRow(routing.Rows[2], []string{"*", "/", "", "fallback"}) {
		t.Errorf("hostless route = %v", routing.Rows[2])
	}
	if tls := table(sections, "TLS"); tls == nil || tls.Rows[0][1] != "shop-tls" {
		t.Errorf("TLS table = %v", tls)
	}
}

func TestDescribeNetworkPolicy(t *testing.T) {
	sections := describeYAML(t, `
apiVersion: networking.k8s.io/v1
kind: NetworkPolicy
metadata:
  name: db
spec:
  podSelector: {}
  policyTypes: [Ingress, Egress]
  ingress:
    - from:
        - podSelector:
            matchLabels:
              app: web
        - ipBlock:
            cidr: 10.0.0.0/8
      ports:
        - port: 5432
        - protocol: UDP
          port: 100
          endPort: 200
  egress:
    - {}
`)
	// An empty podSelector selects every pod, which is worth spelling out.
	requireField(t, sections, "Network Policy", "Pod Selector", "all pods in namespace")
	requireField(t, sections, "Network Policy", "Policy Types", "Ingress, Egress")

	ingress := table(sections, "Ingress Rules")
	if ingress == nil || len(ingress.Rows) != 1 {
		t.Fatalf("Ingress Rules table = %v, want 1 row", ingress)
	}
	if !equalRow(ingress.Rows[0], []string{"pods(app=web); cidr 10.0.0.0/8", "TCP/5432, UDP/100-200"}) {
		t.Errorf("ingress rule = %v", ingress.Rows[0])
	}
	// An empty egress rule permits everything, which must not read as "nothing".
	egress := table(sections, "Egress Rules")
	if egress == nil || !equalRow(egress.Rows[0], []string{"any", "all"}) {
		t.Errorf("egress rule = %v", egress)
	}
}

func TestDescribeClusterRoleNamesTheCoreGroup(t *testing.T) {
	sections := describeYAML(t, `
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: reader
rules:
  - apiGroups: ["", "apps"]
    resources: [pods, deployments]
    verbs: [get, list]
  - nonResourceURLs: ["/healthz"]
    verbs: [get]
`)
	rules := table(sections, "Rules")
	if rules == nil || len(rules.Rows) != 2 {
		t.Fatalf("Rules table = %v, want 2 rows", rules)
	}
	if rules.Rows[0][0] != "(core), apps" {
		t.Errorf("apiGroups = %q, want %q", rules.Rows[0][0], "(core), apps")
	}
	// Non-resource rules have no resources; the URLs stand in for them.
	if rules.Rows[1][1] != "/healthz" {
		t.Errorf("nonResourceURLs = %q", rules.Rows[1][1])
	}
}

func TestDescribeHorizontalPodAutoscaler(t *testing.T) {
	sections := describeYAML(t, `
apiVersion: autoscaling/v2
kind: HorizontalPodAutoscaler
metadata:
  name: web
spec:
  scaleTargetRef:
    kind: Deployment
    name: web
  minReplicas: 2
  maxReplicas: 10
  metrics:
    - type: Resource
      resource:
        name: cpu
        target:
          averageUtilization: 70
    - type: Pods
      pods:
        metric:
          name: queue_depth
        target:
          averageValue: "30"
`)
	requireField(t, sections, "Autoscaler", "Target", "Deployment web")
	metrics := table(sections, "Metrics")
	if metrics == nil || len(metrics.Rows) != 2 {
		t.Fatalf("Metrics table = %v, want 2 rows", metrics)
	}
	if !equalRow(metrics.Rows[0], []string{"Resource", "cpu", "70%"}) {
		t.Errorf("resource metric = %v", metrics.Rows[0])
	}
	if !equalRow(metrics.Rows[1], []string{"Pods", "queue_depth", "30"}) {
		t.Errorf("pods metric = %v", metrics.Rows[1])
	}
}

func TestDescribeArgoCDApplicationWithMultipleSources(t *testing.T) {
	sections := describeYAML(t, `
apiVersion: argoproj.io/v1alpha1
kind: Application
metadata:
  name: shop
spec:
  project: default
  destination:
    server: https://kubernetes.default.svc
    namespace: shop
  sources:
    - repoURL: oci://registry.example.com/shop
      targetRevision: latest
    - repoURL: https://charts.example.com
      chart: shop
      helm:
        valueFiles: [values.yaml]
  syncPolicy:
    automated:
      prune: true
      selfHeal: true
    syncOptions: [ServerSideApply=true]
`)
	requireField(t, sections, "Application", "Destination", "https://kubernetes.default.svc")
	requireField(t, sections, "Source 1", "Repo URL", "oci://registry.example.com/shop")
	requireField(t, sections, "Source 2", "Chart", "shop")
	requireField(t, sections, "Source 2", "Value Files", "values.yaml")
	requireField(t, sections, "Sync Policy", "Automated", "yes")
	requireField(t, sections, "Sync Policy", "Prune", "true")
}

func TestDescribeArgoCDApplicationWithoutAutomatedSync(t *testing.T) {
	sections := describeYAML(t, `
apiVersion: argoproj.io/v1alpha1
kind: Application
metadata:
  name: shop
spec:
  source:
    repoURL: oci://registry.example.com/shop
  syncPolicy:
    syncOptions: [CreateNamespace=true]
`)
	requireField(t, sections, "Sync Policy", "Automated", "no (manual sync)")
	requireField(t, sections, "Source", "Repo URL", "oci://registry.example.com/shop")
}

func TestDescribeHelmRelease(t *testing.T) {
	sections := describeYAML(t, `
apiVersion: helm.toolkit.fluxcd.io/v2
kind: HelmRelease
metadata:
  name: shop
spec:
  interval: 5m
  chart:
    spec:
      chart: shop
      version: 1.2.3
      sourceRef:
        kind: HelmRepository
        name: charts
        namespace: flux-system
  values:
    replicaCount: 2
`)
	requireField(t, sections, "Helm Release", "Interval", "5m")
	requireField(t, sections, "Chart", "Chart", "shop")
	requireField(t, sections, "Chart", "Source", "HelmRepository flux-system/charts")
	// Structured values are too large for a row, so they render as a block.
	var blocks int
	for _, section := range sections {
		if section.Title != "Values" {
			continue
		}
		for _, item := range section.Items {
			if item.Block != nil && strings.Contains(item.Block.Text, "replicaCount: 2") {
				blocks++
			}
		}
	}
	if blocks != 1 {
		t.Errorf("inline Helm values rendered as %d blocks, want 1", blocks)
	}
}

func TestDescribeCertificate(t *testing.T) {
	sections := describeYAML(t, `
apiVersion: cert-manager.io/v1
kind: Certificate
metadata:
  name: web
spec:
  secretName: web-tls
  issuerRef:
    kind: ClusterIssuer
    name: letsencrypt
  dnsNames: [shop.example.com, www.example.com]
  privateKey:
    algorithm: ECDSA
    size: 256
`)
	requireField(t, sections, "Certificate", "Secret Name", "web-tls")
	requireField(t, sections, "Certificate", "Issuer", "ClusterIssuer letsencrypt")
	requireField(t, sections, "Subjects", "DNS Names", "shop.example.com, www.example.com")
	requireField(t, sections, "Key", "Algorithm", "ECDSA")
}

func TestDescribeTraefikMiddlewareNamesItsType(t *testing.T) {
	sections := describeYAML(t, `
apiVersion: traefik.io/v1alpha1
kind: Middleware
metadata:
  name: redirect
spec:
  redirectScheme:
    scheme: https
    permanent: true
`)
	requireField(t, sections, "Middleware: redirectScheme", "scheme", "https")
	requireField(t, sections, "Middleware: redirectScheme", "permanent", "true")
}

// Kinds without a dedicated view still describe: scalars become rows and
// nested structures become YAML blocks. This is the path every custom resource
// takes.
func TestDescribeUnknownKindFallsBackToFields(t *testing.T) {
	sections := describeYAML(t, `
apiVersion: example.com/v1
kind: Widget
metadata:
  name: gizmo
size: large
spec:
  color: blue
status:
  ready: true
`)
	requireField(t, sections, "Metadata", "Kind", "Widget")
	requireField(t, sections, "Fields", "size", "large")
	if _, found := field(sections, "Fields", "status"); found {
		t.Error("status is runtime state and should not be described")
	}
	var specBlock string
	for _, section := range sections {
		if section.Title != "Fields" {
			continue
		}
		for _, item := range section.Items {
			if item.Block != nil && item.Block.Title == "spec" {
				specBlock = item.Block.Text
			}
		}
	}
	if !strings.Contains(specBlock, "color: blue") {
		t.Errorf("spec block = %q", specBlock)
	}
}

// A document that isn't shaped like a Kubernetes resource must degrade to
// empty sections rather than panic.
func TestDescribeToleratesUnexpectedShapes(t *testing.T) {
	sections := describeYAML(t, `
apiVersion: apps/v1
kind: Deployment
metadata: not-a-map
spec:
  replicas: [1, 2]
  template: 7
`)
	if _, found := field(sections, "Metadata", "Name"); found {
		t.Error("a scalar metadata should yield no name")
	}
	requireField(t, sections, "Metadata", "Kind", "Deployment")
	if _, found := field(sections, "Deployment", "Replicas"); found {
		t.Error("a list-valued replicas should not render as a scalar")
	}
}

func TestDescribeNilDocument(t *testing.T) {
	if sections := Describe(nil); sections != nil {
		t.Errorf("Describe(nil) = %v, want nil", sections)
	}
}

func TestHasView(t *testing.T) {
	// Registration ignores the API version, so every version of a kind resolves.
	for _, tt := range []struct{ apiVersion, kind string }{
		{"apps/v1", "Deployment"},
		{"autoscaling/v2", "HorizontalPodAutoscaler"},
		{"v1", "ConfigMap"},
		{"traefik.containo.us/v1alpha1", "IngressRoute"},
	} {
		if !HasView(tt.apiVersion, tt.kind) {
			t.Errorf("expected a view for %s/%s", tt.apiVersion, tt.kind)
		}
	}
	if HasView("example.com/v1", "Widget") {
		t.Error("unregistered kinds should have no dedicated view")
	}
}

func equalRow(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}
