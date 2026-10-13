// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package yamlkit

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/confighub/sdk/core/function/api"
	"github.com/confighub/sdk/core/third_party/gaby"
)

// FindPath returns the node a path names in a resource, resolving merge-key segments
// against the resource's elements. A path through an embedded accessor is found when the
// field holding the embedded value is: what is inside it is the accessor's to read.
func FindPath(doc *gaby.YamlDoc, path api.UnresolvedPath) (*gaby.YamlDoc, bool) {
	structural, _, _ := strings.Cut(string(path), EmbeddedAccessorSeparator)
	resolved, err := ResolveAssociativePaths(doc, api.UnresolvedPath(structural), "", false, nil)
	if err != nil {
		return nil, false
	}
	for _, resolvedPath := range resolved {
		node, found, err := YamlSafePathGetDoc(doc, resolvedPath.Path, true)
		if err == nil && found && node != nil {
			return node, true
		}
	}
	return nil, false
}

// CheckPathSettable reports why a setter could not write a path in a resource, or nil when it
// could. It answers for a setter that creates what is missing, as set-attributes does: a missing
// map key is created, and so is everything below it, but a merge-key segment has to match an
// element unless it is the last segment, which appends one, and an index has to name an element
// that exists or the end of the list. Asking before writing is what lets a caller that names a
// path -- a Link's binding, a resolve setting a value -- report a write to nowhere instead of
// making it.
func CheckPathSettable(doc *gaby.YamlDoc, path api.UnresolvedPath) error {
	structural, _, _ := strings.Cut(string(path), EmbeddedAccessorSeparator)
	resolved, err := ResolveAssociativePaths(doc, api.UnresolvedPath(structural), "", true, nil)
	if err != nil {
		return fmt.Errorf("path %s: %w", string(path), err)
	}
	if len(resolved) == 0 {
		return fmt.Errorf("path %s matches nothing", string(path))
	}
	for _, resolvedPath := range resolved {
		if err := checkUpsertCreatable(doc, resolvedPath.Path); err != nil {
			return err
		}
	}
	return nil
}

// checkUpsertCreatable refuses an upsert of a resolved path that would have to create a
// container under a segment written as an array index. Writing `containers.0.env.3.value` where
// `env` does not exist would otherwise create `env` as a map with the key 3 -- a structure no
// schema with a list there accepts -- and report success. A numeric key in a map that exists is
// an ordinary key. An index into a list that exists names an element, or, as the last segment,
// the end of the list, where a whole element is appended.
func checkUpsertCreatable(doc *gaby.YamlDoc, path api.ResolvedPath) error {
	segments := gaby.DotPathToSlice(string(path))
	node := doc
	for i, segment := range segments {
		child := node.S(segment)
		if child != nil {
			node = child
			continue
		}
		if node.IsArray() {
			length := len(node.Children())
			index, err := strconv.Atoi(segment)
			if err != nil || index != length || i != len(segments)-1 {
				return fmt.Errorf("cannot set path %s: %s has %d elements, so %s names none of them",
					string(path), JoinPathSegments(segments[:i]), length, segment)
			}
			return nil
		}
		for _, created := range segments[i+1:] {
			if created != "" && IsNumber(created) {
				return fmt.Errorf("cannot set path %s: %s does not exist, and %s would be an index into a list that does not exist",
					string(path), JoinPathSegments(segments[:i+1]), created)
			}
		}
		return nil
	}
	return nil
}
