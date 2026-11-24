// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// setPluginIndexFetch replaces the download and counts the attempts.
func setPluginIndexFetch(t *testing.T, fetch func() (*pluginIndex, error)) *int {
	t.Helper()
	var calls int
	orig := pluginIndexFetch
	t.Cleanup(func() { pluginIndexFetch = orig })
	pluginIndexFetch = func() (*pluginIndex, error) {
		calls++
		return fetch()
	}
	return &calls
}

func setPluginIndexNow(t *testing.T, now *time.Time) {
	t.Helper()
	orig := pluginIndexNow
	t.Cleanup(func() { pluginIndexNow = orig })
	pluginIndexNow = func() time.Time { return *now }
}

func TestLoadPluginIndexDownloadsOncePerTTL(t *testing.T) {
	setupPluginTest(t)
	setPluginIndex(t, testPluginIndex)
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	setPluginIndexNow(t, &now)
	published := &pluginIndex{Plugins: []pluginIndexEntry{{Name: "fresh", Source: "example/cub-fresh", Commands: []string{"fresh"}}}}
	calls := setPluginIndexFetch(t, func() (*pluginIndex, error) { return published, nil })

	ix, err := loadPluginIndex()
	if err != nil {
		t.Fatal(err)
	}
	if ix.byCommand("fresh") == nil || ix.byCommand("quiet") != nil {
		t.Errorf("first load = %+v; want the downloaded index in place of the embedded one", ix.Plugins)
	}

	// Within the TTL the downloaded copy is used and nothing is fetched.
	published = &pluginIndex{Plugins: []pluginIndexEntry{{Name: "fresher", Source: "example/cub-fresher", Commands: []string{"fresher"}}}}
	now = now.Add(pluginIndexTTL - time.Minute)
	ix, err = loadPluginIndex()
	if err != nil {
		t.Fatal(err)
	}
	if *calls != 1 || ix.byCommand("fresh") == nil {
		t.Errorf("within the TTL: downloads = %d, index = %+v; want 1 and the first download", *calls, ix.Plugins)
	}

	now = now.Add(2 * time.Minute)
	ix, err = loadPluginIndex()
	if err != nil {
		t.Fatal(err)
	}
	if *calls != 2 || ix.byCommand("fresher") == nil {
		t.Errorf("after the TTL: downloads = %d, index = %+v; want 2 and the second download", *calls, ix.Plugins)
	}
}

func TestLoadPluginIndexWhenDownloadFails(t *testing.T) {
	setupPluginTest(t)
	setPluginIndex(t, testPluginIndex)
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	setPluginIndexNow(t, &now)
	fail := true
	calls := setPluginIndexFetch(t, func() (*pluginIndex, error) {
		if fail {
			return nil, errors.New("no network")
		}
		return &pluginIndex{Plugins: []pluginIndexEntry{{Name: "fresh", Source: "example/cub-fresh", Commands: []string{"fresh"}}}}, nil
	})

	// Nothing downloaded yet: the embedded index, and one attempt per TTL.
	for range 2 {
		ix, err := loadPluginIndex()
		if err != nil {
			t.Fatal(err)
		}
		if ix.byCommand("quiet") == nil {
			t.Errorf("index = %+v; want the embedded one", ix.Plugins)
		}
	}
	if *calls != 1 {
		t.Errorf("downloads = %d, want 1: a failed attempt is not repeated within the TTL", *calls)
	}

	fail = false
	now = now.Add(pluginIndexTTL)
	if ix, err := loadPluginIndex(); err != nil || ix.byCommand("fresh") == nil {
		t.Fatalf("after a successful download: index = %+v, err = %v", ix, err)
	}

	// A later failure keeps the last index downloaded.
	fail = true
	now = now.Add(pluginIndexTTL)
	if ix, err := loadPluginIndex(); err != nil || ix.byCommand("fresh") == nil {
		t.Errorf("after a later failure: index = %+v, err = %v; want the last download kept", ix, err)
	}
}

// A cache file that does not parse is treated as absent.
func TestLoadPluginIndexUnreadableCache(t *testing.T) {
	pluginsDir := setupPluginTest(t)
	setPluginIndex(t, testPluginIndex)
	if err := os.WriteFile(filepath.Join(filepath.Dir(pluginsDir), pluginIndexCacheFile), []byte("{not yaml"), 0o644); err != nil {
		t.Fatal(err)
	}
	calls := setPluginIndexFetch(t, func() (*pluginIndex, error) { return nil, errors.New("no network") })
	ix, err := loadPluginIndex()
	if err != nil || ix.byCommand("quiet") == nil || *calls != 1 {
		t.Errorf("index = %+v, err = %v, downloads = %d; want the embedded index after one attempt", ix, err, *calls)
	}
}

func pluginIndexArchive(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for name, content := range files {
		if err := tw.WriteHeader(&tar.Header{Name: "cub-plugins-main/" + name, Mode: 0o644, Size: int64(len(content))}); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write([]byte(content)); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestFetchPluginIndex(t *testing.T) {
	archive := pluginIndexArchive(t, map[string]string{
		"README.md":              "# index\n",
		"plugins/good.yaml":      "name: good\nsource: example/cub-good\ncommands: [good, g]\nprompt: false\nfutureField: 1\n",
		"plugins/asks.yaml":      "name: asks\nsource: example/cub-asks\ncommands: [asks]\n",
		"plugins/misnamed.yaml":  "name: other\nsource: example/cub-other\ncommands: [other]\n",
		"plugins/nosource.yaml":  "name: nosource\ncommands: [nosource]\n",
		"plugins/nocommand.yaml": "name: nocommand\nsource: example/cub-nocommand\n",
		"plugins/broken.yaml":    "name: [broken\n",
		"plugins/notes.txt":      "not an entry\n",
	})
	status := http.StatusOK
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(status)
		_, _ = w.Write(archive)
	}))
	defer srv.Close()
	orig := pluginIndexArchiveURL
	t.Cleanup(func() { pluginIndexArchiveURL = orig })
	pluginIndexArchiveURL = srv.URL

	ix, err := fetchPluginIndex()
	if err != nil {
		t.Fatal(err)
	}
	if len(ix.Plugins) != 2 {
		t.Fatalf("plugins = %+v; want only the two usable entries", ix.Plugins)
	}
	if e := ix.byCommand("g"); e == nil || e.Source != "example/cub-good" || e.asks() {
		t.Errorf("byCommand(g) = %+v; want example/cub-good, installed without asking", e)
	}
	if e := ix.byName("asks"); e == nil || !e.asks() {
		t.Errorf("byName(asks) = %+v; want an entry that asks", e)
	}

	status = http.StatusNotFound
	if _, err := fetchPluginIndex(); err == nil {
		t.Error("a 404 produced an index")
	}

	status = http.StatusOK
	archive = pluginIndexArchive(t, map[string]string{"plugins/broken.yaml": "name: [broken\n"})
	if _, err := fetchPluginIndex(); err == nil {
		t.Error("an archive with no usable entry produced an index")
	}
}
