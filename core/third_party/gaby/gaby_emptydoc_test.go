package gaby

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestParseEmptyDoc(t *testing.T) {
	sample := []byte(`---
# This is a comment
# Another comment
---
apiVersion: v1
kind: Namespace
metadata:
  name: test
  labels:
    test: test
spec: {}
`)
	doc, err := ParseAll(sample)
	assert.NoError(t, err)

	newDoc, err := ParseAll([]byte(doc.String()))
	assert.NoError(t, err)

	// This was the old behavior
	// assert.Equal(t, "# This is a comment\n# Another comment", newDoc[0].YNode().HeadComment)
	//
	// The new behavior is to exclude the empty doc out of the list
	//
	// assigned to variables for debugging
	doc0String := doc[0].String()
	newDoc0String := newDoc[0].String()
	assert.Equal(t, doc0String, newDoc0String)
}

func TestParseEmptyDocWithAnExtraNewLine(t *testing.T) {
	sample := []byte(`---
# This is a comment
# Another comment

---
apiVersion: v1
kind: Namespace
metadata:
  name: test
  labels:
    test: test
spec: {}
`)
	doc, err := ParseAll(sample)
	assert.NoError(t, err)

	newDoc, err := ParseAll([]byte(doc.String()))
	assert.NoError(t, err)

	// This was the old behavior
	// assert.Equal(t, "# This is a comment\n# Another comment", newDoc[0].YNode().HeadComment)
	//
	// The new behavior is to exclude the empty doc out of the list
	//
	// assigned to variables for debugging
	doc0String := doc[0].String()
	newDoc0String := newDoc[0].String()
	assert.Equal(t, doc0String, newDoc0String)
}
