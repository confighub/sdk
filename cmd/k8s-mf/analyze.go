// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"fmt"
	"sort"

	"github.com/confighub/sdk/k8sutil/mfclass"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/structured-merge-diff/v6/fieldpath"
)

// managerOwnership is one managedFields entry, classified and parsed.
type managerOwnership struct {
	class mfclass.Classification
	set   *fieldpath.Set
	entry metav1.ManagedFieldsEntry
}

// analysis is the classified, parsed view of a resource's managedFields.
type analysis struct {
	obj     *unstructured.Unstructured
	owners  []managerOwnership
	managed *fieldpath.Set       // union of every manager's fields
	proj    *mfclass.Projector   // schema-aware when reading from a cluster
}

// analyze parses and classifies every managedFields entry on obj. proj is the
// projector used to derive owned/default field sets (schema-aware or not).
func analyze(obj *unstructured.Unstructured, proj *mfclass.Projector) (*analysis, error) {
	a := &analysis{obj: obj, managed: fieldpath.NewSet(), proj: proj}
	for _, e := range obj.GetManagedFields() {
		set, err := mfclass.ParseEntry(e)
		if err != nil {
			return nil, err
		}
		a.owners = append(a.owners, managerOwnership{
			class: mfclass.Classify(e),
			set:   set,
			entry: e,
		})
		a.managed = a.managed.Union(set)
	}
	return a, nil
}

// resourceRef is a short "kind/[namespace/]name" label for display.
func resourceRef(obj *unstructured.Unstructured) string {
	kind := obj.GetKind()
	if kind == "" {
		kind = "object"
	}
	if ns := obj.GetNamespace(); ns != "" {
		return fmt.Sprintf("%s/%s/%s", kind, ns, obj.GetName())
	}
	return fmt.Sprintf("%s/%s", kind, obj.GetName())
}

// byCategory groups owners by category, preserving the display order from
// mfclass.Categories(). Empty categories are omitted.
func (a *analysis) byCategory() []categoryReport {
	groups := map[mfclass.Category][]managerOwnership{}
	for _, o := range a.owners {
		groups[o.class.Category] = append(groups[o.class.Category], o)
	}
	reports := []categoryReport{}
	for _, cat := range mfclass.Categories() {
		owners := groups[cat]
		if len(owners) == 0 {
			continue
		}
		union := fieldpath.NewSet()
		var managers []managerReport
		for _, o := range owners {
			union = union.Union(o.set)
			managers = append(managers, newManagerReport(o))
		}
		sort.Slice(managers, func(i, j int) bool { return managers[i].Manager < managers[j].Manager })
		reports = append(reports, categoryReport{
			Category: string(cat),
			Managers: managers,
			Paths:    mfclass.RenderPaths(union),
		})
	}
	return reports
}

// defaultFields are the fields present on the object but owned by no manager —
// values the API server defaulted. Only meaningful for a live/complete object.
//
// The object's own managedFields array and structural/identity fields
// (apiVersion, kind, metadata identity) are not managed by any applier but are
// not interesting "defaults" either, so they are excluded.
func (a *analysis) defaultFields() []string {
	out := []string{}
	for _, p := range mfclass.RenderPaths(a.defaultFieldSet()) {
		if isStructuralNoise(p) {
			continue
		}
		out = append(out, p)
	}
	return out
}

// defaultFieldSet returns the fields present on the object but owned by no
// manager.
//
// It subtracts the owned fields re-derived through the object itself (via the
// projector) rather than the raw managed set, so both operands use the same key
// representation — schema list keys when a schema is available, otherwise the
// keyless SetFromValue guess — and associative-list elements (ports keyed by
// containerPort/protocol, status conditions keyed by type, …) match instead of
// being double-counted as defaults.
func (a *analysis) defaultFieldSet() *fieldpath.Set {
	stripped := a.obj.DeepCopy()
	unstructured.RemoveNestedField(stripped.Object, "metadata", "managedFields")
	all := a.proj.ObjectFieldSet(stripped)
	if a.proj.SchemaAware() {
		// With a schema, the parsed FieldsV1 sets already use the same keys and
		// atomic representation as ToFieldSet, so subtracting them directly is
		// correct.
		return all.Difference(a.managed)
	}
	return all.Difference(a.ownedObjectFieldSet())
}

// ownedObjectFieldSet is the union, in the object's own key representation, of
// every manager's owned fields, for the schemaless path. Each manager's set is
// projected individually and re-derived via ObjectFieldSet so associative-list
// keys match the object; the schemaless guess differs from the FieldsV1 keys,
// which is why the round-trip is needed (the schema-aware path skips it).
func (a *analysis) ownedObjectFieldSet() *fieldpath.Set {
	owned := fieldpath.NewSet()
	for _, o := range a.owners {
		if projected, err := a.proj.Project(a.obj, o.set); err == nil {
			owned = owned.Union(a.proj.ObjectFieldSet(projected))
		} else {
			owned = owned.Union(o.set)
		}
	}
	return owned
}

// structuralNoise are paths that are never applier-managed but are not
// meaningful API-server defaults: object identity and server-populated metadata.
var structuralNoise = map[string]bool{
	".apiVersion":                 true,
	".kind":                       true,
	".metadata.name":              true,
	".metadata.namespace":         true,
	".metadata.generation":        true,
	".metadata.resourceVersion":   true,
	".metadata.uid":               true,
	".metadata.selfLink":          true,
	".metadata.creationTimestamp": true,
}

func isStructuralNoise(path string) bool {
	return structuralNoise[path]
}

// coOwnedFields lists leaf paths owned by more than one manager — a frequent
// source of apply conflicts.
func (a *analysis) coOwnedFields() []coOwnedField {
	owners := map[string][]string{}
	for _, o := range a.owners {
		for _, p := range mfclass.RenderPaths(o.set) {
			owners[p] = append(owners[p], o.entry.Manager)
		}
	}
	out := []coOwnedField{}
	for p, mgrs := range owners {
		if len(mgrs) < 2 {
			continue
		}
		sort.Strings(mgrs)
		out = append(out, coOwnedField{Path: p, Managers: mgrs})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out
}

func newManagerReport(o managerOwnership) managerReport {
	return managerReport{
		Manager:     o.entry.Manager,
		Display:     o.class.Display,
		Category:    string(o.class.Category),
		Operation:   string(o.entry.Operation),
		Subresource: o.entry.Subresource,
		Heuristic:   o.class.Heuristic,
		Paths:       mfclass.RenderPaths(o.set),
	}
}

// --- serializable report shapes (used for -o json|yaml) ---

type managerReport struct {
	Manager     string   `json:"manager"`
	Display     string   `json:"display"`
	Category    string   `json:"category"`
	Operation   string   `json:"operation"`
	Subresource string   `json:"subresource,omitempty"`
	Heuristic   bool     `json:"heuristic,omitempty"`
	Paths       []string `json:"paths"`
}

type categoryReport struct {
	Category string          `json:"category"`
	Managers []managerReport `json:"managers"`
	Paths    []string        `json:"paths"`
}

type coOwnedField struct {
	Path     string   `json:"path"`
	Managers []string `json:"managers"`
}

type categoriesResult struct {
	Resource      string           `json:"resource"`
	Categories    []categoryReport `json:"categories"`
	DefaultFields []string         `json:"defaultFields"`
	CoOwnedFields []coOwnedField   `json:"coOwnedFields"`
}
