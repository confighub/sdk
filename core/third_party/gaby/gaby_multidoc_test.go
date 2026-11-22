package gaby

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMultiDoc(t *testing.T) {
	sample := []byte(
		`test:
  value: 10
---
test2: 20
`)
	val, err := ParseAll(sample)
	if err != nil {
		t.Errorf("Failed to parse: %v", err)
		return
	}
	if val.Search("test", "value").Data() == nil {
		t.Errorf("Didn't find test.value")
	}
	if val.Search("test2").Data() == nil {
		t.Errorf("Didn't find test2")
	}
}

func TestStrangeMultiDoc(t *testing.T) {
	sample := []byte(
		`---

apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: cert-manager-edit
  labels:
    app: cert-manager
    app.kubernetes.io/name: cert-manager
    app.kubernetes.io/instance: cert-manager
    app.kubernetes.io/component: "controller"
    app.kubernetes.io/version: "v1.17.2"
    app.kubernetes.io/managed-by: Helm
    helm.sh/chart: cert-manager-v1.17.2
    rbac.authorization.k8s.io/aggregate-to-edit: "true"
    rbac.authorization.k8s.io/aggregate-to-admin: "true"
rules:
  - apiGroups: ["cert-manager.io"]
    resources: ["certificates", "certificaterequests", "issuers"]
    verbs: ["create", "delete", "deletecollection", "patch", "update"]
  - apiGroups: ["cert-manager.io"]
    resources: ["certificates/status"]
    verbs: ["update"]
  - apiGroups: ["acme.cert-manager.io"]
    resources: ["challenges", "orders"]
    verbs: ["create", "delete", "deletecollection", "patch", "update"]

---# Permission to approve CertificateRequests referencing cert-manager.io Issuers and ClusterIssuers
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: cert-manager-controller-approve:cert-manager-io
  labels:
    app: cert-manager
    app.kubernetes.io/name: cert-manager
    app.kubernetes.io/instance: cert-manager
    app.kubernetes.io/component: "cert-manager"
    app.kubernetes.io/version: "v1.17.2"
    app.kubernetes.io/managed-by: Helm
    helm.sh/chart: cert-manager-v1.17.2
rules:
  - apiGroups: ["cert-manager.io"]
    resources: ["signers"]
    verbs: ["approve"]
    resourceNames:
    - "issuers.cert-manager.io/*"
    - "clusterissuers.cert-manager.io/*"
`)
	docs, err := ParseAll(sample)
	assert.NoError(t, err, "Error parsing YAML")
	assert.Equal(t, 2, len(docs), "Expected 2 documents")
}

// TestParseAllDocumentBoundaries covers how text is divided into documents, and what
// Container.String renders from them. Every function call on a Unit parses it with ParseAll
// and, if the function writes, stores what String renders, so a document or comment missed
// here is deleted from the Unit.
func TestParseAllDocumentBoundaries(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		wantDocs int
		want     string
	}{
		{"one document", "a: 1\n", 1, "a: 1\n"},
		{"two documents", "a: 1\n---\nb: 2\n", 2, "a: 1\n---\nb: 2\n"},
		{"empty", "", 0, ""},
		{"only a separator", "---\n", 0, ""},
		{"leading separator", "---\na: 1\n", 1, "---\na: 1\n"},
		{"trailing separator", "a: 1\n---\n", 1, "a: 1\n"},
		{"empty document between", "a: 1\n---\n---\nb: 2\n", 2, "a: 1\n---\nb: 2\n"},
		{"CRLF line endings", "a: 1\r\n---\r\nb: 2\r\n", 2, "a: 1\n---\nb: 2\n"},
		{"sequence document", "- x\n- y\n---\nb: 2\n", 2, "- x\n- y\n---\nb: 2\n"},

		// A separator is "---" followed by whitespace or the end of the line, and what
		// follows it on the line belongs to the next document.
		{"separator with trailing space", "a: 1\n--- \nb: 2\n", 2, "a: 1\n---\nb: 2\n"},
		{"separator with trailing tab", "a: 1\n---\t\nb: 2\n", 2, "a: 1\n---\nb: 2\n"},
		{"separator with trailing space and CRLF", "a: 1\r\n--- \r\nb: 2\r\n", 2, "a: 1\n---\nb: 2\n"},
		{"separator with trailing space in the middle", "a: 1\n--- \nb: 2\n---\nc: 3\n", 3, "a: 1\n---\nb: 2\n---\nc: 3\n"},
		{"leading separator with trailing space", "--- \na: 1\n", 1, "---\na: 1\n"},
		{"separator with a tag", "a: 1\n--- !!map\nb: 2\n", 2, "a: 1\n---\n!!map\nb: 2\n"},
		{"separator with flow content", "a: 1\n--- {b: 2}\n", 2, "a: 1\n---\n{b: 2}\n"},
		{"separator with a block scalar", "a: 1\n--- |\n  text\n", 2, "a: 1\n---\n|\n  text\n"},
		{"separator with a comment", "a: 1\n--- # about b\nb: 2\n", 2, "a: 1\n---\n# about b\nb: 2\n"},
		{"separator directly followed by a comment", "a: 1\n---# about b\nb: 2\n", 2, "a: 1\n---\n# about b\nb: 2\n"},

		// "..." ends a document, whether or not "---" follows.
		{"document end then separator", "a: 1\n...\n---\nb: 2\n", 2, "a: 1\n---\nb: 2\n"},
		{"document end without separator", "a: 1\n...\nb: 2\n", 2, "a: 1\n---\nb: 2\n"},
		{"document end with a comment", "a: 1\n... # end of a\n---\nb: 2\n", 2, "a: 1\n# end of a\n---\nb: 2\n"},

		// These hold "---" or "..." but are not document markers.
		{"dashes in a value", "a: ---\n", 1, "a: '---'\n"},
		{"dashes and hash in a quoted value", "a: \"x---#y\"\n", 1, "a: \"x---#y\"\n"},
		{"dashes in a block scalar", "a: |\n  ---\n  b\n", 1, "a: |\n  ---\n  b\n"},
		{"dots in a block scalar", "a: |\n  ...\n  b\n", 1, "a: |\n  ...\n  b\n"},

		// Comments in documents of their own.
		{"comments before the first separator", "# head\n---\na: 1\n", 1, "# head\n---\na: 1\n"},
		{"comments before the first separator, two documents", "# head\n---\na: 1\n---\nb: 2\n", 2, "# head\n---\na: 1\n---\nb: 2\n"},
		{"comment-only document between", "a: 1\n---\n# between\n---\nb: 2\n", 2, "a: 1\n---\n# between\nb: 2\n"},
		{"comment heading a document", "a: 1\n---\n# about b\nb: 2\n", 2, "a: 1\n---\n# about b\nb: 2\n"},
		{"trailing comment-only document", "a: 1\n---\n# trailing\n", 1, "a: 1\n\n# trailing\n"},
		{"two trailing comment-only documents", "a: 1\n---\n# t1\n---\n# t2\n", 1, "a: 1\n\n# t1\n# t2\n"},
		{"trailing comment-only document after a foot comment", "a: 1\n# own\n---\n# trailing\n", 1, "a: 1\n# own\n\n# trailing\n"},
		{"trailing comment-only document after a sequence", "- x\n---\n# trailing\n", 1, "- x\n\n# trailing\n"},
		{"trailing comment-only document after a line comment and a foot comment", "a: 1 # x\n# own\n---\n# trailing\n", 1, "a: 1 # x\n\n# own\n# trailing\n"},
		{"trailing comment-only document after a sequence with a line comment and a foot comment", "- x # y\n# own\n---\n# trailing\n", 1, "- x # y\n\n# own\n# trailing\n"},
		{"foot comment", "a: 1\n# foot\n", 1, "a: 1\n# foot\n"},

		// Data that is only comments has no documents to hold them. Callers that write
		// data back keep the original text in this case.
		{"only comments", "# only\n", 0, ""},
		{"only comments around separators", "# only\n---\n# more\n", 0, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			docs, err := ParseAll([]byte(tt.input))
			require.NoError(t, err)
			assert.Len(t, docs, tt.wantDocs)
			got := docs.String()
			assert.Equal(t, tt.want, got)

			// What String renders must parse back to the same documents, or a Unit would
			// change every time a function rewrote it.
			again, err := ParseAll([]byte(got))
			require.NoError(t, err)
			assert.Len(t, again, len(docs))
			assert.Equal(t, got, again.String())
		})
	}
}

// TestParseAllKeepsEveryDocumentsData checks the documents' data, not only how they render.
func TestParseAllKeepsEveryDocumentsData(t *testing.T) {
	docs, err := ParseAll([]byte("a: 1\n--- \nb: 2\n--- !!map\nc: 3\n... # done\nd: 4\n"))
	require.NoError(t, err)
	require.Len(t, docs, 4)
	for i, key := range []string{"a", "b", "c", "d"} {
		assert.Equal(t, i+1, docs[i].Search(key).Data(), "document %d", i)
	}
}

// TestParseAllReportsErrorsInLaterDocuments checks that a malformed document is an error
// wherever it is, rather than the end of what is read.
func TestParseAllReportsErrorsInLaterDocuments(t *testing.T) {
	for name, input := range map[string]string{
		"first document":                  "a: [1\n---\nb: 2\n",
		"second document":                 "a: 1\n---\nb: [2\n",
		"content on the separator line":   "a: 1\n--- [\nb: 2\n",
		"four dashes are not a separator": "a: 1\n----\nb: 2\n",
	} {
		t.Run(name, func(t *testing.T) {
			_, err := ParseAll([]byte(input))
			assert.Error(t, err)
		})
	}
}

func TestDocumentMarker(t *testing.T) {
	tests := []struct {
		line     string
		wantOK   bool
		wantRest string
	}{
		{"---", true, ""},
		{"--- ", true, ""},
		{"---\t", true, ""},
		{"--- !!map", true, "!!map"},
		{"---   {a: 1}", true, "{a: 1}"},
		{"--- # comment", true, "# comment"},
		{"---# comment", true, "# comment"},
		{"...", true, ""},
		{"... # comment", true, "# comment"},
		{"----", false, ""},
		{"---a", false, ""},
		{"....", false, ""},
		{"...a", false, ""},
		{" ---", false, ""},
		{"--", false, ""},
		{"", false, ""},
		{"a: ---", false, ""},
	}
	for _, tt := range tests {
		rest, ok := documentMarker(tt.line)
		assert.Equal(t, tt.wantOK, ok, "%q", tt.line)
		assert.Equal(t, tt.wantRest, rest, "%q", tt.line)
	}
}

func TestNormalizeYAML(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"CRLF", "a: 1\r\nb: 2\r\n", "a: 1\nb: 2"},
		{"surrounding whitespace", "\n\n  a: 1\n\n", "a: 1"},
		{"leading separator", "---\na: 1\n", "a: 1"},
		{"trailing separator", "a: 1\n---\n", "a: 1"},
		{"comment on a separator", "a: 1\n--- # b\nb: 2", "a: 1\n---\n # b\nb: 2"},
		{"comment directly on a separator", "a: 1\n---# b\nb: 2", "a: 1\n---\n# b\nb: 2"},
		{"dashes and hash in a value", "a: \"x---#y\"", "a: \"x---#y\""},
		{"dashes and comment in a value", "a: b --- # c", "a: b --- # c"},
	}
	for _, tt := range tests {
		assert.Equal(t, tt.want, NormalizeYAML(tt.input), tt.name)
	}
}

func TestDocComment(t *testing.T) {
	docs, err := ParseAll([]byte("# about the first\n---\na: 1\n---\nb: 2\n"))
	require.NoError(t, err)
	require.Len(t, docs, 2)

	comment, ok := docs[0].GetDocComment()
	assert.True(t, ok)
	assert.Equal(t, "# about the first", comment)
	_, ok = docs[1].GetDocComment()
	assert.False(t, ok, "only text before the first separator is a doc comment")

	// The doc comment is not data.
	assert.Equal(t, "a: 1\n", docs[0].stringWithoutDocComment())

	docs[1].SetDocComment("about the second")
	assert.Equal(t, "# about the first\n---\na: 1\n# about the second\n---\nb: 2\n", docs.String())

	docs[0].SetDocComment("")
	assert.Equal(t, "---\na: 1\n# about the second\n---\nb: 2\n", docs.String(), "an empty doc comment keeps only the separator")
}

func TestContainerSearchAndData(t *testing.T) {
	docs, err := ParseAll([]byte("a:\n  b: 1\n---\nc: 2\n---\na:\n  b: 3\n"))
	require.NoError(t, err)

	found := docs.Search("a", "b")
	require.Len(t, found, 2)
	assert.Equal(t, 1, found[0].Data())
	assert.Equal(t, 3, found[1].Data())
	assert.Equal(t, 1, found.Data(), "Data is the first document's")

	assert.Nil(t, docs.Search("missing"))
	assert.Nil(t, Container(nil).Data())
	assert.Equal(t, "", Container(nil).String())
	assert.Equal(t, []byte(docs.String()), docs.Bytes())
}
