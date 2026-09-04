// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package generic

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/confighub/sdk/configkit/k8skit"
	"github.com/confighub/sdk/core/configkit/yamlkit"
	"github.com/confighub/sdk/core/function/api"
	"github.com/confighub/sdk/core/third_party/gaby"
)

const listPathsDeployment = `apiVersion: apps/v1
kind: Deployment
metadata:
  name: web
  namespace: default
  labels:
    app.kubernetes.io/name: web
spec:
  replicas: 3
  template:
    spec:
      containers:
      - name: app
        image: nginx:1.25
        ports:
        - containerPort: 8080
          protocol: TCP
      - name: sidecar
        image: otel:1.0
`

func namedArg(name string, value any) api.FunctionArgument {
	return api.FunctionArgument{ParameterName: name, Value: value}
}

// invokeListPaths runs list-paths over the input with the given named arguments, which is
// the form the handler delivers whether the caller named them or not.
func invokeListPaths(t *testing.T, rp yamlkit.ResourceProvider, input string, args ...api.FunctionArgument) api.AttributeValueList {
	t.Helper()
	parsedData, err := gaby.ParseAll([]byte(input))
	require.NoError(t, err)
	_, output, err := genericFnListPaths(rp, parsedData, args, nil)
	require.NoError(t, err)
	paths, ok := output.(api.AttributeValueList)
	require.True(t, ok, "list-paths should return an AttributeValueList")
	return paths
}

func pathStrings(list api.AttributeValueList) []string {
	paths := make([]string, len(list))
	for i, entry := range list {
		paths[i] = string(entry.Path)
	}
	return paths
}

func entryAtPath(t *testing.T, list api.AttributeValueList, path string) api.AttributeValue {
	t.Helper()
	for _, entry := range list {
		if string(entry.Path) == path {
			return entry
		}
	}
	require.Failf(t, "path not reported", "%s not among %v", path, pathStrings(list))
	return api.AttributeValue{}
}

// The point of the function: a list element is named by its merge key, not its position,
// so the reported path survives an insertion ahead of it and can be pasted into a setter.
func TestListPaths_NamesListElementsByMergeKey(t *testing.T) {
	paths := pathStrings(invokeListPaths(t, k8skit.NewK8sResourceProvider(), listPathsDeployment))

	assert.Contains(t, paths, "spec.template.spec.containers.?name=app.image")
	assert.Contains(t, paths, "spec.template.spec.containers.?name=sidecar.image")
	for _, path := range paths {
		assert.NotContains(t, path, "containers.0", "list elements should not be reported positionally")
		assert.NotContains(t, path, "containers.1", "list elements should not be reported positionally")
	}
}

// An array whose elements are identified by a tuple gets every key in the segment;
// matching a container port on its number alone pairs ports that are not the same port.
func TestListPaths_NamesMultiKeyListElements(t *testing.T) {
	list := invokeListPaths(t, k8skit.NewK8sResourceProvider(), listPathsDeployment)
	assert.Contains(t, pathStrings(list),
		"spec.template.spec.containers.?name=app.ports.?containerPort=8080,protocol=TCP.containerPort")
}

// Dots inside a key are escaped, since dots separate segments.
func TestListPaths_EscapesDotsInMapKeys(t *testing.T) {
	list := invokeListPaths(t, k8skit.NewK8sResourceProvider(), listPathsDeployment)
	assert.Contains(t, pathStrings(list), "metadata.labels.app~1kubernetes~1io/name")
}

// Maps and lists are not reported by default: they are most of the rows and almost never
// the row the caller is looking for.
func TestListPaths_ReportsLeavesOnlyByDefault(t *testing.T) {
	paths := pathStrings(invokeListPaths(t, k8skit.NewK8sResourceProvider(), listPathsDeployment))

	assert.NotContains(t, paths, "metadata")
	assert.NotContains(t, paths, "spec.template.spec.containers")
	assert.Contains(t, paths, "metadata.name")
}

func TestListPaths_IncludeSubtreesReportsMapsAndLists(t *testing.T) {
	list := invokeListPaths(t, k8skit.NewK8sResourceProvider(), listPathsDeployment, namedArg("include-subtrees", true))
	paths := pathStrings(list)

	assert.Contains(t, paths, "metadata")
	assert.Contains(t, paths, "spec.template.spec.containers")
	assert.Contains(t, paths, "spec.template.spec.containers.?name=app")

	// A subtree row is a target for set-path, which takes YAML, and has no scalar value.
	containers := entryAtPath(t, list, "spec.template.spec.containers")
	assert.Equal(t, api.DataTypeYAML, containers.DataType)
	assert.Nil(t, containers.Value)
}

// The resource itself is not one of its own paths.
func TestListPaths_OmitsResourceRoot(t *testing.T) {
	list := invokeListPaths(t, k8skit.NewK8sResourceProvider(), listPathsDeployment, namedArg("include-subtrees", true))
	assert.NotContains(t, pathStrings(list), "")
}

func TestListPaths_PathPrefixSelectsSubtree(t *testing.T) {
	list := invokeListPaths(t, k8skit.NewK8sResourceProvider(), listPathsDeployment, namedArg("path-prefix", "spec.template"))
	paths := pathStrings(list)

	require.NotEmpty(t, paths)
	for _, path := range paths {
		assert.True(t, strings.HasPrefix(path, "spec.template"), "%s is outside the prefix", path)
	}
	assert.Contains(t, paths, "spec.template.spec.containers.?name=app.image")
}

// A prefix is matched by whole segments, so it does not select a sibling that merely
// starts with the same characters.
func TestListPaths_PathPrefixMatchesWholeSegments(t *testing.T) {
	const input = `apiVersion: v1
kind: ConfigMap
metadata:
  name: cm
data:
  select: a
  selectorPolicy: b
`
	list := invokeListPaths(t, k8skit.NewK8sResourceProvider(), input, namedArg("path-prefix", "data.select"))
	assert.Equal(t, []string{"data.select"}, pathStrings(list))
}

// Either spelling of a list element works as a prefix: the associative form a caller
// copied out of an earlier result, and the index they read off the raw YAML.
func TestListPaths_PathPrefixAcceptsEitherListSpelling(t *testing.T) {
	rp := k8skit.NewK8sResourceProvider()

	byMergeKey := invokeListPaths(t, rp, listPathsDeployment, namedArg("path-prefix", "spec.template.spec.containers.?name=app"))
	assert.Equal(t, []string{
		"spec.template.spec.containers.?name=app.name",
		"spec.template.spec.containers.?name=app.image",
		"spec.template.spec.containers.?name=app.ports.?containerPort=8080,protocol=TCP.containerPort",
		"spec.template.spec.containers.?name=app.ports.?containerPort=8080,protocol=TCP.protocol",
	}, pathStrings(byMergeKey))

	byIndex := invokeListPaths(t, rp, listPathsDeployment, namedArg("path-prefix", "spec.template.spec.containers.0"))
	assert.Equal(t, pathStrings(byMergeKey), pathStrings(byIndex),
		"the indexed prefix should select the same paths, still reported by merge key")
}

func TestListPaths_DepthLimitsSegments(t *testing.T) {
	paths := pathStrings(invokeListPaths(t, k8skit.NewK8sResourceProvider(), listPathsDeployment, namedArg("depth", 2)))

	assert.Equal(t, []string{
		"apiVersion",
		"kind",
		"metadata.name",
		"metadata.namespace",
		"spec.replicas",
	}, paths)
}

func TestListPaths_RejectsNegativeDepth(t *testing.T) {
	parsedData, err := gaby.ParseAll([]byte(listPathsDeployment))
	require.NoError(t, err)
	_, _, err = genericFnListPaths(k8skit.NewK8sResourceProvider(), parsedData,
		[]api.FunctionArgument{namedArg("depth", -1)}, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "depth")
}

// Arguments are read by name, so a later optional parameter can be supplied without the
// ones before it, and the order they arrive in does not matter.
func TestListPaths_ReadsArgumentsByName(t *testing.T) {
	rp := k8skit.NewK8sResourceProvider()

	list := invokeListPaths(t, rp, listPathsDeployment,
		namedArg("include-subtrees", true),
		namedArg("depth", 1))
	assert.Equal(t, []string{"apiVersion", "kind", "metadata", "spec"}, pathStrings(list))
}

func TestListPaths_ReportsDataTypes(t *testing.T) {
	list := invokeListPaths(t, k8skit.NewK8sResourceProvider(), listPathsDeployment)

	assert.Equal(t, api.DataTypeString, entryAtPath(t, list, "metadata.name").DataType)
	assert.Equal(t, api.DataTypeInt, entryAtPath(t, list, "spec.replicas").DataType)
	assert.Equal(t, 3, entryAtPath(t, list, "spec.replicas").Value)
}

// A value YAML admits but the DataType vocabulary cannot name is reported as no type
// rather than as a type that would send the caller to the wrong setter.
func TestListPaths_ReportsUnnamedScalarTypesAsNone(t *testing.T) {
	const input = `apiVersion: v1
kind: ConfigMap
metadata:
  name: cm
data:
  ratio: 0.75
  empty:
`
	list := invokeListPaths(t, k8skit.NewK8sResourceProvider(), input)

	assert.Equal(t, api.DataTypeNone, entryAtPath(t, list, "data.ratio").DataType)
	assert.Equal(t, api.DataTypeNone, entryAtPath(t, list, "data.empty").DataType)
}

func TestListPaths_ReportsEveryResource(t *testing.T) {
	const input = listPathsDeployment + `---
apiVersion: v1
kind: Service
metadata:
  name: web
  namespace: default
spec:
  clusterIP: 10.0.0.1
`
	list := invokeListPaths(t, k8skit.NewK8sResourceProvider(), input)

	service := entryAtPath(t, list, "spec.clusterIP")
	assert.Equal(t, api.ResourceType("v1/Service"), service.ResourceType)
	assert.Equal(t, api.ResourceName("default/web"), service.ResourceName)

	deploymentReplicas := entryAtPath(t, list, "spec.replicas")
	assert.Equal(t, api.ResourceType("apps/v1/Deployment"), deploymentReplicas.ResourceType)
}

// Comments lifted into the data as $comment$ keys are not paths. Reporting one would
// hand the caller a path that writes a comment, and the comment is already carried on
// the field it belongs to.
func TestListPaths_SkipsCommentKeys(t *testing.T) {
	const input = `apiVersion: v1
kind: ConfigMap
metadata:
  name: cm
data:
  key: value # why this value
`
	parsedData, err := gaby.ParseAll([]byte(input))
	require.NoError(t, err)
	for _, doc := range parsedData {
		require.NoError(t, doc.ExtractCommentsToKeys())
	}
	_, output, err := genericFnListPaths(k8skit.NewK8sResourceProvider(), parsedData, nil, nil)
	require.NoError(t, err)
	list := output.(api.AttributeValueList)

	for _, path := range pathStrings(list) {
		assert.NotContains(t, path, "$comment$")
	}
	assert.Contains(t, entryAtPath(t, list, "data.key").Comment, "why this value")
}

// A path bound to a registered attribute is reported with that attribute's name, which is
// the answer to "which setter do I use" -- the caller who sees one does not need the path.
func TestListPaths_ReportsRegisteredAttributeName(t *testing.T) {
	rp := k8skit.NewK8sResourceProvider()
	const imagePath = "spec.template.spec.containers.*.image"
	yamlkit.RegisterPathsByAttributeName(rp, api.AttributeNameContainerImage,
		api.ResourceType("apps/v1/Deployment"),
		api.PathToVisitorInfoType{
			api.UnresolvedPath(imagePath): {
				Path:          api.UnresolvedPath(imagePath),
				AttributeName: api.AttributeNameContainerImage,
				DataType:      api.DataTypeString,
			},
		}, nil, false, false)

	list := invokeListPaths(t, rp, listPathsDeployment)

	image := entryAtPath(t, list, "spec.template.spec.containers.?name=app.image")
	assert.Equal(t, api.AttributeNameContainerImage, image.AttributeName)
	assert.Equal(t, api.AttributeNameNone, entryAtPath(t, list, "metadata.name").AttributeName)
}

func TestListPaths_AttributesOnlyReportsBoundPathsOnly(t *testing.T) {
	rp := k8skit.NewK8sResourceProvider()
	const imagePath = "spec.template.spec.containers.*.image"
	yamlkit.RegisterPathsByAttributeName(rp, api.AttributeNameContainerImage,
		api.ResourceType("apps/v1/Deployment"),
		api.PathToVisitorInfoType{
			api.UnresolvedPath(imagePath): {
				Path:          api.UnresolvedPath(imagePath),
				AttributeName: api.AttributeNameContainerImage,
				DataType:      api.DataTypeString,
			},
		}, nil, false, false)

	list := invokeListPaths(t, rp, listPathsDeployment, namedArg("attributes-only", true))

	assert.Equal(t, []string{
		"spec.template.spec.containers.?name=app.image",
		"spec.template.spec.containers.?name=sidecar.image",
	}, pathStrings(list))
}

// list-paths does not change the data it walks.
func TestListPaths_LeavesDataAlone(t *testing.T) {
	parsedData, err := gaby.ParseAll([]byte(listPathsDeployment))
	require.NoError(t, err)
	before := parsedData.String()

	result, _, err := genericFnListPaths(k8skit.NewK8sResourceProvider(), parsedData,
		[]api.FunctionArgument{namedArg("include-subtrees", true)}, nil)
	require.NoError(t, err)
	assert.Equal(t, before, result.String())
}
