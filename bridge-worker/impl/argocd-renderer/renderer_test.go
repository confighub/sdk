// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package argocdrenderer

import (
	"context"
	"os"
	"strings"
	"testing"

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
