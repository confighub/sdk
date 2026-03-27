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
	"strings"
	"sync"
	"testing"
)

// setupPluginTest creates a temporary plugin directory and initializes a minimal
// context manager pointing at it. It resets the plugin discovery cache.
func setupPluginTest(t *testing.T) string {
	t.Helper()
	tmpDir := t.TempDir()
	pluginsDir := filepath.Join(tmpDir, "plugins")
	if err := os.MkdirAll(pluginsDir, 0755); err != nil {
		t.Fatal(err)
	}

	// Set up a minimal context manager so pluginDir() works.
	contextManager = &ContextManager{
		configPath: filepath.Join(tmpDir, "config.yaml"),
	}

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
	if p.Path != filepath.Join(dirPath, "main") {
		t.Errorf("unexpected path: %s", p.Path)
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
	found := false
	for _, w := range p.Warnings {
		if w == "directory plugin missing executable 'main'" {
			found = true
		}
	}
	if !found {
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
	found := false
	for _, w := range p.Warnings {
		if w == "not executable" {
			found = true
		}
	}
	if !found {
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
	found := false
	for _, w := range p.Warnings {
		if w == `shadows built-in command "version"` {
			found = true
		}
	}
	if !found {
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
	configPath := filepath.Join(tmpDir, "config.yaml")
	tokenDir := filepath.Join(tmpDir, "tokens")
	if err := os.MkdirAll(tokenDir, 0700); err != nil {
		t.Fatal(err)
	}

	contextManager = &ContextManager{
		configPath: configPath,
		tokenDir:   tokenDir,
		config: &Config{
			APIVersion:     "v1",
			Kind:           "Config",
			CurrentContext: "test-ctx",
			Contexts: []*Context{
				{
					Name: "test-ctx",
					Coordinate: Coordinate{
						ServerURL: "https://example.com",
					},
					Settings: Settings{
						DefaultSpace: "prod",
					},
					Metadata: Metadata{
						TokenFile: filepath.Join(tokenDir, "test-ctx.json"),
					},
				},
			},
		},
	}

	// Write a token file
	tokenContent := `{"accessToken":"test-token-123"}`
	if err := os.WriteFile(filepath.Join(tokenDir, "test-ctx.json"), []byte(tokenContent), 0600); err != nil {
		t.Fatal(err)
	}

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

	err := downloadBinary(server.URL+"/my-plugin", filepath.Join(pluginsDir, "hello"))
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

	destPath := filepath.Join(pluginsDir, "myplugin")
	err := downloadAndExtractTarGz(server.URL+"/plugin.tar.gz", destPath)
	if err != nil {
		t.Fatalf("downloadAndExtractTarGz failed: %v", err)
	}

	// Verify directory structure
	info, err := os.Stat(destPath)
	if err != nil {
		t.Fatalf("plugin dir not found: %v", err)
	}
	if !info.IsDir() {
		t.Error("expected directory")
	}

	mainPath := filepath.Join(destPath, "main")
	mainInfo, err := os.Stat(mainPath)
	if err != nil {
		t.Fatalf("main not found: %v", err)
	}
	if mainInfo.Mode()&0111 == 0 {
		t.Error("expected main to be executable")
	}

	readmePath := filepath.Join(destPath, "README.md")
	if _, err := os.Stat(readmePath); err != nil {
		t.Fatalf("README.md not found: %v", err)
	}
}

func TestPluginInstallExistingNoForce(t *testing.T) {
	pluginsDir := setupPluginTest(t)

	// Create an existing plugin
	createExecutableFile(t, filepath.Join(pluginsDir, "existing"), "#!/bin/sh\necho old")

	// Try to install over it without --force
	destPath := filepath.Join(pluginsDir, "existing")
	if _, err := os.Stat(destPath); err != nil {
		t.Fatal("expected existing plugin to exist")
	}

	// Simulate the check that pluginInstallCmdRun does
	if _, err := os.Stat(destPath); err == nil {
		// Plugin exists and force is false — this should be an error
		err = fmt.Errorf("plugin %q already exists; use --force to overwrite", "existing")
		if err == nil {
			t.Fatal("expected error for existing plugin without --force")
		}
		if !strings.Contains(err.Error(), "already exists") {
			t.Errorf("unexpected error: %v", err)
		}
	}
}

func TestPluginInstallExistingWithForce(t *testing.T) {
	pluginsDir := setupPluginTest(t)

	// Create an existing plugin
	createExecutableFile(t, filepath.Join(pluginsDir, "existing"), "#!/bin/sh\necho old")

	newContent := "#!/bin/sh\necho new"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(newContent))
	}))
	defer server.Close()

	destPath := filepath.Join(pluginsDir, "existing")

	// Remove old and install new (simulating --force)
	if err := os.RemoveAll(destPath); err != nil {
		t.Fatal(err)
	}
	if err := downloadBinary(server.URL+"/existing", destPath); err != nil {
		t.Fatalf("downloadBinary failed: %v", err)
	}

	content, err := os.ReadFile(destPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != newContent {
		t.Errorf("content = %q, want %q", string(content), newContent)
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
	if err := downloadBinary(asset.BrowserDownloadURL, destPath); err != nil {
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

	destPath := filepath.Join(pluginsDir, "myplugin")
	err := downloadAndExtractTarGz(server.URL+"/plugin.tar.gz", destPath)
	if err != nil {
		t.Fatalf("downloadAndExtractTarGz failed: %v", err)
	}

	// The top-level "myrepo-v1.0.0" should have been stripped
	mainPath := filepath.Join(destPath, "main")
	if _, err := os.Stat(mainPath); err != nil {
		t.Fatalf("main not found after stripping top dir: %v", err)
	}

	readmePath := filepath.Join(destPath, "README.md")
	if _, err := os.Stat(readmePath); err != nil {
		t.Fatalf("README.md not found after stripping top dir: %v", err)
	}
}
