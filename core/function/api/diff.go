// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package api

import (
	"strconv"
	"strings"
)

// DiffChangeType is the kind of change a PathChange describes.
type DiffChangeType string

const (
	// The path is present only on the To side.
	DiffChangeTypeAdd DiffChangeType = "Add"
	// The path is present only on the From side.
	DiffChangeTypeDelete DiffChangeType = "Delete"
	// The value at the path changed.
	DiffChangeTypeUpdate DiffChangeType = "Update"
	// The value at the path changed kind, such as from a scalar to a map.
	DiffChangeTypeReplace DiffChangeType = "Replace"
	// The elements of a merge-keyed array are in a different order. FromValue and ToValue
	// are the merge-key values of the elements present on both sides, in each side's order.
	DiffChangeTypeReorder DiffChangeType = "Reorder"
	// A merge-keyed array element's merge-key value changed while the element stayed the
	// same element. FromValue and ToValue are the previous and new merge-key values.
	DiffChangeTypeRename DiffChangeType = "Rename"
)

// ConfigDiff is the difference between two configurations, for display. It is computed with
// the same matching as ComputeMutations -- merge keys, anchors, resource renames -- but
// carries both sides of each change and lists changes in document order.
type ConfigDiff struct {
	Resources []ResourceDiff `description:"One entry per resource that differs, in the To configuration's order; resources only on the From side follow where they were"`
}

// ResourceDiff is the difference in one resource.
type ResourceDiff struct {
	Resource         ResourceInfo  `description:"Identifies the resource on the To side, or on the From side if it was deleted"`
	PreviousResource *ResourceInfo `json:",omitempty" description:"The resource on the From side, when it was matched across a rename"`
	ChangeType       MutationType  `description:"Add, Delete, Update, or None when the resource is unchanged"`
	FromValue        string        `json:",omitempty" description:"The whole resource, for a Delete"`
	ToValue          string        `json:",omitempty" description:"The whole resource, for an Add"`
	Changes          []PathChange  `json:",omitempty" description:"Changes within the resource, for an Update, in document order"`
	Attribution      *MutationInfo `json:",omitempty" description:"For an Add, the To side's MutationSources entry for the resource, which says what added it; returned when include names Attribution"`
}

// PathChange is one changed path within a resource.
type PathChange struct {
	Path        ResolvedPath   `description:"The path as MutationSources and patch functions record it"`
	DisplayPath string         `description:"The path in configuration path syntax, as --path arguments and where filters take it, with each unkeyed array element named by its index"`
	Segments    []PathSegment  `description:"The path split into its segments, for rendering without parsing the path syntax"`
	ChangeType  DiffChangeType `description:"Add, Delete, Update, Replace, Reorder, or Rename"`
	FromValue   string         `json:",omitempty" description:"The value on the From side: a scalar's text, or a YAML block for a map or array"`
	ToValue     string         `json:",omitempty" description:"The value on the To side: a scalar's text, or a YAML block for a map or array"`
	Patch       string         `json:",omitempty" description:"A unified line diff, for multi-line string values"`
	Attribution *MutationInfo  `json:",omitempty" description:"The To side's MutationSources entry for this path, which says what set the new value; returned when include names Attribution"`
}

// PathSegment is one segment of a PathChange's path: a map key, or an array element.
type PathSegment struct {
	Field     string          `json:",omitempty" description:"The map key, for a map segment"`
	MergeKeys []MergeKeyValue `json:",omitempty" description:"The merge keys that identify the element, for an element of a merge-keyed array"`
	FromIndex int             `description:"The element's index on the From side; -1 for a map key or an element absent there"`
	ToIndex   int             `description:"The element's index on the To side; -1 for a map key or an element absent there"`
}

// MergeKeyValue is one merge key of an array element and its value.
type MergeKeyValue struct {
	Key   string
	Value string
}

// FieldSegment returns the segment for a map key.
func FieldSegment(key string) PathSegment {
	return PathSegment{Field: key, FromIndex: -1, ToIndex: -1}
}

// IsElement reports whether the segment is an array element rather than a map key.
func (segment *PathSegment) IsElement() bool {
	return segment.FromIndex >= 0 || segment.ToIndex >= 0 || len(segment.MergeKeys) > 0
}

// DisplayPathFromSegments renders segments in configuration path syntax: map keys with dots
// escaped as ~1, merge-keyed elements as ?key=value, and other elements by their index on
// the To side, or on the From side when the element is only there.
func DisplayPathFromSegments(segments []PathSegment) string {
	parts := make([]string, 0, len(segments))
	for i := range segments {
		segment := &segments[i]
		switch {
		case len(segment.MergeKeys) > 0:
			pairs := make([]string, 0, len(segment.MergeKeys))
			for _, kv := range segment.MergeKeys {
				pairs = append(pairs, escapePathDots(kv.Key)+"="+escapePathDots(kv.Value))
			}
			parts = append(parts, "?"+strings.Join(pairs, ","))
		case segment.ToIndex >= 0:
			parts = append(parts, strconv.Itoa(segment.ToIndex))
		case segment.FromIndex >= 0:
			parts = append(parts, strconv.Itoa(segment.FromIndex))
		default:
			parts = append(parts, escapePathDots(segment.Field))
		}
	}
	return strings.Join(parts, ".")
}

func escapePathDots(s string) string {
	return strings.ReplaceAll(s, ".", "~1")
}
