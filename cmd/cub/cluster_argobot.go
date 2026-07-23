// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"fmt"
	"io"
	"strings"

	"github.com/google/uuid"
)

// argobot install for `cub cluster up`. argobot is a ConfigHub bot that watches
// the event log and force-syncs the corresponding Argo CD Application the moment
// a deploy happens, closing Argo's reconcile-interval gap. It is delivered like
// any other component: a shared base installed from its published OCI config
// bundle, a per-cluster downstream variant, and a child Argo Application in the
// cluster Space that the root app-of-apps picks up.
//
// Everything argobot-specific lives in this file; the rest of the cluster
// package is generic (kind, Argo CD, ConfigHub Spaces/Workers/Targets). The one
// coupling into `cub cluster up` is clusterInstallArgobotStep, called once from
// clusterUpRun.

// argobot install constants. argobot is installed as a ConfigHub component from
// its published OCI config bundle, then given a per-cluster downstream variant.
const (
	// clusterArgobotComponent is the well-known "Component" label value and the
	// base Space is <component>-base.
	clusterArgobotComponent = "argobot"
	// clusterArgobotBaseVariant is the "Variant" label of the shared base Space.
	clusterArgobotBaseVariant = "base"
	// clusterArgobotBaseSpace is the shared base Space slug (one per hub, reused
	// across clusters).
	clusterArgobotBaseSpace = "argobot-base"
	// clusterArgobotOCIRef is the default OCI reference of argobot's config
	// bundle (its Kubernetes manifests), overridable with --argobot-oci.
	clusterArgobotOCIRef = "oci://ghcr.io/confighub/configs/argobot"
	// clusterArgobotNamespace is the namespace argobot's manifests deploy into.
	clusterArgobotNamespace = "argobot"
	// clusterArgobotUnitSlug is the per-file Unit slug for argobot's single
	// manifest file (manifests/argobot.yaml → Unit "argobot").
	clusterArgobotUnitSlug = "argobot"
	// clusterArgobotContainer is the container name in argobot's Deployment.
	clusterArgobotContainer = "argobot"
	// clusterArgobotSecret is the out-of-band Secret argobot reads its worker
	// credentials from (never shipped in the OCI bundle).
	clusterArgobotSecret = "argobot-secrets"
)

// clusterArgobotDeps carries the cluster-derived values the argobot install
// needs from clusterUpRun — the pieces of a freshly created cluster argobot
// binds to.
type clusterArgobotDeps struct {
	spaceID        uuid.UUID // the cluster Space (holds the Argo Application Units)
	targetID       uuid.UUID // the cluster's OCI target
	workerID       uuid.UUID // the cluster's oci-worker (argobot reuses it as its identity)
	workerSecret   string    // the oci-worker secret (goes into the out-of-band Secret)
	ociURLForArgo  string    // OCI endpoint as Argo (in-cluster) reaches it
	kubeconfigPath string
}

// clusterInstallArgobotStep is the single argobot seam into `cub cluster up`.
// It is a no-op returning uuid.Nil when --no-argobot is set; otherwise it
// registers the argobot variant Space for rollback (via registerRollback),
// derives argobot's in-cluster ConfigHub URL, and runs the install, returning
// the child Argo Application Unit ID the caller adds to the cluster Space's
// Release.
func clusterInstallArgobotStep(out io.Writer, opts clusterUpOptions, dep clusterArgobotDeps, registerRollback func(func())) (uuid.UUID, error) {
	if opts.noArgobot {
		return uuid.Nil, nil
	}

	// Register the per-cluster argobot variant Space for rollback before the
	// install runs: a partial failure can leave it with a cross-space
	// release-target reference that would otherwise block deleting the cluster's
	// OCI target. Resolve by slug at rollback time (it may not exist yet). The
	// shared argobot-base Space is intentionally left alone.
	argobotSlug := clusterArgobotComponent + "-" + opts.name
	registerRollback(func() {
		sp, err := apiGetSpaceFromSlug(argobotSlug, "SpaceID")
		if err != nil {
			return // never created
		}
		fmt.Fprintf(out, "Rolling back: cub space delete --recursive %q\n", argobotSlug)
		_ = clusterClearReleaseTarget(sp.SpaceID)
		_ = clusterDeleteSpace(argobotSlug, true)
	})

	// ConfigHub API URL as argobot (in-cluster) reaches it — loopback rewritten
	// to host.docker.internal.
	apiURLForContainer, err := clusterAPIEndpointFromContainer(clusterServerURL())
	if err != nil {
		return uuid.Nil, err
	}

	fmt.Fprintln(out, "\nInstalling argobot (event-driven Argo CD sync)...")
	return clusterInstallArgobot(out, clusterArgobotOptions{
		clusterName:        opts.name,
		clusterSpaceSlug:   opts.spaceSlug,
		appsSpaceID:        dep.spaceID,
		ociTargetRef:       opts.spaceSlug + "/" + clusterOCITargetSlug,
		ociTargetID:        dep.targetID,
		workerID:           dep.workerID.String(),
		workerSecret:       dep.workerSecret,
		ociURLForArgo:      dep.ociURLForArgo,
		apiURLForContainer: apiURLForContainer,
		ociRef:             opts.argobotOCIRef,
		kubeconfigPath:     dep.kubeconfigPath,
	})
}

// clusterArgobotSecretManifest returns the YAML for the out-of-band Secret that
// carries argobot's worker credentials. It is applied straight to the cluster
// via kubectl (never shipped in the OCI bundle) so credential material never
// enters a published Release. The Namespace is created alongside so the Secret
// has somewhere to land before Argo renders argobot's Deployment.
func clusterArgobotSecretManifest(workerID, workerSecret string) []byte {
	return fmt.Appendf(nil, `apiVersion: v1
kind: Namespace
metadata:
  name: %s
---
apiVersion: v1
kind: Secret
metadata:
  name: %s
  namespace: %s
type: Opaque
stringData:
  CONFIGHUB_WORKER_ID: %s
  CONFIGHUB_WORKER_SECRET: %s
`, clusterArgobotNamespace, clusterArgobotSecret, clusterArgobotNamespace, workerID, workerSecret)
}

// clusterArgobotOptions carries everything the argobot install needs from the
// surrounding `cub cluster up` run.
type clusterArgobotOptions struct {
	clusterName        string    // cluster name; also the argobot variant name
	clusterSpaceSlug   string    // the cluster Space slug
	appsSpaceID        uuid.UUID // the Space that holds the Argo Application Units (the cluster Space)
	ociTargetRef       string    // "<clusterSpaceSlug>/oci" — argobot's release + event target
	ociTargetID        uuid.UUID // the OCI target's ID
	workerID           string    // the cluster's oci-worker (argobot's identity)
	workerSecret       string    // the oci-worker secret (goes into the out-of-band Secret)
	ociURLForArgo      string    // OCI endpoint as Argo (in-cluster) reaches it
	apiURLForContainer string    // ConfigHub API URL as argobot (in-cluster) reaches it
	ociRef             string    // argobot config-bundle OCI reference
	kubeconfigPath     string
}

// clusterInstallArgobot installs argobot for a freshly created cluster: it
// reuses the cluster's server-hosted oci-worker as argobot's identity (that
// worker owns the OCI target, so event auto-scoping and the credential Secret
// are the whole story). The argobot Deployment runs in the default kubernetes
// sync mode — it patches an Application's refresh annotation via the in-cluster
// ServiceAccount, needing no Argo CD API token.
//
// It creates argobot's units and publishes argobot's own variant Release, then
// returns the ID of the child Argo Application Unit it created in the cluster
// Space. The caller publishes the cluster Space's Release once, after this
// returns, so that the root app-of-apps' first sync already includes argobot.
func clusterInstallArgobot(out io.Writer, o clusterArgobotOptions) (uuid.UUID, error) {
	argobotSpace := clusterArgobotComponent + "-" + o.clusterName

	// 1. Ensure the shared base component exists, installed from argobot's
	// published OCI config bundle. --granularity per-file keeps the single
	// manifest file as one Unit ("argobot"); --allow-exists makes it a no-op
	// when another cluster already installed the base.
	fmt.Fprintf(out, "Installing argobot base component from %s...\n", o.ociRef)
	if err := runCub("variant", "upload",
		"--component", clusterArgobotComponent,
		"--variant", clusterArgobotBaseVariant,
		"--granularity", "per-file",
		"--allow-exists",
		o.ociRef); err != nil {
		return uuid.Nil, fmt.Errorf("install argobot base component: %w", err)
	}

	// 2. Per-cluster downstream variant, cloned from the base and bound to this
	// cluster's OCI target (which also becomes the variant Space's release
	// target). No --namespace: argobot's manifests already place resources in
	// the argobot and argocd namespaces, and set-namespace is space-wide (it
	// would wrongly move the argocd-namespace RBAC).
	fmt.Fprintf(out, "Creating per-cluster argobot variant %q...\n", argobotSpace)
	if err := runCub("variant", "create",
		"--allow-exists",
		"--target", o.ociTargetRef,
		o.clusterName, clusterArgobotBaseSpace); err != nil {
		return uuid.Nil, fmt.Errorf("create argobot variant: %w", err)
	}

	space, err := apiGetSpaceFromSlug(argobotSpace, "SpaceID")
	if err != nil {
		return uuid.Nil, fmt.Errorf("resolve argobot variant space %q: %w", argobotSpace, err)
	}

	// 3. Point argobot at this ConfigHub. Its event subscription needs no
	// explicit target: argobot auto-discovers the Targets its worker is the
	// bridge for, and the oci-worker it reuses owns the cluster's OCI target.
	// set-env upserts literal env vars; the worker id/secret stay as
	// secretKeyRef (supplied out of band, step 5).
	fmt.Fprintln(out, "Configuring argobot (CONFIGHUB_URL)...")
	if err := runCub("function", "do", "--quiet",
		"--space", argobotSpace, "--unit", clusterArgobotUnitSlug,
		"set-env", clusterArgobotContainer,
		"CONFIGHUB_URL="+o.apiURLForContainer); err != nil {
		return uuid.Nil, fmt.Errorf("configure argobot env: %w", err)
	}

	// 4. Publish the variant's Release; Argo pulls argobot's workload from here.
	unit, err := apiGetUnitFromSlugInSpace(clusterArgobotUnitSlug, space.SpaceID.String(), "UnitID")
	if err != nil {
		return uuid.Nil, fmt.Errorf("resolve argobot unit: %w", err)
	}
	if err := clusterWaitUnitTriggers(space.SpaceID, unit.UnitID); err != nil {
		return uuid.Nil, err
	}
	fmt.Fprintf(out, "Publishing argobot variant Release (%s)...\n", argobotSpace)
	if err := clusterPublishRelease(space.SpaceID); err != nil {
		return uuid.Nil, err
	}

	// 5. Apply the out-of-band worker-credential Secret (and its Namespace) so
	// argobot's pod can authenticate. Kept out of the OCI bundle deliberately.
	fmt.Fprintf(out, "Applying argobot worker-credential Secret to the %q namespace...\n", clusterArgobotNamespace)
	if err := clusterKubectlApply(ctx, o.kubeconfigPath,
		clusterArgobotSecretManifest(o.workerID, o.workerSecret), out); err != nil {
		return uuid.Nil, fmt.Errorf("apply argobot secret: %w", err)
	}

	// 6. Child Argo Application Unit in the cluster Space (alongside root),
	// pulling argobot's variant Release. The caller publishes the cluster
	// Space's Release once, so this Unit lands in the first bundle the root
	// app-of-apps pulls.
	fmt.Fprintf(out, "Creating argobot Argo Application Unit in %q...\n", o.clusterSpaceSlug)
	childRepoURL := strings.TrimRight(o.ociURLForArgo, "/") + "/space/" + argobotSpace
	childManifest := clusterArgoChildAppManifest(argobotSpace, childRepoURL, clusterArgobotNamespace)
	childUnitID, err := clusterCreateK8sYAMLUnit(o.appsSpaceID, o.ociTargetID, argobotSpace, argobotSpace, childManifest)
	if err != nil {
		return uuid.Nil, fmt.Errorf("create argobot Argo Application unit: %w", err)
	}

	fmt.Fprintf(out, "argobot installed: variant %q, Argo app %q (kubernetes sync mode)\n",
		argobotSpace, argobotSpace)
	return childUnitID, nil
}
