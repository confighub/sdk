// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"archive/tar"
	"compress/gzip"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	coreplugin "github.com/confighub/sdk/core/plugin"
	"github.com/spf13/cobra"
)

// githubAPIBaseURL is the base URL for GitHub API calls. Tests override this.
var githubAPIBaseURL = "https://api.github.com"

// installMetadataFile records where a plugin came from so it can be upgraded
// without re-specifying the source. It is cub-owned and kept separate from the
// hook-regenerated cub-plugin.yaml.
const installMetadataFile = ".cub-plugin-install.json"

type installMetadata struct {
	Source      string `json:"source"`
	Tag         string `json:"tag,omitempty"`
	InstalledAt string `json:"installedAt,omitempty"`
}

func writeInstallMetadata(dir, source, tag string) error {
	m := installMetadata{
		Source:      source,
		Tag:         tag,
		InstalledAt: time.Now().UTC().Format(time.RFC3339),
	}
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, installMetadataFile), data, 0644)
}

func readInstallMetadata(dir string) (*installMetadata, error) {
	data, err := os.ReadFile(filepath.Join(dir, installMetadataFile))
	if err != nil {
		return nil, err
	}
	var m installMetadata
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("invalid %s: %w", installMetadataFile, err)
	}
	return &m, nil
}

var pluginInstallArgs struct {
	name       string
	platform   string
	sourceRepo bool
}

var pluginInstallCmd = &cobra.Command{
	Use:   "install <source>",
	Short: "Install a plugin from a URL or GitHub repository",
	Long: getCommandHelp("Install a plugin from a URL or GitHub repository", `Supported source formats:
  https://example.com/plugin.tar.gz   Download and extract tar.gz archive
  https://example.com/plugin           Download as single binary
  https://github.com/org/repo          Install from latest GitHub release
  org/repo                             Shorthand for GitHub
  org/repo@v1.2.0                      Pinned GitHub release tag

For repositories without releases, the repo source is downloaded automatically.
Use --source-repo to force this behavior even when releases exist:
  cub plugin install org/repo --source-repo
  cub plugin install org/repo@branch --source-repo

If the plugin is already installed, install fails. Use 'cub plugin upgrade' to
update it, or 'cub plugin uninstall' first to reinstall fresh.`),
	Args: cobra.ExactArgs(1),
	RunE: pluginInstallCmdRun,
}

func init() {
	pluginInstallCmd.Flags().StringVar(&pluginInstallArgs.name, "name", "", "Override the plugin name")
	pluginInstallCmd.Flags().StringVar(&pluginInstallArgs.platform, "platform", "", "Target platform (e.g. linux/amd64)")
	pluginInstallCmd.Flags().BoolVar(&pluginInstallArgs.sourceRepo, "source-repo", false, "Force install from repository source even if releases exist")
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

// errNoReleases is returned when a GitHub repo has no releases.
var errNoReleases = fmt.Errorf("no releases found")

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
		return nil, fmt.Errorf("%s/%s: %w", src.owner, src.repo, errNoReleases)
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

// repoTarballURL returns the GitHub API URL for downloading a repo tarball.
// If ref is empty, the default branch is used.
func repoTarballURL(src *githubSource) string {
	ref := src.tag
	if ref == "" {
		ref = "HEAD"
	}
	return fmt.Sprintf("%s/repos/%s/%s/tarball/%s", githubAPIBaseURL, src.owner, src.repo, ref)
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

// --- download / extraction primitives -------------------------------------

// httpGet performs a GET and returns the response body, erroring on non-200.
func httpGet(url string) (*http.Response, error) {
	client := &http.Client{Timeout: 5 * time.Minute}
	resp, err := client.Get(url)
	if err != nil {
		return nil, fmt.Errorf("download failed: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, fmt.Errorf("download failed: %s", resp.Status)
	}
	return resp, nil
}

// downloadPluginBinary downloads url to destPath and makes it executable.
func downloadPluginBinary(url, destPath string) error {
	resp, err := httpGet(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	f, err := os.OpenFile(destPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0755)
	if err != nil {
		return err
	}
	if _, err := io.Copy(f, resp.Body); err != nil {
		f.Close()
		return fmt.Errorf("download failed: %w", err)
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Chmod(destPath, 0755)
}

// strippedDir returns the inner directory if dir contains exactly one entry that
// is a directory (the wrapper directory GitHub archives create), else dir.
func strippedDir(dir string) string {
	entries, err := os.ReadDir(dir)
	if err == nil && len(entries) == 1 && entries[0].IsDir() {
		return filepath.Join(dir, entries[0].Name())
	}
	return dir
}

// stageTarGz downloads and extracts a tar.gz from url into a new staging
// directory under parentDir, stripping a single wrapper directory. The caller
// owns the returned directory.
func stageTarGz(url, parentDir string) (string, error) {
	resp, err := httpGet(url)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	return stageTarGzReader(resp.Body, parentDir)
}

// stageTarGzReader extracts a gzipped tar from r into a new staging directory.
func stageTarGzReader(r io.Reader, parentDir string) (string, error) {
	wrapper, err := os.MkdirTemp(parentDir, ".plugin-extract-*")
	if err != nil {
		return "", fmt.Errorf("failed to create temp directory: %w", err)
	}
	defer os.RemoveAll(wrapper)

	if err := extractTarGz(r, wrapper); err != nil {
		return "", fmt.Errorf("extraction failed: %w", err)
	}

	src := strippedDir(wrapper)
	staged, err := os.MkdirTemp(parentDir, ".plugin-install-*")
	if err != nil {
		return "", err
	}
	// MkdirTemp made an empty dir; replace it with the extracted content.
	if err := os.RemoveAll(staged); err != nil {
		return "", err
	}
	if err := os.Rename(src, staged); err != nil {
		return "", fmt.Errorf("extraction failed: %w", err)
	}
	return staged, nil
}

// stageRepoTarball downloads a GitHub repo tarball and stages it.
func stageRepoTarball(src *githubSource, parentDir string) (string, error) {
	url := repoTarballURL(src)

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "cub-cli")
	if token := os.Getenv("GITHUB_TOKEN"); token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	client := &http.Client{Timeout: 5 * time.Minute}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("download failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		if src.tag == "" {
			return "", fmt.Errorf("repository %s/%s not found", src.owner, src.repo)
		}
		return "", fmt.Errorf("repository %s/%s not found or ref %q does not exist", src.owner, src.repo, src.tag)
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("GitHub API returned %s", resp.Status)
	}

	return stageTarGzReader(resp.Body, parentDir)
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

// --- staging finalization --------------------------------------------------

// effectiveCommands returns the commands a manifest contributes, applying the
// legacy single-command fallback for manifests with no commands list.
func effectiveCommands(slot string, m *coreplugin.Manifest) []coreplugin.Command {
	if m != nil && len(m.Commands) > 0 {
		return m.Commands
	}
	ep := "main"
	if m != nil && m.Entrypoint != "" {
		ep = m.Entrypoint
	}
	return []coreplugin.Command{{Name: slot, Entrypoint: ep}}
}

// defaultEntrypoint chooses the entrypoint for the default manifest written when
// a plugin ships no cub-plugin.yaml. A bare binary was saved by the installer
// under a known filename, so use that; a tarball or repo source must contain an
// executable named "main" (the conventional entry point).
func defaultEntrypoint(binPath string) string {
	if binPath != "" {
		return filepath.Base(binPath)
	}
	return "main"
}

// finalizeStaging ensures the staged directory has a manifest, checks for
// command collisions, records install metadata, makes entrypoints executable,
// and atomically promotes it to destPath. excludeSlot is excluded from the
// collision namespace (used on upgrade so a plugin doesn't collide with itself).
func finalizeStaging(staged, destPath, slot, excludeSlot, source, tag string, defaultEntrypoint string) error {
	m, err := coreplugin.Read(staged)
	if err != nil {
		return err
	}
	if m == nil {
		// No manifest produced — write the default single-command manifest.
		def := coreplugin.Manifest{Commands: []coreplugin.Command{{Name: slot, Entrypoint: defaultEntrypoint}}}
		if err := coreplugin.Write(staged, def); err != nil {
			return err
		}
		m = &def
	}

	cmds := effectiveCommands(slot, m)
	if err := checkCommandCollisions(excludeSlot, cmds); err != nil {
		return err
	}

	// Make declared entrypoints executable.
	for _, c := range cmds {
		ep := c.Entrypoint
		if ep == "" {
			ep = "main"
		}
		epPath := filepath.Join(staged, ep)
		if info, statErr := os.Stat(epPath); statErr == nil && !info.IsDir() {
			if err := os.Chmod(epPath, 0755); err != nil {
				return fmt.Errorf("failed to set permissions on %s: %w", ep, err)
			}
		}
	}

	if source != "" {
		if err := writeInstallMetadata(staged, source, tag); err != nil {
			return err
		}
	}

	if err := os.Rename(staged, destPath); err != nil {
		return fmt.Errorf("failed to install plugin: %w", err)
	}
	return nil
}

func pluginInstallCmdRun(cmd *cobra.Command, args []string) error {
	source := args[0]

	// 1. Parse source
	src, err := parseInstallSource(source)
	if err != nil {
		return err
	}

	// 2. Determine plugin name
	name := pluginInstallArgs.name
	if name == "" {
		name = derivePluginName(src)
	}
	if name == "" {
		return fmt.Errorf("cannot determine plugin name; use --name to specify one")
	}

	// 3. Ensure plugin directory exists
	dir := pluginDir()
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create plugin directory: %w", err)
	}

	// 4. Refuse to overwrite an existing plugin
	destPath := filepath.Join(dir, name)
	if _, err := os.Stat(destPath); err == nil {
		return fmt.Errorf("plugin %q is already installed; run 'cub plugin upgrade %s' to update it, or 'cub plugin uninstall %s' first to reinstall fresh", name, name, name)
	}

	// 5. Stage the plugin into a temp directory, run the install hook, finalize.
	staged, tag, binPath, err := fetchPlugin(src, name, dir)
	if err != nil {
		return err
	}
	defer os.RemoveAll(staged)

	if binPath != "" {
		runHook(binPath, staged, coreplugin.HookInstall, "")
	}

	if err := finalizeStaging(staged, destPath, name, name, source, tag, defaultEntrypoint(binPath)); err != nil {
		return err
	}

	tprint("Installed plugin %q to %s", name, destPath)
	return nil
}

// fetchPlugin fetches the plugin into a staging directory. It returns the
// staging dir, the resolved release tag (empty for non-release sources), and —
// for a bare-binary source — the path to the downloaded binary so the caller can
// run the appropriate hook (install or upgrade). binPath is empty for archive
// and repo-source plugins, which carry their own committed manifest.
func fetchPlugin(src any, name, parentDir string) (staged, tag, binPath string, err error) {
	// --source-repo: download repo tarball directly.
	if pluginInstallArgs.sourceRepo {
		ghSrc, ok := src.(*githubSource)
		if !ok {
			return "", "", "", fmt.Errorf("--source-repo can only be used with GitHub sources (org/repo)")
		}
		ref := ghSrc.tag
		if ref == "" {
			ref = "default branch"
		}
		tprint("Downloading %s/%s (%s)...", ghSrc.owner, ghSrc.repo, ref)
		staged, err = stageRepoTarball(ghSrc, parentDir)
		return staged, ghSrc.tag, "", err
	}

	targetOS, targetArch, err := resolvePlatform(pluginInstallArgs.platform)
	if err != nil {
		return "", "", "", err
	}

	var downloadURL string
	var isTarGz bool

	switch s := src.(type) {
	case *directURLSource:
		downloadURL = s.url
		isTarGz = s.isTarGz
	case *githubSource:
		release, rerr := resolveGitHubRelease(s)
		if rerr != nil {
			if errors.Is(rerr, errNoReleases) {
				tprint("No releases found for %s/%s, installing from repository source...", s.owner, s.repo)
				staged, err = stageRepoTarball(s, parentDir)
				return staged, s.tag, "", err
			}
			return "", "", "", rerr
		}
		asset, aerr := matchAsset(release.Assets, targetOS, targetArch)
		if aerr != nil {
			return "", "", "", aerr
		}
		downloadURL = asset.BrowserDownloadURL
		lower := strings.ToLower(asset.Name)
		isTarGz = strings.HasSuffix(lower, ".tar.gz") || strings.HasSuffix(lower, ".tgz")
		tag = release.TagName
		tprint("Found release %s, asset %s", release.TagName, asset.Name)
	}

	tprint("Downloading %s...", downloadURL)
	if isTarGz {
		staged, err = stageTarGz(downloadURL, parentDir)
		return staged, tag, "", err
	}

	// Bare binary: stage as a directory containing the binary. The caller runs
	// the install/upgrade hook against the returned binPath.
	staged, err = os.MkdirTemp(parentDir, ".plugin-install-*")
	if err != nil {
		return "", "", "", err
	}
	binPath = filepath.Join(staged, name)
	if err := downloadPluginBinary(downloadURL, binPath); err != nil {
		os.RemoveAll(staged)
		return "", "", "", err
	}
	return staged, tag, binPath, nil
}
