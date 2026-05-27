// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

// Package plugin defines the on-disk contract between the cub CLI and its
// plugins: the cub-plugin.yaml manifest and the install/upgrade hook protocol.
//
// Plugin authors use HandleHook to make a single binary self-describing — when
// cub installs or upgrades the plugin it invokes the binary with the hook
// environment set, and HandleHook writes the manifest (and any other setup the
// author wants) into the plugin directory. The cub CLI reads the same manifest
// at startup to learn which commands the plugin contributes.
//
// This package intentionally has no dependency on cobra or any CLI framework so
// that it stays cheap to import.
package plugin

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// ManifestFileName is the name of the manifest file that lives beside a
// plugin's executable(s) inside the plugin directory.
const ManifestFileName = "cub-plugin.yaml"

// Hook protocol environment variables. cub sets these when it invokes a plugin
// binary as an install or upgrade hook (rather than as a normal command).
const (
	// EnvHook carries the hook phase: HookInstall or HookUpgrade. It is empty
	// during a normal command invocation.
	EnvHook = "CUB_PLUGIN_HOOK"
	// EnvDir is the plugin's own directory, which the hook should write into.
	EnvDir = "CUB_PLUGIN_DIR"
	// EnvPreviousVersion is the version of the plugin being replaced, set only
	// on the upgrade phase, so the hook can run version-aware migrations.
	EnvPreviousVersion = "CUB_PLUGIN_PREVIOUS_VERSION"
)

// Hook phase values for EnvHook.
const (
	HookInstall = "install"
	HookUpgrade = "upgrade"
)

// Manifest is the parsed form of cub-plugin.yaml. It declares the commands a
// plugin contributes to cub.
type Manifest struct {
	// Name is an optional human-friendly plugin name. It does not affect
	// routing; command names do.
	Name string `yaml:"name,omitempty"`
	// Version is the plugin's own version string. cub records it and passes it
	// back as CUB_PLUGIN_PREVIOUS_VERSION on the next upgrade.
	Version string `yaml:"version,omitempty"`
	// Entrypoint is the legacy single-command form: a directory plugin with no
	// Commands resolves to one command named after its directory whose
	// executable is Entrypoint. Retained for backward compatibility.
	Entrypoint string `yaml:"entrypoint,omitempty"`
	// Commands are the commands this plugin contributes.
	Commands []Command `yaml:"commands,omitempty"`
}

// Command is a single command a plugin contributes to cub. cub routes the
// top-level token; the command's deeper subcommand tree is owned and parsed by
// the plugin itself.
type Command struct {
	// Name is the cub command token, e.g. "vmcluster" for "cub vmcluster".
	Name string `yaml:"name"`
	// Aliases are alternate tokens that route to the same command.
	Aliases []string `yaml:"aliases,omitempty"`
	// Summary is a one-line description shown in help.
	Summary string `yaml:"summary,omitempty"`
	// Entrypoint is the executable within the plugin directory to run.
	Entrypoint string `yaml:"entrypoint"`
	// Args is prepended to the plugin's argv, letting one binary back several
	// commands (e.g. Args ["vm"] so "cub vm ..." runs "<binary> vm ...").
	Args []string `yaml:"args,omitempty"`
}

// Read loads and parses the manifest from a plugin directory. It returns
// (nil, nil) when no manifest file is present.
func Read(dir string) (*Manifest, error) {
	data, err := os.ReadFile(filepath.Join(dir, ManifestFileName))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var m Manifest
	if err := yaml.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("invalid %s: %w", ManifestFileName, err)
	}
	return &m, nil
}

// Write serializes a manifest to cub-plugin.yaml in dir.
func Write(dir string, m Manifest) error {
	data, err := yaml.Marshal(m)
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, ManifestFileName), data, 0644)
}

// Phase reports the current hook phase, or "" if cub is not invoking this
// process as an install/upgrade hook. Authors who want to branch on the phase
// (e.g. preserve config on upgrade) can call it directly.
func Phase() string {
	return os.Getenv(EnvHook)
}

// HandleHook handles the install/upgrade hook if cub is invoking one. When a
// hook is in progress it writes m as the plugin's manifest into the hook
// directory and returns handled=true; the caller should then exit without
// running its normal command logic. During a normal invocation it returns
// handled=false and the caller proceeds as usual.
//
// Any Command whose Entrypoint is empty defaults to the basename of the running
// executable, which is the common single-binary case.
//
// Typical use:
//
//	if handled, err := plugin.HandleHook(manifest); handled {
//		if err != nil { fmt.Fprintln(os.Stderr, err); os.Exit(1) }
//		return
//	}
//	// ... normal command execution ...
func HandleHook(m Manifest) (handled bool, err error) {
	if Phase() == "" {
		return false, nil
	}
	dir := os.Getenv(EnvDir)
	if dir == "" {
		return true, fmt.Errorf("%s is set but %s is empty", EnvHook, EnvDir)
	}
	self := filepath.Base(os.Args[0])
	for i := range m.Commands {
		if m.Commands[i].Entrypoint == "" {
			m.Commands[i].Entrypoint = self
		}
	}
	if err := Write(dir, m); err != nil {
		return true, fmt.Errorf("writing plugin manifest: %w", err)
	}
	return true, nil
}
