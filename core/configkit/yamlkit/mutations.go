// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package yamlkit

import (
	"fmt"
	"log/slog"
	"math"
	"slices"
	"strconv"
	"strings"

	"github.com/cockroachdb/errors"
	"github.com/confighub/sdk/core/constants"
	"github.com/confighub/sdk/core/function/api"
	"github.com/confighub/sdk/core/third_party/gaby"
	"sigs.k8s.io/kustomize/kyaml/yaml"
)

// ComputeMutations and ComputeMutationsForDocs Overview
//
// ComputeMutations performs a structured diff between two YAML configurations (represented
// as gaby.Container, which is a list of parsed YAML documents). It determines what changed
// and records the result as an api.ResourceMutationList: one entry per resource, each
// carrying a resource-level MutationType, a PathMutationMap of leaf-level changes, and
// alias information so renamed resources can still be matched.
//
// The output is the data form of a "patch" that can be:
//
//  1. Replayed onto a different (or earlier) configuration via PatchMutations as part of a
//     three-way merge (e.g., upgrading a downstream Unit from upstream).
//  2. Accumulated across sequential edits via api.OffsetMutations + AddMutations to record
//     a compiled history of who changed what (used as predicates for selective patching).
//  3. Diffed against another mutation set via SubtractMutations / PatchMutations'
//     mutationsToSubtract argument so target-side changes survive the patch.
//
// Inputs:
//   - previousParsedData / modifiedParsedData: the "before" / "after" parsed YAML docs.
//   - functionIndex: a sequence number identifying which operation produced this diff.
//   - resourceProvider: toolchain-specific interface for extracting resource metadata
//     and (importantly) for declaring which array paths are merge-keyed associative arrays.
//
// Algorithm at a glance:
//
//  1. Resource matching (this function): for each modified doc, find the corresponding
//     previous doc by ResourceMergeID, then by ResourceType+Name, then by fuzzy similarity
//     score (path-mutation count / total lines, with a maxMatchScore threshold). Unmatched
//     modified docs are Adds; unmatched previous docs are Deletes. Both old and new names
//     are recorded in Aliases / AliasesWithoutScopes so subsequent operations can match
//     across renames.
//
//  2. Path-level diff (ComputeMutationsForDocs): for matched resources, do a stack-based
//     deep comparison. Maps are compared by key. Arrays are compared by positional index
//     by default; when the resource provider declares a merge key for the array path,
//     elements are matched by merge-key value and paths use the ?key=value;@index syntax
//     so the per-element mutation can be applied at the target element regardless of its
//     current index.
//
//  3. Resource-level MutationType is then Add (modified-only), Delete (previous-only),
//     Update (matched and path map non-empty), or None (matched, no path changes).
//
// Example:
//
//	previous:                                    modified:
//	  apiVersion: apps/v1                          apiVersion: apps/v1
//	  kind: Deployment                             kind: Deployment
//	  metadata:                                    metadata:
//	    name: myapp                                  name: myapp
//	  spec:                                        spec:
//	    replicas: 1                                  replicas: 3
//	    template:                                    template:
//	      spec:                                        spec:
//	        containers:                                  containers:
//	        - name: app                                  - name: app
//	          image: nginx:1.19                            image: nginx:1.20
//
// With the K8s resource provider (merge key "name" on containers) the result is:
//
//	ResourceMutationList{
//	  {
//	    Resource: {ResourceType: "apps/v1/Deployment", ResourceName: "default/myapp", ...},
//	    ResourceMutationInfo: {MutationType: Update, Index: 1},
//	    PathMutationMap: {
//	      "spec.replicas": {MutationType: Update, Value: "3"},
//	      "spec.template.spec.containers.?name=app;@0.image": {MutationType: Update, Value: "nginx:1.20"},
//	    },
//	    Aliases: {"default/myapp": {}},
//	  },
//	}

// MergeKeyLookup is a function that returns the merge key field name for a given
// array path, if one exists. It is used by ComputeMutationsForDocs to match array
// elements by merge key value instead of positional index.
type MergeKeyLookup func(path string) (string, bool)

// AssociativePathSegment builds a path segment encoding both the merge key value
// and the positional index, using the syntax ?key=value;@index.
// The merge key value is escaped to handle dots.
func AssociativePathSegment(mergeKey string, mergeKeyValue string, index int) string {
	return "?" + EscapeDotsInPathSegment(mergeKey) + "=" + EscapeDotsInPathSegment(mergeKeyValue) + ";@" + strconv.Itoa(index)
}

// ResolveAssociativeSegments resolves ?key=value;@index segments in a path to numeric
// indices by looking up elements in the document. It tries key=value match first; if no
// element matches by merge-key value, it considers the positional index:
//
//   - Out of bounds: the index is used as-is. This preserves Add-as-append semantics
//     (e.g., a new element being appended to an array) and is harmless for Delete since
//     the caller checks existence before deleting.
//   - In bounds, element has no merge-key field: legacy data — fall back positionally.
//   - In bounds, element has a different merge-key value: a different element. The
//     segment is left unresolved so the caller can skip the operation.
//
// Returns the resolved path and a bool that is true only when every associative segment
// was resolved (by merge-key match, by out-of-bounds index, or by legacy fallback).
func ResolveAssociativeSegments(doc *gaby.YamlDoc, path string) (string, bool) {
	if !strings.Contains(path, "?") {
		return path, true
	}
	segments := gaby.DotPathToSlice(path)
	var resolvedSegments []string
	currentNode := doc
	allResolved := true
	for _, segment := range segments {
		kv, isAssociative := strings.CutPrefix(segment, "?")
		if !isAssociative {
			resolvedSegments = append(resolvedSegments, EscapeDotsInPathSegment(segment))
			if currentNode != nil {
				currentNode = currentNode.S(segment)
			}
			continue
		}
		// Parse ?key=value or ?key=value;@index
		key, value, ok := strings.Cut(kv, "=")
		if !ok {
			// Invalid, keep as-is
			resolvedSegments = append(resolvedSegments, EscapeDotsInPathSegment(segment))
			currentNode = nil
			allResolved = false
			continue
		}
		fallbackIndex := ""
		if v, idx, ok := strings.Cut(value, ";@"); ok {
			value = v
			fallbackIndex = idx
		}

		resolved := false
		if currentNode != nil {
			elements := currentNode.Children()
			// Try key=value match
			for index, child := range elements {
				fieldValueNode := child.S(key)
				if fieldValueNode != nil && fmt.Sprintf("%v", fieldValueNode.Data()) == value {
					indexStr := strconv.Itoa(index)
					resolvedSegments = append(resolvedSegments, indexStr)
					currentNode = child
					resolved = true
					break
				}
			}
			// Fall back to the positional index when there is no key match. If the index
			// is out of bounds (appending), use it as-is. If the element at the index
			// has no value for the merge key, treat it as legacy data and fall back
			// positionally. Otherwise the in-bounds element has a different merge-key
			// value — it's a different element, so leave the segment unresolved.
			if !resolved && fallbackIndex != "" {
				idx, err := strconv.Atoi(fallbackIndex)
				if err == nil && idx >= 0 {
					if idx >= len(elements) {
						resolvedSegments = append(resolvedSegments, fallbackIndex)
						currentNode = nil
						resolved = true
					} else if elements[idx].S(key) == nil {
						resolvedSegments = append(resolvedSegments, fallbackIndex)
						currentNode = elements[idx]
						resolved = true
					}
				}
			}
		}
		if !resolved {
			// Couldn't resolve. Keep the segment as-is so callers can detect the
			// unresolved state and skip the operation.
			resolvedSegments = append(resolvedSegments, EscapeDotsInPathSegment(segment))
			currentNode = nil
			allResolved = false
		}
	}
	return strings.Join(resolvedSegments, "."), allResolved
}

// mergeArrayOrderMaps merges source-side and target-side ArrayOrderMap entries,
// for each path that source has, by weaving the two desired sequences together.
// Paths only in target are dropped (target's reorder for an array source didn't
// touch is already in place; no patch reorder is needed). See mergeArrayOrders
// for the per-array merge semantics.
//
// sourceArrayElementAliases (a sourceParentPath -> {oldKey -> newKey} map)
// translates target-side keys to source-side keys before merging when source
// renamed an element: target's diff (computed against the merge base) uses
// the previous merge-key value for that element, while source's ArrayOrders
// entry uses the new key. Translating target's order brings them into the
// same key namespace so common elements line up correctly during the merge.
func mergeArrayOrderMaps(sourceArrayOrders, targetArrayOrders api.ArrayOrderMap, sourceArrayElementAliases api.ArrayElementAliasMap) api.ArrayOrderMap {
	if len(sourceArrayOrders) == 0 {
		return nil
	}
	if len(targetArrayOrders) == 0 && len(sourceArrayElementAliases) == 0 {
		return sourceArrayOrders
	}
	result := make(api.ArrayOrderMap, len(sourceArrayOrders))
	for path, sourceOrder := range sourceArrayOrders {
		targetOrder := targetArrayOrders[path]
		if pathAliases, ok := sourceArrayElementAliases[path]; ok && len(pathAliases) > 0 && len(targetOrder) > 0 {
			translated := make([]string, len(targetOrder))
			for i, k := range targetOrder {
				if newKey, ok := pathAliases[k]; ok {
					translated[i] = newKey
				} else {
					translated[i] = k
				}
			}
			targetOrder = translated
		}
		if len(targetOrder) == 0 {
			result[path] = sourceOrder
			continue
		}
		result[path] = mergeArrayOrders(sourceOrder, targetOrder)
	}
	return result
}

// mergeArrayOrders combines source's and target's desired sequences for a single
// merge-keyed array path. The result threads the two so:
//
//   - Common elements (present in both sequences by merge-key value) form a
//     spine in source's order. If source and target disagree about the relative
//     order of common elements, source wins (it's the patch's intent).
//   - Source-only elements (added by source's diff, not in target) keep their
//     position relative to source's spine: each is emitted right after its
//     preceding common element from source's view.
//   - Target-only elements (added by target's diff, not in source) keep their
//     position relative to source's spine using their preceding common element
//     from target's view: each is emitted right after that common.
//   - At each common anchor, source-only elements emit before target-only
//     elements (source has explicit intent to add at that position; target's
//     element predates the patch).
//   - Front-of-array (no preceding common): source-only first, then target-only.
//
// This is the LCS-style merge described in the positional-arrays plan: the
// common subsequence is the LCS picked in source's order; insertions on either
// side are attached to their preceding LCS anchor.
func mergeArrayOrders(sourceOrder, targetOrder []string) []string {
	if len(sourceOrder) == 0 {
		return targetOrder
	}
	if len(targetOrder) == 0 {
		return sourceOrder
	}
	sourceSet := make(map[string]bool, len(sourceOrder))
	for _, k := range sourceOrder {
		sourceSet[k] = true
	}
	targetSet := make(map[string]bool, len(targetOrder))
	for _, k := range targetOrder {
		targetSet[k] = true
	}

	// Bucket source-only keys by their preceding common in source's order.
	// Empty key = "before the first common".
	sourceOnlyAfter := make(map[string][]string)
	var lastCommon string
	for _, s := range sourceOrder {
		if sourceSet[s] && targetSet[s] {
			lastCommon = s
		} else {
			sourceOnlyAfter[lastCommon] = append(sourceOnlyAfter[lastCommon], s)
		}
	}
	// Bucket target-only keys by their preceding common in target's order.
	targetOnlyAfter := make(map[string][]string)
	lastCommon = ""
	for _, t := range targetOrder {
		if sourceSet[t] && targetSet[t] {
			lastCommon = t
		} else if !sourceSet[t] {
			targetOnlyAfter[lastCommon] = append(targetOnlyAfter[lastCommon], t)
		}
	}

	// Emit. Front first, then walk source over commons.
	result := make([]string, 0, len(sourceOrder)+len(targetOrder))
	result = append(result, sourceOnlyAfter[""]...)
	result = append(result, targetOnlyAfter[""]...)
	for _, s := range sourceOrder {
		if !(sourceSet[s] && targetSet[s]) {
			continue // source-only, already emitted via the bucket
		}
		result = append(result, s)
		result = append(result, sourceOnlyAfter[s]...)
		result = append(result, targetOnlyAfter[s]...)
	}
	return result
}

// reorderArrayByMergeKey rearranges the elements of a SequenceNode at the given
// path inside doc so they match desiredOrder by merge-key value. Elements whose
// merge-key value is in desiredOrder are emitted in that order, followed by
// elements not in desiredOrder (in their original relative order). Elements
// without a merge-key value are also kept in their original relative order
// after the keyed ones.
//
// path may contain associative segments — they're resolved against doc first.
// If the path doesn't resolve to a SequenceNode, this is a no-op.
func reorderArrayByMergeKey(doc *gaby.YamlDoc, path string, mergeKey string, desiredOrder []string) {
	if doc == nil || mergeKey == "" || len(desiredOrder) == 0 {
		return
	}
	resolvedPath, ok := ResolveAssociativeSegments(doc, path)
	if !ok {
		return
	}
	arrayDoc := doc.Path(resolvedPath)
	if arrayDoc == nil {
		return
	}
	node := arrayDoc.YNode()
	if node == nil || node.Kind != yaml.SequenceNode {
		return
	}
	content := node.Content
	if len(content) <= 1 {
		return
	}

	// Build merge-key value -> index for current elements. Elements without a
	// merge-key field map to the empty string, but we don't include those in
	// the lookup map (they're always treated as not-in-desired).
	keyToIndex := make(map[string]int, len(content))
	for i, elem := range content {
		if elem.Kind != yaml.MappingNode {
			continue
		}
		for j := 0; j < len(elem.Content)-1; j += 2 {
			k, v := elem.Content[j], elem.Content[j+1]
			if k.Kind == yaml.ScalarNode && k.Value == mergeKey && v.Kind == yaml.ScalarNode {
				keyToIndex[v.Value] = i
				break
			}
		}
	}

	used := make([]bool, len(content))
	reordered := make([]*yaml.Node, 0, len(content))
	for _, key := range desiredOrder {
		if idx, found := keyToIndex[key]; found && !used[idx] {
			reordered = append(reordered, content[idx])
			used[idx] = true
		}
	}
	for i, elem := range content {
		if !used[i] {
			reordered = append(reordered, elem)
		}
	}
	node.Content = reordered
}

// mutationCost returns the cost (in leaf-value units) of a single mutation.
// A path mutation with no Value or with a scalar Value contributes 1; one
// whose Value is a YAML mapping or sequence contributes the number of leaf
// scalars inside the subtree, since deleting/adding a whole container or
// a whole resources block represents many field-level changes even though
// it shows up as a single entry in PathMutationMap.
func mutationCost(info api.MutationInfo) int {
	if info.Value == "" {
		return 1
	}
	parsed, err := gaby.ParseYAML([]byte(info.Value))
	if err != nil || parsed == nil {
		return 1
	}
	n := countLeafNodes(parsed.YNode())
	if n < 1 {
		return 1
	}
	return n
}

// mutationMapCost sums mutationCost over every entry in the map.
func mutationMapCost(m api.MutationMap) int {
	total := 0
	for _, info := range m {
		total += mutationCost(info)
	}
	return total
}

// countLeafNodes returns the number of scalar leaves reachable from node by
// recursively descending into mappings and sequences. A scalar contributes 1.
func countLeafNodes(node *yaml.Node) int {
	if node == nil {
		return 0
	}
	switch node.Kind {
	case yaml.DocumentNode:
		total := 0
		for _, c := range node.Content {
			total += countLeafNodes(c)
		}
		return total
	case yaml.MappingNode:
		total := 0
		// Mapping content alternates key, value; count value subtrees only.
		for i := 1; i < len(node.Content); i += 2 {
			total += countLeafNodes(node.Content[i])
		}
		return total
	case yaml.SequenceNode:
		total := 0
		for _, c := range node.Content {
			total += countLeafNodes(c)
		}
		return total
	case yaml.ScalarNode, yaml.AliasNode:
		return 1
	}
	return 0
}

// appendPathForAdd returns a numeric path that appends to the parent array of an
// associative path whose trailing segment couldn't be matched by merge-key value.
// Used by applyPathMutations when an Add's merge-key path doesn't match any
// existing element and the element at the fallback index has a different
// merge-key value: rather than overwriting that unrelated element, the new
// element is appended to the array (index = len(elements)).
//
// Returns the rewritten path and true on success. Returns false if the path's
// last segment is not associative or the parent path can't be resolved to an
// array in the current doc.
func appendPathForAdd(doc *gaby.YamlDoc, path string) (string, bool) {
	segments := gaby.DotPathToSlice(path)
	if len(segments) == 0 {
		return "", false
	}
	last := segments[len(segments)-1]
	if !strings.HasPrefix(last, "?") {
		return "", false
	}
	parentSegments := segments[:len(segments)-1]
	parentPath := JoinPathSegments(parentSegments)
	parentNode := doc.Path(parentPath)
	if parentNode == nil {
		return "", false
	}
	children := parentNode.Children()
	if children == nil {
		return "", false
	}
	parentSegments = append(parentSegments, strconv.Itoa(len(children)))
	return JoinPathSegments(parentSegments), true
}

// StripAssociativeSegments converts ?key=value;@index segments to just the numeric index.
// For ?key=@index (direct index), extracts just the index.
// Non-associative segments are passed through as-is.
func StripAssociativeSegments(path string) string {
	if !strings.Contains(path, "?") {
		return path
	}
	segments := gaby.DotPathToSlice(path)
	for i, segment := range segments {
		kv, isAssociative := strings.CutPrefix(segment, "?")
		if !isAssociative {
			continue
		}
		_, value, ok := strings.Cut(kv, "=")
		if !ok {
			continue
		}
		if idx, isDirect := strings.CutPrefix(value, "@"); isDirect {
			// ?key=@index -> index
			segments[i] = idx
		} else if _, idx, ok := strings.Cut(value, ";@"); ok {
			// ?key=value;@index -> index
			segments[i] = idx
		}
	}
	return JoinPathSegments(segments)
}

// MergeKeyEntry represents a merge key/value pair extracted from an associative path segment.
type MergeKeyEntry struct {
	Key   string // merge key field name (e.g., "name")
	Value string // merge key value (e.g., "config")
}

// ExtractMergeKeysFromPath extracts merge key/value pairs from associative path segments.
// Path segments of the form ?key=value;@index yield {Key: key, Value: value}.
func ExtractMergeKeysFromPath(path string) []MergeKeyEntry {
	if !strings.Contains(path, "?") {
		return nil
	}
	segments := gaby.DotPathToSlice(path)
	var entries []MergeKeyEntry
	for _, segment := range segments {
		kv, isAssociative := strings.CutPrefix(segment, "?")
		if !isAssociative {
			continue
		}
		key, value, ok := strings.Cut(kv, "=")
		if !ok {
			continue
		}
		if strings.HasPrefix(value, "@") {
			// ?key=@index — direct index, no merge key value
			continue
		}
		if v, _, ok := strings.Cut(value, ";@"); ok {
			value = v
		}
		entries = append(entries, MergeKeyEntry{
			Key:   strings.ReplaceAll(key, "~1", "."),
			Value: strings.ReplaceAll(value, "~1", "."),
		})
	}
	return entries
}

// ComputeMutationsForDocs determines the edits that have been performed to transform the
// previousDoc into modifiedDoc and records them in pathMutationMap (modified in place),
// associated with the provided functionIndex.
//
// mergeKeyLookup, if non-nil, is called with array paths to determine whether the array is
// merge-keyed associative. If so, elements are matched by merge-key value (not positional
// index) and paths use the ?key=value;@index syntax. The positional index is retained as a
// fallback hint, but PatchMutations only uses it when the element at that index has no
// merge-key field (legacy data) or the index is out of bounds (append).
//
// Design notes:
//
//   - Removed paths are not tombstoned. If an element in the downstream is then modified
//     by upstream, the corresponding path will be present in mutationsPatch and absent
//     from the target's data; the upstream path's child will not be re-added because
//     PatchMutations honors the target's removal via mutationsToSubtract.
//   - The reciprocal case — a field modified downstream while the surrounding block is
//     removed upstream — is reconciled in PatchMutations / SubtractMutations.
//
// TODO: Decide what to do about embedded accessors
//
// arrayOrders, if non-nil, is populated with the desired merge-key sequence for
// every merge-keyed array we descend into whose modified-side order or element
// set differs from the previous side. PatchMutations consumes these to reorder
// the target array after path mutations are applied, so positional associative
// arrays preserve source-side ordering.
//
// arrayElementAliases, if non-nil, is populated with element-level renames
// detected inside merge-keyed arrays. When an unmatched modified element and
// an unmatched previous element are similar enough, the pair is treated as a
// rename: child paths are emitted under the previous merge-key value (so they
// align with target-side paths in SubtractMutations) and the alias is
// recorded so PatchMutations rewrites the merge-key field at apply time.
func ComputeMutationsForDocs(rootPath string, previousDoc *gaby.YamlDoc, modifiedDoc *gaby.YamlDoc, functionIndex int64, pathMutationMap api.MutationMap, mergeKeyLookup MergeKeyLookup, arrayOrders api.ArrayOrderMap, arrayElementAliases api.ArrayElementAliasMap) {
	// Define a traversal item for our stack
	type traversalItem struct {
		path        string
		previousDoc *gaby.YamlDoc
		modifiedDoc *gaby.YamlDoc
	}

	// Initialize the stack with the root traversal item
	stack := []traversalItem{{
		path:        rootPath,
		previousDoc: previousDoc,
		modifiedDoc: modifiedDoc,
	}}

	// Process items until the stack is empty
	for len(stack) > 0 {
		// Pop the top item from the stack
		last := len(stack) - 1
		item := stack[last]
		stack = stack[:last]

		path := item.path
		previousDoc := item.previousDoc
		modifiedDoc := item.modifiedDoc

		// Now process this item (similar logic to the recursive function)
		modifiedChildren := modifiedDoc.ChildrenMap()
		previousChildren := previousDoc.ChildrenMap()

		if len(modifiedChildren) > 0 {
			if len(previousChildren) == 0 {
				// modifiedDoc is a map, but previousDoc is not a map, though it exists.
				// The path's contents have completely changed in this case.
				pathMutationMap[api.ResolvedPath(path)] = api.MutationInfo{
					MutationType: api.MutationTypeUpdate,
					Index:        functionIndex,
					Predicate:    true,
					Value:        modifiedDoc.String(), // new data
				}
				continue // process next stack element
			}

			// Process all modified children
			for key, modifiedChild := range modifiedChildren {
				var currentPath string
				if path != "" {
					currentPath = path + "." + EscapeDotsInPathSegment(key)
				} else {
					currentPath = EscapeDotsInPathSegment(key)
				}

				previousChild, present := previousChildren[key]
				if !present {
					pathMutationMap[api.ResolvedPath(currentPath)] = api.MutationInfo{
						MutationType: api.MutationTypeAdd,
						Index:        functionIndex,
						Predicate:    true,
						Value:        modifiedChild.String(), // new data
					}
					continue // process next stack element
				}

				// Instead of recursion, push this item to the stack
				stack = append(stack, traversalItem{
					path:        currentPath,
					previousDoc: previousChild,
					modifiedDoc: modifiedChild,
				})

				delete(previousChildren, key)
			}

			// Remaining previousChildren must have been deleted
			for key, previousChild := range previousChildren {
				var currentPath string
				if path != "" {
					currentPath = path + "." + EscapeDotsInPathSegment(key)
				} else {
					currentPath = EscapeDotsInPathSegment(key)
				}
				pathMutationMap[api.ResolvedPath(currentPath)] = api.MutationInfo{
					MutationType: api.MutationTypeDelete,
					Index:        functionIndex,
					Predicate:    true,
					Value:        previousChild.String(), // deleted data
				}
			}
		} else if modifiedArrayChildren := modifiedDoc.Children(); modifiedArrayChildren != nil {
			// Compare arrays, treating differences in length as additions and deletions.
			// If a merge key is defined for this array path, match elements by merge key
			// value instead of positional index.
			// We'll also land here in the case of an empty map. Or empty arrays.
			previousArrayChildren := previousDoc.Children()
			if len(modifiedArrayChildren) == 0 && len(previousArrayChildren) == 0 {
				// Both are empty. No changes.
				continue // process next stack element
			}

			if !modifiedDoc.IsArray() {
				// modifiedDoc is an empty map.
				if len(previousChildren) != 0 {
					// The map children were deleted.
					for key, previousChild := range previousChildren {
						var currentPath string
						if path != "" {
							currentPath = path + "." + EscapeDotsInPathSegment(key)
						} else {
							currentPath = EscapeDotsInPathSegment(key)
						}
						pathMutationMap[api.ResolvedPath(currentPath)] = api.MutationInfo{
							MutationType: api.MutationTypeDelete,
							Index:        functionIndex,
							Predicate:    true,
							Value:        previousChild.String(), // deleted data
						}
					}
				} else {
					// The whole path was changed.
					pathMutationMap[api.ResolvedPath(path)] = api.MutationInfo{
						MutationType: api.MutationTypeUpdate,
						Index:        functionIndex,
						Predicate:    true,
						Value:        modifiedDoc.String(), // new data
					}
				}
				continue // process next stack element
			}

			if modifiedDoc.IsArray() && !previousDoc.IsArray() {
				// modifiedDoc is an array, but previousDoc is not an array, though it exists.
				// The path's contents have completely changed in this case.
				pathMutationMap[api.ResolvedPath(path)] = api.MutationInfo{
					MutationType: api.MutationTypeUpdate,
					Index:        functionIndex,
					Predicate:    true,
					Value:        modifiedDoc.String(), // new data
				}
				continue // process next stack element
			}

			// Check if this array has a merge key for associative matching.
			var mergeKey string
			if mergeKeyLookup != nil {
				mergeKey, _ = mergeKeyLookup(path)
			}

			if mergeKey != "" {
				// Associative array matching: match elements by merge key value.
				// Build a map from merge key value -> index for previous elements.
				type previousEntry struct {
					index int
					doc   *gaby.YamlDoc
				}
				type pendingAddEntry struct {
					modifiedIndex int
					modifiedChild *gaby.YamlDoc
					keyValue      string
				}
				var pendingAdds []pendingAddEntry
				previousByKey := make(map[string]previousEntry, len(previousArrayChildren))
				previousKeySeq := make([]string, 0, len(previousArrayChildren))
				for i, child := range previousArrayChildren {
					keyNode := child.S(mergeKey)
					if keyNode != nil {
						keyValue := fmt.Sprintf("%v", keyNode.Data())
						previousByKey[keyValue] = previousEntry{index: i, doc: child}
						previousKeySeq = append(previousKeySeq, keyValue)
					}
				}
				// Build the modified-side merge-key sequence for arrayOrders.
				modifiedKeySeq := make([]string, 0, len(modifiedArrayChildren))
				for _, modifiedChild := range modifiedArrayChildren {
					if keyNode := modifiedChild.S(mergeKey); keyNode != nil {
						modifiedKeySeq = append(modifiedKeySeq, fmt.Sprintf("%v", keyNode.Data()))
					}
				}

				// Track which previous elements were matched.
				previousMatched := make([]bool, len(previousArrayChildren))

				for modifiedIndex, modifiedChild := range modifiedArrayChildren {
					keyNode := modifiedChild.S(mergeKey)
					if keyNode == nil {
						// No merge key value on this element; fall back to positional.
						currentPath := path + "." + strconv.Itoa(modifiedIndex)
						if modifiedIndex >= len(previousArrayChildren) {
							pathMutationMap[api.ResolvedPath(currentPath)] = api.MutationInfo{
								MutationType: api.MutationTypeAdd,
								Index:        functionIndex,
								Predicate:    true,
								Value:        modifiedChild.String(),
							}
						} else if !previousMatched[modifiedIndex] {
							previousMatched[modifiedIndex] = true
							stack = append(stack, traversalItem{
								path:        currentPath,
								previousDoc: previousArrayChildren[modifiedIndex],
								modifiedDoc: modifiedChild,
							})
						}
						continue
					}

					keyValue := fmt.Sprintf("%v", keyNode.Data())
					prev, found := previousByKey[keyValue]
					if found {
						// Matched by merge key. Use ?key=value;@index syntax with the
						// modified index for positional context.
						currentPath := path + "." + AssociativePathSegment(mergeKey, keyValue, modifiedIndex)
						previousMatched[prev.index] = true
						stack = append(stack, traversalItem{
							path:        currentPath,
							previousDoc: prev.doc,
							modifiedDoc: modifiedChild,
						})
					} else {
						// Defer: might be a rename. The rename-detection pass below
						// pairs each unmatched modified element against unmatched
						// previous elements via similarity; truly-unmatched ones
						// fall through to Add emission afterward.
						pendingAdds = append(pendingAdds, pendingAddEntry{
							modifiedIndex: modifiedIndex,
							modifiedChild: modifiedChild,
							keyValue:      keyValue,
						})
					}
				}

				// Rename-detection pass. For each pending Add, find the unmatched
				// previous element that yields the smallest sub-diff. If the
				// similarity score (sub-diff path count / modified element line
				// count) is below the threshold, treat the pair as a rename:
				//   - emit child path mutations under the PREVIOUS merge-key value
				//     (so SubtractMutations aligns source's paths with target's
				//     paths, which still use the previous key);
				//   - record the rename in arrayElementAliases so PatchMutations
				//     can rewrite the element's merge-key field at apply time;
				//   - mark the previous element as matched so it won't emit a
				//     Delete in the unmatched-previous loop below.
				for _, pa := range pendingAdds {
					bestPrevIdx := -1
					bestPrevDiff := math.MaxInt
					var bestPrevPathMap api.MutationMap
					var bestPrevArrayOrders api.ArrayOrderMap
					var bestPrevAliases api.ArrayElementAliasMap
					var bestPrevKeyValue string
					for prevIdx := range previousArrayChildren {
						if previousMatched[prevIdx] {
							continue
						}
						prevChild := previousArrayChildren[prevIdx]
						prevKeyNode := prevChild.S(mergeKey)
						if prevKeyNode == nil {
							continue
						}
						prevKeyValue := fmt.Sprintf("%v", prevKeyNode.Data())
						tmpPathMap := api.MutationMap{}
						tmpArrayOrders := api.ArrayOrderMap{}
						tmpAliases := api.ArrayElementAliasMap{}
						subPath := path + "." + AssociativePathSegment(mergeKey, prevKeyValue, pa.modifiedIndex)
						ComputeMutationsForDocs(subPath, prevChild, pa.modifiedChild, functionIndex, tmpPathMap, mergeKeyLookup, tmpArrayOrders, tmpAliases)
						// Cost is the leaf-value count of the sub-diff so a
						// mutation whose Value is a whole subtree (e.g., an
						// Add/Delete of a container or env-var block) is
						// counted at full weight rather than as one entry.
						cost := mutationMapCost(tmpPathMap)
						if cost < bestPrevDiff {
							bestPrevIdx = prevIdx
							bestPrevDiff = cost
							bestPrevPathMap = tmpPathMap
							bestPrevArrayOrders = tmpArrayOrders
							bestPrevAliases = tmpAliases
							bestPrevKeyValue = prevKeyValue
						}
					}

					accepted := false
					var mergeKeyFieldPath api.ResolvedPath
					if bestPrevIdx >= 0 {
						modLines := strings.Count(pa.modifiedChild.String(), "\n")
						score := float64(bestPrevDiff)
						if modLines > 0 {
							score = float64(bestPrevDiff) / float64(modLines)
						}
						// Tighter than the resource-level threshold (1.0): a
						// rename is "the merge key changed and most everything
						// else is the same". 0.3 lets a pure rename plus a
						// handful of correlated field changes (e.g., args on
						// an init container) pair, but rejects a similar-
						// shaped new element coincidentally landing alongside
						// an unrelated removal.
						const renameScoreThreshold = 0.3
						// Sanity check: the sub-diff must include the merge-
						// key field change itself. If it doesn't, this isn't
						// a rename at all (the merge keys differ but the diff
						// somehow elided the .name field) and we shouldn't
						// pair.
						mergeKeyFieldPath = api.ResolvedPath(
							path + "." + AssociativePathSegment(mergeKey, bestPrevKeyValue, pa.modifiedIndex) +
								"." + EscapeDotsInPathSegment(mergeKey))
						if _, hasMergeKeyChange := bestPrevPathMap[mergeKeyFieldPath]; hasMergeKeyChange &&
							score < renameScoreThreshold {
							accepted = true
						}
					}

					if !accepted {
						currentPath := path + "." + AssociativePathSegment(mergeKey, pa.keyValue, pa.modifiedIndex)
						pathMutationMap[api.ResolvedPath(currentPath)] = api.MutationInfo{
							MutationType: api.MutationTypeAdd,
							Index:        functionIndex,
							Predicate:    true,
							Value:        pa.modifiedChild.String(),
						}
						continue
					}

					previousMatched[bestPrevIdx] = true
					// Drop the merge-key field's path Update from the sub-diff:
					// the rename is applied via the ArrayElementAliases rename
					// pass at the end of applyPathMutations. Leaving an
					// explicit ?name=db-init;@N.name -> "db-init-v2" path
					// mutation in place would change the element's merge-key
					// value mid-loop and break subsequent child-path
					// resolution that still uses the previous key. (The path
					// was computed above as part of the rename guard.)
					delete(bestPrevPathMap, mergeKeyFieldPath)
					for p, m := range bestPrevPathMap {
						pathMutationMap[p] = m
					}
					if arrayOrders != nil {
						for p, o := range bestPrevArrayOrders {
							arrayOrders[p] = o
						}
					}
					if arrayElementAliases != nil {
						for p, a := range bestPrevAliases {
							if arrayElementAliases[p] == nil {
								arrayElementAliases[p] = make(map[string]string)
							}
							for k, v := range a {
								arrayElementAliases[p][k] = v
							}
						}
						ap := api.ResolvedPath(path)
						if arrayElementAliases[ap] == nil {
							arrayElementAliases[ap] = make(map[string]string)
						}
						arrayElementAliases[ap][bestPrevKeyValue] = pa.keyValue
					}
				}

				// Any unmatched previous elements were deleted.
				for i, child := range previousArrayChildren {
					if previousMatched[i] {
						continue
					}
					keyNode := child.S(mergeKey)
					var currentPath string
					if keyNode != nil {
						keyValue := fmt.Sprintf("%v", keyNode.Data())
						currentPath = path + "." + AssociativePathSegment(mergeKey, keyValue, i)
					} else {
						currentPath = path + "." + strconv.Itoa(i)
					}
					pathMutationMap[api.ResolvedPath(currentPath)] = api.MutationInfo{
						MutationType: api.MutationTypeDelete,
						Index:        functionIndex,
						Predicate:    true,
						Value:        child.String(),
					}
				}

				// Record the modified-side merge-key sequence so PatchMutations
				// can reorder the target array after path mutations are applied.
				// Skip when the sequence matches previous: nothing to reorder
				// against the merge base.
				if arrayOrders != nil && len(modifiedKeySeq) > 0 && !slices.Equal(modifiedKeySeq, previousKeySeq) {
					arrayOrders[api.ResolvedPath(path)] = modifiedKeySeq
				}
			} else {
				// Non-associative array: positional matching.
				for index, modifiedChild := range modifiedArrayChildren {
					// Arrays have to have a preceding path.
					currentPath := path + "." + strconv.Itoa(index)
					if index >= len(previousArrayChildren) {
						pathMutationMap[api.ResolvedPath(currentPath)] = api.MutationInfo{
							MutationType: api.MutationTypeAdd,
							Index:        functionIndex,
							Predicate:    true,
							Value:        modifiedChild.String(), // new data
						}
						continue // process next stack element
					}

					previousChild := previousArrayChildren[index]

					// Push this comparison to the stack
					stack = append(stack, traversalItem{
						path:        currentPath,
						previousDoc: previousChild,
						modifiedDoc: modifiedChild,
					})
				}

				// Process array elements that were deleted
				index := len(modifiedArrayChildren)
				for index < len(previousArrayChildren) {
					currentPath := path + "." + strconv.Itoa(index)
					pathMutationMap[api.ResolvedPath(currentPath)] = api.MutationInfo{
						MutationType: api.MutationTypeDelete,
						Index:        functionIndex,
						Predicate:    true,
						Value:        previousArrayChildren[index].String(), // previous data
					}
					index++
				}
			}
		} else {
			// modifiedDoc must be a value. Compare the contents.
			if modifiedDoc.String() != previousDoc.String() {
				mutation := api.MutationInfo{
					MutationType: api.MutationTypeUpdate,
					Index:        functionIndex,
					Predicate:    true,
					Value:        modifiedDoc.String(), // new data
				}
				// For string values that may contain structured data or multiple
				// lines, compute a patch so that PatchMutations can apply the
				// change to a modified target (three-way merge) rather than
				// wholesale replacement. Tries JSON and YAML structural diff
				// first (for embedded structured data), then falls back to
				// line-level text diff for multi-line strings.
				// Use Data() to get the actual string values (with real newlines),
				// not String() which returns the YAML serialization (escaped newlines).
				if prevStr, ok := previousDoc.Data().(string); ok {
					if modStr, ok := modifiedDoc.Data().(string); ok {
						if IsPatchableString(prevStr) || IsPatchableString(modStr) {
							mutation.Patch = ComputeScalarPatch(prevStr, modStr)
						}
					}
				}
				pathMutationMap[api.ResolvedPath(path)] = mutation
			}
		}
	}
}

// ComputeMutations performs a kind of diff between two configuration Units where it determines what
// modifications were made at the resource/element level and at the path level. They are recorded in a
// way that can be accumulated and updated over subsequent edits and transformations.
func ComputeMutations(previousParsedData, modifiedParsedData gaby.Container, functionIndex int64, resourceProvider ResourceProvider) (api.ResourceMutationList, error) {
	// There are limits in how accurately we can determine the correspondence between resources/elements
	// across revisions. Once resources/elements change too significantly, they will be determined to be
	// distinct. Some properties, such as the ResourceCategory, ResourceType, and ResourceName, carry more
	// significance than other attributes. Also, presence of paths (keys) should carry more weight than values.
	// Line diffs use surrounding lines for context to identify matches, which sometimes works well,
	// but also can be fragile, such as in the case of insertions of partially similar blocks, or minor
	// changes in syntax, such as presence or absence of trailing commas.
	// Since we don't expect a vast number of resources/elements per unit, an algorithm that is quadratic in
	// numbers of resources/elements, such as using Jaccard Similarity or Levenshtein Distance, is acceptable.
	// As opposed to some kind of higher-dimensional vector distance using embeddings.
	// https://www.geeksforgeeks.org/jaccard-similarity/ -- intersection size divided by union size
	// https://www.geeksforgeeks.org/introduction-to-levenshtein-distance/ -- number of edits
	// To compute Jaccard Similarity we'd need to enumerate all of the paths and values using a visitor.
	// To compute the Levenshtein Distance we can use ComputeMutationsForDocs, but it only provides
	// relatively similarity rather than absolute similarity, so we need to normalize the deltas similar to
	// Jaccard Similarity. For now I'll use the line count as the denominator.
	// Of course, we should optimize for the common case that resources are modified in their same positions
	// and are not renamed nor have types changed.
	// I decided not to impose a canonical order based on resource name because it would cause resources to
	// move when they are renamed, such as during cloning.

	// We could start with either the previous docs or the modified docs. I chose the latter, since the
	// modified docs represent the new/current content.

	// Resource Matching
	// For each document in the modified data, find the corresponding document in the previous data.
	mutations := api.ResourceMutationList{}
	previousDocMatched := make([]bool, len(previousParsedData))
	minUnmatchedPreviousDocIndex := 0
	modifiedDocIndex := 0
	for modifiedDocIndex < len(modifiedParsedData) {
		modifiedDoc := modifiedParsedData[modifiedDocIndex]
		modifiedResourceInfo, err := GetResourceInfo(modifiedDoc, resourceProvider)
		if err != nil {
			return nil, errors.Wrap(err, fmt.Sprintf("error in modified resource/element %d", modifiedDocIndex))
		}
		modifiedResourceCategory := modifiedResourceInfo.ResourceCategory
		modifiedResourceType := modifiedResourceInfo.ResourceType
		modifiedResourceName := modifiedResourceInfo.ResourceName
		modifiedResourceNameOnly := modifiedResourceInfo.ResourceNameWithoutScope
		modifiedResourceMergeID := modifiedResourceInfo.ResourceMergeID

		// When MatchByIDOnly is set and the resource has a ResourceMergeID, only match by
		// ResourceMergeID, skipping name-based and fuzzy matching. This prevents immutable
		// resources with hash-suffixed names (e.g., ConfigMaps) from being incorrectly
		// matched to other versions of the same base resource.
		matchByIDOnly := api.IsUUID(modifiedResourceMergeID) &&
			slices.Contains(GetMutationOptions(modifiedDoc, resourceProvider), constants.MatchByIDOnly)

		// Check whether the "next" resource obviously matches in the previous doc list.
		// If not, we need to search for it. We could make maps of type and name to index,
		// but that wouldn't work in the case of type changes, so I'm punting on that for
		// simplicity for now.

		// Search previousDocMatched starting with minUnmatchedPreviousDocIndex.
		matchIndex := -1
		bestMatchScore := math.MaxFloat64
		// TODO: Determine a reasonable threshold. If the name of a Namespace changes, that's one line in 4, or 0.25.
		// It's also possible that we should always consider another resource of the same type as the same resource
		// if there's only one.
		maxMatchScore := 1.0
		numDocLines := strings.Count(modifiedParsedData.String(), "\n")
		var pathMutationMap api.MutationMap
		var arrayOrderMap api.ArrayOrderMap
		var arrayElementAliasMap api.ArrayElementAliasMap
		var previousResourceMergeID string
		minMutationLength := math.MaxInt
		aliases := map[api.ResourceName]struct{}{}
		aliasesWithoutScopes := map[api.ResourceName]struct{}{}
		for previousDocIndex := minUnmatchedPreviousDocIndex; previousDocIndex < len(previousDocMatched); previousDocIndex++ {
			previousDoc := previousParsedData[previousDocIndex]
			previousResourceInfo, err := GetResourceInfo(previousDoc, resourceProvider)
			if err != nil {
				return nil, errors.Wrap(err, fmt.Sprintf("error in previous resource/element %d", previousDocIndex))
			}
			previousResourceCategory := previousResourceInfo.ResourceCategory
			previousResourceType := previousResourceInfo.ResourceType
			previousResourceName := previousResourceInfo.ResourceName
			previousResourceMergeID = previousResourceInfo.ResourceMergeID
			if previousResourceCategory != modifiedResourceCategory {
				continue
			}
			// TODO: favor exact match
			if !resourceProvider.ResourceTypesAreSimilar(previousResourceType, modifiedResourceType) {
				continue
			}

			// Path-Level Diff
			// Compute the detailed path-level differences between the two documents.
			tmpMutationMap := api.MutationMap{}
			tmpArrayOrders := api.ArrayOrderMap{}
			tmpArrayElementAliases := api.ArrayElementAliasMap{}
			mergeKeyLookup := MergeKeyLookup(func(path string) (string, bool) {
				return resourceProvider.MergeKeyForPath(modifiedResourceType, path)
			})
			ComputeMutationsForDocs("", previousDoc, modifiedDoc, functionIndex, tmpMutationMap, mergeKeyLookup, tmpArrayOrders, tmpArrayElementAliases)

			// ResourceMergeID match — if both have valid UUID ResourceMergeIDs and they match, it's a definite match.
			if api.IsUUID(modifiedResourceMergeID) && api.IsUUID(previousResourceMergeID) && modifiedResourceMergeID == previousResourceMergeID {
				previousResourceNameOnly := previousResourceInfo.ResourceNameWithoutScope
				matchIndex = previousDocIndex
				bestMatchScore = 0.0
				pathMutationMap = tmpMutationMap
				arrayOrderMap = tmpArrayOrders
				arrayElementAliasMap = tmpArrayElementAliases
				aliases = map[api.ResourceName]struct{}{
					previousResourceName: {},
				}
				aliasesWithoutScopes = map[api.ResourceName]struct{}{
					previousResourceNameOnly: {},
				}
				break
			}

			// When MatchByIDOnly is set, skip name-based and fuzzy matching.
			if matchByIDOnly {
				continue
			}

			// Exact name match
			// If ResourceType and ResourceName match exactly, it's a definite match (score = 0).
			// TODO: favor exact match
			// TODO: special-case changes of a placeholder scope to a non-placeholder scope
			previousResourceNameOnly := previousResourceInfo.ResourceNameWithoutScope
			if previousResourceName == modifiedResourceName || previousResourceNameOnly == modifiedResourceNameOnly {
				matchIndex = previousDocIndex
				bestMatchScore = 0.0
				pathMutationMap = tmpMutationMap
				arrayOrderMap = tmpArrayOrders
				arrayElementAliasMap = tmpArrayElementAliases
				// Alias Tracking - record both old and new names
				aliases = map[api.ResourceName]struct{}{
					previousResourceName: {},
				}
				aliasesWithoutScopes = map[api.ResourceName]struct{}{
					previousResourceNameOnly: {},
				}
				break
			}
			// Fuzzy matching
			// If names don't match, use similarity score based on number of path differences.
			// TODO: Figure out a better way to determine name changes.
			// Kustomize records name changes when they occur at the field mutation level, but
			// that doesn't work for out-of-band (non-filter) changes.
			// https://github.com/kubernetes-sigs/kustomize/blob/616c08480583c24b1828111a6e9e720735676979/api/filters/prefix/prefix.go#L29
			// https://github.com/kubernetes-sigs/kustomize/blob/616c08480583c24b1828111a6e9e720735676979/api/filters/suffix/suffix.go#L29
			// TODO: special-case changes of the placeholder name to a non-placeholder name
			// TODO: special-case matching indices and/or clones by setting the score to the minimum matching score
			// TODO: special-case the only resource of matching type
			// TODO: some attributes, like container names and images, are more important than others
			// TODO: Do we need a name kernel pattern to deal with common prefixes and suffixes?
			// Cost is the leaf-value count of the sub-diff (mutationMapCost)
			// so a mutation whose Value is a YAML subtree (e.g., a whole
			// container Add/Delete) is counted at full weight rather than
			// contributing 1 per map entry.
			cost := mutationMapCost(tmpMutationMap)
			if cost < minMutationLength {
				minMutationLength = cost
				pathMutationMap = tmpMutationMap
				arrayOrderMap = tmpArrayOrders
				arrayElementAliasMap = tmpArrayElementAliases
				bestMatchScore = float64(minMutationLength) / float64(numDocLines)
				matchIndex = previousDocIndex
				// Re-initialize aliases and aliasesWithoutScopes
				aliases = map[api.ResourceName]struct{}{
					previousResourceName: {},
				}
				aliasesWithoutScopes = map[api.ResourceName]struct{}{
					previousResourceNameOnly: {},
				}
			}
		}

		// Unmatched resources are Adds
		// If no match was found, then we need to add this resource. During Create,
		// including cloning, the previous data should be empty.
		if matchIndex < 0 || bestMatchScore > maxMatchScore {
			mutations = append(mutations, api.ResourceMutation{
				Resource: api.ResourceInfo{
					ResourceType:             modifiedResourceType,
					ResourceName:             modifiedResourceName,
					ResourceNameWithoutScope: modifiedResourceNameOnly,
					ResourceCategory:         modifiedResourceCategory,
					ResourceMergeID:          modifiedResourceMergeID,
				},
				ResourceMutationInfo: api.MutationInfo{
					MutationType: api.MutationTypeAdd,
					Index:        functionIndex,
					Predicate:    true,
					Value:        modifiedDoc.String(), // new data
				},
				// Don't use pathMutationMap, if any
				PathMutationMap: make(api.MutationMap),
				// Don't use previous aliases, if any
				Aliases: map[api.ResourceName]struct{}{
					modifiedResourceName: {},
				},
				AliasesWithoutScopes: map[api.ResourceName]struct{}{
					modifiedResourceNameOnly: {},
				},
			})
			modifiedDocIndex++
			continue
		}

		// Matched resource - record Update or None mutation
		// A match for the resource was found. It possibly was changed.

		// ComputeMutations is used in several ways, as described above.
		// However, it's generally better to try to use the most accurate
		// ResourceMergeID available.
		resourceMergeID := modifiedResourceMergeID
		if IsEmptyOrPlaceHolder(resourceMergeID) {
			resourceMergeID = previousResourceMergeID
		}

		// Alias Tracking - add new aliases for the modified resource name
		aliases[modifiedResourceName] = struct{}{}
		aliasesWithoutScopes[modifiedResourceNameOnly] = struct{}{}
		mutation := api.ResourceMutation{
			Resource: api.ResourceInfo{
				ResourceType:             modifiedResourceType,
				ResourceName:             modifiedResourceName,
				ResourceNameWithoutScope: modifiedResourceNameOnly,
				ResourceCategory:         modifiedResourceCategory,
				ResourceMergeID:          resourceMergeID,
			},
			ResourceMutationInfo: api.MutationInfo{
				MutationType: api.MutationTypeUpdate, // assume changed
				Index:        functionIndex,
				Predicate:    true,
				// no Value at this level
			},
			PathMutationMap:      pathMutationMap,
			ArrayOrders:          arrayOrderMap,
			ArrayElementAliases:  arrayElementAliasMap,
			Aliases:              aliases,
			AliasesWithoutScopes: aliasesWithoutScopes,
		}
		if len(arrayOrderMap) == 0 {
			mutation.ArrayOrders = nil
		}
		if len(arrayElementAliasMap) == 0 {
			mutation.ArrayElementAliases = nil
		}
		if len(pathMutationMap) == 0 && len(arrayOrderMap) == 0 && len(arrayElementAliasMap) == 0 {
			mutation.ResourceMutationInfo.MutationType = api.MutationTypeNone
		}
		mutations = append(mutations, mutation)
		modifiedDocIndex++

		// This assumes matchIndex >= 0
		// Find the next unmatched index, if any
		previousDocMatched[matchIndex] = true
		if minUnmatchedPreviousDocIndex == matchIndex {
			minUnmatchedPreviousDocIndex++
			for minUnmatchedPreviousDocIndex < len(previousDocMatched) {
				if !previousDocMatched[minUnmatchedPreviousDocIndex] {
					break
				}
				minUnmatchedPreviousDocIndex++
			}
		}
	}

	// Unmatched previous resources are Deletes.
	for minUnmatchedPreviousDocIndex < len(previousDocMatched) {
		// Skip matched resources
		if previousDocMatched[minUnmatchedPreviousDocIndex] {
			minUnmatchedPreviousDocIndex++
			continue
		}

		previousDoc := previousParsedData[minUnmatchedPreviousDocIndex]
		previousResourceInfo, err := GetResourceInfo(previousDoc, resourceProvider)
		if err != nil {
			return nil, err
		}

		previousResourceName := previousResourceInfo.ResourceName
		previousResourceNameOnly := previousResourceInfo.ResourceNameWithoutScope
		mutations = append(mutations, api.ResourceMutation{
			Resource: api.ResourceInfo{
				ResourceType:             previousResourceInfo.ResourceType,
				ResourceName:             previousResourceName,
				ResourceNameWithoutScope: previousResourceNameOnly,
				ResourceCategory:         previousResourceInfo.ResourceCategory,
				ResourceMergeID:          previousResourceInfo.ResourceMergeID,
			},
			ResourceMutationInfo: api.MutationInfo{
				MutationType: api.MutationTypeDelete,
				Index:        functionIndex,
				Predicate:    true,
				Value:        previousDoc.String(), // previous data
			},
			PathMutationMap: make(api.MutationMap),
			Aliases: map[api.ResourceName]struct{}{
				previousResourceName: {},
			},
			AliasesWithoutScopes: map[api.ResourceName]struct{}{
				previousResourceNameOnly: {},
			},
		})
		minUnmatchedPreviousDocIndex++
	}

	return mutations, nil
}

// PatchMutations applies a set of mutations to configuration data, effectively "replaying"
// recorded changes onto a YAML document. It's the inverse of ComputeMutations: whereas
// ComputeMutations determines what changed, PatchMutations applies the recorded changes.
//
// In typical usage mutationsPatch is the diff produced by ComputeMutations against a
// different (or earlier) version of the same configuration — e.g., the diff between an
// upstream Unit's old and new revisions, applied to a downstream Unit. Because of that,
// mutationsPatch may reference resource names, alias names, or paths that don't match
// parsedData verbatim; PatchMutations does its own resource lookup (with alias fallback)
// and path resolution.
//
// Three-way merge: pass mutationsToSubtract to subtract another mutation set (typically
// the diff between the merge base and the current target) from mutationsPatch first.
// This is how target-side changes are preserved against the upstream patch (see
// SubtractMutations). Pass nil (or an empty list) to skip subtraction.
//
// Predicates: mutationsPredicates is the accumulated MutationSources of the data being
// patched (see AddMutations). When a Predicate is false at the resource or any ancestor
// path, that part of mutationsPatch is filtered out. Default Predicate=true means all
// changes are eligible. mutationsPredicates may be nil.
//
// Algorithm:
//
//  1. Resource matching (per document in parsedData): look up the corresponding patch
//     entry by ResourceMergeID, then ResourceTypeAndName, then by predicate aliases (so
//     a renamed resource is still matched to its upstream patch entry).
//
//  2. Resource-level mutation:
//
//     | MutationType     | Action                                           |
//     |------------------|--------------------------------------------------|
//     | Add / Replace    | Replace entire document with the mutation's Value|
//     | Delete           | Set document to nil (filtered on serialization)  |
//     | None             | Skip (no changes)                                |
//     | Update           | Process path-level mutations                     |
//
//  3. Path-level mutation (for Update): sorted by api.SortedMutationMapEntries (numeric
//     segments compared as integers, parents before children), then partitioned so all
//     Deletes run before all non-Deletes. Deletes-first prevents a Delete with positional
//     fallback from clobbering an Add at the same array parent.
//
//     Each path is resolved through ResolveAssociativeSegments, which honors merge keys
//     and only falls back to a positional index when the element at that index has no
//     merge-key field (legacy data) or the index is out of bounds. If the path can't be
//     fully resolved, the operation is skipped — except for Add/Replace, which appends
//     to the parent array (so a new merge-keyed element can be introduced even when its
//     desired index is occupied by a different element). The append-on-clash rule
//     trades position fidelity for data preservation; positional associative arrays
//     such as initContainers will require an additional reorder pass to fully restore
//     source-side ordering.
//
//     Predicate filtering: a path or any ancestor with Predicate=false in
//     mutationsPredicates causes the path to be skipped.
//
//     Apply by type:
//
//     | MutationType     | Action                                                |
//     |------------------|-------------------------------------------------------|
//     | Add / Replace    | Set value at the resolved path (overwrites)           |
//     | Update (scalar)  | If MutationInfo.Patch is set, three-way text merge.   |
//     |                  | Otherwise replace (preserving YAML comments).         |
//     | Update (complex) | Recursive merge with the existing value (preserves    |
//     |                  | nested comments and unset fields).                    |
//     | Delete           | Remove the path from document (no-op if missing)      |
//
//  4. After visiting all existing documents, any unmatched Add/Replace patch entries are
//     parsed and appended as new documents to parsedData. Their per-path mutations are
//     applied to the new document (no subtraction, no predicate filtering).
//
// Errors are accumulated and joined; PatchMutations does its best to apply every patch
// it can rather than aborting on the first problem.
//
// PatchMutations also returns a MutationConflictList recording every part of the patch
// that was not applied: SubtractMutations conflicts (forwarded from the subtract step),
// PredicateFiltered (resource-level and path-level), and UnresolvedPath (when an
// associative segment couldn't be matched against the target). The conflicts are
// advisory — the returned data already reflects the drops.
func PatchMutations(parsedData gaby.Container, mutationsPredicates, mutationsPatch, mutationsToSubtract api.ResourceMutationList, resourceProvider ResourceProvider, options *api.FunctionOptions) (gaby.Container, api.MutationConflictList, error) {
	var conflicts api.MutationConflictList
	if len(mutationsToSubtract) > 0 {
		var subtractConflicts api.MutationConflictList
		mutationsPatch, subtractConflicts = SubtractMutations(mutationsPatch, mutationsToSubtract)
		conflicts = append(conflicts, subtractConflicts...)
	}
	// Build predicate index with prefer-predicate dedup: when multiple mutation sources
	// exist for the same resource (e.g., one from clone and one from triggers), prefer
	// the one with Predicate=true so the resource is not incorrectly filtered out.
	predicateIdx := api.NewResourceMutationIndex(mutationsPredicates)
	for i := range mutationsPredicates {
		resourceInfo := mutationsPredicates[i].Resource
		if resourceInfo.ResourceNameWithoutScope == "" {
			resourceInfo.ResourceNameWithoutScope = resourceProvider.RemoveScopeFromResourceName(resourceInfo.ResourceName)
		}
		key := api.ResourceTypeAndNameFromResourceInfo(resourceInfo)
		if existingIdx, exists := predicateIdx.NameMap[key]; exists {
			if mutationsPredicates[existingIdx].ResourceMutationInfo.Predicate &&
				!mutationsPredicates[i].ResourceMutationInfo.Predicate {
				continue
			}
		}
		predicateIdx.NameMap[key] = i
		if api.IsUUID(resourceInfo.ResourceMergeID) {
			if existingIdx, exists := predicateIdx.ResourceMergeIDMap[resourceInfo.ResourceMergeID]; exists {
				if mutationsPredicates[existingIdx].ResourceMutationInfo.Predicate &&
					!mutationsPredicates[i].ResourceMutationInfo.Predicate {
					continue
				}
			}
			predicateIdx.ResourceMergeIDMap[resourceInfo.ResourceMergeID] = i
		}
	}

	patchIdx := api.NewResourceMutationIndex(mutationsPatch)

	// Track which patch mutations were matched to existing documents.
	// Unmatched Add/Replace mutations need to be appended as new documents.
	matchedPatchIndices := make(map[int]bool)

	var errs []error

	visitor := func(doc *gaby.YamlDoc, _ any, docIndex int, docResourceInfo *api.ResourceInfo) (any, []error) {
		var visitorErrs []error

		// Find predicate for this document
		mutationPredicateIndex, hasPredicate := predicateIdx.Find(*docResourceInfo, nil)

		// Find patch for this document, using predicate aliases as additional aliases
		var predicateAliases map[api.ResourceName]struct{}
		if hasPredicate {
			predicateAliases = mutationsPredicates[mutationPredicateIndex].AliasesWithoutScopes
		}
		mutationPatchIndex, ok := patchIdx.Find(*docResourceInfo, predicateAliases)
		if !ok {
			return nil, nil
		}

		// Filter the patch at the resource level.
		if hasPredicate && !mutationsPredicates[mutationPredicateIndex].ResourceMutationInfo.Predicate {
			slog.Info("patch filtered", "resource", api.ResourceTypeAndNameFromResourceInfo(*docResourceInfo))
			predicateMutInfo := mutationsPredicates[mutationPredicateIndex].ResourceMutationInfo
			conflicts = append(conflicts, api.MutationConflict{
				Reason:   api.ConflictReasonPredicateFiltered,
				Resource: mutationsPatch[mutationPatchIndex].Resource,
				Source:   mutationsPatch[mutationPatchIndex].ResourceMutationInfo,
				Target:   &predicateMutInfo,
			})
			matchedPatchIndices[mutationPatchIndex] = true
			return nil, nil
		}

		matchedPatchIndices[mutationPatchIndex] = true
		resourcePatchMutation := &mutationsPatch[mutationPatchIndex].ResourceMutationInfo
		switch resourcePatchMutation.MutationType {
		case api.MutationTypeAdd, api.MutationTypeReplace:
			// Replace at the resource level means there was a delete then an add, so
			// treat it like add.
			valueString := resourcePatchMutation.Value
			valueDoc, err := gaby.ParseYAML([]byte(valueString))
			if err != nil {
				visitorErrs = append(visitorErrs, fmt.Errorf("error parsing value for resource %s: %w",
					api.ResourceTypeAndNameFromResourceInfo(*docResourceInfo), err))
			}
			parsedData[docIndex] = valueDoc
			// Some paths also could have been modified
		case api.MutationTypeDelete:
			// Mark the document as deleted by setting it to nil
			// The document will be filtered out when serializing the result
			parsedData[docIndex] = nil
			// Shouldn't be any modified paths
			return nil, visitorErrs
		case api.MutationTypeNone:
			// None at the resource level means the resource wasn't modified.
			return nil, nil
		case api.MutationTypeUpdate:
			// Update at the resource level means some paths were modified.
		}

		var pathConflicts api.MutationConflictList
		mergeKeyLookup := MergeKeyLookup(func(path string) (string, bool) {
			return resourceProvider.MergeKeyForPath(docResourceInfo.ResourceType, path)
		})
		visitorErrs, pathConflicts = applyPathMutations(doc, mutationsPatch[mutationPatchIndex].PathMutationMap,
			hasPredicate, mutationsPredicates, mutationPredicateIndex, mutationsPatch[mutationPatchIndex].Resource,
			mutationsPatch[mutationPatchIndex].ArrayOrders, mutationsPatch[mutationPatchIndex].ArrayElementAliases,
			mergeKeyLookup,
			visitorErrs)
		conflicts = append(conflicts, pathConflicts...)
		return nil, visitorErrs
	}

	_, visitErr := VisitResourcesFiltered(parsedData, nil, resourceProvider, options, visitor)
	if visitErr != nil {
		errs = append(errs, visitErr)
	}

	// Append new documents for Add/Replace mutations that didn't match any existing document.
	for i := range mutationsPatch {
		if matchedPatchIndices[i] {
			continue
		}
		resourcePatchMutation := &mutationsPatch[i].ResourceMutationInfo
		switch resourcePatchMutation.MutationType {
		case api.MutationTypeAdd, api.MutationTypeReplace:
			valueString := resourcePatchMutation.Value
			valueDoc, err := gaby.ParseYAML([]byte(valueString))
			if err != nil {
				errs = append(errs, fmt.Errorf("error parsing value for unmatched resource %s: %w",
					api.ResourceTypeAndNameFromResourceInfo(mutationsPatch[i].Resource), err))
				continue
			}
			var pathConflicts api.MutationConflictList
			mergeKeyLookup := MergeKeyLookup(func(path string) (string, bool) {
				return resourceProvider.MergeKeyForPath(mutationsPatch[i].Resource.ResourceType, path)
			})
			errs, pathConflicts = applyPathMutations(valueDoc, mutationsPatch[i].PathMutationMap,
				false, nil, 0, mutationsPatch[i].Resource,
				mutationsPatch[i].ArrayOrders, mutationsPatch[i].ArrayElementAliases,
				mergeKeyLookup,
				errs)
			conflicts = append(conflicts, pathConflicts...)
			parsedData = append(parsedData, valueDoc)
		}
	}

	return parsedData, conflicts, errors.Join(errs...)
}

// applyPathMutations applies path-level mutations from a PathMutationMap to a document.
// If hasPredicate is true, paths whose path or any ancestor has Predicate=false in the
// caller's predicate map are skipped.
//
// Path resolution uses ResolveAssociativeSegments. If a path can't be fully resolved
// (typically because a merge-keyed element no longer exists in the target with the
// recorded merge-key value), Update and Delete operations are skipped. Add/Replace
// instead falls back to appending to the parent array (see appendPathForAdd) so a new
// merge-keyed element from the patch can still be introduced even when its source-side
// index in the target is occupied by an unrelated element.
//
// Within a single PathMutationMap, Deletes run before Adds/Updates/Replaces. This
// matters for "rename" pairs (Delete old + Add new at the same array parent): with
// Deletes-first the old element is gone before the new element's path is resolved, so
// the Add doesn't have to fight the Delete for the same fallback index. ComputeMutations
// produces Deletes and Adds for disjoint elements, so reordering them within a resource
// is safe.
//
// Returns the (possibly extended) errs slice and a list of MutationConflicts for any
// path mutations that were dropped (predicate-filtered or unresolved). Conflicts for
// paths skipped via the Add append-fallback are NOT recorded (the Add was applied,
// just at a different index).
//
// If arrayOrders is non-empty, after path mutations are applied each merge-keyed
// array is reordered to match the recorded source-side merge-key sequence. This
// is what gives positional associative arrays (initContainers, env, ports) their
// correct ordering when a rename, insertion, or reorder is part of the patch.
func applyPathMutations(doc *gaby.YamlDoc, pathMutationMap api.MutationMap,
	hasPredicate bool, mutationsPredicates api.ResourceMutationList, mutationPredicateIndex int,
	resource api.ResourceInfo, arrayOrders api.ArrayOrderMap,
	arrayElementAliases api.ArrayElementAliasMap,
	mergeKeyLookup MergeKeyLookup,
	errs []error) ([]error, api.MutationConflictList) {

	var conflicts api.MutationConflictList

	// Sort paths so parents are processed before children, then partition so all Deletes
	// run before all non-Deletes. Path order is preserved within each partition.
	sorted := api.SortedMutationMapEntries(pathMutationMap)
	patches := make([]api.MutationMapEntry, 0, len(sorted))
	for _, entry := range sorted {
		if entry.MutationInfo.MutationType == api.MutationTypeDelete {
			patches = append(patches, entry)
		}
	}
	for _, entry := range sorted {
		if entry.MutationInfo.MutationType != api.MutationTypeDelete {
			patches = append(patches, entry)
		}
	}

	for i := range patches {
		resolvedPath, resolved := ResolveAssociativeSegments(doc, string(patches[i].Path))
		patchPath := api.ResolvedPath(resolvedPath)
		patchMutation := patches[i].MutationInfo
		if !resolved {
			// The path contains an associative segment whose merge-key value didn't
			// match any element in the current doc, and the element at the fallback
			// index has a different merge-key value (i.e., is not the same element).
			// For Add/Replace, fall back to appending the new element to the parent
			// array — this preserves the upstream's intent to add a new element with
			// a unique merge-key value while avoiding clobbering the unrelated
			// element that happens to occupy the source-side index. For Update and
			// Delete, the element being addressed simply isn't there, so skip.
			if patchMutation.MutationType == api.MutationTypeAdd ||
				patchMutation.MutationType == api.MutationTypeReplace {
				if appendPath, ok := appendPathForAdd(doc, resolvedPath); ok {
					patchPath = api.ResolvedPath(appendPath)
				} else {
					slog.Debug("patch path unresolved (Add), skipping",
						"path", string(patches[i].Path), "resolved", resolvedPath)
					conflicts = append(conflicts, api.MutationConflict{
						Reason:   api.ConflictReasonUnresolvedPath,
						Resource: resource,
						Path:     patches[i].Path,
						Source:   *patchMutation,
					})
					continue
				}
			} else {
				slog.Debug("patch path unresolved, skipping",
					"path", string(patches[i].Path), "resolved", resolvedPath,
					"mutationType", string(patchMutation.MutationType))
				conflicts = append(conflicts, api.MutationConflict{
					Reason:   api.ConflictReasonUnresolvedPath,
					Resource: resource,
					Path:     patches[i].Path,
					Source:   *patchMutation,
				})
				continue
			}
		}
		// Check for patches that conflict with the predicate.
		// TODO: Break down the patch.
		if hasPredicate {
			// Walk up path ancestors to find if any predicate filters this path.
			_, predicateMutation, hasFilter := api.FindAncestorPath(
				mutationsPredicates[mutationPredicateIndex].PathMutationMap, patchPath)
			if hasFilter && !predicateMutation.Predicate {
				slog.Debug("path filtered", "path", string(patchPath))
				predicateMutCopy := predicateMutation
				conflicts = append(conflicts, api.MutationConflict{
					Reason:   api.ConflictReasonPredicateFiltered,
					Resource: resource,
					Path:     patches[i].Path,
					Source:   *patchMutation,
					Target:   &predicateMutCopy,
				})
				continue
			}
		}
		switch patchMutation.MutationType {
		case api.MutationTypeAdd, api.MutationTypeReplace:
			valueString := patchMutation.Value
			valueDoc, err := gaby.ParseYAML([]byte(valueString))
			if err != nil {
				errs = append(errs, fmt.Errorf("error parsing value at path %s: %w", patchPath, err))
			}
			// Note: This doesn't preserve indentation nor field ordering.
			_, err = doc.SetDocExpandP(valueDoc, string(patchPath))
			if err != nil {
				errs = append(errs, fmt.Errorf("error setting value at path %s: %w", patchPath, err))
			}
		case api.MutationTypeUpdate:
			// For updates, try to preserve comments when possible
			valueString := patchMutation.Value
			valueDoc, err := gaby.ParseYAML([]byte(valueString))
			if err != nil {
				errs = append(errs, fmt.Errorf("error parsing value at path %s: %w", patchPath, err))
				continue
			}

			// Check if the value is a complex object (map or list) vs a scalar
			ynode := valueDoc.YNode()
			isScalarValue := ynode.Kind == yaml.ScalarNode

			if isScalarValue {
				// For multi-line string updates with a line-level patch, apply the
				// patch to the target's current value (three-way merge) rather than
				// replacing it wholesale. This correctly handles the case where the
				// target has been independently modified.
				if patchMutation.Patch != "" {
					currentField := doc.Path(string(patchPath))
					if currentField != nil {
						if currentStr, ok := currentField.Data().(string); ok {
							patched, ok := ApplyScalarPatch(currentStr, patchMutation.Patch)
							if ok {
								// Set the patched string directly as a scalar value
								// rather than parsing it as YAML, which would lose
								// multi-line string formatting.
								_, setErr := doc.SetExpandP(patched, string(patchPath))
								if setErr != nil {
									errs = append(errs, fmt.Errorf("error setting patched value at path %s: %w", patchPath, setErr))
								}
								continue
							}
							slog.Info("scalar patch failed, falling back to full value",
								"path", string(patchPath))
							// Fall through to use valueDoc (the full Value) as wholesale replacement.
						}
					}
				}

				// TODO: This may no longer make sense now that comments are represented as attributes.
				// For scalar values, we need to preserve the comment manually
				// Get the current field to check if it has a comment
				currentField := doc.Path(string(patchPath))
				var existingComment string
				if currentField != nil {
					existingComment = currentField.GetComments()
				}

				// Set the new value
				_, err = doc.SetDocExpandP(valueDoc, string(patchPath))
				if err != nil {
					errs = append(errs, fmt.Errorf("error setting value at path %s: %w", patchPath, err))
				} else if existingComment != "" {
					// Restore the comment after setting the value
					updatedField := doc.Path(string(patchPath))
					if updatedField != nil {
						updatedField.SetComment(existingComment)
					}
				}
			} else {
				// For complex objects (maps/lists), use merge to preserve nested comments
				err = doc.MergeDocP(valueDoc, string(patchPath))
				if err != nil {
					errs = append(errs, fmt.Errorf("error merging value at path %s: %w", patchPath, err))
				}
			}
		case api.MutationTypeDelete:
			if !doc.ExistsP(string(patchPath)) {
				continue
			}
			err := doc.DeleteP(string(patchPath))
			if err != nil {
				errs = append(errs, fmt.Errorf("error deleting path %s: %w", patchPath, err))
			}
		case api.MutationTypeNone:
			// Shouldn't happen for paths, but also shouldn't be anything to do
		}
	}

	// Rename pass: for each (arrayPath, oldKey -> newKey) in arrayElementAliases,
	// find the array element whose merge-key value is oldKey at arrayPath in the
	// current doc and rewrite its merge-key field to newKey. Runs after path
	// mutations (which used oldKey-encoded paths to align with target's diff)
	// and before the reorder pass (which uses newKey to look up elements via
	// ArrayOrders).
	if len(arrayElementAliases) > 0 && mergeKeyLookup != nil {
		for arrayPath, aliases := range arrayElementAliases {
			mergeKey, _ := mergeKeyLookup(string(arrayPath))
			if mergeKey == "" {
				continue
			}
			resolvedArrayPath, ok := ResolveAssociativeSegments(doc, string(arrayPath))
			if !ok {
				continue
			}
			arrayDoc := doc.Path(resolvedArrayPath)
			if arrayDoc == nil {
				continue
			}
			node := arrayDoc.YNode()
			if node == nil || node.Kind != yaml.SequenceNode {
				continue
			}
			for _, elem := range node.Content {
				if elem.Kind != yaml.MappingNode {
					continue
				}
				for j := 0; j < len(elem.Content)-1; j += 2 {
					k, v := elem.Content[j], elem.Content[j+1]
					if k.Kind == yaml.ScalarNode && k.Value == mergeKey && v.Kind == yaml.ScalarNode {
						if newKey, ok := aliases[v.Value]; ok {
							v.Value = newKey
						}
						break
					}
				}
			}
		}
	}

	// Reorder pass: for each merge-keyed array path with a recorded desired
	// order, rearrange the target array's elements so they match. This runs
	// after path mutations (so newly-added or appended elements are present)
	// and uses the resource provider's merge-key lookup to identify each
	// element by its key field.
	if len(arrayOrders) > 0 && mergeKeyLookup != nil {
		// Process longer paths first so an inner array (e.g. an env array
		// nested in a container) is reordered before its outer container is
		// reordered around it.
		paths := make([]api.ResolvedPath, 0, len(arrayOrders))
		for p := range arrayOrders {
			paths = append(paths, p)
		}
		slices.SortFunc(paths, func(a, b api.ResolvedPath) int {
			return len(b) - len(a) // longer paths first
		})
		for _, path := range paths {
			mergeKey, _ := mergeKeyLookup(string(path))
			if mergeKey == "" {
				continue
			}
			reorderArrayByMergeKey(doc, string(path), mergeKey, arrayOrders[path])
		}
	}

	return errs, conflicts
}

// Reset walks each path in mutationsPredicates and, where Predicate=true and the value
// at the corresponding location in parsedData is a string or int, sets the value back to
// the toolchain's placeholder marker (PlaceHolderBlockApplyString / PlaceHolderBlockApplyInt).
// Used by the "reset" function to revert the leaves last touched by a chosen subset of
// historical mutations to their unset state, leaving everything else alone.
//
// ResourceMergeID and the legacy ResourceID path are always preserved — they identify the
// resource for cross-unit matching and must not be wiped.
func Reset(parsedData gaby.Container, mutationsPredicates api.ResourceMutationList, resourceProvider ResourceProvider, options *api.FunctionOptions) error {
	mutationPredicateMap := make(map[api.ResourceTypeAndName]int)
	resetResourceMergeIDMap := make(map[string]int)
	for i := range mutationsPredicates {
		resourceInfo := mutationsPredicates[i].Resource
		if resourceInfo.ResourceNameWithoutScope == "" {
			resourceInfo.ResourceNameWithoutScope = resourceProvider.RemoveScopeFromResourceName(resourceInfo.ResourceName)
		}
		resourceInfoKey := api.ResourceTypeAndNameFromResourceInfo(resourceInfo)
		mutationPredicateMap[resourceInfoKey] = i
		if api.IsUUID(resourceInfo.ResourceMergeID) {
			resetResourceMergeIDMap[resourceInfo.ResourceMergeID] = i
		}
	}

	// The ResourceMergeID field must not be reset to a placeholder — it is intended to be
	// stable across the original unit and all clones, enabling cross-unit resource matching.
	resourceMergeIDContextPath := resourceProvider.ContextPath(constants.ResourceMergeIDKeySuffix)
	// Also protect the legacy ResourceID path from being reset.
	legacyResourceIDContextPath := resourceProvider.ContextPath(constants.ResourceIDKeySuffix)

	visitor := func(doc *gaby.YamlDoc, _ any, _ int, docResourceInfo *api.ResourceInfo) (any, []error) {
		resourceInfoKey := api.ResourceTypeAndNameFromResourceInfo(*docResourceInfo)

		// Try ResourceMergeID-based lookup first, fall back to name-based
		mutationPredicateIndex, hasPredicate := 0, false
		if api.IsUUID(docResourceInfo.ResourceMergeID) {
			mutationPredicateIndex, hasPredicate = resetResourceMergeIDMap[docResourceInfo.ResourceMergeID]
		}
		if !hasPredicate {
			mutationPredicateIndex, hasPredicate = mutationPredicateMap[resourceInfoKey]
		}
		if !hasPredicate {
			// Nothing to reset
			return nil, nil
		}

		// TODO: The predicate for the resource could set the default, but would require traversing
		// all the paths, like FindYAMLPathsByValue.
		// shouldBeReset := hasPredicate && mutationsPredicates[mutationPredicateIndex].ResourceMutationInfo.Predicate
		// PathMutationMap is a map, which could be in arbitrary order.
		// We're only going to reset leaves, so that should be ok.

		var errs []error
		for path, mutation := range mutationsPredicates[mutationPredicateIndex].PathMutationMap {
			if !mutation.Predicate {
				// Shouldn't be reset
				continue
			}
			// Never reset the ResourceMergeID — it must remain stable across clones for matching.
			if resourceMergeIDContextPath != "" && strings.HasSuffix(string(path), resourceMergeIDContextPath) {
				continue
			}
			// Also protect the legacy ResourceID path from being reset.
			if legacyResourceIDContextPath != "" && strings.HasSuffix(string(path), legacyResourceIDContextPath) {
				continue
			}
			resolvedPathStr, pathResolved := ResolveAssociativeSegments(doc, string(path))
			if !pathResolved {
				continue
			}
			resolvedPath := api.ResolvedPath(resolvedPathStr)
			value, found, err := YamlSafePathGetValueAnyType(doc, resolvedPath, true)
			if err != nil {
				errs = append(errs, err)
				continue
			}
			if !found {
				continue
			}
			switch value.(type) {
			case string:
				_, err = doc.SetP(PlaceHolderBlockApplyString, string(resolvedPath))
				if err != nil {
					slog.Info("error setting string value at path", "path", string(resolvedPath), "error", err)
				}
			case int:
				_, err = doc.SetP(PlaceHolderBlockApplyInt, string(resolvedPath))
				if err != nil {
					slog.Info("error setting int value at path", "path", string(resolvedPath), "error", err)
				}
			default:
				// Not a leaf or no placeholder value. Skip.
			}
		}
		return nil, errs
	}

	_, err := VisitResourcesFiltered(parsedData, nil, resourceProvider, options, visitor)
	return err
}

// AddMutations merges newMutations into mutations and returns the result, accumulating
// changes over sequential edits to produce a compiled history of all modifications. The
// accumulated form is what's stored as a Unit's MutationSources and what feeds the
// Predicate map passed into PatchMutations.
//
// Algorithm:
//
//  1. Resource matching: by current ResourceTypeAndName, then by ResourceMergeID, then by
//     AliasesWithoutScopes (handling renames). Unmatched new mutations are appended as
//     new resource entries.
//
//  2. Resource-level merge:
//
//     | Existing Type    | New Type              | Result              |
//     |------------------|-----------------------|---------------------|
//     | Any              | None                  | Keep existing       |
//     | Any              | Delete or Replace     | Replace with new    |
//     | None             | Any (non-None)        | Replace with new    |
//     | Delete           | Any (non-Delete)      | Change to Replace   |
//     | Add/Update       | Add/Update            | Merge path mutations|
//
//  3. Path-level merge: process newMutations' paths sorted least-specific to most-specific
//     so parent paths land before children. For each new path:
//
//     - Exact match in existing: replace the existing entry, taking the new MutationType
//       (so a later edit's intent — e.g., an Update on a previously-Added field — is
//       reflected). Exception: Delete → non-Delete becomes Replace, since the field was
//       previously erased and is now being re-set.
//     - Existing path is a child of the new path AND new path is Delete or Replace: drop
//       the now-superseded child paths.
//     - Otherwise: insert the new path verbatim, dropping any existing children it
//       supersedes.
//
//     Because the new MutationType replaces the existing one on exact match, when this
//     accumulated record is later used as a patch, PatchMutations sees the latest intent
//     (e.g., Update to merge with the target's value rather than wholesale Replace).
//
//  4. Alias tracking: union of both sides' Aliases / AliasesWithoutScopes so a resource
//     can still be matched after another rename.
//
// Key behaviors:
//   - Accumulative: designed to be called repeatedly as changes occur.
//   - Last-write-wins for values and types on exact-path matches.
//   - Alias awareness: handles resources renamed between mutation sets.
func AddMutations(mutations, newMutations api.ResourceMutationList) (api.ResourceMutationList, bool) {
	hasMutations := false
	idx := api.NewResourceMutationIndex(mutations)
	for i := range newMutations {
		if newMutations[i].ResourceMutationInfo.MutationType == api.MutationTypeNone {
			continue
		}
		hasMutations = true
		mi, present := idx.Find(newMutations[i].Resource, newMutations[i].AliasesWithoutScopes)
		if !present {
			mutations = append(mutations, newMutations[i])
			continue
		}
		if newMutations[i].ResourceMutationInfo.MutationType == api.MutationTypeDelete ||
			newMutations[i].ResourceMutationInfo.MutationType == api.MutationTypeReplace ||
			mutations[mi].ResourceMutationInfo.MutationType == api.MutationTypeNone {
			mutations[mi] = newMutations[i]
			continue
		}
		if mutations[mi].ResourceMutationInfo.MutationType == api.MutationTypeDelete {
			mutations[mi] = newMutations[i]
			mutations[mi].ResourceMutationInfo.MutationType = api.MutationTypeReplace
			continue
		}

		// Update the resource name, which may have changed.
		mutations[mi].Resource.ResourceName = newMutations[i].Resource.ResourceName
		mutations[mi].Resource.ResourceNameWithoutScope = newMutations[i].Resource.ResourceNameWithoutScope
		// Set the ResourceMergeID, but do not clear it.
		if !IsEmptyOrPlaceHolder(newMutations[i].Resource.ResourceMergeID) {
			mutations[mi].Resource.ResourceMergeID = newMutations[i].Resource.ResourceMergeID
		}
		if mutations[mi].Aliases == nil {
			mutations[mi].Aliases = make(map[api.ResourceName]struct{})
		}
		for alias := range newMutations[i].Aliases {
			mutations[mi].Aliases[alias] = struct{}{}
		}
		if mutations[mi].AliasesWithoutScopes == nil {
			mutations[mi].AliasesWithoutScopes = make(map[api.ResourceName]struct{})
		}
		for alias := range newMutations[i].AliasesWithoutScopes {
			mutations[mi].AliasesWithoutScopes[alias] = struct{}{}
		}

		// Merge ArrayOrders: last-writer-wins per array path. The newer
		// mutation set's view of the desired array order supersedes the
		// older one's. (Both are computed against the same target — newer
		// reflects the latest source-side intent.)
		if len(newMutations[i].ArrayOrders) > 0 {
			if mutations[mi].ArrayOrders == nil {
				mutations[mi].ArrayOrders = make(api.ArrayOrderMap, len(newMutations[i].ArrayOrders))
			}
			for path, order := range newMutations[i].ArrayOrders {
				mutations[mi].ArrayOrders[path] = order
			}
		}

		// Merge ArrayElementAliases: last-writer-wins per (arrayPath, oldKey).
		if len(newMutations[i].ArrayElementAliases) > 0 {
			if mutations[mi].ArrayElementAliases == nil {
				mutations[mi].ArrayElementAliases = make(api.ArrayElementAliasMap, len(newMutations[i].ArrayElementAliases))
			}
			for path, aliases := range newMutations[i].ArrayElementAliases {
				if mutations[mi].ArrayElementAliases[path] == nil {
					mutations[mi].ArrayElementAliases[path] = make(map[string]string, len(aliases))
				}
				for oldKey, newKey := range aliases {
					mutations[mi].ArrayElementAliases[path][oldKey] = newKey
				}
			}
		}

		// Merge the path mutations. The overall MutationType, Add or Update or Replace, should remain the same.
		// If newMutations contains a path that's a prefix of paths in mutations, we need to remove them.
		// If the path matches, then we need to look at the existing MutationType.
		// Otherwise we add the path.
		// Process new paths sorted from least to most specific so that parent paths are
		// processed before children, ensuring child cleanup is correct.
		existingIdx := api.NewPathPrefixIndex(mutations[mi].PathMutationMap)
		for _, entry := range api.SortedMutationMapEntries(newMutations[i].PathMutationMap) {
			path := entry.Path
			mutation := *entry.MutationInfo
			// Exact match: update in place.
			// We originally preserved the original mutation type (either Add or Replace),
			// unless it's Delete. The idea was that the change is relative to less-specific
			// changes in the same set of mutations rather than relative to other configuration data.
			// However, it was unclear the type shouldn't be changed to Update, which would more
			// accurately represent the latest change, so now we update the mutation type, which
			// will cause PatchMutations to attempt to merge if used as a patch.
			if existing, ok := mutations[mi].PathMutationMap[path]; ok {
				mutationType := mutation.MutationType
				if existing.MutationType == api.MutationTypeDelete &&
					mutation.MutationType != api.MutationTypeDelete {
					mutationType = api.MutationTypeReplace
				}
				mutations[mi].PathMutationMap[path] = api.MutationInfo{
					MutationType: mutationType,
					Index:        mutation.Index,
					Predicate:    mutation.Predicate,
					Value:        mutation.Value,
				}
				if mutation.MutationType == api.MutationTypeDelete || mutation.MutationType == api.MutationTypeReplace {
					// Remove any existing child paths it supersedes.
					for _, childPath := range existingIdx.ChildPaths(path) {
						delete(mutations[mi].PathMutationMap, childPath)
					}
				}
			} else {
				// New path: add it and remove any existing child paths it supersedes.
				mutations[mi].PathMutationMap[path] = mutation
				for _, childPath := range existingIdx.ChildPaths(path) {
					delete(mutations[mi].PathMutationMap, childPath)
				}
			}
		}
	}
	return mutations, hasMutations
}

// SubtractMutations removes from mutations any changes that overlap with subtractMutations,
// implementing the "preserve target-side changes" half of three-way merging. Typically
// invoked from PatchMutations via its mutationsToSubtract argument.
//
// Use case:
//
//	source : ComputeMutations(base, sourceEnd)  // upstream changes
//	target : ComputeMutations(base, target)     // local customizations
//	patch  : SubtractMutations(source, target)  // source changes that don't conflict
//
// When PatchMutations applies patch to target, target-side customizations remain because
// the source paths that would have overwritten them have been removed.
//
// Both operands are expected to be diffs produced by ComputeMutations: Add, Delete,
// Update, or None at the resource level. (Replace, which AddMutations may produce when
// accumulating, is handled here defensively but not expected.) Update at the resource
// level has an empty Value — all changes live in PathMutationMap.
//
// Algorithm:
//
//  1. Resource matching: by ResourceMergeID, ResourceTypeAndName, then AliasesWithoutScopes
//     from either side (so renamed resources subtract correctly).
//
//  2. Resource-level subtraction:
//
//     | Subtract Type | Mutation Type | Result                                          |
//     |---------------|---------------|-------------------------------------------------|
//     | Delete        | Any           | Drop (target removed the resource)              |
//     | Replace       | Any           | Drop (target redefined the resource)            |
//     | None          | Any           | Keep (target didn't change it)                  |
//     | Any           | None          | Keep (source didn't change it)                  |
//     | Update/Add    | Delete        | Keep source Delete; emit DeleteShadowed for     |
//     |               |               | each target mutation under it (the target's     |
//     |               |               | edits have nowhere to live once the resource    |
//     |               |               | is gone)                                        |
//     | Update/Add    | Update/Add    | Path-level subtraction                          |
//
//  3. Path-level subtraction: paths are walked using a NewPathPrefixIndex (binary search
//     over a sorted path list) so prefix relationships are O(log n + k):
//
//     - Case 1 (exact match): subtract has the same path → drop the source path.
//     - Case 2 (subtract is ancestor): subtract has spec.containers.0 and source has
//       spec.containers.0.image → drop the source path (parent was changed in target).
//     - Case 3 (subtract is descendant): subtract has spec.containers.0.image and source
//       has spec.containers.0 (whole block). If the source path is a Delete, keep it
//       and emit a DeleteShadowed conflict for each target child path that's being
//       erased — once the parent is gone the child changes can't apply. Otherwise
//       keep the source path and splice in subtract's more-specific paths so
//       PatchMutations' parent-before-child processing lets target's change win.
//
//  4. If subtraction empties an Update's PathMutationMap, the resource-level type
//     downgrades to None.
//
// Returns the patch with subtractions applied, plus a MutationConflictList
// recording every drop (resource-level and path-level) so callers can surface
// them as merge conflicts. The conflicts are advisory — the returned
// ResourceMutationList already reflects the drops.
//
// Key behaviors:
//   - Target precedence: subtractMutations always wins where it overlaps.
//   - Alias awareness: matches resources across renames.
//   - Partial expansion: only splits a parent path when subtract has finer-grained
//     conflicts under it; unaffected branches stay whole.
func SubtractMutations(mutations, subtractMutations api.ResourceMutationList) (api.ResourceMutationList, api.MutationConflictList) {
	subtractIdx := api.NewResourceMutationIndex(subtractMutations)

	result := make(api.ResourceMutationList, 0, len(mutations))
	var conflicts api.MutationConflictList

	for i := range mutations {
		mutation := mutations[i]

		si, found := subtractIdx.Find(mutation.Resource, mutation.AliasesWithoutScopes)
		if !found {
			// No matching subtraction, keep the mutation as is
			result = append(result, mutation)
			continue
		}

		subtractMutation := subtractMutations[si]
		targetResMutInfo := subtractMutation.ResourceMutationInfo

		// Handle resource-level subtraction

		// We expect both operands to be mutations diffs produced by ComputeMutations.
		// ComputeMutations just generates Add, Delete, Update, and None mutations.
		// None means that the resource was present, but not changed.
		// It's AddMutations that converts Delete followed by Add to Replace and
		// Add followed by Update to just Add. We handle Replace here just in case, but
		// don't really expect to see it.
		// Update at the resource level will have an empty Value -- all of the values will be
		// associated with paths.

		// If the resource was deleted in subtractMutations, don't include any changes to it
		if subtractMutation.ResourceMutationInfo.MutationType == api.MutationTypeDelete {
			conflicts = append(conflicts, api.MutationConflict{
				Reason:   api.ConflictReasonSubtracted,
				Resource: mutation.Resource,
				Source:   mutation.ResourceMutationInfo,
				Target:   &targetResMutInfo,
			})
			continue
		}

		// If the resource was replaced in subtractMutations, remove it entirely
		// (the target has completely redefined this resource)
		if subtractMutation.ResourceMutationInfo.MutationType == api.MutationTypeReplace {
			conflicts = append(conflicts, api.MutationConflict{
				Reason:   api.ConflictReasonSubtracted,
				Resource: mutation.Resource,
				Source:   mutation.ResourceMutationInfo,
				Target:   &targetResMutInfo,
			})
			continue
		}

		// Otherwise the resource was not deleted in the target

		// Handle MutationType None
		// in mutation - changes nothing, so it's safe to keep
		// in subtractMutation - the target didn't override anything, so keep it
		if mutation.ResourceMutationInfo.MutationType == api.MutationTypeNone ||
			subtractMutation.ResourceMutationInfo.MutationType == api.MutationTypeNone {
			result = append(result, mutation)
			continue
		}

		// If the source is a Delete at resource level and the target made changes,
		// the source's intent to remove the resource still wins — the target's
		// customizations have nowhere to live once the resource is gone. Let
		// the Delete through and emit DeleteShadowed for the target's
		// resource-level mutation and each path mutation it had, so the caller
		// can surface the lost work.
		if mutation.ResourceMutationInfo.MutationType == api.MutationTypeDelete {
			conflicts = append(conflicts, api.MutationConflict{
				Reason:   api.ConflictReasonDeleteShadowed,
				Resource: mutation.Resource,
				Source:   mutation.ResourceMutationInfo,
				Target:   &targetResMutInfo,
			})
			for _, entry := range api.SortedMutationMapEntries(subtractMutation.PathMutationMap) {
				targetMutCopy := *entry.MutationInfo
				conflicts = append(conflicts, api.MutationConflict{
					Reason:   api.ConflictReasonDeleteShadowed,
					Resource: mutation.Resource,
					Path:     entry.Path,
					Source:   mutation.ResourceMutationInfo,
					Target:   &targetMutCopy,
				})
			}
			result = append(result, mutation)
			continue
		}

		// If the resource was added or updated in subtractMutations and independently added,
		// replaced, or updated in mutations, then merge the two.

		// For Update at resource level, we need to filter out path mutations
		// that were changed in the target. ArrayOrders is merged: each
		// merge-keyed array's source-side desired order is woven with the
		// target-side desired order so target-side inserts keep their
		// relative position in the final merged sequence.
		newMutation := api.ResourceMutation{
			Resource:             mutation.Resource,
			ResourceMutationInfo: mutation.ResourceMutationInfo,
			PathMutationMap:      make(api.MutationMap),
			ArrayOrders:          mergeArrayOrderMaps(mutation.ArrayOrders, subtractMutation.ArrayOrders, mutation.ArrayElementAliases),
			ArrayElementAliases:  mutation.ArrayElementAliases,
			Aliases:              mutation.Aliases,
			AliasesWithoutScopes: mutation.AliasesWithoutScopes,
		}

		// Process each path mutation using sorted iteration and efficient prefix lookups.
		subtractPrefixIdx := api.NewPathPrefixIndex(subtractMutation.PathMutationMap)
		for _, entry := range api.SortedMutationMapEntries(mutation.PathMutationMap) {
			path := entry.Path
			pathMutation := *entry.MutationInfo

			// Case 1: Exact match - subtract this path
			if targetMut, found := subtractMutation.PathMutationMap[path]; found {
				targetMutCopy := targetMut
				conflicts = append(conflicts, api.MutationConflict{
					Reason:   api.ConflictReasonSubtracted,
					Resource: mutation.Resource,
					Path:     path,
					Source:   pathMutation,
					Target:   &targetMutCopy,
				})
				continue
			}

			// Case 2: Check if a subtract path is an ancestor of the mutation path.
			// e.g., subtract has spec.containers.0 and we have spec.containers.0.image
			// In this case, the target changed a parent, so don't apply child changes.
			// Walk up path segments doing map lookups: O(depth) instead of O(n).
			if ancestorPath, ancestorMut, hasAncestor := api.FindAncestorPath(subtractMutation.PathMutationMap, path); hasAncestor {
				_ = ancestorPath
				ancestorMutCopy := ancestorMut
				conflicts = append(conflicts, api.MutationConflict{
					Reason:   api.ConflictReasonSubtracted,
					Resource: mutation.Resource,
					Path:     path,
					Source:   pathMutation,
					Target:   &ancestorMutCopy,
				})
				continue
			}

			// Case 3: Check if the mutation path is an ancestor of any subtract path.
			// e.g., we have spec.containers.0 (a large block) and subtract has spec.containers.0.image
			// Use the prefix index for O(log n) lookup.
			childPaths := subtractPrefixIdx.ChildPaths(path)
			if len(childPaths) > 0 {
				// If the source mutation is a Delete, the target's child changes
				// have nowhere to live once the parent is removed. Let the Delete
				// through and emit DeleteShadowed for each lost child so the
				// caller can surface the dropped target work.
				if pathMutation.MutationType == api.MutationTypeDelete {
					for _, childPath := range childPaths {
						childMut := subtractMutation.PathMutationMap[childPath]
						childMutCopy := childMut
						conflicts = append(conflicts, api.MutationConflict{
							Reason:   api.ConflictReasonDeleteShadowed,
							Resource: mutation.Resource,
							Path:     childPath,
							Source:   pathMutation,
							Target:   &childMutCopy,
						})
					}
					newMutation.PathMutationMap[path] = pathMutation
					continue
				}
				// Keep the mutation path and add the subtractMutation paths that override it.
				// Since PatchMutations processes paths from least specific to most specific,
				// the subtractMutation's more specific paths will override the mutation's value.
				newMutation.PathMutationMap[path] = pathMutation
				for _, childPath := range childPaths {
					newMutation.PathMutationMap[childPath] = subtractMutation.PathMutationMap[childPath]
				}
			} else {
				// No child subtract paths, keep the mutation as-is
				newMutation.PathMutationMap[path] = pathMutation
			}
		}

		// If we removed all path mutations and there are no array reorders or
		// element-rename rewrites to apply either, downgrade an Update to None.
		if len(newMutation.PathMutationMap) == 0 && len(newMutation.ArrayOrders) == 0 &&
			len(newMutation.ArrayElementAliases) == 0 &&
			newMutation.ResourceMutationInfo.MutationType == api.MutationTypeUpdate {
			newMutation.ResourceMutationInfo.MutationType = api.MutationTypeNone
		}

		// Only add if there's something left
		if newMutation.ResourceMutationInfo.MutationType != api.MutationTypeNone || len(result) > 0 {
			result = append(result, newMutation)
		}
	}

	return result, conflicts
}

// FindMutationIndex looks up the mutation index for a specific resource and path
// in a ResourceMutationList. It matches the resource by ResourceTypeAndName,
// handling aliases and scope changes (same pattern as AddMutations).
// For the path, it walks up parent paths to find the most specific mutation index,
// falling back to the resource-level index if no path-level match is found.
// Returns the mutation index and true if found.
func FindMutationIndex(mutationSources api.ResourceMutationList, resource api.ResourceInfo, path api.ResolvedPath) (int64, bool) {
	idx := api.NewResourceMutationIndex(mutationSources)
	mi, found := idx.Find(resource, nil)
	if !found {
		return 0, false
	}

	rm := mutationSources[mi]

	// Build a reverse lookup from stripped (numeric-only) paths to mutation info,
	// so that incoming resolved paths with numeric indices can match mutation entries
	// that use ?key=value;@index format.
	strippedPathMap := make(map[api.ResolvedPath]*api.MutationInfo, len(rm.PathMutationMap))
	for p, info := range rm.PathMutationMap {
		stripped := api.ResolvedPath(StripAssociativeSegments(string(p)))
		if stripped != p {
			infoCopy := info
			strippedPathMap[stripped] = &infoCopy
		}
	}

	// Walk up path segments from most specific to least specific,
	// same pattern as the predicate check at line ~793 in this file.
	pathSegments := gaby.DotPathToSlice(string(path))
	for len(pathSegments) > 0 {
		candidatePath := api.ResolvedPath(JoinPathSegments(pathSegments))
		if mutInfo, ok := rm.PathMutationMap[candidatePath]; ok {
			return mutInfo.Index, true
		}
		// Also try matching against stripped (numeric-only) paths
		if mutInfo, ok := strippedPathMap[candidatePath]; ok {
			return mutInfo.Index, true
		}
		pathSegments = pathSegments[:len(pathSegments)-1]
	}

	// No path-level match; fall back to the resource-level index.
	return rm.ResourceMutationInfo.Index, true
}
