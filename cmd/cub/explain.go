// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"fmt"
	"strings"

	"github.com/confighub/sdk/core/openapi"
	"github.com/spf13/cobra"
)

// addExplainCmd registers an `explain [field]` subcommand on the given parent
// entity command, backed by the named OpenAPI schema. With no argument it
// summarizes every field; with a field name it shows the full description and
// any enum values from a referenced schema.
func addExplainCmd(parent *cobra.Command, schemaName string) {
	cmd := &cobra.Command{
		Use:   "explain [field]",
		Short: fmt.Sprintf("Explain %s fields from the OpenAPI spec", schemaName),
		Long: fmt.Sprintf(`Show documentation for the %s entity, sourced from the OpenAPI spec.

With no argument, lists every field with a one-line description.
With a field name, shows the full description, type, constraints, and any
enum values resolved through referenced schemas.`, schemaName),
		Args: cobra.MaximumNArgs(1),
		// Override the parent's PersistentPreRunE so explain works offline,
		// without requiring auth or a resolvable space.
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error { return nil },
		RunE: func(cmd *cobra.Command, args []string) error {
			return runExplain(schemaName, args)
		},
	}
	parent.AddCommand(cmd)
}

func runExplain(schemaName string, args []string) error {
	schema, err := openapi.LookupSchema(schemaName)
	if err != nil {
		return err
	}
	if len(args) == 0 {
		printSchemaSummary(schemaName, schema)
		return nil
	}
	return printField(schemaName, schema, args[0])
}

func printSchemaSummary(name string, s *openapi.Schema) {
	tprintRaw(fmt.Sprintf("Entity: %s", name))
	if s.Description != "" {
		tprintRaw("")
		tprintRaw(s.Description)
	}
	tprintRaw("")
	view := tableView()
	view.SetHeader([]string{"Field", "Type", "Description"})
	for _, fieldName := range s.PropertyNames() {
		f := s.Properties[fieldName]
		view.Append([]string{fieldName, schemaType(f), firstSentence(f.Description)})
	}
	view.Render()
}

func printField(schemaName string, s *openapi.Schema, fieldName string) error {
	field, ok := s.Properties[fieldName]
	if !ok {
		return fmt.Errorf("%s has no field %q (run `cub %s explain` to list fields)",
			schemaName, fieldName, strings.ToLower(schemaName))
	}

	view := tableView()
	view.Append([]string{"Entity", schemaName})
	view.Append([]string{"Field", fieldName})
	view.Append([]string{"Type", schemaType(field)})
	if field.Format != "" {
		view.Append([]string{"Format", field.Format})
	}
	if field.ReadOnly {
		view.Append([]string{"Read-only", "true"})
	}
	if field.Nullable {
		view.Append([]string{"Nullable", "true"})
	}
	if field.Pattern != "" {
		view.Append([]string{"Pattern", field.Pattern})
	}
	if len(field.Enum) > 0 {
		view.Append([]string{"Values", strings.Join(field.Enum, ", ")})
	}
	if field.Description != "" {
		view.Append([]string{"Description", field.Description})
	}

	if ref := refTarget(field); ref != "" {
		if refSchema, err := openapi.LookupSchema(ref); err == nil {
			if len(refSchema.Enum) > 0 {
				view.Append([]string{ref + " values", strings.Join(refSchema.Enum, ", ")})
			}
			if refSchema.Description != "" {
				view.Append([]string{ref + " description", refSchema.Description})
			}
		}
	}
	view.Render()
	return nil
}

// schemaType returns a human-readable type label for a property schema,
// surfacing array element types and $ref targets.
func schemaType(s *openapi.Schema) string {
	if s == nil {
		return ""
	}
	if ref := s.RefName(); ref != "" {
		return ref
	}
	switch s.Type {
	case "array":
		if s.Items != nil {
			if ref := s.Items.RefName(); ref != "" {
				return "[]" + ref
			}
			if s.Items.Type != "" {
				return "[]" + s.Items.Type
			}
		}
		return "array"
	case "object":
		if s.AdditionalProperties != nil {
			if ref := s.AdditionalProperties.RefName(); ref != "" {
				return "map[string]" + ref
			}
			if s.AdditionalProperties.Type != "" {
				return "map[string]" + s.AdditionalProperties.Type
			}
		}
		return "object"
	}
	return s.Type
}

// refTarget returns the referenced schema name when a field is a direct $ref,
// or when it's an array of $ref items.
func refTarget(s *openapi.Schema) string {
	if s == nil {
		return ""
	}
	if ref := s.RefName(); ref != "" {
		return ref
	}
	if s.Type == "array" && s.Items != nil {
		return s.Items.RefName()
	}
	return ""
}

func firstSentence(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	if i := strings.IndexAny(s, ".\n"); i > 0 {
		return s[:i]
	}
	return s
}
