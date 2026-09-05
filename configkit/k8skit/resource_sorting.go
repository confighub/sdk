// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package k8skit

import (
	"github.com/confighub/sdk/core/function/api"
	"github.com/confighub/sdk/core/workerapi"
)

// ResourcePriority returns the canonical install-order priority for a Kubernetes
// resource type (lower applies first: CRDs, Namespaces, RBAC, config, workloads,
// then post-deployment policies). Exported so callers outside k8skit (e.g. the
// cub upload pipeline) can order resources by the same band map used internally.
func ResourcePriority(resourceType api.ResourceType) int {
	return getResourcePriority(resourceType)
}

// getResourcePriority returns the priority order for different Kubernetes resource kinds.
// Lower numbers have higher priority (will be applied first). The order is declared per type in
// resource_type_specs.yaml as applyPriority, which is where the table that used to be here went.
func getResourcePriority(resourceType api.ResourceType) int {
	if priority, declared := compiledK8sSpecs.ApplyPriorityOf(workerapi.ToolchainKubernetesYAML, resourceType); declared {
		return priority
	}
	// Default priority for a type that declares none.
	return 1000
}
