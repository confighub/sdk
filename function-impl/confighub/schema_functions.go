// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package confighub

import (
	"fmt"
	"log/slog"
	"strings"
	"sync"

	"github.com/cockroachdb/errors"
	"github.com/xeipuuv/gojsonschema"

	"github.com/confighub/sdk/core/configkit/cubkit"
	"github.com/confighub/sdk/core/function/api"
	"github.com/confighub/sdk/core/function/handler"
	"github.com/confighub/sdk/core/third_party/gaby"
	"github.com/confighub/sdk/function-impl/generic"
)

func registerSchemaFunctions(fh handler.FunctionRegistry, rp *cubkit.ConfigHubResourceProviderType) {
	if err := fh.RegisterFunction("vet-schemas", &handler.FunctionRegistration{
		FunctionSignature: api.FunctionSignature{
			FunctionName: "vet-schemas",
			OutputInfo: &api.FunctionOutput{
				ResultName:  "passed",
				Description: "True if every document passes its entity type's schema, false otherwise",
				OutputType:  api.OutputTypeValidationResult,
				Schema:      &api.ValidationResultListSchema,
			},
			Mutating:   false,
			Validating: true,
			Hermetic:   true,
			Idempotent: true,
			Description: "Returns true if every entity document passes the schema of its EntityType, which states the fields a document of that type may set. " +
				"The schemas are built in, so nothing is fetched, and a document whose EntityType has none fails rather than being skipped.",
			FunctionType:          api.FunctionTypeCustom,
			AffectedResourceTypes: cubkit.DocumentSchemaTypes(),
		},
		Function: func(fArgs handler.FunctionImplementationArguments) (gaby.Container, any, error) {
			return generic.VetAgainstSchemas(rp, fArgs.Options, fArgs.ParsedData, compiledDocumentSchema)
		},
	}); err != nil {
		slog.Error("failed to register function", "error", err)
	}
}

// compiledDocumentSchemas holds each entity type's document schema, compiled when first used.
var compiledDocumentSchemas sync.Map // api.ResourceType -> *gojsonschema.Schema

func compiledDocumentSchema(entityType api.ResourceType) (*gojsonschema.Schema, error) {
	if schema, ok := compiledDocumentSchemas.Load(entityType); ok {
		return schema.(*gojsonschema.Schema), nil
	}
	data, ok := cubkit.DocumentSchema(entityType)
	if !ok {
		if entityType == cubkit.ResourceTypeNoEntityType {
			return nil, errors.Wrapf(generic.ErrNoSchema, "the document has no %s", cubkit.EntityTypePath)
		}
		return nil, errors.Wrapf(generic.ErrNoSchema, "%s is not an entity type that can be managed as a document; the types are %s",
			entityType, joinResourceTypes(cubkit.DocumentSchemaTypes()))
	}
	schema, err := gojsonschema.NewSchema(gojsonschema.NewBytesLoader(data))
	if err != nil {
		// The schemas are generated and embedded, so one that does not compile is a bug.
		return nil, fmt.Errorf("compiling the built-in schema of %s: %w", entityType, err)
	}
	compiledDocumentSchemas.Store(entityType, schema)
	return schema, nil
}

func joinResourceTypes(types []api.ResourceType) string {
	names := make([]string, len(types))
	for i, resourceType := range types {
		names[i] = string(resourceType)
	}
	return strings.Join(names, ", ")
}
