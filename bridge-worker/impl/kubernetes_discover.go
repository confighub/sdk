// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package impl

import (
	"context"
	"fmt"
	"strings"

	"github.com/confighub/sdk/bridge-worker/api"
	"github.com/confighub/sdk/configkit/k8skit"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

// ResourceTypeInfo holds information about a discovered resource type
// including its GVK and whether it's namespaced
type ResourceTypeInfo struct {
	GVK        schema.GroupVersionKind
	Namespaced bool
}

// listResourcesForGVK handles the common pattern of listing resources for a GVK with different options
func listResourcesForGVK(k8sclient KubernetesClient, gvk schema.GroupVersionKind, listOptions []client.ListOption, skipKubeSystem bool, logErrorsOnly bool) ([]*unstructured.Unstructured, error) {
	var resources []*unstructured.Unstructured

	// Create list object
	list := &unstructured.UnstructuredList{}
	list.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   gvk.Group,
		Version: gvk.Version,
		Kind:    gvk.Kind + "List",
	})

	if err := k8sclient.List(context.Background(), list, listOptions...); err != nil {
		if logErrorsOnly {
			log.Log.Error(err, "Failed to list resources",
				"group", gvk.Group,
				"version", gvk.Version,
				"kind", gvk.Kind)
			return resources, nil
		}
		return nil, err
	}

	for _, item := range list.Items {
		// Skip kube-system namespace resources if requested
		if skipKubeSystem && item.GetNamespace() == "kube-system" {
			continue
		}
		if item.GetObjectKind().GroupVersionKind() == gvk {
			resources = append(resources, &item)
		}
	}

	return resources, nil
}

// ConfigProvider interface allows for testing by controlling config retrieval
type ConfigProvider interface {
	GetConfig() (*rest.Config, error)
}

// DefaultConfigProvider uses InClusterConfig for production
type DefaultConfigProvider struct{}

func (d DefaultConfigProvider) GetConfig() (*rest.Config, error) {
	return rest.InClusterConfig()
}

var configProvider ConfigProvider = DefaultConfigProvider{}

// SetConfigProvider allows tests to override the config provider
func SetConfigProvider(provider ConfigProvider) {
	configProvider = provider
}

// getResourceTypesWithFallback tries dynamic discovery first, then falls back to static types
func getResourceTypesWithFallback(includeClusterScoped, includeNamespaced bool) (map[string]ResourceTypeInfo, error) {
	// Try dynamic discovery using the KubernetesClient
	var resourceTypes map[string]ResourceTypeInfo

	// Try dynamic discovery using the ConfigProvider, else fallback to static types
	if c, err := configProvider.GetConfig(); err == nil {
		resourceTypes, err := DiscoverAllResourceTypes(c)
		if err != nil {
			return nil, err
		}
		return resourceTypes, nil
	}

	// Fallback to static resource types
	resourceTypes = make(map[string]ResourceTypeInfo)
	if includeClusterScoped {
		for resourceType := range k8skit.K8sClusterScopedResourceTypes {
			gvk, err := parseGroupVersionKind(string(resourceType))
			if err != nil {
				continue
			}
			resourceTypes[string(resourceType)] = ResourceTypeInfo{GVK: gvk, Namespaced: false}
		}
	}
	if includeNamespaced {
		for resourceType := range k8skit.K8sNamespacedResourceTypes {
			gvk, err := parseGroupVersionKind(string(resourceType))
			if err != nil {
				continue
			}
			resourceTypes[string(resourceType)] = ResourceTypeInfo{GVK: gvk, Namespaced: true}
		}
	}
	return resourceTypes, nil
}

// DiscoverAllResourceTypes fetches all available resource types (including CRDs) from the cluster
// Returns a map of resource type string (e.g. group/version/Kind) to ResourceTypeInfo
func DiscoverAllResourceTypes(cfg *rest.Config) (map[string]ResourceTypeInfo, error) {
	discoveryClient, err := discovery.NewDiscoveryClientForConfig(cfg)
	if err != nil {
		return nil, err
	}

	resourceMap := make(map[string]ResourceTypeInfo)

	// Get all APIResourceLists (all groups/versions)
	apiResourceLists, err := discoveryClient.ServerPreferredResources()
	if err != nil {
		return nil, err
	}

	for _, apiResourceList := range apiResourceLists {
		gv, err := schema.ParseGroupVersion(apiResourceList.GroupVersion)
		if err != nil {
			continue
		}
		for _, res := range apiResourceList.APIResources {
			// Ignore subresources (e.g. pods/status)
			if strings.Contains(res.Name, "/") {
				continue
			}
			gvk := schema.GroupVersionKind{
				Group:   gv.Group,
				Version: gv.Version,
				Kind:    res.Kind,
			}
			resourceType := getResourceType(gvk)
			resourceMap[resourceType] = ResourceTypeInfo{
				GVK:        gvk,
				Namespaced: res.Namespaced,
			}
		}
	}
	return resourceMap, nil
}

// GetAllClusterResources fetches all resources from the Kubernetes cluster
// If includeKubeSystem is false, resources from kube-system namespace will be filtered out
func GetAllClusterResources(k8sclient KubernetesClient, includeKubeSystem bool) ([]*unstructured.Unstructured, error) {
	var allResources []*unstructured.Unstructured

	resourceTypes, err := getResourceTypesWithFallback(true, true)
	if err != nil {
		return nil, err
	}

	for _, info := range resourceTypes {
		gvk := info.GVK
		los := &metav1.ListOptions{}
		los.ResourceVersionMatch = metav1.ResourceVersionMatchExact
		los.SetGroupVersionKind(gvk)
		lo := []client.ListOption{&client.ListOptions{Raw: los}}

		resources, err := listResourcesForGVK(k8sclient, gvk, lo, !includeKubeSystem, true)
		if err != nil {
			continue
		}
		allResources = append(allResources, resources...)
	}

	return allResources, nil
}

// GetNamespacedResources fetches resources from specified namespaces
func GetNamespacedResources(k8sclient KubernetesClient, namespaces []string) ([]*unstructured.Unstructured, error) {
	var allResources []*unstructured.Unstructured
	resourceTypes, err := getResourceTypesWithFallback(false, true)
	if err != nil {
		return nil, err
	}

	for _, info := range resourceTypes {
		if !info.Namespaced {
			continue
		}

		for _, namespace := range namespaces {
			gvk := info.GVK
			lo := []client.ListOption{client.InNamespace(namespace)}
			resources, err := listResourcesForGVK(k8sclient, gvk, lo, false, true)
			if err != nil {
				continue
			}
			allResources = append(allResources, resources...)
		}
	}

	return allResources, nil
}

// GetCustomResourceDefinitions fetches only Custom Resource Definitions from the cluster
func GetCustomResourceDefinitions(k8sclient KubernetesClient) ([]*unstructured.Unstructured, error) {
	gvk := schema.GroupVersionKind{
		Group:   "apiextensions.k8s.io",
		Version: "v1",
		Kind:    "CustomResourceDefinition",
	}

	los := &metav1.ListOptions{}
	los.ResourceVersionMatch = metav1.ResourceVersionMatchExact
	los.SetGroupVersionKind(gvk)
	lo := []client.ListOption{&client.ListOptions{Raw: los}}

	return listResourcesForGVK(k8sclient, gvk, lo, false, false)
}

// getResourceType formats the resource type using GVK
func getResourceType(gvk schema.GroupVersionKind) string {
	if gvk.Group == "" {
		return fmt.Sprintf("%s/%s", gvk.Version, gvk.Kind)
	}
	return fmt.Sprintf("%s.%s/%s", gvk.Group, gvk.Version, gvk.Kind)
}

// getResourceName formats the resource name using k8skit's ResourceNameGetter logic
func getResourceName(resource *unstructured.Unstructured) string {
	namespace := resource.GetNamespace()
	name := resource.GetName()
	if namespace != "" {
		return fmt.Sprintf("%s/%s", namespace, name)
	}
	return name
}

// resourcesToResourceInfoList converts a slice of Unstructured objects to ResourceInfoList
func resourcesToResourceInfoList(resources []*unstructured.Unstructured) []api.ResourceInfo {
	resourceInfoList := make([]api.ResourceInfo, 0, len(resources))
	for _, resource := range resources {
		gvk := resource.GetObjectKind().GroupVersionKind()
		resourceInfo := api.ResourceInfo{
			ResourceType: getResourceType(gvk),
			ResourceName: getResourceName(resource),
		}
		resourceInfoList = append(resourceInfoList, resourceInfo)
	}
	return resourceInfoList
}
