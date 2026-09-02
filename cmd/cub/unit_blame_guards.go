// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"fmt"
	"sort"
	"strings"

	"github.com/confighub/sdk/core/function/api"
	goclientnew "github.com/confighub/sdk/core/openapi/goclient-new"
)

// Blame answers "why does this field hold this value", and a guard is an answer to exactly
// that question -- the one a merge is required to read before overwriting. Showing the chain
// that set a value while omitting the reason recorded for it would leave the reader with the
// history and not the policy.
//
// Protection and guards are marked in the same column because they answer the same question at
// different resolutions: `*` says a merge from upstream leaves this alone, `!` says an
// operation must know the reasons before writing it. A field can carry both.

// blameGuardsForPath returns the guards covering one field: its own entry, every annotated
// ancestor, and the resource, unioned with the nearest winning per key.
//
// The rule is not reimplemented here. The stored table is converted to the SDK's type and
// GuardsForPath is asked, so the CLI cannot answer this question differently from the merge
// engine that enforces it -- which is the failure mode a second implementation would have.
func blameGuardsForPath(annotations goclientnew.PathAnnotationList,
	resourceType, resourceName, resourceNameCore, path string) map[string]string {
	entry := matchBlameAnnotations(annotations, resourceType, resourceName, resourceNameCore)
	if entry == nil {
		return nil
	}
	converted := apiResourcePathAnnotations(entry)
	guards := converted.GuardsForPath(api.ResolvedPath(path))
	if len(guards) == 0 {
		return nil
	}
	return guards
}

// matchBlameAnnotations finds the annotation entry for a resource, falling back to the
// unscoped name the way matchBlameResource does: a variant that renamed a resource still
// carries the guards written under the name it had.
func matchBlameAnnotations(annotations goclientnew.PathAnnotationList,
	resourceType, resourceName, resourceNameCore string) *goclientnew.ResourcePathAnnotations {
	var unscoped *goclientnew.ResourcePathAnnotations
	for i := range annotations {
		entry := &annotations[i]
		if entry.Resource == nil || entry.Resource.ResourceType != resourceType {
			continue
		}
		if entry.Resource.ResourceName == resourceName {
			return entry
		}
		if unscoped == nil && resourceNameCore != "" &&
			entry.Resource.ResourceNameWithoutScope == resourceNameCore {
			unscoped = entry
		}
	}
	return unscoped
}

// apiResourcePathAnnotations converts one generated entry to the SDK type, so the shared
// lookup can be used. The two are the same shape under different named types; only the parts
// GuardsForPath reads are carried.
func apiResourcePathAnnotations(entry *goclientnew.ResourcePathAnnotations) api.ResourcePathAnnotations {
	converted := api.ResourcePathAnnotations{}
	if entry.ResourceAnnotations != nil {
		converted.ResourceAnnotations = apiPathAnnotations(*entry.ResourceAnnotations)
	}
	if len(entry.PathAnnotationMap) > 0 {
		converted.PathAnnotationMap = make(map[api.ResolvedPath]api.PathAnnotations, len(entry.PathAnnotationMap))
		for path, annotations := range entry.PathAnnotationMap {
			converted.PathAnnotationMap[api.ResolvedPath(path)] = apiPathAnnotations(annotations)
		}
	}
	return converted
}

func apiPathAnnotations(annotations goclientnew.PathAnnotations) api.PathAnnotations {
	converted := make(api.PathAnnotations, len(annotations))
	for kind, entries := range annotations {
		converted[api.AnnotationKind(kind)] = entries
	}
	return converted
}

// formatBlameGuards renders a field's guards the way --guard takes them, sorted so a listing
// is diffable.
func formatBlameGuards(guards map[string]string) string {
	if len(guards) == 0 {
		return ""
	}
	specs := make([]string, 0, len(guards))
	for key, value := range guards {
		specs = append(specs, fmt.Sprintf("%s=%s", key, value))
	}
	sort.Strings(specs)
	return strings.Join(specs, " ")
}
