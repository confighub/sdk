// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"github.com/spf13/cobra"
)

var k8sCmd = &cobra.Command{
	Use:   "k8s",
	Short: "Kubernetes commands",
	Long:  getCommandHelp(`The k8s subcommands are used to interact with Kubernetes resources and trace them back to ConfigHub`, ""),
}

func init() {
	rootCmd.AddCommand(k8sCmd)
}
