// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package argocdrenderer

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/clientcmd"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func getTestConfig(t *testing.T) (Config, client.Client) {
	t.Helper()

	server := os.Getenv("ARGOCD_SERVER")
	if server == "" {
		t.Skip("ARGOCD_SERVER not set, skipping integration test")
	}

	token := os.Getenv("ARGOCD_AUTH_TOKEN")
	if token == "" {
		t.Skip("ARGOCD_AUTH_TOKEN not set, skipping integration test")
	}

	config := Config{
		ServerAddress: server,
		AuthToken:     token,
		Insecure:      os.Getenv("ARGOCD_INSECURE") != "false",
	}

	// Try to create a k8s client from kubeconfig
	loadingRules := clientcmd.NewDefaultClientConfigLoadingRules()
	k8sCmdConfig := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(
		loadingRules,
		&clientcmd.ConfigOverrides{},
	)
	restConfig, err := k8sCmdConfig.ClientConfig()
	if err != nil {
		t.Fatalf("failed to get kubeconfig: %v", err)
	}

	k8sClient, err := client.New(restConfig, client.Options{})
	if err != nil {
		t.Fatalf("failed to create k8s client: %v", err)
	}

	return config, k8sClient
}

func TestRenderArgoCD_Helm(t *testing.T) {
	config, k8sClient := getTestConfig(t)

	appYAML := []byte(`apiVersion: argoproj.io/v1alpha1
kind: Application
metadata:
  name: helm-guestbook-test
  namespace: argocd
spec:
  project: default
  source:
    repoURL: https://github.com/argoproj/argocd-example-apps.git
    targetRevision: HEAD
    path: helm-guestbook
  destination:
    server: https://kubernetes.default.svc
    namespace: default
`)

	result, err := RenderArgoCD(context.Background(), appYAML, k8sClient, config)
	if err != nil {
		t.Fatalf("RenderArgoCD failed: %v", err)
	}

	manifests := string(result.Manifests)

	if !strings.Contains(manifests, "kind: Deployment") {
		t.Error("expected rendered manifests to contain a Deployment")
	}

	if !strings.Contains(manifests, "kind: Service") {
		t.Error("expected rendered manifests to contain a Service")
	}

	if !strings.Contains(manifests, "guestbook") {
		t.Error("expected rendered manifests to contain 'guestbook'")
	}

	if result.Revision == "" {
		t.Error("expected non-empty revision")
	}

	t.Logf("Rendered %d bytes, revision=%s, sourceType=%s", len(result.Manifests), result.Revision, result.SourceType)
}

func TestRenderArgoCD_Kustomize(t *testing.T) {
	config, k8sClient := getTestConfig(t)

	appYAML := []byte(`apiVersion: argoproj.io/v1alpha1
kind: Application
metadata:
  name: kustomize-guestbook-test
  namespace: argocd
spec:
  project: default
  source:
    repoURL: https://github.com/argoproj/argocd-example-apps.git
    targetRevision: HEAD
    path: kustomize-guestbook
  destination:
    server: https://kubernetes.default.svc
    namespace: default
`)

	result, err := RenderArgoCD(context.Background(), appYAML, k8sClient, config)
	if err != nil {
		t.Fatalf("RenderArgoCD failed: %v", err)
	}

	manifests := string(result.Manifests)

	if !strings.Contains(manifests, "kind: Deployment") {
		t.Error("expected rendered manifests to contain a Deployment")
	}

	if !strings.Contains(manifests, "kind: Service") {
		t.Error("expected rendered manifests to contain a Service")
	}

	if !strings.Contains(manifests, "guestbook") {
		t.Error("expected rendered manifests to contain 'guestbook'")
	}

	if result.Revision == "" {
		t.Error("expected non-empty revision")
	}

	t.Logf("Rendered %d bytes, revision=%s, sourceType=%s", len(result.Manifests), result.Revision, result.SourceType)
}

// TestExtractDestinationSettings pins the destination-settings extraction
// contract. Both syncOptions locations (spec.syncPolicy.syncOptions and
// operation.sync.syncOptions) are searched for CreateNamespace=true, and
// an Application with neither destination namespace nor CreateNamespace
// produces a zero-value DestinationSettings (the renderer then leaves the
// manifests untouched).
func TestExtractDestinationSettings(t *testing.T) {
	tcs := []struct {
		name string
		app  map[string]interface{}
		want DestinationSettings
	}{
		{
			name: "destination namespace alone",
			app: map[string]interface{}{
				"spec": map[string]interface{}{
					"destination": map[string]interface{}{"namespace": "prod"},
				},
			},
			want: DestinationSettings{Namespace: "prod"},
		},
		{
			name: "CreateNamespace in spec.syncPolicy.syncOptions",
			app: map[string]interface{}{
				"spec": map[string]interface{}{
					"destination": map[string]interface{}{"namespace": "prod"},
					"syncPolicy": map[string]interface{}{
						"syncOptions": []interface{}{"CreateNamespace=true", "ServerSideApply=true"},
					},
				},
			},
			want: DestinationSettings{Namespace: "prod", CreateNamespace: true},
		},
		{
			name: "CreateNamespace in operation.sync.syncOptions only",
			app: map[string]interface{}{
				"spec": map[string]interface{}{
					"destination": map[string]interface{}{"namespace": "prod"},
				},
				"operation": map[string]interface{}{
					"sync": map[string]interface{}{
						"syncOptions": []interface{}{"CreateNamespace=true"},
					},
				},
			},
			want: DestinationSettings{Namespace: "prod", CreateNamespace: true},
		},
		{
			name: "no destination, no sync options",
			app:  map[string]interface{}{},
			want: DestinationSettings{},
		},
		{
			name: "destination namespace blank, CreateNamespace set — caller leaves output untouched (no NS doc emitted)",
			app: map[string]interface{}{
				"spec": map[string]interface{}{
					"syncPolicy": map[string]interface{}{
						"syncOptions": []interface{}{"CreateNamespace=true"},
					},
				},
			},
			want: DestinationSettings{CreateNamespace: true},
		},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			got := ExtractDestinationSettings(&unstructured.Unstructured{Object: tc.app})
			if got != tc.want {
				t.Fatalf("ExtractDestinationSettings: want %+v, got %+v", tc.want, got)
			}
		})
	}
}

// namespacedKindClient embeds client.Client (nil for unused methods) and
// answers IsObjectNamespaced from a fixed set of cluster-scoped kinds.
// Used by convertManifestsToYAML tests so we don't need a real cluster.
type namespacedKindClient struct {
	client.Client
}

func (c *namespacedKindClient) IsObjectNamespaced(obj runtime.Object) (bool, error) {
	u, ok := obj.(*unstructured.Unstructured)
	if !ok {
		return true, nil
	}
	switch u.GetKind() {
	case "Namespace", "ClusterRole", "ClusterRoleBinding", "CustomResourceDefinition":
		return false, nil
	default:
		return true, nil
	}
}

// manifestJSON builds a minimal manifest JSON string for tests.
func manifestJSON(t *testing.T, kind, namespace, name string) string {
	t.Helper()
	obj := map[string]interface{}{
		"apiVersion": "v1",
		"kind":       kind,
		"metadata":   map[string]interface{}{"name": name},
	}
	if namespace != "" {
		obj["metadata"].(map[string]interface{})["namespace"] = namespace
	}
	b, err := json.Marshal(obj)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return string(b)
}

// TestConvertManifestsToYAML_BakesDestinationNamespace pins the rendering
// contract that namespaced resources missing metadata.namespace receive
// the source Application's destination namespace, while cluster-scoped
// resources and resources that already declare a namespace are left alone.
func TestConvertManifestsToYAML_BakesDestinationNamespace(t *testing.T) {
	c := &namespacedKindClient{}
	manifests := []string{
		manifestJSON(t, "ConfigMap", "", "cm-no-ns"),     // → "prod"
		manifestJSON(t, "ConfigMap", "explicit", "cm-keeps-ns"), // unchanged
		manifestJSON(t, "ClusterRole", "", "cr"),         // unchanged (cluster-scoped)
	}

	out, err := convertManifestsToYAML(c, manifests, DestinationSettings{Namespace: "prod"})
	if err != nil {
		t.Fatalf("convertManifestsToYAML: %v", err)
	}
	s := string(out)

	// Three documents joined by ---
	if got := strings.Count(s, "kind: ConfigMap"); got != 2 {
		t.Errorf("want 2 ConfigMaps in output, got %d\n%s", got, s)
	}
	if !strings.Contains(s, "name: cm-no-ns") || !strings.Contains(s, "namespace: prod") {
		t.Errorf("cm-no-ns should have namespace baked to prod, got:\n%s", s)
	}
	if !strings.Contains(s, "name: cm-keeps-ns") || !strings.Contains(s, "namespace: explicit") {
		t.Errorf("cm-keeps-ns should preserve its explicit namespace, got:\n%s", s)
	}
	if !strings.Contains(s, "name: cr") {
		t.Errorf("ClusterRole missing from output, got:\n%s", s)
	}
	if strings.Contains(s, "kind: ClusterRole\n") && strings.Contains(s, "namespace: prod") {
		// Cluster-scoped resources must not get a namespace stamped on
		// them. Find the ClusterRole doc and confirm.
		clusterRoleDoc := s[strings.Index(s, "kind: ClusterRole"):]
		if i := strings.Index(clusterRoleDoc, "\n---\n"); i >= 0 {
			clusterRoleDoc = clusterRoleDoc[:i]
		}
		if strings.Contains(clusterRoleDoc, "namespace:") {
			t.Errorf("ClusterRole must not have a namespace, got:\n%s", clusterRoleDoc)
		}
	}
}

// TestConvertManifestsToYAML_EmitsNamespaceDocOnCreateNamespace pins the
// contract that a Namespace document is prepended when the source
// Application set CreateNamespace=true and named a destination namespace.
func TestConvertManifestsToYAML_EmitsNamespaceDocOnCreateNamespace(t *testing.T) {
	c := &namespacedKindClient{}
	manifests := []string{manifestJSON(t, "ConfigMap", "", "cm")}

	t.Run("emits Namespace doc when CreateNamespace=true and namespace is set", func(t *testing.T) {
		out, err := convertManifestsToYAML(c, manifests, DestinationSettings{
			Namespace:       "prod",
			CreateNamespace: true,
		})
		if err != nil {
			t.Fatalf("convertManifestsToYAML: %v", err)
		}
		s := string(out)
		// First document must be the Namespace.
		first := s
		if i := strings.Index(s, "\n---\n"); i >= 0 {
			first = s[:i]
		}
		if !strings.Contains(first, "kind: Namespace") || !strings.Contains(first, "name: prod") {
			t.Fatalf("first document must be Namespace 'prod', got:\n%s", first)
		}
	})

	t.Run("no Namespace doc when CreateNamespace is false", func(t *testing.T) {
		out, err := convertManifestsToYAML(c, manifests, DestinationSettings{Namespace: "prod"})
		if err != nil {
			t.Fatalf("convertManifestsToYAML: %v", err)
		}
		if strings.Contains(string(out), "kind: Namespace") {
			t.Fatalf("output must not include a Namespace document when CreateNamespace is false, got:\n%s", string(out))
		}
	})

	t.Run("no Namespace doc when destination namespace is blank", func(t *testing.T) {
		out, err := convertManifestsToYAML(c, manifests, DestinationSettings{CreateNamespace: true})
		if err != nil {
			t.Fatalf("convertManifestsToYAML: %v", err)
		}
		if strings.Contains(string(out), "kind: Namespace") {
			t.Fatalf("output must not include a Namespace document when destination namespace is blank, got:\n%s", string(out))
		}
	})
}
