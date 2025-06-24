// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package k8skit

import (
	"fmt"
	"sort"

	"github.com/confighub/sdk/function/api"
)

// dependencyGraph represents a directed graph for resource dependencies
type dependencyGraph struct {
	vertices map[api.ResourceTypeAndName]*resourceDoc
	edges    map[api.ResourceTypeAndName]map[api.ResourceTypeAndName]bool // from -> to -> exists
}

// newDependencyGraph creates a new dependency graph
func newDependencyGraph() *dependencyGraph {
	return &dependencyGraph{
		vertices: make(map[api.ResourceTypeAndName]*resourceDoc),
		edges:    make(map[api.ResourceTypeAndName]map[api.ResourceTypeAndName]bool),
	}
}

// getResourceKey returns a unique key for a resource
func getResourceKey(doc resourceDoc) api.ResourceTypeAndName {
	return api.ResourceTypeAndName(string(doc.resourceType) + "#" + string(doc.resourceName))
}

// addVertex adds a vertex to the graph
func (g *dependencyGraph) addVertex(key api.ResourceTypeAndName, doc *resourceDoc) {
	g.vertices[key] = doc
	if g.edges[key] == nil {
		g.edges[key] = make(map[api.ResourceTypeAndName]bool)
	}
}

// addEdge adds a directed edge from 'from' to 'to' (from depends on to)
func (g *dependencyGraph) addEdge(from, to api.ResourceTypeAndName) {
	if g.edges[from] == nil {
		g.edges[from] = make(map[api.ResourceTypeAndName]bool)
	}
	g.edges[from][to] = true
}

// addNamespaceEdges adds implicit dependencies from namespaced resources to their namespaces
func (g *dependencyGraph) addNamespaceEdges(docs []resourceDoc) {
	namespaces := make(map[string]api.ResourceTypeAndName) // namespace name -> resource key

	// First, collect all namespace resources
	for _, doc := range docs {
		// Extract kind from resourceType (format: "apiVersion/kind")
		if doc.resourceType == "v1/Namespace" {
			// Extract name from resourceName (format: "namespace/name" or "/name")
			name := GetNameFromResourceName(doc.resourceName)
			key := getResourceKey(doc)
			namespaces[name] = key
		}
	}

	// Then add edges from namespaced resources to their namespaces
	for _, doc := range docs {
		// Extract namespace from the resource
		namespace := GetNamespaceFromResourceName(doc.resourceName)

		if namespace != "" && doc.resourceType != "v1/Namespace" {
			fromKey := getResourceKey(doc)
			if toKey, exists := namespaces[namespace]; exists {
				g.addEdge(fromKey, toKey)
			}
		}
	}
}

// topologicalSort performs a topological sort of the dependency graph
func (g *dependencyGraph) topologicalSort() ([]resourceDoc, error) {
	// Kahn's algorithm for topological sorting
	inDegree := make(map[api.ResourceTypeAndName]int)

	// Initialize in-degree count
	for vertex := range g.vertices {
		inDegree[vertex] = 0
	}

	// Calculate in-degrees
	for _, edges := range g.edges {
		for to := range edges {
			if _, exists := g.vertices[to]; exists {
				inDegree[to]++
			}
		}
	}

	// Find all vertices with no incoming edges
	queue := make([]api.ResourceTypeAndName, 0)
	for vertex, degree := range inDegree {
		if degree == 0 {
			queue = append(queue, vertex)
		}
	}

	var result []resourceDoc

	// Process vertices in topological order
	for len(queue) > 0 {
		// Sort queue to ensure deterministic output
		sort.SliceStable(queue, func(i, j int) bool {
			return queue[i] < queue[j]
		})

		// Remove a vertex with no incoming edges
		current := queue[0]
		queue = queue[1:]

		if doc, exists := g.vertices[current]; exists {
			result = append(result, *doc)
		}

		// For each neighbor of the current vertex
		for neighbor := range g.edges[current] {
			if _, exists := g.vertices[neighbor]; exists {
				inDegree[neighbor]--
				if inDegree[neighbor] == 0 {
					queue = append(queue, neighbor)
				}
			}
		}
	}

	// Check for cycles
	if len(result) != len(g.vertices) {
		return nil, fmt.Errorf("circular dependency detected in resources")
	}

	return result, nil
}

// getResourcePriority returns the priority order for different Kubernetes resource kinds
// Lower numbers have higher priority (will be applied first)
func getResourcePriority(resourceType api.ResourceType) int {
	// Based on Kustomize's ordering strategy and Kubernetes best practices
	priorityMap := map[api.ResourceType]int{
		// CRDs must be applied first as they define new resource types
		// that other resources in the doc might be instances of
		"apiextensions.k8s.io/v1/CustomResourceDefinition": 10,

		// Namespaces must exist before any namespace-scoped resources
		// can be created within them
		"v1/Namespace": 20,

		// ServiceAccount must be early as it's referenced by RBAC bindings
		"v1/ServiceAccount": 30, // Pods run under service accounts, referenced by RoleBindings

		// Cluster-wide resources that namespace-scoped resources often reference
		"storage.k8s.io/v1/StorageClass": 40, // PVCs may reference storage classes

		// Cluster-scoped RBAC resources
		"rbac.authorization.k8s.io/v1/ClusterRole":        100, // Referenced by ClusterRoleBindings and RoleBindings
		"rbac.authorization.k8s.io/v1/ClusterRoleBinding": 110, // Grants cluster-wide permissions

		// RBAC resources that grant permissions to service accounts
		"rbac.authorization.k8s.io/v1/Role":        200, // Defines permissions within a namespace
		"rbac.authorization.k8s.io/v1/RoleBinding": 210, // Grants Role permissions to users/groups/service accounts

		// Resource constraints should be set up early
		"v1/LimitRange":    220, // Enforces resource constraints on pods
		"v1/ResourceQuota": 230, // Enforces resource quotas in namespace

		// Configuration data - after RBAC so permissions are set, but before workloads that use them
		// Kubernetes will retry pod creation if these don't exist yet
		"v1/Secret":    250, // Pods mount secrets as volumes or env vars
		"v1/ConfigMap": 260, // Pods mount configmaps as volumes or env vars

		// Storage resources - PVs before PVCs as PVCs bind to PVs
		"v1/PersistentVolume":      300, // Cluster-scoped storage that PVCs bind to
		"v1/PersistentVolumeClaim": 310, // Claims storage for pods to use

		// Networking resources created before workloads that use them
		"v1/Service":                   400, // Pods may reference services via DNS or env vars
		"networking.k8s.io/v1/Ingress": 410, // Routes traffic to services

		// Network Policy created before pods
		"networking.k8s.io/v1/NetworkPolicy": 450, // Controls network traffic to/from pods

		// Workloads - depend on all above resources
		"apps/v1/Deployment":  500, // Common workload type
		"apps/v1/StatefulSet": 510, // Workload with stable network identity and storage
		"apps/v1/DaemonSet":   520, // Runs on all/selected nodes
		"apps/v1/ReplicaSet":  530, // Usually created by Deployments
		"batch/v1/Job":        540, // One-time tasks
		"batch/v1/CronJob":    550, // Scheduled jobs
		"v1/Pod":              560, // Lowest level workload

		// Post-deployment policy resources that configure existing workloads
		"autoscaling/v2/HorizontalPodAutoscaler": 600, // Scales deployments/statefulsets based on metrics
		"policy/v1/PodDisruptionBudget":          610, // Limits disruptions during maintenance
	}

	if priority, exists := priorityMap[resourceType]; exists {
		return priority
	}
	// Default priority for unknown resource types
	return 1000
}

// sortResourcesWithDependencies sorts resources in the doc using dependency graph and priority
func sortResourcesWithDependencies(docs []resourceDoc) ([]resourceDoc, error) {
	if len(docs) == 0 {
		return docs, nil
	}

	// Create dependency graph
	graph := newDependencyGraph()

	// Add all documents as vertices
	for i := range docs {
		key := getResourceKey(docs[i])
		graph.addVertex(key, &docs[i])
	}

	// Add implicit dependencies
	graph.addNamespaceEdges(docs)
	// skip CRDs edge compute since CRDs will be in a separate unit
	// graph.addCRDEdges(docs)

	// Perform topological sort
	sorted, err := graph.topologicalSort()
	if err != nil {
		// Fall back to simple priority-based sorting if there are circular dependencies
		return sortResourcesByPriority(docs), nil
	}

	// Additional sorting within the topologically sorted order by priority and name
	// Group by priority and sort within groups
	return stabilizeSortByPriority(sorted), nil
}

// sortResourcesByPriority sorts resources by priority only (fallback method)
func sortResourcesByPriority(docs []resourceDoc) []resourceDoc {
	result := make([]resourceDoc, len(docs))
	copy(result, docs)

	sort.SliceStable(result, func(i, j int) bool {
		// First, sort by kind priority
		priorityi := getResourcePriority(result[i].resourceType)
		priorityj := getResourcePriority(result[j].resourceType)

		if priorityi != priorityj {
			return priorityi < priorityj
		}

		// If same kind, sort by namespace (cluster-scoped resources come first)
		nsi := GetNamespaceFromResourceName(result[i].resourceName)
		nsj := GetNamespaceFromResourceName(result[j].resourceName)
		namei := GetNameFromResourceName(result[i].resourceName)
		namej := GetNameFromResourceName(result[j].resourceName)

		return compareResourcesByNamespaceAndName(namei, nsi, namej, nsj)
	})

	return result
}

// stabilizeSortByPriority maintains topological order but stabilizes it with priority and name
func stabilizeSortByPriority(docs []resourceDoc) []resourceDoc {
	// Group consecutive items with the same priority
	groups := make([][]resourceDoc, 0)
	var currentGroup []resourceDoc
	var lastPriority = -1

	for _, doc := range docs {
		priority := getResourcePriority(doc.resourceType)
		if priority != lastPriority && len(currentGroup) > 0 {
			groups = append(groups, currentGroup)
			currentGroup = []resourceDoc{}
		}
		currentGroup = append(currentGroup, doc)
		lastPriority = priority
	}
	if len(currentGroup) > 0 {
		groups = append(groups, currentGroup)
	}

	// Sort within each group by namespace and name
	result := make([]resourceDoc, 0, len(docs))
	for _, group := range groups {
		sort.SliceStable(group, func(i, j int) bool {
			nsi := GetNamespaceFromResourceName(group[i].resourceName)
			nsj := GetNamespaceFromResourceName(group[j].resourceName)
			namei := GetNameFromResourceName(group[i].resourceName)
			namej := GetNameFromResourceName(group[j].resourceName)

			return compareResourcesByNamespaceAndName(namei, nsi, namej, nsj)
		})
		result = append(result, group...)
	}

	return result
}

// compareResourcesByNamespaceAndName compares two resources by namespace and name
// Returns true if the first resource should come before the second
// Cluster-scoped resources (empty namespace) come first
func compareResourcesByNamespaceAndName(nameA, namespaceA, nameB, namespaceB string) bool {
	if namespaceA != namespaceB {
		// Empty namespace (cluster-scoped) comes first
		if namespaceA == "" {
			return true
		}
		if namespaceB == "" {
			return false
		}
		// Otherwise sort by namespace name
		return namespaceA < namespaceB
	}
	// Finally, sort by name
	return nameA < nameB
}
