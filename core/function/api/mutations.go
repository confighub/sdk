// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package api

import (
	"fmt"
	"strings"
)

// NOTE: Move is not currently supported.

// Mutation Type is the type of mutation performed on the associated configuration element.
type MutationType string

const (
	// No mutations at the level.
	MutationTypeNone MutationType = "None"
	// Add new configuration at the level.
	MutationTypeAdd MutationType = "Add"
	// Update part of the configuration at the level.
	MutationTypeUpdate MutationType = "Update"
	// Replace all of the configuration at the level (equivalent of Delete + Add).
	MutationTypeReplace MutationType = "Replace"
	// Delete the configuration at the level.
	MutationTypeDelete MutationType = "Delete"
)

type ResourceMutation struct {
	Resource             ResourceInfo              `description:"Identifiers of the resource to which the mutations correspond"`
	ResourceMutationInfo MutationInfo              `description:"Resource-level mutation information, such as for Add, Delete, or Replace"`
	PathMutationMap      MutationMap               `description:"Path-level mutation information; more deeply nested paths override values represented at higher levels"`
	Aliases              map[ResourceName]struct{} `json:",omitempty" description:"Names (with scopes, if any) used in current and prior revisions of this resource"`
	AliasesWithoutScopes map[ResourceName]struct{} `json:",omitempty" description:"Names without scopes used in current and prior revisions of this resource"`
}
type ResourceMutationList []ResourceMutation

type MutationMap map[ResolvedPath]MutationInfo

// TODO: should Value be []byte?
// NOTE: If we put a comment on MutationType, then the go client generator incorrectly wraps
// it with an object and the JSON can't be decoded properly.
// Comment: type of mutation performed on the associated configuration element

type MutationInfo struct {
	MutationType MutationType `description:"Type of mutation performed on the associated configuration element: Add, Update, Replace, Delete, or None, if no change"`
	Index        int64        `description:"Function index or sequence number corresponding to the change"`
	Predicate    bool         `description:"Used to decide how to use the mututation"`
	Value        string       `description:"Removed configuration data if MutationType is Delete and otherwise the new data"`
}

type MutationMapEntry struct {
	Path         ResolvedPath
	MutationInfo *MutationInfo
}

// IsUUID returns true if s looks like a valid UUID (8-4-4-4-12 hex format).
// Used to avoid matching on placeholder or other non-UUID ResourceID values.
func IsUUID(s string) bool {
	if len(s) != 36 {
		return false
	}
	for i, c := range s {
		if i == 8 || i == 13 || i == 18 || i == 23 {
			if c != '-' {
				return false
			}
		} else if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')) {
			return false
		}
	}
	return true
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
func AddMutations(mutations, newMutations ResourceMutationList) ResourceMutationList {
	// This can't take the ResourceProvider as a parameter. That's currently defined in yamlkit.
	// This isn't in yamlkit currently because it's used in places that deal with mutations
	// for arbitrary ToolchainTypes outside of functions. I may move it to yamlkit at some point.
	mutationMap := make(map[ResourceTypeAndName]int)
	addResourceIDMap := make(map[string]int)
	for i := range mutations {
		resourceInfo := mutations[i].Resource
		if resourceInfo.ResourceNameWithoutScope == "" {
			// We can't call resourceProvider.RemoveScopeFromResourceName here.
			// FIXME: Remove this once ResourceNameWithoutScope is fully populated.
			_, resourceNameWithoutScope, _ := strings.Cut(string(resourceInfo.ResourceName), "/")
			resourceInfo.ResourceNameWithoutScope = ResourceName(resourceNameWithoutScope)
		}
		mutationMap[ResourceTypeAndNameFromResourceInfo(resourceInfo)] = i
		if IsUUID(resourceInfo.ResourceID) {
			addResourceIDMap[resourceInfo.ResourceID] = i
		}
		// The AliasesWithoutScopes in the existing mutations shouldn't be relevant, because those
		// were names used in the past, not the current or new names.
	}
	for i := range newMutations {
		resourceInfo := newMutations[i].Resource
		if resourceInfo.ResourceNameWithoutScope == "" {
			// We can't call resourceProvider.RemoveScopeFromResourceName here.
			// FIXME: Remove this once all workers have been updated to populate
			// ResourceNameWithoutScope.
			_, resourceNameWithoutScope, _ := strings.Cut(string(resourceInfo.ResourceName), "/")
			resourceInfo.ResourceNameWithoutScope = ResourceName(resourceNameWithoutScope)
		}
		// Try ResourceID-based lookup first
		mi, present := 0, false
		if IsUUID(resourceInfo.ResourceID) {
			mi, present = addResourceIDMap[resourceInfo.ResourceID]
		}
		if !present {
			mi, present = mutationMap[ResourceTypeAndNameFromResourceInfo(resourceInfo)]
		}
		if !present {
			// If the name has changed, then we need to check the AliasesWithoutScopes in newMutations.
			for alias := range newMutations[i].AliasesWithoutScopes {
				// We don't need to update resourceInfo.ResourceName
				resourceInfo.ResourceNameWithoutScope = alias
				mi, present = mutationMap[ResourceTypeAndNameFromResourceInfo(resourceInfo)]
				if present {
					break
				}
			}
			if !present {
				mutations = append(mutations, newMutations[i])
				continue
			}
		}
		if newMutations[i].ResourceMutationInfo.MutationType == MutationTypeNone {
			continue
		}
		if newMutations[i].ResourceMutationInfo.MutationType == MutationTypeDelete ||
			newMutations[i].ResourceMutationInfo.MutationType == MutationTypeReplace ||
			mutations[mi].ResourceMutationInfo.MutationType == MutationTypeNone {
			mutations[mi] = newMutations[i]
			continue
		}
		if mutations[mi].ResourceMutationInfo.MutationType == MutationTypeDelete {
			mutations[mi] = newMutations[i]
			mutations[mi].ResourceMutationInfo.MutationType = MutationTypeReplace
			continue
		}

		// Update the resource name, which may have changed.
		mutations[mi].Resource.ResourceName = newMutations[i].Resource.ResourceName
		mutations[mi].Resource.ResourceNameWithoutScope = newMutations[i].Resource.ResourceNameWithoutScope
		if mutations[mi].Aliases == nil {
			mutations[mi].Aliases = make(map[ResourceName]struct{})
		}
		for alias := range newMutations[i].Aliases {
			mutations[mi].Aliases[alias] = struct{}{}
		}
		if mutations[mi].AliasesWithoutScopes == nil {
			mutations[mi].AliasesWithoutScopes = make(map[ResourceName]struct{})
		}
		for alias := range newMutations[i].AliasesWithoutScopes {
			mutations[mi].AliasesWithoutScopes[alias] = struct{}{}
		}

		// Merge the path mutations. The overall MutationType, Add or Update or Replace, should remain the same.
		// If newMutations contains a path that's a prefix of paths in mutations, we need to remove them.
		// If the path matches, then we need to look at the existing MutationType.
		// Otherwise we add the path.
		// TODO: Make this not quadratic. See PatchMutations.
		for path, mutation := range newMutations[i].PathMutationMap {
			updated := false
			for existingPath := range mutations[mi].PathMutationMap {
				if existingPath == path {
					mutationType := mutations[mi].PathMutationMap[existingPath].MutationType
					if mutationType == MutationTypeDelete &&
						mutation.MutationType != MutationTypeDelete {
						mutations[mi].PathMutationMap[existingPath] = MutationInfo{
							MutationType: MutationTypeReplace,
							Index:        mutation.Index,
							Predicate:    mutation.Predicate,
							Value:        mutation.Value,
						}
					} else {
						// mutationType should be Add, Update, or Replace
						mutations[mi].PathMutationMap[existingPath] = MutationInfo{
							MutationType: mutationType,
							Index:        mutation.Index,
							Predicate:    mutation.Predicate,
							Value:        mutation.Value,
						}
					}
					updated = true
				} else if strings.HasPrefix(string(existingPath), string(path)+".") {
					delete(mutations[mi].PathMutationMap, existingPath)
				}
			}
			if !updated {
				mutations[mi].PathMutationMap[path] = mutation
			}
		}
	}
	return mutations
}

func OffsetMutations(mutations ResourceMutationList, offset int64) {
	for i := range mutations {
		mutations[i].ResourceMutationInfo.Index += offset
		for path, mutationInfo := range mutations[i].PathMutationMap {
			mutationInfo.Index += offset
			mutations[i].PathMutationMap[path] = mutationInfo
		}
	}
}

func NoMutations(mutations ResourceMutationList) bool {
	for _, resourceMutations := range mutations {
		if resourceMutations.ResourceMutationInfo.MutationType != MutationTypeNone {
			return false
		}
	}
	return true
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
func SubtractMutations(mutations, subtractMutations ResourceMutationList) ResourceMutationList {
	// Build maps of mutations to subtract, keyed by ResourceTypeAndName and ResourceID
	subtractMap := make(map[ResourceTypeAndName]int)
	subtractResourceIDMap := make(map[string]int)
	for i := range subtractMutations {
		resourceInfo := subtractMutations[i].Resource
		if resourceInfo.ResourceNameWithoutScope == "" {
			_, resourceNameWithoutScope, _ := strings.Cut(string(resourceInfo.ResourceName), "/")
			resourceInfo.ResourceNameWithoutScope = ResourceName(resourceNameWithoutScope)
		}
		subtractMap[ResourceTypeAndNameFromResourceInfo(resourceInfo)] = i
		if IsUUID(resourceInfo.ResourceID) {
			subtractResourceIDMap[resourceInfo.ResourceID] = i
		}
	}

	result := make(ResourceMutationList, 0, len(mutations))

	for i := range mutations {
		mutation := mutations[i]
		resourceInfo := mutation.Resource
		if resourceInfo.ResourceNameWithoutScope == "" {
			_, resourceNameWithoutScope, _ := strings.Cut(string(resourceInfo.ResourceName), "/")
			resourceInfo.ResourceNameWithoutScope = ResourceName(resourceNameWithoutScope)
		}

		// Find matching subtraction mutation — try ResourceID first
		si, found := 0, false
		if IsUUID(resourceInfo.ResourceID) {
			si, found = subtractResourceIDMap[resourceInfo.ResourceID]
		}
		if !found {
			si, found = subtractMap[ResourceTypeAndNameFromResourceInfo(resourceInfo)]
		}
		if !found {
			// Try aliases from the mutation being subtracted
			for alias := range mutation.AliasesWithoutScopes {
				resourceInfo.ResourceNameWithoutScope = alias
				si, found = subtractMap[ResourceTypeAndNameFromResourceInfo(resourceInfo)]
				if found {
					break
				}
			}
		}
		if !found {
			// Also check aliases from the subtraction mutations
			for si := range subtractMutations {
				subResourceInfo := subtractMutations[si].Resource
				for alias := range subtractMutations[si].AliasesWithoutScopes {
					subResourceInfo.ResourceNameWithoutScope = alias
					if ResourceTypeAndNameFromResourceInfo(subResourceInfo) == ResourceTypeAndNameFromResourceInfo(mutation.Resource) {
						found = true
						break
					}
				}
				if found {
					break
				}
			}
		}

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
		// It's api.AddMutations that converts Delete followed by Add to Replace and
		// Add followed by Update to just Add. We handle Replace here just in case, but
		// don't really expect to see it.
		// Update at the resource level will have an empty Value -- all of the values will be
		// associated with paths.

		// If the resource was deleted in subtractMutations, don't include any changes to it
		if subtractMutation.ResourceMutationInfo.MutationType == MutationTypeDelete {
			// Resource was deleted in target - don't include any source changes
			continue
		}

		// If the resource was replaced in subtractMutations, remove it entirely
		// (the target has completely redefined this resource)
		if subtractMutation.ResourceMutationInfo.MutationType == MutationTypeReplace {
			// Resource was replaced in target - don't include source changes
			// Replace means the target deleted and re-added the resource.
			continue
		}

		// Otherwise the resource was not deleted in the target

		// Handle MutationType None
		// in mutation - changes nothing, so it's safe to keep
		// in subtractMutation - the target didn't override anything, so keep it
		if mutation.ResourceMutationInfo.MutationType == MutationTypeNone ||
			subtractMutation.ResourceMutationInfo.MutationType == MutationTypeNone {
			result = append(result, mutation)
			continue
		}

		// If the mutation is a Delete at resource level, and the subtraction is Update or Add,
		// then the target has modified paths and the deletion should not be allowed.
		if mutation.ResourceMutationInfo.MutationType == MutationTypeDelete {
			// Target modified the resource, don't delete it
			continue
		}

		// If the resource was added or updated in subtractMutations and independently added,
		// replaced, or updated in mutations, then merge the two.

		// For Update at resource level, we need to filter out path mutations
		// that were changed in the target
		newMutation := ResourceMutation{
			Resource:             mutation.Resource,
			ResourceMutationInfo: mutation.ResourceMutationInfo,
			PathMutationMap:      make(MutationMap),
			Aliases:              mutation.Aliases,
			AliasesWithoutScopes: mutation.AliasesWithoutScopes,
		}

		// Process each path mutation
		for path, pathMutation := range mutation.PathMutationMap {
			// Case 1: Exact match - subtract this path
			if _, found := subtractMutation.PathMutationMap[path]; found {
				continue
			}

			// Case 2: Check if a subtract path is a prefix of a mutation path
			// e.g., subtract has spec.containers.0 and we have spec.containers.0.image
			// In this case, the target changed a parent, so don't apply child changes
			// Note: Alternatively we could lookup all prefixes in the map.
			shouldSubtract := false
			for subtractPath := range subtractMutation.PathMutationMap {
				if strings.HasPrefix(string(path), string(subtractPath)+".") {
					shouldSubtract = true
					break
				}
			}

			if shouldSubtract {
				continue
			}

			// Case 3: A mutation path is a prefix of a subtract path
			// e.g., we have spec.containers.0 (a large block) and subtract has spec.containers.0.image
			// Check if any subtractMutation paths are children of this mutation path
			// TODO: Sort the paths to make this more efficient.
			hasChildSubtract := false
			for subtractPath := range subtractMutation.PathMutationMap {
				if strings.HasPrefix(string(subtractPath), string(path)+".") {
					hasChildSubtract = true
					break
				}
			}
			if hasChildSubtract {
				// If the mutation is a Delete, we can't partially un-delete, so skip it entirely
				if pathMutation.MutationType == MutationTypeDelete {
					continue
				}
				// Keep the mutation path and add the subtractMutation paths that override it.
				// Since PatchMutations processes paths from least specific to most specific,
				// the subtractMutation's more specific paths will override the mutation's value.
				newMutation.PathMutationMap[path] = pathMutation
				for subtractPath, subtractPathMutation := range subtractMutation.PathMutationMap {
					if strings.HasPrefix(string(subtractPath), string(path)+".") {
						newMutation.PathMutationMap[subtractPath] = subtractPathMutation
					}
				}
			} else {
				// No child subtract paths, keep the mutation as-is
				newMutation.PathMutationMap[path] = pathMutation
			}
		}

		// If we removed all path mutations and the resource-level mutation was Update,
		// convert to None
		if len(newMutation.PathMutationMap) == 0 &&
			(newMutation.ResourceMutationInfo.MutationType == MutationTypeUpdate) {
			newMutation.ResourceMutationInfo.MutationType = MutationTypeNone
		}

		// Only add if there's something left
		if newMutation.ResourceMutationInfo.MutationType != MutationTypeNone || len(result) > 0 {
			result = append(result, newMutation)
		}
	}

	return result
}

// AttributeValueListToResourceMutationList converts an AttributeValueList to an equivalent
// ResourceMutationList by grouping attribute values by resource and creating Update mutations
// for each attribute path.
func AttributeValueListToResourceMutationList(avl AttributeValueList) ResourceMutationList {
	resourceMap := make(map[ResourceTypeAndName]int)
	var mutations ResourceMutationList

	for _, av := range avl {
		key := ResourceTypeAndNameFromResourceInfo(av.ResourceInfo)
		idx, exists := resourceMap[key]
		if !exists {
			idx = len(mutations)
			resourceMap[key] = idx
			mutations = append(mutations, ResourceMutation{
				Resource:             av.ResourceInfo,
				ResourceMutationInfo: MutationInfo{MutationType: MutationTypeUpdate},
				PathMutationMap:      make(MutationMap),
			})
		}
		mutations[idx].PathMutationMap[av.Path] = MutationInfo{
			MutationType: MutationTypeUpdate,
			Value:        fmt.Sprint(av.Value),
		}
	}

	return mutations
}
