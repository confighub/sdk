// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package cubkit

import (
	_ "embed"

	"github.com/confighub/sdk/core/configkit/yamlkit"
	"github.com/confighub/sdk/core/function/api"
	"github.com/confighub/sdk/core/workerapi"
)

// The ConfigHub/YAML resource-type specs describe ConfigHub's own entity types, one per type that
// can be managed as a document: how its lists merge, which of its fields cannot change, what its
// names are scoped by, and the order to apply it in. Unlike the Kubernetes specs they are not
// written by hand. resource_type_specs.yaml is generated from ConfigHub's entity definitions, so
// the specs and the API cannot disagree.

//go:embed resource_type_specs.yaml
var builtinSpecSetYAML []byte

// Scopes of ConfigHub entity names. Most entities are named within a Space; a Space or a
// Component is named within the Organization.
const (
	ScopeSpace        yamlkit.Scope = "Space"
	ScopeOrganization yamlkit.Scope = "Organization"
)

// AttributeNameImmutable is the attribute declaring the fields that cannot change once an entity
// is created. vet-immutable reads it.
const AttributeNameImmutable = api.AttributeName("immutable")

// BuiltinSpecSet returns the generated ConfigHub/YAML resource-type specs, parsed afresh, so a
// caller cannot disturb the compiled ones.
func BuiltinSpecSet() (yamlkit.SpecSet, error) {
	return yamlkit.LoadSpecSet(builtinSpecSetYAML)
}

// compiledSpecs holds the structure lookups the merge engine reads, compiled once at init.
var compiledSpecs = mustCompileBuiltinSpecs()

func mustCompileBuiltinSpecs() *yamlkit.CompiledSpecs {
	set, err := BuiltinSpecSet()
	if err != nil {
		// The specs are embedded in this package, so a failure here is a bug in the generated
		// file rather than anything a caller did.
		panic("cubkit: parsing built-in resource type specs: " + err.Error())
	}
	compiled, err := yamlkit.CompileSpecSets(set)
	if err != nil {
		panic("cubkit: compiling built-in resource type specs: " + err.Error())
	}
	return compiled
}

// RegisterDeclaredAttributePaths registers the attribute paths the specs declare, pairing each
// with the descriptor its attribute name is registered under.
func RegisterDeclaredAttributePaths(rp *ConfigHubResourceProviderType,
	descriptors map[api.AttributeName]yamlkit.AttributeDescriptor) error {
	return yamlkit.RegisterDeclaredAttributePaths(
		rp, compiledSpecs, workerapi.ToolchainConfigHubYAML, descriptors, nil)
}

// ApplyPriorityOf returns the order an entity type is applied in among others, lower first, and
// whether the type declares one.
func ApplyPriorityOf(entityType api.ResourceType) (int, bool) {
	return compiledSpecs.ApplyPriorityOf(workerapi.ToolchainConfigHubYAML, entityType)
}
