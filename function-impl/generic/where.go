// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package generic

import (
	"fmt"
	"strings"

	"github.com/cockroachdb/errors"
	"github.com/confighub/sdk/configkit"
	"github.com/confighub/sdk/configkit/yamlkit"
	"github.com/confighub/sdk/function/api"
	"github.com/confighub/sdk/function/handler"
	"github.com/confighub/sdk/third_party/gaby"
	"github.com/labstack/gommon/log"
)

func evaluateSplitPathExpressionWithComparators(expression *api.VisitorRelationalExpression, resourceType string, resourceProvider yamlkit.ResourceProvider, parsedData gaby.Container, customComparators []api.CustomStringComparator) (map[api.ResourceTypeAndName]bool, error) {
	matchingResources := map[api.ResourceTypeAndName]bool{}

	// Use VisitPathsDoc to get to the subobjects using the visitor path (left side of |)
	resourceTypeToPaths := yamlkit.GetVisitorMapForPath(resourceProvider, api.ResourceType(resourceType), api.UnresolvedPath(expression.VisitorPath))

	// Custom visitor function that checks the subpath
	visitor := func(doc *gaby.YamlDoc, output any, context yamlkit.VisitorContext, currentDoc *gaby.YamlDoc) (any, error) {
		// Try to get the value at the subpath within this subobject
		value, found, err := yamlkit.YamlSafePathGetValueAnyType(currentDoc, api.ResolvedPath(expression.SubPath), true)

		var matches bool
		if err != nil {
			return output, err
		}

		if !found {
			// Property not present - handle special case for != operator
			if expression.Operator == "!=" {
				matches = true // != always evaluates to true for missing properties
			} else {
				matches = false // Other operators evaluate to false for missing properties
			}
		} else {
			// Property is present - evaluate normally
			var err error
			matches, err = api.EvaluateExpression(&expression.RelationalExpression, value, nil, customComparators)
			if err != nil {
				return output, err
			}
		}

		if matches {
			if existingOutput, ok := output.(map[api.ResourceTypeAndName]bool); ok {
				existingOutput[api.ResourceTypeAndFullNameFromResourceInfo(context.ResourceInfo)] = true
			}
		}

		return output, nil
	}

	_, err := yamlkit.VisitPathsDoc(parsedData, resourceTypeToPaths, []any{}, matchingResources, resourceProvider, visitor, false, nil)
	if err != nil {
		return nil, err
	}

	return matchingResources, nil
}

func registerWhereFilter(fh handler.FunctionRegistry, converter configkit.ConfigConverter, resourceProvider yamlkit.ResourceProvider) {
	fh.RegisterFunction("where-filter", &handler.FunctionRegistration{
		FunctionSignature: api.FunctionSignature{
			FunctionName: "where-filter",
			Parameters: []api.FunctionParameter{
				{
					ParameterName: "resource-type",
					Required:      true,
					Description:   "Resource type (" + resourceProvider.TypeDescription() + ") to match",
					DataType:      api.DataTypeString,
				},
				{
					ParameterName: "where-expression",
					Required:      true,
					Description:   "The specified string is an expression for the purpose of evaluating whether the configuration data matches the filter. It supports conjunctions using `AND` of relational expressions of the form *path* *operator* *literal*. The path specifications are dot-separated, for both map fields and array indices, as in `spec.template.spec.containers.0.image = 'ghcr.io/headlamp-k8s/headlamp:latest' AND spec.replicas > 1`. Path expressions support `*` for wildcard array or map segments and `?key=value` syntax for associative matches of array elements containing objects with a `key` attribute. Strings support the following operators: `<`, `>`, `<=`, `>=`, `=`, `!=`, `LIKE`, `ILIKE`, `~~`, `!~~`, `~`, `!~`, `~*`, `!~*`, `IN`, `NOT IN`. String pattern operators: `LIKE` and `~~` for pattern matching with `%` and `_` wildcards, `ILIKE` for case-insensitive pattern matching, `!~~` for NOT LIKE. String regex operators: `~` for regex matching, `~*` for case-insensitive regex, `!~` and `!~*` for regex not matching (case-sensitive and insensitive). Integers support the following operators: `<`, `>`, `<=`, `>=`, `=`, `!=`, `IN`, `NOT IN`. Boolean values support equality and inequality only. The `IN` and `NOT IN` operators accept a comma-separated list of values in parentheses, such as `spec.template.spec.containers.0.image#reference IN (':latest', ':arm64-latest')`. The syntax `.|` requires the preceding path to exist; otherwise the relation `!=` will always return true regardless what it is compared with. String literals are quoted with single quotes, such as `'string'`. Integer and boolean literals are also supported for attributes of those types.",
					DataType:      api.DataTypeString,
				},
			},
			OutputInfo: &api.FunctionOutput{
				ResultName:  "matched",
				Description: "True if filter passed for at least one resource, false otherwise",
				OutputType:  api.OutputTypeValidationResult,
				Schema:      &validationResultListSchema,
			},
			Mutating:              false,
			Validating:            true,
			Hermetic:              true,
			Idempotent:            true,
			Description:           `Returns true if all terms of the conjunction of relational expressions evaluate to true for at least one matching path of a resource of the specified type. Intended to be used for filtering rather than validating, though it returns the same output type.`,
			FunctionType:          api.FunctionTypeCustom,
			AffectedResourceTypes: []api.ResourceType{api.ResourceTypeAny},
		},
		Function: func(fArgs handler.FunctionImplementationArguments) (gaby.Container, any, error) {
			return genericFnResourceWhereMatch(resourceProvider, fArgs.FunctionContext, fArgs.ParsedData, fArgs.Arguments)
		},
	})
}

func genericFnResourceWhereMatch(resourceProvider yamlkit.ResourceProvider, functionContext *api.FunctionContext, parsedData gaby.Container, args []api.FunctionArgument) (gaby.Container, any, error) {
	return GenericFnResourceWhereMatchWithComparators(resourceProvider, nil, functionContext, parsedData, args)
}

// findMatchingResourcesWithComparators evaluates a where expression against resources and returns
// a map of matching resources keyed by ResourceTypeAndName. It also handles the special case of
// blank whereExpr where it filters by resourceType only.
func findMatchingResourcesWithComparators(resourceProvider yamlkit.ResourceProvider, customComparators []api.CustomStringComparator, functionContext *api.FunctionContext, parsedData gaby.Container, resourceType string, whereExpr string) (map[api.ResourceTypeAndName]bool, error) {
	// Allow empty resourceType
	if resourceType == "" {
		resourceType = "*"
	}
	// Allow blank whereExpr: filter by resourceType only
	if strings.TrimSpace(whereExpr) == "" {
		matchingResources := map[api.ResourceTypeAndName]bool{}
		_, err := yamlkit.VisitResources(parsedData, nil, resourceProvider, func(doc *gaby.YamlDoc, output any, index int, resourceInfo *api.ResourceInfo) (any, []error) {
			if resourceType == "*" || resourceInfo.ResourceType == api.ResourceType(resourceType) {
				matchingResources[api.ResourceTypeAndFullNameFromResourceInfo(*resourceInfo)] = true
			}
			return output, nil
		})
		if err != nil {
			return nil, err
		}
		return matchingResources, nil
	}

	expressions, err := api.ParseAndValidateWhereFilter(whereExpr)
	if err != nil {
		return nil, err
	}
	// Visit and evaluate.
	// If we allow wildcards, then theoretically the evaluation could be combinatoric to compare
	// every combination of matching paths. Luckily because we support only conjunctions, which
	// are commutative, we don't need to compare every combination. We can compare them independently
	// in any order. If any expression evaluates to false for a path that exists, then the resource
	// is not a match. However, if any resource does match, then the config Unit should match.
	// We could provide another function that accepts multiple expressions and applies a top-level
	// disjunction to them to allow for selection (e.g., based on resource type) and validation.
	// With exactly 2 expressions we could pass validation if !match_expr || validate_expr.
	var multiErrs []error
	var output any
	matchingResources := map[api.ResourceTypeAndName]bool{}
	for i, expression := range expressions {
		// The visitor functions visit all resources of the specified type.
		// We need to keep track of which resources have matched.
		// If no paths are found for a resource, that's not a match.
		// If there are errors finding any paths, that's not a match.

		if expression.IsSplitPath {
			// Handle split path syntax with .| separator
			matchingResourcesForExpression, err := evaluateSplitPathExpressionWithComparators(expression, resourceType, resourceProvider, parsedData, customComparators)
			if err != nil {
				multiErrs = append(multiErrs, err)
				matchingResources = nil
				break
			}
			if i == 0 {
				matchingResources = matchingResourcesForExpression
			} else {
				for key := range matchingResources {
					_, matched := matchingResourcesForExpression[key]
					if !matched {
						delete(matchingResources, key)
					}
				}
			}
		} else if strings.HasPrefix(expression.Path, "ConfigHub.") {
			// Handle ConfigHub.* paths that reference ResourceInfo fields
			matchingResourcesForExpression := map[api.ResourceTypeAndName]bool{}
			_, err := yamlkit.VisitResources(parsedData, nil, resourceProvider, func(doc *gaby.YamlDoc, output any, index int, resourceInfo *api.ResourceInfo) (any, []error) {
				// Skip resources that don't match the resource type filter
				if resourceType != "*" && resourceInfo.ResourceType != api.ResourceType(resourceType) {
					return output, nil
				}
				var leftValue any
				switch expression.Path {
				case "ConfigHub.ResourceName":
					leftValue = string(resourceInfo.ResourceName)
				case "ConfigHub.ResourceNameWithoutScope":
					leftValue = string(resourceInfo.ResourceNameWithoutScope)
				case "ConfigHub.ResourceType":
					leftValue = string(resourceInfo.ResourceType)
				case "ConfigHub.ResourceCategory":
					leftValue = string(resourceInfo.ResourceCategory)
				default:
					return output, []error{fmt.Errorf("unsupported ConfigHub path: %s", expression.Path)}
				}
				found, err := api.EvaluateExpression(&expression.RelationalExpression, leftValue, nil, customComparators)
				if err != nil {
					return output, []error{err}
				}
				if found {
					matchingResourcesForExpression[api.ResourceTypeAndFullNameFromResourceInfo(*resourceInfo)] = true
				}
				return output, nil
			})
			if err != nil {
				multiErrs = append(multiErrs, err)
				matchingResources = nil
				break
			}
			if i == 0 {
				matchingResources = matchingResourcesForExpression
			} else {
				for key := range matchingResources {
					_, matched := matchingResourcesForExpression[key]
					if !matched {
						delete(matchingResources, key)
					}
				}
			}
		} else {
			// Handle original path syntax
			getterArgs := make([]api.FunctionArgument, 2)
			getterArgs[0].Value = resourceType
			getterArgs[1].Value = expression.Path
			switch expression.DataType {
			case api.DataTypeString:
				_, output, err = GenericFnGetStringPath(resourceProvider, functionContext, parsedData, getterArgs)
			case api.DataTypeInt:
				_, output, err = GenericFnGetIntPath(resourceProvider, functionContext, parsedData, getterArgs)
			case api.DataTypeBool:
				_, output, err = GenericFnGetBoolPath(resourceProvider, functionContext, parsedData, getterArgs)
			default:
				err = fmt.Errorf("unsupported data type %s", expression.DataType)
			}
			if err != nil {
				multiErrs = append(multiErrs, err)
				matchingResources = nil
				break
			}

			matchingResourcesForExpression := map[api.ResourceTypeAndName]bool{}
			attribValues, ok := output.(api.AttributeValueList)
			if !ok {
				log.Errorf("couldn't convert output to api.AttributeValueList")
				multiErrs = append(multiErrs, fmt.Errorf("internal error"))
				continue
			}
			for _, attribValue := range attribValues {
				//fmt.Printf("path: %s\n", attribValue.Path)
				found, err := api.EvaluateExpression(&expression.RelationalExpression, attribValue.Value, nil, customComparators)
				if err != nil {
					multiErrs = append(multiErrs, err)
				} else if found {
					matchingResourcesForExpression[api.ResourceTypeAndFullNameFromResourceInfo(attribValue.ResourceInfo)] = true
				}
			}
			if i == 0 {
				matchingResources = matchingResourcesForExpression
			} else {
				for key := range matchingResources {
					_, matched := matchingResourcesForExpression[key]
					if !matched {
						delete(matchingResources, key)
					}
				}
			}
		}
	}
	if len(multiErrs) != 0 {
		err = errors.Join(multiErrs...)
		return nil, err
	}
	return matchingResources, nil
}

func GenericFnResourceWhereMatchWithComparators(resourceProvider yamlkit.ResourceProvider, customComparators []api.CustomStringComparator, functionContext *api.FunctionContext, parsedData gaby.Container, args []api.FunctionArgument) (gaby.Container, any, error) {
	resourceType := args[0].Value.(string)
	whereExpr := args[1].Value.(string)

	matchingResources, err := findMatchingResourcesWithComparators(resourceProvider, customComparators, functionContext, parsedData, resourceType, whereExpr)
	if err != nil {
		return parsedData, api.ValidationResultFalse, err
	}
	if len(matchingResources) > 0 {
		return parsedData, api.ValidationResultTrue, nil
	}
	return parsedData, api.ValidationResultFalse, nil
}

func registerSelectWhereResource(fh handler.FunctionRegistry, converter configkit.ConfigConverter, resourceProvider yamlkit.ResourceProvider) {
	fh.RegisterFunction("select-where-resource", &handler.FunctionRegistration{
		FunctionSignature: api.FunctionSignature{
			FunctionName: "select-where-resource",
			Parameters: []api.FunctionParameter{
				{
					ParameterName: "resource-type",
					Required:      true,
					Description:   "Resource type (" + resourceProvider.TypeDescription() + ") to match",
					DataType:      api.DataTypeString,
				},
				{
					ParameterName: "where-expression",
					Required:      true,
					Description:   `Conjunction ("AND") of relational expressions applied to field paths that may contain wildcards; e.g., ".spec.template.spec.containers.*.image CONTAINS 'nginx' AND .metadata.namespace == 'default'"`,
					DataType:      api.DataTypeString,
				},
			},
			OutputInfo:            nil,
			Mutating:              true,
			Validating:            false,
			Hermetic:              true,
			Idempotent:            true,
			Description:           "Returns resources for which all terms of the conjunction of relational expressions evaluate to true",
			FunctionType:          api.FunctionTypeCustom,
			AffectedResourceTypes: []api.ResourceType{api.ResourceTypeAny},
		},
		Function: func(fArgs handler.FunctionImplementationArguments) (gaby.Container, any, error) {
			return genericFnSelectWhereResource(resourceProvider, fArgs.FunctionContext, fArgs.ParsedData, fArgs.Arguments)
		},
	})
}

func genericFnSelectWhereResource(resourceProvider yamlkit.ResourceProvider, functionContext *api.FunctionContext, parsedData gaby.Container, args []api.FunctionArgument) (gaby.Container, any, error) {
	return GenericFnSelectWhereResourceWithComparators(resourceProvider, nil, functionContext, parsedData, args)
}

func GenericFnSelectWhereResourceWithComparators(resourceProvider yamlkit.ResourceProvider, customComparators []api.CustomStringComparator, functionContext *api.FunctionContext, parsedData gaby.Container, args []api.FunctionArgument) (gaby.Container, any, error) {
	resourceType := args[0].Value.(string)
	whereExpr := args[1].Value.(string)

	matchingResources, err := findMatchingResourcesWithComparators(resourceProvider, customComparators, functionContext, parsedData, resourceType, whereExpr)
	if err != nil {
		return parsedData, nil, err
	}

	// Build a list of indices to remove (in reverse order to avoid index shifting)
	indicesToRemove := []int{}
	_, err = yamlkit.VisitResources(parsedData, nil, resourceProvider, func(doc *gaby.YamlDoc, output any, index int, resourceInfo *api.ResourceInfo) (any, []error) {
		key := api.ResourceTypeAndFullNameFromResourceInfo(*resourceInfo)
		if !matchingResources[key] {
			indicesToRemove = append(indicesToRemove, index)
		}
		return output, nil
	})
	if err != nil {
		return parsedData, nil, fmt.Errorf("failed to identify resources to remove: %v", err)
	}

	// Remove resources in reverse order to avoid index shifting
	newParsedData := parsedData
	for i := len(indicesToRemove) - 1; i >= 0; i-- {
		idx := indicesToRemove[i]
		newSlice := make(gaby.Container, len(newParsedData)-1)
		copy(newSlice[:idx], newParsedData[:idx])
		copy(newSlice[idx:], newParsedData[idx+1:])
		newParsedData = newSlice
	}

	return newParsedData, nil, nil
}
