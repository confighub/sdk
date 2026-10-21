// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"fmt"
	"io"
	"strings"

	goclientnew "github.com/confighub/sdk/core/openapi/goclient-new"
	"github.com/google/uuid"
)

// Auto-creation of the Argo CD Application when a deployment variant is attached
// to a cub-cluster target (issue #4783). `cub cluster up` wires a cluster whose
// root "app of apps" watches an apps Space; deploying a variant to that cluster
// means adding one Argo CD Application Unit to that apps Space and republishing
// its Release. This file folds that manual step into `cub variant create
// --target`, so creating a deployment variant makes the cluster aware of it in
// one command.
//
// The signal is the confighub.com/apps-space annotation `cub cluster up` stamps
// on the OCI target, with confighub.com/cluster-controller argo, naming the
// Space that holds the Argo Application Units. Any other target is not an Argo
// cluster target and this is a no-op, so non-cluster variant creates are
// unaffected.

// variantArgoAppManifest returns the YAML for the Argo CD Application that pulls
// a deployment variant's Release bundle. It matches the shape documented in the
// Go-Live tutorial: named after the variant Space, sourced from that Space's OCI
// Release, auto-synced with self-heal. allowEmpty lets Argo sync the Application
// before the variant's first Release is published (the OCI endpoint serves an
// empty bundle until then) instead of erroring on a resource-less sync.
//
// Without prune, Argo never deletes what the Application deployed: a resource
// that leaves the Release stays running, and deleting the Application orphans
// its resources. With prune, the Application gets prune and Argo's resources
// finalizer, so a resource that leaves the Release is deleted, and so is
// everything the Application deployed when the Application itself is deleted.
// Together with allowEmpty, that means an empty Release deletes the deployment.
func variantArgoAppManifest(appName, ociRepoURL string, prune bool) []byte {
	finalizers, pruneLine := "", ""
	if prune {
		finalizers = "  finalizers:\n    - resources-finalizer.argocd.argoproj.io\n"
		pruneLine = "      prune: true\n"
	}
	return fmt.Appendf(nil, `apiVersion: argoproj.io/v1alpha1
kind: Application
metadata:
  name: %s
  namespace: %s
%sspec:
  project: default
  source:
    repoURL: %s
    targetRevision: latest
    path: .
  destination:
    server: https://kubernetes.default.svc
  syncPolicy:
    automated:
      selfHeal: true
%s      allowEmpty: true
`, appName, clusterArgoNamespace, finalizers, ociRepoURL, pruneLine)
}

// createVariantArgoApp auto-creates the Argo CD Application Unit for a deployment
// variant when its target is a cub-cluster Argo target. It reports whether it did
// anything: a target that is not a cub-cluster Argo target returns (false, nil)
// and the caller stays silent.
//
// The Application is named after the variant Space and pulls that Space's Release
// from the OCI endpoint. It is created as a Unit in the cluster's apps Space
// (resolved from the target annotation) and bound to the same OCI target, so it
// lands in the apps Space's Release bundle the root app-of-apps syncs. The apps
// Space's Release is then republished so the root app picks up the new
// Application on its next reconcile.
//
// The Application gets prune and the resources finalizer when prune is set
// (--prune) or the target carries confighub.com/cluster-prune, which `cub cluster
// up --prune` stamps so every deployment on that cluster gets them.
func createVariantArgoApp(out io.Writer, target *goclientnew.Target, variantSpace *goclientnew.Space, targetID uuid.UUID, prune bool) (bool, error) {
	appsSpaceSlug := clusterAppsSpaceSlug(target, clusterControllerArgo)
	if appsSpaceSlug == "" {
		return false, nil
	}
	prune = prune || target.Annotations[clusterAnnotationPrune] == "true"

	appsSpace, err := resolveSpace(appsSpaceSlug, "SpaceID")
	if err != nil {
		return false, fmt.Errorf("resolve argo-apps space %q from target annotation: %w", appsSpaceSlug, err)
	}

	// The Application Unit's slug in the apps Space equals the variant Space
	// slug (also the Argo CD Application name). With --allow-exists, re-running
	// against an existing deployment is a no-op rather than a conflict.
	if allowExists {
		if existing, _ := resolveUnit(variantSpace.Slug, appsSpace.Space.SpaceID.String(), "UnitID"); existing != nil {
			if !jsonOutput {
				fmt.Fprintf(out, "Argo CD Application Unit %q already exists in apps space %q; skipping.\n", variantSpace.Slug, appsSpaceSlug)
			}
			return true, nil
		}
	}

	// OCI endpoint as Argo (in-cluster) reaches it: the registry the server
	// advertises, with loopback rewritten to host.docker.internal for a local
	// kind cluster. The bundle is served at /space/<variant-space-slug>.
	ociServerURL, err := clusterOCIServerURL()
	if err != nil {
		return false, err
	}
	ociURLForArgo, err := clusterOCIEndpointFromContainer(ociServerURL, clusterServerURL())
	if err != nil {
		return false, err
	}
	repoURL := strings.TrimRight(ociURLForArgo, "/") + "/space/" + variantSpace.Slug

	if !jsonOutput {
		pruneNote := ""
		if prune {
			pruneNote = " (prune, resources finalizer)"
		}
		fmt.Fprintf(out, "Creating Argo CD Application Unit %q in apps space %q%s...\n", variantSpace.Slug, appsSpaceSlug, pruneNote)
	}
	manifest := variantArgoAppManifest(variantSpace.Slug, repoURL, prune)
	unitID, err := clusterCreateK8sYAMLUnit(appsSpace.Space.SpaceID, targetID, variantSpace.Slug, variantSpace.Slug,
		clusterUnitTargetUIAnnotations(target.Annotations, variantSpace.Slug), manifest)
	if err != nil {
		return false, fmt.Errorf("create Argo Application unit: %w", err)
	}
	if err := clusterSetClusterUnitID(variantSpace.SpaceID, unitID); err != nil {
		return false, err
	}

	// Republish the apps Space's Release so the root app-of-apps picks up the
	// new Application on its next sync. Wait for the new Unit's triggers to
	// settle first so the published bundle carries its post-trigger revision.
	if err := clusterWaitUnitTriggers(appsSpace.Space.SpaceID, unitID); err != nil {
		return false, err
	}
	if !jsonOutput {
		fmt.Fprintf(out, "Publishing apps space %q Release...\n", appsSpaceSlug)
	}
	if err := clusterPublishRelease(appsSpace.Space.SpaceID); err != nil {
		return false, err
	}
	if !jsonOutput {
		fmt.Fprintf(out, "Argo CD Application %q created; Argo syncs it on its next reconcile.\n", variantSpace.Slug)
	}
	return true, nil
}

// deleteVariantArgoApp is the inverse of createVariantArgoApp, run by `cub
// variant delete` (see deleteVariantClusterApp) before it deletes the variant
// Space.
//
// The order matters. The root app-of-apps self-heals, so an Application deleted
// while the root's Release still holds its Unit is re-created within seconds.
// So the Unit is removed from the apps Space and the Release republished first,
// and the Application is deleted only once the root no longer wants it. Its
// finalizers are set before the publish, because a root with prune deletes it
// as soon as it syncs, and the finalizers then decide whether what it deployed
// goes with it (cascade) or stays running. Setting them here, rather than
// relying on the generated manifest, covers Applications created before
// --prune existed.
//
// The Application is reached through the cluster's kubeconfig, which only the
// machine that attached the cluster has. Without it the Unit is still removed
// and the user is told what is left in the cluster.
func deleteVariantArgoApp(out io.Writer, app *variantClusterApp, cascade bool) error {
	rootPrunes := app.target.Annotations[clusterAnnotationPrune] == "true"
	if !app.inCluster && !cascade && rootPrunes {
		return fmt.Errorf("cluster %q prunes Applications, so without its kubeconfig (%s) the Application %q cannot be kept from deleting what it deployed; run this where the cluster was attached, or drop --orphan",
			app.clusterName, app.kubeconfigPath, app.name)
	}

	if app.inCluster {
		if _, err := clusterArgoSetCascade(ctx, app.kubeconfigPath, app.name, cascade); err != nil {
			return err
		}
	}
	if err := app.removeUnit(out, "Argo CD Application"); err != nil {
		return err
	}

	if !app.inCluster {
		fmt.Fprintf(out, "No kubeconfig for cluster %q at %s, so Argo CD Application %q is left to the cluster.\n", app.clusterName, app.kubeconfigPath, app.name)
		if rootPrunes {
			fmt.Fprintln(out, "Its root Application prunes it on its next sync, with what it deployed if it has Argo's resources finalizer.")
		} else {
			fmt.Fprintf(out, "Its root Application does not prune, so remove it, with what it deployed, where the cluster is reachable:\n"+
				"  kubectl patch application %s -n %s --type merge -p '{\"metadata\":{\"finalizers\":[\"resources-finalizer.argocd.argoproj.io\"]}}'\n"+
				"  kubectl delete application %s -n %s\n",
				app.name, clusterArgoNamespace, app.name, clusterArgoNamespace)
		}
		return nil
	}

	fmt.Fprintf(out, "Waiting for root Application %q to release Application %q (timeout %s)...\n", app.appsSpaceSlug, app.name, variantClusterRootWait)
	if err := clusterArgoWaitRootReleased(ctx, app.kubeconfigPath, app.appsSpaceSlug, app.name, variantClusterRootWait); err != nil {
		return fmt.Errorf("%w; run the delete again once it has", err)
	}
	if _, err := clusterArgoDeleteApplication(ctx, app.kubeconfigPath, app.name, cascade, out); err != nil {
		return err
	}
	return nil
}
