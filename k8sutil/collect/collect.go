// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

// Package collect gathers facts about a Kubernetes cluster (its version, the
// resource types it serves, its storage and ingress classes) and stores them
// on a ConfigHub Target, under the reserved "Cluster." fact prefix.
//
// It is shared by "cub k8s collect" and the bots that run in a cluster, so
// that a Target's facts mean the same thing whoever collected them.
package collect

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/confighub/sdk/core/cubapi"
	goclientnew "github.com/confighub/sdk/core/openapi/goclient-new"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/rest"
)

// FactPrefix is the reserved fact-key prefix for facts collected from a
// Kubernetes cluster. Collection owns this namespace: it (re)writes these keys
// and removes stale ones, while user-defined facts (any key outside the
// prefix) are left untouched.
const FactPrefix = "Cluster."

// NameFact is the cluster's name. It is not discoverable from the cluster, so
// [Facts] never sets it; the caller adds it when it knows one, and [Patch]
// keeps the one a Target already has when the caller does not.
const NameFact = FactPrefix + "Name"

const defaultStorageClassAnnotation = "storageclass.kubernetes.io/is-default-class"

var (
	crdGVR          = schema.GroupVersionResource{Group: "apiextensions.k8s.io", Version: "v1", Resource: "customresourcedefinitions"}
	storageClassGVR = schema.GroupVersionResource{Group: "storage.k8s.io", Version: "v1", Resource: "storageclasses"}
	ingressClassGVR = schema.GroupVersionResource{Group: "networking.k8s.io", Version: "v1", Resource: "ingressclasses"}
)

// Facts gathers facts about the cluster reachable via restConfig, keyed under
// [FactPrefix]. List-valued facts are comma-separated and sorted.
//
// It needs discovery, and list on CustomResourceDefinitions, StorageClasses
// and IngressClasses.
func Facts(ctx context.Context, restConfig *rest.Config) (map[string]string, error) {
	discoveryClient, err := discovery.NewDiscoveryClientForConfig(restConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create discovery client: %w", err)
	}
	dynamicClient, err := dynamic.NewForConfig(restConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create dynamic client: %w", err)
	}
	return facts(ctx, discoveryClient, dynamicClient)
}

func facts(ctx context.Context, discoveryClient discovery.DiscoveryInterface, dynamicClient dynamic.Interface) (map[string]string, error) {
	facts := map[string]string{}

	ver, err := discoveryClient.ServerVersion()
	if err != nil {
		return nil, fmt.Errorf("failed to get server version: %w", err)
	}
	// Cleaned to e.g. "1.31.2" (suitable to pass to kubeconform).
	facts[FactPrefix+"KubernetesVersion"] = strings.TrimPrefix(ver.GitVersion, "v")

	// CRDs, split by scope.
	crds, err := dynamicClient.Resource(crdGVR).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to list CRDs: %w", err)
	}
	crdGVKs := map[string]bool{}
	var crdClusterScoped, crdNamespaceScoped []string
	for i := range crds.Items {
		gvks := servedCRDGVKs(&crds.Items[i])
		for _, g := range gvks {
			crdGVKs[g] = true
		}
		scope, _, _ := unstructured.NestedString(crds.Items[i].Object, "spec", "scope")
		if scope == "Cluster" {
			crdClusterScoped = append(crdClusterScoped, gvks...)
		} else {
			crdNamespaceScoped = append(crdNamespaceScoped, gvks...)
		}
	}
	facts[FactPrefix+"ClusterScopedCRDs"] = sortedCSV(crdClusterScoped)
	facts[FactPrefix+"NamespaceScopedCRDs"] = sortedCSV(crdNamespaceScoped)

	// Built-in (non-CRD) served resource types, split by scope, as group/version/Kind.
	builtinClusterScoped, builtinNamespaceScoped, err := builtinResourceTypes(discoveryClient, crdGVKs)
	if err != nil {
		return nil, err
	}
	facts[FactPrefix+"ClusterScopedResourceTypes"] = sortedCSV(builtinClusterScoped)
	facts[FactPrefix+"NamespaceScopedResourceTypes"] = sortedCSV(builtinNamespaceScoped)

	// Storage classes (plus the default, if any).
	storageClasses, err := dynamicClient.Resource(storageClassGVR).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to list storage classes: %w", err)
	}
	var scNames []string
	defaultStorageClass := ""
	for i := range storageClasses.Items {
		name := storageClasses.Items[i].GetName()
		scNames = append(scNames, name)
		if storageClasses.Items[i].GetAnnotations()[defaultStorageClassAnnotation] == "true" {
			defaultStorageClass = name
		}
	}
	facts[FactPrefix+"StorageClasses"] = sortedCSV(scNames)
	facts[FactPrefix+"DefaultStorageClass"] = defaultStorageClass

	// Ingress classes.
	ingressClasses, err := dynamicClient.Resource(ingressClassGVR).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to list ingress classes: %w", err)
	}
	var icNames []string
	for i := range ingressClasses.Items {
		icNames = append(icNames, ingressClasses.Items[i].GetName())
	}
	facts[FactPrefix+"IngressClasses"] = sortedCSV(icNames)

	return facts, nil
}

// Patch returns the Facts of a JSON merge patch that owns the [FactPrefix]
// namespace: it sets the collected facts and nulls out the facts of current, a
// Target's existing facts, that are under the prefix but no longer collected.
// User facts (keys outside the prefix) are not included, so the merge patch
// preserves them. [NameFact] is kept unless collected carries one, since it is
// not discoverable from the cluster.
func Patch(current, collected map[string]string) map[string]any {
	patch := map[string]any{}
	for k, v := range collected {
		patch[k] = v
	}
	for k := range current {
		if !strings.HasPrefix(k, FactPrefix) || k == NameFact {
			continue
		}
		if _, ok := collected[k]; !ok {
			patch[k] = nil
		}
	}
	return patch
}

// UpdateTarget stores collected on target, as [Patch] describes, and returns
// the updated Target. target is the Target as last read: its Facts decide
// which collected facts are stale. It needs Edit on the Target.
func UpdateTarget(ctx context.Context, api *goclientnew.ClientWithResponses, target *goclientnew.Target, collected map[string]string) (*goclientnew.Target, error) {
	patchJSON, err := json.Marshal(map[string]any{"Facts": Patch(target.Facts, collected)})
	if err != nil {
		return nil, err
	}
	res, err := api.PatchTargetWithBodyWithResponse(ctx, target.SpaceID, target.TargetID,
		&goclientnew.PatchTargetParams{}, "application/merge-patch+json", bytes.NewReader(patchJSON))
	if cubapi.IsAPIError(err, res) {
		return nil, cubapi.InterpretErrorGeneric(err, res)
	}
	return res.JSON200, nil
}

// servedCRDGVKs returns the served "group/version/Kind" identifiers for a CRD, so the
// facts capture which apiVersions the cluster actually serves — not merely that the CRD
// exists. A CRD can serve multiple versions (e.g. v1 and v1beta1); each served version
// produces an entry, and non-served (declared but disabled) versions are omitted.
func servedCRDGVKs(crd *unstructured.Unstructured) []string {
	group, _, _ := unstructured.NestedString(crd.Object, "spec", "group")
	kind, _, _ := unstructured.NestedString(crd.Object, "spec", "names", "kind")
	versions, _, _ := unstructured.NestedSlice(crd.Object, "spec", "versions")
	var gvks []string
	for _, v := range versions {
		vm, ok := v.(map[string]interface{})
		if !ok {
			continue
		}
		if served, _, _ := unstructured.NestedBool(vm, "served"); !served {
			continue
		}
		name, _, _ := unstructured.NestedString(vm, "name")
		if name == "" {
			continue
		}
		gvks = append(gvks, formatGVK(group, name, kind))
	}
	return gvks
}

// builtinResourceTypes returns the served, non-CRD resource types as group/version/Kind,
// split into cluster-scoped and namespace-scoped. It enumerates every served group-version
// (not just the preferred one) so the facts capture which built-in apiVersions the cluster
// actually serves. GVKs present in crdGVKs are skipped, since CRDs are reported separately.
func builtinResourceTypes(dc discovery.DiscoveryInterface, crdGVKs map[string]bool) (clusterScoped, namespaceScoped []string, err error) {
	_, lists, err := dc.ServerGroupsAndResources()
	if err != nil {
		// ServerGroupsAndResources may return partial results with an aggregated error
		// (e.g. an unavailable aggregated API server). Proceed if we got anything.
		if len(lists) == 0 {
			return nil, nil, fmt.Errorf("failed to discover resource types: %w", err)
		}
	}
	clusterSet := map[string]bool{}
	nsSet := map[string]bool{}
	for _, list := range lists {
		if list == nil {
			continue
		}
		gv, perr := schema.ParseGroupVersion(list.GroupVersion)
		if perr != nil {
			continue
		}
		for _, res := range list.APIResources {
			// Skip subresources (e.g. pods/status).
			if strings.Contains(res.Name, "/") {
				continue
			}
			gvk := formatGVK(gv.Group, gv.Version, res.Kind)
			if crdGVKs[gvk] {
				continue // CRD-backed; reported in the CRD facts.
			}
			if res.Namespaced {
				nsSet[gvk] = true
			} else {
				clusterSet[gvk] = true
			}
		}
	}
	return setKeys(clusterSet), setKeys(nsSet), nil
}

// formatGVK renders a group/version/Kind, using the Helm-style core form (version/Kind)
// when the group is empty (e.g. "v1/Pod" vs "apps/v1/Deployment").
func formatGVK(group, version, kind string) string {
	if group == "" {
		return version + "/" + kind
	}
	return group + "/" + version + "/" + kind
}

func setKeys(m map[string]bool) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}

func sortedCSV(items []string) string {
	sort.Strings(items)
	return strings.Join(items, ",")
}
