// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

// Package k8skit is uesd to interpret Kubernetes/YAML configuration units.
package k8skit

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/confighub/sdk/core/configkit/yamlkit"
	"github.com/confighub/sdk/core/constants"
	"github.com/confighub/sdk/core/function/api"
	"github.com/confighub/sdk/core/third_party/gaby"
	"github.com/confighub/sdk/core/workerapi"
	"github.com/gosimple/slug"
	"golang.org/x/text/cases"
	"golang.org/x/text/language"
)

// LabelManagedBy is the standard Kubernetes label for identifying the managing tool.
const LabelManagedBy = "app.kubernetes.io/managed-by"

// Kubernetes annotation prefixes that should be removed during cleanup
// Cannot import directly from k8s.io/pkg/apis/core because it is private
// See https://kubernetes.io/docs/reference/labels-annotations-taints/
// for more information.
const (
	// Kubectl client annotations
	KubectlPrefix = "kubectl.kubernetes.io/"
	// Kubernetes core annotations
	KubernetesPrefix = "kubernetes.io/"
	// Deployment controller annotations
	DeploymentPrefix = "deployment.kubernetes.io/"
	// Persistent volume annotations
	PVPrefix = "pv.kubernetes.io/"
	// Volume beta annotations (legacy)
	VolumeBetaPrefix = "volume.beta.kubernetes.io/"
	// Endpoints annotations
	EndpointsPrefix = "endpoints.kubernetes.io/"
	// Control plane alpha annotations
	ControlPlaneAlphaPrefix = "control-plane.alpha.kubernetes.io/"
	// Scheduler alpha annotations
	SchedulerAlphaPrefix = "scheduler.alpha.kubernetes.io/"
	// Batch annotations
	BatchPrefix = "batch.kubernetes.io/"
	// ArgoCD annotations (tracking-id, refresh, hydrate, etc.)
	// ArgoCD adds tracking-id to every resource it manages at sync time.
	// These are not part of the user's intended config and must be stripped
	// from LiveData to avoid false drift during Refresh.
	ArgoCDPrefix = "argocd.argoproj.io/"

	// Specific annotation keys that should be removed
	// Kubectl specific annotations
	KubectlLastAppliedConfiguration = "kubectl.kubernetes.io/last-applied-configuration"
	KubectlRestartedAt              = "kubectl.kubernetes.io/restartedAt"
	// Kubernetes core annotations
	KubernetesChangeCause = "kubernetes.io/change-cause"
	KubernetesLimitRanger = "kubernetes.io/limit-ranger"
	KubernetesPSP         = "kubernetes.io/psp"
	// Static pod annotations
	KubernetesConfigHash   = "kubernetes.io/config.hash"
	KubernetesConfigMirror = "kubernetes.io/config.mirror"
	KubernetesConfigSource = "kubernetes.io/config.source"
	KubernetesConfigSeen   = "kubernetes.io/config.seen"
	// CLI Utils applier inventory
	KubernetesCLIUtilsInventory = "config.k8s.io/owning-inventory"

	// Kubernetes label prefixes that should be removed during cleanup
	// Controller-generated labels
	ControllerUIDPrefix      = "controller-uid"
	PodTemplateHashPrefix    = "pod-template-hash"
	StatefulSetPodNamePrefix = "statefulset.kubernetes.io/pod-name"
	// Flux Kustomize controller labels added at reconcile time.
	// These track which Kustomization owns each resource and are not part of
	// the user's intended config — must be stripped from LiveData to avoid false drift.
	FluxKustomizePrefix = "kustomize.toolkit.fluxcd.io/"
)

var K8sInternalAnnotationPrefixes = []string{
	ContextKeyPrefix, // ConfigHub
	KubectlPrefix,
	KubernetesPrefix,
	DeploymentPrefix,
	PVPrefix,
	VolumeBetaPrefix,
	EndpointsPrefix,
	ControlPlaneAlphaPrefix,
	SchedulerAlphaPrefix,
	BatchPrefix,
	ArgoCDPrefix,
}

var K8sInternalAnnotationKeys = []string{
	KubernetesChangeCause,
	KubectlLastAppliedConfiguration,
	KubectlRestartedAt,
	KubernetesLimitRanger,
	KubernetesPSP,
	KubernetesConfigHash,
	KubernetesConfigMirror,
	KubernetesConfigSource,
	KubernetesConfigSeen,
	KubernetesCLIUtilsInventory,
}

var K8sInternalLabelPrefixes = []string{
	ControllerUIDPrefix,
	PodTemplateHashPrefix,
	StatefulSetPodNamePrefix,
	FluxKustomizePrefix,
}

// User data errors should not be logged here. They will be logged by the caller.
// Errors indicate that the operation could not be completed.
// Messages should be acceptable to return to the user, and should indicate the
// location of the problem in the configuration data.

// K8sResourceProviderType must be built with NewK8sResourceProvider. The embedded registry
// carries both the path registry a provider registers into and the compiled structure its
// merge-key, exclusive-field and map-key lookups read, and a zero value has neither -- so one
// would answer "no merge keys" for every path, silently, which is the failure this package
// exists to stop.
type K8sResourceProviderType struct {
	yamlkit.ResourceProviderRegistry
}

// NewK8sResourceProvider creates a new K8sResourceProviderType with its own path registry and
// the built-in resource-type specs as its structure.
func NewK8sResourceProvider() *K8sResourceProviderType {
	return &K8sResourceProviderType{
		ResourceProviderRegistry: yamlkit.NewResourceProviderRegistryWithSpecs(
			workerapi.ToolchainKubernetesYAML, compiledK8sSpecs),
	}
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

func (rp *K8sResourceProviderType) ResourceNameStableCoreGetter(doc *gaby.YamlDoc) (api.ResourceName, error) {
	resourceNameStableCorePath := K8sContextPath(constants.ResourceNameStableCoreKeySuffix)
	name, found, err := yamlkit.YamlSafePathGetValue[string](doc, api.ResolvedPath(resourceNameStableCorePath), true)
	if err != nil || !found {
		return "", err
	}
	return api.ResourceName(name), nil
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
	// Check structurally similar types: two types are similar when both declare the same
	// similarity class, which is what the four hand-kept sets said one set at a time.
	class := SimilarityClassOf(resourceTypeA)
	return class != "" && class == SimilarityClassOf(resourceTypeB)
}

// ResourceAndCategoryTypeMaps returns maps of all resources in the provided list of parsed YAML
// documents, from from names to categories+types and categories+types to names.
func (rp *K8sResourceProviderType) ResourceAndCategoryTypeMaps(docs gaby.Container) (resourceMap yamlkit.ResourceNameToCategoryTypesMap, categoryTypeMap yamlkit.ResourceCategoryTypeToNamesMap, err error) {
	return yamlkit.ResourceAndCategoryTypeMaps(docs, rp)
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

// K8sNormalizeName converts a string to one limited to the DNS1123 character set.
func K8sNormalizeName(s string) string {
	s = cases.Title(language.Und, cases.NoLower).String(s)
	s = strings.ReplaceAll(s, "_", nameSeparatorString)
	s = strings.ToLower(slug.Make(s))
	return s
}

func (*K8sResourceProviderType) NormalizeName(name string) string {
	return K8sNormalizeName(name)
}

func (*K8sResourceProviderType) NameSeparator() string {
	return nameSeparatorString
}

const (
	ContextKeyPrefix = "confighub.com/"
	ContextPathPrefx = ".metadata.annotations."

	SpaceIDAnnotation     = ContextKeyPrefix + constants.SpaceIDKeySuffix
	UnitSlugAnnotation    = ContextKeyPrefix + constants.UnitSlugKeySuffix
	RevisionNumAnnotation = ContextKeyPrefix + constants.RevisionNumKeySuffix

	// OriginAnnotation carries the combined ConfigHub source identity of a resource
	// as a JSON Origin value. It supersedes the discrete SpaceID/UnitSlug/RevisionNum
	// annotations for resources bundled in a published Space release.
	OriginAnnotation = ContextKeyPrefix + "origin"
)

// Origin identifies the ConfigHub source of a resource bundled in a published
// Space release. It is stamped as the JSON value of the confighub.com/origin
// annotation on every Kubernetes resource in the bundle, giving a per-resource
// reverse lookup back to the owning Space, Unit, and Revision. Both id and slug
// are carried for Space and Unit: ids are stable and drive UI deep-links, slugs
// are human-readable and stable across variant clones. Release identity is
// deliberately absent — it lives on the OCI manifest, not in the content.
type Origin struct {
	SpaceID     string `json:"spaceId"`
	SpaceSlug   string `json:"spaceSlug"`
	UnitID      string `json:"unitId"`
	UnitSlug    string `json:"unitSlug"`
	RevisionNum int64  `json:"revisionNum"`
}

// EnsureConfigHubContextOnData sets ConfigHub context annotations (UnitSlug, SpaceID,
// RevisionNum) on every resource in the given multi-document YAML data. Uses gaby's
// in-place SetP to preserve comments and key ordering. If the data is empty it is
// returned unchanged. If parsing fails, the original data is returned with the error.
// See also kubernetes.EnsureConfigHubContext in bridge-impl for a variant that
// operates on parsed *unstructured.Unstructured objects.
func EnsureConfigHubContextOnData(data []byte, unitSlug, spaceID string, revisionNum int64) ([]byte, error) {
	if len(data) == 0 {
		return data, nil
	}

	container, err := gaby.ParseAll(data)
	if err != nil {
		return data, fmt.Errorf("failed to parse YAML for annotation injection: %w", err)
	}
	if len(container) == 0 {
		return data, nil
	}

	// NOTE: SetP modifies docs in-place. If a SetP call fails mid-loop, earlier docs
	// will have been mutated but we return the original data bytes. The in-memory
	// container state becomes inconsistent, but since we discard it on error this is
	// safe. In practice SetP on annotation paths should not fail.
	// TODO: If this becomes a concern, deep-copy the container before mutation or
	// collect per-doc errors and return partial results.
	revStr := strconv.FormatInt(revisionNum, 10)
	for _, doc := range container {
		if _, err := doc.SetP(unitSlug, K8sContextPath(constants.UnitSlugKeySuffix)); err != nil {
			return data, fmt.Errorf("failed to set UnitSlug annotation: %w", err)
		}
		if _, err := doc.SetP(spaceID, K8sContextPath(constants.SpaceIDKeySuffix)); err != nil {
			return data, fmt.Errorf("failed to set SpaceID annotation: %w", err)
		}
		if _, err := doc.SetP(revStr, K8sContextPath(constants.RevisionNumKeySuffix)); err != nil {
			return data, fmt.Errorf("failed to set RevisionNum annotation: %w", err)
		}
	}

	return container.Bytes(), nil
}

// EnsureOriginOnData sets the confighub.com/origin annotation, carrying the JSON
// encoding of origin, on every resource in the given multi-document YAML data.
// Like EnsureConfigHubContextOnData it uses gaby's in-place SetP to preserve
// comments and key ordering, returns empty data unchanged, and returns the
// original data with the error on failure. Callers should only invoke this for
// Kubernetes/YAML units; other toolchains have no metadata.annotations to stamp.
func EnsureOriginOnData(data []byte, origin Origin) ([]byte, error) {
	if len(data) == 0 {
		return data, nil
	}

	encoded, err := json.Marshal(origin)
	if err != nil {
		return data, fmt.Errorf("failed to marshal origin annotation: %w", err)
	}

	container, err := gaby.ParseAll(data)
	if err != nil {
		return data, fmt.Errorf("failed to parse YAML for annotation injection: %w", err)
	}
	if len(container) == 0 {
		return data, nil
	}

	// See the mutation-safety note on EnsureConfigHubContextOnData: SetP mutates
	// in-place, but we discard the container on error so the returned bytes are
	// always the untouched original.
	path := K8sContextPath("origin")
	for _, doc := range container {
		if _, err := doc.SetP(string(encoded), path); err != nil {
			return data, fmt.Errorf("failed to set origin annotation: %w", err)
		}
	}

	return container.Bytes(), nil
}

func K8sContextPath(contextField string) string {
	// PascalCase is expected for contextField
	safeKey := yamlkit.EscapeDotsInPathSegment(ContextKeyPrefix + contextField)
	return ContextPathPrefx + safeKey
}

func (*K8sResourceProviderType) ContextPath(contextField string) string {
	return K8sContextPath(contextField)
}

// NativeToYAML converts native YAML comments (HeadComment, LineComment, FootComment)
// into $comment$ map keys so they have a uniform representation across all formats.
func (*K8sResourceProviderType) NativeToYAML(data []byte) ([]byte, error) {
	if len(data) == 0 {
		return data, nil
	}
	docs, err := gaby.ParseAll(data)
	if err != nil {
		return data, nil
	}
	for _, doc := range docs {
		doc.ExtractCommentsToKeys()
	}
	return docs.Bytes(), nil
}

// YAMLToNative converts $comment$ map keys back into native YAML comments.
func (*K8sResourceProviderType) YAMLToNative(yamlData []byte) ([]byte, error) {
	if len(yamlData) == 0 {
		return yamlData, nil
	}
	docs, err := gaby.ParseAll(yamlData)
	if err != nil {
		return yamlData, nil
	}
	for _, doc := range docs {
		doc.InjectCommentsFromKeys()
	}
	return docs.Bytes(), nil
}

func (*K8sResourceProviderType) DataType() api.DataType {
	return api.DataTypeYAML
}

func (*K8sResourceProviderType) GetToolchainType() workerapi.ToolchainType {
	return workerapi.ToolchainKubernetesYAML
}
