// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package yamlkit

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/confighub/sdk/core/function/api"
	"github.com/confighub/sdk/core/third_party/gaby"
)

// IS NULL holds of a resource with nothing at the path -- no annotations at all, or others but
// not this one -- and IS NOT NULL of one with something there, whatever its value.
func TestMatchesWhereResourceNullTests(t *testing.T) {
	const key = "metadata.annotations.exceptions~1guardrails~1confighub~1com/no-rbac-wildcards"
	docs := map[string]string{
		"none":     "kind: ClusterRole\nmetadata:\n  name: none\n",
		"other":    "kind: ClusterRole\nmetadata:\n  name: other\n  annotations:\n    team: x\n",
		"excepted": "kind: ClusterRole\nmetadata:\n  name: excepted\n  annotations:\n    exceptions.guardrails.confighub.com/no-rbac-wildcards: argocd manages every resource\n",
		"empty":    "kind: ClusterRole\nmetadata:\n  name: empty\n  annotations:\n    exceptions.guardrails.confighub.com/no-rbac-wildcards: \"\"\n",
	}
	match := func(name, where string) bool {
		t.Helper()
		doc, err := gaby.ParseYAML([]byte(docs[name]))
		require.NoError(t, err)
		exprs, err := api.ParseAndValidateWhereResource(where)
		require.NoError(t, err)
		matched, err := MatchesWhereResourceExpressions(doc, &api.ResourceInfo{ResourceType: "rbac.authorization.k8s.io/v1/ClusterRole"}, exprs)
		require.NoError(t, err)
		return matched
	}
	for name, excepted := range map[string]bool{"none": false, "other": false, "excepted": true, "empty": true} {
		assert.Equal(t, !excepted, match(name, key+" IS NULL"), "%s IS NULL", name)
		assert.Equal(t, excepted, match(name, key+" IS NOT NULL"), "%s IS NOT NULL", name)
	}
	// Conjoined with a comparison, both must hold.
	assert.True(t, match("other", "kind = 'ClusterRole' AND "+key+" IS NULL"))
	assert.False(t, match("other", "kind = 'Role' AND "+key+" IS NULL"))
}
