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
	"time"

	"github.com/google/uuid"

	"github.com/confighub/sdk/core/cubapi"
	goclientnew "github.com/confighub/sdk/core/openapi/goclient-new"
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

const ruleID = "44444444-4444-4444-4444-444444444444"

// enforcementServer answers what ExecuteEnforcementUpdate sends when a rule's Warn changes: the
// Trigger patch, the Units of its toolchain, and the evaluations. Of the Units, failing fail the
// rule and passing do not. It records each evaluation's request. An evaluation naming
// notEvaluated reports that Unit as failed, with a 207.
func enforcementServer(t *testing.T, failing, passing []string, notEvaluated string) (*cubapi.Client, *[]map[string]any) {
	t.Helper()
	var mu sync.Mutex
	var evaluations []map[string]any
	unit := func(id string, gates map[string]string, errors, warnings []string) string {
		encode := func(v any) []byte {
			b, err := json.Marshal(v)
			if err != nil {
				t.Fatalf("encode %v: %v", v, err)
			}
			return b
		}
		ids, e, w := encode(gates), encode(setOf(errors)), encode(setOf(warnings))
		return fmt.Sprintf(`{"Unit":{"UnitID":%q,"Slug":"u-%s","ValidationTriggerIDs":%s,"ValidationErrors":%s,"ValidationWarnings":%s}}`,
			id, id[len(id)-4:], ids, e, w)
	}
	var units []string
	for i, id := range failing {
		switch i % 3 {
		case 0: // warned, as a rule in audit leaves it
			units = append(units, unit(id, map[string]string{"rule": ruleID}, nil, []string{"rule"}))
		case 1: // blocked, as an enforced rule leaves it
			units = append(units, unit(id, map[string]string{"rule": ruleID}, []string{"rule"}, nil))
		default: // failed under the name the Trigger had before a rename, passed under the new one
			units = append(units, unit(id, map[string]string{"old-rule": ruleID, "rule": ruleID}, nil, []string{"old-rule"}))
		}
	}
	other := "55555555-5555-5555-5555-555555555555"
	for i, id := range passing {
		if i%2 == 0 {
			units = append(units, unit(id, map[string]string{"rule": ruleID}, nil, nil))
		} else { // fails another rule only
			units = append(units, unit(id, map[string]string{"rule": ruleID, "other": other}, []string{"other"}, nil))
		}
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		path := strings.TrimPrefix(r.URL.Path, "/api")
		switch {
		case r.Method == http.MethodPatch && path == "/space/"+testSpaceID+"/trigger/"+ruleID:
			fmt.Fprintf(w, `{"TriggerID":%q,"SpaceID":%q,"Slug":"rule"}`, ruleID, testSpaceID)
		case r.Method == http.MethodGet && path == "/unit":
			if where := r.URL.Query().Get("where"); where != "ToolchainType = 'Kubernetes/YAML'" {
				t.Errorf("units listed with where %q, want the rule's toolchain", where)
			}
			_, _ = w.Write([]byte("[" + strings.Join(units, ",") + "]"))
		case r.Method == http.MethodPost && path == "/function/invoke":
			if r.URL.Query().Has("dry_run") {
				t.Errorf("evaluation sent dry_run=%s; it has to record the verdicts", r.URL.Query().Get("dry_run"))
			}
			body, _ := io.ReadAll(r.Body)
			var req map[string]any
			_ = json.Unmarshal(body, &req)
			req["where"] = r.URL.Query().Get("where")
			evaluations = append(evaluations, req)
			if notEvaluated != "" && strings.Contains(req["where"].(string), notEvaluated) {
				w.WriteHeader(http.StatusMultiStatus)
				fmt.Fprintf(w, `[{"UnitID":%q,"SpaceSlug":"prod","UnitSlug":"web","Success":false,"Error":{"Code":500,"Message":"function failed"}}]`, notEvaluated)
				return
			}
			_, _ = w.Write([]byte(`[]`))
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
	return c, &evaluations
}

func setOf(keys []string) map[string]bool {
	m := map[string]bool{}
	for _, k := range keys {
		m[k] = true
	}
	return m
}

func unitIDs(prefix string, n int) []string {
	ids := make([]string, n)
	for i := range ids {
		ids[i] = fmt.Sprintf("%s-0000-0000-0000-%012d", prefix, i)
	}
	return ids
}

func enforceChange() []EnforcementChange {
	enforce := true
	c, _ := planEnforcementChange("rule", &goclientnew.Trigger{
		TriggerID: uuid.MustParse(ruleID), SpaceID: uuid.MustParse(testSpaceID),
		ToolchainType: "Kubernetes/YAML", Warn: true,
	}, EnforcementUpdate{Enforce: &enforce}, time.Now())
	return []EnforcementChange{c}
}

// Enforcing a rule runs it again on exactly the Units it fails -- under any name it had -- and
// on no others, recording the verdicts, in requests that each name at most 100 Units.
func TestEnforcingReevaluatesOnlyTheFailingUnits(t *testing.T) {
	failing := unitIDs("aaaaaaaa", 204)
	passing := unitIDs("bbbbbbbb", 2)
	c, evaluations := enforcementServer(t, failing, passing, "")
	changes := enforceChange()
	if err := ExecuteEnforcementUpdate(context.Background(), c, changes); err != nil {
		t.Fatalf("ExecuteEnforcementUpdate: %v", err)
	}
	if changes[0].Reevaluated != len(failing) {
		t.Errorf("Reevaluated = %d, want %d", changes[0].Reevaluated, len(failing))
	}
	if len(*evaluations) != 3 {
		t.Fatalf("%d evaluations, want 3 for %d Units", len(*evaluations), len(failing))
	}
	named := map[string]int{}
	for _, req := range *evaluations {
		if triggers, _ := req["Triggers"].([]any); len(triggers) != 1 || triggers[0] != ruleID {
			t.Errorf("evaluation ran Triggers %v, want only the rule", req["Triggers"])
		}
		if req["UpdateValidationResults"] != true {
			t.Error("evaluation does not record its verdicts")
		}
		if req["ToolchainType"] != "Kubernetes/YAML" {
			t.Errorf("evaluation toolchain %v", req["ToolchainType"])
		}
		ids := strings.Count(req["where"].(string), "'") / 2
		if ids > 100 {
			t.Errorf("one evaluation names %d Units", ids)
		}
		for _, id := range append(append([]string{}, failing...), passing...) {
			if strings.Contains(req["where"].(string), id) {
				named[id]++
			}
		}
	}
	for _, id := range failing {
		if named[id] != 1 {
			t.Errorf("failing Unit %s evaluated %d times, want once", id, named[id])
		}
	}
	for _, id := range passing {
		if named[id] != 0 {
			t.Errorf("Unit %s, which passes the rule, was evaluated", id)
		}
	}
}

// A Unit the evaluation could not run on makes the update fail, naming it, since its verdict is
// still the one from before the rule changed.
func TestEnforcingReportsAUnitNotEvaluated(t *testing.T) {
	failing := unitIDs("aaaaaaaa", 3)
	c, _ := enforcementServer(t, failing, nil, failing[1])
	err := ExecuteEnforcementUpdate(context.Background(), c, enforceChange())
	if err == nil || !strings.Contains(err.Error(), "prod/web: function failed") {
		t.Fatalf("err = %v, want it to name the Unit not evaluated", err)
	}
}
