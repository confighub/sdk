// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"encoding/base64"
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"sigs.k8s.io/yaml"
)

// fluxDocs splits a Flux manifest into its objects, keyed by kind.
func fluxDocs(t *testing.T, manifest []byte) map[string]map[string]any {
	t.Helper()
	docs := map[string]map[string]any{}
	for _, doc := range strings.Split(string(manifest), "\n---\n") {
		var obj map[string]any
		if err := yaml.UnmarshalStrict([]byte(doc), &obj); err != nil {
			t.Fatalf("parse %q: %v", doc, err)
		}
		docs[obj["kind"].(string)] = obj
	}
	if len(docs) != 2 || docs["OCIRepository"] == nil || docs["Kustomization"] == nil {
		t.Fatalf("manifest has %v; want one OCIRepository and one Kustomization", docs)
	}
	return docs
}

func fluxField(obj map[string]any, path ...string) any {
	var v any = obj
	for _, p := range path {
		m, ok := v.(map[string]any)
		if !ok {
			return nil
		}
		v = m[p]
	}
	return v
}

func TestClusterFluxChildManifest(t *testing.T) {
	const url = "oci://172.18.0.1:9092/space/orders"
	docs := fluxDocs(t, clusterFluxChildManifest("orders", url, "10m0s", true, false))
	repo, ks := docs["OCIRepository"], docs["Kustomization"]

	// Flux takes the bare repository in url and the tag in ref.tag.
	if got := fluxField(repo, "spec", "url"); got != url {
		t.Errorf("OCIRepository url = %v, want %s", got, url)
	}
	if got := fluxField(repo, "spec", "ref", "tag"); got != "latest" {
		t.Errorf("OCIRepository ref.tag = %v, want latest", got)
	}
	for name, want := range map[string]any{
		"interval":       "10m0s",
		"insecure":       true,
		"secretRef.name": clusterFluxCredsSecret,
	} {
		if got := fluxField(repo, append([]string{"spec"}, strings.Split(name, ".")...)...); got != want {
			t.Errorf("OCIRepository %s = %v, want %v", name, got, want)
		}
	}
	for _, obj := range docs {
		if got := fluxField(obj, "metadata", "name"); got != "orders" {
			t.Errorf("%s name = %v, want orders", obj["kind"], got)
		}
		if got := fluxField(obj, "metadata", "namespace"); got != clusterFluxNamespace {
			t.Errorf("%s namespace = %v, want %s", obj["kind"], got, clusterFluxNamespace)
		}
		if got := fluxField(obj, "metadata", "annotations"); got != nil {
			t.Errorf("%s annotations = %v; a child must be prunable", obj["kind"], got)
		}
	}
	// A child waits on what it applied, so its Healthy condition means something,
	// and without --prune never deletes it.
	for name, want := range map[string]any{
		"sourceRef.kind": "OCIRepository",
		"sourceRef.name": "orders",
		"path":           ".",
		"prune":          false,
		"wait":           true,
	} {
		if got := fluxField(ks, append([]string{"spec"}, strings.Split(name, ".")...)...); got != want {
			t.Errorf("Kustomization %s = %v, want %v", name, got, want)
		}
	}
}

func TestClusterFluxRootManifest(t *testing.T) {
	docs := fluxDocs(t, clusterFluxRootManifest("oci://172.18.0.1:9092/space/c-flux-apps", "1m0s", false, false))
	for _, obj := range docs {
		if got := fluxField(obj, "metadata", "name"); got != clusterFluxRootName {
			t.Errorf("%s name = %v, want %s", obj["kind"], got, clusterFluxRootName)
		}
		// A Release without the root must not make Flux delete it.
		if got := fluxField(obj, "metadata", "annotations", "kustomize.toolkit.fluxcd.io/prune"); got != "disabled" {
			t.Errorf("%s prune annotation = %v, want disabled", obj["kind"], got)
		}
	}
	ks := docs["Kustomization"]
	// Without --prune the root leaves removed children running, and it never
	// waits on them.
	if got := fluxField(ks, "spec", "prune"); got != false {
		t.Errorf("root Kustomization prune = %v, want false", got)
	}
	if got := fluxField(ks, "spec", "wait"); got != false {
		t.Errorf("root Kustomization wait = %v, want false", got)
	}
	if got := fluxField(docs["OCIRepository"], "spec", "insecure"); got != false {
		t.Errorf("root OCIRepository insecure = %v, want false", got)
	}
}

// --prune turns prune on, and changes nothing else.
func TestClusterFluxManifestPrune(t *testing.T) {
	const url = "oci://172.18.0.1:9092/space/orders"
	for name, build := range map[string]func(prune bool) []byte{
		"child": func(prune bool) []byte { return clusterFluxChildManifest("orders", url, "10m0s", true, prune) },
		"root":  func(prune bool) []byte { return clusterFluxRootManifest(url, "1m0s", false, prune) },
	} {
		without, with := fluxDocs(t, build(false)), fluxDocs(t, build(true))
		if got := fluxField(with["Kustomization"], "spec", "prune"); got != true {
			t.Errorf("%s Kustomization prune = %v with --prune, want true", name, got)
		}
		with["Kustomization"]["spec"].(map[string]any)["prune"] = false
		if !reflect.DeepEqual(with, without) {
			t.Errorf("%s manifest with --prune differs beyond prune:\n%v\n%v", name, with, without)
		}
	}
}

func TestClusterFluxCredsManifest(t *testing.T) {
	const url = "oci://172.18.0.1:9092"
	manifest, err := clusterFluxCredsManifest(url, "s3cr:et")
	if err != nil {
		t.Fatal(err)
	}
	var secret struct {
		Type       string            `json:"type"`
		StringData map[string]string `json:"stringData"`
	}
	if err := json.Unmarshal(manifest, &secret); err != nil {
		t.Fatal(err)
	}
	if secret.Type != "kubernetes.io/dockerconfigjson" {
		t.Errorf("type = %q, want kubernetes.io/dockerconfigjson", secret.Type)
	}
	var config struct {
		Auths map[string]struct {
			Username, Password, Auth string
		} `json:"auths"`
	}
	if err := json.Unmarshal([]byte(secret.StringData[".dockerconfigjson"]), &config); err != nil {
		t.Fatal(err)
	}
	// Flux finds the credentials by the host:port of the OCIRepository's URL.
	auth, ok := config.Auths["172.18.0.1:9092"]
	if !ok || len(config.Auths) != 1 {
		t.Fatalf("auths = %v; want one entry keyed 172.18.0.1:9092", config.Auths)
	}
	if auth.Username != "token" || auth.Password != "s3cr:et" {
		t.Errorf("auth = %+v", auth)
	}
	if want := base64.StdEncoding.EncodeToString([]byte("token:s3cr:et")); auth.Auth != want {
		t.Errorf("auth.auth = %q, want %q", auth.Auth, want)
	}

	if _, err := clusterFluxCredsManifest("not a url", "p"); err == nil {
		t.Error("clusterFluxCredsManifest() with no host: want an error")
	}
}

func TestClusterFluxEnvFile(t *testing.T) {
	// A Flux cluster's env file names no UI, so `cub cluster open` asks the
	// cluster Space which controller it runs.
	env := clusterParseEnvFile(clusterBuildFluxEnvFile("mycluster", "/tmp/mycluster.kubeconfig"))
	if env["KUBECONFIG"] != "/tmp/mycluster.kubeconfig" {
		t.Errorf("KUBECONFIG = %q", env["KUBECONFIG"])
	}
	if env["ARGOCD_SERVER"] != "" || env["SVELTOS_DASHBOARD"] != "" {
		t.Errorf("env = %v; want no controller UI", env)
	}
}

func TestClusterAppsSuffix(t *testing.T) {
	for controller, want := range map[string]string{
		clusterControllerArgo:    "-argo-apps",
		clusterControllerSveltos: "-sveltos-apps",
		clusterControllerFlux:    "-flux-apps",
	} {
		if got := clusterAppsSuffix(controller); got != want {
			t.Errorf("clusterAppsSuffix(%q) = %q, want %q", controller, got, want)
		}
	}
}
