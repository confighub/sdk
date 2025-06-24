// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package kubernetes

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/cockroachdb/errors"
	"github.com/confighub/sdk/third_party/gaby"
	k8sschema "github.com/confighub/sdk/third_party/kubernetes"
	openapi_v2 "github.com/google/gnostic/openapiv2"
	"github.com/labstack/gommon/log"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/kube-openapi/pkg/util/proto"
	"k8s.io/kubectl/pkg/util/openapi"
)

var schemaFinder *SchemaFinder

func InitSchemaFinder() error {
	var err error
	schemaFinder, err = NewSchemaFinder()
	return err
}

type SchemaFinder struct {
	resources openapi.Resources
}

type SchemaInfo struct {
	Description string
}

func NewSchemaFinder() (*SchemaFinder, error) {
	data := k8sschema.K8sSchema

	document, err := openapi_v2.ParseDocument(data)
	if err != nil {
		return nil, fmt.Errorf("failed to parse OpenAPI document: %w", err)
	}

	resources, err := openapi.NewOpenAPIData(document)
	if err != nil {
		return nil, fmt.Errorf("failed to create OpenAPI resources: %w", err)
	}

	return &SchemaFinder{resources: resources}, nil
}

func LookupPath(gvkString, fieldPath string) (*SchemaInfo, error) {
	if schemaFinder == nil {
		return nil, errors.New("no schemas loaded")
	}
	return schemaFinder.LookupPath(gvkString, fieldPath)
}

func (e *SchemaFinder) LookupPath(gvkString, fieldPath string) (*SchemaInfo, error) {
	if gvkString == "*" {
		gvkString = "v1/Pod"
	}
	gvkSlice := strings.Split(gvkString, "/")
	var gvk schema.GroupVersionKind
	switch len(gvkSlice) {
	case 3:
		gvk = schema.GroupVersionKind{
			Group:   gvkSlice[0],
			Version: gvkSlice[1],
			Kind:    gvkSlice[2],
		}
	case 2:
		gvk = schema.GroupVersionKind{
			Group:   "",
			Version: gvkSlice[0],
			Kind:    gvkSlice[1],
		}
	default:
		return nil, errors.New("gvk should be of the form apps/v1/Deployment")
	}

	resource := e.resources.LookupResource(gvk)
	if resource == nil {
		return nil, fmt.Errorf("couldn't find resource for %q", gvk)
	}

	s, err := e.lookupField(resource, fieldPath)
	if err != nil {
		return nil, err
	}

	return e.getSchemaInfo(s), nil
}

func (e *SchemaFinder) getSchemaInfo(s proto.Schema) *SchemaInfo {
	return &SchemaInfo{
		Description: s.GetDescription(),
	}
}

var integerLiteralOnlyRegexpString = "^[0-9][0-9]{0,9}$"
var integerLiteralOnlyRegexp = regexp.MustCompile(integerLiteralOnlyRegexpString)

func (e *SchemaFinder) lookupField(resourceSchema proto.Schema, fieldPath string) (proto.Schema, error) {
	currentSchema := resourceSchema
	fields := gaby.DotPathToSlice(fieldPath)

	// TODO: If we hit a deadend, just return what we have, because we may have delved into a map or array.
	for len(fields) != 0 {
		field := fields[0]
		// Chop off embedded accessors
		field = strings.Split(field, "#")[0]

		switch t := currentSchema.(type) {
		case *proto.Kind:
			if sub, exists := t.Fields[field]; exists {
				currentSchema = sub
			} else {
				return nil, fmt.Errorf("field %q not found in kind %q", field, t.GetPath().String())
			}
			fields = fields[1:]

		case *proto.Map:
			// For maps, we continue with the value type
			currentSchema = t.SubType
			fields = fields[1:]

		case *proto.Array:
			// If we're looking up a field in an array, we want to look it up in the array's subtype
			currentSchema = t.SubType
			if !integerLiteralOnlyRegexp.MatchString(field) && !strings.ContainsAny(field, "*?") {
				log.Errorf("field %s was expected to be an array index", field)
			}
			fields = fields[1:]

		case *proto.Ref:
			// For references, resolve and retry the current field
			resolved := t.SubSchema()
			if resolved == nil {
				return nil, fmt.Errorf("failed to resolve reference %q", t.Reference())
			}
			currentSchema = resolved
			// Don't consume the field, retry it on the resolved type

		case *proto.Primitive:
			return nil, fmt.Errorf("cannot lookup field %q in primitive type %q", field, t.Type)

		default:
			return nil, fmt.Errorf("unsupported schema type %T", t)
		}
	}

	return currentSchema, nil
}

// These functions are for debugging

func formatDescription(fieldSchema proto.Schema) string {
	var b strings.Builder
	fmt.Fprintf(&b, "KIND:     %s\n", fieldSchema.GetPath().String())

	if desc := fieldSchema.GetDescription(); desc != "" {
		fmt.Fprintf(&b, "DESCRIPTION:\n%s\n", indentLines(desc, "    "))
	}

	printField(&b, fieldSchema)

	return b.String()
}

func printField(b *strings.Builder, fieldSchema proto.Schema) {
	switch t := fieldSchema.(type) {
	case *proto.Kind:
		printFields(b, t)
	case *proto.Array:
		printArray(b, t)
	case *proto.Primitive:
		printPrimitive(b, t)
	case *proto.Map:
		printMap(b, t)
	case *proto.Ref:
		// printReference(b, t)
		printField(b, t.SubSchema())
	}
}

func printFields(b *strings.Builder, kind *proto.Kind) {
	if len(kind.Fields) > 0 {
		fmt.Fprintf(b, "\nFIELDS:\n")
		for _, name := range kind.Keys() {
			field := kind.Fields[name]
			fmt.Fprintf(b, "   %s\t%s\n", name, typeString(field))
			if desc := field.GetDescription(); desc != "" {
				fmt.Fprintf(b, "      %s\n", indentLines(desc, "      "))
			}
		}
	}
}

func typeString(s proto.Schema) string {
	switch t := s.(type) {
	case *proto.Array:
		return "[]" + typeString(t.SubType)
	case *proto.Primitive:
		return t.Type
	case *proto.Kind:
		return "Object"
	case *proto.Map:
		return fmt.Sprintf("map[string]%s", typeString(t.SubType))
	case *proto.Ref:
		return t.Reference()
	}
	return "<unknown>"
}

func indentLines(s, indent string) string {
	return indent + strings.ReplaceAll(strings.TrimSpace(s), "\n", "\n"+indent)
}

func printArray(b *strings.Builder, array *proto.Array) {
	fmt.Fprintf(b, "\nARRAY TYPE:\n")
	fmt.Fprintf(b, "   []%s\n", typeString(array.SubType))

	if desc := array.SubType.GetDescription(); desc != "" {
		fmt.Fprintf(b, "   %s\n", indentLines(desc, "   "))
	}

	// Recursively show array item structure
	b.WriteString("\nARRAY ITEM STRUCTURE:\n")
	b.WriteString(indentLines(formatDescription(array.SubType), "   "))
}

func printMap(b *strings.Builder, m *proto.Map) {
	fmt.Fprintf(b, "\nMAP TYPE:\n")
	fmt.Fprintf(b, "   map[string]%s\n", typeString(m.SubType))

	if desc := m.SubType.GetDescription(); desc != "" {
		fmt.Fprintf(b, "   %s\n", indentLines(desc, "   "))
	}

	// Recursively show map value structure
	b.WriteString("\nMAP VALUE STRUCTURE:\n")
	b.WriteString(indentLines(formatDescription(m.SubType), "   "))
}

func printReference(b *strings.Builder, m *proto.Ref) {
	fmt.Fprintf(b, "\nREFERENCE:\n")
}

func printPrimitive(b *strings.Builder, p *proto.Primitive) {
	fmt.Fprintf(b, "\nTYPE:\n   %s", p.Type)
	if p.Format != "" {
		fmt.Fprintf(b, " (%s)", p.Format)
	}
	b.WriteString("\n")

	if p.GetDefault() != nil {
		fmt.Fprintf(b, "DEFAULT:\n   %v\n", p.GetDefault())
	}
}
