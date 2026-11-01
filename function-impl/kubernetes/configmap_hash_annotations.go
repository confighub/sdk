// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package kubernetes

import (
	"fmt"
	"log/slog"
	"slices"
	"sort"
	"strings"

	"github.com/confighub/sdk/configkit/k8skit"
	"github.com/confighub/sdk/core/configkit/yamlkit"
	"github.com/confighub/sdk/core/function/api"
	"github.com/confighub/sdk/core/function/handler"
	"github.com/confighub/sdk/core/third_party/gaby"
)

// legacyConfigMapHashAnnotation is the single hash annotation a pod template carried before it
// could carry one per ConfigMap. Needs/Provides still fills it, so a pod template that has it
// is hashed its author's way and is left alone.
const legacyConfigMapHashAnnotation = "confighub.com/Hash"

func registerConfigMapHashAnnotations(fh handler.FunctionRegistry, rp *k8skit.K8sResourceProviderType) {
	if err := fh.RegisterFunction("set-configmap-hash-annotations", &handler.FunctionRegistration{
		FunctionSignature: api.FunctionSignature{
			FunctionName: "set-configmap-hash-annotations",
			Parameters: []api.FunctionParameter{
				{
					ParameterName: "configmap-name",
					Required:      false,
					Description:   "Name of a ConfigMap to annotate for; by default, every ConfigMap referenced other than by a placeholder",
					DataType:      api.DataTypeString,
					Example:       "app-config",
				},
			},
			VarArgs:               true,
			Mutating:              true,
			Validating:            false,
			Hermetic:              true,
			Idempotent:            true,
			Description:           "Adds to each pod template a " + k8skit.ConfigMapHashAnnotationPrefix + "<configmap> annotation for each ConfigMap it references, set to the named placeholder for the ConfigMap's name, which Needs/Provides fills with that ConfigMap's confighub.com/Hash (computed by set-hash), so a change to a ConfigMap rolls the pods using it. Existing annotations are kept, and a pod template with " + legacyConfigMapHashAnnotation + " is left alone.",
			FunctionType:          api.FunctionTypeCustom,
			AffectedResourceTypes: k8skit.PodTemplateResourceTypes(),
		},
		Function: func(fArgs handler.FunctionImplementationArguments) (gaby.Container, any, error) {
			return k8sFnSetConfigMapHashAnnotations(rp, fArgs.Options, fArgs.ParsedData, fArgs.Arguments)
		},
	}); err != nil {
		slog.Error("failed to register function", "error", err)
	}
}

func k8sFnSetConfigMapHashAnnotations(rp *k8skit.K8sResourceProviderType, options *api.FunctionOptions, parsedData gaby.Container, args []api.FunctionArgument) (gaby.Container, any, error) {
	var only map[string]bool
	for _, arg := range args {
		name, ok := arg.Value.(string)
		if !ok || name == "" {
			return parsedData, nil, fmt.Errorf("configmap-name must be a non-empty string, got %v", arg.Value)
		}
		if only == nil {
			only = map[string]bool{}
		}
		only[name] = true
	}

	referencePaths := configMapReferencePaths(rp)
	_, err := yamlkit.VisitResourcesFiltered(parsedData, nil, rp, options, func(doc *gaby.YamlDoc, output any, _ int, resourceInfo *api.ResourceInfo) (any, []error) {
		namesByTemplate, err := configMapsByPodTemplate(doc, resourceInfo.ResourceType, referencePaths[resourceInfo.ResourceType])
		if err != nil {
			return output, []error{err}
		}
		for template, names := range namesByTemplate {
			annotationsPath := template + ".metadata.annotations"
			if doc.Path(annotationsPath+"."+yamlkit.EscapeDotsInPathSegment(legacyConfigMapHashAnnotation)) != nil {
				continue
			}
			for _, name := range names {
				if yamlkit.IsStringPlaceHolderValue(name) || (only != nil && !only[name]) {
					continue
				}
				key := annotationsPath + "." + yamlkit.EscapeDotsInPathSegment(k8skit.ConfigMapHashAnnotation(name))
				if doc.Path(key) != nil {
					continue
				}
				if _, err := doc.SetP(yamlkit.NamedPlaceholderFor(name), key); err != nil {
					return output, []error{fmt.Errorf("set %s: %w", key, err)}
				}
			}
		}
		return output, nil
	})
	return parsedData, nil, err
}

// configMapReferencePaths returns, by resource type, the registered paths naming a ConfigMap
// that are inside a pod template.
func configMapReferencePaths(rp *k8skit.K8sResourceProviderType) map[api.ResourceType][]api.UnresolvedPath {
	paths := map[api.ResourceType][]api.UnresolvedPath{}
	for rt, pathToInfo := range yamlkit.GetRegisteredNeededPathsByProperty(rp, []string{api.PropertyKeyResourceType}) {
		for path, info := range pathToInfo {
			if !slices.Contains(api.PropertyValueAlternatives(info.Details.NeededRequired[api.PropertyKeyResourceType]), "v1/ConfigMap") {
				continue
			}
			if _, ok := k8skit.PodTemplateContaining(rt, string(path)); ok {
				paths[rt] = append(paths[rt], path)
			}
		}
	}
	return paths
}

// configMapsByPodTemplate returns the names of the ConfigMaps each pod template in doc
// references, sorted, keyed by the pod template's path.
func configMapsByPodTemplate(doc *gaby.YamlDoc, rt api.ResourceType, referencePaths []api.UnresolvedPath) (map[string][]string, error) {
	namesByTemplate := map[string][]string{}
	for _, path := range referencePaths {
		template, _ := k8skit.PodTemplateContaining(rt, string(path))
		resolved, err := yamlkit.ResolveAssociativePaths(doc, path, "", false, nil)
		if err != nil {
			return nil, fmt.Errorf("resolve %s: %w", path, err)
		}
		for _, r := range resolved {
			node := doc.Path(string(r.Path))
			if node == nil {
				continue
			}
			name, ok := node.Data().(string)
			name = strings.TrimSpace(name)
			if !ok || name == "" || slices.Contains(namesByTemplate[template], name) {
				continue
			}
			namesByTemplate[template] = append(namesByTemplate[template], name)
		}
	}
	for _, names := range namesByTemplate {
		sort.Strings(names)
	}
	return namesByTemplate, nil
}
