// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package cubkit

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/confighub/sdk/core/configkit/yamlkit"
	"github.com/confighub/sdk/core/function/api"
	"github.com/confighub/sdk/core/third_party/gaby"
	"github.com/confighub/sdk/core/workerapi"
)

// A ChangeWorkflow's Stages are keyed by Name, so reordering them and changing one is a change to
// that stage, named by its key, rather than a change to every position after the first move.
func TestStagesMergeByName(t *testing.T) {
	previous := `EntityType: ChangeWorkflow
Slug: release
Stages:
- Name: dev
  WhereSpace: "Labels.Environment = 'dev'"
- Name: staging
  WhereSpace: "Labels.Environment = 'staging'"
- Name: prod
  WhereSpace: "Labels.Environment = 'prod'"
`
	modified := `EntityType: ChangeWorkflow
Slug: release
Stages:
- Name: dev
  WhereSpace: "Labels.Environment = 'dev'"
- Name: prod
  WhereSpace: "Labels.Environment IN ('prod', 'dr')"
- Name: staging
  WhereSpace: "Labels.Environment = 'staging'"
`
	previousDocs, err := gaby.ParseAll([]byte(previous))
	require.NoError(t, err)
	modifiedDocs, err := gaby.ParseAll([]byte(modified))
	require.NoError(t, err)
	mutations, err := yamlkit.ComputeMutations(previousDocs, modifiedDocs, 1, NewConfigHubResourceProvider())
	require.NoError(t, err)
	require.Len(t, mutations, 1)

	var paths []string
	for path := range mutations[0].PathMutationMap {
		paths = append(paths, string(path))
	}
	require.Len(t, paths, 1, "paths: %v", paths)
	assert.True(t, strings.HasPrefix(paths[0], "Stages.?Name=prod"), "path: %s", paths[0])
	assert.True(t, strings.HasSuffix(paths[0], ".WhereSpace"), "path: %s", paths[0])

	// Without the specs the same edit is positional: every stage from the first move on reads
	// as changed.
	unkeyed := &ConfigHubResourceProviderType{
		ResourceProviderRegistry: yamlkit.NewResourceProviderRegistry(workerapi.ToolchainConfigHubYAML),
	}
	mutations, err = yamlkit.ComputeMutations(previousDocs, modifiedDocs, 1, unkeyed)
	require.NoError(t, err)
	require.Len(t, mutations, 1)
	assert.Greater(t, len(mutations[0].PathMutationMap), 1)
}

func parseDocs(t *testing.T, data string) gaby.Container {
	t.Helper()
	docs, err := gaby.ParseAll([]byte(data))
	require.NoError(t, err)
	return docs
}

func changedPaths(t *testing.T, previous, modified string) map[string]api.MutationType {
	t.Helper()
	mutations, err := yamlkit.ComputeMutations(parseDocs(t, previous), parseDocs(t, modified), 1, NewConfigHubResourceProvider())
	require.NoError(t, err)
	paths := map[string]api.MutationType{}
	for _, mutation := range mutations {
		for path, info := range mutation.PathMutationMap {
			paths[string(path)] = info.MutationType
		}
	}
	return paths
}

const prerequisitesWorkflow = `EntityType: ChangeWorkflow
Slug: release
Stages:
- Name: prod
  Prerequisites: [%s]
`

// A Stage's Prerequisites are a set: their order means nothing, and any other change replaces
// them whole.
func TestPrerequisitesAreASet(t *testing.T) {
	previous := fmt.Sprintf(prerequisitesWorkflow, "tests-passed, approved")
	assert.Empty(t, changedPaths(t, previous, fmt.Sprintf(prerequisitesWorkflow, "approved, tests-passed")))

	modified := fmt.Sprintf(prerequisitesWorkflow, "approved, soaked, tests-passed")
	paths := changedPaths(t, previous, modified)
	require.Len(t, paths, 1, "paths: %v", paths)
	for path, mutationType := range paths {
		assert.True(t, strings.HasPrefix(path, "Stages.?Name=prod"), "path: %s", path)
		assert.True(t, strings.HasSuffix(path, ".Prerequisites"), "path: %s", path)
		assert.Equal(t, api.MutationTypeReplace, mutationType)
	}

	provider := NewConfigHubResourceProvider()
	mutations, err := yamlkit.ComputeMutations(parseDocs(t, previous), parseDocs(t, modified), 1, provider)
	require.NoError(t, err)
	patched, conflicts, err := yamlkit.PatchMutations(parseDocs(t, previous), nil, mutations, nil, provider, nil)
	require.NoError(t, err)
	assert.Empty(t, conflicts)
	assert.Equal(t, parseDocs(t, modified)[0].Data(), patched[0].Data())
}

const pipeline = `EntityType: Invocation
Slug: standardize
ToolchainType: Kubernetes/YAML
FunctionInvocations:
- FunctionName: set-annotation
  Arguments:
  - ParameterName: key
    Value: owner
  - ParameterName: value
    Value: %s
- FunctionName: set-replicas
  Arguments:
  - ParameterName: replicas
    Value: 2
`

// An Invocation's steps are atomic: changing one argument of one step replaces the list, and the
// patch made from that change produces the new list on a copy of the old.
func TestFunctionInvocationsAreAtomic(t *testing.T) {
	previous, modified := fmt.Sprintf(pipeline, "platform"), fmt.Sprintf(pipeline, "payments")
	assert.Equal(t, map[string]api.MutationType{"FunctionInvocations": api.MutationTypeReplace},
		changedPaths(t, previous, modified))

	provider := NewConfigHubResourceProvider()
	mutations, err := yamlkit.ComputeMutations(parseDocs(t, previous), parseDocs(t, modified), 1, provider)
	require.NoError(t, err)
	patched, conflicts, err := yamlkit.PatchMutations(parseDocs(t, previous), nil, mutations, nil, provider, nil)
	require.NoError(t, err)
	assert.Empty(t, conflicts)
	assert.Equal(t, parseDocs(t, modified)[0].Data(), patched[0].Data())
}
