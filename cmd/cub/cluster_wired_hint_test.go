// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"encoding/json"
	"os"
	"slices"
	"strings"
	"testing"
)

func TestClusterDeleteSecretCommands(t *testing.T) {
	for _, tc := range []struct {
		name           string
		opts           clusterAttachOptions
		currentContext string
		want           []string
		contextUnknown bool
	}{
		{
			name:           "kubeconfig provider, default kubeconfig: the context attach read is named",
			opts:           clusterAttachOptions{provider: clusterProviderKubeconfig},
			currentContext: "prod-east",
			want:           []string{"kubectl --context prod-east -n argocd delete secret confighub-oci-creds"},
		},
		{
			name:           "kubeconfig provider, --kubeconfig and --kube-context",
			opts:           clusterAttachOptions{provider: clusterProviderKubeconfig, kubeconfig: "/home/me/my clusters.yaml", kubeContext: "lab"},
			currentContext: "lab",
			want:           []string{"kubectl --kubeconfig '/home/me/my clusters.yaml' --context lab -n argocd delete secret confighub-oci-creds"},
		},
		{
			name: "kubeconfig provider, context unreadable: --kube-context is used",
			opts: clusterAttachOptions{provider: clusterProviderKubeconfig, kubeContext: "lab"},
			want: []string{"kubectl --context lab -n argocd delete secret confighub-oci-creds"},
		},
		{
			name:           "kubeconfig provider, no context known: said so",
			opts:           clusterAttachOptions{provider: clusterProviderKubeconfig},
			want:           []string{"kubectl -n argocd delete secret confighub-oci-creds"},
			contextUnknown: true,
		},
		{
			name: "kind: credentials fetched from kind into a directory of their own",
			opts: clusterAttachOptions{provider: clusterProviderKind, cluster: "dev"},
			want: []string{
				"CUB_TMP=$(mktemp -d)",
				`kind get kubeconfig --name dev > "$CUB_TMP/kubeconfig"`,
				`kubectl --kubeconfig "$CUB_TMP/kubeconfig" -n argocd delete secret confighub-oci-creds`,
				`rm -r "$CUB_TMP"`,
			},
		},
		{
			name: "eks: credentials fetched from the AWS CLI with the attach's profile and region",
			opts: clusterAttachOptions{provider: clusterProviderEKS, cluster: "prod", awsProfile: "ops", awsRegion: "us-west-2"},
			want: []string{
				"CUB_TMP=$(mktemp -d)",
				`aws --profile ops --region us-west-2 eks update-kubeconfig --name prod --kubeconfig "$CUB_TMP/kubeconfig"`,
				`kubectl --kubeconfig "$CUB_TMP/kubeconfig" -n argocd delete secret confighub-oci-creds`,
				`rm -r "$CUB_TMP"`,
			},
		},
		{
			name: "a provider the hint does not know: no command",
			opts: clusterAttachOptions{provider: "gke", cluster: "prod"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			access := clusterAccessForHint(&tc.opts, tc.currentContext)
			got := clusterDeleteSecretCommands(access, "argocd", "confighub-oci-creds")
			if !slices.Equal(got, tc.want) {
				t.Errorf("commands =\n  %s\nwant\n  %s", strings.Join(got, "\n  "), strings.Join(tc.want, "\n  "))
			}
			if access.contextUnknown != tc.contextUnknown {
				t.Errorf("contextUnknown = %v, want %v", access.contextUnknown, tc.contextUnknown)
			}
		})
	}
}

func TestClusterDetachableHere(t *testing.T) {
	const org, server = "org-1", "https://127.0.0.1:41097"
	attached := &clusterRecord{Name: "lab", Origin: clusterOriginAttach, OrganizationID: org}
	for _, tc := range []struct {
		name         string
		rec          *clusterRecord
		localServer  string
		secretOrg    string
		attachServer string
		want         bool
	}{
		{"the same attachment: record's org and kubeconfig's server match", attached, server, org, server, true},
		// The state a rolled-back attach leaves: detach would skip the cluster.
		{"no record", nil, server, org, server, false},
		{"record but no kubeconfig", attached, "", org, server, false},
		// detach refuses it, and down would delete the cluster being attached.
		{"a kind cluster cub created", &clusterRecord{Name: "lab", Origin: clusterOriginKind, OrganizationID: org}, server, org, server, false},
		// The same name used for another cluster: detaching it would cut off a
		// live attachment and leave this Secret in place.
		{"same name, another cluster", attached, "https://10.0.0.5:6443", org, server, false},
		{"same name, another organization", attached, server, "org-2", server, false},
		{"Secret records no organization", attached, server, "", server, false},
		{"attach's kubeconfig unreadable", attached, server, org, "", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := clusterDetachableHere(tc.rec, tc.localServer, tc.secretOrg, tc.attachServer); got != tc.want {
				t.Errorf("clusterDetachableHere = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestClusterLocalDetachName(t *testing.T) {
	t.Setenv("CUB_CONFIG", t.TempDir())
	if err := clusterEnsureKubeconfigDir(); err != nil {
		t.Fatal(err)
	}
	write := func(path string, data []byte) {
		t.Helper()
		if err := os.WriteFile(path, data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	const org = "org-1"
	record := func(name, origin string) []byte {
		data, err := json.Marshal(clusterRecord{Name: name, Origin: origin, OrganizationID: org})
		if err != nil {
			t.Fatal(err)
		}
		return data
	}
	kubeconfig := func(server string) []byte {
		return []byte("apiVersion: v1\nkind: Config\nclusters:\n- name: c\n  cluster:\n    server: " + server + "\n")
	}
	const thisCluster, otherCluster = "https://127.0.0.1:41097", "https://10.0.0.5:6443"
	write(clusterRecordPath("attached"), record("attached", clusterOriginAttach))
	write(clusterKubeconfigPath("attached"), kubeconfig(thisCluster))
	write(clusterRecordPath("other-cluster"), record("other-cluster", clusterOriginAttach))
	write(clusterKubeconfigPath("other-cluster"), kubeconfig(otherCluster))
	write(clusterRecordPath("kind-made"), record("kind-made", clusterOriginKind))
	write(clusterKubeconfigPath("kind-made"), kubeconfig(thisCluster))
	write(clusterRecordPath("no-kubeconfig"), record("no-kubeconfig", clusterOriginAttach))
	// The copy attach is using of the cluster being attached.
	attachKubeconfig := clusterKubeconfigPath("being-attached")
	write(attachKubeconfig, kubeconfig(thisCluster))

	for _, tc := range []struct {
		name, secretOrg, want string
	}{
		{"attached", org, "attached"},
		{"attached", "org-2", ""},
		{"other-cluster", org, ""},
		{"kind-made", org, ""},
		{"no-kubeconfig", org, ""},
		{"elsewhere", org, ""},
		{"", org, ""},
	} {
		if got := clusterLocalDetachName(tc.name, tc.secretOrg, attachKubeconfig); got != tc.want {
			t.Errorf("clusterLocalDetachName(%q, %q) = %q, want %q", tc.name, tc.secretOrg, got, tc.want)
		}
	}
}

func TestClusterWiredHint(t *testing.T) {
	cmds := []string{"kubectl --context lab -n argocd delete secret confighub-oci-creds"}

	got := clusterWiredHint("", cmds, false, "argocd", "confighub-oci-creds")
	if strings.Contains(got, "cub cluster detach") {
		t.Errorf("hint suggests detach with no record on this machine: %q", got)
	}
	if !strings.HasSuffix(got, "\n  "+cmds[0]) {
		t.Errorf("hint does not end with the delete command: %q", got)
	}

	got = clusterWiredHint("lab", cmds, false, "argocd", "confighub-oci-creds")
	if !strings.Contains(got, "'cub cluster detach lab' (with --force if its Space is gone)") || !strings.Contains(got, cmds[0]) {
		t.Errorf("hint lacks detach or the delete command: %q", got)
	}

	got = clusterWiredHint("", []string{"kubectl -n argocd delete secret confighub-oci-creds"}, true, "argocd", "confighub-oci-creds")
	if !strings.Contains(got, "with the cluster being attached as the current kube context") {
		t.Errorf("hint does not say which context a command with no --context needs: %q", got)
	}

	got = clusterWiredHint("", nil, false, "argocd", "confighub-oci-creds")
	if !strings.HasSuffix(got, "delete Secret argocd/confighub-oci-creds from the cluster.") {
		t.Errorf("hint with no command does not name the Secret: %q", got)
	}
}
