// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"maps"
	"net/url"
	"os"
	"os/exec"
	"strconv"
	"strings"

	"github.com/google/uuid"
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
	argobotBase       string
	registry          string
	k8sbotInstaller   bool
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

Its first part — the controller in the cluster, the cluster Space and its OCI
target — is "cub cluster controller-install", and its second — the cluster's
service account, with its key and pull token in the cluster — is "cub cluster
svc-account-bootstrap". Either can be run on its own first, in that order.
Its third — the apps Space and the root — is "cub cluster app-bootstrap",
which finishes the attachment without argobot, and its last, argobot, is "cub
cluster k8sbot-install", which installs it in a cluster attached with
--no-k8sbot.
Attach then picks the cluster up where they left it: it keeps the
credentials, controller, Space, target and service account, takes the
controller and Space from them, and ignores the flags of those parts
(--provider and the credential flags, --controller-version, --no-install and
--no-placeholder-gate).

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
argocd-initial-admin-secret. The exception is a kind cluster that "cub cluster
create" made: its node carries the host port window create mapped, and when
attach installs Argo CD or Sveltos it puts the UI on the window's first port,
records the window on the cluster Space, and "cub cluster open" opens the UI
there, as for "cub cluster up".

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
	clusterAttachCmd.Flags().StringVar(&clusterAttachArgs.controller, "controller", "", "controller to wire to ConfigHub: argo (Argo CD), sveltos (Sveltos) or flux (Flux) (default argo, or the one 'cub cluster controller-install' installed)")
	clusterAttachCmd.Flags().StringVar(&clusterAttachArgs.controllerVersion, "controller-version", "", "controller release to install when absent (default: argo "+clusterArgoDefaultVersion+", sveltos "+clusterSveltosVersion+", flux "+clusterFluxVersion+"; Argo CD must be v3.1 or later, which oci:// sources need)")
	clusterAttachCmd.Flags().BoolVar(&clusterAttachArgs.noInstall, "no-install", false, "never install the controller; fail if the cluster has none")
	clusterAttachCmd.Flags().BoolVar(&clusterAttachArgs.noArgoCD, "no-argocd", false, "--controller argo: the same as --no-install")
	_ = clusterAttachCmd.Flags().MarkHidden("no-argocd")
	clusterAttachCmd.Flags().BoolVar(&clusterAttachArgs.noArgobot, "no-k8sbot", false, "skip installing argobot (the event-driven Argo CD sync, Sveltos redeploy, or Flux reconciliation, bot); 'cub cluster k8sbot-install' installs it later")
	clusterAttachCmd.Flags().StringVar(&clusterAttachArgs.argobotBase, "k8sbot-base", "", clusterK8sbotBaseFlagUsage)
	clusterAttachCmd.Flags().StringVar(&clusterAttachArgs.registry, "registry", "", clusterRegistryFlagUsage)
	clusterAttachCmd.Flags().BoolVar(&clusterAttachArgs.k8sbotInstaller, "k8sbot-installer", false, clusterK8sbotInstallerFlagUsage)
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
	argobotBase       string
	registry          string
	k8sbotInstaller   bool
	noPlaceholderGate bool
	prune             bool
	// origin is clusterOriginAttach, or clusterOriginKind when `cub cluster up`
	// attaches the kind cluster it just created, which makes it a cluster `cub
	// cluster down` deletes rather than one `cub cluster detach` leaves running.
	origin string
}

func clusterAttachCmdRun(cmd *cobra.Command, args []string) error {
	name, err := clusterNameArg(args, clusterAttachArgs.name, "attach", true)
	if err != nil {
		return err
	}
	// --no-argocd is --no-install from when Argo CD was the only controller.
	if clusterAttachArgs.noArgoCD && clusterAttachArgs.controller != "" && clusterAttachArgs.controller != clusterControllerArgo {
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
		argobotBase:       clusterAttachArgs.argobotBase,
		registry:          clusterAttachArgs.registry,
		k8sbotInstaller:   clusterAttachArgs.k8sbotInstaller,
		noPlaceholderGate: clusterAttachArgs.noPlaceholderGate,
		prune:             clusterAttachArgs.prune,
	})
}

func clusterAttachRun(out io.Writer, opts clusterAttachOptions) error {
	if opts.name == "" {
		return fmt.Errorf("a cluster name is required: cub cluster attach <name>")
	}
	// An empty --controller is Argo CD, or for a cluster `cub cluster
	// controller-install` set up, the controller it installed.
	if opts.controller != "" {
		if err := clusterCheckController(opts.controller); err != nil {
			return err
		}
	}
	if _, err := exec.LookPath("kubectl"); err != nil {
		return fmt.Errorf("kubectl not found on PATH")
	}
	if opts.origin == "" {
		opts.origin = clusterOriginAttach
	}

	// A cluster `cub cluster controller-install` set up is picked up where it
	// left off: its credentials, controller, cluster Space and target are kept.
	installedSpace, err := clusterFindControllerInstalled(out, opts.name)
	if err != nil {
		return err
	}
	if installedSpace != nil {
		if opts.controller != "" && opts.controller != installedSpace.Controller {
			return fmt.Errorf("--controller %s: cluster %q has %s, which 'cub cluster controller-install' installed", opts.controller, opts.name, installedSpace.Controller)
		}
		if opts.spaceSlug != "" && opts.spaceSlug != installedSpace.SpaceSlug {
			return fmt.Errorf("--space %s: cluster %q is in space %q, which 'cub cluster controller-install' created", opts.spaceSlug, opts.name, installedSpace.SpaceSlug)
		}
		opts.controller = installedSpace.Controller
		opts.spaceSlug = installedSpace.SpaceSlug
		opts.origin = installedSpace.Origin
		leftBy := "controller-install"
		if installedSpace.Stage == clusterStageServiceAccount {
			leftBy = "svc-account-bootstrap"
		}
		fmt.Fprintf(out, "Picking up cluster %q where 'cub cluster %s' left it (space %q)...\n", opts.name, leftBy, opts.spaceSlug)
	} else {
		if opts.controller == "" {
			opts.controller = clusterControllerArgo
		}
		if err := clusterAttachCheckProviderFlags(&opts); err != nil {
			return err
		}
		if opts.spaceSlug == "" {
			opts.spaceSlug = opts.name
		}
	}
	appsSlug := opts.spaceSlug + clusterAppsSuffix(opts.controller)
	kubeconfigPath := clusterKubeconfigPath(opts.name)
	envFilePath := clusterEnvFilePath(opts.name)

	argobotSlug := ""
	if !opts.noArgobot {
		argobotSlug = clusterArgobotComponent + "-" + opts.name
	}
	// What an earlier stage created is expected to exist; the rest is not.
	freshSpaceSlug, freshServiceAccount := opts.spaceSlug, true
	if installedSpace != nil {
		freshSpaceSlug = ""
		freshServiceAccount = installedSpace.Stage != clusterStageServiceAccount
	}
	if err := clusterRequireFreshConfigHub(opts.name, freshSpaceSlug, appsSlug, argobotSlug, freshServiceAccount); err != nil {
		return err
	}
	var argobotRepository string
	var argobotInstaller *PluginCommand
	if !opts.noArgobot {
		if argobotRepository, err = clusterArgobotRepository(opts.registry); err != nil {
			return err
		}
		if opts.k8sbotInstaller {
			if argobotInstaller, err = clusterFindInstaller(); err != nil {
				return err
			}
		}
		if err := clusterArgobotCheckBaseKind(opts.controller, argobotInstaller != nil); err != nil {
			return err
		}
		mode, err := clusterChooseArgobotBase(out, opts.controller, opts.argobotBase)
		if err != nil {
			return err
		}
		opts.argobotBase = mode
	}

	var rollback []func()
	commit := false
	defer func() {
		if commit {
			return
		}
		for i := len(rollback) - 1; i >= 0; i-- {
			rollback[i]()
		}
	}()
	addRollback := func(f func()) { rollback = append(rollback, f) }

	var inst clusterInstalled
	if installedSpace != nil {
		inst, err = clusterLoadInstalled(installedSpace, kubeconfigPath)
	} else {
		inst, err = clusterControllerInstallStage(out, &opts, addRollback)
		// Recorded now, as `cub cluster controller-install` does, so a run
		// stopped from here on is one the next attach picks up by the stage
		// marker; the stage's rollback removes the record with the other
		// local files.
		if err == nil {
			err = clusterWriteRecord(clusterNewRecord(opts.name, opts.spaceSlug, opts.origin, opts.controller))
		}
	}
	if err != nil {
		return err
	}

	if freshServiceAccount {
		if err := clusterBootstrapServiceAccount(out, clusterServiceAccountSpec{
			name:           opts.name,
			spaceSlug:      opts.spaceSlug,
			kubeconfigPath: kubeconfigPath,
			controller:     opts.controller,
			noArgobot:      opts.noArgobot,
		}, inst, addRollback); err != nil {
			return err
		}
	}

	spec := clusterAttachSpec{
		clusterControllerSpec: clusterControllerSpec{
			name:              opts.name,
			spaceSlug:         opts.spaceSlug,
			kubeconfigPath:    kubeconfigPath,
			origin:            opts.origin,
			controller:        opts.controller,
			noPlaceholderGate: inst.noPlaceholderGate,
		},
		noArgobot:   opts.noArgobot,
		argobotBase: opts.argobotBase,
		prune:       opts.prune,
	}
	// argobot goes in before the first apps-Space Release, so the root's first
	// sync already deploys it.
	var installBot func() error
	if !opts.noArgobot {
		installBot = func() error {
			return clusterInstallK8sbot(out, clusterK8sbotSpec{
				name:              opts.name,
				spaceSlug:         opts.spaceSlug,
				kubeconfigPath:    kubeconfigPath,
				controller:        opts.controller,
				spaceID:           inst.spaceID,
				targetID:          inst.targetID,
				argobotBase:       opts.argobotBase,
				argobotRepository: argobotRepository,
				argobotInstaller:  argobotInstaller,
			}, addRollback)
		}
	}
	result, err := clusterBootstrapApps(out, spec, inst, installBot, addRollback)
	if err != nil {
		return err
	}

	if err := clusterWriteRecord(clusterNewRecord(opts.name, opts.spaceSlug, opts.origin, opts.controller)); err != nil {
		return fmt.Errorf("write cluster record: %w", err)
	}
	if err := clusterWriteEnvFile(opts.name, opts.origin, opts.controller, inst); err != nil {
		return err
	}

	commit = true
	verb := "Attached"
	if opts.origin == clusterOriginKind {
		verb = "Done"
	}
	clusterPrintSummaryHead(out, verb, result, kubeconfigPath, envFilePath)
	clusterPrintControllerSummary(out, opts.name, opts.controller, kubeconfigPath, inst)
	clusterPrintSummaryTail(out, result, envFilePath)
	return nil
}

// clusterCheckController validates --controller.
func clusterCheckController(controller string) error {
	switch controller {
	case clusterControllerArgo, clusterControllerSveltos, clusterControllerFlux:
		return nil
	}
	return fmt.Errorf("--controller %q: must be %s, %s or %s", controller,
		clusterControllerArgo, clusterControllerSveltos, clusterControllerFlux)
}

// clusterFindControllerInstalled returns the cluster Space of a cluster `cub
// cluster controller-install` set up, and perhaps `cub cluster
// svc-account-bootstrap` after it, and nothing has attached yet, switching
// to the context its record names; nil when cub has never managed a cluster of
// that name on this machine. Any other cub-managed cluster is an error.
func clusterFindControllerInstalled(out io.Writer, name string) (*clusterSpace, error) {
	kubeconfigPath := clusterKubeconfigPath(name)
	rec, err := clusterReadRecord(name)
	if err != nil {
		return nil, err
	}
	if rec == nil {
		if _, err := os.Stat(kubeconfigPath); err == nil {
			return nil, fmt.Errorf("kubeconfig %s already exists; cluster %q is already cub-managed (see 'cub cluster list')", kubeconfigPath, name)
		}
		return nil, nil
	}
	alreadyManaged := fmt.Errorf("cluster %q is already cub-managed on this machine, wired to %s (see 'cub cluster list'); use another name, or detach it first", name, clusterRecordWhere(rec))
	if err := clusterUseRecordedContext(out, rec); err != nil {
		return nil, err
	}
	space, err := clusterSpaceByNameInOrg(name)
	if err != nil {
		return nil, fmt.Errorf("look up cluster spaces: %w", err)
	}
	if space == nil || (space.Stage != clusterStageController && space.Stage != clusterStageServiceAccount) {
		return nil, alreadyManaged
	}
	if _, err := os.Stat(kubeconfigPath); err != nil {
		return nil, fmt.Errorf("cluster %q: 'cub cluster controller-install' left no kubeconfig at %s; 'cub cluster detach %s --delete-config' and run it again", name, kubeconfigPath, name)
	}
	return space, nil
}

// clusterLoadInstalled reads back what `cub cluster controller-install` left
// for a cluster: its OCI target, whether it has a placeholder gate, and where
// its controller's UI and port window are.
func clusterLoadInstalled(space *clusterSpace, kubeconfigPath string) (clusterInstalled, error) {
	inst := clusterInstalled{spaceID: space.SpaceID, pickedUp: true}
	target, err := resolveTargetCore(clusterTargetSlug, space.SpaceID.String(), "*")
	if err != nil {
		return inst, fmt.Errorf("cluster space %q: find its OCI target %q: %w", space.SpaceSlug, clusterTargetSlug, err)
	}
	inst.targetID = target.TargetID
	inst.noPlaceholderGate = len(target.TriggerIDs) == 0
	uiPort := space.ArgoPort
	if space.Controller == clusterControllerSveltos {
		uiPort = space.SveltosPort
	}
	if uiPort != "" {
		if inst.uiPort, err = strconv.Atoi(uiPort); err != nil {
			return inst, fmt.Errorf("cluster space %q: UI port %q: %w", space.SpaceSlug, uiPort, err)
		}
	}
	if space.PortRange != "" {
		if inst.portStart, inst.portWindow, err = clusterParsePortRange(space.PortRange); err != nil {
			return inst, fmt.Errorf("cluster space %q: %w", space.SpaceSlug, err)
		}
	}
	if space.Controller == clusterControllerArgo {
		// Best effort: the initial Secret may legitimately be gone.
		inst.argo.adminPassword, _ = clusterArgoAdminPassword(ctx, kubeconfigPath)
	}
	return inst, nil
}

// clusterControllerInstallStage takes a self-contained copy of the cluster's
// credentials, checks the cluster can be wired, and runs
// clusterInstallController on it. It registers the removal of the local files
// and every ConfigHub entity it creates with addRollback. It sets opts.origin
// to clusterOriginKind for a kind cluster `cub cluster create` made, which the
// caller records.
func clusterControllerInstallStage(out io.Writer, opts *clusterAttachOptions, addRollback func(func())) (clusterInstalled, error) {
	var inst clusterInstalled
	eks := opts.provider == clusterProviderEKS
	kubeconfigPath := clusterKubeconfigPath(opts.name)
	envFilePath := clusterEnvFilePath(opts.name)

	// A self-contained copy of the credentials, at the path every other
	// `cub cluster` command looks for. Taken before anything is created, so a
	// kubeconfig that does not name a usable context fails here, with nothing
	// to roll back.
	if err := clusterEnsureKubeconfigDir(); err != nil {
		return inst, err
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
		return inst, err
	}
	if problem := clusterAttachReachabilityProblem(clusterServerURL(), clusterKubeconfigServer(kubeconfig)); problem != "" {
		_ = os.Remove(kubeconfigPath)
		return inst, fmt.Errorf("%s", problem)
	}
	if err := os.WriteFile(kubeconfigPath, kubeconfig, 0o600); err != nil {
		return inst, fmt.Errorf("write kubeconfig: %w", err)
	}
	addRollback(func() {
		fmt.Fprintf(out, "Rolling back: removing %s\n", kubeconfigPath)
		_ = os.Remove(kubeconfigPath)
		_ = os.Remove(envFilePath)
		_ = clusterRemoveRecord(opts.name)
	})

	// Installing the controller and applying the root need cluster-admin.
	// Fail here rather than halfway through a 20k-line manifest.
	fmt.Fprintln(out, "Checking cluster access (needs cluster-admin)...")
	if err := clusterRequireClusterAdmin(ctx, kubeconfigPath); err != nil {
		if !eks {
			return inst, err
		}
		// An EKS cluster that rejects its own kubeconfig is almost always the
		// access-entry cliff; say so, or fix it.
		if err := clusterEKSEnsureAccess(ctx, kubeconfigPath, opts.awsProfile, opts.awsRegion, opts.cluster, opts.grantAccess, out); err != nil {
			return inst, err
		}
	}
	// One ConfigHub per cluster. The registry credentials Secret is the
	// in-cluster mark of an attachment, whichever server it was to.
	if err := clusterAttachRequireUnwired(ctx, kubeconfigPath, opts.controller); err != nil {
		return inst, err
	}

	spaceAnnotations := map[string]string{clusterAnnotationProvider: opts.provider}
	if opts.provider != clusterProviderKubeconfig {
		spaceAnnotations[clusterAnnotationProviderCluster] = opts.cluster
	}
	if eks && opts.awsRegion != "" {
		spaceAnnotations[clusterAnnotationEKSRegion] = opts.awsRegion
	}
	// A kind cluster `cub cluster create` made is cub's, as one `cub cluster
	// up` made is: `cub cluster down` deletes it. It has a host port window
	// mapped into its node, and the first port of it takes the controller's
	// UI, as `cub cluster up` does. Flux has no UI, so the window stays the
	// user's.
	var portStart, portWindow, uiPort int
	if opts.provider == clusterProviderKind {
		labels, err := clusterKindNodeLabels(ctx, kubeconfigPath)
		if err != nil {
			return inst, fmt.Errorf("read the kind cluster's node labels: %w", err)
		}
		if labels[clusterAnnotationOrigin] == clusterOriginKind {
			opts.origin = clusterOriginKind
		}
		if r := labels[clusterAnnotationPortRange]; r != "" {
			if portStart, portWindow, err = clusterParsePortRange(r); err != nil {
				return inst, fmt.Errorf("the kind cluster's port window: %w", err)
			}
		}
		if portWindow > 0 {
			spaceAnnotations[clusterAnnotationPortRange] = fmt.Sprintf("%d-%d", portStart, portStart+portWindow-1)
			if opts.controller != clusterControllerFlux {
				uiPort = portStart
			}
		}
	}
	inst, err = clusterInstallController(out, clusterControllerSpec{
		name:              opts.name,
		spaceSlug:         opts.spaceSlug,
		kubeconfigPath:    kubeconfigPath,
		origin:            opts.origin,
		controller:        opts.controller,
		spaceAnnotations:  spaceAnnotations,
		controllerVersion: opts.controllerVersion,
		installIfAbsent:   !opts.noInstall,
		uiPort:            uiPort,
		noPlaceholderGate: opts.noPlaceholderGate,
	}, addRollback)
	inst.portStart, inst.portWindow = portStart, portWindow
	return inst, err
}

// clusterWriteEnvFile writes the cluster's shell-source env file: the
// controller's UI and credentials when it is on a host port (and for a kind
// cluster `cub cluster up` made, whose Flux has none), otherwise only
// KUBECONFIG and the cub context.
func clusterWriteEnvFile(name, origin, controller string, inst clusterInstalled) error {
	kubeconfigPath := clusterKubeconfigPath(name)
	envContents := clusterBuildAttachEnvFile(name, kubeconfigPath)
	if inst.uiPort > 0 || origin == clusterOriginKind {
		envContents = clusterBuildPortEnvFile(controller, name, kubeconfigPath, inst.uiPort, inst.argo.adminPassword)
	}
	if err := os.WriteFile(clusterEnvFilePath(name), []byte(envContents), 0o600); err != nil {
		return fmt.Errorf("write env file: %w", err)
	}
	return nil
}

// clusterPrintControllerSummary prints where the controller's UI is, or how to
// reach it, and the kind node's port window.
func clusterPrintControllerSummary(out io.Writer, name, controller, kubeconfigPath string, inst clusterInstalled) {
	const leftBy = "as 'cub cluster controller-install' left it"
	switch {
	case inst.uiPort > 0:
		clusterPrintPortUI(out, controller, name, kubeconfigPath, inst.uiPort, inst.argo.adminPassword)
	case inst.pickedUp && controller == clusterControllerSveltos:
		fmt.Fprintf(out, "\nSveltos: %s. Its dashboard is not exposed outside the cluster; reach it with\n  kubectl --kubeconfig %s port-forward svc/dashboard -n %s 8080:80\n",
			leftBy, kubeconfigPath, clusterSveltosNamespace)
	case inst.pickedUp && controller == clusterControllerFlux:
		fmt.Fprintf(out, "\nFlux: %s. It has no UI; see its state with\n  KUBECONFIG=%s %s\n", leftBy, kubeconfigPath, clusterFluxStatusCommand())
	case inst.pickedUp && inst.argo.adminPassword != "":
		fmt.Fprintf(out, "\nArgo CD: %s (initial admin password: %s). Reach the UI with\n  kubectl --kubeconfig %s port-forward svc/argocd-server -n argocd 8080:80\n",
			leftBy, inst.argo.adminPassword, kubeconfigPath)
	case inst.pickedUp:
		fmt.Fprintf(out, "\nArgo CD: %s. Reach the UI with\n  kubectl --kubeconfig %s port-forward svc/argocd-server -n argocd 8080:80\n",
			leftBy, kubeconfigPath)
	case controller == clusterControllerSveltos && inst.controllerInstalled:
		fmt.Fprintf(out, "\nSveltos: installed. Its dashboard is not exposed outside the cluster; reach it with\n  kubectl --kubeconfig %s port-forward svc/dashboard -n %s 8080:80\nand log in with a token from\n  kubectl --kubeconfig %s create token %s --duration=24h\n",
			kubeconfigPath, clusterSveltosNamespace, kubeconfigPath, clusterSveltosDashboardAccount)
	case controller == clusterControllerSveltos:
		fmt.Fprintf(out, "\nSveltos: already installed, left as found\n")
	case controller == clusterControllerFlux && inst.controllerInstalled:
		fmt.Fprintf(out, "\nFlux: installed. It has no UI; see its state with\n  KUBECONFIG=%s %s\n", kubeconfigPath, clusterFluxStatusCommand())
	case controller == clusterControllerFlux:
		fmt.Fprintf(out, "\nFlux: already installed, left as found\n")
	case inst.argo.installed:
		fmt.Fprintf(out, "\nArgo CD: installed (admin / %s). It is not exposed outside the cluster; reach the UI with\n  kubectl --kubeconfig %s port-forward svc/argocd-server -n argocd 8080:80\n",
			inst.argo.adminPassword, kubeconfigPath)
	case inst.argo.adminPassword != "":
		fmt.Fprintf(out, "\nArgo CD: already installed, left as found (initial admin password: %s)\n", inst.argo.adminPassword)
	default:
		fmt.Fprintf(out, "\nArgo CD: already installed, left as found\n")
	}
	switch {
	case inst.uiPort > 0 && inst.portWindow > 1:
		fmt.Fprintf(out, "\nUser NodePort window: %d-%d (%s uses %d; rest are open)\n", inst.portStart, inst.portStart+inst.portWindow-1, controller, inst.uiPort)
	case inst.uiPort == 0 && inst.portWindow > 0:
		fmt.Fprintf(out, "\nUser NodePort window: %d-%d (all open)\n", inst.portStart, inst.portStart+inst.portWindow-1)
	}
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

// clusterControllerSpec is what installing the controller in a reachable
// cluster and representing the cluster in ConfigHub needs: `cub cluster
// controller-install`, and the first part of `cub cluster attach`.
type clusterControllerSpec struct {
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
	// installIfAbsent installs the controller when the cluster has none; false
	// turns its absence into an error (--no-install).
	installIfAbsent bool
	// uiPort is a NodePort the kind node maps to the same host port, for the
	// controller's UI (Argo CD or the Sveltos dashboard); 0 for none. The UI is
	// exposed there only when this installs the controller, since an existing
	// one is left as found.
	uiPort            int
	noPlaceholderGate bool
}

// clusterInstalled is what the controller stage leaves for the rest of an
// attachment: the cluster Space and its OCI target in ConfigHub, and the
// controller in the cluster. It comes from clusterInstallController, or from
// the cluster Space of a cluster `cub cluster controller-install` set up.
type clusterInstalled struct {
	spaceID  uuid.UUID
	targetID uuid.UUID
	argo     clusterArgoResult
	// controllerInstalled is true when this run installed Sveltos or Flux,
	// false when it was already present and left alone. For Argo CD see
	// argo.installed.
	controllerInstalled bool
	// uiPort is the host port the controller's UI is exposed on, 0 when it is
	// not.
	uiPort int
	// portStart and portWindow are the kind node's host port window, a window
	// of 0 for none.
	portStart, portWindow int
	noPlaceholderGate     bool
	// pickedUp is true when the controller stage was `cub cluster
	// controller-install`'s, which this run cannot tell installed the
	// controller or found it there.
	pickedUp bool
}

// clusterAttachSpec is what wiring a reachable cluster into ConfigHub needs,
// independent of how the cluster came to exist: the controller stage, then the
// rest of the attachment.
type clusterAttachSpec struct {
	clusterControllerSpec

	noArgobot bool
	// argobotBase is clusterArgobotBaseUpdate or clusterArgobotBaseClone, as
	// clusterChooseArgobotBase decided it.
	argobotBase string
	// prune gives the root Argo CD Application or Flux Kustomization prune and
	// records it on the OCI target for the deployments `cub variant create`
	// adds (controllers argo and flux).
	prune bool
}

// clusterAttachResult is what clusterAttach leaves behind, for the summary.
type clusterAttachResult struct {
	spec               clusterAttachSpec
	installed          clusterInstalled
	appsSlug           string
	serviceAccountSlug string
}

// clusterInstallController installs the controller in the cluster (unless it
// is there already) and creates the cluster Space, its placeholder gate and
// its OCI target, granting no one access to the target yet. The Space carries
// clusterAnnotationStage until an attachment finishes. Every ConfigHub entity
// it creates is registered with addRollback.
func clusterInstallController(out io.Writer, spec clusterControllerSpec, addRollback func(func())) (clusterInstalled, error) {
	var inst clusterInstalled
	sveltos := spec.controller == clusterControllerSveltos
	kubeconfigPath := spec.kubeconfigPath

	switch spec.controller {
	case clusterControllerSveltos:
		fmt.Fprintln(out, "Ensuring Sveltos...")
		var err error
		inst.controllerInstalled, err = clusterSveltosEnsure(ctx, kubeconfigPath, spec.controllerVersion,
			clusterSveltosOptions{installIfAbsent: spec.installIfAbsent, nodePort: spec.uiPort}, out)
		if err != nil {
			return inst, err
		}
	case clusterControllerFlux:
		fmt.Fprintln(out, "Ensuring Flux...")
		var err error
		inst.controllerInstalled, err = clusterFluxEnsure(ctx, kubeconfigPath, spec.controllerVersion, spec.installIfAbsent, out)
		if err != nil {
			return inst, err
		}
	default:
		fmt.Fprintln(out, "Ensuring Argo CD...")
		var err error
		inst.argo, err = clusterArgoEnsure(ctx, kubeconfigPath, spec.controllerVersion,
			clusterArgoOptions{installIfAbsent: spec.installIfAbsent, nodePort: spec.uiPort}, out)
		if err != nil {
			return inst, err
		}
	}

	// The UI is on the host port only if the install above put it there.
	if spec.uiPort > 0 && (inst.argo.installed || (sveltos && inst.controllerInstalled)) {
		inst.uiPort = spec.uiPort
	}
	uiSpaceAnnotations, uiTargetAnnotations := clusterUIAnnotations(spec.controller, inst.uiPort)

	// The cluster (target-prefix) Space holds the target and channel. It carries
	// the cluster label + confighub.com/cluster-* annotations so `cub cluster
	// list`/`down` find the cluster from it; the apps Space is derived and left
	// unlabeled so the cluster lists once.
	fmt.Fprintf(out, "Creating ConfigHub space %q (target + argobot channel)...\n", spec.spaceSlug)
	hostname, _ := os.Hostname()
	annotations := map[string]string{
		clusterAnnotationName:       spec.name,
		clusterAnnotationHost:       hostname,
		clusterAnnotationOrigin:     spec.origin,
		clusterAnnotationController: spec.controller,
		clusterAnnotationStage:      clusterStageController,
	}
	maps.Copy(annotations, uiSpaceAnnotations)
	maps.Copy(annotations, spec.spaceAnnotations)
	spaceID, err := clusterCreateSpace(spec.spaceSlug, spec.spaceSlug,
		map[string]string{clusterLabel: "true"}, annotations)
	if err != nil {
		return inst, err
	}
	addRollback(func() {
		fmt.Fprintf(out, "Rolling back: cub space delete --recursive %q\n", spec.spaceSlug)
		// This Space has no release target of its own; the apps Space's rollback
		// (registered later, so it runs first) clears its cross-Space reference
		// and deletes the Units bound to this target before we delete it here.
		_ = clusterDeleteSpace(spec.spaceSlug, true)
	})
	inst.spaceID = spaceID

	// The default gate, created BEFORE the target: the server resolves a
	// Target's WhereTrigger into its read-only TriggerIDs at create time, so a
	// Trigger that does not exist yet would not be selected. --no-placeholder-gate
	// skips the Trigger but not the wiring below — the cluster Space stays the
	// place to put gates, it just starts out empty.
	if !spec.noPlaceholderGate {
		fmt.Fprintf(out, "Creating placeholder gate trigger %q (vet-placeholders on every Mutation)...\n", clusterGateTriggerSlug)
		if _, err := clusterCreateGateTrigger(spaceID, clusterGateTriggerSlug); err != nil {
			return inst, fmt.Errorf("create placeholder gate trigger: %w", err)
		}
	}
	inst.noPlaceholderGate = spec.noPlaceholderGate

	fmt.Fprintf(out, "Creating OCI target %q...\n", clusterTargetSlug)
	// apps-space lets API clients resolve the apps Space, which holds the
	// cluster's controller Units, from the target. The controller annotation
	// says which kind of Unit `cub variant create --target` adds there: an Argo
	// Application, a Sveltos ClusterProfile or a Flux Kustomization.
	targetAnnotations := map[string]string{
		clusterAnnotationController: spec.controller,
		clusterAnnotationAppsSpace:  spec.spaceSlug + clusterAppsSuffix(spec.controller),
	}
	maps.Copy(targetAnnotations, uiTargetAnnotations)
	maps.Copy(targetAnnotations, spec.targetAnnotations)
	// Selecting the cluster Space's Triggers makes them gate every Unit bound to
	// this target, wherever that Unit lives — the apps Space, the argobot
	// variant, and every deployment variant, including ones created later.
	inst.targetID, err = clusterCreateOCITarget(spaceID, clusterTargetSlug, clusterTargetSlug,
		targetAnnotations, clusterWhereTriggerForSpace(spaceID))
	if err != nil {
		return inst, err
	}
	return inst, nil
}

// clusterBootstrapApps is the last stage of an attachment, `cub cluster
// app-bootstrap`: the apps Space with its root Unit, the first apps-Space
// Release, and the one kubectl apply that bootstraps the root, after which the
// cluster is attached. The cluster's service account has been set up already,
// with its grants on the OCI target and its credentials in the cluster.
// installBot, when not nil, installs argobot (`cub cluster k8sbot-install`'s
// stage) between creating the root Unit and the first publish, and
// spec.noArgobot says whether it is given. Every ConfigHub entity it creates
// is registered with addRollback; the caller runs those (in reverse) if
// anything after this fails, and clusterBootstrapApps returns an error before
// completing.
func clusterBootstrapApps(out io.Writer, spec clusterAttachSpec, inst clusterInstalled, installBot func() error, addRollback func(func())) (clusterAttachResult, error) {
	var result clusterAttachResult
	sveltos := spec.controller == clusterControllerSveltos
	flux := spec.controller == clusterControllerFlux
	appsSlug := spec.spaceSlug + clusterAppsSuffix(spec.controller)
	kubeconfigPath := spec.kubeconfigPath
	spaceID, targetID := inst.spaceID, inst.targetID
	serviceAccountSlug := clusterServiceAccountSlug(spec.name)

	// The argobot annotation tells `cub variant create` whether a deploy is
	// picked up at once or waits for a poll, and the poll interval is the one
	// the root gets below; prune covers deployments created later.
	attachAnnotations := map[string]string{}
	if !spec.noArgobot {
		attachAnnotations[clusterAnnotationArgobot] = "true"
	}
	if spec.prune {
		attachAnnotations[clusterAnnotationPrune] = "true"
	}
	switch spec.controller {
	case clusterControllerSveltos:
		attachAnnotations[clusterAnnotationSveltosPollInterval] = clusterSveltosPollInterval(!spec.noArgobot)
	case clusterControllerFlux:
		attachAnnotations[clusterAnnotationFluxPollInterval] = clusterFluxPollInterval(!spec.noArgobot)
	}
	targetAnnotations, err := clusterGrantTarget(spaceID, targetID, uuid.Nil, attachAnnotations)
	if err != nil {
		return result, fmt.Errorf("annotate the target: %w", err)
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

	ociURLForArgo, ociInsecure, err := clusterControllerOCIEndpoint()
	if err != nil {
		return result, err
	}
	// OCI URL recorded inside the root's source — the controller pulls from
	// this, so it must be the in-cluster-reachable form. The root references
	// the apps Space's Release, served at /space/<apps-slug>, tag "latest".
	rootRepoURL := strings.TrimRight(ociURLForArgo, "/") + "/space/" + appsSlug

	// rootName is the root's name in the controller, which its URL-TargetUI
	// links to. Flux has no UI, so it needs none.
	var rootManifest []byte
	var rootName string
	switch {
	case sveltos:
		fmt.Fprintf(out, "Creating root ClusterProfile Unit %q in apps space %q...\n", clusterRootUnitSlug, appsSlug)
		rootManifest = clusterSveltosRootProfileManifest(rootRepoURL, clusterSveltosPollInterval(!spec.noArgobot), ociInsecure)
		rootName = clusterSveltosRootProfile
	case flux:
		fmt.Fprintf(out, "Creating root OCIRepository and Kustomization Unit %q in apps space %q...\n", clusterRootUnitSlug, appsSlug)
		rootManifest = clusterFluxRootManifest(rootRepoURL, clusterFluxPollInterval(!spec.noArgobot), ociInsecure, spec.prune)
	default:
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
	// apps Space for the single authoritative publish below.
	if installBot != nil {
		if err := installBot(); err != nil {
			return result, fmt.Errorf("install argobot: %w", err)
		}
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

	// Last, so a cluster that failed partway stays one attach can pick up
	// again: the bot's Space and Channel go on the cluster Space, where `down`
	// and `detach` read them rather than deriving them, and the stage marker
	// comes off.
	botSpace, botChannel := "", ""
	if !spec.noArgobot {
		botSpace, botChannel = clusterArgobotComponent+"-"+spec.name, clusterArgobotChannelSlug
	}
	if err := clusterFinishSpaceAnnotations(spaceID, botSpace, botChannel); err != nil {
		return result, err
	}

	return clusterAttachResult{spec: spec, installed: inst, appsSlug: appsSlug, serviceAccountSlug: serviceAccountSlug}, nil
}

// clusterUIAnnotations returns the annotations that record a controller UI
// exposed on a host port: the port on the cluster Space, which is how `cub
// cluster open` finds it, and a deep link on the OCI target. None for a port
// of 0, or for Flux, which has no UI.
func clusterUIAnnotations(controller string, uiPort int) (space, target map[string]string) {
	if uiPort == 0 {
		return nil, nil
	}
	switch controller {
	case clusterControllerSveltos:
		return map[string]string{clusterAnnotationSveltosPort: strconv.Itoa(uiPort)},
			map[string]string{clusterAnnotationTargetUI: clusterSveltosTargetUI(uiPort)}
	case clusterControllerArgo:
		// The ConfigHub UI substitutes "{slug}" with the Space slug at render
		// time, linking a Unit straight to its Argo CD Application.
		return map[string]string{clusterAnnotationArgoPort: strconv.Itoa(uiPort)},
			map[string]string{clusterAnnotationTargetUI: fmt.Sprintf("http://localhost:%d/applications/argocd/{slug}", uiPort)}
	}
	return nil, nil
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
