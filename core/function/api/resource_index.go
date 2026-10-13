// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package api

import "slices"

// ResourceIndex finds a resource in a list of resources by the rule every comparison of resource
// identity uses:
//
//  1. The same type, without its API version, and the same name, scope included.
//  2. Otherwise the same type and the same name without its scope, when that names exactly one
//     resource in the list.
//
// The second step is what lets a resource be found after its scope changed: a variant replaces
// a confighubplaceholder namespace with its own, set-namespace moves a resource, and a clone's
// Links and conflicts still name the resource in the namespace it had upstream. The first step,
// and the uniqueness the second requires, are what keep apart two resources of one type and name
// in different namespaces of one Unit, such as a Role in kube-system and a Role of the same name
// in the chart's own namespace. A name that matches several of them without its scope matches
// none, rather than whichever comes first or last.
//
// Uniqueness is required on both sides when the index knows the resources it is matched against
// (Counterparts): a stored record of blue/app must not be found for green/app in a configuration
// that has blue/app too, just because the record has no green/app of its own.
//
// Entries that record one resource -- the same type and name, scope included -- are one match,
// and the index chooses among them with Choose.
type ResourceIndex struct {
	identifier         ResourceIdentifier
	names              []ResourceName
	byName             map[ResourceTypeAndName][]int
	byNameWithoutScope map[ResourceTypeAndName][]int
	// counterparts counts, by type and name without scope, the resources the index is matched
	// against, when they are known; nil when not.
	counterparts map[ResourceTypeAndName]map[ResourceName]bool

	// Choose picks one of several entries that record the same resource. It is given their
	// positions in ascending order. The default is the last.
	Choose func(positions []int) int
}

// ResourceMatch is the outcome of looking a resource up in a ResourceIndex.
type ResourceMatch struct {
	// Position is the matched entry's position in the indexed list, when Found.
	Position int
	Found    bool
	// Ambiguous lists the names, scopes included, of the resources the name matched without its
	// scope, when it matched more than one of them and therefore none.
	Ambiguous []ResourceName
}

// NewResourceIndex indexes resources, keyed as the identifier reduces their types and names.
func NewResourceIndex(identifier ResourceIdentifier, resources []ResourceInfo) *ResourceIndex {
	idx := &ResourceIndex{
		identifier:         identifier,
		names:              make([]ResourceName, len(resources)),
		byName:             make(map[ResourceTypeAndName][]int, len(resources)),
		byNameWithoutScope: make(map[ResourceTypeAndName][]int, len(resources)),
	}
	for i := range resources {
		idx.names[i] = resources[i].ResourceName
		fullKey := ResourceTypeAndFullNameFromResourceInfo(identifier, resources[i])
		idx.byName[fullKey] = append(idx.byName[fullKey], i)
		// An empty name without scope says nothing about which resource this is.
		if ResourceNameWithoutScopeFromResourceInfo(identifier, resources[i]) != "" {
			key := ResourceTypeAndNameFromResourceInfo(identifier, resources[i])
			idx.byNameWithoutScope[key] = append(idx.byNameWithoutScope[key], i)
		}
	}
	return idx
}

// Counterparts tells the index the resources it is matched against, such as the resources of the
// configuration a stored record is applied to. A name then matches without its scope only when it
// names at most one of them, too.
func (idx *ResourceIndex) Counterparts(resources []ResourceInfo) {
	idx.counterparts = make(map[ResourceTypeAndName]map[ResourceName]bool, len(resources))
	for i := range resources {
		if ResourceNameWithoutScopeFromResourceInfo(idx.identifier, resources[i]) == "" {
			continue
		}
		key := ResourceTypeAndNameFromResourceInfo(idx.identifier, resources[i])
		if idx.counterparts[key] == nil {
			idx.counterparts[key] = map[ResourceName]bool{}
		}
		idx.counterparts[key][resources[i].ResourceName] = true
	}
}

// Match looks a resource up by the rule ResourceIndex describes.
func (idx *ResourceIndex) Match(resource ResourceInfo) ResourceMatch {
	if positions := idx.byName[ResourceTypeAndFullNameFromResourceInfo(idx.identifier, resource)]; len(positions) > 0 {
		return ResourceMatch{Position: idx.choose(positions), Found: true}
	}
	if ResourceNameWithoutScopeFromResourceInfo(idx.identifier, resource) == "" {
		return ResourceMatch{}
	}
	return idx.MatchWithoutScope(ResourceTypeAndNameFromResourceInfo(idx.identifier, resource))
}

// Find looks a resource up by the rule ResourceIndex describes.
func (idx *ResourceIndex) Find(resource ResourceInfo) (int, bool) {
	match := idx.Match(resource)
	return match.Position, match.Found
}

// MatchWithoutScope looks a resource up by its type and name without scope alone, a key from
// ResourceTypeAndNameFromResourceInfo. It matches only when the key names exactly one resource.
func (idx *ResourceIndex) MatchWithoutScope(key ResourceTypeAndName) ResourceMatch {
	if idx.counterparts != nil && len(idx.counterparts[key]) > 1 {
		return ResourceMatch{}
	}
	return idx.unique(idx.byNameWithoutScope[key])
}

// unique matches the entries at positions when they record one resource, the same name scope
// included, and reports the names they disagree on otherwise.
func (idx *ResourceIndex) unique(positions []int) ResourceMatch {
	if len(positions) == 0 {
		return ResourceMatch{}
	}
	var names []ResourceName
	for _, position := range positions {
		if !slices.Contains(names, idx.names[position]) {
			names = append(names, idx.names[position])
		}
	}
	if len(names) > 1 {
		slices.Sort(names)
		return ResourceMatch{Ambiguous: names}
	}
	return ResourceMatch{Position: idx.choose(positions), Found: true}
}

func (idx *ResourceIndex) choose(positions []int) int {
	if idx.Choose != nil {
		return idx.Choose(positions)
	}
	return positions[len(positions)-1]
}
