// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"fmt"
	"reflect"
	"strings"
	"time"

	"github.com/google/uuid"
)

// DynamicColumnProvider provides dynamic column access for any struct
type DynamicColumnProvider struct {
	entityType    string
	aliases       map[string]string
	formatters    map[reflect.Type]func(reflect.Value) string
	customColumns map[string]func(any) string
}

// NewDynamicColumnProvider creates a provider with common aliases and formatters
func NewDynamicColumnProvider(entity any) *DynamicColumnProvider {
	entityTypeName := strings.TrimPrefix(reflect.TypeOf(entity).Elem().Name(), "Extended")
	return &DynamicColumnProvider{
		entityType: entityTypeName,
		aliases: map[string]string{
			"Name": "Slug", // Common alias across entities
		},
		customColumns: make(map[string]func(any) string),
		formatters: map[reflect.Type]func(reflect.Value) string{
			reflect.TypeOf(time.Time{}): func(v reflect.Value) string {
				if v.IsZero() {
					return ""
				}
				t := v.Interface().(time.Time)
				return t.Format(time.RFC3339)
			},
			reflect.TypeOf(uuid.UUID{}): func(v reflect.Value) string {
				u := v.Interface().(uuid.UUID)
				if u == uuid.Nil {
					return ""
				}
				return u.String()
			},
			reflect.TypeOf((*uuid.UUID)(nil)): func(v reflect.Value) string {
				if v.IsNil() {
					return ""
				}
				return v.Interface().(*uuid.UUID).String()
			},
		},
	}
}

// WithAliases adds or overrides aliases for specific entity types
func (p *DynamicColumnProvider) WithAliases(aliases map[string]string) *DynamicColumnProvider {
	for k, v := range aliases {
		p.aliases[k] = v
	}
	return p
}

// WithCustomColumns adds custom column functions
func (p *DynamicColumnProvider) WithCustomColumns(columns map[string]func(any) string) *DynamicColumnProvider {
	for k, v := range columns {
		p.customColumns[k] = v
	}
	return p
}

// GetValue dynamically gets a value from a struct using a field path
func (p *DynamicColumnProvider) GetValue(obj any, fieldPath string) string {
	// Debug: uncomment to see what's being requested
	// fmt.Printf("GetValue called with fieldPath: %s\n", fieldPath)

	// Check for custom columns first
	if customFunc, ok := p.customColumns[fieldPath]; ok {
		return customFunc(obj)
	}

	// Handle aliases
	if alias, ok := p.aliases[fieldPath]; ok {
		fieldPath = alias
	}

	fieldPrefix := ""
	entityPrefix := p.entityType + "."
	if strings.HasPrefix(fieldPath, entityPrefix) {
		fieldPrefix = entityPrefix
	}

	// Handle special cases for Labels and Annotations (only for non-nested paths)
	if fieldPrefix == "" {
		labelsPrefix := "Labels"
		if strings.HasPrefix(fieldPath, labelsPrefix+".") {
			// First try at top level
			result := p.getMapValue(obj, labelsPrefix, strings.TrimPrefix(fieldPath, labelsPrefix+"."))
			if result != "" {
				return result
			}
			// If not found and we have an entityType (from ExtendedX), try X.Labels
			if p.entityType != "" {
				return p.GetValue(obj, p.entityType+"."+fieldPath)
			}
		}
		annotationsPrefix := "Annotations"
		if strings.HasPrefix(fieldPath, annotationsPrefix+".") {
			// First try at top level
			result := p.getMapValue(obj, annotationsPrefix, strings.TrimPrefix(fieldPath, annotationsPrefix+"."))
			if result != "" {
				return result
			}
			// If not found and we have an entityType (from ExtendedX), try X.Annotations
			if p.entityType != "" {
				return p.GetValue(obj, p.entityType+"."+fieldPath)
			}
		}
	}

	// Use reflection to navigate the field path
	v := reflect.ValueOf(obj)
	if v.Kind() == reflect.Ptr {
		if v.IsNil() {
			return ""
		}
		v = v.Elem()
	}

	parts := strings.Split(fieldPath, ".")
	for i := 0; i < len(parts); i++ {
		part := parts[i]

		// Dereference pointers
		for v.Kind() == reflect.Ptr {
			if v.IsNil() {
				return ""
			}
			v = v.Elem()
		}

		// Check if current value is a struct (to get fields) or map (to get keys)
		if v.Kind() == reflect.Struct {
			// Get the field by name
			field := v.FieldByName(part)
			if !field.IsValid() {
				// If this is the first part and we have an entityType (from ExtendedX),
				// try to find it under the X field as a convenience
				if i == 0 && p.entityType != "" {
					entityField := v.FieldByName(p.entityType)
					if entityField.IsValid() && entityField.Kind() == reflect.Ptr && !entityField.IsNil() {
						entityStruct := entityField.Elem()
						field = entityStruct.FieldByName(part)
						if field.IsValid() {
							// Found it under the entity field, continue from there
							v = field
							continue
						}
					}
				}
				return "?"
			}
			v = field
		} else if v.Kind() == reflect.Map {
			// Current value is a map, so this part is a key
			mapKeyValue := reflect.ValueOf(part)
			value := v.MapIndex(mapKeyValue)
			if !value.IsValid() {
				return ""
			}
			v = value
		} else {
			// Can't navigate further
			return "?"
		}
	}

	return p.formatValue(v)
}

// getMapValue extracts a value from a map field
func (p *DynamicColumnProvider) getMapValue(obj any, mapField, key string) string {
	v := reflect.ValueOf(obj)
	if v.Kind() == reflect.Ptr {
		if v.IsNil() {
			return ""
		}
		v = v.Elem()
	}

	field := v.FieldByName(mapField)
	if !field.IsValid() || field.Kind() != reflect.Map {
		return ""
	}

	mapKey := reflect.ValueOf(key)
	value := field.MapIndex(mapKey)
	if !value.IsValid() {
		return ""
	}

	return p.formatValue(value)
}

// formatValue formats a reflect.Value based on its type
func (p *DynamicColumnProvider) formatValue(v reflect.Value) string {
	if !v.IsValid() {
		return ""
	}

	// Check for nil pointers
	if v.Kind() == reflect.Ptr && v.IsNil() {
		return ""
	}

	// Dereference pointers for type checking
	actualValue := v
	if v.Kind() == reflect.Ptr {
		actualValue = v.Elem()
	}

	// Check custom formatters - check both pointer and non-pointer types
	if formatter, ok := p.formatters[v.Type()]; ok {
		return formatter(v)
	}
	if formatter, ok := p.formatters[actualValue.Type()]; ok {
		return formatter(actualValue)
	}

	// Default formatting
	switch actualValue.Kind() {
	case reflect.String:
		return actualValue.String()
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return fmt.Sprintf("%d", actualValue.Int())
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return fmt.Sprintf("%d", actualValue.Uint())
	case reflect.Float32, reflect.Float64:
		return fmt.Sprintf("%g", actualValue.Float())
	case reflect.Bool:
		return fmt.Sprintf("%t", actualValue.Bool())
	case reflect.Slice:
		if actualValue.Type().Elem().Kind() == reflect.Uint8 {
			// []byte - show size
			return fmt.Sprintf("%d bytes", actualValue.Len())
		}
		if actualValue.Len() == 0 {
			return ""
		}
		return fmt.Sprintf("%d items", actualValue.Len())
	case reflect.Map:
		if actualValue.Len() == 0 {
			return "None"
		} else if actualValue.Len() == 1 {
			for _, key := range actualValue.MapKeys() {
				return key.String()
			}
		}
		return "Multiple"
	default:
		// For complex types, try to use String() method if available
		if stringer, ok := v.Interface().(fmt.Stringer); ok {
			return stringer.String()
		}
		return fmt.Sprintf("%v", v.Interface())
	}
}

// DisplayListGeneric displays a list of entities with dynamic columns
func DisplayListGeneric[T any](entities []*T, columnSpec string, defaultCols []string, aliases map[string]string, customColumns map[string]func(any) string) {
	provider := NewDynamicColumnProvider(new(T))
	if aliases != nil {
		provider.WithAliases(aliases)
	}
	if customColumns != nil {
		provider.WithCustomColumns(customColumns)
	}

	// Parse columns
	var cols []string
	if columnSpec == "" {
		cols = defaultCols
	} else {
		cols = strings.Split(columnSpec, ",")
		for i := range cols {
			cols[i] = strings.TrimSpace(cols[i])
		}
	}

	table := tableView()

	// Set headers
	if !noheader {
		headers := make([]string, len(cols))
		for i, col := range cols {
			headers[i] = columnToHeader(provider, col)
		}
		table.SetHeader(headers)
	}

	// Add rows
	for _, entity := range entities {
		row := make([]string, len(cols))
		for i, col := range cols {
			row[i] = provider.GetValue(entity, col)
		}
		table.Append(row)
	}

	table.Render()
}

// columnToHeader converts column name to header format
func columnToHeader(provider *DynamicColumnProvider, col string) string {
	col = strings.TrimPrefix(col, provider.entityType+".")

	if strings.HasPrefix(col, "Labels.") {
		return "Label:" + strings.TrimPrefix(col, "Labels.")
	}
	if strings.HasPrefix(col, "Annotations.") {
		return "Annotation:" + strings.TrimPrefix(col, "Annotations.")
	}

	// Handle aliases: the provided column name was the alias
	if _, ok := provider.aliases[col]; ok {
		return col
	}

	// Handle slugs
	if col == "Slug" {
		return "Name"
	}
	if strings.HasSuffix(col, ".Slug") {
		return strings.TrimSuffix(col, ".Slug")
	}

	// Handled extended fields - take the last segment/field
	segments := strings.Split(col, ".")
	if len(segments) > 1 {
		col = segments[len(segments)-1]
	}

	// Convert CamelCase to Hyphen-Case
	var result []rune
	for i, r := range col {
		if i > 0 && r >= 'A' && r <= 'Z' {
			// Don't add hyphen if previous character was also uppercase (e.g., "ID" -> "ID", not "I-D")
			if i == 0 || (i > 0 && col[i-1] < 'A' || col[i-1] > 'Z') {
				result = append(result, '-')
			}
		}
		if (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			result = append(result, r)
		} else if len(result) > 0 && result[len(result)-1] != '-' {
			// Convert non-alphanumeric characters
			result = append(result, '-')
		}
	}

	// Special cases for common abbreviations
	header := string(result)
	header = strings.Replace(header, "ID", "ID", -1)
	header = strings.Replace(header, "URL", "URL", -1)
	header = strings.Replace(header, "API", "API", -1)

	return header
}

// buildSelectList builds a select parameter based on specified columns
func buildSelectList(entity string, columnsSpec string, include string, defaultCols []string, aliases map[string]string, customColumnDeps map[string][]string, baseFields []string) string {
	columns := strings.Split(columnsSpec, ",")
	if columnsSpec == "" {
		columns = defaultCols
	}

	fieldsMap := make(map[string]bool)

	for _, col := range columns {
		col = strings.TrimSpace(col)
		originalCol := col

		// Resolve aliases
		if alias, exists := aliases[col]; exists {
			col = alias
		}

		actualCols := []string{col}
		// Add dependencies for custom columns. Should be mutally exclusive with aliases.
		if deps, exists := customColumnDeps[originalCol]; exists {
			actualCols = deps
		}

		for _, actualCol := range actualCols {
			// Handle prefixed columns (Unit.Slug -> Slug)
			if strings.Contains(actualCol, ".") {
				actualCol = strings.TrimPrefix(actualCol, entity+".")
				parts := strings.Split(actualCol, ".")
				if parts[0] == "Labels" || parts[0] == "Annotations" {
					actualCol = parts[0]
				} else if len(parts) > 1 {
					// This is an included relationship field (Space.Slug, Target.Slug, etc.)
					// Keep full actualCol.
					// TODO: Except for UnitStatus, which is synthetic. Fix this.
					if parts[0] == "UnitStatus" {
						continue
					}
				}
			} else if strings.Contains(actualCol, "Count") {
				// This is a summary field
				continue
			}
			// Regular field - add it to select
			fieldsMap[actualCol] = true
		}
	}

	// Always include base required fields
	for _, field := range baseFields {
		fieldsMap[field] = true
	}

	// Add include fields
	if include != "" {
		includeFields := strings.Split(include, ",")
		for _, field := range includeFields {
			fieldsMap[field] = true
		}
	}

	// Convert to slice and join
	var fields []string
	for field := range fieldsMap {
		fields = append(fields, field)
	}

	return strings.Join(fields, ",")
}

// handleSelectParameter processes the select parameter for API calls
// Parameters:
//   - selectParam: the select parameter passed to the function (can be "", "*", or a field list)
//   - globalSelectFields: the global selectFields variable value
//   - autoSelectFunc: a function that returns the auto-selected fields when no select is specified
//
// Returns the select string to be used in the API call, or empty string for all fields
func handleSelectParameter(selectParam string, globalSelectFields string, autoSelectFunc func() string) string {
	// Handle function-level select parameter first
	if selectParam == "*" {
		// "*" means get all fields, represented by empty string
		return ""
	} else if selectParam != "" {
		// Use the provided select parameter
		return selectParam
	}

	// Fall back to global selectFields
	if globalSelectFields == "*" {
		// "*" means get all fields
		return ""
	} else if globalSelectFields != "" {
		// Use global selectFields
		return globalSelectFields
	}

	// Auto-select fields if no select parameter is specified and not in special output mode
	if autoSelectFunc != nil {
		return autoSelectFunc()
	}
	// TODO: If names is true, should just select the appropriate "Slug" field

	// Default to empty string (all fields)
	return ""
}
