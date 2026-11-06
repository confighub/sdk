// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package yamlkit

import (
	"slices"
	"strings"

	"github.com/confighub/sdk/core/function/api"
	"github.com/confighub/sdk/core/third_party/gaby"
)

// A path a person writes into a request -- to protect a field, guard it, or dismiss a conflict
// there -- names a list element however they can: by merge key (containers.?name=main.image), by
// position (containers.1.image), as a record or a conflict shows it (spec.data.?~h=...,~l=4.key),
// or as any element at all (containers.*.image). A record keys the same element one way: by merge
// key where the list has one, and by content where it does not. These turn the one into the other.

// MergeKeyLookupFor returns the merge keys a resource type declares for each array path.
func MergeKeyLookupFor(resourceType api.ResourceType, resourceProvider ResourceProvider) MergeKeyLookup {
	if resourceProvider == nil {
		return nil
	}
	return mergeKeyLookupFor(resourceType, resourceProvider)
}

// StoredPathForm returns one path someone wrote in the form a stored table keys it: canonical,
// each element of a list with no merge key that the path names by content resolved to its position
// in doc, and each element of a list with a merge key named by it. A path that names nothing in doc
// -- a guard on a field that does not exist yet -- and a wildcard segment are kept as written. doc
// may be nil, for a resource the data does not have.
//
// The positions it leaves for lists with no merge key are what a table's keys are followed across
// writes from (ReanchorPathAnnotations), so the key stays with the element it was written for.
func StoredPathForm(doc *gaby.YamlDoc, path api.ResolvedPath, mergeKeyLookup MergeKeyLookup) api.ResolvedPath {
	path = CanonicalMutationPath(path)
	if doc == nil || path == "" {
		return path
	}
	if resolved, ok := ResolveAnchorSegments(doc, string(path)); ok {
		path = api.ResolvedPath(resolved)
	}
	if named, ok := NameArrayElementsByMergeKey(doc, path, mergeKeyLookup); ok {
		return named
	}
	return path
}

// ExpandedPath is one path of a resource that a path someone wrote names.
type ExpandedPath struct {
	// Path is keyed as a stored record keys it: an element of a list with a merge key by the key,
	// and one of a list with none by its content (StoredFormOf).
	Path api.ResolvedPath
	// Named is the same path with an element of a list with no merge key by its position, which is
	// how such an element is addressed from outside, and how SetProtection takes it.
	Named api.ResolvedPath
	// Exists says the resource's data has something at Path.
	Exists bool
}

// ExpandAuthoredPath returns every path of doc that a path someone wrote names, with a wildcard
// segment standing for every element or key there, as ResolveAssociativePaths resolves one. Each is
// keyed as a stored record keys it. A path with no wildcard that names nothing in doc is returned
// as StoredPathForm writes it, not existing; one with a wildcard that matches nothing returns none.
// doc may be nil, for a resource the data does not have.
func ExpandAuthoredPath(doc *gaby.YamlDoc, path api.ResolvedPath, mergeKeyLookup MergeKeyLookup) ([]ExpandedPath, error) {
	wildcard := HasWildcardSegment(path)
	if doc == nil || path == "" {
		if wildcard {
			return nil, nil
		}
		stored := StoredPathForm(doc, path, mergeKeyLookup)
		return []ExpandedPath{{Path: stored, Named: stored, Exists: path == ""}}, nil
	}
	authored := CanonicalMutationPath(path)
	if resolved, ok := ResolveAnchorSegments(doc, string(authored)); ok {
		authored = api.ResolvedPath(resolved)
	}
	resolved, err := ResolveAssociativePaths(doc, api.UnresolvedPath(authored), "", false, nil)
	if err != nil {
		return nil, err
	}
	var expanded []ExpandedPath
	for _, info := range resolved {
		node, found, getErr := YamlSafePathGetDoc(doc, info.Path, true)
		if getErr != nil || !found || node == nil {
			continue
		}
		named, _ := NameArrayElementsByMergeKey(doc, info.Path, mergeKeyLookup)
		stored := StoredFormOf(doc, named, mergeKeyLookup)
		if !slices.ContainsFunc(expanded, func(e ExpandedPath) bool { return e.Path == stored }) {
			expanded = append(expanded, ExpandedPath{Path: stored, Named: named, Exists: true})
		}
	}
	if len(expanded) == 0 && !wildcard {
		stored := StoredPathForm(doc, path, mergeKeyLookup)
		return []ExpandedPath{{Path: stored, Named: stored}}, nil
	}
	return expanded, nil
}

// HasWildcardSegment reports whether a path has a segment matching any element or key: *, or the
// *? and *@ forms ResolveAssociativePaths also expands.
func HasWildcardSegment(path api.ResolvedPath) bool {
	for segment := range strings.SplitSeq(string(path), ".") {
		if strings.HasPrefix(segment, PathWildcardSegment) {
			return true
		}
	}
	return false
}
