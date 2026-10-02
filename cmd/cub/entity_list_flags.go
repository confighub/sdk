// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"encoding/json"
	"fmt"
	"strings"
	"unicode"

	"github.com/cockroachdb/errors"
	"github.com/spf13/cobra"
)

// A list of objects of an entity, such as a View's Columns, is set by a family of flags: one
// that names the elements, and one for each field of an element, whose value is
// <element>=<value>. They are shorthand for --set, so they make the same edits by the same
// means; what they add is that each field is in the command's help under its own name.

// listFlagsSpec says which list a family of flags sets.
type listFlagsSpec struct {
	// entityType and list name the list: the entity type and the list's path in its document.
	entityType, list string
	// key is the field that identifies an element, the list's merge key.
	key string
	// element is the name of the flag that names the elements, which the field flags are
	// prefixed with.
	element string
	// elements holds the element flag's values when the command reads that flag itself; nil has
	// the family register it.
	elements *[]string
	// primary, if set, is a field the element flag sets along with the key, as
	// <key>=<value>, for an element that is little more than the two.
	primary string
	// noun names an element in help text, as in "column".
	noun string
	// newMember makes the element for a key the list does not hold; nil makes one of the key
	// alone.
	newMember func(key string) (map[string]any, error)
}

type listFieldFlag struct {
	field, flag string
	values      []string
}

// listFlags is a family of flags registered on one command.
type listFlags struct {
	spec     listFlagsSpec
	elements *[]string
	fields   []*listFieldFlag
}

// kebabCase writes a field name as a flag does: OrderByDirection is order-by-direction and
// FromUserIDs is from-user-ids.
func kebabCase(name string) string {
	var out strings.Builder
	runes := []rune(name)
	for i, r := range runes {
		if unicode.IsUpper(r) && i > 0 {
			previous := runes[i-1]
			nextIsLower := i+1 < len(runes) && unicode.IsLower(runes[i+1])
			// A plural's s does not start a word: the Ds of IDs stay together.
			pluralS := i+1 < len(runes) && runes[i+1] == 's' && (i+2 == len(runes) || unicode.IsUpper(runes[i+2]))
			if unicode.IsLower(previous) || unicode.IsDigit(previous) || (nextIsLower && !pluralS) {
				out.WriteByte('-')
			}
		}
		out.WriteRune(unicode.ToLower(r))
	}
	return out.String()
}

// addListFlags registers the family of flags for a list on a command: the element flag, unless
// the command has its own, and a flag for each field of an element that holds a plain value or a
// list of them. A field that is itself an object is left to --set.
func addListFlags(cmd *cobra.Command, spec listFlagsSpec) *listFlags {
	flags := &listFlags{spec: spec, elements: spec.elements}
	if flags.elements == nil {
		flags.elements = new([]string)
		form := "<" + spec.key + ">"
		if spec.primary != "" {
			form += "=<" + spec.primary + ">"
		}
		cmd.Flags().StringArrayVar(flags.elements, spec.element, nil,
			fmt.Sprintf("one %s of the %s, as %s (repeatable). On an update the ones named are the ones it has, in that order: "+
				"one it had before keeps its other fields", spec.noun, spec.entityType, kebabForm(form)))
	}

	root, schema, err := fieldSchema(spec.entityType, []pathSegment{{name: spec.list}})
	if err != nil {
		panic(fmt.Sprintf("%s flags of %s: %v", spec.element, spec.entityType, err))
	}
	items, _ := schema["items"].(map[string]any)
	element := derefSchema(root, items)
	properties, _ := element["properties"].(map[string]any)
	if _, ok := properties[spec.key]; !ok {
		panic(fmt.Sprintf("%s flags of %s: an element of %s has no %s", spec.element, spec.entityType, spec.list, spec.key))
	}
	for _, field := range schemaPropertyNames(element) {
		if field == spec.key || field == spec.primary {
			continue
		}
		property := derefSchema(root, properties[field].(map[string]any))
		form := "<value>"
		switch jsonSchemaType(property) {
		case "object":
			continue
		case "array":
			itemSchema, _ := property["items"].(map[string]any)
			if itemType := jsonSchemaType(derefSchema(root, itemSchema)); itemType == "object" || itemType == "array" {
				continue
			}
			form = "<value>[;<value>...]"
		case "boolean":
			form = "true|false"
		}
		name := spec.element + "-" + kebabCase(strings.TrimPrefix(field, strings.ToUpper(spec.noun[:1])+spec.noun[1:]))
		if cmd.Flags().Lookup(name) != nil {
			continue
		}
		description, _ := properties[field].(map[string]any)["description"].(string)
		if description == "" {
			description, _ = property["description"].(string)
		}
		// The first sentence says what the field is; the rest is in the API reference.
		if sentence, _, found := strings.Cut(description, ". "); found {
			description = sentence
		}
		usage := fmt.Sprintf("%s of one %s, as <%s>=%s (repeatable; <%s>=- clears it)",
			field, spec.noun, kebabCase(spec.key), form, kebabCase(spec.key))
		if description != "" {
			usage += ". " + strings.TrimSuffix(description, ".")
		}
		fieldFlag := &listFieldFlag{field: field, flag: name}
		cmd.Flags().StringArrayVar(&fieldFlag.values, name, nil, usage)
		flags.fields = append(flags.fields, fieldFlag)
	}
	return flags
}

// kebabForm writes the placeholders of a form in flag style: <Name>=<Expression> is
// <name>=<expression>.
func kebabForm(form string) string {
	var out strings.Builder
	for _, part := range strings.SplitAfter(form, ">") {
		open := strings.IndexByte(part, '<')
		if open < 0 || !strings.HasSuffix(part, ">") {
			out.WriteString(part)
			continue
		}
		out.WriteString(part[:open+1] + kebabCase(part[open+1:len(part)-1]) + ">")
	}
	return out.String()
}

// given reports whether any flag of the family was given.
func (flags *listFlags) given() bool {
	if len(*flags.elements) > 0 {
		return true
	}
	return flags.fieldsGiven()
}

func (flags *listFlags) fieldsGiven() bool {
	for _, field := range flags.fields {
		if len(field.values) > 0 {
			return true
		}
	}
	return false
}

// elementEdits are the edits the element flag asks for: the list's members, and the primary
// field of each that gives one.
func (flags *listFlags) elementEdits() ([]fieldEdit, error) {
	spec := flags.spec
	if len(*flags.elements) == 0 {
		return nil, nil
	}
	keys := make([]string, 0, len(*flags.elements))
	var edits []fieldEdit
	for _, value := range *flags.elements {
		key, primary, hasPrimary := value, "", false
		if spec.primary != "" {
			key, primary, hasPrimary = strings.Cut(value, "=")
		}
		key = strings.TrimSpace(key)
		if key == "" {
			return nil, errors.Newf("--%s %q names no %s", spec.element, value, spec.noun)
		}
		keys = append(keys, key)
		if hasPrimary {
			edits = append(edits, fieldEdit{path: elementPath(spec.list, spec.key, key) + "." + spec.primary,
				value: primary, flag: "--" + spec.element, mustExist: true})
		}
	}
	members := fieldEdit{path: spec.list, flag: "--" + spec.element, members: keys, memberKey: spec.key, newMember: spec.newMember}
	return append([]fieldEdit{members}, edits...), nil
}

// fieldEdits are the edits the field flags ask for, each of one field of one element.
func (flags *listFlags) fieldEdits() ([]fieldEdit, error) {
	spec := flags.spec
	var edits []fieldEdit
	for _, field := range flags.fields {
		for _, value := range field.values {
			key, fieldValue, found := strings.Cut(value, "=")
			if !found || strings.TrimSpace(key) == "" {
				return nil, errors.Newf("--%s %q must be <%s>=<value>", field.flag, value, kebabCase(spec.key))
			}
			edit := fieldEdit{path: elementPath(spec.list, spec.key, strings.TrimSpace(key)) + "." + field.field,
				flag: "--" + field.flag, mustExist: true}
			if fieldValue == clearFlagValue {
				edit.unset = true
			} else {
				edit.value = fieldValue
			}
			edits = append(edits, edit)
		}
	}
	return edits, nil
}

// queue adds the family's edits to the ones the command's write will make.
func (flags *listFlags) queue() error {
	elementEdits, err := flags.elementEdits()
	if err != nil {
		return err
	}
	fieldEdits, err := flags.fieldEdits()
	if err != nil {
		return err
	}
	pendingFieldEdits = append(append(pendingFieldEdits, elementEdits...), fieldEdits...)
	return nil
}

// queueFields adds only the field flags' edits, for a command that sets the elements itself.
func (flags *listFlags) queueFields() error {
	fieldEdits, err := flags.fieldEdits()
	if err != nil {
		return err
	}
	pendingFieldEdits = append(pendingFieldEdits, fieldEdits...)
	return nil
}

// replaceListEdit is the edit that makes the list at path hold exactly elements, which are
// values the API takes.
func replaceListEdit(flag, path string, elements any) (fieldEdit, error) {
	data, err := json.Marshal(elements)
	if err != nil {
		return fieldEdit{}, err
	}
	list := []any{}
	if err := json.Unmarshal(data, &list); err != nil {
		return fieldEdit{}, err
	}
	return fieldEdit{path: path, flag: flag, replace: list}, nil
}
