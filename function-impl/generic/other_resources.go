// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package generic

import (
	"encoding/json"
	"fmt"
	"maps"
	"slices"

	"github.com/cockroachdb/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"sigs.k8s.io/yaml"

	"github.com/confighub/sdk/configkit/k8skit"
	"github.com/confighub/sdk/core/function/api"
	"github.com/confighub/sdk/core/function/handler"
	"github.com/confighub/sdk/core/third_party/gaby"
)

// OtherResources is the OtherData a general-purpose function such as vet-cel or vet-starlark was
// passed, as an expression reads it: by key, the list of resources passed under it, and, at the
// same positions, where each came from. A key a Trigger's OtherUnitSelections pass holds the
// resources of the selected Units, possibly none; the origin of each names its Unit and Space
// (api.OtherDataOrigin, by its JSON field names). An entry read from one Unit or revision has an
// empty origin for each resource.
type OtherResources struct {
	Resources map[string][]any
	Origins   map[string][]any
}

// NewOtherResources reads a function's parsed OtherData and the origins of its documents.
func NewOtherResources(parsed map[api.OtherDataSource]gaby.Container, origins map[api.OtherDataSource][]api.OtherDataOrigin) (OtherResources, error) {
	other := OtherResources{Resources: map[string][]any{}, Origins: map[string][]any{}}
	for _, key := range slices.Sorted(maps.Keys(parsed)) {
		docs := parsed[key]
		resources := []any{}
		resourceOrigins := []any{}
		for i, doc := range docs {
			if doc == nil || doc.Data() == nil {
				continue
			}
			var resource map[string]any
			if err := yaml.Unmarshal(doc.Bytes(), &resource); err != nil {
				return OtherResources{}, errors.Wrapf(err, "failed to read OtherData %s", key)
			}
			resources = append(resources, resource)
			origin := map[string]any{}
			if documentOrigins := origins[key]; i < len(documentOrigins) {
				encoded, err := json.Marshal(documentOrigins[i])
				if err != nil {
					return OtherResources{}, err
				}
				if err := json.Unmarshal(encoded, &origin); err != nil {
					return OtherResources{}, err
				}
			}
			resourceOrigins = append(resourceOrigins, origin)
		}
		other.Resources[string(key)] = resources
		other.Origins[string(key)] = resourceOrigins
	}
	return other, nil
}

// OtherResourcesOf reads the OtherData a function was passed.
func OtherResourcesOf(fArgs handler.FunctionImplementationArguments) (OtherResources, error) {
	return NewOtherResources(fArgs.ParsedOtherData, fArgs.OtherDataOrigins)
}

// LabelSelectorMatches reports whether a Kubernetes label selector selects a set of labels, both
// as decoded from YAML or JSON: matchLabels and matchExpressions ANDed, read as the API server
// reads a LabelSelector (metav1.LabelSelectorAsSelector). An empty selector selects everything,
// and a null one nothing, as for a NetworkPolicy's podSelector and a PodDisruptionBudget's
// selector respectively. Absent labels are no labels.
func LabelSelectorMatches(selector any, labelValues any) (bool, error) {
	if selector == nil {
		return false, nil
	}
	encoded, err := json.Marshal(selector)
	if err != nil {
		return false, errors.Wrap(err, "the selector is not a label selector")
	}
	var labelSelector metav1.LabelSelector
	if err := json.Unmarshal(encoded, &labelSelector); err != nil {
		return false, errors.Wrap(err, "the selector is not a label selector")
	}
	parsed, err := metav1.LabelSelectorAsSelector(&labelSelector)
	if err != nil {
		return false, errors.Wrap(err, "invalid label selector")
	}
	set := labels.Set{}
	if labelValues != nil {
		labelMap, ok := labelValues.(map[string]any)
		if !ok {
			return false, errors.Newf("labels must be a map, not %T", labelValues)
		}
		for key, value := range labelMap {
			set[key] = fmt.Sprint(value)
		}
	}
	return parsed.Matches(set), nil
}

// PodLabels returns the labels of the pods a Kubernetes resource, as decoded from YAML or JSON,
// runs: a workload's pod template's, wherever its kind keeps it (k8skit.PodLabelsPath), such as a
// CronJob's spec.jobTemplate.spec.template, or a bare Pod's own. A resource that runs
// pods but labels none has no labels, an empty map; one that runs no pods, such as a Service,
// returns nil.
func PodLabels(resource any) any {
	document, ok := resource.(map[string]any)
	if !ok {
		return nil
	}
	apiVersion, _ := document["apiVersion"].(string)
	kind, _ := document["kind"].(string)
	path, ok := k8skit.PodLabelsPath(api.ResourceType(apiVersion + "/" + kind))
	if !ok {
		return nil
	}
	var value any = document
	for _, segment := range path {
		object, ok := value.(map[string]any)
		if !ok {
			return map[string]any{}
		}
		value = object[segment]
	}
	labels, ok := value.(map[string]any)
	if !ok {
		return map[string]any{}
	}
	return labels
}

// namespaceNameLabel is the label the API server gives every Namespace, its name, which a
// namespaceSelector can select it by.
const namespaceNameLabel = "kubernetes.io/metadata.name"

// NamespaceLabels returns the labels of the Namespace named name among namespaces, a list of
// resources as decoded from YAML or JSON, as a namespaceSelector is matched against them: its own,
// with kubernetes.io/metadata.name, which the API server sets on every Namespace. A Namespace not
// among them has that label alone.
func NamespaceLabels(namespaces any, name string) map[string]any {
	labels := map[string]any{}
	list, _ := namespaces.([]any)
	for _, item := range list {
		namespace, ok := item.(map[string]any)
		if !ok || namespace["kind"] != "Namespace" {
			continue
		}
		metadata, _ := namespace["metadata"].(map[string]any)
		if metadata == nil || metadata["name"] != name {
			continue
		}
		if own, ok := metadata["labels"].(map[string]any); ok {
			maps.Copy(labels, own)
		}
		break
	}
	labels[namespaceNameLabel] = name
	return labels
}
