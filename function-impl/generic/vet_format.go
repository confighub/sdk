// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package generic

import (
	"log/slog"

	"github.com/confighub/sdk/core/configkit"
	"github.com/confighub/sdk/core/configkit/yamlkit"
	"github.com/confighub/sdk/core/function/api"
	"github.com/confighub/sdk/core/function/handler"
	"github.com/confighub/sdk/core/third_party/gaby"
)

func registerVetFormat(fh handler.FunctionRegistry, converter configkit.ConfigConverter, resourceProvider yamlkit.ResourceProvider) {
	if err := fh.RegisterFunction("vet-format", &handler.FunctionRegistration{
		FunctionSignature: api.FunctionSignature{
			FunctionName: "vet-format",
			OutputInfo: &api.FunctionOutput{
				ResultName:  "passed",
				Description: "True if no YAML format issues found, false otherwise",
				OutputType:  api.OutputTypeValidationResult,
				Schema:      &api.ValidationResultListSchema,
			},
			Mutating:              false,
			Validating:            true,
			Hermetic:              true,
			Idempotent:            true,
			Description:           "Validates YAML format: no anchors/aliases, no empty values, no duplicate keys, no truthy values (yes/no/on/off/y/n), no old-style octals (0755).",
			FunctionType:          api.FunctionTypeCustom,
			AffectedResourceTypes: []api.ResourceType{api.ResourceTypeAny},
		},
		Function: func(fArgs handler.FunctionImplementationArguments) (gaby.Container, any, error) {
			return genericFnVetFormat(resourceProvider, fArgs.Options, fArgs.ParsedData)
		},
	}); err != nil {
		slog.Error("failed to register function", "error", err)
	}
}

func genericFnVetFormat(resourceProvider yamlkit.ResourceProvider, options *api.FunctionOptions, parsedData gaby.Container) (gaby.Container, any, error) {
	config := yamlkit.DefaultLintConfig()
	var failedAttributes api.AttributeValueList

	whereExpressions := api.GetWhereResourceExpressions(options)
	_, err := yamlkit.VisitResourcesFiltered(parsedData, nil, resourceProvider, whereExpressions,
		func(doc *gaby.YamlDoc, output any, index int, resourceInfo *api.ResourceInfo) (any, []error) {
			ynode := doc.YNode()
			if ynode == nil {
				return output, nil
			}

			findings := yamlkit.LintNode(ynode, config)
			for _, f := range findings {
				attr := api.AttributeValue{
					AttributeInfo: api.AttributeInfo{
						AttributeIdentifier: api.AttributeIdentifier{
							ResourceInfo: *resourceInfo,
							Path:         api.ResolvedPath(f.Path),
						},
						AttributeMetadata: api.AttributeMetadata{
							AttributeName: api.AttributeNameNone,
						},
					},
					Issues: []api.Issue{{
						Identifier: f.Rule,
						Message:    f.Message,
					}},
				}
				failedAttributes = append(failedAttributes, attr)
			}
			return output, nil
		})

	if err != nil {
		return parsedData, api.ValidationResultFalse, err
	}

	if len(failedAttributes) == 0 {
		return parsedData, api.ValidationResultTrue, nil
	}

	result := api.ValidationResult{
		Passed:           false,
		FailedAttributes: failedAttributes,
	}
	return parsedData, result, nil
}
