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

// Auto-creation of the Sveltos ClusterProfile when a deployment variant is
// attached to a `cub cluster up --controller sveltos` target: the Sveltos
// counterpart of createVariantArgoApp. The cluster's root ClusterProfile
// watches an apps Space; deploying a variant to that cluster means adding one
// ClusterProfile Unit to that apps Space and republishing its Release.
//
// Unlike an Argo Application, the profile is not released until the variant has
// a Release of its own. A ClusterProfile whose Release does not exist yet fails,
// and Sveltos backs a failed profile off for 10 to 60 seconds, skipping any
// redeploy argobot asks for in the meantime, so a variant's first publish would
// wait out the backoff. So `cub variant create` only adds the Unit, and the
// variant's first `cub release publish` republishes the apps Space with it.
//
// The signal is the confighub.com/sveltos-apps-space annotation `cub cluster
// up` stamps on the OCI target. A target without it is not a Sveltos cluster
// target and this is a no-op.

// createVariantSveltosProfile adds the ClusterProfile Unit for a deployment
// variant to the cluster's apps Space when its target is a cub-cluster Sveltos
// target, without releasing it (see publishVariantSveltosProfile). It reports
// whether it did anything.
//
// The profile is named after the variant Space and pulls that Space's Release
// from the OCI endpoint. It is created as a Unit in the cluster's apps Space and
// bound to the same OCI target, so it lands in the apps Space's Release bundle
// the root profile deploys.
func createVariantSveltosProfile(out io.Writer, target *goclientnew.Target, variantSpace *goclientnew.Space, targetID uuid.UUID) (bool, error) {
	appsSpaceSlug := target.Annotations[clusterAnnotationSveltosAppsSpace]
	if appsSpaceSlug == "" {
		return false, nil
	}

	appsSpace, err := resolveSpace(appsSpaceSlug, "SpaceID")
	if err != nil {
		return false, fmt.Errorf("resolve sveltos-apps space %q from target annotation: %w", appsSpaceSlug, err)
	}

	if allowExists {
		if existing, _ := resolveUnit(variantSpace.Slug, appsSpace.Space.SpaceID.String(), "UnitID"); existing != nil {
			if !jsonOutput {
				fmt.Fprintf(out, "Sveltos ClusterProfile Unit %q already exists in apps space %q; skipping.\n", variantSpace.Slug, appsSpaceSlug)
			}
			return true, nil
		}
	}

	// OCI endpoint as Sveltos (in-cluster) reaches it, and its transport, the
	// same way `cub cluster up` derived them for the root profile.
	ociServerURL, err := clusterOCIServerURL()
	if err != nil {
		return false, err
	}
	ociURLForCluster, err := clusterOCIEndpointFromContainer(ociServerURL, clusterServerURL())
	if err != nil {
		return false, err
	}
	plainHTTP, err := clusterOCIInsecure(ociServerURL, clusterServerURL())
	if err != nil {
		return false, err
	}
	repoURL := strings.TrimRight(ociURLForCluster, "/") + "/space/" + variantSpace.Slug

	if !jsonOutput {
		fmt.Fprintf(out, "Creating Sveltos ClusterProfile Unit %q in apps space %q...\n", variantSpace.Slug, appsSpaceSlug)
	}
	manifest := clusterSveltosChildProfileManifest(variantSpace.Slug, repoURL, clusterSveltosPollIntervalOf(target), plainHTTP)
	unitID, err := clusterCreateK8sYAMLUnit(appsSpace.Space.SpaceID, targetID, variantSpace.Slug, variantSpace.Slug,
		clusterUnitTargetUIAnnotations(target.Annotations, variantSpace.Slug), manifest)
	if err != nil {
		return false, fmt.Errorf("create Sveltos ClusterProfile unit: %w", err)
	}
	if err := clusterSetClusterUnitID(variantSpace.SpaceID, unitID); err != nil {
		return false, err
	}

	if err := clusterWaitUnitTriggers(appsSpace.Space.SpaceID, unitID); err != nil {
		return false, err
	}
	if !jsonOutput {
		fmt.Fprintf(out, "Sveltos ClusterProfile Unit %q added; \"cub release publish %s\" releases it with the variant's first Release.\n",
			variantSpace.Slug, variantSpace.Slug)
	}
	return true, nil
}

// publishVariantSveltosProfile releases a variant's ClusterProfile Unit, when it
// has not been released yet, by republishing the cluster's apps Space. It runs
// after `cub release publish` of the variant, so the profile reaches the cluster
// only once the Release it pulls exists. It is a no-op for a Space that does
// not release to a cub-cluster Sveltos target, whose apps Space has no Unit for
// it, or whose Unit was released already.
func publishVariantSveltosProfile(out io.Writer, spaceID uuid.UUID) error {
	return publishVariantAppsUnit(out, spaceID, clusterAnnotationSveltosAppsSpace, "Sveltos ClusterProfile", nil)
}

// publishVariantAppsUnit republishes the apps Space named by the
// appsSpaceAnnotation of the release target of the Space spaceID, when that
// apps Space holds a Unit for the Space that has not been released yet. kind
// names the Unit's object in the message. deferred, when not nil, says whether
// the target is one whose Units wait for this publish; for any other it is a
// no-op.
func publishVariantAppsUnit(out io.Writer, spaceID uuid.UUID, appsSpaceAnnotation, kind string, deferred func(*goclientnew.Target) bool) error {
	space, err := resolveSpace(spaceID.String(), "SpaceID,Slug,ReleaseTargetID")
	if err != nil {
		return err
	}
	if space.Space.ReleaseTargetID == nil {
		return nil
	}
	// This runs after every publish. A release target the caller cannot read is
	// not one this can act on, and must not turn a published Release into an
	// error.
	target, err := resolveTarget(space.Space.ReleaseTargetID.String(), "", "TargetID,SpaceID,Annotations")
	if err != nil {
		return nil
	}
	appsSpaceSlug := target.Target.Annotations[appsSpaceAnnotation]
	if appsSpaceSlug == "" || (deferred != nil && !deferred(target.Target)) {
		return nil
	}
	appsSpace, err := resolveSpace(appsSpaceSlug, "SpaceID")
	if err != nil {
		return fmt.Errorf("resolve apps space %q from target annotation %s: %w", appsSpaceSlug, appsSpaceAnnotation, err)
	}
	unit, err := resolveUnit(space.Space.Slug, appsSpace.Space.SpaceID.String(), "UnitID,HeadRevisionNum,LastReleasedRevisionNum")
	if err != nil || unit.Unit.LastReleasedRevisionNum >= unit.Unit.HeadRevisionNum {
		// No Unit for this Space, or it is released already.
		return nil
	}

	if !quiet && !isAlternativeOutput() {
		fmt.Fprintf(out, "Publishing apps space %q Release, with %s %q...\n", appsSpaceSlug, kind, space.Space.Slug)
	}
	return clusterPublishRelease(appsSpace.Space.SpaceID)
}
