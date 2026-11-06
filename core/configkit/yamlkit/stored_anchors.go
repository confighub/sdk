// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package yamlkit

import (
	"maps"
	"slices"
	"strconv"
	"strings"

	"github.com/confighub/sdk/core/function/api"
	"github.com/confighub/sdk/core/third_party/gaby"
)

// A stored record -- a Unit's MutationSources, its guard table -- names an element of a list
// with no merge key by its content: a digest of the element, digests of its neighbors, and the
// list's length. Content is all such an element has, but content changes, and a key nothing
// maintains names the element as it was when the key was written. Read against later data it
// names nothing, or the wrong element, so the merge engine could match it only as text against a
// patch's anchor for the same element as the same content -- which a protected value's owner
// gets until the first upstream change to the element, its neighbors, or the list's length, and
// then silently loses.
//
// So a stored key is kept describing the data it is stored with. Every write re-anchors the
// record against the data the write produced (ReanchorStoredPaths): an element's key is followed
// from the data before the write, where it resolves, through the write's own alignment of each
// list, to where the element sits after it, and rewritten as the anchor computed there. A key
// whose element the write removed has nothing left to name and is dropped, unless it records the
// removal itself. A key into a list by bare index, as records written before anchors hold, is
// followed the same way from the position it names in the data before the write, which is how the
// merge engine has always read it.
//
// Holding to that, a reader resolves a stored key exactly against the data it is stored with
// (ResolveStoredAnchors): the digest is the element's current one, and the neighbors and length
// are the list's current ones. The merge engine compares the target's stored keys with the
// patch's paths in what they resolve to there.

// storedAnchorElement returns the index of the element an anchored segment of a stored key names
// among elements, or -1.
//
// The digest, and the identity when there is one, have to match. Identical elements match alike,
// and a list can hold several -- a repeated argument, a duplicated entry -- so the neighbors and
// length the segment recorded decide among them; when they cannot, the segment names none of them
// rather than whichever comes first, since a protected value attributed to the wrong element
// withholds a change nobody claimed. Matching on content alone is deliberate: a stored key that
// has been kept current names its element's current content, and one that has not cannot be
// placed by guessing.
func storedAnchorElement(elements []*gaby.YamlDoc, segment string) int {
	pairs := strings.TrimPrefix(segment, "?")
	if rest, _, found := strings.Cut(pairs, ";@"); found {
		pairs = rest
	}
	keys, values, ok := associativeSegmentPairs(pairs)
	if !ok {
		return -1
	}
	contentKeys, contentValues, context := splitAnchorContext(keys, values)
	var candidates []int
	for index, element := range elements {
		if elementMatchesPairs(element, contentKeys, contentValues) {
			candidates = append(candidates, index)
		}
	}
	switch {
	case len(candidates) == 1:
		return candidates[0]
	case len(candidates) == 0 || !context.present:
		return -1
	}
	match := -1
	for _, index := range candidates {
		if !neighborsMatch(elements, index, context.previous, context.next) ||
			(context.length >= 0 && context.length != len(elements)) {
			continue
		}
		if match >= 0 {
			return -1
		}
		match = index
	}
	return match
}

// ResolveStoredAnchors rewrites each anchored segment of a stored key as the index of the element
// it names in doc, the data the key is stored with, and leaves every other segment as it is: a
// merge key stays a merge key, a field a field, a bare index a bare index.
//
// That is the form the merge engine compares stored keys and patch paths in (see positionalForm).
// A merge key is left alone because it is already exact and a merge moves merge-keyed elements
// while it runs, deleting one ahead of another; the elements of a list with no merge key do not
// move until the end of a merge (applyArrayElementOps), so an index into one is stable for as long
// as the comparison is made.
//
// The bool is false when some anchored segment names no element of doc; the key is then returned
// as it was.
func ResolveStoredAnchors(doc *gaby.YamlDoc, path string) (string, bool) {
	if doc == nil || !hasAnchorSegment(path) {
		return path, true
	}
	segments := strings.Split(path, ".")
	resolved := make([]string, len(segments))
	out := make([]string, len(segments))
	for i, segment := range segments {
		switch {
		case segmentIsAnchor(segment):
			index := storedAnchorElement(arrayElementsAtResolved(doc, resolved[:i]), segment)
			if index < 0 {
				return path, false
			}
			resolved[i] = strconv.Itoa(index)
			out[i] = resolved[i]
		case strings.HasPrefix(segment, "?"):
			index, ok := resolvedElementIndex(doc, strings.Join(append(append([]string{}, resolved[:i]...), segment), "."))
			if !ok {
				return path, false
			}
			resolved[i] = index
			out[i] = segment
		default:
			resolved[i] = segment
			out[i] = segment
		}
	}
	return strings.Join(out, "."), true
}

// positionalForm is the form of a patch path the merge engine compares stored keys in: the
// canonical path with each anchored segment replaced by the index the patch resolved it to in the
// target. canonicalPath and resolvedPath are the same path before and after resolution, segment
// for segment; anything else is returned canonical.
func positionalForm(canonicalPath, resolvedPath string) string {
	if !hasAnchorSegment(canonicalPath) {
		return canonicalPath
	}
	canonical := strings.Split(canonicalPath, ".")
	resolved := strings.Split(resolvedPath, ".")
	if len(canonical) != len(resolved) {
		return canonicalPath
	}
	for i, segment := range canonical {
		if segmentIsAnchor(segment) {
			canonical[i] = resolved[i]
		}
	}
	return strings.Join(canonical, ".")
}

// arrayElementsAtResolved returns the elements of the list at a path whose segments are all
// resolved already, or nil.
func arrayElementsAtResolved(doc *gaby.YamlDoc, segments []string) []*gaby.YamlDoc {
	if len(segments) == 0 {
		return doc.Children()
	}
	array, found, err := YamlSafePathGetDoc(doc, api.ResolvedPath(strings.Join(segments, ".")), true)
	if err != nil || !found || array == nil || !array.IsArray() {
		return nil
	}
	return array.Children()
}

// storedPathMayBePositional reports whether a stored key could name an element of a list by
// content or by position -- an anchored segment or a bare index -- which is all re-anchoring
// has to look at. A numeric segment may as well be a key of a mapping; the data says which.
func storedPathMayBePositional(path string) bool {
	for _, segment := range strings.Split(path, ".") {
		if segmentIsAnchor(segment) {
			return true
		}
		if _, err := strconv.Atoi(segment); err == nil {
			return true
		}
	}
	return false
}

// hasAnchorSegment reports whether a path names some element by content. A "~" alone does not
// say so: it is also how a dot in a key is escaped (app.kubernetes.io/name is app~1kubernetes~1io).
func hasAnchorSegment(path string) bool {
	if !strings.Contains(path, "?") {
		return false
	}
	for _, segment := range strings.Split(path, ".") {
		if segmentIsAnchor(segment) {
			return true
		}
	}
	return false
}

// reanchorOutcome is what becomes of one stored key when its record is re-anchored.
type reanchorOutcome int

const (
	// reanchorKept: the key needs no change, either because it names no element by content or
	// position, or because it could not be followed and is left as it was.
	reanchorKept reanchorOutcome = iota
	// reanchorMoved: the key is rewritten.
	reanchorMoved
	// reanchorDropped: the key names something inside an element the write removed.
	reanchorDropped
)

// storedPathReanchorer follows the stored keys of one resource across one write, from before, the
// resource's document as the write found it, to after, the document it left. before is nil for a
// resource the write created.
type storedPathReanchorer struct {
	before, after  *gaby.YamlDoc
	mergeKeyLookup MergeKeyLookup
	// alignments caches the alignment of each list, keyed by its resolved paths on both sides.
	alignments map[[2]string]listAlignment
}

// listAlignment is how a write moved the elements of one list: where each element before it that
// survived sits after it, and which elements after it the write inserted.
type listAlignment struct {
	moved    map[int]int
	inserted map[int]bool
}

func newStoredPathReanchorer(before, after *gaby.YamlDoc, mergeKeyLookup MergeKeyLookup) *storedPathReanchorer {
	return &storedPathReanchorer{before: before, after: after, mergeKeyLookup: mergeKeyLookup,
		alignments: map[[2]string]listAlignment{}}
}

// reanchor follows one stored key, of an entry recording a change of mutationType, or of none for
// a key that records no change, as a guard's does.
//
// An entry recording the removal of the element its last segment names says all that can be said
// of an element that is gone, so such a key is kept as it is. An entry recording the insertion of
// the element its last segment names is the one key that names its element as the write made it,
// not as the write found it (AnchoredPathSegment, without context): such a segment is looked for
// among the elements the write inserted, since one identical to an element already in the list
// would otherwise be found there and the insertion recorded on the element that did not move.
func (r *storedPathReanchorer) reanchor(path string, mutationType api.MutationType) (string, reanchorOutcome) {
	recordsRemoval := mutationType == api.MutationTypeDelete
	if r.after == nil || !storedPathMayBePositional(path) {
		return path, reanchorKept
	}
	segments := strings.Split(path, ".")
	// The path as it resolves on each side, and the path to store.
	before := make([]string, 0, len(segments))
	after := make([]string, 0, len(segments))
	out := make([]string, 0, len(segments))
	beforeValid := r.before != nil
	changed := false
	for i, segment := range segments {
		last := i == len(segments)-1
		afterElements, afterIsList := r.listAt(r.after, after)
		var beforeElements []*gaby.YamlDoc
		if beforeValid {
			beforeElements, _ = r.listAt(r.before, before)
		}
		anchored := segmentIsAnchor(segment)
		_, numericErr := strconv.Atoi(segment)
		bareIndex := numericErr == nil && afterIsList
		if bareIndex && r.mergeKeyLookup != nil {
			// A bare index into a merge-keyed list is named by merge key where a record is read
			// (CanonicalizeStoredMutationPaths), when the element has one; one that has none is
			// left as the merge engine reads it.
			if _, declared := r.mergeKeyLookup(strings.Join(segments[:i], ".")); declared {
				bareIndex = false
			}
		}
		if strings.HasPrefix(segment, "?") && !anchored {
			// A merge key names its element exactly on both sides; only the path to it moves.
			afterIndex, ok := resolvedElementIndex(r.after, strings.Join(append(append([]string{}, after...), segment), "."))
			if !ok {
				return path, reanchorKept
			}
			if beforeValid {
				if beforeIndex, ok := resolvedElementIndex(r.before, strings.Join(append(append([]string{}, before...), segment), ".")); ok {
					before = append(before, beforeIndex)
				} else {
					beforeValid = false
				}
			}
			after = append(after, afterIndex)
			out = append(out, segment)
			continue
		}
		if !anchored && !bareIndex {
			before = append(before, segment)
			after = append(after, segment)
			out = append(out, segment)
			continue
		}

		// An element of a list with no merge key, named by content or by position. Where it sat
		// before the write, followed through the write's alignment of the list, is where it sits
		// now; an element the write inserted is found in the data it made.
		afterIndex := -1
		beforeIndex := -1
		if last && anchored && mutationType == api.MutationTypeAdd && !anchorHasContext(segment) && beforeValid {
			inserted := r.alignment(before, after, beforeElements, afterElements).inserted
			afterIndex = storedAnchorElementAmong(afterElements, segment, inserted)
			if afterIndex >= 0 {
				anchor := string(CanonicalMutationPath(api.ResolvedPath(AnchoredPathSegment(afterElements, afterIndex, true))))
				out = append(out, anchor)
				return strings.Join(out, "."), reanchorMoved
			}
		}
		if beforeValid {
			if anchored {
				beforeIndex = storedAnchorElement(beforeElements, segment)
			} else if index := atoiOr(segment, -1); index >= 0 && index < len(beforeElements) {
				beforeIndex = index
			}
		}
		if beforeIndex >= 0 {
			if mapped, ok := r.alignment(before, after, beforeElements, afterElements).moved[beforeIndex]; ok {
				afterIndex = mapped
			}
		} else if anchored {
			afterIndex = storedAnchorElement(afterElements, segment)
		} else if !beforeValid {
			// Nothing to follow it from: the resource is new, so the index is into what the
			// write made.
			if index := atoiOr(segment, -1); index >= 0 && index < len(afterElements) {
				afterIndex = index
			}
		}
		if afterIndex < 0 {
			if beforeIndex >= 0 && !(last && recordsRemoval) {
				// The write removed the element; what the key says about it is moot.
				return path, reanchorDropped
			}
			return path, reanchorKept
		}
		anchor := string(CanonicalMutationPath(api.ResolvedPath(AnchoredPathSegment(afterElements, afterIndex, true))))
		if anchor != segment {
			changed = true
		}
		if beforeIndex >= 0 {
			before = append(before, strconv.Itoa(beforeIndex))
		} else {
			beforeValid = false
		}
		after = append(after, strconv.Itoa(afterIndex))
		out = append(out, anchor)
	}
	if !changed {
		return path, reanchorKept
	}
	return strings.Join(out, "."), reanchorMoved
}

// listAt returns the elements of the list at a resolved path of doc, and whether there is a list
// there.
func (r *storedPathReanchorer) listAt(doc *gaby.YamlDoc, segments []string) ([]*gaby.YamlDoc, bool) {
	var node *gaby.YamlDoc
	if len(segments) == 0 {
		node = doc
	} else {
		found := false
		var err error
		node, found, err = YamlSafePathGetDoc(doc, api.ResolvedPath(strings.Join(segments, ".")), true)
		if err != nil || !found {
			return nil, false
		}
	}
	if node == nil || !node.IsArray() {
		return nil, false
	}
	return node.Children(), true
}

// alignment is how the write moved the elements of one list, by the same alignment the write's
// own diff records the list's changes with.
func (r *storedPathReanchorer) alignment(before, after []string, beforeElements, afterElements []*gaby.YamlDoc) listAlignment {
	key := [2]string{strings.Join(before, "."), strings.Join(after, ".")}
	if alignment, cached := r.alignments[key]; cached {
		return alignment
	}
	aligned := alignArrayElements(beforeElements, afterElements, key[1], r.mergeKeyLookup)
	alignment := listAlignment{moved: make(map[int]int, len(aligned.pairs)), inserted: make(map[int]bool, len(aligned.added))}
	for _, pair := range aligned.pairs {
		alignment.moved[pair[0]] = pair[1]
	}
	for _, index := range aligned.added {
		alignment.inserted[index] = true
	}
	r.alignments[key] = alignment
	return alignment
}

// anchorHasContext reports whether an anchored segment records the list around its element, which
// every anchor does except an insertion's.
func anchorHasContext(segment string) bool {
	pairs := strings.TrimPrefix(segment, "?")
	if rest, _, found := strings.Cut(pairs, ";@"); found {
		pairs = rest
	}
	keys, values, ok := associativeSegmentPairs(pairs)
	if !ok {
		return false
	}
	_, _, context := splitAnchorContext(keys, values)
	return context.present
}

// storedAnchorElementAmong is storedAnchorElement restricted to the elements at the given indices.
func storedAnchorElementAmong(elements []*gaby.YamlDoc, segment string, among map[int]bool) int {
	candidates := make([]*gaby.YamlDoc, len(elements))
	for index := range among {
		if index >= 0 && index < len(elements) {
			candidates[index] = elements[index]
		}
	}
	pairs := strings.TrimPrefix(segment, "?")
	if rest, _, found := strings.Cut(pairs, ";@"); found {
		pairs = rest
	}
	keys, values, ok := associativeSegmentPairs(pairs)
	if !ok {
		return -1
	}
	contentKeys, contentValues, _ := splitAnchorContext(keys, values)
	match := -1
	for index, element := range candidates {
		if element == nil || !elementMatchesPairs(element, contentKeys, contentValues) {
			continue
		}
		if match >= 0 {
			return -1
		}
		match = index
	}
	return match
}

func atoiOr(s string, fallback int) int {
	if n, err := strconv.Atoi(s); err == nil {
		return n
	}
	return fallback
}

// ReanchorStoredPaths keeps a stored record describing the data it is stored with: it rewrites
// every key of mutations that names an element of a list with no merge key, by content or by bare
// index, as the anchor of that element in after, following it there from before. before is the
// data the write found, after the data it made, and mutations the record the write is storing --
// keys carried over from the previous record, and keys the write's own changes added, all of which
// resolve in before, except an insertion's, which resolves in after.
//
// A key inside an element the write removed is dropped: the element it describes is gone, and an
// entry left behind is read as being about whatever takes the element's place. An entry that
// records the removal of the element itself is kept as it is. A key that cannot be followed --
// written against data that is neither, by a chain of functions each diffing its own input -- is
// kept as it was. Two keys that land on one are folded, as foldMutationInfo folds them.
//
// It returns the record unchanged, and false, when there is nothing to rewrite.
func ReanchorStoredPaths(mutations api.ResourceMutationList, before, after gaby.Container,
	resourceProvider ResourceProvider) (api.ResourceMutationList, bool) {
	if len(mutations) == 0 || resourceProvider == nil {
		return mutations, false
	}
	var rewritten api.ResourceMutationList
	for i := range mutations {
		pathMap, changed := reanchorResourcePaths(&mutations[i], before, after, resourceProvider)
		if !changed {
			continue
		}
		if rewritten == nil {
			rewritten = make(api.ResourceMutationList, len(mutations))
			copy(rewritten, mutations)
		}
		rewritten[i].PathMutationMap = pathMap
	}
	if rewritten == nil {
		return mutations, false
	}
	return rewritten, true
}

func reanchorResourcePaths(resourceMutation *api.ResourceMutation, before, after gaby.Container,
	resourceProvider ResourceProvider) (api.MutationMap, bool) {
	needed := false
	for path := range resourceMutation.PathMutationMap {
		if storedPathMayBePositional(string(path)) {
			needed = true
			break
		}
	}
	if !needed {
		return nil, false
	}
	reanchorer := resourceReanchorer(&resourceMutation.Resource, before, after, resourceProvider)
	if reanchorer == nil {
		return nil, false
	}
	rewritten := make(api.MutationMap, len(resourceMutation.PathMutationMap))
	changed := false
	for _, entry := range api.SortedMutationMapEntries(resourceMutation.PathMutationMap) {
		info := *entry.MutationInfo
		path, outcome := reanchorer.reanchor(string(entry.Path), info.MutationType)
		switch outcome {
		case reanchorDropped:
			changed = true
			continue
		case reanchorMoved:
			changed = true
		case reanchorKept:
		}
		key := api.ResolvedPath(path)
		if existing, found := rewritten[key]; found {
			info = foldMutationInfo(existing, info)
			changed = true
		}
		rewritten[key] = info
	}
	return rewritten, changed
}

// ReanchoredKeys returns, for each path of a resource's PathMutationMap that a write from before to
// after moves, the key ReanchorStoredPaths would store it under. A path the write leaves as it is,
// or whose element it removed, has no entry.
//
// It is how a diff of a write is matched against the record the write stored: the diff names an
// element of a list with no merge key as the write found it, and the record as the write left it,
// so the two agree as text only when the write did not change the element.
func ReanchoredKeys(resourceMutation *api.ResourceMutation, before, after gaby.Container,
	resourceProvider ResourceProvider) map[api.ResolvedPath]api.ResolvedPath {
	if resourceProvider == nil {
		return nil
	}
	var reanchorer *storedPathReanchorer
	var keys map[api.ResolvedPath]api.ResolvedPath
	for path, info := range resourceMutation.PathMutationMap {
		if !storedPathMayBePositional(string(path)) {
			continue
		}
		if reanchorer == nil {
			if reanchorer = resourceReanchorer(&resourceMutation.Resource, before, after, resourceProvider); reanchorer == nil {
				return nil
			}
		}
		if key, outcome := reanchorer.reanchor(string(path), info.MutationType); outcome == reanchorMoved {
			if keys == nil {
				keys = map[api.ResolvedPath]api.ResolvedPath{}
			}
			keys[path] = api.ResolvedPath(key)
		}
	}
	return keys
}

// resourceReanchorer finds a resource's documents on both sides of a write, or nil when the write
// left no document for it.
func resourceReanchorer(resource *api.ResourceInfo, before, after gaby.Container,
	resourceProvider ResourceProvider) *storedPathReanchorer {
	afterDoc, afterInfo := FindResourceDoc(after, resourceProvider, resource)
	if afterDoc == nil {
		return nil
	}
	var beforeDoc *gaby.YamlDoc
	if len(before) > 0 {
		beforeDoc, _ = FindResourceDoc(before, resourceProvider, resource)
	}
	resourceType := afterInfo.ResourceType
	mergeKeyLookup := MergeKeyLookup(func(path string) ([]string, bool) {
		return resourceProvider.MergeKeysForPath(resourceType, path)
	})
	return newStoredPathReanchorer(beforeDoc, afterDoc, mergeKeyLookup)
}

// ReanchorPathAnnotations keeps a guard table describing the data it is stored with, as
// ReanchorStoredPaths keeps a record: a guard on a field of an element of a list with no merge key
// follows the element. A guard inside an element the write removed is dropped with it. Two keys
// that land on one keep the annotations of both, the later key's winning where they name the same
// annotation.
func ReanchorPathAnnotations(annotations api.PathAnnotationList, before, after gaby.Container,
	resourceProvider ResourceProvider) (api.PathAnnotationList, bool) {
	if len(annotations) == 0 || resourceProvider == nil {
		return annotations, false
	}
	var rewritten api.PathAnnotationList
	for i := range annotations {
		table, changed := reanchorAnnotationTable(&annotations[i], before, after, resourceProvider)
		if !changed {
			continue
		}
		if rewritten == nil {
			rewritten = make(api.PathAnnotationList, len(annotations))
			copy(rewritten, annotations)
		}
		rewritten[i].PathAnnotationMap = table
	}
	if rewritten == nil {
		return annotations, false
	}
	return rewritten, true
}

func reanchorAnnotationTable(resource *api.ResourcePathAnnotations, before, after gaby.Container,
	resourceProvider ResourceProvider) (map[api.ResolvedPath]api.PathAnnotations, bool) {
	needed := false
	for path := range resource.PathAnnotationMap {
		if storedPathMayBePositional(string(path)) {
			needed = true
			break
		}
	}
	if !needed {
		return nil, false
	}
	reanchorer := resourceReanchorer(&resource.Resource, before, after, resourceProvider)
	if reanchorer == nil {
		return nil, false
	}
	paths := make([]string, 0, len(resource.PathAnnotationMap))
	for path := range resource.PathAnnotationMap {
		paths = append(paths, string(path))
	}
	slices.Sort(paths)
	rewritten := make(map[api.ResolvedPath]api.PathAnnotations, len(resource.PathAnnotationMap))
	changed := false
	for _, path := range paths {
		annotations := resource.PathAnnotationMap[api.ResolvedPath(path)]
		moved, outcome := reanchorer.reanchor(path, "")
		switch outcome {
		case reanchorDropped:
			changed = true
			continue
		case reanchorMoved:
			changed = true
		case reanchorKept:
		}
		key := api.ResolvedPath(moved)
		if existing, found := rewritten[key]; found {
			annotations = mergePathAnnotations(existing, annotations)
			changed = true
		}
		rewritten[key] = annotations
	}
	return rewritten, changed
}

// mergePathAnnotations combines the annotations of two keys that name one path, later's winning
// where both name the same annotation.
func mergePathAnnotations(earlier, later api.PathAnnotations) api.PathAnnotations {
	merged := api.PathAnnotations{}
	for _, source := range []api.PathAnnotations{earlier, later} {
		for kind, entries := range source {
			if merged[kind] == nil {
				merged[kind] = map[string]string{}
			}
			for key, value := range entries {
				merged[kind][key] = value
			}
		}
	}
	return merged
}

// addPositionalKeys enters each key of a canonical protection map that names an element by
// content under its positional form as well, resolved against doc, the target the map describes.
// A positional form another key already has folds into it as foldMutationInfo folds, so a
// Protected on either survives.
func addPositionalKeys(doc *gaby.YamlDoc, protection api.MutationMap) {
	var anchored []api.ResolvedPath
	for path := range protection {
		if hasAnchorSegment(string(path)) {
			anchored = append(anchored, path)
		}
	}
	slices.Sort(anchored)
	for _, path := range anchored {
		positional, ok := ResolveStoredAnchors(doc, string(path))
		if !ok || positional == string(path) {
			continue
		}
		info := protection[path]
		if existing, found := protection[api.ResolvedPath(positional)]; found {
			info = foldMutationInfo(existing, info)
		}
		protection[api.ResolvedPath(positional)] = info
	}
}

// withPositionalGuardKeys returns the guard table with each key that names an element by content
// entered under its positional form as well, resolved against doc, as addPositionalKeys does for
// protection. The table is not modified; one with nothing to add is returned as it is.
func withPositionalGuardKeys(doc *gaby.YamlDoc, guards *api.ResourcePathAnnotations) *api.ResourcePathAnnotations {
	if guards == nil {
		return nil
	}
	var anchored []string
	for path := range guards.PathAnnotationMap {
		if hasAnchorSegment(string(path)) {
			anchored = append(anchored, string(path))
		}
	}
	if len(anchored) == 0 {
		return guards
	}
	slices.Sort(anchored)
	augmented := *guards
	augmented.PathAnnotationMap = make(map[api.ResolvedPath]api.PathAnnotations, len(guards.PathAnnotationMap)+len(anchored))
	for path, annotations := range guards.PathAnnotationMap {
		augmented.PathAnnotationMap[path] = annotations
	}
	for _, path := range anchored {
		positional, ok := ResolveStoredAnchors(doc, path)
		if !ok || positional == path {
			continue
		}
		annotations := guards.PathAnnotationMap[api.ResolvedPath(path)]
		if existing, found := augmented.PathAnnotationMap[api.ResolvedPath(positional)]; found {
			annotations = mergePathAnnotations(existing, annotations)
		}
		augmented.PathAnnotationMap[api.ResolvedPath(positional)] = annotations
	}
	return &augmented
}

// guardsAt returns the guards in force at a path, found by its canonical form and by its
// positional form.
func guardsAt(guards *api.ResourcePathAnnotations, canonicalPath, positionalPath api.ResolvedPath) map[string]string {
	found := guards.GuardsForPath(canonicalPath)
	if positionalPath == canonicalPath {
		return found
	}
	byPosition := guards.GuardsForPath(positionalPath)
	if len(byPosition) == 0 {
		return found
	}
	merged := make(map[string]string, len(found)+len(byPosition))
	for key, value := range byPosition {
		merged[key] = value
	}
	for key, value := range found {
		merged[key] = value
	}
	return merged
}

// StoredPathsMayNeedReanchoring reports whether a record or a guard table holds a key that names
// an element of a list by content or by position, without the data -- the question a caller
// holding unparsed configuration asks before parsing it.
func StoredPathsMayNeedReanchoring(mutations api.ResourceMutationList, annotations api.PathAnnotationList) bool {
	for i := range mutations {
		for path := range mutations[i].PathMutationMap {
			if storedPathMayBePositional(string(path)) {
				return true
			}
		}
	}
	for i := range annotations {
		for path := range annotations[i].PathAnnotationMap {
			if storedPathMayBePositional(string(path)) {
				return true
			}
		}
	}
	return false
}

// StoredFormOf returns a path as a stored record keys it in doc, the data the record describes:
// each bare index into a list with no merge key written as the anchor of the element there. It is
// for a path a person typed, by position, which is how a list with no merge key is addressed
// from outside, to be stored or looked up among keys that name such elements by content.
func StoredFormOf(doc *gaby.YamlDoc, path api.ResolvedPath, mergeKeyLookup MergeKeyLookup) api.ResolvedPath {
	stored, outcome := newStoredPathReanchorer(nil, doc, mergeKeyLookup).reanchor(string(path), "")
	if outcome != reanchorMoved {
		return path
	}
	return api.ResolvedPath(stored)
}

// storedKeysByPosition maps the positional form of each key of a stored map that names an element
// by content to the key, resolved against doc, the data the map describes.
func storedKeysByPosition(doc *gaby.YamlDoc, pathMap api.MutationMap) map[api.ResolvedPath]api.ResolvedPath {
	byPosition := map[api.ResolvedPath]api.ResolvedPath{}
	if doc == nil {
		return byPosition
	}
	for _, path := range slices.Sorted(maps.Keys(pathMap)) {
		if !hasAnchorSegment(string(path)) {
			continue
		}
		if positional, ok := ResolveStoredAnchors(doc, string(path)); ok {
			if _, taken := byPosition[api.ResolvedPath(positional)]; !taken {
				byPosition[api.ResolvedPath(positional)] = path
			}
		}
	}
	return byPosition
}

// ancestorByPosition finds the closest strict ancestor of a positional path among the positional
// forms of a record's keys, returning the stored key.
func ancestorByPosition(byPosition map[api.ResolvedPath]api.ResolvedPath, path api.ResolvedPath) (api.ResolvedPath, bool) {
	remaining := string(path)
	for {
		dot := strings.LastIndex(remaining, ".")
		if dot < 0 {
			return "", false
		}
		remaining = remaining[:dot]
		if stored, found := byPosition[api.ResolvedPath(remaining)]; found {
			return stored, true
		}
	}
}
