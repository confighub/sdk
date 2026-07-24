// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"fmt"
	"io"
	"strings"

	goclientnew "github.com/confighub/sdk/core/openapi/goclient-new"
	"github.com/confighub/sdk/core/worker/api"
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
// The signal is the confighub.com/argo-apps-space annotation `cub cluster up`
// stamps on the OCI target, naming the Space that holds the Argo Application
// Units. A target without it is an ordinary OCI target and this is a no-op, so
// non-cluster variant creates are unaffected.

// variantArgoAppManifest returns the YAML for the Argo CD Application that pulls
// a deployment variant's Release bundle. It matches the shape documented in the
// Go-Live tutorial: named after the variant Space, sourced from that Space's OCI
// Release, auto-synced with self-heal. allowEmpty lets Argo sync the Application
// before the variant's first Release is published (the OCI endpoint serves an
// empty bundle until then) instead of erroring on a resource-less sync.
func variantArgoAppManifest(appName, ociRepoURL string) []byte {
	return fmt.Appendf(nil, `apiVersion: argoproj.io/v1alpha1
kind: Application
metadata:
  name: %s
  namespace: %s
spec:
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
      allowEmpty: true
`, appName, clusterArgoNamespace, ociRepoURL)
}

// createVariantArgoApp auto-creates the Argo CD Application Unit for a deployment
// variant when its target is a cub-cluster Argo target. It reports whether it did
// anything: a target that is not OCI or carries no confighub.com/argo-apps-space
// annotation is not a cluster target, so it returns (false, nil) and the caller
// stays silent.
//
// The Application is named after the variant Space and pulls that Space's Release
// from the OCI endpoint. It is created as a Unit in the cluster's apps Space
// (resolved from the target annotation) and bound to the same OCI target, so it
// lands in the apps Space's Release bundle the root app-of-apps syncs. The apps
// Space's Release is then republished so the root app picks up the new
// Application on its next reconcile.
func createVariantArgoApp(out io.Writer, target *goclientnew.Target, variantSpace *goclientnew.Space, targetID uuid.UUID) (bool, error) {
	appsSpaceSlug := target.Annotations[clusterAnnotationArgoAppsSpace]
	if target.ProviderType != string(api.ProviderOCI) || appsSpaceSlug == "" {
		return false, nil
	}

	appsSpace, err := apiGetSpaceFromSlug(appsSpaceSlug, "SpaceID")
	if err != nil {
		return false, fmt.Errorf("resolve argo-apps space %q from target annotation: %w", appsSpaceSlug, err)
	}

	// The Application Unit's slug in the apps Space equals the variant Space
	// slug (also the Argo CD Application name). With --allow-exists, re-running
	// against an existing deployment is a no-op rather than a conflict.
	if allowExists {
		if existing, _ := apiGetUnitFromSlugInSpace(variantSpace.Slug, appsSpace.SpaceID.String(), "UnitID"); existing != nil {
			if !jsonOutput {
				fmt.Fprintf(out, "Argo CD Application Unit %q already exists in apps space %q; skipping.\n", variantSpace.Slug, appsSpaceSlug)
			}
			return true, nil
		}
	}

	// OCI endpoint as Argo (in-cluster) reaches it: loopback rewritten to
	// host.docker.internal for a local kind cluster, the public oci.<host>:443
	// otherwise. The bundle is served at /space/<variant-space-slug>.
	ociURLForArgo, err := clusterOCIEndpointFromContainer(clusterServerURL())
	if err != nil {
		return false, err
	}
	repoURL := strings.TrimRight(ociURLForArgo, "/") + "/space/" + variantSpace.Slug

	if !jsonOutput {
		fmt.Fprintf(out, "Creating Argo CD Application Unit %q in apps space %q...\n", variantSpace.Slug, appsSpaceSlug)
	}
	manifest := variantArgoAppManifest(variantSpace.Slug, repoURL)
	unitID, err := clusterCreateK8sYAMLUnit(appsSpace.SpaceID, targetID, variantSpace.Slug, variantSpace.Slug, manifest)
	if err != nil {
		return false, fmt.Errorf("create Argo Application unit: %w", err)
	}

	// Republish the apps Space's Release so the root app-of-apps picks up the
	// new Application on its next sync. Wait for the new Unit's triggers to
	// settle first so the published bundle carries its post-trigger revision.
	if err := clusterWaitUnitTriggers(appsSpace.SpaceID, unitID); err != nil {
		return false, err
	}
	if !jsonOutput {
		fmt.Fprintf(out, "Publishing apps space %q Release...\n", appsSpaceSlug)
	}
	if err := clusterPublishRelease(appsSpace.SpaceID); err != nil {
		return false, err
	}
	if !jsonOutput {
		fmt.Fprintf(out, "Argo CD Application %q created; Argo syncs it on its next reconcile.\n", variantSpace.Slug)
	}
	return true, nil
}
