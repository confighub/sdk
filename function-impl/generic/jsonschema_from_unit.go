// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package generic

import (
	"encoding/json"
	"log/slog"
	"strings"

	"github.com/cockroachdb/errors"
	"github.com/xeipuuv/gojsonschema"

	"github.com/confighub/sdk/core/configkit/yamlkit"
	"github.com/confighub/sdk/core/function/api"
	"github.com/confighub/sdk/core/function/handler"
	"github.com/confighub/sdk/core/third_party/gaby"
	"github.com/confighub/sdk/core/workerapi"
)

// SchemaOtherDataKey is the key of the OtherData vet-jsonschema-from-unit reads its schema from.
const SchemaOtherDataKey = api.OtherDataSource("schema")

func registerVetJSONSchemaFromUnit(fh handler.FunctionRegistry, resourceProvider yamlkit.ResourceProvider) {
	if err := fh.RegisterFunction("vet-jsonschema-from-unit", &handler.FunctionRegistration{
		FunctionSignature: api.FunctionSignature{
			FunctionName: "vet-jsonschema-from-unit",
			Parameters: []api.FunctionParameter{
				{
					ParameterName: "ignore-required",
					Required:      false,
					Description:   "When true, strip the top-level `required` keyword from the schema before validation, as vet-jsonschema's ignore-required does.",
					DataType:      api.DataTypeBool,
					Example:       "true",
				},
			},
			OtherDataExpected: []api.OtherDataSource{SchemaOtherDataKey},
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
			Description:           "Validates each resource against the JSON Schema held by the Unit passed as OtherData schema, such as by a Trigger's OtherUnitIDs. Narrow the resources it checks with WhereResource.",
			FunctionType:          api.FunctionTypeCustom,
			AffectedResourceTypes: []api.ResourceType{api.ResourceTypeAny},
		},
		Function: func(fArgs handler.FunctionImplementationArguments) (gaby.Container, any, error) {
			ignoreRequired := false
			if len(fArgs.Arguments) > 0 {
				ignoreRequired, _ = fArgs.Arguments[0].Value.(bool)
			}
			return GenericFnVetJSONSchemaFromUnit(resourceProvider, fArgs.Options, fArgs.FunctionContext,
				fArgs.ParsedData, fArgs.ParsedOtherData[SchemaOtherDataKey], ignoreRequired)
		},
	}); err != nil {
		slog.Error("failed to register function", "error", err)
	}
}

// GenericFnVetJSONSchemaFromUnit validates each resource of parsedData against the JSON Schema
// schemaData holds, the data of the Unit passed as OtherData schema.
func GenericFnVetJSONSchemaFromUnit(resourceProvider yamlkit.ResourceProvider, options *api.FunctionOptions,
	functionContext *api.FunctionContext, parsedData, schemaData gaby.Container, ignoreRequired bool) (gaby.Container, any, error) {
	schema, err := schemaFromOtherData(schemaData, ignoreRequired)
	if err != nil {
		return parsedData, api.ValidationResultFalse, err
	}

	// As in vet-jsonschema: an AppConfig Unit's configHub metadata is not part of the
	// configuration the schema describes.
	metadataPath := ""
	if functionContext != nil && workerapi.IsAppConfigToolchain(functionContext.ToolchainType) {
		metadataPath = strings.TrimSuffix(resourceProvider.ContextPath(""), ".")
	}
	return VetAgainstSchemas(resourceProvider, options, parsedData, metadataPath, func(api.ResourceType) (*gojsonschema.Schema, error) {
		return schema, nil
	})
}

// schemaFromOtherData compiles the one JSON Schema the schema Unit's data holds.
func schemaFromOtherData(schemaData gaby.Container, ignoreRequired bool) (*gojsonschema.Schema, error) {
	var documents []*gaby.YamlDoc
	for _, doc := range schemaData {
		if doc != nil && doc.Data() != nil {
			documents = append(documents, doc)
		}
	}
	switch len(documents) {
	case 0:
		return nil, errors.Newf("no schema to validate against: pass a Unit holding a JSON Schema as OtherData %s", SchemaOtherDataKey)
	case 1:
	default:
		return nil, errors.Newf("the Unit passed as OtherData %s holds %d documents; it has to hold one JSON Schema", SchemaOtherDataKey, len(documents))
	}
	// Comments in the schema Unit become $comment$ keys when it is read, which a schema would
	// take for keywords, or within properties for property names.
	schemaJSON, err := documents[0].MarshalJSONWithoutCommentKeys()
	if err != nil {
		return nil, errors.Wrapf(err, "failed to read the Unit passed as OtherData %s", SchemaOtherDataKey)
	}
	if ignoreRequired {
		var schemaObject map[string]any
		if err := json.Unmarshal(schemaJSON, &schemaObject); err != nil {
			return nil, errors.Wrapf(err, "the Unit passed as OtherData %s does not hold a JSON Schema object", SchemaOtherDataKey)
		}
		delete(schemaObject, "required")
		if schemaJSON, err = json.Marshal(schemaObject); err != nil {
			return nil, errors.WithStack(err)
		}
	}
	schema, err := gojsonschema.NewSchema(gojsonschema.NewBytesLoader(schemaJSON))
	if err != nil {
		return nil, errors.Wrapf(err, "the Unit passed as OtherData %s does not hold a valid JSON Schema", SchemaOtherDataKey)
	}
	return schema, nil
}
