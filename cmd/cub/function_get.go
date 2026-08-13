// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import "github.com/spf13/cobra"

var functionGetCmd = &cobra.Command{
	Use:         "get <function> [<arg1> ...]",
	Short:       "Run non-mutating functions on units",
	Long:        getCommandHelp(functionGetLong, ""),
	Args:        cobra.MinimumNArgs(0),
	Annotations: map[string]string{"OrgLevel": ""},
	RunE: func(cmd *cobra.Command, args []string) error {
		return runFunctionInvocations(cmd, args, ModeNonMutating)
	},
}

const functionGetLong = `Run one or more non-mutating functions on units.

Only functions whose signature has Mutating=false are accepted (includes both
plain read-only and validating functions). Mutating functions are rejected;
use 'cub function set' for those.

A missing get- prefix is supplied, so 'cub function get replicas' invokes
get-replicas.

Default output is the Outputs section of each function response. Select other
sections with --show: output (default), values, data. --show values strips the
per-unit envelope and emits the raw scalars from an AttributeValueList, which is
usually what you want when reading one attribute across many units.`

func init() {
	registerFunctionVerbFlags(functionGetCmd)
	functionCmd.AddCommand(functionGetCmd)
}
