// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/cockroachdb/errors"
	"github.com/confighub/sdk/core/openapi"
)

// checkEntityInput checks an entity's fields read from --from-stdin or --filename, parsed into
// a generic document, against the entity's OpenAPI schema, before they are decoded into the
// entity or sent as a patch. It reports every problem it finds, each with its path:
//
//   - A field the schema does not have. Decoding into the entity refuses one too, but without
//     saying where it is, and the server applies a merge patch without one.
//   - A field only the server sets, which a write ignores. Input that names the entity's own ID
//     is an entity read from the server, so its server-set fields are expected and pass.
//
// A schema name the spec does not have checks nothing.
func checkEntityInput(schemaName string, document any) error {
	checker := &entityInputChecker{}
	if fields, ok := document.(map[string]any); ok {
		if id, ok := fields[schemaName+"ID"].(string); ok && id != "" {
			checker.readFromServer = true
		}
	}
	if schema, err := openapi.LookupSchema(schemaName); err == nil {
		checker.check("", schemaName, schema, document)
	}
	if len(checker.problems) == 0 {
		return nil
	}

	message := fmt.Sprintf("the %s input has fields that would not be written:\n  %s", schemaName,
		strings.Join(checker.problems, "\n  "))
	if command, ok := explainCommandFor(schemaName); ok {
		message += fmt.Sprintf("\nRun `%s` to list the fields, and `%s <field>` to describe one and list its fields", command, command)
	}
	return errors.New(message)
}

type entityInputChecker struct {
	readFromServer bool
	problems       []string
}

// check checks value, found at path, against s. objectName names the object schema s is when
// it is a named one, for the messages.
func (c *entityInputChecker) check(path, objectName string, s *openapi.Schema, value any) {
	if name := s.RefName(); name != "" {
		objectName = name
	}
	s = openapi.Resolve(s)
	if s == nil || len(s.AnyOf) > 0 || len(s.OneOf) > 0 || len(s.AllOf) > 0 {
		return
	}
	switch v := value.(type) {
	case map[string]any:
		keys := make([]string, 0, len(v))
		for key := range v {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			fieldPath := joinInputPath(path, key)
			switch {
			case len(s.Properties) > 0:
				field, ok := s.Properties[key]
				if !ok {
					c.problems = append(c.problems, fmt.Sprintf("%s: %s has no field %q%s", fieldPath, objectName, key, suggestField(s, key)))
					continue
				}
				if field.ReadOnly && !c.readFromServer {
					c.problems = append(c.problems, fmt.Sprintf("%s: set by the server, so a write ignores it", fieldPath))
					continue
				}
				c.check(fieldPath, "", field, v[key])
			case s.AdditionalProperties != nil:
				c.check(fieldPath, "", s.AdditionalProperties, v[key])
			}
		}
	case []any:
		if s.Items == nil {
			return
		}
		for i, element := range v {
			c.check(joinInputPath(path, strconv.Itoa(i)), "", s.Items, element)
		}
	}
}

// joinInputPath appends a field name or list position to a path in ConfigHub's path syntax,
// in which a dot inside a name is written ~1.
func joinInputPath(path, name string) string {
	name = strings.ReplaceAll(name, ".", "~1")
	if path == "" {
		return name
	}
	return path + "." + name
}
