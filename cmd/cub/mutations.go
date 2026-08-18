// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/confighub/sdk/core/function/api"
	goclientnew "github.com/confighub/sdk/core/openapi/goclient-new"
	"github.com/google/uuid"
	"github.com/spf13/cobra"
)

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

// displayResourceMutationList displays a ResourceMutationList in a human-readable format.
// If indicesAreMutationNums is true, the Index values correspond to MutationNum values from
// Mutation entities, and the function will look up mutation details to display.
// If indicesAreMutationNums is false, the indices are dummy values (e.g., from compute-mutations)
// and no mutation lookup is performed.
// priorHeadMutationNum, if > 0, is used to distinguish new changes from prior changes when
// indicesAreMutationNums is true.
// newChangeDescription describes the operation that caused new changes (e.g., function name,
// "PatchUnit", "Refresh"). When priorHeadMutationNum > 0 and there are new mutations,
// the display is split into prior and new sections.
// priorRevision, if non-empty, is a revision identifier ("unit-slug/revision-number")
// used to invoke get-paths on the prior revision to fetch old values for display.
// For dry-run operations, pass "dry-run" to fetch old values from the current unit
// (which still has its original data). For non-dry-run, pass the prior revision.
// Pass "" to skip fetching old values.
func displayResourceMutationList(mutations *goclientnew.ResourceMutationList, indicesAreMutationNums bool, priorHeadMutationNum int64, newChangeDescription string, priorRevision string) {
	if mutations == nil || len(*mutations) == 0 {
		tprintRaw("No mutations")
		return
	}

	// Check if we need to split into prior and new sections
	hasNewMutations := false
	hasPriorMutations := false
	if priorHeadMutationNum > 0 && indicesAreMutationNums {
		for _, rm := range *mutations {
			if rm.ResourceMutationInfo != nil && rm.ResourceMutationInfo.MutationType != nil &&
				*rm.ResourceMutationInfo.MutationType != goclientnew.None {
				if rm.ResourceMutationInfo.Index > priorHeadMutationNum {
					hasNewMutations = true
				} else {
					hasPriorMutations = true
				}
			}
			if rm.PathMutationMap != nil {
				for _, mi := range *rm.PathMutationMap {
					if mi.MutationType != nil && *mi.MutationType != goclientnew.None {
						if mi.Index > priorHeadMutationNum {
							hasNewMutations = true
						} else {
							hasPriorMutations = true
						}
					}
				}
			}
		}
	}

	splitDisplay := hasNewMutations && priorHeadMutationNum > 0

	// If there's a prior head but no new mutations, indicate no new changes were made
	if priorHeadMutationNum > 0 && !hasNewMutations {
		tprintRaw("No new changes")
		if !verbose {
			return
		}
	}

	// Collect and display mutation details for PRIOR mutations only (new ones may not exist in DB)
	var mutationMap map[int64]*goclientnew.ExtendedMutation
	if indicesAreMutationNums && verbose {
		indices := collectUniqueIndices(mutations)
		if len(indices) > 0 {
			if splitDisplay {
				// Only look up prior mutations
				var priorIndices []int64
				for _, idx := range indices {
					if idx <= priorHeadMutationNum {
						priorIndices = append(priorIndices, idx)
					}
				}
				if len(priorIndices) > 0 {
					mutationMap = lookupMutations(priorIndices)
				}
			} else {
				mutationMap = lookupMutations(indices)
			}
		}
	}

	if splitDisplay {
		// Fetch old values for new mutations so we can show old → new.
		// For dry-run, the unit still has its original data, so get-paths on the unit works.
		// For non-dry-run, the unit already has new data, so we invoke get-paths on the
		// prior revision (if available) to get the old values.
		var oldValues map[string]string
		if priorRevision == "dry-run" {
			oldValues = fetchOldPathValues(mutations, priorHeadMutationNum)
		} else if priorRevision != "" {
			oldValues = fetchOldPathValuesFromRevision(mutations, priorHeadMutationNum, priorRevision)
		}

		// Display new changes first
		if hasNewMutations {
			header := "New changes"
			if newChangeDescription != "" {
				header += " from " + newChangeDescription
			}
			tprintRaw(header + ":")
			displayMutationEntries(mutations, indicesAreMutationNums, priorHeadMutationNum, nil, true, oldValues, nil)
		}

		// Then display prior changes (only with --verbose)
		if hasPriorMutations && verbose {
			if hasNewMutations {
				tprintRaw("")
			}
			tprintRaw("Prior changes:")
			displayMutationEntries(mutations, indicesAreMutationNums, priorHeadMutationNum, mutationMap, false, nil, nil)
		}
	} else if verbose || priorHeadMutationNum == 0 {
		// No split - display everything together.
		// When the indices are real MutationNums, the stored Protected flags are meaningful,
		// so separate locally-overridden fields (preserved during merges) from fields
		// eligible to be overwritten by an upstream merge.
		if indicesAreMutationNums {
			protected, unprotected := true, false
			shownOverrides := false
			if anyMutationWithProtection(mutations, true) {
				tprintRaw("Locally overridden (preserved during merges):")
				displayMutationEntries(mutations, indicesAreMutationNums, 0, mutationMap, false, nil, &protected)
				shownOverrides = true
			}
			if anyMutationWithProtection(mutations, false) {
				if shownOverrides {
					tprintRaw("")
				}
				tprintRaw("Eligible for upstream merges:")
				displayMutationEntries(mutations, indicesAreMutationNums, 0, mutationMap, false, nil, &unprotected)
			}
		} else {
			displayMutationEntries(mutations, indicesAreMutationNums, 0, mutationMap, false, nil, nil)
		}
	}

	// Display mutation summary table (only with --verbose)
	if verbose && indicesAreMutationNums && mutationMap != nil && len(mutationMap) > 0 {
		tprintRaw("")
		tprintRaw("Mutation details:")
		displayMutationSummaryTable(mutationMap, 0)
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

// displayMutationEntries renders the resource mutation entries. If showNewOnly is true,
// only mutations with Index > priorHeadMutationNum are shown. If false, only mutations
// with Index <= priorHeadMutationNum (or all if priorHeadMutationNum == 0) are shown.
// oldValues, if non-nil, maps "resourceType/resourceName:path" to old values for display.
// protectionFilter, if non-nil, restricts output to entries whose Protected equals *protectionFilter.
func displayMutationEntries(mutations *goclientnew.ResourceMutationList, indicesAreMutationNums bool, priorHeadMutationNum int64, mutationMap map[int64]*goclientnew.ExtendedMutation, showNewOnly bool, oldValues map[string]string, protectionFilter *bool) {
	first := true
	for _, rm := range *mutations {
		if rm.ResourceMutationInfo == nil || rm.ResourceMutationInfo.MutationType == nil {
			continue
		}
		mutType := *rm.ResourceMutationInfo.MutationType

		// Determine if this resource has any mutations to show in this section
		hasRelevant := false
		if mutType != goclientnew.None {
			isNew := priorHeadMutationNum > 0 && rm.ResourceMutationInfo.Index > priorHeadMutationNum
			if (showNewOnly == isNew || priorHeadMutationNum == 0) && protectionMatches(protectionFilter, rm.ResourceMutationInfo.Protected) {
				hasRelevant = true
			}
		}
		if !hasRelevant && rm.PathMutationMap != nil {
			for _, mi := range *rm.PathMutationMap {
				if mi.MutationType == nil || *mi.MutationType == goclientnew.None {
					continue
				}
				isNew := priorHeadMutationNum > 0 && mi.Index > priorHeadMutationNum
				if (showNewOnly == isNew || priorHeadMutationNum == 0) && protectionMatches(protectionFilter, mi.Protected) {
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
		if mutType != goclientnew.None {
			isNew := priorHeadMutationNum > 0 && rm.ResourceMutationInfo.Index > priorHeadMutationNum
			if (showNewOnly == isNew || priorHeadMutationNum == 0) && protectionMatches(protectionFilter, rm.ResourceMutationInfo.Protected) {
				indexLabel := formatIndexLabel(rm.ResourceMutationInfo.Index, indicesAreMutationNums, 0, mutationMap)
				tprintRaw(fmt.Sprintf("  %s %s", mutationTypeSymbol(mutType), indexLabel))
				if rm.ResourceMutationInfo.Value != "" && verbose {
					displayMutationValue(rm.ResourceMutationInfo.Value, "    ")
				}
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
				isNew := priorHeadMutationNum > 0 && mi.Index > priorHeadMutationNum
				if showNewOnly != isNew && priorHeadMutationNum > 0 {
					continue
				}
				if !protectionMatches(protectionFilter, mi.Protected) {
					continue
				}
				indexLabel := formatIndexLabel(mi.Index, indicesAreMutationNums, 0, mutationMap)
				tprintRaw(fmt.Sprintf("  %s %s  %s", mutationTypeSymbol(*mi.MutationType), displayPath(path), indexLabel))

				// Show old → new values for path mutations
				if oldValues != nil || (mi.Value != "" && verbose) {
					newVal := trimMutationValue(mi.Value)
					oldValKey := pathValueKey(resourceType, resourceName, path)
					oldVal := ""
					if oldValues != nil {
						oldVal = oldValues[oldValKey]
					}
					if oldValues != nil {
						displayOldNewValues(*mi.MutationType, oldVal, newVal, "    ")
					} else if mi.Value != "" && verbose {
						displayMutationValue(mi.Value, "    ")
					}
				}
			}
		}
	}
}

// pathValueKey builds a map key for looking up path values.
func pathValueKey(resourceType, resourceName, path string) string {
	return resourceType + "/" + resourceName + ":" + path
}

// trimMutationValue cleans up a mutation value for display.
func trimMutationValue(value string) string {
	return strings.TrimRight(value, "\n")
}

// indentMultiline adds indent to each line of a potentially multi-line string.
func indentMultiline(s, indent string) string {
	return indent + strings.ReplaceAll(s, "\n", "\n"+indent)
}

// displayOldNewValues shows old and new values for a mutation.
func displayOldNewValues(mutType goclientnew.MutationType, oldVal, newVal, indent string) {
	switch mutType {
	case goclientnew.Add:
		if newVal != "" {
			tprintRaw(fmt.Sprintf("%s%s%s", colorGreen, indentMultiline(newVal, indent), colorReset))
		}
	case goclientnew.Delete:
		if oldVal != "" {
			tprintRaw(fmt.Sprintf("%s%s%s", colorRed, indentMultiline(oldVal, indent), colorReset))
		}
	case goclientnew.Update, goclientnew.Replace:
		if oldVal != "" && newVal != "" {
			tprintRaw(fmt.Sprintf("%s%s%s → %s%s%s", colorRed, indentMultiline(oldVal, indent), colorReset, colorGreen, indentMultiline(newVal, indent), colorReset))
		} else if newVal != "" {
			tprintRaw(fmt.Sprintf("%s→ %s%s%s", indent, colorGreen, indentMultiline(newVal, indent), colorReset))
		} else if oldVal != "" {
			tprintRaw(fmt.Sprintf("%s%s%s →", colorRed, indentMultiline(oldVal, indent), colorReset))
		}
	}
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

// formatIndexLabel returns a label for a mutation index.
func formatIndexLabel(index int64, indicesAreMutationNums bool, priorHeadMutationNum int64, mutationMap map[int64]*goclientnew.ExtendedMutation) string {
	if !indicesAreMutationNums {
		return ""
	}
	label := fmt.Sprintf("(#%d", index)
	isNew := priorHeadMutationNum > 0 && index > priorHeadMutationNum
	if isNew {
		label += " NEW"
	}
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

// displayMutationValue displays a mutation value with indentation, truncating if not verbose.
func displayMutationValue(value string, indent string) {
	lines := strings.Split(strings.TrimRight(value, "\n"), "\n")
	for _, line := range lines {
		tprintRaw(indent + line)
	}
}

// displayMutationSummaryTable shows a table of mutation details.
func displayMutationSummaryTable(mutationMap map[int64]*goclientnew.ExtendedMutation, priorHeadMutationNum int64) {
	table := tableView()
	if !noheader {
		headers := []string{"Num", "Rev", "Source", "Link", "Trigger", "Invocation", "Function"}
		if priorHeadMutationNum > 0 {
			headers = append(headers, "New")
		}
		table.SetHeader(headers)
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
		row := []string{
			fmt.Sprintf("%d", m.MutationNum),
			fmt.Sprintf("%d", m.RevisionNum),
			mergeSourceSlug,
			linkSlug,
			triggerSlug,
			invocationSlug,
			m.FunctionInvocation.FunctionName,
		}
		if priorHeadMutationNum > 0 {
			if m.MutationNum > priorHeadMutationNum {
				row = append(row, "*")
			} else {
				row = append(row, "")
			}
		}
		table.Append(row)
	}
	table.Render()
}

// lookupMutations fetches ExtendedMutation details for the given MutationNum values.
func lookupMutations(indices []int64) map[int64]*goclientnew.ExtendedMutation {
	if len(indices) == 0 {
		return nil
	}

	// Build IN clause
	values := make([]string, len(indices))
	for i, idx := range indices {
		values[i] = fmt.Sprintf("%d", idx)
	}
	whereClause := fmt.Sprintf("MutationNum IN (%s)", strings.Join(values, ", "))

	// We need the unit ID for the mutation list API, but we can search at org level.
	// The mutations API requires a unit ID. We get it from the space-level search.
	// For now, use the apiListMutations which requires unit and space.
	// The caller should have the unit context available.
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

// lookupMutationsForCurrentUnit is set by the caller to provide unit context for mutation lookup.
// This avoids passing unit ID through the display function chain.
var lookupMutationsUnitID string


func lookupMutationsForCurrentUnit(whereClause string) ([]*goclientnew.ExtendedMutation, error) {
	if lookupMutationsUnitID == "" {
		return nil, fmt.Errorf("no unit ID set for mutation lookup")
	}
	return apiListMutations(selectedSpaceID, lookupMutationsUnitID, whereClause, "*", "")
}

// computeMutationsFromDryRun invokes compute-mutations on the server to compute a ResourceMutationList
// from the changed config data returned by a dry-run operation.
// previousData is the base64-encoded config data from before the change.
// changedData is the base64-encoded config data from the dry-run response.
func computeMutationsFromDryRun(previousData, changedData string, unitSpaceID string) (*goclientnew.ResourceMutationList, error) {
	if previousData == "" || changedData == "" {
		return nil, nil
	}

	// Decode previous data
	prevBytes, err := base64.StdEncoding.DecodeString(previousData)
	if err != nil {
		return nil, fmt.Errorf("failed to decode previous data: %w", err)
	}

	// Build function invocation request
	body := newFunctionInvocationsRequest()
	prevDataStr := string(prevBytes)
	functionIndex := "1"
	alreadyConverted := "false"
	reverse := "true"
	invocation := &goclientnew.FunctionInvocation{
		FunctionName: "compute-mutations",
		Arguments: []goclientnew.FunctionArgument{
			argFromString("config-doc-list", prevDataStr),
			argFromString("function-index", functionIndex),
			argFromString("already-converted", alreadyConverted),
			argFromString("reverse", reverse),
		},
	}
	body.FunctionInvocations = &[]goclientnew.FunctionInvocation{*invocation}

	// We need a non-dry-run invocation of compute-mutations on the changed unit data.
	// The unit already has changedData (from the dry-run response). We invoke with dry-run=true
	// since compute-mutations is non-mutating anyway.
	invokeArgs := &invokeArgs{
		Where:  fmt.Sprintf("UnitID = '%s'", lookupMutationsUnitID),
		DryRun: true, // compute-mutations is hermetic and non-mutating
		Body:   body,
	}

	// Save and restore selectedSpaceID if needed
	savedSpaceID := selectedSpaceID
	if unitSpaceID != "" {
		selectedSpaceID = unitSpaceID
	}
	resp, err := invokeFunctionsOnUnits(invokeArgs)
	selectedSpaceID = savedSpaceID
	if err != nil {
		return nil, fmt.Errorf("failed to invoke compute-mutations: %w", err)
	}

	if resp == nil || len(*resp) == 0 {
		return nil, nil
	}

	// Extract the ResourceMutationList from the output
	for _, r := range *resp {
		if !r.Success {
			continue
		}
		outputData, exists := r.Outputs[string(api.OutputTypeResourceMutationList)]
		if !exists || outputData == "" {
			continue
		}
		outputBytes, err := base64.StdEncoding.DecodeString(outputData)
		if err != nil {
			return nil, fmt.Errorf("failed to decode compute-mutations output: %w", err)
		}
		var mutations goclientnew.ResourceMutationList
		if err := json.Unmarshal(outputBytes, &mutations); err != nil {
			return nil, fmt.Errorf("failed to parse compute-mutations output: %w", err)
		}
		return &mutations, nil
	}

	return nil, nil
}

func argFromString(name, value string) goclientnew.FunctionArgument {
	v := &goclientnew.FunctionArgument_Value{}
	v.FromFunctionArgumentValue0(value)
	return goclientnew.FunctionArgument{
		ParameterName: &name,
		Value:         v,
	}
}

type pathRequest struct {
	info     api.AttributeInfo
	inputKey string // key using the original mutation path
}

// collectPathRequests collects paths from new mutations that have Update/Replace/Delete.
func collectPathRequests(mutations *goclientnew.ResourceMutationList, priorHeadMutationNum int64) []pathRequest {
	var requests []pathRequest
	for _, rm := range *mutations {
		resourceName := ""
		resourceType := ""
		if rm.Resource != nil {
			resourceName = rm.Resource.ResourceName
			resourceType = rm.Resource.ResourceType
		}
		if rm.PathMutationMap == nil {
			continue
		}
		for path, mi := range *rm.PathMutationMap {
			if mi.MutationType == nil || *mi.MutationType == goclientnew.None {
				continue
			}
			// Only fetch old values for new mutations
			if priorHeadMutationNum > 0 && mi.Index <= priorHeadMutationNum {
				continue
			}
			// Only for types where old values are meaningful
			if *mi.MutationType == goclientnew.Update ||
				*mi.MutationType == goclientnew.Replace ||
				*mi.MutationType == goclientnew.Delete {
				requests = append(requests, pathRequest{
					info: api.AttributeInfo{
						AttributeIdentifier: api.AttributeIdentifier{
							ResourceInfo: api.ResourceInfo{
								ResourceName: api.ResourceName(resourceName),
								ResourceType: api.ResourceType(resourceType),
							},
							Path: api.ResolvedPath(path),
						},
					},
					inputKey: pathValueKey(resourceType, resourceName, path),
				})
			}
		}
	}
	return requests
}

// buildGetPathsBody builds a FunctionInvocationsRequest for get-paths with the given path requests.
func buildGetPathsBody(requests []pathRequest) *goclientnew.FunctionInvocationsRequest {
	pathInfos := make([]api.AttributeInfo, len(requests))
	for i, r := range requests {
		pathInfos[i] = r.info
	}
	pathsJSON, err := json.Marshal(pathInfos)
	if err != nil {
		return nil
	}
	body := newFunctionInvocationsRequest()
	body.FunctionInvocations = &[]goclientnew.FunctionInvocation{
		{
			FunctionName: "get-paths",
			Arguments: []goclientnew.FunctionArgument{
				argFromString("paths", string(pathsJSON)),
			},
		},
	}
	return body
}

// extractOldValues extracts old values from a get-paths response, matching them to the
// original path requests. Returns a map of "resourceType/resourceName:path" → old value.
func extractOldValues(resp *[]goclientnew.FunctionInvocationsResponse, requests []pathRequest) map[string]string {
	result := make(map[string]string)
	for _, r := range *resp {
		if !r.Success {
			continue
		}
		outputData, exists := r.Outputs[string(api.OutputTypeAttributeValueList)]
		if !exists || outputData == "" {
			continue
		}
		outputBytes, err := base64.StdEncoding.DecodeString(outputData)
		if err != nil {
			continue
		}
		var attrValues api.AttributeValueList
		if err := json.Unmarshal(outputBytes, &attrValues); err != nil {
			continue
		}
		// Match returned values to input requests by resource+response path,
		// falling back to path resolution matching for selector-style paths.
		for _, av := range attrValues {
			respKey := pathValueKey(string(av.ResourceType), string(av.ResourceName), string(av.Path))
			matched := false
			for _, req := range requests {
				if req.inputKey == respKey {
					result[req.inputKey] = fmt.Sprintf("%v", av.Value)
					matched = true
					break
				}
			}
			if !matched {
				// A stored path names an array element by merge key while get-paths answers
				// with the position it resolved to, so the two are reconciled segment by
				// segment. A canonical path -- one carrying no recorded index -- matches any
				// position, so it is only accepted when exactly one request could have asked
				// for this value: pairing the wrong container's old value would be worse than
				// showing none.
				var only *pathRequest
				for i := range requests {
					req := &requests[i]
					if string(req.info.ResourceType) != string(av.ResourceType) ||
						string(req.info.ResourceName) != string(av.ResourceName) ||
						!pathsMatchResolved(string(req.info.Path), string(av.Path)) {
						continue
					}
					if only != nil {
						only = nil
						break
					}
					only = req
				}
				if only != nil {
					result[only.inputKey] = fmt.Sprintf("%v", av.Value)
				}
			}
		}
	}
	return result
}

// fetchOldPathValues calls get-paths on the current unit to fetch old values at paths
// that have new mutations (Index > priorHeadMutationNum) of type Update, Replace, or Delete.
// For dry-run operations, the unit still has its original data, so get-paths returns old values.
func fetchOldPathValues(mutations *goclientnew.ResourceMutationList, priorHeadMutationNum int64) map[string]string {
	if mutations == nil || lookupMutationsUnitID == "" {
		return nil
	}
	requests := collectPathRequests(mutations, priorHeadMutationNum)
	if len(requests) == 0 {
		return nil
	}
	body := buildGetPathsBody(requests)
	if body == nil {
		return nil
	}

	invokeArg := &invokeArgs{
		Where:  fmt.Sprintf("UnitID = '%s'", lookupMutationsUnitID),
		DryRun: true,
		Body:   body,
	}
	resp, err := invokeFunctionsOnUnits(invokeArg)
	if err != nil || resp == nil || len(*resp) == 0 {
		return nil
	}
	return extractOldValues(resp, requests)
}

// fetchOldPathValuesFromRevision calls get-paths on a prior revision to fetch old values.
// Used for non-dry-run operations where the unit already has new data.
func fetchOldPathValuesFromRevision(mutations *goclientnew.ResourceMutationList, priorHeadMutationNum int64, revisionIdentifier string) map[string]string {
	if mutations == nil || revisionIdentifier == "" {
		return nil
	}
	requests := collectPathRequests(mutations, priorHeadMutationNum)
	if len(requests) == 0 {
		return nil
	}
	body := buildGetPathsBody(requests)
	if body == nil {
		return nil
	}

	resp, err := invokeFunctionsOnRevision(revisionIdentifier, *body, true)
	if err != nil || resp == nil || len(*resp) == 0 {
		return nil
	}
	return extractOldValues(resp, requests)
}

// pathsMatchResolved checks if a resolved mutation path (e.g., "a.?name=x;@0.b")
// corresponds to a get-paths response path (e.g., "a.0.b").
// The mutation path may contain array selectors like "?name=x;@N" which get-paths
// resolves to just "N".
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

func pathsMatchResolved(mutationPath, responsePath string) bool {
	// Split both paths into segments
	mutParts := strings.Split(mutationPath, ".")
	respParts := strings.Split(responsePath, ".")

	mi, ri := 0, 0
	for mi < len(mutParts) && ri < len(respParts) {
		mutSeg := mutParts[mi]
		respSeg := respParts[ri]

		if mutSeg == respSeg {
			mi++
			ri++
			continue
		}

		// Check if mutSeg is a selector like "?name=x;@N" and respSeg is "N". A selector
		// with no ";@N" -- the canonical form stored MutationSources now uses -- names the
		// element without saying where it sits, so it matches any position; the caller is
		// what keeps that from pairing the wrong element.
		if strings.HasPrefix(mutSeg, "?") {
			if atIdx := strings.LastIndex(mutSeg, ";@"); atIdx >= 0 {
				if mutSeg[atIdx+2:] == respSeg {
					mi++
					ri++
					continue
				}
			} else if _, err := strconv.Atoi(respSeg); err == nil {
				mi++
				ri++
				continue
			}
		}

		return false
	}
	return mi == len(mutParts) && ri == len(respParts)
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

// displayMutationsForUnit fetches and displays mutations for a unit, distinguishing
// new changes from prior changes if priorHeadMutationNum > 0.
// newChangeDescription describes the operation that caused new changes.
func displayMutationsForUnit(unit *goclientnew.Unit, priorHeadMutationNum int64, newChangeDescription string, priorRevision string) {
	if unit.MutationSources == nil || len(*unit.MutationSources) == 0 {
		tprintRaw("No mutations")
		return
	}
	lookupMutationsUnitID = unit.UnitID.String()
	displayResourceMutationList(unit.MutationSources, true, priorHeadMutationNum, newChangeDescription, priorRevision)
}

// displayMutationsForRevision displays the mutations recorded on a revision, in the
// same form as the head-revision mutations shown for a unit. The recorded indices are
// the unit's MutationNums, so mutation details resolve against the same unit.
func displayMutationsForRevision(rev *goclientnew.Revision) {
	if rev.MutationSources == nil || len(*rev.MutationSources) == 0 {
		tprintRaw("No mutations")
		return
	}
	lookupMutationsUnitID = rev.UnitID.String()
	displayResourceMutationList(rev.MutationSources, true, 0, "", "")
}

// displayMutationsFromDryRun computes and displays mutations from a dry-run operation.
func displayMutationsFromDryRun(previousData, changedData string, unitSpaceID string, newChangeDescription string) {
	mutations, err := computeMutationsFromDryRun(previousData, changedData, unitSpaceID)
	if err != nil {
		tprintErr("Failed to compute mutations: %s", err.Error())
		return
	}
	if mutations == nil || len(*mutations) == 0 {
		tprintRaw("No mutations")
		return
	}
	displayResourceMutationList(mutations, false, 0, newChangeDescription, "dry-run")
}

// displayMutationsForRestore computes and displays the diff produced by a restore.
// Restore now snapshots the restored revision's MutationSources verbatim onto the
// unit, so the usual prior/new split can't tell them apart. We compute a fresh
// diff via compute-mutations on the server and present it under a "New changes
// from restore to N" header.
//
// The compute-mutations function diffs the unit's persisted data against a
// config-doc-list argument. Which side of the diff lives where depends on
// whether this was a dry-run:
//   - Non-dry-run: the unit on the server has the post-restore data, so we pass
//     previousData (pre-restore) as config-doc-list with reverse=false to get
//     the diff pre-restore → post-restore.
//   - Dry-run: the unit on the server still has the pre-restore data (dry-run
//     didn't persist), so we pass restoredData (returned in the dry-run
//     response) as config-doc-list with reverse=true to flip the diff into the
//     same pre-restore → post-restore direction.
func displayMutationsForRestore(previousData, restoredData string, unitSpaceID string, isDryRun bool, priorRevision string, newChangeDescription string) {
	var configDocList, reverse string
	if isDryRun {
		configDocList = restoredData
		reverse = "true"
	} else {
		configDocList = previousData
		reverse = "false"
	}
	mutations, err := invokeComputeMutations(configDocList, reverse, unitSpaceID)
	if err != nil {
		tprintErr("Failed to compute mutations: %s", err.Error())
		return
	}
	if mutations == nil || len(*mutations) == 0 {
		tprintRaw("No new changes")
		return
	}

	// Fetch old values for the displayed paths so the table shows old → new.
	// Dry-run: the unit on the server still has the pre-restore data, so
	// get-paths on the unit returns the old values directly. Non-dry-run:
	// the unit has the post-restore data, so we have to query the prior
	// revision instead.
	var oldValues map[string]string
	if isDryRun {
		oldValues = fetchOldPathValues(mutations, 0)
	} else if priorRevision != "" {
		oldValues = fetchOldPathValuesFromRevision(mutations, 0, priorRevision)
	}

	if newChangeDescription != "" {
		tprintRaw("New changes from " + newChangeDescription + ":")
	}
	displayMutationEntries(mutations, false, 0, nil, false, oldValues, nil)
}

// invokeComputeMutations runs the compute-mutations function on the unit
// identified by lookupMutationsUnitID, with the given base64-encoded
// config-doc-list argument and the given reverse flag (as a string "true"
// or "false"). Returns the resulting ResourceMutationList.
func invokeComputeMutations(configDocListBase64, reverse, unitSpaceID string) (*goclientnew.ResourceMutationList, error) {
	if configDocListBase64 == "" {
		return nil, nil
	}
	cfgBytes, err := base64.StdEncoding.DecodeString(configDocListBase64)
	if err != nil {
		return nil, fmt.Errorf("failed to decode config-doc-list: %w", err)
	}

	body := newFunctionInvocationsRequest()
	functionIndex := "1"
	alreadyConverted := "false"
	invocation := &goclientnew.FunctionInvocation{
		FunctionName: "compute-mutations",
		Arguments: []goclientnew.FunctionArgument{
			argFromString("config-doc-list", string(cfgBytes)),
			argFromString("function-index", functionIndex),
			argFromString("already-converted", alreadyConverted),
			argFromString("reverse", reverse),
		},
	}
	body.FunctionInvocations = &[]goclientnew.FunctionInvocation{*invocation}

	invokeArgs := &invokeArgs{
		Where:  fmt.Sprintf("UnitID = '%s'", lookupMutationsUnitID),
		DryRun: true, // compute-mutations is hermetic and non-mutating
		Body:   body,
	}

	savedSpaceID := selectedSpaceID
	if unitSpaceID != "" {
		selectedSpaceID = unitSpaceID
	}
	resp, err := invokeFunctionsOnUnits(invokeArgs)
	selectedSpaceID = savedSpaceID
	if err != nil {
		return nil, fmt.Errorf("failed to invoke compute-mutations: %w", err)
	}
	if resp == nil || len(*resp) == 0 {
		return nil, nil
	}
	for _, r := range *resp {
		if !r.Success {
			continue
		}
		outputData, exists := r.Outputs[string(api.OutputTypeResourceMutationList)]
		if !exists || outputData == "" {
			continue
		}
		outputBytes, err := base64.StdEncoding.DecodeString(outputData)
		if err != nil {
			return nil, fmt.Errorf("failed to decode compute-mutations output: %w", err)
		}
		var mutations goclientnew.ResourceMutationList
		if err := json.Unmarshal(outputBytes, &mutations); err != nil {
			return nil, fmt.Errorf("failed to parse compute-mutations output: %w", err)
		}
		return &mutations, nil
	}
	return nil, nil
}

// displayMutationsFromFunctionResponse displays mutations from a function invocation response.
// For both dry-run and non-dry-run, the Mutations field contains the full MutationSources
// with mutation indices corresponding to MutationNums. We use priorUnits to distinguish
// new changes from prior ones.
// priorUnits maps UnitID → (HeadMutationNum, unit slug, prior HeadRevisionNum).
// newChangeDescription describes the operation (e.g., function names).
func displayMutationsFromFunctionResponse(resp *[]goclientnew.FunctionInvocationsResponse, isDryRun bool, priorUnits map[string]priorUnitInfo, newChangeDescription string) {
	if resp == nil {
		return
	}
	for _, r := range *resp {
		if !r.Success {
			continue
		}
		tprintRaw(fmt.Sprintf("\nMutations for unit %s:", r.UnitID.String()))

		var priorMutNum int64
		var priorRevision string
		if priorUnits != nil {
			info := priorUnits[r.UnitID.String()]
			priorMutNum = info.HeadMutationNum
			if isDryRun {
				priorRevision = "dry-run"
			} else if info.Slug != "" && info.HeadRevisionNum > 0 {
				priorRevision = fmt.Sprintf("%s/%d", info.Slug, info.HeadRevisionNum)
			}
		}

		if isDryRun {
			// For dry-run, use the Mutations field directly.
			if r.Mutations != nil && len(*r.Mutations) > 0 {
				lookupMutationsUnitID = r.UnitID.String()
				displayResourceMutationList(r.Mutations, true, priorMutNum, newChangeDescription, priorRevision)
			} else {
				tprintRaw("No mutations")
			}
		} else {
			// For non-dry-run, fetch the updated unit to get the latest MutationSources
			unit, err := apiGetUnitInSpace(r.UnitID.String(), r.SpaceID.String(), "*")
			if err != nil {
				tprintErr("Failed to get unit: %s", err.Error())
				continue
			}
			lookupMutationsUnitID = unit.UnitID.String()
			displayMutationsForUnit(unit, priorMutNum, newChangeDescription, priorRevision)
		}
	}
}

// displayMutationsForBulkUnitUpdate displays mutations for each unit successfully
// updated by a bulk patch. It mirrors the single-unit display in unitUpdateCmdRun:
// a dry-run uses the proposed MutationSources returned in the response, a
// non-dry-run refetches the unit for its persisted MutationSources, and a restore
// computes a fresh diff (restore snapshots the restored revision's MutationSources
// verbatim, so the prior/new split can't tell them apart).
// isDryRun is passed rather than read from the global: a command with its own --dry-run flag
// (variant promote) leaves the global false, and a dry run displayed as a real one refetches
// the unmodified unit from the server and reports "No new changes".
func displayMutationsForBulkUnitUpdate(responses *[]goclientnew.UnitCreateOrUpdateResponse, priorUnits map[string]priorUnitInfo, isRestore, isDryRun bool, newChangeDescription string) {
	if responses == nil {
		return
	}
	first := true
	for i := range *responses {
		r := &(*responses)[i]
		if r.Error != nil || r.Unit == nil {
			continue
		}
		unit := r.Unit
		info := priorUnits[unit.UnitID.String()]

		if !first {
			// tprintRaw strips leading newlines, so separate units with their own line.
			tprintRaw("")
		}
		first = false
		tprintRaw(fmt.Sprintf("Mutations for unit %s:", unit.Slug))
		lookupMutationsUnitID = unit.UnitID.String()

		priorRevision := ""
		if isDryRun {
			priorRevision = "dry-run"
		} else if info.Slug != "" && info.HeadRevisionNum > 0 {
			priorRevision = fmt.Sprintf("%s/%d", info.Slug, info.HeadRevisionNum)
		}

		switch {
		case isRestore:
			displayMutationsForRestore(info.Data, unit.Data, unit.SpaceID.String(), isDryRun, priorRevision, newChangeDescription)
		case isDryRun:
			displayMutationsForUnit(unit, info.HeadMutationNum, newChangeDescription, "dry-run")
		default:
			updatedUnit, err := apiGetUnitInSpace(unit.UnitID.String(), unit.SpaceID.String(), "*")
			if err != nil {
				tprintErr("Failed to get unit: %s", err.Error())
				continue
			}
			displayMutationsForUnit(updatedUnit, info.HeadMutationNum, newChangeDescription, priorRevision)
		}
	}
}

// priorUnitInfo stores pre-operation state for a unit.
type priorUnitInfo struct {
	HeadMutationNum int64
	HeadRevisionNum int64
	Slug            string
	// Data is the pre-operation config data, populated only when the caller asks
	// for it (restore display diffs against it).
	Data string
}

// savePriorUnitInfoFromWhere queries units matching a where clause and returns their
// pre-operation state (HeadMutationNum, HeadRevisionNum, Slug).
func savePriorUnitInfoFromWhere(whereClause string, _ string) map[string]priorUnitInfo {
	return savePriorUnitInfoFromWhereWithData(whereClause, false)
}

// savePriorUnitInfoFromWhereWithData is savePriorUnitInfoFromWhere, additionally
// capturing each unit's pre-operation Data when includeData is set.
func savePriorUnitInfoFromWhereWithData(whereClause string, includeData bool) map[string]priorUnitInfo {
	return savePriorUnitInfoInSpace(selectedSpaceID, whereClause, includeData)
}

// savePriorUnitInfoInSpace is savePriorUnitInfoFromWhereWithData for a bulk operation that
// is not scoped to the selected space: `variant promote` names its downstream space as a
// positional argument rather than through --space.
func savePriorUnitInfoInSpace(spaceID, whereClause string, includeData bool) map[string]priorUnitInfo {
	selectList := "UnitID,HeadMutationNum,HeadRevisionNum,Slug"
	if includeData {
		selectList += ",Data"
	}
	units, err := apiListUnits(spaceID, whereClause, selectList)
	if err != nil {
		return nil
	}
	result := make(map[string]priorUnitInfo, len(units))
	for _, u := range units {
		result[u.UnitID.String()] = priorUnitInfo{
			HeadMutationNum: u.HeadMutationNum,
			HeadRevisionNum: u.HeadRevisionNum,
			Slug:            u.Slug,
			Data:            u.Data,
		}
	}
	return result
}
