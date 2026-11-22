package gaby

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mustParseYAMLDoc parses a single YAML document, failing the test on error.
func mustParseYAMLDoc(t *testing.T, src string) *YamlDoc {
	t.Helper()
	doc, err := ParseYAML([]byte(src))
	require.NoError(t, err)
	return doc
}

func TestArrayAppendToExistingSequence(t *testing.T) {
	tests := []struct {
		name  string
		input string
		value interface{}
		path  string
		want  string
	}{
		{
			name:  "scalar onto nested flow sequence",
			input: "a:\n  b: [1]\n",
			value: "x",
			path:  "a.b",
			want:  "a:\n  b: [1, x]\n",
		},
		{
			name:  "map onto block sequence",
			input: "l:\n- k: v\n",
			value: map[string]interface{}{"k": "w"},
			path:  "l",
			want:  "l:\n- k: v\n- k: w\n",
		},
		{
			name:  "onto root sequence",
			input: "- 1\n",
			value: 2,
			path:  "",
			want:  "- 1\n- 2\n",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			doc := mustParseYAMLDoc(t, tt.input)
			require.NoError(t, doc.ArrayAppendP(tt.value, tt.path))
			assert.Equal(t, tt.want, doc.String())
		})
	}
}

func TestArrayAppendUnsupportedValue(t *testing.T) {
	doc := mustParseYAMLDoc(t, "l: [1]\n")
	assert.Error(t, doc.ArrayAppendP(struct{}{}, "l"))
	assert.Equal(t, "l: [1]\n", doc.String())
}

func TestArrayConcatRootSequence(t *testing.T) {
	doc := mustParseYAMLDoc(t, "- 1\n")
	require.NoError(t, doc.ArrayConcat([]interface{}{2, "x"}))
	require.NoError(t, doc.ArrayConcat(3))
	assert.Equal(t, "- 1\n- 2\n- x\n- 3\n", doc.String())
}

func TestArrayRemove(t *testing.T) {
	tests := []struct {
		name    string
		index   int
		path    string
		want    string
		wantErr error
	}{
		{name: "first", index: 0, path: "l", want: "l: [2, 3]\nm: {}\n"},
		{name: "middle", index: 1, path: "l", want: "l: [1, 3]\nm: {}\n"},
		{name: "last", index: 2, path: "l", want: "l: [1, 2]\nm: {}\n"},
		{name: "negative", index: -1, path: "l", wantErr: ErrOutOfBounds},
		{name: "past end", index: 3, path: "l", wantErr: ErrOutOfBounds},
		{name: "not a sequence", index: 0, path: "m", wantErr: ErrNotArray},
		{name: "missing path", index: 0, path: "zz"},
	}
	const input = "l: [1, 2, 3]\nm: {}\n"
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			doc := mustParseYAMLDoc(t, input)
			err := doc.ArrayRemoveP(tt.index, tt.path)
			if tt.want == "" {
				require.Error(t, err)
				if tt.wantErr != nil {
					assert.ErrorIs(t, err, tt.wantErr)
				}
				assert.Equal(t, input, doc.String())
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, doc.String())
		})
	}
}

func TestArrayElement(t *testing.T) {
	doc := mustParseYAMLDoc(t, "a:\n  l:\n  - x\n  - k: v\n  m: {}\n")

	elem, err := doc.ArrayElementP(1, "a.l")
	require.NoError(t, err)
	assert.Equal(t, "k: v\n", elem.String())

	elem, err = doc.ArrayElement(0, "a", "l")
	require.NoError(t, err)
	assert.Equal(t, "x", elem.Data())

	// The returned element aliases the document, so edits through it land in the document.
	elem, err = doc.ArrayElementP(1, "a.l")
	require.NoError(t, err)
	_, err = elem.Set("w", "k")
	require.NoError(t, err)
	assert.Equal(t, "w", doc.Path("a.l.1.k").Data())

	_, err = doc.ArrayElementP(-1, "a.l")
	assert.ErrorIs(t, err, ErrOutOfBounds)
	_, err = doc.ArrayElementP(2, "a.l")
	assert.ErrorIs(t, err, ErrOutOfBounds)
	_, err = doc.ArrayElementP(0, "a.m")
	assert.ErrorIs(t, err, ErrNotArray)
	_, err = doc.ArrayElementP(0, "a.zz")
	assert.Error(t, err)
}

func TestArrayCount(t *testing.T) {
	doc := mustParseYAMLDoc(t, "l: [a, b]\ne: []\nm: {}\n")

	n, err := doc.ArrayCountP("l")
	require.NoError(t, err)
	assert.Equal(t, 2, n)

	n, err = doc.ArrayCount("e")
	require.NoError(t, err)
	assert.Equal(t, 0, n)

	_, err = doc.ArrayCount("m")
	assert.ErrorIs(t, err, ErrNotArray)
	_, err = doc.ArrayCountP("zz")
	assert.Error(t, err)
}

func TestArrayInsert(t *testing.T) {
	tests := []struct {
		name    string
		value   interface{}
		index   int
		path    string
		want    string
		wantErr error
	}{
		{name: "at start", value: "x", index: 0, path: "l", want: "l: [x, 1, 2]\nm: {}\n"},
		{name: "in middle", value: "x", index: 1, path: "l", want: "l: [1, x, 2]\nm: {}\n"},
		{name: "at end", value: "x", index: 2, path: "l", want: "l: [1, 2, x]\nm: {}\n"},
		{name: "map value", value: map[string]interface{}{"k": "v"}, index: 1, path: "l", want: "l: [1, {k: v}, 2]\nm: {}\n"},
		{name: "past end", value: "x", index: 3, path: "l", wantErr: ErrOutOfBounds},
		{name: "negative", value: "x", index: -1, path: "l", wantErr: ErrOutOfBounds},
		{name: "not a sequence", value: "x", index: 0, path: "m", wantErr: ErrNotArray},
		{name: "missing path", value: "x", index: 0, path: "zz"},
		{name: "unsupported value", value: struct{}{}, index: 0, path: "l"},
	}
	const input = "l: [1, 2]\nm: {}\n"
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			doc := mustParseYAMLDoc(t, input)
			err := doc.ArrayInsertP(tt.value, tt.index, tt.path)
			if tt.want == "" {
				require.Error(t, err)
				if tt.wantErr != nil {
					assert.ErrorIs(t, err, tt.wantErr)
				}
				assert.Equal(t, input, doc.String())
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, doc.String())
		})
	}
}

func TestObjectAndArrayCreatePaths(t *testing.T) {
	doc := New()

	obj, err := doc.ObjectP("a.b")
	require.NoError(t, err)
	_, err = obj.Set("v", "k")
	require.NoError(t, err)

	_, err = doc.Object("c")
	require.NoError(t, err)

	arr, err := doc.ArrayP("a.l")
	require.NoError(t, err)
	assert.True(t, arr.IsArray())

	_, err = doc.Array("e")
	require.NoError(t, err)

	_, err = doc.ArrayOfSizeP(2, "a.n")
	require.NoError(t, err)

	_, err = doc.ArrayOfSize(1, "f")
	require.NoError(t, err)

	assert.Equal(t, "a:\n  b:\n    k: v\n  l: []\n  n:\n  - null\n  - null\nc: {}\ne: []\nf:\n- null\n", doc.String())
}

func TestObjectAndArrayPathCollision(t *testing.T) {
	doc := mustParseYAMLDoc(t, "a: 1\n")
	_, err := doc.ObjectP("a.b")
	assert.Error(t, err)
	_, err = doc.ArrayP("a.b")
	assert.Error(t, err)
	_, err = doc.ArrayOfSizeP(2, "a.b")
	assert.Error(t, err)
	assert.Equal(t, "a: 1\n", doc.String())
}

func TestSetIndexAndIndexedConstructors(t *testing.T) {
	doc := mustParseYAMLDoc(t, "l: [1, 2, 3, 4]\nm: {}\n")
	l := doc.Path("l")

	set, err := l.SetIndex("x", 0)
	require.NoError(t, err)
	assert.Equal(t, "x", set.Data())

	_, err = l.ObjectI(1)
	require.NoError(t, err)
	_, err = l.ArrayI(2)
	require.NoError(t, err)
	_, err = l.ArrayOfSizeI(2, 3)
	require.NoError(t, err)

	assert.Equal(t, "l: [x, {}, [], [null, null]]\nm: {}\n", doc.String())
}

func TestSetIndexErrors(t *testing.T) {
	const input = "l: [a]\nm: {}\n"
	doc := mustParseYAMLDoc(t, input)
	l := doc.Path("l")

	tests := []struct {
		name    string
		call    func() (*YamlDoc, error)
		wantErr error
	}{
		{name: "past end", call: func() (*YamlDoc, error) { return l.SetIndex("z", 1) }, wantErr: ErrOutOfBounds},
		{name: "negative", call: func() (*YamlDoc, error) { return l.SetIndex("z", -1) }, wantErr: ErrOutOfBounds},
		{name: "mapping", call: func() (*YamlDoc, error) { return doc.Path("m").SetIndex("z", 0) }, wantErr: ErrNotArray},
		{name: "nil doc", call: func() (*YamlDoc, error) { return (*YamlDoc)(nil).SetIndex("z", 0) }, wantErr: ErrNotArray},
		{name: "ObjectI past end", call: func() (*YamlDoc, error) { return l.ObjectI(1) }, wantErr: ErrOutOfBounds},
		{name: "ArrayI on mapping", call: func() (*YamlDoc, error) { return doc.ArrayI(0) }, wantErr: ErrNotArray},
		{name: "ArrayOfSizeI past end", call: func() (*YamlDoc, error) { return l.ArrayOfSizeI(1, 5) }, wantErr: ErrOutOfBounds},
		{name: "unsupported value", call: func() (*YamlDoc, error) { return l.SetIndex(struct{}{}, 0) }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := tt.call()
			require.Error(t, err)
			if tt.wantErr != nil {
				assert.ErrorIs(t, err, tt.wantErr)
			}
			assert.Equal(t, input, doc.String())
		})
	}
}

func TestIndex(t *testing.T) {
	doc := mustParseYAMLDoc(t, "l: [a, b]\n")
	l := doc.Path("l")

	assert.Equal(t, "a", l.Index(0).Data())
	assert.Equal(t, "b", l.Index(1).Data())
	assert.Nil(t, l.Index(2))
	assert.Nil(t, l.Index(-1))
	assert.Nil(t, doc.Index(0), "a mapping has no index")
	assert.Nil(t, (*YamlDoc)(nil).Index(0))
}

func TestIsArray(t *testing.T) {
	doc := mustParseYAMLDoc(t, "l: [a]\nm: {}\ns: x\n")
	assert.True(t, doc.Path("l").IsArray())
	assert.False(t, doc.Path("m").IsArray())
	assert.False(t, doc.Path("s").IsArray())
	assert.False(t, doc.IsArray())
}

func TestChildren(t *testing.T) {
	doc := mustParseYAMLDoc(t, "l: [a, b, c]\nm:\n  z: 1\n  a: 2\ns: x\n")

	var seq []interface{}
	for _, c := range doc.Path("l").Children() {
		seq = append(seq, c.Data())
	}
	assert.Equal(t, []interface{}{"a", "b", "c"}, seq)

	var vals []interface{}
	for _, c := range doc.Path("m").Children() {
		vals = append(vals, c.Data())
	}
	assert.Equal(t, []interface{}{1, 2}, vals, "mapping children follow source order")

	assert.Nil(t, doc.Path("s").Children())
	assert.Nil(t, (*YamlDoc)(nil).Children())

	// Children alias the document.
	doc.Path("m").Children()[0].YNode().Value = "9"
	assert.Equal(t, 9, doc.Path("m.z").Data())
}

func TestChildrenMap(t *testing.T) {
	doc := mustParseYAMLDoc(t, "m:\n  z: 1\n  a: [x]\nl: [a]\n")

	children := doc.Path("m").ChildrenMap()
	require.Len(t, children, 2)
	assert.Equal(t, 1, children["z"].Data())
	assert.Equal(t, []interface{}{"x"}, children["a"].Data())

	assert.Empty(t, doc.Path("l").ChildrenMap())
	assert.Empty(t, doc.Path("m.z").ChildrenMap())
	assert.NotNil(t, (*YamlDoc)(nil).ChildrenMap())
	assert.Empty(t, (*YamlDoc)(nil).ChildrenMap())
}
