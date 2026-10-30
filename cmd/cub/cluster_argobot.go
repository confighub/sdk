// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"os"
	"os/exec"
	"path"
	"slices"
	"strings"

	"github.com/confighub/sdk/core/cubapi"
	"github.com/confighub/sdk/core/function/api"
	"github.com/confighub/sdk/core/ocibundle"
	goclientnew "github.com/confighub/sdk/core/openapi/goclient-new"
	"github.com/confighub/sdk/core/workerapi"
	"github.com/google/uuid"
	"golang.org/x/term"
	"sigs.k8s.io/yaml"
)

// argobot install for `cub cluster attach` (and so `cub cluster up`). argobot
// is a ConfigHub bot that listens on a Channel and force-syncs the
// corresponding Argo CD Application the moment a deploy happens, closing Argo's
// reconcile-interval gap. It is delivered like
// any other component: a shared base installed from the deployment its release
// image carries, or with --k8sbot-installer from its installer package, and a
// per-cluster downstream variant. The child Argo Application that
// makes argobot live is created the way `cub variant create --target` creates
// one — the cluster's OCI target carries the confighub.com/apps-space
// annotation, so creating the argobot variant against it adds the Application
// Unit to the apps Space (see createVariantClusterApp). argobot is just the
// first user of that path.
//
// Everything argobot-specific lives in this file; the rest of the cluster
// package is generic (kind, Argo CD, ConfigHub Spaces/Targets/ServiceAccounts).
// The one coupling into the attachment is clusterInstallK8sbot, the stage `cub
// cluster k8sbot-install` runs and `cub cluster attach` runs inside
// clusterBootstrapApps.

// argobot install constants. argobot is installed as a ConfigHub component,
// then given a per-cluster downstream variant.
const (
	// clusterArgobotComponent is the well-known "Component" label value and the
	// base Space is <component>-base.
	clusterArgobotComponent = "argobot"
	// clusterArgobotBaseVariant is the "Variant" label of the shared base Space.
	clusterArgobotBaseVariant = "base"
	// clusterArgobotBaseSpace is the shared base Space slug (one per hub, reused
	// across clusters).
	clusterArgobotBaseSpace = "argobot-base"
	// The Sveltos counterparts. argobot's Sveltos deployment needs different
	// RBAC and its own HealthCheck, so it is a base of its own, beside the Argo
	// CD one.
	clusterArgobotSveltosBaseVariant = "sveltos-base"
	clusterArgobotSveltosBaseSpace   = "argobot-sveltos-base"
	// The Flux counterparts, for argobot's Flux deployment, which needs RBAC of
	// its own.
	clusterArgobotFluxBaseVariant = "flux-base"
	clusterArgobotFluxBaseSpace   = "argobot-flux-base"
	// clusterArgobotNamespace is the namespace argobot's manifests deploy into.
	clusterArgobotNamespace = "argobot"
	// clusterArgobotUnitSlug is the Unit holding argobot's Deployment. The
	// manifests become one Unit per resource and a workload keeps its bare name, so the
	// Deployment named argobot lands in a Unit named argobot, while its
	// ServiceAccount, RBAC and Namespace take slugs of their own.
	clusterArgobotUnitSlug = "argobot"
	// clusterArgobotEnvUnitSlug is the AppConfig Unit holding argobot's
	// environment in an install from the installer package, which ConfigHub
	// renders into the argobot-env ConfigMap.
	clusterArgobotEnvUnitSlug = "argobot-env"
	// clusterArgobotEnvConfigSchema is that environment's configSchema, which
	// set-string-path names to edit it.
	clusterArgobotEnvConfigSchema = "Argobot"
	// clusterArgobotRecordUnitSlug is the hidden Unit an installer upload keeps
	// its answers in. A base holding one was installed with the installer.
	clusterArgobotRecordUnitSlug = "installer"
	// clusterInstallerPlugin is the cub plugin command that runs ConfigHub's
	// installer, as `cub installer`.
	clusterInstallerPlugin = "installer"
	// clusterArgobotContainer is the container name in argobot's Deployment.
	clusterArgobotContainer = "argobot"
	// clusterArgobotSecret is the out-of-band Secret argobot reads its service
	// account key from (never shipped in the OCI bundle).
	clusterArgobotSecret = "argobot-secrets"
	// clusterArgobotChannelSlug is the Channel argobot listens on, created in
	// the cluster Space beside the Target whose deploys it reacts to.
	clusterArgobotChannelSlug = "argobot"
)

// What to do with a shared base Space that already exists (--k8sbot-base).
// Update installs argobot's newest release over it before cloning; clone leaves
// it as it is, so every cluster cloned from it runs the same argobot. A base
// that does not exist yet is installed either way.
const (
	clusterArgobotBaseUpdate = "update"
	clusterArgobotBaseClone  = "clone"
)

// The argobot image repositories --registry names. Releases are built on
// ghcr.io; gcr.io carries a copy of each one, for networks that reach Google's
// registry and not GitHub's.
const (
	clusterArgobotGHCR = "ghcr.io/confighub/argobot"
	clusterArgobotGCR  = "gcr.io/confighubapp/argobot"
)

// clusterArgobotRepository reads --registry: "ghcr", "gcr", or the prefix of a
// mirror holding argobot's image as <prefix>/argobot. Empty is ghcr. It returns
// the image repository, without a tag. argobot's deployment is read from the
// newest release there, and runs argobot from the same repository.
func clusterArgobotRepository(registry string) (string, error) {
	switch registry {
	case "", "ghcr", "ghcr.io":
		return clusterArgobotGHCR, nil
	case "gcr", "gcr.io":
		return clusterArgobotGCR, nil
	}
	prefix := strings.TrimSuffix(registry, "/")
	if !strings.Contains(prefix, "/") && !strings.Contains(prefix, ".") {
		return "", fmt.Errorf("--registry %q: expected ghcr, gcr, or a mirror prefix such as registry.example.com/confighub", registry)
	}
	return prefix + "/argobot", nil
}

// clusterRegistryFlagUsage is the --registry help of the commands that install
// argobot.
const clusterRegistryFlagUsage = "where to pull argobot's image from: ghcr, gcr, or a mirror prefix holding <prefix>/argobot, and <prefix>/packages/argobot for --k8sbot-installer (default ghcr)"

// clusterK8sbotInstallerFlagUsage is the --k8sbot-installer help of the
// commands that install argobot.
const clusterK8sbotInstallerFlagUsage = "install argobot's shared base Space with ConfigHub's installer, from its installer package, rather than from the deployment its image carries; needs the installer cub plugin (cub plugin install confighub/installer)"

// clusterK8sbotBaseFlagUsage is the --k8sbot-base help of the commands that
// install argobot.
const clusterK8sbotBaseFlagUsage = "what to do with argobot's shared base Space when it already exists: update (install argobot's newest release over it, then clone it) or clone (clone it as it is); asked when stdin is a terminal, otherwise update"

// clusterArgobotPackageRepository is the repository of argobot's installer
// package, beside its image: <prefix>/packages/argobot.
func clusterArgobotPackageRepository(repository string) string {
	return path.Dir(repository) + "/packages/" + clusterArgobotComponent
}

// clusterArgobotPullOptions are the options argobot's image and package are
// read with: anonymously, since both are public and an expired local login
// would otherwise be refused, and over plain HTTP from a local registry.
func clusterArgobotPullOptions() ocibundle.Options {
	return ocibundle.Options{UserAgent: "cub/" + Version, PlainHTTPLoopback: true}
}

// The event type argobot reacts to: a Release published for the cluster's
// Target. It is what argobot --print-subscriptions prints; named here so the
// Channel is created without running the bot.
const clusterArgobotEventReleasePublished = "release.published"

// clusterK8sbotSpec is what installing argobot in a cluster needs: the
// cluster's Space, OCI target and apps Space, which argobot's Channel and
// Units go in, and the cluster's service account, which it runs as.
type clusterK8sbotSpec struct {
	name           string
	spaceSlug      string
	kubeconfigPath string
	controller     string
	spaceID        uuid.UUID // the cluster Space; argobot's Channel lives here, beside the Target
	targetID       uuid.UUID // the cluster's OCI target, which the Channel's subscription is scoped to
	// argobotBase is clusterArgobotBaseUpdate or clusterArgobotBaseClone, as
	// clusterChooseArgobotBase decided it.
	argobotBase string
	// argobotRepository is the image repository argobot is pulled from, as
	// clusterArgobotRepository read --registry.
	argobotRepository string
	// argobotInstaller is the installer plugin that installs argobot's base, as
	// clusterFindInstaller found it; nil installs it from argobot's image.
	argobotInstaller *PluginCommand
	// standalone is true for `cub cluster k8sbot-install` on a cluster attached
	// already: its root runs, so the stage marks the target as argobot's,
	// records the bot on the cluster Space, and republishes the apps Space
	// itself. Within an attachment, attach does all three.
	standalone bool
}

// clusterInstallK8sbot installs argobot in a cluster, as `cub cluster
// k8sbot-install` and as the last stage of `cub cluster attach` before its
// first apps-Space Release. argobot authenticates as the cluster's service
// account with a key in the argobot-secrets Secret, issued here unless
// svc-account-bootstrap already put one in the cluster. Everything it creates
// is registered with registerRollback.
func clusterInstallK8sbot(out io.Writer, spec clusterK8sbotSpec, registerRollback func(func())) error {
	serviceAccountSlug := clusterServiceAccountSlug(spec.name)
	serviceAccountID, serviceAccountUserID, err := clusterServiceAccountIDs(serviceAccountSlug)
	if err != nil {
		return err
	}
	hasKey, err := clusterHasArgobotKey(spec.kubeconfigPath)
	if err != nil {
		return err
	}
	if !hasKey {
		fmt.Fprintf(out, "Issuing a key for argobot on %q...\n", serviceAccountSlug)
		privateKey, err := clusterCreateServiceAccountKey(serviceAccountID, serviceAccountUserID, "argobot in cluster "+spec.name)
		if err != nil {
			return fmt.Errorf("issue the service account key: %w", err)
		}
		if err := clusterApplyArgobotKey(out, spec.kubeconfigPath, privateKey, registerRollback); err != nil {
			return err
		}
	}

	appsSlug := spec.spaceSlug + clusterAppsSuffix(spec.controller)
	argobotSlug := clusterArgobotComponent + "-" + spec.name
	if spec.standalone {
		// `cub variant create` reads the annotation to decide whether argobot's
		// own deployment is released at once (Flux).
		if _, err := clusterGrantTarget(spec.spaceID, spec.targetID, uuid.Nil,
			map[string]string{clusterAnnotationArgobot: "true"}); err != nil {
			return fmt.Errorf("mark the target as argobot's: %w", err)
		}
		registerRollback(func() {
			_ = clusterRemoveTargetAnnotation(spec.spaceID, spec.targetID, clusterAnnotationArgobot)
		})
		// The root deploys the apps Space's Release, which `cub variant create`
		// adds argobot's Unit to: take it back out so the root drops argobot.
		registerRollback(func() {
			appsSpace, err := resolveSpace(appsSlug, "SpaceID")
			if err != nil {
				return
			}
			unit, err := resolveUnit(argobotSlug, appsSpace.Space.SpaceID.String(), "UnitID")
			if err != nil {
				return
			}
			fmt.Fprintf(out, "Rolling back: removing argobot's Unit from apps space %q\n", appsSlug)
			_, _ = cubClientNew.DeleteUnitWithResponse(ctx, appsSpace.Space.SpaceID, unit.Unit.UnitID, &goclientnew.DeleteUnitParams{})
			_ = clusterPublishRelease(appsSpace.Space.SpaceID)
		})
		// Within an attachment the Channel goes with the cluster Space.
		registerRollback(func() {
			channel, err := resolveChannel(clusterArgobotChannelSlug, spec.spaceID.String(), "ChannelID")
			if err != nil {
				return
			}
			fmt.Fprintf(out, "Rolling back: cub channel delete %s/%s\n", spec.spaceSlug, clusterArgobotChannelSlug)
			_, _ = cubClientNew.DeleteChannelWithResponse(ctx, spec.spaceID, channel.Channel.ChannelID)
		})
		// Before `cub variant create`, so argobot's own deployment polls like
		// the root. Its rollbacks run before the ones above, so the apps-Space
		// publish that takes argobot back out carries the old root.
		if err := clusterPollWithArgobot(out, spec, appsSlug, registerRollback); err != nil {
			return err
		}
	}

	// Register the per-cluster argobot variant Space for rollback before the
	// install runs: a partial failure can leave it with a cross-space
	// release-target reference that would otherwise block deleting the cluster's
	// OCI target. Resolve by slug at rollback time (it may not exist yet). The
	// shared argobot-base Space is intentionally left alone.
	registerRollback(func() {
		sp, err := resolveSpace(argobotSlug, "SpaceID")
		if err != nil {
			return // never created
		}
		fmt.Fprintf(out, "Rolling back: cub space delete --recursive %q\n", argobotSlug)
		_ = clusterClearReleaseTarget(sp.Space.SpaceID)
		_ = clusterDeleteSpace(argobotSlug, true)
	})

	// ConfigHub API URL as argobot (in-cluster) reaches it — loopback rewritten
	// to the docker host address, since a loopback cub server can only be
	// reached by a cluster on this machine. A remote server passes through.
	apiURLForContainer, err := clusterAPIEndpointFromContainer(clusterServerURL())
	if err != nil {
		return err
	}

	switch spec.controller {
	case clusterControllerSveltos:
		fmt.Fprintln(out, "\nInstalling argobot (event-driven Sveltos redeploy)...")
	case clusterControllerFlux:
		fmt.Fprintln(out, "\nInstalling argobot (event-driven Flux reconciliation)...")
	default:
		fmt.Fprintln(out, "\nInstalling argobot (event-driven Argo CD sync)...")
	}
	if err := clusterInstallArgobot(out, clusterArgobotOptions{
		controller:           spec.controller,
		clusterName:          spec.name,
		clusterSpaceID:       spec.spaceID,
		clusterSpaceSlug:     spec.spaceSlug,
		targetID:             spec.targetID,
		ociTargetRef:         spec.spaceSlug + "/" + clusterTargetSlug,
		serviceAccountUserID: serviceAccountUserID,
		apiURLForContainer:   apiURLForContainer,
		kubeconfigPath:       spec.kubeconfigPath,
		baseMode:             spec.argobotBase,
		repository:           spec.argobotRepository,
		installer:            spec.argobotInstaller,
	}); err != nil {
		return err
	}
	if !spec.standalone {
		return nil
	}

	// The root deploys argobot from the apps Space's Release: publish it with
	// argobot's Unit in it, whichever controller's `cub variant create` left it
	// unpublished, and with Argo CD ask the root to re-read it now rather than
	// on its next poll.
	appsSpace, err := resolveSpace(appsSlug, "SpaceID")
	if err != nil {
		return fmt.Errorf("resolve apps space %q: %w", appsSlug, err)
	}
	fmt.Fprintf(out, "Publishing the apps space's release (adds argobot to what the root deploys)...\n")
	if err := clusterPublishRelease(appsSpace.Space.SpaceID); err != nil {
		return err
	}
	if spec.controller == clusterControllerArgo {
		if err := clusterKubectl(ctx, spec.kubeconfigPath, nil, io.Discard,
			"annotate", "application", appsSlug, "-n", clusterArgoNamespace, "argocd.argoproj.io/refresh=hard", "--overwrite"); err != nil {
			fmt.Fprintf(out, "  warning: refresh root Application %q: %v\n", appsSlug, err)
		}
	}
	return clusterFinishSpaceAnnotations(spec.spaceID, argobotSlug, clusterArgobotChannelSlug)
}

// clusterPollWithArgobot moves a Sveltos or Flux cluster attached without
// argobot to the poll interval it has with argobot, as if attached with it:
// infrequent, since argobot has each Release deployed when it is published. The
// interval goes on the target, which deployments created from here on take it
// from (argobot's own first), and on the root Unit, which the apps-Space
// publish after argobot's install carries to the cluster. Deployments that
// already exist keep polling every minute. Argo CD has no interval of cub's to
// change.
func clusterPollWithArgobot(out io.Writer, spec clusterK8sbotSpec, appsSlug string, registerRollback func(func())) error {
	var key string
	var render func(interval, rootRepoURL string, insecure, prune bool) []byte
	switch spec.controller {
	case clusterControllerSveltos:
		key = clusterAnnotationSveltosPollInterval
		render = func(interval, rootRepoURL string, insecure, _ bool) []byte {
			return clusterSveltosRootProfileManifest(rootRepoURL, interval, insecure)
		}
	case clusterControllerFlux:
		key = clusterAnnotationFluxPollInterval
		render = func(interval, rootRepoURL string, insecure, prune bool) []byte {
			return clusterFluxRootManifest(rootRepoURL, interval, insecure, prune)
		}
	default:
		return nil
	}
	interval := clusterSveltosPollInterval(true)
	if spec.controller == clusterControllerFlux {
		interval = clusterFluxPollInterval(true)
	}
	target, err := clusterGetTarget(spec.spaceID, spec.targetID)
	if err != nil {
		return err
	}
	old := target.Annotations[key]
	if old == interval {
		return nil
	}
	prune := target.Annotations[clusterAnnotationPrune] == "true"

	fmt.Fprintf(out, "Moving the cluster's poll interval to %s, as with argobot...\n", interval)
	if _, err := clusterGrantTarget(spec.spaceID, spec.targetID, uuid.Nil, map[string]string{key: interval}); err != nil {
		return fmt.Errorf("record the poll interval on the target: %w", err)
	}
	registerRollback(func() {
		if old == "" {
			_ = clusterRemoveTargetAnnotation(spec.spaceID, spec.targetID, key)
			return
		}
		_, _ = clusterGrantTarget(spec.spaceID, spec.targetID, uuid.Nil, map[string]string{key: old})
	})

	appsSpace, err := resolveSpace(appsSlug, "SpaceID")
	if err != nil {
		return fmt.Errorf("resolve apps space %q: %w", appsSlug, err)
	}
	appsSpaceID := appsSpace.Space.SpaceID
	root, err := resolveUnit(clusterRootUnitSlug, appsSpaceID.String(), "UnitID")
	if err != nil {
		return fmt.Errorf("resolve root unit in apps space %q: %w", appsSlug, err)
	}
	rootUnitID := root.Unit.UnitID
	ociURLForCluster, insecure, err := clusterControllerOCIEndpoint()
	if err != nil {
		return err
	}
	rootRepoURL := strings.TrimRight(ociURLForCluster, "/") + "/space/" + appsSlug
	if _, err := putUnitData(appsSpaceID, rootUnitID, string(render(interval, rootRepoURL, insecure, prune)), nil); err != nil {
		return fmt.Errorf("update root unit: %w", err)
	}
	registerRollback(func() {
		previous := old
		if previous == "" {
			previous = clusterSveltosPollInterval(false)
			if spec.controller == clusterControllerFlux {
				previous = clusterFluxPollInterval(false)
			}
		}
		_, _ = putUnitData(appsSpaceID, rootUnitID, string(render(previous, rootRepoURL, insecure, prune)), nil)
		_ = clusterWaitUnitTriggers(appsSpaceID, rootUnitID)
	})
	return clusterWaitUnitTriggers(appsSpaceID, rootUnitID)
}

// clusterApplyArgobotKey applies argobot's key to the cluster, as the
// argobot-secrets Secret in the argobot namespace (created with it), where
// argobot reads CONFIGHUB_AUTH_PRIVATE_KEY from. Applied straight to the
// cluster so the key never enters a published Release, and before argobot's
// Deployment so it is there when the pod starts.
func clusterApplyArgobotKey(out io.Writer, kubeconfigPath string, privateKey json.RawMessage, registerRollback func(func())) error {
	fmt.Fprintf(out, "Applying argobot key Secret to the %q namespace...\n", clusterArgobotNamespace)
	if err := clusterKubectlApply(ctx, kubeconfigPath, clusterArgobotSecretManifest(privateKey), out); err != nil {
		return fmt.Errorf("apply argobot secret: %w", err)
	}
	registerRollback(func() {
		_ = clusterKubectl(ctx, kubeconfigPath, nil, io.Discard,
			"delete", "secret", clusterArgobotSecret, "-n", clusterArgobotNamespace, "--ignore-not-found")
	})
	return nil
}

// clusterArgobotSubscriptions is what argobot's Channel subscribes to: the
// release events of the cluster's Target, so a bot reacts to its own cluster's
// deploys and nothing else.
func clusterArgobotSubscriptions(targetID uuid.UUID) []goclientnew.EventSubscription {
	target := goclientnew.UUID(targetID)
	return []goclientnew.EventSubscription{
		{
			EventTypes: []string{clusterArgobotEventReleasePublished},
			TargetID:   &target,
		},
	}
}

// clusterCreateArgobotChannel creates argobot's Channel in the cluster Space,
// subscribed to the cluster Target's deploys, and grants Claim to the cluster
// service account's user, which is what lets argobot, running as that service
// account, claim and poll it. The Channel is a mailbox with no identity of its
// own: what it delivers is set here, and argobot only needs to be told its name.
func clusterCreateArgobotChannel(spaceID, targetID, serviceAccountUserID uuid.UUID) (uuid.UUID, error) {
	permissions := goclientnew.Permissions{
		"Claim": {UserIDs: map[string]bool{serviceAccountUserID.String(): true}},
	}
	body := goclientnew.Channel{
		SpaceID:       spaceID,
		Slug:          clusterArgobotChannelSlug,
		DisplayName:   clusterArgobotChannelSlug,
		Permissions:   &permissions,
		Subscriptions: clusterArgobotSubscriptions(targetID),
	}
	res, err := cubClientNew.CreateChannelWithResponse(ctx, spaceID, &goclientnew.CreateChannelParams{}, body)
	if cubapi.IsAPIError(err, res) {
		return uuid.Nil, cubapi.InterpretErrorGeneric(err, res)
	}
	return res.JSON200.ChannelID, nil
}

// clusterArgobotSecretManifest returns the YAML for the out-of-band Secret that
// carries argobot's service account key, as the private JWK argobot reads from
// CONFIGHUB_AUTH_PRIVATE_KEY. It is applied straight to the cluster via kubectl
// (never shipped in the OCI bundle) so credential material never enters a
// published Release. The Namespace is created alongside so the Secret has
// somewhere to land before Argo renders argobot's Deployment.
//
// The JWK goes in as a literal block scalar: one JSON line, indented, with no
// quoting to get wrong. A JWK carries no newline of its own.
func clusterArgobotSecretManifest(privateKey json.RawMessage) []byte {
	return fmt.Appendf(nil, `apiVersion: v1
kind: Namespace
metadata:
  name: %s
---
apiVersion: v1
kind: Secret
metadata:
  name: %s
  namespace: %s
type: Opaque
stringData:
  CONFIGHUB_AUTH_PRIVATE_KEY: |-
    %s
`, clusterArgobotNamespace, clusterArgobotSecret, clusterArgobotNamespace, bytes.TrimSpace(privateKey))
}

// clusterArgobotManifestPath is where argobot's image carries its deployment
// for a controller.
func clusterArgobotManifestPath(controller string) string {
	switch controller {
	case clusterControllerSveltos:
		return "/manifests/sveltos.yaml"
	case clusterControllerFlux:
		return "/manifests/flux.yaml"
	}
	return "/manifests/argocd.yaml"
}

// clusterArgobotManifests reads argobot's deployment for a controller from the
// newest argobot release in repository, without running the image. The
// deployment runs the release it ships in, from ghcr.io; it is returned running
// it from repository.
func clusterArgobotManifests(controller, repository string, out io.Writer) (manifests []byte, ref string, err error) {
	tag, err := ocibundle.NewestRelease(ctx, repository, clusterArgobotPullOptions())
	if err != nil {
		return nil, "", fmt.Errorf("find argobot's newest release: %w", err)
	}
	ref = repository + ":" + tag
	fmt.Fprintf(out, "Reading argobot's deployment from %s...\n", ref)
	manifests, _, err = ocibundle.ReadImageFile(ctx, ref, clusterArgobotManifestPath(controller), clusterArgobotPullOptions())
	if err != nil {
		return nil, "", fmt.Errorf("read argobot's deployment: %w", err)
	}
	if repository != clusterArgobotGHCR {
		manifests = bytes.ReplaceAll(manifests, []byte("image: "+clusterArgobotGHCR+":"), []byte("image: "+repository+":"))
	}
	return manifests, ref, nil
}

// clusterFindInstaller finds ConfigHub's installer, installed as a cub plugin
// (cub plugin install confighub/installer), for --k8sbot-installer. cub runs
// the plugin's own entrypoint, never an "installer" found on PATH, which on
// macOS is the system's package installer.
func clusterFindInstaller() (*PluginCommand, error) {
	installer, ok := resolvePluginCommand(clusterInstallerPlugin)
	if !ok {
		return nil, fmt.Errorf("--k8sbot-installer needs ConfigHub's installer as a cub plugin; install it with: cub plugin install confighub/installer")
	}
	return installer, nil
}

// clusterArgobotPackageComponent is the installer package's component for a
// controller.
func clusterArgobotPackageComponent(controller string) string {
	if controller == clusterControllerArgo {
		return "argocd"
	}
	return controller
}

// clusterArgobotInstallBase installs argobot's shared base Space with the
// installer, from the newest release of argobot's installer package: no
// Channel, Target or URL, which each cluster's variant sets. setup is given the
// base Space, so it reads its earlier answers from that Space's installer
// record, and never another install of argobot's; the upload creates the Space
// when it doesn't exist yet.
func clusterArgobotInstallBase(out io.Writer, installer *PluginCommand, controller, repository string) error {
	baseVariant, baseSpace := clusterArgobotBase(controller)
	packageRepository := clusterArgobotPackageRepository(repository)
	version, err := ocibundle.NewestRelease(ctx, packageRepository, clusterArgobotPullOptions())
	if err != nil {
		return fmt.Errorf("find argobot's newest installer package: %w", err)
	}
	workDir, err := os.MkdirTemp("", "cub-argobot-base-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(workDir)

	ref := ocibundle.Scheme + packageRepository + ":" + version
	setup := []string{"setup", "--pull", ref, "--work-dir", workDir, "--space", baseSpace,
		"--non-interactive", "--namespace", clusterArgobotNamespace,
		"--select", clusterArgobotPackageComponent(controller)}
	if repository != clusterArgobotGHCR {
		// The package runs the argobot release of its own version.
		setup = append(setup, "--set-image", clusterArgobotGHCR+"="+repository+":v"+version)
	}
	fmt.Fprintf(out, "Installing argobot base component from %s with the installer plugin...\n", ref)
	if err := clusterRunInstaller(installer, setup...); err != nil {
		return err
	}
	return clusterRunInstaller(installer, "upload", "--work-dir", workDir, "--space", baseSpace,
		"--variant", baseVariant, "--yes")
}

// clusterRunInstaller runs the installer plugin as `cub installer` would,
// streaming its output, with the environment a plugin gets: the cub context in
// effect and its token.
func clusterRunInstaller(installer *PluginCommand, args ...string) error {
	c := exec.CommandContext(ctx, installer.Entrypoint, append(slices.Clone(installer.Args), args...)...)
	c.Env = pluginEnv()
	c.Stdout = os.Stdout
	c.Stderr = os.Stderr
	if err := c.Run(); err != nil {
		return fmt.Errorf("installer %s: %w", strings.Join(args, " "), err)
	}
	return nil
}

// clusterArgobotOptions carries everything the argobot install needs from the
// surrounding attachment.
type clusterArgobotOptions struct {
	controller           string    // the cluster's controller; argobot's Sveltos and Flux deployments come from bases of their own
	clusterName          string    // cluster name; also the argobot variant name
	clusterSpaceID       uuid.UUID // the cluster Space, where argobot's Channel is created
	clusterSpaceSlug     string    // its slug, the Space part of argobot's CONFIGHUB_CHANNEL
	targetID             uuid.UUID // the cluster's OCI target, which the Channel subscribes to
	ociTargetRef         string    // "<prefix>/target" — argobot's release target, and its CONFIGHUB_TARGET
	serviceAccountUserID uuid.UUID // the cluster service account's user, granted Claim on the Channel
	apiURLForContainer   string    // ConfigHub API URL as argobot (in-cluster) reaches it
	kubeconfigPath       string
	baseMode             string         // clusterArgobotBaseUpdate or clusterArgobotBaseClone, from clusterChooseArgobotBase
	repository           string         // argobot's image repository, from clusterArgobotRepository
	installer            *PluginCommand // the installer plugin that installs the base, from clusterFindInstaller; nil installs it from argobot's image
}

// clusterInstallArgobot installs argobot for a freshly attached cluster: argobot
// runs as the cluster's service account (the OCI target grants that service
// account's user the access live-status reporting needs), and the Channel
// argobot listens on is created in the cluster Space, subscribed to the
// Target's deploys and claimable by that service account. The argobot
// Deployment patches an Application's refresh annotation via the in-cluster
// ServiceAccount, needing no Argo CD API token.
//
// It creates argobot's units and publishes argobot's own variant Release. The
// child Argo Application Unit that makes argobot live is created in the apps
// Space as `cub variant create --target` creates it (step 2), derived from the OCI
// target's apps-space annotation — the same path any deployment variant
// uses. The caller publishes the apps Space's Release once, after this
// returns, so that the root app-of-apps' first sync already includes argobot.
func clusterInstallArgobot(out io.Writer, o clusterArgobotOptions) error {
	argobotSpace := clusterArgobotComponent + "-" + o.clusterName
	baseVariant, baseSpace := clusterArgobotBase(o.controller)

	// 1. Ensure the shared base component exists. With --k8sbot-base clone an
	// existing base is not updated.
	switch {
	case o.installer != nil:
		// The installer's upload is create-or-update, and reads the base's
		// earlier answers from its installer record.
		if o.baseMode == clusterArgobotBaseClone {
			exists, err := clusterSpaceExists(baseSpace)
			if err != nil {
				return err
			}
			if exists {
				fmt.Fprintf(out, "Cloning the existing argobot base %q as it is...\n", baseSpace)
				break
			}
		}
		if err := clusterArgobotInstallBase(out, o.installer, o.controller, o.repository); err != nil {
			return fmt.Errorf("install argobot base component: %w", err)
		}
	default:
		// Installed from the deployment argobot's newest release carries for
		// this controller. An upload is create-or-update, so updating a base
		// another cluster already installed changes only what differs. Either
		// way the base is checked first for Units no upload owns that define
		// argobot's resources, since a variant cloned from it would apply both.
		manifests, ref, err := clusterArgobotManifests(o.controller, o.repository, out)
		if err != nil {
			return err
		}
		upload := clusterArgobotBaseUpload(baseVariant, baseSpace, manifests, ref)
		if err := clusterArgobotCheckBase(baseSpace, upload); err != nil {
			return err
		}
		if o.baseMode == clusterArgobotBaseClone {
			fmt.Fprintf(out, "Cloning the existing argobot base %q as it is...\n", baseSpace)
			break
		}
		fmt.Fprintln(out, "Installing argobot base component...")
		if err := clusterArgobotUploadBase(upload); err != nil {
			return fmt.Errorf("install argobot base component: %w", err)
		}
	}

	// 2. Per-cluster downstream variant, cloned from the base and bound to this
	// cluster's OCI target (which also becomes the variant Space's release
	// target). No namespace: argobot's manifests already place resources in
	// the argobot and argocd namespaces, and run in them on every cluster.
	//
	// Because the OCI target carries the confighub.com/apps-space
	// annotation, this also auto-creates argobot's child Argo Application Unit
	// in the apps Space and republishes it — argobot rides the same path as
	// any deployment variant instead of hand-rolling its own Application.
	//
	// Not AllowExists: the create is fail-fast on a pre-existing variant, and
	// the attachment's precondition check guarantees a fresh argobot Space, so
	// a lingering argobot-<name> is surfaced rather than silently reused.
	fmt.Fprintf(out, "Creating per-cluster argobot variant %q...\n", argobotSpace)
	space, err := clusterCreateArgobotVariant(out, o, baseSpace)
	if err != nil {
		return fmt.Errorf("create argobot variant: %w", err)
	}

	// 3. The Channel argobot listens on, beside the Target in the cluster Space:
	// subscribed to that Target's apply and release events, claimable by the
	// service account argobot runs as. The cluster Space's rollback deletes it
	// with the Space.
	channelRef := o.clusterSpaceSlug + "/" + clusterArgobotChannelSlug
	fmt.Fprintf(out, "Creating argobot channel %q (subscribed to the target's deploys)...\n", channelRef)
	if _, err := clusterCreateArgobotChannel(o.clusterSpaceID, o.targetID, o.serviceAccountUserID); err != nil {
		return fmt.Errorf("create argobot channel: %w", err)
	}

	// 4. Point argobot at this ConfigHub, its Channel, and the Target it
	// stores the cluster's facts on, which the base leaves as placeholders. The
	// key stays a secretKeyRef to the Secret clusterBootstrapServiceAccount
	// applied out of band.
	fmt.Fprintln(out, "Configuring argobot (CONFIGHUB_URL, CONFIGHUB_CHANNEL, CONFIGHUB_TARGET)...")
	if err := clusterConfigureArgobot(space.SpaceID, o.installer != nil, map[string]string{
		"CONFIGHUB_URL":     o.apiURLForContainer,
		"CONFIGHUB_CHANNEL": channelRef,
		"CONFIGHUB_TARGET":  o.ociTargetRef,
	}); err != nil {
		return fmt.Errorf("configure argobot env: %w", err)
	}

	// 5. Publish the variant's Release; Argo pulls argobot's workload from here.
	// Every Unit's triggers settle first, not only the Deployment's: from the
	// installer package the edit also re-renders the argobot-env ConfigMap,
	// whose placeholders the cluster's gate checks again.
	units, err := apiListUnits(space.SpaceID.String(), "", "UnitID")
	if err != nil {
		return fmt.Errorf("list argobot's units: %w", err)
	}
	for _, unit := range units {
		if err := clusterWaitUnitTriggers(space.SpaceID, unit.UnitID); err != nil {
			return err
		}
	}
	fmt.Fprintf(out, "Publishing argobot variant Release (%s)...\n", argobotSpace)
	if err := clusterPublishRelease(space.SpaceID); err != nil {
		return err
	}

	switch o.controller {
	case clusterControllerSveltos:
		fmt.Fprintf(out, "argobot installed: variant %q, Sveltos ClusterProfile %q, channel %q\n",
			argobotSpace, argobotSpace, channelRef)
		return nil
	case clusterControllerFlux:
		fmt.Fprintf(out, "argobot installed: variant %q, Flux Kustomization %q, channel %q\n",
			argobotSpace, argobotSpace, channelRef)
		return nil
	}
	fmt.Fprintf(out, "argobot installed: variant %q, Argo app %q, channel %q\n",
		argobotSpace, argobotSpace, channelRef)
	return nil
}

// clusterConfigureArgobot sets variables of argobot's environment in its
// variant. From the image's deployment they are the Deployment's env, which
// set-env upserts; from the installer package they are the argobot-env
// AppConfig Unit, which ConfigHub renders into the ConfigMap the Deployment
// reads.
//
// The variables are set in one invocation, so the Unit gets one Revision for
// all of them.
func clusterConfigureArgobot(argobotSpaceID uuid.UUID, fromInstaller bool, env map[string]string) error {
	names := slices.Sorted(maps.Keys(env))
	unitSlug, toolchain := clusterArgobotUnitSlug, cubapi.DefaultToolchainType
	var fns []api.FunctionInvocation
	if fromInstaller {
		unitSlug, toolchain = clusterArgobotEnvUnitSlug, string(workerapi.ToolchainAppConfigEnv)
		for _, name := range names {
			fns = append(fns, api.FunctionInvocation{FunctionName: "set-string-path", Arguments: []api.FunctionArgument{
				{Value: clusterArgobotEnvConfigSchema}, {Value: name}, {Value: env[name]},
			}})
		}
	} else {
		args := []api.FunctionArgument{{Value: clusterArgobotContainer}}
		for _, name := range names {
			args = append(args, api.FunctionArgument{Value: name + "=" + env[name]})
		}
		fns = append(fns, api.FunctionInvocation{FunctionName: "set-env", Arguments: args})
	}
	result, err := cubapi.InvokeFunctions(ctx, cubClient, fns, cubapi.Selector{
		Where:         fmt.Sprintf("SpaceID = '%s' AND Slug = '%s'", argobotSpaceID, unitSlug),
		ToolchainType: toolchain,
	}, cubapi.Change{Description: "Configure argobot for this cluster"})
	if err != nil {
		return err
	}
	if len(result.Outcomes) == 0 {
		return fmt.Errorf("argobot's variant has no %s unit %q", toolchain, unitSlug)
	}
	if failed := result.Failed(); len(failed) > 0 {
		return fmt.Errorf("unit %s: %s", unitSlug, failed[0].Error)
	}
	return nil
}

// clusterArgobotBase returns the Variant label and slug of the shared base Space
// for a controller.
func clusterArgobotBase(controller string) (variant, space string) {
	switch controller {
	case clusterControllerSveltos:
		return clusterArgobotSveltosBaseVariant, clusterArgobotSveltosBaseSpace
	case clusterControllerFlux:
		return clusterArgobotFluxBaseVariant, clusterArgobotFluxBaseSpace
	}
	return clusterArgobotBaseVariant, clusterArgobotBaseSpace
}

// clusterChooseArgobotBase decides what the install does with the shared base
// Space: the --k8sbot-base value when given, and otherwise, when the base exists
// and stdin is a terminal, the answer to a question. It runs with the other
// preconditions, before anything is created, so the question is never asked
// halfway through. Without a terminal it is update, which is what every run did
// before the question existed.
func clusterChooseArgobotBase(out io.Writer, controller, flag string) (string, error) {
	switch flag {
	case clusterArgobotBaseUpdate, clusterArgobotBaseClone:
		return flag, nil
	case "":
	default:
		return "", fmt.Errorf("--k8sbot-base %q: must be %s or %s", flag, clusterArgobotBaseUpdate, clusterArgobotBaseClone)
	}
	_, baseSpace := clusterArgobotBase(controller)
	exists, err := clusterSpaceExists(baseSpace)
	if err != nil {
		return "", err
	}
	if !exists || !term.IsTerminal(int(os.Stdin.Fd())) {
		return clusterArgobotBaseUpdate, nil
	}

	current := clusterArgobotBaseImage(baseSpace)
	if current == "" {
		current = "unknown"
	}
	fmt.Fprintf(out, "The shared argobot base %q already exists; it runs %s.\n", baseSpace, current)
	fmt.Fprintln(out, "  [u] update it to argobot's newest release, then clone it (default)")
	fmt.Fprintln(out, "  [c] clone it as it is")
	fmt.Fprint(out, "Update or clone? [U/c]: ")
	answer, err := bufio.NewReader(os.Stdin).ReadString('\n')
	if err != nil {
		return "", fmt.Errorf("read answer: %w", err)
	}
	switch strings.ToLower(strings.TrimSpace(answer)) {
	case "", "u", "update":
		return clusterArgobotBaseUpdate, nil
	case "c", "clone":
		return clusterArgobotBaseClone, nil
	default:
		return "", fmt.Errorf("cancelled: answer u or c, or pass --k8sbot-base %s|%s", clusterArgobotBaseUpdate, clusterArgobotBaseClone)
	}
}

// clusterArgobotCheckBaseKind refuses an existing base installed the other way
// than this install: with the installer, which leaves its hidden installer
// record in the base, or from argobot's image, which leaves argobot's
// Deployment Unit and no record. The two installs write different Units, so
// one over the other would leave both. It runs with the other preconditions.
func clusterArgobotCheckBaseKind(controller string, withInstaller bool) error {
	_, baseSpace := clusterArgobotBase(controller)
	exists, err := clusterSpaceExists(baseSpace)
	if err != nil || !exists {
		return err
	}
	space, err := resolveSpace(baseSpace, "SpaceID")
	if err != nil {
		return err
	}
	spaceID := space.Space.SpaceID.String()
	_, err = resolveUnit(clusterArgobotRecordUnitSlug, spaceID, "UnitID")
	hasRecord := err == nil
	switch {
	case withInstaller && !hasRecord:
		if _, err := resolveUnit(clusterArgobotUnitSlug, spaceID, "UnitID"); err == nil {
			return fmt.Errorf("argobot base %q was installed from argobot's image, not with the installer; install without --k8sbot-installer, or delete the base (cub space delete --recursive %s) to install it with the installer", baseSpace, baseSpace)
		}
	case !withInstaller && hasRecord:
		return fmt.Errorf("argobot base %q was installed with the installer; install with --k8sbot-installer, or delete the base (cub space delete --recursive %s) to install it from argobot's image", baseSpace, baseSpace)
	}
	return nil
}

// clusterArgobotBaseImage is the image argobot's Deployment names in the base
// Space, for the question; "" when it cannot be read.
func clusterArgobotBaseImage(baseSpace string) string {
	space, err := resolveSpace(baseSpace, "SpaceID")
	if err != nil {
		return ""
	}
	unit, err := resolveUnit(clusterArgobotUnitSlug, space.Space.SpaceID.String(), "UnitID")
	if err != nil {
		return ""
	}
	data, err := fetchUnitData(space.Space.SpaceID, unit.Unit.UnitID)
	if err != nil {
		return ""
	}
	return clusterArgobotImageIn(data)
}

// clusterArgobotImageIn finds the image of argobot's container in a Unit's
// data, which may hold several documents.
func clusterArgobotImageIn(data string) string {
	for _, doc := range strings.Split(data, "\n---") {
		var deployment struct {
			Kind string `json:"kind"`
			Spec struct {
				Template struct {
					Spec struct {
						Containers []struct {
							Name  string `json:"name"`
							Image string `json:"image"`
						} `json:"containers"`
					} `json:"spec"`
				} `json:"template"`
			} `json:"spec"`
		}
		if yaml.Unmarshal([]byte(doc), &deployment) != nil || deployment.Kind != "Deployment" {
			continue
		}
		for _, c := range deployment.Spec.Template.Spec.Containers {
			if c.Name == clusterArgobotContainer {
				return c.Image
			}
		}
	}
	return ""
}

// clusterArgobotCheckBase refuses a base Space holding Units that define
// argobot's resources but that no upload of argobot owns, such as the single
// Unit an early installer wrote. An upload never writes or empties such a Unit,
// so it would sit beside the upload's own, and a variant cloned from the base
// would apply both. The check is a dry run of the upload, whose plan reports
// exactly those Units.
func clusterArgobotCheckBase(baseSpace string, upload goclientnew.UploadRequest) error {
	plan, err := cubapi.Upload(ctx, cubClient, upload, true)
	if err != nil {
		return fmt.Errorf("check argobot base %q: %w", baseSpace, err)
	}
	strays := map[string][]string{}
	for _, c := range plan.Components {
		for _, s := range c.Spaces {
			for _, d := range s.Duplicates {
				for _, o := range d.Others {
					if !slices.Contains(strays[o.SpaceSlug], o.UnitSlug) {
						strays[o.SpaceSlug] = append(strays[o.SpaceSlug], o.UnitSlug)
					}
				}
			}
		}
	}
	if len(strays) == 0 {
		return nil
	}
	spaces := make([]string, 0, len(strays))
	for space := range strays {
		spaces = append(spaces, space)
	}
	slices.Sort(spaces)
	var b strings.Builder
	fmt.Fprintf(&b, "argobot base %q has Units that define argobot's resources but were not uploaded as argobot, so a cluster cloned from it would run two copies of argobot. Delete them and run again:", baseSpace)
	for _, space := range spaces {
		slices.Sort(strays[space])
		for _, unit := range strays[space] {
			fmt.Fprintf(&b, "\n  cub unit delete --space %s %s", space, unit)
		}
	}
	return fmt.Errorf("%s", b.String())
}

// clusterArgobotBaseUpload is the upload of argobot's deployment, read from
// image ref, into its shared base Space, as `cub variant upload --component
// argobot --variant <base>` makes it.
func clusterArgobotBaseUpload(baseVariant, baseSpace string, manifests []byte, ref string) goclientnew.UploadRequest {
	return goclientnew.UploadRequest{
		Components:  []goclientnew.UploadComponentRequest{{Name: clusterArgobotComponent, Space: baseSpace}},
		SpaceLabels: map[string]string{"Variant": baseVariant},
		Files:       []goclientnew.UploadRequestFile{{Path: "stdin.yaml", Content: string(manifests)}},
		Source:      &goclientnew.UploadSourceInfo{Ref: ref, Client: "cub", ClientVersion: Version},
	}
}

// clusterArgobotUploadBase writes argobot's base. It empties, without asking,
// the Units of resources argobot's release no longer has, as `cub variant
// upload --yes` does: the base holds only argobot's own deployment, so what a
// release dropped is meant to leave every cluster cloned from it.
func clusterArgobotUploadBase(upload goclientnew.UploadRequest) error {
	result, err := cubapi.Upload(ctx, cubClient, upload, false)
	if err != nil {
		return err
	}
	reportUploadResult(result)
	reportUploadRevert(result)
	return nil
}

// clusterCreateArgobotVariant clones argobot's base into the cluster's variant,
// bound to the cluster's OCI target, and adds the Unit that deploys it to the
// cluster's apps Space, as `cub variant create --target` does.
func clusterCreateArgobotVariant(out io.Writer, o clusterArgobotOptions, baseSpace string) (*goclientnew.Space, error) {
	upstream, err := resolveSpace(baseSpace, "SpaceID")
	if err != nil {
		return nil, err
	}
	target, err := resolveTargetCore(o.ociTargetRef, defaultSpaceID(), "*")
	if err != nil {
		return nil, err
	}
	result, err := cubapi.CreateVariant(ctx, cubClient, cubapi.VariantSpec{
		Upstream: upstream.Space.SpaceID,
		Name:     o.clusterName,
		Target:   &target.TargetID,
	})
	if err != nil {
		return nil, err
	}
	if errs := result.UnitErrors(); len(errs) > 0 {
		return nil, fmt.Errorf("clone argobot's units into %s: %s", result.Space.Slug, strings.Join(errs, "; "))
	}
	if _, err := createVariantClusterApp(out, target, result.Space, target.TargetID, false); err != nil {
		return nil, err
	}
	return result.Space, nil
}
