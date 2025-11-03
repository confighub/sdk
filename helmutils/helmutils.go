// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package helmutils

// Helm label constants
// All label values are sourced from Chart.yaml metadata
const (
	HelmChartLabel           = "HelmChart"           // Chart name from Chart.yaml metadata.Name
	HelmReleaseLabel         = "HelmRelease"         // Release name specified by user
	HelmChartAPIVersionLabel = "HelmChartAPIVersion" // Chart API version from Chart.yaml metadata.APIVersion (e.g., "v2")
	HelmChartVersionLabel    = "HelmChartVersion"    // Chart version from Chart.yaml metadata.Version (semver)
	HelmAppVersionLabel      = "HelmAppVersion"      // Application version from Chart.yaml metadata.AppVersion
)

// HelmMetadata represents Helm chart metadata stored in unit labels
type HelmMetadata struct {
	ReleaseName     string
	ChartName       string
	ChartAPIVersion string
	ChartVersion    string
	AppVersion      string
}

// IsHelmChart determines if a unit is a Helm chart by checking for all required Helm metadata labels.
// A unit is considered a Helm chart only if it has ALL required Helm labels set with non-empty values.
// These labels are set by 'cub helm install' and serve as the authoritative indicator.
// Required labels: HelmRelease, HelmChart, HelmChartVersion, HelmChartAPIVersion
func IsHelmChart(unitLabels map[string]string) bool {
	if unitLabels == nil {
		return false
	}

	// Check that all required Helm labels are present and non-empty
	requiredLabels := []string{
		HelmReleaseLabel,
		HelmChartLabel,
		HelmChartVersionLabel,
		HelmChartAPIVersionLabel,
	}

	for _, label := range requiredLabels {
		if val, ok := unitLabels[label]; !ok || val == "" {
			return false
		}
	}

	return true
}

// ExtractHelmMetadata extracts Helm chart metadata from unit labels.
// Unit labels are set by 'cub helm install' and contain authoritative Helm metadata.
// If a label is not found, it uses the provided default value or a standard default.
//
// Parameters:
//   - unitLabels: Map of unit labels containing Helm metadata
//   - unitSlug: The unit slug to use as default for ReleaseName if not found in labels
//
// Returns:
//   - *HelmMetadata: Extracted Helm metadata with defaults applied where necessary
func ExtractHelmMetadata(unitLabels map[string]string, unitSlug string) *HelmMetadata {
	return &HelmMetadata{
		ReleaseName:     getLabel(unitLabels, HelmReleaseLabel, unitSlug),
		ChartName:       getLabel(unitLabels, HelmChartLabel, "unknown"),
		ChartAPIVersion: getLabel(unitLabels, HelmChartAPIVersionLabel, "v2"),
		ChartVersion:    getLabel(unitLabels, HelmChartVersionLabel, "1.0.0"),
		AppVersion:      getLabel(unitLabels, HelmAppVersionLabel, ""),
	}
}

// getLabel retrieves a label value with fallback to default.
// This is a helper function used internally by ExtractHelmMetadata.
func getLabel(unitLabels map[string]string, key, defaultVal string) string {
	if unitLabels == nil {
		return defaultVal
	}
	if val, ok := unitLabels[key]; ok && val != "" {
		return val
	}
	return defaultVal
}
