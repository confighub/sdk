// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"reflect"
	"strings"
	"testing"

	goclientnew "github.com/confighub/sdk/core/openapi/goclient-new"
	"github.com/google/uuid"
)

// stageTopology builds a graph and its Spaces from slugs. released names the Spaces that have a
// release target; edges are upstream -> downstream.
type stageTopology struct {
	ids    map[string]uuid.UUID
	spaces map[uuid.UUID]*goclientnew.Space
	graph  *spaceLinkGraph
}

func newStageTopology(root string, released []string, labels map[string]map[string]string, edges [][2]string) *stageTopology {
	topology := &stageTopology{ids: map[string]uuid.UUID{}, spaces: map[uuid.UUID]*goclientnew.Space{}}
	id := func(slug string) uuid.UUID {
		if spaceID, ok := topology.ids[slug]; ok {
			return spaceID
		}
		spaceID := uuid.New()
		topology.ids[slug] = spaceID
		topology.spaces[spaceID] = &goclientnew.Space{SpaceID: spaceID, Slug: slug, Labels: labels[slug]}
		return spaceID
	}
	topology.graph = newSpaceLinkGraph(id(root))
	target := uuid.New()
	for _, slug := range released {
		topology.spaces[id(slug)].ReleaseTargetID = &target
	}
	// The edges are added in the order the walk would find them, breadth first from the root.
	for _, edge := range edges {
		upstreamID, downstreamID := id(edge[0]), id(edge[1])
		if topology.graph.addEdge(upstreamID, downstreamID) {
			topology.graph.reached[downstreamID] = true
		}
	}
	return topology
}

func (topology *stageTopology) set(slugs ...string) map[uuid.UUID]bool {
	set := map[uuid.UUID]bool{}
	for _, slug := range slugs {
		set[topology.ids[slug]] = true
	}
	return set
}

func (topology *stageTopology) order(t *testing.T, selected map[uuid.UUID]bool) []string {
	t.Helper()
	ordered, err := topology.graph.orderSpaceStages(topology.graph.reachedThrough(selected), topology.spaces)
	if err != nil {
		t.Fatal(err)
	}
	slugs := make([]string, len(ordered))
	for i, space := range ordered {
		slugs[i] = space.Slug
	}
	return slugs
}

// confighub-app in ConfigHubOps: a base with two bases cloned from it, one feeding a preview base
// and staging, the other production.
func TestOrderSpaceStagesConfigHubApp(t *testing.T) {
	topology := newStageTopology("base-confighub-app",
		[]string{"staging-use2-confighub-app", "prod-use2-confighub-app"},
		map[string]map[string]string{
			"base-preview-confighub-app": {"Stage": "none", "Variant": "preview"},
			"staging-use2-confighub-app": {"Stage": "test", "Variant": "staging"},
			"prod-use2-confighub-app":    {"Stage": "public", "Environment": "prod"},
		},
		[][2]string{
			{"base-confighub-app", "base-rds-confighub-app"},
			{"base-confighub-app", "base-ephemeral-db-confighub-app"},
			{"base-rds-confighub-app", "prod-use2-confighub-app"},
			{"base-ephemeral-db-confighub-app", "staging-use2-confighub-app"},
			{"base-ephemeral-db-confighub-app", "base-preview-confighub-app"},
		})
	want := []string{
		"base-ephemeral-db-confighub-app",
		"base-preview-confighub-app",
		"base-rds-confighub-app",
		"staging-use2-confighub-app",
		"prod-use2-confighub-app",
	}
	if got := topology.order(t, nil); !reflect.DeepEqual(got, want) {
		t.Errorf("order = %v, want %v", got, want)
	}
}

// A production Space free to go early still waits for the test Spaces, and a test Space
// downstream of a base still waits for the base.
func TestOrderSpaceStagesByRank(t *testing.T) {
	topology := newStageTopology("argobot-base",
		[]string{"prod-use2-argobot", "nonprod-argobot", "prod-use2-testwebsite", "nonprod-registrybot", "edge"},
		map[string]map[string]string{
			"prod-use2-argobot":     {"Stage": "public", "Environment": "prod"},
			"nonprod-argobot":       {"Stage": "test", "Environment": "nonprod"},
			"prod-use2-testwebsite": {"Stage": "test", "Environment": "prod-test"},
			// Its Stage label says public, but its slug and Environment say nonprod.
			"nonprod-registrybot": {"Stage": "public", "Environment": "nonprod"},
		},
		[][2]string{
			{"argobot-base", "prod-use2-argobot"},
			{"argobot-base", "nonprod-argobot"},
			{"argobot-base", "edge"},
			{"argobot-base", "region-base"},
			{"region-base", "prod-use2-testwebsite"},
			{"region-base", "nonprod-registrybot"},
		})
	want := []string{"region-base", "nonprod-argobot", "nonprod-registrybot", "prod-use2-testwebsite", "edge", "prod-use2-argobot"}
	if got := topology.order(t, nil); !reflect.DeepEqual(got, want) {
		t.Errorf("order = %v, want %v", got, want)
	}
}

// A Space waits on every reached Space it takes from, not only the first one found.
func TestOrderSpaceStagesWaitsForEveryUpstream(t *testing.T) {
	topology := newStageTopology("root", []string{"prod", "staging", "test"}, nil,
		[][2]string{{"root", "prod"}, {"root", "staging"}, {"staging", "test"}, {"test", "prod"}})
	want := []string{"staging", "test", "prod"}
	if got := topology.order(t, nil); !reflect.DeepEqual(got, want) {
		t.Errorf("order = %v, want %v", got, want)
	}
}

func TestOrderSpaceStagesRefusesACycle(t *testing.T) {
	topology := newStageTopology("root", []string{"a", "b"}, nil,
		[][2]string{{"root", "a"}, {"a", "b"}, {"b", "a"}})
	_, err := topology.graph.orderSpaceStages(topology.graph.reachedThrough(nil), topology.spaces)
	if err == nil || !strings.Contains(err.Error(), "spaces a, b take changes from one another in a cycle") {
		t.Errorf("error = %v, want a cycle naming a and b", err)
	}
}

// The walk enters only selected Spaces, as the server's does for a change order headed for them,
// and a Link into the root is not somewhere the change goes.
func TestReachedThroughSelection(t *testing.T) {
	topology := newStageTopology("base", []string{"nonprod", "prod"}, nil,
		[][2]string{{"base", "nonprod"}, {"nonprod", "prod"}, {"prod", "base"}})
	if got := topology.graph.reachedThrough(topology.set("prod")); len(got) != 0 {
		t.Errorf("reached through prod alone = %v, want nothing: prod takes from nonprod", got)
	}
	if got := topology.order(t, topology.set("nonprod", "prod")); !reflect.DeepEqual(got, []string{"nonprod", "prod"}) {
		t.Errorf("order = %v, want [nonprod prod]", got)
	}
}

func TestUnreachableSpaces(t *testing.T) {
	topology := newStageTopology("base", []string{"nonprod", "prod", "other", "upstream"}, nil,
		[][2]string{{"base", "nonprod"}, {"nonprod", "prod"}})
	topology.graph.reached[topology.ids["upstream"]] = false
	// Spaces the walk never found, which the graph does not hold.
	for _, slug := range []string{"other", "upstream"} {
		spaceID := uuid.New()
		topology.ids[slug] = spaceID
		topology.spaces[spaceID] = &goclientnew.Space{SpaceID: spaceID, Slug: slug}
	}
	slugs := map[uuid.UUID]string{}
	for slug, spaceID := range topology.ids {
		slugs[spaceID] = slug
	}
	selected := topology.set("prod", "other", "upstream")
	ancestorsRead := false
	ancestors := func() (map[uuid.UUID]bool, error) {
		ancestorsRead = true
		return topology.set("upstream"), nil
	}
	problems, err := topology.graph.unreachableSpaces(selected, topology.graph.reachedThrough(selected), slugs, ancestors)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"other: is not downstream of the root space over UpgradeUnit links",
		"prod: takes the change from nonprod, which is not selected",
		"upstream: is upstream of the root space; a change made in the root never flows to it",
	}
	if !reflect.DeepEqual(problems, want) {
		t.Errorf("problems = %q, want %q", problems, want)
	}
	if !ancestorsRead {
		t.Error("the Spaces upstream of the root were not read")
	}

	// Nothing unreachable, nothing read.
	ancestorsRead = false
	selected = topology.set("nonprod", "prod")
	problems, err = topology.graph.unreachableSpaces(selected, topology.graph.reachedThrough(selected), slugs, ancestors)
	if err != nil || len(problems) != 0 || ancestorsRead {
		t.Errorf("problems = %q, err = %v, ancestors read = %v; want none", problems, err, ancestorsRead)
	}
}

func TestSpaceStageWords(t *testing.T) {
	tests := map[string][]string{
		"prod-use2-confighub-app": {"prod", "use", "2", "confighub", "app"},
		"ProdUse2":                {"prod", "use", "2"},
		"PROD_USE2":               {"prod", "use", "2"},
		"non-prod":                {"nonprod"},
		"Pre Prod east":           {"preprod", "east"},
		"pre":                     {"pre"},
		"":                        nil,
	}
	for value, want := range tests {
		if got := spaceStageWords(value); !reflect.DeepEqual(got, want) && (len(got) != 0 || len(want) != 0) {
			t.Errorf("spaceStageWords(%q) = %q, want %q", value, got, want)
		}
	}
}

func TestRankSpaceStage(t *testing.T) {
	target := uuid.New()
	tests := []struct {
		slug     string
		labels   map[string]string
		released bool
		want     spaceStageRank
	}{
		{slug: "nonprod", labels: map[string]string{"Stage": "test"}, want: spaceStageRankBase},
		{slug: "karpenter-base", released: true, want: spaceStageRankBase},
		{slug: "east", labels: map[string]string{"Stage": "base"}, released: true, want: spaceStageRankBase},
		// Base is read from the slug and the Stage label only.
		{slug: "east", labels: map[string]string{"Environment": "base"}, released: true, want: spaceStageRankOther},
		{slug: "nonprod-argocd", released: true, want: spaceStageRankNonprod},
		{slug: "ui-preview-pr-6326", labels: map[string]string{"Stage": "none"}, released: true, want: spaceStageRankNonprod},
		{slug: "east", labels: map[string]string{"Environment": "staging"}, released: true, want: spaceStageRankNonprod},
		{slug: "prod-use2-testwebsite", labels: map[string]string{"Environment": "prod-test"}, released: true, want: spaceStageRankNonprod},
		// Whole words only: testwebsite is not test, so the slug says prod.
		{slug: "prod-use2-testwebsite", released: true, want: spaceStageRankProd},
		{slug: "preprod-use2", released: true, want: spaceStageRankNonprod},
		{slug: "use2-pre-prod", released: true, want: spaceStageRankNonprod},
		{slug: "east", labels: map[string]string{"Environment": "Non_Prod"}, released: true, want: spaceStageRankNonprod},
		{slug: "east", labels: map[string]string{"Stage": "PreProduction"}, released: true, want: spaceStageRankNonprod},
		{slug: "PROD-USE2", released: true, want: spaceStageRankProd},
		{slug: "ProdUse2", released: true, want: spaceStageRankProd},
		{slug: "prod2", released: true, want: spaceStageRankProd},
		{slug: "products", released: true, want: spaceStageRankOther},
		{slug: "latest", released: true, want: spaceStageRankOther},
		{slug: "database", released: true, want: spaceStageRankOther},
		{slug: "prod-use2-argocd", released: true, want: spaceStageRankProd},
		{slug: "east", labels: map[string]string{"Stage": "public"}, released: true, want: spaceStageRankProd},
		{slug: "east", labels: map[string]string{"Environment": "production"}, released: true, want: spaceStageRankProd},
		{slug: "east", released: true, want: spaceStageRankOther},
	}
	for _, test := range tests {
		space := &goclientnew.Space{Slug: test.slug, Labels: test.labels}
		if test.released {
			space.ReleaseTargetID = &target
		}
		if got := rankSpaceStage(space); got != test.want {
			t.Errorf("rankSpaceStage(%s %v, released %v) = %d, want %d", test.slug, test.labels, test.released, got, test.want)
		}
	}
}

// Healthy is left off a stage after a Space with no release target, which could never pass it,
// and the first stage, gated over the root, gets nothing.
func TestSpaceStagesPrerequisites(t *testing.T) {
	target := uuid.New()
	ordered := []*goclientnew.Space{
		{SpaceID: uuid.New(), Slug: "base-preview"},
		{SpaceID: uuid.New(), Slug: "staging", ReleaseTargetID: &target},
		{SpaceID: uuid.New(), Slug: "prod", ReleaseTargetID: &target},
	}
	stages, final := spaceStages(ordered, []string{"Released", "Healthy", "one-approver"})
	got := [][]string{stages[0].Prerequisites, stages[1].Prerequisites, stages[2].Prerequisites, final.Prerequisites}
	want := [][]string{nil, {"Released", "one-approver"}, {"Released", "Healthy", "one-approver"}, {"Released", "Healthy", "one-approver"}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("prerequisites = %q, want %q", got, want)
	}
	if stages[1].WhereSpace != "SpaceID = '"+ordered[1].SpaceID.String()+"'" || stages[1].Name != "staging" {
		t.Errorf("stage = %+v, want staging selecting its SpaceID", stages[1])
	}

	_, final = spaceStages(ordered[:1], []string{"Healthy"})
	if len(final.Prerequisites) != 0 {
		t.Errorf("final prerequisites after a Space with no release target = %q, want none", final.Prerequisites)
	}
}
