// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package cubapi

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"

	api "github.com/confighub/sdk/core/function/api"
	goclientnew "github.com/confighub/sdk/core/openapi/goclient-new"
)

const (
	csTestSpace  = "11111111-1111-1111-1111-111111111111"
	csTestSet    = "22222222-2222-2222-2222-222222222222"
	csTestOther  = "33333333-3333-3333-3333-333333333333"
	csTestUnitID = "44444444-4444-4444-4444-444444444444"
)

type csRequest struct {
	method, path string
	query        map[string]string
}

// changeSetServer answers the requests a write in a ChangeSet makes: resolving the ChangeSet,
// listing the Units it is opened on, the bulk patch that opens it, and the invocation. The one
// Unit it lists is in unitChangeSet ("" for none). It records every request.
func changeSetServer(t *testing.T, unitChangeSet string) (*Client, func() []csRequest) {
	t.Helper()
	var mu sync.Mutex
	var reqs []csRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		q := map[string]string{}
		for k := range r.URL.Query() {
			q[k] = r.URL.Query().Get(k)
		}
		path := strings.TrimPrefix(r.URL.Path, "/api")
		reqs = append(reqs, csRequest{r.Method, path, q})
		w.Header().Set("Content-Type", "application/json")
		unit := fmt.Sprintf(`{"UnitID":%q,"Slug":"web","SpaceID":%q`, csTestUnitID, csTestSpace)
		if unitChangeSet != "" {
			unit += fmt.Sprintf(`,"ChangeSetID":%q`, unitChangeSet)
		}
		unit += "}"
		switch {
		case r.Method == http.MethodGet && path == "/space":
			fmt.Fprintf(w, `[{"Space":{"SpaceID":%q,"Slug":"platform"}}]`, csTestSpace)
		case r.Method == http.MethodGet && path == "/change_set":
			fmt.Fprintf(w, `[{"ChangeSet":{"ChangeSetID":%q,"SpaceID":%q,"Slug":"review","State":"Open"}}]`, csTestSet, csTestSpace)
		case r.Method == http.MethodGet && path == "/unit":
			fmt.Fprintf(w, `[{"Unit":%s}]`, unit)
		case r.Method == http.MethodPatch && path == "/unit":
			fmt.Fprintf(w, `[{"Unit":%s}]`, unit)
		case r.Method == http.MethodPost && path == "/function/invoke":
			fmt.Fprintf(w, `[{"UnitID":%q,"UnitSlug":"web","Success":true,"HasNewMutations":true}]`, csTestUnitID)
		default:
			t.Errorf("unexpected request %s %s", r.Method, path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	c, err := NewClient(ClientOptions{ServerURL: srv.URL, Token: "t"})
	if err != nil {
		t.Fatalf("client: %v", err)
	}
	return c, func() []csRequest {
		mu.Lock()
		defer mu.Unlock()
		return append([]csRequest(nil), reqs...)
	}
}

func find(reqs []csRequest, method, path string) []csRequest {
	var out []csRequest
	for _, r := range reqs {
		if r.method == method && r.path == path {
			out = append(out, r)
		}
	}
	return out
}

var csTestInvocation = api.FunctionInvocation{FunctionName: "set-replicas", Arguments: []api.FunctionArgument{{Value: "3"}}}

// A commit in a ChangeSet opens it on the Units the invocation selects -- its data filter
// included -- by ID, and then writes with change_set_id.
func TestInvokeInChangeSetOpensItOnTheSelectedUnits(t *testing.T) {
	c, sent := changeSetServer(t, "")
	sel := Selector{Where: "Slug = 'web'", WhereData: "spec.replicas > 1"}
	_, err := InvokeFunction(context.Background(), c, csTestInvocation, sel,
		Change{Description: "scale web", ChangeSet: "platform/review"})
	if err != nil {
		t.Fatalf("InvokeFunction: %v", err)
	}
	reqs := sent()
	lists := find(reqs, http.MethodGet, "/unit")
	if len(lists) != 1 || !strings.Contains(lists[0].query["where"], "Slug = 'web'") ||
		lists[0].query["where_data"] != "spec.replicas > 1" || !strings.Contains(lists[0].query["where"], "ToolchainType") {
		t.Fatalf("the Units listed to open the ChangeSet on are not the invocation's: %+v", lists)
	}
	patches := find(reqs, http.MethodPatch, "/unit")
	if len(patches) != 1 || patches[0].query["change_set_id"] != csTestSet || patches[0].query["dry_run"] != "" ||
		!strings.Contains(patches[0].query["where"], csTestUnitID) {
		t.Fatalf("open patch = %+v, want one commit naming the listed Unit by ID, with change_set_id", patches)
	}
	invokes := find(reqs, http.MethodPost, "/function/invoke")
	if len(invokes) != 1 || invokes[0].query["change_set_id"] != csTestSet || invokes[0].query["dry_run"] != "" {
		t.Fatalf("invocation = %+v, want a commit with change_set_id", invokes)
	}
}

// A Unit another ChangeSet holds refuses the write before anything is sent: no patch opens the
// ChangeSet on some Units only, and no invocation writes outside it.
func TestInvokeInChangeSetRefusesAHeldUnit(t *testing.T) {
	c, sent := changeSetServer(t, csTestOther)
	_, err := InvokeFunction(context.Background(), c, csTestInvocation, Selector{Where: "Slug = 'web'"},
		Change{Description: "scale web", ChangeSet: "platform/review"})
	var held *ChangeSetHeldError
	if !errors.As(err, &held) || held.Held != 1 {
		t.Fatalf("err = %v, want a ChangeSetHeldError for the one Unit", err)
	}
	reqs := sent()
	if p := find(reqs, http.MethodPatch, "/unit"); len(p) != 0 {
		t.Errorf("a held Unit still sent the open patch: %+v", p)
	}
	if i := find(reqs, http.MethodPost, "/function/invoke"); len(i) != 0 {
		t.Errorf("a held Unit still sent the invocation: %+v", i)
	}
}

// A dry run in a ChangeSet writes nothing: the open and the invocation are both dry runs.
func TestInvokeInChangeSetDryRunOpensNothing(t *testing.T) {
	c, sent := changeSetServer(t, "")
	_, err := InvokeFunction(context.Background(), c, csTestInvocation, Selector{Where: "Slug = 'web'"},
		Change{ChangeSet: "platform/review"})
	if err != nil {
		t.Fatalf("InvokeFunction: %v", err)
	}
	reqs := sent()
	for _, p := range find(reqs, http.MethodPatch, "/unit") {
		if p.query["dry_run"] != "true" {
			t.Errorf("a dry run sent an open patch that is not a dry run: %+v", p)
		}
	}
	invokes := find(reqs, http.MethodPost, "/function/invoke")
	if len(invokes) != 1 || invokes[0].query["dry_run"] != "true" {
		t.Fatalf("invocation = %+v, want a dry run", invokes)
	}
}

// A write to Units named by ID opens the ChangeSet on exactly those, all or none.
func TestOpenChangeSetOnUnitsRefusesAHeldUnit(t *testing.T) {
	c, sent := changeSetServer(t, csTestOther)
	_, err := OpenChangeSetOnUnits(context.Background(), c, uuid.MustParse(csTestSet), []goclientnew.UUID{uuid.MustParse(csTestUnitID)}, OpenChangeSetOpts{})
	var held *ChangeSetHeldError
	if !errors.As(err, &held) {
		t.Fatalf("err = %v, want a ChangeSetHeldError", err)
	}
	if p := find(sent(), http.MethodPatch, "/unit"); len(p) != 0 {
		t.Errorf("a held Unit still sent the open patch: %+v", p)
	}
}
