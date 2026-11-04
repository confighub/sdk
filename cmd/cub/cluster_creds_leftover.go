// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net/url"
	"os"
	"strings"

	"golang.org/x/term"
)

// An attach that fails, or is stopped, after writing the controller's registry
// credentials Secret can leave it in the cluster when its rollback cannot
// remove it, and the Secret is what marks a cluster as attached. The helpers
// here tell such a leftover from a live attachment's Secret, so a retry does
// not need it deleted by hand.
//
// Replacing a live attachment's Secret would cut that attachment's controller
// off from its registry, so a Secret is only replaced unasked when it records
// both the cluster and the organization it was written for, the organization
// is the current one, and nothing in it claims that cluster. The registry URL
// alone says only which server the Secret is for, and every organization on a
// server shares it.

// clusterAnnotationOrganization records, on the registry credentials Secret,
// the organization of the attachment that wrote it.
const clusterAnnotationOrganization = "confighub.com/organization-id"

// clusterCredsAnnotations are the annotations attach writes on the registry
// credentials Secret: the cluster's name and the current organization. They
// are annotations rather than labels because a label value is limited to 63
// characters, and a cluster name can be up to 123: the service account's slug
// is argo-<name>, and a slug is up to 128 (SlugMaxLength in the server's
// models).
func clusterCredsAnnotations(clusterName string) map[string]string {
	return map[string]string{
		clusterAnnotationName:         clusterName,
		clusterAnnotationOrganization: contextManager.ActiveContext().Coordinate.OrganizationID,
	}
}

// clusterArgoCredsForRegistry reports whether the url of an Argo CD repo-creds
// Secret is ociURL, the registry this server's attach writes into it.
func clusterArgoCredsForRegistry(secretURL, ociURL string) bool {
	return strings.TrimRight(secretURL, "/") == strings.TrimRight(ociURL, "/")
}

// clusterFluxCredsForRegistry reports whether the host a Flux credentials
// Secret holds credentials for is the host of ociURL, as Flux matches them.
func clusterFluxCredsForRegistry(secretHost, ociURL string) bool {
	u, err := url.Parse(ociURL)
	return err == nil && u.Host != "" && u.Host == secretHost
}

// clusterReadCredsAnnotations returns the cluster name and organization ID a
// registry credentials Secret records, each "" when absent, as on one an older
// cub wrote.
func clusterReadCredsAnnotations(ctx context.Context, kubeconfigPath, namespace, secret string) (cluster, organization string, err error) {
	annotation := func(key string) string {
		return "{.metadata.annotations." + strings.ReplaceAll(key, ".", `\.`) + "}"
	}
	raw, err := clusterKubectlOutput(ctx, kubeconfigPath, "get", "secret", secret, "-n", namespace,
		"-o", "jsonpath="+annotation(clusterAnnotationName)+`{"\n"}`+annotation(clusterAnnotationOrganization))
	if err != nil {
		return "", "", fmt.Errorf("read the %s Secret's annotations: %w", secret, err)
	}
	cluster, organization, _ = strings.Cut(string(raw), "\n")
	return strings.TrimSpace(cluster), strings.TrimSpace(organization), nil
}

// clusterClaimedInOrg reports whether the current organization still has the
// cluster Space or the service account of the cluster named name: either one
// means an attachment of that cluster is in use, or was left half-removed.
func clusterClaimedInOrg(name string) (bool, error) {
	space, err := clusterSpaceByNameInOrg(name)
	if err != nil {
		return false, fmt.Errorf("look up cluster spaces: %w", err)
	}
	if space != nil {
		return true, nil
	}
	return clusterServiceAccountExists(clusterServiceAccountSlug(name))
}

// clusterCredsVerdict is what attach does about a registry credentials Secret
// it finds in the cluster.
type clusterCredsVerdict int

const (
	// clusterCredsWired: the cluster is attached, or may be; refuse.
	clusterCredsWired clusterCredsVerdict = iota
	// clusterCredsLeftover: the Secret names the current organization and a
	// cluster nothing in it claims. Replace it, asking first in a terminal.
	clusterCredsLeftover
	// clusterCredsUnnamed: the Secret is for this server's registry and nothing
	// claims the cluster being attached, but it does not say which cluster or
	// organization it was written for, so it may be a live attachment's under
	// another name or in another organization. Replace it only when a terminal
	// answers yes.
	clusterCredsUnnamed
)

// clusterFoundCreds is what attach reads off a registry credentials Secret.
type clusterFoundCreds struct {
	// forRegistry is whether the Secret is for this server's registry.
	forRegistry bool
	// cluster and organization are what the Secret records, "" when absent.
	cluster, organization string
}

// clusterJudgeCreds decides what attach does about a registry credentials
// Secret it found. currentOrg is the organization attach runs in, attaching
// the cluster name being attached, and claimed reports whether the current
// organization has a Space or service account for a cluster name. It returns
// the cluster name it checked.
func clusterJudgeCreds(found clusterFoundCreds, currentOrg, attaching string, claimed func(name string) (bool, error)) (clusterCredsVerdict, string, error) {
	if !found.forRegistry {
		return clusterCredsWired, "", nil
	}
	if found.cluster != "" && found.organization != "" {
		if found.organization != currentOrg {
			return clusterCredsWired, found.cluster, nil
		}
		if c, err := claimed(found.cluster); err != nil || c {
			return clusterCredsWired, found.cluster, err
		}
		return clusterCredsLeftover, found.cluster, nil
	}
	name := found.cluster
	if name == "" {
		name = attaching
	}
	if c, err := claimed(name); err != nil || c {
		return clusterCredsWired, name, err
	}
	return clusterCredsUnnamed, name, nil
}

// clusterLeftoverCreds describes a registry credentials Secret attach may
// replace.
type clusterLeftoverCreds struct {
	namespace, secret string
	// cluster is the name checked for a Space and service account.
	cluster string
	// named is whether the Secret recorded the cluster and organization it was
	// written for (clusterCredsLeftover) or not (clusterCredsUnnamed).
	named bool
	// server is the ConfigHub server checked, for the message.
	server string
}

// clusterAllowLeftoverCreds says why the Secret may be a leftover and decides
// whether attach replaces it. A named one is replaced unasked without a
// terminal, and the question defaults to yes. An unnamed one is never replaced
// without a terminal, and the question defaults to no. It returns false with
// no error when an unnamed Secret is kept for want of a terminal, and an error
// when the answer is no.
func clusterAllowLeftoverCreds(out io.Writer, in io.Reader, interactive bool, left clusterLeftoverCreds) (bool, error) {
	serviceAccount := clusterServiceAccountSlug(left.cluster)
	question := "Replace it with this attachment's credentials? [Y/n]: "
	if left.named {
		fmt.Fprintf(out, "Secret %s/%s was left by an attach of cluster %q in this organization that did not finish: nothing in %s uses it (no cluster Space %q and no service account %q).\n",
			left.namespace, left.secret, left.cluster, left.server, left.cluster, serviceAccount)
		if !interactive {
			fmt.Fprintln(out, "Replacing it with this attachment's credentials.")
			return true, nil
		}
	} else {
		if !interactive {
			return false, nil
		}
		fmt.Fprintf(out, "Secret %s/%s is for this server's registry, and nothing in this organization uses it for cluster %q (no cluster Space %q and no service account %q). An older cub wrote it, so it does not say which cluster or organization it was written for: if this cluster is attached under another name, or in another organization on %s, replacing it cuts that attachment off.\n",
			left.namespace, left.secret, left.cluster, left.cluster, serviceAccount, left.server)
		question = "Replace it with this attachment's credentials? [y/N]: "
	}
	fmt.Fprint(out, question)
	answer, err := bufio.NewReader(in).ReadString('\n')
	if err != nil && answer == "" {
		return false, fmt.Errorf("read answer: %w", err)
	}
	switch strings.ToLower(strings.TrimSpace(answer)) {
	case "y", "yes":
		return true, nil
	case "":
		if left.named {
			return true, nil
		}
	}
	return false, fmt.Errorf("cancelled: Secret %s/%s is left in place, and the cluster stays marked as attached until it is deleted", left.namespace, left.secret)
}

// clusterControllerDisplayName is how messages name the controller.
func clusterControllerDisplayName(controller string) string {
	switch controller {
	case clusterControllerSveltos:
		return "Sveltos"
	case clusterControllerFlux:
		return "Flux"
	}
	return "Argo CD"
}

// clusterStdinIsTerminal reports whether attach can ask a question.
func clusterStdinIsTerminal() bool {
	return term.IsTerminal(int(os.Stdin.Fd()))
}
