// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"github.com/confighub/sdk/function/client"
	"github.com/spf13/cobra"
)

func newOkCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "ok",
		Short: "Check whether the function executor responds",
		Args:  cobra.ExactArgs(0),
		Run: func(_ /*cmd*/ *cobra.Command, _ []string) {
			err := client.Ok(transportConfig)
			failOnError(err)
		},
	}

	return cmd
}
