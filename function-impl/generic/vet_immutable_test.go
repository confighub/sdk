// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package generic

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/confighub/sdk/configkit/k8skit"
	"github.com/confighub/sdk/core/configkit/yamlkit"
	"github.com/confighub/sdk/core/function/api"
	"github.com/confighub/sdk/core/third_party/gaby"
)

// testImmutableRP creates a resource provider with immutable paths registered for testing.
func testImmutableRP() *k8skit.K8sResourceProviderType {
	rp := k8skit.NewK8sResourceProvider()
	// Register immutable paths for Deployment (spec.selector)
	for resourceType, paths := range k8skit.GetImmutablePaths() {
		pathInfos := make(api.PathToVisitorInfoType, len(paths))
		for _, path := range paths {
			unresolvedPath := api.UnresolvedPath(path)
			pathInfos[unresolvedPath] = &api.PathVisitorInfo{
				Path:          unresolvedPath,
				AttributeName: k8skit.AttributeNameImmutable,
				DataType:      api.DataTypeYAML,
			}
		}
		yamlkit.RegisterPathsByAttributeName(
			rp,
			k8skit.AttributeNameImmutable,
			resourceType,
			pathInfos,
			nil, false, false,
		)
	}
	return rp
}

const deploymentCurrent = `apiVersion: apps/v1
kind: Deployment
metadata:
  name: nginx
  namespace: default
spec:
  replicas: 5
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
        image: nginx:1.22
`

const deploymentLive = `apiVersion: apps/v1
kind: Deployment
metadata:
  name: nginx
  namespace: default
spec:
  replicas: 3
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
`

const deploymentSelectorChanged = `apiVersion: apps/v1
kind: Deployment
metadata:
  name: nginx
  namespace: default
spec:
  replicas: 3
  selector:
    matchLabels:
      app: nginx-v2
  template:
    metadata:
      labels:
        app: nginx-v2
    spec:
      containers:
      - name: nginx
        image: nginx:1.21
`

func TestVetImmutable_NoOtherData(t *testing.T) {
	rp := testImmutableRP()
	currentDocs, err := gaby.ParseAll([]byte(deploymentCurrent))
	require.NoError(t, err)

	result, err := GenericVetImmutable(rp, currentDocs, nil, k8skit.AttributeNameImmutable, false, nil)
	require.NoError(t, err)
	assert.True(t, result.Passed, "should pass when no OtherData is provided")
}

func TestVetImmutable_IdenticalData(t *testing.T) {
	rp := testImmutableRP()
	liveDocs, err := gaby.ParseAll([]byte(deploymentLive))
	require.NoError(t, err)
	currentDocs, err := gaby.ParseAll([]byte(deploymentLive))
	require.NoError(t, err)

	otherData := map[api.OtherDataSource]gaby.Container{
		"LastReleasedRevisionNum": liveDocs,
	}

	result, err := GenericVetImmutable(rp, currentDocs, otherData, k8skit.AttributeNameImmutable, false, nil)
	require.NoError(t, err)
	assert.True(t, result.Passed, "should pass when data is identical")
}

func TestVetImmutable_MutableFieldChanged(t *testing.T) {
	rp := testImmutableRP()
	// deploymentCurrent has replicas=5 and image=nginx:1.22 compared to live's replicas=3 and image=nginx:1.21
	// replicas and image are mutable fields for Deployments
	currentDocs, err := gaby.ParseAll([]byte(deploymentCurrent))
	require.NoError(t, err)
	liveDocs, err := gaby.ParseAll([]byte(deploymentLive))
	require.NoError(t, err)

	otherData := map[api.OtherDataSource]gaby.Container{
		"LastReleasedRevisionNum": liveDocs,
	}

	result, err := GenericVetImmutable(rp, currentDocs, otherData, k8skit.AttributeNameImmutable, false, nil)
	require.NoError(t, err)
	assert.True(t, result.Passed, "should pass when only mutable fields changed")
}

func TestVetImmutable_ImmutableFieldChanged(t *testing.T) {
	rp := testImmutableRP()
	// deploymentSelectorChanged has spec.selector changed, which is immutable for Deployments
	currentDocs, err := gaby.ParseAll([]byte(deploymentSelectorChanged))
	require.NoError(t, err)
	liveDocs, err := gaby.ParseAll([]byte(deploymentLive))
	require.NoError(t, err)

	otherData := map[api.OtherDataSource]gaby.Container{
		"LastReleasedRevisionNum": liveDocs,
	}

	result, err := GenericVetImmutable(rp, currentDocs, otherData, k8skit.AttributeNameImmutable, false, nil)
	require.NoError(t, err)
	assert.False(t, result.Passed, "should fail when immutable field changed")
	require.NotEmpty(t, result.FailedAttributes, "should report failed attributes")

	// Check that the failed attribute is the selector path
	found := false
	for _, attr := range result.FailedAttributes {
		if string(attr.Path) == "spec.selector" {
			found = true
			assert.Equal(t, api.ResourceType("apps/v1/Deployment"), attr.ResourceType)
			require.NotEmpty(t, attr.Issues)
			assert.Equal(t, "immutable-field-changed", attr.Issues[0].Identifier)
			break
		}
	}
	assert.True(t, found, "should report spec.selector as changed immutable field")
}

func TestVetImmutable_NewResource(t *testing.T) {
	rp := testImmutableRP()

	// Current has two resources, live has only one -- the new one should not fail
	twoDeployments := deploymentLive + `---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: redis
  namespace: default
spec:
  replicas: 1
  selector:
    matchLabels:
      app: redis
  template:
    metadata:
      labels:
        app: redis
    spec:
      containers:
      - name: redis
        image: redis:7
`
	currentDocs, err := gaby.ParseAll([]byte(twoDeployments))
	require.NoError(t, err)
	liveDocs, err := gaby.ParseAll([]byte(deploymentLive))
	require.NoError(t, err)

	otherData := map[api.OtherDataSource]gaby.Container{
		"LastReleasedRevisionNum": liveDocs,
	}

	result, err := GenericVetImmutable(rp, currentDocs, otherData, k8skit.AttributeNameImmutable, false, nil)
	require.NoError(t, err)
	assert.True(t, result.Passed, "should pass when new resource is added")
}

func TestVetImmutable_EmptyOtherDataMap(t *testing.T) {
	rp := testImmutableRP()
	currentDocs, err := gaby.ParseAll([]byte(deploymentCurrent))
	require.NoError(t, err)

	otherData := map[api.OtherDataSource]gaby.Container{}

	result, err := GenericVetImmutable(rp, currentDocs, otherData, k8skit.AttributeNameImmutable, false, nil)
	require.NoError(t, err)
	assert.True(t, result.Passed, "should pass when OtherData map is empty")
}
