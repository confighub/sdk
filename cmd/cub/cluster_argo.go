// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"time"
)

// Argo CD install + configuration inside a kind cluster, so it can pull config
// from ConfigHub's OCI registry. All operations shell out to kubectl using a
// per-cluster kubeconfig. The intended caller is `cub cluster up`.

// clusterArgoInstallURL is the upstream Argo CD install manifest. Tracks
// "stable" — currency outweighs an exact version pin for a local kind cluster.
const clusterArgoInstallURL = "https://raw.githubusercontent.com/argoproj/argo-cd/stable/manifests/install.yaml"

// clusterArgoNamespace is the conventional Argo CD namespace.
const clusterArgoNamespace = "argocd"

// clusterArgoInstall applies the Argo CD manifest into the argocd namespace,
// switches argocd-server to insecure HTTP + NodePort on the given port, applies
// the argocd-cm ignoreDifferences block needed for the self-referencing root
// Application, waits for argocd-server to be ready, and returns the
// auto-generated admin password.
func clusterArgoInstall(ctx context.Context, kubeconfigPath string, nodePort int, out io.Writer) (string, error) {
	fmt.Fprintf(out, "Creating namespace %q...\n", clusterArgoNamespace)
	if err := clusterKubectl(ctx, kubeconfigPath, nil, out,
		"create", "namespace", clusterArgoNamespace, "--dry-run=client", "-o", "yaml"); err != nil {
		return "", err
	}
	// idempotent ns create via apply
	nsYaml := []byte("apiVersion: v1\nkind: Namespace\nmetadata:\n  name: " + clusterArgoNamespace + "\n")
	if err := clusterKubectlApply(ctx, kubeconfigPath, nsYaml, out); err != nil {
		return "", err
	}

	fmt.Fprintf(out, "Applying Argo CD install manifest from %s...\n", clusterArgoInstallURL)
	// --server-side is required: the applicationsets CRD's annotations
	// exceed kubectl's 256 KiB client-side limit.
	if err := clusterKubectl(ctx, kubeconfigPath, nil, out,
		"apply", "--server-side", "--force-conflicts", "-n", clusterArgoNamespace, "-f", clusterArgoInstallURL); err != nil {
		return "", fmt.Errorf("apply argo install: %w", err)
	}

	fmt.Fprintln(out, "Patching argocd-cmd-params-cm (server.insecure=true)...")
	if err := clusterKubectl(ctx, kubeconfigPath, nil, out,
		"patch", "configmap", "argocd-cmd-params-cm", "-n", clusterArgoNamespace,
		"--type", "merge", "-p", `{"data":{"server.insecure":"true"}}`); err != nil {
		return "", err
	}

	fmt.Fprintln(out, "Patching argocd-cm (ignoreDifferences for self-referencing root Application)...")
	if err := clusterKubectl(ctx, kubeconfigPath, nil, out,
		"patch", "configmap", "argocd-cm", "-n", clusterArgoNamespace,
		"--type", "merge", "-p", clusterArgoIgnoreDifferencesPatch); err != nil {
		return "", err
	}

	fmt.Fprintf(out, "Patching argocd-server Service to NodePort %d...\n", nodePort)
	svcPatch := fmt.Sprintf(
		`{"spec":{"type":"NodePort","ports":[{"name":"http","port":80,"targetPort":8080,"protocol":"TCP","nodePort":%d}]}}`,
		nodePort)
	if err := clusterKubectl(ctx, kubeconfigPath, nil, out,
		"patch", "service", "argocd-server", "-n", clusterArgoNamespace,
		"--type", "merge", "-p", svcPatch); err != nil {
		return "", err
	}

	// Rollout-restart argocd-server so it picks up server.insecure=true and
	// stops re-running its readiness probe against HTTPS.
	fmt.Fprintln(out, "Restarting argocd-server to pick up cmd-params change...")
	if err := clusterKubectl(ctx, kubeconfigPath, nil, out,
		"rollout", "restart", "deployment", "argocd-server", "-n", clusterArgoNamespace); err != nil {
		return "", err
	}

	fmt.Fprintln(out, "Waiting for argocd-server Deployment to be Available (timeout 5m)...")
	if err := clusterKubectl(ctx, kubeconfigPath, nil, out,
		"wait", "--for=condition=Available", "--timeout=5m",
		"deployment/argocd-server", "-n", clusterArgoNamespace); err != nil {
		return "", fmt.Errorf("wait for argocd-server: %w", err)
	}

	fmt.Fprintln(out, "Fetching initial admin password...")
	password, err := clusterArgoWaitAdminPassword(ctx, kubeconfigPath, 2*time.Minute)
	if err != nil {
		return "", err
	}
	return password, nil
}

// clusterArgoApplyOCIRepoCreds installs the Argo repo-creds Secret pointing at
// the ConfigHub OCI registry. Argo uses prefix-matching to pair this with any
// Application whose source.repoURL falls under ociURL.
//
// When insecureHTTP is true the Secret carries Argo's insecureOCIForceHttp flag,
// telling the repo-server to use plain HTTP against the OCI registry. Needed
// when wired up against a local cub server (http://localhost...) whose OCI
// endpoint is also plain HTTP. No effect for production https endpoints.
func clusterArgoApplyOCIRepoCreds(ctx context.Context, kubeconfigPath, ociURL, workerID, workerSecret string, insecureHTTP bool, out io.Writer) error {
	extra := ""
	if insecureHTTP {
		extra = "  insecureOCIForceHttp: \"true\"\n"
	}
	secret := fmt.Sprintf(`apiVersion: v1
kind: Secret
metadata:
  name: confighub-oci-creds
  namespace: %s
  labels:
    argocd.argoproj.io/secret-type: repo-creds
type: Opaque
stringData:
  type: oci
  url: %s
  enableOCI: "true"
%s  username: %s
  password: %s
`, clusterArgoNamespace, ociURL, extra, workerID, workerSecret)
	fmt.Fprintln(out, "Applying confighub-oci-creds repo-creds Secret to argocd namespace...")
	return clusterKubectlApply(ctx, kubeconfigPath, []byte(secret), out)
}

// clusterArgoRootAppManifest returns the YAML for the root "app-of-apps"
// Application. It self-references the same Space's Release bundle, so adding
// new Application Units to the Space and publishing a new Release causes Argo
// to create the corresponding Apps on its next sync. A Release bundle is flat —
// one <unit-slug>.yaml per bundled Unit at the bundle root — so path is ".".
func clusterArgoRootAppManifest(spaceSlug, ociRepoURL string) []byte {
	return fmt.Appendf(nil, `apiVersion: argoproj.io/v1alpha1
kind: Application
metadata:
  name: %s
  namespace: %s
  finalizers:
    - resources-finalizer.argocd.argoproj.io
spec:
  project: default
  source:
    repoURL: %s
    targetRevision: latest
    path: .
  destination:
    server: https://kubernetes.default.svc
    namespace: %s
  syncPolicy:
    automated:
      selfHeal: true
    syncOptions:
      - ServerSideApply=true
      - ServerSideApply.ForceConflicts=true
      - RespectIgnoreDifferences=true
      - CreateNamespace=false
`, spaceSlug, clusterArgoNamespace, ociRepoURL, clusterArgoNamespace)
}

// clusterArgoAdminPassword reads the argocd-initial-admin-secret from the
// cluster and base64-decodes its password field. Errors when the Secret is
// absent: argocd-server creates it on first start, and Argo's own docs tell
// admins to delete it once they have changed the password.
func clusterArgoAdminPassword(ctx context.Context, kubeconfigPath string) (string, error) {
	raw, err := clusterKubectlOutput(ctx, kubeconfigPath,
		"get", "secret", "argocd-initial-admin-secret", "-n", clusterArgoNamespace,
		"-o", "jsonpath={.data.password}")
	if err != nil {
		return "", err
	}
	if len(raw) == 0 {
		return "", fmt.Errorf("secret %s/argocd-initial-admin-secret has no password", clusterArgoNamespace)
	}
	decoded, err := base64.StdEncoding.DecodeString(string(raw))
	if err != nil {
		return "", fmt.Errorf("decode admin password: %w: %w", clusterArgoErrPasswordDecode, err)
	}
	return string(decoded), nil
}

// clusterArgoErrPasswordDecode marks a password that is present but not
// decodable. Unlike a Secret that has not appeared yet, that is terminal, so
// clusterArgoWaitAdminPassword stops polling instead of burning its budget.
var clusterArgoErrPasswordDecode = errors.New("not valid base64")

// clusterArgoWaitAdminPassword polls until the argocd-initial-admin-secret
// exists (created by argocd-server after first start), then base64-decodes the
// password field.
func clusterArgoWaitAdminPassword(ctx context.Context, kubeconfigPath string, budget time.Duration) (string, error) {
	deadline := time.Now().Add(budget)
	var lastErr error
	for {
		password, err := clusterArgoAdminPassword(ctx, kubeconfigPath)
		if err == nil {
			return password, nil
		}
		if errors.Is(err, clusterArgoErrPasswordDecode) {
			return "", err
		}
		lastErr = err
		if time.Now().After(deadline) {
			if lastErr != nil {
				return "", fmt.Errorf("admin secret not present after %s: %w", budget, lastErr)
			}
			return "", fmt.Errorf("admin secret not present after %s", budget)
		}
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(2 * time.Second):
		}
	}
}

// clusterArgoIgnoreDifferencesPatch is a merge patch for argocd-cm that tells
// Argo to ignore the Argo + ConfigHub bookkeeping annotations on
// argoproj.io_Application resources. Without this, the self-referencing root
// Application is perpetually OutOfSync.
var clusterArgoIgnoreDifferencesPatch = func() string {
	patch := map[string]any{
		"data": map[string]any{
			"resource.customizations.ignoreDifferences.argoproj.io_Application": `jqPathExpressions:
- '.metadata.annotations."kubectl.kubernetes.io/last-applied-configuration"'
- '.metadata.annotations."confighub.com/origin"'
- '.metadata.annotations."confighub.com/RevisionNum"'
- '.metadata.annotations."confighub.com/SpaceID"'
- '.metadata.annotations."confighub.com/UnitSlug"'
- '.metadata.annotations."confighub.com/ResourceMergeID"'
- '.metadata.annotations."argocd.argoproj.io/tracking-id"'
- '.metadata.finalizers'
`,
		},
	}
	b, _ := json.Marshal(patch)
	return string(b)
}()
