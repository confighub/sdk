// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"time"

	goclientnew "github.com/confighub/sdk/core/openapi/goclient-new"
)

// Flux install + configuration inside a kind cluster, so it can pull config
// from ConfigHub's OCI registry. The `cub cluster up --controller flux`
// counterpart of cluster_sveltos.go: all operations shell out to kubectl using a
// per-cluster kubeconfig.
//
// A deployment is an OCIRepository, which pulls a Space's Release, plus a
// Kustomization, which applies it; both are named after the Space and live in
// one Unit. The root pair pulls the apps Space, whose Release holds the root
// itself and every deployment's pair, the self-managing layout `flux bootstrap`
// uses for flux-system.

const (
	// clusterFluxVersion pins the Flux release installed when
	// --controller-version names none.
	clusterFluxVersion = "v2.9.2"

	// clusterFluxNamespace is where Flux runs, and where the OCIRepositories,
	// Kustomizations and their registry credentials live.
	clusterFluxNamespace = "flux-system"

	// clusterFluxCredsSecret holds the registry credentials every OCIRepository's
	// secretRef names.
	clusterFluxCredsSecret = "confighub-oci"

	// clusterFluxRootName names the root OCIRepository and Kustomization.
	clusterFluxRootName = "confighub-root"

	// clusterFluxAppsSuffix is the Flux counterpart of clusterArgoAppsSuffix: the
	// apps Space holding the root and child Flux Units.
	clusterFluxAppsSuffix = "-flux-apps"

	// clusterFluxPollIntervalWithoutArgobot is the OCIRepositories' poll interval
	// when polling is the only way a publish reaches the cluster.
	clusterFluxPollIntervalWithoutArgobot = "1m0s"

	// clusterFluxPollIntervalWithArgobot is the OCIRepositories' poll interval
	// when argobot is installed. argobot requests a reconciliation of a Space's
	// OCIRepository on each publish, so polling only catches what argobot missed.
	clusterFluxPollIntervalWithArgobot = "10m0s"

	// clusterFluxFieldManager is the field manager cub patches Kustomizations
	// as, which Flux leaves alone (see clusterFluxSetCascade).
	clusterFluxFieldManager = "cub"

	// clusterFluxTimeout bounds a Kustomization's apply and health check. With
	// wait, a broken app reports Progressing for this long before Degraded.
	clusterFluxTimeout = "2m0s"
)

// clusterFluxManifestURL returns a release's install manifest, applied with
// kubectl so `cub` does not need the flux CLI.
func clusterFluxManifestURL(version string) string {
	return "https://github.com/fluxcd/flux2/releases/download/" + version + "/install.yaml"
}

// clusterFluxInstall applies the Flux install manifest and waits for the two
// controllers `cub cluster up` relies on, source-controller and
// kustomize-controller, to be Available. The manifest also installs Flux's
// helm, notification and image controllers, which nothing here uses.
func clusterFluxInstall(ctx context.Context, kubeconfigPath, version string, out io.Writer) error {
	if version == "" {
		version = clusterFluxVersion
	}
	manifestURL := clusterFluxManifestURL(version)
	fmt.Fprintf(out, "Applying Flux install manifest from %s...\n", manifestURL)
	if err := clusterKubectl(ctx, kubeconfigPath, nil, out,
		"apply", "-f", manifestURL); err != nil {
		return fmt.Errorf("apply flux install: %w", err)
	}

	fmt.Fprintln(out, "Waiting for source-controller and kustomize-controller to be Available (timeout 5m)...")
	if err := clusterKubectl(ctx, kubeconfigPath, nil, out,
		"wait", "--for=condition=Available", "--timeout=5m", "-n", clusterFluxNamespace,
		"deployment/source-controller", "deployment/kustomize-controller"); err != nil {
		return fmt.Errorf("wait for flux: %w", err)
	}
	return nil
}

// clusterFluxEnsure makes sure Flux is in the cluster: installs it if absent
// and installIfAbsent, and reports whether it did. A pre-existing Flux is left
// as found. version is the release to install; "" installs clusterFluxVersion.
func clusterFluxEnsure(ctx context.Context, kubeconfigPath, version string, installIfAbsent bool, out io.Writer) (bool, error) {
	present, err := clusterFluxPresent(ctx, kubeconfigPath)
	if err != nil {
		return false, err
	}
	if present {
		fmt.Fprintf(out, "Flux already present in namespace %q; leaving it in place.\n", clusterFluxNamespace)
		return false, nil
	}
	if !installIfAbsent {
		return false, fmt.Errorf("Flux not found in namespace %q (no kustomize-controller Deployment) and --no-install was given", clusterFluxNamespace)
	}
	if err := clusterFluxInstall(ctx, kubeconfigPath, version, out); err != nil {
		return false, err
	}
	return true, nil
}

// clusterFluxPresent reports whether a kustomize-controller Deployment exists
// in the Flux namespace. A missing namespace or Deployment is "absent"; any
// other kubectl failure is an error, as for clusterArgoPresent.
func clusterFluxPresent(ctx context.Context, kubeconfigPath string) (bool, error) {
	out, err := clusterKubectlOutput(ctx, kubeconfigPath,
		"get", "deployment", "kustomize-controller", "-n", clusterFluxNamespace, "--ignore-not-found", "-o", "name")
	if err != nil {
		return false, fmt.Errorf("check for an existing Flux: %w", err)
	}
	return len(bytes.TrimSpace(out)) > 0, nil
}

// clusterFluxExistingOCICredsHost returns the registry host of the credentials
// Secret an earlier attachment left in the cluster, or "" when there is none:
// the Flux counterpart of clusterArgoExistingOCICredsURL.
func clusterFluxExistingOCICredsHost(ctx context.Context, kubeconfigPath string) (string, error) {
	raw, err := clusterKubectlOutput(ctx, kubeconfigPath,
		"get", "secret", clusterFluxCredsSecret, "-n", clusterFluxNamespace, "--ignore-not-found",
		"-o", `jsonpath={.data.\.dockerconfigjson}`)
	if err != nil {
		return "", fmt.Errorf("check for an existing %s Secret: %w", clusterFluxCredsSecret, err)
	}
	if len(bytes.TrimSpace(raw)) == 0 {
		return "", nil
	}
	return clusterFluxCredsHost(bytes.TrimSpace(raw))
}

// clusterFluxCredsHost returns the registry host a base64-encoded
// .dockerconfigjson holds credentials for.
func clusterFluxCredsHost(encoded []byte) (string, error) {
	decoded, err := base64.StdEncoding.DecodeString(string(encoded))
	if err != nil {
		return "", fmt.Errorf("decode the %s Secret: %w", clusterFluxCredsSecret, err)
	}
	var cfg struct {
		Auths map[string]json.RawMessage `json:"auths"`
	}
	if err := json.Unmarshal(decoded, &cfg); err != nil {
		return "", fmt.Errorf("decode the %s Secret: %w", clusterFluxCredsSecret, err)
	}
	for host := range cfg.Auths {
		return host, nil
	}
	// A Secret by that name with no registry in it still marks an attachment.
	return clusterFluxCredsSecret, nil
}

// clusterFluxCredsManifest returns the dockerconfigjson Secret every
// OCIRepository's secretRef names: a long-lived token of the cluster's service
// account as the password for the registry at ociURL, under the username
// "token" the registry accepts it with. Flux looks credentials up by the
// host:port of the OCIRepository's URL, so ociURL has to be the one the
// OCIRepositories pull from.
func clusterFluxCredsManifest(ociURL, pullToken string) ([]byte, error) {
	u, err := url.Parse(ociURL)
	if err != nil {
		return nil, fmt.Errorf("parse %q: %w", ociURL, err)
	}
	if u.Host == "" {
		return nil, fmt.Errorf("no host in %q", ociURL)
	}
	auth := map[string]string{
		"username": "token",
		"password": pullToken,
		"auth":     base64.StdEncoding.EncodeToString([]byte("token:" + pullToken)),
	}
	dockerConfig, err := json.Marshal(map[string]any{"auths": map[string]any{u.Host: auth}})
	if err != nil {
		return nil, err
	}
	return json.Marshal(map[string]any{
		"apiVersion": "v1",
		"kind":       "Secret",
		"metadata":   map[string]string{"name": clusterFluxCredsSecret, "namespace": clusterFluxNamespace},
		"type":       "kubernetes.io/dockerconfigjson",
		"stringData": map[string]string{".dockerconfigjson": string(dockerConfig)},
	})
}

// clusterFluxApplyOCICreds installs the Secret from clusterFluxCredsManifest.
func clusterFluxApplyOCICreds(ctx context.Context, kubeconfigPath, ociURL, pullToken string, out io.Writer) error {
	secret, err := clusterFluxCredsManifest(ociURL, pullToken)
	if err != nil {
		return err
	}
	fmt.Fprintf(out, "Applying %s Secret to the %s namespace...\n", clusterFluxCredsSecret, clusterFluxNamespace)
	return clusterKubectlApply(ctx, kubeconfigPath, secret, out)
}

// clusterFluxChildManifest returns the YAML for the OCIRepository and
// Kustomization that deploy a deployment Space's Release to the cluster: the
// Flux counterpart of variantArgoAppManifest. Both are named after the Space,
// which is how argobot finds the OCIRepository to reconcile when that Space is
// published. wait makes the Kustomization's Healthy condition cover the health
// of what it applied, which is what argobot reports.
//
// Without prune, Flux never deletes what the Kustomization applied: a resource
// that leaves the Release stays running, and so does everything when the
// Kustomization itself is deleted. With prune, both are deleted, so an empty
// Release deletes the deployment.
func clusterFluxChildManifest(name, ociRepoURL, pollInterval string, insecure, prune bool) []byte {
	return clusterFluxManifest(name, ociRepoURL, pollInterval, insecure, false, prune)
}

// clusterFluxRootManifest returns the YAML for the root OCIRepository and
// Kustomization. They pull the apps Space's Release, which includes the root
// itself, so adding a child Unit to the apps Space and publishing a new Release
// makes Flux create that child. Removing one makes Flux delete it only with
// prune; without, the child keeps running, so a bad or empty Release can never
// delete a deployment.
//
// The root objects are marked kustomize.toolkit.fluxcd.io/prune: disabled, so a
// Release without them never makes Flux delete the root. The root does not wait
// on what it applied: it would health-check the child Kustomizations, so one
// failing child would fail the root and hold back its next apply until the
// check timed out.
func clusterFluxRootManifest(ociRepoURL, pollInterval string, insecure, prune bool) []byte {
	return clusterFluxManifest(clusterFluxRootName, ociRepoURL, pollInterval, insecure, true, prune)
}

// clusterFluxManifest renders an OCIRepository pulling ociRepoURL's latest
// Release, and the Kustomization applying it. Flux takes the tag from ref.tag,
// so ociRepoURL carries none. The Kustomization's interval only corrects drift;
// pollInterval is how often the OCIRepository looks for a new Release.
func clusterFluxManifest(name, ociRepoURL, pollInterval string, insecure, root, prune bool) []byte {
	annotations, wait := "", true
	if root {
		annotations = "\n  annotations:\n    kustomize.toolkit.fluxcd.io/prune: disabled"
		wait = false
	}
	return fmt.Appendf(nil, `apiVersion: source.toolkit.fluxcd.io/v1
kind: OCIRepository
metadata:
  name: %[1]s
  namespace: %[2]s%[3]s
spec:
  interval: %[4]s
  url: %[5]s
  ref:
    tag: latest
  insecure: %[6]t
  secretRef:
    name: %[7]s
---
apiVersion: kustomize.toolkit.fluxcd.io/v1
kind: Kustomization
metadata:
  name: %[1]s
  namespace: %[2]s%[3]s
spec:
  interval: 10m0s
  retryInterval: 1m0s
  timeout: %[8]s
  sourceRef:
    kind: OCIRepository
    name: %[1]s
  path: "."
  prune: %[10]t
  wait: %[9]t
`, name, clusterFluxNamespace, annotations, pollInterval, ociRepoURL, insecure,
		clusterFluxCredsSecret, clusterFluxTimeout, wait, prune)
}

// clusterFluxSetCascade sets a deployment's Kustomization so that deleting it,
// by kubectl or by a root Kustomization pruning it, deletes what it applied
// (cascade: prune on, not suspended) or leaves it running (suspended: Flux
// skips garbage collection when a suspended Kustomization is deleted). It
// reports whether the Kustomization exists. The root's apply resets prune to
// what the Unit says while the root still applies the child. The patch is made
// as cub's own field manager: Flux's server-side apply takes back fields owned
// by kubectl's managers (kubectl-patch among them), so a root that applies the
// child again before it applies a Release without it would otherwise clear
// suspend and let the child prune.
func clusterFluxSetCascade(ctx context.Context, kubeconfigPath, name string, cascade bool) (bool, error) {
	existing, err := clusterKubectlOutput(ctx, kubeconfigPath,
		"get", "kustomization", name, "-n", clusterFluxNamespace, "--ignore-not-found", "-o", "name")
	if err != nil {
		return false, fmt.Errorf("look up Kustomization %q: %w", name, err)
	}
	if len(bytes.TrimSpace(existing)) == 0 {
		return false, nil
	}
	patch := `{"spec":{"suspend":true}}`
	if cascade {
		patch = `{"spec":{"prune":true,"suspend":false}}`
	}
	if _, err := clusterKubectlOutput(ctx, kubeconfigPath,
		"patch", "kustomization", name, "-n", clusterFluxNamespace, "--type=merge",
		"--field-manager="+clusterFluxFieldManager, "-p", patch); err != nil {
		return true, fmt.Errorf("set prune and suspend on Kustomization %q: %w", name, err)
	}
	return true, nil
}

// clusterFluxWaitRootReleased waits until the root Kustomization no longer
// applies the deployment's Kustomization: its inventory, the objects it applied
// from the current Release, drops it. A root with prune has deleted it by then;
// one without has let go of it, so a delete is not undone by the root's next
// apply. A reconciliation of the root's OCIRepository is requested first so the
// root fetches the new Release now rather than on its next poll.
func clusterFluxWaitRootReleased(ctx context.Context, kubeconfigPath, name string, budget time.Duration) error {
	requestedAt := "reconcile.fluxcd.io/requestedAt=" + time.Now().UTC().Format(time.RFC3339Nano)
	for _, kind := range []string{"ocirepository", "kustomization"} {
		if _, err := clusterKubectlOutput(ctx, kubeconfigPath,
			"annotate", kind, clusterFluxRootName, "-n", clusterFluxNamespace, requestedAt, "--overwrite"); err != nil {
			return fmt.Errorf("request a reconciliation of the root %s: %w", kind, err)
		}
	}
	// Flux identifies an inventory entry as <namespace>_<name>_<group>_<kind>.
	childID := clusterFluxNamespace + "_" + name + "_kustomize.toolkit.fluxcd.io_Kustomization"
	deadline := time.Now().Add(budget)
	for {
		raw, err := clusterKubectlOutput(ctx, kubeconfigPath,
			"get", "kustomization", clusterFluxRootName, "-n", clusterFluxNamespace, "-o", "json")
		if err != nil {
			return fmt.Errorf("read the root Kustomization: %w", err)
		}
		var root struct {
			Status struct {
				Inventory struct {
					Entries []struct {
						ID string `json:"id"`
					} `json:"entries"`
				} `json:"inventory"`
			} `json:"status"`
		}
		if err := json.Unmarshal(raw, &root); err != nil {
			return fmt.Errorf("decode the root Kustomization: %w", err)
		}
		applied := false
		for _, e := range root.Status.Inventory.Entries {
			if e.ID == childID {
				applied = true
				break
			}
		}
		if !applied {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("root Kustomization %q still applies Kustomization %q after %s; it has not applied the Release without it", clusterFluxRootName, name, budget)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(2 * time.Second):
		}
	}
}

// clusterFluxDeleteDeployment deletes a deployment's Kustomization and
// OCIRepository, after setting the Kustomization's cascade (see
// clusterFluxSetCascade). With cascade the delete waits for Flux to delete what
// the Kustomization applied. It reports whether the Kustomization existed.
func clusterFluxDeleteDeployment(ctx context.Context, kubeconfigPath, name string, cascade bool, out io.Writer) (bool, error) {
	exists, err := clusterFluxSetCascade(ctx, kubeconfigPath, name, cascade)
	if err != nil {
		return false, err
	}
	if exists {
		verb := "orphaning what it applied"
		if cascade {
			verb = "and what it applied"
		}
		fmt.Fprintf(out, "  deleting Kustomization %q (%s)...\n", name, verb)
		if _, err := clusterKubectlOutput(ctx, kubeconfigPath,
			"delete", "kustomization", name, "-n", clusterFluxNamespace, "--ignore-not-found", "--timeout=5m"); err != nil {
			return true, fmt.Errorf("delete Kustomization %q: %w", name, err)
		}
	}
	if _, err := clusterKubectlOutput(ctx, kubeconfigPath,
		"delete", "ocirepository", name, "-n", clusterFluxNamespace, "--ignore-not-found"); err != nil {
		return exists, fmt.Errorf("delete OCIRepository %q: %w", name, err)
	}
	return exists, nil
}

// clusterFluxPollInterval returns the OCIRepositories' poll interval for a
// cluster: infrequent when argobot requests reconciliations, every minute when
// polling is all there is.
func clusterFluxPollInterval(argobot bool) string {
	if argobot {
		return clusterFluxPollIntervalWithArgobot
	}
	return clusterFluxPollIntervalWithoutArgobot
}

// clusterFluxPollIntervalOf returns the poll interval `cub cluster up` recorded
// on a Flux cluster's target, so a child OCIRepository created later polls like
// the root does.
func clusterFluxPollIntervalOf(target *goclientnew.Target) string {
	if interval := target.Annotations[clusterAnnotationFluxPollInterval]; interval != "" {
		return interval
	}
	return clusterFluxPollIntervalWithoutArgobot
}

// clusterFluxStatusCommand is the command `cub cluster up` and `cub cluster
// open` suggest for a Flux cluster's state, since Flux has no UI.
func clusterFluxStatusCommand() string {
	return "kubectl -n " + clusterFluxNamespace + " get ocirepository,kustomization"
}
