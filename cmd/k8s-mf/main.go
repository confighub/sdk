// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

// Command k8s-mf inspects and repairs Kubernetes server-side-apply managed
// fields. It makes apply operations less surprising and helps debug and fix
// managed-field problems that arise when you "break glass" with kubectl or
// transition a resource between tools (kubectl apply, the ConfigHub bridge,
// ArgoCD, Flux, …).
//
// Subcommands:
//
//	categories     show which fields each category of field manager owns
//	values         show the values of fields owned by appliers
//	takeover       remove other appliers' managers so one applier owns the resource
//	dry-run-apply  server-side dry-run an apply as a given field manager
package main

import (
	"errors"
	"log"

	"github.com/fatih/color"
	"github.com/spf13/cobra"
)

var (
	flagKubeconfig string
	flagContext    string
	flagNamespace  string
)

var rootCmd = &cobra.Command{
	Use:   "k8s-mf",
	Short: "Inspect and repair Kubernetes server-side-apply managed fields",
	Long: `k8s-mf inspects and repairs Kubernetes server-side-apply (SSA) managed fields.

Managed fields record which field manager owns each field of a resource. Leftover
or competing managers are the usual cause of apply surprises — fields silently
retained, deletions blocked, or apply conflicts — especially after a kubectl
"break glass" edit or a transition between tools (kubectl apply, the ConfigHub
bridge, ArgoCD, Flux, Sveltos).

The kubeconfig is loaded with kubectl precedence: --kubeconfig flag, then the
KUBECONFIG environment variable, then $HOME/.kube/config.`,
	SilenceUsage:  true,
	SilenceErrors: true,
}

func main() {
	rootCmd.PersistentFlags().StringVar(&flagKubeconfig, "kubeconfig", "", "Path to the kubeconfig file (overrides KUBECONFIG and the default)")
	rootCmd.PersistentFlags().StringVar(&flagContext, "context", "", "Kubernetes context to use")
	rootCmd.PersistentFlags().StringVarP(&flagNamespace, "namespace", "n", "default", "Namespace of the resource (ignored for cluster-scoped resources)")

	rootCmd.AddCommand(newCategoriesCommand())
	rootCmd.AddCommand(newValuesCommand())
	rootCmd.AddCommand(newCleanupCommand())
	rootCmd.AddCommand(newConflictsCommand())
	rootCmd.AddCommand(newTakeoverCommand())
	rootCmd.AddCommand(newDryRunApplyCommand())

	failOnError(rootCmd.Execute())
}

func failOnError(err error) {
	if err == nil {
		return
	}
	red := color.New(color.FgRed).Add(color.Bold).SprintFunc()
	log.Fatal(errors.New(red(err.Error())))
}
