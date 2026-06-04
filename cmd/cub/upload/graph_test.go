// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package upload

import "testing"

// hasEdge reports whether the edge (from,to) is present.
func hasEdge(edges []Edge, from, to int) bool {
	for _, e := range edges {
		if e.From == from && e.To == to {
			return true
		}
	}
	return false
}

// TestBreakCycles_DropsWeakestEdge verifies the break priority: a selector edge
// is removed before a reference edge that closes the same cycle.
func TestBreakCycles_SelectorBeforeReference(t *testing.T) {
	// 0 --selector--> 1, 1 --reference--> 0
	edges := []Edge{
		{From: 0, To: 1, Kind: EdgeSelector, Reason: "selector"},
		{From: 1, To: 0, Kind: EdgeRefSameScope, Reason: "reference"},
	}
	kept, broken := breakCycles(2, edges)
	if len(broken) != 1 {
		t.Fatalf("expected 1 broken edge, got %d", len(broken))
	}
	if broken[0].Kind != EdgeSelector {
		t.Fatalf("expected the selector edge to break, got %s", broken[0].Kind)
	}
	if !hasEdge(kept, 1, 0) || hasEdge(kept, 0, 1) {
		t.Fatalf("expected only the reference edge to survive, kept=%v", kept)
	}
}

// TestBreakCycles_CrossScopeBeforeSameScope verifies a cross-scope reference is
// removed before a same-namespace reference on the same cycle.
func TestBreakCycles_CrossScopeBeforeSameScope(t *testing.T) {
	edges := []Edge{
		{From: 0, To: 1, Kind: EdgeRefSameScope, Reason: "reference"},
		{From: 1, To: 0, Kind: EdgeRefCrossScope, Reason: "reference (cross-scope)"},
	}
	kept, broken := breakCycles(2, edges)
	if len(broken) != 1 || broken[0].Kind != EdgeRefCrossScope {
		t.Fatalf("expected the cross-scope reference to break, broken=%v", broken)
	}
	if !hasEdge(kept, 0, 1) {
		t.Fatalf("expected the same-scope reference to survive, kept=%v", kept)
	}
}

// TestBreakCycles_DeterministicTie verifies that among equal-weight edges the
// lowest (From,To) is chosen, deterministically.
func TestBreakCycles_DeterministicTie(t *testing.T) {
	// 0->1->2->0, all same-scope references (equal weight).
	edges := []Edge{
		{From: 0, To: 1, Kind: EdgeRefSameScope},
		{From: 1, To: 2, Kind: EdgeRefSameScope},
		{From: 2, To: 0, Kind: EdgeRefSameScope},
	}
	_, broken := breakCycles(3, edges)
	if len(broken) != 1 {
		t.Fatalf("expected 1 broken edge, got %d", len(broken))
	}
	if broken[0].From != 0 || broken[0].To != 1 {
		t.Fatalf("expected edge (0,1) to break by tie-break, got (%d,%d)", broken[0].From, broken[0].To)
	}
}

// TestBreakCycles_Acyclic leaves an acyclic graph untouched.
func TestBreakCycles_Acyclic(t *testing.T) {
	edges := []Edge{
		{From: 2, To: 1, Kind: EdgeRefSameScope},
		{From: 1, To: 0, Kind: EdgeStructural},
	}
	kept, broken := breakCycles(3, edges)
	if len(broken) != 0 || len(kept) != 2 {
		t.Fatalf("expected no breaks, got broken=%d kept=%d", len(broken), len(kept))
	}
}

// TestTopoSort_DependenciesFirstThenPriority verifies dependencies precede their
// dependents and that ready nodes are ordered by priority then name.
func TestTopoSort_DependenciesFirstThenPriority(t *testing.T) {
	// 3 nodes: a Deployment(0) depends on a ConfigMap(1); a Namespace(2) has no
	// deps. Priorities: ns=20, cm=260, deploy=500. Expected order: ns, cm, deploy.
	priority := []int{500, 260, 20}
	name := []string{"deploy", "cm", "ns"}
	edges := []Edge{
		{From: 0, To: 1, Kind: EdgeRefSameScope}, // deploy depends on cm
	}
	order, err := topoSort(3, priority, name, edges)
	if err != nil {
		t.Fatal(err)
	}
	want := []int{2, 1, 0} // ns, cm, deploy
	for i := range want {
		if order[i] != want[i] {
			t.Fatalf("order = %v, want %v", order, want)
		}
	}
}

// TestTopoSort_DetectsResidualCycle errors when the graph still has a cycle.
func TestTopoSort_DetectsResidualCycle(t *testing.T) {
	edges := []Edge{
		{From: 0, To: 1},
		{From: 1, To: 0},
	}
	if _, err := topoSort(2, []int{0, 0}, []string{"a", "b"}, edges); err == nil {
		t.Fatal("expected a residual-cycle error")
	}
}
