// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import "testing"

// setupContextCreateTest installs a ContextManager backed by a temporary HOME and
// resets the flags contextCreateCmdRun reads.
func setupContextCreateTest(t *testing.T) *ContextManager {
	t.Helper()
	cm := setupLoginTargetTest(t)
	prevServer, prevOrg, prevSpace, prevUse := createServer, createOrganization, createSpace, createUse
	t.Cleanup(func() {
		createServer, createOrganization, createSpace, createUse = prevServer, prevOrg, prevSpace, prevUse
	})
	createServer, createOrganization, createSpace, createUse = "", "", "", false
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
