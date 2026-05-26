// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/confighub/sdk/core/cubapi"
	goclientnew "github.com/confighub/sdk/core/openapi/goclient-new"
	"github.com/google/uuid"
	"github.com/spf13/cobra"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

// clusterFactPrefix is the reserved fact-key prefix for facts collected from a
// Kubernetes cluster. Collection owns this namespace: it (re)writes these keys and
// removes stale ones, while user-defined facts (any key outside the prefix) are
// left untouched.
const clusterFactPrefix = "Cluster."

const defaultStorageClassAnnotation = "storageclass.kubernetes.io/is-default-class"

var (
	crdGVR          = schema.GroupVersionResource{Group: "apiextensions.k8s.io", Version: "v1", Resource: "customresourcedefinitions"}
	storageClassGVR = schema.GroupVersionResource{Group: "storage.k8s.io", Version: "v1", Resource: "storageclasses"}
	ingressClassGVR = schema.GroupVersionResource{Group: "networking.k8s.io", Version: "v1", Resource: "ingressclasses"}
)

var (
	k8sCollectKubeContext string
	k8sCollectKubeconfig  string
	k8sCollectClusterName string
	k8sCollectDryRun      bool
)

var k8sCollectCmd = &cobra.Command{
	Use:   "collect [<target-slug-or-id>]",
	Short: "Collect facts from a Kubernetes cluster and store them on a Target",
	Long: getCommandHelp(`Collect facts about a Kubernetes cluster (version, CRDs, storage classes,
ingress classes) and store them on a Target under the reserved "Cluster." fact prefix.

Collection owns the "Cluster." namespace: it (re)writes those facts and removes stale
ones, while leaving user-defined facts (any key outside the prefix) untouched.

The kubeconfig is loaded with the same precedence as 'cub k8s source':

  1. --kubeconfig flag (highest priority, no merging)
  2. KUBECONFIG environment variable (merges multiple files)
  3. $HOME/.kube/config (default)

Examples:
`+"```"+`
  # Collect facts and store them on a target
  cub k8s collect --space my-space --kube-context kind-kind my-target

  # Preview the facts without updating any target
  cub k8s collect --kube-context kind-kind --dry-run
`+"```"+`
`, ""),
	Args: cobra.MaximumNArgs(1),
	PreRunE: func(cmd *cobra.Command, args []string) error {
		// --dry-run only contacts the cluster, so it needs auth but not a space.
		if k8sCollectDryRun {
			return globalPreRun(cmd, args)
		}
		return spacePreRunE(cmd, args)
	},
	RunE: k8sCollectCmdRun,
}

func init() {
	addSpaceFlags(k8sCollectCmd)
	k8sCollectCmd.Flags().StringVar(&k8sCollectKubeContext, "kube-context", "", "kubeconfig context of the cluster to collect facts from (defaults to the current context)")
	k8sCollectCmd.Flags().StringVar(&k8sCollectKubeconfig, "kubeconfig", "", "path to the kubeconfig file to use")
	k8sCollectCmd.Flags().StringVar(&k8sCollectClusterName, "cluster-name", "", "value for the Cluster.Name fact (defaults to the kube context name)")
	k8sCollectCmd.Flags().BoolVar(&k8sCollectDryRun, "dry-run", false, "print the collected facts without updating any target")
	k8sCmd.AddCommand(k8sCollectCmd)
}

func k8sCollectCmdRun(cmd *cobra.Command, args []string) error {
	if !k8sCollectDryRun && len(args) == 0 {
		return fmt.Errorf("a target slug or ID is required unless --dry-run is set")
	}

	// Build a rest.Config with the same kubeconfig precedence as `cub k8s source`.
	loadingRules := clientcmd.NewDefaultClientConfigLoadingRules()
	if k8sCollectKubeconfig != "" {
		loadingRules.ExplicitPath = k8sCollectKubeconfig
	}
	overrides := &clientcmd.ConfigOverrides{}
	if k8sCollectKubeContext != "" {
		overrides.CurrentContext = k8sCollectKubeContext
	}
	restConfig, err := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(loadingRules, overrides).ClientConfig()
	if err != nil {
		return fmt.Errorf("failed to get Kubernetes config: %w", err)
	}

	facts, err := collectKubernetesClusterFacts(restConfig)
	if err != nil {
		return err
	}

	// Cluster.Name is not discoverable from the cluster; use the override or the context name.
	clusterName := k8sCollectClusterName
	if clusterName == "" {
		clusterName = k8sCollectKubeContext
	}
	if clusterName != "" {
		facts[clusterFactPrefix+"Name"] = clusterName
	}

	if k8sCollectDryRun {
		printCollectedFacts(facts)
		return nil
	}

	currentTarget, err := apiGetTargetFromSlug(args[0], selectedSpaceID, "*")
	if err != nil {
		return err
	}
	spaceID := uuid.MustParse(selectedSpaceID)

	// Build a JSON merge patch that owns the "Cluster." namespace: set freshly collected
	// facts and null out previously collected facts that are no longer present. User facts
	// (keys outside the prefix) are not included, so the merge patch preserves them.
	factPatch := map[string]interface{}{}
	for k, v := range facts {
		factPatch[k] = v
	}
	for k := range currentTarget.Target.Facts {
		if strings.HasPrefix(k, clusterFactPrefix) {
			if _, ok := facts[k]; !ok {
				factPatch[k] = nil
			}
		}
	}

	patchJSON, err := json.Marshal(map[string]interface{}{"Facts": factPatch})
	if err != nil {
		return err
	}

	patchParams := &goclientnew.PatchTargetParams{}
	targetRes, err := cubClientNew.PatchTargetWithBodyWithResponse(ctx, spaceID, currentTarget.Target.TargetID, patchParams, "application/merge-patch+json", bytes.NewReader(patchJSON))
	if cubapi.IsAPIError(err, targetRes) {
		return cubapi.InterpretErrorGeneric(err, targetRes)
	}

	tprint("Collected %d facts and updated target %s", len(facts), targetRes.JSON200.Slug)
	printCollectedFacts(facts)
	return nil
}

// collectKubernetesClusterFacts gathers facts about the cluster reachable via restConfig,
// keyed under the reserved "Cluster." prefix. List-valued facts are comma-separated and sorted.
func collectKubernetesClusterFacts(restConfig *rest.Config) (map[string]string, error) {
	facts := map[string]string{}

	discoveryClient, err := discovery.NewDiscoveryClientForConfig(restConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create discovery client: %w", err)
	}
	ver, err := discoveryClient.ServerVersion()
	if err != nil {
		return nil, fmt.Errorf("failed to get server version: %w", err)
	}
	// Cleaned to e.g. "1.31.2" (suitable to pass to kubeconform).
	facts[clusterFactPrefix+"KubernetesVersion"] = strings.TrimPrefix(ver.GitVersion, "v")

	dynamicClient, err := dynamic.NewForConfig(restConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create dynamic client: %w", err)
	}

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
	facts[clusterFactPrefix+"ClusterScopedCRDs"] = sortedCSV(crdClusterScoped)
	facts[clusterFactPrefix+"NamespaceScopedCRDs"] = sortedCSV(crdNamespaceScoped)

	// Built-in (non-CRD) served resource types, split by scope, as group/version/Kind.
	builtinClusterScoped, builtinNamespaceScoped, err := builtinResourceTypes(discoveryClient, crdGVKs)
	if err != nil {
		return nil, err
	}
	facts[clusterFactPrefix+"ClusterScopedResourceTypes"] = sortedCSV(builtinClusterScoped)
	facts[clusterFactPrefix+"NamespaceScopedResourceTypes"] = sortedCSV(builtinNamespaceScoped)

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
	facts[clusterFactPrefix+"StorageClasses"] = sortedCSV(scNames)
	facts[clusterFactPrefix+"DefaultStorageClass"] = defaultStorageClass

	// Ingress classes.
	ingressClasses, err := dynamicClient.Resource(ingressClassGVR).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to list ingress classes: %w", err)
	}
	var icNames []string
	for i := range ingressClasses.Items {
		icNames = append(icNames, ingressClasses.Items[i].GetName())
	}
	facts[clusterFactPrefix+"IngressClasses"] = sortedCSV(icNames)

	return facts, nil
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

// printCollectedFacts prints the facts as --fact flags, sorted, so the output can be
// copied into a manual `cub target update`.
func printCollectedFacts(facts map[string]string) {
	keys := make([]string, 0, len(facts))
	for k := range facts {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		tprint("--fact '%s=%s'", k, facts[k])
	}
}
