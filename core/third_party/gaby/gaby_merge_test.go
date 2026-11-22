package gaby

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMergeWithoutCollisions(t *testing.T) {
	tests := []struct {
		name   string
		dest   string
		source string
		want   string
	}{
		{
			name:   "disjoint top-level keys",
			dest:   "a: 1\n",
			source: "b: 2\n",
			want:   "a: 1\nb: 2\n",
		},
		{
			name:   "nested mappings merge recursively",
			dest:   "a:\n  x: 1\n",
			source: "a:\n  y: 2\nb: [1]\n",
			want:   "a:\n  x: 1\n  y: 2\nb: [1]\n",
		},
		{
			name:   "comments and key order of the destination survive",
			dest:   "# top\nz: 1 # keep\nm:\n  b: 1\n",
			source: "m:\n  a: 2\n",
			want:   "# top\nz: 1 # keep\nm:\n  b: 1\n  a: 2\n",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dest := mustParseYAMLDoc(t, tt.dest)
			require.NoError(t, dest.Merge(mustParseYAMLDoc(t, tt.source)))
			assert.Equal(t, tt.want, dest.String())
		})
	}
}

func TestMergeFnResolvesCollisions(t *testing.T) {
	dest := mustParseYAMLDoc(t, "a: 1\nm:\n  k: old\n  keep: x\nonly: d\n")
	source := mustParseYAMLDoc(t, "a: 2\nm:\n  k: new\nadded: s\n")

	calls := 0
	err := dest.MergeFn(source, func(_, _ interface{}) interface{} {
		calls++
		return "resolved"
	})
	require.NoError(t, err)
	assert.Equal(t, 2, calls, "one call per colliding leaf; nested mappings recurse instead")
	assert.Equal(t, "a: resolved\nm:\n  k: resolved\n  keep: x\nonly: d\nadded: s\n", dest.String())
}

func TestMergeFnRootKindMismatch(t *testing.T) {
	// A scalar destination cannot hold the source's fields, so the whole
	// root is a collision.
	dest := mustParseYAMLDoc(t, "5\n")
	err := dest.MergeFn(mustParseYAMLDoc(t, "a: 1\n"), func(_, _ interface{}) interface{} {
		return map[string]interface{}{"picked": true}
	})
	require.NoError(t, err)
	assert.Equal(t, map[string]interface{}{"picked": true}, dest.Data())
}

func TestMergeFnUnsupportedResolution(t *testing.T) {
	dest := mustParseYAMLDoc(t, "a: 1\n")
	err := dest.MergeFn(mustParseYAMLDoc(t, "a: 2\n"), func(_, _ interface{}) interface{} {
		return struct{}{}
	})
	assert.Error(t, err)
}

func TestMergeDoc(t *testing.T) {
	tests := []struct {
		name   string
		dest   string
		source string
		want   string
	}{
		{
			name:   "adds and overrides fields, keeping destination comments and order",
			dest:   "# top\na: 1\nb: # about b\n  c: 2\n",
			source: "b:\n  d: 3\na: 5\n",
			want:   "# top\na: 5\nb: # about b\n  c: 2\n  d: 3\n",
		},
		{
			name:   "null in the source deletes the field",
			dest:   "a: 1\nb: 2\n",
			source: "a: null\n",
			want:   "b: 2\n",
		},
		{
			name:   "non-associative lists are replaced",
			dest:   "l: [1, 2]\n",
			source: "l: [3]\n",
			want:   "l: [3]\n",
		},
		{
			name:   "associative lists in a known resource merge by key",
			dest:   "apiVersion: v1\nkind: Pod\nspec:\n  containers:\n  - name: a\n    image: a:1\n  - name: b\n    image: b:1\n",
			source: "apiVersion: v1\nkind: Pod\nspec:\n  containers:\n  - name: b\n    image: b:2\n  - name: c\n    image: c:1\n",
			want:   "apiVersion: v1\nkind: Pod\nspec:\n  containers:\n  - name: a\n    image: a:1\n  - name: b\n    image: b:2\n  - name: c\n    image: c:1\n",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dest := mustParseYAMLDoc(t, tt.dest)
			require.NoError(t, dest.MergeDoc(mustParseYAMLDoc(t, tt.source)))
			assert.Equal(t, tt.want, dest.String())
		})
	}
}

func TestMergeDocNilArguments(t *testing.T) {
	dest := mustParseYAMLDoc(t, "a: 1\n")
	require.NoError(t, dest.MergeDoc(nil))
	assert.Equal(t, "a: 1\n", dest.String())

	assert.ErrorIs(t, (*YamlDoc)(nil).MergeDoc(dest), ErrInvalidInputObj)
}

func TestMergeDocKindMismatch(t *testing.T) {
	dest := mustParseYAMLDoc(t, "x: 1\n")
	err := dest.MergeDocP(mustParseYAMLDoc(t, "b: 2\n"), "x")
	assert.Error(t, err)
	assert.Equal(t, "x: 1\n", dest.String())
}

func TestMergeDocP(t *testing.T) {
	tests := []struct {
		name   string
		dest   string
		source string
		path   string
		want   string
	}{
		{
			name:   "nested mapping",
			dest:   "x:\n  a: 1\ny: 2\n",
			source: "b: 2\n",
			path:   "x",
			want:   "x:\n  a: 1\n  b: 2\ny: 2\n",
		},
		{
			name:   "sequence element",
			dest:   "l:\n- a: 1\n- a: 2\n",
			source: "b: 3\n",
			path:   "l.1",
			want:   "l:\n- a: 1\n- a: 2\n  b: 3\n",
		},
		{
			name:   "empty path merges at the root",
			dest:   "a: 1\n",
			source: "b: 2\n",
			path:   "",
			want:   "a: 1\nb: 2\n",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dest := mustParseYAMLDoc(t, tt.dest)
			require.NoError(t, dest.MergeDocP(mustParseYAMLDoc(t, tt.source), tt.path))
			assert.Equal(t, tt.want, dest.String())
		})
	}
}

func TestMergeDocPMissingPath(t *testing.T) {
	dest := mustParseYAMLDoc(t, "x:\n  a: 1\n")
	err := dest.MergeDocP(mustParseYAMLDoc(t, "b: 2\n"), "y")
	assert.ErrorContains(t, err, "failed to find merge target at path y")
	assert.Equal(t, "x:\n  a: 1\n", dest.String())
}
