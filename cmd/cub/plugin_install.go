// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"archive/tar"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

// githubAPIBaseURL is the base URL for GitHub API calls. Tests override this.
var githubAPIBaseURL = "https://api.github.com"

var pluginInstallArgs struct {
	name     string
	force    bool
	platform string
}

var pluginInstallCmd = &cobra.Command{
	Use:   "install <source>",
	Short: "Install a plugin from a URL or GitHub repository",
	Long: getCommandHelp("Install a plugin from a URL or GitHub repository", `Supported source formats:
  https://example.com/plugin.tar.gz   Download and extract tar.gz archive
  https://example.com/plugin           Download as single binary
  https://github.com/org/repo          Install from latest GitHub release
  org/repo                             Shorthand for GitHub
  org/repo@v1.2.0                      Pinned GitHub release tag`),
	Args: cobra.ExactArgs(1),
	RunE: pluginInstallCmdRun,
}

func init() {
	pluginInstallCmd.Flags().StringVar(&pluginInstallArgs.name, "name", "", "Override the plugin name")
	pluginInstallCmd.Flags().BoolVar(&pluginInstallArgs.force, "force", false, "Overwrite an existing plugin")
	pluginInstallCmd.Flags().StringVar(&pluginInstallArgs.platform, "platform", "", "Target platform (e.g. linux/amd64)")
	pluginCmd.AddCommand(pluginInstallCmd)
}

// installSource types returned by parseInstallSource.
type directURLSource struct {
	url         string
	derivedName string
	isTarGz     bool
}

type githubSource struct {
	owner      string
	repo       string
	tag        string
	pluginName string
}

// parseInstallSource classifies the source argument.
func parseInstallSource(source string) (any, error) {
	// HTTPS URLs
	if strings.HasPrefix(source, "https://") {
		// Check if it's a GitHub URL
		if isGitHubURL(source) {
			return parseGitHubURL(source)
		}
		return parseDirectURL(source)
	}

	// Reject other URL schemes
	if strings.Contains(source, "://") {
		return nil, fmt.Errorf("unsupported URL scheme in %q (only https:// is supported)", source)
	}

	// owner/repo or owner/repo@tag shorthand
	return parseGitHubShorthand(source)
}

func isGitHubURL(url string) bool {
	return strings.HasPrefix(url, "https://github.com/")
}

func parseGitHubURL(url string) (*githubSource, error) {
	// Strip https://github.com/
	path := strings.TrimPrefix(url, "https://github.com/")
	path = strings.TrimSuffix(path, "/")
	path = strings.TrimSuffix(path, ".git")

	parts := strings.SplitN(path, "/", 3)
	if len(parts) < 2 || parts[0] == "" || parts[1] == "" {
		return nil, fmt.Errorf("invalid GitHub URL %q: expected https://github.com/owner/repo", url)
	}

	repo := parts[1]
	return &githubSource{
		owner:      parts[0],
		repo:       repo,
		pluginName: stripCubPrefix(repo),
	}, nil
}

func parseDirectURL(url string) (*directURLSource, error) {
	// Extract the last path segment for the name
	lastSlash := strings.LastIndex(url, "/")
	if lastSlash == -1 || lastSlash == len(url)-1 {
		return nil, fmt.Errorf("cannot derive plugin name from URL %q", url)
	}
	segment := url[lastSlash+1:]

	// Strip query string if present
	if idx := strings.Index(segment, "?"); idx != -1 {
		segment = segment[:idx]
	}

	isTarGz := strings.HasSuffix(segment, ".tar.gz") || strings.HasSuffix(segment, ".tgz")

	name := segment
	name = strings.TrimSuffix(name, ".tar.gz")
	name = strings.TrimSuffix(name, ".tgz")
	name = strings.TrimSuffix(name, ".exe")

	if name == "" {
		return nil, fmt.Errorf("cannot derive plugin name from URL %q", url)
	}

	return &directURLSource{
		url:         url,
		derivedName: name,
		isTarGz:     isTarGz,
	}, nil
}

func parseGitHubShorthand(source string) (*githubSource, error) {
	// Split off @tag if present
	var tag string
	atIdx := strings.LastIndex(source, "@")
	path := source
	if atIdx != -1 {
		tag = source[atIdx+1:]
		path = source[:atIdx]
		if tag == "" {
			return nil, fmt.Errorf("invalid source %q: empty tag after @", source)
		}
	}

	parts := strings.SplitN(path, "/", 3)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return nil, fmt.Errorf("invalid source %q: expected owner/repo or owner/repo@tag", source)
	}

	return &githubSource{
		owner:      parts[0],
		repo:       parts[1],
		tag:        tag,
		pluginName: stripCubPrefix(parts[1]),
	}, nil
}

func stripCubPrefix(name string) string {
	return strings.TrimPrefix(name, "cub-")
}

// resolvePlatform parses the --platform flag or defaults to runtime values.
func resolvePlatform(platform string) (string, string, error) {
	if platform == "" {
		return runtime.GOOS, runtime.GOARCH, nil
	}
	parts := strings.SplitN(platform, "/", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", fmt.Errorf("invalid platform %q: expected os/arch (e.g. linux/amd64)", platform)
	}
	return parts[0], parts[1], nil
}

// GitHub API types (minimal).
type ghRelease struct {
	TagName string    `json:"tag_name"`
	Assets  []ghAsset `json:"assets"`
}

type ghAsset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
}

// resolveGitHubRelease fetches release metadata from the GitHub API.
func resolveGitHubRelease(src *githubSource) (*ghRelease, error) {
	var url string
	if src.tag != "" {
		url = fmt.Sprintf("%s/repos/%s/%s/releases/tags/%s", githubAPIBaseURL, src.owner, src.repo, src.tag)
	} else {
		url = fmt.Sprintf("%s/repos/%s/%s/releases/latest", githubAPIBaseURL, src.owner, src.repo)
	}

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "cub-cli")

	if token := os.Getenv("GITHUB_TOKEN"); token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to query GitHub API: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		if src.tag != "" {
			return nil, fmt.Errorf("release %q not found for %s/%s", src.tag, src.owner, src.repo)
		}
		return nil, fmt.Errorf("no releases found for %s/%s", src.owner, src.repo)
	}
	if resp.StatusCode == http.StatusForbidden {
		return nil, fmt.Errorf("GitHub API rate limit exceeded; set GITHUB_TOKEN to authenticate")
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GitHub API returned %s", resp.Status)
	}

	var release ghRelease
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		return nil, fmt.Errorf("failed to parse GitHub release: %w", err)
	}
	return &release, nil
}

// matchAsset selects the best asset for the target platform.
func matchAsset(assets []ghAsset, targetOS, targetArch string) (*ghAsset, error) {
	osAliases := platformAliases(targetOS)
	archAliases := platformAliases(targetArch)

	// Filter out signature/checksum files
	skipSuffixes := []string{".sha256", ".sha512", ".sig", ".asc", ".sbom", ".pem"}

	var candidates []ghAsset
	for _, a := range assets {
		lower := strings.ToLower(a.Name)

		skip := false
		for _, suffix := range skipSuffixes {
			if strings.HasSuffix(lower, suffix) {
				skip = true
				break
			}
		}
		if skip {
			continue
		}

		hasOS := false
		for _, alias := range osAliases {
			if containsWord(lower, strings.ToLower(alias)) {
				hasOS = true
				break
			}
		}

		hasArch := false
		for _, alias := range archAliases {
			if containsWord(lower, strings.ToLower(alias)) {
				hasArch = true
				break
			}
		}

		if hasOS && hasArch {
			candidates = append(candidates, a)
		}
	}

	if len(candidates) == 0 {
		return nil, fmt.Errorf("no release asset found for %s/%s", targetOS, targetArch)
	}

	// Prefer tar.gz/tgz archives
	for i, c := range candidates {
		lower := strings.ToLower(c.Name)
		if strings.HasSuffix(lower, ".tar.gz") || strings.HasSuffix(lower, ".tgz") {
			return &candidates[i], nil
		}
	}

	return &candidates[0], nil
}

// platformAliases returns the set of known aliases for an os or arch value.
func platformAliases(value string) []string {
	aliases := map[string][]string{
		"darwin": {"darwin", "macos", "macOS", "Darwin"},
		"linux":  {"linux", "Linux"},
		"amd64":  {"amd64", "x86_64", "x86-64"},
		"arm64":  {"arm64", "aarch64"},
		"386":    {"386", "i386", "i686"},
	}
	if a, ok := aliases[value]; ok {
		return a
	}
	return []string{value}
}

// containsWord checks if name contains the word, handling common separators.
func containsWord(name, word string) bool {
	return strings.Contains(name, word)
}

// downloadBinary downloads a URL to destPath atomically.
func downloadBinary(url, destPath string) error {
	client := &http.Client{Timeout: 5 * time.Minute}

	resp, err := client.Get(url)
	if err != nil {
		return fmt.Errorf("download failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download failed: %s", resp.Status)
	}

	dir := filepath.Dir(destPath)
	tmpFile, err := os.CreateTemp(dir, ".plugin-download-*")
	if err != nil {
		return fmt.Errorf("failed to create temp file: %w", err)
	}
	tmpPath := tmpFile.Name()
	defer func() {
		tmpFile.Close()
		os.Remove(tmpPath)
	}()

	if _, err := io.Copy(tmpFile, resp.Body); err != nil {
		return fmt.Errorf("download failed: %w", err)
	}
	if err := tmpFile.Close(); err != nil {
		return err
	}

	if err := os.Chmod(tmpPath, 0755); err != nil {
		return fmt.Errorf("failed to set permissions: %w", err)
	}

	if err := os.Rename(tmpPath, destPath); err != nil {
		return fmt.Errorf("failed to install plugin: %w", err)
	}

	return nil
}

// downloadAndExtractTarGz downloads a tar.gz URL and extracts it to destPath.
func downloadAndExtractTarGz(url, destPath string) error {
	client := &http.Client{Timeout: 5 * time.Minute}

	resp, err := client.Get(url)
	if err != nil {
		return fmt.Errorf("download failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download failed: %s", resp.Status)
	}

	// Extract to a temp directory next to the final destination
	dir := filepath.Dir(destPath)
	tmpDir, err := os.MkdirTemp(dir, ".plugin-extract-*")
	if err != nil {
		return fmt.Errorf("failed to create temp directory: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	if err := extractTarGz(resp.Body, tmpDir); err != nil {
		return fmt.Errorf("extraction failed: %w", err)
	}

	// Check if there's a single top-level directory to strip
	extractedDir := tmpDir
	entries, err := os.ReadDir(tmpDir)
	if err != nil {
		return err
	}
	if len(entries) == 1 && entries[0].IsDir() {
		extractedDir = filepath.Join(tmpDir, entries[0].Name())
	}

	if err := os.Rename(extractedDir, destPath); err != nil {
		return fmt.Errorf("failed to install plugin: %w", err)
	}

	// Ensure main entry point is executable if present
	mainPath := filepath.Join(destPath, "main")
	if info, statErr := os.Stat(mainPath); statErr == nil && !info.IsDir() {
		os.Chmod(mainPath, 0755)
	}

	return nil
}

// extractTarGz extracts a gzipped tar archive from r into destDir.
func extractTarGz(r io.Reader, destDir string) error {
	gz, err := gzip.NewReader(r)
	if err != nil {
		return fmt.Errorf("gzip error: %w", err)
	}
	defer gz.Close()

	tr := tar.NewReader(gz)
	for {
		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("tar error: %w", err)
		}

		// Security: prevent path traversal
		cleanName := filepath.Clean(header.Name)
		if strings.HasPrefix(cleanName, "..") || strings.HasPrefix(cleanName, "/") {
			continue
		}

		target := filepath.Join(destDir, cleanName)

		// Verify the target is within destDir
		if !strings.HasPrefix(filepath.Clean(target), filepath.Clean(destDir)) {
			continue
		}

		switch header.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, os.FileMode(header.Mode)|0755); err != nil {
				return err
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
				return err
			}
			f, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, os.FileMode(header.Mode))
			if err != nil {
				return err
			}
			if _, err := io.Copy(f, tr); err != nil {
				f.Close()
				return err
			}
			f.Close()
		}
	}
	return nil
}

// derivePluginName determines the plugin name from the source and flags.
func derivePluginName(src any) string {
	switch s := src.(type) {
	case *directURLSource:
		return s.derivedName
	case *githubSource:
		return s.pluginName
	}
	return ""
}

func pluginInstallCmdRun(cmd *cobra.Command, args []string) error {
	source := args[0]

	// 1. Parse source
	src, err := parseInstallSource(source)
	if err != nil {
		return err
	}

	// 2. Resolve platform
	targetOS, targetArch, err := resolvePlatform(pluginInstallArgs.platform)
	if err != nil {
		return err
	}

	// 3. Determine download URL and whether it's a tar.gz
	var downloadURL string
	var isTarGz bool

	switch s := src.(type) {
	case *directURLSource:
		downloadURL = s.url
		isTarGz = s.isTarGz
	case *githubSource:
		release, err := resolveGitHubRelease(s)
		if err != nil {
			return err
		}
		asset, err := matchAsset(release.Assets, targetOS, targetArch)
		if err != nil {
			return err
		}
		downloadURL = asset.BrowserDownloadURL
		lower := strings.ToLower(asset.Name)
		isTarGz = strings.HasSuffix(lower, ".tar.gz") || strings.HasSuffix(lower, ".tgz")
		tprint("Found release %s, asset %s", release.TagName, asset.Name)
	}

	// 4. Determine plugin name
	name := pluginInstallArgs.name
	if name == "" {
		name = derivePluginName(src)
	}
	if name == "" {
		return fmt.Errorf("cannot determine plugin name; use --name to specify one")
	}

	// 5. Ensure plugin directory exists
	dir := pluginDir()
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create plugin directory: %w", err)
	}

	// 6. Check for existing plugin
	destPath := filepath.Join(dir, name)
	if _, err := os.Stat(destPath); err == nil {
		if !pluginInstallArgs.force {
			return fmt.Errorf("plugin %q already exists; use --force to overwrite", name)
		}
		if err := os.RemoveAll(destPath); err != nil {
			return fmt.Errorf("failed to remove existing plugin: %w", err)
		}
	}

	// 7. Download and install
	tprint("Downloading %s...", downloadURL)
	if isTarGz {
		if err := downloadAndExtractTarGz(downloadURL, destPath); err != nil {
			return err
		}
	} else {
		if err := downloadBinary(downloadURL, destPath); err != nil {
			return err
		}
	}

	tprint("Installed plugin %q to %s", name, destPath)
	return nil
}
