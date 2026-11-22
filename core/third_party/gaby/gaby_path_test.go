package gaby

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPointerToSlice(t *testing.T) {
	tests := []struct {
		name    string
		path    string
		want    []string
		wantErr bool
	}{
		{name: "empty", path: "", want: nil},
		{name: "root", path: "/", want: []string{""}},
		{name: "segments", path: "/a/b/0", want: []string{"a", "b", "0"}},
		{name: "escapes", path: "/a~1b/~0c/~01", want: []string{"a/b", "~c", "~1"}},
		{name: "relative", path: "a/b", wantErr: true},
	}
	parsers := map[string]func(string) ([]string, error){
		"YAMLPointerToSlice": YAMLPointerToSlice,
		"JSONPointerToSlice": JSONPointerToSlice,
	}
	for parserName, parse := range parsers {
		for _, tt := range tests {
			t.Run(parserName+"/"+tt.name, func(t *testing.T) {
				got, err := parse(tt.path)
				if tt.wantErr {
					assert.Error(t, err)
					return
				}
				require.NoError(t, err)
				assert.Equal(t, tt.want, got)
			})
		}
	}
}

func TestUnescapeDotsInPathSegment(t *testing.T) {
	tests := map[string]string{
		"plain":             "plain",
		"example~1com":      "example.com",
		"a~0b":              "a~b",
		"~01":               "~1",
		"app~1kubernetes~1": "app.kubernetes.",
	}
	for in, want := range tests {
		assert.Equal(t, want, UnescapeDotsInPathSegment(in), in)
	}
}

func TestJSONPointer(t *testing.T) {
	doc := mustParseYAMLDoc(t, "a/b:\n- x\n- k: v\nm:\n  n: 1\n")

	tests := []struct {
		name    string
		pointer string
		want    interface{}
		wantErr error
	}{
		{name: "escaped key and index", pointer: "/a~1b/0", want: "x"},
		{name: "nested", pointer: "/a~1b/1/k", want: "v"},
		{name: "mapping", pointer: "/m/n", want: 1},
		{name: "empty pointer is the whole document", pointer: "", want: doc.Data()},
		{name: "index past end", pointer: "/a~1b/2", wantErr: ErrOutOfBounds},
		{name: "missing key", pointer: "/zz"},
		{name: "not a pointer", pointer: "m/n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := doc.JSONPointer(tt.pointer)
			if tt.want == nil {
				require.Error(t, err)
				if tt.wantErr != nil {
					assert.ErrorIs(t, err, tt.wantErr)
				}
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got.Data())
		})
	}
}

func TestJSONPointerToPath(t *testing.T) {
	// The result is meant for the dot-path API, so assert on how that API reads it.
	doc := mustParseYAMLDoc(t, "a:\n  b:\n  - x\n")
	path := JSONPointerToPath("/a/b/0")
	assert.Equal(t, []string{"a", "b", "0"}, DotPathToSlice(path))
	assert.Equal(t, "x", doc.Path(path).Data())
}

func TestSAndExistsP(t *testing.T) {
	doc := mustParseYAMLDoc(t, "metadata:\n  labels:\n    app.kubernetes.io/name: web\nl: [a, b]\n")

	assert.Equal(t, doc.Search("l", "1").Data(), doc.S("l", "1").Data())
	assert.Equal(t, "web", doc.S("metadata", "labels", "app.kubernetes.io/name").Data())
	assert.Nil(t, doc.S("metadata", "zz"))

	tests := map[string]bool{
		"metadata.labels":                          true,
		"metadata.labels.app~1kubernetes~1io/name": true,
		`metadata.labels."app.kubernetes.io/name"`: true,
		"l.1":            true,
		"l.2":            false,
		"metadata.annot": false,
		"l.x":            false,
	}
	for path, want := range tests {
		assert.Equal(t, want, doc.ExistsP(path), path)
	}
}

func TestSetDocP(t *testing.T) {
	tests := []struct {
		name  string
		input string
		value string
		path  string
		want  string
	}{
		{
			name:  "new nested path keeps the inserted document's order and comments",
			input: "a: 1\n",
			value: "# about x\nz: 1\nx: [1]\n",
			path:  "b.c",
			want:  "a: 1\nb:\n  c:\n    # about x\n    z: 1\n    x: [1]\n",
		},
		{
			name:  "replaces an existing sequence element",
			input: "a: [1, 2]\n",
			value: "x: 1\n",
			path:  "a.0",
			want:  "a: [{x: 1}, 2]\n",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			doc := mustParseYAMLDoc(t, tt.input)
			_, err := doc.SetDocP(mustParseYAMLDoc(t, tt.value), tt.path)
			require.NoError(t, err)
			assert.Equal(t, tt.want, doc.String())
		})
	}
}

func TestSetDocPOutOfBounds(t *testing.T) {
	doc := mustParseYAMLDoc(t, "a: [1]\n")
	_, err := doc.SetDocP(mustParseYAMLDoc(t, "x: 1\n"), "a.1")
	assert.ErrorIs(t, err, ErrOutOfBounds)
	assert.Equal(t, "a: [1]\n", doc.String())
}

func TestSetExpandPWithinReach(t *testing.T) {
	tests := []struct {
		name  string
		input string
		value interface{}
		path  string
		want  string
	}{
		{name: "existing index", input: "a: [1, 2]\n", value: 5, path: "a.1", want: "a: [1, 5]\n"},
		{name: "one past the end", input: "a: [1]\n", value: 5, path: "a.1", want: "a: [1, 5]\n"},
		{name: "one past the end, nested", input: "a:\n- k: v\n", value: "w", path: "a.1.k", want: "a:\n- k: v\n- k: w\n"},
		{name: "into an empty sequence", input: "a: []\n", value: "x", path: "a.0", want: "a: [x]\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			doc := mustParseYAMLDoc(t, tt.input)
			_, err := doc.SetExpandP(tt.value, tt.path)
			require.NoError(t, err)
			assert.Equal(t, tt.want, doc.String())
		})
	}
}

func TestSetDocExpandP(t *testing.T) {
	doc := mustParseYAMLDoc(t, "a:\n- 0\n")
	_, err := doc.SetDocExpandP(mustParseYAMLDoc(t, "x: 1\n"), "a.1")
	require.NoError(t, err)
	assert.Equal(t, "a:\n- 0\n- x: 1\n", doc.String())
}

func TestSetYAMLPointer(t *testing.T) {
	doc := mustParseYAMLDoc(t, "a:\n  b/c: 1\nl: [x]\n")

	_, err := doc.SetYAMLPointer(2, "/a/b~1c")
	require.NoError(t, err)
	_, err = doc.SetYAMLPointer("y", "/l/0")
	require.NoError(t, err)
	_, err = doc.SetYAMLPointer(true, "/new/k~0")
	require.NoError(t, err)
	assert.Equal(t, "a:\n  b/c: 2\nl: [y]\nnew:\n  k~: true\n", doc.String())

	_, err = doc.SetYAMLPointer(3, "a")
	assert.Error(t, err)
	_, err = doc.SetYAMLPointer(3, "/l/1")
	assert.ErrorIs(t, err, ErrOutOfBounds)
}

func TestDelete(t *testing.T) {
	tests := []struct {
		name  string
		input string
		path  []string
		want  string
	}{
		{name: "top-level key", input: "a: 1\nb: 2\n", path: []string{"a"}, want: "b: 2\n"},
		{name: "nested key", input: "a:\n  b: 1\n  c: 2\n", path: []string{"a", "b"}, want: "a:\n  c: 2\n"},
		{name: "sequence element", input: "l: [1, 2, 3]\n", path: []string{"l", "1"}, want: "l: [1, 3]\n"},
		{name: "key under a sequence element", input: "l:\n- k: v\n  j: w\n", path: []string{"l", "0", "k"}, want: "l:\n- j: w\n"},
		{name: "keeps comments on siblings", input: "# top\na: 1 # one\nb: 2\n", path: []string{"b"}, want: "# top\na: 1 # one\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			doc := mustParseYAMLDoc(t, tt.input)
			require.NoError(t, doc.Delete(tt.path...))
			assert.Equal(t, tt.want, doc.String())
		})
	}
}

func TestDeleteErrors(t *testing.T) {
	const input = "a: 1\nl:\n- k: v\nm:\n  n: 1\n"
	tests := []struct {
		name    string
		path    []string
		wantErr error
	}{
		{name: "empty path", path: nil, wantErr: ErrInvalidQuery},
		{name: "missing intermediate", path: []string{"zz", "k"}, wantErr: ErrNotFound},
		{name: "intermediate index past end", path: []string{"l", "1", "k"}, wantErr: ErrOutOfBounds},
		{name: "intermediate negative index", path: []string{"l", "-1", "k"}, wantErr: ErrOutOfBounds},
		{name: "non-integer index into a sequence", path: []string{"l", "x", "k"}},
		{name: "leaf index past end", path: []string{"l", "1"}},
		{name: "key on a sequence", path: []string{"l", "k"}},
		{name: "through a scalar", path: []string{"a", "b", "c"}},
		{name: "index on a scalar", path: []string{"a", "0"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			doc := mustParseYAMLDoc(t, input)
			err := doc.Delete(tt.path...)
			require.Error(t, err)
			if tt.wantErr != nil {
				assert.ErrorIs(t, err, tt.wantErr)
			}
			assert.Equal(t, input, doc.String())
		})
	}

	assert.ErrorIs(t, (*YamlDoc)(nil).Delete("a"), ErrNotObj)
}

func TestFlatten(t *testing.T) {
	doc := mustParseYAMLDoc(t, "a:\n  b: x\n  c: [y, {d: z}]\ne: {}\nf: []\n")

	flat, err := doc.Flatten()
	require.NoError(t, err)
	assert.Equal(t, map[string]interface{}{
		"a.b":     "x",
		"a.c.0":   "y",
		"a.c.1.d": "z",
	}, flat)

	flat, err = doc.FlattenIncludeEmpty()
	require.NoError(t, err)
	assert.Equal(t, map[string]interface{}{
		"a.b":     "x",
		"a.c.0":   "y",
		"a.c.1.d": "z",
		"e":       struct{}{},
		"f":       []struct{}{},
	}, flat)
}

func TestFlattenRootSequence(t *testing.T) {
	doc := mustParseYAMLDoc(t, "- a\n- [b]\n- k: c\n")
	flat, err := doc.Flatten()
	require.NoError(t, err)
	assert.Equal(t, map[string]interface{}{"0": "a", "1.0": "b", "2.k": "c"}, flat)
}
