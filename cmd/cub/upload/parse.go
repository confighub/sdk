// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

// Package upload turns a stream of already-rendered Kubernetes manifests (from
// the installer, `kustomize build`, or `helm template`) into a ConfigHub upload
// plan: parsing resources, ensuring a Namespace exists, ordering by priority and
// references (breaking dependency cycles), grouping into Units (one per resource
// or the minimal set with CRDs and AppConfig files split out), and inferring the
// links between them. It performs no rendering of its own.
package upload

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/confighub/sdk/configkit/k8skit"
	fapi "github.com/confighub/sdk/core/function/api"
	"github.com/confighub/sdk/core/third_party/gaby"
)

const (
	crdResourceType    = fapi.ResourceType("apiextensions.k8s.io/v1/CustomResourceDefinition")
	secretResourceType = fapi.ResourceType("v1/Secret")
	namespaceType      = fapi.ResourceType("v1/Namespace")
	configMapType      = fapi.ResourceType("v1/ConfigMap")
)

// Resource is one parsed Kubernetes resource document.
type Resource struct {
	Type       fapi.ResourceType // apiVersion/kind
	ScopedName fapi.ResourceName // "namespace/name" or "/name" for cluster-scoped
	Name       string            // metadata.name
	Namespace  string            // metadata.namespace ("" if cluster-scoped)
	Content    string            // single-document YAML (well-formed, trailing newline)
	Source     string            // "# Source: <origin>\n" comment, may be ""
	IsCRD      bool
	IsSecret   bool
	// AppConfig is non-nil when this document is an AppConfig carrier ConfigMap;
	// such resources are expanded into an AppConfig Unit set rather than uploaded
	// as plain Kubernetes/YAML.
	AppConfig *AppConfigManifest
	// Synthesized marks resources the pipeline created (e.g. an ensured
	// Namespace) rather than ones read from the input.
	Synthesized bool
}

// key returns the canonical type+scope+name identifier for this resource
// ("apiVersion/kind#namespace/name"), matching the keys the function executor
// produces for references and workload labels. A scoped name alone is not unique
// (a Deployment and a Service can share namespace/name), so the type is included.
func (r Resource) key() fapi.ResourceTypeAndName {
	return fapi.ResourceTypeAndFullNameFromResourceInfo(fapi.ResourceInfo{
		ResourceType: r.Type,
		ResourceName: r.ScopedName,
	})
}

// CRDGroupKind returns the "group/kind" a CustomResourceDefinition defines, used
// to match custom resources to the CRD that declares them. Returns ("", false)
// for non-CRDs or a CRD whose group/kind can't be read.
func (r Resource) CRDGroupKind() (string, bool) {
	if !r.IsCRD {
		return "", false
	}
	parsed, err := gaby.ParseAll([]byte(r.Content))
	if err != nil || len(parsed) == 0 {
		return "", false
	}
	group, ok := parsed[0].Path("spec.group").Data().(string)
	if !ok || group == "" {
		return "", false
	}
	kind, ok := parsed[0].Path("spec.names.kind").Data().(string)
	if !ok || kind == "" {
		return "", false
	}
	return group + "/" + kind, true
}

// Parse reads each input — a file, a directory (walked recursively for
// .yaml/.yml), or "-" for stdin — and returns the Kubernetes resources it
// contains in document order, with AppConfig carriers and Secrets flagged.
func Parse(inputs []string) ([]Resource, error) {
	rp := k8skit.NewK8sResourceProvider()
	var resources []Resource

	add := func(origin string, content []byte) error {
		docs, err := gaby.ParseAll(content)
		if err != nil {
			return fmt.Errorf("parse YAML from %s: %w", origin, err)
		}
		for _, doc := range docs {
			docYAML := strings.TrimSpace(doc.String())
			if docYAML == "" {
				continue
			}
			resourceType, err := rp.ResourceTypeGetter(doc)
			if err != nil {
				return fmt.Errorf("extract resource type from %s: %w", origin, err)
			}
			if resourceType == "/" || resourceType == "" {
				// Not a Kubernetes resource (no apiVersion/kind) — skip.
				continue
			}
			resourceName, err := rp.ResourceNameGetter(doc)
			if err != nil {
				return fmt.Errorf("extract resource name from %s: %w", origin, err)
			}
			r := Resource{
				Type:       resourceType,
				ScopedName: resourceName,
				Name:       k8skit.GetNameFromResourceName(resourceName),
				Namespace:  k8skit.GetNamespaceFromResourceName(resourceName),
				Content:    docYAML + "\n",
				Source:     fmt.Sprintf("# Source: %s\n", origin),
				IsCRD:      resourceType == crdResourceType,
				IsSecret:   resourceType == secretResourceType,
			}
			if resourceType == configMapType {
				appCfg, err := DetectAppConfigManifest([]byte(docYAML))
				if err != nil {
					return err
				}
				r.AppConfig = appCfg
			}
			resources = append(resources, r)
		}
		return nil
	}

	for _, in := range inputs {
		if in == "-" {
			data, err := io.ReadAll(os.Stdin)
			if err != nil {
				return nil, fmt.Errorf("read stdin: %w", err)
			}
			if err := add("stdin", data); err != nil {
				return nil, err
			}
			continue
		}
		info, err := os.Stat(in)
		if err != nil {
			return nil, err
		}
		if info.IsDir() {
			err := filepath.WalkDir(in, func(path string, d os.DirEntry, err error) error {
				if err != nil {
					return err
				}
				if d.IsDir() || !isYAMLFile(path) {
					return nil
				}
				data, err := os.ReadFile(path)
				if err != nil {
					return err
				}
				// Record the origin relative to the walked directory so the
				// Source comment reads "backend.yaml" rather than the absolute
				// path (which, for an oci:// input, is a temp directory).
				origin := path
				if rel, relErr := filepath.Rel(in, path); relErr == nil {
					origin = rel
				}
				return add(origin, data)
			})
			if err != nil {
				return nil, err
			}
			continue
		}
		data, err := os.ReadFile(in)
		if err != nil {
			return nil, err
		}
		if err := add(in, data); err != nil {
			return nil, err
		}
	}
	return resources, nil
}

func isYAMLFile(path string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	return ext == ".yaml" || ext == ".yml"
}
