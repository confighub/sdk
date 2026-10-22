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
	noPlaceholderGate bool
	argobotOCI        string
	argoVersion       string
	prune             bool
}

var clusterUpCmd = &cobra.Command{
	Use:   "up [<name>]",
	Args:  cobra.MaximumNArgs(1),
	Short: "Create a kind cluster and attach it to ConfigHub, via Argo CD, Sveltos or Flux",
	Long: `Creates a local kind cluster and attaches it: everything "cub cluster attach"
does — the controller, the cluster and apps Spaces, the service account, the
gated OCI target, the root app-of-apps, argobot — on a cluster this command
just created. See "cub cluster attach --help" for what attaching sets up; this
help covers what "up" adds.

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
with --no-argobot, where polling is all there is. The Sveltos dashboard is
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
every 10 minutes with argobot, every minute with --no-argobot. Flux has no UI;
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
	clusterUpCmd.Flags().BoolVar(&clusterUpArgs.noArgobot, "no-argobot", false, "skip installing argobot (the event-driven Argo CD sync, Sveltos redeploy, or Flux reconciliation, bot)")
	clusterUpCmd.Flags().BoolVar(&clusterUpArgs.noPlaceholderGate, "no-placeholder-gate", false, "skip the default vet-placeholders Trigger, allowing Releases with unfilled confighubplaceholder values to be published to this cluster")
	clusterUpCmd.Flags().StringVar(&clusterUpArgs.argobotOCI, "argobot-oci", clusterArgobotOCIRef, "OCI reference of argobot's config bundle, or a local file or directory of its manifests (default with --controller sveltos: "+clusterArgobotSveltosOCIRef+", with --controller flux: "+clusterArgobotFluxOCIRef+")")
	clusterUpCmd.Flags().StringVar(&clusterUpArgs.argoVersion, "argocd-version", clusterArgoDefaultVersion, "Argo CD release to install (v3.1 or later; oci:// sources need it)")
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
	noPlaceholderGate bool
	argobotOCIRef     string
	argoVersion       string
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
	argobotOCIRef, err := clusterArgobotOCIRefFor(clusterUpArgs.controller, clusterUpArgs.argobotOCI, cmd.Flags().Changed("argobot-oci"))
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
		noPlaceholderGate: clusterUpArgs.noPlaceholderGate,
		argobotOCIRef:     argobotOCIRef,
		argoVersion:       clusterUpArgs.argoVersion,
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
	if err := clusterRequireFreshConfigHub(opts.name, opts.spaceSlug, appsSlug, argobotSlug); err != nil {
		return err
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

	// Reserve a port window: first port = the controller UI's NodePort
	// (argocd-server, or the Sveltos dashboard), the rest (if any) =
	// user-allocatable NodePorts. Flux has no UI, so the whole window is the
	// user's, and --no-ports leaves none.
	hasUI := opts.controller != clusterControllerFlux
	windowSize := clusterPortRangeSize
	if opts.noPorts {
		windowSize = 0
		if hasUI {
			windowSize = 1
		}
	}
	var startPort, uiPort int
	var ports []clusterPortMapping
	portRange := ""
	if windowSize > 0 {
		bound, err := clusterBoundHostPorts(ctx)
		if err != nil {
			return fmt.Errorf("probe docker for bound ports: %w", err)
		}
		startPort, err = clusterPickFreePortWindow(bound, clusterPortRangeStart, clusterPortRangeEnd, windowSize)
		if err != nil {
			return err
		}
		if hasUI {
			uiPort = startPort
		}
		ports = make([]clusterPortMapping, 0, windowSize)
		for p := startPort; p < startPort+windowSize; p++ {
			ports = append(ports, clusterPortMapping{HostPort: p, ContainerPort: p})
		}
		portRange = fmt.Sprintf("%d-%d", startPort, startPort+windowSize-1)
	}

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
			_ = clusterRemoveRecord(opts.name)
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

	// Everything from here is the attachment, with the pieces only a kind
	// cluster has: the port window on the Space, the controller UI exposed on
	// the mapped NodePort, and a deep link to it on the target.
	spaceAnnotations := map[string]string{}
	if portRange != "" {
		spaceAnnotations[clusterAnnotationPortRange] = portRange
	}
	targetAnnotations := map[string]string{}
	switch opts.controller {
	case clusterControllerSveltos:
		spaceAnnotations[clusterAnnotationSveltosPort] = fmt.Sprintf("%d", uiPort)
		targetAnnotations[clusterAnnotationTargetUI] = clusterSveltosTargetUI(uiPort)
	case clusterControllerFlux:
		// No UI, so no port and no deep link.
	default:
		spaceAnnotations[clusterAnnotationArgoPort] = fmt.Sprintf("%d", uiPort)
		// URL-TargetUI deep link: the ConfigHub UI substitutes "{slug}" with the
		// Space slug at render time, linking a Unit straight to its Argo CD
		// Application UI on the locally-forwarded argocd-server NodePort.
		targetAnnotations[clusterAnnotationTargetUI] = fmt.Sprintf("http://localhost:%d/applications/argocd/{slug}", uiPort)
	}
	result, err := clusterAttach(out, clusterAttachSpec{
		name:              opts.name,
		spaceSlug:         opts.spaceSlug,
		kubeconfigPath:    kubeconfigPath,
		origin:            clusterOriginKind,
		controller:        opts.controller,
		spaceAnnotations:  spaceAnnotations,
		targetAnnotations: targetAnnotations,
		argo: clusterArgoOptions{
			version:         opts.argoVersion,
			installIfAbsent: true,
			nodePort:        uiPort,
		},
		sveltos: clusterSveltosOptions{
			installIfAbsent: true,
			nodePort:        uiPort,
		},
		fluxInstallIfAbsent: true,
		noArgobot:           opts.noArgobot,
		noPlaceholderGate:   opts.noPlaceholderGate,
		argobotOCIRef:       opts.argobotOCIRef,
		prune:               opts.prune,
	}, func(f func()) { rollback = append(rollback, f) })
	if err != nil {
		return err
	}
	adminPassword := result.argo.adminPassword

	if err := clusterWriteRecord(clusterNewRecord(opts.name, opts.spaceSlug, clusterOriginKind, opts.controller)); err != nil {
		return fmt.Errorf("write cluster record: %w", err)
	}
	var envContents string
	switch opts.controller {
	case clusterControllerSveltos:
		envContents = clusterBuildSveltosEnvFile(opts.name, kubeconfigPath, uiPort)
	case clusterControllerFlux:
		envContents = clusterBuildFluxEnvFile(opts.name, kubeconfigPath)
	default:
		envContents = clusterBuildEnvFile(opts.name, kubeconfigPath, uiPort, adminPassword)
	}
	if err := os.WriteFile(envFilePath, []byte(envContents), 0o600); err != nil {
		return fmt.Errorf("write env file: %w", err)
	}

	commit = true
	clusterPrintSummaryHead(out, "Done", result, kubeconfigPath, envFilePath)
	switch opts.controller {
	case clusterControllerSveltos:
		fmt.Fprintf(out, "\nSveltos dashboard: http://localhost:%d\n", uiPort)
		fmt.Fprintf(out, "Log in with a token from:\n  KUBECONFIG=%s kubectl create token %s --duration=24h\n", kubeconfigPath, clusterSveltosDashboardAccount)
	case clusterControllerFlux:
		fmt.Fprintf(out, "\nFlux has no UI. See its state with:\n  KUBECONFIG=%s %s\n", kubeconfigPath, clusterFluxStatusCommand())
	default:
		fmt.Fprintf(out, "\nArgo CD: http://localhost:%d  (admin / %s)\n", uiPort, adminPassword)
		fmt.Fprintf(out, "Reopen it later with the password on your clipboard:\n  cub cluster open %s\n", opts.name)
	}
	switch {
	case hasUI && len(ports) > 1:
		fmt.Fprintf(out, "\nUser NodePort window: %d-%d (%s uses %d; rest are open)\n", startPort, startPort+windowSize-1, opts.controller, uiPort)
	case !hasUI && len(ports) > 0:
		fmt.Fprintf(out, "\nUser NodePort window: %d-%d (all open)\n", startPort, startPort+windowSize-1)
	}
	if len(opts.mounts) > 0 {
		fmt.Fprintf(out, "\nHost mounts (host → node, accessible to pods via hostPath):\n")
		for _, m := range opts.mounts {
			fmt.Fprintf(out, "  %s → %s\n", m.HostPath, m.ContainerPath)
		}
	}
	clusterPrintSummaryTail(out, result, envFilePath)
	return nil
}

// clusterArgobotOCIRefFor validates controller and returns the argobot bundle
// to install for it: flagValue when --argobot-oci was given, otherwise the
// controller's own bundle, since argobot's Sveltos and Flux deployments are
// bundles of their own.
func clusterArgobotOCIRefFor(controller, flagValue string, changed bool) (string, error) {
	switch controller {
	case clusterControllerArgo:
		return flagValue, nil
	case clusterControllerSveltos:
		if !changed {
			return clusterArgobotSveltosOCIRef, nil
		}
		return flagValue, nil
	case clusterControllerFlux:
		if !changed {
			return clusterArgobotFluxOCIRef, nil
		}
		return flagValue, nil
	}
	return "", fmt.Errorf("--controller %q: must be %s, %s or %s", controller,
		clusterControllerArgo, clusterControllerSveltos, clusterControllerFlux)
}

// clusterBuildSveltosEnvFile renders the shell-source env file for a cluster
// running Sveltos. The dashboard token is not stored: it expires, so it is
// created when needed.
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
