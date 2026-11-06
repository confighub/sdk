// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package api

import (
	"errors"
	"fmt"
	"strings"
)

// A request names the resource it is about with the fields of a ResourceInfo, read as a person
// writes them rather than as a record stores them:
//
//   - ResourceName identifies one resource, by its whole name (a Kubernetes resource's includes
//     its namespace: prod/web, or /web for a cluster-scoped one) or by a name it had before a
//     rename.
//   - ResourceNameWithoutScope is a filter, not a name: it matches the resource of that name in
//     every scope. A request that records something about one resource refuses a filter that
//     matches more than one.
//   - ResourceType, when given, narrows either, in any API version.

// ErrNoSelectedResource is returned, wrapped, when a selector names no resource.
var ErrNoSelectedResource = errors.New("no resource matches")

// SelectResources returns the positions of the entries of a record that a selector names. A
// selector with neither name selects nothing.
func SelectResources(identifier ResourceIdentifier, entries ResourceMutationList, selector ResourceInfo) []int {
	if selector.ResourceName == "" && selector.ResourceNameWithoutScope == "" {
		return nil
	}
	var positions []int
	for i := range entries {
		entry := &entries[i]
		if selector.ResourceType != "" && !SameResourceType(identifier, entry.Resource.ResourceType, selector.ResourceType) {
			continue
		}
		if name := selector.ResourceName; name != "" {
			if _, alias := entry.Aliases[name]; entry.Resource.ResourceName != name && !alias {
				continue
			}
		}
		if name := selector.ResourceNameWithoutScope; name != "" {
			_, alias := entry.AliasesWithoutScopes[name]
			if ResourceNameWithoutScopeFromResourceInfo(identifier, entry.Resource) != name && !alias {
				continue
			}
		}
		positions = append(positions, i)
	}
	return positions
}

// SelectedResources returns the resources of a record that a selector names, as the record names
// them now.
func SelectedResources(identifier ResourceIdentifier, entries ResourceMutationList, selector ResourceInfo) []ResourceInfo {
	positions := SelectResources(identifier, entries, selector)
	resources := make([]ResourceInfo, len(positions))
	for i, position := range positions {
		resources[i] = entries[position].Resource
	}
	return resources
}

// ValidateWholeResourceName refuses a ResourceName that is not a whole name of the toolchain's
// resources, such as a Kubernetes name without its namespace, which would otherwise name nothing
// and say nothing about why.
func ValidateWholeResourceName(identifier ResourceIdentifier, name ResourceName) error {
	if name == "" || identifier == nil || identifier.RemoveScopeFromResourceName(name) != "" {
		return nil
	}
	return fmt.Errorf("%s is not a whole resource name, which includes its namespace (namespace/name, or /name for a "+
		"cluster-scoped resource); to match a name without its namespace, use ResourceNameWithoutScope", name)
}

// OneSelectedResource is the one resource a selector names, for a request that records something
// about it. It refuses none, wrapping ErrNoSelectedResource, and more than one, saying what would
// tell them apart: for a whole name, which matches several only when they differ in type, the type.
func OneSelectedResource(selector ResourceInfo, resources []ResourceInfo) (ResourceInfo, error) {
	named := selector.ResourceName
	if named == "" {
		named = selector.ResourceNameWithoutScope
	}
	switch len(resources) {
	case 1:
		return resources[0], nil
	case 0:
		if selector.ResourceType != "" {
			return ResourceInfo{}, fmt.Errorf("%w: the unit has no %s %s", ErrNoSelectedResource, selector.ResourceType, named)
		}
		return ResourceInfo{}, fmt.Errorf("%w: the unit has no resource %s", ErrNoSelectedResource, named)
	}
	names := make([]string, len(resources))
	for i := range resources {
		names[i] = string(resources[i].ResourceType) + " " + string(resources[i].ResourceName)
	}
	remedy := "name one by its whole name, or give its type"
	if selector.ResourceName != "" {
		remedy = "give its type"
	}
	return ResourceInfo{}, fmt.Errorf("%s matches %d resources (%s); %s", named, len(resources), strings.Join(names, ", "), remedy)
}
