// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package api_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/confighub/sdk/core/function/api"
)

func guardAnnotations(entries map[string]string) api.PathAnnotations {
	return api.PathAnnotations{api.AnnotationKindGuard: entries}
}

func TestValidatePathAnnotationsAcceptsTheMotivatingCases(t *testing.T) {
	// The two classes §4 names, and a key whose presence is the whole statement.
	for _, entries := range []map[string]string{
		{"owner": "transform-link"},
		{"policy-exception": "host-network"},
		{"policy-exception": "run-as-root", "owner": "transform-link"},
		{"reviewed": ""},
		{"pinned-to": "1.21.4"},
	} {
		assert.NoError(t, api.ValidateUserPathAnnotations(guardAnnotations(entries)), "%v", entries)
	}
}

func TestValidatePathAnnotationsRejectsUnregisteredKind(t *testing.T) {
	err := api.ValidatePathAnnotations(api.PathAnnotations{
		api.AnnotationKind("Placeholder"): {"was": "confighubplaceholder"},
	})
	require.Error(t, err, "a kind no reader understands must not be storable")
	assert.Contains(t, err.Error(), "unknown annotation kind")
}

// TestValidateAnnotationValueRejectsExpressionSeparators is the reason values are restricted at
// all: a value travels through `In (a, b)` lists and --where-style expressions, so a value
// carrying one of those separators would parse as two.
func TestValidateAnnotationValueRejectsExpressionSeparators(t *testing.T) {
	for _, value := range []string{"host,network", "host network", "host'network", "a=b", "(a)", "a/b"} {
		assert.Error(t, api.ValidateAnnotationValue("policy-exception", value),
			"value %q carries a separator an expression form parses on", value)
	}
	// A dot is deliberately allowed -- it is what a version or a field name needs, and no
	// expression form splits on it.
	assert.NoError(t, api.ValidateAnnotationValue("pinned-to", "1.21.4"))
}

func TestValidateAnnotationKeyCharacterClassAndLength(t *testing.T) {
	assert.Error(t, api.ValidateAnnotationKey(""), "an empty key names no class of reason")
	assert.Error(t, api.ValidateAnnotationKey("-leading"), "must start alphanumeric, as AttributeName does")
	assert.Error(t, api.ValidateAnnotationKey("has.dot"), "a key takes the AttributeName class, which has no dot")
	assert.Error(t, api.ValidateAnnotationKey("has/slash"), "the namespace separator is deferred, not allowed")
	assert.NoError(t, api.ValidateAnnotationKey("policy-exception"))
	assert.NoError(t, api.ValidateAnnotationKey("owner_2"))

	atLimit := "k" + strings.Repeat("x", api.MaxAnnotationKeyLength-1)
	assert.NoError(t, api.ValidateAnnotationKey(atLimit))
	assert.Error(t, api.ValidateAnnotationKey(atLimit+"x"))

	valueAtLimit := strings.Repeat("v", api.MaxAnnotationValueLength)
	assert.NoError(t, api.ValidateAnnotationValue("k", valueAtLimit))
	assert.Error(t, api.ValidateAnnotationValue("k", valueAtLimit+"v"))
}

// TestReservedNamespaceIsClosedToUsers covers what §4 says must not be deferred: the room for a
// well-known key has to be taken before anyone authors keys in it, or adding one later collides
// with a key someone is already using.
func TestReservedNamespaceIsClosedToUsers(t *testing.T) {
	reserved := guardAnnotations(map[string]string{api.ReservedAnnotationKeyPrefix + "protected": "true"})

	err := api.ValidateUserPathAnnotations(reserved)
	require.Error(t, err, "a user must not be able to author a reserved key")
	assert.Contains(t, err.Error(), "reserved")

	assert.NoError(t, api.ValidatePathAnnotations(reserved),
		"ConfigHub's own writes use the same namespace, so the check belongs to the user path only")

	// Case is not a way around it.
	assert.Error(t, api.ValidateUserPathAnnotations(
		guardAnnotations(map[string]string{"ConfigHub-Protected": "true"})))
}

func TestValidatePathAnnotationsBoundsCounts(t *testing.T) {
	entries := map[string]string{}
	for i := range api.MaxAnnotationsPerPath {
		entries[string(rune('a'+i%26))+strings.Repeat("k", i/26+1)] = "v"
	}
	require.Len(t, entries, api.MaxAnnotationsPerPath)
	assert.NoError(t, api.ValidatePathAnnotations(guardAnnotations(entries)))

	entries["one-too-many"] = "v"
	assert.Error(t, api.ValidatePathAnnotations(guardAnnotations(entries)),
		"the column cannot be allowed to grow without limit")
}

func TestValidatePathAnnotationListNamesWhereItFailed(t *testing.T) {
	list := api.PathAnnotationList{{
		Resource: api.ResourceInfo{ResourceType: "apps/v1/Deployment", ResourceName: "ns/web"},
		PathAnnotationMap: map[api.ResolvedPath]api.PathAnnotations{
			"spec.template.spec.containers.?name=app.image": guardAnnotations(
				map[string]string{"owner": "transform,link"}),
		},
	}}
	err := api.ValidatePathAnnotationList(list, true)
	require.Error(t, err)
	// An error a person can act on says which resource and which path, not just that
	// something somewhere was malformed.
	assert.Contains(t, err.Error(), "ns/web")
	assert.Contains(t, err.Error(), "containers.?name=app.image")
}

func TestHasPathAnnotationsIsFalseForAnEmptyTable(t *testing.T) {
	assert.False(t, api.HasPathAnnotations(nil))
	assert.False(t, api.HasPathAnnotations(api.PathAnnotationList{}))
	assert.False(t, api.HasPathAnnotations(api.PathAnnotationList{{
		Resource:          api.ResourceInfo{ResourceName: "ns/web"},
		PathAnnotationMap: map[api.ResolvedPath]api.PathAnnotations{},
	}}), "a resource entry carrying nothing is not an annotation")

	assert.True(t, api.HasPathAnnotations(api.PathAnnotationList{{
		Resource:            api.ResourceInfo{ResourceName: "ns/web"},
		ResourceAnnotations: guardAnnotations(map[string]string{"owner": "transform-link"}),
	}}))
}

// TestClonePathAnnotationListSharesNothing is what clone, restore, and every transient copy
// depend on: the maps go three deep, so a shallow copy aliases the annotations themselves and
// an edit to the copy reaches the stored value.
func TestClonePathAnnotationListSharesNothing(t *testing.T) {
	original := api.PathAnnotationList{{
		Resource:             api.ResourceInfo{ResourceType: "apps/v1/Deployment", ResourceName: "ns/web"},
		ResourceAnnotations:  guardAnnotations(map[string]string{"owner": "base"}),
		AliasesWithoutScopes: map[api.ResourceName]struct{}{"web": {}},
		PathAnnotationMap: map[api.ResolvedPath]api.PathAnnotations{
			"spec.template.spec.containers.?name=app.image": guardAnnotations(
				map[string]string{"owner": "transform-link"}),
		},
	}}

	cloned := api.ClonePathAnnotationList(original)
	require.Equal(t, original, cloned, "a clone is equal to what it copied")

	const path = api.ResolvedPath("spec.template.spec.containers.?name=app.image")
	cloned[0].PathAnnotationMap[path][api.AnnotationKindGuard]["owner"] = "someone-else"
	cloned[0].ResourceAnnotations[api.AnnotationKindGuard]["owner"] = "someone-else"
	cloned[0].AliasesWithoutScopes["renamed"] = struct{}{}
	cloned[0].PathAnnotationMap["spec.replicas"] = guardAnnotations(map[string]string{"owner": "new"})

	assert.Equal(t, "transform-link", original[0].PathAnnotationMap[path][api.AnnotationKindGuard]["owner"])
	assert.Equal(t, "base", original[0].ResourceAnnotations[api.AnnotationKindGuard]["owner"])
	assert.NotContains(t, original[0].AliasesWithoutScopes, api.ResourceName("renamed"))
	assert.NotContains(t, original[0].PathAnnotationMap, api.ResolvedPath("spec.replicas"))

	assert.Nil(t, api.ClonePathAnnotationList(nil), "nil clones to nil, so an absent table stays absent")
}

// ---------------------------------------------------------------------------
// Clearances
// ---------------------------------------------------------------------------

func clearance(requirements ...api.ClearanceRequirement) api.Clearance {
	return api.Clearance(requirements)
}

func exists(key string) api.ClearanceRequirement {
	return api.ClearanceRequirement{Key: key, Operator: api.ClearanceOperatorExists}
}

func in(key string, values ...string) api.ClearanceRequirement {
	return api.ClearanceRequirement{Key: key, Operator: api.ClearanceOperatorIn, Values: values}
}

func notIn(key string, values ...string) api.ClearanceRequirement {
	return api.ClearanceRequirement{Key: key, Operator: api.ClearanceOperatorNotIn, Values: values}
}

func doesNotExist(key string) api.ClearanceRequirement {
	return api.ClearanceRequirement{Key: key, Operator: api.ClearanceOperatorDoesNotExist}
}

// TestAnEmptyClearanceClearsNothing is the protect-by-default rule, and the reason a guard is
// worth anything: an operation that says nothing about guards cannot write a guarded path.
func TestAnEmptyClearanceClearsNothing(t *testing.T) {
	guards := map[string]string{"owner": "transform-link"}

	admitted, withheld := api.Clearance(nil).Admits(guards)
	assert.False(t, admitted)
	assert.Equal(t, "owner", withheld.Key)

	// An unguarded path is admitted by an empty clearance, which is the overwhelming
	// majority of paths and the case that has to stay free.
	admitted, _ = api.Clearance(nil).Admits(nil)
	assert.True(t, admitted)
}

func TestClearanceOperatorsAdmitTheRightValues(t *testing.T) {
	guards := map[string]string{"policy-exception": "host-network"}

	for _, tc := range []struct {
		name   string
		c      api.Clearance
		admits bool
	}{
		{"Exists admits any value", clearance(exists("policy-exception")), true},
		{"In admits a listed value", clearance(in("policy-exception", "host-network")), true},
		{"In refuses an unlisted value", clearance(in("policy-exception", "run-as-root")), false},
		{"NotIn admits an unlisted value", clearance(notIn("policy-exception", "run-as-root")), true},
		{"NotIn refuses a listed value", clearance(notIn("policy-exception", "host-network")), false},
		{"a requirement for another key clears nothing", clearance(exists("owner")), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			admitted, _ := tc.c.Admits(guards)
			assert.Equal(t, tc.admits, admitted)
		})
	}
}

// TestEveryGuardKeyMustBeCleared is rule 1: a clearance that covers one reason but not another
// does not get to write the path. This is what makes a narrow clearance narrow.
func TestEveryGuardKeyMustBeCleared(t *testing.T) {
	guards := map[string]string{"owner": "transform-link", "policy-exception": "host-network"}

	admitted, withheld := clearance(exists("owner")).Admits(guards)
	assert.False(t, admitted, "clearing one of two reasons is not clearing the path")
	assert.Equal(t, "policy-exception", withheld.Key)

	admitted, _ = clearance(exists("owner"), in("policy-exception", "host-network")).Admits(guards)
	assert.True(t, admitted, "both reasons covered")
}

// TestDoesNotExistIsAPrecondition is rule 2: it expresses "I am cleared for owner guards, but
// never touch anything carrying a policy-exception", so it withholds regardless of what else
// the clearance covers.
func TestDoesNotExistIsAPrecondition(t *testing.T) {
	guards := map[string]string{"owner": "transform-link", "policy-exception": "host-network"}

	c := clearance(exists("owner"), doesNotExist("policy-exception"))
	admitted, withheld := c.Admits(guards)
	assert.False(t, admitted)
	assert.Equal(t, "policy-exception", withheld.Key)
	assert.True(t, withheld.Precondition,
		"the report distinguishes a key the operation forbade from one it merely did not mention")

	// It only fires on a path that actually carries the key.
	admitted, _ = c.Admits(map[string]string{"owner": "transform-link"})
	assert.True(t, admitted)

	// A clearance that both forbids a key and would otherwise clear it reports the
	// forbidding, being the more deliberate of the two statements.
	both := clearance(exists("policy-exception"), doesNotExist("policy-exception"))
	admitted, withheld = both.Admits(map[string]string{"policy-exception": "host-network"})
	assert.False(t, admitted)
	assert.True(t, withheld.Precondition)
}

// TestTheMotivatingLinkCase is §5's example in executable form: guard the image with
// owner=transform-link, give the TransformPaths link that clearance and the UpgradeUnit link
// nothing, and the upgrade stops overwriting the image while the transform still maintains it.
func TestTheMotivatingLinkCase(t *testing.T) {
	imageGuards := map[string]string{"owner": "transform-link"}

	transformLink := clearance(in("owner", "transform-link"))
	admitted, _ := transformLink.Admits(imageGuards)
	assert.True(t, admitted, "the link that maintains the field may write it")

	upgradeLink := api.Clearance(nil)
	admitted, withheld := upgradeLink.Admits(imageGuards)
	assert.False(t, admitted, "the upgrade is not cleared, so it does not overwrite the image")
	assert.Equal(t, "owner", withheld.Key)
	assert.Equal(t, "transform-link", withheld.Value)
}

func TestValidateClearance(t *testing.T) {
	assert.NoError(t, api.ValidateClearance(clearance(exists("owner"), in("policy-exception", "host-network"))))
	assert.Error(t, api.ValidateClearance(clearance(api.ClearanceRequirement{Key: "owner", Operator: "Maybe"})),
		"an unknown operator must not be silently ignored")
	assert.Error(t, api.ValidateClearance(clearance(api.ClearanceRequirement{
		Key: "owner", Operator: api.ClearanceOperatorIn})), "In with no values admits nothing and is a mistake")
	assert.Error(t, api.ValidateClearance(clearance(api.ClearanceRequirement{
		Key: "owner", Operator: api.ClearanceOperatorExists, Values: []string{"x"}})),
		"Exists takes no values, and accepting them would suggest they mattered")
	assert.Error(t, api.ValidateClearance(clearance(exists("bad.key"))))
}

// ---------------------------------------------------------------------------
// Guard resolution
// ---------------------------------------------------------------------------

// TestGuardsForPathInheritsDownTheChain covers what makes a guard forward-looking policy: a
// guard on a subtree covers what is inside it, including what is added later.
func TestGuardsForPathInheritsDownTheChain(t *testing.T) {
	entry := &api.ResourcePathAnnotations{
		Resource:            api.ResourceInfo{ResourceName: "ns/web"},
		ResourceAnnotations: guardAnnotations(map[string]string{"owner": "platform"}),
		PathAnnotationMap: map[api.ResolvedPath]api.PathAnnotations{
			"spec.template.spec.containers": guardAnnotations(
				map[string]string{"policy-exception": "run-as-root"}),
			"spec.template.spec.containers.?name=app.image": guardAnnotations(
				map[string]string{"pinned-to": "1.21.4"}),
		},
	}

	// A container that did not exist when the guard was written still inherits it.
	guards := entry.GuardsForPath("spec.template.spec.containers.?name=future.image")
	assert.Equal(t, map[string]string{
		"policy-exception": "run-as-root",
		"owner":            "platform",
	}, guards)

	// A path with a guard of its own keeps the ones above it too: annotating a path is not a
	// way to escape the policy covering it.
	guards = entry.GuardsForPath("spec.template.spec.containers.?name=app.image")
	assert.Equal(t, map[string]string{
		"pinned-to":        "1.21.4",
		"policy-exception": "run-as-root",
		"owner":            "platform",
	}, guards)

	// A path outside the guarded subtree carries only the resource's.
	guards = entry.GuardsForPath("spec.replicas")
	assert.Equal(t, map[string]string{"owner": "platform"}, guards)
}

func TestGuardsForPathNearestWinsOnTheSameKey(t *testing.T) {
	entry := &api.ResourcePathAnnotations{
		ResourceAnnotations: guardAnnotations(map[string]string{"owner": "platform"}),
		PathAnnotationMap: map[api.ResolvedPath]api.PathAnnotations{
			"spec.template.spec.containers": guardAnnotations(map[string]string{"owner": "base"}),
			"spec.template.spec.containers.?name=app.image": guardAnnotations(
				map[string]string{"owner": "transform-link"}),
		},
	}
	guards := entry.GuardsForPath("spec.template.spec.containers.?name=app.image")
	assert.Equal(t, map[string]string{"owner": "transform-link"}, guards,
		"the more specific statement about the same class of reason wins")
}

func TestGuardsForPathIsNilWhenThereAreNone(t *testing.T) {
	entry := &api.ResourcePathAnnotations{
		Resource: api.ResourceInfo{ResourceName: "ns/web"},
		PathAnnotationMap: map[api.ResolvedPath]api.PathAnnotations{
			"spec.replicas": guardAnnotations(map[string]string{"owner": "platform"}),
		},
	}
	assert.Nil(t, entry.GuardsForPath("spec.template.spec"),
		"an unguarded path costs nothing, which is the case almost every path is in")

	var absent *api.ResourcePathAnnotations
	assert.Nil(t, absent.GuardsForPath("spec.replicas"))
}

// ---------------------------------------------------------------------------
// Guard propagation
// ---------------------------------------------------------------------------

func table(resource string, paths map[api.ResolvedPath]map[string]string) api.PathAnnotationList {
	entry := api.ResourcePathAnnotations{
		Resource:          api.ResourceInfo{ResourceType: "apps/v1/Deployment", ResourceName: api.ResourceName(resource)},
		PathAnnotationMap: map[api.ResolvedPath]api.PathAnnotations{},
	}
	for path, guards := range paths {
		entry.PathAnnotationMap[path] = guardAnnotations(guards)
	}
	return api.PathAnnotationList{entry}
}

// TestDiffGuardsCarriesARemoval is why guards propagate by diff rather than by union. When a
// workload stops needing hostNetwork and the base drops the exception, the variants have to drop
// it too -- a union would leave every clone carrying a stale guard nobody can account for.
func TestDiffGuardsCarriesARemoval(t *testing.T) {
	base := table("ns/web", map[api.ResolvedPath]map[string]string{
		"spec.template.spec": {"policy-exception": "host-network", "owner": "platform"},
	})
	end := table("ns/web", map[api.ResolvedPath]map[string]string{
		"spec.template.spec": {"owner": "platform"},
	})

	diffs := api.DiffGuards(base, end)
	require.Len(t, diffs, 1)
	require.Len(t, diffs[0].Deltas, 1)
	assert.Equal(t, api.ResolvedPath("spec.template.spec"), diffs[0].Deltas[0].Path)
	assert.Equal(t, []string{"policy-exception"}, diffs[0].Deltas[0].Remove)
	assert.Empty(t, diffs[0].Deltas[0].Set)
}

func TestDiffGuardsCarriesAdditionsAndChanges(t *testing.T) {
	base := table("ns/web", map[api.ResolvedPath]map[string]string{
		"spec.replicas": {"owner": "platform"},
	})
	end := table("ns/web", map[api.ResolvedPath]map[string]string{
		"spec.replicas": {"owner": "sre"},
		"spec.template.spec.containers.?name=app.image": {"owner": "transform-link"},
	})

	diffs := api.DiffGuards(base, end)
	require.Len(t, diffs, 1)
	byPath := map[api.ResolvedPath]api.GuardDelta{}
	for _, delta := range diffs[0].Deltas {
		byPath[delta.Path] = delta
	}
	assert.Equal(t, map[string]string{"owner": "sre"}, byPath["spec.replicas"].Set,
		"a changed value is a change, not a no-op")
	assert.Equal(t, map[string]string{"owner": "transform-link"},
		byPath["spec.template.spec.containers.?name=app.image"].Set)
}

func TestDiffGuardsIsEmptyWhenNothingChanged(t *testing.T) {
	same := table("ns/web", map[api.ResolvedPath]map[string]string{
		"spec.replicas": {"owner": "platform"},
	})
	assert.Empty(t, api.DiffGuards(same, api.ClonePathAnnotationList(same)),
		"an unchanged table produces no work for the merge to do")
}

// TestDiffGuardsReportsAResourceThatIsGone covers the entry the end no longer has: everything it
// carried was removed, and the removal has to travel rather than being lost with the entry.
func TestDiffGuardsReportsAResourceThatIsGone(t *testing.T) {
	base := table("ns/web", map[api.ResolvedPath]map[string]string{
		"spec.replicas": {"owner": "platform"},
	})
	diffs := api.DiffGuards(base, nil)
	require.Len(t, diffs, 1)
	require.Len(t, diffs[0].Deltas, 1)
	assert.Equal(t, []string{"owner"}, diffs[0].Deltas[0].Remove)
}

func TestApplyGuardDeltaRoundTrips(t *testing.T) {
	base := table("ns/web", map[api.ResolvedPath]map[string]string{
		"spec.template.spec": {"policy-exception": "host-network", "owner": "platform"},
	})
	end := table("ns/web", map[api.ResolvedPath]map[string]string{
		"spec.template.spec":                            {"owner": "sre"},
		"spec.template.spec.containers.?name=app.image": {"owner": "transform-link"},
	})

	// Applying the base->end diff to a copy of base reproduces end. This is the property the
	// whole propagation rests on: what a downstream receives is what the upstream changed.
	downstream := api.ClonePathAnnotationList(base)
	for _, diff := range api.DiffGuards(base, end) {
		for i := range diff.Deltas {
			var changed bool
			downstream, changed = api.ApplyGuardDelta(downstream, diff.Resource, &diff.Deltas[i])
			assert.True(t, changed, "a delta the diff produced must change something")
		}
	}

	assert.Equal(t, map[string]string{"owner": "sre"},
		downstream[0].PathAnnotationMap["spec.template.spec"][api.AnnotationKindGuard])
	assert.Equal(t, map[string]string{"owner": "transform-link"},
		downstream[0].PathAnnotationMap["spec.template.spec.containers.?name=app.image"][api.AnnotationKindGuard])
}

// TestApplyGuardDeltaIsIdempotent matters because a merge can be replayed, and a propagation
// that reported a change every time it re-ran would make every replay look like new work.
func TestApplyGuardDeltaIsIdempotent(t *testing.T) {
	downstream := api.PathAnnotationList{}
	resource := api.ResourceInfo{ResourceType: "apps/v1/Deployment", ResourceName: "ns/web"}
	delta := api.GuardDelta{
		Path: "spec.replicas",
		Set:  map[string]string{"owner": "platform"},
	}

	downstream, changed := api.ApplyGuardDelta(downstream, resource, &delta)
	assert.True(t, changed)

	_, changed = api.ApplyGuardDelta(downstream, resource, &delta)
	assert.False(t, changed, "applying the same delta again is not a change")
}
