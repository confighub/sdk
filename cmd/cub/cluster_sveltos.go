// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"strings"

	goclientnew "github.com/confighub/sdk/core/openapi/goclient-new"
)

// Sveltos install + configuration inside a kind cluster, so it can pull config
// from ConfigHub's OCI registry. The `cub cluster up --controller sveltos`
// counterpart of cluster_argo.go: all operations shell out to kubectl using a
// per-cluster kubeconfig.
//
// Sveltos registers the cluster it runs in as SveltosCluster mgmt/mgmt, and
// ClusterProfiles live in that management cluster. The root profile deploys
// the apps Space's ClusterProfiles there (deploymentType Local); each of those
// child profiles pulls a deployment Space's Release and deploys it to the
// clusters it names (deploymentType Remote).

const (
	// clusterSveltosVersion pins the Sveltos release. Its addon controller
	// reads OCI policyRefs over plain HTTP (remoteURL.plainHTTP), which a local
	// cub server needs, and applies CRDs before the resources that use them.
	clusterSveltosVersion = "v1.16.0"

	clusterSveltosManifestURL  = "https://raw.githubusercontent.com/projectsveltos/sveltos/" + clusterSveltosVersion + "/manifest/manifest.yaml"
	clusterSveltosDashboardURL = "https://raw.githubusercontent.com/projectsveltos/sveltos/" + clusterSveltosVersion + "/manifest/dashboard-manifest.yaml"

	// clusterSveltosServiceMonitorCRDURL is the Prometheus Operator
	// ServiceMonitor CRD. The Sveltos manifest includes ServiceMonitors, so
	// without the CRD its kubectl apply exits non-zero.
	clusterSveltosServiceMonitorCRDURL = "https://raw.githubusercontent.com/prometheus-operator/prometheus-operator/v0.86.0/example/prometheus-operator-crd/monitoring.coreos.com_servicemonitors.yaml"

	// clusterSveltosNamespace is where Sveltos runs, and where the profiles'
	// registry credentials live.
	clusterSveltosNamespace = "projectsveltos"

	// clusterSveltosGatewaySecret holds the cluster service account's token,
	// which every ClusterProfile's remoteURL.secretRef names.
	clusterSveltosGatewaySecret = "confighub-gateway"

	// clusterSveltosDashboardAccount is the ServiceAccount whose token logs in
	// to the Sveltos dashboard.
	clusterSveltosDashboardAccount = "platform-admin"

	// clusterSveltosRootProfile is the name of the root ClusterProfile.
	clusterSveltosRootProfile = "confighub-root"

	// clusterSveltosAppsSuffix is the Sveltos counterpart of
	// clusterArgoAppsSuffix: the apps Space holding the ClusterProfile Units.
	clusterSveltosAppsSuffix = "-sveltos-apps"

	// clusterSveltosFetchInterval is Sveltos's own poll interval
	// (remoteURL.interval) when polling is the only way a publish reaches the
	// cluster: a cluster brought up with --no-argobot, or one from before the
	// interval was recorded on its target.
	clusterSveltosFetchInterval = "1m0s"

	// clusterSveltosPollIntervalWithArgobot is Sveltos's own poll interval
	// (remoteURL.interval) when argobot is installed. argobot's force sync is
	// event-driven: it redeploys a profile on each publish, which fetches the new
	// Release within seconds. So Sveltos's polling only catches what argobot
	// missed, and can be infrequent.
	clusterSveltosPollIntervalWithArgobot = "10m0s"
)

// clusterSveltosInstall applies the ServiceMonitor CRD, the Sveltos manifest
// and the dashboard manifest, creates the dashboard's admin ServiceAccount,
// waits for every Sveltos Deployment to be Available, and exposes the dashboard
// on the given NodePort.
func clusterSveltosInstall(ctx context.Context, kubeconfigPath string, nodePort int, out io.Writer) error {
	fmt.Fprintln(out, "Applying the ServiceMonitor CRD the Sveltos manifest needs...")
	if err := clusterKubectl(ctx, kubeconfigPath, nil, out,
		"apply", "--server-side", "-f", clusterSveltosServiceMonitorCRDURL); err != nil {
		return fmt.Errorf("apply ServiceMonitor CRD: %w", err)
	}

	fmt.Fprintf(out, "Applying Sveltos install manifest from %s...\n", clusterSveltosManifestURL)
	if err := clusterKubectl(ctx, kubeconfigPath, nil, out,
		"apply", "-f", clusterSveltosManifestURL); err != nil {
		return fmt.Errorf("apply sveltos install: %w", err)
	}

	fmt.Fprintf(out, "Applying Sveltos dashboard manifest from %s...\n", clusterSveltosDashboardURL)
	if err := clusterKubectl(ctx, kubeconfigPath, nil, out,
		"apply", "-f", clusterSveltosDashboardURL); err != nil {
		return fmt.Errorf("apply sveltos dashboard: %w", err)
	}

	fmt.Fprintf(out, "Creating ServiceAccount %q for the dashboard login...\n", clusterSveltosDashboardAccount)
	account := fmt.Sprintf(`apiVersion: v1
kind: ServiceAccount
metadata:
  name: %[1]s
  namespace: default
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRoleBinding
metadata:
  name: %[1]s-access
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: ClusterRole
  name: cluster-admin
subjects:
- kind: ServiceAccount
  name: %[1]s
  namespace: default
`, clusterSveltosDashboardAccount)
	if err := clusterKubectlApply(ctx, kubeconfigPath, []byte(account), out); err != nil {
		return err
	}

	fmt.Fprintf(out, "Patching dashboard Service to NodePort %d...\n", nodePort)
	svcPatch := fmt.Sprintf(
		`{"spec":{"type":"NodePort","ports":[{"port":80,"targetPort":5173,"protocol":"TCP","nodePort":%d}]}}`,
		nodePort)
	if err := clusterKubectl(ctx, kubeconfigPath, nil, out,
		"patch", "service", "dashboard", "-n", clusterSveltosNamespace,
		"--type", "merge", "-p", svcPatch); err != nil {
		return err
	}

	fmt.Fprintln(out, "Waiting for the Sveltos Deployments to be Available (timeout 5m)...")
	if err := clusterKubectl(ctx, kubeconfigPath, nil, out,
		"wait", "--for=condition=Available", "--timeout=5m",
		"deployment", "--all", "-n", clusterSveltosNamespace); err != nil {
		return fmt.Errorf("wait for sveltos: %w", err)
	}
	return nil
}

// clusterSveltosDashboardEndpoint resolves a Sveltos cluster's dashboard
// endpoint, preferring the env file's SVELTOS_DASHBOARD and falling back to the
// dashboard port annotation on the cluster's Space.
func clusterSveltosDashboardEndpoint(name string, env map[string]string, space *clusterSpace) (string, error) {
	if url := env["SVELTOS_DASHBOARD"]; url != "" {
		return url, nil
	}
	if space == nil {
		return "", fmt.Errorf("cluster %q: no SVELTOS_DASHBOARD in %s and no cub-managed Space for it in the current cub context (are you in the right context? see 'cub cluster list')",
			name, clusterEnvFilePath(name))
	}
	if space.SveltosPort == "" {
		return "", fmt.Errorf("cluster %q: space %q has no %s annotation, so its Sveltos dashboard port is unknown",
			name, space.SpaceSlug, clusterAnnotationSveltosPort)
	}
	return "http://localhost:" + space.SveltosPort, nil
}

// clusterSveltosDashboardTokenDuration is how long a dashboard login token
// from `cub cluster open` is valid.
const clusterSveltosDashboardTokenDuration = "24h"

// clusterSveltosDashboardToken creates a fresh login token for the Sveltos
// dashboard from its ServiceAccount. Tokens expire, so one is created each time
// rather than stored.
func clusterSveltosDashboardToken(ctx context.Context, name string) (string, error) {
	kubeconfigPath := clusterKubeconfigPath(name)
	if _, err := os.Stat(kubeconfigPath); err != nil {
		return "", fmt.Errorf("no kubeconfig at %s to create a dashboard token with", kubeconfigPath)
	}
	token, err := clusterKubectlOutput(ctx, kubeconfigPath,
		"create", "token", clusterSveltosDashboardAccount, "-n", "default",
		"--duration="+clusterSveltosDashboardTokenDuration)
	if err != nil {
		return "", fmt.Errorf("create a dashboard token: %w", err)
	}
	return strings.TrimSpace(string(token)), nil
}

// clusterSveltosApplyOCICreds installs the Secret the ClusterProfiles'
// remoteURL.secretRef names: the cluster service account's token as the
// basic-auth password for the ConfigHub OCI registry, under the username
// "token" the registry accepts it with.
func clusterSveltosApplyOCICreds(ctx context.Context, kubeconfigPath, pullToken string, out io.Writer) error {
	secret := fmt.Sprintf(`apiVersion: v1
kind: Secret
metadata:
  name: %s
  namespace: %s
type: addons.projectsveltos.io/cluster-profile
stringData:
  username: token
  password: %s
`, clusterSveltosGatewaySecret, clusterSveltosNamespace, pullToken)
	fmt.Fprintf(out, "Applying %s Secret to the %s namespace...\n", clusterSveltosGatewaySecret, clusterSveltosNamespace)
	return clusterKubectlApply(ctx, kubeconfigPath, []byte(secret), out)
}

// clusterSveltosChildProfileManifest returns the YAML for the ClusterProfile
// that deploys a deployment Space's Release to the cluster: the Sveltos
// counterpart of variantArgoAppManifest. It is named after the Space, which is
// how argobot finds the profile to redeploy when that Space is published.
// continueOnError lets one object that fails, such as a custom resource whose
// controller is not ready yet, leave the rest of the Release applied.
// pollInterval is Sveltos's own poll interval for it (see
// clusterSveltosPollInterval).
//
// Without prune, the profile has LeavePolicies, so deleting it, which the root
// does as soon as the profile's Unit leaves the apps Space, leaves what it
// deployed running. With prune, Sveltos's default withdraws it. Sveltos deletes
// a resource that leaves the profile's Release either way.
func clusterSveltosChildProfileManifest(profileName, ociRepoURL, pollInterval string, plainHTTP, prune bool) []byte {
	stopMatching := "  stopMatchingBehavior: LeavePolicies\n"
	if prune {
		stopMatching = ""
	}
	return fmt.Appendf(nil, `apiVersion: config.projectsveltos.io/v1beta1
kind: ClusterProfile
metadata:
  name: %s
spec:
  clusterRefs:
  - apiVersion: lib.projectsveltos.io/v1beta1
    kind: SveltosCluster
    namespace: mgmt
    name: mgmt
%s  continueOnError: true
  policyRefs:
  - deploymentType: Remote
    remoteURL:
      url: %s:latest
      interval: %s
      plainHTTP: %t
      secretRef:
        name: %s
        namespace: %s
`, profileName, stopMatching, ociRepoURL, pollInterval, plainHTTP,
		clusterSveltosGatewaySecret, clusterSveltosNamespace)
}

// clusterSveltosSetCascade sets a deployment's ClusterProfile so that deleting
// it, by kubectl or by the root, withdraws what it deployed (cascade) or leaves
// it running. It reports whether the profile exists. The root re-applies a
// child only when the apps Space's Release changes, and the Release a delete
// publishes no longer holds it, so the root does not undo this.
func clusterSveltosSetCascade(ctx context.Context, kubeconfigPath, name string, cascade bool) (bool, error) {
	existing, err := clusterKubectlOutput(ctx, kubeconfigPath,
		"get", "clusterprofile", name, "--ignore-not-found", "-o", "name")
	if err != nil {
		return false, fmt.Errorf("look up ClusterProfile %q: %w", name, err)
	}
	if len(bytes.TrimSpace(existing)) == 0 {
		return false, nil
	}
	behavior := "LeavePolicies"
	if cascade {
		behavior = "WithdrawPolicies"
	}
	if _, err := clusterKubectlOutput(ctx, kubeconfigPath,
		"patch", "clusterprofile", name, "--type=merge", "-p", `{"spec":{"stopMatchingBehavior":"`+behavior+`"}}`); err != nil {
		return true, fmt.Errorf("set stopMatchingBehavior on ClusterProfile %q: %w", name, err)
	}
	return true, nil
}

// clusterSveltosDeleteProfile deletes a deployment's ClusterProfile after
// setting its cascade (see clusterSveltosSetCascade). With cascade the delete
// waits for Sveltos to withdraw what the profile deployed.
func clusterSveltosDeleteProfile(ctx context.Context, kubeconfigPath, name string, cascade bool, out io.Writer) error {
	exists, err := clusterSveltosSetCascade(ctx, kubeconfigPath, name, cascade)
	if err != nil || !exists {
		return err
	}
	verb := "leaving what it deployed"
	if cascade {
		verb = "and what it deployed"
	}
	fmt.Fprintf(out, "  deleting ClusterProfile %q (%s)...\n", name, verb)
	if _, err := clusterKubectlOutput(ctx, kubeconfigPath,
		"delete", "clusterprofile", name, "--ignore-not-found", "--timeout=5m"); err != nil {
		return fmt.Errorf("delete ClusterProfile %q: %w", name, err)
	}
	return nil
}

// clusterSveltosRootProfileManifest returns the YAML for the root
// ClusterProfile. It pulls the apps Space's Release, which includes the root
// itself, and deploys it to the management cluster, so adding a ClusterProfile
// Unit to the apps Space and publishing a new Release makes Sveltos create that
// profile. Sveltos tracks the root as its own owner without conflict. With
// LeavePolicies, deleting the root leaves the child profiles, and the
// deployments they own, in place.
func clusterSveltosRootProfileManifest(ociRepoURL, pollInterval string, plainHTTP bool) []byte {
	return fmt.Appendf(nil, `apiVersion: config.projectsveltos.io/v1beta1
kind: ClusterProfile
metadata:
  name: %s
spec:
  clusterRefs:
  - apiVersion: lib.projectsveltos.io/v1beta1
    kind: SveltosCluster
    namespace: mgmt
    name: mgmt
  stopMatchingBehavior: LeavePolicies
  continueOnError: true
  policyRefs:
  - deploymentType: Local
    remoteURL:
      url: %s:latest
      interval: %s
      plainHTTP: %t
      secretRef:
        name: %s
        namespace: %s
`, clusterSveltosRootProfile, ociRepoURL, pollInterval, plainHTTP,
		clusterSveltosGatewaySecret, clusterSveltosNamespace)
}

// clusterSveltosTargetUI returns a Sveltos cluster's URL-TargetUI: the
// dashboard's page for the ClusterProfile that deploys a Space, which `cub variant
// create` names after the Space. The ConfigHub UI substitutes "{slug}" with the
// Space slug. The dashboard routes a cluster-scoped profile at
// /sveltos/profile/<name>/<kind>.
func clusterSveltosTargetUI(dashboardPort int) string {
	return fmt.Sprintf("http://localhost:%d/sveltos/profile/{slug}/ClusterProfile", dashboardPort)
}

// clusterSveltosPollInterval returns Sveltos's poll interval for a cluster's
// profiles: infrequent when argobot forces syncs, every minute when polling is
// all there is.
func clusterSveltosPollInterval(argobot bool) string {
	if argobot {
		return clusterSveltosPollIntervalWithArgobot
	}
	return clusterSveltosFetchInterval
}

// clusterSveltosPollIntervalOf returns the poll interval `cub cluster up`
// recorded on a Sveltos cluster's target, so a child profile created later polls
// like the root does. A target without one, from before it was recorded, polls
// every minute.
func clusterSveltosPollIntervalOf(target *goclientnew.Target) string {
	if interval := target.Annotations[clusterAnnotationSveltosPollInterval]; interval != "" {
		return interval
	}
	return clusterSveltosFetchInterval
}
