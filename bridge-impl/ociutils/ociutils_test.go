// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package ociutils

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewOCIURLBuilder(t *testing.T) {
	builder := NewOCIURLBuilder("oci.confighub.com")
	assert.Equal(t, "oci.confighub.com", builder.Host)
}

func TestNewOCIURLBuilderFromAPIHost(t *testing.T) {
	tests := []struct {
		name     string
		apiHost  string
		expected string
	}{
		{
			name:     "simple host",
			apiHost:  "hub.confighub.com",
			expected: "oci.hub.confighub.com",
		},
		{
			name:     "with https prefix",
			apiHost:  "https://hub.confighub.com",
			expected: "oci.hub.confighub.com",
		},
		{
			name:     "with http prefix",
			apiHost:  "http://hub.confighub.com",
			expected: "oci.hub.confighub.com",
		},
		{
			name:     "with path",
			apiHost:  "https://hub.confighub.com/api/v1",
			expected: "oci.hub.confighub.com",
		},
		{
			name:     "localhost",
			apiHost:  "localhost:9090",
			expected: "oci.localhost:9090",
		},
		{
			name:     "IP address with port",
			apiHost:  "http://172.18.0.1:9091",
			expected: "172.18.0.1:9092",
		},
		{
			name:     "IP address without port",
			apiHost:  "http://172.18.0.1",
			expected: "172.18.0.1:9092",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			builder := NewOCIURLBuilderFromAPIHost(tt.apiHost)
			assert.Equal(t, tt.expected, builder.Host)
		})
	}
}

func TestOCIURLBuilder_UnitURL(t *testing.T) {
	builder := NewOCIURLBuilder("oci.confighub.com")

	tests := []struct {
		name      string
		spaceSlug string
		unitSlug  string
		reference string
		expected  string
	}{
		{
			name:      "head reference",
			spaceSlug: "production",
			unitSlug:  "my-deployment",
			reference: RefHead,
			expected:  "oci://oci.confighub.com/unit/production/my-deployment:head",
		},
		{
			name:      "live reference",
			spaceSlug: "staging",
			unitSlug:  "backend-service",
			reference: RefLive,
			expected:  "oci://oci.confighub.com/unit/staging/backend-service:live",
		},
		{
			name:      "revision reference",
			spaceSlug: "dev",
			unitSlug:  "config",
			reference: "v5",
			expected:  "oci://oci.confighub.com/unit/dev/config:v5",
		},
		{
			name:      "uppercase unit slug is lowercased",
			spaceSlug: "demo-argo2",
			unitSlug:  "argocd-cubbychat-Application-wet",
			reference: RefLatest,
			expected:  "oci://oci.confighub.com/unit/demo-argo2/argocd-cubbychat-application-wet:latest",
		},
		{
			name:      "uppercase space slug is lowercased",
			spaceSlug: "My-Space",
			unitSlug:  "my-unit",
			reference: RefHead,
			expected:  "oci://oci.confighub.com/unit/my-space/my-unit:head",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			url := builder.UnitURL(tt.spaceSlug, tt.unitSlug, tt.reference)
			assert.Equal(t, tt.expected, url)
		})
	}
}

func TestOCIURLBuilder_TargetURL(t *testing.T) {
	builder := NewOCIURLBuilder("oci.confighub.com")

	tests := []struct {
		name       string
		spaceSlug  string
		targetSlug string
		reference  string
		expected   string
	}{
		{
			name:       "head reference",
			spaceSlug:  "production",
			targetSlug: "k8s-cluster",
			reference:  RefHead,
			expected:   "oci://oci.confighub.com/target/production/k8s-cluster:head",
		},
		{
			name:       "live reference",
			spaceSlug:  "staging",
			targetSlug: "edge-nodes",
			reference:  RefLive,
			expected:   "oci://oci.confighub.com/target/staging/edge-nodes:live",
		},
		{
			name:       "uppercase target slug is lowercased",
			spaceSlug:  "My-Space",
			targetSlug: "K8s-Cluster",
			reference:  RefHead,
			expected:   "oci://oci.confighub.com/target/my-space/k8s-cluster:head",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			url := builder.TargetURL(tt.spaceSlug, tt.targetSlug, tt.reference)
			assert.Equal(t, tt.expected, url)
		})
	}
}

func TestRevisionRef(t *testing.T) {
	assert.Equal(t, "v1", RevisionRef(1))
	assert.Equal(t, "v42", RevisionRef(42))
	assert.Equal(t, "v0", RevisionRef(0))
}

func TestOCIURLBuilder_UnitURLFromInfo(t *testing.T) {
	builder := NewOCIURLBuilder("oci.confighub.com")

	info := UnitOCIInfo{
		SpaceSlug: "prod",
		UnitSlug:  "app",
	}
	url := builder.UnitURLFromInfo(info)
	assert.Equal(t, "oci://oci.confighub.com/unit/prod/app:latest", url)
}

func TestOCIURLBuilder_TargetURLFromInfo(t *testing.T) {
	builder := NewOCIURLBuilder("oci.confighub.com")

	info := TargetOCIInfo{
		SpaceSlug:  "prod",
		TargetSlug: "cluster",
	}
	url := builder.TargetURLFromInfo(info)
	assert.Equal(t, "oci://oci.confighub.com/target/prod/cluster:latest", url)
}

func TestParseOCIURL(t *testing.T) {
	tests := []struct {
		name        string
		url         string
		expected    *ParsedOCIURL
		expectError bool
	}{
		{
			name: "valid unit URL",
			url:  "oci://oci.confighub.com/unit/production/my-deployment:live",
			expected: &ParsedOCIURL{
				Host:         "oci.confighub.com",
				ResourceType: "unit",
				SpaceSlug:    "production",
				ResourceSlug: "my-deployment",
				Reference:    "live",
			},
		},
		{
			name: "valid target URL",
			url:  "oci://oci.confighub.com/target/staging/k8s-cluster:head",
			expected: &ParsedOCIURL{
				Host:         "oci.confighub.com",
				ResourceType: "target",
				SpaceSlug:    "staging",
				ResourceSlug: "k8s-cluster",
				Reference:    "head",
			},
		},
		{
			name: "revision reference",
			url:  "oci://oci.confighub.com/unit/dev/config:v42",
			expected: &ParsedOCIURL{
				Host:         "oci.confighub.com",
				ResourceType: "unit",
				SpaceSlug:    "dev",
				ResourceSlug: "config",
				Reference:    "v42",
			},
		},
		{
			name: "no reference defaults to latest",
			url:  "oci://oci.confighub.com/unit/prod/app",
			expected: &ParsedOCIURL{
				Host:         "oci.confighub.com",
				ResourceType: "unit",
				SpaceSlug:    "prod",
				ResourceSlug: "app",
				Reference:    "latest",
			},
		},
		{
			name:        "missing oci prefix",
			url:         "https://oci.confighub.com/unit/prod/app:live",
			expectError: true,
		},
		{
			name:        "missing path",
			url:         "oci://oci.confighub.com",
			expectError: true,
		},
		{
			name:        "invalid resource type",
			url:         "oci://oci.confighub.com/invalid/prod/app:live",
			expectError: true,
		},
		{
			name:        "too few path components",
			url:         "oci://oci.confighub.com/unit/prod",
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			parsed, err := ParseOCIURL(tt.url)
			if tt.expectError {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.expected, parsed)
		})
	}
}

func TestParsedOCIURL_IsUnit(t *testing.T) {
	unitURL := &ParsedOCIURL{ResourceType: "unit"}
	targetURL := &ParsedOCIURL{ResourceType: "target"}

	assert.True(t, unitURL.IsUnit())
	assert.False(t, unitURL.IsTarget())
	assert.False(t, targetURL.IsUnit())
	assert.True(t, targetURL.IsTarget())
}

func TestParsedOCIURL_String(t *testing.T) {
	parsed := &ParsedOCIURL{
		Host:         "oci.confighub.com",
		ResourceType: "unit",
		SpaceSlug:    "production",
		ResourceSlug: "my-deployment",
		Reference:    "live",
	}

	assert.Equal(t, "oci://oci.confighub.com/unit/production/my-deployment:live", parsed.String())
}

func TestRoundTrip(t *testing.T) {
	builder := NewOCIURLBuilder("oci.confighub.com")

	// Generate URL
	originalURL := builder.UnitURL("production", "my-deployment", "live")

	// Parse it
	parsed, err := ParseOCIURL(originalURL)
	require.NoError(t, err)

	// Convert back to string
	reconstructed := parsed.String()

	assert.Equal(t, originalURL, reconstructed)
}
