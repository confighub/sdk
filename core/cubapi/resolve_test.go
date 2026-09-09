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

const (
	targetUUID = "11111111-1111-1111-1111-111111111111"
	spaceUUID  = "55555555-5555-5555-5555-555555555555"
	otherSpace = "99999999-9999-9999-9999-999999999999"
)

func TestParseRef(t *testing.T) {
	for _, tc := range []struct {
		in        string
		wantSpace string
		wantName  string
		wantIsID  bool
	}{
		{"my-unit", "", "my-unit", false},
		{"my-space/my-unit", "my-space", "my-unit", false},
		{targetUUID, "", targetUUID, true},
		{"my-space/" + targetUUID, "my-space", targetUUID, true},
		{"*/my-unit", "*", "my-unit", false},
		{"  spaced  ", "", "spaced", false},
		{"", "", "", false},
	} {
		got := ParseRef(tc.in)
		if got.Space != tc.wantSpace || got.Name != tc.wantName || got.IsID() != tc.wantIsID {
			t.Errorf("ParseRef(%q) = {Space:%q Name:%q IsID:%v}, want {%q %q %v}",
				tc.in, got.Space, got.Name, got.IsID(), tc.wantSpace, tc.wantName, tc.wantIsID)
		}
	}
}

func TestParseRefRoundTrips(t *testing.T) {
	for _, in := range []string{"my-unit", "my-space/my-unit", targetUUID} {
		if got := ParseRef(in).String(); got != in {
			t.Errorf("ParseRef(%q).String() = %q", in, got)
		}
	}
	// A UUID needs no space, so a qualified one renders bare.
	if got := ParseRef("my-space/" + targetUUID).String(); got != targetUUID {
		t.Errorf("qualified UUID rendered as %q, want %q", got, targetUUID)
	}
}

// captureWhere serves body and records the where clause of each request, so a
// test can assert on the filter that was built rather than only on the result.
func captureWhere(t *testing.T, body string, wheres *[]string) *Client {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		dec, _ := url.QueryUnescape(r.URL.Query().Get("where"))
		*wheres = append(*wheres, dec)
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

// TestResolveByIDIgnoresSpace is the regression guard for the defect this
// resolution layer replaces: the old apiGet<E>FromSlugInSpace routed a UUID to
// a space-scoped endpoint, so a UUID naming an entity in another space came
// back "not found" -- or panicked, when the caller's space global was empty.
// A UUID identifies a target outright, so the filter must carry no SpaceID.
func TestResolveByIDIgnoresSpace(t *testing.T) {
	var wheres []string
	body := `[{"Target":{"Slug":"prod-cluster","TargetID":"` + targetUUID + `","SpaceID":"` + otherSpace + `"}}]`
	c := captureWhere(t, body, &wheres)

	// Ask from a space the target is not in.
	got, err := ResolveTarget(context.Background(), c, ParseRef(targetUUID),
		ResolveOpts{Space: goclientnew.UUID(uuid.MustParse(spaceUUID))})
	if err != nil {
		t.Fatalf("ResolveTarget by UUID: %v", err)
	}
	if got.Target.Slug != "prod-cluster" {
		t.Fatalf("resolved %+v", got.Target)
	}
	if len(wheres) != 1 {
		t.Fatalf("requests = %d, want 1", len(wheres))
	}
	if !strings.Contains(wheres[0], "TargetID = '"+targetUUID+"'") {
		t.Errorf("where = %q, want a TargetID clause", wheres[0])
	}
	if strings.Contains(wheres[0], "SpaceID") {
		t.Errorf("where = %q, must not scope a UUID by space", wheres[0])
	}
}

func TestResolveBySlugScopesToSpace(t *testing.T) {
	var wheres []string
	body := `[{"Target":{"Slug":"prod-cluster","TargetID":"` + targetUUID + `","SpaceID":"` + spaceUUID + `"}}]`
	c := captureWhere(t, body, &wheres)

	if _, err := ResolveTarget(context.Background(), c, ParseRef("prod-cluster"),
		ResolveOpts{Space: goclientnew.UUID(uuid.MustParse(spaceUUID))}); err != nil {
		t.Fatalf("ResolveTarget by slug: %v", err)
	}
	if !strings.Contains(wheres[0], "Slug = 'prod-cluster'") ||
		!strings.Contains(wheres[0], "SpaceID = '"+spaceUUID+"'") {
		t.Errorf("where = %q, want both a Slug and a SpaceID clause", wheres[0])
	}
}

// TestResolveBySlugWithoutSpaceSearchesOrg covers the case that used to panic:
// a command that never resolved a space has no space to scope by, and the
// lookup goes organization-wide rather than parsing an empty string as a UUID.
func TestResolveBySlugWithoutSpaceSearchesOrg(t *testing.T) {
	var wheres []string
	body := `[{"Target":{"Slug":"prod-cluster","TargetID":"` + targetUUID + `"}}]`
	c := captureWhere(t, body, &wheres)

	if _, err := ResolveTarget(context.Background(), c, ParseRef("prod-cluster"), ResolveOpts{}); err != nil {
		t.Fatalf("ResolveTarget: %v", err)
	}
	if strings.Contains(wheres[0], "SpaceID") {
		t.Errorf("where = %q, want no space clause", wheres[0])
	}
}

// TestResolveQualifiedRefOverridesOptsSpace: what the user wrote wins over the
// caller's default, which is what makes "cub target get other-space/t" work
// while --space names something else.
func TestResolveQualifiedRefOverridesOptsSpace(t *testing.T) {
	var wheres []string
	body := `[{"Space":{"Slug":"other","SpaceID":"` + otherSpace + `"}},
	          {"Target":{"Slug":"t","TargetID":"` + targetUUID + `"}}]`
	// One stub serves both the space lookup and the target lookup; each
	// resolver filters the body down to what it matched, so the extra element
	// is ignored by the one it does not belong to.
	c := captureWhere(t, body, &wheres)

	if _, err := ResolveTarget(context.Background(), c, ParseRef("other/t"),
		ResolveOpts{Space: goclientnew.UUID(uuid.MustParse(spaceUUID))}); err != nil {
		t.Fatalf("ResolveTarget: %v", err)
	}
	if len(wheres) != 2 {
		t.Fatalf("requests = %d, want 2 (space then target)", len(wheres))
	}
	if !strings.Contains(wheres[0], "Slug = 'other'") {
		t.Errorf("first where = %q, want the space lookup", wheres[0])
	}
	if !strings.Contains(wheres[1], "SpaceID = '"+otherSpace+"'") {
		t.Errorf("second where = %q, want the ref's space, not opts.Space", wheres[1])
	}
	if strings.Contains(wheres[1], spaceUUID) {
		t.Errorf("second where = %q, must not use opts.Space", wheres[1])
	}
}

func TestResolveWildcardSpaceSearchesOrg(t *testing.T) {
	var wheres []string
	c := captureWhere(t, `[{"Target":{"Slug":"t","TargetID":"`+targetUUID+`"}}]`, &wheres)
	if _, err := ResolveTarget(context.Background(), c, ParseRef("*/t"),
		ResolveOpts{Space: goclientnew.UUID(uuid.MustParse(spaceUUID))}); err != nil {
		t.Fatalf("ResolveTarget: %v", err)
	}
	if strings.Contains(wheres[0], "SpaceID") {
		t.Errorf("where = %q, want no space clause for */slug", wheres[0])
	}
}

func TestResolveAmbiguousSlug(t *testing.T) {
	var wheres []string
	body := `[{"Target":{"Slug":"t","TargetID":"` + targetUUID + `","SpaceID":"` + spaceUUID + `"}},
	          {"Target":{"Slug":"t","TargetID":"22222222-2222-2222-2222-222222222222","SpaceID":"` + otherSpace + `"}}]`
	c := captureWhere(t, body, &wheres)
	_, err := ResolveTarget(context.Background(), c, ParseRef("t"), ResolveOpts{})
	if err == nil {
		t.Fatal("want an error for a slug matching in two spaces")
	}
	if !strings.Contains(err.Error(), "ambiguous") {
		t.Errorf("err = %v, want it to say the reference is ambiguous", err)
	}
}

func TestResolveNotFoundNamesWhatWasSought(t *testing.T) {
	var wheres []string
	c := captureWhere(t, `[]`, &wheres)
	_, err := ResolveTarget(context.Background(), c, ParseRef("ghost"), ResolveOpts{})
	if err == nil {
		t.Fatal("want an error")
	}
	if !strings.Contains(err.Error(), "target") || !strings.Contains(err.Error(), "ghost") {
		t.Errorf("err = %v, want it to name the entity type and the reference", err)
	}
}

func TestResolveEmptyRef(t *testing.T) {
	var wheres []string
	c := captureWhere(t, `[]`, &wheres)
	if _, err := ResolveTarget(context.Background(), c, ParseRef(""), ResolveOpts{}); err == nil {
		t.Fatal("want an error for an empty reference")
	}
	if len(wheres) != 0 {
		t.Errorf("an empty reference must not reach the server, got %d requests", len(wheres))
	}
}

// TestResolveDefaultInclude: the default expansion is the one the entity's
// "get" display needs, so that a slug and a UUID return the same envelope.
func TestResolveDefaultInclude(t *testing.T) {
	var gotInclude string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotInclude, _ = url.QueryUnescape(r.URL.Query().Get("include"))
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[{"Target":{"Slug":"t","TargetID":"` + targetUUID + `"}}]`))
	}))
	t.Cleanup(srv.Close)
	c, err := NewClient(ClientOptions{ServerURL: srv.URL, Token: "t"})
	if err != nil {
		t.Fatalf("client: %v", err)
	}
	if _, err := ResolveTarget(context.Background(), c, ParseRef("t"), ResolveOpts{}); err != nil {
		t.Fatalf("ResolveTarget: %v", err)
	}
	if gotInclude != targetGetInclude {
		t.Errorf("include = %q, want %q", gotInclude, targetGetInclude)
	}

	if _, err := ResolveTarget(context.Background(), c, ParseRef("t"), ResolveOpts{Include: "SpaceID"}); err != nil {
		t.Fatalf("ResolveTarget: %v", err)
	}
	if gotInclude != "SpaceID" {
		t.Errorf("include = %q, want the override to win", gotInclude)
	}
}
