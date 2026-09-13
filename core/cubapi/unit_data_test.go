// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package cubapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	goclientnew "github.com/confighub/sdk/core/openapi/goclient-new"
	"github.com/google/uuid"
)

func unitDataServer(t *testing.T, status int, contentType, body string) *Client {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/data") {
			t.Errorf("path = %q", r.URL.Path)
		}
		w.Header().Set("Content-Type", contentType)
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	c, err := NewClient(ClientOptions{ServerURL: srv.URL, Token: "t"})
	if err != nil {
		t.Fatalf("client: %v", err)
	}
	return c
}

// The data endpoint answers with the configuration itself, not a JSON envelope.
func TestUnitDataReturnsTheRawBody(t *testing.T) {
	c := unitDataServer(t, http.StatusOK, "text/plain", "configHub:\n  configName: installer\n")
	data, err := UnitData(context.Background(), c, goclientnew.UUID(uuid.New()), goclientnew.UUID(uuid.New()))
	if err != nil {
		t.Fatalf("UnitData: %v", err)
	}
	if data != "configHub:\n  configName: installer\n" {
		t.Fatalf("data = %q", data)
	}
}

func TestUnitDataReportsTheServersError(t *testing.T) {
	c := unitDataServer(t, http.StatusNotFound, "application/json", `{"Message":"unit not found"}`)
	_, err := UnitData(context.Background(), c, goclientnew.UUID(uuid.New()), goclientnew.UUID(uuid.New()))
	if err == nil || !IsNotFoundError(err) {
		t.Fatalf("err = %v", err)
	}
}
