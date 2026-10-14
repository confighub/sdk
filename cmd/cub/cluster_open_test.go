// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestClusterParseEnvFile(t *testing.T) {
	// Exactly what `cub cluster up` writes, so a change to clusterBuildEnvFile
	// that this parser cannot read shows up here.
	env := clusterParseEnvFile(clusterBuildEnvFile("mycluster", "/tmp/mycluster.kubeconfig", 30042, `p@ss"word`))

	want := map[string]string{
		"KUBECONFIG":      "/tmp/mycluster.kubeconfig",
		"ARGOCD_SERVER":   "localhost:30042",
		"ARGOCD_USERNAME": "admin",
		"ARGOCD_PASSWORD": `p@ss"word`,
		"ARGOCD_OPTS":     "--plaintext --grpc-web",
	}
	for k, v := range want {
		if env[k] != v {
			t.Errorf("env[%q] = %q, want %q", k, env[k], v)
		}
	}
	if _, ok := env["# cub cluster"]; ok {
		t.Errorf("comment lines should be skipped, got %v", env)
	}
}

func TestClusterArgoURLFromEnvFile(t *testing.T) {
	cases := []struct {
		name string
		env  map[string]string
		want string
	}{
		{"nodeport", map[string]string{"ARGOCD_SERVER": "localhost:30042"}, "http://localhost:30042"},
		{"with scheme", map[string]string{"ARGOCD_SERVER": "https://argo.example.com"}, "https://argo.example.com"},
	}
	for _, c := range cases {
		got, err := clusterArgoURL("mycluster", c.env)
		if err != nil {
			t.Errorf("%s: %v", c.name, err)
			continue
		}
		if got != c.want {
			t.Errorf("%s: clusterArgoURL = %q, want %q", c.name, got, c.want)
		}
	}
}

func TestClusterSveltosEnvFileOpensDashboard(t *testing.T) {
	// Exactly what `cub cluster up --controller sveltos` writes: the env file alone
	// says the cluster runs Sveltos, and where its dashboard is.
	env := clusterParseEnvFile(clusterBuildSveltosEnvFile("mycluster", "/tmp/mycluster.kubeconfig", 30002))

	controller, _, err := clusterOpenController("mycluster", env)
	if err != nil || controller != clusterControllerSveltos {
		t.Fatalf("clusterOpenController() = %q, %v; want a Sveltos cluster from the env file", controller, err)
	}
	got, err := clusterSveltosDashboardEndpoint("mycluster", env, nil)
	if err != nil || got != "http://localhost:30002" {
		t.Errorf("clusterSveltosDashboardEndpoint() = %q, %v; want http://localhost:30002", got, err)
	}

	argoEnv := clusterParseEnvFile(clusterBuildEnvFile("mycluster", "/tmp/mycluster.kubeconfig", 30042, "pw"))
	if controller, _, err := clusterOpenController("mycluster", argoEnv); err != nil || controller != clusterControllerArgo {
		t.Errorf("clusterOpenController() = %q, %v for an Argo env file; want Argo", controller, err)
	}
}

func TestClusterSveltosDashboardEndpointFromSpace(t *testing.T) {
	space := &clusterSpace{SpaceSlug: "mycluster", Controller: clusterControllerSveltos, SveltosPort: "30002"}
	got, err := clusterSveltosDashboardEndpoint("mycluster", map[string]string{}, space)
	if err != nil || got != "http://localhost:30002" {
		t.Errorf("clusterSveltosDashboardEndpoint() = %q, %v; want http://localhost:30002", got, err)
	}

	space.SveltosPort = ""
	if _, err := clusterSveltosDashboardEndpoint("mycluster", map[string]string{}, space); err == nil ||
		!strings.Contains(err.Error(), clusterAnnotationSveltosPort) {
		t.Errorf("clusterSveltosDashboardEndpoint() error = %v; want one naming %s", err, clusterAnnotationSveltosPort)
	}
	if _, err := clusterSveltosDashboardEndpoint("mycluster", map[string]string{}, nil); err == nil {
		t.Error("clusterSveltosDashboardEndpoint() with no env file and no Space: want an error")
	}
}

func TestClusterResolveName(t *testing.T) {
	t.Setenv("CUB_CONFIG", t.TempDir())

	// Explicit name wins, and needs no local kubeconfig.
	if got, err := clusterResolveName("named"); err != nil || got != "named" {
		t.Errorf(`clusterResolveName("named") = %q, %v; want "named", nil`, got, err)
	}

	// No clusters at all.
	if _, err := clusterResolveName(""); err == nil || !strings.Contains(err.Error(), "no cub-managed clusters") {
		t.Errorf("want a no-clusters error, got %v", err)
	}

	if err := clusterEnsureKubeconfigDir(); err != nil {
		t.Fatal(err)
	}
	writeKubeconfig := func(name string) {
		if err := os.WriteFile(clusterKubeconfigPath(name), []byte("{}"), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	// Exactly one cluster: resolved implicitly.
	writeKubeconfig("only-one")
	if got, err := clusterResolveName(""); err != nil || got != "only-one" {
		t.Errorf(`clusterResolveName("") = %q, %v; want "only-one", nil`, got, err)
	}

	// More than one: ambiguous, and the error names the candidates.
	writeKubeconfig("another")
	_, err := clusterResolveName("")
	if err == nil {
		t.Fatal("want an ambiguity error with two clusters, got nil")
	}
	for _, want := range []string{"only-one", "another"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q should name cluster %q", err, want)
		}
	}
}

func TestClusterReadEnvFileMissing(t *testing.T) {
	t.Setenv("CUB_CONFIG", t.TempDir())
	if env := clusterReadEnvFile("gone"); len(env) != 0 {
		t.Errorf("missing env file should parse as empty, got %v", env)
	}
}

func TestClusterArgoPasswordFromEnvFile(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("CUB_CONFIG", dir)
	if err := clusterEnsureKubeconfigDir(); err != nil {
		t.Fatal(err)
	}
	contents := clusterBuildEnvFile("mycluster", filepath.Join(dir, "clusters", "mycluster.kubeconfig"), 30042, "s3cret")
	if err := os.WriteFile(clusterEnvFilePath("mycluster"), []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}

	got, err := clusterArgoPassword("mycluster", clusterReadEnvFile("mycluster"))
	if err != nil || got != "s3cret" {
		t.Errorf(`clusterArgoPassword = %q, %v; want "s3cret", nil`, got, err)
	}

	// No env file and no kubeconfig: reported, not fatal — the caller still opens
	// the UI and prints just the username.
	if _, err := clusterArgoPassword("other", nil); err == nil {
		t.Error("want an error when neither the env file nor a kubeconfig exists")
	}
}
