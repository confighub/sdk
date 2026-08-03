// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"os"
	"path/filepath"
	"testing"
)

// setupContextOverrideTest installs a ContextManager backed by a temporary HOME
// holding "dev" (the persisted current context) and "prod", and resets the
// override globals afterwards. $CUB_CONTEXT is cleared so each case sets its own.
func setupContextOverrideTest(t *testing.T) *ContextManager {
	t.Helper()

	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("CUB_CONFIG", "")
	t.Setenv("CUB_CONTEXT", "")

	cm, err := NewContextManagerWithPath(filepath.Join(home, ".confighub", "config.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	addContext(t, cm, "dev", Coordinate{ServerURL: "https://dev.example.com"})
	addContext(t, cm, "prod", Coordinate{ServerURL: "https://prod.example.com"})
	if err := cm.SetCurrentContext("dev"); err != nil {
		t.Fatal(err)
	}

	prevManager, prevSource, prevFlag := contextManager, activeContextOverrideSource, globalContextFlag
	t.Cleanup(func() {
		contextManager, activeContextOverrideSource, globalContextFlag = prevManager, prevSource, prevFlag
	})
	contextManager = cm
	activeContextOverrideSource = ""
	globalContextFlag = ""
	return cm
}

// TestApplyContextOverrideExportsFlagToChildren is the regression test for
// #4920. Commands that delegate work by running cub again (runCub) spawn a fresh
// process that re-resolves the context from scratch, and --context lives only in
// the parent's memory. Unless it is exported, the child falls back to the
// persisted current context and operates somewhere else entirely — silently,
// when that context happens to be usable.
func TestApplyContextOverrideExportsFlagToChildren(t *testing.T) {
	cm := setupContextOverrideTest(t)
	globalContextFlag = "prod"

	if err := applyContextOverride(); err != nil {
		t.Fatal(err)
	}

	// What a child cub process would resolve.
	if got := os.Getenv("CUB_CONTEXT"); got != "prod" {
		t.Errorf("$CUB_CONTEXT for child processes = %q, want %q", got, "prod")
	}
	// The override is in effect here without disturbing what is persisted.
	if got := cm.ActiveContext().Name; got != "prod" {
		t.Errorf("active context = %q, want %q", got, "prod")
	}
	if got := cm.CurrentContextName(); got != "dev" {
		t.Errorf("persisted current context = %q, want it left at %q", got, "dev")
	}
}

// TestApplyContextOverrideFlagBeatsEnv covers --context winning over an
// inherited $CUB_CONTEXT: the exported value must be the one that actually took
// effect, or a child would disagree with its parent.
func TestApplyContextOverrideFlagBeatsEnv(t *testing.T) {
	setupContextOverrideTest(t)
	t.Setenv("CUB_CONTEXT", "dev")
	globalContextFlag = "prod"

	if err := applyContextOverride(); err != nil {
		t.Fatal(err)
	}

	if got := os.Getenv("CUB_CONTEXT"); got != "prod" {
		t.Errorf("$CUB_CONTEXT = %q, want the flag's %q to have replaced the inherited value", got, "prod")
	}
}

// TestApplyContextOverrideNoOverrideLeavesEnvAlone covers the plain case. With
// no override, parent and child both resolve the persisted current context and
// already agree, so nothing is exported.
func TestApplyContextOverrideNoOverrideLeavesEnvAlone(t *testing.T) {
	setupContextOverrideTest(t)

	if err := applyContextOverride(); err != nil {
		t.Fatal(err)
	}

	if got := os.Getenv("CUB_CONTEXT"); got != "" {
		t.Errorf("$CUB_CONTEXT = %q, want it left empty when there is no override", got)
	}
}

// TestApplyContextOverrideMissingContextDoesNotExport keeps a rejected override
// from reaching a child: naming a context that does not exist is a hard error,
// and the child must not then be pointed at it.
func TestApplyContextOverrideMissingContextDoesNotExport(t *testing.T) {
	setupContextOverrideTest(t)
	globalContextFlag = "nonexistent"

	if err := applyContextOverride(); err == nil {
		t.Fatal("applyContextOverride() = nil, want an error for an unknown context")
	}
	if got := os.Getenv("CUB_CONTEXT"); got != "" {
		t.Errorf("$CUB_CONTEXT = %q, want nothing exported after a rejected override", got)
	}
}
