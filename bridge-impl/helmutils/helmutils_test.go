// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package helmutils

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestIsHelmChart(t *testing.T) {
	t.Run("nil labels", func(t *testing.T) {
		assert.False(t, IsHelmChart(nil))
	})

	t.Run("empty map", func(t *testing.T) {
		assert.False(t, IsHelmChart(map[string]string{}))
	})

	t.Run("missing required label", func(t *testing.T) {
		labels := map[string]string{
			HelmReleaseLabel:      "my-release",
			HelmChartLabel:        "nginx",
			HelmChartVersionLabel: "1.0.0",
			// Missing HelmChartAPIVersionLabel
		}
		assert.False(t, IsHelmChart(labels))
	})

	t.Run("empty value for required label", func(t *testing.T) {
		labels := map[string]string{
			HelmReleaseLabel:         "my-release",
			HelmChartLabel:           "nginx",
			HelmChartVersionLabel:    "1.0.0",
			HelmChartAPIVersionLabel: "",
		}
		assert.False(t, IsHelmChart(labels))
	})

	t.Run("all required labels present", func(t *testing.T) {
		labels := map[string]string{
			HelmReleaseLabel:         "my-release",
			HelmChartLabel:           "nginx",
			HelmChartVersionLabel:    "1.0.0",
			HelmChartAPIVersionLabel: "v2",
		}
		assert.True(t, IsHelmChart(labels))
	})
}

func TestExtractHelmMetadata(t *testing.T) {
	t.Run("nil labels uses defaults", func(t *testing.T) {
		meta := ExtractHelmMetadata(nil, "my-unit")
		assert.Equal(t, "my-unit", meta.ReleaseName)
		assert.Equal(t, "unknown", meta.ChartName)
		assert.Equal(t, "v2", meta.ChartAPIVersion)
		assert.Equal(t, "1.0.0", meta.ChartVersion)
		assert.Equal(t, "", meta.AppVersion)
	})

	t.Run("all labels populated", func(t *testing.T) {
		labels := map[string]string{
			HelmReleaseLabel:         "my-release",
			HelmChartLabel:           "nginx",
			HelmChartAPIVersionLabel: "v2",
			HelmChartVersionLabel:    "1.2.3",
			HelmAppVersionLabel:      "1.25.0",
		}
		meta := ExtractHelmMetadata(labels, "fallback-slug")
		assert.Equal(t, "my-release", meta.ReleaseName)
		assert.Equal(t, "nginx", meta.ChartName)
		assert.Equal(t, "v2", meta.ChartAPIVersion)
		assert.Equal(t, "1.2.3", meta.ChartVersion)
		assert.Equal(t, "1.25.0", meta.AppVersion)
	})
}
