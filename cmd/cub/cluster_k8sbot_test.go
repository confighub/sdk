// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"
	"sigs.k8s.io/yaml"
)

// The key goes into the Secret as a YAML block scalar; what k8sbot reads back
// from CONFIGHUB_AUTH_PRIVATE_KEY must be the JWK exactly.
func TestClusterK8sbotSecretManifestCarriesTheKeyIntact(t *testing.T) {
	key := json.RawMessage(`{"kty":"OKP","crv":"Ed25519","x":"abc","d":"def","confighub_user_external_id":"sa:123"}` + "\n")
	docs := strings.Split(string(clusterK8sbotSecretManifest(key)), "\n---\n")
	if len(docs) != 2 {
		t.Fatalf("documents = %d, want Namespace + Secret", len(docs))
	}
	var secret struct {
		Kind       string            `json:"kind"`
		StringData map[string]string `json:"stringData"`
	}
	if err := yaml.Unmarshal([]byte(docs[1]), &secret); err != nil {
		t.Fatalf("secret document: %v", err)
	}
	if secret.Kind != "Secret" {
		t.Fatalf("kind = %q, want Secret", secret.Kind)
	}
	got := secret.StringData["CONFIGHUB_AUTH_PRIVATE_KEY"]
	want := `{"kty":"OKP","crv":"Ed25519","x":"abc","d":"def","confighub_user_external_id":"sa:123"}`
	if got != want {
		t.Fatalf("CONFIGHUB_AUTH_PRIVATE_KEY = %q, want %q", got, want)
	}
	if _, ok := secret.StringData["CONFIGHUB_WORKER_SECRET"]; ok {
		t.Fatal("the Secret still carries a worker secret")
	}
}

func TestClusterK8sbotSubscriptions(t *testing.T) {
	target := uuid.New()
	subs := clusterK8sbotSubscriptions(target)
	if len(subs) != 1 {
		t.Fatalf("len = %d, want 1", len(subs))
	}
	sub := subs[0]
	if sub.TargetID == nil || uuid.UUID(*sub.TargetID) != target {
		t.Fatalf("TargetID = %v, want %s", sub.TargetID, target)
	}
	if sub.SpaceID != nil {
		t.Fatalf("SpaceID = %v, want nil: the Target scopes the subscription, not a Space", sub.SpaceID)
	}
	if want := []string{clusterK8sbotEventReleasePublished}; !slices.Equal(sub.EventTypes, want) {
		t.Fatalf("EventTypes = %v, want %v", sub.EventTypes, want)
	}
}

func TestClusterK8sbotImageIn(t *testing.T) {
	deployment := `apiVersion: apps/v1
kind: Deployment
metadata:
  name: k8sbot
  namespace: k8sbot
spec:
  template:
    spec:
      containers:
      - name: sidecar
        image: example.com/sidecar:v1
      - name: k8sbot
        image: ghcr.io/confighub/k8sbot:v0.1.6
`
	for name, tc := range map[string]struct {
		data string
		want string
	}{
		"one Deployment": {data: deployment, want: "ghcr.io/confighub/k8sbot:v0.1.6"},
		"all of k8sbot in one Unit": {
			data: "apiVersion: v1\nkind: Namespace\nmetadata:\n  name: k8sbot\n---\n" + deployment + "---\napiVersion: v1\nkind: ServiceAccount\nmetadata:\n  name: k8sbot\n",
			want: "ghcr.io/confighub/k8sbot:v0.1.6",
		},
		"no Deployment":       {data: "apiVersion: v1\nkind: Namespace\nmetadata:\n  name: k8sbot\n"},
		"not YAML":            {data: "{"},
		"no k8sbot container": {data: strings.Replace(deployment, "- name: k8sbot", "- name: other", 1)},
	} {
		t.Run(name, func(t *testing.T) {
			if got := clusterK8sbotImageIn(tc.data); got != tc.want {
				t.Errorf("clusterK8sbotImageIn() = %q, want %q", got, tc.want)
			}
		})
	}
}

// A --k8sbot-base value is taken as given, with no question and no call to
// ConfigHub, and anything else is refused.
func TestClusterChooseK8sbotBaseFlag(t *testing.T) {
	for _, flag := range []string{clusterK8sbotBaseUpdate, clusterK8sbotBaseClone} {
		got, err := clusterChooseK8sbotBase(io.Discard, clusterControllerArgo, flag)
		if err != nil || got != flag {
			t.Errorf("clusterChooseK8sbotBase(%q) = %q, %v; want %q", flag, got, err, flag)
		}
	}
	if _, err := clusterChooseK8sbotBase(io.Discard, clusterControllerArgo, "replace"); err == nil {
		t.Error("clusterChooseK8sbotBase(\"replace\") succeeded; want an error")
	}
}

// --registry names ghcr, gcr, or a mirror prefix, which k8sbot's image sits
// under; a bare word that is neither is refused rather than taken as a host.
func TestClusterK8sbotRepository(t *testing.T) {
	for registry, want := range map[string]string{
		"":                                 clusterK8sbotGHCR,
		"ghcr":                             clusterK8sbotGHCR,
		"gcr":                              clusterK8sbotGCR,
		"gcr.io":                           clusterK8sbotGCR,
		"registry.example.com/confighub/":  "registry.example.com/confighub/k8sbot",
		"registry.example.com:5000/mirror": "registry.example.com:5000/mirror/k8sbot",
	} {
		if got, err := clusterK8sbotRepository(registry); err != nil || got != want {
			t.Errorf("clusterK8sbotRepository(%q) = %q, %v; want %q", registry, got, err, want)
		}
	}
	if _, err := clusterK8sbotRepository("quay"); err == nil {
		t.Error("clusterK8sbotRepository(\"quay\") succeeded; want an error")
	}
}

// k8sbot's installer package sits beside its image, under packages/.
func TestClusterK8sbotPackageRepository(t *testing.T) {
	for repository, want := range map[string]string{
		clusterK8sbotGHCR:                         "ghcr.io/confighub/packages/k8sbot",
		clusterK8sbotGCR:                          "gcr.io/confighubapp/packages/k8sbot",
		"registry.example.com:5000/mirror/k8sbot": "registry.example.com:5000/mirror/packages/k8sbot",
	} {
		if got := clusterK8sbotPackageRepository(repository); got != want {
			t.Errorf("clusterK8sbotPackageRepository(%q) = %q, want %q", repository, got, want)
		}
	}
}

// --k8sbot-installer runs the installer cub plugin's own entrypoint, never an
// "installer" on PATH, such as the one macOS has in /usr/sbin.
func TestClusterFindInstaller(t *testing.T) {
	pluginsDir := setupPluginTest(t)
	onPath := t.TempDir()
	createExecutableFile(t, filepath.Join(onPath, "installer"), "#!/bin/sh\necho installer 0.3.0")
	t.Setenv("PATH", onPath)
	if _, err := clusterFindInstaller(); err == nil || !strings.Contains(err.Error(), "cub plugin install confighub/installer") {
		t.Errorf("clusterFindInstaller() without the plugin: err = %v; want the plugin install command", err)
	}

	dir := filepath.Join(pluginsDir, "installer")
	if err := os.MkdirAll(filepath.Join(dir, "bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	createExecutableFile(t, filepath.Join(dir, "bin", "installer"), "#!/bin/sh\necho installer 0.3.0")
	if err := os.WriteFile(filepath.Join(dir, "cub-plugin.yaml"), []byte("entrypoint: bin/installer\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	pluginCache = nil
	pluginCacheOnce = sync.Once{}
	got, err := clusterFindInstaller()
	if err != nil {
		t.Fatal(err)
	}
	if got.Entrypoint != filepath.Join(dir, "bin", "installer") {
		t.Errorf("entrypoint = %q, want the plugin's %s", got.Entrypoint, filepath.Join(dir, "bin", "installer"))
	}
}
