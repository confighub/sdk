// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package helmutils

import (
	"fmt"
	"os"

	"helm.sh/helm/v3/pkg/chartutil"
	"helm.sh/helm/v3/pkg/strvals"
	"sigs.k8s.io/yaml"
)

const (
	// HelmSourceAPIVersion is the apiVersion of the HelmSource document.
	HelmSourceAPIVersion = "confighub.com/v1alpha1"
	// HelmSourceKind is the kind of the HelmSource document.
	HelmSourceKind = "HelmSource"
	// PlaceholderNamespace is rendered into the base variant when no release
	// namespace is specified; deployments replace it via set-namespace.
	PlaceholderNamespace = "confighubplaceholder"
)

// HelmSource is the KRM-shaped document stored in a helm source space unit.
// One HelmSource describes one Helm release: which chart to render, with which
// values, and how the rendered output maps to units in the base variant space.
// It is the source of truth for upgrades — nothing about a release lives only
// on a command line.
type HelmSource struct {
	APIVersion string             `json:"apiVersion"`
	Kind       string             `json:"kind"`
	Metadata   HelmSourceMetadata `json:"metadata"`
	Spec       HelmSourceSpec     `json:"spec"`
	Status     HelmSourceStatus   `json:"status,omitempty"`
}

type HelmSourceMetadata struct {
	Name string `json:"name"`
}

type HelmSourceSpec struct {
	Chart   HelmSourceChart   `json:"chart"`
	Release HelmSourceRelease `json:"release"`
	// CreateNamespace synthesizes a Namespace unit for the release namespace,
	// mirroring helm install --create-namespace. Synthesis is skipped when the
	// rendered output already contains a Namespace resource with that name.
	CreateNamespace bool `json:"createNamespace,omitempty"`
	// UnitPrefix namespaces the generated unit slugs in the base space. Empty
	// is allowed for exactly one HelmSource per source space.
	UnitPrefix   string `json:"unitPrefix,omitempty"`
	IncludeHooks bool   `json:"includeHooks,omitempty"`
	SkipCRDs     bool   `json:"skipCRDs,omitempty"`
	// Values are the fully merged user-supplied values.
	Values map[string]any `json:"values,omitempty"`
}

type HelmSourceChart struct {
	// Ref locates the chart: an oci:// reference, a local path, or a chart
	// name resolved against Repo.
	Ref string `json:"ref"`
	// Repo is a classic chart repository URL, used when Ref is a bare chart name.
	Repo string `json:"repo,omitempty"`
	// Version is the chart version constraint as given by the user (exact or range).
	Version string `json:"version,omitempty"`
}

type HelmSourceRelease struct {
	Name string `json:"name"`
	// Namespace is the release namespace. Empty means the placeholder
	// namespace, filled per deployment by cub variant create --namespace.
	Namespace string `json:"namespace,omitempty"`
}

// HelmSourceStatus records render outcomes so regeneration is reproducible.
type HelmSourceStatus struct {
	// ResolvedVersion is the concrete chart version the constraint resolved to.
	ResolvedVersion string `json:"resolvedVersion,omitempty"`
	AppVersion      string `json:"appVersion,omitempty"`
}

// RenderNamespace returns the namespace used during rendering: the release
// namespace, or the placeholder when none is specified.
func (s *HelmSource) RenderNamespace() string {
	if s.Spec.Release.Namespace == "" {
		return PlaceholderNamespace
	}
	return s.Spec.Release.Namespace
}

// ParseHelmSource parses and validates a HelmSource document.
func ParseHelmSource(data []byte) (*HelmSource, error) {
	var src HelmSource
	if err := yaml.Unmarshal(data, &src); err != nil {
		return nil, fmt.Errorf("failed to parse HelmSource document: %w", err)
	}
	if src.APIVersion != HelmSourceAPIVersion || src.Kind != HelmSourceKind {
		return nil, fmt.Errorf("not a HelmSource document: got apiVersion %q, kind %q", src.APIVersion, src.Kind)
	}
	if src.Spec.Release.Name == "" {
		return nil, fmt.Errorf("HelmSource %q has no spec.release.name", src.Metadata.Name)
	}
	if src.Spec.Chart.Ref == "" {
		return nil, fmt.Errorf("HelmSource %q has no spec.chart.ref", src.Metadata.Name)
	}
	return &src, nil
}

// Marshal serializes the HelmSource document to YAML.
func (s *HelmSource) Marshal() ([]byte, error) {
	data, err := yaml.Marshal(s)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal HelmSource document: %w", err)
	}
	return data, nil
}

// MergeValues loads and merges Helm values from files and --set expressions
// with standard Helm precedence: defaults < file1 < file2 < ... < --set.
func MergeValues(valuesFiles []string, setValues []string) (map[string]any, error) {
	mergedValues := map[string]any{}

	for _, filePath := range valuesFiles {
		data, err := os.ReadFile(filePath)
		if err != nil {
			return nil, fmt.Errorf("cannot read values file %s: %w", filePath, err)
		}
		fileValues := map[string]any{}
		if err := yaml.Unmarshal(data, &fileValues); err != nil {
			return nil, fmt.Errorf("cannot parse values file %s: %w", filePath, err)
		}
		mergedValues = chartutil.CoalesceTables(fileValues, mergedValues)
	}

	for _, val := range setValues {
		if err := strvals.ParseInto(val, mergedValues); err != nil {
			return nil, fmt.Errorf("failed to parse --set value %q: %w", val, err)
		}
	}

	return mergedValues, nil
}
