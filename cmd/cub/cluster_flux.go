// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/url"

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
	// clusterFluxVersion pins the Flux release.
	clusterFluxVersion = "v2.9.2"

	// clusterFluxManifestURL is the release's install manifest, applied with
	// kubectl so `cub` does not need the flux CLI.
	clusterFluxManifestURL = "https://github.com/fluxcd/flux2/releases/download/" + clusterFluxVersion + "/install.yaml"

	// clusterFluxNamespace is where Flux runs, and where the OCIRepositories,
	// Kustomizations and their registry credentials live.
	clusterFluxNamespace = "flux-system"

	// clusterFluxCredsSecret holds the worker credentials every OCIRepository's
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

	// clusterFluxTimeout bounds a Kustomization's apply and health check. With
	// wait, a broken app reports Progressing for this long before Degraded.
	clusterFluxTimeout = "2m0s"
)

// clusterFluxInstall applies the Flux install manifest and waits for the two
// controllers `cub cluster up` relies on, source-controller and
// kustomize-controller, to be Available. The manifest also installs Flux's
// helm, notification and image controllers, which nothing here uses.
func clusterFluxInstall(ctx context.Context, kubeconfigPath string, out io.Writer) error {
	fmt.Fprintf(out, "Applying Flux install manifest from %s...\n", clusterFluxManifestURL)
	if err := clusterKubectl(ctx, kubeconfigPath, nil, out,
		"apply", "-f", clusterFluxManifestURL); err != nil {
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
func clusterFluxChildManifest(name, ociRepoURL, pollInterval string, insecure bool) []byte {
	return clusterFluxManifest(name, ociRepoURL, pollInterval, insecure, false)
}

// clusterFluxRootManifest returns the YAML for the root OCIRepository and
// Kustomization. They pull the apps Space's Release, which includes the root
// itself, so adding a child Unit to the apps Space and publishing a new Release
// makes Flux create that child, and removing one makes Flux prune it.
//
// The root objects are marked kustomize.toolkit.fluxcd.io/prune: disabled, so a
// Release without them never makes Flux delete the root. The root does not wait
// on what it applied: it would health-check the child Kustomizations, so one
// failing child would fail the root and hold back its next apply until the
// check timed out.
func clusterFluxRootManifest(ociRepoURL, pollInterval string, insecure bool) []byte {
	return clusterFluxManifest(clusterFluxRootName, ociRepoURL, pollInterval, insecure, true)
}

// clusterFluxManifest renders an OCIRepository pulling ociRepoURL's latest
// Release, and the Kustomization applying it. Flux takes the tag from ref.tag,
// so ociRepoURL carries none. The Kustomization's interval only corrects drift;
// pollInterval is how often the OCIRepository looks for a new Release.
func clusterFluxManifest(name, ociRepoURL, pollInterval string, insecure, root bool) []byte {
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
  prune: true
  wait: %[9]t
`, name, clusterFluxNamespace, annotations, pollInterval, ociRepoURL, insecure,
		clusterFluxCredsSecret, clusterFluxTimeout, wait)
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
