// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"fmt"
	"slices"
	"strings"
	"unicode"

	"github.com/cockroachdb/errors"
	"github.com/confighub/sdk/core/cubapi"
	goclientnew "github.com/confighub/sdk/core/openapi/goclient-new"
	"github.com/google/uuid"
)

// A workflow with a stage per Space, as --stage-per-space builds it, promotes a change one Space
// at a time down the clone lineage of the Space it is created in, the root. It is shaped for an
// UpgradeUnit ChangeOrder created in that root: such a change order propagates hop by hop over
// UpgradeUnit Links, so the Spaces it is headed for have to be reachable from the root through
// one another, and a Space has to come after every Space it takes the change from. The server
// refuses a change order that names a Space it could never reach; building the workflow refuses
// the same Spaces, while the selection that named them is still in hand.

// spaceIDQueryChunk bounds how many ids one IN clause names, keeping each where expression well
// under the length the server accepts.
const spaceIDQueryChunk = 100

// spaceLinkGraph is which Spaces take UpgradeUnit changes from which, among the Spaces reachable
// downstream of a root.
type spaceLinkGraph struct {
	root uuid.UUID
	// downstream maps a Space to the Spaces that have a Link to one of its Units.
	downstream map[uuid.UUID][]uuid.UUID
	// upstream is the reverse: the Spaces a Space takes from.
	upstream map[uuid.UUID][]uuid.UUID
	// reached is every Space reachable from the root, the root included.
	reached map[uuid.UUID]bool
}

func newSpaceLinkGraph(root uuid.UUID) *spaceLinkGraph {
	return &spaceLinkGraph{
		root:       root,
		downstream: map[uuid.UUID][]uuid.UUID{},
		upstream:   map[uuid.UUID][]uuid.UUID{},
		reached:    map[uuid.UUID]bool{root: true},
	}
}

// addEdge records that downstreamID takes from upstreamID. A Link within one Space is not one
// Space taking from another, and a Link into the root is something the root takes from elsewhere,
// which is not where a change made in it goes.
func (graph *spaceLinkGraph) addEdge(upstreamID, downstreamID uuid.UUID) bool {
	if upstreamID == downstreamID || downstreamID == graph.root {
		return false
	}
	if slices.Contains(graph.downstream[upstreamID], downstreamID) {
		return false
	}
	graph.downstream[upstreamID] = append(graph.downstream[upstreamID], downstreamID)
	graph.upstream[downstreamID] = append(graph.upstream[downstreamID], upstreamID)
	return true
}

// walkUpgradeUnitGraph follows UpgradeUnit Links outward from the root, the way the server does
// to find where an UpgradeUnit ChangeOrder can propagate, without narrowing it to any selection:
// telling a Space the selection cuts off from one nothing reaches takes the whole graph.
func walkUpgradeUnitGraph(root uuid.UUID) (*spaceLinkGraph, error) {
	graph := newSpaceLinkGraph(root)
	frontier := []uuid.UUID{root}
	for len(frontier) > 0 {
		var next []uuid.UUID
		for chunk := range slices.Chunk(frontier, spaceIDQueryChunk) {
			links, err := cubapi.ListLinks(ctx, cubClient,
				cubapi.NewWhere("UpdateType = 'UpgradeUnit'").In("ToSpaceID", chunk),
				cubapi.ListOpts{Select: "LinkID,SpaceID,ToSpaceID"})
			if err != nil {
				return nil, errors.Wrap(err, "failed to list the UpgradeUnit links downstream of the space")
			}
			for _, link := range links {
				// Link.SpaceID is the downstream Space, ToSpaceID the upstream one.
				downstreamID, upstreamID := link.Link.SpaceID, link.Link.ToSpaceID
				// A repeated edge, or one addEdge leaves out, leads nowhere new.
				if graph.addEdge(upstreamID, downstreamID) && !graph.reached[downstreamID] {
					graph.reached[downstreamID] = true
					next = append(next, downstreamID)
				}
			}
		}
		frontier = next
	}
	return graph, nil
}

// upgradeUnitAncestors walks upstream from the root: the Spaces it takes from, and those they
// take from. Only asked for when a selected Space is unreachable, to say why.
func upgradeUnitAncestors(root uuid.UUID) (map[uuid.UUID]bool, error) {
	ancestors := map[uuid.UUID]bool{}
	visited := map[uuid.UUID]bool{root: true}
	frontier := []uuid.UUID{root}
	for len(frontier) > 0 {
		var next []uuid.UUID
		for chunk := range slices.Chunk(frontier, spaceIDQueryChunk) {
			links, err := cubapi.ListLinks(ctx, cubClient,
				cubapi.NewWhere("UpdateType = 'UpgradeUnit'").In("SpaceID", chunk),
				cubapi.ListOpts{Select: "LinkID,SpaceID,ToSpaceID"})
			if err != nil {
				return nil, errors.Wrap(err, "failed to list the UpgradeUnit links upstream of the space")
			}
			for _, link := range links {
				upstreamID := link.Link.ToSpaceID
				if visited[upstreamID] {
					continue
				}
				visited[upstreamID] = true
				ancestors[upstreamID] = true
				next = append(next, upstreamID)
			}
		}
		frontier = next
	}
	return ancestors, nil
}

// reachedThrough is the Spaces reachable from the root when the walk enters only selected
// Spaces, which is how the server walks for a change order headed for exactly those: the root is
// where the change already is, and every Space between it and a destination has to be a
// destination too. A nil selection enters every Space. The root is not in the result.
func (graph *spaceLinkGraph) reachedThrough(selected map[uuid.UUID]bool) map[uuid.UUID]bool {
	reached := map[uuid.UUID]bool{}
	frontier := []uuid.UUID{graph.root}
	for len(frontier) > 0 {
		var next []uuid.UUID
		for _, spaceID := range frontier {
			for _, downstreamID := range graph.downstream[spaceID] {
				if reached[downstreamID] || (selected != nil && !selected[downstreamID]) {
					continue
				}
				reached[downstreamID] = true
				next = append(next, downstreamID)
			}
		}
		frontier = next
	}
	return reached
}

// unreachableSpaces explains each selected Space the walk through the selection does not reach,
// by slug: one the selection cuts off, naming the Spaces it takes from that were left out; one
// upstream of the root, which a change made in the root never flows to; or one not downstream
// of the root at all. ancestors is read only when a Space is in neither of the
// first two.
func (graph *spaceLinkGraph) unreachableSpaces(selected, reached map[uuid.UUID]bool,
	slugs map[uuid.UUID]string, ancestors func() (map[uuid.UUID]bool, error)) ([]string, error) {
	name := func(spaceID uuid.UUID) string {
		if slug, known := slugs[spaceID]; known {
			return slug
		}
		return spaceID.String()
	}
	var unreached []uuid.UUID
	for spaceID := range selected {
		if !reached[spaceID] {
			unreached = append(unreached, spaceID)
		}
	}
	slices.SortFunc(unreached, func(a, b uuid.UUID) int { return strings.Compare(name(a), name(b)) })

	var problems []string
	var upstreamOfRoot map[uuid.UUID]bool
	for _, spaceID := range unreached {
		if graph.reached[spaceID] {
			var missing []string
			for _, upstreamID := range graph.upstream[spaceID] {
				if upstreamID != graph.root && !selected[upstreamID] {
					missing = append(missing, name(upstreamID))
				}
			}
			slices.Sort(missing)
			if len(missing) == 0 {
				problems = append(problems, fmt.Sprintf("%s: a space further up its path is not selected", name(spaceID)))
			} else {
				problems = append(problems, fmt.Sprintf("%s: takes the change from %s, which is not selected", name(spaceID), strings.Join(missing, ", ")))
			}
			continue
		}
		if upstreamOfRoot == nil {
			var err error
			if upstreamOfRoot, err = ancestors(); err != nil {
				return nil, err
			}
		}
		if upstreamOfRoot[spaceID] {
			problems = append(problems, fmt.Sprintf("%s: is upstream of the root space; a change made in the root never flows to it", name(spaceID)))
		} else {
			problems = append(problems, fmt.Sprintf("%s: is not downstream of the root space over UpgradeUnit links", name(spaceID)))
		}
	}
	return problems, nil
}

// spaceStageRank is where a Space goes among those the Link graph leaves free to go first: the
// order a change is usually rolled out in, read off the Space's slug and labels.
type spaceStageRank int

const (
	spaceStageRankBase spaceStageRank = iota
	spaceStageRankNonprod
	spaceStageRankOther
	spaceStageRankProd
)

// The words that place a Space, matched against whole words of its slug and labels. Inflections
// are listed rather than matched by prefix, which would read "products" as production.
var (
	spaceStageBaseWords    = []string{"base", "bases"}
	spaceStageNonprodWords = []string{"nonprod", "nonproduction", "preprod", "preproduction",
		"test", "tests", "testing", "staging", "preview", "previews"}
	spaceStageProdWords = []string{"prod", "production", "public"}
)

// rankSpaceStage reads how early a Space goes from its slug, its Stage and Environment labels and
// whether it releases anywhere: a base, which says "base" or has no release target, first; then
// a Space for testing, preproduction included; then whatever says nothing either way; and
// production last. A name that says both, like prod-test, is read as the earlier of the two.
func rankSpaceStage(space *goclientnew.Space) spaceStageRank {
	slug := spaceStageWords(space.Slug)
	stage := spaceStageWords(space.Labels["Stage"])
	environment := spaceStageWords(space.Labels["Environment"])
	says := func(words []string, among ...[]string) bool {
		for _, names := range among {
			for _, word := range names {
				if slices.Contains(words, word) {
					return true
				}
			}
		}
		return false
	}
	switch {
	case space.ReleaseTargetID == nil || says(spaceStageBaseWords, slug, stage):
		return spaceStageRankBase
	case says(spaceStageNonprodWords, slug, stage, environment):
		return spaceStageRankNonprod
	case says(spaceStageProdWords, slug, stage, environment):
		return spaceStageRankProd
	default:
		return spaceStageRankOther
	}
}

// spaceStageWords splits a slug or label value into lowercase words: at anything that is not a
// letter or a digit, between a letter and a digit, and where a lowercase letter meets an
// uppercase one, so prod-use2, prod_use_2, ProdUse2 and PROD-USE2 all say prod. "non" or "pre"
// before a word joins it, so non-prod and Pre_Prod say nonprod and preprod rather than prod.
func spaceStageWords(value string) []string {
	var words []string
	var word []rune
	flush := func() {
		if len(word) > 0 {
			words = append(words, strings.ToLower(string(word)))
			word = word[:0]
		}
	}
	runes := []rune(value)
	for i, r := range runes {
		if !unicode.IsLetter(r) && !unicode.IsDigit(r) {
			flush()
			continue
		}
		if i > 0 && len(word) > 0 {
			previous := runes[i-1]
			if unicode.IsDigit(previous) != unicode.IsDigit(r) ||
				(unicode.IsLower(previous) && unicode.IsUpper(r)) {
				flush()
			}
		}
		word = append(word, r)
	}
	flush()

	joined := words[:0:0]
	for i := 0; i < len(words); i++ {
		if (words[i] == "non" || words[i] == "pre") && i+1 < len(words) {
			joined = append(joined, words[i]+words[i+1])
			i++
			continue
		}
		joined = append(joined, words[i])
	}
	return joined
}

// orderSpaceStages puts the reached Spaces in the order a change is promoted into them: every
// Space after each Space it takes from, and among the Spaces free to go next, by rank and then by
// slug. Spaces that take from one another in a cycle have no such order, and are refused.
func (graph *spaceLinkGraph) orderSpaceStages(reached map[uuid.UUID]bool,
	spaces map[uuid.UUID]*goclientnew.Space) ([]*goclientnew.Space, error) {
	// How many reached Spaces each Space still waits on. The root already has the change, and a
	// Space it takes from that is not reached does not carry this change to it, so it waits on
	// neither.
	waiting := map[uuid.UUID]int{}
	for spaceID := range reached {
		for _, upstreamID := range graph.upstream[spaceID] {
			if reached[upstreamID] {
				waiting[spaceID]++
			}
		}
	}

	less := func(a, b uuid.UUID) int {
		if rankA, rankB := rankSpaceStage(spaces[a]), rankSpaceStage(spaces[b]); rankA != rankB {
			return int(rankA) - int(rankB)
		}
		return strings.Compare(spaces[a].Slug, spaces[b].Slug)
	}
	var ready []uuid.UUID
	for spaceID := range reached {
		if waiting[spaceID] == 0 {
			ready = append(ready, spaceID)
		}
	}
	ordered := make([]*goclientnew.Space, 0, len(reached))
	done := map[uuid.UUID]bool{}
	for len(ready) > 0 {
		slices.SortFunc(ready, less)
		spaceID := ready[0]
		ready = ready[1:]
		done[spaceID] = true
		ordered = append(ordered, spaces[spaceID])
		for _, downstreamID := range graph.downstream[spaceID] {
			if !reached[downstreamID] {
				continue
			}
			waiting[downstreamID]--
			if waiting[downstreamID] == 0 {
				ready = append(ready, downstreamID)
			}
		}
	}
	if len(ordered) < len(reached) {
		var cycle []string
		for spaceID := range reached {
			if !done[spaceID] {
				cycle = append(cycle, spaces[spaceID].Slug)
			}
		}
		slices.Sort(cycle)
		return nil, errors.Newf("spaces %s take changes from one another in a cycle, so no stage order promotes each after the spaces it takes from", strings.Join(cycle, ", "))
	}
	return ordered, nil
}

// listSpacesByID fetches the Spaces with the given ids, with what ranking and naming them reads.
func listSpacesByID(spaceIDs []uuid.UUID) (map[uuid.UUID]*goclientnew.Space, error) {
	spaces := map[uuid.UUID]*goclientnew.Space{}
	for chunk := range slices.Chunk(spaceIDs, spaceIDQueryChunk) {
		found, err := cubapi.ListSpaces(ctx, cubClient, cubapi.NewWhere("").In("SpaceID", chunk),
			cubapi.ListOpts{Select: stageSpaceFields})
		if err != nil {
			return nil, errors.Wrap(err, "failed to list the spaces downstream of the space")
		}
		for _, space := range found {
			spaces[space.Space.SpaceID] = space.Space
		}
	}
	return spaces, nil
}

// stageSpaceFields are the Space fields a stage per Space is built from.
const stageSpaceFields = "SpaceID,Slug,Labels,ReleaseTargetID,OrganizationID"

// changeWorkflowStagesPerSpace builds a stage for each Space a change made in root reaches over
// UpgradeUnit Links, in promotion order. A non-empty selection -- a where expression over Spaces,
// a Component, or both, which are ANDed -- narrows it to the Spaces it selects, every one of which
// the change has to be able to reach through the others; without one, every Space the Links reach
// gets a stage. The root has none: a change order made there already has the change.
// prerequisites gate the stages as spaceStages describes.
func changeWorkflowStagesPerSpace(root uuid.UUID, rootSlug, whereSpace string, componentID *uuid.UUID,
	prerequisites []string) ([]goclientnew.ChangeWorkflowStage, *goclientnew.ChangeWorkflowFinalStage, error) {
	graph, err := walkUpgradeUnitGraph(root)
	if err != nil {
		return nil, nil, err
	}

	var selected map[uuid.UUID]bool
	spaces := map[uuid.UUID]*goclientnew.Space{}
	selection := cubapi.NewWhere(whereSpace)
	if componentID != nil {
		selection = selection.And(fmt.Sprintf("ComponentID = '%s'", *componentID))
	}
	if !selection.Empty() {
		found, err := cubapi.ListSpaces(ctx, cubClient, selection, cubapi.ListOpts{Select: stageSpaceFields})
		if err != nil {
			return nil, nil, errors.Wrap(err, "failed to list the spaces --component and --where-space-field select")
		}
		selected = map[uuid.UUID]bool{}
		for _, space := range found {
			if space.Space.SpaceID == root {
				continue
			}
			selected[space.Space.SpaceID] = true
			spaces[space.Space.SpaceID] = space.Space
		}
		if len(selected) == 0 {
			return nil, nil, errors.Newf("no space other than %s matches %q, so there is no space to make a stage of", rootSlug, selection.String())
		}
	}

	reached := graph.reachedThrough(selected)
	if selected != nil {
		var missingSlugs []uuid.UUID
		for spaceID := range selected {
			if !reached[spaceID] {
				missingSlugs = append(missingSlugs, graph.upstream[spaceID]...)
			}
		}
		slugs := map[uuid.UUID]string{root: rootSlug}
		for spaceID, space := range spaces {
			slugs[spaceID] = space.Slug
		}
		ancestorsOfRoot := func() (map[uuid.UUID]bool, error) { return upgradeUnitAncestors(root) }
		if err := addSlugs(slugs, missingSlugs); err != nil {
			return nil, nil, err
		}
		problems, err := graph.unreachableSpaces(selected, reached, slugs, ancestorsOfRoot)
		if err != nil {
			return nil, nil, err
		}
		if len(problems) > 0 {
			return nil, nil, errors.Newf("an UpgradeUnit change order created in %s could not reach every selected space, since it propagates hop by hop over UpgradeUnit links:\n  %s\nSelect the spaces in between too, leave these out with --where-space-field, or create the workflow in a space they are all downstream of",
				rootSlug, strings.Join(problems, "\n  "))
		}
	} else {
		if len(reached) == 0 {
			return nil, nil, errors.Newf("no space takes changes from %s over UpgradeUnit links, so there is no space to make a stage of", rootSlug)
		}
		ids := make([]uuid.UUID, 0, len(reached))
		for spaceID := range reached {
			ids = append(ids, spaceID)
		}
		if spaces, err = listSpacesByID(ids); err != nil {
			return nil, nil, err
		}
		// A Space the caller cannot view is not one a change order can be sent to either.
		for spaceID := range reached {
			if spaces[spaceID] == nil {
				return nil, nil, errors.Newf("space %s takes changes from %s, but could not be read; View permission on it is required", spaceID, rootSlug)
			}
		}
	}

	ordered, err := graph.orderSpaceStages(reached, spaces)
	if err != nil {
		return nil, nil, err
	}
	stages, final := spaceStages(ordered, prerequisites)
	return stages, final, nil
}

// spaceStages makes a stage of each Space, in order, and gives every stage after the first, and
// the final stage, the gates prerequisites names, as --prerequisites does for the stages --stage
// names. A stage's gates are checked over the stage before it, and Healthy can never pass over a
// Space with no release target, so a stage after one, or a final stage when the last Space has
// none, is not given Healthy: it would hold every change there for good.
func spaceStages(ordered []*goclientnew.Space, prerequisites []string) ([]goclientnew.ChangeWorkflowStage, *goclientnew.ChangeWorkflowFinalStage) {
	gatesOver := func(space *goclientnew.Space) []string {
		if len(prerequisites) == 0 {
			return nil
		}
		if space.ReleaseTargetID != nil {
			return slices.Clone(prerequisites)
		}
		return slices.DeleteFunc(slices.Clone(prerequisites), func(gate string) bool {
			return strings.EqualFold(gate, "Healthy")
		})
	}
	stages := make([]goclientnew.ChangeWorkflowStage, 0, len(ordered))
	var previous *goclientnew.Space
	for _, space := range ordered {
		// Selected by id, so renaming the Space does not leave its stage selecting nothing; the
		// stage's name is what a person reads.
		stage := goclientnew.ChangeWorkflowStage{
			Name:       space.Slug,
			WhereSpace: fmt.Sprintf("SpaceID = '%s'", space.SpaceID),
		}
		// The first stage's gates would be checked over the root, where the change starts.
		if previous != nil {
			stage.Prerequisites = gatesOver(previous)
		}
		stages = append(stages, stage)
		previous = space
	}
	final := &goclientnew.ChangeWorkflowFinalStage{}
	if previous != nil {
		final.Prerequisites = gatesOver(previous)
	}
	return stages, final
}

// addSlugs adds the slugs of the given Spaces that slugs does not have yet, for naming them in an
// error.
func addSlugs(slugs map[uuid.UUID]string, spaceIDs []uuid.UUID) error {
	var unknown []uuid.UUID
	for _, spaceID := range spaceIDs {
		if _, known := slugs[spaceID]; !known && !slices.Contains(unknown, spaceID) {
			unknown = append(unknown, spaceID)
		}
	}
	if len(unknown) == 0 {
		return nil
	}
	spaces, err := listSpacesByID(unknown)
	if err != nil {
		return err
	}
	for spaceID, space := range spaces {
		slugs[spaceID] = space.Slug
	}
	return nil
}
