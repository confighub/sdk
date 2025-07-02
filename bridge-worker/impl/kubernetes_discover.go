// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package impl

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/confighub/sdk/bridge-worker/api"
	"github.com/confighub/sdk/configkit/k8skit"
	goclientnew "github.com/confighub/sdk/openapi/goclient-new"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/labels"
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

var crdGVK = schema.GroupVersionKind{
	Group:   "apiextensions.k8s.io",
	Version: "v1",
	Kind:    "CustomResourceDefinition",
}

// shouldSkipResource determines if a resource should be skipped during discovery pre-processing
// Based on public docs
// https://kubernetes.io/docs/reference/using-api/api-concepts/#:~:text=Most%20Kubernetes%20API%20resource%20types,trigger%20API%2Dinitiated%20eviction).
func shouldSkipResource(res metav1.APIResource) bool {
	// 1. Skip if doesn't support list operation
	if !slices.Contains(res.Verbs, "list") {
		return true
	}

	// 2. Skip virtual resources that can never be listed successfully
	if isVirtualResource(res.Kind, res.Name) {
		return true
	}

	// 3. Skip operational resources that only support create (like review APIs)
	if len(res.Verbs) == 1 && res.Verbs[0] == "create" {
		return true
	}

	// 4. Skip if it's missing core CRUD operations that indicate a real resource
	hasGet := slices.Contains(res.Verbs, "get")
	hasWatch := slices.Contains(res.Verbs, "watch")
	if !hasGet && !hasWatch {
		// Likely an operational resource, not a stored resource
		return true
	}

	return false
}

// isVirtualResource checks for virtual resources that can NEVER be listed successfully
func isVirtualResource(kind, name string) bool {
	// Resources that end in "Review" are typically operational, not stored
	if strings.HasSuffix(kind, "Review") {
		return true
	}

	// Virtual resource kinds that will always fail to list
	virtualResourceKinds := []string{
		"Binding",                  // Virtual binding operations (NOT ClusterRoleBinding/RoleBinding)
		"Scale",                    // Virtual scaling interface
		"Eviction",                 // Pod eviction operations
		"LocalSubjectAccessReview", // Authorization checks
		"SelfSubjectAccessReview",  // Self-authorization checks
		"SelfSubjectRulesReview",   // Rules review
		"SubjectAccessReview",      // Access review operations
		"TokenReview",              // Token validation
		"SelfSubjectReview",        // Self subject review
	}

	// Virtual resource names (plural forms) that will always fail to list
	virtualResourceNames := []string{
		"bindings",                  // Virtual binding operations
		"subjectaccessreviews",      // Authorization review API
		"selfsubjectaccessreviews",  // Self authorization review API
		"localsubjectaccessreviews", // Local authorization review API
		"selfsubjectrulesreviews",   // Rules review API
		"tokenreviews",              // Token validation API
		"selfsubjectreviews",        // Self subject review API
	}

	return slices.Contains(virtualResourceKinds, kind) || slices.Contains(virtualResourceNames, name)
}

// isAdministrativeResource checks for administrative resources that are blocked by default
// but can be explicitly requested. This includes cluster-level administrative resources
// and ephemeral resources not typically managed by application developers.
func isAdministrativeResource(kind, name string) bool {
	// Administrative resource kinds (cluster-level administrative resources)
	adminResourceKinds := []string{
		"APIService",                // API aggregation service descriptors
		"CertificateSigningRequest", // Certificate signing requests
		"ComponentStatus",           // Cluster component health status
		"Event",                     // Ephemeral event reports (both v1/Event and events.k8s.io/v1/Event)
		"IPAddress",                 // IP address management
		"Lease",                     // Coordination and leader election leases
		"LeaseCandidate",            // Lease candidate definitions
		"Node",                      // Cluster worker nodes
		"RuntimeClass",              // Container runtime class definitions
		"ServiceCIDR",               // Service CIDR range definitions
	}

	// Administrative resource names (plural forms)
	adminResourceNames := []string{
		"apiservices",                // API aggregation service descriptors
		"certificatesigningrequests", // Certificate signing requests
		"componentstatuses",          // Cluster component health status
		"events",                     // Ephemeral event reports
		"ipaddresses",                // IP address management
		"leases",                     // Coordination and leader election leases
		"leasecandidates",            // Lease candidate definitions
		"nodes",                      // Cluster worker nodes
		"runtimeclasses",             // Container runtime class definitions
		"servicecidrs",               // Service CIDR range definitions
	}

	return slices.Contains(adminResourceKinds, kind) || slices.Contains(adminResourceNames, name)
}

// listResourcesForGVK handles the common pattern of listing resources for a GVK with different options
func listResourcesForGVK(k8sclient KubernetesClient, gvk schema.GroupVersionKind, listOptions []client.ListOption, logErrorsOnly bool) ([]*unstructured.Unstructured, error) {
	var resources []*unstructured.Unstructured

	// Create list object
	list := &unstructured.UnstructuredList{}
	list.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   gvk.Group,
		Version: gvk.Version,
		Kind:    gvk.Kind,
	})

	if err := k8sclient.List(context.Background(), list, listOptions...); err != nil {
		if logErrorsOnly {
			// Check if this is an expected error during discovery (resource type exists but no instances)
			if strings.Contains(err.Error(), "no matches for kind") ||
				strings.Contains(err.Error(), "the server doesn't have a resource type") {
				// These are expected during dynamic discovery - log at info level instead of error
				log.Log.Info("Resource type discovered but no instances found", "gvk", gvk.String(),
					"reason", err.Error())
			} else {
				// Log unexpected errors at error level
				log.Log.Error(err, "Failed to list resources", "gvk", gvk.String())
			}
			return resources, nil
		}
		return nil, err
	}

	for _, item := range list.Items {
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
// This function now properly respects the includeClusterScoped and includeNamespaced flags
// for both dynamic discovery and static fallback cases.
func (rd *ResourceDiscovery) getResourceTypesWithFallback(includeNamespaced bool) (map[string]ResourceTypeInfo, error) {
	// Try dynamic discovery using the ConfigProvider, else fallback to static types
	if allResourceTypes, err := DiscoverAllResourceTypes(rd.cfg); err == nil {
		// Filter the discovered resource types based on include flags
		// This ensures that when include_cluster=false, cluster-scoped resources are excluded
		filtered := make(map[string]ResourceTypeInfo)
		for resourceType, info := range allResourceTypes {
			if (rd.config.IncludeCluster && !info.Namespaced) ||
				(includeNamespaced && info.Namespaced) {
				filtered[resourceType] = info
			}
		}
		return filtered, nil
	}

	// Fallback to static resource types
	resourceTypes := make(map[string]ResourceTypeInfo)
	if rd.config.IncludeCluster {
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
	if cfg == nil {
		return nil, fmt.Errorf("config is nil")
	}
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

			// Skip resources that should be filtered during pre-processing
			if shouldSkipResource(res) {
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

// GetCustomResourceDefinitions fetches only Custom Resource Definitions from the cluster
func GetCustomResourceDefinitions(k8sclient KubernetesClient) ([]*unstructured.Unstructured, error) {
	return listResourcesForGVK(k8sclient, crdGVK, []client.ListOption{}, false)
}

// ValidateResourceTypeFormat checks if a resource type string is properly formatted
// Returns true if the format matches the expected GVK pattern
// Valid formats: "v1/Kind" (core API) or "group/version/Kind" (non-core API)
func ValidateResourceTypeFormat(resourceType string) bool {
	parts := strings.Split(resourceType, "/")
	if len(parts) != 2 && len(parts) != 3 {
		return false
	}

	// Check that all parts are non-empty
	for _, part := range parts {
		if strings.TrimSpace(part) == "" {
			return false
		}
	}

	// Validate that Kind starts with uppercase (Kubernetes convention)
	kind := parts[len(parts)-1]
	if len(kind) == 0 || strings.ToUpper(kind[:1]) != kind[:1] {
		return false
	}

	return true
}

// getResourceType formats the resource type using GVK
func getResourceType(gvk schema.GroupVersionKind) string {
	if gvk.Group == "" {
		return fmt.Sprintf("%s/%s", gvk.Version, gvk.Kind)
	}
	return fmt.Sprintf("%s/%s/%s", gvk.Group, gvk.Version, gvk.Kind)
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

// ImportConfig holds the configuration options for resource import
type ImportConfig struct {
	IncludeSystem  bool
	IncludeCustom  bool
	IncludeCluster bool
	Namespaces     []string
	Filters        []goclientnew.ImportFilter
}

// ResourceDiscovery encapsulates the logic for discovering and filtering Kubernetes resources
type ResourceDiscovery struct {
	client KubernetesClient
	config *ImportConfig
	cfg    *rest.Config
}

// NewResourceDiscovery creates a new ResourceDiscovery instance
func NewResourceDiscovery(client KubernetesClient, config *ImportConfig, cfg *rest.Config) *ResourceDiscovery {
	return &ResourceDiscovery{
		client: client,
		config: config,
		cfg:    cfg,
	}
}

// discoveryQuery represents a resource discovery query with its constraints
type discoveryQuery struct {
	ResourceTypes    map[string]ResourceTypeInfo
	TargetNamespaces []string
	Filters          []goclientnew.ImportFilter
	IncludeSystem    bool
}

// ListOptionsBuilder helps construct Kubernetes list options consistently
type ListOptionsBuilder struct {
	namespace     string
	labelFilters  []goclientnew.ImportFilter
	fieldFilters  []goclientnew.ImportFilter
	includeSystem bool
	isNamespaced  bool
}

// NewListOptionsBuilder creates a new builder
func NewListOptionsBuilder() *ListOptionsBuilder {
	return &ListOptionsBuilder{}
}

// WithNamespace sets the target namespace
func (b *ListOptionsBuilder) WithNamespace(namespace string) *ListOptionsBuilder {
	b.namespace = namespace
	return b
}

// WithFilters adds filters to the builder
func (b *ListOptionsBuilder) WithFilters(filters []goclientnew.ImportFilter) *ListOptionsBuilder {
	for _, filter := range filters {
		switch filter.Type {
		case "label":
			b.labelFilters = append(b.labelFilters, filter)
		case "field":
			b.fieldFilters = append(b.fieldFilters, filter)
		}
	}
	return b
}

// WithSystemFiltering configures system resource filtering
func (b *ListOptionsBuilder) WithSystemFiltering(includeSystem, isNamespaced bool) *ListOptionsBuilder {
	b.includeSystem = includeSystem
	b.isNamespaced = isNamespaced
	return b
}

// Build constructs the final list options
func (b *ListOptionsBuilder) Build() []client.ListOption {
	var listOptions []client.ListOption

	// Add namespace restriction if specified
	if b.namespace != "" {
		listOptions = append(listOptions, client.InNamespace(b.namespace))
	}

	// Add label selectors
	if len(b.labelFilters) > 0 {
		var labelSelectors []string
		for _, filter := range b.labelFilters {
			labelSelectors = append(labelSelectors, buildSelector(filter, true)...)
		}
		if len(labelSelectors) > 0 {
			labelSelector := strings.Join(labelSelectors, ",")
			listOptions = append(listOptions, client.MatchingLabelsSelector{
				Selector: parseSelector(labelSelector),
			})
		}
	}

	// Add field selectors
	var fieldSelectors []string
	for _, filter := range b.fieldFilters {
		fieldSelectors = append(fieldSelectors, buildSelector(filter, false)...)
	}

	// Add system namespace exclusion if needed
	if !b.includeSystem && b.isNamespaced && b.namespace == "" {
		fieldSelectors = append(fieldSelectors, "metadata.namespace!=kube-system")
	}

	if len(fieldSelectors) > 0 {
		fieldSelector := strings.Join(fieldSelectors, ",")
		listOptions = append(listOptions, client.MatchingFieldsSelector{
			Selector: parseFieldSelector(fieldSelector),
		})
	}

	return listOptions
}

// Discover performs resource discovery based on the configured parameters
func (rd *ResourceDiscovery) Discover() ([]*unstructured.Unstructured, error) {
	query, err := rd.buildQuery()
	if err != nil {
		return nil, fmt.Errorf("failed to build discovery query: %w", err)
	}

	resources, err := rd.executeQuery(query)
	if err != nil {
		return nil, fmt.Errorf("failed to execute discovery query: %w", err)
	}

	return rd.applyPostProcessingFilters(resources), nil
}

// buildQuery creates a DiscoveryQuery from the ImportConfig
func (rd *ResourceDiscovery) buildQuery() (*discoveryQuery, error) {
	// Get resource types based on configuration - always include namespaced resources,
	// but only include cluster-scoped resources if explicitly requested
	resourceTypes, err := rd.getResourceTypesWithFallback(true)
	if err != nil {
		return nil, err
	}

	// Filter resource types based on include flags (handles CRDs and additional filtering)
	filteredTypes := rd.filterResourceTypes(resourceTypes)

	// Determine target namespaces
	targetNamespaces := rd.config.Namespaces

	// If include_system=true and we have explicit namespace filters, expand to include system namespaces
	if rd.config.IncludeSystem && len(targetNamespaces) > 0 {
		// Check if any system namespaces are already included
		hasSystemNamespaces := false
		for _, ns := range targetNamespaces {
			if slices.Contains(systemNamespaces, ns) {
				hasSystemNamespaces = true
				break
			}
		}

		// Add system namespaces if not already present
		if !hasSystemNamespaces {
			targetNamespaces = append(targetNamespaces, systemNamespaces...)
		}
	}

	// Filter out system namespaces if include_system=false
	if !rd.config.IncludeSystem {
		targetNamespaces = filterNamespaces(targetNamespaces, rd.config.IncludeSystem)
	}

	return &discoveryQuery{
		ResourceTypes:    filteredTypes,
		TargetNamespaces: targetNamespaces,
		Filters:          rd.config.Filters,
		IncludeSystem:    rd.config.IncludeSystem,
	}, nil
}

// filterResourceTypes filters resource types based on configuration
func (rd *ResourceDiscovery) filterResourceTypes(resourceTypes map[string]ResourceTypeInfo) map[string]ResourceTypeInfo {
	filtered := make(map[string]ResourceTypeInfo)
	for resourceType, info := range resourceTypes {
		// For non-CRD cluster-scoped resources, check include_cluster
		// Handle CRDs separately - they are controlled by include_custom only
		if info.GVK == crdGVK {
			if rd.config.IncludeCustom {
				filtered[resourceType] = info
			}
			continue
		}
		filtered[resourceType] = info
	}
	return filtered
}

// executeQuery executes the discovery query and returns raw resources
func (rd *ResourceDiscovery) executeQuery(query *discoveryQuery) ([]*unstructured.Unstructured, error) {
	if len(query.TargetNamespaces) > 0 {
		// Query specific namespaces
		return rd.queryResources(query, true)
	} else {
		// Query all namespaces
		return rd.queryResources(query, false)
	}
}

func (rd *ResourceDiscovery) queryResources(query *discoveryQuery, namespacesOnly bool) ([]*unstructured.Unstructured, error) {
	var allResources []*unstructured.Unstructured

	for _, info := range query.ResourceTypes {
		if namespacesOnly && !info.Namespaced {
			continue
		}

		listOptionsBuilder := NewListOptionsBuilder().
			WithFilters(query.Filters).
			WithSystemFiltering(query.IncludeSystem, info.Namespaced)

		if namespacesOnly {
			for _, namespace := range query.TargetNamespaces {
				listOptionsBuilder = listOptionsBuilder.WithNamespace(namespace)
				listOptions := listOptionsBuilder.Build()
				resources, err := listResourcesForGVK(rd.client, info.GVK, listOptions, true)
				if err != nil {
					continue // Log error but continue with other resources
				}
				allResources = append(allResources, resources...)
			}
		} else {
			listOptions := listOptionsBuilder.Build()
			resources, err := listResourcesForGVK(rd.client, info.GVK, listOptions, true)
			if err != nil {
				continue // Log error but continue with other resources
			}
			allResources = append(allResources, resources...)
		}
	}

	return allResources, nil
}

// applyPostProcessingFilters applies complex filters that can't be done server-side
func (rd *ResourceDiscovery) applyPostProcessingFilters(resources []*unstructured.Unstructured) []*unstructured.Unstructured {
	return rd.applyGenericFilters(resources)
}

// NewImportConfigFromRequest creates an ImportConfig from an ImportRequest
func NewImportConfigFromRequest(request *goclientnew.ImportRequest) *ImportConfig {
	config := &ImportConfig{
		Filters: request.Filters,
		// Defaults: all false for security and predictability
		IncludeSystem:  parseBoolOption(request.Options, "include_system", false),
		IncludeCustom:  parseBoolOption(request.Options, "include_custom", false),
		IncludeCluster: parseBoolOption(request.Options, "include_cluster", false),
	}

	// Validate resource type formats in filters
	for _, filter := range request.Filters {
		if filter.Type == "resource_type" {
			for _, resourceType := range filter.Values {
				if !ValidateResourceTypeFormat(resourceType) {
					// Note: We continue processing but log the error for debugging
					log.Log.Error(nil, "Invalid resource type format detected",
						"resourceType", resourceType,
						"expectedFormat", "Group/Version/Kind (e.g., 'v1/Pod', 'apps/v1/Deployment')")
				}
			}
		}
	}

	// Extract namespace filters
	config.Namespaces = extractNamespaceFilters(request.Filters)

	return config
}

// parseBoolOption extracts a boolean option from the options map
func parseBoolOption(options *goclientnew.ImportOptions, key string, defaultValue bool) bool {
	if options == nil {
		return defaultValue
	}
	if val, ok := (*options)[key]; ok {
		if boolVal, ok := val.(bool); ok {
			return boolVal
		}
	}
	return defaultValue
}

// GetResourcesWithConfig handles resource import with a clean configuration structure
func GetResourcesWithConfig(k8sclient KubernetesClient, config *ImportConfig, cfg *rest.Config) ([]*unstructured.Unstructured, error) {
	// Use the new ResourceDiscovery structure
	discovery := NewResourceDiscovery(k8sclient, config, cfg)
	allResources, err := discovery.Discover()
	if err != nil {
		return nil, fmt.Errorf("resource discovery failed: %w", err)
	}

	// Add CRDs if requested (only needed when using namespaced queries, since cluster-wide queries already include them)
	if config.IncludeCustom && len(config.Namespaces) > 0 {
		crds, err := GetCustomResourceDefinitions(k8sclient)
		if err != nil {
			return nil, fmt.Errorf("failed to get CustomResourceDefinitions: %w", err)
		}
		allResources = append(allResources, crds...)
	}

	return allResources, nil
}

// extractNamespaceFilters extracts namespace filter values from the filter list
func extractNamespaceFilters(filters []goclientnew.ImportFilter) []string {
	namespaces := []string{} // Initialize as empty slice rather than nil
	for _, filter := range filters {
		if filter.Type == "namespace" && filter.Operator == "include" {
			namespaces = append(namespaces, filter.Values...)
		}
	}
	return namespaces
}

// systemNamespaces are the namespaces that should be excluded by default
var systemNamespaces = []string{
	"kube-system",
	"kube-public",
	"kube-node-lease",
	// Note: "default" is usually not considered a system namespace
	// since users often put workloads there
}

// filterNamespaces removes system namespaces from namespace list if includeSystem is false
func filterNamespaces(namespaces []string, includeSystem bool) []string {
	if includeSystem {
		return namespaces
	}

	filtered := []string{} // Initialize as empty slice rather than nil
	for _, ns := range namespaces {
		if !slices.Contains(systemNamespaces, ns) {
			filtered = append(filtered, ns)
		}
	}
	return filtered
}

// buildSelector converts a filter into Kubernetes selector syntax
// This is a shared implementation for both label and field selectors
// isLabelSelector allows negation for label selectors
// Only adds "!" prefix for label selectors, not field selectors
func buildSelector(filter goclientnew.ImportFilter, isLabelSelector bool) []string {
	var selectors []string
	for _, value := range filter.Values {
		switch filter.Operator {
		case "include", "equals":
			selectors = append(selectors, value)
		case "exclude", "not_equals":
			if strings.Contains(value, "=") {
				parts := strings.SplitN(value, "=", 2)
				selectors = append(selectors, fmt.Sprintf("%s!=%s", parts[0], parts[1]))
			} else {
				if isLabelSelector {
					selectors = append(selectors, fmt.Sprintf("!%s", value))
				}
			}
		}
	}
	return selectors
}

// Helper functions for parsing selectors (simplified implementations)
func parseSelector(labelSelector string) labels.Selector {
	selector, err := labels.Parse(labelSelector)
	if err != nil {
		// Fall back to everything selector if parsing fails
		return labels.Everything()
	}
	return selector
}

func parseFieldSelector(fieldSelector string) fields.Selector {
	selector, err := fields.ParseSelector(fieldSelector)
	if err != nil {
		// Fall back to everything selector if parsing fails
		return fields.Everything()
	}
	return selector
}

// applyGenericFilters applies filter logic to the resource list
func (rd *ResourceDiscovery) applyGenericFilters(resources []*unstructured.Unstructured) []*unstructured.Unstructured {
	if len(rd.config.Filters) == 0 && rd.config.IncludeSystem {
		return resources
	}

	var filtered []*unstructured.Unstructured

	for _, resource := range resources {
		include := true

		resourceGVK := resource.GetObjectKind().GroupVersionKind()
		// Apply system namespace filtering first (catch any resources that slipped through)
		if !rd.config.IncludeSystem && slices.Contains(systemNamespaces, resource.GetNamespace()) {
			include = false
		}
		if resourceGVK == crdGVK && rd.config.IncludeCustom {
			filtered = append(filtered, resource)
			continue
		}

		if include {
			for _, filter := range rd.config.Filters {
				switch filter.Type {
				case "namespace":
					if filter.Operator == "exclude" {
						if slices.Contains(filter.Values, resource.GetNamespace()) {
							include = false
							break
						}
					} else if filter.Operator == "include" {
						// For include filters, only include resources from specified namespaces
						// BUT if include_system=true, also allow system namespaces even if not in original filter
						resourceNamespace := resource.GetNamespace()
						inOriginalFilter := slices.Contains(filter.Values, resourceNamespace)
						inSystemNamespaces := slices.Contains(systemNamespaces, resourceNamespace)

						// Include if: in original filter OR (include_system=true AND in system namespaces)
						// Cluster-scoped resources (empty namespace) should be excluded when namespace filters are applied
						if resourceNamespace == "" || (!inOriginalFilter && !(rd.config.IncludeSystem && inSystemNamespaces)) {
							include = false
							break
						}
					}

				case "resource_type":
					gvk := resource.GetObjectKind().GroupVersionKind()
					resourceType := getResourceType(gvk)
					// Check if this is an administrative resource that should be excluded by default
					// Explicit resource_type filters will override this
					if isAdministrativeResource(resourceGVK.Kind, "") {
						include = false
					}

					if filter.Operator == "exclude" {
						if slices.Contains(filter.Values, resourceType) {
							include = false
							break
						}
					} else if filter.Operator == "include" {
						if slices.Contains(filter.Values, resourceType) {
							break
						} else {
							// if we found the resource type, break out of the loop
							include = false
							break
						}
					}

				case "label":
					labels := resource.GetLabels()
					if filter.Operator == "exclude" {
						for _, labelFilter := range filter.Values {
							if matchesFilter(labels, labelFilter) {
								include = false
								break
							}
						}
					} else if filter.Operator == "include" {
						hasMatch := false
						for _, labelFilter := range filter.Values {
							if matchesFilter(labels, labelFilter) {
								hasMatch = true
								break
							}
						}
						if !hasMatch {
							include = false
							break
						}
					}

				case "annotation":
					annotations := resource.GetAnnotations()
					if filter.Operator == "exclude" {
						for _, annotationFilter := range filter.Values {
							if matchesFilter(annotations, annotationFilter) {
								include = false
								break
							}
						}
					} else if filter.Operator == "include" {
						hasMatch := false
						for _, annotationFilter := range filter.Values {
							if matchesFilter(annotations, annotationFilter) {
								hasMatch = true
								break
							}
						}
						if !hasMatch {
							include = false
							break
						}
					}
				}

				if !include {
					break
				}
			}
		}

		if include {
			filtered = append(filtered, resource)
		}
	}

	return filtered
}

func matchesFilter(lookup map[string]string, filter string) bool {
	if lookup == nil {
		return false
	}

	if strings.Contains(filter, "=") {
		parts := strings.SplitN(filter, "=", 2)
		if len(parts) == 2 {
			key, value := parts[0], parts[1]
			return lookup[key] == value
		}
	}
	// Just check if lookup key exists
	_, exists := lookup[filter]
	return exists
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
