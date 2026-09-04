// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package k8skit

import (
	"testing"

	"github.com/confighub/sdk/core/configkit/yamlkit"
	"github.com/confighub/sdk/core/function/api"
	"github.com/confighub/sdk/core/workerapi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The built-in specs are data rather than Go, so that the same declaration can later be
// loaded from a file or held in a ConfigHub Unit. This is the claim, tested: the embedded
// file survives a render and a reparse, and compiles to the same lookups either way.
func TestBuiltinSpecSetIsData(t *testing.T) {
	set, err := BuiltinSpecSet()
	require.NoError(t, err)
	assert.NotEmpty(t, set.ResourceTypes)
	assert.Equal(t, workerapi.ToolchainKubernetesYAML, set.ToolchainType)

	rendered, err := yamlkit.MarshalSpecSet(set)
	require.NoError(t, err)
	reloaded, err := yamlkit.LoadSpecSet(rendered)
	require.NoError(t, err)
	assert.Equal(t, set, reloaded)

	recompiled, err := yamlkit.CompileSpecSets(reloaded)
	require.NoError(t, err)
	assert.Equal(t,
		compiledK8sSpecs.RenderStructure(workerapi.ToolchainKubernetesYAML),
		recompiled.RenderStructure(workerapi.ToolchainKubernetesYAML))
}

// Registering a PodSpec-bearing CRD is six lines of data that embed a built-in shape, with no
// change to this package. Compiling it here alongside the built-ins is the mechanism the
// loader will use; the type itself is not registered, which is a later stage.
func TestSpecSetLoadedBesideTheBuiltinsCanEmbedItsShapes(t *testing.T) {
	builtin, err := BuiltinSpecSet()
	require.NoError(t, err)

	loaded, err := yamlkit.LoadSpecSet([]byte(`
toolchainType: Kubernetes/YAML
resourceTypes:
  - type: actions.github.com/v1alpha1/AutoscalingRunnerSet
    embeds:
      - {shape: PodTemplate, path: spec.template}
`))
	require.NoError(t, err)

	compiled, err := yamlkit.CompileSpecSets(builtin, loaded)
	require.NoError(t, err)

	runnerSet := api.ResourceType("actions.github.com/v1alpha1/AutoscalingRunnerSet")
	keys, found := compiled.MergeKeysForPath(workerapi.ToolchainKubernetesYAML, runnerSet, "spec.template.spec.containers")
	assert.True(t, found, "a CRD embedding PodTemplate gets the PodSpec's merge keys")
	assert.Equal(t, []string{"name"}, keys)

	keys, found = compiled.MergeKeysForPath(workerapi.ToolchainKubernetesYAML, runnerSet, "spec.template.spec.containers.*.ports")
	assert.True(t, found)
	assert.Equal(t, []string{"containerPort", "protocol"}, keys)

	assert.True(t, compiled.IsMapKeyPath(workerapi.ToolchainKubernetesYAML, runnerSet, "spec.template.metadata.labels.*"))
}
