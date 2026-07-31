// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package api

import (
	"fmt"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/cockroachdb/errors"
	"github.com/google/uuid"
)

// Path expressions support embedded accessors and escaped dots.
// They also support wildcards and associative matches.
// Kubernetes annotations and labels permit slashes
const (
	andOperator = "AND"

	parameterNameRegexpString  = "(?:[A-Za-z][A-Za-z0-9_\\-]{0,127})"
	pathMapSegmentRegexpString = "(?:[A-Za-z$](?:[A-Za-z0-9/_\\-$]|(?:\\~[12])){0,127})"

	pathMapSegmentBoundtoParameterRegexpString = "(?:@" + pathMapSegmentRegexpString + "\\:" + parameterNameRegexpString + ")"
	pathIndexSegmentRegexpString               = "(?:[0-9][0-9]{0,9})"
	pathWildcardSegmentRegexpString            = "\\*(?:(?:\\?" + pathMapSegmentRegexpString + "(?:\\:" + parameterNameRegexpString + ")?)|(?:@\\:" + parameterNameRegexpString + "))?"
	pathAssociativeMatchRegexpString           = "\\?" + pathMapSegmentRegexpString + "(?:\\:" + parameterNameRegexpString + ")?=[^.#][^.#]*"
	pathSegmentRegexpString                    = "(?:" + pathMapSegmentRegexpString + "|" + pathMapSegmentBoundtoParameterRegexpString + "|" + pathIndexSegmentRegexpString + "|" + pathWildcardSegmentRegexpString + "|" + pathAssociativeMatchRegexpString + ")"

	// Path segment without patterns (for right side of split)
	pathSegmentWithoutPatternsRegexpString = "(?:" + pathMapSegmentRegexpString + "|" + pathMapSegmentBoundtoParameterRegexpString + "|" + pathIndexSegmentRegexpString + ")"
	PathRegexpString                       = "^" + pathSegmentRegexpString + "(?:\\." + pathSegmentRegexpString + ")*(?:\\.\\|" + pathSegmentWithoutPatternsRegexpString + "(?:\\." + pathSegmentWithoutPatternsRegexpString + ")*)?(?:#" + pathMapSegmentRegexpString + ")?"
	whitespaceRegexpString                 = "^[ \t][ \t]*"
	relationalOperatorRegexpString         = "^(<=|>=|<|>|=|\\!=|NOT LIKE|LIKE|ILIKE|~~|!~~|~\\*|!~\\*|~|!~|IN|NOT IN)"
	logicalOperatorRegexpString            = "^AND"
	booleanLiteralRegexpString             = "^(true|TRUE|false|FALSE)"
	integerLiteralRegexpString             = "^[0-9][0-9]{0,9}"
	safeStringCharsRegexpString            = `[^'"\\]*`
	stringLiteralRegexpString              = `^'` + safeStringCharsRegexpString + `'`

	// A single IN/NOT IN value of each kind: a quoted safe string (no quote/backslash, so it
	// cannot break out of '...'), a bare integer, or a bare boolean. The bare forms mirror
	// integerLiteralRegexpString and booleanLiteralRegexpString.
	inClauseStringValueRegexpString = `'` + safeStringCharsRegexpString + `'`
	inClauseIntValueRegexpString    = `[0-9][0-9]{0,9}`
	inClauseBoolValueRegexpString   = `(?:true|TRUE|false|FALSE)`
	// Any single value token, regardless of kind (used to extract the values for per-type validation).
	inClauseValueRegexpString = `(?:` + inClauseStringValueRegexpString + `|` + inClauseIntValueRegexpString + `|` + inClauseBoolValueRegexpString + `)`
	// An IN/NOT IN list is a parenthesized, comma-separated list (optional surrounding whitespace)
	// that is homogeneous in kind: all quoted strings, OR all integers, OR all booleans. Writing it
	// this way rejects a mixed list (e.g. `('a', 1)`) at the lexical level, and -- being deliberately
	// strict -- lets no operator, comment, cast, or other text reach the raw SQL WHERE clause.
	// Matching the list's kind to the column type is done separately by ValidateInClauseValues.
	inClauseRegexpString = `^\(\s*(?:` +
		inClauseStringValueRegexpString + `(?:\s*,\s*` + inClauseStringValueRegexpString + `)*` + `|` +
		inClauseIntValueRegexpString + `(?:\s*,\s*` + inClauseIntValueRegexpString + `)*` + `|` +
		inClauseBoolValueRegexpString + `(?:\s*,\s*` + inClauseBoolValueRegexpString + `)*` +
		`)\s*\)`
)

var (
	pathNameRegexp           = regexp.MustCompile(PathRegexpString)
	whitespaceRegexp         = regexp.MustCompile(whitespaceRegexpString)
	relationalOperatorRegexp = regexp.MustCompile(relationalOperatorRegexpString)
	LogicalOperatorRegexp    = regexp.MustCompile(logicalOperatorRegexpString)

	// Exported Literal patterns
	BooleanLiteralRegexp      = regexp.MustCompile(booleanLiteralRegexpString)
	IntegerLiteralRegexp      = regexp.MustCompile(integerLiteralRegexpString)
	SafeStringCharsOnlyRegexp = regexp.MustCompile("^" + safeStringCharsRegexpString + "$")
	StringLiteralRegexp       = regexp.MustCompile(stringLiteralRegexpString)
	// IN | NOT IN clause patterns
	inClauseRegexp = regexp.MustCompile(inClauseRegexpString)
	// inClauseListRegexp matches a whole, well-formed IN list (anchored at both ends);
	// inClauseValueRegexp extracts the individual value tokens from it (quotes intact).
	inClauseListRegexp  = regexp.MustCompile(inClauseRegexpString + "$")
	inClauseValueRegexp = regexp.MustCompile(inClauseValueRegexpString)
	// Per-kind validators for a single IN/NOT IN value token, anchored at both ends. ValidateInClauseValues
	// requires every token in a list to match the one kind the column type expects, so the value's
	// kind (quoted string vs bare int vs bare bool) -- not just its characters -- must match, and the
	// list must be type-consistent. Anchoring matters: a prefix match would let `1 OR 1=1` pass as `1`.
	inClauseStringTokenRegexp = regexp.MustCompile(stringLiteralRegexpString + "$")
	inClauseIntValueRegexp    = regexp.MustCompile(integerLiteralRegexpString + "$")
	inClauseBoolValueRegexp   = regexp.MustCompile(booleanLiteralRegexpString + "$")
)

func ParseLiteral(decodedQueryString string) (string, string, DataType, error) {
	pos := IntegerLiteralRegexp.FindStringIndex(decodedQueryString)
	if pos != nil {
		literal := decodedQueryString[pos[0]:pos[1]]
		decodedQueryString = decodedQueryString[pos[1]:]
		return decodedQueryString, literal, DataTypeInt, nil
	}
	pos = BooleanLiteralRegexp.FindStringIndex(decodedQueryString)
	if pos != nil {
		literal := decodedQueryString[pos[0]:pos[1]]
		decodedQueryString = decodedQueryString[pos[1]:]
		return decodedQueryString, literal, DataTypeBool, nil
	}
	pos = StringLiteralRegexp.FindStringIndex(decodedQueryString)
	if pos != nil {
		literal := decodedQueryString[pos[0]:pos[1]]
		decodedQueryString = decodedQueryString[pos[1]:]
		return decodedQueryString, literal, DataTypeString, nil
	}

	return decodedQueryString, "", DataTypeNone, fmt.Errorf("no operand found at `%s`", decodedQueryString)
}

type RelationalExpression struct {
	Path               string // The path of the left operand, which must be an attribute. During evaluation this is used to trigger custom evaluators.
	Operator           string
	Literal            string
	DataType           DataType
	IsLengthExpression bool // True if this is a LEN(attribute) expression
}

// VisitorRelationalExpression extends RelationalExpression with visitor-specific fields
type VisitorRelationalExpression struct {
	RelationalExpression
	// Fields for split path feature used by function visitors
	VisitorPath string // Left side of .| for visitor
	SubPath     string // Right side of .| for property check
	IsSplitPath bool   // Whether this uses the .|syntax
}

func ParseAndValidateBinaryExpression(decodedQueryString string) (string, *VisitorRelationalExpression, error) {
	return parseAndValidateBinaryExpressionWithRegex(decodedQueryString, relationalOperatorRegexp)
}

// parseAndValidateBinaryExpressionWithRegex allows specifying which operator regex to use
func parseAndValidateBinaryExpressionWithRegex(decodedQueryString string, operatorRegex *regexp.Regexp) (string, *VisitorRelationalExpression, error) {
	var expression VisitorRelationalExpression

	// Whitespace should have been skipped already
	// For now, first operand is always a path name
	pos := pathNameRegexp.FindStringIndex(decodedQueryString)
	if pos == nil {
		return decodedQueryString, &expression, fmt.Errorf("invalid path at `%s`", decodedQueryString)
	}
	path := decodedQueryString[pos[0]:pos[1]]
	decodedQueryString = SkipWhitespace(decodedQueryString[pos[1]:])

	// Check for split path syntax using .| separator
	if strings.Contains(path, ".|") {
		parts := strings.SplitN(path, ".|", 2)
		if len(parts) != 2 {
			return decodedQueryString, &expression, fmt.Errorf("invalid split path syntax at `%s`", path)
		}
		expression.VisitorPath = parts[0]
		expression.SubPath = parts[1]
		expression.IsSplitPath = true
		expression.Path = path // Keep original path for compatibility
	} else {
		expression.Path = path
		expression.IsSplitPath = false
	}

	// Get the operator using the specified regex
	pos = operatorRegex.FindStringIndex(decodedQueryString)
	if pos == nil {
		return decodedQueryString, &expression, fmt.Errorf("invalid operator at `%s`", decodedQueryString)
	}
	// Operator should be a valid SQL operator
	operator := decodedQueryString[pos[0]:pos[1]]
	decodedQueryString = SkipWhitespace(decodedQueryString[pos[1]:])

	// Second operand must be a literal
	var literal string
	var dataType DataType
	var err error

	// Handle IN/NOT IN operators specially
	if operator == "IN" || operator == "NOT IN" {
		decodedQueryString, literal, err = ParseInClause(decodedQueryString)
		if err != nil {
			return decodedQueryString, &expression, err
		}
		dataType = DataTypeString // IN clauses are treated as string lists
	} else {
		decodedQueryString, literal, dataType, err = ParseLiteral(decodedQueryString)
		if err != nil {
			return decodedQueryString, &expression, err
		}
		if dataType == DataTypeBool && (operator != "=" && operator != "!=") {
			return decodedQueryString, &expression, fmt.Errorf("invalid boolean operator `%s`", operator)
		}
	}

	expression.Path = path
	expression.Operator = operator
	expression.Literal = literal
	expression.DataType = dataType
	return decodedQueryString, &expression, nil
}

// SkipWhitespace skips whitespace characters with optional limit
func SkipWhitespace(decodedQueryString string) string {
	return SkipWhitespaceWithLimit(decodedQueryString, -1) // -1 means no limit (unlimited)
}

// SkipWhitespaceWithLimit skips whitespace characters with a character limit
// limit of -1 means no limit, 0 means no whitespace allowed, positive values set max chars
func SkipWhitespaceWithLimit(decodedQueryString string, limit int) string {
	if limit == 0 {
		return decodedQueryString // No whitespace allowed
	}

	var regexPattern string
	if limit < 0 {
		// No limit - use unlimited pattern
		regexPattern = whitespaceRegexpString
	} else {
		// Use limited pattern
		regexPattern = fmt.Sprintf("^[ \t][ \t]{0,%d}", limit)
	}

	limitedRegexp := regexp.MustCompile(regexPattern)
	pos := limitedRegexp.FindStringIndex(decodedQueryString)
	if pos != nil {
		return decodedQueryString[pos[1]:]
	}
	return decodedQueryString
}

func GetLogicalOperator(decodedQueryString string) (string, string) {
	pos := LogicalOperatorRegexp.FindStringIndex(decodedQueryString)
	if pos != nil {
		return decodedQueryString[pos[1]:], decodedQueryString[pos[0]:pos[1]]
	}
	return decodedQueryString, ""
}

const MaxPathLength = 512
const MaxEmbeddedAccessorConfigLength = 1024

// ValidatePath validates a path string against the path name regex.
func ValidatePath(path string) error {
	if path == "" {
		return fmt.Errorf("path must not be empty")
	}
	if len(path) > MaxPathLength {
		return fmt.Errorf("path exceeds maximum length of %d", MaxPathLength)
	}
	fullMatch := regexp.MustCompile(PathRegexpString + "$")
	if !fullMatch.MatchString(path) {
		return fmt.Errorf("invalid path: %s", path)
	}
	return nil
}

const MaxFilterLength = 8192

func ParseAndValidateWhereFilter(queryString string) ([]*VisitorRelationalExpression, error) {
	if len(queryString) > MaxFilterLength {
		return nil, fmt.Errorf("query string exceeds maximum length of %d", MaxFilterLength)
	}

	expressions := []*VisitorRelationalExpression{}

	decodedQueryString := SkipWhitespace(queryString)
	for decodedQueryString != "" {
		var expression *VisitorRelationalExpression
		var err error
		decodedQueryString, expression, err = ParseAndValidateBinaryExpression(decodedQueryString)
		if err != nil {
			return expressions, err
		}
		expressions = append(expressions, expression)
		decodedQueryString = SkipWhitespace(decodedQueryString)
		var operator string
		decodedQueryString, operator = GetLogicalOperator(decodedQueryString)
		if operator == andOperator {
			decodedQueryString = SkipWhitespace(decodedQueryString)
		}
	}

	return expressions, nil
}

// ParseInClause parses an IN clause like "('value1', 'value2', 'value3')"
// Exported for use by internal packages
func ParseInClause(decodedQueryString string) (string, string, error) {
	pos := inClauseRegexp.FindStringIndex(decodedQueryString)
	if pos == nil {
		return decodedQueryString, "", fmt.Errorf("invalid IN clause at `%s`", decodedQueryString)
	}

	literal := decodedQueryString[pos[0]:pos[1]]
	remaining := decodedQueryString[pos[1]:]

	// inClauseRegexp enforces that the list contains only quoted-string/int/bool tokens, so no
	// quote, operator, comment, or cast text can reach the generated SQL. Per-column-type
	// validation is the caller's responsibility via ValidateInClauseValues (the data type is not
	// known here).
	return remaining, literal, nil
}

// ValidateInClauseValues checks that every value in a parsed IN/NOT IN literal is a well-formed
// literal for the given column data type. The entity filter SQL generator concatenates IN values
// directly into a raw SQL WHERE clause — string/UUID/time values quoted, int/bool values bare — so
// this is the gate that stops SQL injection through IN/NOT IN, and it must run during parsing
// (see FilterParser.parseOperand), before any SQL is generated. inClauseRegexp already guarantees
// the list is structurally a set of quoted-string/int/bool tokens; this adds the per-type check a
// single regex over the whole list cannot make — e.g. a quoted, non-numeric value targeting an
// integer column, whose quotes the generator strips before emitting the value bare.
func ValidateInClauseValues(literal string, dataType DataType) error {
	// Pick the single value kind the column type requires. Validating the raw tokens (quotes
	// intact) by kind -- not the quote-stripped content -- means a value's type, not just its
	// characters, must match the column. Because every token must match this one kind, it also
	// enforces a single consistent type across the list: `Slug IN ('a', 1)` (mixed),
	// `HeadRevisionNum IN ('5')` (quoted value for an int column), and `Slug IN (5)` (bare value
	// for a string column) are all rejected.
	var want *regexp.Regexp
	var kind string
	switch dataType {
	case DataTypeString, DataTypeUUID, DataTypeTime, DataTypeStringMap, DataTypeUUIDStringMap, DataTypeJSON:
		// Quoted token whose interior has no quote/backslash, so it cannot escape the '...'.
		// A JSON value at a path is compared in its text form, so its IN list is quoted too:
		// `Data.spec.replicas IN ('1', '2')`.
		want, kind = inClauseStringTokenRegexp, "string"
	case DataTypeInt:
		want, kind = inClauseIntValueRegexp, "integer"
	case DataTypeBool, DataTypeStringBoolMap:
		want, kind = inClauseBoolValueRegexp, "boolean"
	default:
		// Fail closed: never accept a value for a type we do not strictly validate.
		return fmt.Errorf("IN clause is not supported for column type %v", dataType)
	}
	// Require a structurally well-formed list so FindAllString reliably recovers every token
	// (this holds whenever the literal came from ParseInClause, but does not depend on it).
	if !inClauseListRegexp.MatchString(literal) {
		return fmt.Errorf("invalid IN clause `%s`", literal)
	}
	for _, token := range inClauseValueRegexp.FindAllString(literal, -1) {
		if !want.MatchString(token) {
			return fmt.Errorf("invalid %s IN clause value `%s`", kind, token)
		}
	}
	return nil
}

// Import-specific operator support
var ImportSupportedOperators = []string{"=", "!=", "IN", "NOT IN"}
var importRelationalOperatorRegexpString = "^(<=|>=|<|>|=|\\!=|IN|NOT IN|LIKE|ILIKE|~~|!~~|~\\*|!~\\*|~|!~)"
var importRelationalOperatorRegexp = regexp.MustCompile(importRelationalOperatorRegexpString)

// isValidImportOperator checks if an operator is supported for import queries
func isValidImportOperator(operator string) bool {
	for _, op := range ImportSupportedOperators {
		if op == operator {
			return true
		}
	}
	return false
}

// ImportFilter
type ImportFilter struct {
	// Type specifies the filter type (namespace, label, resource_type, etc.)
	Type string `json:",omitempty" description:"Type specifies the filter type (namespace, label, resource_type, etc.)"`

	// Operator specifies how to apply the filter (include, exclude, equals, contains, matches)
	Operator string `json:",omitempty" description:"Operator specifies how to apply the filter (include, exclude, equals, contains, matches)"`

	// Values specifies the filter values
	Values []string `json:",omitempty" description:"Values specifies the filter values"`
}

// ImportOptions represents extensible import configuration
type ImportOptions map[string]interface{}

// ParseAndValidateWhereFilterForImport parses a where filter specifically for import context
func ParseAndValidateWhereFilterForImport(queryString string) ([]*VisitorRelationalExpression, error) {
	expressions := []*VisitorRelationalExpression{}

	// Use import-specific parsing that supports IN/NOT IN operators
	decodedQueryString := SkipWhitespace(queryString)
	for decodedQueryString != "" {
		var expression *VisitorRelationalExpression
		var err error
		// Use import regex that includes IN/NOT IN operators
		decodedQueryString, expression, err = parseAndValidateBinaryExpressionWithRegex(decodedQueryString, importRelationalOperatorRegexp)
		if err != nil {
			return expressions, err
		}
		expressions = append(expressions, expression)
		decodedQueryString = SkipWhitespace(decodedQueryString)
		var operator string
		decodedQueryString, operator = GetLogicalOperator(decodedQueryString)
		if operator == andOperator {
			decodedQueryString = SkipWhitespace(decodedQueryString)
		}
	}

	// Validate that all operators are supported for import context
	for _, expr := range expressions {
		if !isValidImportOperator(expr.Operator) {
			return nil, fmt.Errorf("operator '%s' is not supported for import queries. Supported operators: %v",
				expr.Operator, ImportSupportedOperators)
		}
	}

	return expressions, nil
}

// ValidateImportOperator validates that an operator is supported for import queries
func ValidateImportOperator(operator string) error {
	if !isValidImportOperator(operator) {
		return fmt.Errorf("operator '%s' is not supported for import queries. Supported operators: %v",
			operator, ImportSupportedOperators)
	}
	return nil
}

// ParseWhereFilterForImport parses a where-filter query string into ImportFilters and ImportOptions
// This is the public API version that can be used in tests and other public packages
func ParseWhereFilterForImport(queryString string) ([]ImportFilter, ImportOptions, error) {
	// Parse using the existing RelationalExpression parser
	expressions, err := ParseAndValidateWhereFilterForImport(queryString)
	if err != nil {
		return nil, nil, errors.Wrap(err, "failed to parse where filter")
	}

	var filters []ImportFilter
	options := make(ImportOptions)

	// Convert each RelationalExpression
	for _, expr := range expressions {
		if strings.HasPrefix(expr.Path, "import.") {
			// Handle import options
			err := convertToImportOption(expr, options)
			if err != nil {
				return nil, nil, errors.Wrapf(err, "failed to handle import option '%s'", expr.Path)
			}
		} else {
			// Handle regular filters
			filter, err := convertToImportFilter(expr)
			if err != nil {
				return nil, nil, errors.Wrapf(err, "failed to convert filter for path '%s'", expr.Path)
			}
			filters = append(filters, filter)
		}
	}

	return filters, options, nil
}

// convertToImportFilter converts a VisitorRelationalExpression to an ImportFilter
func convertToImportFilter(expr *VisitorRelationalExpression) (ImportFilter, error) {
	// Map the operator
	operator := mapOperatorToImportOperator(expr.Operator)

	// Extract values
	values := extractValuesFromExpression(expr)

	// Use the path directly as the filter type - let the worker handle path interpretation
	return ImportFilter{
		Type:     expr.Path,
		Operator: operator,
		Values:   values,
	}, nil
}

// convertToImportOption converts a VisitorRelationalExpression with import.* path to an ImportOption
func convertToImportOption(expr *VisitorRelationalExpression, options ImportOptions) error {
	if expr.Operator != "=" {
		return fmt.Errorf("import options only support '=' operator, got '%s'", expr.Operator)
	}

	// Extract option name (remove "import." prefix)
	optionName := strings.TrimPrefix(expr.Path, "import.")

	// Convert literal value to appropriate type
	value, err := convertLiteralToValue(expr.Literal, expr.DataType)
	if err != nil {
		return errors.Wrapf(err, "failed to convert literal value '%s'", expr.Literal)
	}

	options[optionName] = value
	return nil
}

// mapOperatorToImportOperator maps where-filter operators to ImportFilter operators
func mapOperatorToImportOperator(operator string) string {
	switch operator {
	case "=":
		return "include"
	case "!=":
		return "exclude"
	case "IN":
		return "include"
	case "NOT IN":
		return "exclude"
	default:
		return operator
	}
}

// extractValuesFromExpression extracts values from a VisitorRelationalExpression
func extractValuesFromExpression(expr *VisitorRelationalExpression) []string {
	// For IN/NOT IN operators, parse multiple values
	if expr.Operator == "IN" || expr.Operator == "NOT IN" {
		return ParseInClauseValues(expr.Literal)
	}

	// For other operators, single value (remove quotes)
	value := strings.Trim(expr.Literal, "'")
	return []string{value}
}

// ParseInClauseValues parses values from IN/NOT IN clauses like "('value1', 'value2')"
// Exported for use by internal packages
func ParseInClauseValues(literal string) []string {
	// Remove outer parentheses
	literal = strings.Trim(literal, "()")

	// Split by comma and clean up each value
	parts := strings.Split(literal, ",")
	var values []string
	for _, part := range parts {
		value := strings.TrimSpace(part)
		value = strings.Trim(value, "'")
		if value != "" {
			values = append(values, value)
		}
	}

	return values
}

// convertLiteralToValue converts a literal string to the appropriate Go type
func convertLiteralToValue(literal string, dataType DataType) (any, error) {
	switch dataType {
	case DataTypeBool:
		return parseBoolLiteral(literal), nil
	case DataTypeInt:
		// Return as string since ImportOptions expects interface{}
		// The worker will parse it as needed
		return literal, nil
	case DataTypeString:
		// Remove quotes from string literals
		if len(literal) >= 2 && literal[0] == '\'' && literal[len(literal)-1] == '\'' {
			return literal[1 : len(literal)-1], nil
		}
		return literal, nil
	default:
		return literal, nil
	}
}

func convertNumberToInt(value any) (int, error) {
	if intValue, ok := value.(int); ok {
		return intValue, nil
	} else if int64Value, ok := value.(int64); ok {
		// Handle database int64 values
		// TODO: Handle full 64-bit values
		return int(int64Value), nil
	} else if floatValue, ok := value.(float64); ok {
		// Handle JSON numbers that parse as float64
		return int(floatValue), nil
	} else if strValue, ok := value.(string); ok {
		// Handle string literals from parsed queries
		parsed, err := strconv.Atoi(strValue)
		if err != nil {
			return 0, errors.Wrap(err, "internal error: failed to parse string as int")
		}
		return parsed, nil
	} else {
		return 0, fmt.Errorf("internal error: expected int but got %T", value)
	}
}

// CustomStringComparator allows injecting custom string comparison logic for specific path patterns
type CustomStringComparator interface {
	// MatchesPath returns true if this comparator should handle the given path
	MatchesPath(path string) bool
	// Evaluate performs the comparison of a value and literal and returns the result
	Evaluate(expr *RelationalExpression, value string) (bool, error)
}

// EvaluateExpression evaluates a relational expression against a value of any type
// Returns (matched, error) where error indicates type conversion failure
func EvaluateExpression(expr *RelationalExpression, leftValue any, rightValue any, customComparators []CustomStringComparator) (bool, error) {
	// Handle IN/NOT IN operators first (they work with any data type)
	if expr.Operator == "IN" || expr.Operator == "NOT IN" {
		if rightValue != nil {
			return false, fmt.Errorf("expected nil rightValue, got %v", rightValue)
		}
		// This returns a list of unparsed string-encoded values (e.g., quoted strings)
		inValues := ParseInClauseValues(expr.Literal)
		return evaluateInExpression(expr.Operator, leftValue, inValues)
	}

	// The left operand is a path/attribute and the right operand is a path/attribute or literal.
	// Path/attribute values must be extracted before calling EvaluateExpression.

	switch expr.DataType {
	case DataTypeString:
		leftStringValue, ok := leftValue.(string)
		if !ok {
			// Try to convert using reflection for string type aliases
			var err error
			leftStringValue, err = valueToStringWithReflection(leftValue)
			if err != nil {
				return false, fmt.Errorf("internal error: expected string but got %T", leftValue)
			}
		}
		// Check if any custom comparators match this path. They assume the right operand is a literal.
		for _, comparator := range customComparators {
			if comparator.MatchesPath(expr.Path) {
				return comparator.Evaluate(expr, leftStringValue)
			}
		}
		var rightStringValue string
		if rightValue != nil {
			rightStringValue, ok = rightValue.(string)
			if !ok {
				// Try to convert using reflection for string type aliases
				var err error
				rightStringValue, err = valueToStringWithReflection(rightValue)
				if err != nil {
					return false, fmt.Errorf("internal error: expected string but got %T", rightValue)
				}
			}
		} else {
			rightStringValue = parseStringLiteral(expr.Literal)
		}
		return evaluateStringExpression(expr.Operator, leftStringValue, rightStringValue)
	case DataTypeInt:
		leftIntValue, err := convertNumberToInt(leftValue)
		if err != nil {
			return false, fmt.Errorf("internal error: expected number but got %T", leftValue)
		}
		var rightIntValue int
		if rightValue != nil {
			rightIntValue, err = convertNumberToInt(rightValue)
			if err != nil {
				return false, fmt.Errorf("internal error: expected number but got %T", rightValue)
			}
		} else {
			rightIntValue, err = parseIntLiteral(expr.Literal)
			if err != nil {
				return false, errors.Wrap(err, "internal error: invalid number literal")
			}
		}
		result := evaluateIntExpression(expr.Operator, leftIntValue, rightIntValue)
		return result, nil
	case DataTypeBool:
		leftBoolValue, ok := leftValue.(bool)
		if !ok {
			return false, fmt.Errorf("internal error: expected bool but got %T", leftValue)
		}
		var rightBoolValue bool
		if rightValue != nil {
			rightBoolValue, ok = rightValue.(bool)
			if !ok {
				return false, fmt.Errorf("internal error: expected bool but got %T", rightValue)
			}
		} else {
			rightBoolValue = parseBoolLiteral(expr.Literal)
		}
		return evaluateBoolExpression(expr.Operator, leftBoolValue, rightBoolValue), nil
	case DataTypeUUID:
		leftUUIDValue, ok := leftValue.(uuid.UUID)
		if !ok {
			return false, fmt.Errorf("internal error: expected uuid.UUID but got %T", leftValue)
		}
		var rightUUIDValue uuid.UUID
		switch rv := rightValue.(type) {
		case nil:
			// The right operand is a literal in expr.Literal.
			var err error
			rightUUIDValue, err = uuid.Parse(strings.Trim(expr.Literal, "'"))
			if err != nil {
				return false, errors.Wrap(err, "invalid UUID literal")
			}
		case uuid.UUID:
			rightUUIDValue = rv
		case string:
			// A cross-entity operand (or an IN element) may arrive as a string;
			// parse it into a UUID.
			var err error
			rightUUIDValue, err = uuid.Parse(strings.Trim(rv, "'"))
			if err != nil {
				return false, errors.Wrap(err, "invalid UUID value")
			}
		default:
			return false, errors.Errorf("internal error: expected uuid.UUID or string but got %T", rightValue)
		}
		return evaluateUUIDExpression(expr.Operator, leftUUIDValue, rightUUIDValue)
	case DataTypeTime:
		leftTimeValue, ok := leftValue.(time.Time)
		if !ok {
			return false, fmt.Errorf("internal error: expected time.Time but got %T", leftValue)
		}
		var rightTimeValue time.Time
		if rightValue != nil {
			rightTimeValue, ok = rightValue.(time.Time)
			if !ok {
				return false, fmt.Errorf("internal error: expected time.Time but got %T", rightValue)
			}
		} else {
			var err error
			rightTimeValue, err = parseTimeLiteral(expr.Literal)
			if err != nil {
				return false, errors.Wrap(err, "internal error: invalid time literal")
			}
		}
		return evaluateTimeExpression(expr.Operator, leftTimeValue, rightTimeValue)

	// These 3 cases have left operands that are collections (arrays, maps) and scalar right operands

	case DataTypeUUIDArray:
		uuidArrayValue, ok := leftValue.([]uuid.UUID)
		if !ok {
			return false, fmt.Errorf("internal error: expected []uuid.UUID but got %T", leftValue)
		}
		if expr.IsLengthExpression {
			// Length comparison - evaluate LEN(array) against integer literal
			var err error
			var rightIntValue int
			if rightValue != nil {
				rightIntValue, err = convertNumberToInt(rightValue)
				if err != nil {
					return false, fmt.Errorf("internal error: expected number but got %T", rightValue)
				}
			} else {
				rightIntValue, err = parseIntLiteral(expr.Literal)
				if err != nil {
					return false, errors.Wrap(err, "internal error: invalid number literal")
				}
			}
			return evaluateIntExpression(expr.Operator, len(uuidArrayValue), rightIntValue), nil
		}
		// Right operand must be a UUID
		var rightUUIDValue uuid.UUID
		if rightValue != nil {
			rightUUIDValue, ok = rightValue.(uuid.UUID)
			if !ok {
				return false, fmt.Errorf("internal error: expected uuid.UUID but got %T", rightValue)
			}
		} else {
			var err error
			literalStr := strings.Trim(expr.Literal, "'")
			rightUUIDValue, err = uuid.Parse(literalStr)
			if err != nil {
				return false, errors.Wrap(err, "invalid UUID literal")
			}
		}
		return evaluateUUIDArrayExpression(expr.Operator, uuidArrayValue, rightUUIDValue)
	case DataTypeStringBoolMap:
		stringBoolMapValue, ok := leftValue.(map[string]bool)
		if !ok {
			return false, fmt.Errorf("internal error: expected map[string]bool but got %T", leftValue)
		}
		if expr.IsLengthExpression {
			// Length comparison - evaluate LEN(map) against integer literal
			var err error
			var rightIntValue int
			if rightValue != nil {
				rightIntValue, err = convertNumberToInt(rightValue)
				if err != nil {
					return false, fmt.Errorf("internal error: expected number but got %T", rightValue)
				}
			} else {
				rightIntValue, err = parseIntLiteral(expr.Literal)
				if err != nil {
					return false, errors.Wrap(err, "internal error: invalid number literal")
				}
			}
			return evaluateIntExpression(expr.Operator, len(stringBoolMapValue), rightIntValue), nil
		}
		var rightStringValue string
		if rightValue != nil {
			rightStringValue, ok = rightValue.(string)
			if !ok {
				return false, fmt.Errorf("internal error: expected string but got %T", rightValue)
			}
		} else {
			rightStringValue = parseStringLiteral(expr.Literal)
		}
		return evaluateStringBoolMapExpression(expr.Operator, stringBoolMapValue, rightStringValue)
	case DataTypeStringMap:
		stringMapValue, ok := leftValue.(map[string]string)
		if !ok {
			return false, fmt.Errorf("internal error: expected map[string]string but got %T", leftValue)
		}
		if expr.IsLengthExpression {
			// Length comparison - evaluate LEN(map) against integer literal
			var err error
			var rightIntValue int
			if rightValue != nil {
				rightIntValue, err = convertNumberToInt(rightValue)
				if err != nil {
					return false, fmt.Errorf("internal error: expected number but got %T", rightValue)
				}
			} else {
				rightIntValue, err = parseIntLiteral(expr.Literal)
				if err != nil {
					return false, errors.Wrap(err, "internal error: invalid number literal")
				}
			}
			return evaluateIntExpression(expr.Operator, len(stringMapValue), rightIntValue), nil
		}
		var rightStringValue string
		if rightValue != nil {
			rightStringValue, ok = rightValue.(string)
			if !ok {
				return false, fmt.Errorf("internal error: expected string but got %T", rightValue)
			}
		} else {
			rightStringValue = parseStringLiteral(expr.Literal)
		}
		return evaluateStringMapExpression(expr.Operator, stringMapValue, rightStringValue)
	case DataTypeUUIDStringMap:
		uuidStringMapValue, ok := leftValue.(map[uuid.UUID]string)
		if !ok {
			return false, fmt.Errorf("internal error: expected map[uuid]string but got %T", leftValue)
		}
		if expr.IsLengthExpression {
			// Length comparison - evaluate LEN(map) against integer literal
			var err error
			var rightIntValue int
			if rightValue != nil {
				rightIntValue, err = convertNumberToInt(rightValue)
				if err != nil {
					return false, fmt.Errorf("internal error: expected number but got %T", rightValue)
				}
			} else {
				rightIntValue, err = parseIntLiteral(expr.Literal)
				if err != nil {
					return false, errors.Wrap(err, "internal error: invalid number literal")
				}
			}
			return evaluateIntExpression(expr.Operator, len(uuidStringMapValue), rightIntValue), nil
		}
		// Right operand must be a UUID
		var rightUUIDValue uuid.UUID
		if rightValue != nil {
			rightUUIDValue, ok = rightValue.(uuid.UUID)
			if !ok {
				return false, fmt.Errorf("internal error: expected uuid.UUID but got %T", rightValue)
			}
		} else {
			var err error
			literalStr := strings.Trim(expr.Literal, "'")
			rightUUIDValue, err = uuid.Parse(literalStr)
			if err != nil {
				return false, errors.Wrap(err, "invalid UUID literal")
			}
		}
		return evaluateUUIDStringMapExpression(expr.Operator, uuidStringMapValue, rightUUIDValue)
	case DataTypeStringStringUUIDBoolMap:
		// Permissions type: map[string]map[string]map[uuid.UUID]bool
		// This case is primarily handled by the in-memory filter evaluator in filter_impl.go
		// which passes the MapKey and NestedMapKey directly to EvaluatePermissionsExpression.
		// This code path is for direct evaluation where the leftValue is already PermissionsData.
		permissionsValue, ok := leftValue.(PermissionsData)
		if !ok {
			return false, fmt.Errorf("internal error: expected PermissionsData but got %T", leftValue)
		}
		// For direct evaluation without filter context, we can only evaluate LEN(Permissions)
		// Other operations require the action key and field key from the filter expression
		return EvaluatePermissionsExpression(expr.Operator, permissionsValue, rightValue, expr.IsLengthExpression, "", "")
	default:
		return false, fmt.Errorf("unsupported data type %s", expr.DataType)
	}
}

func parseStringLiteral(literal string) string {
	return strings.Trim(literal, "'")
}

// evaluateStringExpression evaluates string relational expressions with custom comparators
func evaluateStringExpression(operator string, leftValue string, rightValue string) (bool, error) {
	switch operator {
	case "=":
		return leftValue == rightValue, nil
	case "!=":
		return leftValue != rightValue, nil
	case "<":
		return leftValue < rightValue, nil
	case "<=":
		return leftValue <= rightValue, nil
	case ">":
		return leftValue > rightValue, nil
	case ">=":
		return leftValue >= rightValue, nil
	case "LIKE":
		return evaluateLikeExpression(leftValue, rightValue, false)
	case "NOT LIKE":
		// SQL NOT LIKE operator
		result, err := evaluateLikeExpression(leftValue, rightValue, false)
		return !result, err
	case "ILIKE":
		return evaluateLikeExpression(leftValue, rightValue, true)
	case "~~":
		// PostgreSQL LIKE operator (same as LIKE)
		return evaluateLikeExpression(leftValue, rightValue, false)
	case "!~~":
		// PostgreSQL NOT LIKE operator (same as NOT LIKE)
		result, err := evaluateLikeExpression(leftValue, rightValue, false)
		return !result, err
	case "~":
		// PostgreSQL POSIX regex match (case-sensitive)
		return evaluateRegexExpression(leftValue, rightValue, false)
	case "~*":
		// PostgreSQL POSIX regex match (case-insensitive)
		return evaluateRegexExpression(leftValue, rightValue, true)
	case "!~":
		// PostgreSQL POSIX regex NOT match (case-sensitive)
		result, err := evaluateRegexExpression(leftValue, rightValue, false)
		return !result, err
	case "!~*":
		// PostgreSQL POSIX regex NOT match (case-insensitive)
		result, err := evaluateRegexExpression(leftValue, rightValue, true)
		return !result, err
	}
	return false, nil
}

func parseIntLiteral(literal string) (int, error) {
	return strconv.Atoi(literal)
}

// evaluateIntExpression evaluates integer relational expressions
func evaluateIntExpression(operator string, leftValue int, rightValue int) bool {
	switch operator {
	case "=":
		return leftValue == rightValue
	case "!=":
		return leftValue != rightValue
	case "<":
		return leftValue < rightValue
	case "<=":
		return leftValue <= rightValue
	case ">":
		return leftValue > rightValue
	case ">=":
		return leftValue >= rightValue
	}
	return false
}

var boolLiterals = map[string]bool{
	"true":  true,
	"TRUE":  true,
	"false": false,
	"FALSE": false,
}

func parseBoolLiteral(literal string) bool {
	return boolLiterals[literal]
}

// evaluateBoolExpression evaluates boolean relational expressions
func evaluateBoolExpression(operator string, leftValue bool, rightValue bool) bool {
	switch operator {
	case "=":
		return leftValue == rightValue
	case "!=":
		return leftValue != rightValue
	}
	return false
}

// evaluateLikeExpression evaluates LIKE and ILIKE expressions with SQL-style wildcards
// % matches zero or more characters, _ matches exactly one character
func evaluateLikeExpression(value, pattern string, caseInsensitive bool) (bool, error) {
	// Convert SQL LIKE pattern to regex pattern
	regexPattern, err := convertLikePatternToRegex(pattern)
	if err != nil {
		return false, err
	}

	// Compile regex with case sensitivity option
	var regex *regexp.Regexp
	if caseInsensitive {
		regex, err = regexp.Compile("(?i)" + regexPattern)
	} else {
		regex, err = regexp.Compile(regexPattern)
	}
	if err != nil {
		return false, errors.Wrap(err, "invalid LIKE pattern")
	}

	return regex.MatchString(value), nil
}

// convertLikePatternToRegex converts SQL LIKE pattern to regex
func convertLikePatternToRegex(pattern string) (string, error) {
	var result strings.Builder
	result.WriteString("^") // Anchor to start

	for i := 0; i < len(pattern); i++ {
		switch pattern[i] {
		case '%':
			result.WriteString(".*") // Zero or more characters
		case '_':
			result.WriteString(".") // Exactly one character
		case '.', '^', '$', '*', '+', '?', '(', ')', '[', ']', '{', '}', '|', '\\':
			// Escape regex special characters
			result.WriteString("\\")
			result.WriteByte(pattern[i])
		default:
			result.WriteByte(pattern[i])
		}
	}

	result.WriteString("$") // Anchor to end
	return result.String(), nil
}

// evaluateRegexExpression evaluates POSIX regular expression operators (~, ~*, !~, !~*)
func evaluateRegexExpression(value, pattern string, caseInsensitive bool) (bool, error) {
	// Compile regex with case sensitivity option
	var regex *regexp.Regexp
	var err error
	if caseInsensitive {
		regex, err = regexp.Compile("(?i)" + pattern)
	} else {
		regex, err = regexp.Compile(pattern)
	}
	if err != nil {
		return false, errors.Wrap(err, "invalid regular expression")
	}

	return regex.MatchString(value), nil
}

func valueToString(value any) (string, error) {
	// Convert the value to string for comparison
	var valueStr string
	switch v := value.(type) {
	case string:
		valueStr = v
	case int:
		valueStr = strconv.Itoa(v)
	case int64:
		valueStr = strconv.FormatInt(v, 10)
	case float64:
		// Convert all floats to integers since actual floats aren't supported
		valueStr = strconv.Itoa(int(v))
	case bool:
		valueStr = strconv.FormatBool(v)
	default:
		return "", fmt.Errorf("unsupported value type for IN operation: %T", value)
	}
	return valueStr, nil
}

// valueToStringWithReflection uses reflection to handle type aliases and custom string types
func valueToStringWithReflection(value any) (string, error) {
	// uuid.UUID is an array under the hood, so reflection can't stringify it;
	// compare it by its canonical string form.
	if u, ok := value.(uuid.UUID); ok {
		return u.String(), nil
	}

	// Use reflection to check if the underlying type is string
	v := reflect.ValueOf(value)

	// Check if the kind is string (handles type aliases)
	if v.Kind() == reflect.String {
		return v.String(), nil
	}

	// Also handle other basic types via reflection
	switch v.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return strconv.FormatInt(v.Int(), 10), nil
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return strconv.FormatUint(v.Uint(), 10), nil
	case reflect.Float32, reflect.Float64:
		// Convert all floats to integers since actual floats aren't supported
		return strconv.Itoa(int(v.Float())), nil
	case reflect.Bool:
		return strconv.FormatBool(v.Bool()), nil
	default:
		return "", fmt.Errorf("unsupported value type for IN operation: %T", value)
	}
}

// evaluateInExpression evaluates IN and NOT IN expressions against any value type
func evaluateInExpression(operator string, value any, inValues []string) (bool, error) {
	// Convert the value to string for comparison
	valueStr, err := valueToString(value)
	if err != nil {
		// If direct conversion fails, try using reflection for type aliases
		valueStr, err = valueToStringWithReflection(value)
		if err != nil {
			return false, err
		}
	}

	// Check if the value is in the list
	found := false
	for _, inValue := range inValues {
		if valueStr == inValue {
			found = true
			break
		}
	}

	// Return result based on operator
	if operator == "IN" {
		return found, nil
	} else { // "NOT IN"
		return !found, nil
	}
}

// evaluateUUIDExpression evaluates UUID relational expressions (equality/inequality only)
func evaluateUUIDExpression(operator string, leftValue uuid.UUID, rightValue uuid.UUID) (bool, error) {
	switch operator {
	case "=":
		return leftValue == rightValue, nil
	case "!=":
		return leftValue != rightValue, nil
	default:
		return false, fmt.Errorf("unsupported operator for UUID: %s", operator)
	}
}

func parseTimeLiteral(literal string) (time.Time, error) {
	// Parse literal as time string (remove quotes if present)
	literalStr := strings.Trim(literal, "'")
	literalTime, err := time.Parse(time.RFC3339, literalStr)
	if err != nil {
		return literalTime, errors.Wrap(err, "invalid time literal")
	}
	return literalTime, nil
}

// evaluateTimeExpression evaluates time relational expressions
func evaluateTimeExpression(operator string, leftValue time.Time, rightValue time.Time) (bool, error) {
	switch operator {
	case "=":
		return leftValue.Equal(rightValue), nil
	case "!=":
		return !leftValue.Equal(rightValue), nil
	case "<":
		return leftValue.Before(rightValue), nil
	case "<=":
		return leftValue.Before(rightValue) || leftValue.Equal(rightValue), nil
	case ">":
		return leftValue.After(rightValue), nil
	case ">=":
		return leftValue.After(rightValue) || leftValue.Equal(rightValue), nil
	default:
		return false, fmt.Errorf("unsupported operator for time: %s", operator)
	}
}

// evaluateUUIDArrayExpression evaluates UUID array expressions with ? operator
func evaluateUUIDArrayExpression(operator string, leftValue []uuid.UUID, rightValue uuid.UUID) (bool, error) {
	switch operator {
	case "?":
		for _, item := range leftValue {
			if item == rightValue {
				return true, nil
			}
		}
		return false, nil
	default:
		return false, fmt.Errorf("unsupported operator for UUID array: %s", operator)
	}
}

// evaluateStringBoolMapExpression evaluates string-bool map expressions with ? operator
func evaluateStringBoolMapExpression(operator string, leftValue map[string]bool, rightValue string) (bool, error) {
	switch operator {
	case "?":
		// Map containment - check if key exists in map
		_, exists := leftValue[rightValue]
		return exists, nil
	default:
		return false, fmt.Errorf("unsupported operator for string-bool map: %s", operator)
	}
}

// evaluateStringMapExpression evaluates string-string map expressions with ? operator
func evaluateStringMapExpression(operator string, leftValue map[string]string, rightValue string) (bool, error) {
	switch operator {
	case "?":
		// Map containment - check if key exists in map
		_, exists := leftValue[rightValue]
		return exists, nil
	default:
		return false, fmt.Errorf("unsupported operator for string map: %s", operator)
	}
}

// evaluateUUIDStringMapExpression evaluates UUID-string map expressions with ? operator
func evaluateUUIDStringMapExpression(operator string, leftValue map[uuid.UUID]string, rightValue uuid.UUID) (bool, error) {
	switch operator {
	case "?":
		// Map containment - check if key exists in map
		_, exists := leftValue[rightValue]
		return exists, nil
	default:
		return false, fmt.Errorf("unsupported operator for UUID-string map: %s", operator)
	}
}

// PermissionsData represents the Permissions type for query evaluation.
// This is map[string]map[string]map[uuid.UUID]bool which corresponds to
// map[ActionCategory]Subjects where Subjects contains UserIDs map[uuid.UUID]bool.
// The structure is: Permissions[action]["UserIDs"][userUUID] = true
type PermissionsData map[string]map[string]map[uuid.UUID]bool

// EvaluatePermissionsExpression evaluates Permissions expressions
// Supports:
//   - LEN(Permissions) - returns total number of action categories with subjects
//   - LEN(Permissions.<action>.UserIDs) - returns number of users for a specific action
//   - Permissions.<action>.UserIDs ? <uuid> - checks if a user has a specific permission
func EvaluatePermissionsExpression(operator string, leftValue PermissionsData, rightValue interface{}, isLengthExpression bool, actionKey string, fieldKey string) (bool, error) {
	if isLengthExpression {
		// Handle LEN(Permissions) - total number of action categories
		if actionKey == "" {
			length := len(leftValue)
			rightInt, err := convertNumberToInt(rightValue)
			if err != nil {
				return false, err
			}
			return evaluateIntExpression(operator, length, rightInt), nil
		}

		// Handle LEN(Permissions.<action>.UserIDs)
		subjects, exists := leftValue[actionKey]
		if !exists {
			// If action doesn't exist, length is 0
			rightInt, err := convertNumberToInt(rightValue)
			if err != nil {
				return false, err
			}
			return evaluateIntExpression(operator, 0, rightInt), nil
		}

		userIDs, exists := subjects[fieldKey]
		if !exists || userIDs == nil {
			rightInt, err := convertNumberToInt(rightValue)
			if err != nil {
				return false, err
			}
			return evaluateIntExpression(operator, 0, rightInt), nil
		}

		rightInt, err := convertNumberToInt(rightValue)
		if err != nil {
			return false, err
		}
		return evaluateIntExpression(operator, len(userIDs), rightInt), nil
	}

	// Handle Permissions.<action>.UserIDs ? <uuid>
	if operator != "?" {
		return false, fmt.Errorf("unsupported operator for Permissions: %s (only ? is supported for containment)", operator)
	}

	// Right value should be a UUID
	var rightUUID uuid.UUID
	switch v := rightValue.(type) {
	case uuid.UUID:
		rightUUID = v
	case string:
		var err error
		rightUUID, err = uuid.Parse(v)
		if err != nil {
			return false, errors.Wrap(err, "invalid UUID")
		}
	default:
		return false, fmt.Errorf("expected UUID for Permissions containment check, got %T", rightValue)
	}

	subjects, exists := leftValue[actionKey]
	if !exists {
		return false, nil
	}

	userIDs, exists := subjects[fieldKey]
	if !exists || userIDs == nil {
		return false, nil
	}

	return userIDs[rightUUID], nil
}
