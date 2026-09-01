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
	Short: "Install a plugin from a local path, URL, or GitHub repository",
	Long: getCommandHelp("Install a plugin from a local path, URL, or GitHub repository", `Supported source formats:
  https://example.com/plugin.tar.gz   Download and extract tar.gz archive
  https://example.com/plugin           Download as single binary
  https://github.com/org/repo          Install from latest GitHub release
  org/repo                             Shorthand for GitHub
  org/repo@v1.2.0                      Pinned GitHub release tag
  ./bin/cub-thing                      Local binary, built on this machine
  ./dist/thing/                        Local directory (needs cub-plugin.yaml or "main")
  ./dist/thing.tar.gz                  Local archive
  file:///abs/path                     Same, named as a URL

A local source is staged exactly like its published equivalent, so installing a
build tests what a release will do: a local binary runs the same install hook
that writes its cub-plugin.yaml, and a directory or archive lands as-is.

Only a path named as one is treated as local -- ./x, ../x, /x, ~/x, or file://.
A bare "owner/repo" is always GitHub, even if a directory of that name exists.

The resolved absolute path is recorded as the install source, so rebuilding and
re-running 'cub plugin upgrade <name>' reinstalls from the same place:

  go build -o bin/cub-thing ./cmd/thing && cub plugin upgrade thing

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

// localKind is the shape of a plugin on this machine, which decides how it is
// staged. Each one is staged to look exactly like its published equivalent, so
// that what is tested locally is what a release will do.
type localKind int

const (
	// localDir is an unpacked plugin: what a tarball contains, already extracted.
	localDir localKind = iota
	// localTarGz is a built archive, staged exactly as a downloaded one.
	localTarGz
	// localBinary is a freshly built executable, staged exactly as a downloaded
	// release asset -- including running the install hook that writes its
	// manifest. This is the shape a plugin author iterates on.
	localBinary
)

// localSource is a plugin taken from a path on this machine rather than fetched.
//
// path is absolute, so that the value recorded as the install source stays
// meaningful for a later 'cub plugin upgrade' run from a different directory.
type localSource struct {
	path       string
	pluginName string
	kind       localKind
}

// parseInstallSource classifies the source argument.
func parseInstallSource(source string) (any, error) {
	// A path on this machine, named as one.
	if isLocalPath(source) {
		return parseLocalSource(source)
	}

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
		return nil, fmt.Errorf("unsupported URL scheme in %q (only https:// and file:// are supported)", source)
	}

	// owner/repo or owner/repo@tag shorthand
	src, err := parseGitHubShorthand(source)
	if err != nil {
		// "dist" or "dist/plugins/thing" are plausible things to type for a
		// local build, and neither is a valid owner/repo. If one of them names
		// something that is actually here, say so rather than reporting a
		// malformed repository name.
		//
		// Note the limit: exactly two segments IS valid shorthand, so
		// "build/thing" stays GitHub even when it exists locally. That is the
		// rule isLocalPath documents, and the reason a local path must say it
		// is one.
		if _, statErr := os.Stat(source); statErr == nil {
			return nil, fmt.Errorf("%w\n\n%q exists on this machine; to install it from there, name it as a path: ./%s", err, source, strings.TrimPrefix(source, "./"))
		}
		return nil, err
	}
	return src, nil
}

// isLocalPath reports whether source names a path on this machine.
//
// Deliberately syntactic: a "file://" URL, an absolute path, or one explicitly
// relative ("./x", "../x", ".", ".."). A bare "owner/repo" is never local, even
// when a directory of that name happens to sit in the working directory --
// otherwise which one wins would depend on where cub was run from, and the same
// command would install different things in different shells.
func isLocalPath(source string) bool {
	if strings.HasPrefix(source, "file://") {
		return true
	}
	if source == "." || source == ".." || strings.HasPrefix(source, "~/") {
		return true
	}
	if filepath.IsAbs(source) {
		return true
	}
	for _, prefix := range []string{"./", "../", `.\`, `..\`} {
		if strings.HasPrefix(source, prefix) {
			return true
		}
	}
	return false
}

// parseLocalSource resolves a local path and classifies what is there.
//
// The path is resolved to an absolute one here, at the moment the working
// directory is still the user's, because it is recorded as the install source
// and 'cub plugin upgrade' re-reads it from wherever it happens to run.
func parseLocalSource(source string) (*localSource, error) {
	path := strings.TrimPrefix(source, "file://")

	if strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, fmt.Errorf("cannot expand %q: %w", source, err)
		}
		path = filepath.Join(home, path[2:])
	}

	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("cannot resolve %q: %w", source, err)
	}

	info, err := os.Stat(abs)
	if err != nil {
		return nil, fmt.Errorf("cannot install from %q: %w", source, err)
	}

	base := filepath.Base(abs)
	lower := strings.ToLower(base)
	isTarGz := strings.HasSuffix(lower, ".tar.gz") || strings.HasSuffix(lower, ".tgz")

	name := base
	name = strings.TrimSuffix(name, ".tar.gz")
	name = strings.TrimSuffix(name, ".tgz")
	name = strings.TrimSuffix(name, ".exe")
	name = stripCubPrefix(name)
	if name == "" || name == "." || name == ".." {
		return nil, fmt.Errorf("cannot derive a plugin name from %q; pass --name", source)
	}

	src := &localSource{path: abs, pluginName: name}
	switch {
	case info.IsDir():
		src.kind = localDir
	case isTarGz:
		src.kind = localTarGz
	default:
		src.kind = localBinary
	}
	return src, nil
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

// quotedList renders names for an error message: "a", "b" and "c".
func quotedList(names []string) string {
	quoted := make([]string, len(names))
	for i, n := range names {
		quoted[i] = fmt.Sprintf("%q", n)
	}
	switch len(quoted) {
	case 1:
		return quoted[0]
	default:
		return strings.Join(quoted[:len(quoted)-1], ", ") + " and " + quoted[len(quoted)-1]
	}
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
	case *localSource:
		return s.pluginName
	}
	return ""
}

// sourceToRecord is what gets written into the install metadata, which is what
// 'cub plugin upgrade' re-parses later. For a local path that is the resolved
// absolute one rather than what was typed, because upgrade will not be run from
// the same working directory.
func sourceToRecord(src any, typed string) string {
	if s, ok := src.(*localSource); ok {
		return s.path
	}
	return typed
}

// stageLocal copies a plugin from this machine into a staging directory.
//
// Each shape is staged to look exactly like its published equivalent, so that
// installing a local build exercises the same code as installing a release: a
// directory or an archive lands as-is and carries its own manifest, while a bare
// binary gets the install hook that writes one.
func stageLocal(s *localSource, name, parentDir string) (staged, binPath string, err error) {
	if s.kind == localTarGz {
		f, oerr := os.Open(s.path)
		if oerr != nil {
			return "", "", oerr
		}
		defer f.Close()
		staged, err = stageTarGzReader(f, parentDir)
		return staged, "", err
	}

	staged, err = os.MkdirTemp(parentDir, ".plugin-install-*")
	if err != nil {
		return "", "", err
	}
	fail := func(err error) (string, string, error) {
		os.RemoveAll(staged)
		return "", "", err
	}

	switch s.kind {
	case localDir:
		// A directory is the unpacked form of a tarball and is held to the same
		// contract: it brings its own manifest, or it contains the conventional
		// "main". Checked here rather than left to the default manifest, which
		// would name an entrypoint that does not exist and install a plugin that
		// only fails when someone runs it.
		m, merr := coreplugin.Read(s.path)
		if merr != nil {
			return fail(fmt.Errorf("reading %s: %w", filepath.Join(s.path, "cub-plugin.yaml"), merr))
		}
		if m == nil {
			if _, statErr := os.Stat(filepath.Join(s.path, "main")); statErr != nil {
				return fail(fmt.Errorf("%s has no cub-plugin.yaml and no executable named \"main\", so cub cannot tell what to run.\n\nTo install a binary you just built, name the binary itself -- its install hook writes the manifest:\n  cub plugin install %s",
					s.path, filepath.Join(s.path, "<binary>")))
			}
		}
		entries, rerr := os.ReadDir(s.path)
		if rerr != nil {
			return fail(rerr)
		}
		for _, e := range entries {
			// Skip the previous install's metadata: it records where that copy
			// came from, and this copy came from somewhere else.
			if e.Name() == installMetadataFile {
				continue
			}
			if err := copyPath(filepath.Join(s.path, e.Name()), filepath.Join(staged, e.Name())); err != nil {
				return fail(err)
			}
		}

	case localBinary:
		binPath = filepath.Join(staged, name)
		if err := copyPath(s.path, binPath); err != nil {
			return fail(err)
		}
		if err := os.Chmod(binPath, 0755); err != nil {
			return fail(err)
		}
	}

	return staged, binPath, nil
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
	shipped := m != nil
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

	// Make declared entrypoints executable, and refuse one that is not there.
	//
	// A manifest names the file to exec. Installing when that file is absent
	// reports success and produces a plugin that fails the first time anyone
	// runs it, with "unknown command" — which describes cub, not the install
	// that went wrong several steps earlier.
	//
	// The common way in is a repository with no releases: the source is
	// installed instead, no manifest comes with it, and the default one names
	// "main", which a Go project does not have.
	var missing []string
	for _, c := range cmds {
		ep := c.Entrypoint
		if ep == "" {
			ep = "main"
		}
		epPath := filepath.Join(staged, ep)
		info, statErr := os.Stat(epPath)
		if statErr != nil || info.IsDir() {
			missing = append(missing, ep)
			continue
		}
		if err := os.Chmod(epPath, 0755); err != nil {
			return fmt.Errorf("failed to set permissions on %s: %w", ep, err)
		}
	}
	if len(missing) > 0 {
		if shipped {
			return fmt.Errorf("this plugin's cub-plugin.yaml names %s, which it does not contain, so there is nothing to run",
				quotedList(missing))
		}
		return fmt.Errorf("nothing here is runnable: no cub-plugin.yaml, and no %s to fall back on.\n"+
			"    This is what installing a repository's source rather than a release looks like.\n"+
			"    A plugin needs either a release carrying a built binary, or a committed cub-plugin.yaml",
			quotedList(missing))
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

	if err := finalizeStaging(staged, destPath, name, name, sourceToRecord(src, source), tag, defaultEntrypoint(binPath)); err != nil {
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

	// A local path is already on this machine; there is nothing to resolve, no
	// platform to match, and no release tag.
	if s, ok := src.(*localSource); ok {
		tprint("Installing from %s...", s.path)
		staged, binPath, err = stageLocal(s, name, parentDir)
		return staged, "", binPath, err
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
