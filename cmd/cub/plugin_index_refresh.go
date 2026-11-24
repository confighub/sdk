// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"sigs.k8s.io/yaml"
)

// pluginIndexArchiveURL is the published index: a repository with one file per
// plugin, downloaded whole. An archive download is one request and is not
// counted against GitHub's API rate limit.
var pluginIndexArchiveURL = "https://github.com/confighub/cub-plugins/archive/refs/heads/main.tar.gz"

const (
	// pluginIndexEntriesDir is the directory of entries within the archive.
	pluginIndexEntriesDir = "plugins"
	// pluginIndexCacheFile is the downloaded index, in the cub config directory.
	pluginIndexCacheFile = "plugin-index.yaml"
	// pluginIndexTTL is how long a downloaded index is used before cub looks
	// for a newer one.
	pluginIndexTTL = 24 * time.Hour
	// pluginIndexFetchTimeout bounds the download. It runs in front of a
	// command the user is waiting on, and the index cub already has is good
	// enough when the network is slow or absent.
	pluginIndexFetchTimeout = 5 * time.Second
)

// pluginIndexCache is the content of pluginIndexCacheFile.
type pluginIndexCache struct {
	// Checked is when cub last tried to download the index, whether or not it
	// succeeded, so a machine with no network tries once per pluginIndexTTL
	// and not on every unknown command.
	Checked time.Time `json:"checked"`
	// Plugins is the last index downloaded. Empty means none has been.
	Plugins []pluginIndexEntry `json:"plugins,omitempty"`
}

// Seams for tests: a test neither downloads the index nor waits a day.
var (
	pluginIndexFetch = fetchPluginIndex
	pluginIndexNow   = time.Now
)

// loadPluginIndex returns the index of known plugins: the published one, as
// last downloaded, or the one this build carries when none has been.
//
// It downloads the published index when the last attempt is older than
// pluginIndexTTL. Only the paths that need the index call it -- an unknown
// command, `cub plugin install <name>` -- so no other command waits on the
// network. A failed download is not an error: the index cub has is used.
func loadPluginIndex() (*pluginIndex, error) {
	embedded, err := embeddedPluginIndex()
	if err != nil {
		return nil, err
	}

	path := filepath.Join(contextManager.ConfigDir(), pluginIndexCacheFile)
	cache := readPluginIndexCache(path)
	now := pluginIndexNow()
	if age := now.Sub(cache.Checked); age >= pluginIndexTTL || age < 0 {
		if fetched, fetchErr := pluginIndexFetch(); fetchErr == nil {
			cache.Plugins = fetched.Plugins
		}
		cache.Checked = now
		// Best effort: without the file, the next lookup downloads again.
		_ = writePluginIndexCache(path, cache)
	}

	if len(cache.Plugins) > 0 {
		return &pluginIndex{Plugins: cache.Plugins}, nil
	}
	return embedded, nil
}

// readPluginIndexCache reads the downloaded index. A missing or unreadable
// file is the same as one never written.
func readPluginIndexCache(path string) pluginIndexCache {
	var cache pluginIndexCache
	data, err := os.ReadFile(path)
	if err != nil {
		return pluginIndexCache{}
	}
	if err := yaml.Unmarshal(data, &cache); err != nil {
		return pluginIndexCache{}
	}
	return cache
}

// writePluginIndexCache replaces the downloaded index in one step, so another
// cub running at the same time never reads half a file.
func writePluginIndexCache(path string, cache pluginIndexCache) error {
	data, err := yaml.Marshal(cache)
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), pluginIndexCacheFile+".*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

// fetchPluginIndex downloads the published index.
func fetchPluginIndex() (*pluginIndex, error) {
	fetchCtx, cancel := context.WithTimeout(context.Background(), pluginIndexFetchTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(fetchCtx, http.MethodGet, pluginIndexArchiveURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "cub-cli")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("download the plugin index: %s", resp.Status)
	}

	dir, err := os.MkdirTemp("", "cub-plugin-index-*")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(dir)
	if err := extractTarGz(resp.Body, dir); err != nil {
		return nil, err
	}
	return readPluginIndexDir(filepath.Join(strippedDir(dir), pluginIndexEntriesDir))
}

// readPluginIndexDir reads an index from a directory with one <name>.yaml per
// plugin. A file cub cannot use is skipped, so one bad entry does not take the
// others with it; a directory with no usable entry is an error, so it never
// replaces an index that has some.
func readPluginIndexDir(dir string) (*pluginIndex, error) {
	files, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var ix pluginIndex
	for _, f := range files {
		name, isYAML := strings.CutSuffix(f.Name(), ".yaml")
		if f.IsDir() || !isYAML {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, f.Name()))
		if err != nil {
			continue
		}
		var entry pluginIndexEntry
		if err := yaml.Unmarshal(data, &entry); err != nil {
			continue
		}
		if entry.Name != name || entry.Source == "" || len(entry.Commands) == 0 {
			continue
		}
		ix.Plugins = append(ix.Plugins, entry)
	}
	if len(ix.Plugins) == 0 {
		return nil, fmt.Errorf("the plugin index has no entries")
	}
	return &ix, nil
}
