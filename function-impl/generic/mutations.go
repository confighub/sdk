// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package generic

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"

	"github.com/swaggest/jsonschema-go"

	"github.com/confighub/sdk/core/configkit"
	"github.com/confighub/sdk/core/configkit/yamlkit"
	"github.com/confighub/sdk/core/function/api"
	"github.com/confighub/sdk/core/function/handler"
	"github.com/confighub/sdk/core/third_party/gaby"
)

func registerPatchMutations(fh handler.FunctionRegistry, converter configkit.ConfigConverter, resourceProvider yamlkit.ResourceProvider) {
	reflector := jsonschema.Reflector{}
	conflictListSchema, err := reflector.Reflect(api.MutationConflictList{})
	if err != nil {
		slog.Error("couldn't get schema for api.MutationConflictList", "error", err)
	}
	if err := fh.RegisterFunction("patch-mutations", &handler.FunctionRegistration{
		FunctionSignature: api.FunctionSignature{
			FunctionName: "patch-mutations",
			Parameters: []api.FunctionParameter{
				{
					ParameterName:    "mutation-protection",
					Required:         true,
					Description:      "The target's MutationSources, whose Protected flags say which paths the patch may not overwrite",
					DataType:         api.DataTypeResourceMutationList,
					ValueConstraints: api.ValueConstraints{Schema: &api.ResourceMutationListSchema},
				},
				{
					ParameterName:    "mutation-patch",
					Required:         true,
					Description:      "Mutations to filter and patch",
					DataType:         api.DataTypeResourceMutationList,
					ValueConstraints: api.ValueConstraints{Schema: &api.ResourceMutationListSchema},
				},
				{
					ParameterName:    "mutations-to-subtract",
					Required:         false,
					Description:      "Mutations to subtract from the patch (e.g., target-side changes relative to a merge base) before applying. Pass an empty list or omit to skip subtraction.",
					DataType:         api.DataTypeResourceMutationList,
					ValueConstraints: api.ValueConstraints{Schema: &api.ResourceMutationListSchema},
				},
				{
					ParameterName: "path-annotations",
					Required:      false,
					Description:   "The target's PathAnnotations, whose guards say which paths this operation must be cleared for before it may write them. Pass an empty list or omit when the target has none, which costs nothing.",
					DataType:      api.DataTypeString,
				},
				{
					ParameterName: "clearance",
					Required:      false,
					Description:   "The classes of reason this operation is cleared for, as a JSON Clearance. An absent or empty clearance clears nothing, so every guarded path is withheld.",
					DataType:      api.DataTypeString,
				},
			},
			OutputInfo: &api.FunctionOutput{
				ResultName:  "conflicts",
				Description: "Mutations from the patch that were dropped (subtracted by the target, blocked by the target's protection, or unresolvable against the target's data) and any target-side mutations the patch erased. Empty when the patch applied cleanly.",
				OutputType:  api.OutputTypeMutationConflictList,
				Schema:      &conflictListSchema,
			},
			Mutating:              true,
			Validating:            false,
			Hermetic:              true,
			Idempotent:            true,
			Description:           "Selectively patch attributes if their mutations indicate they are patchable. Intended to be used with compute-mutations.",
			FunctionType:          api.FunctionTypeCustom,
			AffectedResourceTypes: []api.ResourceType{api.ResourceTypeAny},
		},
		Function: func(fArgs handler.FunctionImplementationArguments) (gaby.Container, any, error) {
			return genericFnPatchMutations(resourceProvider, fArgs.FunctionContext, fArgs.ParsedData, fArgs.Arguments, fArgs.Options)
		},
	}); err != nil {
		slog.Error("failed to register function", "error", err)
	}
}

func genericFnPatchMutations(resourceProvider yamlkit.ResourceProvider, _ *api.FunctionContext, parsedData gaby.Container, args []api.FunctionArgument, options *api.FunctionOptions) (gaby.Container, any, error) {
	mutationProtectionString := args[0].Value.(string)
	var mutationsProtection api.ResourceMutationList
	err := json.Unmarshal([]byte(mutationProtectionString), &mutationsProtection)
	if err != nil {
		return parsedData, nil, err
	}
	mutationPatchString := args[1].Value.(string)
	var mutationsPatch api.ResourceMutationList
	err = json.Unmarshal([]byte(mutationPatchString), &mutationsPatch)
	if err != nil {
		return parsedData, nil, err
	}

	var mutationsToSubtract api.ResourceMutationList
	if len(args) > 2 {
		if subtractString, ok := args[2].Value.(string); ok && subtractString != "" {
			if err := json.Unmarshal([]byte(subtractString), &mutationsToSubtract); err != nil {
				return parsedData, nil, err
			}
		}
	}

	// The guard filter is optional on both halves: a target with no annotations needs no
	// clearance, and an operation with no clearance is still cleared for every unguarded path.
	var guards *yamlkit.GuardFilter
	if len(args) > 3 {
		if annotationsString, ok := args[3].Value.(string); ok && annotationsString != "" {
			var annotations api.PathAnnotationList
			if err := json.Unmarshal([]byte(annotationsString), &annotations); err != nil {
				return parsedData, nil, err
			}
			guards = &yamlkit.GuardFilter{Annotations: annotations}
		}
	}
	if len(args) > 4 && guards != nil {
		if clearanceString, ok := args[4].Value.(string); ok && clearanceString != "" {
			if err := json.Unmarshal([]byte(clearanceString), &guards.Clearance); err != nil {
				return parsedData, nil, err
			}
		}
	}

	parsedData, conflicts, err := yamlkit.PatchMutationsGuarded(parsedData, mutationsProtection, mutationsPatch, mutationsToSubtract, guards, resourceProvider, options)
	if err != nil {
		return parsedData, nil, err
	}
	// Return nil rather than an empty slice when there's nothing to report so
	// the executor doesn't surface an empty Output entry on a clean patch.
	if len(conflicts) == 0 {
		return parsedData, nil, nil
	}
	return parsedData, conflicts, nil
}

func registerSetProtection(fh handler.FunctionRegistry, converter configkit.ConfigConverter, resourceProvider yamlkit.ResourceProvider) {
	if err := fh.RegisterFunction("set-protection", &handler.FunctionRegistration{
		FunctionSignature: api.FunctionSignature{
			FunctionName: "set-protection",
			Parameters: []api.FunctionParameter{
				{
					ParameterName:    "mutation-sources",
					Required:         true,
					Description:      "The unit's current MutationSources",
					DataType:         api.DataTypeResourceMutationList,
					ValueConstraints: api.ValueConstraints{Schema: &api.ResourceMutationListSchema},
				},
				{
					ParameterName: "resource-protection",
					Required:      true,
					Description:   "JSON-encoded list of per-resource path Protected values to set ([]api.ResourceProtection)",
					DataType:      api.DataTypeString,
				},
			},
			OutputInfo: &api.FunctionOutput{
				ResultName:  "mutation-sources",
				Description: "The updated MutationSources with the requested Protected values set.",
				OutputType:  api.OutputTypeResourceMutationList,
				Schema:      &api.ResourceMutationListSchema,
			},
			Mutating:              false,
			Validating:            false,
			Hermetic:              true,
			Idempotent:            true,
			Description:           "Set Protected values on a unit's MutationSources for the given resource paths, marking them local overrides a merge must not overwrite. Fails if any path does not exist in the unit's data.",
			FunctionType:          api.FunctionTypeCustom,
			AffectedResourceTypes: []api.ResourceType{api.ResourceTypeAny},
		},
		Function: func(fArgs handler.FunctionImplementationArguments) (gaby.Container, any, error) {
			return genericFnSetProtection(resourceProvider, fArgs.ParsedData, fArgs.Arguments)
		},
	}); err != nil {
		slog.Error("failed to register function", "error", err)
	}
}

func genericFnSetProtection(resourceProvider yamlkit.ResourceProvider, parsedData gaby.Container, args []api.FunctionArgument) (gaby.Container, any, error) {
	mutationSourcesString := args[0].Value.(string)
	var mutationSources api.ResourceMutationList
	if err := json.Unmarshal([]byte(mutationSourcesString), &mutationSources); err != nil {
		return parsedData, nil, err
	}
	resourceProtectionString := args[1].Value.(string)
	var resourceProtection []api.ResourceProtection
	if err := json.Unmarshal([]byte(resourceProtectionString), &resourceProtection); err != nil {
		return parsedData, nil, err
	}

	var unresolved []api.ResolvedPath
	for _, rp := range resourceProtection {
		var u []api.ResolvedPath
		mutationSources, u = yamlkit.SetProtection(parsedData, mutationSources, rp.Resource, rp.Protected, resourceProvider)
		unresolved = append(unresolved, u...)
	}
	if len(unresolved) > 0 {
		strs := make([]string, len(unresolved))
		for i, p := range unresolved {
			strs[i] = string(p)
		}
		return parsedData, nil, fmt.Errorf("paths not found in unit data: %s", strings.Join(strs, ", "))
	}
	// Non-mutating: parsedData is returned unchanged; the updated MutationSources is the output.
	return parsedData, mutationSources, nil
}
