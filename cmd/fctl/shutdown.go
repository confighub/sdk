// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"github.com/confighub/sdk/function/client"
	"github.com/spf13/cobra"
)

func newShutdownCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "shutdown",
		Short: "Shutdown the function executor",
		Args:  cobra.ExactArgs(0),
		Run: func(_ /*cmd*/ *cobra.Command, _ []string) {
			err := client.Shutdown(transportConfig)
			failOnError(err)
		},
	}

	return cmd
}
