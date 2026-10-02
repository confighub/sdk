// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/cockroachdb/errors"
	"github.com/confighub/sdk/core/configkit/cubkit"
	"github.com/confighub/sdk/core/function/api"
	"github.com/confighub/sdk/core/third_party/gaby"
	"github.com/google/uuid"
	"github.com/spf13/cobra"
	"sigs.k8s.io/yaml"
)

// --set and --unset change any field of an entity by its path in the entity's document: the
// fields a write can set, with the entities it refers to named rather than identified by ID. The
// flags that name one field or one list, such as --stage-where-space, are shorthand for them, so
// one mechanism reads the path, checks it against the document's schema, converts the value to
// the field's type, and changes the entity the command is about to write.
//
// A path is in ConfigHub's path syntax: field names separated by dots, a list element addressed
// by its position or as ?<key>=<value>, and a dot inside a name written ~1.

// fieldEdit is one change to a field of an entity.
type fieldEdit struct {
	// path is the field's path in the entity's document.
	path string
	// value is what to set it to, as it was typed; ignored when unset.
	value string
	// unset removes the field, or the list element the path ends at.
	unset bool
	// flag is the flag the edit came from, for messages.
	flag string
	// mustExist refuses a list element the path goes through that the entity does not have,
	// rather than adding it. A flag that sets one field of an element asks for this, since a
	// name that matches nothing there is a misspelling.
	mustExist bool
	// members, when not nil, makes the list at path hold exactly the elements with these keys
	// in this order: one that is already there stays as it is, and the rest are made by
	// newMember.
	members   []string
	memberKey string
	newMember func(key string) (map[string]any, error)
	// replace, when not nil, is what the list at path becomes, whole.
	replace []any
}

var (
	setFlags   []string
	unsetFlags []string
	// fieldEditCommands is the entity type each command with --set writes.
	fieldEditCommands = map[*cobra.Command]string{}
	// pendingFieldEdits are the edits the flags shorthand for --set ask for. A command's flag
	// reading adds to them, and applyFieldEdits and the patch builder take them.
	pendingFieldEdits []fieldEdit
)

// addFieldEditFlags registers --set and --unset on a command that writes an entity of the type.
func addFieldEditFlags(cmd *cobra.Command, entityType string) {
	fieldEditCommands[cmd] = entityType
	cmd.Flags().StringArrayVar(&setFlags, "set", nil,
		"set a field of the "+entityType+" by its path, as <path>=<value> (repeatable). A path is field names separated by dots; "+
			"a list element is addressed as ?<key>=<value> or by position, as in Stages.?Name=prod.WhereSpace, and is added if it is not there. "+
			"A list of plain values separates them with ';'; an object or a list of objects is given as YAML or JSON. "+
			"A reference is given as it is elsewhere, by slug, <space>/<slug> or ID. With --patch, only a path with no list element in it")
	cmd.Flags().StringArrayVar(&unsetFlags, "unset", nil,
		"clear a field of the "+entityType+" by its path, or remove the list element the path ends at (repeatable)")
}

// runningEntityType is the entity type the running command writes with --set, if it has the flag.
func runningEntityType() (string, bool) {
	for cmd, entityType := range fieldEditCommands {
		if cmd.CommandPath() == runningCommandPath {
			return entityType, true
		}
	}
	return "", false
}

// splitSetExpression splits <path>=<value> at the first "=" that is not part of a ?<key>=<value>
// segment of the path. The value is everything after it.
func splitSetExpression(expression string) (string, string, error) {
	i := 0
	for i < len(expression) {
		switch expression[i] {
		case '"':
			end := strings.IndexByte(expression[i+1:], '"')
			if end < 0 {
				return "", "", errors.Newf("%q has an unterminated quote", expression)
			}
			i += end + 2
		case '?':
			equals := strings.IndexByte(expression[i:], '=')
			if equals < 0 {
				return "", "", errors.Newf("%q: a ?<key>=<value> segment needs a value", expression)
			}
			i += equals + 1
		}
		for i < len(expression) && expression[i] != '.' && expression[i] != '=' {
			i++
		}
		if i >= len(expression) {
			break
		}
		if expression[i] == '=' {
			if i == 0 {
				break
			}
			return expression[:i], expression[i+1:], nil
		}
		i++
	}
	return "", "", errors.Newf("%q must be <path>=<value>", expression)
}

// flagFieldEdits are the edits the command's write will make: the pending ones, which the flags
// for one list or one field ask for, and then those of --set and --unset. The general flags go
// last so that a list is what its own flags make it before a path reaches into it, and so that
// what is named by path is what the entity ends up with.
func flagFieldEdits() ([]fieldEdit, error) {
	edits := make([]fieldEdit, 0, len(setFlags)+len(unsetFlags)+len(pendingFieldEdits))
	edits = append(edits, pendingFieldEdits...)
	for _, expression := range setFlags {
		path, value, err := splitSetExpression(expression)
		if err != nil {
			return nil, errors.Wrap(err, "--set")
		}
		edits = append(edits, fieldEdit{path: path, value: value, flag: "--set"})
	}
	for _, path := range unsetFlags {
		if path == "" {
			return nil, errors.New("--unset needs a path")
		}
		edits = append(edits, fieldEdit{path: path, unset: true, flag: "--unset"})
	}
	return edits, nil
}

// pathSegment is one segment of a path: a field name, a position in a list, or the key and value
// that select a list element.
type pathSegment struct {
	name        string
	index       int
	isIndex     bool
	key, value  string
	associative bool
}

func (s pathSegment) String() string {
	switch {
	case s.associative:
		return "?" + s.key + "=" + s.value
	case s.isIndex:
		return strconv.Itoa(s.index)
	}
	return s.name
}

// pathEscaper writes a name or a value as one segment of a path, where a dot separates segments.
var pathEscaper = strings.NewReplacer("~", "~0", ".", "~1")

// elementPath is the path of the element of the list at listPath whose key field holds value.
func elementPath(listPath, key, value string) string {
	return listPath + ".?" + key + "=" + pathEscaper.Replace(value)
}

func parseFieldPath(path string) ([]pathSegment, error) {
	if strings.TrimSpace(path) == "" {
		return nil, errors.New("the path is empty")
	}
	raw := gaby.DotPathToSlice(path)
	segments := make([]pathSegment, 0, len(raw))
	for _, segment := range raw {
		if rest, ok := strings.CutPrefix(segment, "?"); ok {
			key, value, found := strings.Cut(rest, "=")
			if !found || key == "" {
				return nil, errors.Newf("%q: a list element is selected as ?<key>=<value>", path)
			}
			segments = append(segments, pathSegment{key: key, value: value, associative: true})
			continue
		}
		segments = append(segments, pathSegment{name: segment})
	}
	return segments, nil
}

// documentSchemas holds each entity type's parsed document schema.
var documentSchemas sync.Map

func entityDocumentSchema(entityType string) (map[string]any, error) {
	if schema, ok := documentSchemas.Load(entityType); ok {
		return schema.(map[string]any), nil
	}
	data, ok := cubkit.DocumentSchema(api.ResourceType(entityType))
	if !ok {
		return nil, errors.Newf("a %s has no document, so its fields cannot be set by path", entityType)
	}
	schema := map[string]any{}
	if err := json.Unmarshal(data, &schema); err != nil {
		return nil, err
	}
	documentSchemas.Store(entityType, schema)
	return schema, nil
}

// derefSchema follows a schema's $ref, and an allOf of one schema, into the root's definitions.
func derefSchema(root, schema map[string]any) map[string]any {
	for {
		if ref, ok := schema["$ref"].(string); ok {
			definitions, _ := root["definitions"].(map[string]any)
			target, ok := definitions[strings.TrimPrefix(ref, "#/definitions/")].(map[string]any)
			if !ok {
				return map[string]any{}
			}
			schema = target
			continue
		}
		if allOf, ok := schema["allOf"].([]any); ok && len(allOf) == 1 {
			if only, ok := allOf[0].(map[string]any); ok {
				schema = only
				continue
			}
		}
		return schema
	}
}

func jsonSchemaType(schema map[string]any) string {
	schemaType, _ := schema["type"].(string)
	if schemaType == "" {
		if _, ok := schema["properties"]; ok {
			return "object"
		}
		if _, ok := schema["items"]; ok {
			return "array"
		}
	}
	return schemaType
}

func schemaPropertyNames(schema map[string]any) []string {
	properties, _ := schema["properties"].(map[string]any)
	names := make([]string, 0, len(properties))
	for name := range properties {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// fieldSchema walks the document schema of an entity type along a path, marking the segments
// that turn out to be positions in a list, and returns the schema of what the path ends at.
func fieldSchema(entityType string, segments []pathSegment) (root, schema map[string]any, err error) {
	root, err = entityDocumentSchema(entityType)
	if err != nil {
		return nil, nil, err
	}
	schema = root
	walked := entityType
	for i := range segments {
		segment := &segments[i]
		schema = derefSchema(root, schema)
		switch jsonSchemaType(schema) {
		case "array":
			if !segment.associative {
				index, convErr := strconv.Atoi(segment.name)
				if convErr != nil || index < 0 {
					return nil, nil, errors.Newf("%s is a list: address an element as ?<key>=<value> or by position, not %q", walked, segment.name)
				}
				segment.index, segment.isIndex = index, true
			}
			items, _ := schema["items"].(map[string]any)
			if items == nil {
				items = map[string]any{}
			}
			if segment.associative {
				element := derefSchema(root, items)
				if properties, ok := element["properties"].(map[string]any); ok {
					if _, known := properties[segment.key]; !known {
						return nil, nil, errors.Newf("an element of %s has no field %q; it has %s",
							walked, segment.key, strings.Join(schemaPropertyNames(element), ", "))
					}
				}
			}
			schema = items
		case "object":
			if segment.associative {
				return nil, nil, errors.Newf("%s is not a list, so %s selects nothing in it", walked, segment)
			}
			properties, _ := schema["properties"].(map[string]any)
			if property, ok := properties[segment.name].(map[string]any); ok {
				schema = property
				break
			}
			switch additional := schema["additionalProperties"].(type) {
			case map[string]any:
				schema = additional
			case bool:
				if !additional {
					return nil, nil, unknownFieldError(walked, segment.name, schema)
				}
				schema = map[string]any{}
			default:
				if len(properties) > 0 {
					return nil, nil, unknownFieldError(walked, segment.name, schema)
				}
				schema = map[string]any{}
			}
		case "":
			// Nothing is said about what is here, so anything may be.
			if segment.associative {
				schema = map[string]any{}
			}
		default:
			return nil, nil, errors.Newf("%s is a %s, which has nothing inside it to address", walked, jsonSchemaType(schema))
		}
		walked += "." + segment.String()
	}
	return root, derefSchema(root, schema), nil
}

func unknownFieldError(walked, name string, schema map[string]any) error {
	return errors.Newf("%s has no field %q; it has %s", walked, name, strings.Join(schemaPropertyNames(schema), ", "))
}

// listValueSeparator separates the items of a list inside one flag value.
const listValueSeparator = ";"

// convertFieldValue turns a value as typed into what its schema says it is.
func convertFieldValue(root, schema map[string]any, value string) (any, error) {
	schema = derefSchema(root, schema)
	switch jsonSchemaType(schema) {
	case "string":
		return value, nil
	case "integer":
		number, err := strconv.ParseInt(strings.TrimSpace(value), 10, 64)
		if err != nil {
			return nil, errors.Newf("%q is not an integer", value)
		}
		return number, nil
	case "number":
		number, err := strconv.ParseFloat(strings.TrimSpace(value), 64)
		if err != nil {
			return nil, errors.Newf("%q is not a number", value)
		}
		return number, nil
	case "boolean":
		boolean, err := strconv.ParseBool(strings.TrimSpace(value))
		if err != nil {
			return nil, errors.Newf("%q is not true or false", value)
		}
		return boolean, nil
	case "array":
		items, _ := schema["items"].(map[string]any)
		itemType := jsonSchemaType(derefSchema(root, items))
		if itemType == "object" || itemType == "array" || itemType == "" {
			return parseStructuredValue(value)
		}
		list := []any{}
		if strings.TrimSpace(value) == "" {
			return list, nil
		}
		for _, item := range strings.Split(value, listValueSeparator) {
			converted, err := convertFieldValue(root, items, strings.TrimSpace(item))
			if err != nil {
				return nil, err
			}
			list = append(list, converted)
		}
		return list, nil
	case "object":
		structured, err := parseStructuredValue(value)
		if err != nil {
			return nil, err
		}
		if _, ok := structured.(map[string]any); !ok {
			return nil, errors.Newf("%q is not an object; give it as YAML or JSON", value)
		}
		return structured, nil
	}
	// The schema allows more than one type, as a function argument's value does: read it as
	// YAML would, so that 3 is a number and true a boolean.
	return parseStructuredValue(value)
}

func parseStructuredValue(value string) (any, error) {
	var parsed any
	if err := yaml.Unmarshal([]byte(value), &parsed); err != nil {
		return nil, errors.Wrapf(err, "%q is not YAML or JSON", value)
	}
	return parsed, nil
}

// referenceResolvers resolve a reference as the rest of cub takes one, a slug, <space>/<slug> or
// an ID, by the type of entity it names.
var referenceResolvers = map[string]func(ref string) (uuid.UUID, error){
	"Filter":         resolveFilterID,
	"Target":         resolveTargetID,
	"Component":      resolveComponentID,
	"Invocation":     resolveInvocationID,
	"BridgeWorker":   resolveWorkerID,
	"ChangeWorkflow": resolveChangeWorkflowID,
	"Unit":           resolveUnitID,
	"Space": func(ref string) (uuid.UUID, error) {
		space, err := resolveSpace(ref, "SpaceID")
		if err != nil {
			return uuid.Nil, err
		}
		return space.Space.SpaceID, nil
	},
}

// documentReference is a field of an entity's document that names another entity, where the
// entity itself holds an ID.
type documentReference struct {
	// field is its name in the entity: the document's with ID or IDs for a suffix.
	field  string
	target string
	list   bool
}

// documentReferences are the references the entity type's document has, by the name of the field.
func documentReferences(entityType string) map[string]documentReference {
	references := map[string]documentReference{}
	for _, declared := range cubkit.DeclaredReferences() {
		if string(declared.ResourceType) != entityType {
			continue
		}
		if name, isList := strings.CutSuffix(declared.Path, ".*"); isList {
			if !strings.Contains(name, ".") {
				references[name] = documentReference{field: strings.TrimSuffix(name, "s") + "IDs", target: string(declared.Target), list: true}
			}
			continue
		}
		if !strings.Contains(declared.Path, ".") {
			references[declared.Path] = documentReference{field: declared.Path + "ID", target: string(declared.Target)}
		}
	}
	return references
}

func resolveReference(target, ref string) (string, error) {
	resolve, ok := referenceResolvers[target]
	if !ok {
		return "", errors.Newf("cub cannot resolve a %s by name here", target)
	}
	id, err := resolve(ref)
	if err != nil {
		return "", err
	}
	return id.String(), nil
}

// resolvedEdit is an edit ready to apply to an entity's JSON: its path in the entity's own field
// names, and its value converted.
type resolvedEdit struct {
	fieldEdit
	segments []pathSegment
	value    any
}

// resolveFieldEdit checks an edit against the entity type's document schema and converts its
// value. A reference is resolved to the ID the entity holds.
func resolveFieldEdit(entityType string, edit fieldEdit) (*resolvedEdit, error) {
	wrap := func(err error) error { return errors.Wrapf(err, "%s %s", edit.flag, edit.path) }
	segments, err := parseFieldPath(edit.path)
	if err != nil {
		return nil, wrap(err)
	}
	root, schema, err := fieldSchema(entityType, segments)
	if err != nil {
		return nil, wrap(err)
	}
	resolved := &resolvedEdit{fieldEdit: edit, segments: segments}

	reference, isReference := documentReferences(entityType)[segments[0].name]
	if isReference {
		// A Link's Units are its arguments, and the to-Unit's Space goes with it.
		if entityType == "Link" && reference.target == "Unit" {
			return nil, wrap(errors.New("name a link's units as the command's arguments"))
		}
		segments[0].name = reference.field
	}
	switch {
	case edit.unset:
		return resolved, nil
	case edit.members != nil, edit.replace != nil:
		if jsonSchemaType(schema) != "array" {
			return nil, wrap(errors.New("it is not a list"))
		}
		return resolved, nil
	case isReference && reference.list && len(segments) == 1:
		ids := []any{}
		for _, ref := range strings.Split(edit.value, listValueSeparator) {
			if ref = strings.TrimSpace(ref); ref == "" {
				continue
			}
			id, err := resolveReference(reference.target, ref)
			if err != nil {
				return nil, wrap(err)
			}
			ids = append(ids, id)
		}
		resolved.value = ids
		return resolved, nil
	case isReference:
		id, err := resolveReference(reference.target, edit.value)
		if err != nil {
			return nil, wrap(err)
		}
		resolved.value = id
		return resolved, nil
	}

	last := segments[len(segments)-1]
	value, err := convertFieldValue(root, schema, edit.value)
	if err != nil {
		return nil, wrap(err)
	}
	if last.associative {
		element, ok := value.(map[string]any)
		if !ok {
			return nil, wrap(errors.New("the path ends at a list element, so the value is the element, as YAML or JSON"))
		}
		if _, has := element[last.key]; !has {
			element[last.key] = last.value
		}
	}
	resolved.value = value
	return resolved, nil
}

// findElement is the position of the element of a list that a segment selects, or -1.
func findElement(list []any, segment pathSegment) int {
	if segment.isIndex {
		if segment.index < len(list) {
			return segment.index
		}
		return -1
	}
	for i, element := range list {
		if object, ok := element.(map[string]any); ok && fmt.Sprint(object[segment.key]) == segment.value {
			return i
		}
	}
	return -1
}

// applyResolvedEdit changes the entity's JSON as the edit says.
func applyResolvedEdit(entity map[string]any, edit *resolvedEdit) error {
	wrap := func(err error) error { return errors.Wrapf(err, "%s %s", edit.flag, edit.path) }

	// put stores a value where the container the walk is in holds its child.
	var container any = entity
	put := func(any) {}
	walked := ""
	for i, segment := range edit.segments {
		last := i == len(edit.segments)-1
		if segment.associative || segment.isIndex {
			list, ok := container.([]any)
			if !ok && container != nil {
				return wrap(errors.Newf("%s is not a list", strings.TrimPrefix(walked, ".")))
			}
			position := findElement(list, segment)
			if last {
				switch {
				case edit.unset:
					if position >= 0 {
						put(append(list[:position:position], list[position+1:]...))
					}
				case position >= 0:
					list[position] = edit.value
				case segment.isIndex && segment.index > len(list):
					return wrap(errors.Newf("the list has %d elements, so the next position is %d", len(list), len(list)))
				default:
					put(append(list, edit.value))
				}
				return nil
			}
			if position < 0 {
				if edit.unset {
					return nil
				}
				if edit.mustExist || segment.isIndex {
					return wrap(errors.Newf("%s has no element %s", strings.TrimPrefix(walked, "."), segment))
				}
				list = append(list, map[string]any{segment.key: segment.value})
				position = len(list) - 1
				put(list)
			}
			index := position
			container = list[index]
			owner := list
			put = func(value any) { owner[index] = value }
			walked += "." + segment.String()
			continue
		}

		object, ok := container.(map[string]any)
		if !ok {
			return wrap(errors.Newf("%s is not an object", strings.TrimPrefix(walked, ".")))
		}
		name := segment.name
		if last {
			switch {
			case edit.unset:
				delete(object, name)
			case edit.replace != nil:
				object[name] = edit.replace
			case edit.members != nil:
				members, err := listMembers(object[name], edit)
				if err != nil {
					return wrap(err)
				}
				object[name] = members
			default:
				object[name] = edit.value
			}
			return nil
		}
		child, present := object[name]
		if !present || child == nil {
			if edit.unset {
				return nil
			}
			next := edit.segments[i+1]
			if next.associative || next.isIndex {
				child = []any{}
			} else {
				child = map[string]any{}
			}
			object[name] = child
		}
		container = child
		put = func(value any) { object[name] = value }
		walked += "." + name
	}
	return nil
}

// listMembers is the list an edit's members name: each element the list already has under that
// key, as it is, and a new one for each key it does not have.
func listMembers(current any, edit *resolvedEdit) ([]any, error) {
	existing, _ := current.([]any)
	members := make([]any, 0, len(edit.members))
	for _, key := range edit.members {
		position := findElement(existing, pathSegment{associative: true, key: edit.memberKey, value: key})
		if position >= 0 {
			members = append(members, existing[position])
			continue
		}
		member := map[string]any{edit.memberKey: key}
		if edit.newMember != nil {
			made, err := edit.newMember(key)
			if err != nil {
				return nil, err
			}
			member = made
		}
		members = append(members, member)
	}
	return members, nil
}

// applyFieldEdits applies the edits the flags ask for to an entity the command is about to write
// whole. entity is a pointer to the entity as the API holds it.
func applyFieldEdits(entityType string, entity any) error {
	edits, err := flagFieldEdits()
	if err != nil || len(edits) == 0 {
		return err
	}
	data, err := json.Marshal(entity)
	if err != nil {
		return err
	}
	fields := map[string]any{}
	if err := json.Unmarshal(data, &fields); err != nil {
		return err
	}
	for _, edit := range edits {
		resolved, err := resolveFieldEdit(entityType, edit)
		if err != nil {
			return err
		}
		if err := applyResolvedEdit(fields, resolved); err != nil {
			return err
		}
	}
	data, err = json.Marshal(fields)
	if err != nil {
		return err
	}
	// Into a zero value, so that a field an edit removed is not left holding what it held.
	target := reflect.ValueOf(entity).Elem()
	fresh := reflect.New(target.Type())
	if err := json.Unmarshal(data, fresh.Interface()); err != nil {
		return errors.Wrap(err, "the entity as edited is not one the API takes")
	}
	target.Set(fresh.Elem())
	return nil
}

// withFieldEdits adds the edits the flags ask for to what enhancer puts in a merge patch. A merge
// patch replaces a list whole, so only an edit whose path has no list element in it can be made
// this way; the others need the entity read and written back, which an update without --patch
// does.
func withFieldEdits(enhancer PatchEnhancer) (PatchEnhancer, error) {
	edits, err := flagFieldEdits()
	if err != nil || len(edits) == 0 {
		return enhancer, err
	}
	entityType, ok := runningEntityType()
	if !ok {
		return nil, errors.New("this command does not set fields by path")
	}
	resolved := make([]*resolvedEdit, 0, len(edits))
	for _, edit := range edits {
		r, err := resolveFieldEdit(entityType, edit)
		if err != nil {
			return nil, err
		}
		for _, segment := range r.segments {
			if segment.associative || segment.isIndex {
				return nil, errors.Newf("%s %s changes one element of a list, which a patch cannot do: it replaces a list whole. "+
					"Update one %s without --patch instead", edit.flag, edit.path, entityType)
			}
		}
		if edit.members != nil {
			return nil, errors.Newf("%s keeps the elements a list already has, which a patch cannot do: it replaces a list whole. "+
				"Update one %s without --patch instead", edit.flag, entityType)
		}
		resolved = append(resolved, r)
	}
	return func(patchMap map[string]interface{}) {
		if enhancer != nil {
			enhancer(patchMap)
		}
		for _, edit := range resolved {
			object := patchMap
			for i, segment := range edit.segments {
				if i == len(edit.segments)-1 {
					switch {
					case edit.unset:
						object[segment.name] = nil
					case edit.replace != nil:
						object[segment.name] = edit.replace
					default:
						object[segment.name] = edit.value
					}
					break
				}
				child, ok := object[segment.name].(map[string]interface{})
				if !ok {
					child = map[string]interface{}{}
					object[segment.name] = child
				}
				object = child
			}
		}
	}, nil
}
