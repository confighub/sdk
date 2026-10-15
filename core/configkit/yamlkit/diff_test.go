// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package yamlkit

import (
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/confighub/sdk/core/function/api"
	"github.com/confighub/sdk/core/third_party/gaby"
)

// diffTestResourceProvider declares `name` the merge key of every containers and env array,
// as the Kubernetes provider does.
type diffTestResourceProvider struct {
	testResourceProvider
}

func (diffTestResourceProvider) MergeKeysForPath(_ api.ResourceType, path string) ([]string, bool) {
	segments := strings.Split(path, ".")
	switch segments[len(segments)-1] {
	case "containers", "initContainers", "env":
		return []string{"name"}, true
	}
	return nil, false
}

var diffTestProvider = &diffTestResourceProvider{testResourceProvider: *testProvider}

func computeTestDiff(t *testing.T, previous, modified string) api.ConfigDiff {
	t.Helper()
	return computeTestDiffWithOptions(t, previous, modified, DiffOptions{})
}

func computeTestDiffWithOptions(t *testing.T, previous, modified string, options DiffOptions) api.ConfigDiff {
	t.Helper()
	previousDocs, err := gaby.ParseAll([]byte(previous))
	require.NoError(t, err)
	modifiedDocs, err := gaby.ParseAll([]byte(modified))
	require.NoError(t, err)
	diff, err := ComputeDiff(previousDocs, modifiedDocs, diffTestProvider, options)
	require.NoError(t, err)
	return diff
}

// computeTestDiffWithComments parses as the toolchains do, lifting comments into comment keys.
func computeTestDiffWithComments(t *testing.T, previous, modified string) api.ConfigDiff {
	t.Helper()
	previousDocs, err := gaby.ParseAll([]byte(previous))
	require.NoError(t, err)
	modifiedDocs, err := gaby.ParseAll([]byte(modified))
	require.NoError(t, err)
	for _, doc := range append(slices.Clone(previousDocs), modifiedDocs...) {
		require.NoError(t, doc.ExtractCommentsToKeys())
	}
	diff, err := ComputeDiff(previousDocs, modifiedDocs, diffTestProvider, DiffOptions{})
	require.NoError(t, err)
	return diff
}

// changeSummary renders a resource's changes compactly for comparison.
func changeSummary(resource api.ResourceDiff) []string {
	var lines []string
	for _, change := range resource.Changes {
		lines = append(lines, string(change.ChangeType)+" "+change.DisplayPath+": "+change.FromValue+" -> "+change.ToValue)
	}
	return lines
}

const diffDeployment = `apiVersion: apps/v1
kind: Deployment
metadata:
  name: web
  namespace: prod
spec:
  replicas: 2
  template:
    spec:
      containers:
      - name: app
        image: app:1
        args: [serve, --port, "8080"]
        env:
        - name: A
          value: a
        - name: B
          value: b
      - name: sidecar
        image: sidecar:1
        imagePullPolicy: Always
        workingDir: /srv
        tty: true
        stdin: true
        terminationMessagePath: /dev/termination-log
        command: [run]
`

func TestComputeDiffScalarUpdate(t *testing.T) {
	diff := computeTestDiff(t, diffDeployment,
		strings.Replace(diffDeployment, "image: app:1", "image: app:2", 1))
	require.Len(t, diff.Resources, 1)
	resource := diff.Resources[0]
	assert.Equal(t, api.MutationTypeUpdate, resource.ChangeType)
	assert.Equal(t, []string{"Update spec.template.spec.containers.?name=app.image: app:1 -> app:2"}, changeSummary(resource))

	segments := resource.Changes[0].Segments
	require.Len(t, segments, 6)
	assert.Equal(t, []api.MergeKeyValue{{Key: "name", Value: "app"}}, segments[4].MergeKeys)
	assert.Equal(t, 0, segments[4].FromIndex)
	assert.Equal(t, 0, segments[4].ToIndex)
	assert.Equal(t, "image", segments[5].Field)
}

func TestComputeDiffUnchanged(t *testing.T) {
	assert.Empty(t, computeTestDiff(t, diffDeployment, diffDeployment).Resources)

	docs, err := gaby.ParseAll([]byte(diffDeployment))
	require.NoError(t, err)
	diff, err := ComputeDiff(docs, docs, diffTestProvider, DiffOptions{IncludeUnchanged: true})
	require.NoError(t, err)
	require.Len(t, diff.Resources, 1)
	assert.Equal(t, api.MutationTypeNone, diff.Resources[0].ChangeType)
}

// Inserting an env var ahead of the others is one Add, not an Update of every later
// element, and it is not a reorder.
func TestComputeDiffMergeKeyedInsert(t *testing.T) {
	diff := computeTestDiff(t, diffDeployment, strings.Replace(diffDeployment,
		"        env:\n        - name: A", "        env:\n        - name: Z\n          value: z\n        - name: A", 1))
	require.Len(t, diff.Resources, 1)
	assert.Equal(t, []string{
		"Add spec.template.spec.containers.?name=app.env.?name=Z:  -> name: Z\nvalue: z",
	}, changeSummary(diff.Resources[0]))
	segment := diff.Resources[0].Changes[0].Segments[6]
	assert.Equal(t, -1, segment.FromIndex)
	assert.Equal(t, 0, segment.ToIndex)
}

func TestComputeDiffMergeKeyedReorderAndUpdate(t *testing.T) {
	modified := strings.Replace(diffDeployment,
		"        - name: A\n          value: a\n        - name: B\n          value: b",
		"        - name: B\n          value: b2\n        - name: A\n          value: a", 1)
	diff := computeTestDiff(t, diffDeployment, modified)
	require.Len(t, diff.Resources, 1)
	assert.Equal(t, []string{
		"Reorder spec.template.spec.containers.?name=app.env: - A\n- B -> - B\n- A",
		"Update spec.template.spec.containers.?name=app.env.?name=B.value: b -> b2",
	}, changeSummary(diff.Resources[0]))
}

func TestComputeDiffMergeKeyedDelete(t *testing.T) {
	modified := strings.Replace(diffDeployment, "        - name: A\n          value: a\n", "", 1)
	diff := computeTestDiff(t, diffDeployment, modified)
	require.Len(t, diff.Resources, 1)
	assert.Equal(t, []string{
		"Delete spec.template.spec.containers.?name=app.env.?name=A: name: A\nvalue: a -> ",
	}, changeSummary(diff.Resources[0]))
}

// A positional array element inserted in the middle is one Add at its new index.
func TestComputeDiffPositionalInsert(t *testing.T) {
	diff := computeTestDiff(t, diffDeployment, strings.Replace(diffDeployment,
		`args: [serve, --port, "8080"]`, `args: [serve, --verbose, --port, "8080"]`, 1))
	require.Len(t, diff.Resources, 1)
	assert.Equal(t, []string{
		"Add spec.template.spec.containers.?name=app.args.1:  -> --verbose",
	}, changeSummary(diff.Resources[0]))
}

// A positional array element edited in place is addressed by an anchor in the mutation
// path, and by its index for display.
func TestComputeDiffPositionalUpdate(t *testing.T) {
	diff := computeTestDiff(t, diffDeployment, strings.Replace(diffDeployment,
		`args: [serve, --port, "8080"]`, `args: [serve, --port, "9090"]`, 1))
	require.Len(t, diff.Resources, 1)
	assert.Equal(t, []string{
		`Update spec.template.spec.containers.?name=app.args.2: 8080 -> 9090`,
	}, changeSummary(diff.Resources[0]))
	assert.Contains(t, string(diff.Resources[0].Changes[0].Path), "?~")
}

func TestComputeDiffElementRename(t *testing.T) {
	diff := computeTestDiff(t, diffDeployment, strings.Replace(diffDeployment,
		"      - name: sidecar\n        image: sidecar:1", "      - name: proxy\n        image: sidecar:2", 1))
	require.Len(t, diff.Resources, 1)
	assert.Equal(t, []string{
		"Rename spec.template.spec.containers.?name=proxy: sidecar -> proxy",
		"Update spec.template.spec.containers.?name=proxy.image: sidecar:1 -> sidecar:2",
	}, changeSummary(diff.Resources[0]))
}

func TestComputeDiffResourceRename(t *testing.T) {
	diff := computeTestDiff(t, diffDeployment, strings.Replace(diffDeployment, "name: web", "name: web2", 1))
	require.Len(t, diff.Resources, 1)
	resource := diff.Resources[0]
	require.NotNil(t, resource.PreviousResource)
	assert.Equal(t, api.ResourceName("prod/web"), resource.PreviousResource.ResourceName)
	assert.Equal(t, api.ResourceName("prod/web2"), resource.Resource.ResourceName)
	assert.Equal(t, []string{"Update metadata.name: web -> web2"}, changeSummary(resource))
}

func TestComputeDiffResourceAddAndDelete(t *testing.T) {
	configMap := "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: settings\n  namespace: prod\ndata:\n  a: \"1\"\n"
	service := "apiVersion: v1\nkind: Service\nmetadata:\n  name: web\n  namespace: prod\nspec:\n  ports:\n  - port: 80\n"
	diff := computeTestDiff(t, configMap+"---\n"+diffDeployment, diffDeployment+"---\n"+service)
	require.Len(t, diff.Resources, 2)
	// The ConfigMap came before the Deployment, which survived, so it is listed first.
	assert.Equal(t, api.MutationTypeDelete, diff.Resources[0].ChangeType)
	assert.Equal(t, api.ResourceName("prod/settings"), diff.Resources[0].Resource.ResourceName)
	assert.Contains(t, diff.Resources[0].FromValue, "kind: ConfigMap")
	assert.Equal(t, api.MutationTypeAdd, diff.Resources[1].ChangeType)
	assert.Contains(t, diff.Resources[1].ToValue, "kind: Service")
}

func TestComputeDiffMultilineStringPatch(t *testing.T) {
	previous := "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: settings\ndata:\n  app.properties: |\n    a=1\n    b=2\n    c=3\n"
	modified := strings.Replace(previous, "b=2", "b=20", 1)
	diff := computeTestDiff(t, previous, modified)
	require.Len(t, diff.Resources, 1)
	change := diff.Resources[0].Changes[0]
	assert.Equal(t, "data.app~1properties", change.DisplayPath)
	assert.Equal(t, "app.properties", change.Segments[1].Field)
	assert.Equal(t, "a=1\nb=2\nc=3\n", change.FromValue)
	assert.Equal(t, "a=1\nb=20\nc=3\n", change.ToValue)
	assert.Equal(t, "@@ -1,3 +1,3 @@\n a=1\n-b=2\n+b=20\n c=3\n", change.Patch)
}

func TestComputeDiffReplaceAndAddedSubtree(t *testing.T) {
	previous := "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: settings\ndata:\n  mode: simple\n"
	modified := "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: settings\n  labels:\n    app: web\n    tier: front\ndata:\n  mode:\n    kind: fancy\n"
	diff := computeTestDiff(t, previous, modified)
	require.Len(t, diff.Resources, 1)
	assert.Equal(t, []string{
		// An added map is one change, although its mutations record each leaf.
		"Add metadata.labels:  -> app: web\ntier: front",
		"Replace data.mode: simple -> kind: fancy",
	}, changeSummary(diff.Resources[0]))
}

// Changes are listed in document order, with a removed key where it was.
func TestComputeDiffDocumentOrder(t *testing.T) {
	previous := "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: settings\ndata:\n  z: \"1\"\n  y: \"2\"\n  x: \"3\"\n  w: \"4\"\n"
	modified := "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: settings\ndata:\n  z: \"10\"\n  x: \"3\"\n  w: \"40\"\n  v: \"5\"\n"
	diff := computeTestDiff(t, previous, modified)
	require.Len(t, diff.Resources, 1)
	assert.Equal(t, []string{
		"Update data.z: 1 -> 10",
		"Delete data.y: 2 -> ",
		"Update data.w: 4 -> 40",
		"Add data.v:  -> 5",
	}, changeSummary(diff.Resources[0]))
}

// The diff's walk is the mutation walk: recording must not change the mutations it computes.
func TestComputeDiffLeavesMutationsUnchanged(t *testing.T) {
	previousDocs, err := gaby.ParseAll([]byte(diffDeployment))
	require.NoError(t, err)
	modified := strings.NewReplacer(
		"image: app:1", "image: app:2",
		"      - name: sidecar\n        image: sidecar:1", "      - name: proxy\n        image: sidecar:2",
		`args: [serve, --port, "8080"]`, `args: [serve, --verbose, --port, "9090"]`,
	).Replace(diffDeployment)
	modifiedDocs, err := gaby.ParseAll([]byte(modified))
	require.NoError(t, err)

	plain := diffResourcePair(previousDocs[0], modifiedDocs[0], "apps/v1/Deployment", 3, diffTestProvider)
	for _, context := range []bool{false, true} {
		recorded := diffResourcePairRecorded(previousDocs[0], modifiedDocs[0], "apps/v1/Deployment", 3, diffTestProvider, newDiffRecorder(context))
		assert.Equal(t, plain, recorded, "context %v", context)
	}
}

// Each side says how to read its value, so a string can be told from a number with the same
// text, and a block from a multi-line string.
func TestComputeDiffDataTypes(t *testing.T) {
	previous := "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: settings\ndata:\n  port: \"45\"\n  on: true\n  text: |\n    a: 1\n"
	modified := "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: settings\n  labels:\n    app: web\ndata:\n  port: 45\n  on: \"true\"\n  text: |\n    a: 2\n  ratio: 0.5\n"
	diff := computeTestDiff(t, previous, modified)
	require.Len(t, diff.Resources, 1)
	type typed struct {
		path               string
		fromType, toType   api.DataType
		fromValue, toValue string
	}
	var got []typed
	for _, change := range diff.Resources[0].Changes {
		got = append(got, typed{change.DisplayPath, change.FromDataType, change.ToDataType, change.FromValue, change.ToValue})
	}
	assert.Equal(t, []typed{
		{"metadata.labels", "", api.DataTypeYAML, "", "app: web"},
		{"data.port", api.DataTypeString, api.DataTypeInt, "45", "45"},
		{"data.on", api.DataTypeBool, api.DataTypeString, "true", "true"},
		{"data.text", api.DataTypeString, api.DataTypeString, "a: 1\n", "a: 2\n"},
		// A float has no DataType, as functions report it.
		{"data.ratio", "", "", "", "0.5"},
	}, got)
}

func TestComputeDiffChangeTypeDataTypes(t *testing.T) {
	diff := computeTestDiff(t, diffDeployment, strings.Replace(diffDeployment,
		"        - name: A\n          value: a\n        - name: B\n          value: b",
		"        - name: B\n          value: b\n        - name: A\n          value: a", 1))
	require.Len(t, diff.Resources, 1)
	require.Len(t, diff.Resources[0].Changes, 1)
	reorder := diff.Resources[0].Changes[0]
	assert.Equal(t, api.DiffChangeTypeReorder, reorder.ChangeType)
	assert.Equal(t, api.DataTypeYAML, reorder.FromDataType)
	assert.Equal(t, api.DataTypeYAML, reorder.ToDataType)

	diff = computeTestDiff(t, diffDeployment, strings.Replace(diffDeployment,
		"      - name: sidecar\n        image: sidecar:1", "      - name: proxy\n        image: sidecar:2", 1))
	rename := diff.Resources[0].Changes[0]
	assert.Equal(t, api.DiffChangeTypeRename, rename.ChangeType)
	assert.Equal(t, api.DataTypeString, rename.FromDataType)
	assert.Equal(t, api.DataTypeString, rename.ToDataType)
}

// A comment change is reported at the path of the node the comment annotates, not at the
// comment key the parse stores it under, and a block shows its comments as comments.
func TestComputeDiffComments(t *testing.T) {
	previous := "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: demo\ndata:\n  mode: simple # the mode\n"
	modified := "apiVersion: v1\n# what this ConfigMap is for\nkind: ConfigMap\nmetadata:\n  name: demo\ndata:\n  mode: simple # the new mode\n  extra:\n    # inner\n    x: \"1\" # line\n"
	diff := computeTestDiffWithComments(t, previous, modified)
	require.Len(t, diff.Resources, 1)
	type comment struct {
		changeType  api.DiffChangeType
		displayPath string
		comment     api.DiffComment
		fromValue   string
		toValue     string
	}
	var got []comment
	for _, change := range diff.Resources[0].Changes {
		got = append(got, comment{change.ChangeType, change.DisplayPath, change.Comment, change.FromValue, change.ToValue})
		assert.NotContains(t, change.DisplayPath, CommentKeyPrefix)
		for _, segment := range change.Segments {
			assert.NotContains(t, segment.Field, CommentKeyPrefix)
		}
	}
	assert.Equal(t, []comment{
		{api.DiffChangeTypeAdd, "kind", api.DiffCommentHead, "", "what this ConfigMap is for"},
		{api.DiffChangeTypeUpdate, "data.mode", api.DiffCommentLine, "the mode", "the new mode"},
		{api.DiffChangeTypeAdd, "data.extra", "", "", "# inner\nx: \"1\" # line"},
	}, got)
	// The Path is still the one MutationSources records.
	assert.Equal(t, api.ResolvedPath("$comment$head$kind"), diff.Resources[0].Changes[0].Path)
}

func TestComputeDiffResourceDocumentComments(t *testing.T) {
	added := "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  # the name\n  name: extra\n"
	diff := computeTestDiffWithComments(t, diffDeployment, diffDeployment+"---\n"+added)
	require.Len(t, diff.Resources, 1)
	assert.Equal(t, api.MutationTypeAdd, diff.Resources[0].ChangeType)
	assert.Contains(t, diff.Resources[0].ToValue, "# the name\n")
	assert.NotContains(t, diff.Resources[0].ToValue, CommentKeyPrefix)

	// The comment above a document's separator is the document's own head comment.
	diff = computeTestDiffWithComments(t, diffDeployment, diffDeployment+"---\n# about extra\n---\n"+added)
	require.Len(t, diff.Resources, 1)
	assert.True(t, strings.HasPrefix(diff.Resources[0].ToValue, "# about extra\napiVersion: v1\n"), diff.Resources[0].ToValue)
}

// contextSummary renders a resource's entries, changes and context alike, compactly.
func contextSummary(resource api.ResourceDiff) []string {
	var lines []string
	for _, change := range resource.Changes {
		lines = append(lines, string(change.ChangeType)+" "+change.DisplayPath)
	}
	return lines
}

// Context is every unchanged child of each map or array on the way from a change up to the
// resource, an unchanged map or array as one entry, in document order with the changes.
func TestComputeDiffContext(t *testing.T) {
	modified := strings.Replace(diffDeployment, "image: app:1", "image: app:2", 1)
	diff := computeTestDiffWithOptions(t, diffDeployment, modified, DiffOptions{IncludeContext: true})
	require.Len(t, diff.Resources, 1)
	assert.Equal(t, []string{
		"None apiVersion",
		"None kind",
		"None metadata",
		"None spec.replicas",
		"None spec.template.spec.containers.?name=app.name",
		"Update spec.template.spec.containers.?name=app.image",
		"None spec.template.spec.containers.?name=app.args",
		"None spec.template.spec.containers.?name=app.env",
		"None spec.template.spec.containers.?name=sidecar",
	}, contextSummary(diff.Resources[0]))

	changes := diff.Resources[0].Changes
	assert.Equal(t, "name: web\nnamespace: prod", changes[2].ToValue)
	assert.Equal(t, api.DataTypeYAML, changes[2].ToDataType)
	assert.Empty(t, changes[2].FromValue)
	assert.Equal(t, "2", changes[3].ToValue)
	assert.Equal(t, api.DataTypeInt, changes[3].ToDataType)
	sidecar := changes[8].Segments[4]
	assert.Equal(t, 1, sidecar.FromIndex)
	assert.Equal(t, 1, sidecar.ToIndex)

	// Without the option, only the change.
	assert.Len(t, computeTestDiff(t, diffDeployment, modified).Resources[0].Changes, 1)
}

func TestComputeDiffContextAroundAddDeleteAndRename(t *testing.T) {
	modified := strings.NewReplacer(
		"        - name: A\n          value: a\n", "",
		"      - name: sidecar\n        image: sidecar:1", "      - name: proxy\n        image: sidecar:2",
		"  replicas: 2\n", "  replicas: 2\n  paused: true\n",
	).Replace(diffDeployment)
	diff := computeTestDiffWithOptions(t, diffDeployment, modified, DiffOptions{IncludeContext: true})
	require.Len(t, diff.Resources, 1)
	assert.Equal(t, []string{
		"None apiVersion",
		"None kind",
		"None metadata",
		"None spec.replicas",
		"Add spec.paused",
		"None spec.template.spec.containers.?name=app.name",
		"None spec.template.spec.containers.?name=app.image",
		"None spec.template.spec.containers.?name=app.args",
		"Delete spec.template.spec.containers.?name=app.env.?name=A",
		"None spec.template.spec.containers.?name=app.env.?name=B",
		"Rename spec.template.spec.containers.?name=proxy",
		"Update spec.template.spec.containers.?name=proxy.image",
		"None spec.template.spec.containers.?name=proxy.imagePullPolicy",
		"None spec.template.spec.containers.?name=proxy.workingDir",
		"None spec.template.spec.containers.?name=proxy.tty",
		"None spec.template.spec.containers.?name=proxy.stdin",
		"None spec.template.spec.containers.?name=proxy.terminationMessagePath",
		"None spec.template.spec.containers.?name=proxy.command",
	}, contextSummary(diff.Resources[0]))
}

func TestComputeDiffContextPositional(t *testing.T) {
	modified := strings.Replace(diffDeployment, `args: [serve, --port, "8080"]`, `args: [serve, --port, "9090"]`, 1)
	diff := computeTestDiffWithOptions(t, diffDeployment, modified, DiffOptions{IncludeContext: true})
	require.Len(t, diff.Resources, 1)
	var args []string
	for _, change := range diff.Resources[0].Changes {
		if strings.HasPrefix(change.DisplayPath, "spec.template.spec.containers.?name=app.args.") {
			args = append(args, string(change.ChangeType)+" "+change.DisplayPath+" "+change.ToValue)
		}
	}
	assert.Equal(t, []string{
		"None spec.template.spec.containers.?name=app.args.0 serve",
		"None spec.template.spec.containers.?name=app.args.1 --port",
		"Update spec.template.spec.containers.?name=app.args.2 9090",
	}, args)
}

func TestComputeDiffContextUnchangedResource(t *testing.T) {
	diff := computeTestDiffWithOptions(t, diffDeployment, diffDeployment, DiffOptions{IncludeContext: true, IncludeUnchanged: true})
	require.Len(t, diff.Resources, 1)
	assert.Equal(t, api.MutationTypeNone, diff.Resources[0].ChangeType)
	assert.Empty(t, diff.Resources[0].Changes)
}

func TestDisplayLinePatch(t *testing.T) {
	var previous, modified []string
	for i := 1; i <= 20; i++ {
		line := "line" + strconv.Itoa(i)
		previous = append(previous, line)
		switch i {
		case 2:
			modified = append(modified, "line2 changed")
		case 15:
			// removed
		default:
			modified = append(modified, line)
		}
	}
	patch := DisplayLinePatch(strings.Join(previous, "\n")+"\n", strings.Join(modified, "\n")+"\n")
	assert.Equal(t, `@@ -1,5 +1,5 @@
 line1
-line2
+line2 changed
 line3
 line4
 line5
@@ -12,7 +12,6 @@
 line12
 line13
 line14
-line15
 line16
 line17
 line18
`, patch)
	assert.Empty(t, DisplayLinePatch("same\n", "same\n"))
}
