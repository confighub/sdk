// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"fmt"
	"sort"
	"strings"

	goclientnew "github.com/confighub/sdk/core/openapi/goclient-new"
	"github.com/google/uuid"
	"github.com/spf13/cobra"
)

// This file displays a Unit's or a Revision's MutationSources: what set each value in the
// configuration. That is a record, not a change -- what an operation changed, or would change,
// is a ConfigDiff, displayed by config_diff.go.

var displayMutations bool

// collectUniqueIndices returns the unique Index values from a ResourceMutationList, sorted ascending.
func collectUniqueIndices(mutations *goclientnew.ResourceMutationList) []int64 {
	if mutations == nil {
		return nil
	}
	seen := make(map[int64]struct{})
	for _, rm := range *mutations {
		if rm.ResourceMutationInfo != nil && rm.ResourceMutationInfo.MutationType != nil &&
			*rm.ResourceMutationInfo.MutationType != goclientnew.None {
			seen[rm.ResourceMutationInfo.Index] = struct{}{}
		}
		if rm.PathMutationMap != nil {
			for _, mi := range *rm.PathMutationMap {
				if mi.MutationType != nil && *mi.MutationType != goclientnew.None {
					seen[mi.Index] = struct{}{}
				}
			}
		}
	}
	indices := make([]int64, 0, len(seen))
	for idx := range seen {
		indices = append(indices, idx)
	}
	sort.Slice(indices, func(i, j int) bool { return indices[i] < indices[j] })
	return indices
}

// displayResourceMutationList displays stored MutationSources, whose Index values are the
// MutationNums of the Unit's Mutations. Locally-overridden paths, which merges preserve, are
// shown apart from those an upstream merge may overwrite. With --verbose, each path's value is
// shown and each Mutation is described.
func displayResourceMutationList(mutations *goclientnew.ResourceMutationList) {
	if mutations == nil || len(*mutations) == 0 {
		tprintRaw("No mutations")
		return
	}

	var mutationMap map[int64]*goclientnew.ExtendedMutation
	if verbose {
		mutationMap = lookupMutations(collectUniqueIndices(mutations))
	}

	protected, unprotected := true, false
	shownOverrides := false
	if anyMutationWithProtection(mutations, true) {
		tprintRaw("Locally overridden (preserved during merges):")
		displayMutationEntries(mutations, mutationMap, &protected)
		shownOverrides = true
	}
	if anyMutationWithProtection(mutations, false) {
		if shownOverrides {
			tprintRaw("")
		}
		tprintRaw("Eligible for upstream merges:")
		displayMutationEntries(mutations, mutationMap, &unprotected)
	}

	if len(mutationMap) > 0 {
		tprintRaw("")
		tprintRaw("Mutation details:")
		displayMutationSummaryTable(mutationMap)
	}
}

// protectionMatches reports whether a mutation with the given Protected value should be shown
// for the given filter. A nil filter matches everything; otherwise only entries whose Protected
// equals *filter are shown.
func protectionMatches(filter *bool, protected bool) bool {
	return filter == nil || *filter == protected
}

// anyMutationWithProtection reports whether any non-None mutation (resource-level or path)
// has the given Protected value.
func anyMutationWithProtection(mutations *goclientnew.ResourceMutationList, want bool) bool {
	for _, rm := range *mutations {
		if rm.ResourceMutationInfo != nil && rm.ResourceMutationInfo.MutationType != nil &&
			*rm.ResourceMutationInfo.MutationType != goclientnew.None && rm.ResourceMutationInfo.Protected == want {
			return true
		}
		if rm.PathMutationMap != nil {
			for _, mi := range *rm.PathMutationMap {
				if mi.MutationType != nil && *mi.MutationType != goclientnew.None && mi.Protected == want {
					return true
				}
			}
		}
	}
	return false
}

// displayMutationEntries renders the resource mutation entries whose Protected matches
// protectionFilter, or all of them for a nil filter.
func displayMutationEntries(mutations *goclientnew.ResourceMutationList, mutationMap map[int64]*goclientnew.ExtendedMutation, protectionFilter *bool) {
	first := true
	for _, rm := range *mutations {
		if rm.ResourceMutationInfo == nil || rm.ResourceMutationInfo.MutationType == nil {
			continue
		}
		mutType := *rm.ResourceMutationInfo.MutationType

		// Determine if this resource has any mutations to show in this section
		hasRelevant := mutType != goclientnew.None && protectionMatches(protectionFilter, rm.ResourceMutationInfo.Protected)
		if !hasRelevant && rm.PathMutationMap != nil {
			for _, mi := range *rm.PathMutationMap {
				if mi.MutationType != nil && *mi.MutationType != goclientnew.None && protectionMatches(protectionFilter, mi.Protected) {
					hasRelevant = true
					break
				}
			}
		}
		if !hasRelevant {
			continue
		}

		if !first {
			tprintRaw("")
		}
		first = false

		// Resource header
		resourceName := ""
		resourceType := ""
		if rm.Resource != nil {
			resourceName = rm.Resource.ResourceName
			resourceType = rm.Resource.ResourceType
		}
		tprintRaw(fmt.Sprintf("%sResource: %s %s", colorLightBlue, resourceType, resourceName+colorReset))

		// Resource-level mutation
		if mutType != goclientnew.None && protectionMatches(protectionFilter, rm.ResourceMutationInfo.Protected) {
			tprintRaw(fmt.Sprintf("  %s %s", mutationTypeSymbol(mutType), formatIndexLabel(rm.ResourceMutationInfo.Index, mutationMap)))
			if rm.ResourceMutationInfo.Value != "" && verbose {
				displayMutationValue(rm.ResourceMutationInfo.Value, "    ")
			}
		}

		// Path-level mutations
		if rm.PathMutationMap != nil && len(*rm.PathMutationMap) > 0 {
			paths := make([]string, 0, len(*rm.PathMutationMap))
			for path := range *rm.PathMutationMap {
				paths = append(paths, path)
			}
			sort.Strings(paths)

			for _, path := range paths {
				mi := (*rm.PathMutationMap)[path]
				if mi.MutationType == nil || *mi.MutationType == goclientnew.None {
					continue
				}
				if !protectionMatches(protectionFilter, mi.Protected) {
					continue
				}
				tprintRaw(fmt.Sprintf("  %s %s  %s", mutationTypeSymbol(*mi.MutationType), displayPath(path), formatIndexLabel(mi.Index, mutationMap)))
				if mi.Value != "" && verbose {
					displayMutationValue(mi.Value, "    ")
				}
			}
		}
	}
}

// trimMutationValue cleans up a mutation value for display.
func trimMutationValue(value string) string {
	return strings.TrimRight(value, "\n")
}

// indentMultiline adds indent to each line of a potentially multi-line string.
func indentMultiline(s, indent string) string {
	return indent + strings.ReplaceAll(s, "\n", "\n"+indent)
}

// mutationTypeSymbol returns a colored symbol for a mutation type.
func mutationTypeSymbol(mt goclientnew.MutationType) string {
	switch mt {
	case goclientnew.Add:
		return colorGreen + "+" + colorReset + " [Add]"
	case goclientnew.Update:
		return colorGreen + "~" + colorReset + " [Update]"
	case goclientnew.Replace:
		return colorGreen + "!" + colorReset + " [Replace]"
	case goclientnew.Delete:
		return colorRed + "-" + colorReset + " [Delete]"
	default:
		return "  [None]"
	}
}

// formatIndexLabel returns a label for a mutation index, which is a MutationNum.
func formatIndexLabel(index int64, mutationMap map[int64]*goclientnew.ExtendedMutation) string {
	label := fmt.Sprintf("(#%d", index)
	if mutationMap != nil {
		if em, ok := mutationMap[index]; ok {
			label += " " + describeMutationSource(em)
		}
	}
	label += ")"
	return label
}

// describeMutationSource returns a short description of what caused a mutation.
func describeMutationSource(em *goclientnew.ExtendedMutation) string {
	parts := []string{}
	m := em.Mutation
	if m.FunctionInvocation.FunctionName != "" {
		parts = append(parts, "fn:"+m.FunctionInvocation.FunctionName)
	}
	if em.MergeSource != nil {
		parts = append(parts, "merge:"+em.MergeSource.Slug)
	} else if m.MergeSourceID != nil && *m.MergeSourceID != uuid.Nil {
		parts = append(parts, "merge:"+m.MergeSourceID.String())
	}
	if em.Link != nil {
		parts = append(parts, "link:"+em.Link.Slug)
	} else if m.LinkID != nil && *m.LinkID != uuid.Nil {
		parts = append(parts, "link:"+m.LinkID.String())
	}
	if em.Trigger != nil {
		parts = append(parts, "trigger:"+em.Trigger.Slug)
	} else if m.TriggerID != nil && *m.TriggerID != uuid.Nil {
		parts = append(parts, "trigger:"+m.TriggerID.String())
	}
	if em.Invocation != nil {
		parts = append(parts, "invocation:"+em.Invocation.Slug)
	} else if m.InvocationID != nil && *m.InvocationID != uuid.Nil {
		parts = append(parts, "invocation:"+m.InvocationID.String())
	}
	if m.ProvidedResource.ResourceName != "" {
		parts = append(parts, "provided:"+m.ProvidedResource.ResourceName)
	}
	if len(parts) == 0 {
		return ""
	}
	return strings.Join(parts, ", ")
}

// displayMutationValue displays a mutation value with indentation.
func displayMutationValue(value string, indent string) {
	lines := strings.Split(strings.TrimRight(value, "\n"), "\n")
	for _, line := range lines {
		tprintRaw(indent + line)
	}
}

// displayMutationSummaryTable shows a table of mutation details.
func displayMutationSummaryTable(mutationMap map[int64]*goclientnew.ExtendedMutation) {
	table := tableView()
	if !noheader {
		table.SetHeader([]string{"Num", "Rev", "Source", "Link", "Trigger", "Invocation", "Function"})
	}

	// Sort by MutationNum
	nums := make([]int64, 0, len(mutationMap))
	for num := range mutationMap {
		nums = append(nums, num)
	}
	sort.Slice(nums, func(i, j int) bool { return nums[i] < nums[j] })

	for _, num := range nums {
		em := mutationMap[num]
		m := em.Mutation
		var mergeSourceSlug, linkSlug, triggerSlug, invocationSlug string
		if em.MergeSource != nil {
			mergeSourceSlug = em.MergeSource.Slug
		} else if m.MergeSourceID != nil && *m.MergeSourceID != uuid.Nil {
			mergeSourceSlug = m.MergeSourceID.String()
		}
		if em.Link != nil {
			linkSlug = em.Link.Slug
		} else if m.LinkID != nil && *m.LinkID != uuid.Nil {
			linkSlug = m.LinkID.String()
		}
		if em.Trigger != nil {
			triggerSlug = em.Trigger.Slug
		} else if m.TriggerID != nil && *m.TriggerID != uuid.Nil {
			triggerSlug = m.TriggerID.String()
		}
		if em.Invocation != nil {
			invocationSlug = em.Invocation.Slug
		} else if m.InvocationID != nil && *m.InvocationID != uuid.Nil {
			invocationSlug = m.InvocationID.String()
		}
		table.Append([]string{
			fmt.Sprintf("%d", m.MutationNum),
			fmt.Sprintf("%d", m.RevisionNum),
			mergeSourceSlug,
			linkSlug,
			triggerSlug,
			invocationSlug,
			m.FunctionInvocation.FunctionName,
		})
	}
	table.Render()
}

// lookupMutations fetches ExtendedMutation details for the given MutationNum values.
func lookupMutations(indices []int64) map[int64]*goclientnew.ExtendedMutation {
	if len(indices) == 0 {
		return nil
	}

	values := make([]string, len(indices))
	for i, idx := range indices {
		values[i] = fmt.Sprintf("%d", idx)
	}
	whereClause := fmt.Sprintf("MutationNum IN (%s)", strings.Join(values, ", "))

	mutations, err := lookupMutationsForCurrentUnit(whereClause)
	if err != nil {
		// Non-fatal: just skip mutation details
		return nil
	}

	result := make(map[int64]*goclientnew.ExtendedMutation, len(mutations))
	for _, m := range mutations {
		result[m.Mutation.MutationNum] = m
	}
	return result
}

// lookupMutationsUnitID and lookupMutationsSpaceID name the unit whose mutations the display
// functions look up, and the space it lives in. The caller sets them so the pair does not have
// to be passed through the display function chain.
var lookupMutationsUnitID, lookupMutationsSpaceID string

func lookupMutationsForCurrentUnit(whereClause string) ([]*goclientnew.ExtendedMutation, error) {
	if lookupMutationsUnitID == "" {
		return nil, fmt.Errorf("no unit ID set for mutation lookup")
	}
	return apiListMutations(lookupMutationsSpaceID, lookupMutationsUnitID, whereClause, "*", "")
}

func argFromString(name, value string) goclientnew.FunctionArgument {
	v := &goclientnew.FunctionArgument_Value{}
	v.FromFunctionArgumentValue0(value)
	return goclientnew.FunctionArgument{
		ParameterName: &name,
		Value:         v,
	}
}

// displayPath renders a mutation path for reading. An element of an array with no merge
// key is recorded with an anchor — a digest of its content that lets a patch find the
// element in a copy that has moved it — which is machinery, not information the reader of
// a diff wants. Show the index the anchor carries instead. Merge-keyed elements keep their
// key, which is what identifies them to a reader too.
func displayPath(path string) string {
	if !strings.Contains(path, "?~") {
		return path
	}
	segments := strings.Split(path, ".")
	for i, segment := range segments {
		if !strings.HasPrefix(segment, "?~") {
			continue
		}
		if _, index, found := strings.Cut(segment, ";@"); found {
			segments[i] = index
		}
	}
	return strings.Join(segments, ".")
}

// enableDisplayMutationsFlag adds the --display-mutations flag to a command.
// Deprecated: --display-mutations is retained as an alias for -o mutations.
func enableDisplayMutationsFlag(cmd *cobra.Command) {
	cmd.Flags().BoolVar(&displayMutations, "display-mutations", false, "display resource mutations")
	_ = cmd.Flags().MarkDeprecated("display-mutations", "use -o mutations")
}

// shouldDisplayMutations returns true when mutation display is requested,
// either via the deprecated --display-mutations flag or -o mutations.
func shouldDisplayMutations() bool {
	return displayMutations || effectiveOutput().Kind == OutputMutations
}

// displayMutationsForUnit fetches and displays what set each value in a Unit's configuration.
func displayMutationsForUnit(unit *goclientnew.Unit) {
	mutationSources, err := fetchUnitMutationSources(unit.SpaceID, unit.UnitID)
	if err != nil {
		tprintErr("Failed to get mutation sources: %s", err.Error())
		return
	}
	lookupMutationsUnitID = unit.UnitID.String()
	lookupMutationsSpaceID = unit.SpaceID.String()
	displayResourceMutationList(mutationSources)
}

// displayMutationsForRevision displays the mutations recorded on a revision, in the
// same form as the head-revision mutations shown for a unit. The recorded indices are
// the unit's MutationNums, so mutation details resolve against the same unit.
func displayMutationsForRevision(rev *goclientnew.Revision) {
	mutationSources, err := fetchRevisionMutationSources(rev.SpaceID, rev.UnitID, rev.RevisionID)
	if err != nil {
		tprintErr("Failed to get mutation sources: %s", err.Error())
		return
	}
	lookupMutationsUnitID = rev.UnitID.String()
	lookupMutationsSpaceID = rev.SpaceID.String()
	displayResourceMutationList(mutationSources)
}
