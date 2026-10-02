// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"github.com/cockroachdb/errors"
	goclientnew "github.com/confighub/sdk/core/openapi/goclient-new"
	"github.com/spf13/cobra"
)

// invocationFunctionLines holds --function: the functions of an Invocation that runs more than
// one, each written as a function line.
var invocationFunctionLines []string

func addInvocationFunctionFlag(cmd *cobra.Command) {
	cmd.Flags().StringArrayVar(&invocationFunctionLines, "function", nil,
		"function the invocation runs, as <function> [<argument>...], written as on the cub function do command line (repeatable, in the order they run). "+
			"It takes the place of the function given as arguments, for an invocation of more than one function")
}

// queueInvocationFunctionEdits makes the functions --function names the Invocation's functions.
// args are the command's arguments, which name a function of their own from the third on.
func queueInvocationFunctionEdits(args []string) error {
	if len(invocationFunctionLines) == 0 {
		return nil
	}
	if len(args) > 2 {
		return errors.New("give the invocation's function as arguments or with --function, not both")
	}
	invocations := make(goclientnew.FunctionInvocationList, 0, len(invocationFunctionLines))
	for _, line := range invocationFunctionLines {
		invocation, err := parseFunctionLine(line)
		if err != nil {
			return errors.Wrapf(err, "--function %q", line)
		}
		if invocation == nil {
			return errors.Newf("--function %q names no function", line)
		}
		invocations = append(invocations, *invocation)
	}
	edit, err := replaceListEdit("--function", "FunctionInvocations", invocations)
	if err != nil {
		return err
	}
	pendingFieldEdits = append(pendingFieldEdits, edit)
	return nil
}

// attributeParameterFlags holds --parameter on the attribute commands.
var attributeParameterFlags []string

func addAttributeParameterFlag(cmd *cobra.Command) {
	cmd.Flags().StringArrayVar(&attributeParameterFlags, "parameter", nil,
		"parameter of the attribute's getter and setter functions, as name[:datatype[:required]] (datatype defaults to string, required defaults to true; can be repeated). The ones given are the attribute's parameters")
}

// queueAttributeParameterEdits makes the parameters --parameter declares the Attribute's.
func queueAttributeParameterEdits() error {
	parameters, err := parseDeclaredParameterFlags(attributeParameterFlags)
	if err != nil || len(parameters) == 0 {
		return err
	}
	edit, err := replaceListEdit("--parameter", "Parameters", parameters)
	if err != nil {
		return err
	}
	pendingFieldEdits = append(pendingFieldEdits, edit)
	return nil
}
