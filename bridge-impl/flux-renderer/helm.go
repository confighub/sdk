// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package fluxrenderer

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"helm.sh/helm/v3/pkg/chart"
	"helm.sh/helm/v3/pkg/chartutil"
	"helm.sh/helm/v3/pkg/engine"
	"sigs.k8s.io/yaml"

	"github.com/confighub/sdk/bridge-impl/third_party/fluxcd/helm-controller/loader"
)

const (
	// HelmReleaseKind is the kind for Flux HelmRelease resources.
	HelmReleaseKind = "HelmRelease"
	// HelmReleaseAPIVersionPrefix is the API version prefix for Flux HelmRelease.
	HelmReleaseAPIVersionPrefix = "helm.toolkit.fluxcd.io/"

	// HelmChartKind is the kind for Flux HelmChart resources.
	HelmChartKind = "HelmChart"
	// HelmChartAPIVersionPrefix is the API version prefix for Flux HelmChart.
	HelmChartAPIVersionPrefix = "source.toolkit.fluxcd.io/"
)

// RenderHelmRelease renders a HelmRelease to Kubernetes manifests.
// The input can be a multi-document YAML containing:
// - HelmRelease (required)
// - ConfigMaps/Secrets referenced by ValuesFrom (optional)
func RenderHelmRelease(ctx context.Context, input RenderInput) (*RenderResult, error) {
	// Parse all documents from input
	parsed, err := ParseDocumentsWithResources(input.Documents)
	if err != nil {
		return nil, fmt.Errorf("failed to parse input documents: %w", err)
	}

	// Find the HelmRelease document
	hrDoc := findDocumentByKindAndAPIVersion(parsed.Documents, HelmReleaseKind, HelmReleaseAPIVersionPrefix)
	if hrDoc == nil {
		return nil, fmt.Errorf("no HelmRelease found in input")
	}

	return renderHelmReleaseFromParsed(ctx, hrDoc, parsed, input.Options)
}

// renderHelmReleaseFromParsed renders a HelmRelease using pre-parsed documents.
// This is called by RenderFlux after it has parsed the documents and determined the resource type.
func renderHelmReleaseFromParsed(ctx context.Context, hrDoc *unstructured.Unstructured, parsed *ParsedDocuments, options RenderOptions) (*RenderResult, error) {
	// Extract HelmRelease spec fields we need
	spec, ok := hrDoc.Object["spec"].(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("HelmRelease has no spec")
	}

	// Get release name and namespace
	releaseName := options.ReleaseName
	if releaseName == "" {
		releaseName = getHelmReleaseName(hrDoc, spec)
	}

	namespace := options.Namespace
	if namespace == "" {
		namespace = getHelmReleaseNamespace(hrDoc, spec)
	}

	// Load chart from artifact source
	if options.ArtifactSource.URL == "" {
		return nil, fmt.Errorf("artifact source URL is required")
	}

	httpClient := loader.NewRetryableHTTPClient(ctx, 3)
	loadedChart, err := loader.SecureLoadChartFromURL(httpClient, options.ArtifactSource.URL, options.ArtifactSource.Digest)
	if err != nil {
		return nil, fmt.Errorf("failed to load chart: %w", err)
	}

	// Find the HelmChart if present (for valuesFiles)
	helmChart := findHelmChartForRelease(hrDoc, spec, parsed.HelmCharts)

	// Build values in the correct order of precedence:
	// 1. Chart's default values.yaml (handled by Helm internally)
	// 2. HelmChart's valuesFiles (in order, last overrides first)
	// 3. HelmRelease's inline spec.values
	// 4. HelmRelease's valuesFrom (later sources override earlier)
	values := make(map[string]interface{})

	// Apply HelmChart valuesFiles if present
	if helmChart != nil {
		chartValues, err := loadValuesFilesFromChart(loadedChart, helmChart)
		if err != nil {
			return nil, fmt.Errorf("failed to load values files from chart: %w", err)
		}
		values = chartValues
	}

	// Apply HelmRelease inline values and valuesFrom
	releaseValues, err := buildHelmValues(spec, hrDoc.GetNamespace(), parsed.ConfigMaps, parsed.Secrets)
	if err != nil {
		return nil, fmt.Errorf("failed to build values: %w", err)
	}

	// Merge release values on top of chart values
	values = chartutil.CoalesceTables(releaseValues, values)

	// Process chart dependencies
	if err := chartutil.ProcessDependencies(loadedChart, values); err != nil {
		return nil, fmt.Errorf("failed to process chart dependencies: %w", err)
	}

	// Build render values
	releaseOpts := chartutil.ReleaseOptions{
		Name:      releaseName,
		Namespace: namespace,
		Revision:  1,
		IsInstall: true,
	}

	valuesToRender, err := chartutil.ToRenderValues(loadedChart, values, releaseOpts, chartutil.DefaultCapabilities)
	if err != nil {
		return nil, fmt.Errorf("failed to prepare render values: %w", err)
	}

	// Render templates
	renderEngine := engine.Engine{}
	rendered, err := renderEngine.Render(loadedChart, valuesToRender)
	if err != nil {
		return nil, fmt.Errorf("failed to render chart: %w", err)
	}

	// Combine rendered templates into YAML document list
	manifests := combineRenderedTemplates(rendered, loadedChart.Name())

	return &RenderResult{
		Manifests: []byte(manifests),
		Revision:  loadedChart.Metadata.Version,
	}, nil
}

// findHelmChartForRelease finds the HelmChart referenced by a HelmRelease.
// It checks:
// 1. spec.chartRef if it references a HelmChart
// 2. The implicit naming convention <namespace>-<name> for spec.chart
func findHelmChartForRelease(hrDoc *unstructured.Unstructured, spec map[string]interface{}, helmCharts map[string]*unstructured.Unstructured) *unstructured.Unstructured {
	// Check if there's a chartRef pointing to a HelmChart
	if chartRef, ok := spec["chartRef"].(map[string]interface{}); ok {
		if kind, ok := chartRef["kind"].(string); ok && kind == HelmChartKind {
			name, _ := chartRef["name"].(string)
			namespace := hrDoc.GetNamespace()
			if ns, ok := chartRef["namespace"].(string); ok && ns != "" {
				namespace = ns
			}

			key := resourceKey(namespace, name)
			if hc, ok := helmCharts[key]; ok {
				return hc
			}
			// Try without namespace
			if hc, ok := helmCharts[name]; ok {
				return hc
			}
		}
	}

	// Check for implicitly created HelmChart (naming convention: <namespace>-<name>)
	// The HelmChart is created in the sourceRef namespace with this naming pattern
	if chartSpec, ok := spec["chart"].(map[string]interface{}); ok {
		if innerSpec, ok := chartSpec["spec"].(map[string]interface{}); ok {
			if sourceRef, ok := innerSpec["sourceRef"].(map[string]interface{}); ok {
				sourceNamespace := hrDoc.GetNamespace()
				if ns, ok := sourceRef["namespace"].(string); ok && ns != "" {
					sourceNamespace = ns
				}

				// The implicit HelmChart name follows the pattern: <HelmRelease namespace>-<HelmRelease name>
				implicitName := hrDoc.GetNamespace() + "-" + hrDoc.GetName()
				key := resourceKey(sourceNamespace, implicitName)
				if hc, ok := helmCharts[key]; ok {
					return hc
				}
				// Try without namespace
				if hc, ok := helmCharts[implicitName]; ok {
					return hc
				}
			}
		}
	}

	// If there's only one HelmChart in the input, use it
	if len(helmCharts) == 1 {
		for _, hc := range helmCharts {
			return hc
		}
	}

	return nil
}

// loadValuesFilesFromChart loads and merges values files specified in a HelmChart.
func loadValuesFilesFromChart(loadedChart *chart.Chart, helmChart *unstructured.Unstructured) (map[string]interface{}, error) {
	spec, ok := helmChart.Object["spec"].(map[string]interface{})
	if !ok {
		return nil, nil
	}

	valuesFilesRaw, ok := spec["valuesFiles"].([]interface{})
	if !ok || len(valuesFilesRaw) == 0 {
		return nil, nil
	}

	ignoreMissing := false
	if ignore, ok := spec["ignoreMissingValuesFiles"].(bool); ok {
		ignoreMissing = ignore
	}

	// Convert to string slice
	var valuesFiles []string
	for _, vf := range valuesFilesRaw {
		if s, ok := vf.(string); ok {
			valuesFiles = append(valuesFiles, s)
		}
	}

	// Load and merge values files in order (last overrides first)
	result := make(map[string]interface{})
	for _, filename := range valuesFiles {
		content, err := getChartFileContent(loadedChart, filename)
		if err != nil {
			if ignoreMissing {
				continue
			}
			return nil, fmt.Errorf("failed to read values file %s: %w", filename, err)
		}

		var fileValues map[string]interface{}
		if err := yaml.Unmarshal([]byte(content), &fileValues); err != nil {
			return nil, fmt.Errorf("failed to parse values file %s: %w", filename, err)
		}

		// Merge: later files override earlier files
		result = chartutil.CoalesceTables(fileValues, result)
	}

	return result, nil
}

// getChartFileContent retrieves the content of a file from a loaded Helm chart.
func getChartFileContent(c *chart.Chart, filename string) (string, error) {
	// Check in the chart's Files
	for _, f := range c.Files {
		if f.Name == filename {
			return string(f.Data), nil
		}
	}

	// Check in raw files (some charts package values files differently)
	for _, f := range c.Raw {
		if f.Name == filename {
			return string(f.Data), nil
		}
	}

	return "", fmt.Errorf("file %s not found in chart", filename)
}

// getHelmReleaseName extracts the release name from HelmRelease spec or defaults to metadata.name.
func getHelmReleaseName(hr *unstructured.Unstructured, spec map[string]interface{}) string {
	// Check spec.releaseName
	if releaseName, ok := spec["releaseName"].(string); ok && releaseName != "" {
		return releaseName
	}

	// Default to metadata.name
	return hr.GetName()
}

// getHelmReleaseNamespace extracts the release namespace from HelmRelease spec or defaults to metadata.namespace.
func getHelmReleaseNamespace(hr *unstructured.Unstructured, spec map[string]interface{}) string {
	// Check spec.targetNamespace
	if targetNs, ok := spec["targetNamespace"].(string); ok && targetNs != "" {
		return targetNs
	}

	// Default to metadata.namespace
	ns := hr.GetNamespace()
	if ns == "" {
		return "default"
	}
	return ns
}

// buildHelmValues builds the values map from spec.values and spec.valuesFrom.
func buildHelmValues(spec map[string]interface{}, defaultNamespace string,
	configMaps map[string]*corev1.ConfigMap, secrets map[string]*corev1.Secret) (map[string]interface{}, error) {

	// Start with inline values from spec.values
	values := make(map[string]interface{})
	if specValues, ok := spec["values"].(map[string]interface{}); ok {
		values = deepCopyMap(specValues)
	}

	// Process valuesFrom references
	valuesFrom, ok := spec["valuesFrom"].([]interface{})
	if !ok {
		return values, nil
	}

	for _, vfRaw := range valuesFrom {
		vf, ok := vfRaw.(map[string]interface{})
		if !ok {
			continue
		}

		kind, _ := vf["kind"].(string)
		name, _ := vf["name"].(string)
		valuesKey, _ := vf["valuesKey"].(string)
		targetNamespace, _ := vf["targetNamespace"].(string)
		optional, _ := vf["optional"].(bool)

		if valuesKey == "" {
			valuesKey = "values.yaml"
		}

		ns := targetNamespace
		if ns == "" {
			ns = defaultNamespace
		}

		var data string
		var err error

		switch kind {
		case "ConfigMap":
			data, err = getConfigMapData(configMaps, ns, name, valuesKey)
		case "Secret":
			data, err = getSecretData(secrets, ns, name, valuesKey)
		default:
			if !optional {
				return nil, fmt.Errorf("unsupported valuesFrom kind: %s", kind)
			}
			continue
		}

		if err != nil {
			if optional {
				continue
			}
			return nil, fmt.Errorf("failed to get values from %s/%s: %w", kind, name, err)
		}

		// Parse the values data
		var refValues map[string]interface{}
		if err := yaml.Unmarshal([]byte(data), &refValues); err != nil {
			return nil, fmt.Errorf("failed to parse values from %s/%s: %w", kind, name, err)
		}

		// Merge values (valuesFrom override inline values, like Flux does)
		values = chartutil.CoalesceTables(refValues, values)
	}

	return values, nil
}

// deepCopyMap creates a deep copy of a map[string]interface{}.
func deepCopyMap(m map[string]interface{}) map[string]interface{} {
	result := make(map[string]interface{})
	for k, v := range m {
		switch val := v.(type) {
		case map[string]interface{}:
			result[k] = deepCopyMap(val)
		case []interface{}:
			result[k] = deepCopySlice(val)
		default:
			result[k] = v
		}
	}
	return result
}

// deepCopySlice creates a deep copy of a []interface{}.
func deepCopySlice(s []interface{}) []interface{} {
	result := make([]interface{}, len(s))
	for i, v := range s {
		switch val := v.(type) {
		case map[string]interface{}:
			result[i] = deepCopyMap(val)
		case []interface{}:
			result[i] = deepCopySlice(val)
		default:
			result[i] = v
		}
	}
	return result
}

