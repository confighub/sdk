// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package k8skit

import (
	"testing"

	"github.com/confighub/sdk/core/function/api"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The scope, similarity class and apply priority of a resource type are declared in
// resource_type_specs.yaml, where four hand-kept Go maps and a switch-like priority table used to
// hold them. These pin the behavior that depended on those tables rather than restating their
// contents, which the specs now are.

func TestScopeComesFromTheSpecs(t *testing.T) {
	assert.True(t, IsResourceTypeClusterScoped("v1/Namespace"))
	assert.True(t, IsResourceTypeClusterScoped("rbac.authorization.k8s.io/v1/ClusterRole"))
	assert.False(t, IsResourceTypeClusterScoped("apps/v1/Deployment"))
	assert.False(t, IsResourceTypeClusterScoped("v1/ConfigMap"))

	// A type the specs say nothing about is namespaced, which is what leaving a type out of the
	// enumeration used to mean.
	assert.False(t, IsResourceTypeClusterScoped("example.com/v1/SomethingNobodyDeclared"))

	// Except where the Crossplane group-suffix rule classifies it, which no enumeration could.
	assert.True(t, IsResourceTypeClusterScoped("ec2.aws.upbound.io/v1beta1/VPC"))
	assert.False(t, IsResourceTypeClusterScoped("ec2.aws.m.upbound.io/v1beta1/VPC"))
}

func TestSimilarityComesFromTheDeclaredClass(t *testing.T) {
	rp := NewK8sResourceProvider()

	assert.True(t, rp.ResourceTypesAreSimilar("apps/v1/Deployment", "apps/v1/StatefulSet"),
		"two workload controllers carry a pod spec in the same place")
	assert.True(t, rp.ResourceTypesAreSimilar("v1/ConfigMap", "v1/Secret"))
	assert.False(t, rp.ResourceTypesAreSimilar("apps/v1/Deployment", "v1/ConfigMap"),
		"a workload and a config resource are not interchangeable")

	// Two types that declare no class are not similar to each other. Comparing the classes
	// without this would make every undeclared type similar to every other, since they all
	// share the empty string.
	assert.False(t, rp.ResourceTypesAreSimilar("example.com/v1/Alpha", "example.com/v1/Beta"))
	assert.False(t, rp.ResourceTypesAreSimilar("apps/v1/Deployment", "example.com/v1/Alpha"))

	// Same kind in a different group or version stays similar, which is decided before the
	// class is consulted.
	assert.True(t, rp.ResourceTypesAreSimilar("example.com/v1/Widget", "example.com/v2/Widget"))
}

func TestApplyPriorityOrdersTheThingsThatDependOnEachOther(t *testing.T) {
	crd := ResourcePriority("apiextensions.k8s.io/v1/CustomResourceDefinition")
	namespace := ResourcePriority("v1/Namespace")
	configMap := ResourcePriority("v1/ConfigMap")
	deployment := ResourcePriority("apps/v1/Deployment")
	hpa := ResourcePriority("autoscaling/v2/HorizontalPodAutoscaler")

	assert.Less(t, crd, namespace, "a CRD defines types the rest may be instances of")
	assert.Less(t, namespace, configMap, "a namespace holds what goes in it")
	assert.Less(t, configMap, deployment, "a workload mounts config that has to exist")
	assert.Less(t, deployment, hpa, "a policy configures a workload that has to exist")

	assert.Equal(t, 1000, ResourcePriority("example.com/v1/SomethingNobodyDeclared"),
		"a type declaring no priority sorts after everything that declares one")
}

func TestClusterScopedResourceTypesAreDeclaredNotEnumerated(t *testing.T) {
	declared := ClusterScopedResourceTypes()
	assert.NotEmpty(t, declared)
	for _, resourceType := range declared {
		assert.True(t, IsResourceTypeClusterScoped(resourceType),
			"%s is declared cluster-scoped and should read back that way", resourceType)
	}
	assert.Equal(t, len(declared), len(ClusterScopedResourceTypeSet()))

	// Sorted, so that anything built from it -- a visitor's TypeExceptions, say -- is the same
	// from one run to the next.
	for i := 1; i < len(declared); i++ {
		assert.Less(t, string(declared[i-1]), string(declared[i]))
	}
}

// Every declared type states its scope. A type that states none is treated as namespaced, which
// is indistinguishable from a type nobody has classified -- and that is how v1/PersistentVolume,
// v1/Node and the admission policies came to be treated as namespaced by the map this replaced.
// Requiring the field means a new stanza cannot inherit that silence.
func TestEveryDeclaredTypeStatesItsScope(t *testing.T) {
	set, err := BuiltinSpecSet()
	require.NoError(t, err)
	require.NotEmpty(t, set.ResourceTypes)

	for _, spec := range set.ResourceTypes {
		if spec.Type == api.ResourceTypeAny {
			// The universal stanza is not a type and has no scope.
			continue
		}
		assert.NotEmpty(t, spec.Scope, "%s declares no scope", spec.Type)
	}
}

// The schema locations were two string literals inside k8sFnVetSchemas. They are data now, so
// adding a catalog is one line rather than a code change.
func TestSchemaLocationsComeFromTheSpecs(t *testing.T) {
	locations := SchemaLocations()
	require.Len(t, locations, 3)

	assert.Contains(t, locations[0], "kubernetes-json-schema",
		"the upstream Kubernetes schemas answer for the built-in types, and answer first")
	assert.Contains(t, locations[1], "CRDs-catalog",
		"then the community catalog, which answers for most custom types")
	assert.Contains(t, locations[2], "confighub/schema-catalog",
		"then ours, which holds only what neither of the others carries")

	// Each is a kubeconform template over the resource type rather than a per-type URL, which
	// is what lets one line cover a whole catalog.
	for _, location := range locations[1:] {
		assert.Contains(t, location, "{{.Group}}")
		assert.Contains(t, location, "{{.ResourceKind}}")
	}
}
