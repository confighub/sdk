// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package generic

import (
	"encoding/json"
	"fmt"
	"log/slog"
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
	if err := fh.RegisterFunction("vet-jsonschema", &handler.FunctionRegistration{
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
				{
					ParameterName: "ignore-required",
					Required:      false,
					Description:   "When true, strip the top-level `required` keyword from each schema in the map before validation. Useful when validating a subset of fields whose full schema describes a larger required contract (e.g., a ConfigMap that supplies some but not all of a workload's env vars, where the rest come from a Secret).",
					DataType:      api.DataTypeBool,
					Example:       "true",
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
			return GenericFnVetJSONSchema(resourceProvider, fArgs.Options, fArgs.FunctionContext, fArgs.ParsedData, fArgs.Arguments)
		},
	}); err != nil {
		slog.Error("failed to register function", "error", err)
	}
}

func GenericFnVetJSONSchema(resourceProvider yamlkit.ResourceProvider, options *api.FunctionOptions, _ *api.FunctionContext, parsedData gaby.Container, args []api.FunctionArgument) (gaby.Container, any, error) {
	schemaMapJSON, ok := args[0].Value.(string)
	if !ok {
		return parsedData, api.ValidationResultFalse, errors.New("schema-map must be a string")
	}

	ignoreRequired := false
	if len(args) > 1 {
		if b, ok := args[1].Value.(bool); ok {
			ignoreRequired = b
		}
	}

	// Parse the schema map
	var schemaMap map[string]interface{}
	if err := json.Unmarshal([]byte(schemaMapJSON), &schemaMap); err != nil {
		return parsedData, api.ValidationResultFalse, errors.Wrap(err, "failed to parse schema-map JSON")
	}

	// Strip the top-level `required` keyword from each schema entry when the
	// caller opts in. Useful for partial-contract checks — e.g., when a
	// workload's full env-var schema lists CONFIGHUB_WORKER_ID and
	// CONFIGHUB_WORKER_SECRET as required but those values come from a Secret
	// and only the non-secret subset is being validated here against a
	// ConfigMap. Only the top-level required is removed; nested-object
	// `required` keywords in sub-schemas are left in place.
	if ignoreRequired {
		for resourceType, schemaInterface := range schemaMap {
			if schemaObj, ok := schemaInterface.(map[string]interface{}); ok {
				delete(schemaObj, "required")
				schemaMap[resourceType] = schemaObj
			}
		}
	}

	return VetAgainstSchemas(resourceProvider, options, parsedData, func(resourceType api.ResourceType) (*gojsonschema.Schema, error) {
		schemaInterface, ok := schemaMap[string(resourceType)]
		if !ok {
			// No schema for this resource type, skip validation
			return nil, nil
		}
		schemaBytes, err := json.Marshal(schemaInterface)
		if err != nil {
			return nil, errors.Wrapf(err, "failed to marshal schema for resource type %s", resourceType)
		}
		schema, err := gojsonschema.NewSchema(gojsonschema.NewBytesLoader(schemaBytes))
		if err != nil {
			return nil, errors.Wrapf(err, "invalid schema for resource type %s", resourceType)
		}
		return schema, nil
	})
}

// ErrNoSchema is what a schema lookup returns, wrapped, for a resource that has to have a schema
// and has none. The resource fails validation with it, rather than the function failing.
var ErrNoSchema = errors.New("no schema")

// VetAgainstSchemas validates each resource against the schema schemaFor returns for its type,
// and reports every violation as a failed path. schemaFor returns nil for a type it does not
// validate, and an error wrapping ErrNoSchema for a type that fails for want of one.
func VetAgainstSchemas(resourceProvider yamlkit.ResourceProvider, options *api.FunctionOptions, parsedData gaby.Container,
	schemaFor func(api.ResourceType) (*gojsonschema.Schema, error)) (gaby.Container, any, error) {
	var multiErrs []error
	details := []string{}
	failedPaths := api.AttributeValueList{}
	passed := true

	// Use VisitResources to iterate over each document
	_, err := yamlkit.VisitResourcesFiltered(parsedData, nil, resourceProvider, options, func(doc *gaby.YamlDoc, output any, index int, resourceInfo *api.ResourceInfo) (any, []error) {
		var errs []error

		// Get the resource type
		resourceType := string(resourceInfo.ResourceType)

		schema, err := schemaFor(resourceInfo.ResourceType)
		if errors.Is(err, ErrNoSchema) {
			passed = false
			details = append(details, fmt.Sprintf("Resource %s/%s: %s", resourceType, resourceInfo.ResourceName, err.Error()))
			failedPaths = append(failedPaths, api.AttributeValue{
				AttributeInfo: api.AttributeInfo{
					AttributeIdentifier: api.AttributeIdentifier{ResourceInfo: *resourceInfo, Path: "."},
					AttributeMetadata:   api.AttributeMetadata{AttributeName: api.AttributeNameNone},
				},
			})
			return output, nil
		}
		if err != nil {
			errs = append(errs, err)
			return output, errs
		}
		if schema == nil {
			return output, nil
		}

		// Marshal the document to JSON for validation, stripping $comment$ keys
		docJSON, err := doc.MarshalJSONWithoutCommentKeys()
		if err != nil {
			errs = append(errs, errors.Wrapf(err, "failed to marshal document to JSON for resource %s/%s", resourceType, resourceInfo.ResourceName))
			return output, errs
		}

		// Validate
		result, err := schema.Validate(gojsonschema.NewBytesLoader(docJSON))
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
