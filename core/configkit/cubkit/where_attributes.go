// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package cubkit

import (
	_ "embed"
	"encoding/json"
	"sync"

	"github.com/confighub/sdk/core/entityfilter"
)

// where_attributes.json is what a where expression can name, for every entity type an expression
// can select, extracted from the API's OpenAPI spec, which publishes it as the
// entityfilter.WhereExtension extension.

//go:embed where_attributes.json
var whereAttributesJSON []byte

var whereAttributes = sync.OnceValues(func() (entityfilter.AttributeTable, error) {
	var file struct {
		EntityTypes entityfilter.AttributeTable `json:"entityTypes"`
	}
	if err := json.Unmarshal(whereAttributesJSON, &file); err != nil {
		return nil, err
	}
	return file.EntityTypes, nil
})

// WhereAttributes returns what a where expression can name, by entity type and then attribute
// name. The table is shared, so a caller must not change it.
func WhereAttributes() entityfilter.AttributeTable {
	table, err := whereAttributes()
	if err != nil {
		// The table is embedded in this package, so a failure here is a bug in the generated file
		// rather than anything a caller did.
		panic("cubkit: parsing built-in where attributes: " + err.Error())
	}
	return table
}
