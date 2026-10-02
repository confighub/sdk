// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

// Package entityfilter is about the where expressions that select ConfigHub entities, as a list's
// where parameter, a Filter's Where, and a Space's WhereTrigger do: what each entity type lets an
// expression name. Expressions over configuration data, such as where-resource, are another
// grammar, in the function api package.
package entityfilter

import (
	"sort"

	"github.com/confighub/sdk/core/function/api"
)

// Attribute is what a where expression can do with one attribute of an entity type.
type Attribute struct {
	// DataType is the attribute's type in an expression, which says which operators and literals
	// it takes: a UUIDArray takes `?`, a StringMap is read by key, as in Labels.tier.
	DataType api.DataType `json:"dataType"`
	// References is the entity type an ID attribute names, and empty for any other attribute.
	References string `json:"references,omitempty"`
	// Expandable says the reference can prefix an attribute of what it names, as UpstreamUnitID
	// does in UpstreamUnit.Slug: the entity's endpoints expand it.
	Expandable bool `json:"expandable,omitempty"`
}

// AttributeTable is what a where expression can name, by entity type and then attribute name.
// A name the API nests and the entity's row does not, such as a Mutation's
// FunctionInvocation.Guards, is an attribute name too.
type AttributeTable map[string]map[string]Attribute

// Attributes is what a Parser reads names from. An AttributeTable is one; the server reads its own
// models directly.
type Attributes interface {
	// HasEntityType reports whether expressions can select entities of the type.
	HasEntityType(entityType string) bool
	// Attribute returns an attribute of an entity type by name.
	Attribute(entityType, name string) (Attribute, bool)
	// IsKeyedMap reports whether an attribute of that name, of any type, is a keyed map.
	IsKeyedMap(name string) bool
}

// HasEntityType reports whether the table has the entity type.
func (t AttributeTable) HasEntityType(entityType string) bool {
	_, ok := t[entityType]
	return ok
}

// Attribute returns an attribute of an entity type by name.
func (t AttributeTable) Attribute(entityType, name string) (Attribute, bool) {
	attribute, ok := t[entityType][name]
	return attribute, ok
}

// ExpandableFields returns the references of an entity type that can prefix attributes of what
// they name, sorted: the included fields to parse an expression over that type with, as the server
// does for a list of it or a Filter of it.
func (t AttributeTable) ExpandableFields(entityType string) []string {
	var fields []string
	for name, attribute := range t[entityType] {
		if attribute.Expandable {
			fields = append(fields, name)
		}
	}
	sort.Strings(fields)
	return fields
}

// WhereExtension is the OpenAPI extension that publishes an entity type's attribute table: on each
// property of the entity's schema that an expression can name, as an Attribute, and on the schema
// itself, as a map of Attributes by name, for the qualified names that are no property of it.
const WhereExtension = "x-confighub-where"

// DataTypeIsKeyedMap reports whether a value of this type is addressed by key, which is what makes
// `Labels.owner` and `Tags.<uuid>` mean "one entry" rather than "an attribute of another entity".
//
// The structured types are deliberately absent. PatchMap and the various lists are documents rather
// than keyed collections: nothing addresses an entry of one by name, and JSON-valued attributes are
// queried by path instead (see Revision.Conflicts).
func DataTypeIsKeyedMap(dataType api.DataType) bool {
	switch dataType {
	case api.DataTypeStringMap, api.DataTypeStringBoolMap, api.DataTypeUUIDStringMap,
		api.DataTypeStringUUIDMap, api.DataTypeStringStringUUIDBoolMap:
		return true
	}
	return false
}

// IsKeyedMap reports whether a name before a dot is a keyed map rather than a prefix naming another
// entity, so that `Tags.<uuid>` reads one entry and `Unit.Slug` reads the Unit.
//
// Derived from the attribute's declared data type rather than from a list of names. A list had to be
// kept in step with the models by hand, and it was not: `ChangeOrders` was missing, and so was
// `SkippedUnits`. What a missing entry does is not obvious, which is
// why none of them was noticed. The name after the dot is read as an attribute instead, and the
// identifier lexer stops at the first digit, so `ChangeOrders.ade0e7b9-...` was rejected as
// `ChangeOrders.ade`. Only ids beginning with a hex letter took that path, so the same query worked
// or failed depending on which uuid it was given, which reads as a flaky test rather than a bug.
//
// The lookup is by name across every entity type in the table, which is what the list it replaced
// did: callers have only a name here.
func (t AttributeTable) IsKeyedMap(field string) bool {
	for _, attributes := range t {
		if props, found := attributes[field]; found && DataTypeIsKeyedMap(props.DataType) {
			return true
		}
	}
	return false
}
