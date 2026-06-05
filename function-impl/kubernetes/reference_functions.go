// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package kubernetes

import (
	"fmt"
	"log/slog"
	"strings"

	"sigs.k8s.io/kustomize/kyaml/resid"
	"sigs.k8s.io/yaml"

	"github.com/confighub/sdk/configkit/k8skit"
	"github.com/confighub/sdk/core/configkit/yamlkit"
	"github.com/confighub/sdk/core/constants"
	"github.com/confighub/sdk/core/function/api"
	"github.com/confighub/sdk/core/third_party/gaby"
	kustomizeexcerpts "github.com/confighub/sdk/function-impl/third_party/kustomize"
)

// This file consolidates registration of all cross-resource name-reference paths
// (needs and provides) into a single place. References come from two sources:
//
//  1. kustomize's NameReferenceFieldSpecs — built-in Kubernetes types (Service,
//     ConfigMap, Secret, ServiceAccount, etc.).
//  2. k8skit.CRDReferenceFields — CRD-specific references (Traefik, Flux, cert-manager,
//     ACK, Istio, Gateway API, etc.).
//
// For each referring field path we register a Needs; for each referenced (target)
// resource type we register metadata.name as a Provides so that a resource of that
// type can satisfy references to it. Needs and provides for a given target type share
// the attribute name attributeNameForResourceType(target), which is how the
// needs/provides resolver matches them.

var noncoreDefaultGroup = map[string]string{
	"Deployment":              "apps",
	"ReplicaSet":              "apps",
	"StatefulSet":             "apps",
	"DaemonSet":               "apps",
	"Job":                     "batch",
	"CronJob":                 "batch",
	"HorizontalPodAutoscaler": "autoscaling",
	"Role":                    "rbac.authorization.k8s.io",
	"ClusterRole":             "rbac.authorization.k8s.io",
	"RoleBinding":             "rbac.authorization.k8s.io",
	"ClusterRoleBinding":      "rbac.authorization.k8s.io",
	"Ingress":                 "networking.k8s.io",
	"IngressClass":            "networking.k8s.io",
}

func gvkString(gvk resid.Gvk) api.ResourceType {
	g := gvk.Group
	v := gvk.Version
	k := gvk.Kind
	if g == "" {
		g = noncoreDefaultGroup[k]
	}
	if v == "" {
		if k == "HorizontalPodAutoscaler" {
			v = "v2"
		} else {
			v = "v1"
		}
	}
	if k == "" {
		// This shouldn't happen
		k = "NoKind"
	}
	if g == "" {
		return api.ResourceType(v + "/" + k)
	}
	return api.ResourceType(g + "/" + v + "/" + k)
}

// Create a resource-type-specific attribute name that matches AttributeName character restrictions.
func attributeNameForResourceType(resourceType api.ResourceType) api.AttributeName {
	return api.AttributeName(string(api.AttributeNameResourceName) + "-" + strings.ReplaceAll(string(resourceType), "/", "-"))
}

var segmentIsArray = map[string]struct{}{
	"containers":       {},
	"initContainers":   {},
	"volumes":          {},
	"env":              {},
	"envFrom":          {},
	"sources":          {},
	"imagePullSecrets": {},
	"parameters":       {},
	"paths":            {},
	"webhooks":         {},
	"subjects":         {},
	"apiGroups":        {},
	"nonResourceURLs":  {},
	"resources":        {},
	"resourceNames":    {},
	"verbs":            {},
	"rules":            {}, // in both Roles/ClusterRoles and Ingress
}

// referenceSpec describes a single referring field path and the resource type it points at.
type referenceSpec struct {
	referrer api.ResourceType
	path     api.UnresolvedPath
	target   api.ResourceType
}

// initReferenceFunctions registers all cross-resource name-reference needs and provides
// from kustomize's NameReferenceFieldSpecs and k8skit.CRDReferenceFields.
func initReferenceFunctions(rp *k8skit.K8sResourceProviderType) {
	var needs []referenceSpec
	// targets is the set of every referenced resource type. Each provides its own
	// metadata.name so that a resource of that type can satisfy a reference to it.
	targets := map[api.ResourceType]struct{}{}

	// Source 1: kustomize NameReferenceFieldSpecs. These are stored as inverted
	// backreferences: each entry names a target Gvk and the referring fields that
	// point at it.
	namespaceNbrs := kustomizeexcerpts.NbrSlice{}
	if err := yaml.Unmarshal([]byte(kustomizeexcerpts.NameReferenceFieldSpecs), &namespaceNbrs); err != nil {
		slog.Error("couldn't unmarshal NameReferenceFieldSpecs", "error", err)
	} else {
		for _, nbr := range namespaceNbrs {
			target := gvkString(nbr.Gvk)
			targets[target] = struct{}{}
			for _, field := range nbr.Referrers {
				// This is kind of hacky in lieu of actual schemas. Kustomize always
				// searches arrays, so turn known array segments into wildcards.
				pathSegments := strings.Split(field.Path, "/")
				for i, pathSegment := range pathSegments {
					if _, ok := segmentIsArray[pathSegment]; ok {
						pathSegments[i] = pathSegment + ".*"
					}
				}
				// NOTE: We'd need to insert a path segment above in order to use
				// yamlkit.JoinPathSegments. Kubernetes resources don't have fields with
				// dots in their paths, fortunately.
				needs = append(needs, referenceSpec{
					referrer: gvkString(field.Gvk),
					path:     api.UnresolvedPath(strings.Join(pathSegments, ".")),
					target:   target,
				})
			}
		}
	}

	// Source 2: CRD reference fields. Every referring type (map key) and every target
	// provides its own metadata.name. Registering provides for a referring type that is
	// never itself a target (e.g. IngressRoute) is harmless and lets it satisfy a
	// reference should one be added later.
	for referrer, refs := range k8skit.CRDReferenceFields {
		targets[referrer] = struct{}{}
		for _, ref := range refs {
			targets[ref.Target] = struct{}{}
			needs = append(needs, referenceSpec{
				referrer: referrer,
				path:     api.UnresolvedPath(ref.Path),
				target:   ref.Target,
			})
		}
	}

	// Register metadata.name as a Provides for every referenced resource type.
	for target := range targets {
		attributeName := attributeNameForResourceType(target)
		// Attach the ConfigMap enricher for ConfigMap resource types.
		var enricher yamlkit.AttributeEnricher
		if target == "v1/ConfigMap" {
			enricher = configMapEnricher
		}
		pathInfos := api.PathToVisitorInfoType{
			api.UnresolvedPath("metadata.name"): {
				Path:          api.UnresolvedPath("metadata.name"),
				AttributeName: api.AttributeNameResourceName,
				DataType:      api.DataTypeString,
			},
		}
		yamlkit.RegisterPathsByAttributeName(rp, attributeName, target, pathInfos, &yamlkit.AttributeRegistrationDetails{
			// Function to get the value.
			GetterInvocation: &api.FunctionInvocation{
				FunctionName: "get-resources-of-type",
				Arguments:    []api.FunctionArgument{{ParameterName: "resource-type", Value: string(target)}},
			},
			Enricher: enricher,
			AttributeNeedsProvidesDetails: api.AttributeNeedsProvidesDetails{
				ProvidedProperties: map[string]string{"ResourceType": string(target)},
			},
		}, false, true)
	}

	// Register each referring field path as a Needs.
	for _, spec := range needs {
		attributeName := attributeNameForResourceType(spec.target)
		// Attach the ConfigMap enricher for needed paths that reference ConfigMaps.
		var enricher yamlkit.AttributeEnricher
		if spec.target == "v1/ConfigMap" {
			enricher = configMapEnricher
		}
		pathInfos := api.PathToVisitorInfoType{
			spec.path: {
				Path:          spec.path,
				AttributeName: api.AttributeNameResourceName,
				DataType:      api.DataTypeString,
			},
		}
		yamlkit.RegisterPathsByAttributeName(rp, attributeName, spec.referrer, pathInfos, &yamlkit.AttributeRegistrationDetails{
			// Function to set the value. The parameters are expected to match the
			// corresponding get function's parameters plus its result.
			SetterInvocation: &api.FunctionInvocation{
				FunctionName: "set-references-of-type",
				Arguments:    []api.FunctionArgument{{ParameterName: "resource-type", Value: string(spec.target)}},
			},
			Enricher: enricher,
			AttributeNeedsProvidesDetails: api.AttributeNeedsProvidesDetails{
				NeededRequired: map[string]string{"ResourceType": string(spec.target)},
			},
		}, true, false)
	}
}

// configMapEnricher populates ProvidedProperties and NeededRequired/NeededPreferred
// for ConfigMap needs/provides matching. For provided ConfigMaps (metadata.name), it extracts
// ResourceNameStableCore, ConfigMapFormat, Namespace, and data keys. For needed ConfigMap
// references, it extracts Namespace and envFrom format requirements.
func configMapEnricher(doc *gaby.YamlDoc, attr *api.AttributeValue, isProvided bool) error {
	// Only enrich ConfigMap resources (provided) or references to ConfigMaps (needed).
	// GetPathVisitorInfo merges across all attribute names, so this enricher may be
	// invoked for non-ConfigMap resources (e.g., Namespace metadata.name).
	if isProvided && attr.ResourceType != "v1/ConfigMap" {
		return nil
	}
	if attr.Details == nil {
		attr.Details = &api.AttributeDetails{}
	}
	if isProvided {
		if attr.Details.ProvidedProperties == nil {
			attr.Details.ProvidedProperties = make(map[string]string)
		}
		// Provided Properties should be in the form expected by Needed Requirements and Preferences.

		// Extract ResourceNameStableCore to match with Name keys
		stableCorePath := k8skit.K8sContextPath(constants.ResourceNameStableCoreKeySuffix)
		if v, found, _ := yamlkit.YamlSafePathGetValue[string](doc, api.ResolvedPath(stableCorePath), true); found && v != "" {
			attr.Details.ProvidedProperties["Name"] = v
		}
		// Extract ConfigMapFormat
		formatPath := k8skit.K8sContextPath(constants.ConfigMapFormatKeySuffix)
		if v, found, _ := yamlkit.YamlSafePathGetValue[string](doc, api.ResolvedPath(formatPath), true); found && v != "" {
			attr.Details.ProvidedProperties["ConfigMapFormat"] = v
		}
		// Extract Namespace
		if v, found, _ := yamlkit.YamlSafePathGetValue[string](doc, "metadata.namespace", true); found && v != "" && v != yamlkit.PlaceHolderBlockApplyString {
			attr.Details.ProvidedProperties["Namespace"] = v
		}
		// Extract data keys for subPath matching
		dataDoc, found, _ := yamlkit.YamlSafePathGetDoc(doc, "data", true)
		if found && dataDoc != nil {
			if dataMap, ok := dataDoc.Data().(map[string]any); ok {
				for key := range dataMap {
					// Add the keys for subPath matching
					attr.Details.ProvidedProperties["SubPath-"+key] = "true"
				}
			}
		}
	} else {
		// Needed ConfigMap references
		if attr.Details.NeededRequired == nil {
			attr.Details.NeededRequired = make(map[string]string)
		}
		// Extract Namespace (only require matching when non-placeholder)
		if v, found, _ := yamlkit.YamlSafePathGetValue[string](doc, "metadata.namespace", true); found && v != "" && v != yamlkit.PlaceHolderBlockApplyString {
			attr.Details.NeededRequired["Namespace"] = v
		}
		// Detect envFrom references — these require key/value format
		if strings.Contains(string(attr.Path), "envFrom") || strings.Contains(string(attr.Path), "configMapRef") {
			attr.Details.NeededRequired["ConfigMapFormat"] = constants.ConfigMapFormatKeyValue
			return nil
		}
		// For volume ConfigMap references, look up volumeMount subPaths that reference
		// this volume name.
		if doc == nil || !strings.Contains(string(attr.Path), "volumes") {
			return nil
		}
		attr.Details.NeededRequired["ConfigMapFormat"] = constants.ConfigMapFormatFile
		volumeNamePath := api.ResolvedPath(strings.TrimRight(string(attr.Path), "configMap.name") + ".name")
		volumeNameDoc, found, _ := yamlkit.YamlSafePathGetDoc(doc, volumeNamePath, true)
		if !found {
			return nil
		}
		volumeNameData := volumeNameDoc.Data()
		if volumeNameData == nil {
			return nil
		}
		volumeName, ok := volumeNameData.(string)
		if !ok {
			return nil
		}
		if attr.Details.NeededPreferred == nil {
			attr.Details.NeededPreferred = make(map[string]string)
		}
		attr.Details.NeededPreferred["Name"] = volumeName
		containersPaths, ok := k8skit.ResourceTypeToContainersPaths[attr.ResourceType]
		if !ok {
			return nil
		}
		for _, containersPath := range containersPaths {
			// Iterate over all containers in this path
			containersDoc, found, _ := yamlkit.YamlSafePathGetDoc(doc, api.ResolvedPath(containersPath), true)
			if !found || containersDoc == nil {
				continue
			}
			containers := containersDoc.Children()
			for ci := range containers {
				// Check each volumeMount in this container
				vmPath := fmt.Sprintf("%s.%d.volumeMounts", containersPath, ci)
				vmDoc, found, _ := yamlkit.YamlSafePathGetDoc(doc, api.ResolvedPath(vmPath), true)
				if !found || vmDoc == nil {
					continue
				}
				mounts := vmDoc.Children()
				for _, mount := range mounts {
					mountName, found, _ := yamlkit.YamlSafePathGetValue[string](mount, "name", true)
					if !found || mountName != volumeName {
						continue
					}
					subPath, found, _ := yamlkit.YamlSafePathGetValue[string](mount, "subPath", true)
					if found && subPath != "" {
						attr.Details.NeededRequired["SubPath-"+subPath] = "true"
					}
				}
			}
		}
	}
	return nil
}
