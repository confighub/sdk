// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	goclientnew "github.com/confighub/sdk/core/openapi/goclient-new"
	"github.com/google/uuid"
	"github.com/spf13/cobra"
)

var changeorderGetCmd = &cobra.Command{
	Use:   "get <slug or id>",
	Short: "Get details about a changeorder",
	Args:  cobra.ExactArgs(1),
	Long: getCommandHelp(`Get detailed information about a changeorder in a space including its ID, slug, display name, tags, and description.

Examples:
`+"```"+`
  # Get details about a release changeorder
  cub changeorder get --space my-space release-changeorder

  # Get details about a changeorder in JSON format
  cub changeorder get --space my-space -o json hotfix-changeorder
`+"```"+`
`, ""),
	RunE: changeorderGetCmdRun,
}

func init() {
	addStandardGetFlags(changeorderGetCmd)
	enableOptionalSpace(changeorderGetCmd)
	changeorderCmd.AddCommand(changeorderGetCmd)
}

func changeorderGetCmdRun(cmd *cobra.Command, args []string) error {
	changeorderDetails, err := resolveChangeOrder(args[0], selectedSpaceID, selectFields)
	if err != nil {
		return err
	}

	displayGetResults(changeorderDetails, displayExtendedChangeOrderDetails)
	return nil
}

func displayChangeOrderDetails(changeorderDetails *goclientnew.ChangeOrder) {
	// Create an ExtendedChangeOrder wrapper with just the ChangeOrder set
	extendedChangeOrder := &goclientnew.ExtendedChangeOrder{
		ChangeOrder: changeorderDetails,
		// All other fields (Space, StartTag, EndTag, etc.) will be nil, causing Extended display to show IDs
	}
	displayExtendedChangeOrderDetails(extendedChangeOrder)
}

// changeOrderRollout is the Stage the server has recorded on the ChangeOrder, and whether that
// Stage is the completed one. Both are empty when no workflow governs the ChangeOrder.
func changeOrderRollout(changeOrder *goclientnew.ChangeOrder) (string, string) {
	if changeOrder.ChangeWorkflowID == nil {
		return "", ""
	}
	return changeOrder.Stage, strconv.FormatBool(changeOrder.Stage == "Completed")
}

func displayExtendedChangeOrderDetails(extendedChangeOrder *goclientnew.ExtendedChangeOrder) {
	changeorderDetails := extendedChangeOrder.ChangeOrder
	view := tableView()
	view.Append([]string{"ID", changeorderDetails.ChangeOrderID.String()})
	view.Append([]string{"Name", changeorderDetails.Slug})

	// Show Space slug instead of Space ID when available
	if extendedChangeOrder.Space != nil {
		view.Append([]string{"Space", extendedChangeOrder.Space.Slug})
	} else {
		view.Append([]string{"Space ID", changeorderDetails.SpaceID.String()})
	}

	if changeorderDetails.UpdateType != "" {
		view.Append([]string{"Update Type", changeorderDetails.UpdateType})
	}

	view.Append([]string{"Created At", changeorderDetails.CreatedAt.String()})
	view.Append([]string{"Updated At", changeorderDetails.UpdatedAt.String()})
	view.Append([]string{"Labels", labelsToString(changeorderDetails.Labels)})
	view.Append([]string{"Delete Gates", deleteGatesToString(changeorderDetails.DeleteGates)})
	view.Append([]string{"Annotations", annotationsToString(changeorderDetails.Annotations)})
	view.Append([]string{"Organization ID", changeorderDetails.OrganizationID.String()})

	// Show related entities by slug when available
	if extendedChangeOrder.StartTag != nil {
		view.Append([]string{"Start Tag", extendedChangeOrder.StartTag.Slug})
	}
	if extendedChangeOrder.EndTag != nil {
		view.Append([]string{"End Tag", extendedChangeOrder.EndTag.Slug})
	}
	if extendedChangeOrder.RestoreTag != nil {
		view.Append([]string{"Restore Tag", extendedChangeOrder.RestoreTag.Slug})
	}
	// What an Invoke change order runs, and over which units. Absent on the two update types
	// that follow links, which take their change from revisions the source unit already has.
	if extendedChangeOrder.Invocation != nil {
		view.Append([]string{"Invocation", extendedChangeOrder.Invocation.Slug})
	} else if changeorderDetails.InvocationID != nil {
		view.Append([]string{"Invocation ID", changeorderDetails.InvocationID.String()})
	}
	if len(changeorderDetails.Parameters) > 0 {
		view.Append([]string{"Parameters", changeorderParameters(changeorderDetails.Parameters)})
	}
	if changeorderDetails.WhereUnit != "" {
		view.Append([]string{"Where Unit", changeorderDetails.WhereUnit})
	}
	if extendedChangeOrder.UnitFilter != nil {
		view.Append([]string{"Unit Filter", extendedChangeOrder.UnitFilter.Slug})
	} else if changeorderDetails.UnitFilterID != nil {
		view.Append([]string{"Unit Filter ID", changeorderDetails.UnitFilterID.String()})
	}
	if changeorderDetails.Description != "" {
		view.Append([]string{"Description", changeorderDetails.Description})
	}
	if changeorderDetails.AbortedReason != "" {
		view.Append([]string{"Aborted Reason", changeorderDetails.AbortedReason})
	}
	if len(changeorderDetails.SkippedUnits) > 0 {
		view.Append([]string{"Skipped Units", changeorderSkippedUnits(changeorderDetails.SkippedUnits, changeorderDetails.SpaceID.String())})
	}
	// Where the change has got to, which the server derives when it reads the change order.
	// In-scope first: it is what the other two are measured against.
	view.Append([]string{"State", changeorderDetails.State})
	stage, completed := changeOrderRollout(changeorderDetails)
	view.Append([]string{"Stage", stage})
	view.Append([]string{"Completed", completed})
	// The selection, when there is one, is how the server worked out the in-scope spaces.
	if changeorderDetails.WhereSpace != "" {
		view.Append([]string{"Where Space", changeorderDetails.WhereSpace})
	}
	if extendedChangeOrder.SpaceFilter != nil {
		view.Append([]string{"Space Filter", extendedChangeOrder.SpaceFilter.Slug})
	} else if changeorderDetails.SpaceFilterID != nil {
		view.Append([]string{"Space Filter ID", changeorderDetails.SpaceFilterID.String()})
	}
	view.Append([]string{"In-Scope Spaces", changeorderSpaceSlugs(changeorderDetails.InScopeSpaceIDs)})
	view.Append([]string{"Resolved Spaces", changeorderSpaceSlugs(changeorderDetails.ResolvedSpaceIDs)})
	view.Append([]string{"Released Spaces", changeorderSpaceSlugs(changeorderDetails.ReleasedSpaceIDs)})
	// Where it has been taken back out again, which only a change order somebody has undone has
	// anything to say about.
	if len(changeorderDetails.RestoredSpaceIDs) > 0 {
		view.Append([]string{"Restored Spaces", changeorderSpaceSlugs(changeorderDetails.RestoredSpaceIDs)})
		view.Append([]string{"Released Restored Spaces", changeorderSpaceSlugs(changeorderDetails.ReleasedRestoredSpaceIDs)})
	}

	view.Render()
}

// changeorderParameters renders the values supplied for a parameterized invocation, in name order
// so that the same set reads the same way twice.
func changeorderParameters(parameters map[string]any) string {
	names := make([]string, 0, len(parameters))
	for name := range parameters {
		names = append(names, name)
	}
	sort.Strings(names)
	rendered := make([]string, 0, len(names))
	for _, name := range names {
		rendered = append(rendered, fmt.Sprintf("%s=%v", name, parameters[name]))
	}
	return strings.Join(rendered, ", ")
}

// changeorderSkippedUnits renders what the change order covers nothing of, by unit slug and
// reason. The server stores the reason against the unit's id, since a slug can be renamed.
// The generated client keys a uuid-keyed map by string, since JSON object keys are strings.
func changeorderSkippedUnits(skipped map[string]string, spaceID string) string {
	lines := make([]string, 0, len(skipped))
	for unitID, reason := range skipped {
		name := unitID
		// A skipped unit is always in the change order's own space -- they are the units the
		// derivation walked.
		if unit, err := resolveUnit(unitID, spaceID, "UnitID,Slug"); err == nil && unit != nil {
			name = unit.Unit.Slug
		}
		lines = append(lines, fmt.Sprintf("%s (%s)", name, reason))
	}
	// By name: the map has no order, and the same change order should read the same way twice.
	sort.Strings(lines)
	return strings.Join(lines, ", ")
}

// changeorderSpaceSlugs names Spaces by slug, falling back to the ID for one that cannot be read --
// a change order can reach a Space the caller has no View permission on.
func changeorderSpaceSlugs(spaceIDs []uuid.UUID) string {
	slugs := make([]string, 0, len(spaceIDs))
	for _, spaceID := range spaceIDs {
		space, err := resolveSpace(spaceID.String(), "SpaceID,Slug")
		if err != nil || space == nil {
			slugs = append(slugs, spaceID.String())
			continue
		}
		slugs = append(slugs, space.Space.Slug)
	}
	// By name: the server answers in ID order, which is stable but says nothing.
	sort.Strings(slugs)
	return strings.Join(slugs, ", ")
}
