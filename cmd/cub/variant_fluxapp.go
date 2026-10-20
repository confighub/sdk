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
func createVariantFluxApp(out io.Writer, target *goclientnew.Target, variantSpace *goclientnew.Space, targetID uuid.UUID) (bool, error) {
	appsSpaceSlug := clusterAppsSpaceSlug(target, clusterControllerFlux)
	if appsSpaceSlug == "" {
		return false, nil
	}

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
		fmt.Fprintf(out, "Creating Flux OCIRepository and Kustomization Unit %q in apps space %q...\n", variantSpace.Slug, appsSpaceSlug)
	}
	manifest := clusterFluxChildManifest(variantSpace.Slug, repoURL, clusterFluxPollIntervalOf(target), insecure)
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

// publishVariantFluxApp releases a variant's Flux Unit, when it has not been
// released yet, by republishing the cluster's apps Space. It runs after `cub
// release publish` of the variant, and acts only for a cluster without argobot,
// whose Units createVariantFluxApp leaves unreleased.
func publishVariantFluxApp(out io.Writer, spaceID uuid.UUID) error {
	return publishVariantAppsUnit(out, spaceID, clusterControllerFlux, "Flux Kustomization",
		func(target *goclientnew.Target) bool { return !clusterFluxReleasesOnCreate(target) })
}
