// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFunctionSpecFilePath(t *testing.T) {
	saved := contextManager
	contextManager = nil
	t.Cleanup(func() { contextManager = saved })

	t.Run("context manager wins", func(t *testing.T) {
		dir := t.TempDir()
		cm, err := NewContextManagerWithPath(filepath.Join(dir, "config.yaml"))
		if err != nil {
			t.Fatal(err)
		}
		contextManager = cm
		t.Cleanup(func() { contextManager = nil })
		if got, want := functionSpecFilePath(), filepath.Join(dir, "functions.json"); got != want {
			t.Errorf("functionSpecFilePath() = %q, want %q", got, want)
		}
	})

	// CUB_CONFIG is the config directory, so it needs neither to exist nor to
	// be distinguished from a file path.
	t.Run("CUB_CONFIG directory", func(t *testing.T) {
		dir := t.TempDir()
		t.Setenv("CUB_CONFIG", dir)
		if got, want := functionSpecFilePath(), filepath.Join(dir, "functions.json"); got != want {
			t.Errorf("functionSpecFilePath() = %q, want %q", got, want)
		}
	})

	t.Run("CUB_CONFIG directory that does not exist yet", func(t *testing.T) {
		t.Setenv("CUB_CONFIG", "/custom/store")
		if got, want := functionSpecFilePath(), "/custom/store/functions.json"; got != want {
			t.Errorf("functionSpecFilePath() = %q, want %q", got, want)
		}
	})

	t.Run("default under HOME", func(t *testing.T) {
		t.Setenv("CUB_CONFIG", "")
		t.Setenv("HOME", "/home/someone")
		if got, want := functionSpecFilePath(), "/home/someone/.confighub/functions.json"; got != want {
			t.Errorf("functionSpecFilePath() = %q, want %q", got, want)
		}
	})
}

func TestSaveFunctionsCreatesDirectory(t *testing.T) {
	saved := contextManager
	contextManager = nil
	t.Cleanup(func() { contextManager = saved })

	// Point the store at a directory that does not exist yet, as on a machine
	// that has never run cub. CUB_CONFIG is that directory, not a file in it.
	t.Setenv("CUB_CONFIG", filepath.Join(t.TempDir(), "store"))

	functions := functionsByEntity{builtinFunctionKey: functionsByToolchain{}}
	if err := saveFunctions(functions); err != nil {
		t.Fatalf("saveFunctions() = %v", err)
	}
	if _, err := os.Stat(functionSpecFilePath()); err != nil {
		t.Fatalf("stat cache file: %v", err)
	}
	loaded, err := loadFunctions()
	if err != nil {
		t.Fatalf("loadFunctions() = %v", err)
	}
	if _, ok := loaded[builtinFunctionKey]; !ok {
		t.Errorf("loaded cache missing %q entry", builtinFunctionKey)
	}
}
