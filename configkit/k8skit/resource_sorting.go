// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package k8skit

import (
	"github.com/confighub/sdk/core/function/api"
)

// ResourcePriority returns the canonical install-order priority for a Kubernetes
// resource type (lower applies first: CRDs, Namespaces, RBAC, config, workloads,
// then post-deployment policies). Exported so callers outside k8skit (e.g. the
// cub upload pipeline) can order resources by the same band map used internally.
func ResourcePriority(resourceType api.ResourceType) int {
	return getResourcePriority(resourceType)
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
