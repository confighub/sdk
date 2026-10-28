// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/confighub/sdk/core/cubapi"
	goclientnew "github.com/confighub/sdk/core/openapi/goclient-new"
	"github.com/google/uuid"
	"golang.org/x/term"
	"sigs.k8s.io/yaml"
)

// argobot install for `cub cluster attach` (and so `cub cluster up`). argobot
// is a ConfigHub bot that listens on a Channel and force-syncs the
// corresponding Argo CD Application the moment a deploy happens, closing Argo's
// reconcile-interval gap. It is delivered like
// any other component: a shared base installed from the manifests `argobot
// install` prints, and a per-cluster downstream variant. The child Argo Application that
// makes argobot live is created by `cub variant create --target` itself — the
// cluster's OCI target carries the confighub.com/apps-space annotation, so
// creating the argobot variant against it auto-creates the Application Unit in
// the apps Space (see createVariantArgoApp). argobot is just the first user
// of that path.
//
// Everything argobot-specific lives in this file; the rest of the cluster
// package is generic (kind, Argo CD, ConfigHub Spaces/Targets/ServiceAccounts).
// The one coupling into the attachment is clusterInstallK8sbot, the stage `cub
// cluster k8sbot-install` runs and `cub cluster attach` runs inside
// clusterBootstrapApps.

// argobot install constants. argobot is installed as a ConfigHub component from
// the manifests `argobot install` prints, then given a per-cluster downstream
// variant.
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
	// clusterArgobotInstallPod is that one-off Pod. It runs in the default
	// namespace, which every cluster has before argobot's own exists.
	clusterArgobotInstallPod          = "argobot-install"
	clusterArgobotInstallPodNamespace = "default"
	// clusterArgobotNamespace is the namespace argobot's manifests deploy into.
	clusterArgobotNamespace = "argobot"
	// clusterArgobotUnitSlug is the Unit holding argobot's Deployment. The
	// manifests become one Unit per resource and a workload keeps its bare name, so the
	// Deployment named argobot lands in a Unit named argobot, while its
	// ServiceAccount, RBAC and Namespace take slugs of their own.
	clusterArgobotUnitSlug = "argobot"
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
// Update uploads the manifests `argobot install` prints over it before cloning,
// so the cluster runs the release argobot's :latest image names; clone leaves it
// as it is, so every cluster cloned from it runs the same argobot. A base that
// does not exist yet is installed either way.
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
// the image repository, without a tag: `argobot install` runs from its :latest
// image, and the manifests it prints run argobot from the same repository.
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
const clusterRegistryFlagUsage = "where to pull argobot's image from: ghcr, gcr, or a mirror prefix holding <prefix>/argobot (default ghcr)"

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

// clusterArgobotManifests runs `argobot install --controller <controller>` in
// the cluster as a one-off Pod and returns what it printed: argobot's
// manifests for that controller, with the image pinned to the release the
// install image was built as, in the same repository. The Pod is deleted
// afterwards, whether or not it succeeded.
func clusterArgobotManifests(kubeconfigPath, controller, repository string, out io.Writer) ([]byte, error) {
	deletePod := func(wait string) error {
		return clusterKubectl(ctx, kubeconfigPath, nil, io.Discard,
			"delete", "pod", clusterArgobotInstallPod, "-n", clusterArgobotInstallPodNamespace,
			"--ignore-not-found", "--wait="+wait)
	}
	// A Pod left by an interrupted earlier run would make `kubectl run` fail.
	if err := deletePod("true"); err != nil {
		return nil, err
	}
	image := repository + ":latest"
	fmt.Fprintf(out, "Running `argobot install --controller %s` (%s) in the cluster...\n", controller, image)
	// IfNotPresent: a :latest image otherwise defaults to Always, which would
	// ignore an image loaded into the node with `kind load docker-image`.
	if err := clusterKubectl(ctx, kubeconfigPath, nil, out,
		"run", clusterArgobotInstallPod, "-n", clusterArgobotInstallPodNamespace,
		"--image", image, "--image-pull-policy", "IfNotPresent", "--restart", "Never",
		"--", "install", "--controller", controller, "--image", repository); err != nil {
		return nil, fmt.Errorf("start argobot install: %w", err)
	}
	defer func() { _ = deletePod("false") }()

	if err := clusterArgobotWaitInstallPod(kubeconfigPath, 3*time.Minute); err != nil {
		return nil, err
	}
	manifests, err := clusterKubectlOutput(ctx, kubeconfigPath,
		"logs", "pod/"+clusterArgobotInstallPod, "-n", clusterArgobotInstallPodNamespace)
	if err != nil {
		return nil, fmt.Errorf("read argobot install output: %w", err)
	}
	if len(bytes.TrimSpace(manifests)) == 0 {
		return nil, fmt.Errorf("argobot install --controller %s printed nothing", controller)
	}
	return manifests, nil
}

// clusterArgobotWaitInstallPod polls the install Pod until it has run to
// completion. A Failed Pod is terminal, and its logs carry argobot's error.
func clusterArgobotWaitInstallPod(kubeconfigPath string, budget time.Duration) error {
	deadline := time.Now().Add(budget)
	for {
		phase, err := clusterKubectlOutput(ctx, kubeconfigPath,
			"get", "pod", clusterArgobotInstallPod, "-n", clusterArgobotInstallPodNamespace,
			"-o", "jsonpath={.status.phase}")
		if err != nil {
			return fmt.Errorf("check argobot install: %w", err)
		}
		switch string(phase) {
		case "Succeeded":
			return nil
		case "Failed":
			logs, _ := clusterKubectlOutput(ctx, kubeconfigPath,
				"logs", "pod/"+clusterArgobotInstallPod, "-n", clusterArgobotInstallPodNamespace)
			return fmt.Errorf("argobot install failed: %s", bytes.TrimSpace(logs))
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("argobot install Pod still %q after %s", phase, budget)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(2 * time.Second):
		}
	}
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
	baseMode             string // clusterArgobotBaseUpdate or clusterArgobotBaseClone, from clusterChooseArgobotBase
	repository           string // argobot's image repository, from clusterArgobotRepository
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
// Space by `cub variant create --target` (step 2), auto-derived from the OCI
// target's apps-space annotation — the same path any deployment variant
// uses. The caller publishes the apps Space's Release once, after this
// returns, so that the root app-of-apps' first sync already includes argobot.
func clusterInstallArgobot(out io.Writer, o clusterArgobotOptions) error {
	argobotSpace := clusterArgobotComponent + "-" + o.clusterName
	baseVariant, baseSpace := clusterArgobotBase(o.controller)

	// 1. Ensure the shared base component exists, installed from the manifests
	// `argobot install` prints for this controller. An upload is
	// create-or-update, so updating a base another cluster already installed
	// changes only what differs from the manifests. With --k8sbot-base clone an
	// existing base is not updated. Either way the base is checked first for
	// Units no upload owns that define argobot's resources, since a variant
	// cloned from it would apply both.
	manifests, err := clusterArgobotManifests(o.kubeconfigPath, o.controller, o.repository, out)
	if err != nil {
		return err
	}
	if err := clusterArgobotCheckBase(baseSpace, manifests); err != nil {
		return err
	}
	if o.baseMode == clusterArgobotBaseClone {
		fmt.Fprintf(out, "Cloning the existing argobot base %q as it is...\n", baseSpace)
	} else {
		fmt.Fprintln(out, "Installing argobot base component...")
		if err := runCubStdin(manifests, "variant", "upload",
			"--component", clusterArgobotComponent,
			"--variant", baseVariant,
			"-"); err != nil {
			return fmt.Errorf("install argobot base component: %w", err)
		}
	}

	// 2. Per-cluster downstream variant, cloned from the base and bound to this
	// cluster's OCI target (which also becomes the variant Space's release
	// target). No --namespace: argobot's manifests already place resources in
	// the argobot and argocd namespaces, and run in them on every cluster.
	//
	// Because the OCI target carries the confighub.com/apps-space
	// annotation, this also auto-creates argobot's child Argo Application Unit
	// in the apps Space and republishes it — argobot rides the same path as
	// any deployment variant instead of hand-rolling its own Application.
	//
	// No --allow-exists: `cub variant create` is fail-fast on a pre-existing
	// variant, and the attachment's precondition check guarantees a fresh
	// argobot Space, so a lingering argobot-<name> is surfaced rather than
	// silently reused.
	fmt.Fprintf(out, "Creating per-cluster argobot variant %q...\n", argobotSpace)
	if err := runCub("variant", "create",
		"--target", o.ociTargetRef,
		o.clusterName, baseSpace); err != nil {
		return fmt.Errorf("create argobot variant: %w", err)
	}

	space, err := resolveSpace(argobotSpace, "SpaceID")
	if err != nil {
		return fmt.Errorf("resolve argobot variant space %q: %w", argobotSpace, err)
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
	// stores the cluster's facts on. set-env upserts literal env vars; the key
	// stays a secretKeyRef to the Secret clusterBootstrapServiceAccount applied
	// out of band.
	fmt.Fprintln(out, "Configuring argobot (CONFIGHUB_URL, CONFIGHUB_CHANNEL, CONFIGHUB_TARGET)...")
	if err := runCub("function", "do", "--quiet",
		"--space", argobotSpace, "--unit", clusterArgobotUnitSlug,
		"set-env", clusterArgobotContainer,
		"CONFIGHUB_URL="+o.apiURLForContainer,
		"CONFIGHUB_CHANNEL="+channelRef,
		"CONFIGHUB_TARGET="+o.ociTargetRef); err != nil {
		return fmt.Errorf("configure argobot env: %w", err)
	}

	// 5. Publish the variant's Release; Argo pulls argobot's workload from here.
	unit, err := resolveUnit(clusterArgobotUnitSlug, space.Space.SpaceID.String(), "UnitID")
	if err != nil {
		return fmt.Errorf("resolve argobot unit: %w", err)
	}
	if err := clusterWaitUnitTriggers(space.Space.SpaceID, unit.Unit.UnitID); err != nil {
		return err
	}
	fmt.Fprintf(out, "Publishing argobot variant Release (%s)...\n", argobotSpace)
	if err := clusterPublishRelease(space.Space.SpaceID); err != nil {
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
func clusterChooseArgobotBase(out io.Writer, controller, flag, repository string) (string, error) {
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
	fmt.Fprintf(out, "  [u] update it to what %s installs, then clone it (default)\n", repository+":latest")
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
func clusterArgobotCheckBase(baseSpace string, manifests []byte) error {
	plan, err := cubapi.Upload(ctx, cubClient, goclientnew.UploadRequest{
		Components: []goclientnew.UploadComponentRequest{{Name: clusterArgobotComponent, Space: baseSpace}},
		Files:      []goclientnew.UploadRequestFile{{Path: "stdin.yaml", Content: string(manifests)}},
		Source:     &goclientnew.UploadSourceInfo{Ref: "-", Client: "cub", ClientVersion: Version},
	}, true)
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
