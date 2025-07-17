// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package api

import (
	"fmt"
	"net/url"
	"regexp"
	"strings"
)

// Path expressions support embedded accessors and escaped dots.
// They also support wildcards and associative matches.
// Kubernetes annotations and labels permit slashes
const (
	andOperator   = "AND"
	inOperator    = "IN"
	notInOperator = "NOT IN"

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
	relationalOperatorRegexpString         = "^(<=|>=|<|>|=|\\!=)"
	logicalOperatorRegexpString            = "^AND"
	booleanLiteralRegexpString             = "^(true|false)"
	integerLiteralRegexpString             = "^[0-9][0-9]{0,9}"
	stringLiteralRegexpString              = `^'[^'"\\]{0,255}'`
	inClauseRegexpString                   = "^\\((?:[^)]+)\\)"
)

var (
	pathNameRegexp           = regexp.MustCompile(pathRegexpString)
	whitespaceRegexp         = regexp.MustCompile(whitespaceRegexpString)
	relationalOperatorRegexp = regexp.MustCompile(relationalOperatorRegexpString)
	LogicalOperatorRegexp    = regexp.MustCompile(logicalOperatorRegexpString)

	// Exported Literal patterns
	BooleanLiteralRegexp = regexp.MustCompile(booleanLiteralRegexpString)
	IntegerLiteralRegexp = regexp.MustCompile(integerLiteralRegexpString)
	StringLiteralRegexp  = regexp.MustCompile(stringLiteralRegexpString)
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
	Path     string
	Operator string
	Literal  string
	DataType DataType
	// New fields for split path feature
	VisitorPath string // Left side of .| for visitor
	SubPath     string // Right side of .| for property check
	IsSplitPath bool   // Whether this uses the .|syntax
}

func ParseAndValidateBinaryExpression(decodedQueryString string) (string, *RelationalExpression, error) {
	return parseAndValidateBinaryExpressionWithRegex(decodedQueryString, relationalOperatorRegexp)
}

// parseAndValidateBinaryExpressionWithRegex allows specifying which operator regex to use
func parseAndValidateBinaryExpressionWithRegex(decodedQueryString string, operatorRegex *regexp.Regexp) (string, *RelationalExpression, error) {
	var expression RelationalExpression

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

	// Handle IN/NOT IN operators specially (only if using import regex)
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

// PreprocessQueryString handles URL decoding and length validation
func PreprocessQueryString(queryString string, maxLength int) (string, error) {
	decodedQueryString, err := url.QueryUnescape(queryString)
	if err != nil {
		return "", fmt.Errorf("failed to decode query string: %w", err)
	}
	
	if maxLength > 0 && len(decodedQueryString) > maxLength {
		return "", fmt.Errorf("query string exceeds maximum length of %d", maxLength)
	}
	
	return decodedQueryString, nil
}

func GetLogicalOperator(decodedQueryString string) (string, string) {
	pos := LogicalOperatorRegexp.FindStringIndex(decodedQueryString)
	if pos != nil {
		return decodedQueryString[pos[1]:], decodedQueryString[pos[0]:pos[1]]
	}
	return decodedQueryString, ""
}

func ParseAndValidateWhereFilter(queryString string) ([]*RelationalExpression, error) {
	expressions := []*RelationalExpression{}

	decodedQueryString := SkipWhitespace(queryString)
	for decodedQueryString != "" {
		var expression *RelationalExpression
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
var importRelationalOperatorRegexpString = "^(<=|>=|<|>|=|\\!=|IN|NOT IN)"
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
func ParseAndValidateWhereFilterForImport(queryString string) ([]*RelationalExpression, error) {
	expressions := []*RelationalExpression{}

	// Use import-specific parsing that supports IN/NOT IN operators
	decodedQueryString := SkipWhitespace(queryString)
	for decodedQueryString != "" {
		var expression *RelationalExpression
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

// convertToImportFilter converts a RelationalExpression to an ImportFilter
func convertToImportFilter(expr *RelationalExpression) (ImportFilter, error) {
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

// convertToImportOption converts a RelationalExpression with import.* path to an ImportOption
func convertToImportOption(expr *RelationalExpression, options ImportOptions) error {
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

// extractValuesFromExpression extracts values from a RelationalExpression
func extractValuesFromExpression(expr *RelationalExpression) []string {
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
func convertLiteralToValue(literal string, dataType DataType) (interface{}, error) {
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
