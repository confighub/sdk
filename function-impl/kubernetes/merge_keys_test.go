// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package kubernetes

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/confighub/sdk/function/api"
	"github.com/confighub/sdk/function-impl/generic"
	"github.com/confighub/sdk/third_party/gaby"
)

const deploymentDuplicateEnvFixture = `apiVersion: apps/v1
kind: Deployment
metadata:
  name: nginx
  namespace: default
spec:
  replicas: 1
  selector:
    matchLabels:
      app: nginx
  template:
    metadata:
      labels:
        app: nginx
    spec:
      containers:
      - name: nginx
        image: nginx:1.21
        env:
        - name: FOO
          value: bar
        - name: BAZ
          value: qux
        - name: FOO
          value: duplicate
`

const deploymentNoDuplicateEnvFixture = `apiVersion: apps/v1
kind: Deployment
metadata:
  name: nginx
  namespace: default
spec:
  replicas: 1
  selector:
    matchLabels:
      app: nginx
  template:
    metadata:
      labels:
        app: nginx
    spec:
      containers:
      - name: nginx
        image: nginx:1.21
        env:
        - name: FOO
          value: bar
        - name: BAZ
          value: qux
`

func TestVetMergeKeys_DuplicateEnvVars(t *testing.T) {
	docs, err := gaby.ParseAll([]byte(deploymentDuplicateEnvFixture))
	require.NoError(t, err)

	_, output, err := generic.GenericFnVetMergeKeys(testResourceProvider, docs)
	require.NoError(t, err)

	vr, ok := output.(api.ValidationResult)
	require.True(t, ok)
	assert.False(t, vr.Passed)
	require.Len(t, vr.FailedAttributes, 1)
	assert.Equal(t, api.ResolvedPath("spec.template.spec.containers.0.env.2.name"), vr.FailedAttributes[0].Path)
	assert.Equal(t, "FOO", vr.FailedAttributes[0].Value)
}

func TestVetMergeKeys_NoDuplicateEnvVars(t *testing.T) {
	docs, err := gaby.ParseAll([]byte(deploymentNoDuplicateEnvFixture))
	require.NoError(t, err)

	_, output, err := generic.GenericFnVetMergeKeys(testResourceProvider, docs)
	require.NoError(t, err)

	vr, ok := output.(api.ValidationResult)
	require.True(t, ok)
	assert.True(t, vr.Passed)
	assert.Empty(t, vr.FailedAttributes)
}
