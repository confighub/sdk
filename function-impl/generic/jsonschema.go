// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package generic

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/cockroachdb/errors"
	"github.com/cockroachdb/errors/join"
	"github.com/confighub/sdk/core/configkit"
	"github.com/confighub/sdk/core/configkit/yamlkit"
	"github.com/confighub/sdk/core/function/api"
	"github.com/confighub/sdk/core/function/handler"
	"github.com/confighub/sdk/core/third_party/gaby"
	"github.com/xeipuuv/gojsonschema"
)

func registerVetJSONSchema(fh handler.FunctionRegistry, converter configkit.ConfigConverter, resourceProvider yamlkit.ResourceProvider) {
	fh.RegisterFunction("vet-jsonschema", &handler.FunctionRegistration{
		FunctionSignature: api.FunctionSignature{
			FunctionName: "vet-jsonschema",
			Parameters: []api.FunctionParameter{
				{
					ParameterName: "schema-map",
					Required:      true,
					Description:   "JSON-encoded map from ResourceType to JSONSchema",
					DataType:      api.DataTypeString,
					Example:       "{\"SimpleApp\": {...}}",
				},
			},
			OutputInfo: &api.FunctionOutput{
				ResultName:  "passed",
				Description: "True if all resources pass schema validation, false otherwise",
				OutputType:  api.OutputTypeValidationResult,
				Schema:      &api.ValidationResultListSchema,
			},
			Mutating:              false,
			Validating:            true,
			Hermetic:              true,
			Idempotent:            true,
			Description:           "Validates each resource against its corresponding JSONSchema from the provided map",
			FunctionType:          api.FunctionTypeCustom,
			AffectedResourceTypes: []api.ResourceType{api.ResourceTypeAny},
		},
		Function: func(fArgs handler.FunctionImplementationArguments) (gaby.Container, any, error) {
			return GenericFnVetJSONSchema(resourceProvider, fArgs.FunctionContext, fArgs.ParsedData, fArgs.Arguments)
		},
	})
}

func GenericFnVetJSONSchema(resourceProvider yamlkit.ResourceProvider, _ *api.FunctionContext, parsedData gaby.Container, args []api.FunctionArgument) (gaby.Container, any, error) {
	schemaMapJSON, ok := args[0].Value.(string)
	if !ok {
		return parsedData, api.ValidationResultFalse, errors.New("schema-map must be a string")
	}

	// Parse the schema map
	var schemaMap map[string]interface{}
	if err := json.Unmarshal([]byte(schemaMapJSON), &schemaMap); err != nil {
		return parsedData, api.ValidationResultFalse, errors.Wrap(err, "failed to parse schema-map JSON")
	}

	var multiErrs []error
	details := []string{}
	failedPaths := api.AttributeValueList{}
	passed := true

	// Use VisitResources to iterate over each document
	_, err := yamlkit.VisitResources(parsedData, nil, resourceProvider, func(doc *gaby.YamlDoc, output any, index int, resourceInfo *api.ResourceInfo) (any, []error) {
		var errs []error

		// Get the resource type
		resourceType := string(resourceInfo.ResourceType)

		// Look up the schema for this resource type
		schemaInterface, ok := schemaMap[resourceType]
		if !ok {
			// No schema for this resource type, skip validation
			return output, nil
		}

		// Convert schema to JSON string
		schemaBytes, err := json.Marshal(schemaInterface)
		if err != nil {
			errs = append(errs, errors.Wrapf(err, "failed to marshal schema for resource type %s", resourceType))
			return output, errs
		}

		// Marshal the document to JSON for validation
		docJSON, err := doc.MarshalJSON()
		if err != nil {
			errs = append(errs, errors.Wrapf(err, "failed to marshal document to JSON for resource %s/%s", resourceType, resourceInfo.ResourceName))
			return output, errs
		}

		// Create loaders for gojsonschema
		schemaLoader := gojsonschema.NewStringLoader(string(schemaBytes))
		documentLoader := gojsonschema.NewBytesLoader(docJSON)

		// Validate
		result, err := gojsonschema.Validate(schemaLoader, documentLoader)
		if err != nil {
			errs = append(errs, errors.Wrapf(err, "validation error for resource %s/%s", resourceType, resourceInfo.ResourceName))
			return output, errs
		}

		// Check validation result
		if !result.Valid() {
			passed = false
			for _, desc := range result.Errors() {
				detail := fmt.Sprintf("Resource %s/%s: %s", resourceType, resourceInfo.ResourceName, desc.String())
				details = append(details, detail)

				// Create a failed path entry
				// The field path from gojsonschema is in the format "(root).field.subfield"
				fieldPath := desc.Field()
				// Remove "(root)." prefix if present
				fieldPath = strings.TrimPrefix(fieldPath, "(root).")
				fieldPath = strings.TrimPrefix(fieldPath, "(root)")
				if fieldPath == "" {
					fieldPath = "."
				}

				// For required errors, get the missing property name and construct full path
				var failedValue interface{}
				failedValue = desc.Value()

				if desc.Type() == "required" {
					// Get the missing property name from details
					errorDetails := desc.Details()
					if property, ok := errorDetails["property"]; ok {
						if propertyName, ok := property.(string); ok {
							// Construct the full path to the missing field
							if fieldPath == "" || fieldPath == "." {
								fieldPath = propertyName
							} else {
								fieldPath = fieldPath + "." + propertyName
							}
							// Missing field has nil value
							failedValue = nil
						}
					}
				}

				// TODO: Use AttributeValueForPath to look up the path
				failedPath := api.AttributeValue{
					AttributeInfo: api.AttributeInfo{
						AttributeIdentifier: api.AttributeIdentifier{
							ResourceInfo: *resourceInfo,
							Path:         api.ResolvedPath(fieldPath),
						},
						AttributeMetadata: api.AttributeMetadata{
							AttributeName: api.AttributeNameNone,
						},
					},
					Value: failedValue,
				}
				failedPaths = append(failedPaths, failedPath)
			}
		}

		return output, errs
	})

	if err != nil {
		multiErrs = append(multiErrs, err)
	}

	if passed && len(multiErrs) == 0 {
		return parsedData, api.ValidationResultTrue, nil
	}

	failureResult := api.ValidationResultFalse
	failureResult.Details = details
	failureResult.FailedAttributes = failedPaths

	return parsedData, failureResult, join.Join(multiErrs...)
}
