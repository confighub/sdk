// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package yamlkit

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/confighub/sdk/core/third_party/gaby"
)

// elementsOf parses a document and returns the children of the named array.
func elementsOf(t *testing.T, data, path string) []*gaby.YamlDoc {
	t.Helper()
	docs, err := gaby.ParseAll([]byte(data))
	require.NoError(t, err)
	require.NotEmpty(t, docs)
	array := docs[0].Path(path)
	require.NotNil(t, array, "array %s missing", path)
	return array.Children()
}

// TestElementIdentityFromMarkup covers the structured comment a person writes to say which
// element of an unkeyed array this is. It is the only identity mechanism that lets someone
// correct the engine rather than work around it, so it takes precedence over everything the
// engine would infer on its own.
func TestElementIdentityFromMarkup(t *testing.T) {
	const doc = "spec:\n" +
		"  routes:\n" +
		"  # confighub:id=api\n" +
		"  - match: Host(`a.example.com`)\n" +
		"    kind: Rule\n" +
		"  - match: Host(`b.example.com`) # confighub:id=web\n" +
		"    kind: Rule\n" +
		"  - name: admin # confighub:id\n" +
		"    match: Host(`c.example.com`)\n" +
		"  - match: Host(`d.example.com`)\n" +
		"    kind: Rule\n"

	elements := elementsOf(t, doc, "spec.routes")
	require.Len(t, elements, 4)

	assert.Equal(t, "api", ElementIdentity(elements[0]),
		"a comment on the line above the element names it")
	assert.Equal(t, "web", ElementIdentity(elements[1]),
		"an inline comment lands on the first field, and still names the element")
	assert.Equal(t, "admin", ElementIdentity(elements[2]),
		"a bare directive nominates the field it sits on")
	assert.Equal(t, "", ElementIdentity(elements[3]),
		"an unmarked element has no identity, which is where it was before")
}

// TestElementIdentityMarkupOnScalars covers an args list, where the markup competes with the
// command-line-flag identity the engine infers by itself.
func TestElementIdentityMarkupOnScalars(t *testing.T) {
	const doc = "spec:\n" +
		"  args:\n" +
		"  - --log.level=INFO\n" +
		"  - --entryPoints.web.address=:80 # confighub:id=web-entrypoint\n" +
		"  # confighub:id=first-positional\n" +
		"  - serve\n" +
		"  - --quiet\n"

	elements := elementsOf(t, doc, "spec.args")
	require.Len(t, elements, 4)

	assert.Equal(t, "--log.level", ElementIdentity(elements[0]),
		"an unmarked flag keeps the identity the engine infers")
	assert.Equal(t, "web-entrypoint", ElementIdentity(elements[1]),
		"markup wins over the inferred flag name")
	assert.Equal(t, "first-positional", ElementIdentity(elements[2]),
		"a positional argument has no identity of its own, and markup gives it one")
	assert.Equal(t, "", ElementIdentity(elements[3]),
		"a bare flag is its own content, so there is nothing to separate out")
}

// TestElementIdentityMarkupIsIgnoredWhenUnusable covers what the engine does with markup it
// cannot put in a path. The identity is written into a `?~i=value;@N` segment, whose
// punctuation has no escape, so a value carrying it is ignored rather than corrupting the
// path — the element falls back to being matched by content and position.
func TestElementIdentityMarkupIsIgnoredWhenUnusable(t *testing.T) {
	const doc = "spec:\n" +
		"  routes:\n" +
		"  - match: a # confighub:id=has,comma\n" +
		"  - match: b # confighub:id=has=equals\n" +
		"  - match: c # confighub:id=has;semi\n" +
		"  - match: d # confighub:id=\n" +
		"  - match: e # confighub:id=has.dots.which.are.fine\n" +
		"  - match: f # this is just a comment about confighub:id\n"

	elements := elementsOf(t, doc, "spec.routes")
	require.Len(t, elements, 6)

	for i, name := range []string{"comma", "equals", "semicolon", "empty"} {
		assert.Equalf(t, "", ElementIdentity(elements[i]),
			"an identity containing a %s is not usable in a path segment", name)
	}
	assert.Equal(t, "has.dots.which.are.fine", ElementIdentity(elements[4]),
		"dots are escaped on the way into a path, so they are fine")
	assert.Equal(t, "", ElementIdentity(elements[5]),
		"prose that merely mentions the directive is not a directive")
}
