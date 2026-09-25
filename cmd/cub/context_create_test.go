// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import "testing"

// setupContextCreateTest installs a ContextManager backed by a temporary HOME and
// resets the flags contextCreateCmdRun reads.
func setupContextCreateTest(t *testing.T) *ContextManager {
	t.Helper()
	cm := setupLoginTargetTest(t)
	prevServer, prevOrg, prevUse, prevUIURL := createServer, createOrganization, createUse, createUIURL
	t.Cleanup(func() {
		createServer, createOrganization, createUse, createUIURL = prevServer, prevOrg, prevUse, prevUIURL
	})
	createServer, createOrganization, createUse, createUIURL = "", "", false, ""
	return cm
}

// TestContextCreateLeavesCurrentContextAlone covers the reason contexts exist:
// a context for another server can be set up while every shell on the machine
// keeps working in the one it is in. The current context is shared by all of
// them, so creating one must not move it.
func TestContextCreateLeavesCurrentContextAlone(t *testing.T) {
	cm := setupContextCreateTest(t)
	before := cm.CurrentContextName()
	if before == "" {
		t.Fatal("no current context to begin with")
	}

	createServer = "http://localhost:9091"
	if err := contextCreateCmdRun(nil, []string{"local-9091"}); err != nil {
		t.Fatal(err)
	}

	if got := cm.CurrentContextName(); got != before {
		t.Errorf("current context moved to %q; want %q", got, before)
	}
	ctx, err := cm.GetContext("local-9091")
	if err != nil {
		t.Fatalf("context was not created: %v", err)
	}
	if ctx.Coordinate.ServerURL != "http://localhost:9091" {
		t.Errorf("server URL is %q; want http://localhost:9091", ctx.Coordinate.ServerURL)
	}
}

// TestContextCreateUseSwitches covers --use, which is how the switch is asked
// for now that it does not happen on its own.
func TestContextCreateUseSwitches(t *testing.T) {
	cm := setupContextCreateTest(t)

	createServer = "http://localhost:9091"
	createUse = true
	if err := contextCreateCmdRun(nil, []string{"local-9091"}); err != nil {
		t.Fatal(err)
	}

	if got := cm.CurrentContextName(); got != "local-9091" {
		t.Errorf("current context is %q; want local-9091", got)
	}
}

// TestContextCreateUIURL covers --ui-url: a context whose web UI is served apart
// from the server records where, without a trailing slash, and a value that is
// not an http(s) URL is refused before anything is created.
func TestContextCreateUIURL(t *testing.T) {
	cm := setupContextCreateTest(t)

	createServer = "https://hub.example.com"
	createUIURL = "https://ui.example.com/"
	if err := contextCreateCmdRun(nil, []string{"hub-next"}); err != nil {
		t.Fatal(err)
	}
	ctx, err := cm.GetContext("hub-next")
	if err != nil {
		t.Fatal(err)
	}
	if ctx.Settings.UIURL != "https://ui.example.com" {
		t.Errorf("UI URL is %q; want https://ui.example.com", ctx.Settings.UIURL)
	}

	createUIURL = "ui.example.com"
	if err := contextCreateCmdRun(nil, []string{"bad"}); err == nil {
		t.Error("a UI URL without a scheme was accepted")
	}
	if _, err := cm.GetContext("bad"); err == nil {
		t.Error("the context was created despite the refused UI URL")
	}
}
