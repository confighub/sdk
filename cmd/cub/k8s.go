// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"github.com/spf13/cobra"
)

var k8sCmd = &cobra.Command{
	Use:   "k8s",
	Short: "Kubernetes commands",
	Long: getCommandHelp(`The k8s subcommands work with Kubernetes resources.

"get" and "types" read the resources held in ConfigHub Units, naming resource types the way
kubectl does; they show configuration, not live cluster state. "source", "refresh" and
"collect" reach out to a cluster: to trace a live resource back to its Unit, to bring a
resource's cluster-side changes back into that Unit, and to record cluster facts on a
Target. "crd-spec" reads neither, taking a CustomResourceDefinition file and writing the
resource-type spec that teaches ConfigHub how to address a custom resource's fields.`, ""),
}

func init() {
	rootCmd.AddCommand(k8sCmd)
}
