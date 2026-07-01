// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package generic

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/confighub/sdk/configkit/k8skit"
	"github.com/confighub/sdk/core/function/api"
	"github.com/confighub/sdk/core/third_party/gaby"
)

const setPathDeployment = `apiVersion: apps/v1
kind: Deployment
metadata:
  name: web
  namespace: default
spec:
  template:
    spec:
      containers:
      - name: app
        image: nginx:1.25
`

// invokeSetPath runs GenericFnSetPath (the set-path function) over the input with
// ResourceTypeAny (as the registered function does) and returns the result YAML.
func invokeSetPath(t *testing.T, input, path, value string) string {
	t.Helper()
	rp := k8skit.NewK8sResourceProvider()
	parsedData, err := gaby.ParseAll([]byte(input))
	require.NoError(t, err)
	args := []api.FunctionArgument{
		{Value: string(api.ResourceTypeAny)},
		{Value: path},
		{Value: value},
	}
	result, _, err := GenericFnSetPath(rp, nil, parsedData, args, true, nil)
	require.NoError(t, err)
	return result.String()
}

// containers returns the container docs of the first resource in a YAML string.
func containers(t *testing.T, yamlStr string) []*gaby.YamlDoc {
	t.Helper()
	docs, err := gaby.ParseAll([]byte(yamlStr))
	require.NoError(t, err)
	require.NotEmpty(t, docs)
	c := docs[0].Path("spec.template.spec.containers")
	require.NotNil(t, c, "containers path should exist")
	return c.Children()
}

func containerByName(t *testing.T, cs []*gaby.YamlDoc, name string) *gaby.YamlDoc {
	t.Helper()
	for _, c := range cs {
		if n := c.S("name"); n != nil && n.Data() == name {
			return c
		}
	}
	return nil
}

// A terminal ?key=value that matches nothing appends a new element, with the
// merge key injected from the path even though the value omits it.
func TestSetPath_AppendsContainer(t *testing.T) {
	out := invokeSetPath(t, setPathDeployment,
		"spec.template.spec.containers.?name=otel-collector",
		"image: otel/opentelemetry-collector:0.100")

	cs := containers(t, out)
	assert.Equal(t, 2, len(cs), "a new container should be appended")
	otel := containerByName(t, cs, "otel-collector")
	require.NotNil(t, otel, "appended container carries the injected merge key name=otel-collector")
	assert.Equal(t, "otel/opentelemetry-collector:0.100", otel.S("image").Data())
	require.NotNil(t, containerByName(t, cs, "app"), "existing container is preserved")
}

// Re-running the same set-path finds the element and replaces it — no duplicate.
func TestSetPath_AppendIsIdempotent(t *testing.T) {
	once := invokeSetPath(t, setPathDeployment,
		"spec.template.spec.containers.?name=otel-collector",
		"image: otel/opentelemetry-collector:0.100")
	twice := invokeSetPath(t, once,
		"spec.template.spec.containers.?name=otel-collector",
		"image: otel/opentelemetry-collector:0.101")

	cs := containers(t, twice)
	assert.Equal(t, 2, len(cs), "re-run must not duplicate the container")
	otel := containerByName(t, cs, "otel-collector")
	require.NotNil(t, otel)
	assert.Equal(t, "otel/opentelemetry-collector:0.101", otel.S("image").Data(), "re-run updates in place")
}

// A terminal ?key=value that matches replaces the whole element (upsert = set all
// fields); the merge key is preserved via injection.
func TestSetPath_ReplacesExistingContainer(t *testing.T) {
	out := invokeSetPath(t, setPathDeployment,
		"spec.template.spec.containers.?name=app",
		"image: nginx:1.27")

	cs := containers(t, out)
	assert.Equal(t, 1, len(cs), "no new container")
	app := containerByName(t, cs, "app")
	require.NotNil(t, app, "merge key name=app is preserved via injection")
	assert.Equal(t, "nginx:1.27", app.S("image").Data())
}

// A non-terminal associative path (a field edit) must NOT append a partial
// element when the element is absent — it is a no-op.
func TestSetPath_NonTerminalNoAppend(t *testing.T) {
	out := invokeSetPath(t, setPathDeployment,
		"spec.template.spec.containers.?name=absent.image",
		"nginx:9")

	cs := containers(t, out)
	assert.Equal(t, 1, len(cs), "no container should be appended for a field edit on an absent element")
	assert.Nil(t, containerByName(t, cs, "absent"))
}

// set-attributes with a DataType=YAML attribute goes through the same document
// setter and appends the sidecar.
func TestSetAttributes_YAMLAppendsContainer(t *testing.T) {
	rp := k8skit.NewK8sResourceProvider()
	parsedData, err := gaby.ParseAll([]byte(setPathDeployment))
	require.NoError(t, err)

	attrList := `[{"ResourceType":"apps/v1/Deployment","Path":"spec.template.spec.containers.?name=otel-collector","AttributeName":"otel","DataType":"YAML","Value":"image: otel/col:1"}]`
	result, _, err := genericFnSetAttributes(rp, nil, parsedData, []api.FunctionArgument{{Value: attrList}}, nil)
	require.NoError(t, err)

	cs := containers(t, result.String())
	assert.Equal(t, 2, len(cs))
	otel := containerByName(t, cs, "otel-collector")
	require.NotNil(t, otel)
	assert.Equal(t, "otel/col:1", otel.S("image").Data())
}
