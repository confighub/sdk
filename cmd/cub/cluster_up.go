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
}

var clusterUpCmd = &cobra.Command{
	Use:   "up",
	Short: "Bring up a kind cluster wired to ConfigHub via Argo CD or Sveltos",
	Long: `Creates a kind cluster, installs Argo CD into it, and provisions the
ConfigHub side across two Spaces:

  <name>              the cluster (target-prefix) Space: an OCI target that
                      represents the cluster, and argobot's Channel. This Space
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

Unless --no-argobot is given, argobot is installed: a ConfigHub bot that
force-syncs the matching Argo CD Application the moment a deploy happens. It is
delivered as a component (a shared "argobot-base" installed from its OCI config
bundle, plus a per-cluster "argobot-<name>" variant), listens on a Channel named
"argobot" in the cluster Space that subscribes to the target's apply and release
events, runs as the cluster's service account, and runs in the default
kubernetes sync mode (no Argo CD token needed).

The cluster's identity on the ConfigHub side is one service account, "argo-<name>",
with no organization role: the OCI target grants its user View, ViewChildren and
EditChildren, which is exactly the cluster's deployment path. Argo CD pulls the
cluster's Releases with a long-lived token of it (the argocd/confighub-oci-creds
Secret; with --controller sveltos, projectsveltos/confighub-gateway), and argobot
authenticates as it with a key (the argobot/argobot-secrets Secret). Both are
issued here and handed straight to the cluster; ConfigHub keeps
the public half of the key and a revocable record of the token
("cub serviceaccount token list argo-<name>").

The cluster's OCI target is gated: a "no-placeholders" Trigger is created in the
cluster Space and the target selects every Trigger in that Space, so publishing a
Release that still contains an unfilled "confighubplaceholder" value is blocked
for every Space that releases to this cluster — including deployment variants
that do not exist yet. Add your own gates by creating Triggers in the cluster
Space and running "cub target update --space <name> target --refresh-triggers".
Pass --no-placeholder-gate to skip the placeholder Trigger; the target is wired
either way.

Argo CD is reachable at http://localhost:<argo-port> (server.insecure=true);
admin credentials are written to <config-dir>/clusters/<name>.env for
` + "`source`" + `-ing into your shell along with KUBECONFIG.

With --controller sveltos, Sveltos is installed instead of Argo CD, and the
apps Space is <name>-sveltos-apps. Its root Unit is a ClusterProfile that pulls
the apps Space's own Release and deploys it to the management cluster, so each
ClusterProfile Unit added there, pulling a deployment Space's Release, becomes
a profile Sveltos runs; "cub variant create --target" adds one per deployment.
argobot is installed in its Sveltos mode, from its own base Space
("argobot-sveltos-base"): on each deploy it redeploys the matching profile, and
it reports live status from Sveltos health reports. Sveltos also polls each
Release itself, as a fallback: every 10 minutes with argobot, every minute
with --no-argobot, where polling is all there is. The Sveltos dashboard is reachable at
http://localhost:<dashboard-port>, and logs in with a token from the
platform-admin ServiceAccount.

Use --mount HOST[:CONTAINER] (repeatable) to bind-mount host directories
into the cluster node.`,
	RunE: clusterUpCmdRun,
}

func init() {
	clusterUpCmd.Flags().StringVar(&clusterUpArgs.name, "name", "", "cluster name (auto-generated if empty)")
	clusterUpCmd.Flags().StringVar(&clusterUpArgs.spaceSlug, "space", "", "ConfigHub space slug for the cluster's target and argobot channel (the \"target prefix\"); the apps space is this slug + \"-argo-apps\", or + \"-sveltos-apps\" with --controller sveltos (defaults to <name>)")
	clusterUpCmd.Flags().StringVar(&clusterUpArgs.controller, "controller", clusterControllerArgo, "controller to install and wire to ConfigHub: argo (Argo CD) or sveltos (Sveltos)")
	clusterUpCmd.Flags().StringArrayVar(&clusterUpArgs.mounts, "mount", nil, "host:container bind mount (repeatable; container path defaults to /mnt/<basename>)")
	clusterUpCmd.Flags().BoolVar(&clusterUpArgs.noPorts, "no-ports", false, "only reserve the Argo NodePort; skip the user-app NodePort window")
	clusterUpCmd.Flags().BoolVar(&clusterUpArgs.noArgobot, "no-argobot", false, "skip installing argobot (the event-driven Argo CD sync, or Sveltos redeploy, bot)")
	clusterUpCmd.Flags().BoolVar(&clusterUpArgs.noPlaceholderGate, "no-placeholder-gate", false, "skip the default vet-placeholders Trigger, allowing Releases with unfilled confighubplaceholder values to be published to this cluster")
	clusterUpCmd.Flags().StringVar(&clusterUpArgs.argobotOCI, "argobot-oci", clusterArgobotOCIRef, "OCI reference of argobot's config bundle, or a local file or directory of its manifests (default with --controller sveltos: "+clusterArgobotSveltosOCIRef+")")
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
}

func clusterUpCmdRun(cmd *cobra.Command, args []string) error {
	mounts, err := clusterParseMounts(clusterUpArgs.mounts)
	if err != nil {
		return err
	}
	argobotOCIRef := clusterUpArgs.argobotOCI
	switch clusterUpArgs.controller {
	case clusterControllerArgo:
	case clusterControllerSveltos:
		// argobot's Sveltos deployment is a bundle of its own.
		if !cmd.Flags().Changed("argobot-oci") {
			argobotOCIRef = clusterArgobotSveltosOCIRef
		}
	default:
		return fmt.Errorf("--controller %q: must be %s or %s", clusterUpArgs.controller, clusterControllerArgo, clusterControllerSveltos)
	}
	return clusterUpRun(cmd.OutOrStdout(), clusterUpOptions{
		name:              clusterUpArgs.name,
		spaceSlug:         clusterUpArgs.spaceSlug,
		controller:        clusterUpArgs.controller,
		mounts:            mounts,
		noPorts:           clusterUpArgs.noPorts,
		noArgobot:         clusterUpArgs.noArgobot,
		noPlaceholderGate: clusterUpArgs.noPlaceholderGate,
		argobotOCIRef:     argobotOCIRef,
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
	sveltos := opts.controller == clusterControllerSveltos
	appsSlug := opts.spaceSlug + clusterArgoAppsSuffix
	if sveltos {
		appsSlug = opts.spaceSlug + clusterSveltosAppsSuffix
	}

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

	// The cluster's service account is org-scoped, so a leftover one is not
	// caught by the Space checks above. Surfaced here for the same reason.
	if exists, err := clusterServiceAccountExists(clusterServiceAccountSlug(opts.name)); err != nil {
		return err
	} else if exists {
		return fmt.Errorf("ConfigHub service account %q already exists (leftover cluster identity; delete it with 'cub serviceaccount delete %s')",
			clusterServiceAccountSlug(opts.name), clusterServiceAccountSlug(opts.name))
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
	// user-allocatable NodePorts.
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
	uiPort := startPort
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

	var adminPassword string
	if sveltos {
		fmt.Fprintln(out, "Installing Sveltos...")
		if err := clusterSveltosInstall(ctx, kubeconfigPath, uiPort, out); err != nil {
			return err
		}
	} else {
		fmt.Fprintln(out, "Installing Argo CD...")
		adminPassword, err = clusterArgoInstall(ctx, kubeconfigPath, uiPort, out)
		if err != nil {
			return err
		}
	}

	// The cluster (target-prefix) Space holds the target and channel. It carries
	// the cluster label + confighub.com/cluster-* annotations so `cub cluster
	// list`/`down` find the cluster from it; the apps Space below is derived and
	// left unlabeled so the cluster lists once.
	fmt.Fprintf(out, "Creating ConfigHub space %q (target + argobot channel)...\n", opts.spaceSlug)
	hostname, _ := os.Hostname()
	annotations := map[string]string{
		clusterAnnotationName:       opts.name,
		clusterAnnotationHost:       hostname,
		clusterAnnotationPortRange:  portRange,
		clusterAnnotationController: opts.controller,
	}
	if sveltos {
		annotations[clusterAnnotationSveltosPort] = fmt.Sprintf("%d", uiPort)
	} else {
		annotations[clusterAnnotationArgoPort] = fmt.Sprintf("%d", uiPort)
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

	// The cluster's identity on the ConfigHub side: one service account, used
	// two ways. Argo CD pulls the cluster's Releases from the OCI registry with
	// a long-lived token of it, and argobot authenticates as it with a key. It
	// has no organization role; the Target and Channel below grant its user
	// exactly the cluster's deployment path. Org-scoped, so it is rolled back
	// on its own rather than with a Space.
	serviceAccountSlug := clusterServiceAccountSlug(opts.name)
	fmt.Fprintf(out, "Creating service account %q (OrgRole=none; Argo CD pulls and argobot runs as it)...\n", serviceAccountSlug)
	serviceAccountID, serviceAccountUserID, err := clusterCreateServiceAccount(serviceAccountSlug, serviceAccountSlug,
		map[string]string{clusterAnnotationName: opts.name})
	if err != nil {
		return err
	}
	rollback = append(rollback, func() {
		fmt.Fprintf(out, "Rolling back: cub serviceaccount delete %q\n", serviceAccountSlug)
		_ = clusterDeleteServiceAccount(serviceAccountSlug)
	})
	fmt.Fprintf(out, "Issuing a key for argobot and a token for Argo CD on %q...\n", serviceAccountSlug)
	privateKey, err := clusterCreateServiceAccountKey(serviceAccountID, serviceAccountUserID, "argobot in cluster "+opts.name)
	if err != nil {
		return fmt.Errorf("issue the service account key: %w", err)
	}
	pullToken, err := clusterCreateServiceAccountToken(serviceAccountID, "Argo CD in cluster "+opts.name+" (OCI pull)")
	if err != nil {
		return fmt.Errorf("issue the service account token: %w", err)
	}

	// The default gate, created BEFORE the target: the server resolves a
	// Target's WhereTrigger into its read-only TriggerIDs at create time, so a
	// Trigger that does not exist yet would not be selected. --no-placeholder-gate
	// skips the Trigger but not the wiring below — the cluster Space stays the
	// place to put gates, it just starts out empty.
	if !opts.noPlaceholderGate {
		fmt.Fprintf(out, "Creating placeholder gate trigger %q (vet-placeholders on every Mutation)...\n", clusterGateTriggerSlug)
		if _, err := clusterCreateGateTrigger(spaceID, clusterGateTriggerSlug); err != nil {
			return fmt.Errorf("create placeholder gate trigger: %w", err)
		}
	}

	fmt.Fprintf(out, "Creating OCI target %q (granting %q its deployment path)...\n", clusterTargetSlug, serviceAccountSlug)
	// URL-TargetUI deep link: the ConfigHub UI substitutes "{slug}" with the
	// Space slug at render time, linking a Unit straight to its Argo CD
	// Application UI on the locally-forwarded argocd-server NodePort.
	// argo-apps-space lets API clients resolve the Space holding the cluster's
	// Argo Application Units from the target; that Space is the apps Space.
	// A Sveltos target carries sveltos-apps-space instead, from which `cub
	// variant create --target` adds a ClusterProfile rather than an Argo
	// Application. Its URL-TargetUI links to the Sveltos dashboard's page for that
	// ClusterProfile, which is named after the Space.
	targetAnnotations := map[string]string{
		clusterAnnotationController: opts.controller,
	}
	if sveltos {
		targetAnnotations[clusterAnnotationTargetUI] = clusterSveltosTargetUI(uiPort)
		targetAnnotations[clusterAnnotationSveltosAppsSpace] = appsSlug
		targetAnnotations[clusterAnnotationSveltosPollInterval] = clusterSveltosPollInterval(!opts.noArgobot)
	} else {
		targetAnnotations[clusterAnnotationTargetUI] = fmt.Sprintf("http://localhost:%d/applications/argocd/{slug}", uiPort)
		targetAnnotations[clusterAnnotationArgoAppsSpace] = appsSlug
	}
	// Selecting the cluster Space's Triggers makes them gate every Unit bound to
	// this target, wherever that Unit lives — the apps Space, the argobot
	// variant, and every deployment variant, including ones created later.
	targetID, err := clusterCreateOCITarget(spaceID, serviceAccountUserID, clusterTargetSlug, clusterTargetSlug,
		targetAnnotations, clusterWhereTriggerForSpace(spaceID))
	if err != nil {
		return err
	}

	// The apps Space holds the root app-of-apps and every child Argo Application
	// Unit, or with Sveltos the root ClusterProfile and every child
	// ClusterProfile Unit. Its release target is the OCI target in the cluster
	// Space above — a cross-Space reference, so the apps Space (the config
	// bundle) never points its release target at a target inside itself.
	appsKind := "Argo Application Units"
	if sveltos {
		appsKind = "Sveltos ClusterProfile Units"
	}
	fmt.Fprintf(out, "Creating ConfigHub space %q (%s)...\n", appsSlug, appsKind)
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

	// Where the server says its registry is. Asked once and used for both the
	// endpoint and its transport below, so the two cannot disagree.
	ociServerURL, err := clusterOCIServerURL()
	if err != nil {
		return err
	}
	// OCI URL for Argo (running inside kind) — the advertised registry,
	// rewritten to host.docker.internal for loopback cub servers.
	ociURLForArgo, err := clusterOCIEndpointFromContainer(ociServerURL, clusterServerURL())
	if err != nil {
		return err
	}
	// OCI URL recorded inside the root Application's source.repoURL — Argo
	// pulls from this, so it must be the in-cluster-reachable form. The root
	// app-of-apps references the apps Space's Release, served at
	// /space/<apps-slug>, tag "latest".
	rootRepoURL := strings.TrimRight(ociURLForArgo, "/") + "/space/" + appsSlug

	// Plain-HTTP OCI from the advertised registry's scheme, or failing that from
	// the cub server URL's: http:// API → http:// OCI (the local-dev case),
	// https:// API → TLS OCI.
	ociInsecure, err := clusterOCIInsecure(ociServerURL, clusterServerURL())
	if err != nil {
		return err
	}

	var rootManifest []byte
	if sveltos {
		if err := clusterSveltosApplyOCICreds(ctx, kubeconfigPath, pullToken, out); err != nil {
			return err
		}
		fmt.Fprintf(out, "Creating root ClusterProfile Unit %q in apps space %q...\n", clusterRootUnitSlug, appsSlug)
		rootManifest = clusterSveltosRootProfileManifest(rootRepoURL, clusterSveltosPollInterval(!opts.noArgobot), ociInsecure)
	} else {
		if err := clusterArgoApplyOCIRepoCreds(ctx, kubeconfigPath, ociURLForArgo, pullToken, ociInsecure, out); err != nil {
			return err
		}
		fmt.Fprintf(out, "Creating root Application Unit %q in apps space %q...\n", clusterRootUnitSlug, appsSlug)
		rootManifest = clusterArgoRootAppManifest(appsSlug, rootRepoURL)
	}
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
		clusterSpaceID:       spaceID,
		targetID:             targetID,
		serviceAccountUserID: serviceAccountUserID,
		privateKey:           privateKey,
		kubeconfigPath:       kubeconfigPath,
	}, func(f func()) { rollback = append(rollback, f) }); err != nil {
		return fmt.Errorf("install argobot: %w", err)
	}

	// Publish the apps Space's Release once, after root's triggers settle, so
	// the bundle carries root's post-trigger revision plus any Application Units
	// argobot's install added to the Space (already trigger-settled by variant
	// create). This is the bundle the root app-of-apps pulls on its first sync.
	fmt.Fprintln(out, "\nPublishing the apps space's release (populates the OCI bundle the root pulls)...")
	if err := clusterWaitUnitTriggers(appsSpaceID, rootUnitID); err != nil {
		return err
	}
	if err := clusterPublishRelease(appsSpaceID); err != nil {
		return err
	}

	fmt.Fprintln(out, "Bootstrapping the root via kubectl (the only kubectl-apply moment)...")
	if err := clusterKubectlApply(ctx, kubeconfigPath, rootManifest, out); err != nil {
		return err
	}

	envContents := clusterBuildEnvFile(opts.name, kubeconfigPath, uiPort, adminPassword)
	if sveltos {
		envContents = clusterBuildSveltosEnvFile(opts.name, kubeconfigPath, uiPort)
	}
	if err := os.WriteFile(envFilePath, []byte(envContents), 0o600); err != nil {
		return fmt.Errorf("write env file: %w", err)
	}

	commit = true
	fmt.Fprintf(out, "\nDone.\n  cluster:    %s\n  kubeconfig: %s\n  env file:   %s\n  space:      %s\n  apps space: %s\n  target:     %s/%s\n  identity:   service account %s (Argo CD pulls with a token of it; argobot runs as it with a key)\n  root app:   %s/%s\n",
		opts.name, kubeconfigPath, envFilePath,
		opts.spaceSlug,
		appsSlug,
		opts.spaceSlug, clusterTargetSlug,
		serviceAccountSlug,
		appsSlug, clusterRootUnitSlug)
	if !opts.noArgobot && sveltos {
		fmt.Fprintf(out, "  argobot:    %s-%s (Sveltos ClusterProfile; redeploys on publish), channel %s/%s\n", clusterArgobotComponent, opts.name, opts.spaceSlug, clusterArgobotChannelSlug)
	} else if !opts.noArgobot {
		fmt.Fprintf(out, "  argobot:    %s-%s (Argo app; kubernetes sync mode), channel %s/%s\n", clusterArgobotComponent, opts.name, opts.spaceSlug, clusterArgobotChannelSlug)
	}
	if opts.noPlaceholderGate {
		fmt.Fprintf(out, "  gates:      none (--no-placeholder-gate); Triggers added to space %q gate the\n              target once you run 'cub target update --space %s target --refresh-triggers'\n",
			opts.spaceSlug, opts.spaceSlug)
	} else {
		fmt.Fprintf(out, "  gates:      %s/%s (vet-placeholders) — blocks publishing a Release with\n              unfilled confighubplaceholder values to this cluster\n",
			opts.spaceSlug, clusterGateTriggerSlug)
	}
	if sveltos {
		fmt.Fprintf(out, "\nSveltos dashboard: http://localhost:%d\n", uiPort)
		fmt.Fprintf(out, "Log in with a token from:\n  KUBECONFIG=%s kubectl create token %s --duration=24h\n", kubeconfigPath, clusterSveltosDashboardAccount)
	} else {
		fmt.Fprintf(out, "\nArgo CD: http://localhost:%d  (admin / %s)\n", uiPort, adminPassword)
		fmt.Fprintf(out, "Reopen it later with the password on your clipboard:\n  cub cluster open %s\n", opts.name)
	}
	if len(ports) > 1 {
		fmt.Fprintf(out, "\nUser NodePort window: %d-%d (%s uses %d; rest are open)\n", startPort, startPort+windowSize-1, opts.controller, uiPort)
	}
	if len(opts.mounts) > 0 {
		fmt.Fprintf(out, "\nHost mounts (host → node, accessible to pods via hostPath):\n")
		for _, m := range opts.mounts {
			fmt.Fprintf(out, "  %s → %s\n", m.HostPath, m.ContainerPath)
		}
	}
	if sveltos {
		fmt.Fprintf(out, "\nLoad the cluster into your shell:\n  source %s\n", envFilePath)
		fmt.Fprintf(out, "\nDeploy an app to this cluster:\n  1. cub variant create <name> <base-space> --target %s/%s [--namespace <ns>]\n  2. cub release publish <name>\nStep 1 clones <base-space> into a deployment space bound to the cluster's\ntarget and adds its Sveltos ClusterProfile to the apps space %q; step 2\nmakes the deployment's config go live and, the first time, republishes the\napps space so the root profile applies the ClusterProfile. See\n\"cub variant create --help\".\n",
			opts.spaceSlug, clusterTargetSlug, appsSlug)
		return nil
	}
	fmt.Fprintf(out, "\nLoad the cluster + Argo creds into your shell:\n  source %s\n", envFilePath)
	fmt.Fprintf(out, "\nDeploy an app to this cluster:\n  1. cub variant create <name> <base-space> --target %s/%s [--namespace <ns>]\n  2. cub release publish <name>\nStep 1 clones <base-space> into a deployment space bound to the cluster's\ntarget and auto-creates its Argo CD Application in the apps space %q\n(republishing it so Argo picks it up); step 2 makes the deployment's config\ngo live. See \"cub variant create --help\".\n",
		opts.spaceSlug, clusterTargetSlug, appsSlug)
	return nil
}

// clusterBuildSveltosEnvFile renders the shell-source env file for a cluster
// running Sveltos. The dashboard token is not stored: it expires, so it is
// created when needed.
func clusterBuildSveltosEnvFile(name, kubeconfigPath string, dashboardPort int) string {
	return fmt.Sprintf(`# cub cluster: %s
# Source this file: source %s
export KUBECONFIG=%s
export SVELTOS_DASHBOARD=http://localhost:%d
`, name, clusterEnvFilePath(name), kubeconfigPath, dashboardPort)
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
