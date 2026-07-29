// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"path/filepath"
	"testing"
)

// setupLoginTargetTest installs a ContextManager backed by a temporary HOME and
// resets the login-targeting globals after the test.
func setupLoginTargetTest(t *testing.T) *ContextManager {
	t.Helper()

	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("CUB_CONFIG", "")

	cm, err := NewContextManagerWithPath(filepath.Join(home, ".confighub", "config.yaml"))
	if err != nil {
		t.Fatal(err)
	}

	prevManager, prevSource, prevNew := contextManager, activeContextOverrideSource, newContext
	t.Cleanup(func() {
		contextManager, activeContextOverrideSource, newContext = prevManager, prevSource, prevNew
	})
	contextManager = cm
	activeContextOverrideSource = ""
	newContext = false
	return cm
}

// addContext creates a named context with the given coordinate.
func addContext(t *testing.T, cm *ContextManager, name string, coordinate Coordinate) *Context {
	t.Helper()
	ctx, err := cm.CreateContext(name, coordinate.ServerURL, coordinate.OrganizationID, "")
	if err != nil {
		t.Fatal(err)
	}
	ctx.Coordinate = coordinate
	return ctx
}

// TestLoginTargetContextHonorsOverride is the regression test for `cub auth
// login` under --context / $CUB_CONTEXT: the named context must receive the
// token even when the login coordinate differs from the one it has on record,
// and the persisted current context must not change.
func TestLoginTargetContextHonorsOverride(t *testing.T) {
	cm := setupLoginTargetTest(t)

	// "prod" is already authenticated, and against a different org than the one
	// the login will land in — the case that used to re-target another context.
	addContext(t, cm, "prod", Coordinate{ServerURL: "https://hub.example.com", OrganizationID: "org_old", User: "old@example.com"})
	addContext(t, cm, "dev", Coordinate{ServerURL: "https://hub.example.com", OrganizationID: "org_new", User: "new@example.com"})
	if err := cm.SetCurrentContext("dev"); err != nil {
		t.Fatal(err)
	}
	if err := cm.OverrideCurrentContext("prod"); err != nil {
		t.Fatal(err)
	}
	activeContextOverrideSource = "--context flag"

	coordinate := Coordinate{ServerURL: "https://hub.example.com", OrganizationID: "org_new", User: "new@example.com"}
	ctx, err := loginTargetContext(coordinate)
	if err != nil {
		t.Fatal(err)
	}
	if ctx.Name != "prod" {
		t.Errorf("login target = %q, want %q", ctx.Name, "prod")
	}
	if got := cm.CurrentContextName(); got != "dev" {
		t.Errorf("current context = %q, want it left at %q", got, "dev")
	}
}

// TestLoginTargetContextUnauthenticatedActive covers the plain first login: the
// active context has no user or org yet, so it is the target.
func TestLoginTargetContextUnauthenticatedActive(t *testing.T) {
	cm := setupLoginTargetTest(t)

	addContext(t, cm, "fresh", Coordinate{ServerURL: "https://hub.example.com"})
	if err := cm.SetCurrentContext("fresh"); err != nil {
		t.Fatal(err)
	}

	ctx, err := loginTargetContext(Coordinate{ServerURL: "https://hub.example.com", OrganizationID: "org_a", User: "a@example.com"})
	if err != nil {
		t.Fatal(err)
	}
	if ctx.Name != "fresh" {
		t.Errorf("login target = %q, want %q", ctx.Name, "fresh")
	}
	if got := cm.CurrentContextName(); got != "fresh" {
		t.Errorf("current context = %q, want %q", got, "fresh")
	}
}

// TestLoginTargetContextHandsOffByCoordinate covers the no-override behavior of
// logging in as a different user/org: the context matching the new coordinate
// takes over and becomes current.
func TestLoginTargetContextHandsOffByCoordinate(t *testing.T) {
	cm := setupLoginTargetTest(t)

	addContext(t, cm, "acme", Coordinate{ServerURL: "https://hub.example.com", OrganizationID: "org_acme", User: "a@example.com"})
	addContext(t, cm, "other", Coordinate{ServerURL: "https://hub.example.com", OrganizationID: "org_other", User: "b@example.com"})
	if err := cm.SetCurrentContext("acme"); err != nil {
		t.Fatal(err)
	}

	ctx, err := loginTargetContext(Coordinate{ServerURL: "https://hub.example.com", OrganizationID: "org_other", User: "b@example.com"})
	if err != nil {
		t.Fatal(err)
	}
	if ctx.Name != "other" {
		t.Errorf("login target = %q, want %q", ctx.Name, "other")
	}
	if got := cm.CurrentContextName(); got != "other" {
		t.Errorf("current context = %q, want %q", got, "other")
	}
}

// TestLoginTargetContextCreatesForNewCoordinate covers the no-override case
// where no context matches the coordinate logged into.
func TestLoginTargetContextCreatesForNewCoordinate(t *testing.T) {
	cm := setupLoginTargetTest(t)

	addContext(t, cm, "acme", Coordinate{ServerURL: "https://hub.example.com", OrganizationID: "org_acme", User: "a@example.com"})
	if err := cm.SetCurrentContext("acme"); err != nil {
		t.Fatal(err)
	}

	ctx, err := loginTargetContext(Coordinate{ServerURL: "https://hub.example.com", OrganizationID: "org_new", User: "b@example.com"})
	if err != nil {
		t.Fatal(err)
	}
	if ctx.Name == "acme" {
		t.Error("login target should be a new context, not the active one")
	}
	if got := cm.CurrentContextName(); got != ctx.Name {
		t.Errorf("current context = %q, want the new context %q", got, ctx.Name)
	}
}

// TestLoginTargetContextNewContextBeatsEnvOverride checks that the explicit
// --new-context flag outranks $CUB_CONTEXT rather than being silently ignored.
func TestLoginTargetContextNewContextBeatsEnvOverride(t *testing.T) {
	cm := setupLoginTargetTest(t)

	addContext(t, cm, "acme", Coordinate{ServerURL: "https://hub.example.com", OrganizationID: "org_acme", User: "a@example.com"})
	if err := cm.SetCurrentContext("acme"); err != nil {
		t.Fatal(err)
	}
	if err := cm.OverrideCurrentContext("acme"); err != nil {
		t.Fatal(err)
	}
	activeContextOverrideSource = "CUB_CONTEXT environment variable"
	newContext = true

	ctx, err := loginTargetContext(Coordinate{ServerURL: "https://hub.example.com", OrganizationID: "org_acme", User: "a@example.com"})
	if err != nil {
		t.Fatal(err)
	}
	if ctx.Name == "acme" {
		t.Error("--new-context should outrank $CUB_CONTEXT and create a context")
	}
}

// TestLoginTargetContextNewContextFlag covers --new-context, which must log into
// a freshly created context rather than the active one.
func TestLoginTargetContextNewContextFlag(t *testing.T) {
	cm := setupLoginTargetTest(t)

	addContext(t, cm, "acme", Coordinate{ServerURL: "https://hub.example.com", OrganizationID: "org_acme", User: "a@example.com"})
	if err := cm.SetCurrentContext("acme"); err != nil {
		t.Fatal(err)
	}
	newContext = true

	// Same coordinate as the active context: without --new-context this would
	// simply reuse "acme".
	ctx, err := loginTargetContext(Coordinate{ServerURL: "https://hub.example.com", OrganizationID: "org_acme", User: "a@example.com"})
	if err != nil {
		t.Fatal(err)
	}
	if ctx.Name == "acme" {
		t.Error("--new-context should create a context, not reuse the active one")
	}
	if got := cm.CurrentContextName(); got != ctx.Name {
		t.Errorf("current context = %q, want the new context %q", got, ctx.Name)
	}
}
