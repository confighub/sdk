// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"fmt"
	"io"
	"os"
	"time"

	"github.com/spf13/cobra"
)

var clusterDownArgs struct {
	name  string
	force bool
}

var clusterDownCmd = &cobra.Command{
	Use:   "down",
	Short: "Tear down a kind cluster and its ConfigHub space",
	Long: `Tears down a cub-managed cluster.

Looks up the cluster's Space in the current cub context (via the
confighub.com/cluster-name annotation). If found, deletes the kind
cluster, then deletes the Space recursively (cascading Unit, Target,
Worker), then removes the local kubeconfig.

If no matching Space is found in the current context, fails with a
"are you in the right context?" hint. Pass --force to delete the local
kind cluster + kubeconfig anyway, leaving any ConfigHub side untouched.`,
	RunE: clusterDownCmdRun,
}

func init() {
	clusterDownCmd.Flags().StringVar(&clusterDownArgs.name, "name", "", "cluster name (required)")
	clusterDownCmd.Flags().BoolVar(&clusterDownArgs.force, "force", false, "delete the local kind cluster + kubeconfig even if no matching ConfigHub Space is found in the current context")
	clusterCmd.AddCommand(clusterDownCmd)
}

func clusterDownCmdRun(cmd *cobra.Command, args []string) error {
	if clusterDownArgs.name == "" {
		return fmt.Errorf("--name is required")
	}
	return clusterDownRun(cmd.OutOrStdout(), clusterDownArgs.name, clusterDownArgs.force)
}

func clusterDownRun(out io.Writer, name string, force bool) error {
	hostname, _ := os.Hostname()
	kubeconfigPath := clusterKubeconfigPath(name)

	spaces, err := clusterListSpacesForHost(hostname)
	if err != nil {
		return fmt.Errorf("look up cluster spaces: %w", err)
	}
	var match *clusterSpace
	for i := range spaces {
		if spaces[i].ClusterName == name {
			match = &spaces[i]
			break
		}
	}

	if match == nil {
		if !force {
			return fmt.Errorf("no cub-managed Space for %q in the current cub context (are you in the right context?). Pass --force to delete the local kind cluster + kubeconfig anyway", name)
		}
		fmt.Fprintf(out, "No matching Space in current context — proceeding with --force (local cleanup only)\n")
	}

	// Delete the kind cluster first so the worker pod stops and the
	// connection drops; cub rejects deletion of a Connected worker.
	fmt.Fprintf(out, "Deleting kind cluster %q...\n", name)
	if err := clusterKindDelete(ctx, name, kubeconfigPath, out); err != nil {
		fmt.Fprintf(out, "  warning: %v\n", err)
	}

	if match != nil {
		// The space's own release target reference blocks the recursive delete
		// (spaces_release_target_id_fkey is ON DELETE RESTRICT), so clear it
		// first when set. TODO: remove once the server's recursive delete
		// clears the reference itself (#4782).
		if space, err := apiGetSpaceFromSlug(match.SpaceSlug, "*"); err != nil {
			fmt.Fprintf(out, "  warning: look up space %q: %v\n", match.SpaceSlug, err)
		} else if space.ReleaseTargetID != nil {
			fmt.Fprintf(out, "Clearing release target on Space %q...\n", match.SpaceSlug)
			if err := clusterClearReleaseTarget(space.SpaceID); err != nil {
				fmt.Fprintf(out, "  warning: %v\n", err)
			}
		}

		fmt.Fprintf(out, "Deleting Space %q (recursive: cascades to Unit, Target, Worker)...\n", match.SpaceSlug)
		if err := clusterRetryDelete(30*time.Second, func() error {
			return clusterDeleteSpace(match.SpaceSlug, true)
		}); err != nil {
			fmt.Fprintf(out, "  warning: %v\n", err)
		}
	}

	if err := os.Remove(kubeconfigPath); err != nil && !os.IsNotExist(err) {
		fmt.Fprintf(out, "  warning: removing kubeconfig: %v\n", err)
	}
	envFilePath := clusterEnvFilePath(name)
	if err := os.Remove(envFilePath); err != nil && !os.IsNotExist(err) {
		fmt.Fprintf(out, "  warning: removing env file: %v\n", err)
	}

	fmt.Fprintf(out, "\nDone.\n")
	return nil
}

// clusterRetryDelete retries fn with 2s backoff until it succeeds or the
// budget expires. Used while waiting for the worker connection to drop after
// the kind cluster goes away.
func clusterRetryDelete(budget time.Duration, fn func() error) error {
	deadline := time.Now().Add(budget)
	var lastErr error
	for {
		if err := fn(); err == nil {
			return nil
		} else {
			lastErr = err
		}
		if time.Now().After(deadline) {
			return lastErr
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(2 * time.Second):
		}
	}
}
