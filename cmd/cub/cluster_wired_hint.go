// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"fmt"
	"os"
	"strings"
)

// The "already wired" error says how to remove the registry credentials
// Secret that marks the cluster as attached. Every command it prints has to
// work from where the user stands when attach stops, which rules out two easy
// answers: attach's own copy of the cluster's credentials is removed as attach
// rolls back, so a command cannot name it; and `cub cluster detach` is sure to
// clean the cluster only when this machine has the record and kubeconfig of
// the same attachment, which a matching cluster name does not prove
// (clusterDetachableHere), so it is suggested only then.

// clusterKubectlAccess is how a printed kubectl command reaches the cluster
// being attached: the way attach read its credentials, from the user's
// kubeconfig, kind or the AWS CLI. setup writes a kubeconfig the flags name,
// into a directory of its own so no file of the user's is overwritten, and
// cleanup removes it; both are empty when the user's own kubeconfig is used.
// contextUnknown is set when the flags name no context and the command would
// act on whichever one is current when it is run.
type clusterKubectlAccess struct {
	setup, cleanup []string
	flags          string
	contextUnknown bool
}

// clusterTempKubeconfig is where setup writes the credentials it fetches.
const clusterTempKubeconfig = `"$CUB_TMP/kubeconfig"`

// clusterTempAccess is access through credentials fetch writes to
// clusterTempKubeconfig.
func clusterTempAccess(fetch string) clusterKubectlAccess {
	return clusterKubectlAccess{
		setup:   []string{"CUB_TMP=$(mktemp -d)", fetch},
		flags:   "--kubeconfig " + clusterTempKubeconfig,
		cleanup: []string{`rm -r "$CUB_TMP"`},
	}
}

// clusterAccessForHint returns the access for opts. currentContext is the
// context attach read from the user's kubeconfig, filled in so the command
// does not depend on which context is current when it is run; "" leaves it
// to --kube-context, or to the kubeconfig's current context.
//
// It restates, for each provider, how clusterControllerInstallStage reads the
// cluster's credentials, so a provider added there needs a case here; one
// without falls to the last case, which names no command.
func clusterAccessForHint(opts *clusterAttachOptions, currentContext string) clusterKubectlAccess {
	switch opts.provider {
	case clusterProviderKind:
		// kind's kind-<name> context is in the user's kubeconfig only when kind
		// merged it there, which `cub cluster up` does not, so the credentials
		// are fetched the way attach fetched them.
		return clusterTempAccess(fmt.Sprintf("kind get kubeconfig --name %s > %s", shellQuoteArg(opts.cluster), clusterTempKubeconfig))
	case clusterProviderEKS:
		args := clusterAWSArgs(opts.awsProfile, opts.awsRegion, "eks", "update-kubeconfig", "--name", opts.cluster)
		for i := range args {
			args[i] = shellQuoteArg(args[i])
		}
		return clusterTempAccess("aws " + strings.Join(args, " ") + " --kubeconfig " + clusterTempKubeconfig)
	case clusterProviderKubeconfig:
	default:
		return clusterKubectlAccess{}
	}
	var flags []string
	if opts.kubeconfig != "" {
		flags = append(flags, "--kubeconfig "+shellQuoteArg(opts.kubeconfig))
	}
	kubeContext := currentContext
	if kubeContext == "" {
		kubeContext = opts.kubeContext
	}
	if kubeContext != "" {
		flags = append(flags, "--context "+shellQuoteArg(kubeContext))
	}
	return clusterKubectlAccess{flags: strings.Join(flags, " "), contextUnknown: kubeContext == ""}
}

// clusterDeleteSecretCommands are the commands that delete the Secret, in
// order, or none when access names no way to reach the cluster.
func clusterDeleteSecretCommands(access clusterKubectlAccess, namespace, secret string) []string {
	if access.flags == "" && !access.contextUnknown {
		return nil
	}
	kubectl := "kubectl"
	if access.flags != "" {
		kubectl += " " + access.flags
	}
	cmds := append([]string(nil), access.setup...)
	cmds = append(cmds, fmt.Sprintf("%s -n %s delete secret %s", kubectl, shellQuoteArg(namespace), shellQuoteArg(secret)))
	return append(cmds, access.cleanup...)
}

// clusterDetachableHere reports whether `cub cluster detach <name>` on this
// machine is sure to remove the registry credentials of the cluster being
// attached. A cluster name is unique only within an organization, so the local
// record of that name may be for another cluster, and detaching that one would
// cut off a live attachment and leave this Secret in place. It is the same
// attachment only when the record is for the organization the Secret records,
// secretOrg, and the record's kubeconfig reaches the API server attach is
// using, attachServer; localServer is the API server of the record's
// kubeconfig, "" when there is none, without which detach also skips the
// in-cluster cleanup. (A cluster from before records existed may be found
// without one, through its Space, but that is not sure enough to suggest.)
// detach refuses a kind cluster cub created, which only `cub cluster down`
// removes, by deleting the cluster.
func clusterDetachableHere(rec *clusterRecord, localServer, secretOrg, attachServer string) bool {
	return rec != nil && rec.Origin != clusterOriginKind &&
		secretOrg != "" && rec.OrganizationID == secretOrg &&
		localServer != "" && localServer == attachServer
}

// clusterWiredHint is the end of the "already wired" error: how to remove the
// Secret. detachName is the cluster `cub cluster detach` removes it for, or ""
// when that would not. contextUnknown says deleteCommands act on the current
// kube context, which has to be the cluster being attached.
func clusterWiredHint(detachName string, deleteCommands []string, contextUnknown bool, namespace, secret string) string {
	var b strings.Builder
	if detachName != "" {
		// detach refuses a cluster whose Space or cub context is gone unless
		// told to go on.
		fmt.Fprintf(&b, "It was attached from this machine as cluster %q: detach it with 'cub cluster detach %s' (with --force if its Space is gone). Or, if that attachment is not in use any more, ", detachName, shellQuoteArg(detachName))
	} else {
		b.WriteString("If the attachment that wrote it is not in use any more, ")
	}
	if len(deleteCommands) == 0 {
		fmt.Fprintf(&b, "delete Secret %s/%s from the cluster.", namespace, secret)
		return b.String()
	}
	b.WriteString("delete the Secret")
	if contextUnknown {
		b.WriteString(", with the cluster being attached as the current kube context")
	}
	b.WriteString(":")
	for _, c := range deleteCommands {
		b.WriteString("\n  " + c)
	}
	return b.String()
}

// clusterLocalDetachName returns name when `cub cluster detach name` on this
// machine would remove the credentials of the cluster being attached, as
// clusterDetachableHere decides, and "" otherwise, including when name is ""
// because the Secret does not say which cluster it is for. secretOrg is the
// organization the Secret records, and attachKubeconfigPath the kubeconfig
// attach is using.
func clusterLocalDetachName(name, secretOrg, attachKubeconfigPath string) string {
	if name == "" {
		return ""
	}
	rec, err := clusterReadRecord(name)
	if err != nil {
		return ""
	}
	if !clusterDetachableHere(rec, clusterKubeconfigFileServer(clusterKubeconfigPath(name)), secretOrg, clusterKubeconfigFileServer(attachKubeconfigPath)) {
		return ""
	}
	return name
}

// clusterKubeconfigFileServer is the API server of the kubeconfig at path, ""
// when it cannot be read.
func clusterKubeconfigFileServer(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return clusterKubeconfigServer(data)
}

// clusterKubeconfigContext returns the context attach's copy of the
// credentials names, which is the one it read from the user's kubeconfig, or
// "" when it cannot be read.
func clusterKubeconfigContext(ctx context.Context, kubeconfigPath string) string {
	raw, err := clusterKubectlOutput(ctx, kubeconfigPath, "config", "current-context")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(raw))
}
