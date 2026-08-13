// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"fmt"

	"github.com/spf13/cobra"
)

// addToolchainToWhereClause AND's a ToolchainType equality constraint into an existing
// where clause. Mirrors addSpaceIDToWhereClause. Empty toolchain returns the clause
// unchanged so --toolchain can be left unset.
func addToolchainToWhereClause(whereClause, toolchain string) string {
	if toolchain == "" {
		return whereClause
	}
	constraint := fmt.Sprintf("ToolchainType = '%s'", toolchain)
	if whereClause == "" {
		return constraint
	}
	return fmt.Sprintf("%s AND %s", whereClause, constraint)
}

var functionCmd = &cobra.Command{
	Use:               "function",
	Short:             "Function commands",
	Long:              getFunctionCommandGroupHelp(),
	PersistentPreRunE: spacePreRunE,
}

func getFunctionCommandGroupHelp() string {
	baseHelp := `The function subcommands are used to discover and execute functions.

Functions are explained at https://docs.confighub.com/background/entities/function/.
A guide for how to use functions is at https://docs.confighub.com/guide/functions/.

Invoke a function with the verb matching its kind. Each verb rejects functions of
the wrong kind before invoking anything, so a mistake is an error rather than an
unintended write, and an agent or role can be granted "function get" alone:

  get   non-mutating functions (Mutating=false), including validating ones:
        get-container-image, get-replicas, get-placeholders, get-resources, get-yq
  set   mutating functions (Mutating=true):
        set-container-image, set-replicas, set-yq, search-replace, ensure-namespaces
  vet   validating functions (Validating=true):
        vet-schemas, vet-placeholders, vet-cel, vet-approvedby
  do    any kind. The escape hatch for a single command that must mix kinds.

The verb supplies a missing prefix, so "function set replicas 3" invokes
set-replicas and "function vet placeholders" invokes vet-placeholders.

Arguments are positional, in signature order. To pass them by name
("--parameter-name=value") put "--" before the function name, so that cub does
not read them as its own flags.

Functions are toolchain-specific (Kubernetes/YAML, AppConfig/Properties, etc.)
and operate on the units selected by --space, --unit, --where, and --where-data.
--where-resource narrows which resources within those units are operated on.`
	agentContext := `Functions operate on configuration data stored in ConfigHub without requiring local file retrieval.

Key workflow for agents:
1. Use 'function list' to discover available functions
2. Use 'function explain FUNCTION_NAME' to understand function parameters
3. Invoke with the verb for the function's kind: 'function get', 'function set',
   or 'function vet'. Reach for 'function do' only when one command must mix kinds.

Verb selection is validated against the signature cache that 'function list'
refreshes, so an unknown function name reports that rather than failing at the
server.

Flags for mutating invocations:
- --change-desc: record why the change was made; it becomes the revision's description
- -o mutations: show the per-path diff the invocation produced
- --dry-run: compute the result without writing it (combine with -o mutations to preview)
- --protect: record the paths this change writes as protected local overrides, so a
  later merge from upstream does not overwrite them. Leave it off when applying a
  value decided elsewhere, such as propagating a release.

Some function names are retained as deprecated aliases and should not be used in
new work: yq (use get-yq), yq-i (use set-yq), set-image/get-image (use
set-container-image/get-container-image), cel-validate (use vet-cel),
no-placeholders (use vet-placeholders), is-approved (use vet-approvedby).
'cub function list' marks each one in its description.`

	return getCommandHelp(baseHelp, agentContext)
}

var executorSpace string

func init() {
	addSpaceFlags(functionCmd)
	functionCmd.PersistentFlags().StringVar(&executorSpace, "executor-space", "", "Space ID or slug whose executor to use for builtin functions (org-level only)")
	rootCmd.AddCommand(functionCmd)
}
