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

const upsertTargetYAML = `apiVersion: v1
kind: Namespace
metadata:
  name: myns
---
apiVersion: v1
kind: ConfigMap
metadata:
  name: app
  namespace: myns
data:
  key: old
`

func configMapResource(name, value string) api.Resource {
	namespace, short := "", name
	for i := range name {
		if name[i] == '/' {
			namespace, short = name[:i], name[i+1:]
		}
	}
	body := "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: " + short + "\n"
	if namespace != "" {
		body += "  namespace: " + namespace + "\n"
	}
	body += "data:\n  key: " + value + "\n"
	return api.Resource{
		ResourceInfo: api.ResourceInfo{ResourceType: "v1/ConfigMap", ResourceName: api.ResourceName(name)},
		ResourceBody: body,
	}
}

func resourceNames(t *testing.T, parsedData gaby.Container) []string {
	t.Helper()
	resources, _, err := yamlkit.SelectedResources(parsedData, k8skit.NewK8sResourceProvider(), nil)
	require.NoError(t, err)
	names := make([]string, len(resources))
	for i := range resources {
		names[i] = string(resources[i].ResourceType) + " " + string(resources[i].ResourceName)
	}
	return names
}

func TestUpsertResources(t *testing.T) {
	rp := k8skit.NewK8sResourceProvider()

	t.Run("ReplacesByNameAndAppendsNewNames", func(t *testing.T) {
		parsedData, err := gaby.ParseAll([]byte(upsertTargetYAML))
		require.NoError(t, err)

		result, _, err := upsertResources(rp, nil, parsedData, api.ResourceList{
			configMapResource("myns/app", "new"),
			configMapResource("myns/app-6ff529fhm4", "hashed"),
		})
		require.NoError(t, err)
		assert.Equal(t, []string{"v1/Namespace /myns", "v1/ConfigMap myns/app", "v1/ConfigMap myns/app-6ff529fhm4"},
			resourceNames(t, result))
		assert.Contains(t, result[1].String(), "key: new")
		// The input is left as it was.
		assert.Contains(t, parsedData[1].String(), "key: old")
	})

	t.Run("FindsAResourceAVariantMovedToItsNamespace", func(t *testing.T) {
		parsedData, err := gaby.ParseAll([]byte(upsertTargetYAML))
		require.NoError(t, err)

		result, _, err := upsertResources(rp, nil, parsedData, api.ResourceList{
			configMapResource("confighubplaceholder/app", "new"),
		})
		require.NoError(t, err)
		require.Len(t, result, 2)
		assert.Contains(t, result[1].String(), "key: new")
	})

	t.Run("NameWithoutScopeMustNameOneListedResource", func(t *testing.T) {
		parsedData, err := gaby.ParseAll([]byte(upsertTargetYAML))
		require.NoError(t, err)

		// Neither blue/app nor green/app is myns/app: both are appended.
		result, _, err := upsertResources(rp, nil, parsedData, api.ResourceList{
			configMapResource("blue/app", "blue"),
			configMapResource("green/app", "green"),
		})
		require.NoError(t, err)
		assert.Equal(t, []string{"v1/Namespace /myns", "v1/ConfigMap myns/app", "v1/ConfigMap blue/app", "v1/ConfigMap green/app"},
			resourceNames(t, result))
	})

	t.Run("RefusesAResourceListedTwice", func(t *testing.T) {
		parsedData, err := gaby.ParseAll([]byte(upsertTargetYAML))
		require.NoError(t, err)

		_, _, err = upsertResources(rp, nil, parsedData, api.ResourceList{
			configMapResource("myns/other", "a"),
			configMapResource("myns/other", "b"),
		})
		require.Error(t, err)
	})

	t.Run("EmptyListChangesNothing", func(t *testing.T) {
		parsedData, err := gaby.ParseAll([]byte(upsertTargetYAML))
		require.NoError(t, err)

		result, _, err := upsertResources(rp, nil, parsedData, nil)
		require.NoError(t, err)
		assert.Equal(t, resourceNames(t, parsedData), resourceNames(t, result))
	})
}

func TestDeleteResources(t *testing.T) {
	rp := k8skit.NewK8sResourceProvider()

	t.Run("DeletesWhatIsPresentAndSkipsWhatIsNot", func(t *testing.T) {
		parsedData, err := gaby.ParseAll([]byte(upsertTargetYAML))
		require.NoError(t, err)

		result, _, err := deleteResources(rp, nil, parsedData, api.ResourceInfoList{
			{ResourceType: "v1/ConfigMap", ResourceName: "myns/app"},
			{ResourceType: "v1/ConfigMap", ResourceName: "myns/gone"},
		})
		require.NoError(t, err)
		assert.Equal(t, []string{"v1/Namespace /myns"}, resourceNames(t, result))

		// Deleting again changes nothing.
		again, _, err := deleteResources(rp, nil, result, api.ResourceInfoList{
			{ResourceType: "v1/ConfigMap", ResourceName: "myns/app"},
		})
		require.NoError(t, err)
		assert.Equal(t, resourceNames(t, result), resourceNames(t, again))
	})

	t.Run("FindsAResourceAVariantMovedToItsNamespace", func(t *testing.T) {
		parsedData, err := gaby.ParseAll([]byte(upsertTargetYAML))
		require.NoError(t, err)

		result, _, err := deleteResources(rp, nil, parsedData, api.ResourceInfoList{
			{ResourceType: "v1/ConfigMap", ResourceName: "confighubplaceholder/app"},
		})
		require.NoError(t, err)
		assert.Equal(t, []string{"v1/Namespace /myns"}, resourceNames(t, result))
	})

	t.Run("DeleteResourceIsANoOpForAMissingResource", func(t *testing.T) {
		parsedData, err := gaby.ParseAll([]byte(upsertTargetYAML))
		require.NoError(t, err)

		result, _, err := genericFnDeleteResource(rp, nil, nil, parsedData, []api.FunctionArgument{
			{Value: "v1/ConfigMap"}, {Value: "myns/gone"},
		})
		require.NoError(t, err)
		assert.Equal(t, resourceNames(t, parsedData), resourceNames(t, result))
	})
}
