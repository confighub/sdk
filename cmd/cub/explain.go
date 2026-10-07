// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"fmt"
	"strings"

	"github.com/confighub/sdk/core/openapi"
	"github.com/spf13/cobra"
)

// explainSchemas maps an entity's command ("cub link") to the OpenAPI schema its explain
// subcommand describes, which is also the schema input to its create and update is checked against.
var explainSchemas = map[*cobra.Command]string{}

// addExplainCmd registers an `explain [field]` subcommand on the given parent
// entity command, backed by the named OpenAPI schema. With no argument it
// summarizes every field; with a field path it shows the full description and
// any enum values from a referenced schema, and lists the fields of a field
// that holds objects.
func addExplainCmd(parent *cobra.Command, schemaName string) {
	explainSchemas[parent] = schemaName
	cmd := &cobra.Command{
		Use:   "explain [field]",
		Short: fmt.Sprintf("Explain %s fields from the OpenAPI spec", schemaName),
		Long: fmt.Sprintf(`Show documentation for the %s entity, sourced from the OpenAPI spec.

With no argument, lists every field with a one-line description.
With a field name, shows the full description, type, constraints, and any
enum values resolved through referenced schemas. When the field holds an
object, or a list or map of them, the fields of that object are listed too.

A field of an object is named by its path, with the names separated by dots.
A path goes through a list or a map without naming an element.`, schemaName),
		Example: fmt.Sprintf(`  %[1]s explain
  %[1]s explain Labels`, parent.CommandPath()),
		Args: cobra.MaximumNArgs(1),
		// Override the parent's PersistentPreRunE so explain works offline,
		// without requiring auth or a resolvable space.
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error { return nil },
		RunE: func(cmd *cobra.Command, args []string) error {
			return runExplain(parent.CommandPath(), schemaName, args)
		},
	}
	parent.AddCommand(cmd)
}

// explainCommandFor returns the explain command that describes schemaName, if one does.
func explainCommandFor(schemaName string) (string, bool) {
	for parent, name := range explainSchemas {
		if name == schemaName {
			return parent.CommandPath() + " explain", true
		}
	}
	return "", false
}

func runExplain(commandPath, schemaName string, args []string) error {
	schema, err := openapi.LookupSchema(schemaName)
	if err != nil {
		return err
	}
	if len(args) == 0 {
		printSchemaSummary(schemaName, schema)
		return nil
	}
	return printField(commandPath, schemaName, schema, args[0])
}

func printSchemaSummary(name string, s *openapi.Schema) {
	tprintRaw(fmt.Sprintf("Entity: %s", name))
	if s.Description != "" {
		tprintRaw("")
		tprintRaw(s.Description)
	}
	tprintRaw("")
	printFieldTable(s)
}

// printFieldTable lists the fields of an object schema, one line each.
func printFieldTable(s *openapi.Schema) {
	view := tableView()
	view.SetHeader([]string{"Field", "Type", "Description"})
	for _, fieldName := range s.PropertyNames() {
		f := s.Properties[fieldName]
		view.Append([]string{fieldName, schemaType(f), firstSentence(f.Description)})
	}
	view.Render()
}

// lookupFieldPath finds the field a dotted path names in an object schema. Each name but the
// last has to be a field holding an object, or a list or map of them, and the path goes on
// through the element without naming it.
func lookupFieldPath(schemaName string, s *openapi.Schema, fieldPath string) (*openapi.Schema, error) {
	names := strings.Split(fieldPath, ".")
	object, objectName := s, schemaName
	var field *openapi.Schema
	for i, name := range names {
		var ok bool
		field, ok = object.Properties[name]
		if !ok {
			return nil, fmt.Errorf("%s has no field %q%s", objectName, name, suggestField(object, name))
		}
		if i == len(names)-1 {
			break
		}
		elementName, element := elementSchema(field)
		if element == nil || len(element.Properties) == 0 {
			return nil, fmt.Errorf("%s holds %s, which has no fields, so it has no field %q",
				strings.Join(names[:i+1], "."), schemaType(field), names[i+1])
		}
		object, objectName = element, elementName
		if objectName == "" {
			objectName = strings.Join(names[:i+1], ".")
		}
	}
	return field, nil
}

func printField(commandPath, schemaName string, s *openapi.Schema, fieldPath string) error {
	field, err := lookupFieldPath(schemaName, s, fieldPath)
	if err != nil {
		return fmt.Errorf("%w (run `%s explain` to list fields)", err, commandPath)
	}

	view := tableView()
	view.Append([]string{"Entity", schemaName})
	view.Append([]string{"Field", fieldPath})
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

	elementName, element := elementSchema(field)
	if element != nil && elementName != "" {
		if len(element.Enum) > 0 {
			view.Append([]string{elementName + " values", strings.Join(element.Enum, ", ")})
		}
		if element.Description != "" {
			view.Append([]string{elementName + " description", element.Description})
		}
	}
	view.Render()

	if element != nil && len(element.Properties) > 0 {
		heading := "Fields"
		if elementName != "" {
			heading = "Fields of " + elementName
		}
		tprintRaw("")
		tprintRaw(fmt.Sprintf("%s (explain one with `%s explain %s.<field>`):", heading, commandPath, fieldPath))
		tprintRaw("")
		printFieldTable(element)
	}
	return nil
}

// elementSchema returns what a field holds once any list or map around it is taken away,
// resolved, and the name of the schema it is when it is a named one.
func elementSchema(field *openapi.Schema) (string, *openapi.Schema) {
	for depth := 0; field != nil && depth < 8; depth++ {
		name := field.RefName()
		resolved := openapi.Resolve(field)
		if resolved == nil {
			return name, nil
		}
		switch {
		case resolved.Type == "array" && resolved.Items != nil:
			field = resolved.Items
		case len(resolved.Properties) == 0 && resolved.AdditionalProperties != nil:
			field = resolved.AdditionalProperties
		default:
			return name, resolved
		}
	}
	return "", nil
}

// schemaType returns a human-readable type label for a property schema,
// surfacing array element types and $ref targets. A reference to a schema that
// only names a list or a map, such as BindingList, is shown as the list or map
// it is, since its name says less than its shape.
func schemaType(s *openapi.Schema) string {
	if s == nil {
		return ""
	}
	if ref := s.RefName(); ref != "" {
		resolved := openapi.Resolve(s)
		if resolved == nil || len(resolved.Properties) > 0 || len(resolved.Enum) > 0 {
			return ref
		}
		if resolved.Type == "array" || resolved.AdditionalProperties != nil {
			return schemaType(resolved)
		}
		return ref
	}
	switch s.Type {
	case "array":
		if s.Items != nil {
			if item := schemaType(s.Items); item != "" {
				return "[]" + item
			}
		}
		return "array"
	case "object":
		if s.AdditionalProperties != nil {
			if value := schemaType(s.AdditionalProperties); value != "" {
				return "map[string]" + value
			}
		}
		return "object"
	}
	return s.Type
}

// suggestField returns `; did you mean "X"?` when an object schema has a field whose name is
// close to name, or "" when it has none.
func suggestField(object *openapi.Schema, name string) string {
	best, bestDistance := "", 3
	for _, candidate := range object.PropertyNames() {
		if strings.EqualFold(candidate, name) {
			return fmt.Sprintf("; did you mean %q?", candidate)
		}
		if d := editDistance(strings.ToLower(candidate), strings.ToLower(name)); d < bestDistance {
			best, bestDistance = candidate, d
		}
	}
	if best == "" {
		return ""
	}
	return fmt.Sprintf("; did you mean %q?", best)
}

// editDistance is the Levenshtein distance between a and b.
func editDistance(a, b string) int {
	previous := make([]int, len(b)+1)
	current := make([]int, len(b)+1)
	for j := range previous {
		previous[j] = j
	}
	for i := 1; i <= len(a); i++ {
		current[0] = i
		for j := 1; j <= len(b); j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			current[j] = min(previous[j]+1, current[j-1]+1, previous[j-1]+cost)
		}
		previous, current = current, previous
	}
	return previous[len(b)]
}

func firstSentence(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	// A sentence ends at a period followed by whitespace, not at the dot of a path or a name.
	for i, r := range s {
		if r == '\n' && i > 0 {
			return strings.TrimSuffix(s[:i], ".")
		}
		if r == '.' && i > 0 && (i+1 == len(s) || s[i+1] == ' ' || s[i+1] == '\n') {
			return s[:i]
		}
	}
	return s
}
