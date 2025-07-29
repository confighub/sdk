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

	// Handle special cases for Labels and Annotations
	labelsPrefix := fieldPrefix + "Labels"
	if strings.HasPrefix(fieldPath, labelsPrefix+".") {
		return p.getMapValue(obj, labelsPrefix, strings.TrimPrefix(fieldPath, labelsPrefix+"."))
	}
	annotationsPrefix := fieldPrefix + "Annotations"
	if strings.HasPrefix(fieldPath, annotationsPrefix+".") {
		return p.getMapValue(obj, annotationsPrefix, strings.TrimPrefix(fieldPath, annotationsPrefix+"."))
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
	for _, part := range parts {
		if v.Kind() == reflect.Ptr {
			if v.IsNil() {
				return ""
			}
			v = v.Elem()
		}

		field := v.FieldByName(part)
		if !field.IsValid() {
			return "?"
		}
		v = field
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
