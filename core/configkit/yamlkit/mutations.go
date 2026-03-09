// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package yamlkit

import (
	"fmt"
	"log/slog"
	"math"
	"sort"
	"strconv"
	"strings"

	"github.com/cockroachdb/errors"
	"github.com/confighub/sdk/constants"
	"github.com/confighub/sdk/function/api"
	"github.com/confighub/sdk/third_party/gaby"
	"sigs.k8s.io/kustomize/kyaml/yaml"
)

// ComputeMutations and ComputeMutationsForDocs Overview
//
// ComputeMutations performs a structured diff between two YAML configurations (represented as
// gaby.Container, which is a list of parsed YAML documents). It determines what changes were
// made and records them in a way that can be accumulated over subsequent edits, using api.OffsetMutations
// and api.AddMutations.
//
// ComputeMutations is currently used in three related ways:
// 1. Diffing two revisions of the same unit to use with PatchMutations in a diff/patch (aka three-way merge) of
//    a different unit or the same unit (for redo/rebase)
// 2. Diffing a single change, by a mutating function or a configuration data update or refresh, and accumulating it
//    with prior mutations to later use to set Predicates to enhance (1).
// 3. Diffing revisions of two different units to use with api.SubtractMutations to enhance (1)
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
	// before they are patched with api.SubtractMutations and/or by selecting mutations eligible to
	// be patched, by setting the Predicate values.

	// There's also the reciprocal issue of what to do in the case that a field is modified
	// in a downstream unit and a block of configuration around it (e.g., a resource or a container)
	// is removed from the upstream unit. For now, we will remove those deletions using api.SubtractMutations.

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
				pathMutationMap[api.ResolvedPath(path)] = api.MutationInfo{
					MutationType: api.MutationTypeUpdate,
					Index:        functionIndex,
					Predicate:    true,
					Value:        modifiedDoc.String(), // new data
				}
				// log.Infof("different values: '%s' vs '%s'", previousDoc.String(), modifiedDoc.String())
			}
		}
	}
}

// FIXME: Remove this once all existing clones are converted to Aliases/AliasesWithoutScopes.
const OriginalNameAnnotation = "confighub.com/OriginalName"

var originalNamePath = "metadata.annotations." + EscapeDotsInPathSegment(OriginalNameAnnotation)

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
		modifiedResourceID := modifiedResourceInfo.ResourceID

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
			previousResourceID := previousResourceInfo.ResourceID
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

			// ResourceID match — if both have valid UUID ResourceIDs and they match, it's a definite match.
			if api.IsUUID(modifiedResourceID) && api.IsUUID(previousResourceID) && modifiedResourceID == previousResourceID {
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
					ResourceID:               modifiedResourceID,
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

		// Alias Tracking - add new aliases for the modified resource name
		aliases[modifiedResourceName] = struct{}{}
		aliasesWithoutScopes[modifiedResourceNameOnly] = struct{}{}
		mutation := api.ResourceMutation{
			Resource: api.ResourceInfo{
				ResourceType:             modifiedResourceType,
				ResourceName:             modifiedResourceName,
				ResourceNameWithoutScope: modifiedResourceNameOnly,
				ResourceCategory:         modifiedResourceCategory,
				ResourceID:               modifiedResourceID,
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

	// Unmatched previous resources are Deletes
	// Any remaining unmatched resources were deletions.
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
				ResourceID:               previousResourceInfo.ResourceID,
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
	// If mutationsPredicates is nil, then mutationPredicateMap will be empty.
	// When multiple mutation sources exist for the same resource (e.g., one from
	// clone and one from triggers), prefer the one with Predicate=true so that
	// the resource is not incorrectly filtered out during upgrade.
	mutationPredicateMap := make(map[api.ResourceTypeAndName]int)
	for i := range mutationsPredicates {
		resourceInfo := mutationsPredicates[i].Resource
		// ResourceNameWithoutScope is a new field so it may not be present in all cases.
		if resourceInfo.ResourceNameWithoutScope == "" {
			resourceInfo.ResourceNameWithoutScope = resourceProvider.RemoveScopeFromResourceName(resourceInfo.ResourceName)
		}
		resourceInfoKey := api.ResourceTypeAndNameFromResourceInfo(resourceInfo)
		if existingIdx, exists := mutationPredicateMap[resourceInfoKey]; exists {
			// Don't let a Predicate=false entry overwrite a Predicate=true entry
			if mutationsPredicates[existingIdx].ResourceMutationInfo.Predicate &&
				!mutationsPredicates[i].ResourceMutationInfo.Predicate {
				continue
			}
		}
		mutationPredicateMap[resourceInfoKey] = i
	}

	mutationPatchMap := make(map[api.ResourceTypeAndName]int)
	patchResourceIDMap := make(map[string]int)
	for i := range mutationsPatch {
		resourceInfo := mutationsPatch[i].Resource
		if resourceInfo.ResourceNameWithoutScope == "" {
			resourceInfo.ResourceNameWithoutScope = resourceProvider.RemoveScopeFromResourceName(resourceInfo.ResourceName)
		}
		resourceInfoKey := api.ResourceTypeAndNameFromResourceInfo(resourceInfo)
		mutationPatchMap[resourceInfoKey] = i
		if api.IsUUID(resourceInfo.ResourceID) {
			patchResourceIDMap[resourceInfo.ResourceID] = i
		}
	}

	predicateResourceIDMap := make(map[string]int)
	for i := range mutationsPredicates {
		if api.IsUUID(mutationsPredicates[i].Resource.ResourceID) {
			if existingIdx, exists := predicateResourceIDMap[mutationsPredicates[i].Resource.ResourceID]; exists {
				if mutationsPredicates[existingIdx].ResourceMutationInfo.Predicate &&
					!mutationsPredicates[i].ResourceMutationInfo.Predicate {
					continue
				}
			}
			predicateResourceIDMap[mutationsPredicates[i].Resource.ResourceID] = i
		}
	}

	// Track which patch mutations were matched to existing documents.
	// Unmatched Add/Replace mutations need to be appended as new documents.
	matchedPatchIndices := make(map[int]bool)

	for docIndex, doc := range parsedData {
		docResourceInfo, err := GetResourceInfo(doc, resourceProvider)
		if err != nil {
			return parsedData, err
		}
		resourceInfoKey := api.ResourceTypeAndNameFromResourceInfo(*docResourceInfo)

		// Try ResourceID-based predicate lookup first, fall back to name-based
		mutationPredicateIndex, hasPredicate := 0, false
		if api.IsUUID(docResourceInfo.ResourceID) {
			mutationPredicateIndex, hasPredicate = predicateResourceIDMap[docResourceInfo.ResourceID]
		}
		if !hasPredicate {
			mutationPredicateIndex, hasPredicate = mutationPredicateMap[resourceInfoKey]
		}

		// Filter the patch.
		if hasPredicate && !mutationsPredicates[mutationPredicateIndex].ResourceMutationInfo.Predicate {
			slog.Info("patch filtered", "resource", resourceInfoKey)
			continue
		}

		aliasInfo := api.ResourceInfo{
			ResourceType:     docResourceInfo.ResourceType,
			ResourceCategory: docResourceInfo.ResourceCategory,
		}
		var aliasInfoKey api.ResourceTypeAndName

		// FIXME: Remove this once all existing clones are converted to AliasesWithoutScopes.
		// TODO: This assumes the unit may be a clone, in which case it may be
		// patched from upstream. If the patch were from a clone to be applied
		// upstream, we'd need to get this information and pass it in.
		originalName, found, err := YamlSafePathGetValue[string](doc, api.ResolvedPath(originalNamePath), true)
		if err != nil {
			return parsedData, err
		}
		if found {
			aliasInfo.ResourceName = api.ResourceName(originalName)
			aliasInfo.ResourceNameWithoutScope = resourceProvider.RemoveScopeFromResourceName(api.ResourceName(originalName))
			aliasInfoKey = api.ResourceTypeAndNameFromResourceInfo(aliasInfo)
		}

		// Try ResourceID-based patch lookup first
		mutationPatchIndex, ok := 0, false
		if api.IsUUID(docResourceInfo.ResourceID) {
			mutationPatchIndex, ok = patchResourceIDMap[docResourceInfo.ResourceID]
		}
		if !ok {
			mutationPatchIndex, ok = mutationPatchMap[resourceInfoKey]
		}
		if !ok {
			// originalInfoKey might be "", but that's ok
			mutationPatchIndex, ok = mutationPatchMap[aliasInfoKey]
			if !ok {
				// If present, mutationsPredicates is expected to have been generated from the mutations
				// corresponding to the configuration data being patched. Therefore, it may
				// contain the aliases for resources present in the configuration.
				if hasPredicate {
					// We may have already checked a couple of these, but just check them all.
					for alias := range mutationsPredicates[mutationPredicateIndex].AliasesWithoutScopes {
						// TODO: This doesn't work for resource type changes, like Deployment -> StatefulSet
						// We don't need to set aliasInfo.ResourceName
						aliasInfo.ResourceNameWithoutScope = alias
						aliasInfoKey = api.ResourceTypeAndNameFromResourceInfo(aliasInfo)
						mutationPatchIndex, ok = mutationPatchMap[aliasInfoKey]
						if ok {
							break
						}
					}
				}
				if !ok {
					continue
				}
			}
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
				slog.Info("error parsing value for resource", "resource", string(resourceInfoKey), "error", err)
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

		// PathMutationMap is a map, which could be in arbitrary order.
		// We should process parents before children, so we copy the map into an array
		// and sort it.
		patches := make([]*api.MutationMapEntry, len(mutationsPatch[mutationPatchIndex].PathMutationMap))
		patchIndex := 0
		for patchPath, patchMutation := range mutationsPatch[mutationPatchIndex].PathMutationMap {
			patches[patchIndex] = &api.MutationMapEntry{
				Path:         patchPath,
				MutationInfo: &patchMutation,
			}
			patchIndex++
		}
		sort.Slice(patches, func(i, j int) bool {
			return patches[i].Path < patches[j].Path
		})

		for i := range patches {
			patchPath := api.ResolvedPath(ResolveAssociativeSegments(doc, string(patches[i].Path)))
			patchMutation := patches[i].MutationInfo
			// Check for patches that conflict with the patch.
			// TODO: Break down the patch.
			if hasPredicate {
				filtered := false
				// Check all path prefixes in the map bottom up. We use gaby.DotPathToSlice to handle
				// escaping and quoting, if any.
				pathSegments := gaby.DotPathToSlice(string(patchPath))
				for len(pathSegments) > 0 {
					filteredPath := JoinPathSegments(pathSegments)
					predicateMutation, hasFilter := mutationsPredicates[mutationPredicateIndex].PathMutationMap[api.ResolvedPath(filteredPath)]
					if hasFilter && !predicateMutation.Predicate {
						filtered = true
						break
					}
					pathSegments = pathSegments[:len(pathSegments)-1]
				}
				if filtered {
					slog.Debug("path filtered", "path", string(patchPath))
					continue
				}
			}
			// TODO: what should we do about errors?
			switch patchMutation.MutationType {
			case api.MutationTypeAdd, api.MutationTypeReplace:
				valueString := patchMutation.Value
				valueDoc, err := gaby.ParseYAML([]byte(valueString))
				if err != nil {
					slog.Info("error parsing value at path", "path", string(patchPath), "error", err)
				}
				// Note: This doesn't preserve indentation nor field ordering.
				_, err = doc.SetDocP(valueDoc, string(patchPath))
				if err != nil {
					slog.Info("error setting value at path", "path", string(patchPath), "error", err)
				}
			case api.MutationTypeUpdate:
				// For updates, try to preserve comments when possible
				valueString := patchMutation.Value
				valueDoc, err := gaby.ParseYAML([]byte(valueString))
				if err != nil {
					slog.Info("error parsing value at path", "path", string(patchPath), "error", err)
					continue
				}

				// Check if the value is a complex object (map or list) vs a scalar
				ynode := valueDoc.YNode()
				isScalarValue := ynode.Kind == yaml.ScalarNode

				if isScalarValue {
					// For scalar values, we need to preserve the comment manually
					// Get the current field to check if it has a comment
					currentField := doc.Path(string(patchPath))
					var existingComment string
					if currentField != nil {
						existingComment = currentField.GetComments()
					}

					// Set the new value
					_, err = doc.SetDocP(valueDoc, string(patchPath))
					if err != nil {
						slog.Info("error setting value at path", "path", string(patchPath), "error", err)
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
						slog.Info("error merging value at path", "path", string(patchPath), "error", err)
					}
				}
			case api.MutationTypeDelete:
				err := doc.DeleteP(string(patchPath))
				if err != nil {
					slog.Info("error deleting path", "path", string(patchPath), "error", err)
				}
			case api.MutationTypeNone:
				// Shouldn't happen for paths, but also shouldn't be anything to do
			}
		}
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
				slog.Info("error parsing value for unmatched resource",
					"resource", api.ResourceTypeAndNameFromResourceInfo(mutationsPatch[i].Resource), "error", err)
				continue
			}
			parsedData = append(parsedData, valueDoc)
		}
	}

	return parsedData, nil
}

func Reset(parsedData gaby.Container, mutationsPredicates api.ResourceMutationList, resourceProvider ResourceProvider) error {
	mutationPredicateMap := make(map[api.ResourceTypeAndName]int)
	resetResourceIDMap := make(map[string]int)
	for i := range mutationsPredicates {
		resourceInfo := mutationsPredicates[i].Resource
		if resourceInfo.ResourceNameWithoutScope == "" {
			resourceInfo.ResourceNameWithoutScope = resourceProvider.RemoveScopeFromResourceName(resourceInfo.ResourceName)
		}
		resourceInfoKey := api.ResourceTypeAndNameFromResourceInfo(resourceInfo)
		mutationPredicateMap[resourceInfoKey] = i
		if api.IsUUID(resourceInfo.ResourceID) {
			resetResourceIDMap[resourceInfo.ResourceID] = i
		}
	}

	// The ResourceID field must not be reset to a placeholder — it is intended to be
	// stable across the original unit and all clones, enabling cross-unit resource matching.
	resourceIDContextPath := resourceProvider.ContextPath(constants.ResourceIDKeySuffix)

	visitor := func(doc *gaby.YamlDoc, _ any, _ int, docResourceInfo *api.ResourceInfo) (any, []error) {
		resourceInfoKey := api.ResourceTypeAndNameFromResourceInfo(*docResourceInfo)

		// Try ResourceID-based lookup first, fall back to name-based
		mutationPredicateIndex, hasPredicate := 0, false
		if api.IsUUID(docResourceInfo.ResourceID) {
			mutationPredicateIndex, hasPredicate = resetResourceIDMap[docResourceInfo.ResourceID]
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
			// Never reset the ResourceID — it must remain stable across clones for matching.
			if resourceIDContextPath != "" && strings.HasSuffix(string(path), resourceIDContextPath) {
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

// FindMutationIndex looks up the mutation index for a specific resource and path
// in a ResourceMutationList. It matches the resource by ResourceTypeAndName,
// handling aliases and scope changes (same pattern as api.AddMutations).
// For the path, it walks up parent paths to find the most specific mutation index,
// falling back to the resource-level index if no path-level match is found.
// Returns the mutation index and true if found.
func FindMutationIndex(mutationSources api.ResourceMutationList, resource api.ResourceInfo, path api.ResolvedPath) (int64, bool) {
	// Build lookup maps by ResourceTypeAndName and ResourceID
	mutationMap := make(map[api.ResourceTypeAndName]int, len(mutationSources))
	findResourceIDMap := make(map[string]int, len(mutationSources))
	for i := range mutationSources {
		resourceInfo := mutationSources[i].Resource
		if resourceInfo.ResourceNameWithoutScope == "" {
			_, resourceNameWithoutScope, _ := strings.Cut(string(resourceInfo.ResourceName), "/")
			resourceInfo.ResourceNameWithoutScope = api.ResourceName(resourceNameWithoutScope)
		}
		mutationMap[api.ResourceTypeAndNameFromResourceInfo(resourceInfo)] = i
		if api.IsUUID(resourceInfo.ResourceID) {
			findResourceIDMap[resourceInfo.ResourceID] = i
		}
	}

	// Try ResourceID-based lookup first
	mi, found := 0, false
	if api.IsUUID(resource.ResourceID) {
		mi, found = findResourceIDMap[resource.ResourceID]
	}
	if !found {
		// Fall back to name-based lookup
		if resource.ResourceNameWithoutScope == "" {
			_, resourceNameWithoutScope, _ := strings.Cut(string(resource.ResourceName), "/")
			resource.ResourceNameWithoutScope = api.ResourceName(resourceNameWithoutScope)
		}
		mi, found = mutationMap[api.ResourceTypeAndNameFromResourceInfo(resource)]
	}
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
