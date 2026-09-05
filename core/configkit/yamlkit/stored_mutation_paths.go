// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package yamlkit

import (
	"maps"
	"slices"
	"strconv"

	"github.com/confighub/sdk/core/function/api"
	"github.com/confighub/sdk/core/third_party/gaby"
)

// A stored mutation record's path keys were written with the merge keys the process that wrote
// them knew about, and that set grows: registering a resource type is what gives its arrays
// merge keys, and every path recorded into one of them before that is positional.
//
// The two forms are not interchangeable. Once the type is registered a freshly computed diff
// produces containers.?name=runner.image while the stored key is still containers.0.image, and
// a lookup for one does not find the other -- so the protection recorded on that path is not
// found, and the next write records the same element a second time under the new key. What is
// stored has to be read with today's merge keys, not with the ones in force when it was written.
//
// Rewriting it needs the document the paths address, which is why this is not part of the
// candidate list storedMutationInfo tries: recovering an element's identity means reading what
// sits at the index. So it is done where a stored record meets its document -- on the way into
// PatchMutations, and before a Unit's record is persisted -- rather than in a migration, which
// would have to be written again for every type anyone registers and cannot be written at all
// for a type registered from a file at runtime.
//
// The common case costs nothing. A record whose keys have no positional segment at a merge-keyed
// array is returned as it came, without the document being consulted, which is every record
// written since its type was registered.

// CanonicalizeStoredMutationPaths rewrites the path keys of a stored mutation record into the
// form today's merge keys produce, resolving positional segments against parsedData. It returns
// the record unchanged, and false, when there is nothing to rewrite.
//
// Two entries that rewrite to one key are folded: they are the same element recorded at two
// positions, and the later one by MutationNum wins except that Protected survives from either,
// since a lost Protected silently reopens a path its owner had closed.
func CanonicalizeStoredMutationPaths(mutations api.ResourceMutationList, parsedData gaby.Container,
	resourceProvider ResourceProvider) (api.ResourceMutationList, bool) {
	if len(mutations) == 0 || resourceProvider == nil {
		return mutations, false
	}
	// Which resources have anything to rewrite, decided from the paths and the declared merge
	// keys alone. A record with nothing to do -- the overwhelming majority -- reaches no
	// further than this.
	rewriteNeeded := make([]bool, len(mutations))
	anyNeeded := false
	for i := range mutations {
		rewriteNeeded[i] = storedPathsNeedRewriting(&mutations[i], resourceProvider)
		anyNeeded = anyNeeded || rewriteNeeded[i]
	}
	if !anyNeeded {
		return mutations, false
	}

	rewritten := slices.Clone(mutations)
	changed := false
	for i := range rewritten {
		if !rewriteNeeded[i] {
			continue
		}
		if canonicalizeOneResourcesPaths(&rewritten[i], parsedData, resourceProvider) {
			changed = true
		}
	}
	if !changed {
		return mutations, false
	}
	return rewritten, true
}

// StoredMutationPathsNeedRewriting reports whether CanonicalizeStoredMutationPaths would have
// anything to do, without a document. It is the same question the canonicalization asks first,
// exported for a caller holding a record whose configuration data is not parsed yet: parsing it
// to discover there is nothing to rewrite is the cost worth avoiding.
func StoredMutationPathsNeedRewriting(mutations api.ResourceMutationList, resourceProvider ResourceProvider) bool {
	if resourceProvider == nil {
		return false
	}
	for i := range mutations {
		if storedPathsNeedRewriting(&mutations[i], resourceProvider) {
			return true
		}
	}
	return false
}

// storedPathsNeedRewriting reports whether any key of one resource's record is not in the form
// today's merge keys produce. It reads no document: dropping a ;@index fallback is a string
// operation, and whether a positional segment has a merge key to be named by is answered by the
// resource type's declaration.
func storedPathsNeedRewriting(mutation *api.ResourceMutation, resourceProvider ResourceProvider) bool {
	lookup := mergeKeyLookupFor(mutation.Resource.ResourceType, resourceProvider)
	for path := range mutation.PathMutationMap {
		if pathNeedsRewriting(path, lookup) {
			return true
		}
	}
	for path := range mutation.ArrayOrders {
		if pathNeedsRewriting(path, lookup) {
			return true
		}
	}
	for path := range mutation.ArrayElementAliases {
		if pathNeedsRewriting(path, lookup) {
			return true
		}
	}
	return false
}

func pathNeedsRewriting(path api.ResolvedPath, lookup MergeKeyLookup) bool {
	if CanonicalMutationPath(path) != path {
		return true
	}
	return hasMergeKeyedIndex(path, lookup)
}

// hasMergeKeyedIndex reports whether a path addresses an array element by position at an array
// its resource type declares merge keys for. A position in an array with no merge key --
// command.0, an argument list -- is the only form there is for that element and is not stale.
func hasMergeKeyedIndex(path api.ResolvedPath, lookup MergeKeyLookup) bool {
	segments := gaby.DotPathToSlice(string(path))
	for i, segment := range segments {
		if index, err := strconv.Atoi(segment); err != nil || index < 0 {
			continue
		}
		if _, declared := lookup(JoinPathSegments(segments[:i])); declared {
			return true
		}
	}
	return false
}

func mergeKeyLookupFor(resourceType api.ResourceType, resourceProvider ResourceProvider) MergeKeyLookup {
	return func(arrayPath string) ([]string, bool) {
		return resourceProvider.MergeKeysForPath(resourceType, arrayPath)
	}
}

// canonicalizeOneResourcesPaths rewrites one resource's keys in place on a copied entry.
func canonicalizeOneResourcesPaths(mutation *api.ResourceMutation, parsedData gaby.Container,
	resourceProvider ResourceProvider) bool {
	lookup := mergeKeyLookupFor(mutation.Resource.ResourceType, resourceProvider)
	resource := mutation.Resource
	doc, _ := FindResourceDoc(parsedData, resourceProvider, &resource)

	rewrite := func(path api.ResolvedPath) api.ResolvedPath {
		canonical := CanonicalMutationPath(path)
		if named, ok := NameArrayElementsByMergeKey(doc, canonical, lookup); ok {
			return named
		}
		return canonical
	}

	changed := false
	if len(mutation.PathMutationMap) > 0 {
		rekeyed := make(api.MutationMap, len(mutation.PathMutationMap))
		// Sorted, so that folding two entries onto one key does not depend on map iteration.
		for _, path := range slices.Sorted(maps.Keys(mutation.PathMutationMap)) {
			info := mutation.PathMutationMap[path]
			key := rewrite(path)
			if key != path {
				changed = true
			}
			if existing, collides := rekeyed[key]; collides {
				info = foldMutationInfo(existing, info)
				changed = true
			}
			rekeyed[key] = info
		}
		if changed {
			mutation.PathMutationMap = rekeyed
		}
	}
	if rekeyed, moved := rekeyByPath(mutation.ArrayOrders, rewrite); moved {
		mutation.ArrayOrders = rekeyed
		changed = true
	}
	if rekeyed, moved := rekeyByPath(mutation.ArrayElementAliases, rewrite); moved {
		mutation.ArrayElementAliases = rekeyed
		changed = true
	}
	return changed
}

// rekeyByPath rewrites the keys of one of the path-keyed side tables. A collision keeps the
// entry already there: unlike a MutationInfo these carry no ordering to pick a winner by, and
// both describe the same array.
func rekeyByPath[V any](table map[api.ResolvedPath]V, rewrite func(api.ResolvedPath) api.ResolvedPath) (map[api.ResolvedPath]V, bool) {
	if len(table) == 0 {
		return table, false
	}
	rekeyed := make(map[api.ResolvedPath]V, len(table))
	changed := false
	for _, path := range slices.Sorted(maps.Keys(table)) {
		key := rewrite(path)
		if key != path {
			changed = true
		}
		if _, collides := rekeyed[key]; collides {
			changed = true
			continue
		}
		rekeyed[key] = table[path]
	}
	if !changed {
		return table, false
	}
	return rekeyed, true
}

// foldMutationInfo merges two entries that rewrite to the same key -- the same element, recorded
// twice at different positions. In a stored record Index is the MutationNum that wrote the
// entry, so it orders the two.
func foldMutationInfo(existing, later api.MutationInfo) api.MutationInfo {
	winner, loser := later, existing
	if existing.Index > later.Index {
		winner, loser = existing, later
	}
	if loser.Protected {
		winner.Protected = true
	}
	return winner
}
