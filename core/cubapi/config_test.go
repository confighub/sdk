// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package cubapi

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func writeConfig(t *testing.T, yaml string, tokens map[string]TokenData) string {
	t.Helper()
	dir := t.TempDir()
	configPath := filepath.Join(dir, ConfigFileName)
	if err := os.WriteFile(configPath, []byte(yaml), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	tokenDir := filepath.Join(dir, "tokens")
	if err := os.MkdirAll(tokenDir, 0o700); err != nil {
		t.Fatalf("mkdir tokens: %v", err)
	}
	for name, td := range tokens {
		data, _ := json.Marshal(td)
		if err := os.WriteFile(filepath.Join(tokenDir, name), data, 0o600); err != nil {
			t.Fatalf("write token %s: %v", name, err)
		}
	}
	return configPath
}

const twoContextConfig = `apiVersion: v1
kind: Config
currentContext: prod
contexts:
  - name: prod
    coordinate:
      serverURL: https://hub.confighub.com
      organizationID: org-123
      user: brian@confighub.com
    settings:
      defaultSpace: prod-space
    metadata:
      tokenFile: prod.json
  - name: local
    coordinate:
      serverURL: http://localhost:9090
      organizationID: org-local
      user: dev@confighub.com
    settings:
      defaultSpace: ""
    metadata:
      tokenFile: local.json
`

func TestLoadConfigAndActiveContext(t *testing.T) {
	path := writeConfig(t, twoContextConfig, map[string]TokenData{
		"prod.json":  {AccessToken: "prod-token"},
		"local.json": {AccessToken: "local-token", RefreshToken: "local-refresh"},
	})

	store, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if got := len(store.Contexts()); got != 2 {
		t.Fatalf("Contexts() len = %d, want 2", got)
	}
	if got := store.CurrentContextName(); got != "prod" {
		t.Fatalf("CurrentContextName() = %q, want prod", got)
	}
	active, err := store.ActiveContext()
	if err != nil {
		t.Fatalf("ActiveContext: %v", err)
	}
	if active.Name != "prod" || active.Coordinate.ServerURL != "https://hub.confighub.com" || active.Settings.DefaultSpace != "prod-space" {
		t.Fatalf("active = %+v", active)
	}
	td, err := store.TokenData(active)
	if err != nil {
		t.Fatalf("TokenData: %v", err)
	}
	if td.AccessToken != "prod-token" {
		t.Fatalf("access token = %q", td.AccessToken)
	}
}

func TestUseOverride(t *testing.T) {
	path := writeConfig(t, twoContextConfig, map[string]TokenData{
		"prod.json":  {AccessToken: "prod-token"},
		"local.json": {AccessToken: "local-token"},
	})
	store, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if err := store.Use("local"); err != nil {
		t.Fatalf("Use(local): %v", err)
	}
	active, _ := store.ActiveContext()
	if active.Name != "local" {
		t.Fatalf("override active = %q, want local", active.Name)
	}
	if store.CurrentContextName() != "prod" {
		t.Fatalf("CurrentContextName changed to %q", store.CurrentContextName())
	}
	if err := store.Use("nope"); err == nil {
		t.Fatal("Use(unknown) = nil, want error")
	}
}

func TestLoadConfigMissingFileIsEmptyStore(t *testing.T) {
	path := filepath.Join(t.TempDir(), "absent", ConfigFileName)
	store, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig(missing) = %v, want nil", err)
	}
	if len(store.Contexts()) != 0 {
		t.Fatalf("Contexts() len = %d, want 0", len(store.Contexts()))
	}
	if _, err := store.ActiveContext(); err == nil {
		t.Fatal("ActiveContext() on empty store = nil, want error")
	}
}

func TestLoadConfigRejectsBadInput(t *testing.T) {
	cases := map[string]string{
		"bad apiVersion": "apiVersion: v2\nkind: Config\ncontexts: []\n",
		"bad kind":       "apiVersion: v1\nkind: Nope\ncontexts: []\n",
		"missing currentContext": "apiVersion: v1\nkind: Config\ncurrentContext: ghost\ncontexts: []\n",
	}
	for name, yaml := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := LoadConfig(writeConfig(t, yaml, nil)); err == nil {
				t.Fatalf("LoadConfig(%s) = nil, want error", name)
			}
		})
	}
}

func TestDefaultConfigPath(t *testing.T) {
	t.Setenv("CUB_CONFIG", "/custom/cfg.yaml")
	if got, err := DefaultConfigPath(); err != nil || got != "/custom/cfg.yaml" {
		t.Fatalf("DefaultConfigPath() = %q, %v", got, err)
	}
	t.Setenv("CUB_CONFIG", "")
	t.Setenv("HOME", "/home/tester")
	want := filepath.Join("/home/tester", ConfigHubDir, ConfigFileName)
	if got, err := DefaultConfigPath(); err != nil || got != want {
		t.Fatalf("DefaultConfigPath() = %q, %v; want %q", got, err, want)
	}
}
