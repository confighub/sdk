// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package cliutil

import (
	"fmt"
	"io"
	"reflect"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/google/uuid"
)

// ColumnProvider renders columns from arbitrary structs by dotted field path,
// using reflection. It understands the ConfigHub "Extended<Entity>" envelope
// convention (e.g. ExtendedUnit{Unit: *Unit}): a bare field name is looked up on
// the envelope and, failing that, on the embedded entity. It also resolves
// Labels.<key> / Annotations.<key> against map fields. The zero value is unusable;
// construct one with [NewColumnProvider].
type ColumnProvider struct {
	entityType    string
	aliases       map[string]string
	formatters    map[reflect.Type]func(reflect.Value) string
	customColumns map[string]func(any) string
}

// NewColumnProvider creates a provider for the type of entity (typically a
// pointer to a struct), with common aliases (Name -> Slug) and formatters for
// time.Time and uuid.UUID.
func NewColumnProvider(entity any) *ColumnProvider {
	entityTypeName := strings.TrimPrefix(reflect.TypeOf(entity).Elem().Name(), "Extended")
	return &ColumnProvider{
		entityType: entityTypeName,
		aliases:    map[string]string{"Name": "Slug"},
		customColumns: map[string]func(any) string{},
		formatters: map[reflect.Type]func(reflect.Value) string{
			reflect.TypeOf(time.Time{}): func(v reflect.Value) string {
				if v.IsZero() {
					return ""
				}
				return v.Interface().(time.Time).Format(time.RFC3339)
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

// WithAliases adds column-name aliases (alias -> real field path).
func (p *ColumnProvider) WithAliases(aliases map[string]string) *ColumnProvider {
	for k, v := range aliases {
		p.aliases[k] = v
	}
	return p
}

// WithCustomColumns adds named columns computed by a function over the entity.
func (p *ColumnProvider) WithCustomColumns(custom map[string]func(any) string) *ColumnProvider {
	for k, v := range custom {
		p.customColumns[k] = v
	}
	return p
}

// GetValue returns the string value of fieldPath on obj.
func (p *ColumnProvider) GetValue(obj any, fieldPath string) string {
	if customFunc, ok := p.customColumns[fieldPath]; ok {
		return customFunc(obj)
	}
	if alias, ok := p.aliases[fieldPath]; ok {
		fieldPath = alias
	}

	entityPrefix := p.entityType + "."
	nested := strings.HasPrefix(fieldPath, entityPrefix)

	if !nested {
		for _, mapField := range []string{"Labels", "Annotations"} {
			if strings.HasPrefix(fieldPath, mapField+".") {
				if result := p.getMapValue(obj, mapField, strings.TrimPrefix(fieldPath, mapField+".")); result != "" {
					return result
				}
				if p.entityType != "" {
					return p.GetValue(obj, p.entityType+"."+fieldPath)
				}
			}
		}
	}

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
		for v.Kind() == reflect.Ptr {
			if v.IsNil() {
				return ""
			}
			v = v.Elem()
		}
		switch v.Kind() {
		case reflect.Struct:
			field := v.FieldByName(part)
			if !field.IsValid() {
				// Convenience: a bare field on the Extended envelope falls back
				// to the embedded entity (e.g. ExtendedUnit -> .Unit).
				if i == 0 && p.entityType != "" {
					entityField := v.FieldByName(p.entityType)
					if entityField.IsValid() && entityField.Kind() == reflect.Ptr && !entityField.IsNil() {
						if f := entityField.Elem().FieldByName(part); f.IsValid() {
							v = f
							continue
						}
					}
				}
				return "?"
			}
			v = field
		case reflect.Map:
			value := v.MapIndex(reflect.ValueOf(part))
			if !value.IsValid() {
				return ""
			}
			v = value
		default:
			return "?"
		}
	}
	return p.formatValue(v)
}

func (p *ColumnProvider) getMapValue(obj any, mapField, key string) string {
	v := reflect.ValueOf(obj)
	if v.Kind() == reflect.Ptr {
		if v.IsNil() {
			return ""
		}
		v = v.Elem()
	}
	if v.Kind() != reflect.Struct {
		return ""
	}
	field := v.FieldByName(mapField)
	if !field.IsValid() || field.Kind() != reflect.Map {
		return ""
	}
	value := field.MapIndex(reflect.ValueOf(key))
	if !value.IsValid() {
		return ""
	}
	return p.formatValue(value)
}

func (p *ColumnProvider) formatValue(v reflect.Value) string {
	if formatter, ok := p.formatters[v.Type()]; ok {
		return formatter(v)
	}
	switch v.Kind() {
	case reflect.Ptr:
		if v.IsNil() {
			return ""
		}
		return p.formatValue(v.Elem())
	case reflect.String:
		return v.String()
	case reflect.Bool:
		return fmt.Sprintf("%t", v.Bool())
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return fmt.Sprintf("%d", v.Int())
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return fmt.Sprintf("%d", v.Uint())
	case reflect.Float32, reflect.Float64:
		return fmt.Sprintf("%g", v.Float())
	case reflect.Slice, reflect.Array:
		if v.Len() == 0 {
			return ""
		}
		parts := make([]string, v.Len())
		for i := 0; i < v.Len(); i++ {
			parts[i] = p.formatValue(v.Index(i))
		}
		return strings.Join(parts, ",")
	case reflect.Map:
		return fmt.Sprintf("%d", v.Len())
	default:
		if v.IsValid() && v.CanInterface() {
			return fmt.Sprintf("%v", v.Interface())
		}
		return ""
	}
}

// ColumnHeader converts a column path to a display header: CamelCase becomes
// HYPHEN-CASE and Labels.X / Annotations.X become LABEL:X / ANNOTATION:X.
func ColumnHeader(col string) string {
	if rest, ok := strings.CutPrefix(col, "Labels."); ok {
		return "LABEL:" + rest
	}
	if rest, ok := strings.CutPrefix(col, "Annotations."); ok {
		return "ANNOTATION:" + rest
	}
	col = col[strings.LastIndex(col, ".")+1:]
	var b strings.Builder
	for i, r := range col {
		if i > 0 && r >= 'A' && r <= 'Z' && col[i-1] >= 'a' && col[i-1] <= 'z' {
			b.WriteByte('-')
		}
		b.WriteRune(r)
	}
	return strings.ToUpper(b.String())
}

// TableOptions configures [RenderTable].
type TableOptions struct {
	// NoHeader suppresses the header row.
	NoHeader bool
	// Aliases and CustomColumns are passed to the column provider.
	Aliases       map[string]string
	CustomColumns map[string]func(any) string
}

// RenderTable writes entities as a column table to w. columns is the list of
// dotted field paths; if empty, defaultColumns is used. Headers derive from
// [ColumnHeader] unless opts.NoHeader is set.
func RenderTable[T any](w io.Writer, entities []*T, columns, defaultColumns []string, opts TableOptions) error {
	provider := NewColumnProvider(new(T))
	if opts.Aliases != nil {
		provider.WithAliases(opts.Aliases)
	}
	if opts.CustomColumns != nil {
		provider.WithCustomColumns(opts.CustomColumns)
	}

	cols := columns
	if len(cols) == 0 {
		cols = defaultColumns
	}
	trimmed := make([]string, len(cols))
	for i, c := range cols {
		trimmed[i] = strings.TrimSpace(c)
	}

	tw := tabwriter.NewWriter(w, 0, 2, 2, ' ', 0)
	if !opts.NoHeader {
		headers := make([]string, len(trimmed))
		for i, col := range trimmed {
			headers[i] = ColumnHeader(col)
		}
		if _, err := fmt.Fprintln(tw, strings.Join(headers, "\t")); err != nil {
			return err
		}
	}
	for _, entity := range entities {
		row := make([]string, len(trimmed))
		for i, col := range trimmed {
			row[i] = provider.GetValue(entity, col)
		}
		if _, err := fmt.Fprintln(tw, strings.Join(row, "\t")); err != nil {
			return err
		}
	}
	return tw.Flush()
}
