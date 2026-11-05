// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"encoding/base64"
	"strings"
	"testing"

	"github.com/google/uuid"
	"sigs.k8s.io/yaml"
)

// clusterSecretFromManifest reads a Secret manifest as attach reads the Secret
// from the cluster, where the API server has moved stringData into data.
func clusterSecretFromManifest(t *testing.T, manifest []byte) *clusterSecretObject {
	t.Helper()
	var m struct {
		Type     string `json:"type"`
		Metadata struct {
			Labels map[string]string `json:"labels"`
		} `json:"metadata"`
		StringData map[string]string `json:"stringData"`
	}
	if err := yaml.Unmarshal(manifest, &m); err != nil {
		t.Fatalf("manifest: %v", err)
	}
	s := &clusterSecretObject{Type: m.Type, Data: map[string][]byte{}}
	s.Metadata.Labels = m.Metadata.Labels
	for k, v := range m.StringData {
		s.Data[k] = []byte(v)
	}
	return s
}

// The Secret attach would apply for each controller is one --existing-secrets
// accepts, with the token it was made with.
func TestClusterPullTokenFromSecretAcceptsWhatAttachApplies(t *testing.T) {
	newKeyTestContextManager(t) // the Secrets record the active context's organization
	const ociURL, token = "oci://172.18.0.1:9092", "the-token"
	for _, controller := range []string{clusterControllerArgo, clusterControllerSveltos, clusterControllerFlux} {
		for _, insecure := range []bool{false, true} {
			s, err := clusterControllerCredsSecretFor(controller, "lab", ociURL, token, insecure)
			if err != nil {
				t.Fatal(err)
			}
			got, err := clusterPullTokenFromSecret(controller, clusterSecretFromManifest(t, s.manifest), ociURL, insecure)
			if err != nil {
				t.Errorf("%s, insecure %v: %v", controller, insecure, err)
			} else if got != token {
				t.Errorf("%s, insecure %v: token = %q, want %q", controller, insecure, got, token)
			}
		}
	}
}

// Every problem with the Secret is named at once, so one round of fixes in the
// secret store is enough.
func TestClusterPullTokenFromSecretNamesEveryProblem(t *testing.T) {
	s := &clusterSecretObject{Data: map[string][]byte{"type": []byte("helm"), "url": []byte("oci://elsewhere:9092")}}
	_, err := clusterPullTokenFromSecret(clusterControllerArgo, s, "oci://172.18.0.1:9092", true)
	if err == nil {
		t.Fatal("want an error")
	}
	for _, want := range []string{"argocd.argoproj.io/secret-type=repo-creds", `"oci"`, "oci://172.18.0.1:9092", `"token"`, "insecureOCIForceHttp", "no pull token"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not mention %s", err, want)
		}
	}
}

// A token read from a file with its newline is named as the problem, rather
// than failing as an invalid Authorization header.
func TestClusterPullTokenFromSecretRefusesSurroundingWhitespace(t *testing.T) {
	s := &clusterSecretObject{Type: "addons.projectsveltos.io/cluster-profile",
		Data: map[string][]byte{"username": []byte("token"), "password": []byte("tok\n")}}
	_, err := clusterPullTokenFromSecret(clusterControllerSveltos, s, "oci://172.18.0.1:9092", false)
	if err == nil || !strings.Contains(err.Error(), "whitespace") {
		t.Errorf("err = %v, want one about whitespace", err)
	}
}

// The key a --secrets-dir file holds is the one it was written with.
func TestClusterArgobotKeyInManifest(t *testing.T) {
	const key = `{"kty":"OKP","crv":"Ed25519","x":"abc","d":"def"}`
	got, err := clusterArgobotKeyInManifest(clusterArgobotSecretManifest([]byte(key)))
	if err != nil || string(got) != key {
		t.Errorf("clusterArgobotKeyInManifest = %q, %v; want %q", got, err, key)
	}
	if _, err := clusterArgobotKeyInManifest(clusterArgobotNamespaceManifest()); err == nil {
		t.Error("a manifest with no Secret: want an error")
	}
}

// The user a token is issued to is its internal_user_id claim.
func TestClusterTokenUserID(t *testing.T) {
	id := uuid.New()
	payload := base64.RawURLEncoding.EncodeToString([]byte(`{"sub":"x","internal_user_id":"` + id.String() + `"}`))
	if got, err := clusterTokenUserID("h." + payload + ".s"); err != nil || got != id {
		t.Errorf("clusterTokenUserID = %s, %v; want %s", got, err, id)
	}
	if _, err := clusterTokenUserID("not-a-jwt"); err == nil {
		t.Error("clusterTokenUserID(not-a-jwt): want an error")
	}
}

// Flux matches credentials by the registry's host:port, and a docker config
// may carry only the auth member.
func TestClusterFluxPullToken(t *testing.T) {
	auth := base64.StdEncoding.EncodeToString([]byte("token:tok"))
	cfg := []byte(`{"auths":{"172.18.0.1:9092":{"auth":"` + auth + `"}}}`)
	if got, err := clusterFluxPullToken(cfg, "http://172.18.0.1:9092"); err != nil || got != "tok" {
		t.Errorf("clusterFluxPullToken = %q, %v; want tok", got, err)
	}
	if _, err := clusterFluxPullToken(cfg, "http://172.18.0.1:9093"); err == nil || !strings.Contains(err.Error(), "172.18.0.1:9093") {
		t.Errorf("another registry: err = %v, want one naming 172.18.0.1:9093", err)
	}
}
