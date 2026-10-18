// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"fmt"
	"sort"
	"strings"

	"github.com/confighub/sdk/core/configkit/cubkit"
	"github.com/confighub/sdk/core/entityfilter"
	"github.com/confighub/sdk/core/function/api"
	"github.com/confighub/sdk/core/openapi"
)

// whereEntityTypes names the entity type a where expression selects for an explain schema whose
// name differs from it. The API renamed QueuedOperation to UnitAction, while the filter, and the
// attribute table the spec publishes, kept the model's name.
var whereEntityTypes = map[string]string{"UnitAction": "QueuedOperation"}

// whereEntityType returns the entity type of the where attribute table that a where expression
// over schemaName's entities names attributes of.
func whereEntityType(schemaName string) string {
	if entityType, ok := whereEntityTypes[schemaName]; ok {
		return entityType
	}
	return schemaName
}

// whereAttributesFor returns what a where expression over schemaName's entities can name, from the
// table the API's OpenAPI spec publishes, which is the one the server checks expressions against.
func whereAttributesFor(schemaName string) (map[string]entityfilter.Attribute, error) {
	attributes, ok := cubkit.WhereAttributes()[whereEntityType(schemaName)]
	if !ok || len(attributes) == 0 {
		return nil, fmt.Errorf("%s entities are not selected by where expressions", schemaName)
	}
	return attributes, nil
}

// whereReferenceIsArrayValued reports whether a reference names several entities, so that an
// expression reaches their attributes through a `*` segment, as in FromLink.*.Slug: an array of
// ids, or a map keyed by them.
func whereReferenceIsArrayValued(attribute entityfilter.Attribute) bool {
	return attribute.DataType == api.DataTypeUUIDArray || attribute.DataType == api.DataTypeUUIDStringMap
}

// whereAttributeForms returns how an expression writes an attribute: by its name, by key for a
// keyed map, by path into a JSON document, and, for a reference the entity's endpoints expand,
// as a prefix to an attribute of the entity it names.
func whereAttributeForms(name string, attribute entityfilter.Attribute) []string {
	var forms []string
	switch {
	case attribute.DataType == api.DataTypeStringStringUUIDBoolMap:
		// Permissions is read by action, and then the users that action names.
		forms = append(forms, name+".<action>.UserIDs")
	case entityfilter.DataTypeIsKeyedMap(attribute.DataType):
		forms = append(forms, name+".<key>")
	case attribute.DataType == api.DataTypeJSON:
		forms = append(forms, name, name+".<path>")
	default:
		forms = append(forms, name)
	}
	if attribute.Expandable {
		prefix := entityfilter.FieldNameToEntityName(name)
		if whereReferenceIsArrayValued(attribute) {
			forms = append(forms, prefix+".*.<attribute>")
		} else {
			forms = append(forms, prefix+".<attribute>")
		}
	}
	return forms
}

// whereReferenceExplain returns the command that lists the attributes of the entity type a
// reference names, or "" when no explain command describes it.
func whereReferenceExplain(entityType string) string {
	for schemaName, whereType := range whereEntityTypes {
		if whereType == entityType {
			entityType = schemaName
		}
	}
	command, ok := explainCommandFor(entityType)
	if !ok {
		return ""
	}
	return command + " --where"
}

func runExplainWhere(commandPath, schemaName string, args []string) error {
	attributes, err := whereAttributesFor(schemaName)
	if err != nil {
		return err
	}
	if len(args) == 1 {
		return printWhereAttribute(commandPath, schemaName, attributes, args[0])
	}

	// A deprecated name is still accepted, but listing it beside the attribute it stands for
	// invites using it, so it is only named below the table.
	names := make([]string, 0, len(attributes))
	var deprecated []string
	for name, attribute := range attributes {
		if attribute.DeprecatedAliasOf != "" {
			deprecated = append(deprecated, fmt.Sprintf("%s (use %s)", name, attribute.DeprecatedAliasOf))
			continue
		}
		names = append(names, name)
	}
	sort.Strings(names)
	sort.Strings(deprecated)

	tprintRaw(fmt.Sprintf("Entity: %s", schemaName))
	tprintRaw("")
	tprintRaw(fmt.Sprintf("The attributes a --where expression over %s entities can name. A name written with a", schemaName))
	tprintRaw("prefix, as in Space.<attribute>, names an attribute of the entity a reference names, which")
	tprintRaw("that entity's `explain --where` lists. The operators each type takes are in the \"Where")
	tprintRaw("expressions\" section of `cub --help-overview`.")
	tprintRaw("")
	view := tableView()
	view.SetHeader([]string{"Attribute", "Type", "Written as", "References"})
	for _, name := range names {
		attribute := attributes[name]
		view.Append([]string{name, string(attribute.DataType), strings.Join(whereAttributeForms(name, attribute), ", "), attribute.References})
	}
	view.Render()
	if len(deprecated) > 0 {
		tprintRaw("")
		tprintRaw("Deprecated names, still accepted: " + strings.Join(deprecated, ", "))
	}
	return nil
}

func printWhereAttribute(commandPath, schemaName string, attributes map[string]entityfilter.Attribute, name string) error {
	attribute, ok := attributes[name]
	if !ok {
		best, bestDistance := "", 3
		for candidate := range attributes {
			if d := editDistance(strings.ToLower(candidate), strings.ToLower(name)); d < bestDistance ||
				(d == bestDistance && best != "" && candidate < best) {
				best, bestDistance = candidate, d
			}
		}
		suggestion := ""
		if best != "" {
			suggestion = fmt.Sprintf("; did you mean %q?", best)
		}
		return fmt.Errorf("a where expression over %s entities cannot name %q%s (run `%s explain --where` to list the attributes)",
			schemaName, name, suggestion, commandPath)
	}

	view := tableView()
	view.Append([]string{"Entity", schemaName})
	view.Append([]string{"Attribute", name})
	view.Append([]string{"Type", string(attribute.DataType)})
	view.Append([]string{"Written as", strings.Join(whereAttributeForms(name, attribute), ", ")})
	if attribute.DeprecatedAliasOf != "" {
		view.Append([]string{"Deprecated", "an alias of " + attribute.DeprecatedAliasOf + ", which expressions should name instead"})
	}
	if attribute.References != "" {
		view.Append([]string{"References", attribute.References})
		if attribute.Expandable {
			if command := whereReferenceExplain(attribute.References); command != "" {
				view.Append([]string{"Attributes of " + attribute.References, command})
			}
		}
	}
	if schema, err := openapi.LookupSchema(schemaName); err == nil {
		if field, ok := schema.Properties[name]; ok && field.Description != "" {
			view.Append([]string{"Description", field.Description})
		}
	}
	view.Render()
	return nil
}
