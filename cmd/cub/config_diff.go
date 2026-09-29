// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/confighub/sdk/core/cubapi"
	goclientnew "github.com/confighub/sdk/core/openapi/goclient-new"
	"github.com/google/uuid"
)

// fetchUnitDiff reads the difference between two Revisions of a Unit, each named by a
// Revision selector the server resolves.
func fetchUnitDiff(spaceID, unitID uuid.UUID, from, to string) (*goclientnew.UnitDiff, error) {
	params := &goclientnew.GetUnitDiffParams{}
	if from != "" {
		params.From = &from
	}
	if to != "" {
		params.To = &to
	}
	res, err := cubClientNew.GetUnitDiffWithResponse(ctx, spaceID, unitID, params)
	if cubapi.IsAPIError(err, res) {
		return nil, cubapi.InterpretErrorGeneric(err, res)
	}
	return res.JSON200, nil
}

// diffConfigurations reads the difference between two configurations, each a Unit at a
// Revision or configuration given inline.
func diffConfigurations(request goclientnew.DiffRequest) (*goclientnew.DiffResult, error) {
	res, err := cubClientNew.DiffConfigurationsWithResponse(ctx, &goclientnew.DiffConfigurationsParams{}, request)
	if cubapi.IsAPIError(err, res) {
		return nil, cubapi.InterpretErrorGeneric(err, res)
	}
	return res.JSON200, nil
}

// unitDiffSide is one side of a DiffRequest: a Unit at a Revision number.
func unitDiffSide(unitID uuid.UUID, revisionNum int64) *goclientnew.DiffSide {
	return &goclientnew.DiffSide{UnitID: unitID, Revision: strconv.FormatInt(revisionNum, 10)}
}

// inlineDiffSide is one side of a DiffRequest: configuration given inline.
func inlineDiffSide(data []byte, toolchainType string) *goclientnew.DiffSide {
	return &goclientnew.DiffSide{Data: string(data), ToolchainType: toolchainType}
}

// displayConfigDiff renders a ConfigDiff: each resource that differs, and under it each
// changed path with its value on both sides. This is the structured diff every
// `-o mutations` display shows for a change -- a Revision diff, a dry run, or a write.
func displayConfigDiff(diff *goclientnew.ConfigDiff) {
	if diff == nil || len(diff.Resources) == 0 {
		tprintRaw("No changes")
		return
	}
	for i := range diff.Resources {
		if i > 0 {
			tprintRaw("")
		}
		displayResourceDiff(&diff.Resources[i])
	}
}

func displayResourceDiff(resource *goclientnew.ResourceDiff) {
	changeType := goclientnew.None
	if resource.ChangeType != nil {
		changeType = *resource.ChangeType
	}
	header := fmt.Sprintf("%sResource: %s%s", colorLightBlue, resourceDiffLabel(resource.Resource), colorReset)
	switch changeType {
	case goclientnew.Add:
		header += "  " + colorGreen + "(added)" + colorReset
	case goclientnew.Delete:
		header += "  " + colorRed + "(deleted)" + colorReset
	case goclientnew.None:
		header += "  (unchanged)"
	}
	if resource.PreviousResource != nil {
		header += "  (was " + resourceDiffLabel(resource.PreviousResource) + ")"
	}
	tprintRaw(header + attributionLabel(resource.Attribution))

	switch changeType {
	case goclientnew.Add:
		tprintRaw(colorGreen + indentMultiline(trimMutationValue(resource.ToValue), "    ") + colorReset)
	case goclientnew.Delete:
		tprintRaw(colorRed + indentMultiline(trimMutationValue(resource.FromValue), "    ") + colorReset)
	}
	for i := range resource.Changes {
		displayPathChange(&resource.Changes[i])
	}
}

func resourceDiffLabel(resource *goclientnew.ResourceInfo) string {
	if resource == nil {
		return ""
	}
	return resource.ResourceType + " " + resource.ResourceName
}

func displayPathChange(change *goclientnew.PathChange) {
	const indent = "      "
	var symbol string
	switch change.ChangeType {
	case "Add":
		symbol = colorGreen + "+" + colorReset
	case "Delete":
		symbol = colorRed + "-" + colorReset
	case "Replace":
		symbol = colorGreen + "!" + colorReset
	default:
		symbol = colorGreen + "~" + colorReset
	}
	tprintRaw(fmt.Sprintf("  %s [%s] %s%s", symbol, change.ChangeType, change.DisplayPath, attributionLabel(change.Attribution)))

	fromValue, toValue := trimMutationValue(change.FromValue), trimMutationValue(change.ToValue)
	switch {
	case change.ChangeType == "Add":
		tprintRaw(colorGreen + indentMultiline(toValue, indent) + colorReset)
	case change.ChangeType == "Delete":
		tprintRaw(colorRed + indentMultiline(fromValue, indent) + colorReset)
	case change.Patch != "":
		displayPatch(change.Patch, indent)
	case strings.Contains(fromValue, "\n") || strings.Contains(toValue, "\n"):
		tprintRaw(colorRed + indentMultiline(fromValue, indent) + colorReset)
		tprintRaw(indent + "→")
		tprintRaw(colorGreen + indentMultiline(toValue, indent) + colorReset)
	default:
		tprintRaw(fmt.Sprintf("%s%s%s%s → %s%s%s", indent, colorRed, fromValue, colorReset, colorGreen, toValue, colorReset))
	}
}

// displayPatch prints a unified line diff, colored by line.
func displayPatch(patch, indent string) {
	for _, line := range strings.Split(strings.TrimRight(patch, "\n"), "\n") {
		switch {
		case strings.HasPrefix(line, "+++"), strings.HasPrefix(line, "---"):
			continue
		case strings.HasPrefix(line, "+"):
			tprintRaw(indent + colorGreen + line + colorReset)
		case strings.HasPrefix(line, "-"):
			tprintRaw(indent + colorRed + line + colorReset)
		default:
			tprintRaw(indent + line)
		}
	}
}

// attributionLabel says what set a value, when the diff was asked for with Attribution.
func attributionLabel(info *goclientnew.MutationInfo) string {
	if info == nil {
		return ""
	}
	return fmt.Sprintf("  (#%d)", info.Index)
}

// displayUnitChanges prints what an operation changed, or would change, in one Unit.
func displayUnitChanges(unitLabel, changeDescription string, diff *goclientnew.ConfigDiff) {
	header := "Changes to unit " + unitLabel
	if changeDescription != "" {
		header += " from " + changeDescription
	}
	tprintRaw(header + ":")
	if diff == nil {
		tprintRaw("The server returned no diff")
		return
	}
	displayConfigDiff(diff)
}

// displayDiffsFromFunctionResponse prints what an invocation changed, or on a dry run would
// change, in each Unit it succeeded on, from the Diff invokeIncludes asked for.
func displayDiffsFromFunctionResponse(resp *[]goclientnew.FunctionInvocationsResponse, changeDescription string) {
	if resp == nil {
		return
	}
	for i := range *resp {
		r := &(*resp)[i]
		if !r.Success {
			continue
		}
		label := r.UnitSlug
		if label == "" {
			label = r.UnitID.String()
		}
		tprintRaw("")
		displayUnitChanges(label, changeDescription, r.Diff)
	}
}

// displayDiffsForUnitWrites prints what each successful write of a bulk Unit operation changed,
// or on a dry run would change, from the Diff includeWriteDiff asked for.
func displayDiffsForUnitWrites(responses *[]goclientnew.UnitCreateOrUpdateResponse, changeDescription string) {
	if responses == nil {
		return
	}
	for i := range *responses {
		r := &(*responses)[i]
		if r.Error != nil || r.Unit == nil {
			continue
		}
		tprintRaw("")
		displayUnitChanges(r.Unit.Slug, changeDescription, r.Diff)
	}
}
