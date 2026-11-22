package gaby

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetComments(t *testing.T) {
	doc := mustParseYAMLDoc(t, "a: 1 # about a\nb: 2\n")
	assert.Equal(t, "# about a", doc.Path("a").GetComments())
	assert.Equal(t, "", doc.Path("b").GetComments())

	node := doc.Path("b").YNode()
	node.HeadComment = "# head"
	node.LineComment = "# line"
	node.FootComment = "# foot"
	assert.Equal(t, "# head\n# line\n# foot", doc.Path("b").GetComments())
}

func TestSetComment(t *testing.T) {
	doc := mustParseYAMLDoc(t, "# top\na: 1 # old\nb: 2\n")
	doc.Path("a").SetComment("new")
	doc.Path("b").SetComment("# added")
	assert.Equal(t, "# top\na: 1 # new\nb: 2 # added\n", doc.String())
	assert.Equal(t, "new", doc.Path("a").GetComments())
}

const commentKeysDoc = `a: 1
$comment$line$a: about a
b:
  $comment$head$c: above c
  c: 2
  l:
  - $comment$line$k: about k
    k: v
`

func TestStripCommentKeys(t *testing.T) {
	doc := mustParseYAMLDoc(t, commentKeysDoc)

	stripped := doc.StripCommentKeys()
	assert.Equal(t, "a: 1\nb:\n  c: 2\n  l:\n  - k: v\n", stripped.String())
	assert.Equal(t, commentKeysDoc, doc.String(), "the original is left untouched")

	assert.Equal(t, stripped.Bytes(), doc.BytesWithoutCommentKeys())

	j, err := doc.MarshalJSONWithoutCommentKeys()
	require.NoError(t, err)
	assert.JSONEq(t, `{"a":1,"b":{"c":2,"l":[{"k":"v"}]}}`, string(j))

	assert.Nil(t, (*YamlDoc)(nil).StripCommentKeys())
}

func TestStripCommentKeysKeepsLookalikes(t *testing.T) {
	// Only head, line and foot are comment kinds; other $comment$ keys are data.
	src := "$comment$other$a: x\n$comment$: y\na: 1 # native\n"
	doc := mustParseYAMLDoc(t, src)
	assert.Equal(t, src, doc.StripCommentKeys().String())
}

func TestGetCommentKeys(t *testing.T) {
	doc := mustParseYAMLDoc(t, "$comment$head$a: h\n$comment$line$a: l\n$comment$foot$a: f\na: 1\nb:\n  $comment$line$c: lc\n  c: 2\nlist: [x]\n")

	tests := []struct {
		path             string
		head, line, foot string
	}{
		{path: "a", head: "h", line: "l", foot: "f"},
		{path: "b.c", line: "lc"},
		{path: "b", head: "", line: "", foot: ""},
		{path: "zz.c"},
		{path: "list.0"},
		{path: ""},
	}
	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			head, line, foot := doc.GetCommentKeys(tt.path)
			assert.Equal(t, tt.head, head)
			assert.Equal(t, tt.line, line)
			assert.Equal(t, tt.foot, foot)
		})
	}
}

func TestSetCommentKey(t *testing.T) {
	doc := mustParseYAMLDoc(t, "a: 1\nb:\n  c: 2\n")

	require.NoError(t, doc.SetCommentKey("b.c", "line", "about c"))
	require.NoError(t, doc.SetCommentKey("a", "head", "above a"))
	// Setting again overwrites rather than adding a second key.
	require.NoError(t, doc.SetCommentKey("b.c", "line", "about c, again"))

	assert.Equal(t, "a: 1\nb:\n  c: 2\n  $comment$line$c: about c, again\n$comment$head$a: above a\n", doc.String())

	head, line, _ := doc.GetCommentKeys("b.c")
	assert.Equal(t, "", head)
	assert.Equal(t, "about c, again", line)
	head, _, _ = doc.GetCommentKeys("a")
	assert.Equal(t, "above a", head)

	assert.ErrorIs(t, doc.SetCommentKey("", "line", "x"), ErrInvalidPath)
	assert.Error(t, doc.SetCommentKey("a.b", "line", "x"), "a is a scalar, so it cannot hold a comment key")
}

func TestDeleteCommentKeysForPath(t *testing.T) {
	doc := mustParseYAMLDoc(t, "b:\n  $comment$head$c: x\n  $comment$line$c: y\n  c: 2\n  $comment$foot$c: z\n  $comment$line$d: other\n  d: 3\n")

	require.NoError(t, doc.DeleteCommentKeysForPath("b.c"))
	assert.Equal(t, "b:\n  c: 2\n  $comment$line$d: other\n  d: 3\n", doc.String())

	// Nothing left to delete, and paths that do not exist, are not errors.
	require.NoError(t, doc.DeleteCommentKeysForPath("b.c"))
	require.NoError(t, doc.DeleteCommentKeysForPath("zz.c"))
	require.NoError(t, doc.DeleteCommentKeysForPath(""))
	assert.Equal(t, "b:\n  c: 2\n  $comment$line$d: other\n  d: 3\n", doc.String())
}
