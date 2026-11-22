package gaby

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"sigs.k8s.io/kustomize/kyaml/yaml"
)

// assertRendersAndReparses checks what a document renders, and that it is still YAML.
func assertRendersAndReparses(t *testing.T, doc *YamlDoc, want string) {
	t.Helper()
	got := doc.String()
	assert.Equal(t, want, got)
	_, err := ParseYAML([]byte(got))
	assert.NoError(t, err, "rendered YAML does not parse:\n%s", got)
}

func TestSetReplacesTheValue(t *testing.T) {
	tests := []struct {
		name  string
		input string
		path  string
		value interface{}
		want  string
	}{
		{"mapping over a scalar", "a: 1\n", "a", map[string]interface{}{}, "a: {}\n"},
		{"sequence over a string", "a: x\n", "a", []interface{}{}, "a: []\n"},
		{"sequence over a mapping", "a: {b: 1}\n", "a", []interface{}{}, "a: []\n"},
		{"mapping over a sequence", "a: [1]\n", "a", map[string]interface{}{}, "a: {}\n"},
		{"sequence over a sequence", "l: [1, 2]\n", "l", []interface{}{3}, "l: [3]\n"},
		{"mapping over a mapping", "a: {b: 1}\n", "a", map[string]interface{}{"k": "v"}, "a: {k: v}\n"},
		{"scalar over a block mapping", "a:\n  b: 1\n", "a", "x", "a: x\n"},
		{"null over a sequence", "a: [1]\n", "a", nil, "a: null\n"},

		// A replacement of the same kind keeps the style; one of another kind does not.
		{"quoted string stays quoted", "a: \"1\"\n", "a", "2", "a: \"2\"\n"},
		{"number over a quoted string is a number", "a: \"1\"\n", "a", 2, "a: 2\n"},
		{"string over a quoted string that is not a string", "a: \"x\"\n", "a", "true", "a: \"true\"\n"},
		{"block sequence stays block", "l:\n- 1\n- 2\n", "l", []interface{}{3}, "l:\n- 3\n"},

		// Comments stay with the value.
		{"line comment on a scalar", "a: 1 # c\n", "a", "x", "a: x # c\n"},
		{"line comment on a flow mapping", "a: {b: 1} # c\n", "a", map[string]interface{}{"k": "v"}, "a: {k: v} # c\n"},
		{"line comment on a scalar replaced by a block mapping", "a: 1 # c\nb: 2\n", "a", map[string]interface{}{"k": "v"}, "a:\n  # c\n  k: v\nb: 2\n"},
		{"line comment on a scalar replaced by a block sequence", "a: 1 # c\nb: 2\n", "a", []interface{}{1}, "a:\n# c\n- 1\nb: 2\n"},
		{"line comment on a scalar replaced by an empty mapping", "a: 1 # c\nb: 2\n", "a", map[string]interface{}{}, "a: {} # c\nb: 2\n"},
		{"commented null key replaced by a mapping", "a: # c\nb: 2\n", "a", map[string]interface{}{"k": "v"}, "a: # c\n  k: v\nb: 2\n"},
		{"commented null key replaced by an empty mapping", "a: # c\nb: 2\n", "a", map[string]interface{}{}, "a: {} # c\nb: 2\n"},
		{"commented null key replaced by an empty sequence", "a: # c\nb: 2\n", "a", []interface{}{}, "a: [] # c\nb: 2\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			doc := mustParseYAMLDoc(t, tt.input)
			_, err := doc.SetP(tt.value, tt.path)
			require.NoError(t, err)
			assertRendersAndReparses(t, doc, tt.want)
		})
	}
}

func TestObjectAndArrayReplaceWhatIsThere(t *testing.T) {
	doc := mustParseYAMLDoc(t, "a: x\nb: {k: v}\nc: [1]\n")
	_, err := doc.ArrayP("a")
	require.NoError(t, err)
	_, err = doc.ObjectP("b")
	require.NoError(t, err)
	_, err = doc.ObjectP("c")
	require.NoError(t, err)
	assertRendersAndReparses(t, doc, "a: []\nb: {}\nc: {}\n")
}

func TestSetAcceptsNodes(t *testing.T) {
	value := mustParseYAMLDoc(t, "k: v # c\n")

	doc := mustParseYAMLDoc(t, "a: 1\n")
	_, err := doc.Set(GetRoot(value), "a")
	require.NoError(t, err)
	assertRendersAndReparses(t, doc, "a:\n  k: v # c\n")

	// A node is the whole value, so the old value's comments go with it.
	doc = mustParseYAMLDoc(t, "a: 1 # old\n")
	_, err = doc.Set(&yaml.Node{Kind: yaml.ScalarNode, Tag: yaml.NodeTagInt, Value: "2"}, "a")
	require.NoError(t, err)
	assertRendersAndReparses(t, doc, "a: 2\n")

	doc = mustParseYAMLDoc(t, "a: 1\n")
	_, err = doc.Set((*yaml.RNode)(nil), "a")
	require.NoError(t, err)
	assertRendersAndReparses(t, doc, "a: null\n")
}

func TestArrayAppendTargets(t *testing.T) {
	tests := []struct {
		name  string
		input string
		path  string
		want  string
	}{
		{"sequence", "a: [1]\n", "a", "a: [1, 2]\n"},
		{"scalar", "a: 1\n", "a", "a:\n- 1\n- 2\n"},
		{"mapping", "a: {b: 1}\n", "a", "a:\n- {b: 1}\n- 2\n"},
		{"null", "a:\nb: 1\n", "a", "a:\n- 2\nb: 1\n"},
		{"missing", "a: 1\n", "b", "a: 1\nb:\n- 2\n"},
		{"missing parent", "a: 1\n", "x.y", "a: 1\nx:\n  y:\n  - 2\n"},
		{"scalar with a line comment", "a: 1 # c\nb: 2\n", "a", "a:\n- 1 # c\n- 2\nb: 2\n"},
		{"null with a line comment", "a: # c\nb: 2\n", "a", "a: # c\n- 2\nb: 2\n"},
		{"flow sequence with a line comment", "a: [] # c\n", "a", "a: [2] # c\n"},
		{"scalar with a head comment", "# about a\na: 1\n", "a", "# about a\na:\n- 1\n- 2\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			doc := mustParseYAMLDoc(t, tt.input)
			require.NoError(t, doc.ArrayAppendP(2, tt.path))
			assertRendersAndReparses(t, doc, tt.want)
		})
	}
}

func TestArrayAppendAppendsASliceAsOneElement(t *testing.T) {
	doc := mustParseYAMLDoc(t, "a: [1]\n")
	require.NoError(t, doc.ArrayAppendP([]interface{}{2, 3}, "a"))
	assertRendersAndReparses(t, doc, "a: [1, [2, 3]]\n")
}

func TestArrayAppendThroughAScalar(t *testing.T) {
	doc := mustParseYAMLDoc(t, "a: 1\n")
	assert.Error(t, doc.ArrayAppendP(2, "a.b"))
	assert.Equal(t, "a: 1\n", doc.String())
}

func TestArrayConcatTargets(t *testing.T) {
	tests := []struct {
		name  string
		input string
		path  string
		value interface{}
		want  string
	}{
		{"elements onto a sequence", "a: [1]\n", "a", []interface{}{2, 3}, "a: [1, 2, 3]\n"},
		{"one value onto a sequence", "a: [1]\n", "a", 2, "a: [1, 2]\n"},
		{"elements onto a scalar", "a: 1\n", "a", []interface{}{2, 3}, "a:\n- 1\n- 2\n- 3\n"},
		{"elements onto a missing path", "a: 1\n", "z", []interface{}{2, 3}, "a: 1\nz:\n- 2\n- 3\n"},
		{"no elements onto a missing path", "a: 1\n", "z", []interface{}{}, "a: 1\nz: []\n"},
		{"no elements onto a null", "a: null\nb: 2\n", "a", []interface{}{}, "a: []\nb: 2\n"},
		{"no elements onto a null with a line comment", "a: null # c\nb: 2\n", "a", []interface{}{}, "a: [] # c\nb: 2\n"},
		{"no elements onto a commented null key", "a: # c\nb: 2\n", "a", []interface{}{}, "a: [] # c\nb: 2\n"},
		{"no elements onto a scalar", "a: 1 # c\n", "a", []interface{}{}, "a:\n- 1 # c\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			doc := mustParseYAMLDoc(t, tt.input)
			require.NoError(t, doc.ArrayConcatP(tt.value, tt.path))
			assertRendersAndReparses(t, doc, tt.want)
		})
	}
}

func TestArrayConcatOntoTheRoot(t *testing.T) {
	tests := []struct {
		name  string
		input string
		value interface{}
		want  string
	}{
		{"no elements onto a null", "null\n", []interface{}{}, "[]\n"},
		{"no elements onto a null with a line comment", "null # c\n", []interface{}{}, "[] # c\n"},
		{"elements onto a null", "null\n", []interface{}{1, 2}, "- 1\n- 2\n"},
		{"elements onto a sequence", "- 1\n", []interface{}{2}, "- 1\n- 2\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			doc := mustParseYAMLDoc(t, tt.input)
			require.NoError(t, doc.ArrayConcat(tt.value))
			assertRendersAndReparses(t, doc, tt.want)
		})
	}
}

func TestMergeCollisions(t *testing.T) {
	tests := []struct {
		name   string
		dest   string
		source string
		want   map[string]interface{}
	}{
		{"scalars", "a: 1\n", "a: 2\n", map[string]interface{}{"a": []interface{}{1, 2}}},
		{"sequence and scalar", "a: [1]\n", "a: 2\n", map[string]interface{}{"a": []interface{}{1, 2}}},
		{"scalar and sequence", "a: 1\n", "a: [2, 3]\n", map[string]interface{}{"a": []interface{}{1, 2, 3}}},
		{"sequences", "a: [1]\n", "a: [2]\n", map[string]interface{}{"a": []interface{}{1, 2}}},
		{"nested", "m: {a: x}\n", "m: {a: y, b: z}\n", map[string]interface{}{"m": map[string]interface{}{"a": []interface{}{"x", "y"}, "b": "z"}}},
		{"mapping and scalar", "a: {k: v}\n", "a: 1\n", map[string]interface{}{"a": []interface{}{map[string]interface{}{"k": "v"}, 1}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dest := mustParseYAMLDoc(t, tt.dest)
			require.NoError(t, dest.Merge(mustParseYAMLDoc(t, tt.source)))
			assert.Equal(t, tt.want, dest.Data())
		})
	}
}

func TestMergeFnPassesDecodedValues(t *testing.T) {
	dest := mustParseYAMLDoc(t, "a: 1\nl: [x]\n")
	source := mustParseYAMLDoc(t, "a: 2\nl: [y]\n")
	type collision struct{ dest, source interface{} }
	var got []collision
	require.NoError(t, dest.MergeFn(source, func(d, s interface{}) interface{} {
		got = append(got, collision{d, s})
		return s
	}))
	assert.Equal(t, []collision{{1, 2}, {[]interface{}{"x"}, []interface{}{"y"}}}, got)
	assert.Equal(t, map[string]interface{}{"a": 2, "l": []interface{}{"y"}}, dest.Data())
}

func TestSetExpandFillsGapsWithNulls(t *testing.T) {
	doc := mustParseYAMLDoc(t, "a: []\n")
	_, err := doc.SetExpandP(5, "a.2")
	require.NoError(t, err)
	assertRendersAndReparses(t, doc, "a: [null, null, 5]\n")

	doc = mustParseYAMLDoc(t, "a: []\n")
	_, err = doc.SetExpandP("v", "a.2.b")
	require.NoError(t, err)
	assertRendersAndReparses(t, doc, "a: [null, null, {b: v}]\n")
}

func TestDeleteSequenceElementsAndMappingKeys(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		path    []string
		want    string
		wantErr error
	}{
		{name: "element", input: "l: [1, 2]\n", path: []string{"l", "0"}, want: "l: [2]\n"},
		{name: "negative index", input: "l: [1, 2]\n", path: []string{"l", "-1"}, wantErr: ErrOutOfBounds},
		{name: "index past the end", input: "l: [1, 2]\n", path: []string{"l", "2"}, wantErr: ErrOutOfBounds},
		{name: "key in a sequence", input: "l: [1, 2]\n", path: []string{"l", "x"}},
		{name: "integer-looking key", input: "m:\n  \"1\": a\n  b: c\n", path: []string{"m", "1"}, want: "m:\n  b: c\n"},
		{name: "missing key", input: "a: 1\n", path: []string{"zz"}, want: "a: 1\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			doc := mustParseYAMLDoc(t, tt.input)
			err := doc.Delete(tt.path...)
			if tt.want == "" {
				require.Error(t, err)
				if tt.wantErr != nil {
					assert.ErrorIs(t, err, tt.wantErr)
				}
				assert.Equal(t, tt.input, doc.String())
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, doc.String())
		})
	}
}

func TestElementRemoverBounds(t *testing.T) {
	for _, index := range []int{-1, 2} {
		doc := mustParseYAMLDoc(t, "[a, b]\n")
		_, err := ElementRemover{Index: index}.Filter(GetRoot(doc))
		assert.ErrorIs(t, err, ErrOutOfBounds, "index %d", index)
	}
}

func TestJSONPointerToPathEscapes(t *testing.T) {
	tests := []struct {
		pointer  string
		want     string
		segments []string
	}{
		{"/a/b/0", ".a.b.0", []string{"a", "b", "0"}},
		{"/a~1b", ".a/b", []string{"a/b"}},
		{"/a.b", ".a~1b", []string{"a.b"}},
		{"/a~0b", ".a~0b", []string{"a~b"}},
		{"/app.kubernetes.io~1name", ".app~1kubernetes~1io/name", []string{"app.kubernetes.io/name"}},
		{"", "", nil},
	}
	for _, tt := range tests {
		path := JSONPointerToPath(tt.pointer)
		assert.Equal(t, tt.want, path, tt.pointer)
		assert.Equal(t, tt.segments, DotPathToSlice(path), tt.pointer)
	}

	doc := mustParseYAMLDoc(t, "metadata:\n  labels:\n    app.kubernetes.io/name: web\n")
	assert.Equal(t, "web", doc.Path(JSONPointerToPath("/metadata/labels/app.kubernetes.io~1name")).Data())
}

func TestIsArrayOnNil(t *testing.T) {
	assert.False(t, (*YamlDoc)(nil).IsArray())
	assert.False(t, (&YamlDoc{}).IsArray())
}

func TestFlattenTypesAndRoots(t *testing.T) {
	doc := mustParseYAMLDoc(t, "a: 1\nb: true\nc: x\nd: [1.5, null]\n")
	flat, err := doc.Flatten()
	require.NoError(t, err)
	assert.Equal(t, map[string]interface{}{"a": 1, "b": true, "c": "x", "d.0": 1.5, "d.1": nil}, flat)

	_, err = mustParseYAMLDoc(t, "just a scalar\n").Flatten()
	assert.ErrorIs(t, err, ErrNotObjOrArray)
	_, err = mustParseYAMLDoc(t, "just a scalar\n").FlattenIncludeEmpty()
	assert.ErrorIs(t, err, ErrNotObjOrArray)
	_, err = (*YamlDoc)(nil).Flatten()
	assert.ErrorIs(t, err, ErrNotObjOrArray)
}

// A string that starts with a tab and holds a newline is double-quoted when set, since the
// literal block the encoder would otherwise write is not read back, whatever style the node had.
func TestSetKeepsATabLeadingMultiLineStringReadable(t *testing.T) {
	for _, input := range []string{"app:\n  value: x\n", "app:\n  value: |-\n    a\n    b\n"} {
		for _, value := range []string{"\t\n", "\tx\ny"} {
			doc, err := ParseYAML([]byte(input))
			require.NoError(t, err)
			_, err = doc.SetP(value, "app.value")
			require.NoError(t, err)
			reparsed, err := ParseYAML([]byte(doc.String()))
			require.NoError(t, err, "rendered YAML does not parse:\n%s", doc.String())
			assert.Equal(t, value, reparsed.Path("app.value").Data())
		}
	}
}
