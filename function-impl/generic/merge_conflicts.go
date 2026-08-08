// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package generic

import (
	"fmt"
	"log/slog"
	"strings"

	"github.com/confighub/sdk/core/configkit"
	"github.com/confighub/sdk/core/configkit/yamlkit"
	"github.com/confighub/sdk/core/function/api"
	"github.com/confighub/sdk/core/function/handler"
	"github.com/confighub/sdk/core/third_party/gaby"
)

// registerVetNoMergeConflicts registers the validating function that turns a Unit's
// outstanding merge conflicts into an ApplyGate.
//
// A merge reports what it could not do — a path the downstream owns, a path its protection
// protects, a path it could not locate, a downstream change an upstream deletion displaced
// — and those reports stay on the Unit until someone applies or dismisses them. Whether an
// unresolved merge should stop a Unit from being applied is a policy question, not a
// property of the merge, so it belongs in a Trigger a Space opts into rather than in the
// merge itself.
//
// The conflicts arrive in the FunctionContext, which the server fills only when the
// invocation asks (FunctionInvocationOptions.IncludeConflicts).
func registerVetNoMergeConflicts(fh handler.FunctionRegistry, _ configkit.ConfigConverter, resourceProvider yamlkit.ResourceProvider) {
	if err := fh.RegisterFunction("vet-no-merge-conflicts", &handler.FunctionRegistration{
		FunctionSignature: api.FunctionSignature{
			FunctionName: "vet-no-merge-conflicts",
			Parameters: []api.FunctionParameter{
				{
					ParameterName: "reasons",
					Required:      false,
					Description: "Comma-separated conflict reasons to fail on: Subtracted, DeleteShadowed, " +
						"ProtectedPath, UnresolvedPath, ExclusiveWithheld, ExclusiveCleared. " +
						"Empty fails on any outstanding conflict.",
					DataType: api.DataTypeString,
					Example:  "UnresolvedPath,DeleteShadowed",
				},
			},
			OutputInfo: &api.FunctionOutput{
				ResultName:  "passed",
				Description: "True if the unit has no outstanding merge conflicts of the specified reasons",
				OutputType:  api.OutputTypeValidationResult,
				Schema:      &api.ValidationResultListSchema,
			},
			Mutating:              false,
			Validating:            true,
			Hermetic:              true,
			Idempotent:            true,
			Description:           "Returns true if the unit has no outstanding merge conflicts",
			FunctionType:          api.FunctionTypeCustom,
			AffectedResourceTypes: []api.ResourceType{api.ResourceTypeAny},
		},
		Function: func(fArgs handler.FunctionImplementationArguments) (gaby.Container, any, error) {
			return genericFnVetNoMergeConflicts(resourceProvider, fArgs.FunctionContext, fArgs.ParsedData, fArgs.Arguments)
		},
	}); err != nil {
		slog.Error("failed to register function", "error", err)
	}
}

// parseConflictReasons turns the comma-separated argument into a set. An empty argument
// means every reason.
func parseConflictReasons(arg string) map[api.ConflictReason]struct{} {
	arg = strings.TrimSpace(arg)
	if arg == "" {
		return nil
	}
	reasons := map[api.ConflictReason]struct{}{}
	for _, reason := range strings.Split(arg, ",") {
		if reason = strings.TrimSpace(reason); reason != "" {
			reasons[api.ConflictReason(reason)] = struct{}{}
		}
	}
	return reasons
}

func genericFnVetNoMergeConflicts(_ yamlkit.ResourceProvider, functionContext *api.FunctionContext,
	parsedData gaby.Container, args []api.FunctionArgument) (gaby.Container, any, error) {
	var reasons map[api.ConflictReason]struct{}
	if len(args) > 0 {
		if arg, ok := args[0].Value.(string); ok {
			reasons = parseConflictReasons(arg)
		}
	}

	result := api.ValidationResult{Passed: true}
	for _, conflict := range functionContext.Conflicts {
		if reasons != nil {
			if _, selected := reasons[conflict.Reason]; !selected {
				continue
			}
		}
		result.Passed = false
		result.Issues = append(result.Issues, api.Issue{
			Identifier: string(conflict.Reason),
			Message:    describeConflict(conflict),
		})
	}
	return parsedData, result, nil
}

// describeConflict says what the merge wanted to do and where, in one line.
func describeConflict(conflict api.MutationConflict) string {
	where := string(conflict.Resource.ResourceName)
	if conflict.Path != "" {
		where += " " + string(conflict.Path)
	}
	if conflict.Source.MutationType == "" {
		return fmt.Sprintf("a merge could not apply a change to %s", where)
	}
	return fmt.Sprintf("a merge could not apply %s to %s",
		strings.ToLower(string(conflict.Source.MutationType)), where)
}
