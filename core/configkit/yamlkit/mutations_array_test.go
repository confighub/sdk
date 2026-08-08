// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package yamlkit

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/confighub/sdk/core/function/api"
	"github.com/confighub/sdk/core/third_party/gaby"
)

// These tests cover merging of positional (non-merge-keyed) arrays: the ones the resource
// provider declares no merge key for, such as a Traefik IngressRoute's spec.routes or a
// plain list of arguments. An element removed from — or inserted into — the middle of such
// an array used to be diffed as a cascade of Updates over every element after it, which
// overwrote whatever the downstream copy had customized in those elements. The engine now
// records the structural change itself, so a merge disturbs no more of the target than a
// textual patch of the same change would.

// routesDoc builds an IngressRoute-like resource whose spec.routes elements are maps.
// Each element is given as "match|priority".
func routesDoc(elements ...string) string {
	var b strings.Builder
	b.WriteString("apiVersion: traefik.io/v1alpha1\nkind: IngressRoute\nmetadata:\n  name: ir\n  namespace: ns\nspec:\n  routes:\n")
	for _, e := range elements {
		match, priority, _ := strings.Cut(e, "|")
		fmt.Fprintf(&b, "  - match: %s\n    priority: %s\n", match, priority)
	}
	b.WriteString("  tls:\n    certResolver: letsencrypt\n")
	return b.String()
}

// argsDoc builds a resource whose spec.args is an array of scalars.
func argsDoc(elements ...string) string {
	var b strings.Builder
	b.WriteString("apiVersion: v1\nkind: Thing\nmetadata:\n  name: t\n  namespace: ns\nspec:\n  args:\n")
	for _, e := range elements {
		fmt.Fprintf(&b, "  - %s\n", e)
	}
	return b.String()
}

func parseDocs(t *testing.T, data string) gaby.Container {
	t.Helper()
	parsed, err := gaby.ParseAll([]byte(data))
	require.NoError(t, err)
	return parsed
}

// arrayElements returns one string per element of the array at path, with newlines
// flattened, so an expected array can be written as a slice of literals.
func arrayElements(t *testing.T, parsed gaby.Container, path string) []string {
	t.Helper()
	require.NotEmpty(t, parsed)
	array := parsed[0].Path(path)
	require.NotNil(t, array, "array %s missing from %s", path, parsed.String())
	elements := []string{}
	for _, element := range array.Children() {
		elements = append(elements, strings.Join(strings.Fields(strings.TrimSpace(element.String())), " "))
	}
	return elements
}

// mutationTypes returns path -> MutationType for a single-resource mutation list, with
// each element's anchor reduced to the index it carries. What these cases are about is
// which element the patch names and what it does to it; the anchor that says which element
// it is has its own tests (TestMergeAnchorLocatesMovedElement).
func mutationTypes(t *testing.T, mutations api.ResourceMutationList) map[string]api.MutationType {
	t.Helper()
	require.Len(t, mutations, 1)
	types := map[string]api.MutationType{}
	for path, info := range mutations[0].PathMutationMap {
		types[StripAssociativeSegments(string(path))] = info.MutationType
	}
	return types
}

// mergeArrayCase is one upstream change replayed onto a downstream copy that has its own
// customizations, checked with subtraction both off (the default: the stored protection
// decide what may be overwritten, and this test supplies none) and on.
type mergeArrayCase struct {
	name      string
	arrayPath string
	// base is the shared ancestor, upstream the changed source, downstream the
	// customized target.
	base, upstream, downstream string
	// wantPatch is the exact set of path mutations the upstream change should produce.
	wantPatch map[string]api.MutationType
	// want is the merged array, element by element.
	want []string
}

func runMergeArrayCase(t *testing.T, c mergeArrayCase) {
	t.Helper()
	base := parseDocs(t, c.base)

	patch, err := ComputeMutations(base, parseDocs(t, c.upstream), 1, testProvider)
	require.NoError(t, err)
	assert.Equal(t, c.wantPatch, mutationTypes(t, patch), "patch for %s", c.name)

	targetDiff, err := ComputeMutations(base, parseDocs(t, c.downstream), 2, testProvider)
	require.NoError(t, err)

	merged, _, err := PatchMutations(parseDocs(t, c.downstream), nil, patch, nil, testProvider, nil)
	require.NoError(t, err)
	assert.Equal(t, c.want, arrayElements(t, merged, c.arrayPath), "merged without subtraction")

	mergedSubtracted, _, err := PatchMutations(parseDocs(t, c.downstream), nil, patch, targetDiff, testProvider, nil)
	require.NoError(t, err)
	assert.Equal(t, c.want, arrayElements(t, mergedSubtracted, c.arrayPath), "merged with subtraction")
}

func TestMergePositionalArrayRemoval(t *testing.T) {
	cases := []mergeArrayCase{
		{
			// The reported case: a Traefik IngressRoute with two routes, both match
			// rules customized downstream, whose first route is dropped upstream.
			name:       "remove the first of two elements",
			arrayPath:  "spec.routes",
			base:       routesDoc("up-a|100", "up-b|50"),
			upstream:   routesDoc("up-b|50"),
			downstream: routesDoc("down-a|100", "down-b|50"),
			wantPatch:  map[string]api.MutationType{"spec.routes.0": api.MutationTypeDelete},
			want:       []string{"match: down-b priority: 50"},
		},
		{
			name:       "remove the middle of three elements",
			arrayPath:  "spec.routes",
			base:       routesDoc("up-a|100", "up-b|50", "up-c|10"),
			upstream:   routesDoc("up-a|100", "up-c|10"),
			downstream: routesDoc("down-a|100", "down-b|50", "down-c|10"),
			wantPatch:  map[string]api.MutationType{"spec.routes.1": api.MutationTypeDelete},
			want:       []string{"match: down-a priority: 100", "match: down-c priority: 10"},
		},
		{
			name:       "remove the last of three elements",
			arrayPath:  "spec.routes",
			base:       routesDoc("up-a|100", "up-b|50", "up-c|10"),
			upstream:   routesDoc("up-a|100", "up-b|50"),
			downstream: routesDoc("down-a|100", "down-b|50", "down-c|10"),
			wantPatch:  map[string]api.MutationType{"spec.routes.2": api.MutationTypeDelete},
			want:       []string{"match: down-a priority: 100", "match: down-b priority: 50"},
		},
		{
			name:       "remove two of four elements",
			arrayPath:  "spec.routes",
			base:       routesDoc("up-a|100", "up-b|50", "up-c|10", "up-d|5"),
			upstream:   routesDoc("up-b|50", "up-d|5"),
			downstream: routesDoc("down-a|100", "down-b|50", "down-c|10", "down-d|5"),
			wantPatch: map[string]api.MutationType{
				"spec.routes.0": api.MutationTypeDelete,
				"spec.routes.2": api.MutationTypeDelete,
			},
			want: []string{"match: down-b priority: 50", "match: down-d priority: 5"},
		},
		{
			// A field of a surviving element is edited upstream in the same change.
			// The edit is recorded against the element's index before the removal and
			// still has to land on that element after it.
			name:       "remove an element and edit a later one",
			arrayPath:  "spec.routes",
			base:       routesDoc("up-a|100", "up-b|50"),
			upstream:   routesDoc("up-b|55"),
			downstream: routesDoc("down-a|100", "down-b|50"),
			wantPatch: map[string]api.MutationType{
				"spec.routes.0":          api.MutationTypeDelete,
				"spec.routes.1.priority": api.MutationTypeUpdate,
			},
			want: []string{"match: down-b priority: 55"},
		},
		{
			name:       "remove the first of three scalars",
			arrayPath:  "spec.args",
			base:       argsDoc("--a", "--b", "--c"),
			upstream:   argsDoc("--b", "--c"),
			downstream: argsDoc("--a", "--b", "--c"),
			wantPatch:  map[string]api.MutationType{"spec.args.0": api.MutationTypeDelete},
			want:       []string{"--b", "--c"},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) { runMergeArrayCase(t, c) })
	}
}

func TestMergePositionalArrayInsertion(t *testing.T) {
	cases := []mergeArrayCase{
		{
			name:       "insert at the front",
			arrayPath:  "spec.routes",
			base:       routesDoc("up-a|100", "up-b|50"),
			upstream:   routesDoc("up-new|200", "up-a|100", "up-b|50"),
			downstream: routesDoc("down-a|100", "down-b|50"),
			wantPatch:  map[string]api.MutationType{"spec.routes.0": api.MutationTypeAdd},
			want: []string{
				"match: up-new priority: 200",
				"match: down-a priority: 100",
				"match: down-b priority: 50",
			},
		},
		{
			name:       "insert in the middle",
			arrayPath:  "spec.routes",
			base:       routesDoc("up-a|100", "up-b|50"),
			upstream:   routesDoc("up-a|100", "up-new|200", "up-b|50"),
			downstream: routesDoc("down-a|100", "down-b|50"),
			wantPatch:  map[string]api.MutationType{"spec.routes.1": api.MutationTypeAdd},
			want: []string{
				"match: down-a priority: 100",
				"match: up-new priority: 200",
				"match: down-b priority: 50",
			},
		},
		{
			name:       "append at the end",
			arrayPath:  "spec.routes",
			base:       routesDoc("up-a|100", "up-b|50"),
			upstream:   routesDoc("up-a|100", "up-b|50", "up-new|200"),
			downstream: routesDoc("down-a|100", "down-b|50"),
			wantPatch:  map[string]api.MutationType{"spec.routes.2": api.MutationTypeAdd},
			want: []string{
				"match: down-a priority: 100",
				"match: down-b priority: 50",
				"match: up-new priority: 200",
			},
		},
		{
			name:       "insert two elements at different positions",
			arrayPath:  "spec.routes",
			base:       routesDoc("up-a|100", "up-b|50"),
			upstream:   routesDoc("up-x|300", "up-a|100", "up-y|200", "up-b|50"),
			downstream: routesDoc("down-a|100", "down-b|50"),
			wantPatch: map[string]api.MutationType{
				"spec.routes.0": api.MutationTypeAdd,
				"spec.routes.2": api.MutationTypeAdd,
			},
			want: []string{
				"match: up-x priority: 300",
				"match: down-a priority: 100",
				"match: up-y priority: 200",
				"match: down-b priority: 50",
			},
		},
		{
			name:       "remove one element and insert another",
			arrayPath:  "spec.routes",
			base:       routesDoc("up-a|100", "up-b|50"),
			upstream:   routesDoc("up-b|50", "up-new|200"),
			downstream: routesDoc("down-a|100", "down-b|50"),
			wantPatch: map[string]api.MutationType{
				"spec.routes.0": api.MutationTypeDelete,
				"spec.routes.1": api.MutationTypeAdd,
			},
			want: []string{"match: down-b priority: 50", "match: up-new priority: 200"},
		},
		{
			name:       "insert a scalar in the middle",
			arrayPath:  "spec.args",
			base:       argsDoc("--a", "--c"),
			upstream:   argsDoc("--a", "--b", "--c"),
			downstream: argsDoc("--a", "--c"),
			wantPatch:  map[string]api.MutationType{"spec.args.1": api.MutationTypeAdd},
			want:       []string{"--a", "--b", "--c"},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) { runMergeArrayCase(t, c) })
	}
}

// TestMergePositionalArrayInPlaceEdits pins the behavior that did not change: when the
// elements of an array pair up one for one, the diff is still the field-level diff of each
// pair, so a downstream customization of a different field of the same element survives.
func TestMergePositionalArrayInPlaceEdits(t *testing.T) {
	cases := []mergeArrayCase{
		{
			name:       "edit a field of one element",
			arrayPath:  "spec.routes",
			base:       routesDoc("up-a|100", "up-b|50"),
			upstream:   routesDoc("up-a|100", "up-b|55"),
			downstream: routesDoc("down-a|100", "down-b|50"),
			wantPatch:  map[string]api.MutationType{"spec.routes.1.priority": api.MutationTypeUpdate},
			want:       []string{"match: down-a priority: 100", "match: down-b priority: 55"},
		},
		{
			name:       "edit a scalar in place",
			arrayPath:  "spec.args",
			base:       argsDoc("--a", "--b"),
			upstream:   argsDoc("--a", "--b2"),
			downstream: argsDoc("--a", "--b"),
			wantPatch:  map[string]api.MutationType{"spec.args.1": api.MutationTypeUpdate},
			want:       []string{"--a", "--b2"},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) { runMergeArrayCase(t, c) })
	}
}

// TestMergeNestedPositionalArrayRemoval covers removals at two levels at once: reshaping the
// outer array renumbers the elements holding the inner ones, so the inner removal has to be
// applied while the outer indices still hold.
func TestMergeNestedPositionalArrayRemoval(t *testing.T) {
	doc := func(secondRoutePorts string) string {
		return `apiVersion: traefik.io/v1alpha1
kind: IngressRoute
metadata:
  name: ir
  namespace: ns
spec:
  routes:
  - match: first
    ports:
    - 1
    - 2
  - match: second
    ports:
` + secondRoutePorts
	}
	base := doc("    - 3\n    - 4\n    - 5\n")
	// Upstream drops the first route and the middle port of the second.
	upstream := `apiVersion: traefik.io/v1alpha1
kind: IngressRoute
metadata:
  name: ir
  namespace: ns
spec:
  routes:
  - match: second
    ports:
    - 3
    - 5
`
	downstream := doc("    - 3\n    - 4\n    - 5\n")

	patch, err := ComputeMutations(parseDocs(t, base), parseDocs(t, upstream), 1, testProvider)
	require.NoError(t, err)
	assert.Equal(t, map[string]api.MutationType{
		"spec.routes.0":         api.MutationTypeDelete,
		"spec.routes.1.ports.1": api.MutationTypeDelete,
	}, mutationTypes(t, patch))

	merged, _, err := PatchMutations(parseDocs(t, downstream), nil, patch, nil, testProvider, nil)
	require.NoError(t, err)
	assert.Equal(t, []string{"match: second ports: - 3 - 5"}, arrayElements(t, merged, "spec.routes"))
}

// TestMergePositionalArrayLargeFallback checks that an array too large for the alignment
// still merges, using the positional pairing the engine falls back to.
func TestMergePositionalArrayLargeFallback(t *testing.T) {
	elements := make([]string, 0, 60)
	for i := range 60 {
		elements = append(elements, fmt.Sprintf("--arg%d", i))
	}
	base := argsDoc(elements...)
	upstream := argsDoc(append(append([]string{}, elements...), "--extra")...)

	patch, err := ComputeMutations(parseDocs(t, base), parseDocs(t, upstream), 1, testProvider)
	require.NoError(t, err)
	assert.Equal(t, map[string]api.MutationType{"spec.args.60": api.MutationTypeAdd}, mutationTypes(t, patch))

	merged, _, err := PatchMutations(parseDocs(t, base), nil, patch, nil, testProvider, nil)
	require.NoError(t, err)
	assert.Equal(t, append(append([]string{}, elements...), "--extra"), arrayElements(t, merged, "spec.args"))
}
