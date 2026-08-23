// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package cubapi

import (
	"path/filepath"
	"testing"
)

// KeyPath decides whether a --private-key value names a file or an alias. The
// distinction is the whole reason aliases work, and getting it wrong is silent:
// a value treated as a path when it was meant as an alias fails with "no such
// file", and one treated as an alias when it was meant as a relative path reads
// somebody else's key.
func TestKeyPathResolution(t *testing.T) {
	configPath := writeConfig(t, twoContextConfig, nil)
	store, err := LoadConfig(configPath)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	keyDir := filepath.Join(filepath.Dir(configPath), "keys")

	if got := store.KeyDir(); got != keyDir {
		t.Fatalf("KeyDir = %q, want %q", got, keyDir)
	}

	tests := []struct {
		name string
		in   string
		want string
	}{
		{"bare alias gains .jwk and lands in the key dir", "ci", filepath.Join(keyDir, "ci.jwk")},
		{"alias keeps an extension it already has", "ci.jwk", filepath.Join(keyDir, "ci.jwk")},
		{"a non-jwk extension is still honoured", "ci.json", filepath.Join(keyDir, "ci.json")},
		{"absolute paths are used as-is", "/etc/confighub/ci.jwk", "/etc/confighub/ci.jwk"},
		{"an explicit relative path is a path, not an alias", "./ci", "./ci"},
		{"a nested relative path is a path", "keys/ci.jwk", "keys/ci.jwk"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := store.KeyPath(tc.in); got != tc.want {
				t.Errorf("KeyPath(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

// A "~" value expands against $HOME rather than being taken literally, matching
// TokenPath. A literal "~" directory is not what anyone typing it means.
func TestKeyPathExpandsHome(t *testing.T) {
	configPath := writeConfig(t, twoContextConfig, nil)
	store, err := LoadConfig(configPath)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}

	t.Setenv("HOME", "/home/tester")
	got := store.KeyPath("~/secrets/ci.jwk")
	if want := "/home/tester/secrets/ci.jwk"; got != want {
		t.Errorf("KeyPath = %q, want %q", got, want)
	}
}

// Keys and tokens are siblings under the config directory. They are separate
// directories because they have different lifetimes: a token is disposable and
// re-mintable, a private key is not.
func TestKeyDirIsSiblingOfTokenDir(t *testing.T) {
	configPath := writeConfig(t, twoContextConfig, nil)
	store, err := LoadConfig(configPath)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}

	if store.KeyDir() == store.TokenPath("x.json") {
		t.Fatal("key directory collides with the token directory")
	}
	if filepath.Dir(store.KeyDir()) != filepath.Dir(configPath) {
		t.Errorf("key dir %q is not beside the config file %q", store.KeyDir(), configPath)
	}
}
