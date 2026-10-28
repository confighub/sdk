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
	name              string
	spaceSlug         string
	controller        string
	mounts            []string
	noPorts           bool
	noArgobot         bool
	argobotBase       string
	registry          string
	noPlaceholderGate bool
	controllerVersion string
	prune             bool
}

var clusterUpCmd = &cobra.Command{
	Use:   "up [<name>]",
	Args:  cobra.MaximumNArgs(1),
	Short: "Create a kind cluster and attach it to ConfigHub, via Argo CD, Sveltos or Flux",
	Long: `Creates a local kind cluster and attaches it: "cub cluster create" followed by
"cub cluster attach --provider kind", which sets up the controller, the
cluster and apps Spaces, the service account, the gated OCI target, the root
app-of-apps and argobot. See "cub cluster attach --help" for what attaching
sets up; this help covers what "up" adds. Unlike a cluster attached by hand,
one "up" brings up is torn down again with "cub cluster down".

The kind node gets a window of host ports mapped through: the first is the
controller UI's NodePort, so Argo CD is at http://localhost:<argo-port>
(server.insecure=true) with no port-forward; the rest are free for your own
NodePort Services. --no-ports reserves only the UI's. The window and the UI
port are recorded as annotations on the cluster Space, which is how "cub
cluster open" finds the UI.

Admin credentials are written to <config-dir>/clusters/<name>.env for
` + "`source`" + `-ing into your shell along with KUBECONFIG and CUB_CONTEXT.

With --controller sveltos, Sveltos is installed instead of Argo CD, and the
apps Space is <name>-sveltos-apps. Its root Unit is a ClusterProfile that pulls
the apps Space's own Release and deploys it to the management cluster, so each
ClusterProfile Unit added there, pulling a deployment Space's Release, becomes
a profile Sveltos runs; "cub variant create --target" adds one per deployment.
argobot is installed in its Sveltos mode, from its own base Space
("argobot-sveltos-base"): on each deploy it redeploys the matching profile, and
it reports live status from Sveltos health reports. Sveltos also polls each
Release itself, as a fallback: every 10 minutes with argobot, every minute
with --no-k8sbot, where polling is all there is. The Sveltos dashboard is
reachable at http://localhost:<dashboard-port>, and logs in with a token from
the platform-admin ServiceAccount.

With --controller flux, Flux is installed instead, and the apps Space is
<name>-flux-apps. Its root Unit is an OCIRepository and Kustomization, both
named confighub-root, that pull and apply the apps Space's own Release, so each
Unit added there, holding an OCIRepository and Kustomization that pull and
apply a deployment Space's Release, becomes a deployment Flux runs; "cub variant
create --target" adds one per deployment. argobot is installed in its Flux mode,
from its own base Space ("argobot-flux-base"): on each deploy it requests a
reconciliation of the matching OCIRepository, and it reports live status from
the Kustomizations. The OCIRepositories also poll for a new Release themselves:
every 10 minutes with argobot, every minute with --no-k8sbot. Flux has no UI;
the whole NodePort window is left for your apps, and
"kubectl -n flux-system get ocirepository,kustomization" shows Flux's state.

Use --mount HOST[:CONTAINER] (repeatable) to bind-mount host directories
into the cluster node.

--prune lets the controller delete what a removed deployment deployed; see
"cub cluster attach --help". With Flux it does the same with Kustomizations:
by default neither the root Kustomization nor a deployment's prunes, and
--prune turns prune on for the root and for every deployment "cub variant
create --target" adds later. Sveltos cannot be kept from deleting: its root
always deletes a deployment's ClusterProfile whose Unit leaves the apps space,
and a profile always deletes a resource that leaves its Release. By default
a deployment's ClusterProfile has stopMatchingBehavior LeavePolicies, so
deleting it leaves what it deployed running; --prune drops that, so what it
deployed is withdrawn.

"cub cluster down" deletes the kind cluster again; the ConfigHub side is kept
unless --delete-config is given.`,
	RunE: clusterUpCmdRun,
}

func init() {
	clusterUpCmd.Flags().StringVar(&clusterUpArgs.name, "name", "", "cluster name (may also be given as the argument; auto-generated if empty)")
	_ = clusterUpCmd.Flags().MarkHidden("name")
	clusterUpCmd.Flags().StringVar(&clusterUpArgs.spaceSlug, "space", "", "ConfigHub space slug for the cluster's target and argobot channel (the \"target prefix\"); the apps space is this slug + \"-argo-apps\", + \"-sveltos-apps\" with --controller sveltos, or + \"-flux-apps\" with --controller flux (defaults to <name>)")
	clusterUpCmd.Flags().StringVar(&clusterUpArgs.controller, "controller", clusterControllerArgo, "controller to install and wire to ConfigHub: argo (Argo CD), sveltos (Sveltos) or flux (Flux)")
	clusterUpCmd.Flags().StringArrayVar(&clusterUpArgs.mounts, "mount", nil, "host:container bind mount (repeatable; container path defaults to /mnt/<basename>)")
	clusterUpCmd.Flags().BoolVar(&clusterUpArgs.noPorts, "no-ports", false, "only reserve the controller UI's NodePort (none for Flux); skip the user-app NodePort window")
	clusterUpCmd.Flags().BoolVar(&clusterUpArgs.noArgobot, "no-k8sbot", false, "skip installing argobot (the event-driven Argo CD sync, Sveltos redeploy, or Flux reconciliation, bot); 'cub cluster k8sbot-install' installs it later")
	clusterUpCmd.Flags().StringVar(&clusterUpArgs.argobotBase, "k8sbot-base", "", "what to do with argobot's shared base Space when it already exists: update (upload what argobot:latest installs over it, then clone it) or clone (clone it as it is); asked when stdin is a terminal, otherwise update")
	clusterUpCmd.Flags().StringVar(&clusterUpArgs.registry, "registry", "", clusterRegistryFlagUsage)
	clusterUpCmd.Flags().BoolVar(&clusterUpArgs.noPlaceholderGate, "no-placeholder-gate", false, "skip the default vet-placeholders Trigger, allowing Releases with unfilled confighubplaceholder values to be published to this cluster")
	clusterUpCmd.Flags().StringVar(&clusterUpArgs.controllerVersion, "controller-version", "", "controller release to install (default: argo "+clusterArgoDefaultVersion+", sveltos "+clusterSveltosVersion+", flux "+clusterFluxVersion+"; Argo CD must be v3.1 or later, which oci:// sources need)")
	clusterUpCmd.Flags().BoolVar(&clusterUpArgs.prune, "prune", false, "let the controller delete what a removed deployment deployed: the root prunes deployments whose Units leave the apps space (Sveltos's always does), and deployments created later prune what they deployed")
	clusterCmd.AddCommand(clusterUpCmd)
}

type clusterUpOptions struct {
	name              string
	spaceSlug         string
	controller        string
	mounts            []clusterMount
	noPorts           bool
	noArgobot         bool
	argobotBase       string
	registry          string
	noPlaceholderGate bool
	controllerVersion string
	prune             bool
}

func clusterUpCmdRun(cmd *cobra.Command, args []string) error {
	name, err := clusterNameArg(args, clusterUpArgs.name, "up", false)
	if err != nil {
		return err
	}
	mounts, err := clusterParseMounts(clusterUpArgs.mounts)
	if err != nil {
		return err
	}
	return clusterUpRun(cmd.OutOrStdout(), clusterUpOptions{
		name:              name,
		spaceSlug:         clusterUpArgs.spaceSlug,
		controller:        clusterUpArgs.controller,
		mounts:            mounts,
		noPorts:           clusterUpArgs.noPorts,
		noArgobot:         clusterUpArgs.noArgobot,
		argobotBase:       clusterUpArgs.argobotBase,
		registry:          clusterUpArgs.registry,
		noPlaceholderGate: clusterUpArgs.noPlaceholderGate,
		controllerVersion: clusterUpArgs.controllerVersion,
		prune:             clusterUpArgs.prune,
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
	switch opts.controller {
	case clusterControllerArgo, clusterControllerSveltos, clusterControllerFlux:
	default:
		return fmt.Errorf("--controller %q: must be %s, %s or %s", opts.controller,
			clusterControllerArgo, clusterControllerSveltos, clusterControllerFlux)
	}
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
	appsSlug := opts.spaceSlug + clusterAppsSuffix(opts.controller)

	if rec, err := clusterReadRecord(opts.name); err != nil {
		return err
	} else if rec != nil {
		return fmt.Errorf("cluster %q is already cub-managed on this machine, wired to %s (see 'cub cluster list'); use another name, or 'cub cluster down %s' first", opts.name, clusterRecordWhere(rec), opts.name)
	}
	if _, err := os.Stat(clusterKubeconfigPath(opts.name)); err == nil {
		return fmt.Errorf("kubeconfig %s already exists; cluster %q may already be cub-managed", clusterKubeconfigPath(opts.name), opts.name)
	}

	// Surface leftovers before building the kind cluster rather than failing
	// mid-run.
	argobotSlug := ""
	if !opts.noArgobot {
		argobotSlug = clusterArgobotComponent + "-" + opts.name
	}
	if err := clusterRequireFreshConfigHub(opts.name, opts.spaceSlug, appsSlug, argobotSlug, true); err != nil {
		return err
	}
	if !opts.noArgobot {
		repository, err := clusterArgobotRepository(opts.registry)
		if err != nil {
			return err
		}
		mode, err := clusterChooseArgobotBase(out, opts.controller, opts.argobotBase, repository)
		if err != nil {
			return err
		}
		opts.argobotBase = mode
	}

	// A cluster of the same name deleted without `cub cluster down` leaves its
	// dashboard tokens behind, which `cub cluster open` would hand over until
	// they expired: the new cluster rejects them.
	if err := os.RemoveAll(clusterSveltosTokenDir(opts.name)); err != nil {
		return fmt.Errorf("remove a previous cluster's Sveltos dashboard tokens: %w", err)
	}

	// kind's own kubeconfig goes to a scratch file, kept until the end so the
	// rollback's `kind delete` can clean it: attach writes the copy every other
	// `cub cluster` command reads, from `kind get kubeconfig`.
	scratch, err := os.CreateTemp("", "cub-cluster-"+opts.name+"-*.kubeconfig")
	if err != nil {
		return err
	}
	scratch.Close()
	defer os.Remove(scratch.Name())

	// `cub cluster create`. The first port of the window is for the
	// controller's UI (argocd-server, or the Sveltos dashboard), which attach
	// puts there; --no-ports keeps only that one. Flux has no UI, so the whole
	// window is the user's, and --no-ports leaves none.
	_, undoCreate, err := clusterCreateKind(out, clusterCreateSpec{
		name:           opts.name,
		kubeconfigPath: scratch.Name(),
		mounts:         opts.mounts,
		noPorts:        opts.noPorts,
		uiPort:         opts.controller != clusterControllerFlux,
	})
	if err != nil {
		return err
	}
	clusterPrintMounts(out, opts.mounts)

	// `cub cluster attach --provider kind`, as a cluster cub created: `cub
	// cluster down` deletes it. attach rolls back its own side when it fails;
	// the kind cluster is ours to delete.
	if err := clusterAttachRun(out, clusterAttachOptions{
		name:              opts.name,
		spaceSlug:         opts.spaceSlug,
		provider:          clusterProviderKind,
		cluster:           opts.name,
		controller:        opts.controller,
		controllerVersion: opts.controllerVersion,
		noArgobot:         opts.noArgobot,
		argobotBase:       opts.argobotBase,
		registry:          opts.registry,
		noPlaceholderGate: opts.noPlaceholderGate,
		prune:             opts.prune,
		origin:            clusterOriginKind,
	}); err != nil {
		undoCreate()
		return err
	}
	return nil
}

// clusterBuildPortEnvFile renders the shell-source env file for a kind cluster
// whose controller UI is on host port uiPort (Flux has none).
func clusterBuildPortEnvFile(controller, name, kubeconfigPath string, uiPort int, adminPassword string) string {
	switch controller {
	case clusterControllerSveltos:
		return clusterBuildSveltosEnvFile(name, kubeconfigPath, uiPort)
	case clusterControllerFlux:
		return clusterBuildFluxEnvFile(name, kubeconfigPath)
	}
	return clusterBuildEnvFile(name, kubeconfigPath, uiPort, adminPassword)
}

// clusterPrintPortUI prints where a kind cluster's controller UI is on the
// host, and how to log in to it.
func clusterPrintPortUI(out io.Writer, controller, name, kubeconfigPath string, uiPort int, adminPassword string) {
	switch controller {
	case clusterControllerSveltos:
		fmt.Fprintf(out, "\nSveltos dashboard: http://localhost:%d\n", uiPort)
		fmt.Fprintf(out, "Log in with a token from:\n  KUBECONFIG=%s kubectl create token %s --duration=24h\n", kubeconfigPath, clusterSveltosDashboardAccount)
	case clusterControllerFlux:
		fmt.Fprintf(out, "\nFlux has no UI. See its state with:\n  KUBECONFIG=%s %s\n", kubeconfigPath, clusterFluxStatusCommand())
	default:
		fmt.Fprintf(out, "\nArgo CD: http://localhost:%d  (admin / %s)\n", uiPort, adminPassword)
		fmt.Fprintf(out, "Reopen it later with the password on your clipboard:\n  cub cluster open %s\n", name)
	}
}

// clusterBuildSveltosEnvFile renders the shell-source env file for a cluster
// running Sveltos. The dashboard token is not in it: `cub cluster open` keeps it
// in a file of its own, and replaces it when it expires.
func clusterBuildSveltosEnvFile(name, kubeconfigPath string, dashboardPort int) string {
	return fmt.Sprintf(`# cub cluster: %s
# Source this file: source %s
export KUBECONFIG=%s
export SVELTOS_DASHBOARD=http://localhost:%d
%s`, name, clusterEnvFilePath(name), kubeconfigPath, dashboardPort, clusterEnvFileContextLine())
}

// clusterBuildFluxEnvFile renders the shell-source env file for a cluster
// running Flux, which has no UI to point at.
func clusterBuildFluxEnvFile(name, kubeconfigPath string) string {
	return fmt.Sprintf(`# cub cluster: %s
# Source this file: source %s
export KUBECONFIG=%s
%s`, name, clusterEnvFilePath(name), kubeconfigPath, clusterEnvFileContextLine())
}

// clusterAppsSuffix returns the suffix of the apps Space of a cluster running
// controller.
func clusterAppsSuffix(controller string) string {
	switch controller {
	case clusterControllerSveltos:
		return clusterSveltosAppsSuffix
	case clusterControllerFlux:
		return clusterFluxAppsSuffix
	}
	return clusterArgoAppsSuffix
}

// clusterAppsKind describes the Units in the apps Space of a cluster running
// controller.
func clusterAppsKind(controller string) string {
	switch controller {
	case clusterControllerSveltos:
		return "Sveltos ClusterProfile Units"
	case clusterControllerFlux:
		return "Flux OCIRepository and Kustomization Units"
	}
	return "Argo Application Units"
}

// clusterBuildEnvFile renders the shell-source env file for a kind cluster
// running Argo CD: KUBECONFIG, the cub context, the Argo CD endpoint and its
// admin credentials.
func clusterBuildEnvFile(name, kubeconfigPath string, argoPort int, adminPassword string) string {
	return fmt.Sprintf(`# cub cluster: %s
# Source this file: source %s
export KUBECONFIG=%s
export ARGOCD_SERVER=localhost:%d
export ARGOCD_OPTS="--plaintext --grpc-web"
export ARGOCD_USERNAME=admin
export ARGOCD_PASSWORD=%q
%s`, name, clusterEnvFilePath(name), kubeconfigPath, argoPort, adminPassword, clusterEnvFileContextLine())
}
