// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"encoding/json"
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
