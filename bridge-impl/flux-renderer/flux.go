// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package fluxrenderer

import (
	"context"
	"fmt"
	"strings"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// NOTE: The results of rendering aren't being deployed as the renderer unit, so UnitSlug and SpaceID
// annotations should not be added by the renderer. They will be added by the deployment bridge.

// RenderFlux renders a Flux HelmRelease or Kustomization to Kubernetes manifests.
// It automatically detects the resource type and calls the appropriate renderer.
//
// The input can be a multi-document YAML containing:
// - A HelmRelease or Kustomization resource (required, exactly one)
// - ConfigMaps/Secrets referenced by ValuesFrom or PostBuild.substituteFrom (optional)
//
// Returns an error if:
// - No HelmRelease or Kustomization is found
// - More than one HelmRelease is found
// - More than one Kustomization is found
// - Both HelmRelease and Kustomization are found
func RenderFlux(ctx context.Context, input RenderInput) (*RenderResult, error) {
	// Parse all documents from input
	parsed, err := ParseDocumentsWithResources(input.Documents)
	if err != nil {
		return nil, fmt.Errorf("failed to parse input documents: %w", err)
	}

	// Find all HelmRelease and Kustomization documents
	helmReleases := FindAllDocumentsByKindAndAPIVersion(parsed.Documents, HelmReleaseKind, HelmReleaseAPIVersionPrefix)
	kustomizations := FindAllDocumentsByKindAndAPIVersion(parsed.Documents, KustomizationKind, KustomizationAPIVersionPrefix)

	// Validate we have exactly one Flux resource
	totalFluxResources := len(helmReleases) + len(kustomizations)
	if totalFluxResources == 0 {
		return nil, fmt.Errorf("no HelmRelease or Kustomization found in input")
	}

	if len(helmReleases) > 1 {
		return nil, fmt.Errorf("found %d HelmRelease resources, expected at most 1", len(helmReleases))
	}

	if len(kustomizations) > 1 {
		return nil, fmt.Errorf("found %d Kustomization resources, expected at most 1", len(kustomizations))
	}

	if len(helmReleases) > 0 && len(kustomizations) > 0 {
		return nil, fmt.Errorf("found both HelmRelease and Kustomization resources, expected only one type")
	}

	// Dispatch to appropriate renderer
	if len(helmReleases) > 0 {
		return renderHelmReleaseFromParsed(ctx, helmReleases[0], parsed, input.Options)
	}

	return renderKustomizationFromParsed(ctx, kustomizations[0], parsed, input.Options)
}

// FindAllDocumentsByKindAndAPIVersion finds all documents matching the given kind and API version prefix.
func FindAllDocumentsByKindAndAPIVersion(docs []*unstructured.Unstructured, kind, apiVersionPrefix string) []*unstructured.Unstructured {
	var matches []*unstructured.Unstructured
	for _, doc := range docs {
		if doc.GetKind() == kind && strings.HasPrefix(doc.GetAPIVersion(), apiVersionPrefix) {
			matches = append(matches, doc)
		}
	}
	return matches
}
