// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/exec"
	"strings"

	"github.com/spf13/cobra"
	"sigs.k8s.io/yaml"
)

var clusterAttachArgs struct {
	name              string
	spaceSlug         string
	provider          string
	cluster           string
	kubeconfig        string
	kubeContext       string
	awsRegion         string
	awsProfile        string
	grantAccess       bool
	controller        string
	controllerVersion string
	noInstall         bool
	noArgoCD          bool
	noArgobot         bool
	noPlaceholderGate bool
	prune             bool
}

var clusterAttachCmd = &cobra.Command{
	Use:   "attach <name>",
	Args:  cobra.MaximumNArgs(1),
	Short: "Wire a cluster you already have into ConfigHub, via Argo CD, Sveltos or Flux",
	Long: `Attaches a cluster you already have — any cluster your kubeconfig can reach
with cluster-admin — into ConfigHub. It is everything "cub cluster up" does
after creating its kind cluster, and "cub cluster up" is built on it.

Attach never creates or destroys a cluster. The inverse, "cub cluster detach",
removes the wiring and leaves the cluster running.

Where the cluster's credentials come from is --provider:

  kubeconfig  (default) your kubeconfig — $KUBECONFIG or ~/.kube/config, or
              --kubeconfig — at its current context, or the one --kube-context names.
  eks         the AWS CLI: "aws eks update-kubeconfig" for the cluster named by
              --cluster (defaults to the cluster name), honoring --aws-region and
              --aws-profile or the usual AWS environment. The kubeconfig
              authenticates through the CLI, so it stays good as long as your
              AWS credentials are.
  kind        a kind cluster already on this machine, named by --cluster
              (defaults to the cluster name), through "kind get kubeconfig". "cub cluster
              up" is the command that creates one and attaches it.

A self-contained copy of the credentials is written to
<config-dir>/clusters/<name>.kubeconfig, beside a record of which ConfigHub
server and organization the cluster is wired to, so "cub cluster list", "open"
and "down" find it whichever cub context is current. A kubeconfig with a
static token that expires, expires in the copy too.

An EKS cluster created by something other than you — an ACK controller, a
pipeline role — grants its bootstrap admin access to that identity, and
rejects you with a bare 401. Attach diagnoses that: if your AWS identity has
no access entry on the cluster it prints the commands that add one, or runs
them for you with --grant-access (an access entry with the
AmazonEKSClusterAdminPolicy).

What it sets up, in the cluster:

  Argo CD             installed from the upstream manifest if the argocd
                      namespace has no argocd-server; an existing Argo CD is
                      left as found (--no-install fails instead of installing).
                      Either way argocd-cm gets the ignoreDifferences block the
                      self-referencing root Application needs, unless it
                      already carries one.
  repo-creds Secret   a long-lived token of the cluster's service account, so
                      Argo CD can pull from ConfigHub's OCI registry.
  root Application    bootstrapped once with kubectl; from then on Argo pulls
                      everything else from ConfigHub. It never deletes an
                      Application unless --prune is given (below).
  argobot             unless --no-k8sbot: a bot that listens on a Channel and
                      force-syncs the matching Argo Application the moment a
                      deploy happens, instead of waiting for Argo's poll, and
                      reports each Release's live status back.

And in ConfigHub:

  <name>              the cluster Space: an OCI target that represents the
                      cluster, and argobot's Channel. A pure namespace — it
                      holds no config bundle.
  <name>-argo-apps    the apps Space: the root "app of apps" Application Unit
                      and every child Argo Application Unit. Its release target
                      is the OCI target in the cluster Space, so the apps Space
                      is the config bundle Argo pulls.
  argo-<name>         a service account with no organization role, the
                      cluster's identity: the target grants its user View,
                      Edit, ViewChildren and EditChildren; Argo CD pulls with a token
                      of it and argobot runs as it with a key.

The OCI target is gated: a "no-placeholders" Trigger in the cluster Space
blocks publishing a Release that still contains an unfilled
"confighubplaceholder" value, for every Space that releases to this cluster.
Pass --no-placeholder-gate to skip the Trigger; the target is wired either way.

By default nothing cub wires into Argo CD deletes anything: a deployment whose
Application Unit is removed from the apps Space keeps running, so a bad or
empty Release can never take workloads down. --prune opts the cluster into
removal: the root Application prunes an Application whose Unit is gone, and
"cub variant create --target" gives each deployment's Application prune and
Argo's resources finalizer, so removing the Unit removes what the deployment
deployed. --prune is recorded on the OCI target, so it covers deployments
created later; it does not change Applications that already exist.

A cluster is wired to one ConfigHub at a time: attach refuses a cluster whose
Argo CD already holds ConfigHub registry credentials, and a remote cluster
against a ConfigHub on localhost, which Argo inside the cluster could never
reach.

Argo CD is not exposed outside the cluster by attach. Reach its UI with
"kubectl port-forward svc/argocd-server -n argocd 8080:80" against the
kubeconfig attach wrote; the initial admin password is in
argocd-initial-admin-secret.

With --controller sveltos, Sveltos takes Argo CD's place: it is installed
from the upstream manifest, with its dashboard, if the projectsveltos
namespace has no addon-controller, and an existing Sveltos is left as found
(--no-install fails instead of installing). The apps Space is
<name>-sveltos-apps, its root Unit a ClusterProfile that pulls the apps
Space's own Release, and argobot comes from "argobot-sveltos-base". The
dashboard is not exposed outside the cluster; reach it with "kubectl
port-forward svc/dashboard -n projectsveltos 8080:80", and log in with a
token from the platform-admin ServiceAccount. See "cub cluster up --help" for
how the profiles, polling and --prune work.

With --controller flux, Flux takes Argo CD's place: it is installed from the
upstream manifest if the flux-system namespace has no kustomize-controller,
and an existing Flux is left as found (--no-install fails instead of
installing). The apps Space is <name>-flux-apps, its root Unit an
OCIRepository and Kustomization, both named confighub-root, that pull and
apply the apps Space's own Release, and argobot comes from
"argobot-flux-base". Flux has no UI; "kubectl -n flux-system get
ocirepository,kustomization" shows its state. See "cub cluster up --help" for
how the Kustomizations, polling and --prune work.

A Sveltos or Flux cluster is refused, like an Argo CD one, when it already
holds the registry credentials of an attachment: the confighub-gateway Secret
in projectsveltos, or the confighub-oci Secret in flux-system.

Examples:
  # Attach whatever your current kube context points at
  cub cluster attach staging

  # Attach a named context from a specific kubeconfig
  cub cluster attach prod-eu --kubeconfig ~/.kube/eu.yaml --kube-context prod-eu

  # Attach a cluster that already runs Argo CD, without argobot
  cub cluster attach shared --no-install --no-k8sbot

  # Attach a cluster through Flux, using the Flux it already runs
  cub cluster attach staging --controller flux --no-install

  # Attach an EKS cluster of the same name, granting yourself cluster-admin first
  cub cluster attach inference-demo --provider eks \
    --aws-region us-west-2 --aws-profile cubdev --grant-access

  # Attach a kind cluster you created by hand
  cub cluster attach lab --provider kind`,
	RunE: clusterAttachCmdRun,
}

func init() {
	clusterAttachCmd.Flags().StringVar(&clusterAttachArgs.name, "name", "", "cluster name (the argument; kept as a flag for older scripts)")
	_ = clusterAttachCmd.Flags().MarkHidden("name")
	clusterAttachCmd.Flags().StringVar(&clusterAttachArgs.spaceSlug, "space", "", "ConfigHub space slug for the cluster's target and argobot channel (the \"target prefix\"); the apps space is this slug + \"-argo-apps\", + \"-sveltos-apps\" with --controller sveltos, or + \"-flux-apps\" with --controller flux (defaults to <name>)")
	clusterAttachCmd.Flags().StringVar(&clusterAttachArgs.provider, "provider", clusterProviderKubeconfig, "where the cluster's credentials come from: kubeconfig, eks or kind")
	clusterAttachCmd.Flags().StringVar(&clusterAttachArgs.cluster, "cluster", "", "the cluster's name at the provider (--provider eks or kind; defaults to the cluster name)")
	clusterAttachCmd.Flags().StringVar(&clusterAttachArgs.kubeconfig, "kubeconfig", "", "--provider kubeconfig: the kubeconfig to read the cluster from (defaults to $KUBECONFIG or ~/.kube/config)")
	clusterAttachCmd.Flags().StringVar(&clusterAttachArgs.kubeContext, "kube-context", "", "--provider kubeconfig: the kube context to attach (defaults to the kubeconfig's current context)")
	clusterAttachCmd.Flags().StringVar(&clusterAttachArgs.awsRegion, "aws-region", "", "--provider eks: the cluster's AWS region (defaults to the AWS CLI's: AWS_REGION or the profile's)")
	clusterAttachCmd.Flags().StringVar(&clusterAttachArgs.awsProfile, "aws-profile", "", "--provider eks: the AWS CLI profile (defaults to AWS_PROFILE)")
	clusterAttachCmd.Flags().BoolVar(&clusterAttachArgs.grantAccess, "grant-access", false, "--provider eks: if your AWS identity has no access entry on the cluster, add one with cluster-admin")
	clusterAttachCmd.Flags().StringVar(&clusterAttachArgs.controller, "controller", clusterControllerArgo, "controller to wire to ConfigHub: argo (Argo CD), sveltos (Sveltos) or flux (Flux)")
	clusterAttachCmd.Flags().StringVar(&clusterAttachArgs.controllerVersion, "controller-version", "", "controller release to install when absent (default: argo "+clusterArgoDefaultVersion+", sveltos "+clusterSveltosVersion+", flux "+clusterFluxVersion+"; Argo CD must be v3.1 or later, which oci:// sources need)")
	clusterAttachCmd.Flags().BoolVar(&clusterAttachArgs.noInstall, "no-install", false, "never install the controller; fail if the cluster has none")
	clusterAttachCmd.Flags().BoolVar(&clusterAttachArgs.noArgoCD, "no-argocd", false, "--controller argo: the same as --no-install")
	_ = clusterAttachCmd.Flags().MarkHidden("no-argocd")
	clusterAttachCmd.Flags().BoolVar(&clusterAttachArgs.noArgobot, "no-k8sbot", false, "skip installing argobot (the event-driven Argo CD sync, Sveltos redeploy, or Flux reconciliation, bot)")
	clusterAttachCmd.Flags().BoolVar(&clusterAttachArgs.noPlaceholderGate, "no-placeholder-gate", false, "skip the default vet-placeholders Trigger, allowing Releases with unfilled confighubplaceholder values to be published to this cluster")
	clusterAttachCmd.Flags().BoolVar(&clusterAttachArgs.prune, "prune", false, "let the controller delete what a removed deployment deployed: the root prunes deployments whose Units leave the apps space (Sveltos's always does), and deployments created later prune what they deployed")
	clusterCmd.AddCommand(clusterAttachCmd)
}

// clusterAttachOptions are the `cub cluster attach` inputs.
type clusterAttachOptions struct {
	name              string
	spaceSlug         string
	provider          string
	cluster           string
	kubeconfig        string
	kubeContext       string
	awsRegion         string
	awsProfile        string
	grantAccess       bool
	controller        string
	controllerVersion string
	noInstall         bool
	noArgobot         bool
	noPlaceholderGate bool
	prune             bool
}

func clusterAttachCmdRun(cmd *cobra.Command, args []string) error {
	name, err := clusterNameArg(args, clusterAttachArgs.name, "attach", true)
	if err != nil {
		return err
	}
	// --no-argocd is --no-install from when Argo CD was the only controller.
	if clusterAttachArgs.noArgoCD && clusterAttachArgs.controller != clusterControllerArgo {
		return fmt.Errorf("--no-argocd applies to --controller %s; use --no-install", clusterControllerArgo)
	}
	return clusterAttachRun(cmd.OutOrStdout(), clusterAttachOptions{
		name:              name,
		spaceSlug:         clusterAttachArgs.spaceSlug,
		provider:          clusterAttachArgs.provider,
		cluster:           clusterAttachArgs.cluster,
		kubeconfig:        clusterAttachArgs.kubeconfig,
		kubeContext:       clusterAttachArgs.kubeContext,
		awsRegion:         clusterAttachArgs.awsRegion,
		awsProfile:        clusterAttachArgs.awsProfile,
		grantAccess:       clusterAttachArgs.grantAccess,
		controller:        clusterAttachArgs.controller,
		controllerVersion: clusterAttachArgs.controllerVersion,
		noInstall:         clusterAttachArgs.noInstall || clusterAttachArgs.noArgoCD,
		noArgobot:         clusterAttachArgs.noArgobot,
		noPlaceholderGate: clusterAttachArgs.noPlaceholderGate,
		prune:             clusterAttachArgs.prune,
	})
}

func clusterAttachRun(out io.Writer, opts clusterAttachOptions) error {
	if opts.name == "" {
		return fmt.Errorf("a cluster name is required: cub cluster attach <name>")
	}
	switch opts.controller {
	case clusterControllerArgo, clusterControllerSveltos, clusterControllerFlux:
	default:
		return fmt.Errorf("--controller %q: must be %s, %s or %s", opts.controller,
			clusterControllerArgo, clusterControllerSveltos, clusterControllerFlux)
	}
	if _, err := exec.LookPath("kubectl"); err != nil {
		return fmt.Errorf("kubectl not found on PATH")
	}
	if err := clusterAttachCheckProviderFlags(&opts); err != nil {
		return err
	}
	eks := opts.provider == clusterProviderEKS
	if opts.spaceSlug == "" {
		opts.spaceSlug = opts.name
	}
	appsSlug := opts.spaceSlug + clusterAppsSuffix(opts.controller)
	kubeconfigPath := clusterKubeconfigPath(opts.name)
	envFilePath := clusterEnvFilePath(opts.name)

	if rec, err := clusterReadRecord(opts.name); err != nil {
		return err
	} else if rec != nil {
		return fmt.Errorf("cluster %q is already cub-managed on this machine, wired to %s (see 'cub cluster list'); use another name, or detach it first (see 'cub cluster list')", opts.name, clusterRecordWhere(rec))
	}
	if _, err := os.Stat(kubeconfigPath); err == nil {
		return fmt.Errorf("kubeconfig %s already exists; cluster %q is already cub-managed (see 'cub cluster list')", kubeconfigPath, opts.name)
	}
	argobotSlug := ""
	if !opts.noArgobot {
		argobotSlug = clusterArgobotComponent + "-" + opts.name
	}
	if err := clusterRequireFreshConfigHub(opts.name, opts.spaceSlug, appsSlug, argobotSlug); err != nil {
		return err
	}

	// A self-contained copy of the credentials, at the path every other
	// `cub cluster` command looks for. Taken before anything is created, so a
	// kubeconfig that does not name a usable context fails here, with nothing
	// to roll back.
	if err := clusterEnsureKubeconfigDir(); err != nil {
		return err
	}
	var kubeconfig []byte
	var err error
	switch opts.provider {
	case clusterProviderEKS:
		fmt.Fprintf(out, "Resolving EKS cluster %q through the AWS CLI...\n", opts.cluster)
		kubeconfig, err = clusterEKSKubeconfig(ctx, opts.awsProfile, opts.awsRegion, opts.cluster, kubeconfigPath)
	case clusterProviderKind:
		fmt.Fprintf(out, "Reading kind cluster %q's credentials...\n", opts.cluster)
		kubeconfig, err = clusterKindKubeconfig(ctx, opts.cluster)
	default:
		fmt.Fprintf(out, "Reading cluster credentials from %s...\n", clusterDescribeKubeconfigSource(opts.kubeconfig, opts.kubeContext))
		kubeconfig, err = clusterExportKubeconfig(ctx, opts.kubeconfig, opts.kubeContext)
	}
	if err != nil {
		_ = os.Remove(kubeconfigPath)
		return err
	}
	if problem := clusterAttachReachabilityProblem(clusterServerURL(), clusterKubeconfigServer(kubeconfig)); problem != "" {
		_ = os.Remove(kubeconfigPath)
		return fmt.Errorf("%s", problem)
	}
	if err := os.WriteFile(kubeconfigPath, kubeconfig, 0o600); err != nil {
		return fmt.Errorf("write kubeconfig: %w", err)
	}
	rollback := []func(){
		func() {
			fmt.Fprintf(out, "Rolling back: removing %s\n", kubeconfigPath)
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

	// Installing the controller and applying the root need cluster-admin.
	// Fail here rather than halfway through a 20k-line manifest.
	fmt.Fprintln(out, "Checking cluster access (needs cluster-admin)...")
	if err := clusterRequireClusterAdmin(ctx, kubeconfigPath); err != nil {
		if !eks {
			return err
		}
		// An EKS cluster that rejects its own kubeconfig is almost always the
		// access-entry cliff; say so, or fix it.
		if err := clusterEKSEnsureAccess(ctx, kubeconfigPath, opts.awsProfile, opts.awsRegion, opts.cluster, opts.grantAccess, out); err != nil {
			return err
		}
	}
	// One ConfigHub per cluster. The registry credentials Secret is the
	// in-cluster mark of an attachment, whichever server it was to.
	if err := clusterAttachRequireUnwired(ctx, kubeconfigPath, opts.controller); err != nil {
		return err
	}

	spaceAnnotations := map[string]string{clusterAnnotationProvider: opts.provider}
	if opts.provider != clusterProviderKubeconfig {
		spaceAnnotations[clusterAnnotationProviderCluster] = opts.cluster
	}
	if eks && opts.awsRegion != "" {
		spaceAnnotations[clusterAnnotationEKSRegion] = opts.awsRegion
	}
	result, err := clusterAttach(out, clusterAttachSpec{
		name:              opts.name,
		spaceSlug:         opts.spaceSlug,
		kubeconfigPath:    kubeconfigPath,
		origin:            clusterOriginAttach,
		controller:        opts.controller,
		spaceAnnotations:  spaceAnnotations,
		controllerVersion: opts.controllerVersion,
		argo: clusterArgoOptions{
			installIfAbsent: !opts.noInstall,
		},
		sveltos: clusterSveltosOptions{
			installIfAbsent: !opts.noInstall,
		},
		fluxInstallIfAbsent: !opts.noInstall,
		noArgobot:           opts.noArgobot,
		noPlaceholderGate:   opts.noPlaceholderGate,
		prune:               opts.prune,
	}, func(f func()) { rollback = append(rollback, f) })
	if err != nil {
		return err
	}

	if err := clusterWriteRecord(clusterNewRecord(opts.name, opts.spaceSlug, clusterOriginAttach, opts.controller)); err != nil {
		return fmt.Errorf("write cluster record: %w", err)
	}
	if err := os.WriteFile(envFilePath, []byte(clusterBuildAttachEnvFile(opts.name, kubeconfigPath)), 0o600); err != nil {
		return fmt.Errorf("write env file: %w", err)
	}

	commit = true
	clusterPrintSummaryHead(out, "Attached", result, kubeconfigPath, envFilePath)
	switch {
	case opts.controller == clusterControllerSveltos && result.controllerInstalled:
		fmt.Fprintf(out, "\nSveltos: installed. Its dashboard is not exposed outside the cluster; reach it with\n  kubectl --kubeconfig %s port-forward svc/dashboard -n %s 8080:80\nand log in with a token from\n  kubectl --kubeconfig %s create token %s --duration=24h\n",
			kubeconfigPath, clusterSveltosNamespace, kubeconfigPath, clusterSveltosDashboardAccount)
	case opts.controller == clusterControllerSveltos:
		fmt.Fprintf(out, "\nSveltos: already installed, left as found\n")
	case opts.controller == clusterControllerFlux && result.controllerInstalled:
		fmt.Fprintf(out, "\nFlux: installed. It has no UI; see its state with\n  KUBECONFIG=%s %s\n", kubeconfigPath, clusterFluxStatusCommand())
	case opts.controller == clusterControllerFlux:
		fmt.Fprintf(out, "\nFlux: already installed, left as found\n")
	case result.argo.installed:
		fmt.Fprintf(out, "\nArgo CD: installed (admin / %s). It is not exposed outside the cluster; reach the UI with\n  kubectl --kubeconfig %s port-forward svc/argocd-server -n argocd 8080:80\n",
			result.argo.adminPassword, kubeconfigPath)
	case result.argo.adminPassword != "":
		fmt.Fprintf(out, "\nArgo CD: already installed, left as found (initial admin password: %s)\n", result.argo.adminPassword)
	default:
		fmt.Fprintf(out, "\nArgo CD: already installed, left as found\n")
	}
	clusterPrintSummaryTail(out, result, envFilePath)
	return nil
}

// clusterAttachRequireUnwired fails when an earlier attachment's registry
// credentials Secret is in the cluster: whichever ConfigHub it was to, the
// cluster is already wired to one.
func clusterAttachRequireUnwired(ctx context.Context, kubeconfigPath, controller string) error {
	const detachHint = "Detach it first with 'cub cluster detach' on the machine that attached it, or delete that Secret by hand"
	switch controller {
	case clusterControllerSveltos:
		if wired, err := clusterSveltosHasOCICreds(ctx, kubeconfigPath); err != nil {
			return err
		} else if wired {
			return fmt.Errorf("the cluster is already wired to a ConfigHub: Sveltos holds registry credentials (Secret %s/%s). %s",
				clusterSveltosNamespace, clusterSveltosGatewaySecret, detachHint)
		}
	case clusterControllerFlux:
		if existing, err := clusterFluxExistingOCICredsHost(ctx, kubeconfigPath); err != nil {
			return err
		} else if existing != "" {
			return fmt.Errorf("the cluster is already wired to a ConfigHub: Flux holds registry credentials for %s (Secret %s/%s). %s",
				existing, clusterFluxNamespace, clusterFluxCredsSecret, detachHint)
		}
	default:
		if existing, err := clusterArgoExistingOCICredsURL(ctx, kubeconfigPath); err != nil {
			return err
		} else if existing != "" {
			return fmt.Errorf("the cluster is already wired to a ConfigHub: Argo CD holds registry credentials for %s (Secret %s/%s). %s",
				existing, clusterArgoNamespace, clusterArgoOCICredsSecret, detachHint)
		}
	}
	return nil
}

// clusterAttachCheckProviderFlags validates --provider and the flags that
// belong to each provider, and fills --cluster from the cluster name where a provider
// names the cluster itself. Flags of another provider are refused rather than
// ignored: a --kube-context passed with --provider eks means the user expected it
// to do something.
func clusterAttachCheckProviderFlags(opts *clusterAttachOptions) error {
	kubeconfigFlags := opts.kubeconfig != "" || opts.kubeContext != ""
	eksFlags := opts.awsRegion != "" || opts.awsProfile != "" || opts.grantAccess
	switch opts.provider {
	case clusterProviderKubeconfig:
		if opts.cluster != "" {
			return fmt.Errorf("--cluster applies to --provider eks or kind; with --provider kubeconfig name the cluster with --kube-context")
		}
		if eksFlags {
			return fmt.Errorf("--aws-region, --aws-profile and --grant-access apply to --provider eks only")
		}
	case clusterProviderEKS:
		if kubeconfigFlags {
			return fmt.Errorf("--kubeconfig and --kube-context apply to --provider kubeconfig only; --provider eks resolves the kubeconfig itself")
		}
	case clusterProviderKind:
		if kubeconfigFlags {
			return fmt.Errorf("--kubeconfig and --kube-context apply to --provider kubeconfig only; --provider kind resolves the kubeconfig itself")
		}
		if eksFlags {
			return fmt.Errorf("--aws-region, --aws-profile and --grant-access apply to --provider eks only")
		}
	default:
		return fmt.Errorf("--provider %q: must be %s, %s or %s", opts.provider, clusterProviderKubeconfig, clusterProviderEKS, clusterProviderKind)
	}
	if opts.cluster == "" {
		opts.cluster = opts.name
	}
	return nil
}

// clusterDescribeKubeconfigSource names where attach is reading the cluster
// from, for the progress line.
func clusterDescribeKubeconfigSource(kubeconfig, kubeContext string) string {
	src := kubeconfig
	if src == "" {
		if env := os.Getenv("KUBECONFIG"); env != "" {
			src = "$KUBECONFIG (" + env + ")"
		} else {
			src = "~/.kube/config"
		}
	}
	if kubeContext == "" {
		return src + ", current context"
	}
	return src + ", context " + kubeContext
}

// clusterKubectlConfigViewArgs builds the `kubectl config view` invocation that
// exports one context as a standalone kubeconfig: --minify keeps only that
// context and what it references, --flatten inlines certificate files, and
// --raw stops the certificate data being redacted. The kubeconfig and kube context
// are passed through only when set, so kubectl's own loading rules
// ($KUBECONFIG, ~/.kube/config, current-context) apply otherwise.
func clusterKubectlConfigViewArgs(kubeconfig, kubeContext string) []string {
	args := []string{"config", "view", "--minify", "--flatten", "--raw", "-o", "yaml"}
	if kubeconfig != "" {
		args = append(args, "--kubeconfig", kubeconfig)
	}
	if kubeContext != "" {
		args = append(args, "--context", kubeContext)
	}
	return args
}

// clusterExportKubeconfig returns a self-contained kubeconfig for the chosen
// context. It runs kubectl without cub's per-cluster --kubeconfig, because the
// source is the user's own config.
func clusterExportKubeconfig(ctx context.Context, kubeconfig, kubeContext string) ([]byte, error) {
	if kubeconfig != "" {
		if _, err := os.Stat(kubeconfig); err != nil {
			return nil, fmt.Errorf("--kubeconfig: %w", err)
		}
	}
	var stdout, stderr bytes.Buffer
	cmd := exec.CommandContext(ctx, "kubectl", clusterKubectlConfigViewArgs(kubeconfig, kubeContext)...)
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("read kubeconfig: %w: %s", err, strings.TrimSpace(stderr.String()))
	}
	if clusterKubeconfigServer(stdout.Bytes()) == "" {
		if kubeContext != "" {
			return nil, fmt.Errorf("context %q not found in %s", kubeContext, clusterDescribeKubeconfigSource(kubeconfig, ""))
		}
		return nil, fmt.Errorf("no current context in %s; pass --kube-context", clusterDescribeKubeconfigSource(kubeconfig, ""))
	}
	return stdout.Bytes(), nil
}

// clusterKubeconfigServer returns the API server URL of the first cluster in a
// kubeconfig, or "" when it names none.
func clusterKubeconfigServer(kubeconfig []byte) string {
	var cfg struct {
		Clusters []struct {
			Cluster struct {
				Server string `json:"server"`
			} `json:"cluster"`
		} `json:"clusters"`
	}
	if err := yaml.Unmarshal(kubeconfig, &cfg); err != nil || len(cfg.Clusters) == 0 {
		return ""
	}
	return cfg.Clusters[0].Cluster.Server
}

// clusterAttachReachabilityProblem explains why a cluster could not reach the
// ConfigHub cub is logged into, or returns "" when nothing says it cannot. A
// ConfigHub on this machine's loopback is reachable only by a cluster on this
// machine; one whose API server is somewhere else would get a root Application
// that never pulls. An API server on loopback (kind, minikube, docker-desktop)
// is local and passes.
func clusterAttachReachabilityProblem(confighubURL, apiServerURL string) string {
	hubHost, err := clusterHost(confighubURL)
	if err != nil || !clusterIsLoopback(hubHost) {
		return ""
	}
	u, err := url.Parse(apiServerURL)
	if err != nil || u.Hostname() == "" {
		return ""
	}
	if clusterIsLoopback(u.Hostname()) {
		return ""
	}
	return fmt.Sprintf("the cub context's ConfigHub is on this machine (%s), which a cluster at %s cannot reach; attach a local cluster, or switch to a context on a reachable ConfigHub",
		confighubURL, apiServerURL)
}

// clusterRequireClusterAdmin fails unless the kubeconfig reaches the cluster
// and can create ClusterRoles — the permission an Argo CD install needs, and a
// fair proxy for cluster-admin. kubectl answers "no" with a non-zero exit, so
// the two failure modes are told apart by what it printed.
func clusterRequireClusterAdmin(ctx context.Context, kubeconfigPath string) error {
	var stdout, stderr bytes.Buffer
	cmd := exec.CommandContext(ctx, "kubectl", "--kubeconfig", kubeconfigPath,
		"auth", "can-i", "create", "clusterroles")
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	answer := strings.TrimSpace(stdout.String())
	switch {
	case err == nil && answer == "yes":
		return nil
	case answer == "no":
		return fmt.Errorf("the kubeconfig's identity cannot create ClusterRoles; attaching needs cluster-admin on the cluster")
	default:
		return fmt.Errorf("cannot reach the cluster with that kubeconfig: %s", strings.TrimSpace(stderr.String()))
	}
}

// clusterBuildAttachEnvFile renders the shell-source env file for an attached
// cluster. Argo CD is not exposed on a host port for these, so only KUBECONFIG
// and the cub context are set; `cub cluster open` reads the admin password from
// the cluster when the file has none.
func clusterBuildAttachEnvFile(name, kubeconfigPath string) string {
	return fmt.Sprintf(`# cub cluster: %s (attached)
# Source this file: source %s
export KUBECONFIG=%s
%s`, name, clusterEnvFilePath(name), kubeconfigPath, clusterEnvFileContextLine())
}

// clusterEnvFileContextLine is the `export CUB_CONTEXT=...` line every cluster
// env file ends with, so sourcing the file points cub at the cluster's
// organization in that shell as well as kubectl at the cluster. Empty when the
// active context has no name, which only a hand-edited config can produce.
func clusterEnvFileContextLine() string {
	if contextManager == nil {
		return ""
	}
	name := contextManager.ActiveContextName()
	if name == "" {
		return ""
	}
	return fmt.Sprintf("export CUB_CONTEXT=%q\n", name)
}

// clusterAttachSpec is what wiring a reachable cluster into ConfigHub needs,
// independent of how the cluster came to exist. `cub cluster attach` fills it
// from the user's kubeconfig; `cub cluster up` fills it from the kind cluster
// it just created, adding the port annotations and the controller's NodePort
// only it knows.
type clusterAttachSpec struct {
	name           string
	spaceSlug      string
	kubeconfigPath string
	origin         string // clusterOriginKind or clusterOriginAttach
	controller     string // clusterControllerArgo, clusterControllerSveltos or clusterControllerFlux

	// spaceAnnotations and targetAnnotations are added to the cluster Space and
	// the OCI target on top of the ones every attachment writes.
	spaceAnnotations  map[string]string
	targetAnnotations map[string]string

	// controllerVersion is the controller release to install when the cluster
	// has none (--controller-version); "" installs the controller's pinned
	// default.
	controllerVersion string
	// argo says how to get Argo CD into the cluster (controller argo).
	argo clusterArgoOptions
	// sveltos says how to get Sveltos into the cluster (controller sveltos).
	sveltos clusterSveltosOptions
	// fluxInstallIfAbsent installs Flux when the cluster has none; false turns
	// its absence into an error (controller flux, --no-install).
	fluxInstallIfAbsent bool

	noArgobot         bool
	noPlaceholderGate bool
	// prune gives the root Argo CD Application or Flux Kustomization prune and
	// records it on the OCI target for the deployments `cub variant create`
	// adds (controllers argo and flux).
	prune bool
}

// clusterAttachResult is what clusterAttach leaves behind, for the summary.
type clusterAttachResult struct {
	spec               clusterAttachSpec
	appsSlug           string
	serviceAccountSlug string
	argo               clusterArgoResult
	// controllerInstalled is true when this attachment installed Sveltos or
	// Flux, false when it was already present and left alone. For Argo CD see
	// argo.installed.
	controllerInstalled bool
}

// clusterAttach wires a cluster into ConfigHub: the controller in the cluster,
// the cluster and apps Spaces with their service account, gate, target and root
// Unit, the registry credentials, argobot, the first apps-Space Release, and
// the one kubectl apply that bootstraps the root. Every ConfigHub entity it
// creates is registered with addRollback; the caller runs those (in reverse)
// if anything after this fails, and clusterAttach returns an error before
// completing.
func clusterAttach(out io.Writer, spec clusterAttachSpec, addRollback func(func())) (clusterAttachResult, error) {
	var result clusterAttachResult
	sveltos := spec.controller == clusterControllerSveltos
	flux := spec.controller == clusterControllerFlux
	appsSlug := spec.spaceSlug + clusterAppsSuffix(spec.controller)
	kubeconfigPath := spec.kubeconfigPath

	var argo clusterArgoResult
	var controllerInstalled bool
	switch spec.controller {
	case clusterControllerSveltos:
		fmt.Fprintln(out, "Ensuring Sveltos...")
		var err error
		controllerInstalled, err = clusterSveltosEnsure(ctx, kubeconfigPath, spec.controllerVersion, spec.sveltos, out)
		if err != nil {
			return result, err
		}
	case clusterControllerFlux:
		fmt.Fprintln(out, "Ensuring Flux...")
		var err error
		controllerInstalled, err = clusterFluxEnsure(ctx, kubeconfigPath, spec.controllerVersion, spec.fluxInstallIfAbsent, out)
		if err != nil {
			return result, err
		}
	default:
		fmt.Fprintln(out, "Ensuring Argo CD...")
		var err error
		argo, err = clusterArgoEnsure(ctx, kubeconfigPath, spec.controllerVersion, spec.argo, out)
		if err != nil {
			return result, err
		}
	}

	// The cluster (target-prefix) Space holds the target and channel. It carries
	// the cluster label + confighub.com/cluster-* annotations so `cub cluster
	// list`/`down` find the cluster from it; the apps Space below is derived and
	// left unlabeled so the cluster lists once. The bot's Space and Channel are
	// recorded here too, so `down` reads them rather than deriving them.
	fmt.Fprintf(out, "Creating ConfigHub space %q (target + argobot channel)...\n", spec.spaceSlug)
	hostname, _ := os.Hostname()
	annotations := map[string]string{
		clusterAnnotationName:       spec.name,
		clusterAnnotationHost:       hostname,
		clusterAnnotationOrigin:     spec.origin,
		clusterAnnotationController: spec.controller,
	}
	if !spec.noArgobot {
		annotations[clusterAnnotationBotSpace] = clusterArgobotComponent + "-" + spec.name
		annotations[clusterAnnotationBotChannel] = clusterArgobotChannelSlug
	}
	for k, v := range spec.spaceAnnotations {
		annotations[k] = v
	}
	spaceID, err := clusterCreateSpace(spec.spaceSlug, spec.spaceSlug,
		map[string]string{clusterLabel: "true"}, annotations)
	if err != nil {
		return result, err
	}
	addRollback(func() {
		fmt.Fprintf(out, "Rolling back: cub space delete --recursive %q\n", spec.spaceSlug)
		// This Space has no release target of its own; the apps Space's rollback
		// (registered later, so it runs first) clears its cross-Space reference
		// and deletes the Units bound to this target before we delete it here.
		_ = clusterDeleteSpace(spec.spaceSlug, true)
	})

	// The cluster's identity on the ConfigHub side: one service account, used
	// two ways. The controller pulls the cluster's Releases from the OCI
	// registry with a long-lived token of it, and argobot authenticates as it
	// with a key. It has no organization role; the Target and Channel below
	// grant its user exactly the cluster's deployment path. Org-scoped, so it
	// is rolled back on its own rather than with a Space.
	serviceAccountSlug := clusterServiceAccountSlug(spec.name)
	fmt.Fprintf(out, "Creating service account %q (OrgRole=none; the controller pulls and argobot runs as it)...\n", serviceAccountSlug)
	serviceAccountID, serviceAccountUserID, err := clusterCreateServiceAccount(serviceAccountSlug, serviceAccountSlug,
		map[string]string{clusterAnnotationName: spec.name})
	if err != nil {
		return result, err
	}
	addRollback(func() {
		fmt.Fprintf(out, "Rolling back: cub serviceaccount delete %q\n", serviceAccountSlug)
		_ = clusterDeleteServiceAccount(serviceAccountSlug)
	})
	fmt.Fprintf(out, "Issuing a key for argobot and a pull token on %q...\n", serviceAccountSlug)
	privateKey, err := clusterCreateServiceAccountKey(serviceAccountID, serviceAccountUserID, "argobot in cluster "+spec.name)
	if err != nil {
		return result, fmt.Errorf("issue the service account key: %w", err)
	}
	pullToken, err := clusterCreateServiceAccountToken(serviceAccountID, "cluster "+spec.name+" (OCI pull)")
	if err != nil {
		return result, fmt.Errorf("issue the service account token: %w", err)
	}

	// The default gate, created BEFORE the target: the server resolves a
	// Target's WhereTrigger into its read-only TriggerIDs at create time, so a
	// Trigger that does not exist yet would not be selected. --no-placeholder-gate
	// skips the Trigger but not the wiring below — the cluster Space stays the
	// place to put gates, it just starts out empty.
	if !spec.noPlaceholderGate {
		fmt.Fprintf(out, "Creating placeholder gate trigger %q (vet-placeholders on every Mutation)...\n", clusterGateTriggerSlug)
		if _, err := clusterCreateGateTrigger(spaceID, clusterGateTriggerSlug); err != nil {
			return result, fmt.Errorf("create placeholder gate trigger: %w", err)
		}
	}

	fmt.Fprintf(out, "Creating OCI target %q (granting %q its deployment path)...\n", clusterTargetSlug, serviceAccountSlug)
	// apps-space lets API clients resolve the apps Space, which holds the
	// cluster's controller Units, from the target. The controller annotation
	// says which kind of Unit `cub variant create --target` adds there: an Argo
	// Application, a Sveltos ClusterProfile or a Flux Kustomization. The argobot
	// annotation tells `cub variant create` whether a deploy is picked up at
	// once or waits for a poll.
	targetAnnotations := map[string]string{
		clusterAnnotationController: spec.controller,
		clusterAnnotationAppsSpace:  appsSlug,
	}
	if !spec.noArgobot {
		targetAnnotations[clusterAnnotationArgobot] = "true"
	}
	if spec.prune {
		targetAnnotations[clusterAnnotationPrune] = "true"
	}
	switch spec.controller {
	case clusterControllerSveltos:
		targetAnnotations[clusterAnnotationSveltosPollInterval] = clusterSveltosPollInterval(!spec.noArgobot)
	case clusterControllerFlux:
		targetAnnotations[clusterAnnotationFluxPollInterval] = clusterFluxPollInterval(!spec.noArgobot)
	}
	for k, v := range spec.targetAnnotations {
		targetAnnotations[k] = v
	}
	// Selecting the cluster Space's Triggers makes them gate every Unit bound to
	// this target, wherever that Unit lives — the apps Space, the argobot
	// variant, and every deployment variant, including ones created later.
	targetID, err := clusterCreateOCITarget(spaceID, serviceAccountUserID, clusterTargetSlug, clusterTargetSlug,
		targetAnnotations, clusterWhereTriggerForSpace(spaceID))
	if err != nil {
		return result, err
	}

	// The apps Space holds the root app-of-apps and every child Argo Application
	// Unit, or with Sveltos the root ClusterProfile and every child
	// ClusterProfile Unit, or with Flux the root and every child OCIRepository
	// and Kustomization. Its release target is the OCI target in the cluster
	// Space above — a cross-Space reference, so the apps Space (the config
	// bundle) never points its release target at a target inside itself.
	fmt.Fprintf(out, "Creating ConfigHub space %q (%s)...\n", appsSlug, clusterAppsKind(spec.controller))
	appsSpaceID, err := clusterCreateSpace(appsSlug, appsSlug, nil, nil)
	if err != nil {
		return result, err
	}
	addRollback(func() {
		fmt.Fprintf(out, "Rolling back: cub space delete --recursive %q\n", appsSlug)
		// Clear the cross-Space release-target reference first (ON DELETE
		// RESTRICT blocks deleting the OCI target it points at, which lives in
		// the cluster Space).
		_ = clusterClearReleaseTarget(appsSpaceID)
		_ = clusterDeleteSpace(appsSlug, true)
	})

	fmt.Fprintf(out, "Setting the apps space's release target to %q...\n", spec.spaceSlug+"/"+clusterTargetSlug)
	if err := clusterSetReleaseTarget(appsSpaceID, targetID); err != nil {
		return result, err
	}

	// Where the server says its registry is. Asked once and used for both the
	// endpoint and its transport below, so the two cannot disagree.
	ociServerURL, err := clusterOCIServerURL()
	if err != nil {
		return result, err
	}
	// OCI URL for the controller (running inside the cluster) — the advertised
	// registry, rewritten to the docker host address for loopback cub servers,
	// which only a cluster on this machine can reach anyway. A remote server
	// passes through untouched.
	ociURLForArgo, err := clusterOCIEndpointFromContainer(ociServerURL, clusterServerURL())
	if err != nil {
		return result, err
	}
	// OCI URL recorded inside the root's source — the controller pulls from
	// this, so it must be the in-cluster-reachable form. The root references
	// the apps Space's Release, served at /space/<apps-slug>, tag "latest".
	rootRepoURL := strings.TrimRight(ociURLForArgo, "/") + "/space/" + appsSlug

	// Plain-HTTP OCI from the advertised registry's scheme, or failing that from
	// the cub server URL's: http:// API → http:// OCI (the local-dev case),
	// https:// API → TLS OCI.
	ociInsecure, err := clusterOCIInsecure(ociServerURL, clusterServerURL())
	if err != nil {
		return result, err
	}

	// rootName is the root's name in the controller, which its URL-TargetUI
	// links to. Flux has no UI, so it needs none.
	var rootManifest []byte
	var rootName string
	switch {
	case sveltos:
		if err := clusterSveltosApplyOCICreds(ctx, kubeconfigPath, pullToken, out); err != nil {
			return result, err
		}
		fmt.Fprintf(out, "Creating root ClusterProfile Unit %q in apps space %q...\n", clusterRootUnitSlug, appsSlug)
		rootManifest = clusterSveltosRootProfileManifest(rootRepoURL, clusterSveltosPollInterval(!spec.noArgobot), ociInsecure)
		rootName = clusterSveltosRootProfile
	case flux:
		if err := clusterFluxApplyOCICreds(ctx, kubeconfigPath, ociURLForArgo, pullToken, out); err != nil {
			return result, err
		}
		fmt.Fprintf(out, "Creating root OCIRepository and Kustomization Unit %q in apps space %q...\n", clusterRootUnitSlug, appsSlug)
		rootManifest = clusterFluxRootManifest(rootRepoURL, clusterFluxPollInterval(!spec.noArgobot), ociInsecure, spec.prune)
	default:
		if err := clusterArgoApplyOCIRepoCreds(ctx, kubeconfigPath, ociURLForArgo, pullToken, ociInsecure, out); err != nil {
			return result, err
		}
		fmt.Fprintf(out, "Creating root Application Unit %q in apps space %q...\n", clusterRootUnitSlug, appsSlug)
		rootManifest = clusterArgoRootAppManifest(appsSlug, rootRepoURL, spec.prune)
		rootName = appsSlug
	}
	var rootAnnotations map[string]string
	if rootName != "" {
		rootAnnotations = clusterUnitTargetUIAnnotations(targetAnnotations, rootName)
	}
	rootUnitID, err := clusterCreateK8sYAMLUnit(appsSpaceID, targetID, clusterRootUnitSlug, clusterRootUnitSlug, rootAnnotations, rootManifest)
	if err != nil {
		return result, err
	}
	// The root deploys the apps Space's own Release.
	if err := clusterSetClusterUnitID(appsSpaceID, rootUnitID); err != nil {
		return result, err
	}

	// Install argobot (unless --no-k8sbot) BEFORE the final apps-Space publish
	// and root bootstrap, so its child Application Unit is already in the first
	// bundle the root pulls — the root app-of-apps then creates it on its
	// initial sync rather than only on a later reconcile. The step creates that
	// child Unit via `cub variant create --target` (auto-derived from the OCI
	// target's apps-space annotation, the same path any deployment uses) and
	// publishes argobot's own variant Release; the child Unit is left in the
	// apps Space for the single authoritative publish below. All argobot
	// specifics live in cluster_argobot.go.
	if err := clusterInstallArgobotStep(out, spec, clusterArgobotDeps{
		clusterSpaceID:       spaceID,
		targetID:             targetID,
		serviceAccountUserID: serviceAccountUserID,
		privateKey:           privateKey,
		kubeconfigPath:       kubeconfigPath,
	}, addRollback); err != nil {
		return result, fmt.Errorf("install argobot: %w", err)
	}

	// Publish the apps Space's Release once, after root's triggers settle, so
	// the bundle carries root's post-trigger revision plus any Application Units
	// argobot's install added to the Space (already trigger-settled by variant
	// create). This is the bundle the root pulls on its first sync.
	fmt.Fprintln(out, "\nPublishing the apps space's release (populates the OCI bundle the root pulls)...")
	if err := clusterWaitUnitTriggers(appsSpaceID, rootUnitID); err != nil {
		return result, err
	}
	if err := clusterPublishRelease(appsSpaceID); err != nil {
		return result, err
	}

	fmt.Fprintln(out, "Bootstrapping the root via kubectl (the only kubectl-apply moment)...")
	if err := clusterKubectlApply(ctx, kubeconfigPath, rootManifest, out); err != nil {
		return result, err
	}

	return clusterAttachResult{spec: spec, appsSlug: appsSlug, serviceAccountSlug: serviceAccountSlug, argo: argo, controllerInstalled: controllerInstalled}, nil
}

// clusterPrintSummaryHead prints the "Done" block every attachment ends with:
// what was created, on both sides. verb is "Attached" or "Done".
func clusterPrintSummaryHead(out io.Writer, verb string, r clusterAttachResult, kubeconfigPath, envFilePath string) {
	spec := r.spec
	fmt.Fprintf(out, "\n%s.\n  cluster:    %s\n  context:    %s\n  kubeconfig: %s\n  env file:   %s\n  space:      %s\n  apps space: %s\n  target:     %s/%s\n  identity:   service account %s (the controller pulls with a token of it; argobot runs as it with a key)\n  root:       %s/%s\n",
		verb,
		spec.name, contextManager.ActiveContextName(), kubeconfigPath, envFilePath,
		spec.spaceSlug,
		r.appsSlug,
		spec.spaceSlug, clusterTargetSlug,
		r.serviceAccountSlug,
		r.appsSlug, clusterRootUnitSlug)
	if !spec.noArgobot {
		switch spec.controller {
		case clusterControllerSveltos:
			fmt.Fprintf(out, "  argobot:    %s-%s (Sveltos ClusterProfile; redeploys on publish), channel %s/%s\n", clusterArgobotComponent, spec.name, spec.spaceSlug, clusterArgobotChannelSlug)
		case clusterControllerFlux:
			fmt.Fprintf(out, "  argobot:    %s-%s (Flux Kustomization; reconciles on publish), channel %s/%s\n", clusterArgobotComponent, spec.name, spec.spaceSlug, clusterArgobotChannelSlug)
		default:
			fmt.Fprintf(out, "  argobot:    %s-%s (Argo app; refreshes on publish), channel %s/%s\n", clusterArgobotComponent, spec.name, spec.spaceSlug, clusterArgobotChannelSlug)
		}
	}
	if spec.noPlaceholderGate {
		fmt.Fprintf(out, "  gates:      none (--no-placeholder-gate); Triggers added to space %q gate the\n              target once you run 'cub target update --space %s target --refresh-triggers'\n",
			spec.spaceSlug, spec.spaceSlug)
	} else {
		fmt.Fprintf(out, "  gates:      %s/%s (vet-placeholders) — blocks publishing a Release with\n              unfilled confighubplaceholder values to this cluster\n",
			spec.spaceSlug, clusterGateTriggerSlug)
	}
}

// clusterPrintSummaryTail prints how to use the cluster from here.
func clusterPrintSummaryTail(out io.Writer, r clusterAttachResult, envFilePath string) {
	fmt.Fprintf(out, "\nLoad the cluster and its cub context into your shell:\n  source %s\n", envFilePath)
	if r.spec.controller == clusterControllerFlux {
		fmt.Fprintf(out, "\nDeploy an app to this cluster:\n  1. cub variant create <name> <base-space> --target %s/%s [--namespace <ns>]\n  2. cub release publish <name>\nStep 1 clones <base-space> into a deployment space bound to the cluster's\ntarget and adds its Flux OCIRepository and Kustomization to the apps space\n%q; step 2 makes the deployment's config go live. ",
			r.spec.spaceSlug, clusterTargetSlug, r.appsSlug)
		if r.spec.noArgobot {
			fmt.Fprintf(out, "Without argobot, step 2\nalso republishes the apps space the first time, so the root applies them.\n")
		} else {
			fmt.Fprintf(out, "Step 1 republishes\nthe apps space, and argobot deploys step 2's Release at once.\n")
		}
		fmt.Fprintf(out, "See \"cub variant create --help\".\n")
		return
	}
	if r.spec.controller == clusterControllerSveltos {
		fmt.Fprintf(out, "\nDeploy an app to this cluster:\n  1. cub variant create <name> <base-space> --target %s/%s [--namespace <ns>]\n  2. cub release publish <name>\nStep 1 clones <base-space> into a deployment space bound to the cluster's\ntarget and adds its Sveltos ClusterProfile to the apps space %q; step 2\nmakes the deployment's config go live and, the first time, republishes the\napps space so the root profile applies the ClusterProfile. See\n\"cub variant create --help\".\n",
			r.spec.spaceSlug, clusterTargetSlug, r.appsSlug)
		return
	}
	fmt.Fprintf(out, "\nDeploy an app to this cluster:\n  1. cub variant create <name> <base-space> --target %s/%s [--namespace <ns>]\n  2. cub release publish <name>\nStep 1 clones <base-space> into a deployment space bound to the cluster's\ntarget and auto-creates its Argo CD Application in the apps space %q\n(republishing it so Argo picks it up); step 2 makes the deployment's config\ngo live. See \"cub variant create --help\".\n",
		r.spec.spaceSlug, clusterTargetSlug, r.appsSlug)
}
