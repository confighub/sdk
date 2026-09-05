// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package yamlkit

import (
	"testing"

	"github.com/confighub/sdk/core/function/api"
	"github.com/confighub/sdk/core/workerapi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const testToolchain = workerapi.ToolchainKubernetesYAML

func TestNormalizeStructurePath(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"spec.containers", "spec.containers"},
		{"spec.containers.0.env", "spec.containers.*.env"},
		{"spec.containers.12.env.3", "spec.containers.*.env.*"},
		{"metadata.ownerReferences", "metadata.ownerReferences"},
		{"webhooks.0.matchConditions", "webhooks.*.matchConditions"},
		{"spec.template.spec.containers.?name=nginx.env", "spec.template.spec.containers.*.env"},
		{"spec.template.spec.containers.?name=nginx;@0.env", "spec.template.spec.containers.*.env"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			assert.Equal(t, tt.want, NormalizeStructurePath(tt.input))
		})
	}
}

func TestJoinRelativePath(t *testing.T) {
	assert.Equal(t, "spec.template", JoinRelativePath("spec", "template"))
	assert.Equal(t, "template", JoinRelativePath("", "template"))
	assert.Equal(t, "spec", JoinRelativePath("spec", ""))
	assert.Equal(t, "", JoinRelativePath("", ""))
}

// A shape states its paths relative to its own root, so the same declaration lands at
// different absolute paths depending on where each type embeds it. That is the fact the
// per-concern tables could not express: they stored one of the outputs and derived the rest.
func TestCompileSpecsJoinsEmbeddedShapesToWhereTheySit(t *testing.T) {
	compiled, err := CompileSpecSets(nestedShapeSet())
	require.NoError(t, err)

	deployment := api.ResourceType("apps/v1/Deployment")
	keys, found := compiled.MergeKeysForPath(testToolchain, deployment, "spec.template.spec.containers")
	assert.True(t, found)
	assert.Equal(t, []string{"name"}, keys)
	keys, found = compiled.MergeKeysForPath(testToolchain, deployment, "spec.template.spec.containers.*.env")
	assert.True(t, found)
	assert.Equal(t, []string{"name"}, keys)
	assert.True(t, compiled.IsMapKeyPath(testToolchain, deployment, "spec.template.metadata.labels.*"))
	assert.True(t, compiled.IsMapKeyPath(testToolchain, deployment, "spec.template.spec.nodeSelector.*"))

	// The same PodSpec embedded one level up, with no pod template around it.
	pod := api.ResourceType("v1/Pod")
	keys, found = compiled.MergeKeysForPath(testToolchain, pod, "spec.containers.*.env")
	assert.True(t, found)
	assert.Equal(t, []string{"name"}, keys)
	assert.True(t, compiled.IsMapKeyPath(testToolchain, pod, "spec.nodeSelector.*"))
	assert.False(t, compiled.IsMapKeyPath(testToolchain, pod, "spec.metadata.labels.*"),
		"a Pod has no pod template, so it must not inherit the template's metadata paths")

	fields, found := compiled.ExclusiveFieldsForPath(testToolchain, pod, "spec.containers.0.env.3")
	assert.True(t, found, "a numeric index normalizes to a wildcard for lookup")
	assert.Equal(t, []string{"value", "valueFrom"}, fields.Members)
}

// A composite merge key keeps its extra keys in declared order: Key first.
func TestCompileSpecsKeepsCompositeMergeKeys(t *testing.T) {
	compiled, err := CompileSpecSets(SpecSet{
		ToolchainType: testToolchain,
		ResourceTypes: []ResourceTypeSpec{{
			Type: api.ResourceType("v1/Service"),
			Declaration: Declaration{
				MergeKeys: []MergeKeyField{{Path: "spec.ports", Key: "port", ExtraKeys: []string{"protocol"}}},
			},
		}},
	})
	require.NoError(t, err)

	keys, found := compiled.MergeKeysForPath(testToolchain, api.ResourceType("v1/Service"), "spec.ports")
	assert.True(t, found)
	assert.Equal(t, []string{"port", "protocol"}, keys)
}

// All three structure lookups fall back to what is declared for every resource type.
func TestCompileSpecsWildcardFallback(t *testing.T) {
	compiled, err := CompileSpecSets(SpecSet{
		ToolchainType: testToolchain,
		ResourceTypes: []ResourceTypeSpec{{
			Type: api.ResourceTypeAny,
			Declaration: Declaration{
				MergeKeys:       []MergeKeyField{{Path: "metadata.ownerReferences", Key: "uid"}},
				MapKeyPaths:     []string{"metadata.labels.*"},
				ExclusiveFields: []ExclusiveFieldGroup{{Path: "spec", Members: []string{"a", "b"}}},
			},
		}},
	})
	require.NoError(t, err)

	unknown := api.ResourceType("example.com/v1/Widget")
	keys, found := compiled.MergeKeysForPath(testToolchain, unknown, "metadata.ownerReferences")
	assert.True(t, found)
	assert.Equal(t, []string{"uid"}, keys)
	assert.True(t, compiled.IsMapKeyPath(testToolchain, unknown, "metadata.labels.*"))

	fields, found := compiled.ExclusiveFieldsForPath(testToolchain, unknown, "spec")
	assert.True(t, found)
	assert.Equal(t, []string{"a", "b"}, fields.Members)
}

// ResourceType is not unique across toolchains, so the same name in two toolchains is two
// specs.
func TestCompileSpecsKeysByToolchain(t *testing.T) {
	compiled, err := CompileSpecSets(SpecSet{
		ResourceTypes: []ResourceTypeSpec{
			{
				ToolchainType: workerapi.ToolchainKubernetesYAML,
				Type:          api.ResourceType("NoSchema"),
				Declaration:   Declaration{MergeKeys: []MergeKeyField{{Path: "items", Key: "name"}}},
			},
			{
				ToolchainType: workerapi.ToolchainAppConfigYAML,
				Type:          api.ResourceType("NoSchema"),
				Declaration:   Declaration{MergeKeys: []MergeKeyField{{Path: "items", Key: "id"}}},
			},
		},
	})
	require.NoError(t, err)

	keys, _ := compiled.MergeKeysForPath(workerapi.ToolchainKubernetesYAML, api.ResourceType("NoSchema"), "items")
	assert.Equal(t, []string{"name"}, keys)
	keys, _ = compiled.MergeKeysForPath(workerapi.ToolchainAppConfigYAML, api.ResourceType("NoSchema"), "items")
	assert.Equal(t, []string{"id"}, keys)
}

// A shape that embeds itself would expand forever. The compiler reports it instead, naming
// the shape, rather than hanging at package init.
func TestCompileSpecsRejectsSelfEmbeddingShape(t *testing.T) {
	_, err := CompileSpecSets(SpecSet{
		ToolchainType: testToolchain,
		Shapes: map[string]Declaration{
			"Recursive": {Embeds: []ShapeEmbed{{Shape: "Recursive", Path: "child"}}},
		},
		ResourceTypes: []ResourceTypeSpec{{
			Type:        api.ResourceType("example.com/v1/Tree"),
			Declaration: Declaration{Embeds: []ShapeEmbed{{Shape: "Recursive", Path: "root"}}},
		}},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "embeds itself")
	assert.Contains(t, err.Error(), "example.com/v1/Tree")
}

// A shape reached twice by different routes is not a cycle. A PodSpec embeds the same
// Container shape three times, so getting this wrong would break every workload.
func TestCompileSpecsAllowsTheSameShapeEmbeddedTwice(t *testing.T) {
	compiled, err := CompileSpecSets(SpecSet{
		ToolchainType: testToolchain,
		Shapes: map[string]Declaration{
			"Leaf": {MergeKeys: []MergeKeyField{{Path: "items", Key: "name"}}},
			"Branch": {Embeds: []ShapeEmbed{
				{Shape: "Leaf", Path: "left"},
				{Shape: "Leaf", Path: "right"},
			}},
		},
		ResourceTypes: []ResourceTypeSpec{{
			Type:        api.ResourceType("example.com/v1/Tree"),
			Declaration: Declaration{Embeds: []ShapeEmbed{{Shape: "Branch", Path: "spec"}}},
		}},
	})
	require.NoError(t, err)

	tree := api.ResourceType("example.com/v1/Tree")
	_, found := compiled.MergeKeysForPath(testToolchain, tree, "spec.left.items")
	assert.True(t, found)
	_, found = compiled.MergeKeysForPath(testToolchain, tree, "spec.right.items")
	assert.True(t, found)
}

func TestCompileSpecsRejectsUndeclaredShape(t *testing.T) {
	_, err := CompileSpecSets(SpecSet{
		ToolchainType: testToolchain,
		ResourceTypes: []ResourceTypeSpec{{
			Type:        api.ResourceType("example.com/v1/Widget"),
			Declaration: Declaration{Embeds: []ShapeEmbed{{Shape: "PodSpec", Path: "spec"}}},
		}},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), `undeclared shape "PodSpec"`)
}

func TestCompileSpecsRejectsSpecWithNoToolchain(t *testing.T) {
	_, err := CompileSpecSets(SpecSet{
		ResourceTypes: []ResourceTypeSpec{{Type: api.ResourceType("example.com/v1/Widget")}},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no toolchain type")
}

func TestCompileSpecsRejectsDuplicateShapeAcrossSets(t *testing.T) {
	shape := SpecSet{
		ToolchainType: testToolchain,
		Shapes:        map[string]Declaration{"PodSpec": {}},
	}
	_, err := CompileSpecSets(shape, shape)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "declared in more than one spec set")
}

// A set registering a new type embeds a shape another set declares -- a CRD with an ordinary
// PodSpec is the case that needs it, and it is why shapes resolve across sets rather than
// within one.
func TestCompileSpecSetsResolveShapesAcrossSets(t *testing.T) {
	compiled, err := CompileSpecSets(
		nestedShapeSet(),
		SpecSet{
			ToolchainType: testToolchain,
			ResourceTypes: []ResourceTypeSpec{{
				Type:        api.ResourceType("actions.github.com/v1alpha1/AutoscalingRunnerSet"),
				Declaration: Declaration{Embeds: []ShapeEmbed{{Shape: "PodTemplate", Path: "spec.template"}}},
			}},
		},
	)
	require.NoError(t, err)

	runnerSet := api.ResourceType("actions.github.com/v1alpha1/AutoscalingRunnerSet")
	keys, found := compiled.MergeKeysForPath(testToolchain, runnerSet, "spec.template.spec.containers")
	assert.True(t, found, "a CRD embedding a built-in shape gets the shape's merge keys")
	assert.Equal(t, []string{"name"}, keys)
}

// Specs are data. A set that has been through YAML must compile to the same thing as the set
// it was rendered from, or the format and the Go types have drifted.
func TestSpecSetRoundTripsThroughYAML(t *testing.T) {
	original := nestedShapeSet()

	rendered, err := MarshalSpecSet(original)
	require.NoError(t, err)

	reloaded, err := LoadSpecSet(rendered)
	require.NoError(t, err)
	assert.Equal(t, original, reloaded)

	fromGo, err := CompileSpecSets(original)
	require.NoError(t, err)
	fromYAML, err := CompileSpecSets(reloaded)
	require.NoError(t, err)
	assert.Equal(t,
		fromGo.RenderStructure(testToolchain),
		fromYAML.RenderStructure(testToolchain))
}

func TestLoadSpecSetRejectsUnknownFields(t *testing.T) {
	_, err := LoadSpecSet([]byte(`
toolchainType: Kubernetes/YAML
resourceTypes:
  - type: v1/Pod
    mergeKys: []
`))
	require.Error(t, err, "a misspelled field must not be silently ignored")
}

// nestedShapeSet is a Container/PodSpec/PodTemplate arrangement in miniature: the shape
// nesting the built-in Kubernetes specs use, small enough to assert about exhaustively.
func nestedShapeSet() SpecSet {
	return SpecSet{
		ToolchainType: testToolchain,
		Shapes: map[string]Declaration{
			"Container": {
				MergeKeys:       []MergeKeyField{{Path: "env", Key: "name"}},
				ExclusiveFields: []ExclusiveFieldGroup{{Path: "env.*", Members: []string{"value", "valueFrom"}}},
			},
			"PodSpec": {
				Embeds:      []ShapeEmbed{{Shape: "Container", Path: "containers.*"}},
				MergeKeys:   []MergeKeyField{{Path: "containers", Key: "name"}},
				MapKeyPaths: []string{"nodeSelector.*"},
			},
			"PodTemplate": {
				Embeds:      []ShapeEmbed{{Shape: "PodSpec", Path: "spec"}},
				MapKeyPaths: []string{"metadata.labels.*"},
			},
		},
		ResourceTypes: []ResourceTypeSpec{
			{
				Type:        api.ResourceType("apps/v1/Deployment"),
				Declaration: Declaration{Embeds: []ShapeEmbed{{Shape: "PodTemplate", Path: "spec.template"}}},
			},
			{
				Type:        api.ResourceType("v1/Pod"),
				Declaration: Declaration{Embeds: []ShapeEmbed{{Shape: "PodSpec", Path: "spec"}}},
			},
		},
	}
}

// Attribute paths join through embeds the same way structure facts do, so an attribute
// declared on a shape lands wherever the shape sits.
func TestCompileSpecsJoinsAttributePathsThroughEmbeds(t *testing.T) {
	compiled, err := CompileSpecSets(SpecSet{
		ToolchainType: testToolchain,
		Shapes: map[string]Declaration{
			"PodSpec": {
				Attributes: map[api.AttributeName][]AttributePath{
					"container-name": {{Path: "containers.*.name"}},
				},
			},
			"PodTemplate": {Embeds: []ShapeEmbed{{Shape: "PodSpec", Path: "spec"}}},
		},
		ResourceTypes: []ResourceTypeSpec{
			{
				Type:        api.ResourceType("apps/v1/Deployment"),
				Declaration: Declaration{Embeds: []ShapeEmbed{{Shape: "PodTemplate", Path: "spec.template"}}},
			},
			{
				Type: api.ResourceType("v1/Pod"),
				Declaration: Declaration{
					Embeds:     []ShapeEmbed{{Shape: "PodSpec", Path: "spec"}},
					Attributes: map[api.AttributeName][]AttributePath{"immutable": {{Path: "spec.nodeName"}}},
				},
			},
		},
	})
	require.NoError(t, err)

	deployment := compiled.DeclaredAttributes(testToolchain, api.ResourceType("apps/v1/Deployment"))
	assert.Equal(t,
		[]AttributePath{{Path: "spec.template.spec.containers.*.name"}},
		deployment["container-name"])

	pod := compiled.DeclaredAttributes(testToolchain, api.ResourceType("v1/Pod"))
	assert.Equal(t, []AttributePath{{Path: "spec.containers.*.name"}}, pod["container-name"])
	assert.Equal(t, []AttributePath{{Path: "spec.nodeName"}}, pod["immutable"],
		"a type's own attributes sit beside those its shapes bring")

	assert.Equal(t,
		[]api.ResourceType{"apps/v1/Deployment", "v1/Pod"},
		compiled.ResourceTypesWithAttributes(testToolchain))
}

// A declared attribute with no descriptor is the failure the spec model exists to prevent:
// a path that registers nothing and says nothing about it.
func TestRegisterDeclaredAttributePathsRejectsUndescribedAttribute(t *testing.T) {
	compiled, err := CompileSpecSets(SpecSet{
		ToolchainType: testToolchain,
		ResourceTypes: []ResourceTypeSpec{{
			Type:        api.ResourceType("v1/Pod"),
			Declaration: Declaration{Attributes: map[api.AttributeName][]AttributePath{"mispelled": {{Path: "spec.nodeName"}}}},
		}},
	})
	require.NoError(t, err)

	err = RegisterDeclaredAttributePaths(nil, compiled, testToolchain, nil, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), `attribute "mispelled"`)
	assert.Contains(t, err.Error(), "no descriptor")
}

// A map-key path names the map whose children are dynamic keys, and normalizePath asks about
// it with a path ending in the wildcard. One declared without that suffix can never match, so
// it is a declaration that silently does nothing -- exactly what specs exist to prevent.
func TestCompileSpecsRejectsMapKeyPathWithoutWildcard(t *testing.T) {
	_, err := CompileSpecSets(SpecSet{
		ToolchainType: testToolchain,
		ResourceTypes: []ResourceTypeSpec{{
			Type:        api.ResourceType("v1/Pod"),
			Declaration: Declaration{MapKeyPaths: []string{"spec.containers.*.env.*.valueFrom.fieldRef.fieldPath"}},
		}},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "can never match")
}

// The suffix is checked after the path is joined to where its shape sits, so a shape cannot
// smuggle one in.
func TestCompileSpecsRejectsMapKeyPathWithoutWildcardInShape(t *testing.T) {
	_, err := CompileSpecSets(SpecSet{
		ToolchainType: testToolchain,
		Shapes:        map[string]Declaration{"PodSpec": {MapKeyPaths: []string{"nodeSelector"}}},
		ResourceTypes: []ResourceTypeSpec{{
			Type:        api.ResourceType("v1/Pod"),
			Declaration: Declaration{Embeds: []ShapeEmbed{{Shape: "PodSpec", Path: "spec"}}},
		}},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "spec.nodeSelector")
}

// A spec read from YAML or JSON has no integers: every number arrives as a float64. An
// int-typed path handed one rejects it and silently keeps whatever was already there, which is
// how a set of pod security defaults loses exactly its integer fields.
func TestCompileSpecsCoercesNumericDefaultsToTheDeclaredType(t *testing.T) {
	set, err := LoadSpecSet([]byte(`
toolchainType: Kubernetes/YAML
resourceTypes:
  - type: v1/Pod
    attributes:
      defaults:
        - {path: spec.securityContext.runAsUser, dataType: int, default: 1000}
        - {path: spec.securityContext.runAsNonRoot, dataType: bool, default: true}
`))
	require.NoError(t, err)
	compiled, err := CompileSpecSets(set)
	require.NoError(t, err)

	rp := &testResourceProvider{registry: NewResourceProviderRegistry(testToolchain)}
	require.NoError(t, RegisterDeclaredAttributePaths(rp, compiled, testToolchain,
		map[api.AttributeName]AttributeDescriptor{"defaults": {}}, nil))

	paths := rp.GetPathRegistry()["defaults"][api.ResourceType("v1/Pod")]
	assert.Equal(t, 1000, paths["spec.securityContext.runAsUser"].Details.DefaultValue,
		"an int default must not stay a float64")
	assert.Equal(t, true, paths["spec.securityContext.runAsNonRoot"].Details.DefaultValue)
}

// Where a schema is fetched from is declared, not written into the function that fetches it.
// The locations are per toolchain because that is the granularity a catalog covers; the
// per-type field is for the one whose schema is not where the templates say, and for the one
// that has none.
func TestSchemaLocationsAreDeclaredPerToolchain(t *testing.T) {
	kubernetes, err := LoadSpecSet([]byte(`
toolchainType: Kubernetes/YAML
schemaLocations:
  - https://example.com/k8s/{{.ResourceKind}}.json
  - https://example.com/crds/{{.Group}}/{{.ResourceKind}}_{{.ResourceAPIVersion}}.json
resourceTypes:
  - type: example.com/v1/Widget
    schema: https://example.com/one-off/widget.json
  - type: example.com/v1/Gadget
    schema: none
  - type: example.com/v1/Ordinary
`))
	require.NoError(t, err)
	appconfig, err := LoadSpecSet([]byte(`
toolchainType: AppConfig/TOML
resourceTypes:
  - type: Config
`))
	require.NoError(t, err)

	compiled, err := CompileSpecSets(kubernetes, appconfig)
	require.NoError(t, err)

	assert.Equal(t, []string{
		"https://example.com/k8s/{{.ResourceKind}}.json",
		"https://example.com/crds/{{.Group}}/{{.ResourceKind}}_{{.ResourceAPIVersion}}.json",
	}, compiled.SchemaLocationsFor(workerapi.ToolchainKubernetesYAML), "in declaration order, most authoritative first")

	assert.Empty(t, compiled.SchemaLocationsFor(workerapi.ToolchainAppConfigTOML),
		"a toolchain whose formats have no schema to fetch declares none")

	assert.Equal(t, "https://example.com/one-off/widget.json",
		compiled.SchemaFor(workerapi.ToolchainKubernetesYAML, "example.com/v1/Widget"))
	assert.Equal(t, SchemaNone,
		compiled.SchemaFor(workerapi.ToolchainKubernetesYAML, "example.com/v1/Gadget"),
		"a type known to have no schema says so, so a validator can report it as unchecked")
	assert.Empty(t, compiled.SchemaFor(workerapi.ToolchainKubernetesYAML, "example.com/v1/Ordinary"),
		"the ordinary case is to look where the templates say")

	// The returned slice is the caller's, not the snapshot's.
	locations := compiled.SchemaLocationsFor(workerapi.ToolchainKubernetesYAML)
	locations[0] = "mutated"
	assert.NotEqual(t, "mutated", compiled.SchemaLocationsFor(workerapi.ToolchainKubernetesYAML)[0])
}
