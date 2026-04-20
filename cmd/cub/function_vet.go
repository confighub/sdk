// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import "github.com/spf13/cobra"

var functionVetCmd = &cobra.Command{
	Use:         "vet <function> [<arg1> ...]",
	Short:       "Run validating functions on units",
	Long:        getCommandHelp(functionVetLong, ""),
	Args:        cobra.MinimumNArgs(0),
	Annotations: map[string]string{"OrgLevel": ""},
	RunE: func(cmd *cobra.Command, args []string) error {
		return runFunctionInvocations(cmd, args, ModeValidating)
	},
}

const functionVetLong = `Run one or more validating functions on units.

Only functions whose signature has Validating=true are accepted; others are
rejected before invocation. Use 'cub function list' to see which functions
are validating.

For invoking non-mutating inspection functions (e.g. get-image), use
'cub function get'. For mutating functions (e.g. set-image), use
'cub function set'. 'cub function do' is the mixed escape hatch.`

func init() {
	registerFunctionVerbFlags(functionVetCmd)
	functionCmd.AddCommand(functionVetCmd)
}
