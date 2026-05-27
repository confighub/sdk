// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	coreplugin "github.com/confighub/sdk/core/plugin"
	"github.com/spf13/cobra"
)

var pluginUpgradeArgs struct {
	all      bool
	platform string
}

var pluginUpgradeCmd = &cobra.Command{
	Use:   "upgrade [name[@tag]]",
	Short: "Upgrade an installed plugin to the latest release",
	Long: getCommandHelp("Upgrade an installed plugin", `Re-fetches the plugin from the source it was installed from and replaces it
in place, preserving user-created files in the plugin directory and re-running
the plugin's install hook in upgrade mode.

  cub plugin upgrade lk           Upgrade lk to the latest release
  cub plugin upgrade lk@v1.2.0    Re-pin lk to a specific release tag
  cub plugin upgrade --all        Upgrade every plugin with a recorded source`),
	Args: cobra.MaximumNArgs(1),
	RunE: pluginUpgradeCmdRun,
}

func init() {
	pluginUpgradeCmd.Flags().BoolVar(&pluginUpgradeArgs.all, "all", false, "Upgrade all installed plugins")
	pluginUpgradeCmd.Flags().StringVar(&pluginUpgradeArgs.platform, "platform", "", "Target platform (e.g. linux/amd64)")
	pluginCmd.AddCommand(pluginUpgradeCmd)
}

func pluginUpgradeCmdRun(cmd *cobra.Command, args []string) error {
	dir := pluginDir()
	// fetchPlugin reads the platform from the install args; share the flag value.
	pluginInstallArgs.platform = pluginUpgradeArgs.platform

	if pluginUpgradeArgs.all {
		if len(args) > 0 {
			return fmt.Errorf("cannot combine --all with a plugin name")
		}
		return upgradeAllPlugins(dir)
	}

	if len(args) != 1 {
		return fmt.Errorf("specify a plugin name to upgrade, or --all")
	}

	name, tag := splitNameTag(args[0])
	return upgradeOnePlugin(dir, name, tag)
}

// splitNameTag splits "name@tag" into its parts; tag is empty if absent.
func splitNameTag(s string) (name, tag string) {
	if i := strings.LastIndex(s, "@"); i != -1 {
		return s[:i], s[i+1:]
	}
	return s, ""
}

func upgradeAllPlugins(dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			tprint("No plugins installed.")
			return nil
		}
		return err
	}

	var upgraded, skipped int
	for _, e := range entries {
		name := e.Name()
		if strings.HasPrefix(name, ".") || !e.IsDir() {
			continue
		}
		if _, err := readInstallMetadata(filepath.Join(dir, name)); err != nil {
			tprint("Skipping %q: no recorded install source", name)
			skipped++
			continue
		}
		if err := upgradeOnePlugin(dir, name, ""); err != nil {
			return fmt.Errorf("upgrading %q: %w", name, err)
		}
		upgraded++
	}
	tprint("Upgraded %d plugin(s), skipped %d.", upgraded, skipped)
	return nil
}

func upgradeOnePlugin(dir, name, tagOverride string) error {
	destPath := filepath.Join(dir, name)
	info, err := os.Stat(destPath)
	if err != nil {
		return fmt.Errorf("plugin %q is not installed; use 'cub plugin install'", name)
	}
	if !info.IsDir() {
		return fmt.Errorf("plugin %q was installed as a bare binary and cannot be upgraded in place; reinstall it with 'cub plugin install <source>'", name)
	}

	meta, err := readInstallMetadata(destPath)
	if err != nil {
		return fmt.Errorf("plugin %q has no recorded install source; reinstall it with 'cub plugin install <source>'", name)
	}

	source := meta.Source
	if tagOverride != "" {
		source = repinSource(source, tagOverride)
	}

	src, err := parseInstallSource(source)
	if err != nil {
		return err
	}

	prevVersion := priorManifestVersion(destPath)

	// Fetch the new artifact into a staging directory.
	staged, newTag, binPath, err := fetchPlugin(src, name, dir)
	if err != nil {
		return err
	}
	defer os.RemoveAll(staged)

	// Carry prior user-created files forward (the manifest is dropped so the new
	// binary regenerates it; install metadata is rewritten by finalizeStaging).
	if err := carryPriorContents(destPath, staged); err != nil {
		return fmt.Errorf("preserving plugin files: %w", err)
	}

	// Bare-binary plugins regenerate their manifest via the upgrade hook.
	if binPath != "" {
		runHook(binPath, staged, coreplugin.HookUpgrade, prevVersion)
	}

	// Move the old install aside so the staged directory can take its place.
	// The "." prefix keeps the backup out of plugin discovery / collision checks.
	backup := filepath.Join(dir, "."+name+".upgrade-old")
	_ = os.RemoveAll(backup)
	if err := os.Rename(destPath, backup); err != nil {
		return fmt.Errorf("failed to replace existing plugin: %w", err)
	}
	if err := finalizeStaging(staged, destPath, name, name, source, newTag, defaultEntrypoint(binPath)); err != nil {
		// Restore the previous install on failure.
		_ = os.Rename(backup, destPath)
		return err
	}
	_ = os.RemoveAll(backup)

	tprint("Upgraded plugin %q", name)
	return nil
}

// repinSource rewrites a GitHub shorthand/URL source to point at a specific tag.
// Direct (non-GitHub) URLs cannot be re-pinned and are returned unchanged.
func repinSource(source, tag string) string {
	if strings.HasPrefix(source, "https://") && !isGitHubURL(source) {
		return source
	}
	// Strip an existing @tag from a shorthand (owner/repo@old).
	if i := strings.LastIndex(source, "@"); i != -1 && !strings.Contains(source[i:], "/") {
		source = source[:i]
	}
	return source + "@" + tag
}

// priorManifestVersion returns the version recorded in a plugin's manifest, or
// "" if unavailable.
func priorManifestVersion(dir string) string {
	m, err := coreplugin.Read(dir)
	if err != nil || m == nil {
		return ""
	}
	return m.Version
}

// carryPriorContents copies files from the old plugin directory into the staged
// directory, skipping the manifest (regenerated) and cub-owned metadata, and
// never overwriting files already staged (the freshly fetched binary/archive).
func carryPriorContents(oldDir, staged string) error {
	entries, err := os.ReadDir(oldDir)
	if err != nil {
		return err
	}
	for _, e := range entries {
		name := e.Name()
		if name == coreplugin.ManifestFileName || name == installMetadataFile {
			continue
		}
		if strings.HasPrefix(name, ".plugin-") || strings.HasSuffix(name, ".upgrade-old") {
			continue
		}
		dst := filepath.Join(staged, name)
		if _, err := os.Stat(dst); err == nil {
			continue // already staged
		}
		if err := copyPath(filepath.Join(oldDir, name), dst); err != nil {
			return err
		}
	}
	return nil
}

// copyPath recursively copies a file or directory tree. Symlinks are skipped.
func copyPath(src, dst string) error {
	info, err := os.Lstat(src)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return nil
	}
	if info.IsDir() {
		if err := os.MkdirAll(dst, info.Mode().Perm()|0700); err != nil {
			return err
		}
		entries, err := os.ReadDir(src)
		if err != nil {
			return err
		}
		for _, e := range entries {
			if err := copyPath(filepath.Join(src, e.Name()), filepath.Join(dst, e.Name())); err != nil {
				return err
			}
		}
		return nil
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, info.Mode().Perm())
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = io.Copy(out, in)
	return err
}
