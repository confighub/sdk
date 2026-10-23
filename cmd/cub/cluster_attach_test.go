// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"io"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestClusterKubectlConfigViewArgs(t *testing.T) {
	base := []string{"config", "view", "--minify", "--flatten", "--raw", "-o", "yaml"}
	cases := []struct {
		name       string
		kubeconfig string
		context    string
		want       []string
	}{
		{"defaults to kubectl's own loading rules", "", "", base},
		{"explicit kubeconfig", "/k/eu.yaml", "", append(append([]string{}, base...), "--kubeconfig", "/k/eu.yaml")},
		{"explicit context", "", "prod", append(append([]string{}, base...), "--context", "prod")},
		{"both", "/k/eu.yaml", "prod", append(append([]string{}, base...), "--kubeconfig", "/k/eu.yaml", "--context", "prod")},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := clusterKubectlConfigViewArgs(tc.kubeconfig, tc.context)
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("got %v, want %v", got, tc.want)
			}
		})
	}
}

func TestClusterKubeconfigServer(t *testing.T) {
	kubeconfig := []byte(`apiVersion: v1
kind: Config
clusters:
- name: kind-x
  cluster:
    server: https://127.0.0.1:55001
contexts: []
`)
	if got := clusterKubeconfigServer(kubeconfig); got != "https://127.0.0.1:55001" {
		t.Errorf("got %q", got)
	}
	// What `kubectl config view --minify` prints for a context that does not
	// exist: a config with no clusters.
	if got := clusterKubeconfigServer([]byte("apiVersion: v1\nkind: Config\nclusters: null\n")); got != "" {
		t.Errorf("empty config: got %q, want \"\"", got)
	}
}

func TestClusterAttachReachabilityProblem(t *testing.T) {
	cases := []struct {
		name      string
		hub       string
		apiServer string
		wantErr   bool
	}{
		{"remote hub, remote cluster", "https://hub.confighub.com", "https://ABC.gr7.us-west-2.eks.amazonaws.com", false},
		{"remote hub, local cluster", "https://hub.confighub.com", "https://127.0.0.1:55001", false},
		{"local hub, local cluster (kind)", "http://localhost:9090", "https://127.0.0.1:55001", false},
		{"local hub, remote cluster", "http://localhost:9090", "https://ABC.gr7.us-west-2.eks.amazonaws.com", true},
		{"unparseable api server is not a verdict", "http://localhost:9090", "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := clusterAttachReachabilityProblem(tc.hub, tc.apiServer)
			if (got != "") != tc.wantErr {
				t.Errorf("got %q, wantErr=%v", got, tc.wantErr)
			}
		})
	}
}

func TestClusterOriginOf(t *testing.T) {
	cases := []struct {
		name        string
		annotations map[string]string
		want        string
	}{
		{"absent means kind: every Space before the annotation was a kind cluster", map[string]string{}, clusterOriginKind},
		{"nil map", nil, clusterOriginKind},
		{"kind", map[string]string{clusterAnnotationOrigin: clusterOriginKind}, clusterOriginKind},
		{"attach", map[string]string{clusterAnnotationOrigin: clusterOriginAttach}, clusterOriginAttach},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := clusterOriginOf(tc.annotations); got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestClusterRecordRoundTrip(t *testing.T) {
	t.Setenv("CUB_CONFIG", t.TempDir())
	if rec, err := clusterReadRecord("none"); err != nil || rec != nil {
		t.Fatalf("missing record: got %v, %v; want nil, nil", rec, err)
	}
	in := clusterRecord{
		Name: "edge", SpaceSlug: "edge-space", ServerURL: "https://hub.example.com/",
		OrganizationID: "org-1", Origin: clusterOriginAttach, Controller: clusterControllerArgo,
		CreatedAt: time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC),
	}
	if err := clusterWriteRecord(in); err != nil {
		t.Fatal(err)
	}
	got, err := clusterReadRecord("edge")
	if err != nil || got == nil {
		t.Fatalf("read: %v, %v", got, err)
	}
	// The trailing slash is normalized away so coordinates compare equal
	// however the server URL was typed.
	in.ServerURL = "https://hub.example.com"
	if !reflect.DeepEqual(*got, in) {
		t.Errorf("round trip changed the record:\n got %+v\nwant %+v", *got, in)
	}
	if !clusterRecordMatchesCoordinate(got, Coordinate{ServerURL: "https://hub.example.com/", OrganizationID: "org-1", User: "someone-else"}) {
		t.Error("the user must not be part of the match")
	}
	if clusterRecordMatchesCoordinate(got, Coordinate{ServerURL: "https://hub.example.com", OrganizationID: "org-2"}) {
		t.Error("a different organization must not match")
	}
	if err := clusterRemoveRecord("edge"); err != nil {
		t.Fatal(err)
	}
	if err := clusterRemoveRecord("edge"); err != nil {
		t.Errorf("removing twice must be a no-op, got %v", err)
	}
}

func TestClusterStatusOf(t *testing.T) {
	// kindPresent must not influence an attached cluster's status: it is not a
	// kind cluster, whatever kind happens to have under that name. A cluster
	// wired elsewhere is reported as such, never as drift.
	rec := &clusterRecord{Name: "x", Origin: clusterOriginAttach}
	cases := []struct {
		name string
		row  clusterListRow
		want string
	}{
		{"attached, ready", clusterListRow{origin: clusterOriginAttach, inContext: true, kubeconfig: "/k", spaceSlug: "s"}, "Ready"},
		{"attached, ready despite a same-named kind cluster", clusterListRow{origin: clusterOriginAttach, inContext: true, kubeconfig: "/k", spaceSlug: "s", kindPresent: true}, "Ready"},
		{"attached elsewhere, no creds here", clusterListRow{origin: clusterOriginAttach, inContext: true, spaceSlug: "s"}, "No local credentials (attached elsewhere)"},
		{"kind rows keep the drift logic", clusterListRow{origin: clusterOriginKind, inContext: true, kubeconfig: "/k", spaceSlug: "s"}, "Drift: kind cluster missing"},
		{"other context is not drift", clusterListRow{origin: clusterOriginAttach, record: rec, kubeconfig: "/k"}, "Other context (not checked)"},
		{"other context, kind gone", clusterListRow{origin: clusterOriginKind, record: &clusterRecord{Origin: clusterOriginKind}, kubeconfig: "/k"}, "Other context; kind cluster missing"},
		{"legacy kubeconfig with nothing to match", clusterListRow{origin: clusterOriginKind, kubeconfig: "/k", kindPresent: true}, "No record and no Space in current context (list in its context to adopt it)"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := clusterStatusOf(&tc.row); got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestClusterAttachEnvFile(t *testing.T) {
	// The attached-cluster env file carries KUBECONFIG and the context, and no
	// Argo endpoint: `cub cluster open` must then fall back rather than read
	// empty values.
	env := clusterParseEnvFile(clusterBuildAttachEnvFile("edge", "/tmp/edge.kubeconfig"))
	if env["KUBECONFIG"] != "/tmp/edge.kubeconfig" {
		t.Errorf("KUBECONFIG = %q", env["KUBECONFIG"])
	}
	for _, k := range []string{"ARGOCD_SERVER", "ARGOCD_PASSWORD"} {
		if _, ok := env[k]; ok {
			t.Errorf("%s should be absent for an attached cluster, got %q", k, env[k])
		}
	}
	// Every env file ends with the context line; the parser reads it back.
	if line := clusterEnvFileContextLine(); line != "" {
		if !strings.HasPrefix(line, "export CUB_CONTEXT=") {
			t.Errorf("context line = %q", line)
		}
		if env["CUB_CONTEXT"] == "" {
			t.Errorf("CUB_CONTEXT missing from parsed env: %v", env)
		}
	}
}

func TestClusterArgoInstallURL(t *testing.T) {
	if got, want := clusterArgoInstallURL("stable"), "https://raw.githubusercontent.com/argoproj/argo-cd/stable/manifests/install.yaml"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
	if got, want := clusterArgoInstallURL("v3.4.5"), "https://raw.githubusercontent.com/argoproj/argo-cd/v3.4.5/manifests/install.yaml"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestClusterBotSpaceOf(t *testing.T) {
	if got := clusterBotSpaceOf(&clusterSpace{ClusterName: "dev", BotSpace: "k8sbot-dev"}); got != "k8sbot-dev" {
		t.Errorf("recorded bot space ignored: %q", got)
	}
	if got := clusterBotSpaceOf(&clusterSpace{ClusterName: "dev"}); got != "argobot-dev" {
		t.Errorf("convention fallback: %q", got)
	}
}

func TestClusterEKSAccessEntryPrincipal(t *testing.T) {
	cases := map[string]string{
		// An SSO login reports the session, not the role. EKS wants the role.
		"arn:aws:sts::025066259430:assumed-role/AWSReservedSSO_Admin_abc/jesper@example.com": "arn:aws:iam::025066259430:role/AWSReservedSSO_Admin_abc",
		"arn:aws:sts::025066259430:assumed-role/SomeRole/session-name":                       "arn:aws:iam::025066259430:role/SomeRole",
		// An IAM user is already the right shape.
		"arn:aws:iam::025066259430:user/Local": "arn:aws:iam::025066259430:user/Local",
		"arn:aws:iam::025066259430:role/Plain": "arn:aws:iam::025066259430:role/Plain",
		// Partitions other than aws must survive.
		"arn:aws-us-gov:sts::1:assumed-role/R/s": "arn:aws-us-gov:iam::1:role/R",
		// Anything unrecognised is returned unchanged rather than mangled.
		"":                                  "",
		"not-an-arn":                        "not-an-arn",
		"arn:aws:sts::1:federated-user/bob": "arn:aws:sts::1:federated-user/bob",
	}
	for in, want := range cases {
		if got := clusterEKSAccessEntryPrincipal(in); got != want {
			t.Errorf("clusterEKSAccessEntryPrincipal(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestClusterAWSArgs(t *testing.T) {
	cases := []struct {
		name    string
		profile string
		region  string
		want    []string
	}{
		{"neither: the CLI's own defaults apply", "", "", []string{"eks", "list-clusters"}},
		{"profile", "cubdev", "", []string{"--profile", "cubdev", "eks", "list-clusters"}},
		{"region", "", "us-west-2", []string{"--region", "us-west-2", "eks", "list-clusters"}},
		{"both", "cubdev", "us-west-2", []string{"--profile", "cubdev", "--region", "us-west-2", "eks", "list-clusters"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := clusterAWSArgs(tc.profile, tc.region, "eks", "list-clusters"); !reflect.DeepEqual(got, tc.want) {
				t.Errorf("got %v, want %v", got, tc.want)
			}
		})
	}
}

func TestClusterAttachFlagCombinations(t *testing.T) {
	// The flag checks run before anything is touched, so they can be exercised
	// without a server or a cluster.
	cases := []struct {
		name string
		opts clusterAttachOptions
		want string
	}{
		{"eks with kubeconfig", clusterAttachOptions{name: "x", controller: clusterControllerArgo, provider: clusterProviderEKS, kubeconfig: "/k"}, "--provider kubeconfig only"},
		{"eks with context", clusterAttachOptions{name: "x", controller: clusterControllerArgo, provider: clusterProviderEKS, kubeContext: "ctx"}, "--provider kubeconfig only"},
		{"grant-access without eks", clusterAttachOptions{name: "x", controller: clusterControllerArgo, provider: clusterProviderKubeconfig, grantAccess: true}, "--provider eks only"},
		{"aws-profile with kind", clusterAttachOptions{name: "x", controller: clusterControllerArgo, provider: clusterProviderKind, awsProfile: "p"}, "--provider eks only"},
		{"cluster with kubeconfig provider", clusterAttachOptions{name: "x", controller: clusterControllerArgo, provider: clusterProviderKubeconfig, cluster: "c"}, "--cluster applies to"},
		{"unknown provider", clusterAttachOptions{name: "x", controller: clusterControllerArgo, provider: "gke"}, "--provider \"gke\""},
		{"unknown controller", clusterAttachOptions{name: "x", controller: "fleet", provider: clusterProviderKubeconfig}, "--controller \"fleet\""},
		{"sveltos with eks flags", clusterAttachOptions{name: "x", controller: clusterControllerSveltos, provider: clusterProviderKubeconfig, awsRegion: "r"}, "--provider eks only"},
		{"flux with eks flags", clusterAttachOptions{name: "x", controller: clusterControllerFlux, provider: clusterProviderKind, kubeContext: "c"}, "--provider kubeconfig only"},
		{"no name", clusterAttachOptions{controller: clusterControllerArgo}, "a cluster name is required"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := clusterAttachRun(io.Discard, tc.opts)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("got %v, want an error containing %q", err, tc.want)
			}
		})
	}
}

func TestClusterControllerFrom(t *testing.T) {
	cases := []struct {
		name  string
		rec   *clusterRecord
		match *clusterSpace
		want  string
	}{
		{"neither: Argo CD, from before controllers were recorded", nil, nil, clusterControllerArgo},
		{"record only", &clusterRecord{Controller: clusterControllerFlux}, nil, clusterControllerFlux},
		{"space wins over record", &clusterRecord{Controller: clusterControllerFlux}, &clusterSpace{Controller: clusterControllerSveltos}, clusterControllerSveltos},
		{"space without one falls back to record", &clusterRecord{Controller: clusterControllerFlux}, &clusterSpace{}, clusterControllerFlux},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := clusterControllerFrom(tc.rec, tc.match); got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestClusterAttachProviderClusterDefaultsToName(t *testing.T) {
	for _, provider := range []string{clusterProviderEKS, clusterProviderKind} {
		opts := clusterAttachOptions{name: "inference-demo", provider: provider}
		if err := clusterAttachCheckProviderFlags(&opts); err != nil {
			t.Fatalf("%s: %v", provider, err)
		}
		if opts.cluster != "inference-demo" {
			t.Errorf("%s: cluster = %q, want the cub name", provider, opts.cluster)
		}
	}
	opts := clusterAttachOptions{name: "inference", provider: clusterProviderEKS, cluster: "inference-demo"}
	if err := clusterAttachCheckProviderFlags(&opts); err != nil || opts.cluster != "inference-demo" {
		t.Errorf("an explicit --cluster must win: %q, %v", opts.cluster, err)
	}
}

func TestClusterNameArg(t *testing.T) {
	cases := []struct {
		name     string
		args     []string
		flag     string
		required bool
		want     string
		wantErr  string
	}{
		{"positional", []string{"dev"}, "", true, "dev", ""},
		{"flag alias", nil, "dev", true, "dev", ""},
		{"both, agreeing", []string{"dev"}, "dev", true, "dev", ""},
		{"both, disagreeing", []string{"dev"}, "prod", true, "", "named twice"},
		{"neither, required", nil, "", true, "", "a cluster name is required"},
		{"neither, optional (up generates one)", nil, "", false, "", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := clusterNameArg(tc.args, tc.flag, "x", tc.required)
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Errorf("got %v, want an error containing %q", err, tc.wantErr)
				}
				return
			}
			if err != nil || got != tc.want {
				t.Errorf("got %q, %v; want %q", got, err, tc.want)
			}
		})
	}
}

func TestClusterUnitTargetUIAnnotations(t *testing.T) {
	cases := []struct {
		name              string
		targetAnnotations map[string]string
		unitName          string
		want              map[string]string
	}{
		{
			"argo application",
			map[string]string{clusterAnnotationTargetUI: "http://localhost:31080/applications/argocd/{slug}"},
			"web-dev",
			map[string]string{clusterAnnotationTargetUI: "http://localhost:31080/applications/argocd/web-dev"},
		},
		{
			"sveltos root profile",
			map[string]string{clusterAnnotationTargetUI: clusterSveltosTargetUI(31080)},
			clusterSveltosRootProfile,
			map[string]string{clusterAnnotationTargetUI: "http://localhost:31080/sveltos/profile/confighub-root/ClusterProfile"},
		},
		{"target without a UI link", map[string]string{clusterAnnotationController: clusterControllerFlux}, "web-dev", nil},
		{"target without annotations", nil, "web-dev", nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := clusterUnitTargetUIAnnotations(tc.targetAnnotations, tc.unitName)
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("got %v, want %v", got, tc.want)
			}
		})
	}
}
