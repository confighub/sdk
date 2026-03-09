// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

// Package argocdrenderer provides functionality for rendering ArgoCD Application
// resources to Kubernetes manifests by calling the ArgoCD API.
package argocdrenderer

// Config holds the configuration for connecting to an ArgoCD server.
type Config struct {
	// ServerAddress is the ArgoCD server address (e.g. "argocd-server.argocd.svc.cluster.local").
	ServerAddress string

	// AuthToken is the Bearer token for ArgoCD API authentication.
	AuthToken string

	// Insecure skips TLS verification when true.
	Insecure bool
}

// ManifestResponse represents the response from the ArgoCD manifests API endpoint.
type ManifestResponse struct {
	Manifests  []string `json:"manifests"`
	Namespace  string   `json:"namespace"`
	Revision   string   `json:"revision"`
	SourceType string   `json:"sourceType"`
}

// RenderResult contains the rendered manifests from ArgoCD.
type RenderResult struct {
	// Manifests is the rendered YAML documents as a byte slice (multi-doc, --- separated).
	Manifests []byte

	// Revision is the source revision used.
	Revision string

	// SourceType is the ArgoCD source type (e.g. "Helm", "Kustomize", "Directory").
	SourceType string
}
