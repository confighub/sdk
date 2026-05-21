// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package kubernetes

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/confighub/sdk/core/function/api"
	"github.com/confighub/sdk/core/third_party/gaby"
)

// configMapDoc renders a minimal v1/ConfigMap document with the given
// ResourceNameStableCore and RevisionNum annotations. If immutable is true,
// the document includes `immutable: true`.
func configMapDoc(name, stableCore, revisionNum string, immutable bool, asLatest bool) string {
	var sb strings.Builder
	sb.WriteString("apiVersion: v1\n")
	sb.WriteString("kind: ConfigMap\n")
	sb.WriteString("metadata:\n")
	sb.WriteString("  name: " + name + "\n")
	sb.WriteString("  namespace: default\n")
	sb.WriteString("  annotations:\n")
	sb.WriteString("    confighub.com/ResourceNameStableCore: " + stableCore + "\n")
	sb.WriteString("    confighub.com/RevisionNum: \"" + revisionNum + "\"\n")
	if asLatest {
		sb.WriteString("    confighub.com/RenderRevision: Latest\n")
	}
	if immutable {
		sb.WriteString("immutable: true\n")
	}
	sb.WriteString("data:\n  key: value\n")
	return sb.String()
}

func joinDocs(docs ...string) string {
	return strings.Join(docs, "---\n")
}

func TestPruneConfigMaps_PrunesByRevisionAndRemarksLatest(t *testing.T) {
	// Five immutable ConfigMaps in the same group; out-of-order RevisionNums.
	input := joinDocs(
		configMapDoc("cm-aaa", "cm", "2", true, true),
		configMapDoc("cm-bbb", "cm", "5", true, false),
		configMapDoc("cm-ccc", "cm", "1", true, false),
		configMapDoc("cm-ddd", "cm", "4", true, false),
		configMapDoc("cm-eee", "cm", "3", true, false),
	)
	parsed, err := gaby.ParseAll([]byte(input))
	require.NoError(t, err)

	args := []api.FunctionArgument{{ParameterName: "revision-history-limit", Value: 3}}
	out, _, err := fnPruneConfigMaps(testResourceProvider, nil, parsed, args, nil)
	require.NoError(t, err)

	// We expect 3 ConfigMaps to survive: RevisionNums 5, 4, 3 (cm-bbb, cm-ddd, cm-eee).
	require.Equal(t, 3, len(out), "expected 3 ConfigMaps after pruning")

	names := map[string]bool{}
	latestCount := 0
	for _, doc := range out {
		name, _ := doc.Path("metadata.name").Data().(string)
		names[name] = true
		if rr := doc.Path("metadata.annotations.confighub~1com/RenderRevision"); rr != nil {
			if s, _ := rr.Data().(string); s == "Latest" {
				latestCount++
			}
		}
	}
	assert.True(t, names["cm-bbb"], "newest cm-bbb should be retained")
	assert.True(t, names["cm-ddd"], "cm-ddd should be retained")
	assert.True(t, names["cm-eee"], "cm-eee should be retained")
	assert.False(t, names["cm-aaa"], "cm-aaa should be pruned")
	assert.False(t, names["cm-ccc"], "cm-ccc should be pruned")
	assert.Equal(t, 1, latestCount, "exactly one ConfigMap should remain marked Latest")
}

func TestPruneConfigMaps_LeavesMutableUntouched(t *testing.T) {
	input := configMapDoc("cm-mutable", "cm", "7", false, false)
	parsed, err := gaby.ParseAll([]byte(input))
	require.NoError(t, err)
	originalCount := len(parsed)

	args := []api.FunctionArgument{{ParameterName: "revision-history-limit", Value: 1}}
	out, _, err := fnPruneConfigMaps(testResourceProvider, nil, parsed, args, nil)
	require.NoError(t, err)
	assert.Equal(t, originalCount, len(out), "mutable ConfigMaps must not be pruned")
}

func TestPruneConfigMaps_MultipleGroupsIndependent(t *testing.T) {
	input := joinDocs(
		configMapDoc("a-1", "a", "1", true, false),
		configMapDoc("a-2", "a", "2", true, false),
		configMapDoc("a-3", "a", "3", true, false),
		configMapDoc("b-1", "b", "1", true, false),
		configMapDoc("b-2", "b", "2", true, false),
	)
	parsed, err := gaby.ParseAll([]byte(input))
	require.NoError(t, err)

	args := []api.FunctionArgument{{ParameterName: "revision-history-limit", Value: 2}}
	out, _, err := fnPruneConfigMaps(testResourceProvider, nil, parsed, args, nil)
	require.NoError(t, err)
	// a: prune 1 (keep a-2, a-3); b: keep both.
	require.Equal(t, 4, len(out))
	names := map[string]bool{}
	for _, doc := range out {
		name, _ := doc.Path("metadata.name").Data().(string)
		names[name] = true
	}
	assert.False(t, names["a-1"])
	assert.True(t, names["a-2"])
	assert.True(t, names["a-3"])
	assert.True(t, names["b-1"])
	assert.True(t, names["b-2"])
}

func TestPruneConfigMaps_UnderLimitIsNoop(t *testing.T) {
	input := joinDocs(
		configMapDoc("c-1", "c", "1", true, false),
		configMapDoc("c-2", "c", "2", true, true),
	)
	parsed, err := gaby.ParseAll([]byte(input))
	require.NoError(t, err)

	args := []api.FunctionArgument{{ParameterName: "revision-history-limit", Value: 10}}
	out, _, err := fnPruneConfigMaps(testResourceProvider, nil, parsed, args, nil)
	require.NoError(t, err)
	require.Equal(t, 2, len(out))
	// Latest should be on the highest RevisionNum (c-2); the older one should be marked IgnoreProvided.
	for _, doc := range out {
		name, _ := doc.Path("metadata.name").Data().(string)
		rr := ""
		if n := doc.Path("metadata.annotations.confighub~1com/RenderRevision"); n != nil {
			rr, _ = n.Data().(string)
		}
		vo := ""
		if n := doc.Path("metadata.annotations.confighub~1com/VisitorOptions"); n != nil {
			vo, _ = n.Data().(string)
		}
		if name == "c-2" {
			assert.Equal(t, "Latest", rr, "newest should be Latest")
		} else if name == "c-1" {
			assert.Equal(t, "", rr, "older entries should not be marked Latest")
			assert.Equal(t, "IgnoreProvided", vo, "older retained entries should be marked IgnoreProvided")
		}
	}
}
