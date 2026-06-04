// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package upload

import (
	"errors"
	"sort"
)

// errCycleRemains indicates topoSort was given a graph that still has a cycle
// (breakCycles should have been run first).
var errCycleRemains = errors.New("residual dependency cycle after breaking")

// EdgeKind classifies a dependency edge and sets its break priority. When a
// cycle is found the lowest-weight edge on it is removed, so: direct references
// survive over both selector matches and structural (namespace/CRD) edges, and
// same-namespace references survive over references that cross the
// namespace/cluster scope boundary. A selector is the weakest. A structural edge
// (a resource to its Namespace, a custom resource to its CRD) ranks above a
// selector but below any reference — its target is normally a sink, so it only
// lands on a cycle in split-Unit cases (e.g. an AppConfig carrier whose
// namespace lives in another Unit), where keeping the reference is preferable.
type EdgeKind int

const (
	EdgeSelector      EdgeKind = iota // label-selector match (weakest; broken first)
	EdgeStructural                    // namespace / CRD ownership
	EdgeRefCrossScope                 // reference crossing namespace/cluster scope
	EdgeRefSameScope                  // reference within the same namespace (strongest)
)

func (k EdgeKind) weight() int { return int(k) + 1 }

func (k EdgeKind) String() string {
	switch k {
	case EdgeSelector:
		return "selector"
	case EdgeRefCrossScope:
		return "reference (cross-scope)"
	case EdgeRefSameScope:
		return "reference"
	case EdgeStructural:
		return "structural"
	default:
		return "unknown"
	}
}

// Edge is a directed dependency over node indices: From depends on To, so To is
// ordered before From and (for links) the link points From → To.
type Edge struct {
	From   int
	To     int
	Kind   EdgeKind
	Reason string // human-readable, e.g. "reference:v1/ConfigMap"
}

// BrokenEdge is an edge removed to break a cycle, with the cycle it sat on
// (node indices, in traversal order).
type BrokenEdge struct {
	Edge
	Cycle []int
}

// breakCycles removes the minimum-weight edge from each detected cycle until the
// graph over n nodes is acyclic, returning the surviving edges and the removed
// ones. Deterministic: ties break on (weight, From, To).
func breakCycles(n int, edges []Edge) (kept []Edge, broken []BrokenEdge) {
	live := make([]bool, len(edges))
	for i := range live {
		live[i] = true
	}
	for {
		cyc := findCycle(n, edges, live)
		if cyc == nil {
			break
		}
		best := -1
		for _, ei := range cyc {
			if best == -1 || breaksFirst(edges[ei], edges[best]) {
				best = ei
			}
		}
		live[best] = false
		broken = append(broken, BrokenEdge{Edge: edges[best], Cycle: cycleNodes(edges, cyc)})
	}
	for i, e := range edges {
		if live[i] {
			kept = append(kept, e)
		}
	}
	return kept, broken
}

// breaksFirst reports whether edge a should be removed before edge b when both
// lie on a cycle.
func breaksFirst(a, b Edge) bool {
	if a.Kind.weight() != b.Kind.weight() {
		return a.Kind.weight() < b.Kind.weight()
	}
	if a.From != b.From {
		return a.From < b.From
	}
	return a.To < b.To
}

// findCycle returns the live edge indices forming one cycle, or nil if the graph
// is acyclic. Standard DFS with a gray/black coloring and an edge stack.
func findCycle(n int, edges []Edge, live []bool) []int {
	adj := make([][]int, n) // node -> live outgoing edge indices
	for i, e := range edges {
		if live[i] {
			adj[e.From] = append(adj[e.From], i)
		}
	}
	const (
		white = 0
		gray  = 1
		black = 2
	)
	color := make([]int, n)
	var stack []int // edge indices on the current DFS path
	var result []int

	var dfs func(u int) bool
	dfs = func(u int) bool {
		color[u] = gray
		for _, ei := range adj[u] {
			v := edges[ei].To
			switch color[v] {
			case gray:
				// Back edge u→v closes a cycle. Collect the path edges from the
				// edge that first left v, up through ei.
				cyc := []int{}
				for i := len(stack) - 1; i >= 0; i-- {
					cyc = append(cyc, stack[i])
					if edges[stack[i]].From == v {
						break
					}
				}
				// Reverse so the cycle reads in forward (From→To) order, then append ei.
				for l, r := 0, len(cyc)-1; l < r; l, r = l+1, r-1 {
					cyc[l], cyc[r] = cyc[r], cyc[l]
				}
				result = append(cyc, ei)
				return true
			case white:
				stack = append(stack, ei)
				if dfs(v) {
					return true
				}
				stack = stack[:len(stack)-1]
			}
		}
		color[u] = black
		return false
	}

	for u := 0; u < n; u++ {
		if color[u] == white && dfs(u) {
			return result
		}
	}
	return nil
}

// cycleNodes converts a list of cycle edge indices into the node sequence.
func cycleNodes(edges []Edge, cyc []int) []int {
	nodes := make([]int, 0, len(cyc))
	for _, ei := range cyc {
		nodes = append(nodes, edges[ei].From)
	}
	return nodes
}

// topoSort orders the n nodes so every dependency (To) precedes its dependent
// (From). Among nodes that are simultaneously ready it prefers the lower
// priority[i] (install-order band), then the lexically smaller name[i], then the
// lower index — giving a stable, install-sensible order. The edge set must be
// acyclic (run breakCycles first); a residual cycle yields an error.
func topoSort(n int, priority []int, name []string, edges []Edge) ([]int, error) {
	indeg := make([]int, n) // number of prerequisites (outgoing edges) still unmet
	dependents := make([][]int, n)
	for _, e := range edges {
		indeg[e.From]++
		dependents[e.To] = append(dependents[e.To], e.From)
	}

	ready := make([]int, 0, n)
	for u := 0; u < n; u++ {
		if indeg[u] == 0 {
			ready = append(ready, u)
		}
	}

	less := func(a, b int) bool {
		if priority[a] != priority[b] {
			return priority[a] < priority[b]
		}
		if name[a] != name[b] {
			return name[a] < name[b]
		}
		return a < b
	}

	out := make([]int, 0, n)
	for len(ready) > 0 {
		sort.Slice(ready, func(i, j int) bool { return less(ready[i], ready[j]) })
		u := ready[0]
		ready = ready[1:]
		out = append(out, u)
		for _, w := range dependents[u] {
			indeg[w]--
			if indeg[w] == 0 {
				ready = append(ready, w)
			}
		}
	}
	if len(out) != n {
		return nil, errCycleRemains
	}
	return out, nil
}
