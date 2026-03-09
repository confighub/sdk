// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package fluxrenderer

import (
	"bytes"
	"fmt"
	"io"
	"sort"
	"strings"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/yaml"
)

// ParsedDocuments holds the parsed resources from a multi-document YAML input.
type ParsedDocuments struct {
	// All unstructured documents
	Documents []*unstructured.Unstructured

	// ConfigMaps keyed by namespace/name
	ConfigMaps map[string]*corev1.ConfigMap

	// Secrets keyed by namespace/name
	Secrets map[string]*corev1.Secret

	// HelmCharts keyed by namespace/name
	HelmCharts map[string]*unstructured.Unstructured
}

// parseYAMLDocuments splits a multi-document YAML into individual unstructured objects.
func parseYAMLDocuments(data []byte) ([]*unstructured.Unstructured, error) {
	var docs []*unstructured.Unstructured
	decoder := yaml.NewYAMLOrJSONDecoder(bytes.NewReader(data), 4096)

	for {
		var obj unstructured.Unstructured
		if err := decoder.Decode(&obj); err != nil {
			if err == io.EOF {
				break
			}
			return nil, fmt.Errorf("failed to decode YAML document: %w", err)
		}
		if obj.Object != nil && len(obj.Object) > 0 {
			docs = append(docs, obj.DeepCopy())
		}
	}
	return docs, nil
}

// ParseDocumentsWithResources parses YAML documents and extracts ConfigMaps, Secrets, and HelmCharts.
func ParseDocumentsWithResources(data []byte) (*ParsedDocuments, error) {
	docs, err := parseYAMLDocuments(data)
	if err != nil {
		return nil, err
	}

	result := &ParsedDocuments{
		Documents:  docs,
		ConfigMaps: make(map[string]*corev1.ConfigMap),
		Secrets:    make(map[string]*corev1.Secret),
		HelmCharts: make(map[string]*unstructured.Unstructured),
	}

	for _, doc := range docs {
		switch doc.GetKind() {
		case "ConfigMap":
			cm := &corev1.ConfigMap{}
			if err := runtime.DefaultUnstructuredConverter.FromUnstructured(doc.Object, cm); err != nil {
				return nil, fmt.Errorf("failed to convert ConfigMap %s/%s: %w",
					doc.GetNamespace(), doc.GetName(), err)
			}
			key := resourceKey(cm.Namespace, cm.Name)
			result.ConfigMaps[key] = cm

		case "Secret":
			secret := &corev1.Secret{}
			if err := runtime.DefaultUnstructuredConverter.FromUnstructured(doc.Object, secret); err != nil {
				return nil, fmt.Errorf("failed to convert Secret %s/%s: %w",
					doc.GetNamespace(), doc.GetName(), err)
			}
			key := resourceKey(secret.Namespace, secret.Name)
			result.Secrets[key] = secret

		case "HelmChart":
			// Only include Flux HelmCharts (source.toolkit.fluxcd.io)
			if strings.HasPrefix(doc.GetAPIVersion(), "source.toolkit.fluxcd.io/") {
				key := resourceKey(doc.GetNamespace(), doc.GetName())
				result.HelmCharts[key] = doc
			}
		}
	}

	return result, nil
}

// resourceKey creates a key for looking up resources by namespace/name.
func resourceKey(namespace, name string) string {
	if namespace == "" {
		return name
	}
	return namespace + "/" + name
}

// combineRenderedTemplates combines Helm rendered templates into a single YAML document list.
func combineRenderedTemplates(rendered map[string]string, chartName string) string {
	var buf strings.Builder
	first := true

	// Sort keys for deterministic output
	keys := make([]string, 0, len(rendered))
	for k := range rendered {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	for _, name := range keys {
		content := rendered[name]

		// Skip empty templates
		content = strings.TrimSpace(content)
		if content == "" {
			continue
		}

		// Skip NOTES.txt
		if strings.HasSuffix(name, "NOTES.txt") {
			continue
		}

		// Skip test files
		if strings.Contains(name, "/tests/") {
			continue
		}

		if !first {
			buf.WriteString("---\n")
		}
		first = false

		// Add source comment for traceability
		buf.WriteString(fmt.Sprintf("# Source: %s\n", name))
		buf.WriteString(content)
		if !strings.HasSuffix(content, "\n") {
			buf.WriteString("\n")
		}
	}

	return buf.String()
}

// findDocumentByKindAndAPIVersion finds a document matching the given kind and API version prefix.
func findDocumentByKindAndAPIVersion(docs []*unstructured.Unstructured, kind, apiVersionPrefix string) *unstructured.Unstructured {
	for _, doc := range docs {
		if doc.GetKind() == kind && strings.HasPrefix(doc.GetAPIVersion(), apiVersionPrefix) {
			return doc
		}
	}
	return nil
}

// getConfigMapData retrieves data from a ConfigMap by namespace/name.
func getConfigMapData(configMaps map[string]*corev1.ConfigMap, namespace, name, key string) (string, error) {
	cmKey := resourceKey(namespace, name)
	cm, ok := configMaps[cmKey]
	if !ok {
		// Try without namespace for cases where namespace is empty
		cm, ok = configMaps[name]
		if !ok {
			return "", fmt.Errorf("ConfigMap %s not found in input", cmKey)
		}
	}

	data, ok := cm.Data[key]
	if !ok {
		return "", fmt.Errorf("key %s not found in ConfigMap %s", key, cmKey)
	}
	return data, nil
}

// getSecretData retrieves data from a Secret by namespace/name.
func getSecretData(secrets map[string]*corev1.Secret, namespace, name, key string) (string, error) {
	secretKey := resourceKey(namespace, name)
	secret, ok := secrets[secretKey]
	if !ok {
		// Try without namespace for cases where namespace is empty
		secret, ok = secrets[name]
		if !ok {
			return "", fmt.Errorf("Secret %s not found in input", secretKey)
		}
	}

	data, ok := secret.Data[key]
	if !ok {
		return "", fmt.Errorf("key %s not found in Secret %s", key, secretKey)
	}
	return string(data), nil
}
