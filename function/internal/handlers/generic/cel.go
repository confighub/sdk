// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package generic

import (
	"fmt"

	"github.com/cockroachdb/errors"
	"github.com/confighub/sdk/configkit"
	"github.com/confighub/sdk/configkit/yamlkit"
	"github.com/confighub/sdk/function/api"
	"github.com/confighub/sdk/function/handler"
	"github.com/confighub/sdk/third_party/gaby"
	"github.com/google/cel-go/cel"
	"github.com/google/cel-go/common/types"
	"sigs.k8s.io/yaml"
)

func registerVetCELExpr(fh handler.FunctionRegistry, converter configkit.ConfigConverter, resourceProvider yamlkit.ResourceProvider) {
	fh.RegisterFunction("vet-celexpr", &handler.FunctionRegistration{
		FunctionSignature: api.FunctionSignature{
			FunctionName: "vet-celexpr",
			Parameters: []api.FunctionParameter{
				{
					ParameterName: "validation-expr",
					Required:      true,
					Description:   "CEL (Common Expression Language) expression to validate each resource. The current resource is referenced with the prefix 'r.' See https://cel.dev/ for language details.",
					DataType:      api.DataTypeCEL,
					// TODO: Override this with ToolchainType-specific examples.
					Example: "r.kind != 'Deployment' || (r.spec.template.spec.securityContext.runAsNonRoot == true && r.spec.template.spec.containers.all(container, !has(container.securityContext.runAsNonRoot) || container.securityContext.runAsNonRoot == true)) || r.spec.template.spec.containers.all(container, has(container.securityContext.runAsNonRoot) && container.securityContext.runAsNonRoot == true)",
				},
			},
			OutputInfo: &api.FunctionOutput{
				ResultName:  "passed",
				Description: "True if validation passed, false otherwise",
				OutputType:  api.OutputTypeValidationResult,
				Schema:      &validationResultListSchema,
			},
			Mutating:              false,
			Validating:            true,
			Hermetic:              true,
			Idempotent:            true,
			Description:           `Returns true if validation expression evaluates to true for all resources`,
			FunctionType:          api.FunctionTypeCustom,
			AffectedResourceTypes: []api.ResourceType{api.ResourceTypeAny},
		},
		Function: func(functionContext *api.FunctionContext, parsedData gaby.Container, args []api.FunctionArgument) (gaby.Container, any, error) {
			return genericFnCELValidate(resourceProvider, functionContext, parsedData, args)
		},
	})
}

// TODO: Deprecated in favor of vet-celexpr. Remove this.
func registerCELValidate(fh handler.FunctionRegistry, converter configkit.ConfigConverter, resourceProvider yamlkit.ResourceProvider) {
	fh.RegisterFunction("cel-validate", &handler.FunctionRegistration{
		FunctionSignature: api.FunctionSignature{
			FunctionName: "cel-validate",
			Parameters: []api.FunctionParameter{
				{
					ParameterName: "validation-expr",
					Required:      true,
					Description:   "CEL (Common Expression Language) expression to validate each resource. The current resource is refenced with the prefix 'r.' See https://cel.dev/ for language details.",
					DataType:      api.DataTypeCEL,
					// TODO: Override this with ToolchainType-specific examples.
					Example: "r.kind != 'Deployment' || r.spec.template.spec.containers.all(container, container.securityContext.runAsNonRoot == true)",
				},
			},
			OutputInfo: &api.FunctionOutput{
				ResultName:  "passed",
				Description: "True if validation passed, false otherwise",
				OutputType:  api.OutputTypeValidationResult,
				Schema:      &validationResultListSchema,
			},
			Mutating:              false,
			Validating:            true,
			Hermetic:              true,
			Idempotent:            true,
			Description:           `[Deprecated; use vet-celexpr instead] Returns true if validation expression evaluates to true for all resources`,
			FunctionType:          api.FunctionTypeCustom,
			AffectedResourceTypes: []api.ResourceType{api.ResourceTypeAny},
		},
		Function: func(functionContext *api.FunctionContext, parsedData gaby.Container, args []api.FunctionArgument) (gaby.Container, any, error) {
			return genericFnCELValidate(resourceProvider, functionContext, parsedData, args)
		},
	})
}

func genericFnCELValidate(resourceProvider yamlkit.ResourceProvider, functionContext *api.FunctionContext, parsedData gaby.Container, args []api.FunctionArgument) (gaby.Container, any, error) {
	validationExpr := args[0].Value.(string)

	env, err := cel.NewEnv(
		cel.Variable("r", cel.DynType),
	)
	if err != nil {
		return parsedData, api.ValidationResultFalse, fmt.Errorf("failed to create CEL environment: %v", err)
	}

	expr, issues := env.Compile(validationExpr)
	if issues != nil {
		return parsedData, api.ValidationResultFalse, fmt.Errorf("failed to compile expression %s: %v", validationExpr, issues)
	}

	if !expr.OutputType().IsExactType(cel.BoolType) {
		return parsedData, api.ValidationResultFalse, fmt.Errorf("expression %s does not evaluate to a boolean", validationExpr)
	}

	program, err := env.Program(expr)
	if err != nil {
		return parsedData, api.ValidationResultFalse, fmt.Errorf("failed to create program for expression %s: %v", validationExpr, err)
	}

	multiErrors := []error{}
	details := []string{}
	passed := true
	for _, doc := range parsedData {
		var dataMap map[string]any
		if err := yaml.Unmarshal(doc.Bytes(), &dataMap); err != nil {
			return parsedData, api.ValidationResultFalse, fmt.Errorf("failed to unmarshal data for config %s: %v", functionContext.UnitSlug, err)
		}

		obj := map[string]any{
			"r": dataMap,
		}

		resourceName, err := resourceProvider.ResourceNameGetter(doc)
		if err != nil {
			multiErrors = append(multiErrors, errors.Wrap(err, "could not extract resource name"))
			resourceName = "unknown"
		}
		val, _, err := program.Eval(obj)
		if err != nil {
			passed = false
			// Treat evaluation errors as expected and just fail the check, rather than parsing or expression errors.
			// There are many such error strings that the evaluator can return, so don't check them all.
			// Example prefixes:
			// "no such key:"
			// "index out of bounds:"
			// "no such attribute(s):"
			errorString := err.Error()
			details = append(details, "validation expression "+validationExpr+" could not be evaluated on resource "+string(resourceName)+": "+errorString)
			continue
		}
		if val != types.True {
			passed = false
			details = append(details, "resource "+string(resourceName)+" failed validation expression "+validationExpr)
		}
	}

	if passed {
		return parsedData, api.ValidationResultTrue, nil
	}

	failedResult := api.ValidationResultFalse
	failedResult.Details = details
	return parsedData, failedResult, errors.Join(multiErrors...)
}
