// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"os"

	"github.com/confighub/sdk/core/cubapi"
	goclientnew "github.com/confighub/sdk/core/openapi/goclient-new"
	"github.com/confighub/sdk/cubutil/diffview"
	"github.com/google/uuid"
)

// The diff reads and the rendering are shared with other tools: cubapi asks the server for a
// ConfigDiff and diffview draws one. These adapt them to cub's client and stdout.

// fetchUnitDiff reads the difference between two Revisions of a Unit, each named by a
// Revision selector the server resolves.
func fetchUnitDiff(spaceID, unitID uuid.UUID, from, to string, context bool) (*goclientnew.UnitDiff, error) {
	return cubapi.GetUnitDiff(ctx, cubClient, spaceID, unitID, from, to, context)
}

// diffConfigurations reads the difference between two configurations, each a Unit at a
// Revision or configuration given inline.
func diffConfigurations(request goclientnew.DiffRequest, context bool) (*goclientnew.DiffResult, error) {
	return cubapi.DiffConfigurations(ctx, cubClient, request, context)
}

// unitDiffSide is one side of a DiffRequest: a Unit at a Revision number.
func unitDiffSide(unitID uuid.UUID, revisionNum int64) *goclientnew.DiffSide {
	return cubapi.UnitDiffSide(unitID, revisionNum)
}

// inlineDiffSide is one side of a DiffRequest: configuration given inline.
func inlineDiffSide(data []byte, toolchainType string) *goclientnew.DiffSide {
	return cubapi.InlineDiffSide(data, toolchainType)
}

// diffStyle is how cub draws a diff: always in color, as it always has.
var diffStyle = diffview.Style{Color: true}

// displayConfigDiff renders a ConfigDiff: each resource that differs, and under it each
// changed path with its value on both sides, and any unchanged path returned as context with
// its value. This is the structured diff every `-o mutations` display shows for a change --
// a Revision diff, a dry run, or a write.
func displayConfigDiff(diff *goclientnew.ConfigDiff) {
	diffview.WriteConfigDiff(os.Stdout, diff, diffStyle)
}

// displayUnitChanges prints what an operation changed, or would change, in one Unit.
func displayUnitChanges(unitLabel, changeDescription string, diff *goclientnew.ConfigDiff) {
	diffview.WriteUnitChanges(os.Stdout, unitLabel, changeDescription, diff, diffStyle)
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
