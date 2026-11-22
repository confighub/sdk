package gaby

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// renderingsCorpus is every YAML file under the repository's test-data that parses, plus the
// document boundary cases ParseAll handles specially.
func renderingsCorpus(t *testing.T) map[string]string {
	t.Helper()
	corpus := map[string]string{
		"header comment":            "# Header\n---\nreplicas: 3 # inline\n",
		"bare separator first":      "---\na: 1\n---\nb: 2\n",
		"comment between documents": "a: 1\n---\n# between\n---\nb: 2\n",
		"trailing comment":          "a: 1\n---\n# trailing\n",
		"document comment block":    "# about a\n\na: 1\n---\nb: 2 # line\n",
		"flow document":             "a: 1\n--- {b: 2}\n",
		"empty":                     "",
	}
	root := filepath.Join("..", "..", "..", "..", "test-data")
	_ = filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || (filepath.Ext(path) != ".yaml" && filepath.Ext(path) != ".yml") {
			return nil //nolint:nilerr // an unreadable entry is skipped
		}
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			return nil //nolint:nilerr // an unreadable file is skipped
		}
		if _, parseErr := ParseAll(data); parseErr == nil {
			corpus[path] = string(data)
		}
		return nil
	})
	return corpus
}

// Concatenating the renderings is String, and each document's text stands alone: it parses to
// one document that renders as the same text, without the document comment, which is in the
// prefix.
func TestRenderingsJoinToString(t *testing.T) {
	corpus := renderingsCorpus(t)
	require.Greater(t, len(corpus), 50, "the test-data corpus was not found")
	for name, input := range corpus {
		docs, err := ParseAll([]byte(input))
		require.NoError(t, err, name)
		renderings := docs.Renderings()
		var joined strings.Builder
		for i, rendering := range renderings {
			joined.WriteString(rendering.Prefix)
			joined.WriteString(rendering.Text)
			if i > 0 {
				assert.True(t, strings.HasSuffix(rendering.Prefix, "---\n"), "%s: document %d has no separator", name, i)
			}

			alone, err := ParseAll([]byte(rendering.Text))
			require.NoError(t, err, "%s: document %d", name, i)
			require.Len(t, alone, 1, "%s: document %d", name, i)
			_, hasDocComment := alone[0].GetDocComment()
			assert.False(t, hasDocComment, "%s: document %d", name, i)
			assert.Equal(t, rendering.Text, alone.String(), "%s: document %d", name, i)
		}
		assert.Equal(t, docs.String(), joined.String(), name)
	}
}

func TestRenderingsPrefixes(t *testing.T) {
	docs, err := ParseAll([]byte("# Header\n---\na: 1\n---\nb: 2\n"))
	require.NoError(t, err)
	assert.Equal(t, []DocumentRendering{
		{Prefix: "# Header\n---\n", Text: "a: 1\n"},
		{Prefix: "---\n", Text: "b: 2\n"},
	}, docs.Renderings())

	docs, err = ParseAll([]byte("---\na: 1\n"))
	require.NoError(t, err)
	assert.Equal(t, []DocumentRendering{{Prefix: "---\n", Text: "a: 1\n"}}, docs.Renderings())

	assert.Empty(t, Container{}.Renderings())
}
