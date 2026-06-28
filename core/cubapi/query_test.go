// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package cubapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	goclientnew "github.com/confighub/sdk/core/openapi/goclient-new"
	"github.com/google/uuid"
)

func TestWhereBuilder(t *testing.T) {
	id := goclientnew.UUID(uuid.MustParse("11111111-1111-1111-1111-111111111111"))
	id2 := goclientnew.UUID(uuid.MustParse("22222222-2222-2222-2222-222222222222"))

	tests := []struct {
		name string
		w    Where
		want string
	}{
		{"empty", Where{}, ""},
		{"single", NewWhere("ToolchainType = 'Kubernetes/YAML'"), "ToolchainType = 'Kubernetes/YAML'"},
		{"and skips empty", NewWhere("A = 1").And("").And("B = 2"), "A = 1 AND B = 2"},
		{"slug", NewWhere("").Slug("checkout"), "Slug = 'checkout'"},
		{"spaceID", NewWhere("A = 1").SpaceID(id), "A = 1 AND SpaceID = '11111111-1111-1111-1111-111111111111'"},
		{"in", NewWhere("").In("SpaceID", []goclientnew.UUID{id, id2}),
			"SpaceID IN ('11111111-1111-1111-1111-111111111111','22222222-2222-2222-2222-222222222222')"},
		{"in empty is noop", NewWhere("A = 1").In("SpaceID", nil), "A = 1"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.w.String(); got != tc.want {
				t.Fatalf("String() = %q, want %q", got, tc.want)
			}
		})
	}
	base := NewWhere("A = 1")
	_ = base.And("B = 2")
	if base.String() != "A = 1" {
		t.Fatalf("And mutated receiver: %q", base.String())
	}
}

func stubServer(t *testing.T, body string, lastWhere *string) *Client {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if lastWhere != nil {
			*lastWhere = r.URL.Query().Get("where")
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	c, err := NewClient(ClientOptions{ServerURL: srv.URL, Token: "t"})
	if err != nil {
		t.Fatalf("client: %v", err)
	}
	return c
}

func TestSelectFields(t *testing.T) {
	if got := SelectFields("*"); got != "" {
		t.Fatalf(`SelectFields("*") = %q, want ""`, got)
	}
	if got := SelectFields("Slug,SpaceID"); got != "Slug,SpaceID" {
		t.Fatalf("SelectFields passthrough = %q", got)
	}
	if got := SelectFields(""); got != "" {
		t.Fatalf(`SelectFields("") = %q`, got)
	}
}

func TestListUnits(t *testing.T) {
	body := `[
	  {"Unit":{"Slug":"checkout","UnitID":"33333333-3333-3333-3333-333333333333","ToolchainType":"Kubernetes/YAML"},"Space":{"Slug":"prod"}},
	  {"Unit":{"Slug":"cart","UnitID":"44444444-4444-4444-4444-444444444444","ToolchainType":"Kubernetes/YAML"}}
	]`
	var gotWhere string
	c := stubServer(t, body, &gotWhere)

	units, err := ListUnits(context.Background(), c, NewWhere("ToolchainType = 'Kubernetes/YAML'"), ListOpts{Include: "SpaceID"})
	if err != nil {
		t.Fatalf("ListUnits: %v", err)
	}
	if len(units) != 2 || units[0].Unit == nil || units[0].Unit.Slug != "checkout" {
		t.Fatalf("units = %+v", units)
	}
	if units[0].Space == nil || units[0].Space.Slug != "prod" {
		t.Fatalf("unit[0].Space = %+v", units[0].Space)
	}
	if dec, _ := url.QueryUnescape(gotWhere); dec != "ToolchainType = 'Kubernetes/YAML'" {
		t.Fatalf("where = %q", dec)
	}
}

func TestListFiltersMutator(t *testing.T) {
	body := `[{"Filter":{"Slug":"my-filter","From":"Unit"}}]`
	var gotEntity, gotID string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotEntity = r.URL.Query().Get("entity")
		gotID = r.URL.Query().Get("id")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	c, err := NewClient(ClientOptions{ServerURL: srv.URL, Token: "t"})
	if err != nil {
		t.Fatalf("client: %v", err)
	}

	entity := "Space"
	id := "abc-123"
	filters, err := ListFilters(context.Background(), c, NewWhere("From = 'Unit'"), ListOpts{Include: "SpaceID"},
		func(p *goclientnew.ListAllFiltersParams) {
			p.Entity = &entity
			p.Id = &id
		})
	if err != nil {
		t.Fatalf("ListFilters: %v", err)
	}
	if len(filters) != 1 || filters[0].Filter.Slug != "my-filter" {
		t.Fatalf("filters = %+v", filters)
	}
	if gotEntity != "Space" || gotID != "abc-123" {
		t.Fatalf("mutator not applied: entity=%q id=%q", gotEntity, gotID)
	}
}

func TestListUnitsMutator(t *testing.T) {
	var gotResourceType, gotView string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotResourceType = r.URL.Query().Get("resource_type")
		gotView = r.URL.Query().Get("view")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`[{"Unit":{"Slug":"checkout"}}]`))
	}))
	t.Cleanup(srv.Close)
	c, err := NewClient(ClientOptions{ServerURL: srv.URL, Token: "t"})
	if err != nil {
		t.Fatalf("client: %v", err)
	}
	rt := "apps/v1/Deployment"
	view := "my-view"
	units, err := ListUnits(context.Background(), c, NewWhere("ToolchainType = 'Kubernetes/YAML'"), ListOpts{Include: "SpaceID"},
		func(p *goclientnew.ListAllUnitsParams) {
			p.ResourceType = &rt
			p.View = &view
		})
	if err != nil {
		t.Fatalf("ListUnits: %v", err)
	}
	if len(units) != 1 || units[0].Unit.Slug != "checkout" {
		t.Fatalf("units = %+v", units)
	}
	if gotResourceType != rt || gotView != view {
		t.Fatalf("mutator not applied: resource_type=%q view=%q", gotResourceType, gotView)
	}
}

func TestListChangeSets(t *testing.T) {
	body := `[{"ChangeSet":{"Slug":"cs1"}},{"ChangeSet":{"Slug":"cs2"}}]`
	var gotWhere string
	c := stubServer(t, body, &gotWhere)
	cs, err := ListChangeSets(context.Background(), c, NewWhere("StartTagID = 'x'"), ListOpts{Include: "SpaceID"})
	if err != nil {
		t.Fatalf("ListChangeSets: %v", err)
	}
	if len(cs) != 2 || cs[0].ChangeSet.Slug != "cs1" {
		t.Fatalf("changesets = %+v", cs)
	}
}

func TestListBridgeWorkersMutatorAndFilter(t *testing.T) {
	var gotSummary, gotFilter string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotSummary = r.URL.Query().Get("summary")
		gotFilter = r.URL.Query().Get("filter")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`[{"BridgeWorker":{"Slug":"w1"}}]`))
	}))
	t.Cleanup(srv.Close)
	c, err := NewClient(ClientOptions{ServerURL: srv.URL, Token: "t"})
	if err != nil {
		t.Fatalf("client: %v", err)
	}
	workers, err := ListBridgeWorkers(context.Background(), c, Where{}, ListOpts{Filter: "space/my-filter"},
		func(p *goclientnew.ListAllBridgeWorkersParams) {
			summary := true
			p.Summary = &summary
		})
	if err != nil {
		t.Fatalf("ListBridgeWorkers: %v", err)
	}
	if len(workers) != 1 || workers[0].BridgeWorker.Slug != "w1" {
		t.Fatalf("workers = %+v", workers)
	}
	if gotSummary != "true" {
		t.Fatalf("summary = %q, want true", gotSummary)
	}
	if gotFilter != "space/my-filter" {
		t.Fatalf("filter = %q, want space/my-filter", gotFilter)
	}
}

func TestResolveSpace(t *testing.T) {
	var gotWhere string
	c := stubServer(t, `[{"Space":{"Slug":"prod","SpaceID":"55555555-5555-5555-5555-555555555555"}}]`, &gotWhere)
	sp, err := ResolveSpace(context.Background(), c, "prod")
	if err != nil || sp.Slug != "prod" {
		t.Fatalf("ResolveSpace = %v, %v", sp, err)
	}
	if dec, _ := url.QueryUnescape(gotWhere); !strings.Contains(dec, "Slug = 'prod'") {
		t.Fatalf("where = %q", dec)
	}
}

func TestListSpacesMutator(t *testing.T) {
	var gotSummary string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotSummary = r.URL.Query().Get("summary")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`[{"Space":{"Slug":"prod"}}]`))
	}))
	t.Cleanup(srv.Close)
	c, err := NewClient(ClientOptions{ServerURL: srv.URL, Token: "t"})
	if err != nil {
		t.Fatalf("client: %v", err)
	}
	spaces, err := ListSpaces(context.Background(), c, Where{}, ListOpts{}, func(p *goclientnew.ListSpacesParams) {
		summary := true
		p.Summary = &summary
	})
	if err != nil {
		t.Fatalf("ListSpaces: %v", err)
	}
	if len(spaces) != 1 || spaces[0].Space.Slug != "prod" {
		t.Fatalf("spaces = %+v", spaces)
	}
	if gotSummary != "true" {
		t.Fatalf("summary param = %q, want true", gotSummary)
	}
}

func TestResolveSpaceNotFound(t *testing.T) {
	c := stubServer(t, `[]`, nil)
	if _, err := ResolveSpace(context.Background(), c, "ghost"); err == nil {
		t.Fatal("ResolveSpace(missing) = nil, want error")
	}
}

func TestSpaceSlugByID(t *testing.T) {
	body := `[
	  {"Space":{"Slug":"prod","SpaceID":"55555555-5555-5555-5555-555555555555"}},
	  {"Space":{"Slug":"staging","SpaceID":"66666666-6666-6666-6666-666666666666"}}
	]`
	c := stubServer(t, body, nil)
	m, err := SpaceSlugByID(context.Background(), c)
	if err != nil {
		t.Fatalf("SpaceSlugByID: %v", err)
	}
	if m[goclientnew.UUID(uuid.MustParse("55555555-5555-5555-5555-555555555555"))] != "prod" || len(m) != 2 {
		t.Fatalf("map = %+v", m)
	}
}
