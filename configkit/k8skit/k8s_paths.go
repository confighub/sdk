// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package k8skit

import (
	"github.com/confighub/sdk/core/configkit/yamlkit"
	"github.com/confighub/sdk/core/function/api"
)

// ResourceTypeToNeededHostnamePaths maps Kubernetes resource types to the
// dot-separated paths where they carry a DNS hostname. These drive the
// get/set-hostname, get/set-hostname-subdomain, and get/set-hostname-domain
// functions. Each path must address an individual hostname scalar (string-array
// elements via a `*` wildcard are fine, e.g. spec.dnsNames.*); fields that pack
// multiple hostnames into one string (e.g. a Traefik IngressRoute match
// expression) are not handled here.
var ResourceTypeToNeededHostnamePaths = map[api.ResourceType][]string{
	api.ResourceType("networking.k8s.io/v1/Ingress"):            {"spec.rules.*.host", "spec.tls.*.hosts.*"},
	api.ResourceType("v1/Service"):                              {"metadata.annotations." + yamlkit.EscapeDotsInPathSegment("external-dns.alpha.kubernetes.io/hostname")},
	api.ResourceType("cert-manager.io/v1/Certificate"):          {"spec.commonName", "spec.dnsNames.*"},
	api.ResourceType("gateway.networking.k8s.io/v1/Gateway"):    {"spec.listeners.*.hostname"},
	api.ResourceType("gateway.networking.k8s.io/v1/HTTPRoute"):  {"spec.hostnames.*"},
	api.ResourceType("gateway.networking.k8s.io/v1/GRPCRoute"):  {"spec.hostnames.*"},
	api.ResourceType("externaldns.k8s.io/v1alpha1/DNSEndpoint"): {"spec.endpoints.*.dnsName"},
}

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

// ResourceTypeToPodTemplateLabelsPaths maps Kubernetes resource types to the
// dot-separated paths where the pod-template labels map lives (the labels that
// will be applied to pods produced by the workload). For v1/Pod, this is the
// pod's own metadata.labels.
var ResourceTypeToPodTemplateLabelsPaths = map[api.ResourceType][]string{
	api.ResourceType("apps/v1/Deployment"):           {"spec.template.metadata.labels"},
	api.ResourceType("apps/v1/ReplicaSet"):           {"spec.template.metadata.labels"},
	api.ResourceType("apps/v1/DaemonSet"):            {"spec.template.metadata.labels"},
	api.ResourceType("apps/v1/StatefulSet"):          {"spec.template.metadata.labels"},
	api.ResourceType("batch/v1/Job"):                 {"spec.template.metadata.labels"},
	api.ResourceType("batch/v1/CronJob"):             {"spec.jobTemplate.spec.template.metadata.labels"},
	api.ResourceType("v1/Pod"):                       {"metadata.labels"},
	api.ResourceType("argoproj.io/v1alpha1/Rollout"): {"spec.template.metadata.labels"},
}

// ResourceTypeToPodLabelSelectorPaths maps Kubernetes resource types to the
// dot-separated paths where the pod label-selector map lives. For
// matchLabels-style selectors this is the path of the matchLabels map; for the
// flat v1/Service selector it's spec.selector itself. Only selectors that
// target the resource's own workload pods are included — e.g. NetworkPolicy's
// spec.podSelector is here, but spec.ingress[].from[].podSelector and
// spec.egress[].to[].podSelector are not, since those reference other
// workloads.
var ResourceTypeToPodLabelSelectorPaths = map[api.ResourceType][]string{
	api.ResourceType("apps/v1/Deployment"):                  {"spec.selector.matchLabels"},
	api.ResourceType("apps/v1/ReplicaSet"):                  {"spec.selector.matchLabels"},
	api.ResourceType("apps/v1/DaemonSet"):                   {"spec.selector.matchLabels"},
	api.ResourceType("apps/v1/StatefulSet"):                 {"spec.selector.matchLabels"},
	api.ResourceType("batch/v1/Job"):                        {"spec.selector.matchLabels"},
	api.ResourceType("batch/v1/CronJob"):                    {"spec.jobTemplate.spec.selector.matchLabels"},
	api.ResourceType("argoproj.io/v1alpha1/Rollout"):        {"spec.selector.matchLabels"},
	api.ResourceType("v1/Service"):                          {"spec.selector"},
	api.ResourceType("policy/v1/PodDisruptionBudget"):       {"spec.selector.matchLabels"},
	api.ResourceType("networking.k8s.io/v1/NetworkPolicy"):  {"spec.podSelector.matchLabels"},
	api.ResourceType("monitoring.coreos.com/v1/PodMonitor"): {"spec.selector.matchLabels"},
}
