// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

// Package ociutils provides utilities for generating OCI registry URLs
// for ConfigHub units and targets.
package ociutils

import (
	"fmt"
	"net"
	"strconv"
	"strings"
)

// Reference tag constants for OCI URLs
const (
	// RefHead points to the current unit state (head revision)
	RefHead = "head"
	// RefLatest is an alias for RefHead
	RefLatest = "latest"
	// RefLive points to the currently applied/live revision
	RefLive = "live"
	// RefLastApplied points to the last applied revision
	RefLastApplied = "last-applied"
)

// OCIURLBuilder helps construct OCI registry URLs for ConfigHub resources.
type OCIURLBuilder struct {
	// Host is the OCI registry host (e.g., "oci.confighub.com")
	Host string
}

// NewOCIURLBuilder creates a new OCIURLBuilder with the given host.
// If host is empty, it defaults to constructing from the API host.
func NewOCIURLBuilder(host string) *OCIURLBuilder {
	return &OCIURLBuilder{Host: host}
}

// NewOCIURLBuilderFromAPIHost creates a new OCIURLBuilder by deriving
// the OCI host from the API host (e.g., "hub.confighub.com" -> "oci.hub.confighub.com").
// For IP addresses, the "oci." prefix is not added, and the port is changed to 9092 (OCI server port).
func NewOCIURLBuilderFromAPIHost(apiHost string) *OCIURLBuilder {
	// Remove protocol if present
	host := strings.TrimPrefix(apiHost, "https://")
	host = strings.TrimPrefix(host, "http://")

	// Remove trailing slash and path
	if idx := strings.Index(host, "/"); idx != -1 {
		host = host[:idx]
	}

	// Extract hostname without port for IP detection
	hostname := host
	if colonIdx := strings.LastIndex(host, ":"); colonIdx != -1 {
		hostname = host[:colonIdx]
	}

	// Check if it's an IP address
	var ociHost string
	if net.ParseIP(hostname) != nil {
		// For IP addresses, don't add "oci." prefix, use OCI server port 9092
		ociHost = hostname + ":9092"
	} else {
		// For domain names, prepend "oci." to the host
		ociHost = "oci." + host
	}

	return &OCIURLBuilder{Host: ociHost}
}

// UnitURL generates an OCI URL for a specific unit.
// Format: oci://{host}/unit/{spaceSlug}/{unitSlug}:{reference}
//
// DEAD ENDPOINT. The ConfigHub OCI server no longer serves /v2/unit/... — that
// path was removed with the on-the-fly bundle generation it depended on (see
// docs/specs/bridge-sunset-plan.md, stage 0). URLs built here resolve to 400.
// The only remaining callers are the ArgoCD and Flux OCI bridges, which are
// deleted in stage 3; this builder goes with them.
//
// Parameters:
//   - spaceSlug: The space slug (e.g., "production")
//   - unitSlug: The unit slug (e.g., "my-deployment")
//   - reference: The reference tag (e.g., "head", "live", "v5")
//
// Example: oci://oci.confighub.com/unit/production/my-deployment:live
func (b *OCIURLBuilder) UnitURL(spaceSlug, unitSlug, reference string) string {
	return fmt.Sprintf("oci://%s/unit/%s/%s:%s", b.Host, strings.ToLower(spaceSlug), strings.ToLower(unitSlug), reference)
}

// TargetURL generates an OCI URL for a target bundle.
// Format: oci://{host}/target/{spaceSlug}/{targetSlug}:{reference}
//
// DEAD ENDPOINT. See UnitURL — /v2/target/... was removed alongside it.
//
// Parameters:
//   - spaceSlug: The space slug (e.g., "production")
//   - targetSlug: The target slug (e.g., "k8s-cluster")
//   - reference: The reference tag (e.g., "head", "live", "v5")
//
// Example: oci://oci.confighub.com/target/production/k8s-cluster:live
func (b *OCIURLBuilder) TargetURL(spaceSlug, targetSlug, reference string) string {
	return fmt.Sprintf("oci://%s/target/%s/%s:%s", b.Host, strings.ToLower(spaceSlug), strings.ToLower(targetSlug), reference)
}

// RevisionRef returns a reference tag for a specific revision number.
// Format: v{revisionNum} (e.g., "v5", "v42")
func RevisionRef(revisionNum int) string {
	return fmt.Sprintf("v%d", revisionNum)
}

// HelmRevisionRef returns a SemVer 2.0 OCI tag pinned to a specific
// ConfigHub revision number for Helm units. Format: 0.{revisionNum}.0
// (e.g., "0.5.0", "0.42.0").
//
// Helm OCI consumers (ArgoCD HelmSource, Flux HelmRepository/HelmRelease)
// require the targetRevision/version field to be a valid SemVer 2.0
// string, so plain "v{N}" or bare "{N}" cannot be used. The 0.{N}.0
// shape sorts numerically by minor version per SemVer §11 (numeric
// identifiers compare numerically), avoiding the lexical-sort hazard
// of letter prefixes. The Helm chart's actual version is preserved on
// the OCI artifact's manifest annotations rather than the tag.
func HelmRevisionRef(revisionNum int) string {
	return fmt.Sprintf("0.%d.0", revisionNum)
}

// ParseHelmRevisionRef parses a tag in the HelmRevisionRef format and
// returns the revision number it pins. Returns (n, true) if ref is
// "0.{n}.0" with n > 0, otherwise (0, false).
func ParseHelmRevisionRef(ref string) (int, bool) {
	const prefix = "0."
	const suffix = ".0"
	if !strings.HasPrefix(ref, prefix) || !strings.HasSuffix(ref, suffix) {
		return 0, false
	}
	mid := ref[len(prefix) : len(ref)-len(suffix)]
	if mid == "" {
		return 0, false
	}
	n, err := strconv.Atoi(mid)
	if err != nil || n <= 0 {
		return 0, false
	}
	return n, true
}

// UnitOCIInfo contains the information needed to construct a unit OCI URL.
type UnitOCIInfo struct {
	SpaceSlug string
	UnitSlug  string
}

// UnitURLFromInfo generates an OCI URL from UnitOCIInfo using "latest" reference.
func (b *OCIURLBuilder) UnitURLFromInfo(info UnitOCIInfo) string {
	return b.UnitURL(info.SpaceSlug, info.UnitSlug, RefLatest)
}

// TargetOCIInfo contains the information needed to construct a target OCI URL.
type TargetOCIInfo struct {
	SpaceSlug  string
	TargetSlug string
}

// TargetURLFromInfo generates an OCI URL from TargetOCIInfo using "latest" reference.
func (b *OCIURLBuilder) TargetURLFromInfo(info TargetOCIInfo) string {
	return b.TargetURL(info.SpaceSlug, info.TargetSlug, RefLatest)
}

// ParseOCIURL parses an OCI URL and returns its components.
// Returns an error if the URL format is invalid.
func ParseOCIURL(url string) (*ParsedOCIURL, error) {
	// Remove oci:// prefix
	if !strings.HasPrefix(url, "oci://") {
		return nil, fmt.Errorf("invalid OCI URL: must start with oci://")
	}
	url = strings.TrimPrefix(url, "oci://")

	// Split host and path
	parts := strings.SplitN(url, "/", 2)
	if len(parts) != 2 {
		return nil, fmt.Errorf("invalid OCI URL: missing path")
	}

	host := parts[0]
	pathWithRef := parts[1]

	// Split path and reference
	var path, reference string
	if colonIdx := strings.LastIndex(pathWithRef, ":"); colonIdx != -1 {
		path = pathWithRef[:colonIdx]
		reference = pathWithRef[colonIdx+1:]
	} else {
		path = pathWithRef
		reference = RefLatest // Default to latest if no reference specified
	}

	// Parse path components
	pathParts := strings.Split(path, "/")
	if len(pathParts) < 3 {
		return nil, fmt.Errorf("invalid OCI URL: path must have at least 3 components (type/space/name)")
	}

	resourceType := pathParts[0]
	if resourceType != "unit" && resourceType != "target" {
		return nil, fmt.Errorf("invalid OCI URL: resource type must be 'unit' or 'target', got '%s'", resourceType)
	}

	return &ParsedOCIURL{
		Host:         host,
		ResourceType: resourceType,
		SpaceSlug:    pathParts[1],
		ResourceSlug: pathParts[2],
		Reference:    reference,
	}, nil
}

// ParsedOCIURL represents a parsed OCI URL.
type ParsedOCIURL struct {
	Host         string // e.g., "oci.confighub.com"
	ResourceType string // "unit" or "target"
	SpaceSlug    string // e.g., "production"
	ResourceSlug string // unit slug or target slug
	Reference    string // e.g., "head", "live", "v5"
}

// IsUnit returns true if this URL points to a unit resource.
func (p *ParsedOCIURL) IsUnit() bool {
	return p.ResourceType == "unit"
}

// IsTarget returns true if this URL points to a target resource.
func (p *ParsedOCIURL) IsTarget() bool {
	return p.ResourceType == "target"
}

// String reconstructs the OCI URL string.
func (p *ParsedOCIURL) String() string {
	return fmt.Sprintf("oci://%s/%s/%s/%s:%s", p.Host, p.ResourceType, p.SpaceSlug, p.ResourceSlug, p.Reference)
}
