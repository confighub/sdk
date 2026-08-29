// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package mfclass

import (
	"bytes"
	"fmt"
	"sort"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/util/managedfields"
	"sigs.k8s.io/structured-merge-diff/v6/fieldpath"
	"sigs.k8s.io/structured-merge-diff/v6/typed"
	"sigs.k8s.io/structured-merge-diff/v6/value"
)

// Projector projects an object down to a field set (Project) and derives a
// field set from an object (ObjectFieldSet).
//
// With a structured-merge-diff schema — a managedfields.TypeConverter built
// from the cluster's OpenAPI — it is atomic-correct (a field owned as
// "f:selector: {}" that the schema marks atomic yields the whole subtree, while
// the same shape on a granular field yields only the key) and uses the schema's
// associative-list keys. Without a schema it falls back to schemaless
// heuristics that approximate the same result; the residual limitation is that
// atomic-vs-granular key-only ownership cannot be told apart.
type Projector struct {
	tc managedfields.TypeConverter
}

// NewProjector returns a Projector. A nil TypeConverter selects the schemaless
// fallback.
func NewProjector(tc managedfields.TypeConverter) *Projector {
	return &Projector{tc: tc}
}

// SchemaAware reports whether the projector has a schema.
func (p *Projector) SchemaAware() bool { return p != nil && p.tc != nil }

// schemalessProjector backs the package-level Project / ObjectFieldSet helpers.
var schemalessProjector = &Projector{}

// ParseEntry parses the FieldsV1 of a managedFields entry into a fieldpath.Set
// describing exactly the fields that manager owns. An entry with no FieldsV1
// yields an empty set.
func ParseEntry(e metav1.ManagedFieldsEntry) (*fieldpath.Set, error) {
	s := fieldpath.NewSet()
	if e.FieldsV1 == nil || len(e.FieldsV1.Raw) == 0 {
		return s, nil
	}
	if err := s.FromJSON(bytes.NewReader(e.FieldsV1.Raw)); err != nil {
		return nil, fmt.Errorf("parse FieldsV1 for manager %q: %w", e.Manager, err)
	}
	return s, nil
}

// Union returns the union of the given sets. Nil sets are ignored.
func Union(sets ...*fieldpath.Set) *fieldpath.Set {
	out := fieldpath.NewSet()
	for _, s := range sets {
		if s != nil {
			out = out.Union(s)
		}
	}
	return out
}

// ObjectFieldSet returns the set of every field present on obj (schemaless).
// Differencing this against the union of all managed sets yields the default
// (unmanaged) fields the API server populated.
func ObjectFieldSet(obj *unstructured.Unstructured) *fieldpath.Set {
	return schemalessProjector.ObjectFieldSet(obj)
}

// ObjectFieldSet returns the set of every field present on obj. With a schema
// it uses the typed value's ToFieldSet (schema list keys, atomic fields as a
// single member); otherwise it falls back to the keyless SetFromValue.
func (p *Projector) ObjectFieldSet(obj *unstructured.Unstructured) *fieldpath.Set {
	if p.SchemaAware() {
		if tv, err := p.tc.ObjectToTyped(obj); err == nil {
			if fs, err := tv.ToFieldSet(); err == nil {
				return fs
			}
		}
	}
	return fieldpath.SetFromValue(value.NewValueInterface(obj.Object))
}

// RenderPaths returns the set's leaf paths as sorted, human-readable strings
// (e.g. ".spec.template.spec.containers[name=\"app\"].image").
func RenderPaths(s *fieldpath.Set) []string {
	if s == nil {
		return nil
	}
	var paths []string
	s.Leaves().Iterate(func(p fieldpath.Path) {
		paths = append(paths, p.String())
	})
	sort.Strings(paths)
	return paths
}

// Project returns a new object containing only the values at the paths in set
// (schemaless). See Projector.Project.
func Project(obj *unstructured.Unstructured, set *fieldpath.Set) (*unstructured.Unstructured, error) {
	return schemalessProjector.Project(obj, set)
}

// Project returns a new object containing only the values at the paths in set,
// preserving the object's structure. List-element key fields are retained so
// projected list items remain identifiable. This is the field-set-driven
// equivalent of the cleanup package's keepOnlyManagedFields, used to show the values an
// applier (or category) owns.
func (p *Projector) Project(obj *unstructured.Unstructured, set *fieldpath.Set) (*unstructured.Unstructured, error) {
	if set == nil || set.Empty() {
		return &unstructured.Unstructured{Object: map[string]interface{}{}}, nil
	}
	if p.SchemaAware() {
		if out, ok := p.projectSchema(obj, set); ok {
			return out, nil
		}
		// Fall through to the schemaless path if the object can't be typed
		// (e.g. its GVK is absent from the fetched OpenAPI).
	}
	return projectDeduced(obj, set)
}

// projectSchema extracts the owned fields using the schema. It returns ok=false
// when the object cannot be typed so the caller can fall back.
func (p *Projector) projectSchema(obj *unstructured.Unstructured, set *fieldpath.Set) (*unstructured.Unstructured, bool) {
	tv, err := p.tc.ObjectToTyped(obj)
	if err != nil {
		return nil, false
	}
	out, err := p.tc.TypedToObject(tv.ExtractItems(set, typed.WithAppendKeyFields()))
	if err != nil {
		return nil, false
	}
	u, ok := out.(*unstructured.Unstructured)
	if !ok {
		return &unstructured.Unstructured{Object: map[string]interface{}{}}, true
	}
	// Schema extraction of an associative list can leave nil placeholders for
	// the unselected positions; drop them so the projection is clean.
	if m, ok := compactNilListElems(u.Object).(map[string]interface{}); ok {
		u.Object = m
	}
	return u, true
}

// compactNilListElems recursively removes nil elements from lists.
func compactNilListElems(v interface{}) interface{} {
	switch t := v.(type) {
	case map[string]interface{}:
		for k, child := range t {
			t[k] = compactNilListElems(child)
		}
		return t
	case []interface{}:
		out := make([]interface{}, 0, len(t))
		for _, child := range t {
			if child == nil {
				continue
			}
			out = append(out, compactNilListElems(child))
		}
		return out
	default:
		return v
	}
}

// projectDeduced is the schemaless projection: structured-merge-diff's deduced
// (schema-free) type plus heuristics that compensate for its limitations.
func projectDeduced(obj *unstructured.Unstructured, set *fieldpath.Set) (*unstructured.Unstructured, error) {
	tv, err := typed.DeducedParseableType.FromUnstructured(obj.Object)
	if err != nil {
		return nil, fmt.Errorf("deduce typed value: %w", err)
	}
	extracted := tv.ExtractItems(set, typed.WithAppendKeyFields())
	raw := extracted.AsValue().Unstructured()
	m, ok := raw.(map[string]interface{})
	if !ok {
		// Extraction of a top-level map always yields a map; treat anything
		// else as empty rather than panicking.
		return &unstructured.Unstructured{Object: map[string]interface{}{}}, nil
	}
	// A field owned only as an empty key (FieldsV1 "f:spec: {}", i.e. the
	// manager owns the field's presence but none of its contents) extracts as a
	// nil value. Represent it faithfully as the empty shape it was applied with
	// (an empty map or list, per the source), rather than the noisy "spec: null"
	// that ExtractItems produces, so the projection round-trips to what the
	// manager applied.
	normalizeEmptyOwned(obj.Object, m)
	// ExtractItems with the schemaless deduced type can drop some scalar leaf
	// values (observed for deeply nested annotation entries). Re-copy any scalar
	// leaf the manager owns straight from the source. Scalars are unambiguous —
	// unlike composite "f:x: {}" key-only ownership, which normalizeEmptyOwned
	// already handled and which we must not expand here.
	overlayScalarLeaves(obj.Object, m, set)
	return &unstructured.Unstructured{Object: m}, nil
}

// overlayScalarLeaves ensures every scalar-valued leaf in set is present in dst,
// copied from src. Only pure field-name paths are handled (associative-list
// items are projected correctly by ExtractItems); composite values are left to
// normalizeEmptyOwned to avoid the atomic-vs-key-only ambiguity.
func overlayScalarLeaves(src, dst map[string]interface{}, set *fieldpath.Set) {
	set.Leaves().Iterate(func(p fieldpath.Path) {
		fields := make([]string, 0, len(p))
		for _, pe := range p {
			if pe.FieldName == nil {
				return // not a pure field-name path
			}
			fields = append(fields, *pe.FieldName)
		}
		val, found, err := unstructured.NestedFieldNoCopy(src, fields...)
		if err != nil || !found {
			return
		}
		switch val.(type) {
		case map[string]interface{}, []interface{}:
			return // composite — not our concern
		}
		_ = unstructured.SetNestedField(dst, val, fields...)
	})
}

// normalizeEmptyOwned rewrites nil values that ExtractItems emits for key-only
// ownership into the empty composite shape (map or list) of the corresponding
// source field, or drops them when the source field is scalar/absent.
func normalizeEmptyOwned(src, dst map[string]interface{}) {
	for k, v := range dst {
		switch dv := v.(type) {
		case nil:
			switch src[k].(type) {
			case map[string]interface{}:
				dst[k] = map[string]interface{}{}
			case []interface{}:
				dst[k] = []interface{}{}
			default:
				delete(dst, k)
			}
		case map[string]interface{}:
			if sm, ok := src[k].(map[string]interface{}); ok {
				normalizeEmptyOwned(sm, dv)
			}
		}
	}
}

// IdentityFieldSet is the set of object-identity fields (apiVersion, kind, and
// the metadata name/namespace). These are not tracked in managedFields but are
// kept when projecting so the result is a recognizable, valid manifest —
// mirroring the essential fields the cleanup package preserves.
func IdentityFieldSet() *fieldpath.Set {
	return fieldpath.NewSet(
		fieldpath.MakePathOrDie("apiVersion"),
		fieldpath.MakePathOrDie("kind"),
		fieldpath.MakePathOrDie("metadata", "name"),
		fieldpath.MakePathOrDie("metadata", "namespace"),
	)
}
