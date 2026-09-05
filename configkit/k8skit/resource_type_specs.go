// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package k8skit

import (
	_ "embed"

	"github.com/confighub/sdk/core/configkit/yamlkit"
	"github.com/confighub/sdk/core/function/api"
	"github.com/confighub/sdk/core/workerapi"
)

// The built-in Kubernetes resource-type specs are data, not Go: resource_type_specs.yaml is
// the whole declaration, and this file only loads it. The same format loads from a file given
// to a server or worker, and is the format a stored spec Unit is meant to hold, so a type
// registered without a ConfigHub release is registered exactly the way the built-ins are.
// See docs/design/resource-type-specs.md.

//go:embed resource_type_specs.yaml
var builtinSpecSetYAML []byte

// BuiltinSpecSet returns the built-in Kubernetes resource-type specs, parsed. Callers that
// want to add types compile it alongside their own sets rather than mutating it; the returned
// value is a fresh parse, so a caller cannot disturb the compiled built-ins.
func BuiltinSpecSet() (yamlkit.SpecSet, error) {
	return yamlkit.LoadSpecSet(builtinSpecSetYAML)
}

// compiledK8sSpecs holds the structure lookups the merge engine reads, compiled once at init.
var compiledK8sSpecs = mustCompileBuiltinSpecs()

func mustCompileBuiltinSpecs() *yamlkit.CompiledSpecs {
	set, err := BuiltinSpecSet()
	if err != nil {
		// The specs are embedded in this package, so a failure here is a bug in the file
		// beside this one rather than anything a caller did.
		panic("k8skit: parsing built-in resource type specs: " + err.Error())
	}
	compiled, err := yamlkit.CompileSpecSets(set)
	if err != nil {
		panic("k8skit: compiling built-in resource type specs: " + err.Error())
	}
	return compiled
}

// AttributeNameImmutable is the attribute name declaring the fields that are immutable after
// creation. Changing one requires deleting and recreating the resource rather than an in-place
// update, which is what vet-immutable reports.
const AttributeNameImmutable = api.AttributeName("immutable")

// Shape names the built-in specs embed, for the accessors below.
const (
	ShapePodSpec     = "PodSpec"
	ShapePodTemplate = "PodTemplate"
	ShapeContainers  = "Containers"
)

// PodSpecPaths returns where a resource type carries a PodSpec, or nil if it carries none.
// It replaces the table that listed the same thing, and answers for every type that embeds the
// shape rather than for the ones somebody remembered to add.
func PodSpecPaths(resourceType api.ResourceType) []string {
	return compiledK8sSpecs.ShapePaths(workerapi.ToolchainKubernetesYAML, resourceType, ShapePodSpec)
}

// PodTemplatePaths returns where a resource type carries a pod template. A v1/Pod carries none:
// it is the pod, so its own metadata is its pod metadata, and callers wanting pod metadata should
// treat the empty result as the resource root rather than as an absence.
func PodTemplatePaths(resourceType api.ResourceType) []string {
	return compiledK8sSpecs.ShapePaths(workerapi.ToolchainKubernetesYAML, resourceType, ShapePodTemplate)
}

// ContainerArrayPaths returns a resource type's container arrays -- containers, initContainers
// and ephemeralContainers under each PodSpec it carries -- or nil if it has none.
func ContainerArrayPaths(resourceType api.ResourceType) []string {
	return compiledK8sSpecs.ShapePaths(workerapi.ToolchainKubernetesYAML, resourceType, ShapeContainers)
}

// PodSpecResourceTypes returns every resource type carrying a PodSpec, sorted.
func PodSpecResourceTypes() []api.ResourceType {
	return compiledK8sSpecs.ResourceTypesEmbeddingShape(workerapi.ToolchainKubernetesYAML, ShapePodSpec)
}

// ContainerResourceTypes returns every resource type carrying containers, sorted.
func ContainerResourceTypes() []api.ResourceType {
	return compiledK8sSpecs.ResourceTypesEmbeddingShape(workerapi.ToolchainKubernetesYAML, ShapeContainers)
}

// DeclaredAttributePaths returns the paths the built-in specs declare for one attribute,
// keyed by resource type. It is for the functions that read an attribute's paths without going
// through the path registry; anything registered is better read from the registry.
func DeclaredAttributePaths(attributeName api.AttributeName) map[api.ResourceType][]string {
	paths := make(map[api.ResourceType][]string)
	for _, resourceType := range compiledK8sSpecs.ResourceTypesWithAttributes(workerapi.ToolchainKubernetesYAML) {
		declared := compiledK8sSpecs.DeclaredAttributes(workerapi.ToolchainKubernetesYAML, resourceType)[attributeName]
		for _, attributePath := range declared {
			paths[resourceType] = append(paths[resourceType], attributePath.Path)
		}
	}
	return paths
}

// DeclaredReferences returns every path the built-in specs declare as naming another resource,
// with the type it names, sorted. It is the CRD half of the references ConfigHub knows: the
// other half is kustomize's NameReferenceFieldSpecs, which is a third-party table parsed at
// registration rather than something declared here.
func DeclaredReferences() []yamlkit.DeclaredReference {
	return compiledK8sSpecs.ReferencePaths(workerapi.ToolchainKubernetesYAML)
}

// SchemaLocations returns where a Kubernetes resource type's JSON schema is fetched from, most
// authoritative first, as the templates kubeconform reads. Declared in the spec file rather than
// written into the function, so adding a catalog is one line of data.
func SchemaLocations() []string {
	return compiledK8sSpecs.SchemaLocationsFor(workerapi.ToolchainKubernetesYAML)
}

// SchemaFor returns what a resource type declares about its own schema: a location that
// overrides the templates, yamlkit.SchemaNone for a type known to have none, or "" for the
// ordinary case.
func SchemaFor(resourceType api.ResourceType) string {
	return compiledK8sSpecs.SchemaFor(workerapi.ToolchainKubernetesYAML, resourceType)
}

// ScopeOf returns the scope the built-in specs declare for a resource type, or "" for a type
// they say nothing about.
func ScopeOf(resourceType api.ResourceType) yamlkit.Scope {
	return compiledK8sSpecs.ScopeOf(workerapi.ToolchainKubernetesYAML, resourceType)
}

// SimilarityClassOf returns the similarity class the built-in specs declare for a resource type,
// or "" for a type they place in none.
func SimilarityClassOf(resourceType api.ResourceType) string {
	return compiledK8sSpecs.SimilarityClassOf(workerapi.ToolchainKubernetesYAML, resourceType)
}

// ClusterScopedResourceTypes returns every type the built-in specs declare cluster-scoped,
// sorted. It is what the curated map used to be, and it is still not exhaustive: see
// IsResourceTypeClusterScoped for the Crossplane rule that covers what a list cannot.
func ClusterScopedResourceTypes() []api.ResourceType {
	return compiledK8sSpecs.ResourceTypesWithScope(workerapi.ToolchainKubernetesYAML, yamlkit.ScopeCluster)
}

// ClusterScopedResourceTypeSet returns the cluster-scoped types as a set, for a caller that
// needs to test membership rather than iterate -- a path visitor's TypeExceptions, say.
func ClusterScopedResourceTypeSet() map[api.ResourceType]struct{} {
	types := ClusterScopedResourceTypes()
	set := make(map[api.ResourceType]struct{}, len(types))
	for _, resourceType := range types {
		set[resourceType] = struct{}{}
	}
	return set
}

// SimilarityClassWorkload is the class of types that carry a pod spec in the same place, which is
// what lets a mutation to one be replayed against another.
const SimilarityClassWorkload = "workload"

// GetImmutablePaths returns the immutable field paths the built-in specs declare, keyed by
// resource type. vet-immutable reads them from the path registry; this is for callers that
// want the declaration itself without building a provider.
//
// The list covers commonly managed resources and is not exhaustive. A type it does not name
// is ordinary, not an error.
func GetImmutablePaths() map[api.ResourceType][]string {
	return DeclaredAttributePaths(AttributeNameImmutable)
}

const (
	// AttributeNamePodTemplateLabels is the attribute declaring where the labels that end up on
	// the pods a workload produces live. It is declared on the PodTemplate shape, so every type
	// embedding a pod template has it at the right depth.
	AttributeNamePodTemplateLabels = api.AttributeName("pod-template-labels")

	// AttributeNamePodLabelSelector is the attribute declaring where the selector that finds
	// those pods lives. Only selectors targeting a resource's own pods are declared; see the
	// attribute's description in resource_type_specs.yaml.
	AttributeNamePodLabelSelector = api.AttributeName("pod-label-selector")

	// AttributeNameWorkloadLabels is the group standing for both of the above, so
	// get/set-workload-labels read them together while they stay distinguishable when written:
	// a selector is immutable on most workloads and the template labels are not.
	AttributeNameWorkloadLabels = api.AttributeName("workload-labels")
)

// RegisterWorkloadLabelsGroup declares workload-labels as the union of the two attributes that
// carry pod labels, so a caller can read both through one name.
func RegisterWorkloadLabelsGroup(rp *K8sResourceProviderType) {
	yamlkit.RegisterAttributeGroup(rp, AttributeNameWorkloadLabels,
		AttributeNamePodTemplateLabels, AttributeNamePodLabelSelector)
}

// RegisterDeclaredAttributePaths registers the attribute paths the built-in specs declare,
// pairing each with the descriptor its attribute name is registered under. enrich adds
// per-path detail only Kubernetes can compute, such as a field's schema description.
func RegisterDeclaredAttributePaths(
	rp *K8sResourceProviderType,
	descriptors map[api.AttributeName]yamlkit.AttributeDescriptor,
	enrich yamlkit.PathEnricher,
) error {
	return yamlkit.RegisterDeclaredAttributePaths(
		rp, compiledK8sSpecs, workerapi.ToolchainKubernetesYAML, descriptors, enrich)
}
