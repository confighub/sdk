// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package entityfilter

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/cockroachdb/errors"
	"github.com/google/uuid"

	"github.com/confighub/sdk/core/function/api"
)

// Constants and regex patterns from the original filter implementation
const (
	maxFilterLength = api.MaxFilterLength
	lengthFunction  = "LEN"

	attributeNameCoreRegexpString = "[A-Za-z][A-Za-z]{0,40}"
	uuidPrefixRegexpString        = "^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}"
	// Allow dots in length expressions for Permissions paths like LEN(Permissions.Edit.UserIDs)
	lengthPathRegexpString    = "[A-Za-z][A-Za-z.]{0,60}"
	lengthRegexpString        = "^" + lengthFunction + "\\(" + lengthPathRegexpString + "\\)"
	attributeNameRegexpString = "^" + attributeNameCoreRegexpString
	// `Prefix.Attribute`, and for an array-valued reference `Prefix.*.Attribute`. The `*`
	// selects every element of the expansion, matching the segment a `Data.` path uses for
	// the same job; see Expression.IsArrayElement.
	entityPrefixRegexpString       = "^([A-Za-z][A-Za-z0-9]*)\\.((?:\\*\\.)?)(" + attributeNameCoreRegexpString + ")"
	relationalOperatorRegexpString = "^(<=|>=|<|>|=|\\!=|\\?|NOT LIKE|LIKE|ILIKE|~~|!~~|~\\*|!~\\*|~|!~|IS NULL|IS NOT NULL)"
	inOperatorRegexpString         = "^(IN|NOT IN)"
	truthTestRegexpString          = "^(IS NOT FALSE|IS NOT TRUE|IS FALSE|IS TRUE)"

	// Paths into a DataTypeJSON attribute, such as `Data.spec.replicas`. The syntax mirrors the
	// configuration-data filter's paths in the api package's query.go so that the same path works in
	// both filters. The server translates one of these into a SQL/JSON path expression.
	//
	// Object keys may contain slashes (Kubernetes labels and annotations do) and escape a
	// literal dot as `~1`; a bare integer addresses an array element; `*` matches every element;
	// and `?key=value` selects the elements whose key has that value.
	//
	// Nothing in this alphabet can terminate a SQL string literal or a jsonpath string literal,
	// which is what makes it safe to build the jsonpath by concatenation -- keep it that way.
	// The associative value is deliberately stricter than the configuration-data filter's
	// `[^.#]*`: quotes, backslashes and spaces are excluded, both because they could escape one
	// of those two nesting levels and because allowing spaces makes the value ambiguous with the
	// relational operator that follows it.
	jsonPathKeyRegexpString      = "(?:[A-Za-z_$](?:[A-Za-z0-9/_$\\-]|(?:~1)){0,127})"
	jsonPathIndexRegexpString    = "(?:[0-9][0-9]{0,9})"
	jsonPathAssocValRegexpString = "[^.#'\"\\\\ \t]{1,255}"
	jsonPathAssocRegexpString    = "(?:\\?" + jsonPathKeyRegexpString + "=" + jsonPathAssocValRegexpString + ")"
	jsonPathSegmentRegexpString  = "(?:" + jsonPathKeyRegexpString + "|" + jsonPathIndexRegexpString +
		"|\\*|" + jsonPathAssocRegexpString + ")"
	// A split path (`a.b.|c.d`) selects with the left side and tests a property on the right.
	// The right side admits no patterns, matching the configuration-data filter.
	jsonPathPlainSegmentRegexpString = "(?:" + jsonPathKeyRegexpString + "|" + jsonPathIndexRegexpString + ")"
	jsonPathRegexpString             = "^" + jsonPathSegmentRegexpString + "(?:\\." + jsonPathSegmentRegexpString + ")*" +
		"(?:\\.\\|" + jsonPathPlainSegmentRegexpString + "(?:\\." + jsonPathPlainSegmentRegexpString + ")*)?"
)

// MaxJSONPathLength bounds a single `Data.` path expression.
const MaxJSONPathLength = 512

var (
	lengthRegexp             = regexp.MustCompile(lengthRegexpString)
	attributeNameRegexp      = regexp.MustCompile(attributeNameRegexpString)
	uuidPrefixRegexp         = regexp.MustCompile(uuidPrefixRegexpString)
	entityPrefixRegexp       = regexp.MustCompile(entityPrefixRegexpString)
	relationalOperatorRegexp = regexp.MustCompile(relationalOperatorRegexpString)
	// The function part of a gate name, which carries the function's position in its Trigger's
	// Invocation when that has several functions, as <function>:<position>.
	gateFunctionRegexp          = regexp.MustCompile(api.FunctionNamePrefixRegexpString + "(?::[1-9][0-9]{0,8})?")
	functionAttributeNameRegexp = regexp.MustCompile(api.AttributeNamePrefixRegexpString)
	inOperatorRegexp            = regexp.MustCompile(inOperatorRegexpString)
	truthTestRegexp             = regexp.MustCompile(truthTestRegexpString)
	jsonPathRegexp              = regexp.MustCompile(jsonPathRegexpString)
)

// Parser parses where expressions over one entity type, checking each name against an attribute
// table. It is the grammar only: what an expression means for storage, and the values of
// substituted entities, are the caller's.
type Parser struct {
	attributes        Attributes
	primaryEntityName string            // the entity type being filtered, as "Unit"
	includedFieldMap  map[string]string // prefix -> the field it expands, as "UpstreamUnit" -> "UpstreamUnitID"
	substitutedTypes  map[string]string // prefix -> the entity type of a substituted entity
}

// NewParser returns a parser of expressions over primaryEntityName. includedFields are the
// reference fields whose entities an expression may name by prefix, as UpstreamUnitID allows
// UpstreamUnit.Slug. substitutedTypes are the prefixes of entities whose values the caller will
// substitute, with the entity type of each, so the names after them can be checked.
func NewParser(attributes Attributes, primaryEntityName string, includedFields []string, substitutedTypes map[string]string) *Parser {
	includedFieldMap := make(map[string]string)
	for _, fieldName := range includedFields {
		includedFieldMap[FieldNameToEntityName(fieldName)] = fieldName
	}
	if substitutedTypes == nil {
		substitutedTypes = map[string]string{}
	}
	return &Parser{
		attributes:        attributes,
		primaryEntityName: primaryEntityName,
		includedFieldMap:  includedFieldMap,
		substitutedTypes:  substitutedTypes,
	}
}

// TODO: Expanders should give us this
// IncludedField returns the reference field an entity prefix expands, as UpstreamUnitID for
// UpstreamUnit, and whether the parser was given it.
func (p *Parser) IncludedField(prefix string) (string, bool) {
	field, ok := p.includedFieldMap[prefix]
	return field, ok
}

// FieldNameToEntityName converts a field name to the corresponding entity name
// Examples: "UpstreamUnitID" -> "UpstreamUnit", "HeadRevisionNum" -> "HeadRevision"
func FieldNameToEntityName(fieldName string) string {
	// Remove common suffixes
	if strings.HasSuffix(fieldName, "ID") {
		return strings.TrimSuffix(fieldName, "ID")
	}
	if strings.HasSuffix(fieldName, "IDs") {
		return strings.TrimSuffix(fieldName, "IDs") + "s"
	}
	if strings.HasSuffix(fieldName, "Num") {
		return strings.TrimSuffix(fieldName, "Num")
	}

	// Default: return as-is: Tags
	return fieldName
}

// isValidEntityPrefix checks if the given prefix is a valid entity name and not a map attribute
func (p *Parser) isValidEntityPrefix(prefix string, isArrayElement bool) bool {
	// If this is the primary entity name, it's valid
	if p.primaryEntityName != "" && prefix == p.primaryEntityName {
		return true
	}

	// An attribute that is both a map and an expanded reference carries both syntaxes:
	// Revision.Tags and Revision.Releases are keyed by the ids of what they name, so
	// `Releases.<id> = ''` reads one key -- which is what it has always meant -- while
	// `Releases.*.Published` reaches the entities those keys name. The `*` is what tells the
	// two apart, so without it this is a map access and not a prefix at all.
	if p.attributes.IsKeyedMap(prefix) && !isArrayElement {
		return false
	}

	// Check if this prefix matches any of the included field entities
	_, present := p.includedFieldMap[prefix]
	if present {
		return true // really we could return present at this point
	}

	// Check if this prefix is a substituted entity
	_, isSubstituted := p.substitutedTypes[prefix]
	if isSubstituted {
		return true
	}

	// This is not really necessary, but I left it here to highlight the syntax overlap.
	// Special case: check for map attributes that should NOT be treated as entity prefixes
	// Labels and ValidationErrors (and Annotations eventually) use dot notation but are not entity prefixes
	if p.attributes.IsKeyedMap(prefix) {
		return false
	}

	return false
}

// isQualifiedAttribute reports whether `Prefix.Attribute` is itself a declared attribute of the
// entity being filtered, rather than a reference to another entity.
//
// It is how an attribute the API nests is named the way the API names it, when the row does not
// nest it. Mutation.FunctionInvocation is the case: the API returns FunctionName, Arguments,
// Clearance and Guards inside a FunctionInvocation object, while the server stores them as flat
// columns of the Mutation. Declaring `FunctionInvocation.Guards` lets the filter read as the
// response does and still be one predicate against one row -- unlike an entity prefix, which
// means a second entity and a term evaluated in memory after expansion.
//
// Trigger needs none of this: it embeds FunctionInvocation anonymously, so the API promotes the
// same fields to the top level and `Arguments` already names them.
func (p *Parser) isQualifiedAttribute(qualifiedName string) bool {
	if p.primaryEntityName == "" {
		return false
	}
	_, found := p.attributes.Attribute(p.primaryEntityName, qualifiedName)
	return found
}

// Parse parses a where expression, a conjunction of terms, into one Expression per term, checking
// each name against the attribute table.
func (p *Parser) Parse(queryString string) ([]*Expression, error) {
	if len(queryString) > maxFilterLength {
		return nil, fmt.Errorf("query string exceeds maximum length of %d", maxFilterLength)
	}

	var expressions []*Expression
	queryString = api.SkipWhitespaceWithLimit(queryString, 255)

	for queryString != "" {
		expression, remaining, err := p.parseAndValidateBinaryExpression(queryString)
		if err != nil {
			return nil, err
		}
		expressions = append(expressions, expression)
		if len(expressions) > api.MaxTerms {
			return nil, fmt.Errorf("expression has more than %d terms", api.MaxTerms)
		}
		queryString, err = api.ConsumeConjunction(remaining)
		if err != nil {
			return nil, err
		}
	}

	return expressions, nil
}

// referenceIsArrayValued reports whether an included field names several entities rather than
// one. Two column shapes hold several ids -- an array of them (Unit.FromLinkID,
// Space.TriggerIDs, Space.AttributeIDs, Target.TriggerIDs) and a map keyed by
// them (Revision.Tags, Revision.ChangeOrders, Revision.Releases, whose values label the ids
// rather than naming more entities). Either way the expansion is a slice --
// ExtendedUnit.FromLink is []*Link and ExtendedRevision.Tags is []*Tag -- which is what the `*`
// segment selects across, so it is the expansion's shape that decides this rather than the
// column's.
func referenceIsArrayValued(attribs Attribute) bool {
	return attribs.DataType == api.DataTypeUUIDArray || attribs.DataType == api.DataTypeUUIDStringMap
}

// validateAttributeName finds the attribute a name after a prefix refers to, and the entity type
// whose attribute it is.
func (p *Parser) validateAttributeName(entityPrefix, attributeName string, isArrayElement bool) (*Attribute, string, error) {
	entityType := p.primaryEntityName

	// A substituted entity's attributes are those of its type, which the caller states.
	if substitutedType, isSubstituted := p.substitutedTypes[entityPrefix]; isSubstituted {
		if entityPrefix != p.primaryEntityName {
			entityType = substitutedType
			if entityType == "" {
				return nil, "", errors.Newf("could not determine entity type for substituted entity `%s`", entityPrefix)
			}
		}
	} else if entityPrefix != p.primaryEntityName {
		// Get the attributes for that entity type
		fieldName, found := p.includedFieldMap[entityPrefix]
		if !found {
			return nil, "", errors.Newf("unrecognized entity prefix `%s`", entityPrefix)
		}
		if !p.attributes.HasEntityType(entityType) {
			return nil, "", errors.Newf("unrecognized entity type `%s`", entityPrefix)
		}
		attribs, found := p.attributes.Attribute(entityType, fieldName)
		if !found {
			return nil, "", errors.Newf("could not find attributes of included field `%s`", fieldName)
		}
		// The `*` has to agree with what the reference holds. Without it an array-valued
		// reference names no single value to compare, and it used to parse and then match
		// nothing, since the evaluator had a slice where it expected one struct. With it a
		// single-valued reference would claim to select several.
		// Every included field should say which entity its ids name. One that does not cannot
		// be resolved to an attribute set, and dereferencing it here is what turned
		// `Tags.Slug` into a nil-pointer panic before Revision.Tags had a Reference.
		if attribs.References == "" {
			return nil, "", errors.Newf("included field `%s` does not name an entity type, so `%s.%s` cannot be resolved",
				fieldName, entityPrefix, attributeName)
		}
		arrayValued := referenceIsArrayValued(attribs)
		if arrayValued && !isArrayElement {
			return nil, "", errors.Newf("`%s` is a list, so `%s.%s` does not name a single value; "+
				"use `%s.*.%s` to match when any element does, or filter the ids with `%s ? '<uuid>'`",
				entityPrefix, entityPrefix, attributeName, entityPrefix, attributeName, fieldName)
		}
		if !arrayValued && isArrayElement {
			return nil, "", errors.Newf("`%s` is not a list, so `%s.*.%s` does not apply; use `%s.%s`",
				entityPrefix, entityPrefix, attributeName, entityPrefix, attributeName)
		}
		entityType = attribs.References
	}

	if isArrayElement && entityPrefix == p.primaryEntityName {
		return nil, "", errors.Newf("`%s` is the entity being filtered, not a list; use `%s.%s`",
			entityPrefix, entityPrefix, attributeName)
	}

	// Validate attribute exists
	if !p.attributes.HasEntityType(entityType) {
		return nil, "", errors.Newf("unrecognized entity type `%s`", entityPrefix)
	}
	attribs, ok := p.attributes.Attribute(entityType, attributeName)
	if !ok {
		return nil, "", errors.Newf("unrecognized attribute name `%s`", attributeName)
	}

	return &attribs, entityType, nil
}

// parseAndValidateBinaryExpression parses a single binary expression
func (p *Parser) parseAndValidateBinaryExpression(decodedQueryString string) (*Expression, string, error) {
	// Check for length expression first
	pos := lengthRegexp.FindStringIndex(decodedQueryString)
	lengthExpr := pos != nil
	if lengthExpr {
		// Skip the length function name and opening parenthesis
		decodedQueryString = decodedQueryString[len(lengthFunction)+1:]
	}

	// Parse attribute name with optional entity prefix
	var entityPrefix string
	var attributeName string
	var isExtendedTerm bool
	var isArrayElement bool
	// The name an attribute is declared under, when the API nests it and the row does not, as
	// with Mutation's `FunctionInvocation.Guards`. Empty for every other attribute, whose
	// declared name and field name are the same.
	var qualifiedAttributeName string

	// Try to parse entity prefix first (e.g., "Unit.HeadRevisionNum" or "UpstreamUnit.DisplayName")
	entityPrefixPos := entityPrefixRegexp.FindStringSubmatch(decodedQueryString)
	if entityPrefixPos != nil {
		potentialEntityPrefix := entityPrefixPos[1]
		potentialArrayElement := entityPrefixPos[2] != ""
		potentialAttributeName := entityPrefixPos[3]

		// Check if this is a real entity prefix or just a map attribute (like Labels.Environment)
		if p.isValidEntityPrefix(potentialEntityPrefix, potentialArrayElement) {
			entityPrefix = potentialEntityPrefix
			isArrayElement = potentialArrayElement
			attributeName = potentialAttributeName
			decodedQueryString = decodedQueryString[len(entityPrefixPos[0]):]

			// Determine if this is an extended term
			if p.primaryEntityName != "" && entityPrefix != p.primaryEntityName {
				isExtendedTerm = true
			}
			// Substituted entities are also considered extended for right-hand operands
			if _, isSubstituted := p.substitutedTypes[entityPrefix]; isSubstituted && entityPrefix != p.primaryEntityName {
				isExtendedTerm = true
			}
		} else if qualified := potentialEntityPrefix + "." + potentialAttributeName; !potentialArrayElement &&
			p.isQualifiedAttribute(qualified) {
			// A field the API nests within the entity being filtered, named as the API names it.
			// The column is the entity's own, so this stays one SQL predicate; only the lookup
			// uses the qualified name, while everything downstream -- the map-key parser, the
			// projection, the in-memory evaluator -- sees the field name the struct and the row
			// use.
			qualifiedAttributeName = qualified
			attributeName = potentialAttributeName
			decodedQueryString = decodedQueryString[len(entityPrefixPos[0]):]
			entityPrefix = p.primaryEntityName
		} else {
			// This is not an entity prefix, parse as regular attribute (e.g., Labels)
			pos := attributeNameRegexp.FindStringIndex(decodedQueryString)
			if pos == nil {
				return nil, decodedQueryString, errors.Newf("invalid attribute name at `%s`", decodedQueryString)
			}
			attributeName = decodedQueryString[pos[0]:pos[1]]
			decodedQueryString = decodedQueryString[pos[1]:]

			// If no entity prefix and we have a primary entity, assume it's the primary entity
			if p.primaryEntityName != "" {
				entityPrefix = p.primaryEntityName
			}
		}
	} else {
		// No entity prefix, parse attribute name only
		pos := attributeNameRegexp.FindStringIndex(decodedQueryString)
		if pos == nil {
			return nil, decodedQueryString, errors.Newf("invalid attribute name at `%s`", decodedQueryString)
		}
		attributeName = decodedQueryString[pos[0]:pos[1]]
		decodedQueryString = decodedQueryString[pos[1]:]

		// If no entity prefix and we have a primary entity, assume it's the primary entity
		if p.primaryEntityName != "" {
			entityPrefix = p.primaryEntityName
		}
	}

	// Validate attribute exists
	lookupAttributeName := attributeName
	if qualifiedAttributeName != "" {
		lookupAttributeName = qualifiedAttributeName
	}
	attribs, attributeEntityType, err := p.validateAttributeName(entityPrefix, lookupAttributeName, isArrayElement)
	if err != nil {
		return nil, decodedQueryString, err
	}

	// Validate length expression support
	lengthClosed := false
	if lengthExpr {
		if !supportsLengthExpression(attribs.DataType) {
			return nil, decodedQueryString, errors.Newf("length not supported for attribute `%s` of type %s", attributeName, attribs.DataType)
		}
		// For Permissions type with path like LEN(Permissions.Edit.UserIDs), delay skipping the closing paren
		// until after parsing the map key. For other types like LEN(Labels), skip it now.
		if attribs.DataType != api.DataTypeStringStringUUIDBoolMap || (len(decodedQueryString) > 0 && decodedQueryString[0] == ')') {
			if !strings.HasPrefix(decodedQueryString, ")") {
				return nil, decodedQueryString, errors.Newf("expected `)` to close LEN at `%s`", decodedQueryString)
			}
			decodedQueryString = decodedQueryString[1:]
			lengthClosed = true
		}
	}

	// Parse map keys for special attributes
	var mapKey string
	var nestedMapKey string
	if !lengthExpr && attribs.DataType == api.DataTypeStringMap {
		switch attributeName {
		case "Labels", "Annotations", "Facts", "Guards":
			decodedQueryString, mapKey, err = parseSingleKeyMapKey(decodedQueryString, attributeName)
			if err != nil {
				return nil, decodedQueryString, err
			}
		case "Values":
			decodedQueryString, mapKey, err = parseValuesMapKey(decodedQueryString, attributeName)
			if err != nil {
				return nil, decodedQueryString, err
			}
		default:
			return nil, decodedQueryString, fmt.Errorf("unsupported StringMap %s", attributeName)
		}
	} else if !lengthExpr && attribs.DataType == api.DataTypeStringBoolMap {
		if isValidationMapAttribute(attributeName) {
			decodedQueryString, mapKey, err = parseValidationErrorsMapKey(decodedQueryString, attributeName)
			if err != nil {
				return nil, decodedQueryString, err
			}
		} else {
			// DeleteGates, DestroyGates
			decodedQueryString, mapKey, err = parseSingleKeyMapKey(decodedQueryString, attributeName)
			if err != nil {
				return nil, decodedQueryString, err
			}
		}
	} else if !lengthExpr && attribs.DataType == api.DataTypeUUIDStringMap {
		decodedQueryString, mapKey, err = parseUUIDMapKey(decodedQueryString, attributeName)
		if err != nil {
			return nil, decodedQueryString, err
		}
	} else if !lengthExpr && attribs.DataType == api.DataTypeStringUUIDMap {
		// Keyed as the map whose keys it records the Triggers of.
		switch attributeName {
		case "ValidationTriggerIDs":
			decodedQueryString, mapKey, err = parseValidationErrorsMapKey(decodedQueryString, attributeName)
		case "ValueTriggerIDs":
			decodedQueryString, mapKey, err = parseValuesMapKey(decodedQueryString, attributeName)
		default:
			err = fmt.Errorf("unsupported StringUUIDMap %s", attributeName)
		}
		if err != nil {
			return nil, decodedQueryString, err
		}
	} else if attribs.DataType == api.DataTypeJSON {
		decodedQueryString, mapKey, err = parseJSONPath(decodedQueryString, attributeName)
		if err != nil {
			return nil, decodedQueryString, err
		}
	} else if attribs.DataType == api.DataTypeStringStringUUIDBoolMap {
		// Permissions type: Permissions.<action>.UserIDs ? <uuid> or LEN(Permissions.<action>.UserIDs) or LEN(Permissions)
		// For LEN(Permissions) we don't need to parse any key
		if !lengthExpr || (lengthExpr && len(decodedQueryString) > 0 && decodedQueryString[0] == '.') {
			decodedQueryString, mapKey, nestedMapKey, err = parsePermissionsMapKey(decodedQueryString, attributeName, lengthExpr)
			if err != nil {
				return nil, decodedQueryString, err
			}
			// mapKey is the action (e.g., "Edit"), nestedMapKey is the field (e.g., "UserIDs")
		}
		// For LEN expressions on Permissions with a path, skip the closing paren now
		if lengthExpr && !lengthClosed {
			if !strings.HasPrefix(decodedQueryString, ")") {
				return nil, decodedQueryString, errors.Newf("expected `)` to close LEN at `%s`", decodedQueryString)
			}
			decodedQueryString = decodedQueryString[1:]
		}
	}

	decodedQueryString = api.SkipWhitespaceWithLimit(decodedQueryString, 255)

	// Parse operator
	operator, remaining, err := p.parseOperator(decodedQueryString, attribs, lengthExpr)
	if err != nil {
		return nil, decodedQueryString, err
	}
	decodedQueryString = remaining

	// Parse operand
	operand, dataType, remaining, operandEntityPrefix, operandIsExtended, err := p.parseOperand(decodedQueryString, attribs, lengthExpr, operator)
	if err != nil {
		return nil, decodedQueryString, err
	}
	decodedQueryString = remaining

	// Check for a trailing truth-value test modifier (IS [NOT] {TRUE|FALSE})
	var truthTest string
	trimmed := api.SkipWhitespaceWithLimit(decodedQueryString, 255)
	if pos := truthTestRegexp.FindStringIndex(trimmed); pos != nil {
		truthTest = trimmed[pos[0]:pos[1]]
		decodedQueryString = trimmed[pos[1]:]
	}

	// If the operand is an extended reference to an included entity (not substituted),
	// this entire expression becomes extended
	if operandIsExtended {
		// Only mark as extended if the operand is not a substituted entity
		// Substituted entities will be handled during substitution
		if _, isSubstituted := p.substitutedTypes[operandEntityPrefix]; !isSubstituted {
			isExtendedTerm = true
		}
	}

	expression := &Expression{
		RelationalExpression: api.RelationalExpression{
			Path:               attributeName,
			Operator:           operator,
			Literal:            operand, // This may be a literal or an attribute name
			DataType:           dataType,
			IsLengthExpression: lengthExpr,
		},
		MapKey:              mapKey,
		NestedMapKey:        nestedMapKey,
		Attribute:           *attribs,
		AttributeEntityType: attributeEntityType,
		AttributeName:       lookupAttributeName,
		EntityPrefix:        entityPrefix,
		OperandEntityPrefix: operandEntityPrefix, // Will be non-empty if the operand is an attribute
		IsExtendedTerm:      isExtendedTerm,
		IsArrayElement:      isArrayElement,
		TruthTest:           truthTest,
	}
	return expression, decodedQueryString, nil
}

// parseOperator parses and validates the operator
func (p *Parser) parseOperator(decodedQueryString string, attribs *Attribute, lengthExpr bool) (string, string, error) {
	// Check for IN/NOT IN operator (always enabled)
	inPos := inOperatorRegexp.FindStringIndex(decodedQueryString)
	if inPos != nil {
		operator := decodedQueryString[inPos[0]:inPos[1]]
		return operator, decodedQueryString[inPos[1]:], nil
	}

	// Parse standard operators
	pos := relationalOperatorRegexp.FindStringIndex(decodedQueryString)
	if pos == nil {
		return "", decodedQueryString, errors.Newf("invalid operator at `%s`", decodedQueryString)
	}

	// Whitespace after the operator is the operand's to skip: IS NULL takes none, and what
	// follows it is the separator before the next term.
	operator := decodedQueryString[pos[0]:pos[1]]
	remaining := decodedQueryString[pos[1]:]

	// Validate operator for data type
	if err := p.validateOperator(operator, attribs, lengthExpr); err != nil {
		return "", decodedQueryString, err
	}

	return operator, remaining, nil
}

// validateOperator validates that the operator is allowed for the given attribute type
// This is called after parsing the operator but before parsing the map key, so we validate
// based on the overall map type. Additional validation for specific map value types happens
// during operand parsing when we know if a map key is present.
func (p *Parser) validateOperator(operator string, attribs *Attribute, lengthExpr bool) error {
	// String comparison operators (LIKE, ILIKE, ~~, !~~, ~, ~*, !~, !~*) are only valid for string types
	stringOnlyOps := map[string]bool{
		"LIKE": true, "NOT LIKE": true, "ILIKE": true, "~~": true, "!~~": true,
		"~": true, "~*": true, "!~": true, "!~*": true,
	}
	if stringOnlyOps[operator] && attribs.DataType != api.DataTypeString && attribs.DataType != api.DataTypeStringMap &&
		attribs.DataType != api.DataTypeJSON {
		return errors.Newf("invalid operator %s; string comparison operators are only supported for string attributes", operator)
	}

	// UUIDs can only be compared for equality, inequality, and NULL checks
	if attribs.DataType == api.DataTypeUUID && operator != "=" && operator != "!=" && operator != "IS NULL" && operator != "IS NOT NULL" {
		return errors.Newf("invalid operator %s; only =, !=, IS NULL, and IS NOT NULL are supported for UUID comparisons", operator)
	}

	// Check allowed operators for maps (when not using length expressions)
	// For StringBoolMap, UUIDStringMap and StringUUIDMap, we only allow =, !=, IN, NOT IN, ?, IS NULL, and IS NOT NULL
	// For StringMap, we also allow string comparison operators
	if (attribs.DataType == api.DataTypeStringBoolMap || attribs.DataType == api.DataTypeUUIDStringMap ||
		attribs.DataType == api.DataTypeStringUUIDMap) && !lengthExpr {
		allowedMapOps := map[string]bool{"=": true, "!=": true, "IN": true, "NOT IN": true, "?": true, "IS NULL": true, "IS NOT NULL": true}
		if !allowedMapOps[operator] {
			return errors.Newf("invalid operator %s; only =, !=, IN, NOT IN, ?, IS NULL, and IS NOT NULL are supported for boolean/UUID map comparisons", operator)
		}
	}

	// For StringStringUUIDBoolMap (Permissions), only allow ? for containment and relational operators for LEN
	if attribs.DataType == api.DataTypeStringStringUUIDBoolMap && !lengthExpr {
		if operator != "?" {
			return errors.Newf("invalid operator %s; only ? is supported for Permissions containment expressions", operator)
		}
	}

	if operator == "?" && attribs.DataType != api.DataTypeUUIDArray && attribs.DataType != api.DataTypeStringMap && attribs.DataType != api.DataTypeStringBoolMap && attribs.DataType != api.DataTypeUUIDStringMap && attribs.DataType != api.DataTypeStringUUIDMap && attribs.DataType != api.DataTypeStringStringUUIDBoolMap && attribs.DataType != api.DataTypeJSON {
		return errors.Newf("? is only supported for array and map containment expressions")
	}

	if attribs.DataType == api.DataTypeUUIDArray && !lengthExpr && operator != "?" {
		return errors.Newf("invalid operator %s; only ? is supported for array comparisons", operator)
	}

	return nil
}

// parseOperand parses the operand (literal or attribute reference)
// Returns: operand value, data type, remaining string, entity prefix, is extended term, error
func (p *Parser) parseOperand(decodedQueryString string, attribs *Attribute, lengthExpr bool, operator string) (string, api.DataType, string, string, bool, error) {
	// Handle IS NULL and IS NOT NULL operators - they don't need operands
	if operator == "IS NULL" || operator == "IS NOT NULL" {
		return "", attribs.DataType, decodedQueryString, "", false, nil
	}
	decodedQueryString = api.SkipWhitespaceWithLimit(decodedQueryString, 255)

	// Handle IN/NOT IN operators specially
	if operator == "IN" || operator == "NOT IN" {
		// Use public API to parse the IN clause
		remaining, inClause, err := api.ParseInClause(decodedQueryString)
		if err != nil {
			return "", api.DataTypeNone, decodedQueryString, "", false, errors.Newf("invalid IN clause: %v", err)
		}
		// Validate each value against the column's data type before it can reach SQL generation.
		// IN values are concatenated into the raw WHERE clause (quoted for string/UUID/time, bare
		// for int/bool), so a value that is not a well-formed literal of the column type must be
		// rejected here, at parse time. This is the gate that prevents SQL injection via IN/NOT IN.
		if err := api.ValidateInClauseValues(inClause, attribs.DataType); err != nil {
			return "", api.DataTypeNone, decodedQueryString, "", false, err
		}
		return inClause, api.DataTypeString, remaining, "", false, nil
	}

	// Determine expected data type
	expectedDataType := attribs.DataType
	if lengthExpr {
		expectedDataType = api.DataTypeInt
	}

	// Try to parse as literal first using public API
	remaining, literal, dataType, err := api.ParseLiteral(decodedQueryString)
	if err == nil {
		if err := api.ValidatePatternOperand(operator, literal, dataType); err != nil {
			return "", api.DataTypeNone, decodedQueryString, "", false, err
		}
		// For map types and array types, we accept string literals and process them specially
		if (expectedDataType == api.DataTypeStringMap || expectedDataType == api.DataTypeStringBoolMap || expectedDataType == api.DataTypeUUIDStringMap || expectedDataType == api.DataTypeUUIDArray) && dataType == api.DataTypeString {
			return literal, dataType, remaining, "", false, nil
		}
		// A time is written as a quoted string, so the literal parses as one. The attribute's
		// own type is what is reported, not the literal's: SQL casts the literal either way,
		// but the in-memory evaluator switches on this to choose a comparison, and comparing
		// times as strings would be lexicographic -- right only while both sides happen to be
		// written the same way. The DataTypeTime case parses the literal itself.
		if expectedDataType == api.DataTypeTime && dataType == api.DataTypeString {
			if err := api.ValidateTimeLiteral(literal); err != nil {
				return "", api.DataTypeNone, decodedQueryString, "", false, err
			}
			return literal, expectedDataType, remaining, "", false, nil
		}
		// For boolean map types, we accept boolean literals
		if expectedDataType == api.DataTypeStringBoolMap && dataType == api.DataTypeBool {
			return literal, dataType, remaining, "", false, nil
		}
		// A UUID is written as a quoted string too, and the same reasoning applies: report the
		// attribute's type so the in-memory evaluator compares uuids rather than their text.
		// Only for a lone UUID -- a map keyed by them takes the literal as a key, which is a
		// string.
		if expectedDataType == api.DataTypeUUID && dataType == api.DataTypeString {
			if _, uuidErr := uuid.Parse(strings.Trim(literal, "'")); uuidErr != nil {
				return "", api.DataTypeNone, decodedQueryString, "", false, errors.Newf("expected a UUID literal, got %s", literal)
			}
			return literal, expectedDataType, remaining, "", false, nil
		}
		if expectedDataType == api.DataTypeUUIDStringMap && dataType == api.DataTypeString {
			return literal, dataType, remaining, "", false, nil
		}
		// A map whose values are UUIDs: `?` names a key, which is a string; any other operator
		// compares the value at a key, which is a UUID and is refused here if it is not one,
		// rather than compared as text and never matching.
		if expectedDataType == api.DataTypeStringUUIDMap && dataType == api.DataTypeString {
			if operator == "?" {
				return literal, dataType, remaining, "", false, nil
			}
			if _, uuidErr := uuid.Parse(strings.Trim(literal, "'")); uuidErr != nil {
				return "", api.DataTypeNone, decodedQueryString, "", false, errors.Newf("expected a UUID literal, got %s", literal)
			}
			return literal, api.DataTypeUUID, remaining, "", false, nil
		}
		// For StringStringUUIDBoolMap (Permissions), we accept string literals (UUIDs for containment check)
		if expectedDataType == api.DataTypeStringStringUUIDBoolMap && dataType == api.DataTypeString {
			return literal, dataType, remaining, "", false, nil
		}
		// A JSON attribute holds values of any JSON type, so accept a literal of any type and
		// report the literal's own type. The SQL generator and the in-memory evaluator use it
		// to decide how to compare the extracted value.
		if expectedDataType == api.DataTypeJSON {
			return literal, dataType, remaining, "", false, nil
		}
		// For other types, validate exact match
		if dataType != expectedDataType {
			return "", api.DataTypeNone, decodedQueryString, "", false, errors.Newf("expected %s literal, got %s", expectedDataType, dataType)
		}
		return literal, dataType, remaining, "", false, nil
	}

	// A quote begins a literal, so what is wrong with it is what to report.
	if strings.HasPrefix(decodedQueryString, "'") {
		return "", api.DataTypeNone, decodedQueryString, "", false, err
	}

	// Try to parse as attribute reference
	remaining, attributeRef, entityPrefix, isExtendedRef, refErr := p.parseAttributeReference(decodedQueryString, expectedDataType)
	if refErr == nil {
		return attributeRef, expectedDataType, remaining, entityPrefix, isExtendedRef, nil
	}
	if attributeNameRegexp.MatchString(decodedQueryString) {
		return "", api.DataTypeNone, decodedQueryString, "", false, refErr
	}

	return "", api.DataTypeNone, decodedQueryString, "", false, errors.Newf("no valid operand found at `%s`", decodedQueryString)
}

// parseAttributeReference attempts to parse an attribute reference with optional entity prefix
// Returns: remaining string, column reference, entity prefix, is extended term, error
func (p *Parser) parseAttributeReference(decodedQueryString string, expectedDataType api.DataType) (string, string, string, bool, error) {
	var entityPrefix string
	var attributeName string
	var isExtendedTerm bool
	var isArrayElement bool

	// Try to parse entity prefix first (e.g., "Unit.HeadRevisionNum" or "UpstreamUnit.DisplayName")
	entityPrefixPos := entityPrefixRegexp.FindStringSubmatch(decodedQueryString)
	if entityPrefixPos != nil {
		potentialEntityPrefix := entityPrefixPos[1]
		potentialArrayElement := entityPrefixPos[2] != ""
		potentialAttributeName := entityPrefixPos[3]

		// Check if this is a real entity prefix or just a map attribute (like Labels.Environment)
		if p.isValidEntityPrefix(potentialEntityPrefix, potentialArrayElement) {
			entityPrefix = potentialEntityPrefix
			isArrayElement = potentialArrayElement
			attributeName = potentialAttributeName
			decodedQueryString = decodedQueryString[len(entityPrefixPos[0]):]

			// Determine if this is an extended term
			if p.primaryEntityName != "" && entityPrefix != p.primaryEntityName {
				isExtendedTerm = true
			}
			// Substituted entities are also considered extended for right-hand operands
			if _, isSubstituted := p.substitutedTypes[entityPrefix]; isSubstituted && entityPrefix != p.primaryEntityName {
				isExtendedTerm = true
			}
		} else {
			// This is not an entity prefix, parse as regular attribute (e.g., Labels)
			pos := attributeNameRegexp.FindStringIndex(decodedQueryString)
			if pos == nil {
				return decodedQueryString, "", "", false, errors.New("no attribute reference found")
			}
			attributeName = decodedQueryString[pos[0]:pos[1]]
			decodedQueryString = decodedQueryString[pos[1]:]

			// If no entity prefix and we have a primary entity, assume it's the primary entity
			if p.primaryEntityName != "" {
				entityPrefix = p.primaryEntityName
			}
		}
	} else {
		// No entity prefix, parse attribute name only
		pos := attributeNameRegexp.FindStringIndex(decodedQueryString)
		if pos == nil {
			return decodedQueryString, "", "", false, errors.New("no attribute reference found")
		}
		attributeName = decodedQueryString[pos[0]:pos[1]]
		decodedQueryString = decodedQueryString[pos[1]:]

		// If no entity prefix and we have a primary entity, assume it's the primary entity
		if p.primaryEntityName != "" {
			entityPrefix = p.primaryEntityName
		}
	}

	// A substituted entity's attribute is read from the value the caller holds, so its type is not
	// checked against the left side's here; the substitution compares the two values. Its name is
	// checked against the entity's type, as the left side's is: the value is read by reflection,
	// which would otherwise reach any exported field of the entity, whether filterable or not.
	if substitutedType, isSubstituted := p.substitutedTypes[entityPrefix]; isSubstituted {
		if entityPrefix != p.primaryEntityName {
			if _, found := p.attributes.Attribute(substitutedType, attributeName); !found {
				return decodedQueryString, "", "", false, errors.Newf("unrecognized attribute name `%s` of `%s`", attributeName, entityPrefix)
			}
		}
		// Mark as extended if it's not the primary entity
		if entityPrefix != p.primaryEntityName {
			isExtendedTerm = true
		}

		// For substituted entities, we need to handle map key syntax for Labels, ValidationErrors, Values, etc.
		// Check if we have a map key (e.g., Labels.Environment)
		if p.attributes.IsKeyedMap(attributeName) {
			// Parse the map key
			if strings.HasPrefix(decodedQueryString, ".") {
				decodedQueryString = decodedQueryString[1:] // Skip the dot
				// Parse the map key part
				pos := attributeNameRegexp.FindStringIndex(decodedQueryString)
				if pos != nil {
					mapKey := decodedQueryString[pos[0]:pos[1]]
					decodedQueryString = decodedQueryString[pos[1]:]
					// Return with the map key included in the attribute name
					return decodedQueryString, attributeName + "." + mapKey, entityPrefix, isExtendedTerm, nil
				}
			}
		}

		// Return without full validation - we'll handle this during substitution
		return decodedQueryString, attributeName, entityPrefix, isExtendedTerm, nil
	}

	if p.attributes.IsKeyedMap(attributeName) {
		return decodedQueryString, "", "", false, errors.New(attributeName + " must be the first expression operand")
	}

	// A list on the right would be comparing against several values at once, which the
	// language has no reading for. Refused rather than evaluated against whichever element
	// came first.
	if isArrayElement {
		return decodedQueryString, "", "", false, errors.Newf(
			"`%s.*.%s` is a list and cannot be the right-hand operand; compare against a literal",
			entityPrefix, attributeName)
	}

	// Validate attribute exists for non-substituted entities
	attribs, _, err := p.validateAttributeName(entityPrefix, attributeName, isArrayElement)
	if err != nil {
		return decodedQueryString, "", "", false, err
	}

	if attribs.DataType != expectedDataType {
		return decodedQueryString, "", "", false, errors.Newf("attribute %s is not of type %s", attributeName, string(expectedDataType))
	}

	return decodedQueryString, attributeName, entityPrefix, isExtendedTerm, nil
}

// supportsLengthExpression checks if a data type supports LEN() operations
func supportsLengthExpression(dataType api.DataType) bool {
	switch dataType {
	case api.DataTypeStringMap, api.DataTypeStringBoolMap, api.DataTypeUUIDStringMap, api.DataTypeStringUUIDMap, api.DataTypeUUIDArray, api.DataTypeStringStringUUIDBoolMap:
		return true
	default:
		return false
	}
}
