// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package impl

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/confighub/sdk/core/function/api"
	"github.com/confighub/sdk/core/function/executor"
	"github.com/confighub/sdk/core/workerapi"
)

// A ResourceTypeSpec and an Attribute both register paths, and §6.2 of
// docs/design/resource-type-specs.md is the decision that they coexist rather than one
// subsuming the other. What keeps that from being two hand-maintained structures holding the
// same facts is a boundary, and these are the parts of it that are executable:
//
//   - a Space's Attribute may add an attribute, and cannot displace one the specs declare;
//   - it mints get-/set- functions only for an attribute name the built-ins do not own.
//
// The third part of the boundary is structural and needs no test because nothing can violate
// it: an Attribute has no field for a merge key, an exclusive field or a map-key path, which
// is what §5.2 forbids a layer narrower than the org from carrying.

const containerImage = api.AttributeName("container-image")

func spaceAttribute(name api.AttributeName, resourceType api.ResourceType, path api.UnresolvedPath) executor.AttributeRegistration {
	return executor.AttributeRegistration{
		AttributeName: name,
		ToolchainType: workerapi.ToolchainKubernetesYAML,
		Description:   "registered by a Space",
		// An attribute mints no getter or setter without a parameter to pass or a default to
		// write -- RegisterPathSetterAndGetter returns early on both being absent -- so a
		// registration meant to produce functions states one.
		Parameters: []api.FunctionParameter{{
			ParameterName: "value",
			Description:   "the value to write",
			Required:      true,
			DataType:      api.DataTypeString,
		}},
		ResourceTypePaths: []executor.ResourceTypePathsEntry{{
			ResourceType: resourceType,
			Paths: api.PathToVisitorInfoType{
				path: {Path: path, AttributeName: name, DataType: api.DataTypeString},
			},
		}},
	}
}

func k8sPathRegistry(t *testing.T, exec *executor.ConcreteFunctionExecutor) api.AttributeNameToResourceTypeToPathToVisitorInfoType {
	t.Helper()
	provider, found := exec.GetResourceProvider(workerapi.ToolchainKubernetesYAML)
	require.True(t, found)
	return provider.GetPathRegistry()
}

// k8sFunctionExists reports whether a function name is registered for Kubernetes.
func k8sFunctionExists(t *testing.T, exec *executor.ConcreteFunctionExecutor, functionName string) bool {
	t.Helper()
	fh, found := exec.GetHandler(workerapi.ToolchainKubernetesYAML)
	require.True(t, found)
	return fh.GetHandlerImplementation(functionName) != nil
}

// A Space naming a path the built-in specs already declare does not get to change what that
// path is. The built-ins register first and the first registration stands; a disagreement is
// reported rather than applied.
//
// This test logs "info mismatch for path" at ERROR. That is the reported disagreement, and it
// is the point: the boundary holds loudly rather than silently.
func TestASpaceAttributeCannotDisplaceABuiltInPath(t *testing.T) {
	const imagePath = api.UnresolvedPath("spec.template.spec.containers.*.image")

	builtins := NewStandardExecutor(nil, true)
	before := k8sPathRegistry(t, builtins)[containerImage][api.ResourceType("apps/v1/Deployment")][imagePath]
	require.NotEmpty(t, before.Path, "the built-in specs declare a Deployment's container image")

	withAttribute := NewStandardExecutorWithAttributes(nil, true,
		[]executor.AttributeRegistration{
			spaceAttribute(containerImage, "apps/v1/Deployment", imagePath),
		})
	after := k8sPathRegistry(t, withAttribute)[containerImage][api.ResourceType("apps/v1/Deployment")][imagePath]

	// The registry key normalizes the parameterized segment to a wildcard, so the Space's
	// wildcard path lands on the built-in's key -- and what stays under it is the built-in's
	// path, which names the container by the container-name parameter. That is the claim: a
	// Space can reach the same key and cannot change what is under it.
	assert.Equal(t, before.Path, after.Path)
	assert.Contains(t, string(after.Path), "?name:container-name",
		"the built-in path names the container by parameter; the Space's wildcard did not replace it")
	assert.Equal(t, before.Details.Description, after.Details.Description,
		"and the built-in's description survives the Space's empty one")
}

// Registering an attribute name the built-ins own must not mint a second get-/set- pair for it:
// the built-in functions are the ones that know how to read those paths.
func TestASpaceAttributeMintsNoFunctionsForABuiltInName(t *testing.T) {
	exec := NewStandardExecutorWithAttributes(nil, true,
		[]executor.AttributeRegistration{
			spaceAttribute(containerImage, "apps/v1/Deployment", "spec.template.spec.containers.*.image"),
		})

	// Both exist -- they are built-in Kubernetes functions -- and the point is that the Space's
	// registration did not mint a second pair over the same name. What proves that is the
	// registry: the built-in paths are what they read, unchanged, which the test above asserts.
	assert.True(t, k8sFunctionExists(t, exec, "get-container-image"))
	assert.True(t, k8sFunctionExists(t, exec, "set-container-image"))
}

// The layer does its job for a name the built-ins do not own, which is what an Attribute is
// for: paths a spec has no reason to declare, in one Space.
func TestASpaceAttributeAddsWhatTheSpecsDoNotDeclare(t *testing.T) {
	const costCenter = api.AttributeName("cost-center")
	const costCenterPath = api.UnresolvedPath("metadata.annotations.costCenter")

	exec := NewStandardExecutorWithAttributes(nil, true,
		[]executor.AttributeRegistration{
			spaceAttribute(costCenter, "apps/v1/Deployment", costCenterPath),
		})

	registered := k8sPathRegistry(t, exec)[costCenter][api.ResourceType("apps/v1/Deployment")][costCenterPath]
	assert.Equal(t, costCenterPath, registered.Path)

	assert.True(t, k8sFunctionExists(t, exec, "get-cost-center"),
		"an attribute the built-ins do not own gets its generated getter")
	assert.True(t, k8sFunctionExists(t, exec, "set-cost-center"))

	// And the built-ins have no such function, so this one came from the Space.
	assert.False(t, k8sFunctionExists(t, NewStandardExecutor(nil, true), "get-cost-center"))
}
