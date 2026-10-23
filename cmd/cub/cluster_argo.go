// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"time"
)

// Argo CD install + configuration inside a cluster, so it can pull config from
// ConfigHub's OCI registry. All operations shell out to kubectl using a
// per-cluster kubeconfig. The caller is clusterAttach, on behalf of both `cub
// cluster up` and `cub cluster attach`.

// clusterArgoDefaultVersion is the Argo CD release installed when none is
// named. "stable" tracks upstream's stable tag — currency outweighs an exact
// pin for a cluster cub wires up. Whatever is chosen must be v3.1 or later:
// earlier releases cannot resolve an oci:// source for plain manifests and fail
// with `unsupported scheme "oci"` while still reporting the Application
// Healthy, so the failure is quiet.
const clusterArgoDefaultVersion = "stable"

// clusterArgoInstallURL returns the upstream Argo CD install manifest for a
// release tag ("stable", "v3.4.5", ...).
func clusterArgoInstallURL(version string) string {
	return "https://raw.githubusercontent.com/argoproj/argo-cd/" + version + "/manifests/install.yaml"
}

// clusterArgoNamespace is the conventional Argo CD namespace. It is a constant
// rather than an option: every Argo Application cub writes — the root
// app-of-apps here and the child Applications `cub variant create` derives from
// the target annotation — is placed in it, and the latter runs without any
// cluster in hand to read a per-cluster choice from.
const clusterArgoNamespace = "argocd"

// clusterArgoOCICredsSecret is the repo-creds Secret carrying the cluster
// service account's token for ConfigHub's OCI registry.
const clusterArgoOCICredsSecret = "confighub-oci-creds"

// clusterArgoIgnoreDifferencesKey is the argocd-cm key that makes Argo ignore
// the bookkeeping annotations on Application resources.
const clusterArgoIgnoreDifferencesKey = "resource.customizations.ignoreDifferences.argoproj.io_Application"

// clusterArgoOptions says how clusterArgoEnsure should get Argo CD into a
// cluster.
type clusterArgoOptions struct {
	// installIfAbsent installs Argo CD when the namespace has no argocd-server;
	// false turns its absence into an error (--no-install).
	installIfAbsent bool
	// nodePort, when positive, exposes argocd-server on that NodePort over plain
	// HTTP (server.insecure=true). `cub cluster up` sets it to the host port it
	// mapped into the kind node; an attached cluster leaves it 0 and keeps
	// whatever exposure Argo already has.
	nodePort int
}

// clusterArgoResult reports what clusterArgoEnsure found or did.
type clusterArgoResult struct {
	// installed is true when this call installed Argo CD, false when it was
	// already present and left alone.
	installed bool
	// adminPassword is the initial admin password, or "" when it could not be
	// read — normal for a pre-existing install whose admin rotated it and
	// deleted the initial Secret, as Argo's docs recommend.
	adminPassword string
}

// clusterArgoEnsure makes sure Argo CD is in the cluster and able to run the
// self-referencing root Application: installs it if absent (per opts), and
// applies the argocd-cm ignoreDifferences block that keeps the root app from
// reporting perpetually OutOfSync. A pre-existing Argo CD is otherwise left as
// found; in particular an existing ignoreDifferences entry for Applications is
// someone's deliberate configuration and is not overwritten. version is the
// release to install; "" installs clusterArgoDefaultVersion.
func clusterArgoEnsure(ctx context.Context, kubeconfigPath, version string, opts clusterArgoOptions, out io.Writer) (clusterArgoResult, error) {
	present, err := clusterArgoPresent(ctx, kubeconfigPath)
	if err != nil {
		return clusterArgoResult{}, err
	}
	if present {
		fmt.Fprintf(out, "Argo CD already present in namespace %q; leaving it in place.\n", clusterArgoNamespace)
		if err := clusterArgoEnsureIgnoreDifferences(ctx, kubeconfigPath, out); err != nil {
			return clusterArgoResult{}, err
		}
		// Best effort: the initial Secret may legitimately be gone.
		password, _ := clusterArgoAdminPassword(ctx, kubeconfigPath)
		return clusterArgoResult{installed: false, adminPassword: password}, nil
	}
	if !opts.installIfAbsent {
		return clusterArgoResult{}, fmt.Errorf("Argo CD not found in namespace %q (no argocd-server Deployment) and --no-install was given", clusterArgoNamespace)
	}
	password, err := clusterArgoInstall(ctx, kubeconfigPath, version, opts, out)
	if err != nil {
		return clusterArgoResult{}, err
	}
	return clusterArgoResult{installed: true, adminPassword: password}, nil
}

// clusterArgoPresent reports whether an argocd-server Deployment exists in the
// Argo namespace. A missing namespace or Deployment is "absent"; any other
// kubectl failure (unreachable cluster, no permission) is an error, since
// guessing "absent" there would lead straight into a 20k-line install attempt.
func clusterArgoPresent(ctx context.Context, kubeconfigPath string) (bool, error) {
	out, err := clusterKubectlOutput(ctx, kubeconfigPath,
		"get", "deployment", "argocd-server", "-n", clusterArgoNamespace, "--ignore-not-found", "-o", "name")
	if err != nil {
		return false, fmt.Errorf("check for an existing Argo CD: %w", err)
	}
	return len(bytes.TrimSpace(out)) > 0, nil
}

// clusterArgoInstall applies the Argo CD manifest into the argocd namespace,
// applies the argocd-cm ignoreDifferences block needed for the self-referencing
// root Application, optionally switches argocd-server to insecure HTTP on a
// NodePort, waits for the parts of Argo that serve to be ready, and returns the
// auto-generated admin password.
func clusterArgoInstall(ctx context.Context, kubeconfigPath, version string, opts clusterArgoOptions, out io.Writer) (string, error) {
	if version == "" {
		version = clusterArgoDefaultVersion
	}
	installURL := clusterArgoInstallURL(version)

	fmt.Fprintf(out, "Creating namespace %q...\n", clusterArgoNamespace)
	// idempotent ns create via apply
	nsYaml := []byte("apiVersion: v1\nkind: Namespace\nmetadata:\n  name: " + clusterArgoNamespace + "\n")
	if err := clusterKubectlApply(ctx, kubeconfigPath, nsYaml, out); err != nil {
		return "", err
	}

	fmt.Fprintf(out, "Applying Argo CD install manifest from %s...\n", installURL)
	// --server-side is required: the applicationsets CRD's annotations
	// exceed kubectl's 256 KiB client-side limit.
	if err := clusterKubectl(ctx, kubeconfigPath, nil, out,
		"apply", "--server-side", "--force-conflicts", "-n", clusterArgoNamespace, "-f", installURL); err != nil {
		return "", fmt.Errorf("apply argo install: %w", err)
	}

	if err := clusterArgoEnsureIgnoreDifferences(ctx, kubeconfigPath, out); err != nil {
		return "", err
	}

	if opts.nodePort > 0 {
		if err := clusterArgoExposeNodePort(ctx, kubeconfigPath, opts.nodePort, out); err != nil {
			return "", err
		}
	}

	// argocd-server being Available is not the same as Argo being able to
	// render anything: the repo-server and the application-controller are
	// separate workloads, and a root Application applied before they are up
	// fails its first comparison with "connection refused" and is not retried
	// until the next reconcile, by default three minutes later. Wait for the
	// pieces that actually serve.
	fmt.Fprintln(out, "Waiting for Argo CD to be Available (timeout 5m)...")
	for _, workload := range []string{"deployment/argocd-server", "deployment/argocd-repo-server", "statefulset/argocd-application-controller"} {
		if err := clusterKubectl(ctx, kubeconfigPath, nil, out,
			"rollout", "status", workload, "-n", clusterArgoNamespace, "--timeout=5m"); err != nil {
			return "", fmt.Errorf("wait for %s: %w", workload, err)
		}
	}

	fmt.Fprintln(out, "Fetching initial admin password...")
	password, err := clusterArgoWaitAdminPassword(ctx, kubeconfigPath, 2*time.Minute)
	if err != nil {
		return "", err
	}
	return password, nil
}

// clusterArgoExposeNodePort switches argocd-server to plain HTTP on a NodePort,
// for a kind cluster whose node has that port mapped to the host. Argo CD is
// then reachable at http://localhost:<nodePort> without a port-forward.
func clusterArgoExposeNodePort(ctx context.Context, kubeconfigPath string, nodePort int, out io.Writer) error {
	fmt.Fprintln(out, "Patching argocd-cmd-params-cm (server.insecure=true)...")
	if err := clusterKubectl(ctx, kubeconfigPath, nil, out,
		"patch", "configmap", "argocd-cmd-params-cm", "-n", clusterArgoNamespace,
		"--type", "merge", "-p", `{"data":{"server.insecure":"true"}}`); err != nil {
		return err
	}

	fmt.Fprintf(out, "Patching argocd-server Service to NodePort %d...\n", nodePort)
	svcPatch := fmt.Sprintf(
		`{"spec":{"type":"NodePort","ports":[{"name":"http","port":80,"targetPort":8080,"protocol":"TCP","nodePort":%d}]}}`,
		nodePort)
	if err := clusterKubectl(ctx, kubeconfigPath, nil, out,
		"patch", "service", "argocd-server", "-n", clusterArgoNamespace,
		"--type", "merge", "-p", svcPatch); err != nil {
		return err
	}

	// Rollout-restart argocd-server so it picks up server.insecure=true and
	// stops re-running its readiness probe against HTTPS.
	fmt.Fprintln(out, "Restarting argocd-server to pick up cmd-params change...")
	return clusterKubectl(ctx, kubeconfigPath, nil, out,
		"rollout", "restart", "deployment", "argocd-server", "-n", clusterArgoNamespace)
}

// clusterArgoEnsureIgnoreDifferences applies the argocd-cm ignoreDifferences
// block for Application resources unless the ConfigMap already carries one. An
// existing entry is left alone even though it may not cover ConfigHub's
// annotations: overwriting it would silently discard whatever its author put
// there, and the symptom of leaving it (the root Application shows OutOfSync)
// is visible and named in the output.
func clusterArgoEnsureIgnoreDifferences(ctx context.Context, kubeconfigPath string, out io.Writer) error {
	raw, err := clusterKubectlOutput(ctx, kubeconfigPath,
		"get", "configmap", "argocd-cm", "-n", clusterArgoNamespace, "--ignore-not-found", "-o", "json")
	if err != nil {
		return fmt.Errorf("read argocd-cm: %w", err)
	}
	if len(bytes.TrimSpace(raw)) > 0 {
		var cm struct {
			Data map[string]string `json:"data"`
		}
		if err := json.Unmarshal(raw, &cm); err != nil {
			return fmt.Errorf("decode argocd-cm: %w", err)
		}
		if existing := cm.Data[clusterArgoIgnoreDifferencesKey]; existing != "" {
			if existing == clusterArgoIgnoreDifferencesValue {
				return nil
			}
			fmt.Fprintf(out, "argocd-cm already sets %s; leaving it as is. If the root Application\n"+
				"reports OutOfSync, add the confighub.com/* Application annotations to that block.\n",
				clusterArgoIgnoreDifferencesKey)
			return nil
		}
	}
	fmt.Fprintln(out, "Patching argocd-cm (ignoreDifferences for self-referencing root Application)...")
	return clusterKubectl(ctx, kubeconfigPath, nil, out,
		"patch", "configmap", "argocd-cm", "-n", clusterArgoNamespace,
		"--type", "merge", "-p", clusterArgoIgnoreDifferencesPatch)
}

// clusterArgoExistingOCICredsURL returns the registry URL of the repo-creds
// Secret an earlier attachment left in the cluster, or "" when there is none.
// Its presence means the cluster is already wired to a ConfigHub: a cluster
// holds one such Secret, and attaching it twice would leave two sets of
// ConfigHub objects claiming the same cluster.
func clusterArgoExistingOCICredsURL(ctx context.Context, kubeconfigPath string) (string, error) {
	raw, err := clusterKubectlOutput(ctx, kubeconfigPath,
		"get", "secret", clusterArgoOCICredsSecret, "-n", clusterArgoNamespace, "--ignore-not-found",
		"-o", "jsonpath={.data.url}")
	if err != nil {
		return "", fmt.Errorf("check for an existing %s Secret: %w", clusterArgoOCICredsSecret, err)
	}
	if len(bytes.TrimSpace(raw)) == 0 {
		return "", nil
	}
	decoded, err := base64.StdEncoding.DecodeString(string(bytes.TrimSpace(raw)))
	if err != nil {
		return "", fmt.Errorf("decode the %s Secret's url: %w", clusterArgoOCICredsSecret, err)
	}
	return string(decoded), nil
}

// clusterArgoApplyOCIRepoCreds installs the Argo repo-creds Secret pointing at
// the ConfigHub OCI registry. Argo uses prefix-matching to pair this with any
// Application whose source.repoURL falls under ociURL.
//
// When insecureHTTP is true the Secret carries Argo's insecureOCIForceHttp flag,
// telling the repo-server to use plain HTTP against the OCI registry. Needed
// when wired up against a local cub server (http://localhost...) whose OCI
// endpoint is also plain HTTP. No effect for production https endpoints.
//
// The credential is a long-lived token of the cluster's service account,
// presented the way ConfigHub's registry takes any of its own tokens: as the
// password, with the fixed username "token".
func clusterArgoApplyOCIRepoCreds(ctx context.Context, kubeconfigPath, ociURL, pullToken string, insecureHTTP bool, out io.Writer) error {
	extra := ""
	if insecureHTTP {
		extra = "  insecureOCIForceHttp: \"true\"\n"
	}
	secret := fmt.Sprintf(`apiVersion: v1
kind: Secret
metadata:
  name: %s
  namespace: %s
  labels:
    argocd.argoproj.io/secret-type: repo-creds
type: Opaque
stringData:
  type: oci
  url: %s
  enableOCI: "true"
%s  username: token
  password: %s
`, clusterArgoOCICredsSecret, clusterArgoNamespace, ociURL, extra, pullToken)
	fmt.Fprintln(out, "Applying confighub-oci-creds repo-creds Secret to argocd namespace...")
	return clusterKubectlApply(ctx, kubeconfigPath, []byte(secret), out)
}

// clusterArgoRootAppManifest returns the YAML for the root "app-of-apps"
// Application. It self-references the same Space's Release bundle, so adding
// new Application Units to the Space and publishing a new Release causes Argo
// to create the corresponding Apps on its next sync. A Release bundle is flat —
// one <unit-slug>.yaml per bundled Unit at the bundle root — so path is ".".
//
// Without prune, an Application Unit removed from the Space leaves its
// Application running in the cluster, so a bad or empty Release can never
// delete one. With prune, the root deletes it.
func clusterArgoRootAppManifest(spaceSlug, ociRepoURL string, prune bool) []byte {
	pruneLine := ""
	if prune {
		pruneLine = "      prune: true\n"
	}
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
%s    syncOptions:
      - ServerSideApply=true
      - ServerSideApply.ForceConflicts=true
      - RespectIgnoreDifferences=true
      - CreateNamespace=false
`, spaceSlug, clusterArgoNamespace, ociRepoURL, clusterArgoNamespace, pruneLine)
}

// clusterArgoDeleteApplication deletes one Argo CD Application, reporting
// whether it existed. With cascade, Argo's resources-finalizer is set first so
// Argo deletes what the Application deployed; without it, finalizers are
// stripped so the delete orphans those resources.
func clusterArgoDeleteApplication(ctx context.Context, kubeconfigPath, name string, cascade bool, out io.Writer) (bool, error) {
	exists, err := clusterArgoSetCascade(ctx, kubeconfigPath, name, cascade)
	if err != nil || !exists {
		return exists, err
	}
	deleteArgs := []string{"delete", "application", name, "-n", clusterArgoNamespace, "--ignore-not-found", "--cascade=orphan"}
	verb := "orphaning its resources"
	if cascade {
		deleteArgs = []string{"delete", "application", name, "-n", clusterArgoNamespace, "--ignore-not-found", "--timeout=5m"}
		verb = "and its resources"
	}
	fmt.Fprintf(out, "  deleting Application %q (%s)...\n", name, verb)
	if _, err := clusterKubectlOutput(ctx, kubeconfigPath, deleteArgs...); err != nil {
		return true, fmt.Errorf("delete Application %q: %w", name, err)
	}
	return true, nil
}

// clusterArgoSetCascade sets an Application's finalizers so that deleting it,
// by kubectl or by a root Application pruning it, deletes what it deployed
// (cascade) or leaves it running. It reports whether the Application exists.
// The finalizers are among the fields argocd-cm tells Argo to ignore, so the
// root's self-heal does not put the generated ones back.
func clusterArgoSetCascade(ctx context.Context, kubeconfigPath, name string, cascade bool) (bool, error) {
	existing, err := clusterKubectlOutput(ctx, kubeconfigPath,
		"get", "application", name, "-n", clusterArgoNamespace, "--ignore-not-found", "-o", "name")
	if err != nil {
		return false, fmt.Errorf("look up Application %q: %w", name, err)
	}
	if len(bytes.TrimSpace(existing)) == 0 {
		return false, nil
	}
	finalizers := `{"metadata":{"finalizers":[]}}`
	if cascade {
		finalizers = `{"metadata":{"finalizers":["resources-finalizer.argocd.argoproj.io"]}}`
	}
	if _, err := clusterKubectlOutput(ctx, kubeconfigPath,
		"patch", "application", name, "-n", clusterArgoNamespace, "--type=merge", "-p", finalizers); err != nil {
		return true, fmt.Errorf("set finalizers on Application %q: %w", name, err)
	}
	return true, nil
}

// clusterArgoWaitRootReleased waits until the root Application no longer wants
// the child Application name: its tracked resources either drop it (the root
// has prune and deleted it) or mark it requiresPruning (the root synced a
// Release without it and, lacking prune, left it). Until then the root's
// self-heal re-creates a child deleted from under it, so a child is deleted
// only after this returns. A hard refresh is requested first so the root
// re-reads its Release now rather than on its next poll.
func clusterArgoWaitRootReleased(ctx context.Context, kubeconfigPath, rootName, name string, budget time.Duration) error {
	if _, err := clusterKubectlOutput(ctx, kubeconfigPath,
		"annotate", "application", rootName, "-n", clusterArgoNamespace, "argocd.argoproj.io/refresh=hard", "--overwrite"); err != nil {
		return fmt.Errorf("refresh root Application %q: %w", rootName, err)
	}
	deadline := time.Now().Add(budget)
	for {
		raw, err := clusterKubectlOutput(ctx, kubeconfigPath,
			"get", "application", rootName, "-n", clusterArgoNamespace, "-o", "json")
		if err != nil {
			return fmt.Errorf("read root Application %q: %w", rootName, err)
		}
		var root struct {
			Status struct {
				Resources []struct {
					Kind            string `json:"kind"`
					Name            string `json:"name"`
					RequiresPruning bool   `json:"requiresPruning"`
				} `json:"resources"`
			} `json:"status"`
		}
		if err := json.Unmarshal(raw, &root); err != nil {
			return fmt.Errorf("decode root Application %q: %w", rootName, err)
		}
		wanted := false
		for _, r := range root.Status.Resources {
			if r.Kind == "Application" && r.Name == name && !r.RequiresPruning {
				wanted = true
				break
			}
		}
		if !wanted {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("root Application %q still deploys Application %q after %s; it has not synced the Release without it", rootName, name, budget)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(2 * time.Second):
		}
	}
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

// clusterArgoIgnoreDifferencesValue tells Argo to ignore the Argo + ConfigHub
// bookkeeping annotations on argoproj.io_Application resources. Without it,
// the self-referencing root Application is perpetually OutOfSync.
const clusterArgoIgnoreDifferencesValue = `jqPathExpressions:
- '.metadata.annotations."kubectl.kubernetes.io/last-applied-configuration"'
- '.metadata.annotations."confighub.com/origin"'
- '.metadata.annotations."confighub.com/RevisionNum"'
- '.metadata.annotations."confighub.com/SpaceID"'
- '.metadata.annotations."confighub.com/UnitSlug"'
- '.metadata.annotations."confighub.com/ResourceMergeID"'
- '.metadata.annotations."argocd.argoproj.io/tracking-id"'
- '.metadata.finalizers'
`

// clusterArgoIgnoreDifferencesPatch is the merge patch for argocd-cm that
// installs clusterArgoIgnoreDifferencesValue.
var clusterArgoIgnoreDifferencesPatch = func() string {
	patch := map[string]any{
		"data": map[string]any{
			clusterArgoIgnoreDifferencesKey: clusterArgoIgnoreDifferencesValue,
		},
	}
	b, _ := json.Marshal(patch)
	return string(b)
}()
