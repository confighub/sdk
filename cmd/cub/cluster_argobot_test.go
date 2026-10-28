// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"encoding/json"
	"io"
	"slices"
	"strings"
	"testing"

	"github.com/google/uuid"
	"sigs.k8s.io/yaml"
)

// The key goes into the Secret as a YAML block scalar; what argobot reads back
// from CONFIGHUB_AUTH_PRIVATE_KEY must be the JWK exactly.
func TestClusterArgobotSecretManifestCarriesTheKeyIntact(t *testing.T) {
	key := json.RawMessage(`{"kty":"OKP","crv":"Ed25519","x":"abc","d":"def","confighub_user_external_id":"sa:123"}` + "\n")
	docs := strings.Split(string(clusterArgobotSecretManifest(key)), "\n---\n")
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

func TestClusterArgobotSubscriptions(t *testing.T) {
	target := uuid.New()
	subs := clusterArgobotSubscriptions(target)
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
	if want := []string{clusterArgobotEventReleasePublished}; !slices.Equal(sub.EventTypes, want) {
		t.Fatalf("EventTypes = %v, want %v", sub.EventTypes, want)
	}
}

func TestClusterArgobotImageIn(t *testing.T) {
	deployment := `apiVersion: apps/v1
kind: Deployment
metadata:
  name: argobot
  namespace: argobot
spec:
  template:
    spec:
      containers:
      - name: sidecar
        image: example.com/sidecar:v1
      - name: argobot
        image: ghcr.io/confighub/argobot:v0.1.6
`
	for name, tc := range map[string]struct {
		data string
		want string
	}{
		"one Deployment": {data: deployment, want: "ghcr.io/confighub/argobot:v0.1.6"},
		"all of argobot in one Unit": {
			data: "apiVersion: v1\nkind: Namespace\nmetadata:\n  name: argobot\n---\n" + deployment + "---\napiVersion: v1\nkind: ServiceAccount\nmetadata:\n  name: argobot\n",
			want: "ghcr.io/confighub/argobot:v0.1.6",
		},
		"no Deployment":        {data: "apiVersion: v1\nkind: Namespace\nmetadata:\n  name: argobot\n"},
		"not YAML":             {data: "{"},
		"no argobot container": {data: strings.Replace(deployment, "- name: argobot", "- name: other", 1)},
	} {
		t.Run(name, func(t *testing.T) {
			if got := clusterArgobotImageIn(tc.data); got != tc.want {
				t.Errorf("clusterArgobotImageIn() = %q, want %q", got, tc.want)
			}
		})
	}
}

// A --k8sbot-base value is taken as given, with no question and no call to
// ConfigHub, and anything else is refused.
func TestClusterChooseArgobotBaseFlag(t *testing.T) {
	for _, flag := range []string{clusterArgobotBaseUpdate, clusterArgobotBaseClone} {
		got, err := clusterChooseArgobotBase(io.Discard, clusterControllerArgo, flag, clusterArgobotGHCR)
		if err != nil || got != flag {
			t.Errorf("clusterChooseArgobotBase(%q) = %q, %v; want %q", flag, got, err, flag)
		}
	}
	if _, err := clusterChooseArgobotBase(io.Discard, clusterControllerArgo, "replace", clusterArgobotGHCR); err == nil {
		t.Error("clusterChooseArgobotBase(\"replace\") succeeded; want an error")
	}
}

// --registry names ghcr, gcr, or a mirror prefix, which argobot's image sits
// under; a bare word that is neither is refused rather than taken as a host.
func TestClusterArgobotRepository(t *testing.T) {
	for registry, want := range map[string]string{
		"":                                 clusterArgobotGHCR,
		"ghcr":                             clusterArgobotGHCR,
		"gcr":                              clusterArgobotGCR,
		"gcr.io":                           clusterArgobotGCR,
		"registry.example.com/confighub/":  "registry.example.com/confighub/argobot",
		"registry.example.com:5000/mirror": "registry.example.com:5000/mirror/argobot",
	} {
		if got, err := clusterArgobotRepository(registry); err != nil || got != want {
			t.Errorf("clusterArgobotRepository(%q) = %q, %v; want %q", registry, got, err, want)
		}
	}
	if _, err := clusterArgobotRepository("quay"); err == nil {
		t.Error("clusterArgobotRepository(\"quay\") succeeded; want an error")
	}
}
