// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package k8skit_test

import (
	"testing"

	"github.com/confighub/sdk/configkit/k8skit"
	"github.com/confighub/sdk/core/configkit/yamlkit"
	"github.com/confighub/sdk/core/function/api"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func deploymentWithEnvFrom(configMapName string) string {
	return `apiVersion: apps/v1
kind: Deployment
metadata:
  name: web
  namespace: prod
spec:
  template:
    spec:
      containers:
      - name: web
        image: nginx
        envFrom:
        - configMapRef: {name: ` + configMapName + `}
`
}

var webDeployment = api.ResourceInfo{
	ResourceName:             "prod/web",
	ResourceNameWithoutScope: "web",
	ResourceType:             "apps/v1/Deployment",
}

// An edit inside an element of a list with no merge key (envFrom) is credited to the Mutation that
// made it, however the path names the elements around it: the record names the envFrom element by
// its content, and a caller by its index, with the container by its merge key or its index.
func TestFindMutationIndex_FindsAnEditInAListWithNoMergeKey(t *testing.T) {
	provider := k8skit.NewK8sResourceProvider()
	created := deploymentWithEnvFrom("confighubplaceholder")
	resolved := deploymentWithEnvFrom("web-config")
	edited := deploymentWithEnvFrom("web-config-typo")

	record, err := yamlkit.ComputeMutations(parseConfig(t, ""), parseConfig(t, created), 1, provider)
	require.NoError(t, err)
	record = recordWrite(t, record, created, resolved, 2)
	record = recordWrite(t, record, resolved, edited, 3)

	for _, path := range []api.ResolvedPath{
		"spec.template.spec.containers.?name=web.envFrom.0.configMapRef.name",
		"spec.template.spec.containers.0.envFrom.0.configMapRef.name",
	} {
		index, found := yamlkit.FindMutationIndex(parseConfig(t, edited), record, webDeployment, path, provider)
		assert.True(t, found, path)
		assert.Equal(t, int64(3), index, "%s, record %v", path, keysOf(record))
	}

	// A path nothing has written since the resource was created falls back to its creation.
	index, found := yamlkit.FindMutationIndex(parseConfig(t, edited), record, webDeployment,
		"spec.template.spec.containers.?name=web.image", provider)
	assert.True(t, found)
	assert.Equal(t, int64(1), index)
}
