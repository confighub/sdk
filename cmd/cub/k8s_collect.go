// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"fmt"
	"sort"

	"github.com/confighub/sdk/k8sutil/collect"
	"github.com/spf13/cobra"
	"k8s.io/client-go/tools/clientcmd"
)

var (
	k8sCollectKubeContext string
	k8sCollectKubeconfig  string
	k8sCollectClusterName string
	k8sCollectDryRun      bool
)

var k8sCollectCmd = &cobra.Command{
	Use:   "collect [<target-slug-or-id>]",
	Short: "Collect facts from a Kubernetes cluster and store them on a Target",
	Long: getCommandHelp(`Collect facts about a Kubernetes cluster (version, CRDs, storage classes,
ingress classes) and store them on a Target under the reserved "Cluster." fact prefix.

Collection owns the "Cluster." namespace: it (re)writes those facts and removes stale
ones, while leaving user-defined facts (any key outside the prefix) untouched.
Cluster.Name is not discoverable, so it is set only from --cluster-name or
--kube-context, and otherwise left as it was.

The kubeconfig is loaded with the same precedence as 'cub k8s source':

  1. --kubeconfig flag (highest priority, no merging)
  2. KUBECONFIG environment variable (merges multiple files)
  3. $HOME/.kube/config (default)

Examples:
`+"```"+`
  # Collect facts and store them on a target
  cub k8s collect --space my-space --kube-context kind-kind my-target

  # Preview the facts without updating any target
  cub k8s collect --kube-context kind-kind --dry-run
`+"```"+`
`, ""),
	Args: cobra.MaximumNArgs(1),
	PreRunE: func(cmd *cobra.Command, args []string) error {
		// --dry-run only contacts the cluster, so it needs auth but not a space.
		if k8sCollectDryRun {
			return globalPreRun(cmd, args)
		}
		return spacePreRunE(cmd, args)
	},
	RunE: k8sCollectCmdRun,
}

func init() {
	addSpaceFlags(k8sCollectCmd)
	k8sCollectCmd.Flags().StringVar(&k8sCollectKubeContext, "kube-context", "", "kubeconfig context of the cluster to collect facts from (defaults to the current context)")
	k8sCollectCmd.Flags().StringVar(&k8sCollectKubeconfig, "kubeconfig", "", "path to the kubeconfig file to use")
	k8sCollectCmd.Flags().StringVar(&k8sCollectClusterName, "cluster-name", "", "value for the Cluster.Name fact (defaults to the kube context name)")
	k8sCollectCmd.Flags().BoolVar(&k8sCollectDryRun, "dry-run", false, "print the collected facts without updating any target")
	enableOptionalSpace(k8sCollectCmd)
	k8sCmd.AddCommand(k8sCollectCmd)
}

func k8sCollectCmdRun(cmd *cobra.Command, args []string) error {
	if !k8sCollectDryRun && len(args) == 0 {
		return fmt.Errorf("a target slug or ID is required unless --dry-run is set")
	}

	// Build a rest.Config with the same kubeconfig precedence as `cub k8s source`.
	loadingRules := clientcmd.NewDefaultClientConfigLoadingRules()
	if k8sCollectKubeconfig != "" {
		loadingRules.ExplicitPath = k8sCollectKubeconfig
	}
	overrides := &clientcmd.ConfigOverrides{}
	if k8sCollectKubeContext != "" {
		overrides.CurrentContext = k8sCollectKubeContext
	}
	restConfig, err := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(loadingRules, overrides).ClientConfig()
	if err != nil {
		return fmt.Errorf("failed to get Kubernetes config: %w", err)
	}

	facts, err := collect.Facts(ctx, restConfig)
	if err != nil {
		return err
	}

	// Cluster.Name is not discoverable from the cluster; use the override or the context name.
	clusterName := k8sCollectClusterName
	if clusterName == "" {
		clusterName = k8sCollectKubeContext
	}
	if clusterName != "" {
		facts[collect.NameFact] = clusterName
	}

	if k8sCollectDryRun {
		printCollectedFacts(facts)
		return nil
	}

	currentTarget, err := resolveTarget(args[0], selectedSpaceID, "*")
	if err != nil {
		return err
	}

	target, err := collect.UpdateTarget(ctx, cubClientNew, currentTarget.Target, facts)
	if err != nil {
		return err
	}

	tprint("Collected %d facts and updated target %s", len(facts), target.Slug)
	printCollectedFacts(facts)
	return nil
}

// printCollectedFacts prints the facts as --fact flags, sorted, so the output can be
// copied into a manual `cub target update`.
func printCollectedFacts(facts map[string]string) {
	keys := make([]string, 0, len(facts))
	for k := range facts {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		tprint("--fact '%s=%s'", k, facts[k])
	}
}
