// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package yamlkit

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/confighub/sdk/core/function/api"
	"github.com/confighub/sdk/core/third_party/gaby"
)

func TestComputeLinePatch_NoChange(t *testing.T) {
	text := "line one\nline two\nline three\n"
	patch := ComputeLinePatch(text, text)
	assert.Empty(t, patch)
}

func TestComputeLinePatch_SimpleChange(t *testing.T) {
	previous := "line one\nline two\nline three\n"
	modified := "line one\nline TWO\nline three\n"
	patch := ComputeLinePatch(previous, modified)
	assert.NotEmpty(t, patch)
}

func TestApplyLinePatch_ExactMatch(t *testing.T) {
	previous := "line one\nline two\nline three\n"
	modified := "line one\nline TWO\nline three\n"
	patch := ComputeLinePatch(previous, modified)

	result, ok := ApplyLinePatch(previous, patch)
	assert.True(t, ok)
	assert.Equal(t, modified, result)
}

func TestApplyLinePatch_ThreeWayMerge(t *testing.T) {
	// Base version
	base := "line one\nline two\nline three\nline four\n"
	// Upstream changes line two
	upstream := "line one\nline TWO\nline three\nline four\n"
	// Downstream changes line four
	downstream := "line one\nline two\nline three\nline FOUR\n"

	// Compute patch from base -> upstream
	patch := ComputeLinePatch(base, upstream)
	require.NotEmpty(t, patch)

	// Apply upstream patch to downstream (three-way merge)
	result, ok := ApplyLinePatch(downstream, patch)
	assert.True(t, ok)
	// Both changes should be present
	assert.Equal(t, "line one\nline TWO\nline three\nline FOUR\n", result)
}

func TestApplyLinePatch_InsertionMerge(t *testing.T) {
	// Base version
	base := "line one\nline two\nline three\n"
	// Upstream inserts a line after line one
	upstream := "line one\nnew line\nline two\nline three\n"
	// Downstream modifies line three
	downstream := "line one\nline two\nline THREE\n"

	// Compute patch from base -> upstream
	patch := ComputeLinePatch(base, upstream)
	require.NotEmpty(t, patch)

	// Apply upstream insertion to downstream
	result, ok := ApplyLinePatch(downstream, patch)
	assert.True(t, ok)
	// Both the insertion and modification should be present
	assert.Equal(t, "line one\nnew line\nline two\nline THREE\n", result)
}

func TestApplyLinePatch_DeletionMerge(t *testing.T) {
	// Base version
	base := "line one\nline two\nline three\nline four\n"
	// Upstream deletes line two
	upstream := "line one\nline three\nline four\n"
	// Downstream modifies line four
	downstream := "line one\nline two\nline three\nline FOUR\n"

	// Compute patch from base -> upstream
	patch := ComputeLinePatch(base, upstream)
	require.NotEmpty(t, patch)

	// Apply upstream deletion to downstream
	result, ok := ApplyLinePatch(downstream, patch)
	assert.True(t, ok)
	// Deletion and modification should both be present
	assert.Equal(t, "line one\nline three\nline FOUR\n", result)
}

func TestApplyLinePatch_EmptyPatch(t *testing.T) {
	target := "some text\n"
	result, ok := ApplyLinePatch(target, "")
	assert.False(t, ok)
	assert.Equal(t, target, result)
}

func TestApplyLinePatch_InvalidPatch(t *testing.T) {
	target := "some text\n"
	result, ok := ApplyLinePatch(target, "not a valid patch")
	assert.False(t, ok)
	assert.Equal(t, target, result)
}

func TestIsMultiLineString(t *testing.T) {
	assert.False(t, IsMultiLineString("single line"))
	assert.False(t, IsMultiLineString(""))
	assert.False(t, IsMultiLineString("single line\n"))  // trailing newline from gaby serialization
	assert.False(t, IsMultiLineString("\n"))              // just a newline
	assert.True(t, IsMultiLineString("line one\nline two"))
	assert.True(t, IsMultiLineString("line one\nline two\n"))
}

// TestComputeMutationsForDocs_MultiLineStringPatch tests that ComputeMutationsForDocs
// populates the Patch field for multi-line string value changes.
func TestComputeMutationsForDocs_MultiLineStringPatch(t *testing.T) {
	previousYAML := `text: |
  line one
  line two
  line three
`
	modifiedYAML := `text: |
  line one
  line TWO
  line three
`

	previousDoc, err := gaby.ParseYAML([]byte(previousYAML))
	require.NoError(t, err)
	modifiedDoc, err := gaby.ParseYAML([]byte(modifiedYAML))
	require.NoError(t, err)

	pathMutationMap := make(api.MutationMap)
	ComputeMutationsForDocs("", previousDoc, modifiedDoc, 0, pathMutationMap, nil)

	mutation, ok := pathMutationMap["text"]
	require.True(t, ok, "expected mutation at path 'text'")
	assert.Equal(t, api.MutationTypeUpdate, mutation.MutationType)
	assert.NotEmpty(t, mutation.Patch, "expected Patch to be populated for multi-line string")
	assert.Contains(t, mutation.Value, "line TWO")
}

// TestComputeMutationsForDocs_SingleLineNoPatch tests that single-line string changes
// do NOT get a Patch field.
func TestComputeMutationsForDocs_SingleLineNoPatch(t *testing.T) {
	previousYAML := `name: alice`
	modifiedYAML := `name: bob`

	previousDoc, err := gaby.ParseYAML([]byte(previousYAML))
	require.NoError(t, err)
	modifiedDoc, err := gaby.ParseYAML([]byte(modifiedYAML))
	require.NoError(t, err)

	pathMutationMap := make(api.MutationMap)
	ComputeMutationsForDocs("", previousDoc, modifiedDoc, 0, pathMutationMap, nil)

	mutation, ok := pathMutationMap["name"]
	require.True(t, ok)
	assert.Equal(t, api.MutationTypeUpdate, mutation.MutationType)
	assert.Empty(t, mutation.Patch, "single-line string should not get a Patch")
}

// TestPatchMutations_LinePatchThreeWayMerge tests the end-to-end three-way merge via
// ComputeMutations + PatchMutations for multi-line string values.
func TestPatchMutations_LinePatchThreeWayMerge(t *testing.T) {
	// Simulate three-way merge:
	// Base has a multi-line text field
	// Upstream modifies one line
	// Downstream (target) modifies a different line
	// Result should have both changes

	// Use explicit \n in the string values to avoid YAML block scalar indentation issues.
	baseYAML := "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: myconfig\ndata:\n  readme: \"line one\\nline two\\nline three\\nline four\\n\"\n"
	upstreamYAML := "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: myconfig\ndata:\n  readme: \"line one\\nline TWO\\nline three\\nline four\\n\"\n"
	targetYAML := "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: myconfig\ndata:\n  readme: \"line one\\nline two\\nline three\\nline FOUR\\n\"\n"

	expectedReadme := "line one\nline TWO\nline three\nline FOUR\n"

	baseParsed, err := gaby.ParseAll([]byte(baseYAML))
	require.NoError(t, err)
	upstreamParsed, err := gaby.ParseAll([]byte(upstreamYAML))
	require.NoError(t, err)
	targetParsed, err := gaby.ParseAll([]byte(targetYAML))
	require.NoError(t, err)

	// Verify the string values are parsed correctly
	baseReadme := baseParsed[0].Path("data.readme")
	require.NotNil(t, baseReadme)
	require.Equal(t, "line one\nline two\nline three\nline four\n", baseReadme.Data().(string))

	// Compute mutations from base -> upstream
	mutations, err := ComputeMutations(baseParsed, upstreamParsed, 0, testProvider)
	require.NoError(t, err)

	// Verify that the mutation has a Patch field
	require.Len(t, mutations, 1)
	readmeMutation, hasMutation := mutations[0].PathMutationMap["data.readme"]
	require.True(t, hasMutation, "expected mutation at data.readme")
	require.NotEmpty(t, readmeMutation.Patch, "expected Patch field for multi-line string")

	// Apply mutations to target (three-way merge)
	result, err := PatchMutations(targetParsed, nil, mutations, testProvider, nil)
	require.NoError(t, err)
	require.Len(t, result, 1)

	// Extract the readme field value
	readmeNode := result[0].Path("data.readme")
	require.NotNil(t, readmeNode)
	readmeValue, ok := readmeNode.Data().(string)
	require.True(t, ok)
	assert.Equal(t, expectedReadme, readmeValue)
}

// --- Structural Patch Tests ---

func TestComputeScalarPatch_JSON(t *testing.T) {
	previous := `{"name":"alice","age":30,"city":"NYC"}`
	modified := `{"name":"alice","age":31,"city":"NYC"}`

	patch := ComputeScalarPatch(previous, modified)
	require.NotEmpty(t, patch)
	// Should be a structural patch (starts with '{')
	assert.True(t, strings.HasPrefix(patch, "{"), "expected structural JSON patch, got: %s", patch)
	assert.Contains(t, patch, `"format":"json"`)
}

func TestComputeScalarPatch_YAML(t *testing.T) {
	// YAML autodetection is disabled (frontmatter in text documents would be
	// misidentified), so YAML content gets a line-level text patch instead of
	// a structural one.
	previous := "name: alice\nage: 30\ncity: NYC\n"
	modified := "name: alice\nage: 31\ncity: NYC\n"

	patch := ComputeScalarPatch(previous, modified)
	require.NotEmpty(t, patch)
	assert.True(t, strings.HasPrefix(patch, "@@ "), "expected line-level text patch, got: %s", patch)
}

func TestComputeScalarPatch_PlainText(t *testing.T) {
	previous := "line one\nline two\nline three\n"
	modified := "line one\nline TWO\nline three\n"

	patch := ComputeScalarPatch(previous, modified)
	require.NotEmpty(t, patch)
	// Should be a line-level text patch (starts with '@@ ')
	assert.True(t, strings.HasPrefix(patch, "@@ "), "expected line-level text patch, got: %s", patch)
}

func TestApplyScalarPatch_JSONThreeWayMerge(t *testing.T) {
	base := `{"name":"alice","age":30,"city":"NYC"}`
	// Upstream changes age
	upstream := `{"name":"alice","age":31,"city":"NYC"}`
	// Downstream changes city
	downstream := `{"name":"alice","age":30,"city":"LA"}`

	patch := ComputeScalarPatch(base, upstream)
	require.NotEmpty(t, patch)

	result, ok := ApplyScalarPatch(downstream, patch)
	assert.True(t, ok)

	// Both changes should be present: age=31, city=LA
	ja := &JSONAccessor{}
	age, err := ja.Extract(result, "age")
	require.NoError(t, err)
	assert.Equal(t, 31, age)

	city, err := ja.Extract(result, "city")
	require.NoError(t, err)
	assert.Equal(t, "LA", city)
}

func TestApplyScalarPatch_YAMLThreeWayMerge(t *testing.T) {
	// YAML autodetection is disabled, so this uses line-level patching.
	// Line-level three-way merge still works when changes are on different lines.
	base := "name: alice\nage: 30\ncity: NYC\n"
	// Upstream changes age
	upstream := "name: alice\nage: 31\ncity: NYC\n"
	// Downstream changes city
	downstream := "name: alice\nage: 30\ncity: LA\n"

	patch := ComputeScalarPatch(base, upstream)
	require.NotEmpty(t, patch)

	result, ok := ApplyScalarPatch(downstream, patch)
	assert.True(t, ok)

	// Both changes should be present
	assert.Contains(t, result, "age: 31")
	assert.Contains(t, result, "city: LA")
}

func TestApplyScalarPatch_JSONFieldAddition(t *testing.T) {
	base := `{"name":"alice"}`
	upstream := `{"name":"alice","role":"admin"}`
	downstream := `{"name":"bob"}`

	patch := ComputeScalarPatch(base, upstream)
	require.NotEmpty(t, patch)

	result, ok := ApplyScalarPatch(downstream, patch)
	assert.True(t, ok)

	// Downstream's name change and upstream's role addition should both be present
	ja := &JSONAccessor{}
	name, err := ja.Extract(result, "name")
	require.NoError(t, err)
	assert.Equal(t, "bob", name)

	role, err := ja.Extract(result, "role")
	require.NoError(t, err)
	assert.Equal(t, "admin", role)
}

func TestApplyScalarPatch_JSONFieldDeletion(t *testing.T) {
	base := `{"name":"alice","age":30,"city":"NYC"}`
	// Upstream deletes city
	upstream := `{"name":"alice","age":30}`
	// Downstream changes name
	downstream := `{"name":"bob","age":30,"city":"NYC"}`

	patch := ComputeScalarPatch(base, upstream)
	require.NotEmpty(t, patch)

	result, ok := ApplyScalarPatch(downstream, patch)
	assert.True(t, ok)

	ja := &JSONAccessor{}
	name, err := ja.Extract(result, "name")
	require.NoError(t, err)
	assert.Equal(t, "bob", name)

	// city should be deleted
	_, err = ja.Extract(result, "city")
	assert.Error(t, err)
}

func TestPatchMutations_JSONStructuralThreeWayMerge(t *testing.T) {
	// End-to-end: a ConfigMap data field containing JSON
	baseYAML := "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: myconfig\ndata:\n  config: '{\"host\":\"localhost\",\"port\":8080,\"debug\":false}'\n"
	upstreamYAML := "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: myconfig\ndata:\n  config: '{\"host\":\"localhost\",\"port\":9090,\"debug\":false}'\n"
	targetYAML := "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: myconfig\ndata:\n  config: '{\"host\":\"prod.example.com\",\"port\":8080,\"debug\":false}'\n"

	baseParsed, err := gaby.ParseAll([]byte(baseYAML))
	require.NoError(t, err)
	upstreamParsed, err := gaby.ParseAll([]byte(upstreamYAML))
	require.NoError(t, err)
	targetParsed, err := gaby.ParseAll([]byte(targetYAML))
	require.NoError(t, err)

	mutations, err := ComputeMutations(baseParsed, upstreamParsed, 0, testProvider)
	require.NoError(t, err)

	// Verify structural JSON patch was computed
	require.Len(t, mutations, 1)
	configMutation, hasMutation := mutations[0].PathMutationMap["data.config"]
	require.True(t, hasMutation)
	require.True(t, strings.HasPrefix(configMutation.Patch, "{"), "expected structural patch")

	// Apply to target
	result, err := PatchMutations(targetParsed, nil, mutations, testProvider, nil)
	require.NoError(t, err)
	require.Len(t, result, 1)

	configNode := result[0].Path("data.config")
	require.NotNil(t, configNode)
	configValue, ok := configNode.Data().(string)
	require.True(t, ok)

	// Both changes: host=prod.example.com (downstream), port=9090 (upstream)
	ja := &JSONAccessor{}
	host, err := ja.Extract(configValue, "host")
	require.NoError(t, err)
	assert.Equal(t, "prod.example.com", host)

	port, err := ja.Extract(configValue, "port")
	require.NoError(t, err)
	assert.Equal(t, 9090, port)
}
