// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package upload

import (
	"fmt"
	"sort"
	"strings"

	"github.com/confighub/sdk/configkit/k8skit"
	"github.com/confighub/sdk/core/configkit/cubkit"
	fapi "github.com/confighub/sdk/core/function/api"
)

// Granularity selects how resources map to Units.
type Granularity string

const (
	// PerResource creates one Unit per resource.
	PerResource Granularity = "per-resource"
	// Minimal creates the fewest Units possible: one for everything, with CRDs
	// and each AppConfig file split into their own Units.
	Minimal Granularity = "minimal"
)

const toolchainKubernetesYAML = "Kubernetes/YAML"

// UnitKind tells the command how to materialize a Unit.
type UnitKind int

const (
	// UnitNormal is a plain Kubernetes/YAML Unit.
	UnitNormal UnitKind = iota
	// UnitCRD is the CRDs Unit (also Kubernetes/YAML; split out for lifecycle).
	UnitCRD
	// UnitAppConfig is an AppConfig data Unit: the command also creates the
	// render-configmap Invocation, the placeholder Unit, and the Upsert link.
	UnitAppConfig
)

// Unit is one Unit to create.
type Unit struct {
	Slug      string
	Toolchain string
	Content   string // YAML body; empty for an AppConfig placeholder (filled by its link)
	Kind      UnitKind
	AppConfig *AppConfigManifest // set when Kind == UnitAppConfig
}

// LinkEdge is an inferred Unit→Unit link.
type LinkEdge struct {
	FromUnit string
	ToUnit   string
	Reason   string
}

// UnmatchedReference is a reference that resolved to no resource in the bundle —
// typically a resource that lives in the cluster but isn't part of this upload.
type UnmatchedReference struct {
	FromUnit   string
	TargetType string
	TargetName string
}

// BrokenEdgeReport describes one dependency edge removed to break a cycle.
type BrokenEdgeReport struct {
	From   string
	To     string
	Kind   string
	Reason string
	Cycle  []string
}

// Plan is the full result of analyzing a rendered bundle: the Units to create,
// the links between them, the edges broken to keep ordering and links acyclic,
// the Secrets that were skipped, and references that didn't resolve.
type Plan struct {
	Units          []Unit
	Links          []LinkEdge
	BrokenOrdering []BrokenEdgeReport
	BrokenLinks    []BrokenEdgeReport
	SkippedSecrets []Resource
	Unmatched      []UnmatchedReference
}

// internal unmatched reference carrying the source resource index.
type unmatchedRef struct {
	fromIdx    int
	targetType string
	targetName string
}

// BuildPlan analyzes the parsed resources and produces an upload Plan. baseSlug
// is the slug stem for the minimal-mode Units; namespace, when non-empty and not
// "default", causes a Namespace resource to be synthesized if absent.
func BuildPlan(resources []Resource, gran Granularity, baseSlug, namespace string) (*Plan, error) {
	resources = ensureNamespace(resources, namespace)

	plan := &Plan{}
	kept := resources[:0:0]
	for _, r := range resources {
		if r.IsSecret {
			plan.SkippedSecrets = append(plan.SkippedSecrets, r)
			continue
		}
		kept = append(kept, r)
	}
	resources = kept

	// Reference + workload-label graph over the whole bundle (in-process funcs).
	var bundle strings.Builder
	for _, r := range resources {
		bundle.WriteString("---\n")
		bundle.WriteString(r.Content)
	}
	refs, err := extractReferences([]byte(bundle.String()))
	if err != nil {
		return nil, err
	}
	wls, err := extractWorkloadLabels([]byte(bundle.String()))
	if err != nil {
		return nil, err
	}

	edges, unmatched := buildResourceEdges(resources, refs, wls)

	// Group resources into Units and record each resource's link-target Unit slug.
	groups, linkUnitSlug := groupUnits(resources, gran, baseSlug)

	// Ordering pass: break cycles over the resource graph, topo-sort, then order
	// each multi-resource Unit's documents by that global order.
	orderEdges, brokenOrder := breakCycles(len(resources), edges)
	priorities := make([]int, len(resources))
	sortNames := make([]string, len(resources))
	displayNames := make([]string, len(resources))
	for i, r := range resources {
		priorities[i] = k8skit.ResourcePriority(r.Type)
		sortNames[i] = string(r.key()) // unique, for deterministic tie-breaking
		displayNames[i] = kindOf(r.Type) + "/" + r.Name
	}
	order, err := topoSort(len(resources), priorities, sortNames, orderEdges)
	if err != nil {
		return nil, err
	}
	orderPos := make([]int, len(resources))
	for pos, idx := range order {
		orderPos[idx] = pos
	}
	// Within-Unit document order only matters when two resources share a Unit, so
	// report a broken ordering edge only then. In per-resource mode no two
	// resources share a Unit, so these are dropped (the same cycle surfaces as a
	// broken *link* below, which is the meaningful report).
	var withinUnitBroken []BrokenEdge
	for _, b := range brokenOrder {
		if linkUnitSlug[b.From] != "" && linkUnitSlug[b.From] == linkUnitSlug[b.To] {
			withinUnitBroken = append(withinUnitBroken, b)
		}
	}
	plan.BrokenOrdering = reportBrokenEdges(withinUnitBroken, displayNames)

	// Materialize Units, ordering each group's documents.
	for _, g := range groups {
		if g.kind == UnitAppConfig {
			plan.Units = append(plan.Units, Unit{
				Slug:      g.slug,
				Toolchain: g.appCfg.Toolchain,
				Content:   string(g.appCfg.Content),
				Kind:      UnitAppConfig,
				AppConfig: g.appCfg,
			})
			continue
		}
		idxs := append([]int(nil), g.resIdxs...)
		sort.Slice(idxs, func(i, j int) bool { return orderPos[idxs[i]] < orderPos[idxs[j]] })
		var b strings.Builder
		for _, idx := range idxs {
			b.WriteString("---\n")
			b.WriteString(resources[idx].Source)
			b.WriteString(resources[idx].Content)
		}
		plan.Units = append(plan.Units, Unit{
			Slug:      g.slug,
			Toolchain: toolchainKubernetesYAML,
			Content:   b.String(),
			Kind:      g.kind,
		})
	}

	// Link pass: collapse resource edges to Unit edges, break cycles, emit links.
	plan.Links, plan.BrokenLinks = inferLinks(edges, linkUnitSlug)

	// Resolve unmatched references to their source Unit slug and filter built-ins.
	for _, u := range unmatched {
		if u.targetType == "rbac.authorization.k8s.io/v1/ClusterRole" && isBuiltInClusterRole(u.targetName) {
			continue
		}
		plan.Unmatched = append(plan.Unmatched, UnmatchedReference{
			FromUnit:   linkUnitSlug[u.fromIdx],
			TargetType: u.targetType,
			TargetName: u.targetName,
		})
	}
	return plan, nil
}

// ensureNamespace prepends a Namespace resource when one is requested and not
// already present (and not the implicit "default").
func ensureNamespace(resources []Resource, namespace string) []Resource {
	if namespace == "" || namespace == "default" {
		return resources
	}
	for _, r := range resources {
		if r.Type == namespaceType && r.Name == namespace {
			return resources
		}
	}
	ns := Resource{
		Type:        namespaceType,
		ScopedName:  fapi.ResourceName("/" + namespace),
		Name:        namespace,
		Content:     fmt.Sprintf("apiVersion: v1\nkind: Namespace\nmetadata:\n  name: %s\n", namespace),
		Source:      "# Source: cub variant upload (ensured Namespace)\n",
		Synthesized: true,
	}
	return append([]Resource{ns}, resources...)
}

type unitGroup struct {
	slug    string
	kind    UnitKind
	resIdxs []int
	appCfg  *AppConfigManifest
}

// groupUnits assigns resources to Units and returns the groups plus, for every
// resource, the slug of the Unit that represents it as a link target (an
// AppConfig carrier maps to its placeholder Unit, which holds the rendered
// ConfigMap).
func groupUnits(resources []Resource, gran Granularity, baseSlug string) ([]unitGroup, []string) {
	linkUnitSlug := make([]string, len(resources))
	used := map[string]bool{}
	uniqueSlug := func(s string) string {
		s = cubkit.CubNormalizeName(s)
		if s == "" {
			s = "resource"
		}
		cand := s
		for n := 2; used[cand]; n++ {
			cand = fmt.Sprintf("%s-%d", s, n)
		}
		used[cand] = true
		return cand
	}

	var groups []unitGroup

	// AppConfig carriers always become their own Unit set, in both modes.
	for i, r := range resources {
		if r.AppConfig == nil {
			continue
		}
		dataSlug := uniqueSlug(r.AppConfig.UnitSlug())
		// Reserve the placeholder slug so nothing else collides with it.
		placeholder := uniqueSlug(r.AppConfig.PlaceholderSlug())
		linkUnitSlug[i] = placeholder
		groups = append(groups, unitGroup{slug: dataSlug, kind: UnitAppConfig, appCfg: r.AppConfig})
	}

	switch gran {
	case PerResource:
		for i, r := range resources {
			if r.AppConfig != nil {
				continue
			}
			slug := uniqueSlug(resourceSlug(r))
			linkUnitSlug[i] = slug
			kind := UnitNormal
			if r.IsCRD {
				kind = UnitCRD
			}
			groups = append(groups, unitGroup{slug: slug, kind: kind, resIdxs: []int{i}})
		}
	default: // Minimal
		// Split infrastructure with its own lifecycle into separate Units:
		// Namespaces, CRDs, and plain (rendered) ConfigMaps each get their own
		// Unit, leaving everything else in the main Unit. Splitting the
		// Namespace in particular avoids a spurious Unit-level cycle when a
		// split-out ConfigMap (which depends on the Namespace) is referenced by
		// a workload in the main Unit.
		var nsIdxs, crdIdxs, cmIdxs, mainIdxs []int
		for i, r := range resources {
			if r.AppConfig != nil {
				continue
			}
			switch {
			case r.Type == namespaceType:
				nsIdxs = append(nsIdxs, i)
			case r.IsCRD:
				crdIdxs = append(crdIdxs, i)
			case r.Type == configMapType:
				cmIdxs = append(cmIdxs, i)
			default:
				mainIdxs = append(mainIdxs, i)
			}
		}
		addBucket := func(idxs []int, slugStem string, kind UnitKind) {
			if len(idxs) == 0 {
				return
			}
			slug := uniqueSlug(slugStem)
			for _, i := range idxs {
				linkUnitSlug[i] = slug
			}
			groups = append(groups, unitGroup{slug: slug, kind: kind, resIdxs: idxs})
		}
		addBucket(nsIdxs, baseSlug+"-namespaces", UnitNormal)
		addBucket(crdIdxs, baseSlug+"-crds", UnitCRD)
		addBucket(cmIdxs, baseSlug+"-configmaps", UnitNormal)
		addBucket(mainIdxs, baseSlug, UnitNormal)
	}
	return groups, linkUnitSlug
}

// placeholderValue is ConfigHub's sentinel value (used for a namespace, a name,
// etc.): a resource carries it until a link or set-namespace substitutes the real
// value. It is never a meaningful disambiguator, so it is omitted from generated
// slugs — otherwise a Unit's slug would freeze the placeholder even after the
// resource it names is renamed in a variant.
const placeholderValue = "confighubplaceholder"

// resourceSlug derives a per-resource Unit slug from kind, namespace, and name.
// Placeholder namespace/name values are omitted (so a synthesized placeholder
// Namespace becomes "namespace", not "namespace-confighubplaceholder"), and the
// namespace is included only when it is a real, disambiguating value.
func resourceSlug(r Resource) string {
	kind := kindOf(r.Type)
	parts := []string{}
	if r.Namespace != "" && r.Namespace != placeholderValue {
		parts = append(parts, r.Namespace)
	}
	parts = append(parts, kind)
	if r.Name != "" && r.Name != placeholderValue {
		parts = append(parts, r.Name)
	}
	return strings.Join(parts, "-")
}

func kindOf(rt fapi.ResourceType) string {
	s := string(rt)
	if i := strings.LastIndex(s, "/"); i >= 0 {
		s = s[i+1:]
	}
	return strings.ToLower(s)
}

// buildResourceEdges builds the resource-level dependency graph from references,
// namespace ownership, CRD ownership, and label selectors. Returns the strongest
// edge per (from,to) pair plus references that resolved to nothing.
func buildResourceEdges(resources []Resource, refs []reference, wls map[fapi.ResourceTypeAndName]*workloadLabels) ([]Edge, []unmatchedRef) {
	byTypeScoped := map[fapi.ResourceTypeAndName]int{}
	byTypeName := map[string][]int{}
	nsByName := map[string]int{}
	crdByGK := map[string]int{}
	for i, r := range resources {
		byTypeScoped[r.key()] = i
		byTypeName[string(r.Type)+"\x00"+r.Name] = append(byTypeName[string(r.Type)+"\x00"+r.Name], i)
		if r.Type == namespaceType {
			nsByName[r.Name] = i
		}
		if gk, ok := r.CRDGroupKind(); ok {
			crdByGK[gk] = i
		}
	}

	em := map[[2]int]Edge{}
	put := func(from, to int, kind EdgeKind, reason string) {
		if from == to || from < 0 || to < 0 {
			return
		}
		k := [2]int{from, to}
		if prev, ok := em[k]; ok && prev.Kind.weight() >= kind.weight() {
			return
		}
		em[k] = Edge{From: from, To: to, Kind: kind, Reason: reason}
	}

	var unmatched []unmatchedRef
	unmatchedSeen := map[string]bool{}
	for _, ref := range refs {
		from, ok := byTypeScoped[ref.From]
		if !ok {
			continue
		}
		matches := byTypeName[string(ref.TargetType)+"\x00"+string(ref.TargetName)]
		if len(matches) == 0 {
			key := fmt.Sprintf("%d\x00%s\x00%s", from, ref.TargetType, ref.TargetName)
			if !unmatchedSeen[key] {
				unmatchedSeen[key] = true
				unmatched = append(unmatched, unmatchedRef{fromIdx: from, targetType: string(ref.TargetType), targetName: string(ref.TargetName)})
			}
			continue
		}
		for _, to := range matches {
			kind := EdgeRefSameScope
			if resources[from].Namespace != resources[to].Namespace {
				kind = EdgeRefCrossScope
			}
			put(from, to, kind, "reference:"+string(ref.TargetType))
		}
	}

	for i, r := range resources {
		if r.Namespace != "" && r.Type != namespaceType {
			if nsIdx, ok := nsByName[r.Namespace]; ok {
				put(i, nsIdx, EdgeStructural, "namespace")
			}
		}
		if !r.IsCRD {
			if gk, ok := groupKindFromResourceType(r.Type); ok {
				if crdIdx, ok := crdByGK[gk]; ok {
					put(i, crdIdx, EdgeStructural, "crd")
				}
			}
		}
	}

	for i, r := range resources {
		wl := wls[r.key()]
		if wl == nil || len(wl.selectors) == 0 {
			continue
		}
		for j, r2 := range resources {
			if j == i {
				continue
			}
			wl2 := wls[r2.key()]
			if wl2 == nil || len(wl2.templates) == 0 {
				continue
			}
			if anySelectorMatches(wl.selectors, wl2.templates) {
				put(i, j, EdgeSelector, "selector")
			}
		}
	}

	edges := make([]Edge, 0, len(em))
	for _, e := range em {
		edges = append(edges, e)
	}
	sort.Slice(edges, func(i, j int) bool {
		if edges[i].From != edges[j].From {
			return edges[i].From < edges[j].From
		}
		return edges[i].To < edges[j].To
	})
	return edges, unmatched
}

// inferLinks collapses resource edges to Unit edges (dropping intra-Unit ones),
// breaks any cycles in the Unit graph, and returns the surviving links plus a
// report of the broken ones.
func inferLinks(edges []Edge, linkUnitSlug []string) ([]LinkEdge, []BrokenEdgeReport) {
	// Assign a node index to each distinct link Unit slug.
	nodeOf := map[string]int{}
	var slugs []string
	idxOf := func(slug string) int {
		if n, ok := nodeOf[slug]; ok {
			return n
		}
		n := len(slugs)
		nodeOf[slug] = n
		slugs = append(slugs, slug)
		return n
	}
	for _, s := range linkUnitSlug {
		if s != "" {
			idxOf(s)
		}
	}

	em := map[[2]int]Edge{}
	for _, e := range edges {
		fs, ts := linkUnitSlug[e.From], linkUnitSlug[e.To]
		if fs == "" || ts == "" || fs == ts {
			continue
		}
		from, to := idxOf(fs), idxOf(ts)
		k := [2]int{from, to}
		if prev, ok := em[k]; ok && prev.Kind.weight() >= e.Kind.weight() {
			continue
		}
		em[k] = Edge{From: from, To: to, Kind: e.Kind, Reason: e.Reason}
	}
	unitEdges := make([]Edge, 0, len(em))
	for _, e := range em {
		unitEdges = append(unitEdges, e)
	}
	sort.Slice(unitEdges, func(i, j int) bool {
		if unitEdges[i].From != unitEdges[j].From {
			return unitEdges[i].From < unitEdges[j].From
		}
		return unitEdges[i].To < unitEdges[j].To
	})

	kept, broken := breakCycles(len(slugs), unitEdges)
	links := make([]LinkEdge, 0, len(kept))
	for _, e := range kept {
		links = append(links, LinkEdge{FromUnit: slugs[e.From], ToUnit: slugs[e.To], Reason: e.Reason})
	}
	sort.Slice(links, func(i, j int) bool {
		if links[i].FromUnit != links[j].FromUnit {
			return links[i].FromUnit < links[j].FromUnit
		}
		return links[i].ToUnit < links[j].ToUnit
	})
	return links, reportBrokenEdges(broken, slugs)
}

func reportBrokenEdges(broken []BrokenEdge, names []string) []BrokenEdgeReport {
	if len(broken) == 0 {
		return nil
	}
	out := make([]BrokenEdgeReport, 0, len(broken))
	for _, b := range broken {
		cyc := make([]string, 0, len(b.Cycle))
		for _, n := range b.Cycle {
			cyc = append(cyc, names[n])
		}
		out = append(out, BrokenEdgeReport{
			From:   names[b.From],
			To:     names[b.To],
			Kind:   b.Kind.String(),
			Reason: b.Reason,
			Cycle:  cyc,
		})
	}
	return out
}

// groupKindFromResourceType extracts "group/kind" from an "<group>/<version>/<kind>"
// ResourceType (core "v1/Kind" has no dotted group and returns false).
func groupKindFromResourceType(rt fapi.ResourceType) (string, bool) {
	parts := strings.Split(string(rt), "/")
	if len(parts) != 3 {
		return "", false
	}
	if !strings.Contains(parts[0], ".") {
		return "", false
	}
	return parts[0] + "/" + parts[2], true
}

// isBuiltInClusterRole reports whether name is a Kubernetes built-in ClusterRole
// that pre-exists in every cluster, so an unresolved reference to it is expected.
func isBuiltInClusterRole(name string) bool {
	switch name {
	case "cluster-admin", "admin", "edit", "view":
		return true
	}
	return strings.HasPrefix(name, "system:")
}
