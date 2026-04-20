// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import "github.com/spf13/cobra"

var functionSetCmd = &cobra.Command{
	Use:         "set <function> [<arg1> ...]",
	Short:       "Run mutating functions on units",
	Long:        getCommandHelp(functionSetLong, ""),
	Args:        cobra.MinimumNArgs(0),
	Annotations: map[string]string{"OrgLevel": ""},
	RunE: func(cmd *cobra.Command, args []string) error {
		return runFunctionInvocations(cmd, args, ModeMutating)
	},
}

const functionSetLong = `Run one or more mutating functions on units.

Only functions whose signature has Mutating=true are accepted; non-mutating
functions are rejected. For inspection use 'cub function get'; for validation
use 'cub function vet'.

Common idioms:
  cub function set --dry-run -o mutations set-image nginx nginx:1.25
  cub function set --show data set-replicas 3`

func init() {
	registerFunctionVerbFlags(functionSetCmd)
	functionCmd.AddCommand(functionSetCmd)
}
