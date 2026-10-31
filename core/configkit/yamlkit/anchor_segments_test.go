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

const anchorSegmentsBefore = `apiVersion: external-secrets.io/v1
kind: ExternalSecret
metadata:
  name: app
  namespace: default
spec:
  data:
  - secretKey: user
    remoteRef:
      key: db-user
  - secretKey: password
    remoteRef:
      key: db-password
  - secretKey: host
    remoteRef:
      key: db-host
`

func TestResolveAnchorSegments(t *testing.T) {
	parse := func(data string) gaby.Container {
		docs, err := gaby.ParseAll([]byte(data))
		require.NoError(t, err)
		return docs
	}
	edit := func(data string) string { return strings.Replace(data, "key: db-password", "key: db-password-v2", 1) }
	insertAhead := func(data string) string {
		return strings.Replace(data, "  data:\n", "  data:\n  - secretKey: port\n    remoteRef:\n      key: db-port\n", 1)
	}

	// spec.data has no merge key, so the change is recorded under a content-identity segment,
	// and a record stores it without the positional fallback.
	mutations, err := ComputeMutations(parse(anchorSegmentsBefore), parse(edit(anchorSegmentsBefore)), 2, testProvider)
	require.NoError(t, err)
	require.Len(t, mutations, 1)
	var fresh api.ResolvedPath
	for path := range mutations[0].PathMutationMap {
		fresh = path
	}
	require.Contains(t, string(fresh), "spec.data.?~", "the element should be named by its content")
	stored := string(CanonicalMutationPath(fresh))
	require.NotContains(t, stored, ";@")

	for _, tc := range []struct {
		name, data, want string
	}{
		// The digest is of the element as the edit found it, so in the data the edit left it is
		// placed by its neighbors.
		{"edited", edit(anchorSegmentsBefore), "spec.data.1.remoteRef.key"},
		{"edited and moved", insertAhead(edit(anchorSegmentsBefore)), "spec.data.2.remoteRef.key"},
		// Where the element is as the edit found it, it is placed by its content, wherever it is.
		{"unedited and moved", insertAhead(anchorSegmentsBefore), "spec.data.2.remoteRef.key"},
	} {
		resolved, ok := ResolveAnchorSegments(parse(tc.data)[0], stored)
		assert.True(t, ok, tc.name)
		assert.Equal(t, tc.want, resolved, tc.name)
	}
	_, ok := ResolveAssociativeSegments(parse(insertAhead(edit(anchorSegmentsBefore)))[0], stored)
	assert.False(t, ok, "the merge engine's resolution does not place an edited element")

	// An element whose content and neighbors have all changed cannot be placed.
	rewritten := strings.NewReplacer("key: db-password", "key: db-password-v2", "key: db-user", "key: db-user-v2",
		"key: db-host", "key: db-host-v2").Replace(anchorSegmentsBefore)
	_, ok = ResolveAnchorSegments(parse(rewritten)[0], stored)
	assert.False(t, ok)

	// A path with no content-identity segment is left alone, merge-keyed segments included.
	plain := "spec.template.spec.containers.?name=app;@0.image"
	resolved, ok := ResolveAnchorSegments(parse(anchorSegmentsBefore)[0], plain)
	assert.True(t, ok)
	assert.Equal(t, plain, resolved)

	// An element that is not there resolves to nothing.
	emptied := "apiVersion: external-secrets.io/v1\nkind: ExternalSecret\nmetadata:\n  name: app\n  namespace: default\nspec:\n  data: []\n"
	_, ok = ResolveAnchorSegments(parse(emptied)[0], stored)
	assert.False(t, ok)
}

// TestPlaceChangeAnchors: one change removed the first element of a list with no merge key and
// edited all the others, so every anchor it recorded, neighbors included, digests content that is
// gone, and every element moved up one. Followed through the change, each entry names the element
// it changed in the data the change made, and from there in later data.
func TestPlaceChangeAnchors(t *testing.T) {
	element := func(name, key string) string {
		return "  - secretKey: " + name + "\n    remoteRef:\n      key: " + key + "\n"
	}
	header := "apiVersion: external-secrets.io/v1\nkind: ExternalSecret\nmetadata:\n  name: env\n  namespace: default\nspec:\n  data:\n"
	parse := func(data string) *gaby.YamlDoc {
		docs, err := gaby.ParseAll([]byte(data))
		require.NoError(t, err)
		return docs[0]
	}
	before := header + element("A", "/old/a") + element("B", "/old/b") + element("C", "/old/c") + element("D", "/old/d")
	after := header + element("B", "/new/b") + element("C", "/new/c") + element("D", "/new/d")

	mutations, err := ComputeMutations(gaby.Container{parse(before)}, gaby.Container{parse(after)}, 4, testProvider)
	require.NoError(t, err)
	require.Len(t, mutations, 1)
	change := map[api.ResolvedPath]api.MutationType{}
	for path, info := range mutations[0].PathMutationMap {
		change[CanonicalMutationPath(path)] = info.MutationType
	}

	placed := PlaceChangeAnchors(parse(before), parse(after), change)
	want := map[string]string{"/new/b": "spec.data.0.remoteRef.key", "/new/c": "spec.data.1.remoteRef.key", "/new/d": "spec.data.2.remoteRef.key"}
	later := header + element("B", "/new/b") + element("C", "/new/c") + element("D", "/new/d")
	found := 0
	for path, info := range mutations[0].PathMutationMap {
		if !strings.HasSuffix(string(path), ".remoteRef.key") {
			continue
		}
		stored := CanonicalMutationPath(path)
		_, ok := ResolveAnchorSegments(parse(later), string(stored))
		assert.False(t, ok, "%s should not be placed by its anchor alone", stored)
		followed, ok := placed[stored]
		require.True(t, ok, "%s should be followed through the change", stored)
		resolved, ok := ResolveAnchorSegments(parse(later), followed)
		require.True(t, ok, followed)
		assert.Equal(t, want[strings.TrimSpace(info.Value)], resolved, stored)
		found++
	}
	assert.Equal(t, 3, found)

	// A record that no longer accounts for the removal is not followed.
	for path, mutationType := range change {
		if mutationType == api.MutationTypeDelete {
			delete(change, path)
		}
	}
	for path := range PlaceChangeAnchors(parse(before), parse(after), change) {
		if strings.HasSuffix(string(path), ".remoteRef.key") {
			t.Errorf("%s should not be followed without the removal", path)
		}
	}
}
