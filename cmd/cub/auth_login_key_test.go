// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	"testing"
)

// newKeyTestContextManager points the CLI's context manager at a throwaway
// config directory, so key lookups resolve inside the test rather than against
// the developer's real ~/.confighub.
func newKeyTestContextManager(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.yaml")

	cm, err := NewContextManagerWithPath(configPath)
	if err != nil {
		t.Fatalf("NewContextManagerWithPath: %v", err)
	}
	saved := contextManager
	contextManager = cm
	t.Cleanup(func() { contextManager = saved })

	return filepath.Join(dir, "keys")
}

func TestLoadPrivateKeyFromAlias(t *testing.T) {
	keyDir := newKeyTestContextManager(t)
	if err := os.WriteFile(filepath.Join(keyDir, "ci.jwk"), []byte(`{"kty":"OKP"}`), 0o600); err != nil {
		t.Fatalf("write key: %v", err)
	}

	data, source, err := loadPrivateKey("ci")
	if err != nil {
		t.Fatalf("loadPrivateKey: %v", err)
	}
	if string(data) != `{"kty":"OKP"}` {
		t.Errorf("data = %q", data)
	}
	if want := filepath.Join(keyDir, "ci.jwk"); source != want {
		t.Errorf("source = %q, want %q", source, want)
	}
}

// The environment variable may hold the key itself, not only a path to one: a
// CI system that can only set variables should not have to invent a file.
func TestLoadPrivateKeyInlineFromEnv(t *testing.T) {
	newKeyTestContextManager(t)
	t.Setenv(privateKeyEnvVar, `{"kty":"OKP","crv":"Ed25519"}`)

	data, source, err := loadPrivateKey(privateKeyFromEnv)
	if err != nil {
		t.Fatalf("loadPrivateKey: %v", err)
	}
	if string(data) != `{"kty":"OKP","crv":"Ed25519"}` {
		t.Errorf("data = %q", data)
	}
	if source != "$"+privateKeyEnvVar {
		t.Errorf("source = %q", source)
	}
}

// It may equally hold a path, for a CI system that mounts secrets as files.
func TestLoadPrivateKeyPathFromEnv(t *testing.T) {
	newKeyTestContextManager(t)
	path := filepath.Join(t.TempDir(), "mounted.jwk")
	if err := os.WriteFile(path, []byte(`{"kty":"OKP"}`), 0o600); err != nil {
		t.Fatalf("write key: %v", err)
	}
	t.Setenv(privateKeyEnvVar, path)

	data, _, err := loadPrivateKey(privateKeyFromEnv)
	if err != nil {
		t.Fatalf("loadPrivateKey: %v", err)
	}
	if string(data) != `{"kty":"OKP"}` {
		t.Errorf("data = %q", data)
	}
}

// Bare --private-key with nothing in the environment must say which variable to
// set. Failing with a bare "no such file" would name a path nobody typed.
func TestLoadPrivateKeyBareWithNoEnvExplainsItself(t *testing.T) {
	newKeyTestContextManager(t)
	t.Setenv(privateKeyEnvVar, "")

	_, _, err := loadPrivateKey(privateKeyFromEnv)
	if err == nil {
		t.Fatal("expected an error when the variable is unset")
	}
	if !strings.Contains(err.Error(), privateKeyEnvVar) {
		t.Errorf("error does not name %s: %v", privateKeyEnvVar, err)
	}
}

// A missing alias must report the path it expanded to. Reporting the alias
// alone would describe a file name that does not exist anywhere on disk.
func TestLoadPrivateKeyMissingAliasReportsExpandedPath(t *testing.T) {
	keyDir := newKeyTestContextManager(t)

	_, _, err := loadPrivateKey("absent")
	if err == nil {
		t.Fatal("expected an error for a missing alias")
	}
	if !strings.Contains(err.Error(), filepath.Join(keyDir, "absent.jwk")) {
		t.Errorf("error does not name the expanded path: %v", err)
	}
	if !strings.Contains(err.Error(), `"absent"`) {
		t.Errorf("error does not name the alias asked for: %v", err)
	}
}

// Naming a key twice is refused rather than ranked. Precedence would be
// defensible but not silence: these name identities, so quietly picking one
// authenticates as a principal the caller did not choose.
func TestLoadPrivateKeyRefusesFlagAndEnvTogether(t *testing.T) {
	keyDir := newKeyTestContextManager(t)
	if err := os.WriteFile(filepath.Join(keyDir, "ci.jwk"), []byte(`{"kty":"OKP"}`), 0o600); err != nil {
		t.Fatalf("write key: %v", err)
	}
	t.Setenv(privateKeyEnvVar, "/somewhere/else.jwk")

	_, _, err := loadPrivateKey("ci")
	if err == nil {
		t.Fatal("expected an error when both sources name a key")
	}
	if !strings.Contains(err.Error(), privateKeyEnvVar) || !strings.Contains(err.Error(), "--private-key") {
		t.Errorf("error does not name both sources: %v", err)
	}
}

// Refused even when the two happen to agree: equality across an alias, a path
// and an inline JWK is not a question worth answering, and "unset one" is the
// same fix either way.
func TestLoadPrivateKeyRefusesEvenWhenSourcesAgree(t *testing.T) {
	keyDir := newKeyTestContextManager(t)
	path := filepath.Join(keyDir, "ci.jwk")
	if err := os.WriteFile(path, []byte(`{"kty":"OKP"}`), 0o600); err != nil {
		t.Fatalf("write key: %v", err)
	}
	t.Setenv(privateKeyEnvVar, path)

	if _, _, err := loadPrivateKey(path); err == nil {
		t.Fatal("expected an error even when both sources name the same key")
	}
}

// The variable is read only when a key was asked for. An exported variable must
// never turn an ordinary login into a key-authenticated one.
func TestLoadPrivateKeyEnvStillWorksWhenItIsTheOnlySource(t *testing.T) {
	newKeyTestContextManager(t)
	t.Setenv(privateKeyEnvVar, `{"kty":"OKP","crv":"Ed25519"}`)

	data, source, err := loadPrivateKey(privateKeyFromEnv)
	if err != nil {
		t.Fatalf("loadPrivateKey: %v", err)
	}
	if string(data) != `{"kty":"OKP","crv":"Ed25519"}` || source != "$"+privateKeyEnvVar {
		t.Errorf("data = %q, source = %q", data, source)
	}
}

// keyAlias exists so `cub user key add --generate` can print something the user
// can pass back to `--private-key`. A key written outside the key directory has
// no alias, and claiming one would print a login line that does not work.
func TestKeyAliasOnlyForKeysInTheKeyDir(t *testing.T) {
	keyDir := newKeyTestContextManager(t)

	alias, ok := keyAlias(filepath.Join(keyDir, "ci.jwk"))
	if !ok || alias != "ci" {
		t.Errorf("keyAlias in key dir = (%q, %v), want (\"ci\", true)", alias, ok)
	}

	if _, ok := keyAlias("/somewhere/else/ci.jwk"); ok {
		t.Error("a key outside the key directory must not report an alias")
	}
}

// The space form of --private-key is the mistake an optional-value flag
// invites, and cobra's own report for it ("unknown command") describes
// something else entirely.
func TestAuthLoginArgsExplainsTheSpaceForm(t *testing.T) {
	cmd := &cobra.Command{}
	cmd.Flags().StringVar(&privateKeyRef, "private-key", "", "")
	cmd.Flags().Lookup("private-key").NoOptDefVal = privateKeyFromEnv
	t.Cleanup(func() { privateKeyRef = "" })

	if err := cmd.ParseFlags([]string{"--private-key", "ci"}); err != nil {
		t.Fatalf("ParseFlags: %v", err)
	}
	// pflag leaves the sentinel in place and hands "ci" back as a positional.
	err := authLoginArgs(cmd, cmd.Flags().Args())
	if err == nil {
		t.Fatal("expected an error for the space form")
	}
	if !strings.Contains(err.Error(), "--private-key=ci") {
		t.Errorf("error does not suggest the attached form: %v", err)
	}
}

// A positional argument that has nothing to do with --private-key must still be
// rejected the way it always was.
func TestAuthLoginArgsStillRejectsStrayArguments(t *testing.T) {
	cmd := &cobra.Command{}
	cmd.Flags().StringVar(&privateKeyRef, "private-key", "", "")
	t.Cleanup(func() { privateKeyRef = "" })

	if err := authLoginArgs(cmd, []string{"nonsense"}); err == nil {
		t.Fatal("expected stray arguments to be rejected")
	}
}
