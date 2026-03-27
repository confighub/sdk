// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package k8skit

import "github.com/confighub/sdk/core/function/api"

// ResourceTypeToPodSpecPaths maps Kubernetes resource types to the dot-separated
// paths where their PodSpec lives.
var ResourceTypeToPodSpecPaths = map[api.ResourceType][]string{
	api.ResourceType("apps/v1/Deployment"):           {"spec.template.spec"},
	api.ResourceType("apps/v1/ReplicaSet"):           {"spec.template.spec"},
	api.ResourceType("apps/v1/DaemonSet"):            {"spec.template.spec"},
	api.ResourceType("apps/v1/StatefulSet"):          {"spec.template.spec"},
	api.ResourceType("batch/v1/Job"):                 {"spec.template.spec"},
	api.ResourceType("batch/v1/CronJob"):             {"spec.jobTemplate.spec.template.spec"},
	api.ResourceType("v1/Pod"):                       {"spec"},
	api.ResourceType("argoproj.io/v1alpha1/Rollout"): {"spec.template.spec"},
}

// ContainersPaths lists the relative paths under a PodSpec where containers
// can be found.
var ContainersPaths = []string{"containers", "initContainers", "ephemeralContainers"}

// ResourceTypeToContainersPaths maps Kubernetes resource types to the full
// dot-separated paths where container lists appear.
var ResourceTypeToContainersPaths = map[api.ResourceType][]string{
	api.ResourceType("apps/v1/Deployment"):           {"spec.template.spec.containers", "spec.template.spec.initContainers", "spec.template.spec.ephemeralContainers"},
	api.ResourceType("apps/v1/ReplicaSet"):           {"spec.template.spec.containers", "spec.template.spec.initContainers", "spec.template.spec.ephemeralContainers"},
	api.ResourceType("apps/v1/DaemonSet"):            {"spec.template.spec.containers", "spec.template.spec.initContainers", "spec.template.spec.ephemeralContainers"},
	api.ResourceType("apps/v1/StatefulSet"):          {"spec.template.spec.containers", "spec.template.spec.initContainers", "spec.template.spec.ephemeralContainers"},
	api.ResourceType("batch/v1/Job"):                 {"spec.template.spec.containers", "spec.template.spec.initContainers", "spec.template.spec.ephemeralContainers"},
	api.ResourceType("batch/v1/CronJob"):             {"spec.jobTemplate.spec.template.spec.containers", "spec.jobTemplate.spec.template.spec.initContainers", "spec.jobTemplate.spec.template.spec.ephemeralContainers"},
	api.ResourceType("v1/Pod"):                       {"spec.containers", "spec.initContainers", "spec.ephemeralContainers"},
	api.ResourceType("argoproj.io/v1alpha1/Rollout"): {"spec.template.spec.containers", "spec.template.spec.initContainers", "spec.template.spec.ephemeralContainers"},
}

// ReplicatedControllerResourceTypes lists resource types that manage replicated pods.
var ReplicatedControllerResourceTypes = []api.ResourceType{
	api.ResourceType("apps/v1/Deployment"),
	api.ResourceType("apps/v1/ReplicaSet"),
	api.ResourceType("apps/v1/StatefulSet"),
	api.ResourceType("argoproj.io/v1alpha1/Rollout"),
}
