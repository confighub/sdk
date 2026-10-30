// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package blame

import (
	"context"
	"fmt"
	"strings"

	"github.com/cockroachdb/errors"
	"github.com/confighub/sdk/core/function/api"
	goclientnew "github.com/confighub/sdk/core/openapi/goclient-new"
)

// PathReport is everything recorded about one field: what Path returns.
type PathReport struct {
	*Field
	// Changes carries the chain, with its records, so the field's own copy is left out:
	// this one is never set, and hides it. A "-" tag would not, since encoding/json drops
	// such a field before choosing between it and the embedded one.
	Chain []*Origin `json:",omitempty"`
	// Annotations are the path's own entries, every kind; Guards on the field are the guards
	// that apply to it, its ancestors' and the resource's included.
	// Released says whether the unit's last Release includes the value, in words.
	Released    string                       `json:",omitempty"`
	Annotations map[string]map[string]string `json:",omitempty"`
	Changes     []PathChange                 `json:",omitempty"`
	History     []PathChange                 `json:",omitempty"`
}

// PathChange is one change to the field, with the records it was read from.
type PathChange struct {
	*Origin
	MutationType string                        `json:",omitempty"`
	Value        string                        `json:",omitempty"`
	Mutation     *goclientnew.ExtendedMutation `json:",omitempty"`
	Revision     *goclientnew.ExtendedRevision `json:",omitempty"`
}

// Path reports everything recorded about one field of a unit: f is one of the fields
// Unit returned for it. With history, it also lists every change the unit recorded to the
// field, which reads the record of every Revision of the unit.
func (r *Resolver) Path(ctx context.Context, unit *goclientnew.Unit, f *Field, history bool) (*PathReport, error) {
	identifier, _ := blameResourceProvider(unit.ToolchainType)
	report := &PathReport{Field: f}
	if f.Absent {
		// A field the unit does not have has nothing to release and no record to read.
		return report, nil
	}
	report.Released = unitPathReleased(f, unit, r.space(ctx, unit.SpaceID))
	report.Annotations = unitPathAnnotations(identifier, unit, f)
	for _, origin := range f.Chain {
		report.Changes = append(report.Changes, unitPathChangeFor(origin, nil))
	}
	if history {
		changes, err := r.history(ctx, identifier, unit, f)
		if err != nil {
			return nil, err
		}
		report.History = changes
	}
	return report, nil
}

// FindField finds the one field an argument names: by REF prefix first, and then,
// since a path can look like a REF, by path.
func FindField(fields []*Field, argument, resourceFilter string) (*Field, error) {
	if resourceFilter != "" {
		fields = Filter(fields, "", resourceFilter, true)
		if len(fields) == 0 {
			return nil, errors.Newf("no resource %q in the unit", resourceFilter)
		}
	}

	if IsRef(argument) {
		var matched []*Field
		for _, f := range fields {
			if strings.HasPrefix(f.Ref, argument) {
				matched = append(matched, f)
			}
		}
		if len(matched) == 1 {
			return matched[0], nil
		}
		if len(matched) > 1 {
			return nil, errors.Newf("%q is the start of %d REFs; give more of it:\n%s",
				argument, len(matched), formatUnitPathCandidates(matched))
		}
	}

	path := AuthoredPath(argument)
	var exact, under []*Field
	for _, f := range fields {
		switch {
		case f.Path == path:
			exact = append(exact, f)
		case strings.HasPrefix(f.Path, path+"."):
			under = append(under, f)
		}
	}
	switch {
	case len(exact) == 1:
		return exact[0], nil
	case len(exact) > 1:
		return nil, errors.Newf("%d resources have %s; name one with --resource:\n%s",
			len(exact), path, formatUnitPathCandidates(exact))
	case len(under) > 0:
		return nil, errors.Newf("%s holds %d fields; name one of them:\n%s",
			path, len(under), formatUnitPathCandidates(under))
	}
	return nil, errors.Newf("no field %s in the unit: name one by the REF or path 'cub unit blame' prints", argument)
}

func formatUnitPathCandidates(fields []*Field) string {
	lines := make([]string, 0, len(fields))
	for _, f := range fields {
		lines = append(lines, fmt.Sprintf("  %s  %s %s  %s", f.Ref, f.ResourceType, f.ResourceName, f.Path))
	}
	return strings.Join(lines, "\n")
}

// unitPathAnnotations returns every annotation recorded on the field's own path, by kind.
// Guards are reported separately, as the guards that apply to the field wherever on its
// ancestry they were set.
func unitPathAnnotations(identifier api.ResourceIdentifier, unit *goclientnew.Unit, f *Field) map[string]map[string]string {
	if unit.PathAnnotations == nil {
		return nil
	}
	entry := matchBlameAnnotations(identifier, *unit.PathAnnotations, f.ResourceType, f.ResourceName, f.resourceNameCore)
	if entry == nil {
		return nil
	}
	annotations, ok := entry.PathAnnotationMap[f.Path]
	if !ok {
		return nil
	}
	byKind := map[string]map[string]string{}
	for kind, values := range annotations {
		if kind != string(api.AnnotationKindGuard) && len(values) > 0 {
			byKind[kind] = values
		}
	}
	if len(byKind) == 0 {
		return nil
	}
	return byKind
}

func unitPathChangeFor(origin *Origin, info *goclientnew.MutationInfo) PathChange {
	change := PathChange{Origin: origin, Mutation: origin.mutation, Revision: origin.revision}
	if info != nil {
		if info.MutationType != nil {
			change.MutationType = string(*info.MutationType)
		}
		change.Value = strings.TrimRight(info.Value, "\n")
	}
	return change
}

// unitPathHistory lists every change this unit's record credits the field to, oldest first.
//
// Each Revision's record names the last change to every path as of that Revision, so reading
// them in order and keeping each change the first time it appears is the field's history. Only
// a record of the path itself or of a value containing it counts: the resource-level entry
// covers every field the resource arrived with, and in an earlier Revision it would credit the
// resource's creation with a field that was added after it.
func (r *Resolver) history(ctx context.Context, identifier api.ResourceIdentifier, unit *goclientnew.Unit, f *Field) ([]PathChange, error) {
	revisions, err := r.allRevisionSources(ctx, unit)
	if err != nil {
		return nil, err
	}

	var history []PathChange
	seen := map[int64]bool{}
	var previous *goclientnew.MutationInfo
	for _, revision := range revisions {
		var info *goclientnew.MutationInfo
		if revision.MutationSources != nil {
			if rm := matchBlameResource(identifier, *revision.MutationSources, f.ResourceType, f.ResourceName, f.resourceNameCore); rm != nil {
				info = lookupPathMutation(rm.PathMutationMap, f.storedPath)
				if info == nil {
					info = lookupAncestorMutation(rm.PathMutationMap, f.storedPath)
				}
			}
		}
		last := previous
		previous = info
		if info == nil {
			continue
		}
		var origin *Origin
		if info.Index == 0 {
			// An entry naming no Mutation is a change only where its value is new; one that
			// repeats the entry before it is a record rewritten without its Mutation, and
			// the change is the one already listed. See unrecordedCredit.
			if sameBlameEntryValue(last, info) {
				continue
			}
			origin = r.describeUnrecorded(ctx, unit, revision.RevisionNum)
		} else {
			if seen[info.Index] {
				continue
			}
			seen[info.Index] = true
			if origin, err = r.describe(ctx, unit, info.Index); err != nil {
				return nil, err
			}
		}
		if origin != nil {
			history = append(history, unitPathChangeFor(origin, info))
		}
	}
	return history, nil
}

// unitPathReleased says whether the unit's last Release includes the field's value, and why,
// which is what the U flag summarizes.
func unitPathReleased(f *Field, unit *goclientnew.Unit, space spaceInfo) string {
	if !space.releasable {
		return fmt.Sprintf("nothing to release: space %s has no release target", space.slug)
	}
	last := unit.LastReleasedRevisionNum
	var here *Origin
	if len(f.Chain) > 0 {
		here = f.Chain[0]
	}
	switch {
	case !blameUnreleased(here, last):
		return fmt.Sprintf("yes: the unit's last release is of revision %d", last)
	case last == 0:
		return "no: the unit has never been released"
	default:
		return fmt.Sprintf("no: set in revision %d, after the unit's last release of revision %d", here.RevisionNum, last)
	}
}
