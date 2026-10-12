// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"fmt"
	"sort"
	"strings"

	"github.com/confighub/sdk/core/cubapi"
	goclientnew "github.com/confighub/sdk/core/openapi/goclient-new"
	"github.com/spf13/cobra"
)

var componentTreeCmd = &cobra.Command{
	Use:   "tree [<component>]",
	Short: "Show a component's variants as a tree",
	Long:  getComponentTreeHelp(),
	Args:  cobra.MaximumNArgs(1),
	RunE:  componentTreeCmdRun,
}

func getComponentTreeHelp() string {
	baseHelp := `Show the variants of a Component as a tree, each under the variant it was cloned from,
with the release target of each deployment. Without a Component, every Component's tree is
shown.

A variant's place in the tree comes from its units: a cloned unit records the unit it came
from, so a variant whose units were cloned from another variant of the Component hangs
under it. A variant with no such upstream, the base, is a root. A variant whose units
have no common upstream, or an upstream outside the Component, is a root too.

Examples:
` + "```" + `
  # One component
  cub component tree cubbychat

  cubbychat
  └── base
      ├── dev     → dev/target
      └── prod    → prod/target

  # Every component
  cub component tree
` + "```" + `
`
	agentContext := `The tree is the promotion graph: a change made on a variant flows to the variants under it
with 'cub variant promote'. The arrow names the Target a deployment's releases go to, as
<space>/<target>; a variant without one is not deployed. Variants are named as --variant
<component>/<variant> takes them.`
	return getCommandHelp(baseHelp, agentContext)
}

func init() {
	componentCmd.AddCommand(componentTreeCmd)
}

// variantNode is one variant in a component's tree.
type variantNode struct {
	SpaceID  goclientnew.UUID
	Name     string // the Variant label, or the slug for a space without one
	Target   string // the release target as <space>/<target>, or "" for a base
	Children []*variantNode
}

// componentTree is one component's variants, as a forest: usually one root, the base.
type componentTree struct {
	Slug  string
	Roots []*variantNode
}

func componentTreeCmdRun(_ *cobra.Command, args []string) error {
	var components []*goclientnew.ExtendedComponent
	if len(args) == 1 {
		component, err := resolveComponent(args[0], "ComponentID,Slug")
		if err != nil {
			return err
		}
		components = []*goclientnew.ExtendedComponent{component}
	} else {
		var err error
		components, err = cubapi.ListComponents(ctx, cubClient, cubapi.Where{}, cubapi.ListOpts{Select: "ComponentID,Slug,OrganizationID"})
		if err != nil {
			return err
		}
		sort.Slice(components, func(i, j int) bool { return components[i].Component.Slug < components[j].Component.Slug })
	}
	trees, err := buildComponentTrees(components)
	if err != nil {
		return err
	}
	for i, tree := range trees {
		if i > 0 {
			tprintRaw("")
		}
		for _, line := range renderComponentTree(tree) {
			tprintRaw(line)
		}
	}
	return nil
}

// buildComponentTrees reads the spaces and units of the components once and arranges each
// component's variants under their upstreams.
func buildComponentTrees(components []*goclientnew.ExtendedComponent) ([]componentTree, error) {
	// One component is read by its id; every component by the ComponentID being set.
	spaceWhere := cubapi.NewWhere("ComponentID IS NOT NULL")
	unitWhere := cubapi.NewWhere("Space.ComponentID IS NOT NULL")
	if len(components) == 1 {
		id := components[0].Component.ComponentID.String()
		spaceWhere = cubapi.NewWhere(fmt.Sprintf("ComponentID = '%s'", id))
		unitWhere = cubapi.NewWhere(fmt.Sprintf("Space.ComponentID = '%s'", id))
	}
	spaces, err := cubapi.ListSpaces(ctx, cubClient, spaceWhere, cubapi.ListOpts{
		Select: "SpaceID,Slug,ComponentID,Labels,ReleaseTargetID,OrganizationID",
	})
	if err != nil {
		return nil, err
	}
	units, err := cubapi.ListUnits(ctx, cubClient, unitWhere.And("UpstreamSpaceID IS NOT NULL"), cubapi.ListOpts{
		Select: "UnitID,SpaceID,UpstreamSpaceID",
	})
	if err != nil {
		return nil, err
	}
	targets, err := targetNames()
	if err != nil {
		return nil, err
	}

	// How many of each space's units came from each other space. The upstream most of a
	// variant's units share is the variant it was cloned from.
	upstreams := map[goclientnew.UUID]map[goclientnew.UUID]int{}
	for _, unit := range units {
		if unit == nil || unit.Unit == nil || unit.Unit.UpstreamSpaceID == nil {
			continue
		}
		if upstreams[unit.Unit.SpaceID] == nil {
			upstreams[unit.Unit.SpaceID] = map[goclientnew.UUID]int{}
		}
		upstreams[unit.Unit.SpaceID][*unit.Unit.UpstreamSpaceID]++
	}

	byComponent := map[goclientnew.UUID][]*goclientnew.ExtendedSpace{}
	for _, space := range spaces {
		if space == nil || space.Space == nil || space.Space.ComponentID == nil {
			continue
		}
		byComponent[*space.Space.ComponentID] = append(byComponent[*space.Space.ComponentID], space)
	}

	trees := make([]componentTree, 0, len(components))
	for _, component := range components {
		trees = append(trees, componentTree{
			Slug:  component.Component.Slug,
			Roots: arrangeVariants(byComponent[component.Component.ComponentID], upstreams, targets),
		})
	}
	return trees, nil
}

// targetNames maps every Target the caller can see to <space>/<slug>.
func targetNames() (map[goclientnew.UUID]string, error) {
	targets, err := cubapi.ListTargets(ctx, cubClient, cubapi.Where{}, cubapi.ListOpts{
		Select:  "TargetID,Slug,SpaceID,OrganizationID",
		Include: "SpaceID",
	})
	if err != nil {
		return nil, err
	}
	names := map[goclientnew.UUID]string{}
	for _, target := range targets {
		if target == nil || target.Target == nil {
			continue
		}
		name := target.Target.Slug
		if target.Space != nil && target.Space.Slug != "" {
			name = target.Space.Slug + "/" + name
		}
		names[target.Target.TargetID] = name
	}
	return names, nil
}

// arrangeVariants hangs each of a component's spaces under the space most of its units
// were cloned from, when that is another space of the same component, and returns the
// roots. A cycle, which the upstream links do not forbid, is broken at its smallest name so
// that every variant is shown once.
func arrangeVariants(spaces []*goclientnew.ExtendedSpace, upstreams map[goclientnew.UUID]map[goclientnew.UUID]int, targets map[goclientnew.UUID]string) []*variantNode {
	nodes := map[goclientnew.UUID]*variantNode{}
	for _, space := range spaces {
		node := &variantNode{SpaceID: space.Space.SpaceID, Name: space.Space.Labels["Variant"]}
		if node.Name == "" {
			node.Name = space.Space.Slug
		}
		if space.Space.ReleaseTargetID != nil {
			node.Target = targets[*space.Space.ReleaseTargetID]
			if node.Target == "" {
				node.Target = space.Space.ReleaseTargetID.String()
			}
		}
		nodes[space.Space.SpaceID] = node
	}
	parent := map[goclientnew.UUID]goclientnew.UUID{}
	for spaceID := range nodes {
		if upstream, ok := mostCommonUpstream(upstreams[spaceID]); ok && upstream != spaceID {
			if _, inComponent := nodes[upstream]; inComponent {
				parent[spaceID] = upstream
			}
		}
	}

	byName := func(a, b *variantNode) bool {
		return a.Name < b.Name || (a.Name == b.Name && a.SpaceID.String() < b.SpaceID.String())
	}
	var roots []*variantNode
	for id, node := range nodes {
		if parentID, ok := parent[id]; ok {
			nodes[parentID].Children = append(nodes[parentID].Children, node)
		} else {
			roots = append(roots, node)
		}
	}
	for _, node := range nodes {
		sort.Slice(node.Children, func(i, j int) bool { return byName(node.Children[i], node.Children[j]) })
	}
	sort.Slice(roots, func(i, j int) bool { return byName(roots[i], roots[j]) })

	// A node a root does not reach is in a cycle. Detach the smallest-named one from its
	// parent and make it a root, then look again, until everything is reached.
	for {
		reached := map[goclientnew.UUID]bool{}
		var walk func(*variantNode)
		walk = func(node *variantNode) {
			if reached[node.SpaceID] {
				return
			}
			reached[node.SpaceID] = true
			for _, child := range node.Children {
				walk(child)
			}
		}
		for _, root := range roots {
			walk(root)
		}
		var stranded *variantNode
		for _, node := range nodes {
			if !reached[node.SpaceID] && (stranded == nil || byName(node, stranded)) {
				stranded = node
			}
		}
		if stranded == nil {
			return roots
		}
		parentNode := nodes[parent[stranded.SpaceID]]
		kept := parentNode.Children[:0]
		for _, child := range parentNode.Children {
			if child != stranded {
				kept = append(kept, child)
			}
		}
		parentNode.Children = kept
		roots = append(roots, stranded)
		sort.Slice(roots, func(i, j int) bool { return byName(roots[i], roots[j]) })
	}
}

// mostCommonUpstream is the upstream space most of a variant's units share, with ties
// broken by id so that the answer is the same on every run.
func mostCommonUpstream(counts map[goclientnew.UUID]int) (goclientnew.UUID, bool) {
	var best goclientnew.UUID
	bestCount := 0
	for upstream, count := range counts {
		if count > bestCount || (count == bestCount && upstream.String() < best.String()) {
			best, bestCount = upstream, count
		}
	}
	return best, bestCount > 0
}

// renderComponentTree draws one component: its slug, then its variants with tree
// connectors, and an arrow to the release target of each deployment, aligned in a column.
func renderComponentTree(tree componentTree) []string {
	type line struct{ tree, target string }
	lines := []line{{tree: tree.Slug}}
	var draw func(node *variantNode, prefix string, last bool)
	draw = func(node *variantNode, prefix string, last bool) {
		connector, childPrefix := "├── ", prefix+"│   "
		if last {
			connector, childPrefix = "└── ", prefix+"    "
		}
		lines = append(lines, line{tree: prefix + connector + node.Name, target: node.Target})
		for i, child := range node.Children {
			draw(child, childPrefix, i == len(node.Children)-1)
		}
	}
	for i, root := range tree.Roots {
		draw(root, "", i == len(tree.Roots)-1)
	}
	width := 0
	for _, l := range lines {
		if l.target != "" {
			width = max(width, len([]rune(l.tree)))
		}
	}
	out := make([]string, 0, len(lines))
	for _, l := range lines {
		if l.target == "" {
			out = append(out, l.tree)
			continue
		}
		out = append(out, l.tree+strings.Repeat(" ", width-len([]rune(l.tree)))+"    → "+l.target)
	}
	return out
}
