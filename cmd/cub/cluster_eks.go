// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"time"
)

// EKS support for `cub cluster attach --provider eks`: the cluster's kubeconfig
// comes from the AWS CLI, and the one way an EKS cluster commonly refuses its
// own creator is diagnosed and, with --grant-access, fixed. Everything shells
// out to `aws`, which is already how the user is logged in.
//
// `aws eks update-kubeconfig` writes a kubeconfig whose credentials are an exec
// plugin (`aws eks get-token`), so the copy attach keeps does not expire the way
// a static token would: it is good for as long as the AWS credentials are.

// clusterAnnotationEKSRegion records the AWS region of an EKS attachment on
// the cluster Space, beside the provider annotations in cluster.go.
const clusterAnnotationEKSRegion = "confighub.com/cluster-eks-region"

// clusterEKSAccessPolicyAdmin is the EKS access policy --grant-access
// associates: cluster-admin, which installing Argo CD needs.
const clusterEKSAccessPolicyAdmin = "arn:aws:eks::aws:cluster-access-policy/AmazonEKSClusterAdminPolicy"

// clusterAWSArgs prepends the profile and region the user chose to an `aws`
// invocation. Both are optional: the CLI resolves them from AWS_PROFILE,
// AWS_REGION and the profile's own config when they are not given.
func clusterAWSArgs(profile, region string, args ...string) []string {
	var full []string
	if profile != "" {
		full = append(full, "--profile", profile)
	}
	if region != "" {
		full = append(full, "--region", region)
	}
	return append(full, args...)
}

// clusterAWS runs the AWS CLI and returns its stdout. stderr is folded into
// the error, since that is where the CLI explains itself.
func clusterAWS(ctx context.Context, profile, region string, args ...string) ([]byte, error) {
	if _, err := exec.LookPath("aws"); err != nil {
		return nil, fmt.Errorf("aws not found on PATH; --eks-cluster needs the AWS CLI")
	}
	var stdout, stderr bytes.Buffer
	cmd := exec.CommandContext(ctx, "aws", clusterAWSArgs(profile, region, args...)...)
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("aws %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	return stdout.Bytes(), nil
}

// clusterEKSKubeconfig writes the EKS cluster's kubeconfig to path through
// `aws eks update-kubeconfig` and returns its contents. The file is written
// directly to cub's per-cluster path: the CLI merges into an existing file, and
// a fresh path is the only way to get exactly one cluster.
func clusterEKSKubeconfig(ctx context.Context, profile, region, cluster, path string) ([]byte, error) {
	if _, err := clusterAWS(ctx, profile, region, "eks", "update-kubeconfig",
		"--name", cluster, "--kubeconfig", path); err != nil {
		return nil, fmt.Errorf("resolve EKS cluster %q: %w", cluster, err)
	}
	return clusterExportKubeconfig(ctx, path, "")
}

// clusterEKSAccessEntryPrincipal converts the caller's STS identity into the
// ARN an EKS access entry takes.
//
// sts:GetCallerIdentity returns the SESSION for anything assumed — an SSO login
// reports arn:aws:sts::123:assumed-role/AWSReservedSSO_Admin_abc/user@example.com
// — and EKS wants the underlying role, arn:aws:iam::123:role/AWSReservedSSO_Admin_abc.
// Passing the session ARN is accepted by nothing and the error does not explain
// why. An IAM user ARN needs no conversion.
func clusterEKSAccessEntryPrincipal(callerARN string) string {
	parts := strings.Split(callerARN, ":")
	if len(parts) < 6 || parts[2] != "sts" {
		return callerARN
	}
	res := strings.SplitN(parts[5], "/", 3)
	if len(res) < 2 || res[0] != "assumed-role" {
		return callerARN
	}
	return fmt.Sprintf("arn:%s:iam::%s:role/%s", parts[1], parts[4], res[1])
}

// clusterEKSCallerPrincipal returns the access-entry principal for whoever the
// AWS CLI is logged in as.
func clusterEKSCallerPrincipal(ctx context.Context, profile, region string) (string, error) {
	out, err := clusterAWS(ctx, profile, region, "sts", "get-caller-identity", "--output", "json")
	if err != nil {
		return "", err
	}
	var id struct {
		Arn string `json:"Arn"`
	}
	if err := json.Unmarshal(out, &id); err != nil || id.Arn == "" {
		return "", fmt.Errorf("sts get-caller-identity returned no Arn")
	}
	return clusterEKSAccessEntryPrincipal(id.Arn), nil
}

// clusterEKSHasAccessEntry reports whether the principal has an access entry
// on the cluster.
func clusterEKSHasAccessEntry(ctx context.Context, profile, region, cluster, principal string) (bool, error) {
	out, err := clusterAWS(ctx, profile, region, "eks", "list-access-entries",
		"--cluster-name", cluster, "--output", "json")
	if err != nil {
		return false, err
	}
	var entries struct {
		AccessEntries []string `json:"accessEntries"`
	}
	if err := json.Unmarshal(out, &entries); err != nil {
		return false, fmt.Errorf("parse list-access-entries: %w", err)
	}
	for _, e := range entries.AccessEntries {
		if e == principal {
			return true, nil
		}
	}
	return false, nil
}

// clusterEKSGrantAccess gives the principal cluster-admin on the cluster
// through an access entry and the admin access policy.
func clusterEKSGrantAccess(ctx context.Context, profile, region, cluster, principal string) error {
	if _, err := clusterAWS(ctx, profile, region, "eks", "create-access-entry",
		"--cluster-name", cluster, "--principal-arn", principal, "--type", "STANDARD"); err != nil {
		return fmt.Errorf("create the access entry for %s: %w", principal, err)
	}
	if _, err := clusterAWS(ctx, profile, region, "eks", "associate-access-policy",
		"--cluster-name", cluster, "--principal-arn", principal,
		"--access-scope", "type=cluster", "--policy-arn", clusterEKSAccessPolicyAdmin); err != nil {
		return fmt.Errorf("associate the admin policy for %s: %w", principal, err)
	}
	return nil
}

// clusterEKSEnsureAccess is what runs when an EKS cluster rejects the
// kubeconfig the AWS CLI just produced. This is a cliff EKS builds for its own
// creators: a cluster created by automation (an ACK controller, a pipeline
// role) grants bootstrap admin to THAT identity, and the human attaching it is
// a different identity with no access entry, who gets a bare 401 that says
// nothing about why.
//
// If the caller already has an entry the rejection is something else, and
// saying otherwise would send them the wrong way. If not, --grant-access adds
// one and waits for it to take effect (about 30s in practice); without the
// flag the exact commands are printed instead.
func clusterEKSEnsureAccess(ctx context.Context, kubeconfigPath, profile, region, cluster string, grant bool, out io.Writer) error {
	principal, err := clusterEKSCallerPrincipal(ctx, profile, region)
	if err != nil {
		return fmt.Errorf("the cluster rejects the kubeconfig, and the AWS identity could not be read to say why: %w", err)
	}
	has, err := clusterEKSHasAccessEntry(ctx, profile, region, cluster, principal)
	if err != nil {
		return fmt.Errorf("the cluster rejects the kubeconfig, and its access entries could not be listed to say why: %w", err)
	}
	if has {
		return fmt.Errorf("the cluster rejects the kubeconfig even though %s has an access entry on it; check the entry's access policies", principal)
	}
	regionArg := ""
	if region != "" {
		regionArg = " --region " + region
	}
	if !grant {
		return fmt.Errorf(
			"the cluster rejects your credentials: %s has no EKS access entry.\n"+
				"The identity that created the cluster holds its bootstrap admin access and\n"+
				"yours was never granted any. Re-run with --grant-access to add it, or do it by hand:\n"+
				"  aws eks create-access-entry --cluster-name %s%s \\\n"+
				"    --principal-arn %s --type STANDARD\n"+
				"  aws eks associate-access-policy --cluster-name %s%s \\\n"+
				"    --principal-arn %s --access-scope type=cluster \\\n"+
				"    --policy-arn %s",
			principal, cluster, regionArg, principal, cluster, regionArg, principal, clusterEKSAccessPolicyAdmin)
	}

	fmt.Fprintf(out, "Granting %s cluster-admin on EKS cluster %q...\n", principal, cluster)
	if err := clusterEKSGrantAccess(ctx, profile, region, cluster, principal); err != nil {
		return err
	}
	deadline := time.Now().Add(3 * time.Minute)
	for {
		if err := clusterRequireClusterAdmin(ctx, kubeconfigPath); err == nil {
			fmt.Fprintln(out, "Access granted.")
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("granted %s access but the cluster still rejects it after 3m", principal)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(10 * time.Second):
		}
	}
}
