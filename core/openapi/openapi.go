// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

// Package openapi exposes the embedded OpenAPI spec for runtime documentation
// lookups, e.g., the `cub <entity> explain` commands.
package openapi

import (
	_ "embed"
	"fmt"
	"sort"
	"strings"
	"sync"

	"gopkg.in/yaml.v3"
)

//go:embed openapi.yaml
var specYAML []byte

// Schema is a minimal projection of an OpenAPI schema, sufficient for surfacing
// human-readable property documentation.
type Schema struct {
	Description          string             `yaml:"description"`
	Type                 string             `yaml:"type"`
	Format               string             `yaml:"format"`
	Enum                 []string           `yaml:"enum"`
	Properties           map[string]*Schema `yaml:"properties"`
	Items                *Schema            `yaml:"items"`
	AdditionalProperties *Schema            `yaml:"additionalProperties"`
	Ref                  string             `yaml:"$ref"`
	AllOf                []*Schema          `yaml:"allOf"`
	AnyOf                []*Schema          `yaml:"anyOf"`
	OneOf                []*Schema          `yaml:"oneOf"`
	ReadOnly             bool               `yaml:"readOnly"`
	Nullable             bool               `yaml:"nullable"`
	Required             []string           `yaml:"required"`
	Pattern              string             `yaml:"pattern"`
	Example              any                `yaml:"example"`
}

type spec struct {
	Components struct {
		Schemas map[string]*Schema `yaml:"schemas"`
	} `yaml:"components"`
}

var (
	once     sync.Once
	schemas  map[string]*Schema
	parseErr error
)

func load() (map[string]*Schema, error) {
	once.Do(func() {
		var s spec
		if err := yaml.Unmarshal(specYAML, &s); err != nil {
			parseErr = err
			return
		}
		schemas = s.Components.Schemas
	})
	return schemas, parseErr
}

// LookupSchema returns the named top-level schema from the embedded OpenAPI
// spec. The error indicates either a parse failure or an unknown schema name.
func LookupSchema(name string) (*Schema, error) {
	all, err := load()
	if err != nil {
		return nil, fmt.Errorf("parse OpenAPI spec: %w", err)
	}
	s, ok := all[name]
	if !ok {
		return nil, fmt.Errorf("schema %q not found in OpenAPI spec", name)
	}
	return s, nil
}

// PropertyNames returns the names of a schema's properties in sorted order.
func (s *Schema) PropertyNames() []string {
	if s == nil {
		return nil
	}
	names := make([]string, 0, len(s.Properties))
	for n := range s.Properties {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

// RefName returns the last segment of a $ref pointer (e.g.,
// "#/components/schemas/UpdateType" -> "UpdateType"), or "" if the schema is
// not a reference. A property that references a schema and also has a
// description or readOnly of its own is written as an allOf of that one $ref,
// since siblings of a $ref are ignored, so that form names the schema too.
func (s *Schema) RefName() string {
	if s == nil {
		return ""
	}
	if s.Ref == "" {
		if len(s.AllOf) == 1 {
			return s.AllOf[0].RefName()
		}
		return ""
	}
	if i := strings.LastIndex(s.Ref, "/"); i >= 0 {
		return s.Ref[i+1:]
	}
	return s.Ref
}

// Resolve returns the schema s stands for: the one its $ref names, or its
// allOf's one member names, followed until it is neither. A reference to a
// schema the spec does not have resolves to nil. The result is shared, and must
// not be modified.
func Resolve(s *Schema) *Schema {
	for depth := 0; s != nil && depth < 32; depth++ {
		switch {
		case s.Ref != "":
			next, err := LookupSchema(s.RefName())
			if err != nil {
				return nil
			}
			s = next
		case len(s.AllOf) == 1:
			s = s.AllOf[0]
		default:
			return s
		}
	}
	return s
}
