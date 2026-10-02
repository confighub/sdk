// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package cubkit

import (
	_ "embed"
	"encoding/json"
	"sort"
	"sync"

	"github.com/confighub/sdk/core/function/api"
)

// document_schemas.json holds a JSON Schema for the document of each ConfigHub entity type that
// can be managed as one, generated from the same entity definitions as the API, so a document
// that passes it has the fields the API accepts. A reference is the name a document holds, not
// the ID the API holds, and a field the document may not set is refused.

//go:embed document_schemas.json
var documentSchemasJSON []byte

type documentSchemaFile struct {
	Documents   map[api.ResourceType]map[string]any `json:"documents"`
	Definitions map[string]any                      `json:"definitions"`
}

var documentSchemas = sync.OnceValues(func() (map[api.ResourceType][]byte, error) {
	var file documentSchemaFile
	if err := json.Unmarshal(documentSchemasJSON, &file); err != nil {
		return nil, err
	}
	schemas := make(map[api.ResourceType][]byte, len(file.Documents))
	for entityType, schema := range file.Documents {
		// Each schema carries the definitions, so it can be compiled on its own.
		schema["definitions"] = file.Definitions
		data, err := json.Marshal(schema)
		if err != nil {
			return nil, err
		}
		schemas[entityType] = data
	}
	return schemas, nil
})

// DocumentSchema returns the JSON Schema of an entity type's document, complete with the
// definitions it refers to, and whether the type has one.
func DocumentSchema(entityType api.ResourceType) ([]byte, bool) {
	schemas, err := documentSchemas()
	if err != nil {
		// The schemas are embedded in this package, so a failure here is a bug in the generated
		// file rather than anything a caller did.
		panic("cubkit: parsing built-in document schemas: " + err.Error())
	}
	schema, ok := schemas[entityType]
	return schema, ok
}

// DocumentSchemaTypes returns the entity types that have a document schema, sorted.
func DocumentSchemaTypes() []api.ResourceType {
	schemas, err := documentSchemas()
	if err != nil {
		panic("cubkit: parsing built-in document schemas: " + err.Error())
	}
	types := make([]api.ResourceType, 0, len(schemas))
	for entityType := range schemas {
		types = append(types, entityType)
	}
	sort.Slice(types, func(i, j int) bool { return types[i] < types[j] })
	return types
}
