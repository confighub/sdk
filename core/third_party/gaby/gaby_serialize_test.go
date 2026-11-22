package gaby

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/iotest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	orderedmap "github.com/wk8/go-ordered-map/v2"
	"sigs.k8s.io/kustomize/kyaml/yaml"
)

func TestBytesIndent(t *testing.T) {
	doc := mustParseYAMLDoc(t, "# top\na:\n  b:\n  - 1\n  c: x # about c\n")

	assert.Equal(t, "# top\na:\n    b:\n      - 1\n    c: x # about c\n", doc.StringIndent(4))
	assert.Equal(t, doc.StringIndent(4), string(doc.BytesIndent(4)))
	assert.Equal(t, "# top\na:\n  b:\n  - 1\n  c: x # about c\n", doc.StringIndent(2))

	assert.Equal(t, "", (*YamlDoc)(nil).StringIndent(2))
}

func TestMarshalYAML(t *testing.T) {
	doc := mustParseYAMLDoc(t, "# top\nz: 1\na: [x] # about a\n")
	b, err := doc.MarshalYAML()
	require.NoError(t, err)
	assert.Equal(t, doc.String(), string(b))
	assert.Equal(t, "# top\nz: 1\na: [x] # about a\n", string(b))

	b, err = (*YamlDoc)(nil).MarshalYAML()
	require.NoError(t, err)
	assert.Equal(t, EmptyDocument, b)
}

func TestMarshalJSON(t *testing.T) {
	doc := mustParseYAMLDoc(t, "b: 1\na: [x, true, null]\nm:\n  f: 1.5\n")
	b, err := doc.MarshalJSON()
	require.NoError(t, err)
	assert.JSONEq(t, `{"b":1,"a":["x",true,null],"m":{"f":1.5}}`, string(b))

	b, err = (*YamlDoc)(nil).MarshalJSON()
	require.NoError(t, err)
	assert.Equal(t, EmptyDocument, b)
}

func TestParseJSON(t *testing.T) {
	doc, err := ParseJSON([]byte(`{"b":1,"a":[true,"x",{"k":null}]}`))
	require.NoError(t, err)
	assert.Equal(t, map[string]interface{}{
		"b": 1,
		"a": []interface{}{true, "x", map[string]interface{}{"k": nil}},
	}, doc.Data())

	_, err = doc.SetP("v", "a.2.k")
	require.NoError(t, err)
	assert.Equal(t, "v", doc.Path("a.2.k").Data())

	_, err = ParseJSON([]byte(`{"b":`))
	assert.Error(t, err)
}

func TestParseYAMLFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "doc.yaml")
	require.NoError(t, os.WriteFile(path, []byte("# top\na: 1\n"), 0o600))

	doc, err := ParseYAMLFile(path)
	require.NoError(t, err)
	assert.Equal(t, "# top\na: 1\n", doc.String())

	_, err = ParseYAMLFile("")
	assert.ErrorIs(t, err, ErrInvalidPath)

	_, err = ParseYAMLFile(filepath.Join(dir, "missing.yaml"))
	assert.ErrorIs(t, err, os.ErrNotExist)

	bad := filepath.Join(dir, "bad.yaml")
	require.NoError(t, os.WriteFile(bad, []byte("a: [\n"), 0o600))
	_, err = ParseYAMLFile(bad)
	assert.Error(t, err)
}

func TestParseYAMLBuffer(t *testing.T) {
	doc, err := ParseYAMLBuffer(strings.NewReader("z: 1\na: 2\n"))
	require.NoError(t, err)
	assert.Equal(t, "z: 1\na: 2\n", doc.String())

	readErr := errors.New("read failed")
	_, err = ParseYAMLBuffer(iotest.ErrReader(readErr))
	assert.ErrorIs(t, err, readErr)

	_, err = ParseYAMLBuffer(strings.NewReader("a: [\n"))
	assert.Error(t, err)
}

func TestNewFromData(t *testing.T) {
	ordered := orderedmap.New[string, interface{}]()
	ordered.Set("z", 1)
	ordered.Set("a", []interface{}{"x", int64(2), 1.5, false, nil})

	tests := []struct {
		name string
		data interface{}
		want string
	}{
		{name: "ordered map keeps key order", data: ordered, want: "z: 1\na:\n- x\n- 2\n- 1.5\n- false\n- null\n"},
		{name: "map", data: map[string]interface{}{"k": map[string]interface{}{"n": "v"}}, want: "k:\n  n: v\n"},
		{name: "sequence", data: []interface{}{1, "x"}, want: "- 1\n- x\n"},
		{name: "string scalar that looks like an int stays a string", data: "5", want: "\"5\"\n"},
		{name: "int scalar", data: 5, want: "5\n"},
		{name: "null", data: nil, want: "null\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			doc, err := NewFromData(tt.data)
			require.NoError(t, err)
			assert.Equal(t, tt.want, doc.String())
		})
	}

	_, err := NewFromData(struct{}{})
	assert.Error(t, err)
	_, err = NewFromData([]interface{}{struct{}{}})
	assert.Error(t, err)
	_, err = NewFromData(map[string]interface{}{"k": struct{}{}})
	assert.Error(t, err)
}

func TestWrapSetRootGetRoot(t *testing.T) {
	node := yaml.MustParse("a: 1\n")
	doc := Wrap(node)
	assert.Same(t, node, GetRoot(doc))
	assert.Equal(t, "a: 1\n", doc.String())

	// Edits through the wrapper land in the wrapped node.
	_, err := doc.SetP(2, "a")
	require.NoError(t, err)
	assert.Equal(t, "a: 2\n", node.MustString())

	other := yaml.MustParse("b: 3\n")
	SetRoot(doc, other)
	assert.Same(t, other, GetRoot(doc))
	assert.Equal(t, "b: 3\n", doc.String())
}

func TestDataOrdered(t *testing.T) {
	doc := mustParseYAMLDoc(t, "z: 1\na:\n- x\n- y: 2\n  b: 3\nm: null\n")

	om, ok := doc.DataOrdered().(*orderedmap.OrderedMap[string, interface{}])
	require.True(t, ok)
	assert.Equal(t, []string{"z", "a", "m"}, orderedKeys(om))
	assert.Equal(t, 1, om.Value("z"))
	assert.Nil(t, om.Value("m"))

	seq, ok := om.Value("a").([]interface{})
	require.True(t, ok)
	require.Len(t, seq, 2)
	assert.Equal(t, "x", seq[0])
	inner, ok := seq[1].(*orderedmap.OrderedMap[string, interface{}])
	require.True(t, ok)
	assert.Equal(t, []string{"y", "b"}, orderedKeys(inner))

	// The result feeds back into NewFromData without losing order.
	round, err := NewFromData(doc.DataOrdered())
	require.NoError(t, err)
	assert.Equal(t, doc.Data(), round.Data())
	assert.Equal(t, []string{"z", "a", "m"}, orderedKeys(round.DataOrdered().(*orderedmap.OrderedMap[string, interface{}])))

	assert.Nil(t, (*YamlDoc)(nil).DataOrdered())
	assert.Equal(t, true, mustParseYAMLDoc(t, "true\n").DataOrdered())
}

func TestDataOrderedDocumentNode(t *testing.T) {
	mapping := yaml.MustParse("b: 1\na: 2\n").YNode()
	doc := Wrap(yaml.NewRNode(&yaml.Node{Kind: yaml.DocumentNode, Content: []*yaml.Node{mapping}}))
	om, ok := doc.DataOrdered().(*orderedmap.OrderedMap[string, interface{}])
	require.True(t, ok)
	assert.Equal(t, []string{"b", "a"}, orderedKeys(om))
}

func orderedKeys(om *orderedmap.OrderedMap[string, interface{}]) []string {
	var keys []string
	for pair := om.Oldest(); pair != nil; pair = pair.Next() {
		keys = append(keys, pair.Key)
	}
	return keys
}
