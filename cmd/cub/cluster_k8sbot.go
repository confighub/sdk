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
	"path"
	"slices"
	"strings"

	"github.com/confighub/installer/pkg/installer"
	"github.com/confighub/sdk/core/cubapi"
	"github.com/confighub/sdk/core/function/api"
	"github.com/confighub/sdk/core/ocibundle"
	goclientnew "github.com/confighub/sdk/core/openapi/goclient-new"
	"github.com/confighub/sdk/core/workerapi"
	"github.com/google/uuid"
	"golang.org/x/term"
	"sigs.k8s.io/yaml"
)

// k8sbot install for `cub cluster attach` (and so `cub cluster up`). k8sbot
// is a ConfigHub bot that listens on a Channel and force-syncs the
// corresponding Argo CD Application the moment a deploy happens, closing Argo's
// reconcile-interval gap. It is delivered like
// any other component: a shared base installed from the deployment its release
// image carries, or with --k8sbot-installer from its installer package, and a
// per-cluster downstream variant. The child Argo Application that
// makes k8sbot live is created the way `cub variant create --target` creates
// one — the cluster's OCI target carries the confighub.com/apps-space
// annotation, so creating the k8sbot variant against it adds the Application
// Unit to the apps Space (see createVariantClusterApp). k8sbot is just the
// first user of that path.
//
// Everything k8sbot-specific lives in this file; the rest of the cluster
// package is generic (kind, Argo CD, ConfigHub Spaces/Targets/ServiceAccounts).
// The one coupling into the attachment is clusterInstallK8sbot, the stage `cub
// cluster k8sbot-install` runs and `cub cluster attach` runs inside
// clusterBootstrapApps.

// k8sbot install constants. k8sbot is installed as a ConfigHub component,
// then given a per-cluster downstream variant.
const (
	// clusterK8sbotComponent is the well-known "Component" label value and the
	// base Space is <component>-base.
	clusterK8sbotComponent = "k8sbot"
	// clusterK8sbotBaseVariant is the "Variant" label of the shared base Space.
	clusterK8sbotBaseVariant = "base"
	// clusterK8sbotBaseSpace is the shared base Space slug (one per hub, reused
	// across clusters).
	clusterK8sbotBaseSpace = "k8sbot-base"
	// The Sveltos counterparts. k8sbot's Sveltos deployment needs different
	// RBAC and its own HealthCheck, so it is a base of its own, beside the Argo
	// CD one.
	clusterK8sbotSveltosBaseVariant = "sveltos-base"
	clusterK8sbotSveltosBaseSpace   = "k8sbot-sveltos-base"
	// The Flux counterparts, for k8sbot's Flux deployment, which needs RBAC of
	// its own.
	clusterK8sbotFluxBaseVariant = "flux-base"
	clusterK8sbotFluxBaseSpace   = "k8sbot-flux-base"
	// clusterK8sbotNamespace is the namespace k8sbot's manifests deploy into.
	clusterK8sbotNamespace = "k8sbot"
	// clusterK8sbotUnitSlug is the Unit holding k8sbot's Deployment. The
	// manifests become one Unit per resource and a workload keeps its bare name, so the
	// Deployment named k8sbot lands in a Unit named k8sbot, while its
	// ServiceAccount, RBAC and Namespace take slugs of their own.
	clusterK8sbotUnitSlug = "k8sbot"
	// clusterK8sbotEnvUnitSlug is the AppConfig Unit holding k8sbot's
	// environment in an install from its installer package through 0.2.8,
	// which ConfigHub renders into the k8sbot-env ConfigMap.
	clusterK8sbotEnvUnitSlug = "k8sbot-env"
	// clusterK8sbotEnvConfigSchema is that environment's configSchema, which
	// set-string-path names to edit it.
	clusterK8sbotEnvConfigSchema = "K8sbot"
	// clusterK8sbotRecordUnitSlug is the hidden Unit an installer upload keeps
	// its answers in. A base holding one was installed with the installer.
	clusterK8sbotRecordUnitSlug = "installer"
	// clusterK8sbotContainer is the container name in k8sbot's Deployment.
	clusterK8sbotContainer = "k8sbot"
	// clusterK8sbotSecret is the out-of-band Secret k8sbot reads its service
	// account key from (never shipped in the OCI bundle).
	clusterK8sbotSecret = "k8sbot-secrets"
	// clusterK8sbotChannelSlug is the Channel k8sbot listens on, created in
	// the cluster Space beside the Target whose deploys it reacts to.
	clusterK8sbotChannelSlug = "k8sbot"
)

// k8sbot was called argobot. A cluster attached by a cub from before the
// rename runs it under that name: its variant Space is argobot-<name>, its key
// is the Secret argobot-secrets in the argobot Namespace, and its target
// carries clusterAnnotationArgobot. cub reads these names to recognize such a
// bot and remove it; it never installs one.
const (
	clusterArgobotComponent = "argobot"
	clusterArgobotNamespace = "argobot"
	clusterArgobotSecret    = "argobot-secrets"
)

// clusterBotKeySecret is the key Secret of the bot whose variant Space is
// botSpace: argobot's for a cluster attached before the rename, k8sbot's
// otherwise. The Secret's Namespace is the one the bot runs in.
func clusterBotKeySecret(botSpace string) clusterSecret {
	if strings.HasPrefix(botSpace, clusterArgobotComponent+"-") {
		return clusterSecret{namespace: clusterArgobotNamespace, name: clusterArgobotSecret}
	}
	return clusterK8sbotKeySecret()
}

// What to do with a shared base Space that already exists (--k8sbot-base).
// Update installs k8sbot's newest release over it before cloning; clone leaves
// it as it is, so every cluster cloned from it runs the same k8sbot. A base
// that does not exist yet is installed either way.
const (
	clusterK8sbotBaseUpdate = "update"
	clusterK8sbotBaseClone  = "clone"
)

// The k8sbot image repositories --registry names. Releases are built on
// ghcr.io; gcr.io carries a copy of each one, for networks that reach Google's
// registry and not GitHub's.
const (
	clusterK8sbotGHCR = "ghcr.io/confighub/k8sbot"
	clusterK8sbotGCR  = "gcr.io/confighubapp/k8sbot"
)

// clusterK8sbotRepository reads --registry: "ghcr", "gcr", or the prefix of a
// mirror holding k8sbot's image as <prefix>/k8sbot. Empty is ghcr. It returns
// the image repository, without a tag. k8sbot's deployment is read from the
// newest release there, and runs k8sbot from the same repository.
func clusterK8sbotRepository(registry string) (string, error) {
	switch registry {
	case "", "ghcr", "ghcr.io":
		return clusterK8sbotGHCR, nil
	case "gcr", "gcr.io":
		return clusterK8sbotGCR, nil
	}
	prefix := strings.TrimSuffix(registry, "/")
	if !strings.Contains(prefix, "/") && !strings.Contains(prefix, ".") {
		return "", fmt.Errorf("--registry %q: expected ghcr, gcr, or a mirror prefix such as registry.example.com/confighub", registry)
	}
	return prefix + "/k8sbot", nil
}

// clusterRegistryFlagUsage is the --registry help of the commands that install
// k8sbot.
const clusterRegistryFlagUsage = "where to pull k8sbot's image from: ghcr, gcr, or a mirror prefix holding <prefix>/k8sbot, and <prefix>/packages/k8sbot for --k8sbot-installer (default ghcr)"

// clusterK8sbotInstallerFlagUsage is the --k8sbot-installer help of the
// commands that install k8sbot.
const clusterK8sbotInstallerFlagUsage = "install k8sbot's shared base Space with ConfigHub's installer, from its installer package, rather than from the deployment its image carries"

// clusterK8sbotBaseFlagUsage is the --k8sbot-base help of the commands that
// install k8sbot.
const clusterK8sbotBaseFlagUsage = "what to do with k8sbot's shared base Space when it already exists: update (install k8sbot's newest release over it, then clone it) or clone (clone it as it is); asked when stdin is a terminal, otherwise update"

// clusterK8sbotPackageRepository is the repository of k8sbot's installer
// package, beside its image: <prefix>/packages/k8sbot.
func clusterK8sbotPackageRepository(repository string) string {
	return path.Dir(repository) + "/packages/" + clusterK8sbotComponent
}

// clusterK8sbotPullOptions are the options k8sbot's image and package are
// read with: anonymously, since both are public and an expired local login
// would otherwise be refused, and over plain HTTP from a local registry.
func clusterK8sbotPullOptions() ocibundle.Options {
	return ocibundle.Options{UserAgent: "cub/" + Version, PlainHTTPLoopback: true}
}

// The event type k8sbot reacts to: a Release published for the cluster's
// Target. It is what k8sbot --print-subscriptions prints; named here so the
// Channel is created without running the bot.
const clusterK8sbotEventReleasePublished = "release.published"

// clusterK8sbotSpec is what installing k8sbot in a cluster needs: the
// cluster's Space, OCI target and apps Space, which k8sbot's Channel and
// Units go in, and the cluster's service account, which it runs as.
type clusterK8sbotSpec struct {
	name           string
	spaceSlug      string
	kubeconfigPath string
	controller     string
	spaceID        uuid.UUID // the cluster Space; k8sbot's Channel lives here, beside the Target
	targetID       uuid.UUID // the cluster's OCI target, which the Channel's subscription is scoped to
	// k8sbotBase is clusterK8sbotBaseUpdate or clusterK8sbotBaseClone, as
	// clusterChooseK8sbotBase decided it.
	k8sbotBase string
	// k8sbotRepository is the image repository k8sbot is pulled from, as
	// clusterK8sbotRepository read --registry.
	k8sbotRepository string
	// k8sbotInstaller installs k8sbot's base with ConfigHub's installer, from
	// k8sbot's installer package, rather than from k8sbot's image.
	k8sbotInstaller bool
	// standalone is true for `cub cluster k8sbot-install` on a cluster attached
	// already: its root runs, so the stage marks the target as k8sbot's,
	// records the bot on the cluster Space, and republishes the apps Space
	// itself. Within an attachment, attach does all three.
	standalone bool
	// secretsDir is --secrets-dir: where the Kubernetes manifest of k8sbot's
	// key Secret is written instead of being applied (clusterPutSecret).
	secretsDir string
}

// clusterInstallK8sbot installs k8sbot in a cluster, as `cub cluster
// k8sbot-install` and as the last stage of `cub cluster attach` before its
// first apps-Space Release. k8sbot authenticates as the cluster's service
// account with a key in the k8sbot-secrets Secret, issued here unless
// svc-account-bootstrap already put one in the cluster. Everything it creates
// is registered with registerRollback.
func clusterInstallK8sbot(out io.Writer, spec clusterK8sbotSpec, registerRollback func(clusterUndo)) error {
	serviceAccountSlug := clusterServiceAccountSlug(spec.name)
	serviceAccountID, serviceAccountUserID, err := clusterServiceAccountIDs(serviceAccountSlug)
	if err != nil {
		return err
	}
	hasKey, err := clusterHasKeySecret(spec.kubeconfigPath, clusterK8sbotKeySecret())
	if err != nil {
		return err
	}
	keyFile := ""
	if spec.secretsDir != "" {
		// The service account stage wrote the key for the secret store, which
		// may not have put it in the cluster yet. A second key would replace
		// the file under an operator who has loaded the first.
		path := clusterSecretFilePath(spec.secretsDir, clusterK8sbotKeySecret())
		if _, err := os.Stat(path); err == nil {
			keyFile = path
		} else if !os.IsNotExist(err) {
			return fmt.Errorf("check for k8sbot's key file: %w", err)
		}
	}
	switch {
	case keyFile != "":
		// A file of an earlier attachment of this name holds a key of a service
		// account deleted since, which k8sbot would fail to authenticate with.
		manifest, err := os.ReadFile(keyFile)
		if err != nil {
			return fmt.Errorf("read k8sbot's key file: %w", err)
		}
		key, err := clusterK8sbotKeyInManifest(manifest)
		if err == nil {
			err = clusterCheckK8sbotKey(key, serviceAccountUserID, serviceAccountSlug)
		}
		if err != nil {
			return fmt.Errorf("k8sbot's key file %s: %w; remove the file for a key to be issued", keyFile, err)
		}
		fmt.Fprintf(out, "Using k8sbot's key from %s, which the secret store is to put in the cluster\n", keyFile)
	case hasKey:
		// A Secret a secret store keeps is used as it is, so check it holds
		// what k8sbot reads before k8sbot is deployed to read it.
		if err := clusterRequireK8sbotKeyField(spec.kubeconfigPath); err != nil {
			return err
		}
		fmt.Fprintf(out, "Using k8sbot's key from the Secret %s/%s already in the cluster\n", clusterK8sbotNamespace, clusterK8sbotSecret)
	default:
		fmt.Fprintf(out, "Issuing a key for k8sbot on %q...\n", serviceAccountSlug)
		privateKey, err := clusterCreateServiceAccountKey(serviceAccountID, serviceAccountUserID, "k8sbot in cluster "+spec.name)
		if err != nil {
			return fmt.Errorf("issue the service account key: %w", err)
		}
		if err := clusterApplyK8sbotKey(out, spec.kubeconfigPath, spec.secretsDir, privateKey, registerRollback); err != nil {
			return err
		}
	}

	appsSlug := spec.spaceSlug + clusterAppsSuffix(spec.controller)
	k8sbotSlug := clusterK8sbotComponent + "-" + spec.name
	if spec.standalone {
		// `cub variant create` reads the annotation to decide whether k8sbot's
		// own deployment is released at once (Flux).
		if _, err := clusterGrantTarget(spec.spaceID, spec.targetID, uuid.Nil,
			map[string]string{clusterAnnotationK8sbot: "true"}); err != nil {
			return fmt.Errorf("mark the target as k8sbot's: %w", err)
		}
		registerRollback(clusterUndo{
			what: fmt.Sprintf("annotation %s on target %s/%s", clusterAnnotationK8sbot, spec.spaceSlug, clusterTargetSlug),
			fix: []string{fmt.Sprintf("cub target update --patch %s/%s --annotation %s=-",
				spec.spaceSlug, clusterTargetSlug, clusterAnnotationK8sbot)},
			run: func() error {
				fmt.Fprintf(out, "Rolling back: removing annotation %s from target %s/%s\n", clusterAnnotationK8sbot, spec.spaceSlug, clusterTargetSlug)
				return clusterRemoveTargetAnnotation(spec.spaceID, spec.targetID, clusterAnnotationK8sbot)
			},
		})
		// The root deploys the apps Space's Release, which `cub variant create`
		// adds k8sbot's Unit to: take it back out so the root drops k8sbot.
		registerRollback(clusterUndo{
			what: fmt.Sprintf("k8sbot's Unit %s/%s, in the apps space's Release", appsSlug, k8sbotSlug),
			fix: []string{
				fmt.Sprintf("cub unit delete %s/%s", appsSlug, k8sbotSlug),
				fmt.Sprintf("cub release publish %s", appsSlug),
			},
			run: func() error {
				appsSpace, err := resolveSpace(appsSlug, "SpaceID")
				if cubapi.IsNotFoundError(err) {
					return nil
				} else if err != nil {
					return err
				}
				unit, err := resolveUnit(k8sbotSlug, appsSpace.Space.SpaceID.String(), "UnitID")
				if cubapi.IsNotFoundError(err) {
					return nil // never added
				} else if err != nil {
					return err
				}
				fmt.Fprintf(out, "Rolling back: removing k8sbot's Unit from apps space %q\n", appsSlug)
				res, err := cubClientNew.DeleteUnitWithResponse(ctx, appsSpace.Space.SpaceID, unit.Unit.UnitID, &goclientnew.DeleteUnitParams{})
				if cubapi.IsAPIError(err, res) {
					return cubapi.InterpretErrorGeneric(err, res)
				}
				if err := clusterPublishRelease(appsSpace.Space.SpaceID); err != nil {
					return &clusterUndoPartial{
						fix: []string{fmt.Sprintf("cub release publish %s", appsSlug)},
						err: fmt.Errorf("k8sbot's Unit is deleted, but publishing the apps space's Release without it failed: %w", err),
					}
				}
				return nil
			},
		})
		// Within an attachment the Channel goes with the cluster Space.
		registerRollback(clusterUndo{
			what: fmt.Sprintf("Channel %s/%s", spec.spaceSlug, clusterK8sbotChannelSlug),
			fix:  []string{fmt.Sprintf("cub channel delete %s/%s", spec.spaceSlug, clusterK8sbotChannelSlug)},
			run: func() error {
				channel, err := resolveChannel(clusterK8sbotChannelSlug, spec.spaceID.String(), "ChannelID")
				if cubapi.IsNotFoundError(err) {
					return nil // never created
				} else if err != nil {
					return err
				}
				fmt.Fprintf(out, "Rolling back: cub channel delete %s/%s\n", spec.spaceSlug, clusterK8sbotChannelSlug)
				res, err := cubClientNew.DeleteChannelWithResponse(ctx, spec.spaceID, channel.Channel.ChannelID, nil)
				if cubapi.IsAPIError(err, res) {
					return cubapi.InterpretErrorGeneric(err, res)
				}
				return nil
			},
		})
		// Before `cub variant create`, so k8sbot's own deployment polls like
		// the root. Its rollbacks run before the ones above, so the apps-Space
		// publish that takes k8sbot back out carries the old root.
		if err := clusterPollWithK8sbot(out, spec, appsSlug, registerRollback); err != nil {
			return err
		}
	}

	// Register the per-cluster k8sbot variant Space for rollback before the
	// install runs: a partial failure can leave it with a cross-space
	// release-target reference that would otherwise block deleting the cluster's
	// OCI target. Resolve by slug at rollback time (it may not exist yet). The
	// shared k8sbot-base Space is intentionally left alone.
	registerRollback(clusterUndoDeleteSpace(out, k8sbotSlug, true))

	// ConfigHub API URL as k8sbot (in-cluster) reaches it — loopback rewritten
	// to the docker host address, since a loopback cub server can only be
	// reached by a cluster on this machine. A remote server passes through.
	apiURLForContainer, err := clusterAPIEndpointFromContainer(clusterServerURL())
	if err != nil {
		return err
	}

	switch spec.controller {
	case clusterControllerSveltos:
		fmt.Fprintln(out, "\nInstalling k8sbot (event-driven Sveltos redeploy)...")
	case clusterControllerFlux:
		fmt.Fprintln(out, "\nInstalling k8sbot (event-driven Flux reconciliation)...")
	default:
		fmt.Fprintln(out, "\nInstalling k8sbot (event-driven Argo CD sync)...")
	}
	if err := clusterDeployK8sbot(out, clusterK8sbotOptions{
		controller:           spec.controller,
		clusterName:          spec.name,
		clusterSpaceID:       spec.spaceID,
		clusterSpaceSlug:     spec.spaceSlug,
		targetID:             spec.targetID,
		ociTargetRef:         spec.spaceSlug + "/" + clusterTargetSlug,
		serviceAccountUserID: serviceAccountUserID,
		apiURLForContainer:   apiURLForContainer,
		kubeconfigPath:       spec.kubeconfigPath,
		baseMode:             spec.k8sbotBase,
		repository:           spec.k8sbotRepository,
		installer:            spec.k8sbotInstaller,
	}); err != nil {
		return err
	}
	if !spec.standalone {
		return nil
	}

	// The root deploys k8sbot from the apps Space's Release: publish it with
	// k8sbot's Unit in it, whichever controller's `cub variant create` left it
	// unpublished, and with Argo CD ask the root to re-read it now rather than
	// on its next poll.
	appsSpace, err := resolveSpace(appsSlug, "SpaceID")
	if err != nil {
		return fmt.Errorf("resolve apps space %q: %w", appsSlug, err)
	}
	fmt.Fprintf(out, "Publishing the apps space's release (adds k8sbot to what the root deploys)...\n")
	if err := clusterPublishRelease(appsSpace.Space.SpaceID); err != nil {
		return err
	}
	if spec.controller == clusterControllerArgo {
		if err := clusterKubectl(ctx, spec.kubeconfigPath, nil, io.Discard,
			"annotate", "application", appsSlug, "-n", clusterArgoNamespace, "argocd.argoproj.io/refresh=hard", "--overwrite"); err != nil {
			fmt.Fprintf(out, "  warning: refresh root Application %q: %v\n", appsSlug, err)
		}
	}
	return clusterFinishSpaceAnnotations(spec.spaceID, k8sbotSlug, clusterK8sbotChannelSlug)
}

// clusterPollWithK8sbot moves a Sveltos or Flux cluster attached without
// k8sbot to the poll interval it has with k8sbot, as if attached with it:
// infrequent, since k8sbot has each Release deployed when it is published. The
// interval goes on the target, which deployments created from here on take it
// from (k8sbot's own first), and on the root Unit, which the apps-Space
// publish after k8sbot's install carries to the cluster. Deployments that
// already exist keep polling every minute. Argo CD has no interval of cub's to
// change.
func clusterPollWithK8sbot(out io.Writer, spec clusterK8sbotSpec, appsSlug string, registerRollback func(clusterUndo)) error {
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

	fmt.Fprintf(out, "Moving the cluster's poll interval to %s, as with k8sbot...\n", interval)
	if _, err := clusterGrantTarget(spec.spaceID, spec.targetID, uuid.Nil, map[string]string{key: interval}); err != nil {
		return fmt.Errorf("record the poll interval on the target: %w", err)
	}
	restoreTo := old
	if old == "" {
		restoreTo = "-"
	}
	registerRollback(clusterUndo{
		what: fmt.Sprintf("annotation %s=%s on target %s/%s", key, interval, spec.spaceSlug, clusterTargetSlug),
		fix: []string{fmt.Sprintf("cub target update --patch %s/%s --annotation %s=%s",
			spec.spaceSlug, clusterTargetSlug, key, restoreTo)},
		run: func() error {
			fmt.Fprintf(out, "Rolling back: restoring annotation %s on target %s/%s\n", key, spec.spaceSlug, clusterTargetSlug)
			if old == "" {
				return clusterRemoveTargetAnnotation(spec.spaceID, spec.targetID, key)
			}
			_, err := clusterGrantTarget(spec.spaceID, spec.targetID, uuid.Nil, map[string]string{key: old})
			return err
		},
	})

	appsSpace, err := resolveSpace(appsSlug, "SpaceID")
	if err != nil {
		return fmt.Errorf("resolve apps space %q: %w", appsSlug, err)
	}
	appsSpaceID := appsSpace.Space.SpaceID
	root, err := resolveUnit(clusterRootUnitSlug, appsSpaceID.String(), "UnitID,HeadRevisionNum")
	if err != nil {
		return fmt.Errorf("resolve root unit in apps space %q: %w", appsSlug, err)
	}
	rootUnitID := root.Unit.UnitID
	rootRevision := root.Unit.HeadRevisionNum
	ociURLForCluster, insecure, err := clusterControllerOCIEndpoint()
	if err != nil {
		return err
	}
	rootRepoURL := strings.TrimRight(ociURLForCluster, "/") + "/space/" + appsSlug
	if _, err := putUnitData(appsSpaceID, rootUnitID, string(render(interval, rootRepoURL, insecure, prune)), nil); err != nil {
		return fmt.Errorf("update root unit: %w", err)
	}
	registerRollback(clusterUndo{
		what: fmt.Sprintf("the root Unit %s/%s at the %s poll interval", appsSlug, clusterRootUnitSlug, interval),
		fix: []string{
			fmt.Sprintf("cub unit update --space %s %s --restore %d", appsSlug, clusterRootUnitSlug, rootRevision),
			fmt.Sprintf("cub release publish %s", appsSlug),
		},
		run: func() error {
			previous := old
			if previous == "" {
				previous = clusterSveltosPollInterval(false)
				if spec.controller == clusterControllerFlux {
					previous = clusterFluxPollInterval(false)
				}
			}
			fmt.Fprintf(out, "Rolling back: moving root Unit %s/%s back to the %s poll interval\n", appsSlug, clusterRootUnitSlug, previous)
			if _, err := putUnitData(appsSpaceID, rootUnitID, string(render(previous, rootRepoURL, insecure, prune)), nil); err != nil {
				return err
			}
			// The root is restored by now. The wait only lets the publish a
			// later step makes find no pending Triggers; if that publish is
			// refused for it, that step reports it.
			if err := clusterWaitUnitTriggers(appsSpaceID, rootUnitID); err != nil {
				fmt.Fprintf(out, "  note: the root's Triggers had not finished evaluating: %v\n", err)
			}
			return nil
		},
	})
	return clusterWaitUnitTriggers(appsSpaceID, rootUnitID)
}

// clusterApplyK8sbotKey applies k8sbot's key to the cluster, as the
// k8sbot-secrets Secret in the k8sbot namespace (created with it), where
// k8sbot reads CONFIGHUB_AUTH_PRIVATE_KEY from. Applied straight to the
// cluster so the key never enters a published Release, and before k8sbot's
// Deployment so it is there when the pod starts. With secretsDir set the
// Secret is written there instead (clusterPutSecret), and only the namespace is
// applied, for the secret store to put the Secret in.
func clusterApplyK8sbotKey(out io.Writer, kubeconfigPath, secretsDir string, privateKey json.RawMessage, registerRollback func(clusterUndo)) error {
	if secretsDir != "" {
		fmt.Fprintf(out, "Applying the %q namespace...\n", clusterK8sbotNamespace)
		if err := clusterKubectlApply(ctx, kubeconfigPath, clusterK8sbotNamespaceManifest(), out); err != nil {
			return fmt.Errorf("apply the %s namespace: %w", clusterK8sbotNamespace, err)
		}
	}
	return clusterPutSecret(out, kubeconfigPath, secretsDir, clusterSecret{
		namespace: clusterK8sbotNamespace,
		name:      clusterK8sbotSecret,
		manifest:  clusterK8sbotSecretManifest(privateKey),
	}, registerRollback)
}

// clusterK8sbotNamespaceManifest is the k8sbot namespace, the first document
// of clusterK8sbotSecretManifest.
func clusterK8sbotNamespaceManifest() []byte {
	return fmt.Appendf(nil, "apiVersion: v1\nkind: Namespace\nmetadata:\n  name: %s\n", clusterK8sbotNamespace)
}

// clusterK8sbotSubscriptions is what k8sbot's Channel subscribes to: the
// release events of the cluster's Target, so a bot reacts to its own cluster's
// deploys and nothing else.
func clusterK8sbotSubscriptions(targetID uuid.UUID) []goclientnew.EventSubscription {
	target := goclientnew.UUID(targetID)
	return []goclientnew.EventSubscription{
		{
			EventTypes: []string{clusterK8sbotEventReleasePublished},
			TargetID:   &target,
		},
	}
}

// clusterCreateK8sbotChannel creates k8sbot's Channel in the cluster Space,
// subscribed to the cluster Target's deploys, and grants Claim to the cluster
// service account's user, which is what lets k8sbot, running as that service
// account, claim and poll it. The Channel is a mailbox with no identity of its
// own: what it delivers is set here, and k8sbot only needs to be told its name.
func clusterCreateK8sbotChannel(spaceID, targetID, serviceAccountUserID uuid.UUID) (uuid.UUID, error) {
	permissions := goclientnew.Permissions{
		"Claim": {UserIDs: map[string]bool{serviceAccountUserID.String(): true}},
	}
	body := goclientnew.Channel{
		SpaceID:       spaceID,
		Slug:          clusterK8sbotChannelSlug,
		DisplayName:   clusterK8sbotChannelSlug,
		Permissions:   &permissions,
		Subscriptions: clusterK8sbotSubscriptions(targetID),
	}
	res, err := cubClientNew.CreateChannelWithResponse(ctx, spaceID, &goclientnew.CreateChannelParams{}, body)
	if cubapi.IsAPIError(err, res) {
		return uuid.Nil, cubapi.InterpretErrorGeneric(err, res)
	}
	return res.JSON200.ChannelID, nil
}

// clusterK8sbotSecretManifest returns the YAML for the out-of-band Secret that
// carries k8sbot's service account key, as the private JWK k8sbot reads from
// CONFIGHUB_AUTH_PRIVATE_KEY. It is applied straight to the cluster via kubectl
// (never shipped in the OCI bundle) so credential material never enters a
// published Release. The Namespace is created alongside so the Secret has
// somewhere to land before Argo renders k8sbot's Deployment.
//
// The JWK goes in as a literal block scalar: one JSON line, indented, with no
// quoting to get wrong. A JWK carries no newline of its own.
func clusterK8sbotSecretManifest(privateKey json.RawMessage) []byte {
	return fmt.Appendf(clusterK8sbotNamespaceManifest(), `---
apiVersion: v1
kind: Secret
metadata:
  name: %s
  namespace: %s
type: Opaque
stringData:
  CONFIGHUB_AUTH_PRIVATE_KEY: |-
    %s
`, clusterK8sbotSecret, clusterK8sbotNamespace, bytes.TrimSpace(privateKey))
}

// clusterK8sbotManifestPath is where k8sbot's image carries its deployment
// for a controller.
func clusterK8sbotManifestPath(controller string) string {
	switch controller {
	case clusterControllerSveltos:
		return "/manifests/sveltos.yaml"
	case clusterControllerFlux:
		return "/manifests/flux.yaml"
	}
	return "/manifests/argocd.yaml"
}

// clusterK8sbotManifests reads k8sbot's deployment for a controller from the
// newest k8sbot release in repository, without running the image. The
// deployment runs the release it ships in, from ghcr.io; it is returned running
// it from repository.
func clusterK8sbotManifests(controller, repository string, out io.Writer) (manifests []byte, ref string, err error) {
	tag, err := ocibundle.NewestRelease(ctx, repository, clusterK8sbotPullOptions())
	if err != nil {
		return nil, "", fmt.Errorf("find k8sbot's newest release: %w", err)
	}
	ref = repository + ":" + tag
	fmt.Fprintf(out, "Reading k8sbot's deployment from %s...\n", ref)
	manifests, _, err = ocibundle.ReadImageFile(ctx, ref, clusterK8sbotManifestPath(controller), clusterK8sbotPullOptions())
	if err != nil {
		return nil, "", fmt.Errorf("read k8sbot's deployment: %w", err)
	}
	if repository != clusterK8sbotGHCR {
		manifests = bytes.ReplaceAll(manifests, []byte("image: "+clusterK8sbotGHCR+":"), []byte("image: "+repository+":"))
	}
	return manifests, ref, nil
}

// clusterK8sbotPackageComponent is the installer package's component for a
// controller.
func clusterK8sbotPackageComponent(controller string) string {
	if controller == clusterControllerArgo {
		return "argocd"
	}
	return controller
}

// clusterK8sbotInstallBase installs k8sbot's shared base Space with the
// installer, from the newest release of k8sbot's installer package: no
// Channel, Target or URL, which each cluster's variant sets. Setup is given the
// base Space, so it reads its earlier answers from that Space's installer
// record, and never another install of k8sbot's; the upload creates the Space
// when it doesn't exist yet.
func clusterK8sbotInstallBase(out io.Writer, controller, repository string) error {
	baseVariant, baseSpace := clusterK8sbotBase(controller)
	packageRepository := clusterK8sbotPackageRepository(repository)
	version, err := ocibundle.NewestRelease(ctx, packageRepository, clusterK8sbotPullOptions())
	if err != nil {
		return fmt.Errorf("find k8sbot's newest installer package: %w", err)
	}
	workDir, err := os.MkdirTemp("", "cub-k8sbot-base-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(workDir)

	// Read anonymously, as the newest release was: the package is public.
	inst, err := newCubInstaller(out, true)
	if err != nil {
		return err
	}
	ref := ocibundle.Scheme + packageRepository + ":" + version
	opts := installer.InstallOptions{
		Space: baseSpace,
		Setup: installer.SetupOptions{
			WorkDir:   workDir,
			Pull:      ref,
			Namespace: clusterK8sbotNamespace,
			Select:    []string{clusterK8sbotPackageComponent(controller)},
		},
		Upload: installer.UploadOptions{Variant: baseVariant},
	}
	if repository != clusterK8sbotGHCR {
		// The package runs the k8sbot release of its own version.
		opts.Setup.SetImage = map[string]string{clusterK8sbotGHCR: repository + ":v" + version}
	}
	fmt.Fprintf(out, "Installing k8sbot base component from %s with the installer...\n", ref)
	// Install prints what the upload did to out.
	result, err := inst.Install(ctx, opts)
	if err != nil {
		return fmt.Errorf("install k8sbot's base: %w", err)
	}
	if result.Upload.Failed() {
		return fmt.Errorf("install k8sbot's base: some writes failed; the rest landed, and installing again retries the failures")
	}
	return nil
}

// clusterK8sbotOptions carries everything the k8sbot install needs from the
// surrounding attachment.
type clusterK8sbotOptions struct {
	controller           string    // the cluster's controller; k8sbot's Sveltos and Flux deployments come from bases of their own
	clusterName          string    // cluster name; also the k8sbot variant name
	clusterSpaceID       uuid.UUID // the cluster Space, where k8sbot's Channel is created
	clusterSpaceSlug     string    // its slug, the Space part of k8sbot's CONFIGHUB_CHANNEL
	targetID             uuid.UUID // the cluster's OCI target, which the Channel subscribes to
	ociTargetRef         string    // "<prefix>/target" — k8sbot's release target, and its CONFIGHUB_TARGET
	serviceAccountUserID uuid.UUID // the cluster service account's user, granted Claim on the Channel
	apiURLForContainer   string    // ConfigHub API URL as k8sbot (in-cluster) reaches it
	kubeconfigPath       string
	baseMode             string // clusterK8sbotBaseUpdate or clusterK8sbotBaseClone, from clusterChooseK8sbotBase
	repository           string // k8sbot's image repository, from clusterK8sbotRepository
	installer            bool   // install the base with ConfigHub's installer, from k8sbot's installer package, rather than from k8sbot's image
}

// clusterDeployK8sbot deploys k8sbot for a freshly attached cluster: k8sbot
// runs as the cluster's service account (the OCI target grants that service
// account's user the access live-status reporting needs), and the Channel
// k8sbot listens on is created in the cluster Space, subscribed to the
// Target's deploys and claimable by that service account. The k8sbot
// Deployment patches an Application's refresh annotation via the in-cluster
// ServiceAccount, needing no Argo CD API token.
//
// It creates k8sbot's units and publishes k8sbot's own variant Release. The
// child Argo Application Unit that makes k8sbot live is created in the apps
// Space as `cub variant create --target` creates it (step 2), derived from the OCI
// target's apps-space annotation — the same path any deployment variant
// uses. The caller publishes the apps Space's Release once, after this
// returns, so that the root app-of-apps' first sync already includes k8sbot.
func clusterDeployK8sbot(out io.Writer, o clusterK8sbotOptions) error {
	k8sbotSpace := clusterK8sbotComponent + "-" + o.clusterName
	baseVariant, baseSpace := clusterK8sbotBase(o.controller)

	// 1. Ensure the shared base component exists. With --k8sbot-base clone an
	// existing base is not updated.
	switch {
	case o.installer:
		// The installer's upload is create-or-update, and reads the base's
		// earlier answers from its installer record.
		if o.baseMode == clusterK8sbotBaseClone {
			exists, err := clusterSpaceExists(baseSpace)
			if err != nil {
				return err
			}
			if exists {
				fmt.Fprintf(out, "Cloning the existing k8sbot base %q as it is...\n", baseSpace)
				break
			}
		}
		if err := clusterK8sbotInstallBase(out, o.controller, o.repository); err != nil {
			return fmt.Errorf("install k8sbot base component: %w", err)
		}
	default:
		// Installed from the deployment k8sbot's newest release carries for
		// this controller. An upload is create-or-update, so updating a base
		// another cluster already installed changes only what differs. Either
		// way the base is checked first for Units no upload owns that define
		// k8sbot's resources, since a variant cloned from it would apply both.
		manifests, ref, err := clusterK8sbotManifests(o.controller, o.repository, out)
		if err != nil {
			return err
		}
		upload := clusterK8sbotBaseUpload(baseVariant, baseSpace, manifests, ref)
		if err := clusterK8sbotCheckBase(baseSpace, upload); err != nil {
			return err
		}
		if o.baseMode == clusterK8sbotBaseClone {
			fmt.Fprintf(out, "Cloning the existing k8sbot base %q as it is...\n", baseSpace)
			break
		}
		fmt.Fprintln(out, "Installing k8sbot base component...")
		if err := clusterK8sbotUploadBase(upload); err != nil {
			return fmt.Errorf("install k8sbot base component: %w", err)
		}
	}

	// 2. Per-cluster downstream variant, cloned from the base and bound to this
	// cluster's OCI target (which also becomes the variant Space's release
	// target). No namespace: k8sbot's manifests already place resources in
	// the k8sbot and argocd namespaces, and run in them on every cluster.
	//
	// Because the OCI target carries the confighub.com/apps-space
	// annotation, this also auto-creates k8sbot's child Argo Application Unit
	// in the apps Space and republishes it — k8sbot rides the same path as
	// any deployment variant instead of hand-rolling its own Application.
	//
	// Not AllowExists: the create is fail-fast on a pre-existing variant, and
	// the attachment's precondition check guarantees a fresh k8sbot Space, so
	// a lingering k8sbot-<name> is surfaced rather than silently reused.
	fmt.Fprintf(out, "Creating per-cluster k8sbot variant %q...\n", k8sbotSpace)
	space, err := clusterCreateK8sbotVariant(out, o, baseSpace)
	if err != nil {
		return fmt.Errorf("create k8sbot variant: %w", err)
	}

	// 3. The Channel k8sbot listens on, beside the Target in the cluster Space:
	// subscribed to that Target's apply and release events, claimable by the
	// service account k8sbot runs as. The cluster Space's rollback deletes it
	// with the Space.
	channelRef := o.clusterSpaceSlug + "/" + clusterK8sbotChannelSlug
	fmt.Fprintf(out, "Creating k8sbot channel %q (subscribed to the target's deploys)...\n", channelRef)
	if _, err := clusterCreateK8sbotChannel(o.clusterSpaceID, o.targetID, o.serviceAccountUserID); err != nil {
		return fmt.Errorf("create k8sbot channel: %w", err)
	}

	// 4. Point k8sbot at this ConfigHub, its Channel, and the Target it
	// stores the cluster's facts on, which the base leaves as placeholders. The
	// key stays a secretKeyRef to the Secret clusterBootstrapServiceAccount
	// applied out of band.
	fmt.Fprintln(out, "Configuring k8sbot (CONFIGHUB_URL, CONFIGHUB_CHANNEL, CONFIGHUB_TARGET)...")
	if err := clusterConfigureK8sbot(space.SpaceID, o.installer, map[string]string{
		"CONFIGHUB_URL":     o.apiURLForContainer,
		"CONFIGHUB_CHANNEL": channelRef,
		"CONFIGHUB_TARGET":  o.ociTargetRef,
	}); err != nil {
		return fmt.Errorf("configure k8sbot env: %w", err)
	}

	// 5. Publish the variant's Release; Argo pulls k8sbot's workload from here.
	// Every Unit's triggers settle first, not only the Deployment's: from an
	// installer package with the k8sbot-env AppConfig Unit the edit also
	// re-renders that ConfigMap, whose placeholders the cluster's gate checks
	// again.
	units, err := apiListUnits(space.SpaceID.String(), "", "UnitID")
	if err != nil {
		return fmt.Errorf("list k8sbot's units: %w", err)
	}
	for _, unit := range units {
		if err := clusterWaitUnitTriggers(space.SpaceID, unit.UnitID); err != nil {
			return err
		}
	}
	fmt.Fprintf(out, "Publishing k8sbot variant Release (%s)...\n", k8sbotSpace)
	if err := clusterPublishRelease(space.SpaceID); err != nil {
		return err
	}

	switch o.controller {
	case clusterControllerSveltos:
		fmt.Fprintf(out, "k8sbot installed: variant %q, Sveltos ClusterProfile %q, channel %q\n",
			k8sbotSpace, k8sbotSpace, channelRef)
		return nil
	case clusterControllerFlux:
		fmt.Fprintf(out, "k8sbot installed: variant %q, Flux Kustomization %q, channel %q\n",
			k8sbotSpace, k8sbotSpace, channelRef)
		return nil
	}
	fmt.Fprintf(out, "k8sbot installed: variant %q, Argo app %q, channel %q\n",
		k8sbotSpace, k8sbotSpace, channelRef)
	return nil
}

// clusterConfigureK8sbot sets variables of k8sbot's environment in its
// variant: the Deployment's env, which set-env upserts. k8sbot's installer
// package through 0.2.8 kept them in the k8sbot-env AppConfig Unit instead,
// which ConfigHub renders into the ConfigMap the Deployment reads, so from the
// installer they are set there when the variant has that Unit.
//
// The variables are set in one invocation, so the Unit gets one Revision for
// all of them.
func clusterConfigureK8sbot(k8sbotSpaceID uuid.UUID, fromInstaller bool, env map[string]string) error {
	names := slices.Sorted(maps.Keys(env))
	if fromInstaller {
		var fns []api.FunctionInvocation
		for _, name := range names {
			fns = append(fns, api.FunctionInvocation{FunctionName: "set-string-path", Arguments: []api.FunctionArgument{
				{Value: clusterK8sbotEnvConfigSchema}, {Value: name}, {Value: env[name]},
			}})
		}
		found, err := clusterInvokeK8sbotUnit(k8sbotSpaceID, clusterK8sbotEnvUnitSlug, string(workerapi.ToolchainAppConfigEnv), fns)
		if err != nil || found {
			return err
		}
	}
	args := []api.FunctionArgument{{Value: clusterK8sbotContainer}}
	for _, name := range names {
		args = append(args, api.FunctionArgument{Value: name + "=" + env[name]})
	}
	found, err := clusterInvokeK8sbotUnit(k8sbotSpaceID, clusterK8sbotUnitSlug, cubapi.DefaultToolchainType,
		[]api.FunctionInvocation{{FunctionName: "set-env", Arguments: args}})
	if err != nil {
		return err
	}
	if !found {
		return fmt.Errorf("k8sbot's variant has no unit %q", clusterK8sbotUnitSlug)
	}
	return nil
}

// clusterInvokeK8sbotUnit runs fns on the Unit of k8sbot's variant with that
// slug and toolchain, and reports whether there is one.
func clusterInvokeK8sbotUnit(k8sbotSpaceID uuid.UUID, unitSlug, toolchain string, fns []api.FunctionInvocation) (bool, error) {
	result, err := cubapi.InvokeFunctions(ctx, cubClient, fns, cubapi.Selector{
		Where:         fmt.Sprintf("SpaceID = '%s' AND Slug = '%s'", k8sbotSpaceID, unitSlug),
		ToolchainType: toolchain,
	}, cubapi.Change{Description: "Configure k8sbot for this cluster"})
	if err != nil {
		return false, err
	}
	if len(result.Outcomes) == 0 {
		return false, nil
	}
	if failed := result.Failed(); len(failed) > 0 {
		return true, fmt.Errorf("unit %s: %s", unitSlug, failed[0].Error)
	}
	return true, nil
}

// clusterK8sbotBase returns the Variant label and slug of the shared base Space
// for a controller.
func clusterK8sbotBase(controller string) (variant, space string) {
	switch controller {
	case clusterControllerSveltos:
		return clusterK8sbotSveltosBaseVariant, clusterK8sbotSveltosBaseSpace
	case clusterControllerFlux:
		return clusterK8sbotFluxBaseVariant, clusterK8sbotFluxBaseSpace
	}
	return clusterK8sbotBaseVariant, clusterK8sbotBaseSpace
}

// clusterChooseK8sbotBase decides what the install does with the shared base
// Space: the --k8sbot-base value when given, and otherwise, when the base exists
// and stdin is a terminal, the answer to a question. It runs with the other
// preconditions, before anything is created, so the question is never asked
// halfway through. Without a terminal it is update, which is what every run did
// before the question existed.
func clusterChooseK8sbotBase(out io.Writer, controller, flag string) (string, error) {
	switch flag {
	case clusterK8sbotBaseUpdate, clusterK8sbotBaseClone:
		return flag, nil
	case "":
	default:
		return "", fmt.Errorf("--k8sbot-base %q: must be %s or %s", flag, clusterK8sbotBaseUpdate, clusterK8sbotBaseClone)
	}
	_, baseSpace := clusterK8sbotBase(controller)
	exists, err := clusterSpaceExists(baseSpace)
	if err != nil {
		return "", err
	}
	if !exists || !term.IsTerminal(int(os.Stdin.Fd())) {
		return clusterK8sbotBaseUpdate, nil
	}

	current := clusterK8sbotBaseImage(baseSpace)
	if current == "" {
		current = "unknown"
	}
	fmt.Fprintf(out, "The shared k8sbot base %q already exists; it runs %s.\n", baseSpace, current)
	fmt.Fprintln(out, "  [u] update it to k8sbot's newest release, then clone it (default)")
	fmt.Fprintln(out, "  [c] clone it as it is")
	fmt.Fprint(out, "Update or clone? [U/c]: ")
	answer, err := bufio.NewReader(os.Stdin).ReadString('\n')
	if err != nil {
		return "", fmt.Errorf("read answer: %w", err)
	}
	switch strings.ToLower(strings.TrimSpace(answer)) {
	case "", "u", "update":
		return clusterK8sbotBaseUpdate, nil
	case "c", "clone":
		return clusterK8sbotBaseClone, nil
	default:
		return "", fmt.Errorf("cancelled: answer u or c, or pass --k8sbot-base %s|%s", clusterK8sbotBaseUpdate, clusterK8sbotBaseClone)
	}
}

// clusterK8sbotCheckBaseKind refuses an existing base installed the other way
// than this install: with the installer, which leaves its hidden installer
// record in the base, or from k8sbot's image, which leaves k8sbot's
// Deployment Unit and no record. The two installs write different Units, so
// one over the other would leave both. It runs with the other preconditions.
func clusterK8sbotCheckBaseKind(controller string, withInstaller bool) error {
	_, baseSpace := clusterK8sbotBase(controller)
	exists, err := clusterSpaceExists(baseSpace)
	if err != nil || !exists {
		return err
	}
	space, err := resolveSpace(baseSpace, "SpaceID")
	if err != nil {
		return err
	}
	spaceID := space.Space.SpaceID.String()
	_, err = resolveUnit(clusterK8sbotRecordUnitSlug, spaceID, "UnitID")
	hasRecord := err == nil
	switch {
	case withInstaller && !hasRecord:
		if _, err := resolveUnit(clusterK8sbotUnitSlug, spaceID, "UnitID"); err == nil {
			return fmt.Errorf("k8sbot base %q was installed from k8sbot's image, not with the installer; install without --k8sbot-installer, or delete the base (cub space delete --recursive %s) to install it with the installer", baseSpace, baseSpace)
		}
	case !withInstaller && hasRecord:
		return fmt.Errorf("k8sbot base %q was installed with the installer; install with --k8sbot-installer, or delete the base (cub space delete --recursive %s) to install it from k8sbot's image", baseSpace, baseSpace)
	}
	return nil
}

// clusterK8sbotBaseImage is the image k8sbot's Deployment names in the base
// Space, for the question; "" when it cannot be read.
func clusterK8sbotBaseImage(baseSpace string) string {
	space, err := resolveSpace(baseSpace, "SpaceID")
	if err != nil {
		return ""
	}
	unit, err := resolveUnit(clusterK8sbotUnitSlug, space.Space.SpaceID.String(), "UnitID")
	if err != nil {
		return ""
	}
	data, err := fetchUnitData(space.Space.SpaceID, unit.Unit.UnitID)
	if err != nil {
		return ""
	}
	return clusterK8sbotImageIn(data)
}

// clusterK8sbotImageIn finds the image of k8sbot's container in a Unit's
// data, which may hold several documents.
func clusterK8sbotImageIn(data string) string {
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
			if c.Name == clusterK8sbotContainer {
				return c.Image
			}
		}
	}
	return ""
}

// clusterK8sbotCheckBase refuses a base Space holding Units that define
// k8sbot's resources but that no upload of k8sbot owns, such as the single
// Unit an early installer wrote. An upload never writes or empties such a Unit,
// so it would sit beside the upload's own, and a variant cloned from the base
// would apply both. The check is a dry run of the upload, whose plan reports
// exactly those Units.
func clusterK8sbotCheckBase(baseSpace string, upload goclientnew.UploadRequest) error {
	plan, err := cubapi.Upload(ctx, cubClient, upload, true)
	if err != nil {
		return fmt.Errorf("check k8sbot base %q: %w", baseSpace, err)
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
	fmt.Fprintf(&b, "k8sbot base %q has Units that define k8sbot's resources but were not uploaded as k8sbot, so a cluster cloned from it would run two copies of k8sbot. Delete them and run again:", baseSpace)
	for _, space := range spaces {
		slices.Sort(strays[space])
		for _, unit := range strays[space] {
			fmt.Fprintf(&b, "\n  cub unit delete --space %s %s", space, unit)
		}
	}
	return fmt.Errorf("%s", b.String())
}

// clusterK8sbotBaseUpload is the upload of k8sbot's deployment, read from
// image ref, into its shared base Space, as `cub variant upload --component
// k8sbot --variant <base>` makes it.
func clusterK8sbotBaseUpload(baseVariant, baseSpace string, manifests []byte, ref string) goclientnew.UploadRequest {
	return goclientnew.UploadRequest{
		Components:  []goclientnew.UploadComponentRequest{{Name: clusterK8sbotComponent, Space: baseSpace}},
		SpaceLabels: map[string]string{"Variant": baseVariant},
		Files:       []goclientnew.UploadRequestFile{{Path: "stdin.yaml", Content: string(manifests)}},
		Source:      &goclientnew.UploadSourceInfo{Ref: ref, Client: "cub", ClientVersion: Version},
	}
}

// clusterK8sbotUploadBase writes k8sbot's base. It empties, without asking,
// the Units of resources k8sbot's release no longer has, as `cub variant
// upload --yes` does: the base holds only k8sbot's own deployment, so what a
// release dropped is meant to leave every cluster cloned from it.
func clusterK8sbotUploadBase(upload goclientnew.UploadRequest) error {
	result, err := cubapi.Upload(ctx, cubClient, upload, false)
	if err != nil {
		return err
	}
	reportUploadResult(result)
	reportUploadRevert(result)
	return nil
}

// clusterCreateK8sbotVariant clones k8sbot's base into the cluster's variant,
// bound to the cluster's OCI target, and adds the Unit that deploys it to the
// cluster's apps Space, as `cub variant create --target` does.
func clusterCreateK8sbotVariant(out io.Writer, o clusterK8sbotOptions, baseSpace string) (*goclientnew.Space, error) {
	upstream, err := resolveSpace(baseSpace, "SpaceID")
	if err != nil {
		return nil, err
	}
	target, err := resolveTargetCore(o.ociTargetRef, defaultSpaceID(), "*")
	if err != nil {
		return nil, err
	}
	result, err := cubapi.CreateVariant(ctx, cubClient, cubapi.VariantSpec{
		Upstream:      upstream.Space.SpaceID,
		Name:          o.clusterName,
		ReleaseTarget: &target.TargetID,
	})
	if err != nil {
		return nil, err
	}
	if errs := result.UnitErrors(); len(errs) > 0 {
		return nil, fmt.Errorf("clone k8sbot's units into %s: %s", result.Space.Slug, strings.Join(errs, "; "))
	}
	if _, err := createVariantClusterApp(out, target, result.Space, target.TargetID, false); err != nil {
		return nil, err
	}
	return result.Space, nil
}
