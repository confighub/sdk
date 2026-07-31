// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

// Package k8sdescribe renders "kubectl describe"-style summaries of Kubernetes
// resources stored in ConfigHub. It is the Go counterpart of the friendly
// resource views in the web UI (ui/src/pages/x/resource-explorer): both render
// *desired state* — the YAML held in a Unit — so every field is optional and a
// malformed document degrades to empty sections rather than an error.
//
// Describe returns a neutral section/field/table tree; the caller decides how
// to print it.
package k8sdescribe

import (
	"fmt"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// Field is one label/value row.
type Field struct {
	Label string
	Value string
}

// Table is a compact column-oriented listing (containers, ports, rules, ...).
type Table struct {
	Columns []string
	Rows    [][]string
}

// Block is a named multi-line value, such as a ConfigMap entry or inline Helm
// values. Callers typically print these verbatim, indented.
type Block struct {
	Title string
	Text  string
}

// Item is one element of a Section, rendered in the order it was added.
type Item struct {
	Field *Field
	Table *Table
	Block *Block
	// Note is an italic-style aside in the UI ("No routes defined."), printed
	// as plain text here.
	Note string
}

// Section is a titled group of items.
type Section struct {
	Title string
	Items []Item
}

// Empty reports whether the section would render nothing.
func (s *Section) Empty() bool { return len(s.Items) == 0 }

func newSection(title string) *Section { return &Section{Title: title} }

// field appends a label/value row, dropping it when the value is empty. This
// lets each view list every potentially-interesting field without a presence
// check at each call site, mirroring FieldList in the UI.
func (s *Section) field(label, value string) *Section {
	if value == "" {
		return s
	}
	s.Items = append(s.Items, Item{Field: &Field{Label: label, Value: value}})
	return s
}

// table appends a table, dropping it when it has no rows.
func (s *Section) table(columns []string, rows [][]string) *Section {
	if len(rows) == 0 {
		return s
	}
	s.Items = append(s.Items, Item{Table: &Table{Columns: columns, Rows: rows}})
	return s
}

func (s *Section) block(title, text string) *Section {
	if strings.TrimSpace(text) == "" {
		return s
	}
	s.Items = append(s.Items, Item{Block: &Block{Title: title, Text: text}})
	return s
}

func (s *Section) note(text string) *Section {
	s.Items = append(s.Items, Item{Note: text})
	return s
}

// add appends the section unless it is empty.
func add(sections []*Section, s *Section) []*Section {
	if s == nil || s.Empty() {
		return sections
	}
	return append(sections, s)
}

// ---------------------------------------------------------------------------
// Safe accessors for a parsed YAML document. Views receive `any` and narrow
// with these instead of type-asserting, so unexpected shapes yield empty
// output rather than a panic.
// ---------------------------------------------------------------------------

type record = map[string]any

func asRecord(v any) record {
	if m, ok := v.(map[string]any); ok {
		return m
	}
	return nil
}

func asArray(v any) []any {
	if a, ok := v.([]any); ok {
		return a
	}
	return nil
}

func asString(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}

// asScalar renders a string, number, or boolean; anything else yields "".
func asScalar(v any) string {
	switch t := v.(type) {
	case string:
		return t
	case bool:
		return fmt.Sprintf("%t", t)
	case int, int32, int64, uint, uint32, uint64:
		return fmt.Sprintf("%d", t)
	case float32, float64:
		return strings.TrimRight(strings.TrimRight(fmt.Sprintf("%f", t), "0"), ".")
	default:
		return ""
	}
}

func asStringMap(v any) map[string]string {
	rec := asRecord(v)
	if rec == nil {
		return nil
	}
	out := make(map[string]string, len(rec))
	for k, val := range rec {
		if s := asScalar(val); s != "" {
			out[k] = s
		}
	}
	return out
}

func asStringSlice(v any) []string {
	arr := asArray(v)
	out := make([]string, 0, len(arr))
	for _, e := range arr {
		if s := asScalar(e); s != "" {
			out = append(out, s)
		}
	}
	return out
}

// get walks a fixed key path through nested records.
func get(root any, keys ...string) any {
	current := root
	for _, key := range keys {
		rec := asRecord(current)
		if rec == nil {
			return nil
		}
		current = rec[key]
	}
	return current
}

// str is get + asScalar, the most common combination in the views.
func str(root any, keys ...string) string { return asScalar(get(root, keys...)) }

func joinList(v any) string { return strings.Join(asStringSlice(v), ", ") }

// sortedKeys makes map rendering deterministic, which matters for a CLI whose
// output is diffed and grepped.
func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// selectorString renders a label map as the "k=v,k=v" selector syntax.
func selectorString(m map[string]string) string {
	if len(m) == 0 {
		return ""
	}
	parts := make([]string, 0, len(m))
	for _, k := range sortedKeys(m) {
		parts = append(parts, k+"="+m[k])
	}
	return strings.Join(parts, ",")
}

// nameRefs collects the .name of each element of a list of object references
// (imagePullSecrets, secrets, middlewares, ...).
func nameRefs(v any) []string {
	arr := asArray(v)
	out := make([]string, 0, len(arr))
	for _, e := range arr {
		if n := str(e, "name"); n != "" {
			out = append(out, n)
		}
	}
	return out
}

// yamlText renders a nested value as YAML for a Block.
func yamlText(v any) string {
	out, err := yaml.Marshal(v)
	if err != nil {
		return ""
	}
	return strings.TrimRight(string(out), "\n")
}

// firstKey returns the sole/first key of a record, used for the several
// Kubernetes "union" objects that name their variant with a single key
// (Middleware type, SecretStore provider, volume source, DNS solver).
func firstKey(rec record) string {
	keys := make([]string, 0, len(rec))
	for k := range rec {
		keys = append(keys, k)
	}
	if len(keys) == 0 {
		return ""
	}
	sort.Strings(keys)
	return keys[0]
}

// scalarsAndNested splits a record into scalar label/value rows and nested
// values rendered as YAML blocks. Shared by the generic view and the Traefik
// middleware view, which both face arbitrary configuration shapes.
func scalarsAndNested(s *Section, rec record, skip map[string]bool) {
	keys := make([]string, 0, len(rec))
	for k := range rec {
		if skip == nil || !skip[k] {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	var nested []string
	for _, k := range keys {
		if scalar := asScalar(rec[k]); scalar != "" {
			s.field(k, scalar)
		} else if rec[k] != nil {
			nested = append(nested, k)
		}
	}
	for _, k := range nested {
		s.block(k, yamlText(rec[k]))
	}
}
