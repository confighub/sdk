// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package fluxrenderer

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

const (
	// Test chart from stefanprodan/podinfo
	testChartURL    = "https://stefanprodan.github.io/podinfo/podinfo-6.9.4.tgz"
	testChartDigest = "sha256:23faaaf89a1ec4905345b9eb5124028770719985740ab9f917a476c37e6d3ee2"
)

func TestRenderHelmRelease_InlineValues(t *testing.T) {
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
    ingress:
      enabled: true
      hosts:
        - host: podinfo.local
          paths:
            - path: /
              pathType: Prefix
`),
		Options: RenderOptions{
			ArtifactSource: ArtifactSource{
				URL:    testChartURL,
				Digest: testChartDigest,
			},
		},
	}

	result, err := RenderHelmRelease(ctx, input)
	require.NoError(t, err)
	require.NotNil(t, result)

	manifests := string(result.Manifests)

	// Should contain a Deployment
	assert.Contains(t, manifests, "kind: Deployment")
	assert.Contains(t, manifests, "name: podinfo")

	// Should contain the configured replicas
	assert.Contains(t, manifests, "replicas: 2")

	// Should contain an Ingress (since ingress.enabled: true)
	assert.Contains(t, manifests, "kind: Ingress")
	assert.Contains(t, manifests, "podinfo.local")

	// Should have source comments for traceability
	assert.Contains(t, manifests, "# Source:")
}

func TestRenderHelmRelease_ValuesFromConfigMap(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	// Test with ValuesFrom referencing a ConfigMap
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
  valuesFrom:
    - kind: ConfigMap
      name: podinfo-values
  values:
    replicaCount: 1
---
apiVersion: v1
kind: ConfigMap
metadata:
  name: podinfo-values
  namespace: podinfo
data:
  values.yaml: |
    replicaCount: 5
    ui:
      message: "Hello from ConfigMap"
`),
		Options: RenderOptions{
			ArtifactSource: ArtifactSource{
				URL:    testChartURL,
				Digest: testChartDigest,
			},
		},
	}

	result, err := RenderHelmRelease(ctx, input)
	require.NoError(t, err)
	require.NotNil(t, result)

	manifests := string(result.Manifests)

	// ConfigMap values should override inline values
	// In Flux, valuesFrom values take precedence over inline values
	assert.Contains(t, manifests, "replicas: 5")
	assert.Contains(t, manifests, "Hello from ConfigMap")
}

func TestRenderHelmRelease_ValuesFromSecret(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	// Test with ValuesFrom referencing a Secret
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
  valuesFrom:
    - kind: Secret
      name: podinfo-secrets
      valuesKey: config.yaml
  values:
    replicaCount: 1
---
apiVersion: v1
kind: Secret
metadata:
  name: podinfo-secrets
  namespace: podinfo
data:
  # base64 encoded: replicaCount: 3
  config.yaml: cmVwbGljYUNvdW50OiAz
`),
		Options: RenderOptions{
			ArtifactSource: ArtifactSource{
				URL:    testChartURL,
				Digest: testChartDigest,
			},
		},
	}

	result, err := RenderHelmRelease(ctx, input)
	require.NoError(t, err)
	require.NotNil(t, result)

	manifests := string(result.Manifests)

	// Secret values should be applied
	assert.Contains(t, manifests, "replicas: 3")
}

func TestRenderHelmRelease_OptionalValuesFrom(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	// Test with optional ValuesFrom that doesn't exist
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
  valuesFrom:
    - kind: ConfigMap
      name: nonexistent-config
      optional: true
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

	result, err := RenderHelmRelease(ctx, input)
	require.NoError(t, err)
	require.NotNil(t, result)

	manifests := string(result.Manifests)

	// Should still render with inline values since optional reference is missing
	assert.Contains(t, manifests, "replicas: 2")
}

func TestRenderHelmRelease_CustomReleaseName(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	input := RenderInput{
		Documents: []byte(`
apiVersion: helm.toolkit.fluxcd.io/v2
kind: HelmRelease
metadata:
  name: podinfo
  namespace: default
spec:
  releaseName: my-custom-release
  interval: 50m
  chart:
    spec:
      chart: podinfo
      version: "6.x"
      sourceRef:
        kind: HelmRepository
        name: podinfo
`),
		Options: RenderOptions{
			ArtifactSource: ArtifactSource{
				URL:    testChartURL,
				Digest: testChartDigest,
			},
		},
	}

	result, err := RenderHelmRelease(ctx, input)
	require.NoError(t, err)
	require.NotNil(t, result)

	manifests := string(result.Manifests)

	// Should use custom release name in labels
	assert.Contains(t, manifests, "my-custom-release")
}

func TestRenderHelmRelease_TargetNamespace(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	input := RenderInput{
		Documents: []byte(`
apiVersion: helm.toolkit.fluxcd.io/v2
kind: HelmRelease
metadata:
  name: podinfo
  namespace: flux-system
spec:
  targetNamespace: production
  interval: 50m
  chart:
    spec:
      chart: podinfo
      version: "6.x"
      sourceRef:
        kind: HelmRepository
        name: podinfo
`),
		Options: RenderOptions{
			ArtifactSource: ArtifactSource{
				URL:    testChartURL,
				Digest: testChartDigest,
			},
		},
	}

	result, err := RenderHelmRelease(ctx, input)
	require.NoError(t, err)
	require.NotNil(t, result)

	manifests := string(result.Manifests)

	// Resources should be in the target namespace
	assert.Contains(t, manifests, "namespace: production")
}

func TestRenderHelmRelease_NamespaceOverride(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
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
      version: "6.x"
      sourceRef:
        kind: HelmRepository
        name: podinfo
`),
		Options: RenderOptions{
			Namespace: "override-namespace",
			ArtifactSource: ArtifactSource{
				URL:    testChartURL,
				Digest: testChartDigest,
			},
		},
	}

	result, err := RenderHelmRelease(ctx, input)
	require.NoError(t, err)
	require.NotNil(t, result)

	manifests := string(result.Manifests)

	// Option namespace should override spec
	assert.Contains(t, manifests, "namespace: override-namespace")
}

func TestRenderHelmRelease_NoHelmRelease(t *testing.T) {
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
				URL:    testChartURL,
				Digest: testChartDigest,
			},
		},
	}

	_, err := RenderHelmRelease(ctx, input)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no HelmRelease found")
}

func TestRenderHelmRelease_MissingArtifactURL(t *testing.T) {
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
      version: "6.x"
`),
		Options: RenderOptions{
			ArtifactSource: ArtifactSource{
				URL: "", // Missing URL
			},
		},
	}

	_, err := RenderHelmRelease(ctx, input)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "artifact source URL is required")
}

func TestRenderHelmRelease_ManifestFormat(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
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
      version: "6.x"
      sourceRef:
        kind: HelmRepository
        name: podinfo
`),
		Options: RenderOptions{
			ArtifactSource: ArtifactSource{
				URL:    testChartURL,
				Digest: testChartDigest,
			},
		},
	}

	result, err := RenderHelmRelease(ctx, input)
	require.NoError(t, err)
	require.NotNil(t, result)

	manifests := string(result.Manifests)

	// Should have valid YAML with proper document separators
	docs := strings.Split(manifests, "---")
	assert.Greater(t, len(docs), 1, "should have multiple YAML documents")

	// Each non-empty document should have apiVersion and kind
	for _, doc := range docs {
		doc = strings.TrimSpace(doc)
		if doc == "" || strings.HasPrefix(doc, "#") {
			continue
		}
		assert.Contains(t, doc, "apiVersion:")
		assert.Contains(t, doc, "kind:")
	}
}

func TestRenderHelmRelease_ChartVersion(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
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
      version: "6.x"
      sourceRef:
        kind: HelmRepository
        name: podinfo
`),
		Options: RenderOptions{
			ArtifactSource: ArtifactSource{
				URL:    testChartURL,
				Digest: testChartDigest,
			},
		},
	}

	result, err := RenderHelmRelease(ctx, input)
	require.NoError(t, err)
	require.NotNil(t, result)

	// Revision should be the chart version
	assert.NotEmpty(t, result.Revision)
	assert.Contains(t, result.Revision, "6.")
}

func TestRenderHelmRelease_WithHelmChart(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	// Test with HelmChart that specifies valuesFiles
	// Note: This test verifies the HelmChart is found and processed,
	// but the actual values files must exist in the chart
	input := RenderInput{
		Documents: []byte(`
apiVersion: helm.toolkit.fluxcd.io/v2
kind: HelmRelease
metadata:
  name: podinfo
  namespace: podinfo
spec:
  interval: 50m
  chartRef:
    kind: HelmChart
    name: podinfo-chart
  values:
    replicaCount: 2
---
apiVersion: source.toolkit.fluxcd.io/v1
kind: HelmChart
metadata:
  name: podinfo-chart
  namespace: podinfo
spec:
  chart: podinfo
  version: "6.x"
  sourceRef:
    kind: HelmRepository
    name: podinfo
  valuesFiles:
    - values.yaml
  ignoreMissingValuesFiles: true
`),
		Options: RenderOptions{
			ArtifactSource: ArtifactSource{
				URL:    testChartURL,
				Digest: testChartDigest,
			},
		},
	}

	result, err := RenderHelmRelease(ctx, input)
	require.NoError(t, err)
	require.NotNil(t, result)

	manifests := string(result.Manifests)
	assert.Contains(t, manifests, "kind: Deployment")
	// The inline value should override any chart defaults
	assert.Contains(t, manifests, "replicas: 2")
}

func TestRenderHelmRelease_HelmChartWithMissingValuesFile(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	// Test with HelmChart that references a non-existent values file
	// but ignoreMissingValuesFiles is true
	input := RenderInput{
		Documents: []byte(`
apiVersion: helm.toolkit.fluxcd.io/v2
kind: HelmRelease
metadata:
  name: podinfo
  namespace: podinfo
spec:
  interval: 50m
  chartRef:
    kind: HelmChart
    name: podinfo-chart
---
apiVersion: source.toolkit.fluxcd.io/v1
kind: HelmChart
metadata:
  name: podinfo-chart
  namespace: podinfo
spec:
  chart: podinfo
  version: "6.x"
  sourceRef:
    kind: HelmRepository
    name: podinfo
  valuesFiles:
    - nonexistent-values.yaml
  ignoreMissingValuesFiles: true
`),
		Options: RenderOptions{
			ArtifactSource: ArtifactSource{
				URL:    testChartURL,
				Digest: testChartDigest,
			},
		},
	}

	// Should succeed because ignoreMissingValuesFiles is true
	result, err := RenderHelmRelease(ctx, input)
	require.NoError(t, err)
	require.NotNil(t, result)
}

func TestRenderHelmRelease_HelmChartMissingValuesFileError(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	// Test with HelmChart that references a non-existent values file
	// and ignoreMissingValuesFiles is false (default)
	input := RenderInput{
		Documents: []byte(`
apiVersion: helm.toolkit.fluxcd.io/v2
kind: HelmRelease
metadata:
  name: podinfo
  namespace: podinfo
spec:
  interval: 50m
  chartRef:
    kind: HelmChart
    name: podinfo-chart
---
apiVersion: source.toolkit.fluxcd.io/v1
kind: HelmChart
metadata:
  name: podinfo-chart
  namespace: podinfo
spec:
  chart: podinfo
  version: "6.x"
  sourceRef:
    kind: HelmRepository
    name: podinfo
  valuesFiles:
    - nonexistent-values.yaml
`),
		Options: RenderOptions{
			ArtifactSource: ArtifactSource{
				URL:    testChartURL,
				Digest: testChartDigest,
			},
		},
	}

	// Should fail because the values file doesn't exist
	_, err := RenderHelmRelease(ctx, input)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "nonexistent-values.yaml")
}

func TestFindHelmChartForRelease(t *testing.T) {
	// Test finding HelmChart via chartRef
	t.Run("chartRef", func(t *testing.T) {
		helmCharts := map[string]*unstructured.Unstructured{
			"default/my-chart": {
				Object: map[string]interface{}{
					"apiVersion": "source.toolkit.fluxcd.io/v1",
					"kind":       "HelmChart",
					"metadata": map[string]interface{}{
						"name":      "my-chart",
						"namespace": "default",
					},
				},
			},
		}

		hrDoc := &unstructured.Unstructured{
			Object: map[string]interface{}{
				"apiVersion": "helm.toolkit.fluxcd.io/v2",
				"kind":       "HelmRelease",
				"metadata": map[string]interface{}{
					"name":      "my-release",
					"namespace": "default",
				},
			},
		}

		spec := map[string]interface{}{
			"chartRef": map[string]interface{}{
				"kind": "HelmChart",
				"name": "my-chart",
			},
		}

		result := findHelmChartForRelease(hrDoc, spec, helmCharts)
		require.NotNil(t, result)
		assert.Equal(t, "my-chart", result.GetName())
	})

	// Test finding HelmChart via implicit naming
	t.Run("implicit naming", func(t *testing.T) {
		helmCharts := map[string]*unstructured.Unstructured{
			"flux-system/default-my-release": {
				Object: map[string]interface{}{
					"apiVersion": "source.toolkit.fluxcd.io/v1",
					"kind":       "HelmChart",
					"metadata": map[string]interface{}{
						"name":      "default-my-release",
						"namespace": "flux-system",
					},
				},
			},
		}

		hrDoc := &unstructured.Unstructured{
			Object: map[string]interface{}{
				"apiVersion": "helm.toolkit.fluxcd.io/v2",
				"kind":       "HelmRelease",
				"metadata": map[string]interface{}{
					"name":      "my-release",
					"namespace": "default",
				},
			},
		}

		spec := map[string]interface{}{
			"chart": map[string]interface{}{
				"spec": map[string]interface{}{
					"chart": "some-chart",
					"sourceRef": map[string]interface{}{
						"kind":      "HelmRepository",
						"name":      "bitnami",
						"namespace": "flux-system",
					},
				},
			},
		}

		result := findHelmChartForRelease(hrDoc, spec, helmCharts)
		require.NotNil(t, result)
		assert.Equal(t, "default-my-release", result.GetName())
	})

	// Test single HelmChart fallback
	t.Run("single chart fallback", func(t *testing.T) {
		helmCharts := map[string]*unstructured.Unstructured{
			"default/only-chart": {
				Object: map[string]interface{}{
					"apiVersion": "source.toolkit.fluxcd.io/v1",
					"kind":       "HelmChart",
					"metadata": map[string]interface{}{
						"name":      "only-chart",
						"namespace": "default",
					},
				},
			},
		}

		hrDoc := &unstructured.Unstructured{
			Object: map[string]interface{}{
				"apiVersion": "helm.toolkit.fluxcd.io/v2",
				"kind":       "HelmRelease",
				"metadata": map[string]interface{}{
					"name":      "my-release",
					"namespace": "default",
				},
			},
		}

		spec := map[string]interface{}{}

		result := findHelmChartForRelease(hrDoc, spec, helmCharts)
		require.NotNil(t, result)
		assert.Equal(t, "only-chart", result.GetName())
	})

	// Test no HelmChart found
	t.Run("no chart found", func(t *testing.T) {
		helmCharts := map[string]*unstructured.Unstructured{}

		hrDoc := &unstructured.Unstructured{
			Object: map[string]interface{}{
				"apiVersion": "helm.toolkit.fluxcd.io/v2",
				"kind":       "HelmRelease",
				"metadata": map[string]interface{}{
					"name":      "my-release",
					"namespace": "default",
				},
			},
		}

		spec := map[string]interface{}{}

		result := findHelmChartForRelease(hrDoc, spec, helmCharts)
		assert.Nil(t, result)
	})
}

// TestHelmReleaseCreatesNamespace pins the extraction of the
// spec.install.createNamespace flag from a HelmRelease spec. Missing or
// non-bool values default to false.
func TestHelmReleaseCreatesNamespace(t *testing.T) {
	tcs := []struct {
		name string
		spec map[string]interface{}
		want bool
	}{
		{"no install block", map[string]interface{}{}, false},
		{"install block, no createNamespace", map[string]interface{}{
			"install": map[string]interface{}{"remediation": map[string]interface{}{}},
		}, false},
		{"createNamespace=true", map[string]interface{}{
			"install": map[string]interface{}{"createNamespace": true},
		}, true},
		{"createNamespace=false", map[string]interface{}{
			"install": map[string]interface{}{"createNamespace": false},
		}, false},
		{"createNamespace as string is ignored", map[string]interface{}{
			"install": map[string]interface{}{"createNamespace": "true"},
		}, false},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			if got := helmReleaseCreatesNamespace(tc.spec); got != tc.want {
				t.Fatalf("want %v, got %v", tc.want, got)
			}
		})
	}
}

// TestBakeNamespaceIntoFile pins both the per-document bake behaviour
// (namespaced resources get the namespace stamped, cluster-scoped and
// already-namespaced resources are left alone) and the multi-document
// preservation contract: gaby.ParseAll keeps each document's `# Source:`
// marker through the round-trip.
func TestBakeNamespaceIntoFile(t *testing.T) {
	content := "" +
		"# Source: chart/templates/cm.yaml\n" +
		"apiVersion: v1\n" +
		"kind: ConfigMap\n" +
		"metadata:\n" +
		"  name: cm\n" +
		"---\n" +
		"# Source: chart/templates/cm-explicit.yaml\n" +
		"apiVersion: v1\n" +
		"kind: ConfigMap\n" +
		"metadata:\n" +
		"  name: cm-explicit\n" +
		"  namespace: explicit\n" +
		"---\n" +
		"# Source: chart/templates/cr.yaml\n" +
		"apiVersion: rbac.authorization.k8s.io/v1\n" +
		"kind: ClusterRole\n" +
		"metadata:\n" +
		"  name: cr\n"

	out, err := bakeNamespaceIntoFile(content, "prod")
	require.NoError(t, err)

	// All three # Source: markers survive the gaby round-trip.
	assert.Contains(t, out, "# Source: chart/templates/cm.yaml")
	assert.Contains(t, out, "# Source: chart/templates/cm-explicit.yaml")
	assert.Contains(t, out, "# Source: chart/templates/cr.yaml")

	// The first ConfigMap (no explicit namespace) gets prod baked in.
	cmDocStart := strings.Index(out, "name: cm\n")
	require.True(t, cmDocStart > 0)
	cmDocEnd := strings.Index(out[cmDocStart:], "---")
	if cmDocEnd < 0 {
		cmDocEnd = len(out) - cmDocStart
	}
	assert.Contains(t, out[cmDocStart:cmDocStart+cmDocEnd], "namespace: prod")

	// The second ConfigMap keeps its explicit namespace; bake is not
	// allowed to clobber it.
	assert.Contains(t, out, "namespace: explicit")

	// ClusterRole did not receive a namespace.
	crStart := strings.Index(out, "kind: ClusterRole")
	require.True(t, crStart > 0)
	assert.NotContains(t, out[crStart:], "namespace:")
}

// TestBakeNamespaceIntoFile_NonYAMLLeavesContentUntouched confirms the
// defensive fallback: if gaby cannot parse the content (Helm sometimes
// emits comment-only files), the bake returns the content as-is.
func TestBakeNamespaceIntoFile_NonYAMLLeavesContentUntouched(t *testing.T) {
	// A stand-alone comment block is valid YAML to gaby, but is also a
	// document we shouldn't touch; verify nothing is added.
	content := "# only a comment\n"
	out, err := bakeNamespaceIntoFile(content, "prod")
	require.NoError(t, err)
	assert.NotContains(t, out, "namespace: prod")
}

// TestBakeNamespaceIntoRenderedHelm pins the top-level contract that
// drives the renderer: every chart file's resources get the destination
// namespace baked in, and a Namespace document is added as a synthetic
// file when CreateNamespace=true.
func TestBakeNamespaceIntoRenderedHelm(t *testing.T) {
	t.Run("bake only — no Namespace doc when createNamespace is false", func(t *testing.T) {
		in := map[string]string{
			"chart/templates/cm.yaml": "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: cm\n",
		}
		out, err := bakeNamespaceIntoRenderedHelm(in, "prod", false)
		require.NoError(t, err)
		assert.Contains(t, out["chart/templates/cm.yaml"], "namespace: prod")
		_, hasNS := out["__confighub_namespace.yaml"]
		assert.False(t, hasNS, "no synthetic Namespace doc expected")
	})
	t.Run("synthetic Namespace doc when createNamespace=true and namespace set", func(t *testing.T) {
		in := map[string]string{
			"chart/templates/cm.yaml": "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: cm\n",
		}
		out, err := bakeNamespaceIntoRenderedHelm(in, "prod", true)
		require.NoError(t, err)
		ns, hasNS := out["__confighub_namespace.yaml"]
		require.True(t, hasNS)
		assert.Contains(t, ns, "kind: Namespace")
		assert.Contains(t, ns, "name: prod")
	})
	t.Run("no-op when namespace empty and createNamespace false", func(t *testing.T) {
		in := map[string]string{
			"chart/templates/cm.yaml": "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: cm\n",
		}
		out, err := bakeNamespaceIntoRenderedHelm(in, "", false)
		require.NoError(t, err)
		assert.Equal(t, in, out)
	})
	t.Run("createNamespace=true but empty namespace emits no Namespace doc", func(t *testing.T) {
		in := map[string]string{
			"chart/templates/cm.yaml": "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: cm\n",
		}
		out, err := bakeNamespaceIntoRenderedHelm(in, "", true)
		require.NoError(t, err)
		_, hasNS := out["__confighub_namespace.yaml"]
		assert.False(t, hasNS)
	})
}
