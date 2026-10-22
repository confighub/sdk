// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package yamlkit

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/confighub/sdk/core/function/api"
)

// A function's AffectedResourceTypes comes from here, so it has to be the same in every process:
// the types in order, or the wildcard alone when it is among them.
func TestResourceTypesForPathMap(t *testing.T) {
	types := map[api.ResourceType][]string{"v1/Service": {"a"}, "apps/v1/Deployment": {"b"}, "v1/ConfigMap": {"c"}}
	withAny := map[api.ResourceType][]string{"v1/Service": {"a"}, api.ResourceTypeAny: {"b"}, "apps/v1/Deployment": {"c"}}
	for range 20 {
		assert.Equal(t, []api.ResourceType{"apps/v1/Deployment", "v1/ConfigMap", "v1/Service"}, ResourceTypesForPathMap(types))
		assert.Equal(t, []api.ResourceType{api.ResourceTypeAny}, ResourceTypesForPathMap(withAny))
	}
}
