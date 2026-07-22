// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package k8skit

import (
	"strings"

	"github.com/confighub/sdk/core/function/api"
)

// kustomize keeps a list of namespaced resource types, which we may want to consider using:
// https://github.com/kubernetes-sigs/kustomize/blob/65567a37331715052d98e9b538d6bdb5089da8cc/kyaml/openapi/openapi.go#L94

// TODO: Make it possible to update this list dynamically

var K8sClusterScopedResourceTypes = map[api.ResourceType]struct{}{
	api.ResourceType("v1/Namespace"):                                                   {},
	api.ResourceType("rbac.authorization.k8s.io/v1/ClusterRole"):                       {},
	api.ResourceType("rbac.authorization.k8s.io/v1/ClusterRoleBinding"):                {},
	api.ResourceType("apiextensions.k8s.io/v1/CustomResourceDefinition"):               {},
	api.ResourceType("admissionregistration.k8s.io/v1/MutatingWebhookConfiguration"):   {},
	api.ResourceType("admissionregistration.k8s.io/v1/ValidatingWebhookConfiguration"): {},
	api.ResourceType("apiregistration.k8s.io/v1/APIService"):                           {},
	api.ResourceType("storage.k8s.io/v1/StorageClass"):                                 {},
	api.ResourceType("storage.k8s.io/v1/CSIDriver"):                                    {},
	api.ResourceType("networking.k8s.io/v1/IngressClass"):                              {},
	// Traefik
	api.ResourceType("gateway.networking.k8s.io/v1/GatewayClass"):       {},
	api.ResourceType("gateway.networking.k8s.io/v1beta1/GatewayClass"):  {},
	api.ResourceType("gateway.networking.k8s.io/v1alpha1/GatewayClass"): {},
	api.ResourceType("hub.traefik.io/v1/AccessControlPolicy"):           {},
	api.ResourceType("hub.traefik.io/v1beta1/AccessControlPolicy"):      {},
	api.ResourceType("hub.traefik.io/v1alpha1/AccessControlPolicy"):     {},
	//  External Secrets Operator
	api.ResourceType("external-secrets.io/v1/ClusterExternalSecret"):             {},
	api.ResourceType("external-secrets.io/v1beta1/ClusterExternalSecret"):        {},
	api.ResourceType("external-secrets.io/v1alpha1/ClusterExternalSecret"):       {},
	api.ResourceType("generators.external-secrets.io/v1/ClusterGenerator"):       {},
	api.ResourceType("generators.external-secrets.io/v1beta1/ClusterGenerator"):  {},
	api.ResourceType("generators.external-secrets.io/v1alpha1/ClusterGenerator"): {},
	api.ResourceType("external-secrets.io/v1/ClusterPushSecret"):                 {},
	api.ResourceType("external-secrets.io/v1beta1/ClusterPushSecret"):            {},
	api.ResourceType("external-secrets.io/v1alpha1/ClusterPushSecret"):           {},
	api.ResourceType("external-secrets.io/v1/ClusterSecretStore"):                {},
	api.ResourceType("external-secrets.io/v1beta1/ClusterSecretStore"):           {},
	api.ResourceType("external-secrets.io/v1alpha1/ClusterSecretStore"):          {},
	// Cert Manager
	api.ResourceType("cert-manager.io/v1/ClusterIssuer"): {},
	// Argo CD
	api.ResourceType("argoproj.io/v1alpha1/AppProject"): {},
	// Argo Workflows
	api.ResourceType("argoproj.io/v1alpha1/ClusterWorkflowTemplate"): {},
	// Argo Rollouts
	api.ResourceType("argoproj.io/v1alpha1/ClusterAnalysisTemplate"): {},
	// Kyverno
	api.ResourceType("kyverno.io/v1/ClusterPolicy"):                     {},
	api.ResourceType("kyverno.io/v2beta1/ClusterPolicy"):                {},
	api.ResourceType("kyverno.io/v2/ClusterCleanupPolicy"):              {},
	api.ResourceType("kyverno.io/v2alpha1/ClusterCleanupPolicy"):        {},
	api.ResourceType("kyverno.io/v2beta1/ClusterCleanupPolicy"):         {},
	api.ResourceType("kyverno.io/v2/ClusterAdmissionReport"):            {},
	api.ResourceType("kyverno.io/v1alpha2/ClusterAdmissionReport"):      {},
	api.ResourceType("kyverno.io/v2/ClusterBackgroundScanReport"):       {},
	api.ResourceType("kyverno.io/v1alpha2/ClusterBackgroundScanReport"): {},
	// Gatekeeper
	api.ResourceType("templates.gatekeeper.sh/v1/ConstraintTemplate"):       {},
	api.ResourceType("templates.gatekeeper.sh/v1alpha1/ConstraintTemplate"): {},
	api.ResourceType("templates.gatekeeper.sh/v1beta1/ConstraintTemplate"):  {},
	api.ResourceType("mutations.gatekeeper.sh/v1/Assign"):                   {},
	api.ResourceType("mutations.gatekeeper.sh/v1alpha1/Assign"):             {},
	api.ResourceType("mutations.gatekeeper.sh/v1beta1/Assign"):              {},
	api.ResourceType("mutations.gatekeeper.sh/v1/AssignMetadata"):           {},
	api.ResourceType("mutations.gatekeeper.sh/v1alpha1/AssignMetadata"):     {},
	api.ResourceType("mutations.gatekeeper.sh/v1beta1/AssignMetadata"):      {},
	api.ResourceType("mutations.gatekeeper.sh/v1/ModifySet"):                {},
	api.ResourceType("mutations.gatekeeper.sh/v1alpha1/ModifySet"):          {},
	api.ResourceType("mutations.gatekeeper.sh/v1beta1/ModifySet"):           {},
	api.ResourceType("mutations.gatekeeper.sh/v1alpha1/AssignImage"):        {},
	// Crossplane
	api.ResourceType("apiextensions.crossplane.io/v1/CompositeResourceDefinition"): {},
	api.ResourceType("apiextensions.crossplane.io/v2/CompositeResourceDefinition"): {},
	api.ResourceType("apiextensions.crossplane.io/v1/Composition"):                 {},
	api.ResourceType("apiextensions.crossplane.io/v1/CompositionRevision"):         {},
	api.ResourceType("apiextensions.crossplane.io/v1alpha1/CompositionRevision"):   {},
	api.ResourceType("apiextensions.crossplane.io/v1beta1/CompositionRevision"):    {},
	api.ResourceType("apiextensions.crossplane.io/v1alpha1/EnvironmentConfig"):     {},
	api.ResourceType("apiextensions.crossplane.io/v1beta1/EnvironmentConfig"):      {},
	api.ResourceType("pkg.crossplane.io/v1/Provider"):                              {},
	api.ResourceType("pkg.crossplane.io/v1/ProviderRevision"):                      {},
	api.ResourceType("pkg.crossplane.io/v1/Configuration"):                         {},
	api.ResourceType("pkg.crossplane.io/v1/ConfigurationRevision"):                 {},
	api.ResourceType("pkg.crossplane.io/v1beta1/DeploymentRuntimeConfig"):          {},
	api.ResourceType("pkg.crossplane.io/v1/Function"):                              {},
	api.ResourceType("pkg.crossplane.io/v1beta1/Function"):                         {},
	api.ResourceType("pkg.crossplane.io/v1/FunctionRevision"):                      {},
	api.ResourceType("pkg.crossplane.io/v1beta1/FunctionRevision"):                 {},
	// Cilium
	api.ResourceType("cilium.io/v2/CiliumClusterwideNetworkPolicy"): {},
	api.ResourceType("cilium.io/v2/CiliumClusterwideEnvoyConfig"):   {},
	api.ResourceType("cilium.io/v2/CiliumNode"):                     {},
	api.ResourceType("cilium.io/v2/CiliumIdentity"):                 {},
	api.ResourceType("cilium.io/v2/CiliumExternalWorkload"):         {},
	api.ResourceType("cilium.io/v2/CiliumCIDRGroup"):                {},
	api.ResourceType("cilium.io/v2alpha1/CiliumCIDRGroup"):          {},
	// Snapshot storage
	api.ResourceType("snapshot.storage.k8s.io/v1/VolumeSnapshotClass"):   {},
	api.ResourceType("snapshot.storage.k8s.io/v1/VolumeSnapshotContent"): {},
	// FluxCD: none
	// Trident
	api.ResourceType("trident.netapp.io/v1/TridentConfigurator"): {},
	// AWS ACK (AWS Controllers for Kubernetes)
	api.ResourceType("services.k8s.aws/v1alpha1/IAMRoleSelector"): {},
}

// Crossplane encodes scope in the API group rather than per type, which is the only tractable way
// to classify it: a provider family ships thousands of managed resource types and adds more with
// each release, so enumerating them in K8sClusterScopedResourceTypes would dwarf the hand-curated
// map above and go stale immediately.
//
// Managed resources under *.upbound.io and *.crossplane.io are cluster-scoped, except the
// Crossplane v2 namespaced variants, which carry a ".m." infix (eks.aws.m.upbound.io). This is a
// generalization of data already in the map — every Crossplane control-plane type enumerated there
// (Composition, Provider, …) is cluster-scoped and matches the rule.
//
// It does not classify user-defined composite resources, whose API groups are arbitrary; those
// still need an explicit registration or a where-expression.
const (
	crossplaneUpboundSuffix   = ".upbound.io"
	crossplaneCoreSuffix      = ".crossplane.io"
	crossplaneNamespacedInfix = ".m."
)

// IsCrossplaneClusterScopedGroup reports whether a Crossplane API group is cluster-scoped by the
// group-suffix rule.
func IsCrossplaneClusterScopedGroup(group string) bool {
	if !strings.HasSuffix(group, crossplaneUpboundSuffix) && !strings.HasSuffix(group, crossplaneCoreSuffix) {
		return false
	}
	return !strings.Contains(group, crossplaneNamespacedInfix)
}

// IsResourceTypeClusterScoped reports whether a resource type is cluster-scoped: either it is in
// the curated K8sClusterScopedResourceTypes map, or its API group matches the Crossplane
// group-suffix rule (see IsCrossplaneClusterScopedGroup).
func IsResourceTypeClusterScoped(rt api.ResourceType) bool {
	if _, ok := K8sClusterScopedResourceTypes[rt]; ok {
		return true
	}
	return IsCrossplaneClusterScopedGroup(apiGroupOf(rt))
}

// apiGroupOf extracts the API group from a "[group/]version/Kind" resource type. A core type
// ("v1/Pod") has no group and yields "".
func apiGroupOf(rt api.ResourceType) string {
	parts := strings.Split(string(rt), "/")
	if len(parts) < 3 {
		return ""
	}
	return parts[0]
}

// IsClusterScoped returns true if the given apiVersion and kind represent a cluster-scoped resource.
// It checks against the known cluster-scoped resource types in K8sClusterScopedResourceTypes.
func IsClusterScoped(apiVersion, kind string) bool {
	resourceType := api.ResourceType(apiVersion + "/" + kind)
	return IsResourceTypeClusterScoped(resourceType)
}
