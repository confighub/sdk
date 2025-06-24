// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package k8skit

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/confighub/sdk/function/api"
	"github.com/confighub/sdk/third_party/gaby"
)

// SplitResourcesResult contains the separated CRDs and regular resources
type SplitResourcesResult struct {
	CRDs      string
	Resources string
}

// UnitGroup represents a logical grouping of Kubernetes resources that should be deployed together
type UnitGroup struct {
	Name      string
	Slug      string
	Type      UnitGroupType
	Resources []ResourceDocument
	Links     []string // References to other unit groups this depends on
}

// UnitGroupType represents the type of unit group
type UnitGroupType string

const (
	UnitGroupTypeNamespace       UnitGroupType = "namespace"      // Namespace + policy resources
	UnitGroupTypeCRD             UnitGroupType = "crd"            // Individual Custom Resource Definition
	UnitGroupTypeClusterRBAC     UnitGroupType = "cluster-rbac"   // ClusterRole + ClusterRoleBinding resources
	UnitGroupTypeWorkload        UnitGroupType = "workload"       // Deployment + Service + Ingress + etc.
	UnitGroupTypeConfig          UnitGroupType = "config"         // ConfigMap or Secret (individual)
	UnitGroupTypeFluxHelmRelease UnitGroupType = "flux-helm"      // Flux HelmRelease
	UnitGroupTypeClusterScoped   UnitGroupType = "cluster-scoped" // Other cluster-scoped resources
	UnitGroupTypeStandalone      UnitGroupType = "standalone"     // Resources that don't fit other categories
)

// ResourceDocument represents a parsed Kubernetes resource with metadata
type ResourceDocument struct {
	ResourceType api.ResourceType
	ResourceName api.ResourceName
	Namespace    string
	Name         string
	Content      string
	Source       string
	Labels       map[string]string
	References   []ResourceReference
	OwnerRefs    []OwnerReference
}

// ResourceReference represents a reference from one resource to another
type ResourceReference struct {
	TargetType      api.ResourceType
	TargetName      string
	TargetNamespace string
	ReferenceType   ReferenceType
}

// ReferenceType indicates the type of reference
type ReferenceType string

const (
	ReferenceTypeSelector   ReferenceType = "selector"    // label selector
	ReferenceTypeConfigRef  ReferenceType = "config-ref"  // configMapRef, secretRef
	ReferenceTypeServiceRef ReferenceType = "service-ref" // service reference
	ReferenceTypeVolumeRef  ReferenceType = "volume-ref"  // volume mount reference
	ReferenceTypeSubjectRef ReferenceType = "subject-ref" // RBAC subject reference
	ReferenceTypeRoleRef    ReferenceType = "role-ref"    // RBAC role reference
)

// OwnerReference represents a Kubernetes ownerReference
type OwnerReference struct {
	APIVersion string
	Kind       string
	Name       string
}

// resourceDoc represents a parsed Kubernetes resource document
type resourceDoc struct {
	resourceType api.ResourceType
	resourceName api.ResourceName
	content      string
	source       string
}

// SplitResources separates rendered resources into CRDs and regular resources
func SplitResources(renderedResources map[string]string, sourceName string) (*SplitResourcesResult, error) {
	var (
		crdDocs     []resourceDoc
		regularDocs []resourceDoc
	)

	for fileName, content := range renderedResources {
		// Skip empty files, partials, or NOTES.txt
		if strings.TrimSpace(content) == "" || strings.HasPrefix(filepath.Base(fileName), "_") || filepath.Base(fileName) == "NOTES.txt" {
			continue
		}

		docs, err := gaby.ParseAll([]byte(content))
		if err != nil {
			return nil, fmt.Errorf("failed to parse YAML content from file '%s': %w\n%s", fileName, err, content)
		}

		sourceComment := fmt.Sprintf("# Source: %s/%s\n", sourceName, fileName)

		for _, doc := range docs {
			// Use k8skit to extract resource type (apiVersion/kind)
			resourceType, err := K8sResourceProvider.ResourceTypeGetter(doc)
			if err != nil {
				return nil, fmt.Errorf("failed to extract resource type from YAML in file '%s': %w", fileName, err)
			}
			// Use k8skit to extract resource name (namespace/name or /name for cluster-scoped)
			resourceName, err := K8sResourceProvider.ResourceNameGetter(doc)
			if err != nil {
				return nil, fmt.Errorf("failed to extract resource name from YAML in file '%s': %w", fileName, err)
			}
			// Prepare the document string. doc.String() ensures it's well-formed YAML.
			docYAML := strings.TrimSpace(doc.String()) + "\n"

			resDoc := resourceDoc{
				resourceType: resourceType,
				resourceName: resourceName,
				content:      docYAML,
				source:       sourceComment,
			}

			isCRD := resourceType == "apiextensions.k8s.io/v1/CustomResourceDefinition"
			if isCRD {
				crdDocs = append(crdDocs, resDoc)
			} else {
				regularDocs = append(regularDocs, resDoc)
			}
		}
	}

	// Sort CRDs only with the stable sort as there's not dependencies among them
	sortedCrds := stabilizeSortByPriority(crdDocs)
	// Sort regular resources with dependencies - the algorithm taken from kubernetes-sigs/cli-util
	sortedRegularDocs, err := sortResourcesWithDependencies(regularDocs)
	if err != nil {
		return nil, err
	}

	// Build the output strings
	var (
		crdOutputBuilder     strings.Builder
		regularOutputBuilder strings.Builder
	)

	for _, doc := range sortedCrds {
		crdOutputBuilder.WriteString("---\n")
		crdOutputBuilder.WriteString(doc.source)
		crdOutputBuilder.WriteString(doc.content)
	}

	for _, doc := range sortedRegularDocs {
		regularOutputBuilder.WriteString("---\n")
		regularOutputBuilder.WriteString(doc.source)
		regularOutputBuilder.WriteString(doc.content)
	}

	return &SplitResourcesResult{
		CRDs:      crdOutputBuilder.String(),
		Resources: regularOutputBuilder.String(),
	}, nil
}

// Helper functions to extract fields from resourceType and resourceName

// GetNameFromResourceName extracts name from "namespace/name" or "/name" format
func GetNameFromResourceName(resourceName api.ResourceName) string {
	nameStr := string(resourceName)
	if strings.HasPrefix(nameStr, "/") {
		// Cluster-scoped: "/name"
		return nameStr[1:]
	} else {
		// Namespaced: "namespace/name"
		parts := strings.SplitN(nameStr, "/", 2)
		if len(parts) == 2 {
			return parts[1]
		}
	}
	return ""
}

// GetNamespaceFromResourceName extracts namespace from "namespace/name" format (empty for cluster-scoped)
func GetNamespaceFromResourceName(resourceName api.ResourceName) string {
	nameStr := string(resourceName)
	if strings.HasPrefix(nameStr, "/") {
		// Cluster-scoped: "/name" - no namespace
		return ""
	} else {
		// Namespaced: "namespace/name"
		parts := strings.SplitN(nameStr, "/", 2)
		if len(parts) == 2 {
			return parts[0]
		}
	}
	return ""
}
