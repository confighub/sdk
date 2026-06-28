// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package cubapi

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadOrInitCreatesDefault(t *testing.T) {
	t.Setenv("CUB_CONFIG", "")
	t.Setenv("HOME", t.TempDir())

	store, err := LoadOrInit("", "honey-paw")
	if err != nil {
		t.Fatalf("LoadOrInit: %v", err)
	}
	if names := store.GetAllContextNames(); len(names) != 1 || names[0] != "honey-paw" {
		t.Fatalf("contexts = %v, want [honey-paw]", names)
	}
	if store.CurrentContextName() != "honey-paw" {
		t.Fatalf("current = %q, want honey-paw", store.CurrentContextName())
	}
	active, err := store.ActiveContext()
	if err != nil {
		t.Fatalf("ActiveContext: %v", err)
	}
	if active.Coordinate.ServerURL != DefaultServerURL || active.Settings.DefaultSpace != "default" {
		t.Fatalf("defaults not applied: %+v", active)
	}

	// The config was persisted and reloads with the same context.
	reloaded, err := LoadConfig(store.ConfigPath())
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if reloaded.CurrentContextName() != "honey-paw" {
		t.Fatalf("reloaded current = %q", reloaded.CurrentContextName())
	}
}

func TestLoadOrInitLoadsExisting(t *testing.T) {
	path := writeConfig(t, twoContextConfig, nil)
	store, err := LoadOrInit(path, "should-not-be-created")
	if err != nil {
		t.Fatalf("LoadOrInit: %v", err)
	}
	if got := len(store.Contexts()); got != 2 {
		t.Fatalf("contexts = %d, want 2 (existing, not re-initialized)", got)
	}
	if store.CurrentContextName() != "prod" {
		t.Fatalf("current = %q, want prod", store.CurrentContextName())
	}
}

func TestCreateContextValidationAndDefaults(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	store := newStore(filepath.Join(t.TempDir(), "config.yaml"))

	if _, err := store.CreateContext("", "", "", ""); err == nil {
		t.Fatal("CreateContext(empty name) = nil, want error")
	}
	if _, err := store.CreateContext("bad name!", "", "", ""); err == nil {
		t.Fatal("CreateContext(invalid name) = nil, want error")
	}

	first, err := store.CreateContext("prod", "", "", "")
	if err != nil {
		t.Fatalf("CreateContext(prod): %v", err)
	}
	if first.Coordinate.ServerURL != DefaultServerURL || first.Settings.DefaultSpace != "default" {
		t.Fatalf("defaults not applied: %+v", first)
	}
	if store.CurrentContextName() != "prod" {
		t.Fatal("first context should become current")
	}

	if _, err := store.CreateContext("prod", "", "", ""); err == nil {
		t.Fatal("CreateContext(duplicate) = nil, want error")
	}

	second, err := store.CreateContext("staging", "http://localhost:9090", "org-1", "team")
	if err != nil {
		t.Fatalf("CreateContext(staging): %v", err)
	}
	if second.Coordinate.ServerURL != "http://localhost:9090" || second.Settings.DefaultSpace != "team" {
		t.Fatalf("explicit values not kept: %+v", second)
	}
	if store.CurrentContextName() != "prod" {
		t.Fatal("second context should not change current")
	}
}

func TestSetAndDeleteContext(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	store := newStore(filepath.Join(t.TempDir(), "config.yaml"))
	_, _ = store.CreateContext("a", "", "", "")
	_, _ = store.CreateContext("b", "", "", "")

	if err := store.SetCurrentContext("b"); err != nil {
		t.Fatalf("SetCurrentContext: %v", err)
	}
	if store.CurrentContextName() != "b" {
		t.Fatalf("current = %q, want b", store.CurrentContextName())
	}

	// Deleting the current context switches to the remaining one.
	if err := store.DeleteContext("b"); err != nil {
		t.Fatalf("DeleteContext(b): %v", err)
	}
	if store.CurrentContextName() != "a" {
		t.Fatalf("current after delete = %q, want a", store.CurrentContextName())
	}
	// Cannot delete the last context.
	if err := store.DeleteContext("a"); err == nil {
		t.Fatal("DeleteContext(last) = nil, want error")
	}
}

func TestRenameContextMovesToken(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	store := newStore(filepath.Join(os.Getenv("HOME"), ".confighub", "config.yaml"))
	ctx, err := store.CreateContext("old", "", "", "")
	if err != nil {
		t.Fatalf("CreateContext: %v", err)
	}
	if err := store.SaveTokenData(ctx, &TokenData{AccessToken: "tok"}); err != nil {
		t.Fatalf("SaveTokenData: %v", err)
	}

	if err := store.RenameContext("old", "new"); err != nil {
		t.Fatalf("RenameContext: %v", err)
	}
	if store.CurrentContextName() != "new" {
		t.Fatalf("current = %q, want new", store.CurrentContextName())
	}
	renamed, err := store.Context("new")
	if err != nil {
		t.Fatalf("Context(new): %v", err)
	}
	td, err := store.TokenData(renamed)
	if err != nil {
		t.Fatalf("TokenData after rename: %v", err)
	}
	if td.AccessToken != "tok" {
		t.Fatalf("token = %q, want tok", td.AccessToken)
	}
}

func TestSaveLoadDeleteTokenData(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	store := newStore(filepath.Join(os.Getenv("HOME"), ".confighub", "config.yaml"))
	ctx, _ := store.CreateContext("a", "", "", "")

	if err := store.SaveTokenData(ctx, &TokenData{AccessToken: "x", RefreshToken: "y"}); err != nil {
		t.Fatalf("SaveTokenData: %v", err)
	}
	td, err := store.TokenData(ctx)
	if err != nil {
		t.Fatalf("TokenData: %v", err)
	}
	if td.AccessToken != "x" || td.RefreshToken != "y" {
		t.Fatalf("token = %+v", td)
	}
	if err := store.DeleteTokenData(ctx); err != nil {
		t.Fatalf("DeleteTokenData: %v", err)
	}
	if _, err := store.TokenData(ctx); err == nil {
		t.Fatal("TokenData after delete = nil error, want error")
	}
	// Deleting an absent token file is not an error.
	if err := store.DeleteTokenData(ctx); err != nil {
		t.Fatalf("DeleteTokenData(absent): %v", err)
	}
}

func TestFindContextByCoordinate(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	store := newStore(filepath.Join(t.TempDir(), "config.yaml"))
	_, _ = store.CreateContext("a", "http://a", "org-a", "")
	_, _ = store.CreateContext("b", "http://b", "org-b", "")

	got, err := store.FindContextByCoordinate(Coordinate{ServerURL: "http://b", OrganizationID: "org-b"})
	if err != nil {
		t.Fatalf("FindContextByCoordinate: %v", err)
	}
	if got.Name != "b" {
		t.Fatalf("found %q, want b", got.Name)
	}
	if _, err := store.FindContextByCoordinate(Coordinate{ServerURL: "http://nope"}); err == nil {
		t.Fatal("FindContextByCoordinate(absent) = nil, want error")
	}
}

func TestSaveConfigRoundtrip(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	path := filepath.Join(os.Getenv("HOME"), ".confighub", "config.yaml")
	store := newStore(path)
	_, _ = store.CreateContext("a", "http://a", "org-a", "sp-a")
	_, _ = store.CreateContext("b", "", "", "")
	if err := store.SaveConfig(); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}

	reloaded, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if len(reloaded.Contexts()) != 2 || reloaded.CurrentContextName() != "a" {
		t.Fatalf("reloaded = %d contexts, current %q", len(reloaded.Contexts()), reloaded.CurrentContextName())
	}
	a, _ := reloaded.Context("a")
	if a.Coordinate.ServerURL != "http://a" || a.Settings.DefaultSpace != "sp-a" {
		t.Fatalf("context a not roundtripped: %+v", a)
	}
}
