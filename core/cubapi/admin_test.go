// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package cubapi

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	api "github.com/confighub/sdk/core/function/api"
	goclientnew "github.com/confighub/sdk/core/openapi/goclient-new"
	"github.com/google/uuid"
)

type adminRec struct {
	method string
	path   string
	query  string
	body   []byte
}

func adminServer(t *testing.T, rec *adminRec, resp string) *Client {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec.method = r.Method
		rec.path = r.URL.Path
		rec.query = r.URL.RawQuery
		rec.body, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(resp))
	}))
	t.Cleanup(srv.Close)
	c, err := NewClient(ClientOptions{ServerURL: srv.URL, Token: "t"})
	if err != nil {
		t.Fatalf("client: %v", err)
	}
	return c
}

func TestEnsureSpaceSendsAllowExists(t *testing.T) {
	var rec adminRec
	c := adminServer(t, &rec, `{"SpaceID":"11111111-1111-1111-1111-111111111111","Slug":"rbac-edits"}`)
	sp, err := EnsureSpace(context.Background(), c, goclientnew.Space{Slug: "rbac-edits", Labels: map[string]string{"app": "rbac-manager"}})
	if err != nil {
		t.Fatalf("EnsureSpace: %v", err)
	}
	if sp.Slug != "rbac-edits" {
		t.Fatalf("slug = %q", sp.Slug)
	}
	if !strings.Contains(rec.query, "allow_exists=true") {
		t.Fatalf("query = %q", rec.query)
	}
	// A Space's backing Unit would live in another Space, which the description does not name.
	if strings.Contains(rec.query, "with_backing_units") {
		t.Fatalf("query = %q, want no backing Unit for a Space", rec.query)
	}
	var sent goclientnew.Space
	_ = json.Unmarshal(rec.body, &sent)
	if sent.Slug != "rbac-edits" || sent.Labels["app"] != "rbac-manager" {
		t.Fatalf("sent = %+v", sent)
	}
}

func TestEnsureTrigger(t *testing.T) {
	var rec adminRec
	c := adminServer(t, &rec, `{"Slug":"no-rbac-wildcards"}`)
	spaceID := goclientnew.UUID(uuid.MustParse("22222222-2222-2222-2222-222222222222"))
	_, err := EnsureTrigger(context.Background(), c, goclientnew.Trigger{
		SpaceID:       spaceID,
		Slug:          "no-rbac-wildcards",
		Event:         "Mutation",
		ToolchainType: "Kubernetes/YAML",
		FunctionName:  "vet-celexpr",
		Arguments:     Arguments([]api.FunctionArgument{{Value: "expr"}}),
		Warn:          true,
	})
	if err != nil {
		t.Fatalf("EnsureTrigger: %v", err)
	}
	if !strings.Contains(rec.path, spaceID.String()) {
		t.Fatalf("path = %q, want space id", rec.path)
	}
	if !strings.Contains(rec.query, "with_backing_units=true") {
		t.Fatalf("query = %q, want the create to ask for a backing Unit", rec.query)
	}
	var sent goclientnew.Trigger
	_ = json.Unmarshal(rec.body, &sent)
	if !sent.Warn || sent.FunctionName != "vet-celexpr" || sent.Event != "Mutation" {
		t.Fatalf("sent = %+v", sent)
	}
}

func TestEnsureInvocationCarriesParametersAndTemplatedArgs(t *testing.T) {
	var rec adminRec
	c := adminServer(t, &rec, `{"Slug":"rbac-add-verb"}`)
	spaceID := goclientnew.UUID(uuid.MustParse("33333333-3333-3333-3333-333333333333"))
	_, err := EnsureInvocation(context.Background(), c, goclientnew.Invocation{
		SpaceID:       spaceID,
		Slug:          "rbac-add-verb",
		ToolchainType: "Kubernetes/YAML",
		FunctionInvocations: FunctionInvocations(api.FunctionInvocation{
			FunctionName: "set-yq",
			Arguments: []api.FunctionArgument{
				{ParameterName: "yq-expression", Value: "..."},
				{ParameterName: "param", Value: "role={{ .Params.role }}", Evaluator: api.EvaluatorTemplate},
			},
		}),
		Parameters: []goclientnew.FunctionParameter{{ParameterName: "role", DataType: "string", Required: true}},
	})
	if err != nil {
		t.Fatalf("EnsureInvocation: %v", err)
	}
	var sent goclientnew.Invocation
	_ = json.Unmarshal(rec.body, &sent)
	if len(sent.Parameters) != 1 || sent.Parameters[0].ParameterName != "role" {
		t.Fatalf("parameters = %+v", sent.Parameters)
	}
	if sent.FunctionInvocations == nil || len(*sent.FunctionInvocations) != 1 {
		t.Fatalf("function invocations = %+v", sent.FunctionInvocations)
	}
	args := (*sent.FunctionInvocations)[0].Arguments
	if len(args) != 2 || args[1].Evaluator == nil || *args[1].Evaluator != api.EvaluatorTemplate {
		t.Fatalf("arguments = %+v", args)
	}
}

// existingTriggerServer answers the lookup of one existing Trigger and the bulk patch of it with
// bulkResponse, recording the patch.
func existingTriggerServer(t *testing.T, rec *adminRec, triggerID string, bulkResponse string) *Client {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		path := strings.TrimPrefix(r.URL.Path, "/api")
		switch {
		case r.Method == http.MethodGet && path == "/trigger":
			fmt.Fprintf(w, `[{"Trigger":{"TriggerID":%q,"Slug":"no-rbac-wildcards"}}]`, triggerID)
		case r.Method == http.MethodPatch && path == "/trigger":
			rec.method = r.Method
			rec.path = path
			rec.query = r.URL.RawQuery
			rec.body, _ = io.ReadAll(r.Body)
			_, _ = w.Write([]byte(bulkResponse))
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
	return c
}

// An existing entity is patched through the bulk patch of it alone, which is the patch that can
// give it a backing Unit when it was created without one.
func TestEnsureTriggerUpdatesWithBackingUnit(t *testing.T) {
	var rec adminRec
	triggerID := "66666666-6666-6666-6666-666666666666"
	c := existingTriggerServer(t, &rec, triggerID,
		fmt.Sprintf(`[{"Trigger":{"TriggerID":%q,"Slug":"no-rbac-wildcards","Warn":true}}]`, triggerID))
	got, err := EnsureTrigger(context.Background(), c, goclientnew.Trigger{
		SpaceID: goclientnew.UUID(uuid.MustParse("22222222-2222-2222-2222-222222222222")),
		Slug:    "no-rbac-wildcards",
		Warn:    true,
	})
	if err != nil {
		t.Fatalf("EnsureTrigger: %v", err)
	}
	if got.TriggerID.String() != triggerID || !got.Warn {
		t.Fatalf("returned %+v", got)
	}
	query, err := url.ParseQuery(rec.query)
	if err != nil {
		t.Fatalf("query %q: %v", rec.query, err)
	}
	if want := "TriggerID = '" + triggerID + "'"; query.Get("where") != want {
		t.Fatalf("where = %q, want %q", query.Get("where"), want)
	}
	if query.Get("with_backing_units") != "true" {
		t.Fatalf("query = %q, want with_backing_units=true", rec.query)
	}
	if body := patchMap(t, rec.body); body["Warn"] != true {
		t.Fatalf("patch = %v", body)
	}
}

// The error a bulk patch reports for the entity is the Ensure's error, not a nil entity.
func TestEnsureTriggerReportsTheUpdatesError(t *testing.T) {
	var rec adminRec
	c := existingTriggerServer(t, &rec, "66666666-6666-6666-6666-666666666666",
		`[{"Error":{"Message":"cannot create the backing Unit","Details":["no Create permission"]}}]`)
	_, err := EnsureTrigger(context.Background(), c, goclientnew.Trigger{
		SpaceID: goclientnew.UUID(uuid.MustParse("22222222-2222-2222-2222-222222222222")),
		Slug:    "no-rbac-wildcards",
	})
	if err == nil || !strings.Contains(err.Error(), "cannot create the backing Unit: no Create permission") {
		t.Fatalf("err = %v", err)
	}
}

func TestSetSpaceTriggerFilter(t *testing.T) {
	var rec adminRec
	c := adminServer(t, &rec, `{"SpaceID":"44444444-4444-4444-4444-444444444444","Slug":"prod"}`)
	space := &goclientnew.Space{
		SpaceID:      goclientnew.UUID(uuid.MustParse("44444444-4444-4444-4444-444444444444")),
		Slug:         "prod",
		WhereTrigger: "old-expr",
	}
	filterID := goclientnew.UUID(uuid.MustParse("55555555-5555-5555-5555-555555555555"))
	if err := SetSpaceTriggerFilter(context.Background(), c, space, filterID); err != nil {
		t.Fatalf("SetSpaceTriggerFilter: %v", err)
	}
	var sent goclientnew.Space
	_ = json.Unmarshal(rec.body, &sent)
	if sent.WhereTrigger != "" {
		t.Fatalf("WhereTrigger not cleared: %q", sent.WhereTrigger)
	}
	if sent.TriggerFilterID == nil || *sent.TriggerFilterID != filterID {
		t.Fatalf("TriggerFilterID = %v", sent.TriggerFilterID)
	}
}
