// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package yamlkit

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/confighub/sdk/core/function/api"
	"github.com/confighub/sdk/core/workerapi"
)

// MergeKeysForPath, ExclusiveFieldsForPath and IsMapKeyPath are one implementation on the
// registry every ResourceProvider embeds, rather than one per toolchain. Eight toolchains used
// to write all three by hand to say "this format has no schema declaring one", which is what a
// provider with no compiled specs says here.

func TestAToolchainWithNoSpecsDeclaresNoStructure(t *testing.T) {
	registry := NewResourceProviderRegistry(workerapi.ToolchainAppConfigTOML)

	keys, found := registry.MergeKeysForPath("Properties", "servers")
	assert.False(t, found)
	assert.Nil(t, keys)

	fields, found := registry.ExclusiveFieldsForPath("Properties", "auth")
	assert.False(t, found)
	assert.Equal(t, ExclusiveFields{}, fields)

	assert.False(t, registry.IsMapKeyPath("Properties", "labels.*"))
}

// And it answers for real the moment its toolchain has specs, which is the whole point of the
// lookups being one implementation over a keyed snapshot rather than eight stubs.
func TestAToolchainWithSpecsAnswersFromThem(t *testing.T) {
	set, err := LoadSpecSet([]byte(`
toolchainType: AppConfig/TOML
resourceTypes:
  - type: Config
    mergeKeys:
      - {path: servers, key: hostname}
    exclusiveFields:
      - path: auth
        members: [token, password]
    mapKeyPaths:
      - labels.*
`))
	require.NoError(t, err)
	compiled, err := CompileSpecSets(set)
	require.NoError(t, err)

	registry := NewResourceProviderRegistryWithSpecs(workerapi.ToolchainAppConfigTOML, compiled)

	keys, found := registry.MergeKeysForPath("Config", "servers")
	assert.True(t, found)
	assert.Equal(t, []string{"hostname"}, keys)

	fields, found := registry.ExclusiveFieldsForPath("Config", "auth")
	assert.True(t, found)
	assert.Equal(t, []string{"token", "password"}, fields.Members)

	assert.True(t, registry.IsMapKeyPath("Config", "labels.*"))

	// The snapshot is keyed by toolchain as well as by type, so one toolchain's declaration is
	// not another's. A resource type means a different thing in each.
	other := NewResourceProviderRegistryWithSpecs(workerapi.ToolchainAppConfigINI, compiled)
	_, found = other.MergeKeysForPath("Config", "servers")
	assert.False(t, found, "the same compiled specs answer only for the toolchain that declared them")
}

// The lookups read the registry the provider was built with, not a package variable, so two
// providers in one process can hold different structure. That is what §5.2 asks for.
func TestTwoProvidersInOneProcessHoldDifferentStructure(t *testing.T) {
	set, err := LoadSpecSet([]byte(`
toolchainType: AppConfig/TOML
resourceTypes:
  - type: Config
    mergeKeys:
      - {path: servers, key: hostname}
`))
	require.NoError(t, err)
	compiled, err := CompileSpecSets(set)
	require.NoError(t, err)

	withSpecs := NewResourceProviderRegistryWithSpecs(workerapi.ToolchainAppConfigTOML, compiled)
	withoutSpecs := NewResourceProviderRegistry(workerapi.ToolchainAppConfigTOML)

	_, found := withSpecs.MergeKeysForPath(api.ResourceType("Config"), "servers")
	assert.True(t, found)
	_, found = withoutSpecs.MergeKeysForPath(api.ResourceType("Config"), "servers")
	assert.False(t, found)
}
