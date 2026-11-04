// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"bytes"
	"errors"
	"maps"
	"strings"
	"testing"

	"sigs.k8s.io/yaml"
)

func TestClusterArgoCredsForRegistry(t *testing.T) {
	for _, tc := range []struct {
		secretURL, ociURL string
		want              bool
	}{
		{"oci://172.18.0.1:9092", "oci://172.18.0.1:9092", true},
		{"oci://172.18.0.1:9092/", "oci://172.18.0.1:9092", true},
		{"oci://oci.hub.confighub.com", "oci://172.18.0.1:9092", false},
		{"oci://172.18.0.1:9093", "oci://172.18.0.1:9092", false},
	} {
		if got := clusterArgoCredsForRegistry(tc.secretURL, tc.ociURL); got != tc.want {
			t.Errorf("clusterArgoCredsForRegistry(%q, %q) = %v, want %v", tc.secretURL, tc.ociURL, got, tc.want)
		}
	}
}

func TestClusterFluxCredsForRegistry(t *testing.T) {
	for _, tc := range []struct {
		secretHost, ociURL string
		want               bool
	}{
		{"172.18.0.1:9092", "oci://172.18.0.1:9092", true},
		{"oci.hub.confighub.com", "oci://172.18.0.1:9092", false},
		// A Secret with no registry in it reads back as the Secret's name.
		{clusterFluxCredsSecret, "oci://172.18.0.1:9092", false},
		{"172.18.0.1:9092", "not a url", false},
	} {
		if got := clusterFluxCredsForRegistry(tc.secretHost, tc.ociURL); got != tc.want {
			t.Errorf("clusterFluxCredsForRegistry(%q, %q) = %v, want %v", tc.secretHost, tc.ociURL, got, tc.want)
		}
	}
}

func TestClusterArgoCredsSecretRecordsClusterAndOrganization(t *testing.T) {
	// A cluster name may be longer than the 63 characters a label value allows.
	long := strings.Repeat("c", 100)
	annotations := map[string]string{clusterAnnotationName: long, clusterAnnotationOrganization: "org-1"}
	manifest := clusterArgoOCIRepoCredsManifest(annotations, "oci://172.18.0.1:9092", "tok", false)
	var secret struct {
		Metadata struct {
			Labels      map[string]string `json:"labels"`
			Annotations map[string]string `json:"annotations"`
		} `json:"metadata"`
		StringData map[string]string `json:"stringData"`
	}
	if err := yaml.Unmarshal([]byte(manifest), &secret); err != nil {
		t.Fatalf("%v:\n%s", err, manifest)
	}
	if !maps.Equal(secret.Metadata.Annotations, annotations) {
		t.Errorf("annotations = %v, want %v", secret.Metadata.Annotations, annotations)
	}
	if want := map[string]string{"argocd.argoproj.io/secret-type": "repo-creds"}; !maps.Equal(secret.Metadata.Labels, want) {
		t.Errorf("labels = %v, want %v", secret.Metadata.Labels, want)
	}
	if secret.StringData["url"] != "oci://172.18.0.1:9092" || secret.StringData["password"] != "tok" {
		t.Errorf("stringData = %v", secret.StringData)
	}
}

func TestClusterJudgeCreds(t *testing.T) {
	const org, otherOrg = "org-1", "org-2"
	// "live" has a Space or service account in the current organization;
	// nothing else does.
	claimed := func(name string) (bool, error) { return name == "live", nil }
	for _, tc := range []struct {
		name        string
		found       clusterFoundCreds
		attaching   string
		want        clusterCredsVerdict
		wantChecked string
	}{
		{"another server's registry", clusterFoundCreds{forRegistry: false, cluster: "gone", organization: org}, "lab",
			clusterCredsWired, ""},
		{"names this org and a cluster nothing claims", clusterFoundCreds{forRegistry: true, cluster: "gone", organization: org}, "lab",
			clusterCredsLeftover, "gone"},
		{"names this org and a live cluster under another name", clusterFoundCreds{forRegistry: true, cluster: "live", organization: org}, "lab",
			clusterCredsWired, "live"},
		{"names another organization on this server", clusterFoundCreds{forRegistry: true, cluster: "gone", organization: otherOrg}, "lab",
			clusterCredsWired, "gone"},
		{"an older cub's, nothing claims the name being attached", clusterFoundCreds{forRegistry: true}, "lab",
			clusterCredsUnnamed, "lab"},
		{"an older cub's, the name being attached is live", clusterFoundCreds{forRegistry: true}, "live",
			clusterCredsWired, "live"},
		{"a cluster but no organization", clusterFoundCreds{forRegistry: true, cluster: "gone"}, "lab",
			clusterCredsUnnamed, "gone"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, checked, err := clusterJudgeCreds(tc.found, org, tc.attaching, claimed)
			if err != nil {
				t.Fatal(err)
			}
			if got != tc.want || checked != tc.wantChecked {
				t.Errorf("verdict, checked = %v, %q; want %v, %q", got, checked, tc.want, tc.wantChecked)
			}
		})
	}

	t.Run("a failed lookup refuses", func(t *testing.T) {
		failing := func(string) (bool, error) { return false, errors.New("server unreachable") }
		got, _, err := clusterJudgeCreds(clusterFoundCreds{forRegistry: true, cluster: "gone", organization: org}, org, "lab", failing)
		if err == nil || got != clusterCredsWired {
			t.Errorf("verdict, err = %v, %v; want wired and the error", got, err)
		}
	})
}

func TestClusterAllowLeftoverCreds(t *testing.T) {
	for _, tc := range []struct {
		name        string
		named       bool
		interactive bool
		answer      string
		wantReplace bool
		wantErr     bool
		wantAsk     string
	}{
		{"named, no terminal: replaced", true, false, "", true, false, ""},
		{"named, enter: replaced", true, true, "\n", true, false, "[Y/n]"},
		{"named, no: kept", true, true, "n\n", false, true, "[Y/n]"},
		{"unnamed, no terminal: kept, not asked", false, false, "", false, false, ""},
		{"unnamed, enter: kept", false, true, "\n", false, true, "[y/N]"},
		{"unnamed, yes: replaced", false, true, "y\n", true, false, "[y/N]"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			left := clusterLeftoverCreds{namespace: "argocd", secret: "confighub-oci-creds", cluster: "lab", named: tc.named, server: "http://localhost:9090"}
			var out bytes.Buffer
			replace, err := clusterAllowLeftoverCreds(&out, strings.NewReader(tc.answer), tc.interactive, left)
			if replace != tc.wantReplace || (err != nil) != tc.wantErr {
				t.Errorf("replace, err = %v, %v; want %v, error %v", replace, err, tc.wantReplace, tc.wantErr)
			}
			got := out.String()
			if tc.wantAsk == "" && strings.Contains(got, "Replace it") && strings.Contains(got, "?") {
				t.Errorf("asked without a terminal: %q", got)
			}
			if tc.wantAsk != "" && !strings.Contains(got, tc.wantAsk) {
				t.Errorf("question lacks %s: %q", tc.wantAsk, got)
			}
			if tc.named && !strings.Contains(got, `left by an attach of cluster "lab" in this organization`) {
				t.Errorf("output does not say why the Secret is a leftover: %q", got)
			}
			if !tc.named && tc.interactive && !strings.Contains(got, "replacing it cuts that attachment off") {
				t.Errorf("output does not warn what replacing an unnamed Secret risks: %q", got)
			}
		})
	}
}
