// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// No test downloads the published index; one that wants a downloaded index
// sets pluginIndexFetch itself.
func init() {
	pluginIndexFetch = func() (*pluginIndex, error) { return nil, errors.New("tests do not download the plugin index") }
}

// stubKnownPluginInstall replaces the download with one that writes a plugin
// providing the given commands, and records what it was asked to install.
func stubKnownPluginInstall(t *testing.T, pluginsDir string, commands ...string) *[]string {
	t.Helper()
	var calls []string
	orig := knownPluginInstall
	t.Cleanup(func() { knownPluginInstall = orig })
	knownPluginInstall = func(source, name string) error {
		calls = append(calls, source+" as "+name)
		dir := filepath.Join(pluginsDir, name)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
		manifest := "commands:\n"
		for _, c := range commands {
			manifest += "  - name: " + c + "\n    entrypoint: main\n"
		}
		createExecutableFile(t, filepath.Join(dir, "main"), "#!/bin/sh\n")
		return os.WriteFile(filepath.Join(dir, "cub-plugin.yaml"), []byte(manifest), 0o644)
	}
	return &calls
}

func setPluginIndex(t *testing.T, index string) {
	t.Helper()
	orig := pluginIndexData
	t.Cleanup(func() { pluginIndexData = orig })
	pluginIndexData = []byte(index)
}

func setPluginTerminal(t *testing.T, isTerminal bool, answer string) {
	t.Helper()
	origTerm, origIn := pluginStdinIsTerminal, pluginPromptIn
	t.Cleanup(func() { pluginStdinIsTerminal, pluginPromptIn = origTerm, origIn })
	pluginStdinIsTerminal = func() bool { return isTerminal }
	pluginPromptIn = io.Reader(strings.NewReader(answer))
}

const testPluginIndex = `plugins:
  - name: quiet
    source: example/cub-quiet
    commands: [quiet, q]
    prompt: false
    futureField: ignored
  - name: asking
    source: example/cub-asking
    commands: [asking]
  - name: newer
    source: example/cub-newer
    commands: [newer]
    minCubVersion: v9.0.0
    prompt: false
`

// The index this build ships parses, and no entry claims a built-in command or
// a command another entry has.
func TestEmbeddedPluginIndex(t *testing.T) {
	ix, err := embeddedPluginIndex()
	if err != nil {
		t.Fatal(err)
	}
	if len(ix.Plugins) == 0 {
		t.Fatal("the embedded plugin index has no plugins")
	}
	builtins := getBuiltinCommandNames()
	owner := map[string]string{}
	names := map[string]bool{}
	for _, e := range ix.Plugins {
		if e.Name == "" || e.Source == "" || len(e.Commands) == 0 {
			t.Errorf("entry %+v: name, source and commands are all required", e)
		}
		if names[e.Name] {
			t.Errorf("plugin name %q appears twice", e.Name)
		}
		names[e.Name] = true
		if _, err := parseGitHubShorthand(e.Source); err != nil {
			t.Errorf("plugin %q: source %q: %v", e.Name, e.Source, err)
		}
		for _, c := range e.Commands {
			if builtins[c] {
				t.Errorf("plugin %q claims %q, a built-in command", e.Name, c)
			}
			if prev, ok := owner[c]; ok {
				t.Errorf("command %q is claimed by both %q and %q", c, prev, e.Name)
			}
			owner[c] = e.Name
		}
	}
	if e := ix.byCommand("installer"); e == nil || e.Source != "confighub/installer" || e.asks() {
		t.Errorf("byCommand(installer) = %+v; want confighub/installer, installed without asking", e)
	}
}

func TestInstallKnownPlugin(t *testing.T) {
	tests := []struct {
		name       string
		token      string
		mode       string
		terminal   bool
		answer     string
		version    string
		wantKnown  bool
		wantCalls  int
		wantErrHas string
	}{
		{name: "not in the index", token: "nope", wantKnown: false},
		{name: "prompt false installs without a terminal", token: "quiet", wantKnown: true, wantCalls: 1},
		{name: "an alias resolves to the plugin", token: "q", wantKnown: true, wantCalls: 1},
		{name: "prompt defaults to asking; yes", token: "asking", terminal: true, answer: "y\n", wantKnown: true, wantCalls: 1},
		{name: "enter means yes", token: "asking", terminal: true, answer: "\n", wantKnown: true, wantCalls: 1},
		{name: "declined", token: "asking", terminal: true, answer: "n\n", wantKnown: true, wantErrHas: "not installed"},
		{name: "asking without a terminal", token: "asking", wantKnown: true, wantErrHas: "cub plugin install example/cub-asking"},
		{name: "auto overrides prompt", token: "asking", mode: "auto", wantKnown: true, wantCalls: 1},
		{name: "ask overrides prompt false", token: "quiet", mode: "ask", terminal: true, answer: "no\n", wantKnown: true, wantErrHas: "not installed"},
		{name: "never names the install command", token: "quiet", mode: "never", wantKnown: true, wantErrHas: "cub plugin install example/cub-quiet"},
		{name: "invalid mode", token: "quiet", mode: "sometimes", wantKnown: true, wantErrHas: "CUB_PLUGIN_AUTO_INSTALL"},
		{name: "cub too old", token: "newer", version: "0.11.0", wantKnown: true, wantErrHas: "needs cub v9.0.0"},
		{name: "a development build skips the version check", token: "newer", version: "dev", wantKnown: true, wantCalls: 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pluginsDir := setupPluginTest(t)
			setPluginIndex(t, testPluginIndex)
			setPluginTerminal(t, tt.terminal, tt.answer)
			t.Setenv(pluginAutoInstallEnv, tt.mode)
			if tt.version != "" {
				orig := Version
				t.Cleanup(func() { Version = orig })
				Version = tt.version
			}
			calls := stubKnownPluginInstall(t, pluginsDir, "quiet", "q", "asking", "newer")

			known, err := installKnownPlugin(tt.token)
			if known != tt.wantKnown {
				t.Errorf("known = %v, want %v", known, tt.wantKnown)
			}
			if len(*calls) != tt.wantCalls {
				t.Errorf("installs = %v, want %d", *calls, tt.wantCalls)
			}
			if tt.wantErrHas == "" {
				if err != nil {
					t.Fatalf("err = %v", err)
				}
			} else if err == nil || !strings.Contains(err.Error(), tt.wantErrHas) {
				t.Fatalf("err = %v, want it to contain %q", err, tt.wantErrHas)
			}
			if tt.wantCalls == 1 {
				if _, ok := resolvePluginCommand(tt.token); !ok {
					t.Errorf("%q does not resolve after the install", tt.token)
				}
			}
		})
	}
}

// An installed release from before the plugin had the command is not
// reinstalled over; the user is told to upgrade it.
func TestInstallKnownPluginAlreadyInstalledWithoutCommand(t *testing.T) {
	pluginsDir := setupPluginTest(t)
	setPluginIndex(t, testPluginIndex)
	setPluginTerminal(t, false, "")
	calls := stubKnownPluginInstall(t, pluginsDir, "quiet")
	if err := os.MkdirAll(filepath.Join(pluginsDir, "quiet"), 0o755); err != nil {
		t.Fatal(err)
	}
	known, err := installKnownPlugin("q")
	if !known || err == nil || !strings.Contains(err.Error(), "cub plugin upgrade quiet") {
		t.Errorf("known = %v, err = %v; want the upgrade command", known, err)
	}
	if len(*calls) != 0 {
		t.Errorf("installs = %v, want none", *calls)
	}
}

func TestInstallKnownPluginFailures(t *testing.T) {
	pluginsDir := setupPluginTest(t)
	setPluginIndex(t, testPluginIndex)
	setPluginTerminal(t, false, "")

	orig := knownPluginInstall
	t.Cleanup(func() { knownPluginInstall = orig })
	knownPluginInstall = func(source, name string) error { return errors.New("no network") }
	if known, err := installKnownPlugin("quiet"); !known || err == nil || !strings.Contains(err.Error(), "no network") {
		t.Errorf("known = %v, err = %v; want the install's error", known, err)
	}

	// The latest release does not have the command the index says it has.
	stubKnownPluginInstall(t, pluginsDir, "quiet")
	if known, err := installKnownPlugin("q"); !known || err == nil || !strings.Contains(err.Error(), "does not provide") {
		t.Errorf("known = %v, err = %v; want a missing-command error", known, err)
	}
}

func TestKnownPluginSource(t *testing.T) {
	setupPluginTest(t)
	setPluginIndex(t, testPluginIndex)
	tests := []struct {
		arg, source, name string
		ok                bool
	}{
		{arg: "quiet", source: "example/cub-quiet", name: "quiet", ok: true},
		{arg: "quiet@v1.2.0", source: "example/cub-quiet@v1.2.0", name: "quiet", ok: true},
		{arg: "q"},
		{arg: "unknown"},
		{arg: "example/cub-quiet"},
		{arg: "./quiet"},
		{arg: "https://example.com/quiet"},
	}
	for _, tt := range tests {
		source, name, ok := knownPluginSource(tt.arg)
		if source != tt.source || name != tt.name || ok != tt.ok {
			t.Errorf("knownPluginSource(%q) = %q, %q, %v; want %q, %q, %v", tt.arg, source, name, ok, tt.source, tt.name, tt.ok)
		}
	}
}

// The command the user typed runs after the install, and its stdout may be
// piped, so nothing the install prints may land there.
func TestInstallKnownPluginKeepsStdoutClean(t *testing.T) {
	setupPluginTest(t)
	setPluginIndex(t, testPluginIndex)
	setPluginTerminal(t, false, "")

	origInstall := knownPluginInstall
	t.Cleanup(func() { knownPluginInstall = origInstall })
	knownPluginInstall = func(source, name string) error {
		tprint("Downloading %s...", source)
		return errors.New("stop here")
	}

	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	origStdout := os.Stdout
	os.Stdout = w
	_, _ = installKnownPlugin("quiet")
	os.Stdout = origStdout
	w.Close()
	got, err := io.ReadAll(r)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Errorf("the install wrote to stdout: %q", got)
	}
}

func TestListAllPlugins(t *testing.T) {
	pluginsDir := setupPluginTest(t)
	setPluginIndex(t, testPluginIndex)
	install := func(slot, command, source, tag string) {
		t.Helper()
		dir := filepath.Join(pluginsDir, slot)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		createExecutableFile(t, filepath.Join(dir, "main"), "#!/bin/sh\n")
		manifest := "commands:\n  - name: " + command + "\n    entrypoint: main\n"
		if err := os.WriteFile(filepath.Join(dir, "cub-plugin.yaml"), []byte(manifest), 0o644); err != nil {
			t.Fatal(err)
		}
		if source != "" {
			if err := writeInstallMetadata(dir, source, tag); err != nil {
				t.Fatal(err)
			}
		}
	}
	install("quiet", "quiet", "example/cub-quiet", "v1.2.0")        // known, by name
	install("ask", "asking", "example/cub-asking@v0.3.0", "v0.3.0") // known, under another name
	install("mine", "mine", "", "")                                 // not in the index

	ix, err := loadPluginIndex()
	if err != nil {
		t.Fatal(err)
	}
	rows := listAllPlugins(discoverPlugins(), ix)
	var got []string
	for _, r := range rows {
		got = append(got, fmt.Sprintf("%s|%s|%s|%v|%s", r.Name, strings.Join(r.Commands, ","), r.Source, r.Installed, r.Version))
	}
	want := []string{
		"ask|asking|example/cub-asking@v0.3.0|true|v0.3.0",
		"mine|mine||true|",
		"newer|newer|example/cub-newer|false|",
		"quiet|quiet|example/cub-quiet|true|v1.2.0",
	}
	if !slices.Equal(got, want) {
		t.Errorf("rows:\n  %s\nwant:\n  %s", strings.Join(got, "\n  "), strings.Join(want, "\n  "))
	}
}
