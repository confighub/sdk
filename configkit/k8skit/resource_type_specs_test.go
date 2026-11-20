// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package k8skit

import (
	"slices"
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

// Every type's name format is one the checks know, and the declarations land where they are read.
func TestNameFormatsAndCorrectnessDeclarations(t *testing.T) {
	set, err := BuiltinSpecSet()
	require.NoError(t, err)
	for _, spec := range set.ResourceTypes {
		if spec.NameFormat != "" {
			assert.Contains(t, NameFormats, spec.NameFormat, "%s", spec.Type)
		}
	}
	assert.Equal(t, NameFormatDNS1123Label, NameFormatOf("v1/Namespace"))
	assert.Equal(t, NameFormatDNS1035Label, NameFormatOf("v1/Service"))
	assert.Equal(t, NameFormatPathSegment, NameFormatOf("rbac.authorization.k8s.io/v1/Role"))
	assert.Equal(t, NameFormatDNS1123Subdomain, NameFormatOf("apps/v1/Deployment"))

	required := RequiredExclusiveFields("policy/v1/PodDisruptionBudget")
	require.Contains(t, required, "spec")
	assert.Equal(t, []string{"maxUnavailable", "minAvailable"}, required["spec"].Members)
	assert.Empty(t, RequiredExclusiveFields("apps/v1/Deployment"))

	deprecated := DeprecatedPaths("apps/v1/Deployment")
	require.Len(t, deprecated, 1)
	assert.Equal(t, "spec.template.spec.serviceAccount", deprecated[0].Path)
	assert.Equal(t, "spec.template.spec.serviceAccountName", deprecated[0].Replacement)
}

// A Pod's labels are its own; a workload's are its pod template's, wherever its kind keeps it; a
// PodTemplate, and a kind with no template, run no pods.
func TestPodLabelsPath(t *testing.T) {
	for resourceType, want := range map[api.ResourceType][]string{
		"v1/Pod":                       {"metadata", "labels"},
		"apps/v1/Deployment":           {"spec", "template", "metadata", "labels"},
		"batch/v1/CronJob":             {"spec", "jobTemplate", "spec", "template", "metadata", "labels"},
		"argoproj.io/v1alpha1/Rollout": {"spec", "template", "metadata", "labels"},
	} {
		got, ok := PodLabelsPath(resourceType)
		if !ok || !slices.Equal(got, want) {
			t.Errorf("PodLabelsPath(%s) = %v, %v; want %v", resourceType, got, ok, want)
		}
	}
	for _, resourceType := range []api.ResourceType{"v1/PodTemplate", "v1/Service", "networking.k8s.io/v1/NetworkPolicy"} {
		if got, ok := PodLabelsPath(resourceType); ok {
			t.Errorf("PodLabelsPath(%s) = %v, want none", resourceType, got)
		}
	}
	types := PodLabelsResourceTypes()
	if !slices.Contains(types, "v1/Pod") || !slices.Contains(types, "batch/v1/Job") || slices.Contains(types, "v1/PodTemplate") {
		t.Errorf("PodLabelsResourceTypes() = %v", types)
	}
}
