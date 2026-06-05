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
	name      string
	spaceSlug string
	mounts    []string
	noPorts   bool
}

var clusterUpCmd = &cobra.Command{
	Use:   "up",
	Short: "Bring up a kind cluster wired to ConfigHub via Argo CD",
	Long: `Creates a kind cluster, installs Argo CD into it, and provisions the
ConfigHub side: a Space containing a server-hosted OCI worker, an OCI
target owned by that worker, and a self-referencing root "app of
apps" Application Unit. The root Application is bootstrapped once via
kubectl apply; from then on, adding a new Application Unit to the Space
causes Argo to create the corresponding app on its next sync.

Argo CD is reachable at http://localhost:<argo-port> (server.insecure=true);
admin credentials are written to <config-dir>/clusters/<name>.env for
` + "`source`" + `-ing into your shell along with KUBECONFIG.

Use --mount HOST[:CONTAINER] (repeatable) to bind-mount host directories
into the cluster node.`,
	RunE: clusterUpCmdRun,
}

func init() {
	clusterUpCmd.Flags().StringVar(&clusterUpArgs.name, "name", "", "cluster name (auto-generated if empty)")
	clusterUpCmd.Flags().StringVar(&clusterUpArgs.spaceSlug, "space", "", "ConfigHub space slug (defaults to <name>-cluster)")
	clusterUpCmd.Flags().StringArrayVar(&clusterUpArgs.mounts, "mount", nil, "host:container bind mount (repeatable; container path defaults to /mnt/<basename>)")
	clusterUpCmd.Flags().BoolVar(&clusterUpArgs.noPorts, "no-ports", false, "only reserve the Argo NodePort; skip the user-app NodePort window")
	clusterCmd.AddCommand(clusterUpCmd)
}

type clusterUpOptions struct {
	name      string
	spaceSlug string
	mounts    []clusterMount
	noPorts   bool
}

func clusterUpCmdRun(cmd *cobra.Command, args []string) error {
	mounts, err := clusterParseMounts(clusterUpArgs.mounts)
	if err != nil {
		return err
	}
	return clusterUpRun(cmd.OutOrStdout(), clusterUpOptions{
		name:      clusterUpArgs.name,
		spaceSlug: clusterUpArgs.spaceSlug,
		mounts:    mounts,
		noPorts:   clusterUpArgs.noPorts,
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
		opts.spaceSlug = opts.name + "-cluster"
	}

	if _, err := os.Stat(clusterKubeconfigPath(opts.name)); err == nil {
		return fmt.Errorf("kubeconfig %s already exists; cluster %q may already be cub-managed", clusterKubeconfigPath(opts.name), opts.name)
	}

	if exists, err := clusterSpaceExists(opts.spaceSlug); err != nil {
		return err
	} else if exists {
		return fmt.Errorf("ConfigHub space %q already exists", opts.spaceSlug)
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

	fmt.Fprintf(out, "Creating ConfigHub space %q...\n", opts.spaceSlug)
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
		_ = clusterDeleteSpace(opts.spaceSlug, true)
	})

	fmt.Fprintf(out, "Creating server-hosted OCI worker %q (OrgRole=none)...\n", clusterOCIWorkerSlug)
	workerID, workerSecret, err := clusterCreateOCIWorker(spaceID, clusterOCIWorkerSlug, clusterOCIWorkerSlug)
	if err != nil {
		return err
	}

	fmt.Fprintf(out, "Creating OCI target %q owned by worker %q...\n", clusterOCITargetSlug, clusterOCIWorkerSlug)
	// URL-TargetUI deep link: the ConfigHub UI substitutes "{slug}" with the
	// Space slug at render time, linking a Unit straight to its Argo CD
	// Application UI on the locally-forwarded argocd-server NodePort.
	targetAnnotations := map[string]string{
		clusterAnnotationTargetUI: fmt.Sprintf("http://localhost:%d/applications/argocd/{slug}", argoPort),
	}
	targetID, err := clusterCreateOCITarget(spaceID, workerID, clusterOCITargetSlug, clusterOCITargetSlug, targetAnnotations)
	if err != nil {
		return err
	}

	// OCI URL for Argo (running inside kind) — rewritten to
	// host.docker.internal for loopback cub servers.
	ociURLForArgo, err := clusterOCIEndpointFromContainer(clusterServerURL())
	if err != nil {
		return err
	}
	// OCI URL recorded inside the root Application's source.repoURL — Argo
	// pulls from this, so it must be the in-cluster-reachable form.
	rootRepoURL := strings.TrimRight(ociURLForArgo, "/") + "/target/" + opts.spaceSlug + "/" + clusterOCITargetSlug

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

	fmt.Fprintf(out, "Creating root Application Unit %q in space %q...\n", clusterRootUnitSlug, opts.spaceSlug)
	rootManifest := clusterArgoRootAppManifest(opts.spaceSlug, rootRepoURL)
	rootUnitID, err := clusterCreateRootUnit(spaceID, targetID, clusterRootUnitSlug, clusterRootUnitSlug, rootManifest)
	if err != nil {
		return err
	}

	fmt.Fprintln(out, "Applying root Unit (cub unit apply — publishes to OCI; populates the OCI bundle)...")
	if err := clusterApplyUnit(spaceID, rootUnitID); err != nil {
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
	fmt.Fprintf(out, "\nDone.\n  cluster:    %s\n  kubeconfig: %s\n  env file:   %s\n  space:      %s\n  worker:     %s/%s\n  target:     %s/%s\n  root app:   %s/%s\n",
		opts.name, kubeconfigPath, envFilePath,
		opts.spaceSlug,
		opts.spaceSlug, clusterOCIWorkerSlug,
		opts.spaceSlug, clusterOCITargetSlug,
		opts.spaceSlug, clusterRootUnitSlug)
	fmt.Fprintf(out, "\nArgo CD: http://localhost:%d  (admin / %s)\n", argoPort, adminPassword)
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
	fmt.Fprintf(out, "\nAdd a new Argo app:\n  1. Author an Application CR YAML, source.path: ./<workload-space>\n  2. cub unit create --space %s <slug> <file> --target %s/%s\n  3. cub unit apply --space %s <slug>\n  4. Argo's root sync picks it up on its next reconcile.\n",
		opts.spaceSlug, opts.spaceSlug, clusterOCITargetSlug, opts.spaceSlug)
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
