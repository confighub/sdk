// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package fluxrenderer

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/drone/envsubst/v2"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/kustomize/api/resource"
	"sigs.k8s.io/yaml"

	generator "github.com/fluxcd/pkg/kustomize"

	"github.com/confighub/sdk/configkit/k8skit"
)

const (
	// KustomizationKind is the kind for Flux Kustomization resources.
	KustomizationKind = "Kustomization"
	// KustomizationAPIVersionPrefix is the API version prefix for Flux Kustomization.
	KustomizationAPIVersionPrefix = "kustomize.toolkit.fluxcd.io/"

	// SubstituteAnnotation is the annotation to disable variable substitution.
	SubstituteAnnotation = "kustomize.toolkit.fluxcd.io/substitute"
)

// RenderKustomization renders a Flux Kustomization to Kubernetes manifests.
// The input can be a multi-document YAML containing:
// - Kustomization (required)
// - ConfigMaps/Secrets referenced by PostBuild.substituteFrom (optional)
func RenderKustomization(ctx context.Context, input RenderInput) (*RenderResult, error) {
	// Parse all documents from input
	parsed, err := ParseDocumentsWithResources(input.Documents)
	if err != nil {
		return nil, fmt.Errorf("failed to parse input documents: %w", err)
	}

	// Find the Flux Kustomization document (not kustomize.config.k8s.io/v1)
	ksDoc := findDocumentByKindAndAPIVersion(parsed.Documents, KustomizationKind, KustomizationAPIVersionPrefix)
	if ksDoc == nil {
		return nil, fmt.Errorf("no Flux Kustomization found in input")
	}

	return renderKustomizationFromParsed(ctx, ksDoc, parsed, input.Options)
}

// renderKustomizationFromParsed renders a Kustomization using pre-parsed documents.
// This is called by RenderFlux after it has parsed the documents and determined the resource type.
func renderKustomizationFromParsed(ctx context.Context, ksDoc *unstructured.Unstructured, parsed *ParsedDocuments, options RenderOptions) (*RenderResult, error) {
	// Extract Kustomization spec
	spec, ok := ksDoc.Object["spec"].(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("Kustomization has no spec")
	}

	// Fetch and extract artifact
	if options.ArtifactSource.URL == "" {
		return nil, fmt.Errorf("artifact source URL is required")
	}

	tmpDir, cleanup, err := FetchArtifact(ctx, options.ArtifactSource.URL, options.ArtifactSource.Digest)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch artifact: %w", err)
	}
	defer cleanup()

	// Resolve build path
	path := "."
	if p, ok := spec["path"].(string); ok && p != "" {
		path = p
	}
	dirPath := filepath.Join(tmpDir, path)

	// Generate kustomization.yaml if needed using Flux generator
	gen := generator.NewGenerator(tmpDir, *ksDoc)
	action, err := gen.WriteFile(dirPath)
	if err != nil {
		return nil, fmt.Errorf("failed to generate kustomization: %w", err)
	}
	defer generator.CleanDirectory(dirPath, action)

	// Build kustomization using SecureBuild
	resMap, err := generator.SecureBuild(tmpDir, dirPath, false /* no remote bases */)
	if err != nil {
		return nil, fmt.Errorf("kustomize build failed: %w", err)
	}

	// Apply variable substitution if PostBuild is configured
	postBuild, hasPostBuild := spec["postBuild"].(map[string]interface{})
	if hasPostBuild {
		// Load substitution variables from inline + ConfigMaps/Secrets
		vars, err := loadSubstitutionVars(postBuild, ksDoc.GetNamespace(), parsed.ConfigMaps, parsed.Secrets)
		if err != nil {
			return nil, fmt.Errorf("failed to load substitution vars: %w", err)
		}

		// Apply substitution to each resource
		for _, res := range resMap.Resources() {
			if err := substituteVarsInResource(res, vars); err != nil {
				return nil, fmt.Errorf("failed to substitute vars in %s/%s: %w",
					res.GetKind(), res.GetName(), err)
			}
		}
	}

	// Get target namespace from spec or options
	targetNamespace := options.Namespace
	if targetNamespace == "" {
		if ns, ok := spec["targetNamespace"].(string); ok && ns != "" {
			targetNamespace = ns
		}
	}

	// Apply target namespace if specified
	if targetNamespace != "" {
		for _, res := range resMap.Resources() {
			// Only set namespace on namespaced resources that don't have one
			if res.GetNamespace() == "" && !k8skit.IsClusterScoped(res.GetApiVersion(), res.GetKind()) {
				res.SetNamespace(targetNamespace)
			}
		}
	}

	// Convert to YAML
	manifests, err := resMap.AsYaml()
	if err != nil {
		return nil, fmt.Errorf("failed to convert to YAML: %w", err)
	}

	return &RenderResult{
		Manifests: manifests,
		Revision:  "", // Could be set from artifact metadata if available
	}, nil
}

// loadSubstitutionVars loads variables from inline spec and referenced ConfigMaps/Secrets.
func loadSubstitutionVars(postBuild map[string]interface{}, defaultNamespace string,
	configMaps map[string]*corev1.ConfigMap, secrets map[string]*corev1.Secret) (map[string]string, error) {

	vars := make(map[string]string)

	// Start with inline vars from postBuild.substitute
	if substitute, ok := postBuild["substitute"].(map[string]interface{}); ok {
		for k, v := range substitute {
			if strVal, ok := v.(string); ok {
				vars[k] = strVal
			}
		}
	}

	// Merge vars from ConfigMaps/Secrets referenced in substituteFrom
	substituteFrom, ok := postBuild["substituteFrom"].([]interface{})
	if !ok {
		return vars, nil
	}

	for _, refRaw := range substituteFrom {
		ref, ok := refRaw.(map[string]interface{})
		if !ok {
			continue
		}

		kind, _ := ref["kind"].(string)
		name, _ := ref["name"].(string)
		optional, _ := ref["optional"].(bool)

		ns := defaultNamespace
		key := resourceKey(ns, name)

		switch kind {
		case "ConfigMap":
			cm, ok := configMaps[key]
			if !ok {
				cm, ok = configMaps[name] // Try without namespace
				if !ok {
					if optional {
						continue
					}
					return nil, fmt.Errorf("ConfigMap %s not found in input", key)
				}
			}
			for k, v := range cm.Data {
				vars[k] = v
			}
		case "Secret":
			secret, ok := secrets[key]
			if !ok {
				secret, ok = secrets[name] // Try without namespace
				if !ok {
					if optional {
						continue
					}
					return nil, fmt.Errorf("Secret %s not found in input", key)
				}
			}
			for k, v := range secret.Data {
				vars[k] = string(v)
			}
		default:
			if !optional {
				return nil, fmt.Errorf("unsupported substituteFrom kind: %s", kind)
			}
		}
	}

	return vars, nil
}

// substituteVarsInResource applies variable substitution to a kustomize resource.
func substituteVarsInResource(res *resource.Resource, vars map[string]string) error {
	// Check if substitution is disabled via annotation
	annotations := res.GetAnnotations()
	if val, ok := annotations[SubstituteAnnotation]; ok && val == "disabled" {
		return nil
	}

	// Get resource as unstructured for manipulation
	u := &unstructured.Unstructured{}
	resJSON, err := res.MarshalJSON()
	if err != nil {
		return fmt.Errorf("failed to marshal resource: %w", err)
	}
	if err := u.UnmarshalJSON(resJSON); err != nil {
		return fmt.Errorf("failed to unmarshal resource: %w", err)
	}

	// Apply substitution recursively
	substituted, err := substituteVarsInValue(u.Object, vars)
	if err != nil {
		return err
	}

	// Update the resource with substituted values
	substitutedMap, ok := substituted.(map[string]interface{})
	if !ok {
		return fmt.Errorf("unexpected type after substitution")
	}

	// Convert back to YAML and reload into resource
	substitutedYAML, err := yaml.Marshal(substitutedMap)
	if err != nil {
		return fmt.Errorf("failed to marshal substituted resource: %w", err)
	}

	if err := res.UnmarshalJSON(substitutedYAML); err != nil {
		return fmt.Errorf("failed to unmarshal substituted resource: %w", err)
	}

	return nil
}

// substituteVarsInValue recursively substitutes variables in a value.
func substituteVarsInValue(value interface{}, vars map[string]string) (interface{}, error) {
	switch v := value.(type) {
	case string:
		// Apply envsubst substitution
		result, err := envsubst.Eval(v, func(key string) string {
			if val, ok := vars[key]; ok {
				return val
			}
			// Return empty string for undefined vars (like bash default behavior)
			return ""
		})
		if err != nil {
			return nil, fmt.Errorf("envsubst failed: %w", err)
		}
		return result, nil

	case map[string]interface{}:
		result := make(map[string]interface{})
		for k, val := range v {
			substituted, err := substituteVarsInValue(val, vars)
			if err != nil {
				return nil, err
			}
			result[k] = substituted
		}
		return result, nil

	case []interface{}:
		result := make([]interface{}, len(v))
		for i, val := range v {
			substituted, err := substituteVarsInValue(val, vars)
			if err != nil {
				return nil, err
			}
			result[i] = substituted
		}
		return result, nil

	default:
		// Non-string primitives (int, bool, etc.) are returned as-is
		return value, nil
	}
}

// getKustomizationPath extracts the path from a Kustomization spec.
func getKustomizationPath(spec map[string]interface{}) string {
	if path, ok := spec["path"].(string); ok && path != "" {
		// Clean the path but preserve leading ./
		path = strings.TrimPrefix(path, "/")
		if path == "" {
			return "."
		}
		return path
	}
	return "."
}
