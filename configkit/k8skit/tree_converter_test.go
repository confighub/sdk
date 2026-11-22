// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package k8skit

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/confighub/sdk/core/configkit"
	"github.com/confighub/sdk/core/third_party/gaby"
)

// treeConverterInputs are the cases the corpus may not cover: header comments above a document's
// separator, which ParseAll records as a $comment$head$ key and Container.String renders above the
// ---, inline and foot comments, and a document that is not a map.
var treeConverterInputs = []string{
	"# file header\n---\napiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: a\n---\n# header b\n" +
		"apiVersion: v1\nkind: ConfigMap # kind line\nmetadata:\n  name: b\ndata:\n  k: v # trailing\n# foot\n",
	"---\napiVersion: v1\nkind: Namespace\nmetadata:\n  name: n\n",
	"# only header\n---\n- a\n- b\n",
	"",
}

// treeConverterCorpus is every YAML file under test-data that parses, read from the repository
// when the module is tested inside it.
func treeConverterCorpus(t *testing.T) []string {
	t.Helper()
	inputs := append([]string{}, treeConverterInputs...)
	root := filepath.Join("..", "..", "..", "test-data")
	_ = filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || info.Size() > 6<<20 ||
			!(strings.HasSuffix(path, ".yaml") || strings.HasSuffix(path, ".yml")) {
			return nil //nolint:nilerr // an unreadable entry is left out of the corpus
		}
		data, readErr := os.ReadFile(path)
		if readErr == nil {
			if _, parseErr := gaby.ParseAll(data); parseErr == nil {
				inputs = append(inputs, string(data))
			}
		}
		return nil
	})
	return inputs
}

// freshTree parses data as the function handler does: converted to YAML, then parsed.
func freshTree(t *testing.T, p *K8sResourceProviderType, input string) gaby.Container {
	t.Helper()
	yamlData, err := p.NativeToYAML([]byte(input))
	require.NoError(t, err)
	tree, err := gaby.ParseAll(yamlData)
	require.NoError(t, err)
	return tree
}

// TreeToNative on freshly parsed documents must return exactly what YAMLToNative returns for their
// serialization, since the function handler uses one in place of the other.
func TestTreeToNativeMatchesYAMLToNative(t *testing.T) {
	p := NewK8sResourceProvider()
	for i, input := range treeConverterCorpus(t) {
		tree := freshTree(t, p, input)
		want, err := p.YAMLToNative([]byte(tree.String()))
		require.NoError(t, err)
		got, err := p.TreeToNative(tree.DeepCopy())
		require.NoError(t, err)
		require.Equal(t, string(want), string(got), "input %d", i)

		// get-resources renders each document on its own.
		for j, doc := range tree {
			want, err := p.YAMLToNative([]byte(doc.String()))
			require.NoError(t, err)
			got, err := configkit.RenderNative(p, gaby.Container{doc.DeepCopy()})
			require.NoError(t, err)
			require.Equal(t, string(want), string(got), "input %d, document %d", i, j)
		}
	}
}

// The same holds after the documents are edited, as render-configmap edits its copy before
// rendering it.
func TestTreeToNativeMatchesYAMLToNativeAfterEdits(t *testing.T) {
	p := NewK8sResourceProvider()
	for i, input := range treeConverterCorpus(t) {
		tree := freshTree(t, p, input)
		for _, doc := range tree {
			_, _ = doc.SetP("added", "metadata.labels.added")
			_, _ = doc.SetP("multi\nline\n", "metadata.annotations.note")
			_ = doc.DeleteP("metadata.name")
		}
		want, err := p.YAMLToNative([]byte(tree.String()))
		require.NoError(t, err)
		got, err := p.TreeToNative(tree.DeepCopy())
		require.NoError(t, err)
		assert.Equal(t, string(want), string(got), "input %d", i)
	}
}

// A copy is the same documents, node styles included: rendering a lone scalar of the copy gives
// what rendering it from the original gives. The flow mapping is the case where parsing the
// converted text restyles a scalar, so the copy must keep that style rather than recompute it.
func TestDeepCopyKeepsNodeStyles(t *testing.T) {
	p := NewK8sResourceProvider()
	tree := freshTree(t, p, "spec:\n  containers: [{name: web, image: nginx:2}]\n")
	original := tree[0].Path("spec.containers.0.image")
	require.NotNil(t, original)
	copied := tree.DeepCopy()[0].Path("spec.containers.0.image")
	require.NotNil(t, copied)
	assert.Equal(t, original.String(), copied.String())
	assert.Equal(t, original.YNode().Style, copied.YNode().Style)
}
