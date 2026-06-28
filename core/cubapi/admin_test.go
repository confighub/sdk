// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package cubapi

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
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
		FunctionName:  "set-yq",
		Arguments: Arguments([]api.FunctionArgument{
			{ParameterName: "yq-expression", Value: "..."},
			{ParameterName: "param", Value: "role={{ .Params.role }}", Evaluator: api.EvaluatorTemplate},
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
	if len(sent.Arguments) != 2 || sent.Arguments[1].Evaluator == nil || *sent.Arguments[1].Evaluator != api.EvaluatorTemplate {
		t.Fatalf("arguments = %+v", sent.Arguments)
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
