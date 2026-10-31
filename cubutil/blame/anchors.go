// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package blame

import (
	"context"
	"maps"
	"slices"
	"strings"

	"github.com/confighub/sdk/core/configkit/yamlkit"
	"github.com/confighub/sdk/core/cubapi"
	"github.com/confighub/sdk/core/function/api"
	goclientnew "github.com/confighub/sdk/core/openapi/goclient-new"
	"github.com/confighub/sdk/core/third_party/gaby"
	"github.com/google/uuid"
)

// A list with no merge key -- an ExternalSecret's spec.data, a container's args -- records a
// change to one of its elements under a content-identity segment, which names the element by
// digests of it and of its neighbors rather than by position:
// spec.data.?~h=9b1164c8,~p=c4b0,~n=^,~l=4.remoteRef.key. The fields blame lists come from
// the data, where the same element is spec.data.0. The two cannot be compared as text, since
// the digest is of the element as the change found it, so every path a record holds is first
// resolved against the data the record describes, as a merge resolves it. Without that, a
// value someone changed and protected is credited to whatever brought the resource, and shown
// unprotected.

// resourceDocs is a unit's configuration at one Revision, one YAML document per resource.
type resourceDocs struct {
	identifier api.ResourceIdentifier
	resources  []*goclientnew.ResourceInfo
	docs       []*gaby.YamlDoc
}

// doc returns a resource's document, matched as every stored record is, or nil.
func (d *resourceDocs) doc(resource *goclientnew.ResourceInfo) *gaby.YamlDoc {
	if d == nil || resource == nil {
		return nil
	}
	if i, found := matchBlameResourceInfo(d.identifier, d.resources,
		resource.ResourceType, resource.ResourceName, resource.ResourceNameWithoutScope); found {
		return d.docs[i]
	}
	return nil
}

// resolve returns a path of a resource with each content-identity segment written as the index
// of the element it names, and the path as it was when one does not name an element.
func (d *resourceDocs) resolve(resource *goclientnew.ResourceInfo, path string) string {
	if !hasAnchorSegment(path) {
		return path
	}
	doc := d.doc(resource)
	if doc == nil {
		return path
	}
	if resolved, ok := yamlkit.ResolveAnchorSegments(doc, path); ok {
		return resolved
	}
	return path
}

// hasAnchorSegment is a cheap test that a path may have a content-identity segment, which
// yamlkit makes exact.
func hasAnchorSegment(path string) bool {
	return strings.Contains(path, "?") && strings.Contains(path, "~")
}

func recordHasAnchors(sources goclientnew.ResourceMutationList) bool {
	for _, rm := range sources {
		if rm.PathMutationMap == nil {
			continue
		}
		for path := range *rm.PathMutationMap {
			if hasAnchorSegment(path) {
				return true
			}
		}
	}
	return false
}

// resolveRecordAnchors returns a copy of a record with its paths resolved against docs, or the
// record itself when it has none to resolve.
//
// A path the data does not place by itself -- its element, and the neighbors it recorded, have
// all been edited since -- is followed through the change that wrote it, from the data that
// change was computed against to the data it made (yamlkit.PlaceChangeAnchors), and resolved
// again from there. change reads those two for a Mutation; it is nil where they are not to be read.
//
// Two paths can resolve to the same field: one element recorded by two changes that found it
// with different content. They are folded as yamlkit folds a record's canonical keys -- the
// later change is the credit, and the field is protected if either said so, since a lost
// Protected reads as an override that is not there.
func resolveRecordAnchors(docs *resourceDocs, sources goclientnew.ResourceMutationList,
	change func(mutationNum int64) (before, after *resourceDocs)) goclientnew.ResourceMutationList {
	if docs == nil || !recordHasAnchors(sources) {
		return sources
	}
	resolved := slices.Clone(sources)
	for i := range resolved {
		original := resolved[i].PathMutationMap
		if original == nil {
			continue
		}
		pathMutationMap := make(goclientnew.MutationMap, len(*original))
		placements := map[int64]map[api.ResolvedPath]string{}
		for _, path := range slices.Sorted(maps.Keys(*original)) {
			info := (*original)[path]
			key := docs.resolve(resolved[i].Resource, path)
			if key == path && hasAnchorSegment(path) && change != nil && info.Index != 0 {
				placed, followed := placements[info.Index]
				if !followed {
					placed = placeChange(change, resolved[i].Resource, *original, info.Index)
					placements[info.Index] = placed
				}
				if after, ok := placed[api.ResolvedPath(path)]; ok {
					if again := docs.resolve(resolved[i].Resource, after); again != after {
						key = again
					}
				}
			}
			if existing, ok := pathMutationMap[key]; ok {
				protected := existing.Protected || info.Protected
				if info.Index < existing.Index {
					info = existing
				}
				info.Protected = protected
			}
			pathMutationMap[key] = info
		}
		resolved[i].PathMutationMap = &pathMutationMap
	}
	return resolved
}

// placeChange follows the content-identity paths one Mutation recorded for a resource into the
// data that Mutation made.
func placeChange(change func(mutationNum int64) (before, after *resourceDocs), resource *goclientnew.ResourceInfo,
	pathMutationMap goclientnew.MutationMap, mutationNum int64) map[api.ResolvedPath]string {
	before, after := change(mutationNum)
	beforeDoc, afterDoc := before.doc(resource), after.doc(resource)
	if beforeDoc == nil || afterDoc == nil {
		return nil
	}
	recorded := map[api.ResolvedPath]api.MutationType{}
	for path, info := range pathMutationMap {
		if info.Index == mutationNum && info.MutationType != nil {
			recorded[api.ResolvedPath(path)] = api.MutationType(*info.MutationType)
		}
	}
	return yamlkit.PlaceChangeAnchors(beforeDoc, afterDoc, recorded)
}

// resolveUnitAnchors returns a copy of a unit whose conflicts, Link findings, guards and failed
// validation checks name fields by resolved paths, so that they are matched to the fields of
// the data they were recorded against. The unit's own record is left to resolveRecordAnchors.
func resolveUnitAnchors(docs *resourceDocs, unit *goclientnew.Unit) *goclientnew.Unit {
	if docs == nil {
		return unit
	}
	resolved := *unit
	if unit.Conflicts != nil {
		conflicts := slices.Clone(*unit.Conflicts)
		for i := range conflicts {
			conflicts[i].Path = docs.resolve(conflicts[i].Resource, conflicts[i].Path)
		}
		resolved.Conflicts = &conflicts
	}
	if unit.LinkFindings != nil {
		findings := slices.Clone(*unit.LinkFindings)
		for i := range findings {
			findings[i].Path = docs.resolve(findings[i].Resource, findings[i].Path)
		}
		resolved.LinkFindings = &findings
	}
	if unit.PathAnnotations != nil {
		annotations := slices.Clone(*unit.PathAnnotations)
		for i := range annotations {
			if annotations[i].PathAnnotationMap == nil {
				continue
			}
			pathAnnotationMap := make(map[string]goclientnew.PathAnnotations, len(annotations[i].PathAnnotationMap))
			for path, entry := range annotations[i].PathAnnotationMap {
				pathAnnotationMap[docs.resolve(annotations[i].Resource, path)] = entry
			}
			annotations[i].PathAnnotationMap = pathAnnotationMap
		}
		resolved.PathAnnotations = &annotations
	}
	if len(unit.ValidationResults) > 0 {
		results := make(map[string]goclientnew.ValidationResultList, len(unit.ValidationResults))
		for gate, list := range unit.ValidationResults {
			list = slices.Clone(list)
			for i := range list {
				if list[i].FailedAttributes == nil {
					continue
				}
				attributes := slices.Clone(*list[i].FailedAttributes)
				for j := range attributes {
					attributes[j].Path = docs.resolve(&goclientnew.ResourceInfo{
						ResourceType:             attributes[j].ResourceType,
						ResourceName:             attributes[j].ResourceName,
						ResourceNameWithoutScope: attributes[j].ResourceNameWithoutScope,
					}, attributes[j].Path)
				}
				list[i].FailedAttributes = &attributes
			}
			results[gate] = list
		}
		resolved.ValidationResults = results
	}
	return &resolved
}

// revisionRecord returns a record of a unit's at one Revision with its paths resolved against
// that Revision's data, which is read only when the record has a path that needs it. A record
// whose data cannot be read is returned as it is, and its paths looked up as they are.
func (r *Resolver) revisionRecord(ctx context.Context, unit *goclientnew.Unit, revisionID uuid.UUID,
	sources goclientnew.ResourceMutationList) goclientnew.ResourceMutationList {
	if !recordHasAnchors(sources) {
		return sources
	}
	return resolveRecordAnchors(r.revisionDocs(ctx, unit, revisionID), sources, r.mutationChange(ctx, unit))
}

// revisionDocs reads a unit's configuration as of one Revision, once. Nil when it cannot be read.
func (r *Resolver) revisionDocs(ctx context.Context, unit *goclientnew.Unit, revisionID uuid.UUID) *resourceDocs {
	docs, cached := r.docs[revisionID]
	if !cached {
		if data, err := cubapi.RevisionData(ctx, r.client, unit.SpaceID, unit.UnitID, revisionID); err == nil {
			_, docs, _ = flattenUnitFields(data, unit.ToolchainType)
		}
		r.docs[revisionID] = docs
	}
	return docs
}

// mutationChange returns what reads a unit's configuration on either side of one of its
// Mutations: the data its change was computed against, in the Revision before the one it made,
// and the data it made. Either is nil where it cannot be read, as both are for a Mutation that is
// not recorded.
func (r *Resolver) mutationChange(ctx context.Context, unit *goclientnew.Unit) func(mutationNum int64) (*resourceDocs, *resourceDocs) {
	docsAt := func(revisionNum int64) *resourceDocs {
		revision, err := r.revision(ctx, unit, revisionNum)
		if err != nil || revision == nil || revision.Revision == nil {
			return nil
		}
		return r.revisionDocs(ctx, unit, revision.Revision.RevisionID)
	}
	return func(mutationNum int64) (*resourceDocs, *resourceDocs) {
		mutations, err := r.unitMutations(ctx, unit)
		if err != nil || mutations[mutationNum] == nil {
			return nil, nil
		}
		revisionNum := mutations[mutationNum].Mutation.RevisionNum
		return docsAt(revisionNum - 1), docsAt(revisionNum)
	}
}
