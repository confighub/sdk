// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"

	coreplugin "github.com/confighub/sdk/core/plugin"
)

// setTestContextManager installs a real ContextManager at configPath with a
// current "test-ctx" context, for plugin tests that exercise pluginDir/pluginEnv.
func setTestContextManager(t *testing.T, configPath, server, space, token string) {
	t.Helper()
	cm, err := NewContextManagerWithPath(configPath)
	if err != nil {
		t.Fatal(err)
	}
	ctx, err := cm.CreateContext("test-ctx", server, "", space)
	if err != nil {
		t.Fatal(err)
	}
	if err := cm.SetCurrentContext("test-ctx"); err != nil {
		t.Fatal(err)
	}
	if token != "" {
		if err := cm.SaveTokenData(ctx, &TokenData{AccessToken: token}); err != nil {
			t.Fatal(err)
		}
	}
	contextManager = cm
}

// setupPluginTest creates a temporary plugin directory and initializes a minimal
// context manager pointing at it. It resets the plugin discovery cache.
func setupPluginTest(t *testing.T) string {
	t.Helper()
	tmpDir := t.TempDir()
	pluginsDir := filepath.Join(tmpDir, "plugins")
	if err := os.MkdirAll(pluginsDir, 0755); err != nil {
		t.Fatal(err)
	}

	// Set up a minimal but valid context manager so pluginDir() and pluginEnv()
	// work (the latter is used when running install/upgrade hooks).
	setTestContextManager(t, filepath.Join(tmpDir, "config.yaml"), "https://example.com", "", "")

	// Reset the plugin cache for each test.
	pluginCache = nil
	pluginCacheOnce = sync.Once{}

	return pluginsDir
}

func createExecutableFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0755); err != nil {
		t.Fatal(err)
	}
}

func createNonExecutableFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}

// firstEntrypoint returns the resolved entrypoint of a plugin's first command.
func firstEntrypoint(p *Plugin) string {
	if len(p.Commands) == 0 {
		return ""
	}
	return p.Commands[0].Entrypoint
}

// hasWarning reports whether any of the plugin's warnings contains substr.
func hasWarning(p *Plugin, substr string) bool {
	for _, w := range p.Warnings {
		if strings.Contains(w, substr) {
			return true
		}
	}
	return false
}

// resetPluginInstallArgs clears the shared install flags between tests.
func resetPluginInstallArgs(t *testing.T) {
	t.Helper()
	pluginInstallArgs.name = ""
	pluginInstallArgs.platform = ""
	pluginInstallArgs.sourceRepo = false
}

func TestDiscoverSingleFilePlugin(t *testing.T) {
	pluginsDir := setupPluginTest(t)

	createExecutableFile(t, filepath.Join(pluginsDir, "hello"), "#!/bin/sh\necho hello")

	plugins := discoverPlugins()
	if len(plugins) != 1 {
		t.Fatalf("expected 1 plugin, got %d", len(plugins))
	}
	p := plugins[0]
	if p.Name != "hello" {
		t.Errorf("expected name 'hello', got %q", p.Name)
	}
	if p.Path != filepath.Join(pluginsDir, "hello") {
		t.Errorf("unexpected path: %s", p.Path)
	}
	if len(p.Warnings) != 0 {
		t.Errorf("expected no warnings, got %v", p.Warnings)
	}
}

func TestDiscoverDirectoryPlugin(t *testing.T) {
	pluginsDir := setupPluginTest(t)

	dirPath := filepath.Join(pluginsDir, "deploy")
	if err := os.MkdirAll(dirPath, 0755); err != nil {
		t.Fatal(err)
	}
	createExecutableFile(t, filepath.Join(dirPath, "main"), "#!/bin/sh\necho deploy")

	plugins := discoverPlugins()
	if len(plugins) != 1 {
		t.Fatalf("expected 1 plugin, got %d", len(plugins))
	}
	p := plugins[0]
	if p.Name != "deploy" {
		t.Errorf("expected name 'deploy', got %q", p.Name)
	}
	if p.Path != dirPath {
		t.Errorf("unexpected path: %s", p.Path)
	}
	if firstEntrypoint(p) != filepath.Join(dirPath, "main") {
		t.Errorf("unexpected entrypoint: %s", firstEntrypoint(p))
	}
	if len(p.Warnings) != 0 {
		t.Errorf("expected no warnings, got %v", p.Warnings)
	}
}

func TestDiscoverDirectoryPluginMissingMain(t *testing.T) {
	pluginsDir := setupPluginTest(t)

	dirPath := filepath.Join(pluginsDir, "broken")
	if err := os.MkdirAll(dirPath, 0755); err != nil {
		t.Fatal(err)
	}
	// No "main" file inside

	plugins := discoverPlugins()
	if len(plugins) != 1 {
		t.Fatalf("expected 1 plugin, got %d", len(plugins))
	}
	p := plugins[0]
	if len(p.Warnings) == 0 {
		t.Fatal("expected warnings for missing main")
	}
	if !hasWarning(p, `missing executable "main"`) {
		t.Errorf("expected 'missing main' warning, got %v", p.Warnings)
	}
}

func TestDiscoverNotExecutablePlugin(t *testing.T) {
	pluginsDir := setupPluginTest(t)

	createNonExecutableFile(t, filepath.Join(pluginsDir, "noexec"), "#!/bin/sh\necho nope")

	plugins := discoverPlugins()
	if len(plugins) != 1 {
		t.Fatalf("expected 1 plugin, got %d", len(plugins))
	}
	p := plugins[0]
	if len(p.Warnings) == 0 {
		t.Fatal("expected warnings for non-executable")
	}
	if !hasWarning(p, "not executable") {
		t.Errorf("expected 'not executable' warning, got %v", p.Warnings)
	}
}

func TestDiscoverShadowsBuiltinWarning(t *testing.T) {
	pluginsDir := setupPluginTest(t)

	// "version" is a built-in command on rootCmd
	createExecutableFile(t, filepath.Join(pluginsDir, "version"), "#!/bin/sh\necho version")

	plugins := discoverPlugins()
	if len(plugins) != 1 {
		t.Fatalf("expected 1 plugin, got %d", len(plugins))
	}
	p := plugins[0]
	if !hasWarning(p, `shadows built-in command "version"`) {
		t.Errorf("expected shadow warning, got %v", p.Warnings)
	}
}

func TestFindPluginByName(t *testing.T) {
	pluginsDir := setupPluginTest(t)

	createExecutableFile(t, filepath.Join(pluginsDir, "foo"), "#!/bin/sh\necho foo")
	createExecutableFile(t, filepath.Join(pluginsDir, "foo-bar"), "#!/bin/sh\necho foo-bar")

	// "foo-bar" matches the foo-bar plugin directly
	path, remaining, err := findPlugin([]string{"foo-bar"})
	if err != nil {
		t.Fatal(err)
	}
	if path != filepath.Join(pluginsDir, "foo-bar") {
		t.Errorf("expected foo-bar, got %s", path)
	}
	if len(remaining) != 0 {
		t.Errorf("expected no remaining args, got %v", remaining)
	}

	// "foo bar baz" matches foo with remaining ["bar", "baz"]
	path, remaining, err = findPlugin([]string{"foo", "bar", "baz"})
	if err != nil {
		t.Fatal(err)
	}
	if path != filepath.Join(pluginsDir, "foo") {
		t.Errorf("expected foo, got %s", path)
	}
	if len(remaining) != 2 || remaining[0] != "bar" || remaining[1] != "baz" {
		t.Errorf("expected remaining [bar baz], got %v", remaining)
	}
}

func TestFindPluginDirectoryPlugin(t *testing.T) {
	pluginsDir := setupPluginTest(t)

	dirPath := filepath.Join(pluginsDir, "deploy")
	if err := os.MkdirAll(dirPath, 0755); err != nil {
		t.Fatal(err)
	}
	createExecutableFile(t, filepath.Join(dirPath, "main"), "#!/bin/sh\necho deploy")

	path, remaining, err := findPlugin([]string{"deploy", "--target", "prod"})
	if err != nil {
		t.Fatal(err)
	}
	if path != filepath.Join(dirPath, "main") {
		t.Errorf("expected dir plugin main, got %s", path)
	}
	if len(remaining) != 2 || remaining[0] != "--target" || remaining[1] != "prod" {
		t.Errorf("expected remaining [--target prod], got %v", remaining)
	}
}

func TestFindPluginNotFound(t *testing.T) {
	setupPluginTest(t)

	_, _, err := findPlugin([]string{"nonexistent"})
	if err == nil {
		t.Fatal("expected error for nonexistent plugin")
	}
}

func TestPluginEnv(t *testing.T) {
	tmpDir := t.TempDir()
	setTestContextManager(t, filepath.Join(tmpDir, "config.yaml"), "https://example.com", "prod", "test-token-123")

	env := pluginEnv()

	envMap := make(map[string]string)
	for _, e := range env {
		parts := splitEnvVar(e)
		if parts != nil {
			envMap[parts[0]] = parts[1]
		}
	}

	if envMap["CUB_PLUGIN"] != "1" {
		t.Errorf("expected CUB_PLUGIN=1, got %q", envMap["CUB_PLUGIN"])
	}
	if envMap["CUB_CONFIG"] != tmpDir {
		t.Errorf("expected CUB_CONFIG=%s, got %q", tmpDir, envMap["CUB_CONFIG"])
	}
	if envMap["CUB_CONTEXT"] != "test-ctx" {
		t.Errorf("expected CUB_CONTEXT=test-ctx, got %q", envMap["CUB_CONTEXT"])
	}
	if envMap["CUB_SERVER"] != "https://example.com" {
		t.Errorf("expected CUB_SERVER=https://example.com, got %q", envMap["CUB_SERVER"])
	}
	if envMap["CUB_SPACE"] != "prod" {
		t.Errorf("expected CUB_SPACE=prod, got %q", envMap["CUB_SPACE"])
	}
	if envMap["CUB_TOKEN"] != "test-token-123" {
		t.Errorf("expected CUB_TOKEN=test-token-123, got %q", envMap["CUB_TOKEN"])
	}
}

// splitEnvVar splits "KEY=VALUE" into [KEY, VALUE], handling values that contain "=".
func splitEnvVar(s string) []string {
	idx := 0
	for i, c := range s {
		if c == '=' {
			idx = i
			break
		}
	}
	if idx == 0 {
		return nil
	}
	return []string{s[:idx], s[idx+1:]}
}

func TestDiscoverIgnoresHiddenFiles(t *testing.T) {
	pluginsDir := setupPluginTest(t)

	// Create hidden entries that should be ignored
	createExecutableFile(t, filepath.Join(pluginsDir, ".DS_Store"), "")
	createExecutableFile(t, filepath.Join(pluginsDir, ".hidden"), "#!/bin/sh\necho nope")

	// Create a valid plugin
	createExecutableFile(t, filepath.Join(pluginsDir, "valid"), "#!/bin/sh\necho ok")

	plugins := discoverPlugins()
	if len(plugins) != 1 {
		t.Fatalf("expected 1 plugin, got %d", len(plugins))
	}
	if plugins[0].Name != "valid" {
		t.Errorf("expected name 'valid', got %q", plugins[0].Name)
	}
}

func TestDiscoverDashedPluginName(t *testing.T) {
	pluginsDir := setupPluginTest(t)

	createExecutableFile(t, filepath.Join(pluginsDir, "foo-bar-baz"), "#!/bin/sh\necho ok")

	plugins := discoverPlugins()
	if len(plugins) != 1 {
		t.Fatalf("expected 1 plugin, got %d", len(plugins))
	}
	p := plugins[0]
	if p.Name != "foo-bar-baz" {
		t.Errorf("expected name 'foo-bar-baz', got %q", p.Name)
	}
}

// --- plugin install tests ---

func TestParseInstallSource(t *testing.T) {
	tests := []struct {
		name      string
		source    string
		wantType  string // "direct", "github", "error"
		wantOwner string
		wantRepo  string
		wantTag   string
		wantURL   string
		wantTarGz bool
	}{
		{
			name:     "direct URL binary",
			source:   "https://example.com/my-plugin",
			wantType: "direct",
			wantURL:  "https://example.com/my-plugin",
		},
		{
			name:      "direct URL tar.gz",
			source:    "https://example.com/my-plugin.tar.gz",
			wantType:  "direct",
			wantURL:   "https://example.com/my-plugin.tar.gz",
			wantTarGz: true,
		},
		{
			name:      "direct URL tgz",
			source:    "https://example.com/my-plugin.tgz",
			wantType:  "direct",
			wantURL:   "https://example.com/my-plugin.tgz",
			wantTarGz: true,
		},
		{
			name:      "GitHub URL",
			source:    "https://github.com/myorg/cub-myplugin",
			wantType:  "github",
			wantOwner: "myorg",
			wantRepo:  "cub-myplugin",
		},
		{
			name:      "GitHub shorthand",
			source:    "myorg/myrepo",
			wantType:  "github",
			wantOwner: "myorg",
			wantRepo:  "myrepo",
		},
		{
			name:      "GitHub shorthand with tag",
			source:    "myorg/myrepo@v1.2.0",
			wantType:  "github",
			wantOwner: "myorg",
			wantRepo:  "myrepo",
			wantTag:   "v1.2.0",
		},
		{
			name:     "unsupported scheme",
			source:   "ftp://example.com/plugin",
			wantType: "error",
		},
		{
			name:     "invalid shorthand - no slash",
			source:   "justaname",
			wantType: "error",
		},
		{
			name:     "invalid shorthand - empty tag",
			source:   "org/repo@",
			wantType: "error",
		},
		{
			name:     "invalid GitHub URL - missing repo",
			source:   "https://github.com/orgonly",
			wantType: "error",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := parseInstallSource(tt.source)
			if tt.wantType == "error" {
				if err == nil {
					t.Fatalf("expected error, got %+v", result)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			switch tt.wantType {
			case "direct":
				d, ok := result.(*directURLSource)
				if !ok {
					t.Fatalf("expected directURLSource, got %T", result)
				}
				if d.url != tt.wantURL {
					t.Errorf("url = %q, want %q", d.url, tt.wantURL)
				}
				if d.isTarGz != tt.wantTarGz {
					t.Errorf("isTarGz = %v, want %v", d.isTarGz, tt.wantTarGz)
				}
			case "github":
				g, ok := result.(*githubSource)
				if !ok {
					t.Fatalf("expected githubSource, got %T", result)
				}
				if g.owner != tt.wantOwner {
					t.Errorf("owner = %q, want %q", g.owner, tt.wantOwner)
				}
				if g.repo != tt.wantRepo {
					t.Errorf("repo = %q, want %q", g.repo, tt.wantRepo)
				}
				if g.tag != tt.wantTag {
					t.Errorf("tag = %q, want %q", g.tag, tt.wantTag)
				}
			}
		})
	}
}

func TestResolvePlatform(t *testing.T) {
	// Default (empty) should return runtime values
	os, arch, err := resolvePlatform("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if os == "" || arch == "" {
		t.Fatal("expected non-empty os and arch")
	}

	// Explicit
	os, arch, err = resolvePlatform("linux/arm64")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if os != "linux" || arch != "arm64" {
		t.Errorf("got %s/%s, want linux/arm64", os, arch)
	}

	// Invalid
	_, _, err = resolvePlatform("invalid")
	if err == nil {
		t.Fatal("expected error for invalid platform")
	}

	_, _, err = resolvePlatform("/")
	if err == nil {
		t.Fatal("expected error for empty segments")
	}
}

func TestMatchAsset(t *testing.T) {
	tests := []struct {
		name      string
		assets    []ghAsset
		os        string
		arch      string
		wantName  string
		wantError bool
	}{
		{
			name: "exact match",
			assets: []ghAsset{
				{Name: "plugin-linux-amd64.tar.gz", BrowserDownloadURL: "https://example.com/linux"},
				{Name: "plugin-darwin-arm64.tar.gz", BrowserDownloadURL: "https://example.com/darwin"},
			},
			os:       "darwin",
			arch:     "arm64",
			wantName: "plugin-darwin-arm64.tar.gz",
		},
		{
			name: "alias match x86_64 for amd64",
			assets: []ghAsset{
				{Name: "plugin-linux-x86_64.tar.gz", BrowserDownloadURL: "https://example.com/linux"},
			},
			os:       "linux",
			arch:     "amd64",
			wantName: "plugin-linux-x86_64.tar.gz",
		},
		{
			name: "alias match macOS for darwin",
			assets: []ghAsset{
				{Name: "plugin-macOS-arm64.tar.gz", BrowserDownloadURL: "https://example.com/mac"},
			},
			os:       "darwin",
			arch:     "arm64",
			wantName: "plugin-macOS-arm64.tar.gz",
		},
		{
			name: "alias match aarch64 for arm64",
			assets: []ghAsset{
				{Name: "plugin-linux-aarch64.tar.gz", BrowserDownloadURL: "https://example.com/a"},
			},
			os:       "linux",
			arch:     "arm64",
			wantName: "plugin-linux-aarch64.tar.gz",
		},
		{
			name: "filters checksum files",
			assets: []ghAsset{
				{Name: "plugin-linux-amd64.tar.gz", BrowserDownloadURL: "https://example.com/bin"},
				{Name: "plugin-linux-amd64.tar.gz.sha256", BrowserDownloadURL: "https://example.com/sha"},
				{Name: "plugin-linux-amd64.tar.gz.sig", BrowserDownloadURL: "https://example.com/sig"},
			},
			os:       "linux",
			arch:     "amd64",
			wantName: "plugin-linux-amd64.tar.gz",
		},
		{
			name: "prefers tar.gz over binary",
			assets: []ghAsset{
				{Name: "plugin-linux-amd64", BrowserDownloadURL: "https://example.com/bin"},
				{Name: "plugin-linux-amd64.tar.gz", BrowserDownloadURL: "https://example.com/tgz"},
			},
			os:       "linux",
			arch:     "amd64",
			wantName: "plugin-linux-amd64.tar.gz",
		},
		{
			name: "no match",
			assets: []ghAsset{
				{Name: "plugin-windows-amd64.zip", BrowserDownloadURL: "https://example.com/win"},
			},
			os:        "linux",
			arch:      "arm64",
			wantError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			asset, err := matchAsset(tt.assets, tt.os, tt.arch)
			if tt.wantError {
				if err == nil {
					t.Fatalf("expected error, got %+v", asset)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if asset.Name != tt.wantName {
				t.Errorf("got asset %q, want %q", asset.Name, tt.wantName)
			}
		})
	}
}

func TestGithubPluginName(t *testing.T) {
	tests := []struct {
		repo string
		want string
	}{
		{"cub-deploy", "deploy"},
		{"cub-foo-bar", "foo-bar"},
		{"myplugin", "myplugin"},
	}

	for _, tt := range tests {
		t.Run(tt.repo, func(t *testing.T) {
			got := stripCubPrefix(tt.repo)
			if got != tt.want {
				t.Errorf("stripCubPrefix(%q) = %q, want %q", tt.repo, got, tt.want)
			}
		})
	}
}

func TestDerivePluginName(t *testing.T) {
	tests := []struct {
		name string
		src  any
		want string
	}{
		{
			name: "direct URL tar.gz",
			src:  &directURLSource{url: "https://example.com/my-plugin.tar.gz", derivedName: "my-plugin", isTarGz: true},
			want: "my-plugin",
		},
		{
			name: "direct URL binary",
			src:  &directURLSource{url: "https://example.com/cool-tool", derivedName: "cool-tool"},
			want: "cool-tool",
		},
		{
			name: "github source",
			src:  &githubSource{owner: "org", repo: "cub-deploy", pluginName: "deploy"},
			want: "deploy",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := derivePluginName(tt.src)
			if got != tt.want {
				t.Errorf("derivePluginName() = %q, want %q", got, tt.want)
			}
		})
	}
}

// createTestTarGz creates a tar.gz archive in memory.
func createTestTarGz(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gw)

	for name, content := range files {
		hdr := &tar.Header{
			Name: name,
			Mode: 0755,
			Size: int64(len(content)),
		}
		if strings.HasSuffix(name, "/") {
			hdr.Typeflag = tar.TypeDir
			hdr.Size = 0
		} else {
			hdr.Typeflag = tar.TypeReg
		}
		if err := tw.WriteHeader(hdr); err != nil {
			t.Fatal(err)
		}
		if hdr.Typeflag == tar.TypeReg {
			if _, err := tw.Write([]byte(content)); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestPluginInstallBinary(t *testing.T) {
	pluginsDir := setupPluginTest(t)

	binaryContent := "#!/bin/sh\necho hello"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(binaryContent))
	}))
	defer server.Close()

	err := downloadPluginBinary(server.URL+"/my-plugin", filepath.Join(pluginsDir, "hello"))
	if err != nil {
		t.Fatalf("downloadBinary failed: %v", err)
	}

	// Verify the file exists and is executable
	info, err := os.Stat(filepath.Join(pluginsDir, "hello"))
	if err != nil {
		t.Fatalf("plugin file not found: %v", err)
	}
	if info.Mode()&0111 == 0 {
		t.Error("expected executable permissions")
	}

	content, err := os.ReadFile(filepath.Join(pluginsDir, "hello"))
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != binaryContent {
		t.Errorf("content = %q, want %q", string(content), binaryContent)
	}
}

func TestPluginInstallTarGz(t *testing.T) {
	pluginsDir := setupPluginTest(t)

	archive := createTestTarGz(t, map[string]string{
		"main":      "#!/bin/sh\necho hello",
		"README.md": "# My Plugin",
	})

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(archive)
	}))
	defer server.Close()

	staged, err := stageTarGz(server.URL+"/plugin.tar.gz", pluginsDir)
	if err != nil {
		t.Fatalf("stageTarGz failed: %v", err)
	}
	defer os.RemoveAll(staged)

	info, err := os.Stat(staged)
	if err != nil {
		t.Fatalf("staged dir not found: %v", err)
	}
	if !info.IsDir() {
		t.Error("expected directory")
	}

	mainPath := filepath.Join(staged, "main")
	mainInfo, err := os.Stat(mainPath)
	if err != nil {
		t.Fatalf("main not found: %v", err)
	}
	if mainInfo.Mode()&0111 == 0 {
		t.Error("expected main to be executable")
	}

	readmePath := filepath.Join(staged, "README.md")
	if _, err := os.Stat(readmePath); err != nil {
		t.Fatalf("README.md not found: %v", err)
	}
}

func TestPluginInstallExistingErrors(t *testing.T) {
	pluginsDir := setupPluginTest(t)
	resetPluginInstallArgs(t)

	// Create an existing plugin in the slot that the source would resolve to.
	createExecutableFile(t, filepath.Join(pluginsDir, "existing"), "#!/bin/sh\necho old")

	// Installing over it must fail before any network access.
	err := pluginInstallCmdRun(nil, []string{"someorg/cub-existing"})
	if err == nil {
		t.Fatal("expected error installing over an existing plugin")
	}
	if !strings.Contains(err.Error(), "already installed") {
		t.Errorf("unexpected error: %v", err)
	}
	if !strings.Contains(err.Error(), "upgrade") {
		t.Errorf("error should point to upgrade: %v", err)
	}
}

func TestPluginInstallGitHubAPI(t *testing.T) {
	pluginsDir := setupPluginTest(t)

	binaryContent := "#!/bin/sh\necho github-plugin"

	// Create a mock GitHub API + download server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/releases/latest"):
			release := ghRelease{
				TagName: "v1.0.0",
				Assets: []ghAsset{
					{
						Name:               "plugin-linux-amd64",
						BrowserDownloadURL: fmt.Sprintf("http://%s/download/plugin-linux-amd64", r.Host),
					},
					{
						Name:               "plugin-darwin-arm64",
						BrowserDownloadURL: fmt.Sprintf("http://%s/download/plugin-darwin-arm64", r.Host),
					},
					{
						Name:               "plugin-linux-amd64.sha256",
						BrowserDownloadURL: fmt.Sprintf("http://%s/download/plugin-linux-amd64.sha256", r.Host),
					},
				},
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(release)
		case strings.HasPrefix(r.URL.Path, "/download/"):
			w.Write([]byte(binaryContent))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	// Override the GitHub API base URL for the test
	origBaseURL := githubAPIBaseURL
	githubAPIBaseURL = server.URL
	defer func() { githubAPIBaseURL = origBaseURL }()

	src := &githubSource{
		owner:      "testorg",
		repo:       "cub-myplugin",
		pluginName: "myplugin",
	}

	release, err := resolveGitHubRelease(src)
	if err != nil {
		t.Fatalf("resolveGitHubRelease failed: %v", err)
	}
	if release.TagName != "v1.0.0" {
		t.Errorf("tag = %q, want v1.0.0", release.TagName)
	}

	asset, err := matchAsset(release.Assets, "linux", "amd64")
	if err != nil {
		t.Fatalf("matchAsset failed: %v", err)
	}
	if asset.Name != "plugin-linux-amd64" {
		t.Errorf("asset = %q, want plugin-linux-amd64", asset.Name)
	}

	// Download the asset
	destPath := filepath.Join(pluginsDir, "myplugin")
	if err := downloadPluginBinary(asset.BrowserDownloadURL, destPath); err != nil {
		t.Fatalf("downloadBinary failed: %v", err)
	}

	content, err := os.ReadFile(destPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != binaryContent {
		t.Errorf("content = %q, want %q", string(content), binaryContent)
	}
}

// --- plugin uninstall tests ---

func TestPluginUninstallSingleFile(t *testing.T) {
	pluginsDir := setupPluginTest(t)

	createExecutableFile(t, filepath.Join(pluginsDir, "hello"), "#!/bin/sh\necho hello")

	err := pluginUninstallCmdRun(nil, []string{"hello"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if _, err := os.Stat(filepath.Join(pluginsDir, "hello")); !os.IsNotExist(err) {
		t.Error("expected plugin file to be removed")
	}
}

func TestPluginUninstallDirectoryPlugin(t *testing.T) {
	pluginsDir := setupPluginTest(t)

	dirPath := filepath.Join(pluginsDir, "deploy")
	if err := os.MkdirAll(dirPath, 0755); err != nil {
		t.Fatal(err)
	}
	createExecutableFile(t, filepath.Join(dirPath, "main"), "#!/bin/sh\necho deploy")
	createNonExecutableFile(t, filepath.Join(dirPath, "lib.sh"), "# helper")

	err := pluginUninstallCmdRun(nil, []string{"deploy"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if _, err := os.Stat(dirPath); !os.IsNotExist(err) {
		t.Error("expected plugin directory to be removed")
	}
}

func TestPluginUninstallNotInstalled(t *testing.T) {
	setupPluginTest(t)

	err := pluginUninstallCmdRun(nil, []string{"nonexistent"})
	if err == nil {
		t.Fatal("expected error for nonexistent plugin")
	}
	if !strings.Contains(err.Error(), "is not installed") {
		t.Errorf("unexpected error: %v", err)
	}
}

// --- cub-plugin.yaml manifest tests ---

func TestDiscoverManifestEntrypoint(t *testing.T) {
	pluginsDir := setupPluginTest(t)

	// Directory plugin with cub-plugin.yaml specifying a custom entrypoint
	dirPath := filepath.Join(pluginsDir, "vmcluster")
	if err := os.MkdirAll(dirPath, 0755); err != nil {
		t.Fatal(err)
	}
	createExecutableFile(t, filepath.Join(dirPath, "vmctl"), "#!/bin/sh\necho vmctl")
	if err := os.WriteFile(filepath.Join(dirPath, "cub-plugin.yaml"), []byte("entrypoint: vmctl\n"), 0644); err != nil {
		t.Fatal(err)
	}

	plugins := discoverPlugins()
	if len(plugins) != 1 {
		t.Fatalf("expected 1 plugin, got %d", len(plugins))
	}
	p := plugins[0]
	if p.Name != "vmcluster" {
		t.Errorf("expected name 'vmcluster', got %q", p.Name)
	}
	if firstEntrypoint(p) != filepath.Join(dirPath, "vmctl") {
		t.Errorf("expected entrypoint to vmctl, got %s", firstEntrypoint(p))
	}
	if len(p.Warnings) != 0 {
		t.Errorf("expected no warnings, got %v", p.Warnings)
	}
}

func TestDiscoverManifestEntrypointMissing(t *testing.T) {
	pluginsDir := setupPluginTest(t)

	dirPath := filepath.Join(pluginsDir, "myplugin")
	if err := os.MkdirAll(dirPath, 0755); err != nil {
		t.Fatal(err)
	}
	// Manifest points to "run.sh" but file doesn't exist
	if err := os.WriteFile(filepath.Join(dirPath, "cub-plugin.yaml"), []byte("entrypoint: run.sh\n"), 0644); err != nil {
		t.Fatal(err)
	}

	plugins := discoverPlugins()
	if len(plugins) != 1 {
		t.Fatalf("expected 1 plugin, got %d", len(plugins))
	}
	found := false
	for _, w := range plugins[0].Warnings {
		if strings.Contains(w, `"run.sh"`) {
			found = true
		}
	}
	if !found {
		t.Errorf("expected warning about missing 'run.sh', got %v", plugins[0].Warnings)
	}
}

func TestDiscoverManifestFallbackToMain(t *testing.T) {
	pluginsDir := setupPluginTest(t)

	// Directory plugin with no manifest — should fall back to "main"
	dirPath := filepath.Join(pluginsDir, "classic")
	if err := os.MkdirAll(dirPath, 0755); err != nil {
		t.Fatal(err)
	}
	createExecutableFile(t, filepath.Join(dirPath, "main"), "#!/bin/sh\necho classic")

	plugins := discoverPlugins()
	if len(plugins) != 1 {
		t.Fatalf("expected 1 plugin, got %d", len(plugins))
	}
	if firstEntrypoint(plugins[0]) != filepath.Join(dirPath, "main") {
		t.Errorf("expected entrypoint to main, got %s", firstEntrypoint(plugins[0]))
	}
	if len(plugins[0].Warnings) != 0 {
		t.Errorf("expected no warnings, got %v", plugins[0].Warnings)
	}
}

func TestFindPluginManifestEntrypoint(t *testing.T) {
	pluginsDir := setupPluginTest(t)

	dirPath := filepath.Join(pluginsDir, "vmcluster")
	if err := os.MkdirAll(dirPath, 0755); err != nil {
		t.Fatal(err)
	}
	createExecutableFile(t, filepath.Join(dirPath, "vmctl"), "#!/bin/sh\necho vmctl")
	if err := os.WriteFile(filepath.Join(dirPath, "cub-plugin.yaml"), []byte("entrypoint: vmctl\n"), 0644); err != nil {
		t.Fatal(err)
	}

	path, remaining, err := findPlugin([]string{"vmcluster", "status"})
	if err != nil {
		t.Fatal(err)
	}
	if path != filepath.Join(dirPath, "vmctl") {
		t.Errorf("expected vmctl path, got %s", path)
	}
	if len(remaining) != 1 || remaining[0] != "status" {
		t.Errorf("expected remaining [status], got %v", remaining)
	}
}

func TestDiscoverManifestMalformedYAML(t *testing.T) {
	pluginsDir := setupPluginTest(t)

	dirPath := filepath.Join(pluginsDir, "badyaml")
	if err := os.MkdirAll(dirPath, 0755); err != nil {
		t.Fatal(err)
	}
	// Write invalid YAML
	if err := os.WriteFile(filepath.Join(dirPath, "cub-plugin.yaml"), []byte(":\n  :\n  - [broken"), 0644); err != nil {
		t.Fatal(err)
	}

	plugins := discoverPlugins()
	if len(plugins) != 1 {
		t.Fatalf("expected 1 plugin, got %d", len(plugins))
	}
	found := false
	for _, w := range plugins[0].Warnings {
		if strings.Contains(w, "invalid cub-plugin.yaml") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected warning about invalid cub-plugin.yaml, got %v", plugins[0].Warnings)
	}
}

func TestFindPluginManifestMalformedYAML(t *testing.T) {
	pluginsDir := setupPluginTest(t)

	dirPath := filepath.Join(pluginsDir, "badyaml")
	if err := os.MkdirAll(dirPath, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dirPath, "cub-plugin.yaml"), []byte(":\n  :\n  - [broken"), 0644); err != nil {
		t.Fatal(err)
	}

	// A malformed manifest yields a discovery warning, not a usable command, so
	// the command does not resolve.
	_, _, err := findPlugin([]string{"badyaml"})
	if err == nil {
		t.Fatal("expected error for malformed manifest")
	}

	// The parse failure is surfaced as a warning during discovery.
	plugins := discoverPlugins()
	if len(plugins) != 1 || !hasWarning(plugins[0], "invalid cub-plugin.yaml") {
		t.Errorf("expected 'invalid cub-plugin.yaml' warning, got: %v", plugins)
	}
}

// --- --source-repo install tests ---

func TestPluginInstallSourceRepo(t *testing.T) {
	pluginsDir := setupPluginTest(t)

	// Create a tarball mimicking a GitHub repo download (single top-level dir)
	archive := createTestTarGz(t, map[string]string{
		"cub-vmcluster-abc123/":                "",
		"cub-vmcluster-abc123/vmctl":           "#!/bin/sh\necho vmctl",
		"cub-vmcluster-abc123/cub-plugin.yaml": "entrypoint: vmctl\n",
		"cub-vmcluster-abc123/README.md":       "# vmcluster",
	})

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/tarball/") {
			w.Write(archive)
		} else {
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	origBaseURL := githubAPIBaseURL
	githubAPIBaseURL = server.URL
	defer func() { githubAPIBaseURL = origBaseURL }()

	src := &githubSource{
		owner:      "jesperfj",
		repo:       "cub-vmcluster",
		pluginName: "vmcluster",
	}

	staged, err := stageRepoTarball(src, pluginsDir)
	if err != nil {
		t.Fatalf("stageRepoTarball failed: %v", err)
	}
	defer os.RemoveAll(staged)

	// Verify directory was extracted with top-level dir stripped
	vmctlPath := filepath.Join(staged, "vmctl")
	info, err := os.Stat(vmctlPath)
	if err != nil {
		t.Fatalf("vmctl not found: %v", err)
	}
	if info.Mode()&0111 == 0 {
		t.Error("expected vmctl to be executable")
	}

	// Verify the committed manifest survived extraction.
	data, err := os.ReadFile(filepath.Join(staged, "cub-plugin.yaml"))
	if err != nil {
		t.Fatalf("cub-plugin.yaml not found: %v", err)
	}
	if !strings.Contains(string(data), "vmctl") {
		t.Errorf("expected manifest to reference vmctl, got %q", string(data))
	}
}

func TestPluginInstallSourceRepoWithRef(t *testing.T) {
	pluginsDir := setupPluginTest(t)

	archive := createTestTarGz(t, map[string]string{
		"repo-dev/":     "",
		"repo-dev/main": "#!/bin/sh\necho dev",
	})

	var requestedPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestedPath = r.URL.Path
		w.Write(archive)
	}))
	defer server.Close()

	origBaseURL := githubAPIBaseURL
	githubAPIBaseURL = server.URL
	defer func() { githubAPIBaseURL = origBaseURL }()

	src := &githubSource{
		owner:      "org",
		repo:       "cub-myplugin",
		tag:        "dev",
		pluginName: "myplugin",
	}

	staged, err := stageRepoTarball(src, pluginsDir)
	if err != nil {
		t.Fatalf("stageRepoTarball failed: %v", err)
	}
	defer os.RemoveAll(staged)

	// Verify the ref was included in the API URL
	if !strings.HasSuffix(requestedPath, "/tarball/dev") {
		t.Errorf("expected tarball URL with ref 'dev', got path %q", requestedPath)
	}
}

func TestPluginInstallAutoFallbackToRepo(t *testing.T) {
	pluginsDir := setupPluginTest(t)

	archive := createTestTarGz(t, map[string]string{
		"cub-myplugin-abc123/":                "",
		"cub-myplugin-abc123/main":            "#!/bin/sh\necho hello",
		"cub-myplugin-abc123/cub-plugin.yaml": "entrypoint: main\n",
	})

	// Mock server: 404 on releases, serve tarball on /tarball/
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.Contains(r.URL.Path, "/releases/"):
			http.NotFound(w, r)
		case strings.Contains(r.URL.Path, "/tarball/"):
			w.Write(archive)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	origBaseURL := githubAPIBaseURL
	githubAPIBaseURL = server.URL
	defer func() { githubAPIBaseURL = origBaseURL }()

	// Exercise the full pluginInstallCmdRun path
	resetPluginInstallArgs(t)

	err := pluginInstallCmdRun(nil, []string{"testorg/cub-myplugin"})
	if err != nil {
		t.Fatalf("pluginInstallCmdRun failed: %v", err)
	}

	// Verify the plugin was installed via the fallback path
	mainPath := filepath.Join(pluginsDir, "myplugin", "main")
	if _, err := os.Stat(mainPath); err != nil {
		t.Fatalf("main not found after fallback install: %v", err)
	}

	// The install source should have been recorded for later upgrade.
	if _, err := readInstallMetadata(filepath.Join(pluginsDir, "myplugin")); err != nil {
		t.Errorf("expected install metadata to be recorded: %v", err)
	}
}

func TestPluginInstallPinnedTagNoFallback(t *testing.T) {
	// Mock server: 404 on a specific release tag
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	defer server.Close()

	origBaseURL := githubAPIBaseURL
	githubAPIBaseURL = server.URL
	defer func() { githubAPIBaseURL = origBaseURL }()

	src := &githubSource{
		owner:      "testorg",
		repo:       "cub-myplugin",
		tag:        "v1.0.0",
		pluginName: "myplugin",
	}

	_, err := resolveGitHubRelease(src)
	if err == nil {
		t.Fatal("expected error for pinned tag 404")
	}
	// Should be a specific "release not found" error, NOT the generic "no releases found"
	if strings.Contains(err.Error(), "no releases found") {
		t.Fatalf("pinned tag should not trigger fallback error, got: %v", err)
	}
	if !strings.Contains(err.Error(), "v1.0.0") {
		t.Fatalf("expected error mentioning the tag, got: %v", err)
	}
}

func TestExtractTarGzStripsSingleTopDir(t *testing.T) {
	pluginsDir := setupPluginTest(t)

	// Archive with a single top-level directory (as GitHub creates)
	archive := createTestTarGz(t, map[string]string{
		"myrepo-v1.0.0/":          "",
		"myrepo-v1.0.0/main":      "#!/bin/sh\necho hello",
		"myrepo-v1.0.0/README.md": "# Plugin",
	})

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(archive)
	}))
	defer server.Close()

	staged, err := stageTarGz(server.URL+"/plugin.tar.gz", pluginsDir)
	if err != nil {
		t.Fatalf("stageTarGz failed: %v", err)
	}
	defer os.RemoveAll(staged)

	// The top-level "myrepo-v1.0.0" should have been stripped
	mainPath := filepath.Join(staged, "main")
	if _, err := os.Stat(mainPath); err != nil {
		t.Fatalf("main not found after stripping top dir: %v", err)
	}

	readmePath := filepath.Join(staged, "README.md")
	if _, err := os.Stat(readmePath); err != nil {
		t.Fatalf("README.md not found after stripping top dir: %v", err)
	}
}

// --- collision and hook tests ---

func TestCheckCommandCollisions(t *testing.T) {
	pluginsDir := setupPluginTest(t)

	// An installed plugin "tools" that contributes commands "deploy" and "dep".
	toolsDir := filepath.Join(pluginsDir, "tools")
	if err := os.MkdirAll(toolsDir, 0755); err != nil {
		t.Fatal(err)
	}
	createExecutableFile(t, filepath.Join(toolsDir, "tools"), "#!/bin/sh\n")
	manifest := "commands:\n  - name: deploy\n    aliases: [dep]\n    entrypoint: tools\n"
	if err := os.WriteFile(filepath.Join(toolsDir, "cub-plugin.yaml"), []byte(manifest), 0644); err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		name    string
		exclude string
		cmds    []struct{ name, alias string }
		wantErr string
	}{
		{"builtin", "new", []struct{ name, alias string }{{"version", ""}}, "built-in"},
		{"otherplugin", "new", []struct{ name, alias string }{{"deploy", ""}}, "already provided"},
		{"otherplugin alias", "new", []struct{ name, alias string }{{"dep", ""}}, "already provided"},
		{"duplicate within manifest", "new", []struct{ name, alias string }{{"a", "a"}}, "more than once"},
		{"ok", "new", []struct{ name, alias string }{{"fresh", "f"}}, ""},
		{"self excluded", "tools", []struct{ name, alias string }{{"deploy", "dep"}}, ""},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var cmds []coreplugin.Command
			for _, c := range tc.cmds {
				cc := coreplugin.Command{Name: c.name, Entrypoint: "x"}
				if c.alias != "" {
					cc.Aliases = []string{c.alias}
				}
				cmds = append(cmds, cc)
			}
			err := checkCommandCollisions(tc.exclude, cmds)
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("expected no error, got %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("expected error containing %q, got %v", tc.wantErr, err)
			}
		})
	}
}

// TestPluginInstallRunsHookAndManifest installs a bare-binary plugin whose hook
// writes a multi-command manifest, then verifies discovery and routing.
func TestPluginInstallRunsHookAndManifest(t *testing.T) {
	pluginsDir := setupPluginTest(t)
	resetPluginInstallArgs(t)

	// A "binary" that, when invoked as a hook, writes a manifest declaring two
	// commands (with an alias) backed by itself, then exits. Otherwise it echoes.
	hookScript := `#!/bin/sh
if [ -n "$CUB_PLUGIN_HOOK" ]; then
  cat > "$CUB_PLUGIN_DIR/cub-plugin.yaml" <<YAML
name: demo
version: 1.0.0
commands:
  - name: demo
    aliases: [dmo]
    entrypoint: demo
  - name: demo-extra
    entrypoint: demo
    args: ["extra"]
YAML
  exit 0
fi
echo "ran: $@"
`
	assetName := fmt.Sprintf("demo-%s-%s", runtime.GOOS, runtime.GOARCH)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/releases/latest"):
			release := ghRelease{
				TagName: "v1.0.0",
				Assets: []ghAsset{{
					Name:               assetName,
					BrowserDownloadURL: fmt.Sprintf("http://%s/download/%s", r.Host, assetName),
				}},
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(release)
		case strings.HasPrefix(r.URL.Path, "/download/"):
			w.Write([]byte(hookScript))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	origBaseURL := githubAPIBaseURL
	githubAPIBaseURL = server.URL
	defer func() { githubAPIBaseURL = origBaseURL }()

	if err := pluginInstallCmdRun(nil, []string{"someorg/cub-demo"}); err != nil {
		t.Fatalf("install failed: %v", err)
	}

	// The manifest the hook wrote should drive discovery.
	plugins := discoverPlugins()
	if len(plugins) != 1 {
		t.Fatalf("expected 1 plugin, got %d", len(plugins))
	}
	p := plugins[0]
	if p.Name != "demo" {
		t.Errorf("expected slot 'demo', got %q", p.Name)
	}
	if len(p.Warnings) != 0 {
		t.Errorf("unexpected warnings: %v", p.Warnings)
	}
	if len(p.Commands) != 2 {
		t.Fatalf("expected 2 commands, got %d: %+v", len(p.Commands), p.Commands)
	}
	if p.Version != "1.0.0" {
		t.Errorf("expected version 1.0.0, got %q", p.Version)
	}

	// Routing: the alias resolves to the binary with no args prefix.
	binPath := filepath.Join(pluginsDir, "demo", "demo")
	path, remaining, err := findPlugin([]string{"dmo", "go"})
	if err != nil {
		t.Fatal(err)
	}
	if path != binPath {
		t.Errorf("expected %s, got %s", binPath, path)
	}
	if len(remaining) != 1 || remaining[0] != "go" {
		t.Errorf("expected [go], got %v", remaining)
	}

	// Routing: the second command prepends its args prefix.
	_, remaining, err = findPlugin([]string{"demo-extra", "run"})
	if err != nil {
		t.Fatal(err)
	}
	if len(remaining) != 2 || remaining[0] != "extra" || remaining[1] != "run" {
		t.Errorf("expected [extra run], got %v", remaining)
	}

	// Install metadata was recorded.
	if _, err := readInstallMetadata(filepath.Join(pluginsDir, "demo")); err != nil {
		t.Errorf("expected install metadata: %v", err)
	}
}

// TestPluginUpgradePreservesStateAndRegenerates installs a bare-binary plugin,
// then upgrades it, verifying user files are preserved, the upgrade hook runs
// with the previous version, and the manifest is regenerated.
func TestPluginUpgradePreservesStateAndRegenerates(t *testing.T) {
	pluginsDir := setupPluginTest(t)
	resetPluginInstallArgs(t)

	version := "1.0.0"
	assetName := fmt.Sprintf("demo-%s-%s", runtime.GOOS, runtime.GOARCH)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/releases/latest"):
			release := ghRelease{
				TagName: "v" + version,
				Assets: []ghAsset{{
					Name:               assetName,
					BrowserDownloadURL: fmt.Sprintf("http://%s/download/%s", r.Host, assetName),
				}},
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(release)
		case strings.HasPrefix(r.URL.Path, "/download/"):
			// The hook records phase + previous version and regenerates the manifest.
			script := fmt.Sprintf(`#!/bin/sh
if [ -n "$CUB_PLUGIN_HOOK" ]; then
  echo "$CUB_PLUGIN_HOOK $CUB_PLUGIN_PREVIOUS_VERSION" > "$CUB_PLUGIN_DIR/hookinfo"
  cat > "$CUB_PLUGIN_DIR/cub-plugin.yaml" <<YAML
name: demo
version: %s
commands:
  - name: demo
    entrypoint: demo
YAML
  exit 0
fi
`, version)
			w.Write([]byte(script))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	origBaseURL := githubAPIBaseURL
	githubAPIBaseURL = server.URL
	defer func() { githubAPIBaseURL = origBaseURL }()

	// Install v1.0.0.
	if err := pluginInstallCmdRun(nil, []string{"someorg/cub-demo"}); err != nil {
		t.Fatalf("install failed: %v", err)
	}
	demoDir := filepath.Join(pluginsDir, "demo")

	// Simulate a user-created config file in the plugin directory.
	userFile := filepath.Join(demoDir, "config.txt")
	if err := os.WriteFile(userFile, []byte("user data"), 0644); err != nil {
		t.Fatal(err)
	}

	// Bump the served version and upgrade.
	version = "2.0.0"
	pluginCache = nil
	pluginCacheOnce = sync.Once{}
	if err := upgradeOnePlugin(pluginsDir, "demo", ""); err != nil {
		t.Fatalf("upgrade failed: %v", err)
	}

	// User file preserved.
	if data, err := os.ReadFile(userFile); err != nil || string(data) != "user data" {
		t.Errorf("user file not preserved: data=%q err=%v", string(data), err)
	}

	// Hook ran in upgrade mode with the previous version.
	info, err := os.ReadFile(filepath.Join(demoDir, "hookinfo"))
	if err != nil {
		t.Fatalf("hookinfo not found: %v", err)
	}
	if strings.TrimSpace(string(info)) != "upgrade 1.0.0" {
		t.Errorf("expected 'upgrade 1.0.0', got %q", strings.TrimSpace(string(info)))
	}

	// Manifest regenerated to the new version.
	m, err := coreplugin.Read(demoDir)
	if err != nil || m == nil {
		t.Fatalf("read manifest: %v", err)
	}
	if m.Version != "2.0.0" {
		t.Errorf("expected version 2.0.0, got %q", m.Version)
	}
}

// TestPluginInstallTarballNoManifestDefaultsToMain verifies that a tarball
// release without a committed cub-plugin.yaml falls back to a single command
// whose entrypoint is "main".
func TestPluginInstallTarballNoManifestDefaultsToMain(t *testing.T) {
	pluginsDir := setupPluginTest(t)
	resetPluginInstallArgs(t)

	archive := createTestTarGz(t, map[string]string{
		"main":      "#!/bin/sh\necho hi",
		"README.md": "# plugin",
	})
	assetName := fmt.Sprintf("myplugin-%s-%s.tar.gz", runtime.GOOS, runtime.GOARCH)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/releases/latest"):
			rel := ghRelease{
				TagName: "v1.0.0",
				Assets: []ghAsset{{
					Name:               assetName,
					BrowserDownloadURL: fmt.Sprintf("http://%s/download/%s", r.Host, assetName),
				}},
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(rel)
		case strings.HasPrefix(r.URL.Path, "/download/"):
			w.Write(archive)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	origBaseURL := githubAPIBaseURL
	githubAPIBaseURL = server.URL
	defer func() { githubAPIBaseURL = origBaseURL }()

	if err := pluginInstallCmdRun(nil, []string{"someorg/cub-myplugin"}); err != nil {
		t.Fatalf("install failed: %v", err)
	}

	m, err := coreplugin.Read(filepath.Join(pluginsDir, "myplugin"))
	if err != nil || m == nil {
		t.Fatalf("read manifest: %v", err)
	}
	if len(m.Commands) != 1 || m.Commands[0].Name != "myplugin" || m.Commands[0].Entrypoint != "main" {
		t.Fatalf("expected single command myplugin->main, got %+v", m.Commands)
	}

	// The command resolves to the "main" executable.
	path, _, err := findPlugin([]string{"myplugin"})
	if err != nil {
		t.Fatal(err)
	}
	if path != filepath.Join(pluginsDir, "myplugin", "main") {
		t.Errorf("expected entrypoint .../myplugin/main, got %s", path)
	}
}

// --- installing from a local path ------------------------------------------

// localHookScript is a stand-in for a freshly built plugin binary: invoked as an
// install hook it writes its own manifest, exactly as a real plugin does, and
// otherwise it runs as the plugin.
const localHookScript = `#!/bin/sh
if [ -n "$CUB_PLUGIN_HOOK" ]; then
  cat > "$CUB_PLUGIN_DIR/cub-plugin.yaml" <<YAML
name: thing
version: 1.0.0
commands:
  - name: thing
    entrypoint: thing
YAML
  exit 0
fi
echo "ran: $@"
`

func TestParseInstallSourceLocal(t *testing.T) {
	dir := t.TempDir()

	binPath := filepath.Join(dir, "cub-thing")
	createExecutableFile(t, binPath, "#!/bin/sh\n")

	tarPath := filepath.Join(dir, "thing.tar.gz")
	if err := os.WriteFile(tarPath, createTestTarGz(t, map[string]string{"main": "#!/bin/sh\n"}), 0644); err != nil {
		t.Fatal(err)
	}

	pkgDir := filepath.Join(dir, "cub-packaged")
	if err := os.MkdirAll(pkgDir, 0755); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name     string
		source   string
		wantKind localKind
		wantName string
	}{
		{"absolute binary", binPath, localBinary, "thing"},
		{"absolute archive", tarPath, localTarGz, "thing"},
		{"absolute directory", pkgDir, localDir, "packaged"},
		{"file:// URL", "file://" + binPath, localBinary, "thing"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			src, err := parseInstallSource(tt.source)
			if err != nil {
				t.Fatalf("parseInstallSource(%q): %v", tt.source, err)
			}
			local, ok := src.(*localSource)
			if !ok {
				t.Fatalf("got %T, want *localSource", src)
			}
			if local.kind != tt.wantKind {
				t.Errorf("kind = %v, want %v", local.kind, tt.wantKind)
			}
			if local.pluginName != tt.wantName {
				t.Errorf("name = %q, want %q", local.pluginName, tt.wantName)
			}
			if !filepath.IsAbs(local.path) {
				t.Errorf("path = %q, want an absolute path", local.path)
			}
		})
	}
}

// A relative path resolves against the working directory and is recorded
// absolute, so that a later upgrade from elsewhere still finds it.
func TestParseInstallSourceLocalRelativeBecomesAbsolute(t *testing.T) {
	dir := t.TempDir()
	createExecutableFile(t, filepath.Join(dir, "cub-thing"), "#!/bin/sh\n")
	t.Chdir(dir)

	src, err := parseInstallSource("./cub-thing")
	if err != nil {
		t.Fatalf("parseInstallSource: %v", err)
	}
	local, ok := src.(*localSource)
	if !ok {
		t.Fatalf("got %T, want *localSource", src)
	}
	// Compare resolved paths: t.TempDir is under /var on macOS, a symlink to
	// /private/var, and only one side of the comparison goes through Abs.
	want, _ := filepath.EvalSymlinks(filepath.Join(dir, "cub-thing"))
	got, _ := filepath.EvalSymlinks(local.path)
	if got != want {
		t.Errorf("path = %q, want %q", got, want)
	}
}

// "owner/repo" is GitHub even when a directory of that name is sitting right
// there, so the same command does not mean different things in different shells.
func TestParseInstallSourceLocalDoesNotShadowGitHubShorthand(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "confighub", "cub-thing"), 0755); err != nil {
		t.Fatal(err)
	}
	t.Chdir(dir)

	src, err := parseInstallSource("confighub/cub-thing")
	if err != nil {
		t.Fatalf("parseInstallSource: %v", err)
	}
	gh, ok := src.(*githubSource)
	if !ok {
		t.Fatalf("got %T, want *githubSource", src)
	}
	if gh.owner != "confighub" || gh.repo != "cub-thing" {
		t.Errorf("got %s/%s, want confighub/cub-thing", gh.owner, gh.repo)
	}
}

func TestParseInstallSourceLocalMissingPath(t *testing.T) {
	if _, err := parseInstallSource("./definitely-not-here"); err == nil {
		t.Fatal("expected an error for a path that does not exist")
	}
}

// The case this exists for: install a binary built on this machine and have it
// treated exactly as a downloaded release asset, install hook included.
func TestPluginInstallLocalBinaryRunsHook(t *testing.T) {
	pluginsDir := setupPluginTest(t)
	resetPluginInstallArgs(t)

	buildDir := t.TempDir()
	binPath := filepath.Join(buildDir, "cub-thing")
	createExecutableFile(t, binPath, localHookScript)

	if err := pluginInstallCmdRun(nil, []string{binPath}); err != nil {
		t.Fatalf("install failed: %v", err)
	}

	installed := filepath.Join(pluginsDir, "thing")
	if _, err := os.Stat(filepath.Join(installed, "cub-plugin.yaml")); err != nil {
		t.Fatalf("hook did not write a manifest: %v", err)
	}

	info, err := os.Stat(filepath.Join(installed, "thing"))
	if err != nil {
		t.Fatalf("binary not installed under the plugin name: %v", err)
	}
	if info.Mode()&0111 == 0 {
		t.Error("installed binary is not executable")
	}

	// The recorded source has to survive a change of working directory, since
	// that is what upgrade re-reads.
	meta, err := readInstallMetadata(installed)
	if err != nil {
		t.Fatalf("no install metadata: %v", err)
	}
	if !filepath.IsAbs(meta.Source) {
		t.Errorf("recorded source = %q, want an absolute path", meta.Source)
	}
}

// A directory is the unpacked form of an archive and lands as-is, hook-free.
func TestPluginInstallLocalDirectory(t *testing.T) {
	pluginsDir := setupPluginTest(t)
	resetPluginInstallArgs(t)

	buildDir := filepath.Join(t.TempDir(), "cub-thing")
	if err := os.MkdirAll(buildDir, 0755); err != nil {
		t.Fatal(err)
	}
	createExecutableFile(t, filepath.Join(buildDir, "main"), "#!/bin/sh\necho hi")
	if err := os.WriteFile(filepath.Join(buildDir, "README.md"), []byte("# thing"), 0644); err != nil {
		t.Fatal(err)
	}

	if err := pluginInstallCmdRun(nil, []string{buildDir}); err != nil {
		t.Fatalf("install failed: %v", err)
	}

	installed := filepath.Join(pluginsDir, "thing")
	for _, f := range []string{"main", "README.md"} {
		if _, err := os.Stat(filepath.Join(installed, f)); err != nil {
			t.Errorf("%s not installed: %v", f, err)
		}
	}
}

// Without a manifest or a "main", the default manifest would name an entrypoint
// that is not there, installing a plugin that only fails when someone runs it.
// Refuse at install time and say what to do instead.
func TestPluginInstallLocalDirectoryNeedsManifestOrMain(t *testing.T) {
	setupPluginTest(t)
	resetPluginInstallArgs(t)

	buildDir := filepath.Join(t.TempDir(), "cub-thing")
	if err := os.MkdirAll(buildDir, 0755); err != nil {
		t.Fatal(err)
	}
	// Named for the plugin, not "main" -- the shape a plain `go build -o` leaves.
	createExecutableFile(t, filepath.Join(buildDir, "thing"), "#!/bin/sh\n")

	err := pluginInstallCmdRun(nil, []string{buildDir})
	if err == nil {
		t.Fatal("expected an error for a directory with no manifest and no main")
	}
	if !strings.Contains(err.Error(), "cub-plugin.yaml") || !strings.Contains(err.Error(), "main") {
		t.Errorf("error should name both accepted shapes, got: %v", err)
	}
}

// The dev loop: rebuild in place, then upgrade. Nothing re-specifies the source.
func TestPluginUpgradeFromLocalPath(t *testing.T) {
	pluginsDir := setupPluginTest(t)
	resetPluginInstallArgs(t)

	buildDir := t.TempDir()
	binPath := filepath.Join(buildDir, "cub-thing")
	createExecutableFile(t, binPath, localHookScript)

	if err := pluginInstallCmdRun(nil, []string{binPath}); err != nil {
		t.Fatalf("install failed: %v", err)
	}

	// Rebuild: same path, different content.
	rebuilt := strings.Replace(localHookScript, `echo "ran: $@"`, `echo "rebuilt: $@"`, 1)
	createExecutableFile(t, binPath, rebuilt)

	if err := upgradeOnePlugin(pluginsDir, "thing", ""); err != nil {
		t.Fatalf("upgrade failed: %v", err)
	}

	content, err := os.ReadFile(filepath.Join(pluginsDir, "thing", "thing"))
	if err != nil {
		t.Fatalf("reading upgraded binary: %v", err)
	}
	if !strings.Contains(string(content), "rebuilt:") {
		t.Error("upgrade did not pick up the rebuilt binary")
	}
}
