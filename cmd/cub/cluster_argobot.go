// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"

	"github.com/confighub/sdk/core/cubapi"
	goclientnew "github.com/confighub/sdk/core/openapi/goclient-new"
	"github.com/google/uuid"
)

// argobot install for `cub cluster up`. argobot is a ConfigHub bot that watches
// the event log and force-syncs the corresponding Argo CD Application the moment
// a deploy happens, closing Argo's reconcile-interval gap. It is delivered like
// any other component: a shared base installed from its published OCI config
// bundle and a per-cluster downstream variant. The child Argo Application that
// makes argobot live is created by `cub variant create --target` itself — the
// cluster's OCI target carries the confighub.com/argo-apps-space annotation, so
// creating the argobot variant against it auto-creates the Application Unit in
// the apps Space (see createVariantArgoApp). argobot is just the first user
// of that path.
//
// Everything argobot-specific lives in this file; the rest of the cluster
// package is generic (kind, Argo CD, ConfigHub Spaces/Targets/ServiceAccounts). The one
// coupling into `cub cluster up` is clusterInstallArgobotStep, called once from
// clusterUpRun.

// argobot install constants. argobot is installed as a ConfigHub component from
// its published OCI config bundle, then given a per-cluster downstream variant.
const (
	// clusterArgobotComponent is the well-known "Component" label value and the
	// base Space is <component>-base.
	clusterArgobotComponent = "argobot"
	// clusterArgobotBaseVariant is the "Variant" label of the shared base Space.
	clusterArgobotBaseVariant = "base"
	// clusterArgobotBaseSpace is the shared base Space slug (one per hub, reused
	// across clusters).
	clusterArgobotBaseSpace = "argobot-base"
	// clusterArgobotOCIRef is the default OCI reference of argobot's config
	// bundle (its Kubernetes manifests), overridable with --argobot-oci.
	clusterArgobotOCIRef = "oci://ghcr.io/confighub/configs/argobot"
	// The Sveltos counterparts. argobot's Sveltos deployment
	// (manifests/sveltos/argobot.yaml in its repository) needs different RBAC
	// and its own HealthCheck, so it is a base of its own, beside the Argo CD one.
	clusterArgobotSveltosBaseVariant = "sveltos-base"
	clusterArgobotSveltosBaseSpace   = "argobot-sveltos-base"
	clusterArgobotSveltosOCIRef      = "oci://ghcr.io/confighub/configs/argobot-sveltos"
	// clusterArgobotNamespace is the namespace argobot's manifests deploy into.
	clusterArgobotNamespace = "argobot"
	// clusterArgobotUnitSlug is the Unit holding argobot's Deployment. The bundle
	// becomes one Unit per resource and a workload keeps its bare name, so the
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

// The event type argobot reacts to: a Release published for the cluster's
// Target. It is what argobot --print-subscriptions prints; named here so the
// Channel is created without running the bot.
const clusterArgobotEventReleasePublished = "release.published"

// clusterArgobotDeps carries the cluster-derived values the argobot install
// needs from clusterUpRun — the pieces of a freshly created cluster argobot
// binds to. The child Argo Application is created by `cub variant create` from
// the OCI target's argo-apps-space annotation, so the install itself only needs
// the identity argobot runs as (for the Channel grant and the out-of-band
// Secret) and the kubeconfig.
type clusterArgobotDeps struct {
	clusterSpaceID       uuid.UUID       // the cluster Space; argobot's Channel lives here, beside the Target
	targetID             uuid.UUID       // the cluster's OCI target, which the Channel's subscription is scoped to
	serviceAccountUserID uuid.UUID       // the user of the cluster's service account, which claims the Channel
	privateKey           json.RawMessage // its private key (goes into the out-of-band Secret)
	kubeconfigPath       string
}

// clusterInstallArgobotStep is the single argobot seam into `cub cluster up`.
// It is a no-op when --no-argobot is set; otherwise it registers the argobot
// variant Space for rollback (via registerRollback), derives argobot's
// in-cluster ConfigHub URL, and runs the install. The child Argo Application
// Unit lands in the apps Space as a side effect of `cub variant create
// --target` (auto-created from the OCI target's argo-apps-space annotation), so
// there is nothing for the caller to add to the apps Space's Release.
func clusterInstallArgobotStep(out io.Writer, opts clusterUpOptions, dep clusterArgobotDeps, registerRollback func(func())) error {
	if opts.noArgobot {
		return nil
	}

	// Register the per-cluster argobot variant Space for rollback before the
	// install runs: a partial failure can leave it with a cross-space
	// release-target reference that would otherwise block deleting the cluster's
	// OCI target. Resolve by slug at rollback time (it may not exist yet). The
	// shared argobot-base Space is intentionally left alone.
	argobotSlug := clusterArgobotComponent + "-" + opts.name
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
	// to host.docker.internal.
	apiURLForContainer, err := clusterAPIEndpointFromContainer(clusterServerURL())
	if err != nil {
		return err
	}

	sveltos := opts.controller == clusterControllerSveltos
	if sveltos {
		fmt.Fprintln(out, "\nInstalling argobot (event-driven Sveltos redeploy)...")
	} else {
		fmt.Fprintln(out, "\nInstalling argobot (event-driven Argo CD sync)...")
	}
	return clusterInstallArgobot(out, clusterArgobotOptions{
		sveltos:              sveltos,
		clusterName:          opts.name,
		clusterSpaceID:       dep.clusterSpaceID,
		clusterSpaceSlug:     opts.spaceSlug,
		targetID:             dep.targetID,
		ociTargetRef:         opts.spaceSlug + "/" + clusterTargetSlug,
		serviceAccountUserID: dep.serviceAccountUserID,
		privateKey:           dep.privateKey,
		apiURLForContainer:   apiURLForContainer,
		ociRef:               opts.argobotOCIRef,
		kubeconfigPath:       dep.kubeconfigPath,
	})
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

// clusterArgobotOptions carries everything the argobot install needs from the
// surrounding `cub cluster up` run.
type clusterArgobotOptions struct {
	sveltos              bool            // install argobot's Sveltos deployment, from its own base
	clusterName          string          // cluster name; also the argobot variant name
	clusterSpaceID       uuid.UUID       // the cluster Space, where argobot's Channel is created
	clusterSpaceSlug     string          // its slug, the Space part of argobot's CONFIGHUB_CHANNEL
	targetID             uuid.UUID       // the cluster's OCI target, which the Channel subscribes to
	ociTargetRef         string          // "<prefix>/target" — argobot's release target
	serviceAccountUserID uuid.UUID       // the cluster service account's user, granted Claim on the Channel
	privateKey           json.RawMessage // the service account's private key (goes into the out-of-band Secret)
	apiURLForContainer   string          // ConfigHub API URL as argobot (in-cluster) reaches it
	ociRef               string          // argobot config-bundle OCI reference
	kubeconfigPath       string
}

// clusterInstallArgobot installs argobot for a freshly created cluster: argobot
// runs as the cluster's service account (the OCI target grants that service
// account's user the access live-status reporting needs), and the Channel
// argobot listens on is created in the cluster Space, subscribed to the
// Target's deploys and claimable by that service account. The argobot
// Deployment runs in the default kubernetes sync mode — it patches an
// Application's refresh annotation via the in-cluster ServiceAccount, needing
// no Argo CD API token.
//
// It creates argobot's units and publishes argobot's own variant Release. The
// child Argo Application Unit that makes argobot live is created in the apps
// Space by `cub variant create --target` (step 2), auto-derived from the OCI
// target's argo-apps-space annotation — the same path any deployment variant
// uses. The caller publishes the apps Space's Release once, after this
// returns, so that the root app-of-apps' first sync already includes argobot.
func clusterInstallArgobot(out io.Writer, o clusterArgobotOptions) error {
	argobotSpace := clusterArgobotComponent + "-" + o.clusterName
	baseVariant, baseSpace := clusterArgobotBaseVariant, clusterArgobotBaseSpace
	if o.sveltos {
		baseVariant, baseSpace = clusterArgobotSveltosBaseVariant, clusterArgobotSveltosBaseSpace
	}

	// 1. Ensure the shared base component exists, installed from argobot's
	// published OCI config bundle. Re-installing is a no-op: an upload is
	// create-or-update, so a base another cluster already installed is matched
	// against the bundle and left alone rather than conflicting with it. The
	// pull is anonymous — local Docker credentials are never used (see
	// ociAuthClient) — so a stale `docker login ghcr.io` can't turn into a hard
	// 403 here.
	fmt.Fprintf(out, "Installing argobot base component from %s...\n", o.ociRef)
	if err := runCub("variant", "upload",
		"--component", clusterArgobotComponent,
		"--variant", baseVariant,
		o.ociRef); err != nil {
		return fmt.Errorf("install argobot base component: %w", err)
	}

	// 2. Per-cluster downstream variant, cloned from the base and bound to this
	// cluster's OCI target (which also becomes the variant Space's release
	// target). No --namespace: argobot's manifests already place resources in
	// the argobot and argocd namespaces, and run in them on every cluster.
	//
	// Because the OCI target carries the confighub.com/argo-apps-space
	// annotation, this also auto-creates argobot's child Argo Application Unit
	// in the apps Space and republishes it — argobot rides the same path as
	// any deployment variant instead of hand-rolling its own Application.
	//
	// No --allow-exists: `cub variant create` is fail-fast on a pre-existing
	// variant, and clusterUpRun guarantees a fresh argobot Space via its
	// precondition check, so a lingering argobot-<name> is surfaced rather than
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

	// 4. Point argobot at this ConfigHub and its Channel. set-env upserts
	// literal env vars; the key stays a secretKeyRef (supplied out of band,
	// step 6).
	fmt.Fprintln(out, "Configuring argobot (CONFIGHUB_URL, CONFIGHUB_CHANNEL)...")
	if err := runCub("function", "do", "--quiet",
		"--space", argobotSpace, "--unit", clusterArgobotUnitSlug,
		"set-env", clusterArgobotContainer,
		"CONFIGHUB_URL="+o.apiURLForContainer,
		"CONFIGHUB_CHANNEL="+channelRef); err != nil {
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

	// 6. Apply the out-of-band key Secret (and its Namespace) so argobot's pod
	// can authenticate. Kept out of the OCI bundle deliberately.
	fmt.Fprintf(out, "Applying argobot key Secret to the %q namespace...\n", clusterArgobotNamespace)
	if err := clusterKubectlApply(ctx, o.kubeconfigPath,
		clusterArgobotSecretManifest(o.privateKey), out); err != nil {
		return fmt.Errorf("apply argobot secret: %w", err)
	}

	if o.sveltos {
		fmt.Fprintf(out, "argobot installed: variant %q, Sveltos ClusterProfile %q, channel %q\n",
			argobotSpace, argobotSpace, channelRef)
		return nil
	}
	fmt.Fprintf(out, "argobot installed: variant %q, Argo app %q, channel %q (kubernetes sync mode)\n",
		argobotSpace, argobotSpace, channelRef)
	return nil
}
