// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package generic

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/confighub/sdk/core/function/api"
	"github.com/confighub/sdk/core/third_party/gaby"
)

// vet-no-link-findings is the policy hook for Links that could not resolve: whether a Unit
// holding a value its Link can no longer supply may reach a cluster is the Space's question.
func TestVetNoLinkFindings(t *testing.T) {
	parsed, err := gaby.ParseAll([]byte("apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: cm\n"))
	require.NoError(t, err)

	linkID := uuid.New()
	findings := api.LinkFindingList{
		{
			LinkID:   linkID,
			Reason:   api.LinkFindingProvidedNotFound,
			Resource: api.ResourceInfo{ResourceName: "platform/platform-facts", ResourceType: "v1/ConfigMap"},
			Path:     "data.argocdNamespace",
			Entry:    "ManualBindings[0]",
		},
		{LinkID: linkID, Reason: api.LinkFindingResolveFailed, Details: "set-attributes failed"},
	}
	vet := func(findings api.LinkFindingList, reasons string) api.ValidationResult {
		t.Helper()
		var args []api.FunctionArgument
		if reasons != "" {
			args = []api.FunctionArgument{{Value: reasons}}
		}
		_, result, err := genericFnVetNoLinkFindings(&api.FunctionContext{LinkFindings: findings}, parsed, args)
		require.NoError(t, err)
		return result.(api.ValidationResult)
	}

	assert.True(t, vet(nil, "").Passed, "a unit whose links found everything passes")

	result := vet(findings, "")
	assert.False(t, result.Passed)
	require.Len(t, result.Issues, 2)
	assert.Equal(t, "ProvidedNotFound", result.Issues[0].Identifier)
	assert.Contains(t, result.Issues[0].Message, "ManualBindings[0] v1/ConfigMap platform/platform-facts data.argocdNamespace")
	assert.Contains(t, result.Issues[1].Message, "set-attributes failed")

	result = vet(findings, "ResolveFailed")
	assert.False(t, result.Passed)
	require.Len(t, result.Issues, 1, "only the reasons asked about fail")
	assert.True(t, vet(findings, "NeededNotFound").Passed)
}
