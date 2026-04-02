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
// ComputeMutations performs a structured diff between two YAML configurations (represented as
// gaby.Container, which is a list of parsed YAML documents). It determines what changes were
// made and records them in a way that can be accumulated over subsequent edits, using api.OffsetMutations
// and AddMutations.
//
// ComputeMutations is currently used in three related ways:
// 1. Diffing two revisions of the same unit to use with PatchMutations in a diff/patch (aka three-way merge) of
//    a different unit or the same unit (for redo/rebase)
// 2. Diffing a single change, by a mutating function or a configuration data update or refresh, and accumulating it
//    with prior mutations to later use to set Predicates to enhance (1).
// 3. Diffing revisions of two different units to use with SubtractMutations to enhance (1)
//
// Input:
//   - previousParsedData: The "before" state (parsed YAML documents)
//   - modifiedParsedData: The "after" state (parsed YAML documents)
//   - functionIndex: A sequence number to track which function/operation made the change
//   - resourceProvider: Toolchain-specific interface for extracting resource metadata
//
// Output:
//   - api.ResourceMutationList: A list of mutations, one per resource/document
//
// Algorithm:
//
// 1. Resource Matching
//
// For each document in the modified data, it tries to find the corresponding document in
// the previous data:
//   - Exact name match: If ResourceType and ResourceName match exactly, it's a definite match
//   - Fuzzy matching: If names don't match, it uses a similarity score based on the number
//     of path-level differences divided by total lines. This handles renamed resources.
//   - Match threshold: If the best match score exceeds 1.0, the resource is considered new
//
// 2. Path-Level Diff via ComputeMutationsForDocs
//
// For matched resources, it performs a deep comparison using a stack-based traversal:
//   - Compares children of maps recursively
//   - Detects Add: key exists in modified but not previous
//   - Detects Update: key exists in both but contents differ
//   - Detects Delete: key exists in previous but not modified
//   - Handles arrays by index
//
// 3. Mutation Types
//
// For each resource, the function assigns a ResourceMutationInfo.MutationType:
//   - Add: Resource in modified, not in previous
//   - Update: Resource in both, has path changes
//   - None: Resource in both, no path changes
//   - Delete: Resource in previous, not in modified
//
// 4. Alias Tracking
//
// When resources are matched (even with different names), both the old and new names are
// recorded in Aliases and AliasesWithoutScopes. This enables tracking resources across renames.
//
// Example:
//
// Given previousParsedData:
//
//	apiVersion: apps/v1
//	kind: Deployment
//	metadata:
//	  name: myapp
//	spec:
//	  replicas: 1
//
// And modifiedParsedData:
//
//	apiVersion: apps/v1
//	kind: Deployment
//	metadata:
//	  name: myapp
//	spec:
//	  replicas: 3
//	  image: nginx:1.20
//
// ComputeMutations would return:
//
//	ResourceMutationList{
//	  {
//	    Resource: {ResourceType: "apps/v1/Deployment", ResourceName: "default/myapp", ...},
//	    ResourceMutationInfo: {MutationType: Update, Index: 1},
//	    PathMutationMap: {
//	      "spec.replicas": {MutationType: Update, Value: "3"},
//	      "spec.image":    {MutationType: Add, Value: "nginx:1.20"},
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

// ResolveAssociativeSegments resolves ?key=value;@index segments in a path to numeric indices
// by looking up elements in the document. It tries key=value match first, then falls back to
// the positional index. Returns the resolved path with numeric indices only.
func ResolveAssociativeSegments(doc *gaby.YamlDoc, path string) string {
	if !strings.Contains(path, "?") {
		return path
	}
	segments := gaby.DotPathToSlice(path)
	var resolvedSegments []string
	currentNode := doc
	for _, segment := range segments {
		if !strings.HasPrefix(segment, "?") {
			resolvedSegments = append(resolvedSegments, EscapeDotsInPathSegment(segment))
			if currentNode != nil {
				currentNode = currentNode.S(segment)
			}
			continue
		}
		// Parse ?key=value or ?key=value;@index
		kv := strings.TrimPrefix(segment, "?")
		kvParts := strings.SplitN(kv, "=", 2)
		if len(kvParts) != 2 {
			// Invalid, keep as-is
			resolvedSegments = append(resolvedSegments, EscapeDotsInPathSegment(segment))
			currentNode = nil
			continue
		}
		key := kvParts[0]
		value := kvParts[1]
		fallbackIndex := ""
		if semiAt := strings.Index(value, ";@"); semiAt >= 0 {
			fallbackIndex = value[semiAt+2:]
			value = value[:semiAt]
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
			// Fall back to positional index
			if !resolved && fallbackIndex != "" {
				idx, err := strconv.Atoi(fallbackIndex)
				if err == nil && idx >= 0 && idx < len(elements) {
					resolvedSegments = append(resolvedSegments, fallbackIndex)
					currentNode = elements[idx]
					resolved = true
				}
			}
		}
		if !resolved {
			// Can't resolve — use fallback index if available, otherwise keep segment as-is
			if fallbackIndex != "" {
				resolvedSegments = append(resolvedSegments, fallbackIndex)
			} else {
				resolvedSegments = append(resolvedSegments, EscapeDotsInPathSegment(segment))
			}
			currentNode = nil
		}
	}
	return strings.Join(resolvedSegments, ".")
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
		if !strings.HasPrefix(segment, "?") {
			continue
		}
		kv := strings.TrimPrefix(segment, "?")
		kvParts := strings.SplitN(kv, "=", 2)
		if len(kvParts) != 2 {
			continue
		}
		value := kvParts[1]
		if strings.HasPrefix(value, "@") {
			// ?key=@index -> index
			segments[i] = strings.TrimPrefix(value, "@")
		} else if semiAt := strings.Index(value, ";@"); semiAt >= 0 {
			// ?key=value;@index -> index
			segments[i] = value[semiAt+2:]
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
		if !strings.HasPrefix(segment, "?") {
			continue
		}
		kv := strings.TrimPrefix(segment, "?")
		kvParts := strings.SplitN(kv, "=", 2)
		if len(kvParts) != 2 {
			continue
		}
		key := strings.ReplaceAll(kvParts[0], "~1", ".")
		value := kvParts[1]
		if strings.HasPrefix(value, "@") {
			// ?key=@index — direct index, no merge key value
			continue
		}
		if semiAt := strings.Index(value, ";@"); semiAt >= 0 {
			value = value[:semiAt]
		}
		value = strings.ReplaceAll(value, "~1", ".")
		entries = append(entries, MergeKeyEntry{Key: key, Value: value})
	}
	return entries
}

// ComputeMutationsForDocs determines the edits that have been performed to transform the previousDoc
// into modifiedDoc. The resulting mutations are associated with the provided functionIndex.
// The pathMutationMap is modified in place.
//
// mergeKeyLookup, if non-nil, is called with array paths to determine whether the array
// is associative (has a merge key). If so, elements are matched by merge key value
// instead of positional index, and paths use the ?key=value;@index syntax.
func ComputeMutationsForDocs(rootPath string, previousDoc *gaby.YamlDoc, modifiedDoc *gaby.YamlDoc, functionIndex int64, pathMutationMap api.MutationMap, mergeKeyLookup MergeKeyLookup) {
	// NOTE: We do not tombstone removed paths in mutations so they are not later re-added
	// by a patch. Example: a port in a Service is removed from a downstream unit and
	// some part of that port spec is modified in the upstream unit. The next PatchMutations
	// for upgrade would reinsert the port. We handle that by removing them from the mutations
	// before they are patched with SubtractMutations and/or by selecting mutations eligible to
	// be patched, by setting the Predicate values.

	// There's also the reciprocal issue of what to do in the case that a field is modified
	// in a downstream unit and a block of configuration around it (e.g., a resource or a container)
	// is removed from the upstream unit. For now, we will remove those deletions using SubtractMutations.

	// TODO: Decide what to do about embedded accessors

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
				previousByKey := make(map[string]previousEntry, len(previousArrayChildren))
				for i, child := range previousArrayChildren {
					keyNode := child.S(mergeKey)
					if keyNode != nil {
						keyValue := fmt.Sprintf("%v", keyNode.Data())
						previousByKey[keyValue] = previousEntry{index: i, doc: child}
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
						// Not found in previous; this is an addition.
						currentPath := path + "." + AssociativePathSegment(mergeKey, keyValue, modifiedIndex)
						pathMutationMap[api.ResolvedPath(currentPath)] = api.MutationInfo{
							MutationType: api.MutationTypeAdd,
							Index:        functionIndex,
							Predicate:    true,
							Value:        modifiedChild.String(),
						}
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
			mergeKeyLookup := MergeKeyLookup(func(path string) (string, bool) {
				return resourceProvider.MergeKeyForPath(modifiedResourceType, path)
			})
			ComputeMutationsForDocs("", previousDoc, modifiedDoc, functionIndex, tmpMutationMap, mergeKeyLookup)

			// ResourceMergeID match — if both have valid UUID ResourceMergeIDs and they match, it's a definite match.
			if api.IsUUID(modifiedResourceMergeID) && api.IsUUID(previousResourceMergeID) && modifiedResourceMergeID == previousResourceMergeID {
				previousResourceNameOnly := previousResourceInfo.ResourceNameWithoutScope
				matchIndex = previousDocIndex
				bestMatchScore = 0.0
				pathMutationMap = tmpMutationMap
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
			// TODO: take into account the number of subpaths (leaf values) of the paths in the map
			if len(tmpMutationMap) < minMutationLength {
				minMutationLength = len(tmpMutationMap)
				pathMutationMap = tmpMutationMap
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
			Aliases:              aliases,
			AliasesWithoutScopes: aliasesWithoutScopes,
		}
		if len(pathMutationMap) == 0 {
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
// recorded changes onto a YAML document. It's the inverse of ComputeMutations - where
// ComputeMutations determines what changed, PatchMutations applies those changes.
//
// mutationsPatch is sometimes generated from other configuration units, such as in the canonical
// case of upgrade from upstream. Or may be generated from past revisions or even live state.
// So it may not match the provided configuration data in some ways, such as resource names
// and whole resources.
// By default all resources and paths are patchable. Predicates are used to preserve existing changes.
// mutationsPredicates is expected to have been generated from the mutations corresponding to the
// configuration data being patched. So it is expected to match the contents of parsedData.
// It is acceptable for mutationsPredicates to be nil.
//
// Algorithm:
//
// 1. Process Each Document
//
// For each document in parsedData:
//
// Resource Matching:
//   - Tries to find matching patch by current resource name
//   - Falls back to originalName annotation (for cloned resources)
//   - Falls back to AliasesWithoutScopes from predicates
//
// Predicate Filtering (Resource Level):
//   - If predicate exists and Predicate == false, skip the entire resource
//
// Resource-Level Mutations:
//
//	| MutationType     | Action                                           |
//	|------------------|--------------------------------------------------|
//	| Add / Replace    | Replace entire document with the mutation's Value|
//	| Delete           | Set document to nil (filtered on serialization)  |
//	| None             | Skip (no changes)                                |
//	| Update           | Process path-level mutations                     |
//
// 2. Path-Level Mutations
//
// For Update mutations, process each path in PathMutationMap:
//
//   - Sort paths: Process parent paths before children (lexicographic sort)
//
//   - Predicate filtering: Check path and all parent paths for Predicate == false
//
//   - Apply by type:
//
//     | MutationType     | Action                                    |
//     |------------------|-------------------------------------------|
//     | Add / Replace    | Set value at path (overwrites)            |
//     | Update           | Merge value at path (preserves comments)  |
//     | Delete           | Remove the path from document             |
//
// Key Behaviors:
//   - Alias awareness: Matches resources even when names differ between patch and target
//   - Predicate filtering: Allows selective application of changes
//   - Comment preservation: Update mutations try to preserve YAML comments
//   - Parent-first ordering: Ensures parent paths are applied before children
//   - Graceful handling: Logs errors but continues processing other mutations
func PatchMutations(parsedData gaby.Container, mutationsPredicates, mutationsPatch api.ResourceMutationList, resourceProvider ResourceProvider) (gaby.Container, error) {
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

	for docIndex, doc := range parsedData {
		docResourceInfo, err := GetResourceInfo(doc, resourceProvider)
		if err != nil {
			errs = append(errs, err)
			continue
		}

		// Find predicate for this document
		mutationPredicateIndex, hasPredicate := predicateIdx.Find(*docResourceInfo, nil)

		// Filter the patch.
		if hasPredicate && !mutationsPredicates[mutationPredicateIndex].ResourceMutationInfo.Predicate {
			slog.Info("patch filtered", "resource", api.ResourceTypeAndNameFromResourceInfo(*docResourceInfo))
			continue
		}

		// Find patch for this document, using predicate aliases as additional aliases
		var predicateAliases map[api.ResourceName]struct{}
		if hasPredicate {
			predicateAliases = mutationsPredicates[mutationPredicateIndex].AliasesWithoutScopes
		}
		mutationPatchIndex, ok := patchIdx.Find(*docResourceInfo, predicateAliases)
		if !ok {
			continue
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
				errs = append(errs, fmt.Errorf("error parsing value for resource %s: %w",
					api.ResourceTypeAndNameFromResourceInfo(*docResourceInfo), err))
			}
			parsedData[docIndex] = valueDoc
			// Some paths also could have been modified
		case api.MutationTypeDelete:
			// Mark the document as deleted by setting it to nil
			// The document will be filtered out when serializing the result
			parsedData[docIndex] = nil
			// Shouldn't be any modified paths
			continue
		case api.MutationTypeNone:
			// None at the resource level means the resource wasn't modified.
			continue
		case api.MutationTypeUpdate:
			// Update at the resource level means some paths were modified.
		}

		errs = applyPathMutations(doc, mutationsPatch[mutationPatchIndex].PathMutationMap,
			hasPredicate, mutationsPredicates, mutationPredicateIndex, errs)
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
			errs = applyPathMutations(valueDoc, mutationsPatch[i].PathMutationMap,
				false, nil, 0, errs)
			parsedData = append(parsedData, valueDoc)
		}
	}

	return parsedData, errors.Join(errs...)
}

// applyPathMutations applies path-level mutations from a PathMutationMap to a document.
// If hasPredicate is true, paths are filtered against the predicate's PathMutationMap.
func applyPathMutations(doc *gaby.YamlDoc, pathMutationMap api.MutationMap,
	hasPredicate bool, mutationsPredicates api.ResourceMutationList, mutationPredicateIndex int,
	errs []error) []error {

	// Sort paths so parents are processed before children.
	patches := api.SortedMutationMapEntries(pathMutationMap)

	for i := range patches {
		patchPath := api.ResolvedPath(ResolveAssociativeSegments(doc, string(patches[i].Path)))
		patchMutation := patches[i].MutationInfo
		// Check for patches that conflict with the predicate.
		// TODO: Break down the patch.
		if hasPredicate {
			// Walk up path ancestors to find if any predicate filters this path.
			_, predicateMutation, hasFilter := api.FindAncestorPath(
				mutationsPredicates[mutationPredicateIndex].PathMutationMap, patchPath)
			if hasFilter && !predicateMutation.Predicate {
				slog.Debug("path filtered", "path", string(patchPath))
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
	return errs
}

func Reset(parsedData gaby.Container, mutationsPredicates api.ResourceMutationList, resourceProvider ResourceProvider) error {
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
			resolvedPath := api.ResolvedPath(ResolveAssociativeSegments(doc, string(path)))
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

	_, err := VisitResources(parsedData, nil, resourceProvider, visitor)
	return err
}

// AddMutations merges newMutations into (existing) mutations and returns the result.
// It's used to accumulate changes over sequential edits, creating a compiled history
// of all modifications.
//
// Algorithm:
//
// 1. Process Each New Mutation
//
// For each mutation in newMutations:
//
// Resource Matching:
//   - First tries to match by current ResourceTypeAndName
//   - If not found, checks AliasesWithoutScopes to handle renamed resources
//   - If still not found, appends as a new resource mutation
//
// Resource-Level Merge Rules:
//
//	| Existing Type    | New Type              | Result              |
//	|------------------|-----------------------|---------------------|
//	| Any              | None                  | Keep existing       |
//	| Any              | Delete or Replace     | Replace with new    |
//	| None             | Any (non-None)        | Replace with new    |
//	| Delete           | Any (non-Delete)      | Change to Replace   |
//	| Add/Update       | Add/Update            | Merge path mutations|
//
// 2. Path-Level Merge
//
// When merging Add/Update mutations, the path mutations are combined:
//   - Exact path match: Update the value, preserving the original mutation type
//     (except Delete → non-Delete becomes Replace)
//   - New path is prefix of existing: Remove existing child paths (the new value supersedes them)
//   - New path not found: Add it to the map.
//
// One implication of this approach is that a path and value might appear at the resource level
// or in the path map at a higher level (path prefix), or even at multiple levels, and can be
// then overridden by a value of a more specific path. The values need to be patched from least
// specific to most specific in order to produce the resulting configuration data.
//
// 3. Alias Tracking
//
// Merges aliases from both mutations to track all names the resource has had.
//
// Key Behaviors:
//   - Accumulative: Designed to be called repeatedly as changes occur
//   - Last-write-wins for values: New values replace old values at the same path
//   - Type preservation: Original mutation type is preserved (Add stays Add, Update stays Update)
//   - Alias awareness: Handles resources that have been renamed between mutations
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

// SubtractMutations removes mutations that overlap with subtractMutations from mutations.
// It's used in three-way merging to ensure that changes made in a target unit take precedence
// over changes from a source unit.
//
// Use Case:
//
// When merging source unit changes into a target unit:
//   - Source: base → sourceEnd (upstream changes)
//   - Target: base → target (local customizations)
//   - Result: SubtractMutations(sourceMutations, targetMutations) gives changes that
//     won't overwrite local customizations
//
// Both operands are expected to be mutation diffs produced by ComputeMutations.
// ComputeMutations generates Add, Delete, Update, and None mutations (not Replace).
// None means the resource was present but not changed. AddMutations is what converts
// Delete followed by Add to Replace. Replace is handled here just in case, but not expected.
// Update at the resource level has an empty Value - all values are in the PathMutationMap.
//
// Algorithm:
//
// 1. Resource Matching
//
// For each mutation, find corresponding subtraction mutation by:
//   - Current ResourceTypeAndName
//   - AliasesWithoutScopes from either mutation (handles renamed resources)
//
// 2. Resource-Level Subtraction
//
//	| Subtract Type | Mutation Type | Result                                     |
//	|---------------|---------------|--------------------------------------------|
//	| Delete        | Any           | Remove entirely (target deleted)           |
//	| Replace       | Any           | Remove entirely (target redefined)         |
//	| None          | Any           | Keep mutation (target didn't change it)    |
//	| Any           | None          | Keep mutation (source didn't change it)    |
//	| Update/Add    | Delete        | Remove (don't delete what target modified) |
//	| Update/Add    | Update/Add    | Process path-level subtraction             |
//
// 3. Path-Level Subtraction
//
// For each path in the mutation's PathMutationMap:
//   - Case 1 - Exact match: Path exists in subtractMutations → remove it
//   - Case 2 - Subtract path is prefix: e.g., subtract has spec.containers.0,
//     mutation has spec.containers.0.image → remove it (parent was changed)
//   - Case 3 - Mutation path is prefix: e.g., mutation has spec.containers.0 (whole block),
//     subtract has spec.containers.0.image → if the mutation is a Delete, skip it entirely
//     (can't partially un-delete). Otherwise, keep the mutation and add the subtractMutation
//     paths. PatchMutations processes paths from least specific to most specific, so the
//     subtractMutation's more specific paths will override the mutation's value.
//
// Key Behaviors:
//   - Target precedence: Changes in subtractMutations take priority
//   - Alias awareness: Matches resources across renames
//   - Partial expansion: Only expands paths as needed, keeping unaffected branches whole
//   - Type conversion: If all paths are subtracted from an Update, it becomes None
func SubtractMutations(mutations, subtractMutations api.ResourceMutationList) api.ResourceMutationList {
	subtractIdx := api.NewResourceMutationIndex(subtractMutations)

	result := make(api.ResourceMutationList, 0, len(mutations))

	for i := range mutations {
		mutation := mutations[i]

		si, found := subtractIdx.Find(mutation.Resource, mutation.AliasesWithoutScopes)
		if !found {
			// No matching subtraction, keep the mutation as is
			result = append(result, mutation)
			continue
		}

		subtractMutation := subtractMutations[si]

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
			// Resource was deleted in target - don't include any source changes
			continue
		}

		// If the resource was replaced in subtractMutations, remove it entirely
		// (the target has completely redefined this resource)
		if subtractMutation.ResourceMutationInfo.MutationType == api.MutationTypeReplace {
			// Resource was replaced in target - don't include source changes
			// Replace means the target deleted and re-added the resource.
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

		// If the mutation is a Delete at resource level, and the subtraction is Update or Add,
		// then the target has modified paths and the deletion should not be allowed.
		if mutation.ResourceMutationInfo.MutationType == api.MutationTypeDelete {
			// Target modified the resource, don't delete it
			continue
		}

		// If the resource was added or updated in subtractMutations and independently added,
		// replaced, or updated in mutations, then merge the two.

		// For Update at resource level, we need to filter out path mutations
		// that were changed in the target
		newMutation := api.ResourceMutation{
			Resource:             mutation.Resource,
			ResourceMutationInfo: mutation.ResourceMutationInfo,
			PathMutationMap:      make(api.MutationMap),
			Aliases:              mutation.Aliases,
			AliasesWithoutScopes: mutation.AliasesWithoutScopes,
		}

		// Process each path mutation using sorted iteration and efficient prefix lookups.
		subtractPrefixIdx := api.NewPathPrefixIndex(subtractMutation.PathMutationMap)
		for _, entry := range api.SortedMutationMapEntries(mutation.PathMutationMap) {
			path := entry.Path
			pathMutation := *entry.MutationInfo

			// Case 1: Exact match - subtract this path
			if _, found := subtractMutation.PathMutationMap[path]; found {
				continue
			}

			// Case 2: Check if a subtract path is an ancestor of the mutation path.
			// e.g., subtract has spec.containers.0 and we have spec.containers.0.image
			// In this case, the target changed a parent, so don't apply child changes.
			// Walk up path segments doing map lookups: O(depth) instead of O(n).
			if api.HasAncestorPath(subtractMutation.PathMutationMap, path) {
				continue
			}

			// Case 3: Check if the mutation path is an ancestor of any subtract path.
			// e.g., we have spec.containers.0 (a large block) and subtract has spec.containers.0.image
			// Use the prefix index for O(log n) lookup.
			childPaths := subtractPrefixIdx.ChildPaths(path)
			if len(childPaths) > 0 {
				// If the mutation is a Delete, we can't partially un-delete, so skip it entirely
				if pathMutation.MutationType == api.MutationTypeDelete {
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

		// If we removed all path mutations and the resource-level mutation was Update,
		// convert to None
		if len(newMutation.PathMutationMap) == 0 &&
			(newMutation.ResourceMutationInfo.MutationType == api.MutationTypeUpdate) {
			newMutation.ResourceMutationInfo.MutationType = api.MutationTypeNone
		}

		// Only add if there's something left
		if newMutation.ResourceMutationInfo.MutationType != api.MutationTypeNone || len(result) > 0 {
			result = append(result, newMutation)
		}
	}

	return result
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
