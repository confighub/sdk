// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package cubapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRefreshSession(t *testing.T) {
	var gotPath, gotAuth string
	var gotBody RefreshRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		if gotBody.RefreshToken != "good" {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"message":"Invalid or expired refresh token"}`))
			return
		}
		_, _ = w.Write([]byte(`{"access_token":"new-access","refresh_token":"new-refresh","organization_id":"org-1"}`))
	}))
	defer srv.Close()

	// A context's server URL may carry the API root; the endpoint is beside it.
	session, err := RefreshSession(context.Background(), srv.Client(), srv.URL+"/api/", "good", "old-access", "org-1")
	if err != nil {
		t.Fatalf("RefreshSession: %v", err)
	}
	if gotPath != "/auth/refresh" {
		t.Errorf("path = %q", gotPath)
	}
	if gotAuth != "Bearer old-access" {
		t.Errorf("Authorization = %q", gotAuth)
	}
	if gotBody.OrganizationID != "org-1" {
		t.Errorf("organization_id = %q", gotBody.OrganizationID)
	}
	if session.AccessToken != "new-access" || session.RefreshToken != "new-refresh" || session.OrganizationID != "org-1" {
		t.Errorf("session = %+v", session)
	}

	// No access token is needed; none is sent.
	if _, err := RefreshSession(context.Background(), srv.Client(), srv.URL, "good", "", ""); err != nil {
		t.Fatalf("RefreshSession without an access token: %v", err)
	}
	if gotAuth != "" {
		t.Errorf("Authorization = %q, want none", gotAuth)
	}

	_, err = RefreshSession(context.Background(), srv.Client(), srv.URL, "bad", "old-access", "")
	var refreshErr *RefreshError
	if !errors.As(err, &refreshErr) {
		t.Fatalf("error = %v, want a RefreshError", err)
	}
	if refreshErr.StatusCode != http.StatusUnauthorized || refreshErr.Message != "Invalid or expired refresh token" {
		t.Errorf("RefreshError = %+v", refreshErr)
	}

	if _, err := RefreshSession(context.Background(), srv.Client(), srv.URL, "", "old-access", ""); err == nil {
		t.Error("expected an error with no refresh token")
	}
}
