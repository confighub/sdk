// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package api

import "strings"

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

// AddMutations merges newMutations into (existing) mutations and returns the result.
func AddMutations(mutations, newMutations ResourceMutationList) ResourceMutationList {
	// This can't take the ResourceProvider as a parameter. That's currently defined in yamlkit.
	// This isn't in yamlkit currently because it's used in places that deal with mutations
	// for arbitrary ToolchainTypes outside of functions. I may move it to yamlkit at some point.
	mutationMap := make(map[ResourceTypeAndName]int)
	for i := range mutations {
		resourceInfo := mutations[i].Resource
		if resourceInfo.ResourceNameWithoutScope == "" {
			// We can't call resourceProvider.RemoveScopeFromResourceName here.
			// FIXME: Remove this once ResourceNameWithoutScope is fully populated.
			_, resourceNameWithoutScope, _ := strings.Cut(string(resourceInfo.ResourceName), "/")
			resourceInfo.ResourceNameWithoutScope = ResourceName(resourceNameWithoutScope)
		}
		mutationMap[ResourceTypeAndNameFromResourceInfo(resourceInfo)] = i
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
		mi, present := mutationMap[ResourceTypeAndNameFromResourceInfo(resourceInfo)]
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
				if strings.HasPrefix(string(existingPath), string(path)) {
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
					} else {
						delete(mutations[mi].PathMutationMap, existingPath)
					}
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
