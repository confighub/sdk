// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

// Package fluxrenderer provides functionality for rendering Flux HelmRelease
// and Kustomization resources to Kubernetes manifests.
package fluxrenderer

import "time"

// RenderOptions configures the rendering behavior.
type RenderOptions struct {
	// Namespace to use for rendering (defaults to resource namespace).
	Namespace string

	// ReleaseName override for Helm (defaults to HelmRelease name).
	ReleaseName string

	// ArtifactSource specifies where to fetch the chart/kustomize artifact.
	ArtifactSource ArtifactSource

	// Timeout for artifact fetching. Defaults to 5 minutes if not set.
	Timeout time.Duration
}

// ArtifactSource represents where to fetch the artifact from.
type ArtifactSource struct {
	// URL is the HTTP URL to the artifact (chart tgz or kustomize tarball).
	URL string

	// Digest is the SHA256 digest for verification (e.g., "sha256:abc123...").
	Digest string
}

// RenderResult contains the rendered manifests.
type RenderResult struct {
	// Manifests is the rendered YAML documents as a byte slice.
	Manifests []byte

	// Revision is the source revision used (chart version or git commit).
	Revision string
}

// RenderInput represents the input to the renderer.
// The input byte slice can contain multiple YAML documents:
// - The HelmRelease or Kustomization resource (required)
// - ConfigMaps referenced by ValuesFrom (optional)
// - Secrets referenced by ValuesFrom (optional)
// - ConfigMaps/Secrets for PostBuild substitution (optional)
type RenderInput struct {
	// Documents is the multi-document YAML input.
	Documents []byte

	// Options configures rendering behavior.
	Options RenderOptions
}

// DefaultTimeout is the default timeout for artifact fetching.
const DefaultTimeout = 5 * time.Minute

// GetTimeout returns the timeout, using DefaultTimeout if not set.
func (o RenderOptions) GetTimeout() time.Duration {
	if o.Timeout == 0 {
		return DefaultTimeout
	}
	return o.Timeout
}
