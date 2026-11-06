// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package k8skit_test

import (
	"strconv"
	"strings"
	"testing"

	"github.com/confighub/sdk/configkit/k8skit"
	"github.com/confighub/sdk/core/configkit/yamlkit"
	"github.com/confighub/sdk/core/function/api"
	"github.com/confighub/sdk/core/third_party/gaby"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A list with no merge key names its elements by content in a stored record. Protection a
// downstream Unit put on a field of such an element has to keep protecting it while the element
// and the list around it change upstream, which is when protection matters.

func externalSecret(entries ...string) string {
	return `apiVersion: external-secrets.io/v1
kind: ExternalSecret
metadata:
  name: app-env
  namespace: default
spec:
  data:
` + strings.Join(entries, "")
}

func secretEntry(secretKey, key, extra string) string {
	return "  - secretKey: " + secretKey + "\n    remoteRef:\n      key: " + key + "\n" + extra
}

func parseConfig(t *testing.T, data string) gaby.Container {
	t.Helper()
	parsed, err := gaby.ParseAll([]byte(data))
	require.NoError(t, err)
	return parsed
}

// downstreamRecord is the record a downstream Unit holds after one protected change from base to
// target: the change's diff, every path it wrote protected, as --protect records it, kept against
// the data it describes as every write keeps it.
func downstreamRecord(t *testing.T, base, target string) api.ResourceMutationList {
	t.Helper()
	provider := k8skit.NewK8sResourceProvider()
	record, err := yamlkit.ComputeMutations(parseConfig(t, base), parseConfig(t, target), 1, provider)
	require.NoError(t, err)
	for i := range record {
		for path, info := range record[i].PathMutationMap {
			info.Protected = true
			record[i].PathMutationMap[path] = info
		}
	}
	record, _ = yamlkit.ReanchorStoredPaths(record, parseConfig(t, base), parseConfig(t, target), provider)
	return record
}

// recordWrite is what a Unit's record becomes when a write takes its data from before to after:
// the write's own changes added, unprotected, as merged content is, and the whole kept against
// after.
func recordWrite(t *testing.T, record api.ResourceMutationList, before, after string, index int64) api.ResourceMutationList {
	t.Helper()
	provider := k8skit.NewK8sResourceProvider()
	diff, err := yamlkit.ComputeMutations(parseConfig(t, before), parseConfig(t, after), index, provider)
	require.NoError(t, err)
	record, _ = yamlkit.AddMutations(provider, record, diff)
	record, _ = yamlkit.ReanchorStoredPaths(record, parseConfig(t, before), parseConfig(t, after), provider)
	return record
}

func mergeUpstream(t *testing.T, target string, record api.ResourceMutationList,
	upstreamBefore, upstreamAfter string) (string, api.MutationConflictList) {
	t.Helper()
	provider := k8skit.NewK8sResourceProvider()
	patch, err := yamlkit.ComputeMutations(parseConfig(t, upstreamBefore), parseConfig(t, upstreamAfter), 2, provider)
	require.NoError(t, err)
	patched, conflicts, err := yamlkit.PatchMutations(parseConfig(t, target), record, patch, nil, provider, nil)
	require.NoError(t, err)
	return string(patched.Bytes()), conflicts
}

var (
	clonedSecret = externalSecret(
		secretEntry("OLD_KEY", "/placeholder/env/OLD_KEY", ""),
		secretEntry("SIGNING_KEY", "/placeholder/env/SIGNING_KEY", ""),
	)
	// The downstream Unit points the signing key at its own parameter, protected.
	downstreamSecret = externalSecret(
		secretEntry("OLD_KEY", "/placeholder/env/OLD_KEY", ""),
		secretEntry("SIGNING_KEY", "/prod/env/SIGNING_KEY", ""),
	)
)

// The base case, which works: upstream changes the protected field of an element nothing else
// has touched since the clone.
func TestPatch_AnchoredProtectionHoldsOnAnUntouchedElement(t *testing.T) {
	record := downstreamRecord(t, clonedSecret, downstreamSecret)
	upstream := externalSecret(
		secretEntry("OLD_KEY", "/placeholder/env/OLD_KEY", ""),
		secretEntry("SIGNING_KEY", "/placeholder/v2/SIGNING_KEY", ""),
	)

	got, _ := mergeUpstream(t, downstreamSecret, record, clonedSecret, upstream)
	assert.Contains(t, got, "key: /prod/env/SIGNING_KEY")
}

// Upstream first edits another field of the same element, then the protected one. The element's
// digest upstream is no longer the one the downstream record names it by.
func TestPatch_AnchoredProtectionHoldsAfterUpstreamEditsTheElement(t *testing.T) {
	record := downstreamRecord(t, clonedSecret, downstreamSecret)
	upstreamEdited := externalSecret(
		secretEntry("OLD_KEY", "/placeholder/env/OLD_KEY", ""),
		secretEntry("SIGNING_KEY", "/placeholder/env/SIGNING_KEY", "      nullBytePolicy: Ignore\n"),
	)
	// The downstream Unit takes the first upstream change; its record keeps the protected entry.
	afterFirst, _ := mergeUpstream(t, downstreamSecret, record, clonedSecret, upstreamEdited)
	require.Contains(t, afterFirst, "key: /prod/env/SIGNING_KEY")
	require.Contains(t, afterFirst, "nullBytePolicy: Ignore")

	upstreamRenamed := externalSecret(
		secretEntry("OLD_KEY", "/placeholder/env/OLD_KEY", ""),
		secretEntry("SIGNING_KEY", "/placeholder/v2/SIGNING_KEY", "      nullBytePolicy: Ignore\n"),
	)
	record = recordWrite(t, record, downstreamSecret, afterFirst, 2)

	got, conflicts := mergeUpstream(t, afterFirst, record, upstreamEdited, upstreamRenamed)
	assert.Contains(t, got, "key: /prod/env/SIGNING_KEY", "the protected value must survive")
	assert.NotEmpty(t, conflicts, "and the withheld change must be reported")
}

// Upstream removes the element before the protected one, then changes the protected field. The
// element's neighbors and the list's length are no longer the ones the record names it by.
func TestPatch_AnchoredProtectionHoldsAfterUpstreamRemovesANeighbor(t *testing.T) {
	record := downstreamRecord(t, clonedSecret, downstreamSecret)
	upstreamShorter := externalSecret(
		secretEntry("SIGNING_KEY", "/placeholder/env/SIGNING_KEY", ""),
	)
	afterFirst, _ := mergeUpstream(t, downstreamSecret, record, clonedSecret, upstreamShorter)
	require.NotContains(t, afterFirst, "OLD_KEY")
	require.Contains(t, afterFirst, "key: /prod/env/SIGNING_KEY")

	upstreamRenamed := externalSecret(
		secretEntry("SIGNING_KEY", "/placeholder/v2/SIGNING_KEY", ""),
	)
	record = recordWrite(t, record, downstreamSecret, afterFirst, 2)

	got, _ := mergeUpstream(t, afterFirst, record, upstreamShorter, upstreamRenamed)
	assert.Contains(t, got, "key: /prod/env/SIGNING_KEY", "the protected value must survive")
}

// A guard written by position on a list with no merge key. The patch names the element by
// content, and the guard has to be found all the same.
func TestPatch_PositionalGuardHoldsAgainstAnAnchoredPatch(t *testing.T) {
	provider := k8skit.NewK8sResourceProvider()
	upstream := externalSecret(
		secretEntry("OLD_KEY", "/placeholder/env/OLD_KEY", ""),
		secretEntry("SIGNING_KEY", "/placeholder/v2/SIGNING_KEY", ""),
	)
	patch, err := yamlkit.ComputeMutations(parseConfig(t, clonedSecret), parseConfig(t, upstream), 2, provider)
	require.NoError(t, err)
	guards := &yamlkit.GuardFilter{
		Annotations: api.PathAnnotationList{{
			Resource: externalSecretResource,
			PathAnnotationMap: map[api.ResolvedPath]api.PathAnnotations{
				"spec.data.1.remoteRef.key": {api.AnnotationKindGuard: {"owner": "platform-team"}},
			},
		}},
	}

	patched, conflicts, err := yamlkit.PatchMutationsGuarded(parseConfig(t, downstreamSecret), nil, patch, nil,
		guards, provider, nil)
	require.NoError(t, err)

	got := string(patched.Bytes())
	assert.Contains(t, got, "key: /prod/env/SIGNING_KEY", "the guarded value must survive")
	require.NotEmpty(t, conflicts)
	assert.Equal(t, api.ConflictReasonGuarded, conflicts[0].Reason)
}

func keysOf(record api.ResourceMutationList) []string {
	var keys []string
	for path := range record[0].PathMutationMap {
		keys = append(keys, string(path))
	}
	return keys
}

// positionsOf maps the positional form of each key of a record to its entry, resolved against data.
func positionsOf(t *testing.T, record api.ResourceMutationList, data string) map[string]api.MutationInfo {
	t.Helper()
	doc := parseConfig(t, data)[0]
	byPosition := map[string]api.MutationInfo{}
	for path, info := range record[0].PathMutationMap {
		positional, ok := yamlkit.ResolveStoredAnchors(doc, string(path))
		require.True(t, ok, "key %s must resolve against the data it is stored with", path)
		byPosition[positional] = info
	}
	return byPosition
}

// Removing an element drops what the record says about its fields, which would otherwise be read
// as being about whatever takes the element's place, and keeps the record of the removal.
func TestReanchor_DropsKeysInsideARemovedElement(t *testing.T) {
	record := downstreamRecord(t, clonedSecret, externalSecret(
		secretEntry("OLD_KEY", "/prod/env/OLD_KEY", ""),
		secretEntry("SIGNING_KEY", "/prod/env/SIGNING_KEY", ""),
	))
	before := externalSecret(
		secretEntry("OLD_KEY", "/prod/env/OLD_KEY", ""),
		secretEntry("SIGNING_KEY", "/prod/env/SIGNING_KEY", ""),
	)
	after := externalSecret(secretEntry("SIGNING_KEY", "/prod/env/SIGNING_KEY", ""))
	record = recordWrite(t, record, before, after, 2)

	var removals, others int
	for path, info := range record[0].PathMutationMap {
		if info.MutationType == api.MutationTypeDelete {
			removals++
			continue
		}
		others++
		positional, ok := yamlkit.ResolveStoredAnchors(parseConfig(t, after)[0], string(path))
		require.True(t, ok, "%s must name an element that is there", path)
		assert.Equal(t, "spec.data.0.remoteRef.key", positional)
		assert.True(t, info.Protected, "the surviving element keeps its protection")
	}
	assert.Equal(t, 1, removals, "the removal itself is recorded: %v", keysOf(record))
	assert.Equal(t, 1, others, "nothing is left about the removed element's fields: %v", keysOf(record))
}

// A key by bare index, as a record written before anchors holds, names the element at that index
// in the data the write found. It follows the element past an insertion ahead of it, and is
// rewritten as the element's anchor.
func TestReanchor_FollowsABareIndexPastAnInsertion(t *testing.T) {
	record := api.ResourceMutationList{{
		Resource:             externalSecretResource,
		ResourceMutationInfo: api.MutationInfo{MutationType: api.MutationTypeUpdate, Index: 1},
		PathMutationMap: api.MutationMap{
			"spec.data.1.remoteRef.key": {MutationType: api.MutationTypeUpdate, Index: 1, Protected: true,
				Value: "/prod/env/SIGNING_KEY\n"},
		},
	}}
	after := externalSecret(
		secretEntry("NEW_KEY", "/placeholder/env/NEW_KEY", ""),
		secretEntry("OLD_KEY", "/placeholder/env/OLD_KEY", ""),
		secretEntry("SIGNING_KEY", "/prod/env/SIGNING_KEY", ""),
	)
	record = recordWrite(t, record, downstreamSecret, after, 2)

	entry, found := positionsOf(t, record, after)["spec.data.2.remoteRef.key"]
	require.True(t, found, "the entry follows its element to index 2: %v", keysOf(record))
	assert.True(t, entry.Protected)
	for _, key := range keysOf(record) {
		assert.NotContains(t, key, "spec.data.1.", "no entry is left at the old index")
	}
}

// Two identical elements share a digest; the neighbors and length the key records say which one
// it names.
func TestResolveStoredAnchors_TellsIdenticalElementsApartByTheirNeighbors(t *testing.T) {
	data := externalSecret(
		secretEntry("A", "/same", ""),
		secretEntry("B", "/b", ""),
		secretEntry("A", "/same", ""),
		secretEntry("C", "/c", ""),
	)
	doc := parseConfig(t, data)[0]
	elements := doc.Path("spec.data").Children()
	for _, index := range []int{0, 2} {
		anchor := string(yamlkit.CanonicalMutationPath(api.ResolvedPath(yamlkit.AnchoredPathSegment(elements, index, true))))
		positional, ok := yamlkit.ResolveStoredAnchors(doc, "spec.data."+anchor+".secretKey")
		require.True(t, ok)
		assert.Equal(t, "spec.data."+strconv.Itoa(index)+".secretKey", positional)
	}
}

// Protection set by position on a list with no merge key changes the entry the record has for the
// element, which names it by content, rather than adding a second entry beside it.
func TestSetProtection_ByPositionChangesTheAnchoredEntry(t *testing.T) {
	provider := k8skit.NewK8sResourceProvider()
	record := downstreamRecord(t, clonedSecret, downstreamSecret)
	require.Len(t, record[0].PathMutationMap, 1)

	updated, unresolved := yamlkit.SetProtection(parseConfig(t, downstreamSecret), record, externalSecretResource,
		map[api.ResolvedPath]bool{"spec.data.1.remoteRef.key": false}, provider)
	assert.Empty(t, unresolved)
	require.Len(t, updated[0].PathMutationMap, 1, "no second entry: %v", keysOf(updated))
	for _, info := range updated[0].PathMutationMap {
		assert.False(t, info.Protected)
	}
}

// A guard on a field of an element follows the element as protection does.
func TestReanchor_GuardFollowsItsElement(t *testing.T) {
	provider := k8skit.NewK8sResourceProvider()
	guards := api.PathAnnotationList{{
		Resource: externalSecretResource,
		PathAnnotationMap: map[api.ResolvedPath]api.PathAnnotations{
			"spec.data.1.remoteRef.key": {api.AnnotationKindGuard: {"owner": "platform-team"}},
		},
	}}
	after := externalSecret(
		secretEntry("NEW_KEY", "/placeholder/env/NEW_KEY", ""),
		secretEntry("OLD_KEY", "/placeholder/env/OLD_KEY", ""),
		secretEntry("SIGNING_KEY", "/prod/env/SIGNING_KEY", ""),
	)
	moved, changed := yamlkit.ReanchorPathAnnotations(guards, parseConfig(t, downstreamSecret), parseConfig(t, after), provider)
	require.True(t, changed)
	require.Len(t, moved[0].PathAnnotationMap, 1)
	for path := range moved[0].PathAnnotationMap {
		positional, ok := yamlkit.ResolveStoredAnchors(parseConfig(t, after)[0], string(path))
		require.True(t, ok)
		assert.Equal(t, "spec.data.2.remoteRef.key", positional)
	}
}

// An element a write inserts that is identical to one already in the list is recorded by its
// content, which the existing element shares. The insertion's entry has to land on the element
// the write inserted, not on the one that was already there.
func TestReanchor_InsertionOfADuplicateLandsOnTheInsertedElement(t *testing.T) {
	before := externalSecret(
		secretEntry("B", "/b", ""),
		secretEntry("A", "/a", ""),
	)
	after := externalSecret(
		secretEntry("B", "/b", ""),
		secretEntry("A", "/a", ""),
		secretEntry("B", "/b", ""),
	)
	record := recordWrite(t, nil, before, after, 2)

	byPosition := positionsOf(t, record, after)
	entry, found := byPosition["spec.data.2"]
	require.True(t, found, "the insertion is recorded on the element it inserted: %v", keysOf(record))
	assert.Equal(t, api.MutationTypeAdd, entry.MutationType)
	_, onExisting := byPosition["spec.data.0"]
	assert.False(t, onExisting, "nothing is recorded on the element that was already there")
}

// An escaped dot is not an anchor: a record of label edits has nothing to re-anchor, and a write
// need not parse its data to find that out.
func TestStoredPathsMayNeedReanchoring_IgnoresEscapedDots(t *testing.T) {
	record := api.ResourceMutationList{{
		Resource: externalSecretResource,
		PathMutationMap: api.MutationMap{
			"metadata.labels.app~1kubernetes~1io/name": {MutationType: api.MutationTypeUpdate},
		},
	}}
	assert.False(t, yamlkit.StoredPathsMayNeedReanchoring(record, nil))
	record[0].PathMutationMap["spec.data.?~h=8b7b7275,~p=^,~n=d013,~l=3.secretKey"] = api.MutationInfo{MutationType: api.MutationTypeUpdate}
	assert.True(t, yamlkit.StoredPathsMayNeedReanchoring(record, nil))
}

// A write's diff names an element of a list with no merge key as the write found it, and the
// record the write stores names it as the write left it. ReanchoredKeys takes the one to the other.
func TestReanchoredKeys_TakesAWritesDiffToTheRecordItStored(t *testing.T) {
	provider := k8skit.NewK8sResourceProvider()
	after := externalSecret(
		secretEntry("OLD_KEY", "/placeholder/env/OLD_KEY", ""),
		secretEntry("SIGNING_KEY", "/placeholder/env/SIGNING_KEY-v1", ""),
	)
	diff, err := yamlkit.ComputeMutations(parseConfig(t, clonedSecret), parseConfig(t, after), 1, provider)
	require.NoError(t, err)
	record, _ := yamlkit.ReanchorStoredPaths(diff, parseConfig(t, clonedSecret), parseConfig(t, after), provider)

	keys := yamlkit.ReanchoredKeys(&diff[0], parseConfig(t, clonedSecret), parseConfig(t, after), provider)
	require.Len(t, keys, 1, "the edited element's key moves: %v", keysOf(diff))
	for path, key := range keys {
		assert.NotEqual(t, path, key)
		_, found := record[0].PathMutationMap[key]
		assert.True(t, found, "%s is a key of the record %v", key, keysOf(record))
	}
}
