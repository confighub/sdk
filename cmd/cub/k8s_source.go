// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"fmt"
	"strings"

	"github.com/confighub/sdk/configkit/k8skit"
	"github.com/confighub/sdk/core/third_party/gaby"
	"github.com/skratchdot/open-golang/open"
	"github.com/spf13/cobra"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/restmapper"
	"k8s.io/client-go/tools/clientcmd"
)

var k8sSourceCmd = &cobra.Command{
	Use:   "source <kind> <name>",
	Short: "Find the ConfigHub source of a Kubernetes resource",
	Long: getCommandHelp(`Retrieve a Kubernetes resource and trace it back to its ConfigHub unit.

This command gets a Kubernetes resource and uses ConfigHub annotations to determine
which unit manages the resource. It then opens a browser window to view the unit
in the ConfigHub UI.

The resource must have ConfigHub annotations (confighub.com/UnitSlug and confighub.com/SpaceID)
to be traced back to its source.

The kubeconfig file is loaded with the following precedence:

  1. --kubeconfig flag (highest priority, no merging)
  2. KUBECONFIG environment variable (merges multiple files)
  3. $HOME/.kube/config (default)

Examples:
`+"```"+`
  # Find the source of a deployment
  cub k8s source deployment my-app

  # Find the source of a deployment in a specific namespace
  cub k8s source deployment my-app --namespace my-namespace

  # Find the source of a cluster-scoped resource
  cub k8s source clusterrole my-role

  # Use a specific kubeconfig file
  cub k8s source deployment my-app --kubeconfig /path/to/config
`+"```"+`
`, ""),
	Args: cobra.ExactArgs(2),
	RunE: k8sSourceCmdRun,
}

var (
	k8sNamespace   string
	k8sApiVersion  string
	k8sKubeContext string
	k8sKubeconfig  string
)

func init() {
	k8sSourceCmd.Flags().StringVarP(&k8sNamespace, "namespace", "n", "default", "Namespace of the resource (ignored for cluster-scoped resources)")
	k8sSourceCmd.Flags().StringVar(&k8sApiVersion, "api-version", "", "API version of the resource (e.g., 'apps/v1', 'v1'). If not specified, common versions will be tried.")
	k8sSourceCmd.Flags().StringVar(&k8sKubeContext, "kube-context", "", "Kubernetes context to use")
	k8sSourceCmd.Flags().StringVar(&k8sKubeconfig, "kubeconfig", "", "Path to the kubeconfig file to use for CLI requests")
	k8sCmd.AddCommand(k8sSourceCmd)
}

func k8sSourceCmdRun(cmd *cobra.Command, args []string) error {
	kind := args[0]
	name := args[1]

	// Get Kubernetes client with proper kubeconfig loading precedence:
	// 1. --kubeconfig flag (highest priority, no merging)
	// 2. KUBECONFIG environment variable (merges multiple files)
	// 3. $HOME/.kube/config (default)
	loadingRules := clientcmd.NewDefaultClientConfigLoadingRules()

	// If --kubeconfig flag is set, use only that file (highest priority)
	if k8sKubeconfig != "" {
		loadingRules.ExplicitPath = k8sKubeconfig
	}
	// Otherwise, NewDefaultClientConfigLoadingRules() already handles:
	// - KUBECONFIG environment variable (with merging)
	// - Default $HOME/.kube/config path

	configOverrides := &clientcmd.ConfigOverrides{}
	if k8sKubeContext != "" {
		configOverrides.CurrentContext = k8sKubeContext
	}

	config := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(loadingRules, configOverrides)
	restConfig, err := config.ClientConfig()
	if err != nil {
		return fmt.Errorf("failed to get Kubernetes config: %w", err)
	}

	dynamicClient, err := dynamic.NewForConfig(restConfig)
	if err != nil {
		return fmt.Errorf("failed to create Kubernetes client: %w", err)
	}

	// Create discovery client for REST mapper
	discoveryClient, err := discovery.NewDiscoveryClientForConfig(restConfig)
	if err != nil {
		return fmt.Errorf("failed to create discovery client: %w", err)
	}

	// Create REST mapper to determine resource scope
	groupResources, err := restmapper.GetAPIGroupResources(discoveryClient)
	if err != nil {
		return fmt.Errorf("failed to get API group resources: %w", err)
	}
	mapper := restmapper.NewDiscoveryRESTMapper(groupResources)

	// Get the resource
	resource, err := getK8sResource(dynamicClient, mapper, kind, name, k8sNamespace, k8sApiVersion)
	if err != nil {
		return err
	}

	// Extract ConfigHub annotations
	annotations := resource.GetAnnotations()
	if annotations == nil {
		return fmt.Errorf("resource %s/%s has no annotations", kind, name)
	}

	// Get the annotation paths for UnitSlug and SpaceID
	unitSlugAnnotation := getAnnotationKey("UnitSlug")
	spaceIDAnnotation := getAnnotationKey("SpaceID")

	unitSlug, hasUnitSlug := annotations[unitSlugAnnotation]
	spaceID, hasSpaceID := annotations[spaceIDAnnotation]

	if !hasUnitSlug || !hasSpaceID {
		return fmt.Errorf("resource %s/%s is not managed by ConfigHub (missing required annotations: %s and %s)",
			kind, name, unitSlugAnnotation, spaceIDAnnotation)
	}

	tprint("Resource: %s/%s", kind, name)
	tprint("ConfigHub Unit: %s", unitSlug)
	tprint("ConfigHub Space: %s", spaceID)

	// Get the unit details
	unit, err := apiGetUnitFromSlugInSpace(unitSlug, spaceID, "UnitID")
	if err != nil {
		return fmt.Errorf("failed to get unit %s in space %s: %w", unitSlug, spaceID, err)
	}

	// Build the unit URL
	serverURL := strings.TrimSuffix(contextManager.ActiveContext().Coordinate.ServerURL, "/")
	unitURL := fmt.Sprintf("%s/units/%s/%s", serverURL, spaceID, unit.UnitID.String())

	tprint("Opening browser to: %s", unitURL)

	// Open the browser
	if err := open.Run(unitURL); err != nil {
		return fmt.Errorf("failed to open browser: %w (URL: %s)", err, unitURL)
	}

	return nil
}

// getAnnotationKey converts a context field to its annotation key
func getAnnotationKey(contextField string) string {
	// The ContextPath method returns the full path like ".metadata.annotations.confighub~1com/UnitSlug"
	// where dots are escaped with ~1 (tilde escaping)
	// We need to extract just the annotation key and unescape it
	fullPath := k8skit.K8sContextPath(contextField)
	// Remove the ".metadata.annotations." prefix
	key := strings.TrimPrefix(fullPath, ".metadata.annotations.")
	// Unescape the tilde-encoded dots using gaby's UnescapeDotsInPathSegment
	return gaby.UnescapeDotsInPathSegment(key)
}

// getK8sResource retrieves a Kubernetes resource by kind and name
func getK8sResource(dynamicClient dynamic.Interface, mapper meta.RESTMapper, kind, name, namespace, apiVersion string) (*unstructured.Unstructured, error) {
	// If apiVersion is provided, use it directly
	if apiVersion != "" {
		return getResourceWithAPIVersion(dynamicClient, mapper, kind, name, namespace, apiVersion)
	}

	// Otherwise, try common API versions for the kind
	commonVersions := getCommonAPIVersions(kind)
	var lastErr error

	for _, version := range commonVersions {
		resource, err := getResourceWithAPIVersion(dynamicClient, mapper, kind, name, namespace, version)
		if err == nil {
			return resource, nil
		}
		lastErr = err
	}

	return nil, fmt.Errorf("failed to find resource %s/%s with common API versions: %w", kind, name, lastErr)
}

// getResourceWithAPIVersion retrieves a resource with a specific API version
func getResourceWithAPIVersion(dynamicClient dynamic.Interface, mapper meta.RESTMapper, kind, name, namespace, apiVersion string) (*unstructured.Unstructured, error) {
	gv, err := schema.ParseGroupVersion(apiVersion)
	if err != nil {
		return nil, fmt.Errorf("invalid API version %s: %w", apiVersion, err)
	}

	// Create GVK for the resource
	gvk := schema.GroupVersionKind{
		Group:   gv.Group,
		Version: gv.Version,
		Kind:    kind,
	}

	// Use REST mapper to find the resource mapping
	mapping, err := mapper.RESTMapping(gvk.GroupKind(), gvk.Version)
	if err != nil {
		return nil, fmt.Errorf("failed to get REST mapping for %s: %w", gvk.String(), err)
	}

	// Try to get the resource
	resourceInterface := dynamicClient.Resource(mapping.Resource)

	// Check if this is a namespaced resource using the scope from the mapping
	var obj *unstructured.Unstructured
	var err2 error

	if mapping.Scope.Name() == meta.RESTScopeNameNamespace {
		// Namespaced resource
		obj, err2 = resourceInterface.Namespace(namespace).Get(context.Background(), name, metav1.GetOptions{})
	} else {
		// Cluster-scoped resource
		obj, err2 = resourceInterface.Get(context.Background(), name, metav1.GetOptions{})
	}

	if err2 != nil {
		return nil, err2
	}

	return obj, nil
}

// getCommonAPIVersions returns common API versions to try for a given kind
func getCommonAPIVersions(kind string) []string {
	kind = strings.ToLower(kind)

	// Map of kinds to their common API versions
	apiVersionMap := map[string][]string{
		"pod":                   {"v1"},
		"service":               {"v1"},
		"configmap":             {"v1"},
		"secret":                {"v1"},
		"serviceaccount":        {"v1"},
		"namespace":             {"v1"},
		"node":                  {"v1"},
		"persistentvolume":      {"v1"},
		"persistentvolumeclaim": {"v1"},
		"deployment":            {"apps/v1", "apps/v1beta2", "apps/v1beta1"},
		"statefulset":           {"apps/v1", "apps/v1beta2", "apps/v1beta1"},
		"daemonset":             {"apps/v1", "apps/v1beta2"},
		"replicaset":            {"apps/v1", "apps/v1beta2"},
		"job":                   {"batch/v1"},
		"cronjob":               {"batch/v1", "batch/v1beta1"},
		"ingress":               {"networking.k8s.io/v1", "networking.k8s.io/v1beta1"},
		"networkpolicy":         {"networking.k8s.io/v1"},
		"role":                  {"rbac.authorization.k8s.io/v1"},
		"rolebinding":           {"rbac.authorization.k8s.io/v1"},
		"clusterrole":           {"rbac.authorization.k8s.io/v1"},
		"clusterrolebinding":    {"rbac.authorization.k8s.io/v1"},
	}

	if versions, ok := apiVersionMap[kind]; ok {
		return versions
	}

	// Default to v1 for unknown kinds
	return []string{"v1"}
}
