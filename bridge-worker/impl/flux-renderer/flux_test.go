// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package fluxrenderer

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRenderFlux_HelmRelease(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	input := RenderInput{
		Documents: []byte(`
apiVersion: helm.toolkit.fluxcd.io/v2
kind: HelmRelease
metadata:
  name: podinfo
  namespace: podinfo
spec:
  interval: 50m
  chart:
    spec:
      chart: podinfo
      version: "6.x"
      sourceRef:
        kind: HelmRepository
        name: podinfo
  values:
    replicaCount: 2
`),
		Options: RenderOptions{
			ArtifactSource: ArtifactSource{
				URL:    testChartURL,
				Digest: testChartDigest,
			},
		},
	}

	result, err := RenderFlux(ctx, input)
	require.NoError(t, err)
	require.NotNil(t, result)

	manifests := string(result.Manifests)
	assert.Contains(t, manifests, "kind: Deployment")
	assert.Contains(t, manifests, "replicas: 2")
}

func TestRenderFlux_NoFluxResource(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	input := RenderInput{
		Documents: []byte(`
apiVersion: v1
kind: ConfigMap
metadata:
  name: some-config
data:
  key: value
`),
		Options: RenderOptions{
			ArtifactSource: ArtifactSource{
				URL: testChartURL,
			},
		},
	}

	_, err := RenderFlux(ctx, input)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no HelmRelease or Kustomization found")
}

func TestRenderFlux_MultipleHelmReleases(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	input := RenderInput{
		Documents: []byte(`
apiVersion: helm.toolkit.fluxcd.io/v2
kind: HelmRelease
metadata:
  name: podinfo-1
  namespace: default
spec:
  interval: 50m
  chart:
    spec:
      chart: podinfo
---
apiVersion: helm.toolkit.fluxcd.io/v2
kind: HelmRelease
metadata:
  name: podinfo-2
  namespace: default
spec:
  interval: 50m
  chart:
    spec:
      chart: podinfo
`),
		Options: RenderOptions{
			ArtifactSource: ArtifactSource{
				URL: testChartURL,
			},
		},
	}

	_, err := RenderFlux(ctx, input)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "found 2 HelmRelease resources")
}

func TestRenderFlux_MultipleKustomizations(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	input := RenderInput{
		Documents: []byte(`
apiVersion: kustomize.toolkit.fluxcd.io/v1
kind: Kustomization
metadata:
  name: app-1
  namespace: flux-system
spec:
  interval: 10m
  path: ./
  sourceRef:
    kind: GitRepository
    name: test-repo
---
apiVersion: kustomize.toolkit.fluxcd.io/v1
kind: Kustomization
metadata:
  name: app-2
  namespace: flux-system
spec:
  interval: 10m
  path: ./
  sourceRef:
    kind: GitRepository
    name: test-repo
`),
		Options: RenderOptions{
			ArtifactSource: ArtifactSource{
				URL: "http://example.com/artifact.tar.gz",
			},
		},
	}

	_, err := RenderFlux(ctx, input)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "found 2 Kustomization resources")
}

func TestRenderFlux_BothHelmReleaseAndKustomization(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	input := RenderInput{
		Documents: []byte(`
apiVersion: helm.toolkit.fluxcd.io/v2
kind: HelmRelease
metadata:
  name: podinfo
  namespace: default
spec:
  interval: 50m
  chart:
    spec:
      chart: podinfo
---
apiVersion: kustomize.toolkit.fluxcd.io/v1
kind: Kustomization
metadata:
  name: app
  namespace: flux-system
spec:
  interval: 10m
  path: ./
  sourceRef:
    kind: GitRepository
    name: test-repo
`),
		Options: RenderOptions{
			ArtifactSource: ArtifactSource{
				URL: testChartURL,
			},
		},
	}

	_, err := RenderFlux(ctx, input)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "found both HelmRelease and Kustomization")
}

func TestFindAllDocumentsByKindAndAPIVersion(t *testing.T) {
	docs := []byte(`
apiVersion: helm.toolkit.fluxcd.io/v2
kind: HelmRelease
metadata:
  name: hr-1
---
apiVersion: helm.toolkit.fluxcd.io/v2beta1
kind: HelmRelease
metadata:
  name: hr-2
---
apiVersion: kustomize.toolkit.fluxcd.io/v1
kind: Kustomization
metadata:
  name: ks-1
---
apiVersion: v1
kind: ConfigMap
metadata:
  name: config
`)

	parsed, err := parseYAMLDocuments(docs)
	require.NoError(t, err)

	// Should find both HelmReleases (v2 and v2beta1)
	helmReleases := FindAllDocumentsByKindAndAPIVersion(parsed, HelmReleaseKind, HelmReleaseAPIVersionPrefix)
	require.Len(t, helmReleases, 2)
	assert.Equal(t, "hr-1", helmReleases[0].GetName())
	assert.Equal(t, "hr-2", helmReleases[1].GetName())

	// Should find the Kustomization
	kustomizations := FindAllDocumentsByKindAndAPIVersion(parsed, KustomizationKind, KustomizationAPIVersionPrefix)
	require.Len(t, kustomizations, 1)
	assert.Equal(t, "ks-1", kustomizations[0].GetName())

	// Should not find any unknown types
	unknown := FindAllDocumentsByKindAndAPIVersion(parsed, "Unknown", "unknown.io/")
	assert.Len(t, unknown, 0)
}
