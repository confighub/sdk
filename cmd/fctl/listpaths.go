// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"encoding/json"
	"fmt"

	"github.com/confighub/sdk/function/client"
	"github.com/spf13/cobra"
)

func newListPathsCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "listpaths",
		Short: "List registered paths",
		Args:  cobra.ExactArgs(0),
		Run: func(_ /*cmd*/ *cobra.Command, _ []string) {
			respMsg, err := client.GetRegisteredPaths(transportConfig, toolchain)
			failOnError(err)
			out, err := json.MarshalIndent(respMsg, "", "  ")
			failOnError(err)
			fmt.Println(string(out))
		},
	}

	return cmd
}
