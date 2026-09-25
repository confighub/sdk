// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import "testing"

// Every browser link follows the context's UI URL when it has one, and the
// server URL otherwise; `context set --ui-url` changes it and an empty value
// goes back to the server.
func TestContextSetUIURL(t *testing.T) {
	cm := setupLoginTargetTest(t)
	prev := setUIURL
	t.Cleanup(func() { setUIURL = prev })

	ctx := addContext(t, cm, "hub", Coordinate{ServerURL: "https://hub.example.com"})
	if err := cm.SetCurrentContext("hub"); err != nil {
		t.Fatal(err)
	}
	if got := webUIServerURL(); got != "https://hub.example.com" {
		t.Errorf("with no UI URL, links go to %q; want the server URL", got)
	}

	setUIURL = "https://ui.example.com/"
	if err := contextSetCmdRun(nil, nil); err != nil {
		t.Fatal(err)
	}
	if got := webUIServerURL(); got != "https://ui.example.com" {
		t.Errorf("links go to %q; want https://ui.example.com", got)
	}

	setUIURL = ""
	if err := contextSetCmdRun(nil, []string{"hub"}); err != nil {
		t.Fatal(err)
	}
	if ctx.Settings.UIURL != "" || webUIServerURL() != "https://hub.example.com" {
		t.Errorf("clearing left UI URL %q, links to %q", ctx.Settings.UIURL, webUIServerURL())
	}

	setUIURL = "ftp://ui.example.com"
	if err := contextSetCmdRun(nil, nil); err == nil {
		t.Error("a non-http UI URL was accepted")
	}
}

// The sign-in link opens the context's UI with the ticket in the fragment when a
// UI URL is set, and is the server's own link otherwise, so a server that embeds
// its UI keeps working whichever version it runs.
func TestBrowserSignInLink(t *testing.T) {
	ticket := browserSessionResponse{URL: "https://hub.example.com/cli-signin#ticket=abc", Ticket: "abc"}

	ctx := &Context{Coordinate: Coordinate{ServerURL: "https://hub.example.com"}}
	if got, err := browserSignInLink(ctx, ticket); err != nil || got != ticket.URL {
		t.Errorf("without a UI URL: %q, %v; want the server's link", got, err)
	}

	ctx.Settings.UIURL = "https://ui.example.com"
	if got, err := browserSignInLink(ctx, ticket); err != nil || got != "https://ui.example.com/cli-signin#ticket=abc" {
		t.Errorf("with a UI URL: %q, %v", got, err)
	}

	if _, err := browserSignInLink(ctx, browserSessionResponse{URL: ticket.URL}); err == nil {
		t.Error("a response with no ticket was accepted when the link has to be built")
	}
}
