// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package guardrails

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/confighub/sdk/core/cubapi"
)

const (
	testSpaceID = "11111111-1111-1111-1111-111111111111"
	promotedID  = "22222222-2222-2222-2222-222222222222"
	warnedID    = "33333333-3333-3333-3333-333333333333"
)

// fakeServer answers the reads and writes Execute makes for a plan that wires
// nothing, and records what each Trigger write sent, by slug.
//
// The policy Space exists. Of the Triggers, promoted exists with Warn false,
// warned exists with Warn true, new does not exist, and reading broken fails.
func fakeServer(t *testing.T) (*cubapi.Client, map[string]map[string]any) {
	t.Helper()
	var mu sync.Mutex
	sent := map[string]map[string]any{}
	existing := map[string]string{
		"promoted": fmt.Sprintf(`{"TriggerID":%q,"SpaceID":%q,"Slug":"promoted","Warn":false}`, promotedID, testSpaceID),
		"warned":   fmt.Sprintf(`{"TriggerID":%q,"SpaceID":%q,"Slug":"warned","Warn":true}`, warnedID, testSpaceID),
	}
	record := func(r *http.Request) map[string]any {
		body, _ := io.ReadAll(r.Body)
		var m map[string]any
		_ = json.Unmarshal(body, &m)
		return m
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		where := r.URL.Query().Get("where")
		path := strings.TrimPrefix(r.URL.Path, "/api")
		switch {
		case r.Method == http.MethodGet && path == "/space":
			fmt.Fprintf(w, `[{"Space":{"SpaceID":%q,"Slug":"common"}}]`, testSpaceID)
		case r.Method == http.MethodPatch && path == "/space/"+testSpaceID:
			fmt.Fprintf(w, `{"SpaceID":%q,"Slug":"common"}`, testSpaceID)
		case r.Method == http.MethodGet && path == "/trigger":
			for slug, trigger := range existing {
				if strings.Contains(where, "'"+slug+"'") {
					fmt.Fprintf(w, `[{"Trigger":%s}]`, trigger)
					return
				}
			}
			if strings.Contains(where, "'broken'") {
				w.WriteHeader(http.StatusInternalServerError)
				_, _ = w.Write([]byte(`{"Code":500,"Message":"database unavailable"}`))
				return
			}
			_, _ = w.Write([]byte(`[]`))
		case r.Method == http.MethodPatch && path == "/trigger":
			m := record(r)
			if r.URL.Query().Get("with_backing_units") != "true" {
				t.Errorf("patch of %s asks for no backing Unit", where)
			}
			for slug, id := range map[string]string{"promoted": promotedID, "warned": warnedID} {
				if where == "TriggerID = '"+id+"'" {
					sent[slug] = m
					fmt.Fprintf(w, `[{"Trigger":%s}]`, existing[slug])
					return
				}
			}
			t.Errorf("patch of an unknown trigger: %s", where)
		case r.Method == http.MethodPost && path == "/space/"+testSpaceID+"/trigger":
			m := record(r)
			slug, _ := m["Slug"].(string)
			sent[slug] = m
			fmt.Fprintf(w, `{"SpaceID":%q,"Slug":%q}`, testSpaceID, slug)
		default:
			t.Errorf("unexpected request %s %s", r.Method, path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	c, err := cubapi.NewClient(cubapi.ClientOptions{ServerURL: srv.URL, Token: "t"})
	if err != nil {
		t.Fatalf("client: %v", err)
	}
	return c, sent
}

func packOf(slugs ...string) Pack {
	p := Pack{Group: "test"}
	for _, s := range slugs {
		p.Rules = append(p.Rules, Rule{Slug: s, Expression: "true"})
	}
	return p
}

// Re-installing updates a rule's Trigger but leaves one promoted with --unwarn
// blocking; a new rule installs as a warning.
func TestExecuteKeepsAPromotedRuleBlocking(t *testing.T) {
	c, sent := fakeServer(t)
	if err := packOf("promoted", "warned", "new").Execute(context.Background(), c, "common", Plan{}, nil); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	for slug, want := range map[string]bool{"promoted": false, "warned": true, "new": true} {
		body, ok := sent[slug]
		if !ok {
			t.Errorf("%s: not written", slug)
			continue
		}
		if got, _ := body["Warn"].(bool); got != want {
			t.Errorf("%s: sent Warn %v, want %v", slug, body["Warn"], want)
		}
	}
}

// A Trigger that cannot be read is not assumed new: writing it with Warn true
// would demote it if it had been promoted.
func TestExecuteStopsWhenATriggerCannotBeRead(t *testing.T) {
	c, sent := fakeServer(t)
	err := packOf("broken").Execute(context.Background(), c, "common", Plan{}, nil)
	if err == nil || !strings.Contains(err.Error(), "read trigger broken") {
		t.Fatalf("err = %v, want it to name the trigger it could not read", err)
	}
	if _, ok := sent["broken"]; ok {
		t.Errorf("broken was written after its read failed: %v", sent["broken"])
	}
}
