// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/spf13/cobra"
)

var clusterSvcAccountBootstrapArgs struct {
	noArgobot  bool
	secretsDir string
}

var clusterSvcAccountBootstrapCmd = &cobra.Command{
	Use:   "svc-account-bootstrap <name>",
	Args:  cobra.ExactArgs(1),
	Short: "Create a cluster's service account and put its credentials in the cluster",
	Long: `The second part of "cub cluster attach", on its own, for a cluster "cub cluster
controller-install" set up. "cub cluster attach <name>" finishes the job
later, picking the cluster up where this left it.

In ConfigHub it creates the cluster's identity, a service account with no
organization role named argo-<name>, and grants its user View, Edit,
ViewChildren and EditChildren on the cluster's OCI target: what the controller
needs to pull the Releases of every Space that releases to the target, and
argobot to find the target, store the cluster's facts on it and record each
Release's live status. Claim on argobot's Channel is granted where the Channel
is created, when argobot is installed.

In the cluster it puts the service account's credentials:

  a private key       in the argobot-secrets Secret in the argobot namespace,
                      which argobot authenticates with. Skipped with
                      --no-k8sbot, for a cluster that will not run argobot;
                      installing argobot later issues one.
  a pull token        a token that does not expire, as the controller's
                      registry credentials: Argo CD's confighub-oci-creds
                      repo-creds Secret, Sveltos's confighub-gateway Secret, or
                      Flux's confighub-oci Secret.

Neither credential is kept anywhere else: ConfigHub holds the public half of
the key and a revocable record of the token.

A cluster that takes credentials only from a secret store, through the
External Secrets Operator, gets them with --secrets-dir <dir>: each Secret's
Kubernetes manifest, exactly what
would be applied, is written to <dir>/<name>/<namespace>_<secret>.yaml instead
of being applied. The values are in plain text, in the manifest's stringData,
so the file is readable only by you; argobot's also holds the argobot
Namespace, which is applied. Load the values into the store, and have the
store keep a Secret of that name in the cluster with the manifest's type,
labels, annotations and keys.
The controller cannot pull, nor argobot start, until it is there. Pass the same
--secrets-dir to "cub cluster attach", which then installs argobot with the key
in the file rather than issuing another. The key and the token belong to the
cluster's service account, which this creates, so they cannot be put in the
store before it runs. The Secrets Store CSI driver's secretObjects keeps a
Secret only while a pod mounts its volume, which argobot and the controller do
not; see "cub cluster attach --help".

"cub cluster detach <name> --delete-config" undoes this with the rest of the
cluster's wiring.

Examples:
  cub cluster create lab
  cub cluster controller-install lab --provider kind
  cub cluster svc-account-bootstrap lab
  cub cluster attach lab`,
	RunE: clusterSvcAccountBootstrapCmdRun,
}

func init() {
	clusterSvcAccountBootstrapCmd.Flags().BoolVar(&clusterSvcAccountBootstrapArgs.noArgobot, "no-k8sbot", false, "skip argobot's private key, for a cluster that will not run argobot (installing argobot later issues one)")
	clusterSvcAccountBootstrapCmd.Flags().StringVar(&clusterSvcAccountBootstrapArgs.secretsDir, "secrets-dir", "", clusterSecretsDirFlagUsage)
	clusterCmd.AddCommand(clusterSvcAccountBootstrapCmd)
}

func clusterSvcAccountBootstrapCmdRun(cmd *cobra.Command, args []string) error {
	a := clusterSvcAccountBootstrapArgs
	return clusterSvcAccountBootstrapRun(cmd.OutOrStdout(), args[0], a.noArgobot, a.secretsDir)
}

// clusterSvcAccountBootstrapRun runs the service account stage of an
// attachment on a cluster `cub cluster controller-install` set up; the stage
// moves its cluster Space on to clusterStageServiceAccount, so `cub cluster
// attach` finishes it.
func clusterSvcAccountBootstrapRun(out io.Writer, name string, noArgobot bool, secretsDirFlag string) (err error) {
	if _, err := exec.LookPath("kubectl"); err != nil {
		return fmt.Errorf("kubectl not found on PATH")
	}
	space, err := clusterFindControllerInstalled(out, name)
	if err != nil {
		return err
	}
	switch {
	case space == nil:
		return fmt.Errorf("cluster %q has no controller installed by cub; run 'cub cluster controller-install %s' first", name, name)
	case space.Stage == clusterStageServiceAccount:
		return fmt.Errorf("cluster %q already has its service account (argo-%s); finish with 'cub cluster attach %s'", name, name, name)
	}
	serviceAccountSlug := clusterServiceAccountSlug(name)
	if exists, err := clusterServiceAccountExists(serviceAccountSlug); err != nil {
		return err
	} else if exists {
		return fmt.Errorf("ConfigHub service account %q already exists (leftover cluster identity; delete it with 'cub serviceaccount delete %s')", serviceAccountSlug, serviceAccountSlug)
	}
	// The controller's registry credentials are matched to the registry by its
	// address. Argo CD's confighub-oci-creds repo-creds Secret carries a url, and
	// Argo CD uses it for every Application whose source.repoURL starts with that
	// url. Flux's confighub-oci Secret is a docker config keyed by the registry's
	// host:port, which has to be the host of the OCIRepository's URL. (Sveltos's
	// confighub-gateway Secret records no URL.) This stage writes the credentials'
	// half and app-bootstrap the root's source, which they are matched against, so
	// both say which registry they use.
	if err := clusterPrintControllerOCIEndpoint(out); err != nil {
		return err
	}
	kubeconfigPath := clusterKubeconfigPath(name)
	inst, err := clusterLoadInstalled(space, kubeconfigPath)
	if err != nil {
		return err
	}

	secretsDir := clusterSecretsDirFor(secretsDirFlag, name)
	rollback := clusterBeginRollback(out)
	commit := false
	defer func() { rollback.finish(commit, &err) }()
	if err := clusterBootstrapServiceAccount(out, clusterServiceAccountSpec{
		name:           name,
		spaceSlug:      space.SpaceSlug,
		kubeconfigPath: kubeconfigPath,
		controller:     space.Controller,
		noArgobot:      noArgobot,
		secretsDir:     secretsDir,
	}, inst, rollback.add); err != nil {
		return err
	}
	commit = true

	fmt.Fprintf(out, "\nService account bootstrapped.\n  cluster:    %s\n  identity:   service account %s, granted its deployment path on %s/%s\n",
		name, serviceAccountSlug, space.SpaceSlug, clusterTargetSlug)
	creds := clusterSecret{namespace: clusterControllerNamespace(space.Controller), name: clusterControllerCredsSecret(space.Controller)}
	fmt.Fprintf(out, "  pull token: %s\n", clusterDescribeSecret(secretsDir, creds))
	if noArgobot {
		fmt.Fprintf(out, "  argobot key: none (--no-k8sbot)\n")
	} else {
		fmt.Fprintf(out, "  argobot key: %s\n", clusterDescribeSecret(secretsDir, clusterArgobotKeySecret()))
	}
	flags := ""
	if noArgobot {
		flags += " --no-k8sbot"
	}
	if secretsDirFlag != "" {
		flags += " --secrets-dir " + secretsDirFlag
	}
	fmt.Fprintf(out, "\nFinish wiring it into ConfigHub — the apps space, the root and argobot — with:\n  cub cluster attach %s%s\n", name, flags)
	return nil
}

// clusterServiceAccountSpec is what the service account stage needs.
type clusterServiceAccountSpec struct {
	name           string
	spaceSlug      string
	kubeconfigPath string
	controller     string
	// noArgobot skips argobot's key, which only argobot uses.
	noArgobot bool
	// secretsDir is --secrets-dir: where the Kubernetes manifests of the
	// credentials Secrets are written instead of being applied
	// (clusterPutSecret).
	secretsDir string
	// existingSecrets is --existing-secrets: the service account exists, and
	// its credentials are in Secrets a secret store keeps in the cluster, which
	// are waited for up to existingSecretsTimeout and checked rather than
	// written (clusterAdoptServiceAccount).
	existingSecrets        bool
	existingSecretsTimeout time.Duration
}

// clusterAdoptServiceAccount is the service account stage for --existing-secrets:
// the cluster's service account, which the operator created, is granted the
// target as clusterBootstrapServiceAccount grants it, and its credentials are
// read from the Secrets in the cluster instead of issued. Each is checked by
// authenticating with it, so a credential of another identity or another
// ConfigHub, or one revoked since, fails here rather than as a controller that
// cannot pull. Neither the service account nor the Secrets are rolled back:
// attach made neither. The cluster Space records that the Secrets were there,
// so `cub cluster detach` leaves them.
func clusterAdoptServiceAccount(out io.Writer, spec clusterServiceAccountSpec, inst clusterInstalled) error {
	serviceAccountSlug := clusterServiceAccountSlug(spec.name)
	_, serviceAccountUserID, err := clusterServiceAccountIDs(serviceAccountSlug)
	if err != nil {
		return fmt.Errorf("--existing-secrets: %w; create it, and the credentials the Secrets hold, first (see 'cub cluster attach --help')", err)
	}
	fmt.Fprintf(out, "Using service account %q, and its credentials from the Secrets in the cluster...\n", serviceAccountSlug)
	fmt.Fprintf(out, "Granting %q its deployment path on OCI target %q...\n", serviceAccountSlug, clusterTargetSlug)
	if _, err := clusterGrantTarget(inst.spaceID, inst.targetID, serviceAccountUserID, nil); err != nil {
		return fmt.Errorf("grant the service account access to the target: %w", err)
	}

	// The store can only put argobot's Secret in a namespace that is there, and
	// nothing else creates it before argobot's Release is deployed, after this
	// stage. Applied before either wait, so the store can create both Secrets
	// at once. It is left on failure: the store's own objects may be in it.
	if !spec.noArgobot {
		fmt.Fprintf(out, "Applying the %q namespace, for the secret store to put argobot's Secret in...\n", clusterArgobotNamespace)
		if err := clusterKubectlApply(ctx, spec.kubeconfigPath, clusterArgobotNamespaceManifest(), out); err != nil {
			return fmt.Errorf("apply the %s namespace: %w", clusterArgobotNamespace, err)
		}
	}
	ociURL, ociInsecure, err := clusterControllerOCIEndpoint()
	if err != nil {
		return err
	}
	namespace, name := clusterControllerNamespace(spec.controller), clusterControllerCredsSecret(spec.controller)
	creds, err := clusterWaitForSecret(out, spec.kubeconfigPath, namespace, name, spec.existingSecretsTimeout)
	if err != nil {
		return err
	}
	if err := clusterRequirePullToken(creds, spec.controller, ociURL, ociInsecure, serviceAccountUserID, serviceAccountSlug); err != nil {
		return err
	}
	fmt.Fprintf(out, "Secret %s/%s: the controller can pull with it, as %s\n", namespace, name, serviceAccountSlug)
	if !spec.noArgobot {
		key, err := clusterWaitForSecret(out, spec.kubeconfigPath, clusterArgobotNamespace, clusterArgobotSecret, spec.existingSecretsTimeout)
		if err != nil {
			return err
		}
		if err := clusterRequireArgobotKey(key, serviceAccountUserID, serviceAccountSlug); err != nil {
			return err
		}
		fmt.Fprintf(out, "Secret %s/%s: argobot can authenticate with it, as %s\n", clusterArgobotNamespace, clusterArgobotSecret, serviceAccountSlug)
	}

	patchData, err := json.Marshal(map[string]any{
		"Annotations": map[string]string{clusterAnnotationExistingSecrets: "true"},
	})
	if err != nil {
		return err
	}
	if _, err := patchSpace(inst.spaceID, patchData); err != nil {
		return fmt.Errorf("record the existing Secrets on the cluster space: %w", err)
	}
	return clusterSetSpaceStage(inst.spaceID, clusterStageServiceAccount)
}

// clusterBootstrapServiceAccount creates the cluster's identity on the
// ConfigHub side and puts its credentials in the cluster. One service account,
// used two ways: the controller pulls the cluster's Releases from the OCI
// registry with a long-lived token of it, and argobot authenticates as it with
// a key. It has no organization role; the Target, granted here, and argobot's
// Channel, granted where it is created, give its user exactly the cluster's
// deployment path. The service account is org-scoped, so it is rolled back on
// its own rather than with a Space, and the credentials Secrets are rolled back
// too, since the registry one marks the cluster as wired. When it is done it
// moves the cluster Space's stage marker on to clusterStageServiceAccount.
func clusterBootstrapServiceAccount(out io.Writer, spec clusterServiceAccountSpec, inst clusterInstalled, addRollback func(clusterUndo)) error {
	if spec.existingSecrets {
		return clusterAdoptServiceAccount(out, spec, inst)
	}
	serviceAccountSlug := clusterServiceAccountSlug(spec.name)
	fmt.Fprintf(out, "Creating service account %q (OrgRole=none; the controller pulls and argobot runs as it)...\n", serviceAccountSlug)
	serviceAccountID, serviceAccountUserID, err := clusterCreateServiceAccount(serviceAccountSlug, serviceAccountSlug,
		map[string]string{clusterAnnotationName: spec.name})
	if err != nil {
		return err
	}
	addRollback(clusterUndo{
		what: fmt.Sprintf("service account %q", serviceAccountSlug),
		fix:  []string{fmt.Sprintf("cub serviceaccount delete %s", serviceAccountSlug)},
		run: func() error {
			fmt.Fprintf(out, "Rolling back: cub serviceaccount delete %q\n", serviceAccountSlug)
			return clusterDeleteServiceAccount(serviceAccountSlug)
		},
	})

	fmt.Fprintf(out, "Granting %q its deployment path on OCI target %q...\n", serviceAccountSlug, clusterTargetSlug)
	if _, err := clusterGrantTarget(inst.spaceID, inst.targetID, serviceAccountUserID, nil); err != nil {
		return fmt.Errorf("grant the service account access to the target: %w", err)
	}

	if spec.noArgobot && spec.secretsDir != "" {
		// A key file left by an earlier attachment of a cluster of this name
		// holds a key of a service account deleted since; installing argobot
		// later would take it for this one's.
		if err := os.Remove(clusterSecretFilePath(spec.secretsDir, clusterArgobotKeySecret())); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("remove the stale argobot key file: %w", err)
		}
	}
	if !spec.noArgobot {
		fmt.Fprintf(out, "Issuing a key for argobot on %q...\n", serviceAccountSlug)
		privateKey, err := clusterCreateServiceAccountKey(serviceAccountID, serviceAccountUserID, "argobot in cluster "+spec.name)
		if err != nil {
			return fmt.Errorf("issue the service account key: %w", err)
		}
		if err := clusterApplyArgobotKey(out, spec.kubeconfigPath, spec.secretsDir, privateKey, addRollback); err != nil {
			return err
		}
	}

	fmt.Fprintf(out, "Issuing a pull token on %q...\n", serviceAccountSlug)
	pullToken, err := clusterCreateServiceAccountToken(serviceAccountID, "cluster "+spec.name+" (OCI pull)")
	if err != nil {
		return fmt.Errorf("issue the service account token: %w", err)
	}
	ociURLForArgo, ociInsecure, err := clusterControllerOCIEndpoint()
	if err != nil {
		return err
	}
	creds, err := clusterControllerCredsSecretFor(spec.controller, spec.name, ociURLForArgo, pullToken, ociInsecure)
	if err != nil {
		return err
	}
	if err := clusterPutSecret(out, spec.kubeconfigPath, spec.secretsDir, creds, addRollback); err != nil {
		return err
	}
	// Last, so a run stopped before here is picked up at the controller stage
	// and one stopped after it at this one, whether the stage ran on its own or
	// inside `cub cluster attach`.
	return clusterSetSpaceStage(inst.spaceID, clusterStageServiceAccount)
}

// clusterControllerOCI is the registry the controller is set up with.
type clusterControllerOCI struct {
	// advertised is the registry URL the server advertises at /api/info, ""
	// when it advertises none; apiURL is the server's.
	advertised, apiURL string
	// url is the registry as the controller, inside the cluster, reaches it, and
	// insecure whether that is plain HTTP.
	url      string
	insecure bool
}

// clusterResolveControllerOCI asks the server where its registry is once per
// command, and every caller gets that one answer. The URL is printed before
// anything is written with it (clusterPrintControllerOCIEndpoint), and then
// written into the controller's credentials and into the root's source, by
// different stages; a second request could tell those a different registry
// from the one that was printed, or from each other.
var clusterResolveControllerOCI = sync.OnceValues(func() (clusterControllerOCI, error) {
	advertised, err := clusterOCIServerURL()
	if err != nil {
		return clusterControllerOCI{}, err
	}
	apiURL := clusterServerURL()
	url, err := clusterOCIEndpointFromContainer(advertised, apiURL)
	if err != nil {
		return clusterControllerOCI{}, err
	}
	insecure, err := clusterOCIInsecure(advertised, apiURL)
	if err != nil {
		return clusterControllerOCI{}, err
	}
	return clusterControllerOCI{advertised: advertised, apiURL: apiURL, url: url, insecure: insecure}, nil
})

// clusterControllerOCIEndpoint returns the OCI registry URL as the controller,
// running inside the cluster, reaches it, and whether it is plain HTTP, from
// the one answer clusterResolveControllerOCI gets. A loopback registry is
// rewritten to the docker host address, which only a cluster on this machine
// can reach anyway; a remote one passes through untouched. Plain HTTP comes from
// the advertised registry's scheme, or failing that from the cub server URL's:
// http:// API → http:// OCI (the local-dev case), https:// API → TLS OCI.
func clusterControllerOCIEndpoint() (string, bool, error) {
	oci, err := clusterResolveControllerOCI()
	if err != nil {
		return "", false, err
	}
	return oci.url, oci.insecure, nil
}

// clusterPrintControllerOCIEndpoint says which registry URL the controller is
// set up with, and where it came from, before anything is written with it: the
// repo-creds Secret and the root's source both carry it, and a wrong one shows
// only later, as a controller that cannot pull.
func clusterPrintControllerOCIEndpoint(out io.Writer) error {
	oci, err := clusterResolveControllerOCI()
	if err != nil {
		return err
	}
	fmt.Fprint(out, clusterDescribeControllerOCIEndpoint(oci))
	return nil
}

// clusterDescribeControllerOCIEndpoint is the text clusterPrintControllerOCIEndpoint
// prints: the registry as the controller will reach it, and how that follows from
// what the server advertises. The server's OCI_EXTERNAL_URL is offered as the fix
// only where it decides the address. For a server on this machine it does not:
// the host is replaced by the docker network's address for this machine, whatever
// the server says, so only the port and scheme would follow it.
func clusterDescribeControllerOCIEndpoint(oci clusterControllerOCI) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Registry the controller will pull Releases from: %s", oci.url)
	if oci.insecure {
		b.WriteString(" (plain HTTP)")
	}
	b.WriteString("\n")
	if oci.advertised == "" {
		fmt.Fprintf(&b, "  derived from the API URL %s: the server advertises no registry at /api/info\n", oci.apiURL)
	} else {
		fmt.Fprintf(&b, "  advertised by %s at /api/info as %s\n", oci.apiURL, oci.advertised)
	}
	if beforeRewrite, err := clusterOCIEndpoint(oci.advertised, oci.apiURL); err == nil && beforeRewrite != oci.url {
		b.WriteString("  its host rewritten to the docker network's address for this machine, which a container in a cluster on this machine reaches the server by\n")
		return b.String()
	}
	b.WriteString("  If the cluster reaches the registry at another address, set OCI_EXTERNAL_URL on the ConfigHub server.\n")
	return b.String()
}

// clusterControllerCredsSecret returns the Secret, in the controller's
// namespace, holding the controller's ConfigHub registry credentials.
func clusterControllerCredsSecret(controller string) string {
	switch controller {
	case clusterControllerSveltos:
		return clusterSveltosGatewaySecret
	case clusterControllerFlux:
		return clusterFluxCredsSecret
	}
	return clusterArgoOCICredsSecret
}

// clusterControllerCredsSecretFor is the Secret holding the controller's
// registry credentials: pullToken, for the registry at ociURL. The Argo CD and
// Flux Secrets record clusterName, so a later attach that finds one can tell
// whether anything still uses it.
func clusterControllerCredsSecretFor(controller, clusterName, ociURL, pullToken string, insecureHTTP bool) (clusterSecret, error) {
	s := clusterSecret{namespace: clusterControllerNamespace(controller), name: clusterControllerCredsSecret(controller)}
	switch controller {
	case clusterControllerSveltos:
		s.manifest = clusterSveltosOCICredsManifest(pullToken)
	case clusterControllerFlux:
		manifest, err := clusterFluxCredsManifest(clusterCredsAnnotations(clusterName), ociURL, pullToken)
		if err != nil {
			return s, err
		}
		s.manifest = manifest
	default:
		s.manifest = []byte(clusterArgoOCIRepoCredsManifest(clusterCredsAnnotations(clusterName), ociURL, pullToken, insecureHTTP))
	}
	return s, nil
}

// clusterHasArgobotKey reports whether argobot's key Secret is in the cluster,
// which `cub cluster svc-account-bootstrap --no-k8sbot` leaves out.
func clusterHasArgobotKey(kubeconfigPath string) (bool, error) {
	got, err := clusterKubectlOutput(ctx, kubeconfigPath,
		"get", "secret", clusterArgobotSecret, "-n", clusterArgobotNamespace, "--ignore-not-found", "-o", "name")
	if err != nil {
		return false, fmt.Errorf("check for argobot's key Secret: %w", err)
	}
	return len(bytes.TrimSpace(got)) > 0, nil
}

// clusterRequireArgobotKeyField fails when argobot's key Secret has no
// CONFIGHUB_AUTH_PRIVATE_KEY, the key argobot reads. cub never writes one
// without it; a secret store that was set up by hand might.
func clusterRequireArgobotKeyField(kubeconfigPath string) error {
	got, err := clusterKubectlOutput(ctx, kubeconfigPath,
		"get", "secret", clusterArgobotSecret, "-n", clusterArgobotNamespace,
		"-o", "jsonpath={.data.CONFIGHUB_AUTH_PRIVATE_KEY}")
	if err != nil {
		return fmt.Errorf("read argobot's key Secret: %w", err)
	}
	if len(bytes.TrimSpace(got)) == 0 {
		return fmt.Errorf("the Secret %s/%s has no CONFIGHUB_AUTH_PRIVATE_KEY, which argobot authenticates with: add it, or delete the Secret for cub to issue a key",
			clusterArgobotNamespace, clusterArgobotSecret)
	}
	return nil
}
