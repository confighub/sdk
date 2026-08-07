// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"fmt"
	"strings"

	goclientnew "github.com/confighub/sdk/core/openapi/goclient-new"
	"github.com/spf13/cobra"
)

// FunctionKindMode constrains which kinds of functions a verb-scoped command
// (vet/get/set) will accept. ModeAny is the escape hatch used by
// `cub function do` / `cub function exec`.
type FunctionKindMode int

const (
	ModeAny          FunctionKindMode = iota // do / exec: any kind
	ModeValidating                           // vet: Validating=true only
	ModeNonMutating                          // get: Mutating=false
	ModeMutating                             // set: Mutating=true only
)

func (m FunctionKindMode) verb() string {
	switch m {
	case ModeValidating:
		return "vet"
	case ModeNonMutating:
		return "get"
	case ModeMutating:
		return "set"
	}
	return "do"
}

// resolveFunctionNameForVerb implements the verb-prefix convenience:
//
//	cub function set image      → set-image
//	cub function vet placeholders → vet-placeholders
//	cub function get replicas   → get-replicas
//
// The rewrite happens only when all three conditions hold:
//  1. The verb is vet/get/set (ModeAny returns the name unchanged).
//  2. The name does not already start with the corresponding verb prefix.
//  3. The as-typed name is not a registered function but <prefix><name> is.
//
// Signatures are sourced from the on-disk cache populated by listAndSaveFunctions.
// If the initial lookup misses, the cache is refreshed once before giving up.
// When neither form resolves, the original name is returned so the downstream
// kind-validation error message refers to what the user actually typed.
func resolveFunctionNameForVerb(mode FunctionKindMode, name string) string {
	if mode == ModeAny || name == "" {
		return name
	}
	prefix := mode.verb() + "-"
	if strings.HasPrefix(name, prefix) {
		return name
	}

	lookup := func() (string, bool) {
		cached, _ := loadFunctions()
		sigs := flattenFunctionSignatures(cached)
		if _, ok := sigs[name]; ok {
			return name, true
		}
		if _, ok := sigs[prefix+name]; ok {
			return prefix + name, true
		}
		return "", false
	}

	if resolved, ok := lookup(); ok {
		return resolved
	}
	// Cache miss: best-effort refresh, then retry once. Pass --worker through
	// so worker-provided functions participate in the convenience.
	if _, _, err := listAndMaybeSaveFunctions("", workerSlug, "", ""); err == nil {
		if resolved, ok := lookup(); ok {
			return resolved
		}
	}
	if workerSlug != "" {
		// Also refresh the builtin entity so verb-prefix resolution against
		// builtin functions still works when --worker is set but the short
		// name refers to a builtin.
		if _, _, err := listAndMaybeSaveFunctions("", "", "", ""); err == nil {
			if resolved, ok := lookup(); ok {
				return resolved
			}
		}
	}
	return name
}

// validateFunctionKinds rejects function invocations (direct, via triggers, or
// via invocations) whose kind doesn't match the verb's permitted set.
//
// Function signatures come from the on-disk cache populated by
// listAndSaveFunctions. Trigger and Invocation function names come from the
// entities that newFunctionInvocationsRequest already resolved while
// constructing the request body (resolvedTriggers / resolvedInvocations).
func validateFunctionKinds(mode FunctionKindMode, body *goclientnew.FunctionInvocationsRequest) error {
	if mode == ModeAny {
		return nil
	}

	// Gather function names from direct invocations and referenced triggers/invocations.
	var names []string
	if body.FunctionInvocations != nil {
		for _, inv := range *body.FunctionInvocations {
			if inv.FunctionName != "" {
				names = append(names, inv.FunctionName)
			}
		}
	}
	for _, t := range resolvedTriggers {
		if t.FunctionName != "" {
			names = append(names, t.FunctionName)
		}
	}
	for _, inv := range resolvedInvocations {
		if inv.FunctionName != "" {
			names = append(names, inv.FunctionName)
		}
	}

	if len(names) == 0 {
		return fmt.Errorf("no functions specified")
	}

	// Load the cached function signature catalog; refresh if the cache is empty
	// or missing entries we need.
	cached, _ := loadFunctions()
	sigs := flattenFunctionSignatures(cached)
	missing := false
	for _, n := range names {
		if _, ok := sigs[n]; !ok {
			missing = true
			break
		}
	}
	if missing {
		// Best-effort refresh. Include --worker if set so worker-provided
		// functions are validated against their own catalog.
		if _, _, err := listAndMaybeSaveFunctions("", workerSlug, "", ""); err == nil {
			cached, _ = loadFunctions()
			sigs = flattenFunctionSignatures(cached)
		}
		// If the worker refresh didn't resolve everything, top up with builtin.
		stillMissing := false
		for _, n := range names {
			if _, ok := sigs[n]; !ok {
				stillMissing = true
				break
			}
		}
		if stillMissing && workerSlug != "" {
			if _, _, err := listAndMaybeSaveFunctions("", "", "", ""); err == nil {
				cached, _ = loadFunctions()
				sigs = flattenFunctionSignatures(cached)
			}
		}
	}

	for _, n := range names {
		sig, ok := sigs[n]
		if !ok {
			return fmt.Errorf(
				"function %q not found in cached signatures; run 'cub function list' to refresh",
				n,
			)
		}
		if err := checkFunctionKind(mode, n, sig); err != nil {
			return err
		}
	}
	return nil
}

// flattenFunctionSignatures collapses the entity→toolchain→function map into
// a single map keyed by function name. If multiple toolchains define the same
// name, the last one wins; in practice name collisions across toolchains are
// not expected for ConfigHub-managed functions.
func flattenFunctionSignatures(cached functionsByEntity) map[string]goclientnew.FunctionSignature {
	out := map[string]goclientnew.FunctionSignature{}
	for _, byToolchain := range cached {
		for _, byName := range byToolchain {
			for name, sig := range byName {
				out[name] = sig
			}
		}
	}
	return out
}

// functionKindCommandLabel is the command prefix used in kind-mismatch error
// messages. Defaults to the `cub function` verbs; `cub invocation invoke`
// overrides it so its errors name the command the user actually ran.
var functionKindCommandLabel = "cub function"

func checkFunctionKind(mode FunctionKindMode, name string, sig goclientnew.FunctionSignature) error {
	cmd := fmt.Sprintf("%s %s", functionKindCommandLabel, mode.verb())
	switch mode {
	case ModeValidating:
		if !sig.Validating {
			return fmt.Errorf("'%s' only accepts validating functions; %q is not validating", cmd, name)
		}
	case ModeNonMutating:
		if sig.Mutating {
			return fmt.Errorf("'%s' rejects mutating functions; %q is mutating", cmd, name)
		}
	case ModeMutating:
		if !sig.Mutating {
			return fmt.Errorf("'%s' only accepts mutating functions; %q is not mutating", cmd, name)
		}
	}
	return nil
}

// registerFunctionVerbFlags copies the flag set from `functionDoCmd` onto a
// verb-scoped command. This keeps vet/get/set in sync with do's flag surface
// without duplicating registrations.
func registerFunctionVerbFlags(cmd *cobra.Command) {
	// Reuse functionDoCmd's flag registration pattern. We cannot call
	// functionDoCmd.Flags() directly because flag values are shared globals;
	// registering the same flag twice on different commands is fine since
	// spf13/cobra namespaces per-command by pointer.
	cmd.Flags().StringVar(&workerSlug, "worker", "", "worker to execute the function")
	enableShowFlag(cmd)
	cmd.Flags().StringVar(&changeDescription, "change-desc", "", "change description")
	cmd.Flags().StringVar(&functionChangesetSlug, "changeset", "", "changeset to associate units with")
	cmd.Flags().StringSliceVar(&unitIdentifiers, "unit", []string{}, "target specific units by slug or UUID (can be repeated or comma-separated)")
	cmd.Flags().StringVar(&revisionIdentifier, "revision", "", "target a specific revision (format: unit-slug/revision-number, e.g. mydeployment/3)")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "dry run mode: execute functions but skip updating configuration data")
	cmd.Flags().BoolVar(&preservePredicates, "preserve-predicates", false, "keep the stored predicates of the paths this change writes instead of recording new ones: each path keeps the override protection it already has, and a new path is left overwritable")
	cmd.Flags().StringSliceVar(&functionTriggerIdentifiers, "trigger", []string{}, "execute triggers by UUID, slug, or space/slug (can be repeated or comma-separated)")
	cmd.Flags().StringSliceVar(&functionInvocationIdentifiers, "invocation", []string{}, "execute invocations by UUID, slug, or space/slug (can be repeated or comma-separated)")
	cmd.Flags().BoolVar(&updateApplyGates, "update-apply-gates", false, "update ApplyGates on units based on trigger results (requires --trigger)")
	enableWhereFlag(cmd)
	enableFilterFlag(cmd)
	addStandardDisplayFlags(cmd)
	enableWaitFlag(cmd)
	enableDisplayMutationsFlag(cmd)
	cmd.Flags().StringVar(&resourceType, "resource-type", "", "resource-type filter")
	cmd.Flags().StringVar(&whereData, "where-data", "", "where data filter")
	cmd.Flags().StringVar(&whereResource, "where-resource", "", "filter which resources the function operates on")
	cmd.Flags().StringVar(&functionToolchainType, "toolchain", "Kubernetes/YAML", "Toolchain type for the function invocations")
	cmd.Flags().StringVar(&functionLiveStateType, "livestate-type", "", "Invoke the function on the live state and use the flag value as the toolchain type for live state.")
	cmd.Flags().StringVar(&functionOtherDataSource, "other-data-source", "", "additional data source to pass to functions (e.g., LiveRevisionNum)")
	enableOutputFileFlag(cmd)
}
