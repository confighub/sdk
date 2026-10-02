// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package entityfilter

import "github.com/confighub/sdk/core/function/api"

// Expression is one term of a where expression, parsed and checked against an attribute table.
type Expression struct {
	// RelationalExpression holds the attribute's field name as Path, the operator, and the operand:
	// a literal, or the name of another attribute when OperandEntityPrefix is set.
	api.RelationalExpression

	MapKey       string // The key of a map attribute: Labels.<key>, ValidationErrors.<gate>, Values.<key>, DeleteGates.<key>
	NestedMapKey string // For Permissions.<action>.UserIDs, UserIDs; the action is MapKey

	// Attribute is what the table says about the attribute the term names. AttributeEntityType and
	// AttributeName are where it is in the table: the type of the entity the prefix names, and the
	// name it is declared under, which for a qualified name such as FunctionInvocation.Guards is
	// not Path.
	Attribute           Attribute
	AttributeEntityType string
	AttributeName       string

	// Entity prefixes, for a term naming an attribute of another entity than the one filtered.
	EntityPrefix        string // The prefix, as "UpstreamUnit", or the filtered entity's own type
	OperandEntityPrefix string // The same, for an operand that is an attribute
	// IsExtendedTerm says the term names an attribute of an included entity, on either side, so it
	// can only be evaluated once that entity is in hand.
	IsExtendedTerm bool

	// IsArrayElement is set by the `*` segment of an array-valued reference --
	// `FromLink.*.Slug`, where the Unit's FromLinkID names a list and the expansion is a
	// list of Links. It selects every element, so the term holds when any one of them
	// satisfies it, the same quantification a `Data.` path uses for a segment that selects
	// several values. Without the `*` an array-valued prefix is a parse error, since
	// `FromLink.Slug` names no single value to compare.
	IsArrayElement        bool
	OperandIsArrayElement bool // The same, for an operand that references an entity

	// Truth-value test modifier: "IS TRUE", "IS FALSE", "IS NOT TRUE", "IS NOT FALSE"
	// Applied as a post-fix modifier to the result of a binary expression.
	// Useful for nullable columns: `MergeSourceID = 'uuid' IS NOT FALSE` matches rows
	// where MergeSourceID equals the value OR is NULL.
	TruthTest string
}
