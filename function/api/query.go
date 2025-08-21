// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package api

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
)

// Path expressions support embedded accessors and escaped dots.
// They also support wildcards and associative matches.
// Kubernetes annotations and labels permit slashes
const (
	andOperator = "AND"

	parameterNameRegexpString  = "(?:[A-Za-z][A-Za-z0-9_\\-]{0,127})"
	pathMapSegmentRegexpString = "(?:[A-Za-z](?:[A-Za-z0-9/_\\-]|(?:\\~[12])){0,127})"

	pathMapSegmentBoundtoParameterRegexpString = "(?:@" + pathMapSegmentRegexpString + "\\:" + parameterNameRegexpString + ")"
	pathIndexSegmentRegexpString               = "(?:[0-9][0-9]{0,9})"
	pathWildcardSegmentRegexpString            = "\\*(?:(?:\\?" + pathMapSegmentRegexpString + "(?:\\:" + parameterNameRegexpString + ")?)|(?:@\\:" + parameterNameRegexpString + "))?"
	pathAssociativeMatchRegexpString           = "\\?" + pathMapSegmentRegexpString + "(?:\\:" + parameterNameRegexpString + ")?=[^.][^.]*"
	pathSegmentRegexpString                    = "(?:" + pathMapSegmentRegexpString + "|" + pathMapSegmentBoundtoParameterRegexpString + "|" + pathIndexSegmentRegexpString + "|" + pathWildcardSegmentRegexpString + "|" + pathAssociativeMatchRegexpString + ")"

	// Path segment without patterns (for right side of split)
	pathSegmentWithoutPatternsRegexpString = "(?:" + pathMapSegmentRegexpString + "|" + pathMapSegmentBoundtoParameterRegexpString + "|" + pathIndexSegmentRegexpString + ")"
	pathRegexpString                       = "^" + pathSegmentRegexpString + "(?:\\." + pathSegmentRegexpString + ")*(?:\\.\\|" + pathSegmentWithoutPatternsRegexpString + "(?:\\." + pathSegmentWithoutPatternsRegexpString + ")*)?(?:#" + pathMapSegmentRegexpString + ")?"
	whitespaceRegexpString                 = "^[ \t][ \t]*"
	relationalOperatorRegexpString         = "^(<=|>=|<|>|=|\\!=|LIKE|ILIKE|~~|!~~|~\\*|!~\\*|~|!~|IN|NOT IN)"
	logicalOperatorRegexpString            = "^AND"
	booleanLiteralRegexpString             = "^(true|false)"
	integerLiteralRegexpString             = "^[0-9][0-9]{0,9}"
	safeStringCharsRegexpString            = `[^'"\\]*`
	stringLiteralRegexpString              = `^'` + safeStringCharsRegexpString + `'`
	inClauseRegexpString                   = "^\\((?:[^)]+)\\)"
)

var (
	pathNameRegexp           = regexp.MustCompile(pathRegexpString)
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

func ParseAndValidateWhereFilter(queryString string) ([]*VisitorRelationalExpression, error) {
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

	return remaining, literal, nil
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
	Type string `json:",omitempty"`

	// Operator specifies how to apply the filter (include, exclude, equals, contains, matches)
	Operator string `json:",omitempty"`

	// Values specifies the filter values
	Values []string `json:",omitempty"`
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
		return nil, nil, fmt.Errorf("failed to parse where filter: %w", err)
	}

	var filters []ImportFilter
	options := make(ImportOptions)

	// Convert each RelationalExpression
	for _, expr := range expressions {
		if strings.HasPrefix(expr.Path, "import.") {
			// Handle import options
			err := convertToImportOption(expr, options)
			if err != nil {
				return nil, nil, fmt.Errorf("failed to handle import option '%s': %w", expr.Path, err)
			}
		} else {
			// Handle regular filters
			filter, err := convertToImportFilter(expr)
			if err != nil {
				return nil, nil, fmt.Errorf("failed to convert filter for path '%s': %w", expr.Path, err)
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
		return fmt.Errorf("failed to convert literal value '%s': %w", expr.Literal, err)
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
		return literal == "true", nil
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
			return false, fmt.Errorf("internal error: expected string but got %T", leftValue)
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
				return false, fmt.Errorf("internal error: expected string but got %T", rightValue)
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
				return false, fmt.Errorf("internal error: invalid number literal: %w", err)
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
				return false, fmt.Errorf("invalid UUID literal: %w", err)
			}
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
				return false, fmt.Errorf("internal error: invalid time literal: %w", err)
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
					return false, fmt.Errorf("internal error: invalid number literal: %w", err)
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
				return false, fmt.Errorf("invalid UUID literal: %w", err)
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
					return false, fmt.Errorf("internal error: invalid number literal: %w", err)
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
					return false, fmt.Errorf("internal error: invalid number literal: %w", err)
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
					return false, fmt.Errorf("internal error: invalid number literal: %w", err)
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
				return false, fmt.Errorf("invalid UUID literal: %w", err)
			}
		}
		return evaluateUUIDStringMapExpression(expr.Operator, uuidStringMapValue, rightUUIDValue)
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
	case "ILIKE":
		return evaluateLikeExpression(leftValue, rightValue, true)
	case "~~":
		// PostgreSQL LIKE operator (same as LIKE)
		return evaluateLikeExpression(leftValue, rightValue, false)
	case "!~~":
		// PostgreSQL NOT LIKE operator
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

func parseBoolLiteral(literal string) bool {
	return literal == "true"
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
		return false, fmt.Errorf("invalid LIKE pattern: %w", err)
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
		return false, fmt.Errorf("invalid regular expression: %w", err)
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

// evaluateInExpression evaluates IN and NOT IN expressions against any value type
func evaluateInExpression(operator string, value any, inValues []string) (bool, error) {
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
		return false, fmt.Errorf("unsupported value type for IN operation: %T", value)
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
		return literalTime, fmt.Errorf("invalid time literal: %w", err)
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
