// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package cubkit

import (
	"strings"
	"testing"

	"github.com/confighub/sdk/core/configkit/yamlkit"
)

// Comment handling for the ConfigHub/YAML converter.
//
// NativeToYAML and YAMLToNative move comments between native YAML head/line/foot comments and
// the $comment$TYPE$TARGET map keys that carry them through formats with no comments of their
// own. yamlkit's comments_test.go covers the key encoding itself; this covers the converter that
// applies it to ConfigHub's own entity documents, which had no tests of its own.
//
// Three properties, in order of how much depends on them: comments survive the round trip, the
// round trip is idempotent so any normalisation it applies happens at most once, and
// configuration with no comments is returned byte for byte.

const commentedEntities = `# leading comment block
# second line of the leading block
apiVersion: confighub.com/v1
kind: Space
metadata:
  name: staging # inline comment on name
spec:
  # head comment on the label map
  labels:
    Environment: staging
`

// TestNativeToYAMLEncodesCommentsAsKeys pins the encoding, because everything else depends on
// it. The leading block is keyed with an empty target, which is how a comment belonging to the
// document rather than to any of its fields is represented.
func TestNativeToYAMLEncodesCommentsAsKeys(t *testing.T) {
	converted, err := NewConfigHubResourceProvider().NativeToYAML([]byte(commentedEntities))
	if err != nil {
		t.Fatalf("NativeToYAML: %v", err)
	}
	yaml := string(converted)
	t.Logf("YAML:\n%s", yaml)

	for _, want := range []string{
		yamlkit.CommentKey(yamlkit.CommentHead, ""),
		yamlkit.CommentKey(yamlkit.CommentHead, "labels"),
		yamlkit.CommentKey(yamlkit.CommentLine, "name"),
	} {
		if !strings.Contains(yaml, want) {
			t.Errorf("NativeToYAML did not emit the comment key %q", want)
		}
	}
}

// TestRoundTripPreservesComments is the behaviour that lets a ConfigHub/YAML Unit pass through
// the JSON projection without the author losing what they wrote.
func TestRoundTripPreservesComments(t *testing.T) {
	provider := NewConfigHubResourceProvider()

	converted, err := provider.NativeToYAML([]byte(commentedEntities))
	if err != nil {
		t.Fatalf("NativeToYAML: %v", err)
	}
	restored, err := provider.YAMLToNative(converted)
	if err != nil {
		t.Fatalf("YAMLToNative: %v", err)
	}
	native := string(restored)
	t.Logf("native:\n%s", native)

	for _, comment := range []string{
		"# leading comment block",
		"# second line of the leading block",
		"# inline comment on name",
		"# head comment on the label map",
	} {
		if !strings.Contains(native, comment) {
			t.Errorf("round trip lost the comment %q", comment)
		}
	}
	if !strings.Contains(native, "Environment: staging") {
		t.Errorf("round trip lost a value")
	}
	if strings.Contains(native, yamlkit.CommentKeyPrefix) {
		t.Errorf("the native form leaked the synthetic comment keys")
	}
}

// TestRoundTripIsIdempotent is the property that matters most and is the easiest to lose by
// accident. The round trip is not byte-for-byte lossless -- it normalises the column an inline
// comment sits in, among other cosmetics -- so what makes the native form safe to regenerate is
// that a second pass changes nothing.
func TestRoundTripIsIdempotent(t *testing.T) {
	provider := NewConfigHubResourceProvider()

	roundTrip := func(t *testing.T, in []byte) []byte {
		t.Helper()
		converted, err := provider.NativeToYAML(in)
		if err != nil {
			t.Fatalf("NativeToYAML: %v", err)
		}
		restored, err := provider.YAMLToNative(converted)
		if err != nil {
			t.Fatalf("YAMLToNative: %v", err)
		}
		return restored
	}

	first := roundTrip(t, []byte(commentedEntities))
	second := roundTrip(t, first)

	if string(first) != string(second) {
		t.Errorf("a second round trip must be a fixed point, so normalisation happens at most once\nfirst:\n%s\nsecond:\n%s",
			string(first), string(second))
	}
}

// TestRoundTripWithoutCommentsIsUnchanged is the base case, worth asserting separately so that
// the conversion is provably invisible to the configuration that carries no comments at all.
func TestRoundTripWithoutCommentsIsUnchanged(t *testing.T) {
	provider := NewConfigHubResourceProvider()

	const config = `apiVersion: confighub.com/v1
kind: Space
metadata:
  name: staging
`

	converted, err := provider.NativeToYAML([]byte(config))
	if err != nil {
		t.Fatalf("NativeToYAML: %v", err)
	}
	restored, err := provider.YAMLToNative(converted)
	if err != nil {
		t.Fatalf("YAMLToNative: %v", err)
	}
	if string(restored) != config {
		t.Errorf("round trip changed comment-free configuration\nwant:\n%s\ngot:\n%s", config, string(restored))
	}
}

// TestEmptyDataIsUnchanged covers the early return in both directions. Empty configuration is
// not a hypothetical: every Unit starts with an empty Revision.
func TestEmptyDataIsUnchanged(t *testing.T) {
	provider := NewConfigHubResourceProvider()

	converted, err := provider.NativeToYAML(nil)
	if err != nil {
		t.Fatalf("NativeToYAML: %v", err)
	}
	if len(converted) != 0 {
		t.Errorf("NativeToYAML(nil) returned %q", string(converted))
	}

	restored, err := provider.YAMLToNative(nil)
	if err != nil {
		t.Fatalf("YAMLToNative: %v", err)
	}
	if len(restored) != 0 {
		t.Errorf("YAMLToNative(nil) returned %q", string(restored))
	}
}
