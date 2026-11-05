// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package yamlkit

import (
	"slices"
	"strings"

	"github.com/confighub/sdk/core/function/api"
	"github.com/confighub/sdk/core/third_party/gaby"
)

// PathWildcardSegment, as a segment of a path pattern, matches any one segment: an element of a
// list, however it is named, or a key of a map.
const PathWildcardSegment = "*"

// PathPatternMatches reports whether a path someone wrote names a path a record holds, such as
// the path of a merge conflict, in doc, the data of the resource both are about.
//
// The two seldom agree as text. A record names an element of a list by its merge key with the
// position it had (?name=main;@0), or, in a list with no merge key, by digests of its content
// and its neighbors (?~h=9b1164c8,~p=c4b0,~n=^,~l=4) -- neither of which anyone can be expected
// to type. So both are compared in every form doc resolves them to: as written, without the
// position hints, with each content-identity segment as the index of the element it names, and
// with every list segment as an index. A person can then write an element by position, by merge
// key, or as the record has it; and with PathWildcardSegment, as any element at all.
//
// doc may be nil, for a resource the data does not have; the paths are then compared only in the
// forms they have without it.
func PathPatternMatches(doc *gaby.YamlDoc, pattern, path api.ResolvedPath) bool {
	if pattern == path {
		return true
	}
	patternForms := pathForms(doc, string(pattern))
	for _, pathForm := range pathForms(doc, string(path)) {
		for _, patternForm := range patternForms {
			if pathSegmentsMatch(patternForm, pathForm) {
				return true
			}
		}
	}
	return false
}

// pathForms returns the forms a path is compared in: see PathPatternMatches.
func pathForms(doc *gaby.YamlDoc, path string) []string {
	forms := []string{path}
	add := func(form string) {
		if !slices.Contains(forms, form) {
			forms = append(forms, form)
		}
	}
	canonical := string(CanonicalMutationPath(api.ResolvedPath(path)))
	add(canonical)
	if doc == nil || !strings.Contains(path, "?") {
		return forms
	}
	if resolved, _ := ResolveAnchorSegments(doc, canonical); resolved != canonical {
		add(resolved)
		canonical = resolved
	}
	if positional, ok := ResolveAssociativeSegments(doc, canonical); ok {
		add(positional)
	}
	return forms
}

// pathSegmentsMatch compares two paths segment by segment, a wildcard in the pattern matching any
// one segment. Splitting on "." is safe: dots within segments are escaped as ~1.
func pathSegmentsMatch(pattern, path string) bool {
	if !strings.Contains(pattern, PathWildcardSegment) {
		return pattern == path
	}
	patternSegments := strings.Split(pattern, ".")
	pathSegments := strings.Split(path, ".")
	if len(patternSegments) != len(pathSegments) {
		return false
	}
	for i := range patternSegments {
		if patternSegments[i] != PathWildcardSegment && patternSegments[i] != pathSegments[i] {
			return false
		}
	}
	return true
}
