// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"
)

// invocationParamFlags holds repeated --param name=value flags supplying values
// for the referenced Invocation's declared parameters.
var invocationParamFlags []string

// invocationInvokeCmd groups the verb-scoped execution subcommands. It is a
// separate subcommand (rather than top-level invocation verbs) so the execution
// verbs do not collide with the entity-CRUD `cub invocation get`.
var invocationInvokeCmd = &cobra.Command{
	Use:   "invoke",
	Short: "Execute a stored Invocation (get/set/vet by operation class)",
	Long: getCommandHelp(`Execute a stored Invocation, supplying values for its declared parameters.

The operation class is selected by the verb so that an AI agent's permissions can
be constrained by the command, mirroring 'cub function get/set/vet':

  cub invocation invoke get <invocation>   # non-mutating Invocations only
  cub invocation invoke set <invocation>   # mutating Invocations only
  cub invocation invoke vet <invocation>   # validating Invocations only

To execute fully-bound (non-parameterized) Invocations, possibly several at once,
use 'cub function do --invocation' (and its verb-scoped forms).`, ""),
}

var invocationInvokeGetCmd = &cobra.Command{
	Use:         "get <invocation> [--param name=value ...]",
	Short:       "Execute a non-mutating stored Invocation",
	Long:        getCommandHelp(invocationInvokeLong("get", "non-mutating", "Mutating=false"), ""),
	Args:        cobra.ExactArgs(1),
	Annotations: map[string]string{"OrgLevel": ""},
	RunE: func(cmd *cobra.Command, args []string) error {
		return runInvocationInvoke(cmd, args, ModeNonMutating)
	},
}

var invocationInvokeSetCmd = &cobra.Command{
	Use:         "set <invocation> [--param name=value ...]",
	Short:       "Execute a mutating stored Invocation",
	Long:        getCommandHelp(invocationInvokeLong("set", "mutating", "Mutating=true"), ""),
	Args:        cobra.ExactArgs(1),
	Annotations: map[string]string{"OrgLevel": ""},
	RunE: func(cmd *cobra.Command, args []string) error {
		return runInvocationInvoke(cmd, args, ModeMutating)
	},
}

var invocationInvokeVetCmd = &cobra.Command{
	Use:         "vet <invocation> [--param name=value ...]",
	Short:       "Execute a validating stored Invocation",
	Long:        getCommandHelp(invocationInvokeLong("vet", "validating", "Validating=true"), ""),
	Args:        cobra.ExactArgs(1),
	Annotations: map[string]string{"OrgLevel": ""},
	RunE: func(cmd *cobra.Command, args []string) error {
		return runInvocationInvoke(cmd, args, ModeValidating)
	},
}

func invocationInvokeLong(verb, kind, constraint string) string {
	return fmt.Sprintf(`Execute a single stored Invocation whose underlying function is %s (%s),
supplying values for its declared parameters.

'cub invocation invoke %s' accepts only %s Invocations, so an agent's
permissions can be scoped to this operation class via the command.

Supply each declared parameter with a repeated --param flag:

  cub invocation invoke %s rbac-add-verb \
    --space '*' --where "Space.Labels.Component = 'checkout'" \
    --param verb=create --param role=app-reader --param namespace=prod

The supplied values are validated against the Invocation's declared Parameters
(every required parameter must be present; unknown names are rejected) and become
the scope for expanding the Invocation's templated argument values.`, kind, constraint, verb, kind, verb)
}

func init() {
	for _, c := range []*cobra.Command{invocationInvokeGetCmd, invocationInvokeSetCmd, invocationInvokeVetCmd} {
		registerFunctionVerbFlags(c)
		c.Flags().StringArrayVar(&invocationParamFlags, "param", []string{}, "value for a declared parameter, as name=value (can be repeated)")
		invocationInvokeCmd.AddCommand(c)
	}
	invocationCmd.AddCommand(invocationInvokeCmd)
}

// runInvocationInvoke executes a single parameterized Invocation referenced by
// args[0], supplying --param values, with the verb's kind constraint enforced.
func runInvocationInvoke(cmd *cobra.Command, args []string, mode FunctionKindMode) error {
	params, err := parseInvocationParamFlags(invocationParamFlags)
	if err != nil {
		return err
	}

	// Hand off to the shared function-invocation runner. It builds the request
	// (resolving the Invocation and attaching params via these package vars),
	// enforces the kind constraint, scopes to the selected units, and renders
	// the response. No direct function args are passed.
	parameterizedInvocationIdentifier = args[0]
	parameterizedInvocationParams = params
	functionKindCommandLabel = "cub invocation invoke"
	return runFunctionInvocations(cmd, []string{}, mode)
}

// parseInvocationParamFlags parses repeated name=value strings into a map.
func parseInvocationParamFlags(flags []string) (map[string]any, error) {
	params := map[string]any{}
	for _, kv := range flags {
		name, value, ok := strings.Cut(kv, "=")
		if !ok || name == "" {
			return nil, fmt.Errorf("--param must be in name=value form, got %q", kv)
		}
		params[name] = value
	}
	return params, nil
}
