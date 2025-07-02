// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

// Package k8skit is uesd to interpret Kubernetes/YAML configuration units.
package k8skit

import (
	"strings"

	"github.com/confighub/sdk/configkit/yamlkit"
	"github.com/confighub/sdk/function/api"
	"github.com/confighub/sdk/third_party/gaby"
	"github.com/gosimple/slug"
	"golang.org/x/text/cases"
	"golang.org/x/text/language"
)

// User data errors should not be logged here. They will be logged by the caller.
// Errors indicate that the operation could not be completed.
// Messages should be acceptable to return to the user, and should indicate the
// location of the problem in the configuration data.

type K8sResourceProviderType struct{}

var pathRegistry = make(api.AttributeNameToResourceTypeToPathToVisitorInfoType)

func (*K8sResourceProviderType) GetPathRegistry() api.AttributeNameToResourceTypeToPathToVisitorInfoType {
	return pathRegistry
}

// K8sResourceProvider implements the ResourceProvider and ConfigConverter interfaces for Kubernetes/YAML.
var K8sResourceProvider = &K8sResourceProviderType{}

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
	// FluxCD: none
	// Trident
	api.ResourceType("trident.netapp.io/v1/TridentConfigurator"): {},
}

// K8sNamespacedResourceTypes contains all known namespaced resource types
var K8sNamespacedResourceTypes = map[api.ResourceType]struct{}{
	api.ResourceType("v1/Pod"):                                   {},
	api.ResourceType("v1/Service"):                               {},
	api.ResourceType("v1/ConfigMap"):                             {},
	api.ResourceType("v1/Secret"):                                {},
	api.ResourceType("v1/ServiceAccount"):                        {},
	api.ResourceType("apps/v1/Deployment"):                       {},
	api.ResourceType("apps/v1/StatefulSet"):                      {},
	api.ResourceType("apps/v1/DaemonSet"):                        {},
	api.ResourceType("apps/v1/ReplicaSet"):                       {},
	api.ResourceType("batch/v1/Job"):                             {},
	api.ResourceType("batch/v1/CronJob"):                         {},
	api.ResourceType("networking.k8s.io/v1/Ingress"):             {},
	api.ResourceType("rbac.authorization.k8s.io/v1/Role"):        {},
	api.ResourceType("rbac.authorization.k8s.io/v1/RoleBinding"): {},
	// Add more namespaced resource types as needed
}

// K8sWorkloadResourceTypes contains resource types that are workload resources
// These types all contain pod specs in the same location, so sometimes people may
// change the resource type.
var K8sWorkloadResourceTypes = map[api.ResourceType]struct{}{
	api.ResourceType("apps/v1/Deployment"):  {},
	api.ResourceType("apps/v1/ReplicaSet"):  {},
	api.ResourceType("apps/v1/DaemonSet"):   {},
	api.ResourceType("apps/v1/StatefulSet"): {},
	api.ResourceType("batch/v1/Job"):        {},
	api.ResourceType("batch/v1/CronJob"):    {},
}

// K8sConfigResourceTypes contains resource types that store configuration
var K8sConfigResourceTypes = map[api.ResourceType]struct{}{
	api.ResourceType("v1/ConfigMap"): {},
	api.ResourceType("v1/Secret"):    {},
}

// K8sRoleResourceTypes contains resource types related to RBAC roles
var K8sRoleResourceTypes = map[api.ResourceType]struct{}{
	api.ResourceType("rbac.authorization.k8s.io/v1/Role"):        {},
	api.ResourceType("rbac.authorization.k8s.io/v1/ClusterRole"): {},
}

// K8sRoleBindingResourceTypes contains resource types related to RBAC role bindings
var K8sRoleBindingResourceTypes = map[api.ResourceType]struct{}{
	api.ResourceType("rbac.authorization.k8s.io/v1/RoleBinding"):        {},
	api.ResourceType("rbac.authorization.k8s.io/v1/ClusterRoleBinding"): {},
}

// areBothInTypeSet checks if both resource types are in the given type set
func areBothInTypeSet(resourceTypeA, resourceTypeB api.ResourceType, typeSet map[api.ResourceType]struct{}) bool {
	_, aIsInSet := typeSet[resourceTypeA]
	_, bIsInSet := typeSet[resourceTypeB]
	return aIsInSet && bIsInSet
}

// DefaultResourceCategory returns the default resource category to asssume, which is Resource in this case.
func (*K8sResourceProviderType) DefaultResourceCategory() api.ResourceCategory {
	return api.ResourceCategoryResource
}

// ResourceCategoryGetter just returns ResourceCategoryResource for Kubernetes documents.
func (*K8sResourceProviderType) ResourceCategoryGetter(doc *gaby.YamlDoc) (api.ResourceCategory, error) {
	// TODO: check that the document is non-empty?
	return api.ResourceCategoryResource, nil
}

// ResourceTypeGetter extracts the apiVersion and kind from the Kubernetes resource in
// the provided parsed YAML document and returns an api.ResourceType string of the form
// <apiVersion>/<kind>, including in the case of core APIs, which have no explicit Kubernetes
// resource group.
func (*K8sResourceProviderType) ResourceTypeGetter(doc *gaby.YamlDoc) (api.ResourceType, error) {
	apiVersion, _, err := yamlkit.YamlSafePathGetValue[string](doc, api.ResolvedPath("apiVersion"), false)
	if err != nil {
		return "", err
	}
	kind, _, err := yamlkit.YamlSafePathGetValue[string](doc, api.ResolvedPath("kind"), false)
	if err != nil {
		return "", err
	}
	return api.ResourceType(apiVersion + "/" + kind), nil
}

// ResourceNameGetter extracts the namespace and name from the Kubernetes resource in the
// provided parsed YAML document and resturns an api.ResourceName string of the form
// <namespace>/<name>, including in the cases of a namespace-scoped or cluster-scoped resource
// with no namespace.
func (*K8sResourceProviderType) ResourceNameGetter(doc *gaby.YamlDoc) (api.ResourceName, error) {
	// The namespace might not be present, in which case this will return an empty string.
	namespace, _, err := yamlkit.YamlSafePathGetValue[string](doc, api.ResolvedPath("metadata.namespace"), true)
	if err != nil {
		return "", err
	}
	name, _, err := yamlkit.YamlSafePathGetValue[string](doc, api.ResolvedPath("metadata.name"), false)
	if err != nil {
		return "", err
	}
	return api.ResourceName(namespace + "/" + name), nil
}

const scopelessResourceNamePath = "metadata.name"

func (*K8sResourceProviderType) ScopelessResourceNamePath() api.ResolvedPath {
	return api.ResolvedPath(scopelessResourceNamePath)
}

// The current ResourceName representation for K8s is namespace/name. The namespace
// is likely to change across clones and may otherwise initially not be
// present or a placeholder, so in some cases we want to ignore it for matching under the assumption
// that having multiple resources of the same type with the name name in the
// same unit would be confusing anyway. We should discourage mutating names, except when
// cloning "scope" resources like Kubernetes namespaces.
// In declarative systems, names represent identity and purpose. Terraform,
// Helm, and other IaC tools will delete and re-create the resource if the
// name changes.
//
// For cases where names must be mutated for uniqueness reasons, we could
// support a convention so that we could match on prefix, suffix, or "kernel".
// Eventually we could also implement a similarity analysis to attempt to
// match sufficiently similar resources.
func (*K8sResourceProviderType) RemoveScopeFromResourceName(resourceName api.ResourceName) api.ResourceName {
	_, justResourceName, _ := strings.Cut(string(resourceName), "/")
	return api.ResourceName(justResourceName)
}

func (*K8sResourceProviderType) SetResourceName(doc *gaby.YamlDoc, name string) error {
	_, err := doc.SetP(name, string(scopelessResourceNamePath))
	return err
}

func (*K8sResourceProviderType) ResourceTypesAreSimilar(resourceTypeA, resourceTypeB api.ResourceType) bool {
	if resourceTypeA == resourceTypeB {
		return true
	}
	// Strip groups and versions to check same type, different versions
	resourceTypeAString := string(resourceTypeA)
	resourceTypeASegments := strings.Split(resourceTypeAString, "/")
	resourceTypeBString := string(resourceTypeB)
	resourceTypeBSegments := strings.Split(resourceTypeBString, "/")
	if resourceTypeASegments[len(resourceTypeASegments)-1] == resourceTypeBSegments[len(resourceTypeBSegments)-1] {
		return true
	}
	// Check structurally similar types
	return areBothInTypeSet(resourceTypeA, resourceTypeB, K8sWorkloadResourceTypes) ||
		areBothInTypeSet(resourceTypeA, resourceTypeB, K8sConfigResourceTypes) ||
		areBothInTypeSet(resourceTypeA, resourceTypeB, K8sRoleResourceTypes) ||
		areBothInTypeSet(resourceTypeA, resourceTypeB, K8sRoleBindingResourceTypes)
}

// ResourceAndCategoryTypeMaps returns maps of all resources in the provided list of parsed YAML
// documents, from from names to categories+types and categories+types to names.
func (*K8sResourceProviderType) ResourceAndCategoryTypeMaps(docs gaby.Container) (resourceMap yamlkit.ResourceNameToCategoryTypesMap, categoryTypeMap yamlkit.ResourceCategoryTypeToNamesMap, err error) {
	return yamlkit.ResourceAndCategoryTypeMaps(docs, K8sResourceProvider)
}

func (*K8sResourceProviderType) TypeDescription() string {
	return "[Group/]Version/Kind"
}

// IsDNSLabelRune reports whether a character is in the DNS1123 label character set.
func IsDNSLabelRune(c rune) bool {
	// Check if the byte value falls within the range of DNS label characters
	return (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '-'
}

const nameSeparatorString = "-"

// ToDNSLabel converts a string to one limited to the DNS1123 character set.
func ToDNSLabel(s string) string {
	s = cases.Title(language.Und, cases.NoLower).String(s)
	s = strings.ReplaceAll(s, "_", nameSeparatorString)
	s = strings.ToLower(slug.Make(s))
	return s
}

func (*K8sResourceProviderType) NormalizeName(name string) string {
	return ToDNSLabel(name)
}

func (*K8sResourceProviderType) NameSeparator() string {
	return nameSeparatorString
}

const (
	contextKeyPrefix = "confighub.com/"
	contextPathPrefx = ".metadata.annotations."
)

func (*K8sResourceProviderType) ContextPath(contextField string) string {
	// PascalCase is expected for contextField
	safeKey := yamlkit.EscapeDotsInPathSegment(contextKeyPrefix + contextField)
	return contextPathPrefx + safeKey
}

// The conversions are no-ops since Kubernetes/YAML is already YAML.

func (*K8sResourceProviderType) NativeToYAML(data []byte) ([]byte, error) {
	// TODO: deep copy?
	return data, nil
}

func (*K8sResourceProviderType) YAMLToNative(yamlData []byte) ([]byte, error) {
	// TODO: deep copy?
	return yamlData, nil
}

func (*K8sResourceProviderType) DataType() api.DataType {
	return api.DataTypeYAML
}
