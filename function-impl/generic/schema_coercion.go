// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package generic

import (
	"encoding/json"
	"regexp"
	"strconv"
	"strings"

	"github.com/confighub/sdk/core/configkit/yamlkit"
	"github.com/confighub/sdk/core/third_party/gaby"
)

// Formats such as properties, INI and env files have no types, so every value in them is a
// string. A JSON Schema describing such a file may still declare a port an integer or a flag a
// boolean, meaning the text the application parses. As JSON Schema validators such as Ajv do
// with their coerceTypes option, a string the schema declares as an integer, a number or a
// boolean, and not as a string, is converted to that type before validation when its text is
// one: "8080" to 8080 and "true" to true. A string whose text isn't one stays a string and
// fails the type check, and a converted value is checked against the schema's other keywords,
// such as minimum.

// CoerceTextToSchemaTypes converts the strings in value, a document decoded from JSON, that
// schema, a decoded JSON Schema, declares as integers, numbers or booleans and not as strings,
// to those types, when their text is one. Integers and numbers become json.Numbers. The
// schemas that apply to a value are found through properties, patternProperties,
// additionalProperties, items, prefixItems and additionalItems, through allOf, anyOf and oneOf,
// and through $refs within the schema. value is changed in place, and the result is returned.
func CoerceTextToSchemaTypes(value any, schema any) any {
	c := schemaCoercer{root: schema}
	return c.coerce(value, c.expand(schema, nil))
}

type schemaCoercer struct {
	root any
}

// expand returns schema and the schemas it applies through allOf, anyOf, oneOf and $ref.
// visited holds the $refs being expanded, so that a recursive schema ends.
func (c *schemaCoercer) expand(schema any, visited map[string]bool) []map[string]any {
	object, ok := schema.(map[string]any)
	if !ok {
		return nil
	}
	result := []map[string]any{object}
	if ref, ok := object["$ref"].(string); ok && !visited[ref] {
		if target, ok := c.resolve(ref); ok {
			if visited == nil {
				visited = map[string]bool{}
			}
			visited[ref] = true
			result = append(result, c.expand(target, visited)...)
			delete(visited, ref)
		}
	}
	for _, keyword := range []string{"allOf", "anyOf", "oneOf"} {
		members, _ := object[keyword].([]any)
		for _, member := range members {
			result = append(result, c.expand(member, visited)...)
		}
	}
	return result
}

// resolve returns the subschema a $ref within the schema, such as #/$defs/port, points to.
func (c *schemaCoercer) resolve(ref string) (any, bool) {
	if !strings.HasPrefix(ref, "#") {
		return nil, false
	}
	current := c.root
	pointer := strings.TrimPrefix(ref, "#")
	if pointer == "" {
		return current, true
	}
	for _, token := range strings.Split(strings.TrimPrefix(pointer, "/"), "/") {
		token = strings.ReplaceAll(strings.ReplaceAll(token, "~1", "/"), "~0", "~")
		switch node := current.(type) {
		case map[string]any:
			next, ok := node[token]
			if !ok {
				return nil, false
			}
			current = next
		case []any:
			i, err := strconv.Atoi(token)
			if err != nil || i < 0 || i >= len(node) {
				return nil, false
			}
			current = node[i]
		default:
			return nil, false
		}
	}
	return current, true
}

func (c *schemaCoercer) coerce(value any, schemas []map[string]any) any {
	if len(schemas) == 0 {
		return value
	}
	switch v := value.(type) {
	case string:
		return coerceString(v, declaredTypes(schemas))
	case map[string]any:
		for key, child := range v {
			var childSchemas []map[string]any
			for _, schema := range schemas {
				for _, applied := range propertySchemas(schema, key) {
					childSchemas = append(childSchemas, c.expand(applied, nil)...)
				}
			}
			v[key] = c.coerce(child, childSchemas)
		}
		return v
	case []any:
		for i, element := range v {
			var elementSchemas []map[string]any
			for _, schema := range schemas {
				if applied := itemSchema(schema, i); applied != nil {
					elementSchemas = append(elementSchemas, c.expand(applied, nil)...)
				}
			}
			v[i] = c.coerce(element, elementSchemas)
		}
		return v
	}
	return value
}

// declaredTypes returns the types the schemas declare with the type keyword.
func declaredTypes(schemas []map[string]any) map[string]bool {
	types := map[string]bool{}
	for _, schema := range schemas {
		switch t := schema["type"].(type) {
		case string:
			types[t] = true
		case []any:
			for _, member := range t {
				if name, ok := member.(string); ok {
					types[name] = true
				}
			}
		}
	}
	return types
}

// coerceString converts text to a number if the schemas declare an integer or a number and its
// text is one, leaving the validator to decide whether it is an integer, and otherwise to a
// boolean if they declare one and its text is one, unless they declare it a string.
func coerceString(text string, types map[string]bool) any {
	if types["string"] {
		return text
	}
	if types["integer"] || types["number"] {
		if gaby.IsJSONNumber(text) {
			return json.Number(text)
		}
		// As an application parses it, such as +5 or 02134.
		if i, ok := yamlkit.TextAs[int](text); ok {
			return json.Number(strconv.Itoa(i))
		}
	}
	if types["boolean"] {
		if b, ok := yamlkit.TextAs[bool](text); ok {
			return b
		}
	}
	return text
}

// propertySchemas returns the subschemas of an object schema that apply to the property key.
func propertySchemas(schema map[string]any, key string) []any {
	var result []any
	matched := false
	if properties, ok := schema["properties"].(map[string]any); ok {
		if property, ok := properties[key]; ok {
			result = append(result, property)
			matched = true
		}
	}
	if patterns, ok := schema["patternProperties"].(map[string]any); ok {
		for pattern, property := range patterns {
			if re, err := regexp.Compile(pattern); err == nil && re.MatchString(key) {
				result = append(result, property)
				matched = true
			}
		}
	}
	if !matched {
		if additional, ok := schema["additionalProperties"].(map[string]any); ok {
			result = append(result, additional)
		}
	}
	return result
}

// itemSchema returns the subschema of an array schema that applies to the element at index i.
func itemSchema(schema map[string]any, i int) any {
	if prefix, ok := schema["prefixItems"].([]any); ok {
		if i < len(prefix) {
			return prefix[i]
		}
		return schema["items"]
	}
	switch items := schema["items"].(type) {
	case map[string]any:
		return items
	case []any:
		if i < len(items) {
			return items[i]
		}
		return schema["additionalItems"]
	}
	return nil
}
