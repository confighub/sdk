// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package api

import (
	"fmt"
	"slices"
	"sort"
	"strconv"
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
	ArrayOrders          ArrayOrderMap             `json:",omitempty" description:"For merge-keyed arrays whose element set or order changed in this mutation: the desired sequence of merge-key values in source order, keyed by the array's parent path. PatchMutations applies this as a reorder pass after path mutations so positional associative arrays (e.g., Kubernetes initContainers, env, ports) preserve source-side ordering rather than landing append-on-clash."`
	ArrayElementAliases  ArrayElementAliasMap      `json:",omitempty" description:"For merge-keyed arrays in which an element was renamed (its merge-key value changed): the array parent path mapped to a previous-merge-key -> new-merge-key map. PatchMutations rewrites the matched element's merge-key field accordingly so child paths and ArrayOrders entries resolve under the new key."`
	Aliases              map[ResourceName]struct{} `json:",omitempty" description:"Names (with scopes, if any) used in current and prior revisions of this resource"`
	AliasesWithoutScopes map[ResourceName]struct{} `json:",omitempty" description:"Names without scopes used in current and prior revisions of this resource"`
}
type ResourceMutationList []ResourceMutation

type MutationMap map[ResolvedPath]MutationInfo

// ArrayOrderMap records, for each merge-keyed array path whose element set or
// order changed in a ResourceMutation, the desired sequence of merge-key values
// in source order. PatchMutations consumes this to reorder the target's array
// after path mutations are applied, so positional associative arrays (e.g.,
// Kubernetes initContainers, env, ports) preserve source-side ordering rather
// than collecting Adds at the end.
type ArrayOrderMap map[ResolvedPath][]string

// ArrayElementAliasMap records merge-key renames detected at the element level
// inside a merge-keyed array. The outer key is the array's parent path; the
// inner map is from the previous merge-key value to the new merge-key value.
// PatchMutations rewrites the merge-key field on each affected element after
// applying path mutations and before the reorder pass, so child paths and
// ArrayOrders entries (which use the new key) resolve correctly while
// SubtractMutations (which compares paths between source and target diffs)
// can keep using the previous key — the diff path encoded in PathMutationMap
// stays under the previous key for that reason.
type ArrayElementAliasMap map[ResolvedPath]map[string]string

// TODO: should Value be []byte?
// NOTE: If we put a comment on MutationType, then the go client generator incorrectly wraps
// it with an object and the JSON can't be decoded properly.
// Comment: type of mutation performed on the associated configuration element

type MutationInfo struct {
	MutationType MutationType `description:"Type of mutation performed on the associated configuration element: Add, Update, Replace, Delete, or None, if no change"`
	Index        int64        `description:"Function index or sequence number corresponding to the change"`
	Predicate    bool         `description:"Used to decide how to use the mututation"`
	Value        string       `description:"Removed configuration data if MutationType is Delete and otherwise the new data"`
	Patch        string       `json:",omitempty" description:"Line-level patch for multi-line string updates, in unified diff format. When present on an Update, PatchMutations applies this to the target value instead of replacing with Value. Falls back to Value if the patch cannot be applied cleanly."`
}

type MutationMapEntry struct {
	Path         ResolvedPath
	MutationInfo *MutationInfo
}

// ConflictReason describes why a mutation was dropped from a patch instead of
// being applied. Surfaced via SubtractMutations and PatchMutations.
type ConflictReason string

const (
	// ConflictReasonSubtracted: the source mutation was removed by
	// SubtractMutations because the target had its own mutation at the same
	// path or under an ancestor of the source path.
	ConflictReasonSubtracted ConflictReason = "Subtracted"
	// ConflictReasonDeleteShadowed: the source mutation was a Delete on a
	// path or resource and the target had child mutations under it. The
	// Delete still applies (the parent is gone, so the target's child
	// edits have nowhere to live); this conflict surfaces each lost target
	// mutation so the caller can see what was discarded. The conflict's
	// Path is the displaced target path; Source is the source-side parent
	// Delete; Target is the displaced target mutation.
	ConflictReasonDeleteShadowed ConflictReason = "DeleteShadowed"
	// ConflictReasonPredicateFiltered: a path or resource in the patch was
	// skipped by PatchMutations because the corresponding predicate had
	// Predicate=false (directly or via an ancestor path).
	ConflictReasonPredicateFiltered ConflictReason = "PredicateFiltered"
	// ConflictReasonUnresolvedPath: a path in the patch couldn't be fully
	// resolved against the target document — typically because the
	// merge-key value at an associative segment didn't match any element
	// in the target. PatchMutations skips Update and Delete in this case.
	// (Add/Replace falls back to appending instead and does not produce
	// this conflict.)
	ConflictReasonUnresolvedPath ConflictReason = "UnresolvedPath"
)

// MutationConflict records that a mutation in a patch was not applied because
// of an interaction with the target's own state (subtraction, predicate filter,
// or unresolved associative path). Conflicts are advisory: the patched output
// is correct as-is, but the conflict tells the caller what part of the patch
// they wanted to apply was dropped, so they can surface it as a merge issue.
type MutationConflict struct {
	Reason   ConflictReason `description:"Why the mutation was dropped"`
	Resource ResourceInfo   `description:"Resource the mutation applied to"`
	Path     ResolvedPath   `json:",omitempty" description:"Path of the mutation; empty for resource-level conflicts"`
	Source   MutationInfo   `description:"The dropped source-side mutation"`
	Target   *MutationInfo  `json:",omitempty" description:"The target-side mutation that caused the drop, when applicable (Subtracted, DeleteShadowed)"`
}

type MutationConflictList []MutationConflict

func HasMutations(mutations ResourceMutationList) bool {
	for i := range mutations {
		if mutations[i].ResourceMutationInfo.MutationType != MutationTypeNone {
			return true
		}
	}
	return false
}

// SortedMutationMapEntries returns the entries of a MutationMap sorted by path.
// Paths are split into segments on "." (dots within segments are escaped as ~1).
// Numeric segments are compared numerically so that array indices sort correctly
// (e.g., "env.2" < "env.10"). Parent paths sort before children.
func SortedMutationMapEntries(m MutationMap) []MutationMapEntry {
	entries := make([]MutationMapEntry, 0, len(m))
	for path, info := range m {
		infoCopy := info
		entries = append(entries, MutationMapEntry{Path: path, MutationInfo: &infoCopy})
	}
	slices.SortFunc(entries, func(a, b MutationMapEntry) int {
		return comparePathsNumerically(string(a.Path), string(b.Path))
	})
	return entries
}

// comparePathsNumerically compares two dot-separated paths segment by segment.
// Numeric segments are compared as integers; non-numeric segments are compared
// lexicographically. Shorter paths sort before longer paths when all leading
// segments are equal (parent before child).
func comparePathsNumerically(a, b string) int {
	segsA := strings.Split(a, ".")
	segsB := strings.Split(b, ".")
	n := min(len(segsA), len(segsB))
	for k := range n {
		sa, sb := segsA[k], segsB[k]
		na, errA := strconv.Atoi(sa)
		nb, errB := strconv.Atoi(sb)
		if errA == nil && errB == nil {
			if na != nb {
				if na < nb {
					return -1
				}
				return 1
			}
			continue
		}
		if sa != sb {
			if sa < sb {
				return -1
			}
			return 1
		}
	}
	if len(segsA) < len(segsB) {
		return -1
	}
	if len(segsA) > len(segsB) {
		return 1
	}
	return 0
}

// PathPrefixIndex provides efficient lookup of child paths (paths that start with a
// given prefix + ".") using binary search on a sorted slice of paths. It is built from
// a MutationMap and used by AddMutations and SubtractMutations to avoid iterating over
// all paths when checking for prefix relationships.
type PathPrefixIndex struct {
	SortedPaths []ResolvedPath
}

// NewPathPrefixIndex builds a PathPrefixIndex from a MutationMap.
func NewPathPrefixIndex(m MutationMap) *PathPrefixIndex {
	paths := make([]ResolvedPath, 0, len(m))
	for path := range m {
		paths = append(paths, path)
	}
	slices.Sort(paths)
	return &PathPrefixIndex{SortedPaths: paths}
}

// HasChildPath returns true if any path in the index starts with prefix + ".".
// Uses binary search for O(log n) performance.
func (idx *PathPrefixIndex) HasChildPath(prefix ResolvedPath) bool {
	target := prefix + "."
	// Binary search for the first path >= target
	i := sort.Search(len(idx.SortedPaths), func(i int) bool {
		return idx.SortedPaths[i] >= target
	})
	return i < len(idx.SortedPaths) && strings.HasPrefix(string(idx.SortedPaths[i]), string(target))
}

// ChildPaths returns all paths in the index that start with prefix + ".".
// Uses binary search to find the range, returning O(log n + k) where k is the result count.
func (idx *PathPrefixIndex) ChildPaths(prefix ResolvedPath) []ResolvedPath {
	target := prefix + "."
	// Binary search for the first path >= target
	start := sort.Search(len(idx.SortedPaths), func(i int) bool {
		return idx.SortedPaths[i] >= target
	})
	var result []ResolvedPath
	for i := start; i < len(idx.SortedPaths); i++ {
		if !strings.HasPrefix(string(idx.SortedPaths[i]), string(target)) {
			break
		}
		result = append(result, idx.SortedPaths[i])
	}
	return result
}

// FindAncestorPath walks up from path to find the most specific ancestor (or exact match)
// present in the map. It iteratively removes the last path segment and checks for a match.
// Since dots within segments are escaped as ~1, splitting on "." is safe.
// Returns the matching path, its MutationInfo, and whether a match was found.
func FindAncestorPath(m MutationMap, path ResolvedPath) (ResolvedPath, MutationInfo, bool) {
	p := string(path)
	for {
		if info, ok := m[ResolvedPath(p)]; ok {
			return ResolvedPath(p), info, true
		}
		lastDot := strings.LastIndex(p, ".")
		if lastDot < 0 {
			break
		}
		p = p[:lastDot]
	}
	return "", MutationInfo{}, false
}

// HasAncestorPath returns true if any ancestor of path (not including path itself) exists
// in the map. An ancestor is a path formed by removing one or more trailing segments.
func HasAncestorPath(m MutationMap, path ResolvedPath) bool {
	p := string(path)
	for {
		lastDot := strings.LastIndex(p, ".")
		if lastDot < 0 {
			break
		}
		p = p[:lastDot]
		if _, ok := m[ResolvedPath(p)]; ok {
			return true
		}
	}
	return false
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

// ResourceMutationIndex provides efficient lookup of resources in a ResourceMutationList
// by ResourceID, ResourceTypeAndName, and AliasesWithoutScopes. It is used by AddMutations,
// SubtractMutations, PatchMutations, and FindMutationIndex to match resources consistently.
type ResourceMutationIndex struct {
	NameMap            map[ResourceTypeAndName]int
	AliasNameMap       map[ResourceTypeAndName]int // from AliasesWithoutScopes of indexed mutations
	ResourceMergeIDMap map[string]int
}

// NewResourceMutationIndex builds an index from a ResourceMutationList.
// For duplicate keys, the last entry wins.
func NewResourceMutationIndex(mutations ResourceMutationList) *ResourceMutationIndex {
	idx := &ResourceMutationIndex{
		NameMap:            make(map[ResourceTypeAndName]int, len(mutations)),
		AliasNameMap:       make(map[ResourceTypeAndName]int, len(mutations)),
		ResourceMergeIDMap: make(map[string]int, len(mutations)),
	}
	for i := range mutations {
		resourceInfo := mutations[i].Resource
		if IsUUID(resourceInfo.ResourceMergeID) {
			idx.ResourceMergeIDMap[resourceInfo.ResourceMergeID] = i
		}
		if resourceInfo.ResourceNameWithoutScope == "" {
			_, resourceNameWithoutScope, _ := strings.Cut(string(resourceInfo.ResourceName), "/")
			resourceInfo.ResourceNameWithoutScope = ResourceName(resourceNameWithoutScope)
		}
		idx.NameMap[ResourceTypeAndNameFromResourceInfo(resourceInfo)] = i
		for alias := range mutations[i].AliasesWithoutScopes {
			aliasInfo := ResourceInfo{
				ResourceType:             resourceInfo.ResourceType,
				ResourceCategory:         resourceInfo.ResourceCategory,
				ResourceNameWithoutScope: alias,
			}
			aliasKey := ResourceTypeAndNameFromResourceInfo(aliasInfo)
			if _, exists := idx.AliasNameMap[aliasKey]; !exists {
				idx.AliasNameMap[aliasKey] = i
			}
		}
	}
	return idx
}

// Find looks up a resource in the index using multiple strategies:
//  1. ResourceMergeID match
//  2. ResourceTypeAndName match
//  3. callerAliases against indexed mutation names
//  4. Indexed mutation aliases against the caller's name
func (idx *ResourceMutationIndex) Find(resource ResourceInfo, callerAliases map[ResourceName]struct{}) (int, bool) {
	if resource.ResourceNameWithoutScope == "" {
		_, resourceNameWithoutScope, _ := strings.Cut(string(resource.ResourceName), "/")
		resource.ResourceNameWithoutScope = ResourceName(resourceNameWithoutScope)
	}
	// 1. ResourceMergeID
	if IsUUID(resource.ResourceMergeID) {
		if i, ok := idx.ResourceMergeIDMap[resource.ResourceMergeID]; ok {
			return i, true
		}
	}
	// 2. ResourceTypeAndName
	key := ResourceTypeAndNameFromResourceInfo(resource)
	if i, ok := idx.NameMap[key]; ok {
		return i, true
	}
	// 3. Caller's aliases against indexed names
	for alias := range callerAliases {
		aliasInfo := resource
		aliasInfo.ResourceNameWithoutScope = alias
		if i, ok := idx.NameMap[ResourceTypeAndNameFromResourceInfo(aliasInfo)]; ok {
			return i, true
		}
	}
	// 4. Indexed mutation aliases against caller's name
	if i, ok := idx.AliasNameMap[key]; ok {
		return i, true
	}
	return 0, false
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
