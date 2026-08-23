// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package k8skit

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/confighub/sdk/core/configkit/yamlkit"
)

// Comment handling for the Kubernetes/YAML converter.
//
// NativeToYAML and YAMLToNative move comments between native YAML head/line/foot comments and
// the $comment$TYPE$TARGET map keys that carry them through formats with no comments of their
// own. The AppConfig toolchains each cover this for their own format; this covers the one that
// holds nearly all real configuration, and it was the only converter without such a test.
//
// The property the tests below establish, in order of how much depends on it: comments survive
// the round trip, the round trip is *idempotent* so any normalisation it applies is a one-time
// change rather than a drift, and configuration with no comments is returned byte for byte.

const commentedManifest = `# leading comment block
# second line of the leading block
apiVersion: v1
kind: ConfigMap
metadata:
  name: first # inline comment on name
data:
  # head comment on the key
  key: value
---
# head comment of the second document
apiVersion: v1
kind: ConfigMap
metadata:
  name: second
`

// TestNativeToYAMLEncodesCommentsAsKeys pins the encoding itself, because everything else
// depends on it: a comment that is not turned into a key is a comment that a JSON projection
// cannot carry. The leading block is keyed with an empty target, which is how a comment
// belonging to the document rather than to any of its fields is represented.
func TestNativeToYAMLEncodesCommentsAsKeys(t *testing.T) {
	converted, err := NewK8sResourceProvider().NativeToYAML([]byte(commentedManifest))
	require.NoError(t, err)
	yaml := string(converted)
	t.Logf("YAML:\n%s", yaml)

	assert.Contains(t, yaml, yamlkit.CommentKey(yamlkit.CommentHead, ""),
		"the leading comment block should be keyed to the document itself")
	assert.Contains(t, yaml, yamlkit.CommentKey(yamlkit.CommentHead, "key"),
		"a head comment on a field should be keyed to that field")
	assert.Contains(t, yaml, yamlkit.CommentKey(yamlkit.CommentLine, "name"),
		"an inline comment should be keyed to the field it follows")

	assert.Contains(t, yaml, "leading comment block")
	assert.Contains(t, yaml, "head comment of the second document")
}

// TestRoundTripPreservesComments is the behaviour §15 of the large-field-access design depends
// on: a Unit's configuration can pass through the JSON projection without the author losing
// what they wrote.
func TestRoundTripPreservesComments(t *testing.T) {
	provider := NewK8sResourceProvider()

	converted, err := provider.NativeToYAML([]byte(commentedManifest))
	require.NoError(t, err)
	restored, err := provider.YAMLToNative(converted)
	require.NoError(t, err)
	native := string(restored)
	t.Logf("native:\n%s", native)

	for _, comment := range []string{
		"# leading comment block",
		"# second line of the leading block",
		"# inline comment on name",
		"# head comment on the key",
		"# head comment of the second document",
	} {
		assert.Contains(t, native, comment, "round trip lost a comment")
	}

	assert.Contains(t, native, "\n---\n", "round trip lost the document separator")
	assert.Contains(t, native, "name: first")
	assert.Contains(t, native, "name: second")
	assert.NotContains(t, native, yamlkit.CommentKeyPrefix,
		"the native form must not leak the synthetic comment keys")
}

// TestRoundTripIsIdempotent is the property that matters most, and the one that is easiest to
// lose by accident. The round trip is not byte-for-byte lossless -- it normalises the column an
// inline comment sits in, among other cosmetics -- so what makes the native form safe to
// regenerate is that a second pass changes nothing. Normalising once is a one-time edit to a
// user's file; normalising differently on each pass would be a file that never stops changing.
func TestRoundTripIsIdempotent(t *testing.T) {
	provider := NewK8sResourceProvider()

	roundTrip := func(in []byte) []byte {
		converted, err := provider.NativeToYAML(in)
		require.NoError(t, err)
		restored, err := provider.YAMLToNative(converted)
		require.NoError(t, err)
		return restored
	}

	first := roundTrip([]byte(commentedManifest))
	second := roundTrip(first)

	assert.Equal(t, string(first), string(second),
		"a second round trip must be a fixed point, so normalisation happens at most once")
}

// TestRoundTripPreservesCommentsBetweenDocuments covers the case a multi-document manifest makes
// possible and a single document does not: a comment sitting between two documents, which the
// parser may attach as a foot comment on the first or a head comment on the second. Which one it
// picks is not the point -- that it survives at all is.
func TestRoundTripPreservesCommentsBetweenDocuments(t *testing.T) {
	provider := NewK8sResourceProvider()

	const manifest = `apiVersion: v1
kind: ConfigMap
metadata:
  name: first
---
# a comment between the two documents
apiVersion: v1
kind: ConfigMap
metadata:
  name: second
`

	converted, err := provider.NativeToYAML([]byte(manifest))
	require.NoError(t, err)
	restored, err := provider.YAMLToNative(converted)
	require.NoError(t, err)
	native := string(restored)
	t.Logf("native:\n%s", native)

	assert.Contains(t, native, "# a comment between the two documents")
	assert.Equal(t, 1, strings.Count(native, "\n---\n"), "the separator should neither be lost nor duplicated")
}

// TestRoundTripWithoutCommentsIsUnchanged is the base case, and it is worth asserting separately:
// configuration with nothing to encode should come back exactly as it went in, so that the
// conversion is invisible to the overwhelming majority of Units that carry no comments at all.
func TestRoundTripWithoutCommentsIsUnchanged(t *testing.T) {
	provider := NewK8sResourceProvider()

	const manifest = `apiVersion: apps/v1
kind: Deployment
metadata:
  name: app
spec:
  replicas: 3
`

	converted, err := provider.NativeToYAML([]byte(manifest))
	require.NoError(t, err)
	restored, err := provider.YAMLToNative(converted)
	require.NoError(t, err)

	assert.Equal(t, manifest, string(restored))
}

// TestEmptyDataIsUnchanged covers the early return in both directions. Empty configuration is
// not a hypothetical: every Unit starts with an empty Revision.
func TestEmptyDataIsUnchanged(t *testing.T) {
	provider := NewK8sResourceProvider()

	converted, err := provider.NativeToYAML(nil)
	require.NoError(t, err)
	assert.Empty(t, converted)

	restored, err := provider.YAMLToNative(nil)
	require.NoError(t, err)
	assert.Empty(t, restored)
}
