// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// The sign-in link is the one the server built; a response without it is an
// error rather than a guess.
func TestBrowserSignInLink(t *testing.T) {
	ticket := browserSessionResponse{URL: "https://hub.example.com/cli-signin#ticket=abc", Ticket: "abc"}
	if got, err := browserSignInLink(ticket); err != nil || got != ticket.URL {
		t.Errorf("%q, %v; want the server's link", got, err)
	}
	if _, err := browserSignInLink(browserSessionResponse{Ticket: "abc"}); err == nil {
		t.Error("a response with no link was accepted")
	}
}

// Browser links go to the UI URL the server advertised, and to the server URL
// when it advertised none.
func TestContextUIURL(t *testing.T) {
	ctx := &Context{Coordinate: Coordinate{ServerURL: "http://localhost:9090"}}
	if got := contextUIURL(ctx); got != "http://localhost:9090" {
		t.Errorf("with nothing known, links go to %q; want the server URL", got)
	}
	ctx.Settings.UIURL = "http://localhost:5173"
	if got := contextUIURL(ctx); got != "http://localhost:5173" {
		t.Errorf("links go to %q; want what the server advertised", got)
	}
}

// uiTestServer is a server that advertises uiURL in /api/info and answers
// /config.json with configStatus.
func uiTestServer(t *testing.T, uiURL string, configStatus int) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/api/info", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"Version": "v0.0-test", "UIURL": uiURL})
	})
	mux.HandleFunc("/config.json", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(configStatus)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

// Only a server that answers /config.json with 404 is taken to serve no UI; one
// that cannot be reached is given the benefit of the doubt.
func TestServesWebUI(t *testing.T) {
	if !servesWebUI(uiTestServer(t, "", http.StatusOK).URL) {
		t.Error("a host that answers /config.json was taken to serve no UI")
	}
	if servesWebUI(uiTestServer(t, "", http.StatusNotFound).URL) {
		t.Error("a host that answers /config.json with 404 was taken to serve a UI")
	}
	if !servesWebUI("http://127.0.0.1:1") {
		t.Error("an unreachable host was taken to serve no UI")
	}
}

// A login records where the server says its UI is, and forgets it when the
// server stops saying so.
func TestNoteWebUILocationRecordsWhatTheServerAdvertises(t *testing.T) {
	cm := setupLoginTargetTest(t)

	srv := uiTestServer(t, "http://localhost:5173/", http.StatusNotFound)
	ctx := addContext(t, cm, "local", Coordinate{ServerURL: srv.URL})
	if err := cm.SetCurrentContext("local"); err != nil {
		t.Fatal(err)
	}
	noteWebUILocation(true)
	if ctx.Settings.UIURL != "http://localhost:5173" || webUIServerURL() != "http://localhost:5173" {
		t.Errorf("recorded %q, links to %q; want http://localhost:5173", ctx.Settings.UIURL, webUIServerURL())
	}

	silent := uiTestServer(t, "", http.StatusNotFound)
	ctx.Coordinate.ServerURL = silent.URL
	noteWebUILocation(true)
	if ctx.Settings.UIURL != "" || webUIServerURL() != silent.URL {
		t.Errorf("a server that advertises nothing left %q, links to %q", ctx.Settings.UIURL, webUIServerURL())
	}
}
