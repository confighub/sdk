// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package k8skit

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/confighub/sdk/core/configkit/yamlkit"
	"github.com/confighub/sdk/core/function/api"
)

// A CRD shaped like the ones this exists for: a pod template under spec, a keyed array, a
// freeform map, a union, and a field the schema says cannot change.
const runnerSetCRD = `apiVersion: apiextensions.k8s.io/v1
kind: CustomResourceDefinition
metadata:
  name: autoscalingrunnersets.actions.github.com
spec:
  group: actions.github.com
  names:
    kind: AutoscalingRunnerSet
    plural: autoscalingrunnersets
  scope: Namespaced
  versions:
    - name: v1alpha1
      served: true
      storage: true
      schema:
        openAPIV3Schema:
          type: object
          properties:
            apiVersion: {type: string}
            kind: {type: string}
            metadata: {type: object}
            spec:
              type: object
              properties:
                githubConfigUrl:
                  type: string
                  x-kubernetes-validations:
                    - rule: self == oldSelf
                      message: githubConfigUrl is immutable
                runnerGroup:
                  type: string
                  x-kubernetes-validations:
                    - rule: self == oldSelf || oldSelf == ''
                listenerTemplate:
                  type: object
                  properties:
                    annotations:
                      type: object
                      additionalProperties: {type: string}
                proxy:
                  type: object
                  oneOf:
                    - required: [http]
                    - required: [https]
                  properties:
                    http: {type: object}
                    https: {type: object}
                template:
                  type: object
                  properties:
                    spec:
                      type: object
                      properties:
                        containers:
                          type: array
                          x-kubernetes-list-type: map
                          x-kubernetes-list-map-keys: [name]
                          items:
                            type: object
                            properties:
                              name: {type: string}
                              image: {type: string}
                        volumes:
                          type: array
                          x-kubernetes-patch-merge-key: name
                          items:
                            type: object
                            properties:
                              name: {type: string}
            status:
              type: object
              properties:
                conditions:
                  type: array
                  x-kubernetes-list-type: map
                  x-kubernetes-list-map-keys: [type]
                  items: {type: object}
`

func generatedSpec(t *testing.T, crd string, options CRDSpecOptions) CRDSpec {
	t.Helper()
	specs, err := SpecsFromCRD([]byte(crd), options)
	require.NoError(t, err)
	require.Len(t, specs, 1)
	return specs[0]
}

func TestCRDSpecReadsWhatTheSchemaStates(t *testing.T) {
	generated := generatedSpec(t, runnerSetCRD, DefaultCRDSpecOptions())

	assert.Equal(t, api.ResourceType("actions.github.com/v1alpha1/AutoscalingRunnerSet"), generated.Spec.Type)
	assert.Equal(t, yamlkit.ScopeNamespaced, generated.Spec.Scope)

	assert.Equal(t, []string{"spec.listenerTemplate.annotations.*"}, generated.Spec.MapKeyPaths,
		"an object with an additionalProperties schema is a map whose children are keys")

	require.Len(t, generated.Spec.ExclusiveFields, 1)
	assert.Equal(t, "spec.proxy", generated.Spec.ExclusiveFields[0].Path)
	assert.Equal(t, []string{"http", "https"}, generated.Spec.ExclusiveFields[0].Members)

	immutable := generated.Spec.Attributes[AttributeNameImmutable]
	require.Len(t, immutable, 1, "only the plain equality is taken as immutability")
	assert.Equal(t, "spec.githubConfigUrl", immutable[0].Path)
	assert.Contains(t, generated.Notes, `spec.runnerGroup has the validation "self == oldSelf || oldSelf == ''", `+
		"which constrains changes without forbidding them; add it as immutable by hand if it means that")
}

// The pod template is where the whole registration is, and it is also the one thing here that is
// a guess -- so it comes back separately from what the schema states outright.
func TestCRDSpecSuggestsThePodTemplateAndDropsWhatItCovers(t *testing.T) {
	generated := generatedSpec(t, runnerSetCRD, DefaultCRDSpecOptions())

	assert.Equal(t, []yamlkit.ShapeEmbed{{Shape: ShapePodTemplate, Path: "spec.template"}},
		generated.SuggestedEmbeds, "a pod spec at X.spec is a pod template at X")

	// The shape declares the containers and volumes keys, so restating them per type would be
	// the duplication specs exist to remove.
	for _, mergeKey := range generated.Spec.MergeKeys {
		assert.NotContains(t, mergeKey.Path, "spec.template.")
	}
}

func TestCRDSpecWithoutTheMatchDeclaresTheArraysItself(t *testing.T) {
	generated := generatedSpec(t, runnerSetCRD, CRDSpecOptions{MatchPodSpec: false})

	assert.Empty(t, generated.SuggestedEmbeds)
	assert.Equal(t, []yamlkit.MergeKeyField{
		{Path: "spec.template.spec.containers", Key: "name"},
		{Path: "spec.template.spec.volumes", Key: "name"},
	}, generated.Spec.MergeKeys, "both spellings of a keyed array are read")
}

// status is not authored configuration, so its keyed arrays are not the type's merge keys.
func TestCRDSpecDoesNotReadStatus(t *testing.T) {
	generated := generatedSpec(t, runnerSetCRD, CRDSpecOptions{MatchPodSpec: false})

	for _, mergeKey := range generated.Spec.MergeKeys {
		assert.NotContains(t, mergeKey.Path, "status")
	}
	assert.Contains(t, generated.Notes, "status is not authored configuration and was not read")
}

func TestCRDSpecEmitsOneSpecPerVersion(t *testing.T) {
	const twoVersions = `apiVersion: apiextensions.k8s.io/v1
kind: CustomResourceDefinition
metadata: {name: widgets.example.com}
spec:
  group: example.com
  names: {kind: Widget}
  scope: Cluster
  versions:
    - name: v1alpha1
      schema: {openAPIV3Schema: {type: object}}
    - name: v1
      schema: {openAPIV3Schema: {type: object}}
`
	specs, err := SpecsFromCRD([]byte(twoVersions), DefaultCRDSpecOptions())
	require.NoError(t, err)
	require.Len(t, specs, 2, "a version is part of the resource type, so each gets a spec")
	assert.Equal(t, api.ResourceType("example.com/v1alpha1/Widget"), specs[0].Spec.Type)
	assert.Equal(t, api.ResourceType("example.com/v1/Widget"), specs[1].Spec.Type)
	assert.Equal(t, yamlkit.ScopeCluster, specs[1].Spec.Scope)
}

func TestCRDSpecRejectsADocumentWithNoCRD(t *testing.T) {
	_, err := SpecsFromCRD([]byte("apiVersion: v1\nkind: ConfigMap\nmetadata: {name: x}\n"), DefaultCRDSpecOptions())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no CustomResourceDefinition")
}

// What the generator produces has to compile, or it is not a spec.
func TestGeneratedSpecCompiles(t *testing.T) {
	generated := generatedSpec(t, runnerSetCRD, DefaultCRDSpecOptions())
	spec := generated.Spec
	spec.Embeds = generated.SuggestedEmbeds

	builtin, err := BuiltinSpecSet()
	require.NoError(t, err)
	set := yamlkit.SpecSet{
		ToolchainType: builtin.ToolchainType,
		Shapes:        builtin.Shapes,
		ResourceTypes: []yamlkit.ResourceTypeSpec{spec},
	}
	compiled, err := yamlkit.CompileSpecSets(set)
	require.NoError(t, err)

	keys, found := compiled.MergeKeysForPath(builtin.ToolchainType, spec.Type, "spec.template.spec.containers")
	assert.True(t, found, "the suggested embed is what gives the type its pod-spec merge keys")
	assert.Equal(t, []string{"name"}, keys)
}
