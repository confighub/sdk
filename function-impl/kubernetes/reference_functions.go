// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package kubernetes

import (
	"fmt"
	"sort"
	"strings"

	"github.com/confighub/sdk/configkit/k8skit"
	"github.com/confighub/sdk/core/configkit/yamlkit"
	"github.com/confighub/sdk/core/constants"
	"github.com/confighub/sdk/core/function/api"
	"github.com/confighub/sdk/core/third_party/gaby"
)

// This file registers every cross-resource name reference, needs and provides. A reference is
// a resource-name path declared with the type it names, so what to register is read from the
// resource-type specs rather than assembled here.
//
// For each declared path we register a Needs; for each type named by one we register
// metadata.name as a Provides, so a resource of that type can satisfy references to it. Both
// sides carry a ResourceType property naming the target, which is what the resolver matches on.

// referenceSpec describes a single referring field path and the resource type it points at.
// attributeName is the attribute the path is a path of: resource-name for a plain reference,
// and a second, narrower name for a path that is also something else -- a namespace field is a
// reference to a Namespace and the resource's own namespace, and get-namespace reads it under
// the second name.
type referenceSpec struct {
	referrer      api.ResourceType
	attributeName api.AttributeName
	path          api.UnresolvedPath
	target        api.ResourceType
}

// initReferenceFunctions registers the needs and provides for every reference the
// resource-type specs declare.
func initReferenceFunctions(rp *k8skit.K8sResourceProviderType) {
	var needs []referenceSpec
	// targets is the set of every referenced resource type. Each provides its own
	// metadata.name so that a resource of that type can satisfy a reference to it.
	targets := map[api.ResourceType]struct{}{}

	// Every declaring type and every target provides its own metadata.name. Registering
	// provides for a declaring type that is never itself a target (e.g. IngressRoute) is
	// harmless and lets it satisfy a reference should one be added later.
	for _, reference := range k8skit.DeclaredReferences() {
		// A wildcard type is not a resource type, so it provides no name of its own. A field
		// every resource has -- metadata.namespace -- is declared against it.
		if reference.ResourceType != api.ResourceTypeAny {
			targets[reference.ResourceType] = struct{}{}
		}
		targets[reference.Target] = struct{}{}
		needs = append(needs, referenceSpec{
			referrer:      reference.ResourceType,
			attributeName: reference.AttributeName,
			path:          api.UnresolvedPath(reference.Path),
			target:        reference.Target,
		})
	}

	// Register metadata.name as a Provides for every referenced resource type.
	//
	// Sorted, as are the needs below, because both are assembled by ranging over maps and a
	// path that several of them reach accumulates its setters and its required alternatives in
	// the order they arrive. Registration that differs run to run is a registry that differs
	// run to run, and it is served that way on /paths.
	sortedTargets := make([]api.ResourceType, 0, len(targets))
	for target := range targets {
		sortedTargets = append(sortedTargets, target)
	}
	sort.Slice(sortedTargets, func(i, j int) bool { return sortedTargets[i] < sortedTargets[j] })
	for _, target := range sortedTargets {
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
		yamlkit.RegisterPathsByAttributeName(rp, api.AttributeNameResourceName, target, pathInfos, &yamlkit.AttributeRegistrationDetails{
			Enricher: enricher,
			AttributeNeedsProvidesDetails: api.AttributeNeedsProvidesDetails{
				ProvidedProperties: map[string]string{"ResourceType": string(target)},
			},
		}, false, true)
	}

	// Register each referring field path as a Needs.
	sort.Slice(needs, func(i, j int) bool {
		if needs[i].referrer != needs[j].referrer {
			return needs[i].referrer < needs[j].referrer
		}
		if needs[i].attributeName != needs[j].attributeName {
			return needs[i].attributeName < needs[j].attributeName
		}
		if needs[i].path != needs[j].path {
			return needs[i].path < needs[j].path
		}
		return needs[i].target < needs[j].target
	})
	for _, spec := range needs {
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
		yamlkit.RegisterPathsByAttributeName(rp, spec.attributeName, spec.referrer, pathInfos, &yamlkit.AttributeRegistrationDetails{
			// Function to set the value. The parameters are expected to match the
			// corresponding get function's parameters plus its result.
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
		containersPaths := k8skit.ContainerArrayPaths(attr.ResourceType)
		if len(containersPaths) == 0 {
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
