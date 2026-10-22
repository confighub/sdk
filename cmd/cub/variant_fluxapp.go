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

// Auto-creation of a deployment's Flux OCIRepository and Kustomization when a
// deployment variant is attached to a `cub cluster up --controller flux` target:
// the Flux counterpart of createVariantArgoApp. The cluster's root Kustomization
// applies an apps Space; deploying a variant to that cluster means adding one
// Unit holding the pair to that apps Space and republishing its Release.
//
// When the pair is released depends on argobot. An OCIRepository whose Release
// does not exist yet fails, and Flux retries it with an exponential backoff of
// up to 15 minutes. A reconciliation request skips the backoff, so with argobot,
// which requests one on each publish, the Unit is released straight away, as an
// Argo CD Application is, and the variant's first publish deploys at once
// wherever it is published from. Without argobot, nothing would cut the backoff
// short, so `cub variant create` only adds the Unit, and the variant's first
// `cub release publish` republishes the apps Space with it, as with Sveltos.
//
// The signal is the confighub.com/apps-space annotation `cub cluster up` stamps
// on the OCI target, with confighub.com/cluster-controller flux. Any other
// target is not a Flux cluster target and this is a no-op.

// createVariantFluxApp adds the Flux Unit for a deployment variant to the
// cluster's apps Space when its target is a cub-cluster Flux target, and
// releases it when the cluster has argobot (see publishVariantFluxApp). It
// reports whether it did anything.
//
// The Kustomization prunes what it applied when prune is set (--prune) or the
// target carries confighub.com/cluster-prune, which `cub cluster up --controller
// flux --prune` stamps so every deployment on that cluster prunes.
func createVariantFluxApp(out io.Writer, target *goclientnew.Target, variantSpace *goclientnew.Space, targetID uuid.UUID, prune bool) (bool, error) {
	appsSpaceSlug := clusterAppsSpaceSlug(target, clusterControllerFlux)
	if appsSpaceSlug == "" {
		return false, nil
	}
	prune = prune || target.Annotations[clusterAnnotationPrune] == "true"

	appsSpace, err := resolveSpace(appsSpaceSlug, "SpaceID")
	if err != nil {
		return false, fmt.Errorf("resolve flux-apps space %q from target annotation: %w", appsSpaceSlug, err)
	}

	if allowExists {
		if existing, _ := resolveUnit(variantSpace.Slug, appsSpace.Space.SpaceID.String(), "UnitID"); existing != nil {
			if !jsonOutput {
				fmt.Fprintf(out, "Flux Unit %q already exists in apps space %q; skipping.\n", variantSpace.Slug, appsSpaceSlug)
			}
			return true, nil
		}
	}

	// OCI endpoint as Flux (in-cluster) reaches it, and its transport, the same
	// way `cub cluster up` derived them for the root.
	ociServerURL, err := clusterOCIServerURL()
	if err != nil {
		return false, err
	}
	ociURLForCluster, err := clusterOCIEndpointFromContainer(ociServerURL, clusterServerURL())
	if err != nil {
		return false, err
	}
	insecure, err := clusterOCIInsecure(ociServerURL, clusterServerURL())
	if err != nil {
		return false, err
	}
	repoURL := strings.TrimRight(ociURLForCluster, "/") + "/space/" + variantSpace.Slug

	if !jsonOutput {
		pruneNote := ""
		if prune {
			pruneNote = " (prune)"
		}
		fmt.Fprintf(out, "Creating Flux OCIRepository and Kustomization Unit %q in apps space %q%s...\n", variantSpace.Slug, appsSpaceSlug, pruneNote)
	}
	manifest := clusterFluxChildManifest(variantSpace.Slug, repoURL, clusterFluxPollIntervalOf(target), insecure, prune)
	unitID, err := clusterCreateK8sYAMLUnit(appsSpace.Space.SpaceID, targetID, variantSpace.Slug, variantSpace.Slug, nil, manifest)
	if err != nil {
		return false, fmt.Errorf("create Flux unit: %w", err)
	}
	if err := clusterSetClusterUnitID(variantSpace.SpaceID, unitID); err != nil {
		return false, err
	}

	if err := clusterWaitUnitTriggers(appsSpace.Space.SpaceID, unitID); err != nil {
		return false, err
	}
	if !clusterFluxReleasesOnCreate(target) {
		if !jsonOutput {
			fmt.Fprintf(out, "Flux Unit %q added; \"cub release publish %s\" releases it with the variant's first Release.\n",
				variantSpace.Slug, variantSpace.Slug)
		}
		return true, nil
	}
	if !jsonOutput {
		fmt.Fprintf(out, "Publishing apps space %q Release...\n", appsSpaceSlug)
	}
	if err := clusterPublishRelease(appsSpace.Space.SpaceID); err != nil {
		return false, err
	}
	if !jsonOutput {
		fmt.Fprintf(out, "Flux Kustomization %q created; argobot deploys the variant's first Release when it is published.\n", variantSpace.Slug)
	}
	return true, nil
}

// clusterFluxReleasesOnCreate says a Flux cluster's child Units are released
// when they are created: the cluster has argobot to skip Flux's backoff.
func clusterFluxReleasesOnCreate(target *goclientnew.Target) bool {
	return target.Annotations[clusterAnnotationArgobot] == "true"
}

// deleteVariantFluxApp is the inverse of createVariantFluxApp, run by `cub
// variant delete` (see deleteVariantClusterApp) before it deletes the variant
// Space: the Flux counterpart of deleteVariantArgoApp.
//
// The Unit is removed from the apps Space and the Release republished, so the
// root Kustomization stops applying the deployment. A root with prune then
// deletes the deployment's OCIRepository and Kustomization itself, and one
// without leaves them, to be deleted here. Either way, whether what the
// deployment applied goes too is up to its Kustomization: it is set to prune
// (cascade) or suspended (--orphan) before the publish, because a pruning root
// deletes it as soon as it applies the Release.
//
// Without the cluster's kubeconfig the Unit is still removed and the user is
// told what is left in the cluster.
func deleteVariantFluxApp(out io.Writer, app *variantClusterApp, cascade bool) error {
	rootPrunes := app.target.Annotations[clusterAnnotationPrune] == "true"
	if !app.inCluster && !cascade && rootPrunes {
		return fmt.Errorf("cluster %q prunes Kustomizations, so without its kubeconfig (%s) the Kustomization %q cannot be kept from deleting what it applied; run this where the cluster was brought up, or drop --orphan",
			app.clusterName, app.kubeconfigPath, app.name)
	}

	if app.inCluster {
		if _, err := clusterFluxSetCascade(ctx, app.kubeconfigPath, app.name, cascade); err != nil {
			return err
		}
	}
	if err := app.removeUnit(out, "Flux OCIRepository and Kustomization"); err != nil {
		return err
	}

	if !app.inCluster {
		fmt.Fprintf(out, "No kubeconfig for cluster %q at %s, so Flux Kustomization %q is left to the cluster.\n", app.clusterName, app.kubeconfigPath, app.name)
		if rootPrunes {
			fmt.Fprintln(out, "Its root Kustomization prunes it on its next reconciliation, with what it applied if it prunes.")
		} else {
			fmt.Fprintf(out, "Its root Kustomization does not prune, so remove it, with what it applied, where the cluster is reachable:\n"+
				"  kubectl patch kustomization %[1]s -n %[2]s --type merge -p '{\"spec\":{\"prune\":true,\"suspend\":false}}'\n"+
				"  kubectl delete kustomization %[1]s -n %[2]s\n"+
				"  kubectl delete ocirepository %[1]s -n %[2]s\n",
				app.name, clusterFluxNamespace)
		}
		return nil
	}

	fmt.Fprintf(out, "Waiting for root Kustomization %q to release Kustomization %q (timeout %s)...\n", clusterFluxRootName, app.name, variantClusterRootWait)
	if err := clusterFluxWaitRootReleased(ctx, app.kubeconfigPath, app.name, variantClusterRootWait); err != nil {
		return fmt.Errorf("%w; run the delete again once it has", err)
	}
	_, err := clusterFluxDeleteDeployment(ctx, app.kubeconfigPath, app.name, cascade, out)
	return err
}

// publishVariantFluxApp releases a variant's Flux Unit, when it has not been
// released yet, by republishing the cluster's apps Space. It runs after `cub
// release publish` of the variant, and acts only for a cluster without argobot,
// whose Units createVariantFluxApp leaves unreleased.
func publishVariantFluxApp(out io.Writer, spaceID uuid.UUID) error {
	return publishVariantAppsUnit(out, spaceID, clusterControllerFlux, "Flux Kustomization",
		func(target *goclientnew.Target) bool { return !clusterFluxReleasesOnCreate(target) })
}
