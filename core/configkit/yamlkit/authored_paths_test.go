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

const authoredDeployment = `apiVersion: apps/v1
kind: Deployment
metadata:
  name: web
spec:
  replicas: 2
  template:
    spec:
      containers:
        - name: sidecar
          image: proxy:1
        - name: main
          image: app:1
          args: [a, b]
`

func authoredDoc(t *testing.T) *gaby.YamlDoc {
	t.Helper()
	parsed, err := gaby.ParseAll([]byte(authoredDeployment))
	require.NoError(t, err)
	return parsed[0]
}

// containers has a merge key, name; args has none.
func authoredMergeKeys(arrayPath string) ([]string, bool) {
	if arrayPath == "spec.template.spec.containers" {
		return []string{"name"}, true
	}
	return nil, false
}

func paths(expanded []ExpandedPath) []string {
	out := make([]string, len(expanded))
	for i, e := range expanded {
		out[i] = string(e.Path)
		if !e.Exists {
			out[i] += " (absent)"
		}
	}
	return out
}

// A path names a list element by merge key, by position, or as a record shows it, and "*" names
// every element; each comes back as a record keys it.
func TestExpandAuthoredPath(t *testing.T) {
	doc := authoredDoc(t)
	for path, want := range map[api.ResolvedPath][]string{
		"spec.replicas": {"spec.replicas"},
		"spec.template.spec.containers.?name=main.image":    {"spec.template.spec.containers.?name=main.image"},
		"spec.template.spec.containers.?name=main;@1.image": {"spec.template.spec.containers.?name=main.image"},
		"spec.template.spec.containers.1.image":             {"spec.template.spec.containers.?name=main.image"},
		"spec.template.spec.containers.*.image": {
			"spec.template.spec.containers.?name=sidecar.image",
			"spec.template.spec.containers.?name=main.image",
		},
		// A path that names nothing is returned as written, to be refused or kept by the caller.
		"spec.paused": {"spec.paused (absent)"},
		"spec.template.spec.containers.?name=other.image": {"spec.template.spec.containers.?name=other.image (absent)"},
		// A wildcard that matches nothing names nothing.
		"spec.template.spec.volumes.*.name": {},
	} {
		expanded, err := ExpandAuthoredPath(doc, path, authoredMergeKeys)
		require.NoError(t, err, "path %s", path)
		assert.Equal(t, want, paths(expanded), "path %s", path)
	}

	// An element of a list with no merge key is keyed by its content, which is how the record
	// follows it across writes; the same path written by position or by that key names it.
	expanded, err := ExpandAuthoredPath(doc, "spec.template.spec.containers.?name=main.args.1", authoredMergeKeys)
	require.NoError(t, err)
	require.Len(t, expanded, 1)
	anchored := expanded[0].Path
	assert.True(t, strings.HasPrefix(string(anchored), "spec.template.spec.containers.?name=main.args.?~h="), string(anchored))
	again, err := ExpandAuthoredPath(doc, anchored, authoredMergeKeys)
	require.NoError(t, err)
	assert.Equal(t, []ExpandedPath{{Path: anchored, Named: "spec.template.spec.containers.?name=main.args.1", Exists: true}}, again)

	// With no data to read, a path is kept as written and a wildcard names nothing.
	expanded, err = ExpandAuthoredPath(nil, "spec.replicas", authoredMergeKeys)
	require.NoError(t, err)
	assert.Equal(t, []string{"spec.replicas (absent)"}, paths(expanded))
	expanded, err = ExpandAuthoredPath(nil, "spec.*", authoredMergeKeys)
	require.NoError(t, err)
	assert.Empty(t, expanded)
}

// One path is keyed as a table keys it, and a wildcard or a path that names nothing is kept.
func TestStoredPathForm(t *testing.T) {
	doc := authoredDoc(t)
	for path, want := range map[api.ResolvedPath]api.ResolvedPath{
		"spec.template.spec.containers.1.image":             "spec.template.spec.containers.?name=main.image",
		"spec.template.spec.containers.?name=main;@1.image": "spec.template.spec.containers.?name=main.image",
		"spec.template.spec.containers.*.image":             "spec.template.spec.containers.*.image",
		"spec.template.spec.containers.?name=main.args.1":   "spec.template.spec.containers.?name=main.args.1",
		"spec.paused": "spec.paused",
		"":            "",
	} {
		assert.Equal(t, want, StoredPathForm(doc, path, authoredMergeKeys), "path %s", path)
	}
}
