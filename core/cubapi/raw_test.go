// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package cubapi

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

// A raw request goes where the generated client's would, carrying the same credentials, and
// its response comes back whatever its status.
func TestDoSendsARawRequestWithTheClientsCredentials(t *testing.T) {
	var gotMethod, gotPath, gotQuery, gotAuth, gotType, gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath, gotQuery = r.Method, r.URL.Path, r.URL.RawQuery
		gotAuth, gotType = r.Header.Get("Authorization"), r.Header.Get("Content-Type")
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		w.WriteHeader(http.StatusMultiStatus)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	t.Cleanup(srv.Close)
	c, err := NewClient(ClientOptions{ServerURL: srv.URL, Token: "tok"})
	if err != nil {
		t.Fatal(err)
	}

	res, err := c.Do(context.Background(), "post", "/api/upload?dry_run=true",
		url.Values{"include": {"Mutations"}}, strings.NewReader(`{"Files":[]}`), "application/json")
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	defer res.Body.Close()
	body, _ := io.ReadAll(res.Body)

	if gotMethod != http.MethodPost || gotPath != "/api/upload" {
		t.Errorf("request = %s %s", gotMethod, gotPath)
	}
	if q, _ := url.ParseQuery(gotQuery); q.Get("dry_run") != "true" || q.Get("include") != "Mutations" {
		t.Errorf("query = %q", gotQuery)
	}
	if gotAuth != "Bearer tok" || gotType != "application/json" || gotBody != `{"Files":[]}` {
		t.Errorf("auth = %q, content type = %q, body = %q", gotAuth, gotType, gotBody)
	}
	if res.StatusCode != http.StatusMultiStatus || string(body) != `{"ok":true}` {
		t.Errorf("response = %d %s", res.StatusCode, body)
	}
}
