// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package cubapi

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	goclientnew "github.com/confighub/sdk/core/openapi/goclient-new"
	"github.com/google/uuid"
)

func diffServer(t *testing.T, body string, gotPath *string, gotQuery *url.Values, gotBody *[]byte) *Client {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*gotPath = r.URL.Path
		*gotQuery = r.URL.Query()
		*gotBody, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	c, err := NewClient(ClientOptions{ServerURL: srv.URL, Token: "t"})
	if err != nil {
		t.Fatalf("client: %v", err)
	}
	return c
}

func TestGetUnitDiff(t *testing.T) {
	var path string
	var query url.Values
	var body []byte
	c := diffServer(t, `{"Diff":{"Resources":[]}}`, &path, &query, &body)
	spaceID, unitID := uuid.New(), uuid.New()

	diff, err := GetUnitDiff(context.Background(), c, spaceID, unitID, "LastApplied", "", true)
	if err != nil {
		t.Fatalf("GetUnitDiff: %v", err)
	}
	if diff == nil {
		t.Fatal("diff = nil")
	}
	if !strings.Contains(path, spaceID.String()) || !strings.Contains(path, unitID.String()) {
		t.Errorf("path = %q, want the Space and Unit", path)
	}
	if query.Get("from") != "LastApplied" || query.Has("to") || query.Get("include") != "Context" {
		t.Errorf("query = %v, want from=LastApplied, no to, include=Context", query)
	}

	if _, err := GetUnitDiff(context.Background(), c, spaceID, unitID, "", "", false); err != nil {
		t.Fatalf("GetUnitDiff: %v", err)
	}
	if query.Has("include") || query.Has("from") {
		t.Errorf("query = %v, want neither include nor from", query)
	}
}

func TestDiffConfigurations(t *testing.T) {
	var path string
	var query url.Values
	var body []byte
	c := diffServer(t, `{"Diff":{"Resources":[]}}`, &path, &query, &body)
	unitID := uuid.New()

	_, err := DiffConfigurations(context.Background(), c, goclientnew.DiffRequest{
		From: UnitDiffSide(unitID, 7),
		To:   InlineDiffSide([]byte("kind: ConfigMap\n"), "Kubernetes/YAML"),
	}, false)
	if err != nil {
		t.Fatalf("DiffConfigurations: %v", err)
	}
	var sent goclientnew.DiffRequest
	if err := json.Unmarshal(body, &sent); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if sent.From == nil || sent.From.UnitID != unitID || sent.From.Revision != "7" {
		t.Errorf("From = %+v, want Unit %s at Revision 7", sent.From, unitID)
	}
	if sent.To == nil || sent.To.Data != "kind: ConfigMap\n" || sent.To.ToolchainType != "Kubernetes/YAML" {
		t.Errorf("To = %+v, want the inline configuration", sent.To)
	}
	if query.Has("include") {
		t.Errorf("query = %v, want no include", query)
	}
}

func TestSearchUnitDiffs(t *testing.T) {
	var path string
	var query url.Values
	var body []byte
	unitID := uuid.New()
	c := diffServer(t, `[{"UnitID":"`+unitID.String()+`","FromRevisionNum":3,"ToRevisionNum":4,"Diff":{"Resources":[]}}]`, &path, &query, &body)

	diffs, err := SearchUnitDiffs(context.Background(), c, "SpaceID = 'x'", "Before:ChangeOrder:y", "4", true)
	if err != nil {
		t.Fatalf("SearchUnitDiffs: %v", err)
	}
	if len(diffs) != 1 || diffs[0].UnitID != unitID || diffs[0].ToRevisionNum != 4 {
		t.Fatalf("diffs = %+v", diffs)
	}
	if query.Get("where") != "SpaceID = 'x'" || query.Get("from") != "Before:ChangeOrder:y" || query.Get("to") != "4" || query.Get("include") != "Context" {
		t.Errorf("query = %v", query)
	}
}
