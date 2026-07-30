// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
)

var clusterUpArgs struct {
	name       string
	spaceSlug  string
	mounts     []string
	noPorts    bool
	noArgobot  bool
	argobotOCI string
}

var clusterUpCmd = &cobra.Command{
	Use:   "up",
	Short: "Bring up a kind cluster wired to ConfigHub via Argo CD",
	Long: `Creates a kind cluster, installs Argo CD into it, and provisions the
ConfigHub side across two Spaces:

  <name>              the cluster (target-prefix) Space: a server-hosted OCI
                      worker and an OCI target owned by that worker. This Space
                      is a pure namespace — it holds no config bundle.
  <name>-argo-apps    the apps Space: the root "app of apps" Application Unit
                      and every child Argo Application Unit. Its release target
                      is the OCI target in the cluster Space (a cross-Space
                      reference), so the apps Space is the config bundle Argo
                      pulls, and neither Space's release target points at a
                      target inside itself.

The root Application is bootstrapped once via kubectl apply; from then on,
adding an Application Unit to the apps Space and publishing a new Release
("cub release publish <name>-argo-apps") causes Argo to create the corresponding
app on its next sync. The OCI target carries a confighub.com/argo-apps-space
annotation (pointing at the apps Space) so API clients can resolve the Space
holding the Argo Application Units from the target.

Unless --no-argobot is given, argobot is installed: a ConfigHub bot that watches
the event log and force-syncs the matching Argo CD Application the moment a
deploy happens. It is delivered as a component (a shared "argobot-base" installed
from its OCI config bundle, plus a per-cluster "argobot-<name>" variant), reuses
the cluster's worker as its identity, and runs in the default kubernetes sync
mode (no Argo CD token needed).

Argo CD is reachable at http://localhost:<argo-port> (server.insecure=true);
admin credentials are written to <config-dir>/clusters/<name>.env for
` + "`source`" + `-ing into your shell along with KUBECONFIG.

Use --mount HOST[:CONTAINER] (repeatable) to bind-mount host directories
into the cluster node.`,
	RunE: clusterUpCmdRun,
}

func init() {
	clusterUpCmd.Flags().StringVar(&clusterUpArgs.name, "name", "", "cluster name (auto-generated if empty)")
	clusterUpCmd.Flags().StringVar(&clusterUpArgs.spaceSlug, "space", "", "ConfigHub space slug for the cluster's worker and target (the \"target prefix\"); the Argo apps space is this slug + \"-argo-apps\" (defaults to <name>)")
	clusterUpCmd.Flags().StringArrayVar(&clusterUpArgs.mounts, "mount", nil, "host:container bind mount (repeatable; container path defaults to /mnt/<basename>)")
	clusterUpCmd.Flags().BoolVar(&clusterUpArgs.noPorts, "no-ports", false, "only reserve the Argo NodePort; skip the user-app NodePort window")
	clusterUpCmd.Flags().BoolVar(&clusterUpArgs.noArgobot, "no-argobot", false, "skip installing argobot (the event-driven Argo CD sync bot)")
	clusterUpCmd.Flags().StringVar(&clusterUpArgs.argobotOCI, "argobot-oci", clusterArgobotOCIRef, "OCI reference of argobot's config bundle")
	clusterCmd.AddCommand(clusterUpCmd)
}

type clusterUpOptions struct {
	name          string
	spaceSlug     string
	mounts        []clusterMount
	noPorts       bool
	noArgobot     bool
	argobotOCIRef string
}

func clusterUpCmdRun(cmd *cobra.Command, args []string) error {
	mounts, err := clusterParseMounts(clusterUpArgs.mounts)
	if err != nil {
		return err
	}
	return clusterUpRun(cmd.OutOrStdout(), clusterUpOptions{
		name:          clusterUpArgs.name,
		spaceSlug:     clusterUpArgs.spaceSlug,
		mounts:        mounts,
		noPorts:       clusterUpArgs.noPorts,
		noArgobot:     clusterUpArgs.noArgobot,
		argobotOCIRef: clusterUpArgs.argobotOCI,
	})
}

// clusterParseMounts parses --mount values of the form HOST[:CONTAINER].
func clusterParseMounts(args []string) ([]clusterMount, error) {
	if len(args) == 0 {
		return nil, nil
	}
	out := make([]clusterMount, 0, len(args))
	for _, raw := range args {
		host, container, _ := strings.Cut(raw, ":")
		host = strings.TrimSpace(host)
		container = strings.TrimSpace(container)
		if host == "" {
			return nil, fmt.Errorf("--mount %q: empty host path", raw)
		}
		if strings.HasPrefix(host, "~") {
			home, err := os.UserHomeDir()
			if err != nil {
				return nil, fmt.Errorf("--mount %q: expand ~: %w", raw, err)
			}
			host = filepath.Join(home, strings.TrimPrefix(host, "~"))
		}
		abs, err := filepath.Abs(host)
		if err != nil {
			return nil, fmt.Errorf("--mount %q: resolve %q: %w", raw, host, err)
		}
		if _, err := os.Stat(abs); err != nil {
			return nil, fmt.Errorf("--mount %q: %w", raw, err)
		}
		if container == "" {
			container = "/mnt/" + filepath.Base(abs)
		}
		if !strings.HasPrefix(container, "/") {
			return nil, fmt.Errorf("--mount %q: container path must be absolute", raw)
		}
		out = append(out, clusterMount{HostPath: abs, ContainerPath: container})
	}
	return out, nil
}

func clusterUpRun(out io.Writer, opts clusterUpOptions) error {
	if err := clusterKindAvailable(); err != nil {
		return err
	}
	if _, err := exec.LookPath("kubectl"); err != nil {
		return fmt.Errorf("kubectl not found on PATH")
	}

	var err error
	if opts.name == "" {
		fmt.Fprintln(out, "Generating cluster name...")
		opts.name, err = clusterNewPrefix()
		if err != nil {
			return err
		}
	}
	if opts.spaceSlug == "" {
		opts.spaceSlug = opts.name
	}
	appsSlug := opts.spaceSlug + clusterArgoAppsSuffix

	if _, err := os.Stat(clusterKubeconfigPath(opts.name)); err == nil {
		return fmt.Errorf("kubeconfig %s already exists; cluster %q may already be cub-managed", clusterKubeconfigPath(opts.name), opts.name)
	}

	for _, slug := range []string{opts.spaceSlug, appsSlug} {
		if exists, err := clusterSpaceExists(slug); err != nil {
			return err
		} else if exists {
			return fmt.Errorf("ConfigHub space %q already exists", slug)
		}
	}

	// argobot's per-cluster variant Space is created (fail-fast, no --allow-exists)
	// during the install, so surface a leftover one here before building the kind
	// cluster rather than failing mid-run. Skipped with --no-argobot.
	if !opts.noArgobot {
		argobotSlug := clusterArgobotComponent + "-" + opts.name
		if exists, err := clusterSpaceExists(argobotSlug); err != nil {
			return err
		} else if exists {
			return fmt.Errorf("ConfigHub space %q already exists (leftover argobot install; delete it with 'cub space delete --recursive %s' or pass --no-argobot)", argobotSlug, argobotSlug)
		}
	}

	if exists, err := clusterKindExists(ctx, opts.name); err != nil {
		return err
	} else if exists {
		return fmt.Errorf("kind cluster %q already exists", opts.name)
	}

	if err := clusterEnsureKubeconfigDir(); err != nil {
		return err
	}

	kubeconfigPath := clusterKubeconfigPath(opts.name)
	envFilePath := clusterEnvFilePath(opts.name)

	// Reserve a port window: first port = argocd-server NodePort,
	// the rest (if any) = user-allocatable NodePorts.
	windowSize := clusterPortRangeSize
	if opts.noPorts {
		windowSize = 1
	}
	bound, err := clusterBoundHostPorts(ctx)
	if err != nil {
		return fmt.Errorf("probe docker for bound ports: %w", err)
	}
	startPort, err := clusterPickFreePortWindow(bound, clusterPortRangeStart, clusterPortRangeEnd, windowSize)
	if err != nil {
		return err
	}
	argoPort := startPort
	ports := make([]clusterPortMapping, 0, windowSize)
	for p := startPort; p < startPort+windowSize; p++ {
		ports = append(ports, clusterPortMapping{HostPort: p, ContainerPort: p})
	}
	portRange := fmt.Sprintf("%d-%d", startPort, startPort+windowSize-1)

	fmt.Fprintf(out, "Creating kind cluster %q (kubeconfig: %s)...\n", opts.name, kubeconfigPath)
	if _, err := clusterKindCreate(ctx, opts.name, kubeconfigPath, ports, opts.mounts, out); err != nil {
		return err
	}
	rollback := []func(){
		func() {
			fmt.Fprintf(out, "Rolling back: kind delete cluster %q\n", opts.name)
			_ = clusterKindDelete(ctx, opts.name, kubeconfigPath, out)
			// kind drains its entries but leaves the kubeconfig file behind;
			// remove it so a retry with the same name doesn't trip the
			// "kubeconfig already exists" precondition.
			_ = os.Remove(kubeconfigPath)
			_ = os.Remove(envFilePath)
		},
	}
	commit := false
	defer func() {
		if commit {
			return
		}
		for i := len(rollback) - 1; i >= 0; i-- {
			rollback[i]()
		}
	}()

	fmt.Fprintln(out, "Installing Argo CD...")
	adminPassword, err := clusterArgoInstall(ctx, kubeconfigPath, argoPort, out)
	if err != nil {
		return err
	}

	// The cluster (target-prefix) Space holds the worker and target. It carries
	// the cluster label + confighub.com/cluster-* annotations so `cub cluster
	// list`/`down` find the cluster from it; the apps Space below is derived and
	// left unlabeled so the cluster lists once.
	fmt.Fprintf(out, "Creating ConfigHub space %q (worker + target)...\n", opts.spaceSlug)
	hostname, _ := os.Hostname()
	annotations := map[string]string{
		clusterAnnotationName:      opts.name,
		clusterAnnotationHost:      hostname,
		clusterAnnotationPortRange: portRange,
		clusterAnnotationArgoPort:  fmt.Sprintf("%d", argoPort),
	}
	spaceID, err := clusterCreateSpace(opts.spaceSlug, opts.spaceSlug,
		map[string]string{clusterLabel: "true"}, annotations)
	if err != nil {
		return err
	}
	rollback = append(rollback, func() {
		fmt.Fprintf(out, "Rolling back: cub space delete --recursive %q\n", opts.spaceSlug)
		// This Space has no release target of its own; the apps Space's rollback
		// (registered later, so it runs first) clears its cross-Space reference
		// and deletes the Units bound to this target before we delete it here.
		_ = clusterDeleteSpace(opts.spaceSlug, true)
	})

	fmt.Fprintf(out, "Creating server-hosted OCI worker %q (OrgRole=none)...\n", clusterWorkerSlug)
	workerID, workerSecret, err := clusterCreateOCIWorker(spaceID, clusterWorkerSlug, clusterWorkerSlug)
	if err != nil {
		return err
	}

	fmt.Fprintf(out, "Creating OCI target %q owned by worker %q...\n", clusterTargetSlug, clusterWorkerSlug)
	// URL-TargetUI deep link: the ConfigHub UI substitutes "{slug}" with the
	// Space slug at render time, linking a Unit straight to its Argo CD
	// Application UI on the locally-forwarded argocd-server NodePort.
	// argo-apps-space lets API clients resolve the Space holding the cluster's
	// Argo Application Units from the target; that Space is the apps Space.
	targetAnnotations := map[string]string{
		clusterAnnotationTargetUI:      fmt.Sprintf("http://localhost:%d/applications/argocd/{slug}", argoPort),
		clusterAnnotationArgoAppsSpace: appsSlug,
	}
	targetID, err := clusterCreateOCITarget(spaceID, workerID, clusterTargetSlug, clusterTargetSlug, targetAnnotations)
	if err != nil {
		return err
	}

	// The apps Space holds the root app-of-apps and every child Argo Application
	// Unit. Its release target is the OCI target in the cluster Space above — a
	// cross-Space reference, so the apps Space (the config bundle) never points
	// its release target at a target inside itself.
	fmt.Fprintf(out, "Creating ConfigHub space %q (Argo Application Units)...\n", appsSlug)
	appsSpaceID, err := clusterCreateSpace(appsSlug, appsSlug, nil, nil)
	if err != nil {
		return err
	}
	rollback = append(rollback, func() {
		fmt.Fprintf(out, "Rolling back: cub space delete --recursive %q\n", appsSlug)
		// Clear the cross-Space release-target reference first (ON DELETE
		// RESTRICT blocks deleting the OCI target it points at, which lives in
		// the cluster Space).
		_ = clusterClearReleaseTarget(appsSpaceID)
		_ = clusterDeleteSpace(appsSlug, true)
	})

	fmt.Fprintf(out, "Setting the apps space's release target to %q...\n", opts.spaceSlug+"/"+clusterTargetSlug)
	if err := clusterSetReleaseTarget(appsSpaceID, targetID); err != nil {
		return err
	}

	// OCI URL for Argo (running inside kind) — rewritten to
	// host.docker.internal for loopback cub servers.
	ociURLForArgo, err := clusterOCIEndpointFromContainer(clusterServerURL())
	if err != nil {
		return err
	}
	// OCI URL recorded inside the root Application's source.repoURL — Argo
	// pulls from this, so it must be the in-cluster-reachable form. The root
	// app-of-apps references the apps Space's Release, served at
	// /space/<apps-slug>, tag "latest".
	rootRepoURL := strings.TrimRight(ociURLForArgo, "/") + "/space/" + appsSlug

	// Auto-detect plain-HTTP OCI from the cub server URL scheme. http:// API
	// → http:// OCI (the local-dev case). https:// API → TLS OCI.
	ociInsecure, err := clusterOCIInsecure(clusterServerURL())
	if err != nil {
		return err
	}

	fmt.Fprintln(out, "Applying confighub-oci-creds repo-creds Secret to argocd namespace...")
	if err := clusterArgoApplyOCIRepoCreds(ctx, kubeconfigPath, ociURLForArgo, workerID.String(), workerSecret, ociInsecure, out); err != nil {
		return err
	}

	fmt.Fprintf(out, "Creating root Application Unit %q in apps space %q...\n", clusterRootUnitSlug, appsSlug)
	rootManifest := clusterArgoRootAppManifest(appsSlug, rootRepoURL)
	rootUnitID, err := clusterCreateK8sYAMLUnit(appsSpaceID, targetID, clusterRootUnitSlug, clusterRootUnitSlug, rootManifest)
	if err != nil {
		return err
	}

	// Install argobot (unless --no-argobot) BEFORE the final apps-Space publish
	// and root bootstrap, so its child Application Unit is already in the first
	// bundle Argo pulls — the root app-of-apps then creates it on its initial
	// sync rather than only on a later reconcile. The step creates that child
	// Unit via `cub variant create --target` (auto-derived from the OCI target's
	// argo-apps-space annotation, the same path any deployment uses) and
	// publishes argobot's own variant Release; the child Unit is left in the apps
	// Space for the single authoritative publish below. All argobot specifics
	// live in cluster_argobot.go.
	if err := clusterInstallArgobotStep(out, opts, clusterArgobotDeps{
		workerID:       workerID,
		workerSecret:   workerSecret,
		kubeconfigPath: kubeconfigPath,
	}, func(f func()) { rollback = append(rollback, f) }); err != nil {
		return fmt.Errorf("install argobot: %w", err)
	}

	// Publish the apps Space's Release once, after root's triggers settle, so
	// the bundle carries root's post-trigger revision plus any Application Units
	// argobot's install added to the Space (already trigger-settled by variant
	// create). This is the bundle the root app-of-apps pulls on its first sync.
	fmt.Fprintln(out, "\nPublishing the apps space's release (populates the OCI bundle Argo pulls)...")
	if err := clusterWaitUnitTriggers(appsSpaceID, rootUnitID); err != nil {
		return err
	}
	if err := clusterPublishRelease(appsSpaceID); err != nil {
		return err
	}

	fmt.Fprintln(out, "Bootstrapping root Application via kubectl (the only kubectl-apply moment)...")
	if err := clusterKubectlApply(ctx, kubeconfigPath, rootManifest, out); err != nil {
		return err
	}

	envContents := clusterBuildEnvFile(opts.name, kubeconfigPath, argoPort, adminPassword)
	if err := os.WriteFile(envFilePath, []byte(envContents), 0o600); err != nil {
		return fmt.Errorf("write env file: %w", err)
	}

	commit = true
	fmt.Fprintf(out, "\nDone.\n  cluster:    %s\n  kubeconfig: %s\n  env file:   %s\n  space:      %s\n  apps space: %s\n  worker:     %s/%s\n  target:     %s/%s\n  root app:   %s/%s\n",
		opts.name, kubeconfigPath, envFilePath,
		opts.spaceSlug,
		appsSlug,
		opts.spaceSlug, clusterWorkerSlug,
		opts.spaceSlug, clusterTargetSlug,
		appsSlug, clusterRootUnitSlug)
	if !opts.noArgobot {
		fmt.Fprintf(out, "  argobot:    %s-%s (Argo app; kubernetes sync mode)\n", clusterArgobotComponent, opts.name)
	}
	fmt.Fprintf(out, "\nArgo CD: http://localhost:%d  (admin / %s)\n", argoPort, adminPassword)
	fmt.Fprintf(out, "Reopen it later with the password on your clipboard:\n  cub cluster open %s\n", opts.name)
	if len(ports) > 1 {
		fmt.Fprintf(out, "\nUser NodePort window: %d-%d (argo uses %d; rest are open)\n", startPort, startPort+windowSize-1, argoPort)
	}
	if len(opts.mounts) > 0 {
		fmt.Fprintf(out, "\nHost mounts (host → node, accessible to pods via hostPath):\n")
		for _, m := range opts.mounts {
			fmt.Fprintf(out, "  %s → %s\n", m.HostPath, m.ContainerPath)
		}
	}
	fmt.Fprintf(out, "\nLoad the cluster + Argo creds into your shell:\n  source %s\n", envFilePath)
	fmt.Fprintf(out, "\nDeploy an app to this cluster:\n  1. cub variant create <name> <base-space> --target %s/%s [--namespace <ns>]\n  2. cub release publish <name>\nStep 1 clones <base-space> into a deployment space bound to the cluster's\ntarget and auto-creates its Argo CD Application in the apps space %q\n(republishing it so Argo picks it up); step 2 makes the deployment's config\ngo live. See \"cub variant create --help\".\n",
		opts.spaceSlug, clusterTargetSlug, appsSlug)
	return nil
}

// clusterBuildEnvFile renders the shell-source env file for a cluster.
func clusterBuildEnvFile(name, kubeconfigPath string, argoPort int, adminPassword string) string {
	return fmt.Sprintf(`# cub cluster: %s
# Source this file: source %s
export KUBECONFIG=%s
export ARGOCD_SERVER=localhost:%d
export ARGOCD_OPTS="--plaintext --grpc-web"
export ARGOCD_USERNAME=admin
export ARGOCD_PASSWORD=%q
`, name, clusterEnvFilePath(name), kubeconfigPath, argoPort, adminPassword)
}
