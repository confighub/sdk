// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package api

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/confighub/sdk/core/workerapi"
)

// TestToolchainAnyIsNotASupportedToolchain guards an invariant the system relies on:
// ToolchainAny ("*") is a wildcard that may appear only on a Target/worker ConfigType,
// never as a Unit's toolchain or a function-executor dispatch key. That separation holds
// precisely because ToolchainAny is absent from SupportedToolchains — the same map used
// both for function-executor routing and (via IsSupportedToolchain) for the Unit
// toolchain allowlist in unit.go's isValidToolchainType. If a future change adds
// ToolchainAny here (e.g. to wire up a function-side "any"), Units would silently start
// accepting "*"; this test forces that decision to be deliberate.
func TestToolchainAnyIsNotASupportedToolchain(t *testing.T) {
	assert.NotContains(t, SupportedToolchains, workerapi.ToolchainAny,
		"ToolchainAny must not be a supported toolchain; it would then become assignable to Units")
	assert.False(t, IsSupportedToolchain(workerapi.ToolchainAny),
		"IsSupportedToolchain(ToolchainAny) must be false so Unit create/update rejects it")
}
