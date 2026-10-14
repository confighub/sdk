// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"os"
	"reflect"
	"testing"
)

// TestSplitPluginArgs pins the rule for a plugin invocation: the root's
// persistent flags are cub's wherever they appear, everything else is the
// plugin's, and -- ends the scan.
func TestSplitPluginArgs(t *testing.T) {
	cases := []struct {
		name       string
		args       []string
		rootFlags  []string
		pluginArgs []string
	}{
		{
			name:       "flag before the plugin name",
			args:       []string{"--context", "prod", "demo", "up"},
			rootFlags:  []string{"--context", "prod"},
			pluginArgs: []string{"demo", "up"},
		},
		{
			name:       "flag after the plugin name",
			args:       []string{"demo", "up", "--context", "prod"},
			rootFlags:  []string{"--context", "prod"},
			pluginArgs: []string{"demo", "up"},
		},
		{
			name:       "equals spelling",
			args:       []string{"demo", "--context=prod", "up"},
			rootFlags:  []string{"--context=prod"},
			pluginArgs: []string{"demo", "up"},
		},
		{
			name:       "bool flag takes no value",
			args:       []string{"--debug", "demo", "--debug", "up"},
			rootFlags:  []string{"--debug", "--debug"},
			pluginArgs: []string{"demo", "up"},
		},
		{
			name:       "the plugin's own flags pass through",
			args:       []string{"demo", "up", "--scenario", "meridian", "-v", "--dry-run"},
			rootFlags:  nil,
			pluginArgs: []string{"demo", "up", "--scenario", "meridian", "-v", "--dry-run"},
		},
		{
			name:       "double dash ends the scan",
			args:       []string{"demo", "--context", "prod", "--", "--context", "other"},
			rootFlags:  []string{"--context", "prod"},
			pluginArgs: []string{"demo", "--", "--context", "other"},
		},
		{
			name:       "valued flag at the end keeps the bare flag for pflag to reject",
			args:       []string{"demo", "--context"},
			rootFlags:  []string{"--context"},
			pluginArgs: []string{"demo"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rootFlags, pluginArgs := splitPluginArgs(tc.args)
			if !reflect.DeepEqual(rootFlags, tc.rootFlags) {
				t.Errorf("root flags = %q, want %q", rootFlags, tc.rootFlags)
			}
			if !reflect.DeepEqual(pluginArgs, tc.pluginArgs) {
				t.Errorf("plugin args = %q, want %q", pluginArgs, tc.pluginArgs)
			}
		})
	}
}

// TestPreparePluginInvocationAppliesContextFlag is the regression test for
// --context being ignored on the plugin path. Cobra fails in Find for a command
// it does not know, before any flag is parsed, so until the plugin path parsed
// the root flags itself the override ran with an empty flag and the plugin got
// the persisted context regardless of what was asked for.
func TestPreparePluginInvocationAppliesContextFlag(t *testing.T) {
	for _, args := range [][]string{
		{"--context", "prod", "ctxprobe", "a"},
		{"ctxprobe", "a", "--context", "prod"},
		{"ctxprobe", "--context=prod", "a"},
	} {
		t.Run(args[0]+" "+args[1], func(t *testing.T) {
			cm := setupPluginInvocationTest(t)

			pluginArgs, err := preparePluginInvocation(args)
			if err != nil {
				t.Fatal(err)
			}
			if want := []string{"ctxprobe", "a"}; !reflect.DeepEqual(pluginArgs, want) {
				t.Errorf("plugin args = %q, want %q", pluginArgs, want)
			}
			if got := cm.ActiveContext().Name; got != "prod" {
				t.Errorf("active context = %q, want %q", got, "prod")
			}
			// What pluginEnv exports as CUB_CONTEXT.
			if got := os.Getenv("CUB_CONTEXT"); got != "prod" {
				t.Errorf("$CUB_CONTEXT = %q, want %q", got, "prod")
			}
			if activeContextOverrideSource != contextSourceFlag {
				t.Errorf("override source = %q, want %q", activeContextOverrideSource, contextSourceFlag)
			}
		})
	}
}

// TestPreparePluginInvocationExportsDebug: --debug reaches the plugin the way it
// reaches a child cub, as $CONFIGHUB_DEBUG.
func TestPreparePluginInvocationExportsDebug(t *testing.T) {
	setupPluginInvocationTest(t)
	t.Setenv("CONFIGHUB_DEBUG", "")

	if _, err := preparePluginInvocation([]string{"ctxprobe", "--debug"}); err != nil {
		t.Fatal(err)
	}
	if got := os.Getenv("CONFIGHUB_DEBUG"); got != "1" {
		t.Errorf("$CONFIGHUB_DEBUG = %q, want %q", got, "1")
	}
}

// TestPreparePluginInvocationRejectsMissingContext: a named-but-missing context
// is refused before the plugin runs, as it is for a built-in command.
func TestPreparePluginInvocationRejectsMissingContext(t *testing.T) {
	setupPluginInvocationTest(t)

	if _, err := preparePluginInvocation([]string{"ctxprobe", "--context", "nope"}); err == nil {
		t.Fatal("expected an error for a context that does not exist")
	}
	if _, err := preparePluginInvocation([]string{"ctxprobe", "--context"}); err == nil {
		t.Fatal("expected an error for --context with no value")
	}
}

// setupPluginInvocationTest extends setupContextOverrideTest with the flag
// state preparePluginInvocation writes through pflag, which a Parse does not
// reset and the next case must not inherit.
func setupPluginInvocationTest(t *testing.T) *ContextManager {
	t.Helper()
	cm := setupContextOverrideTest(t)
	prevDebug := debug
	t.Cleanup(func() { debug = prevDebug })
	debug = false
	return cm
}
