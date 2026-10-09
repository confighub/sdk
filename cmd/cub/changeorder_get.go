// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/confighub/sdk/core/cubapi"
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

  # Also list the container images the changeorder changed in each space it reached
  cub changeorder get --space my-space --container-images release-changeorder
`+"```"+`
`, ""),
	RunE: changeorderGetCmdRun,
}

var changeorderGetContainerImages bool

func init() {
	addStandardGetFlags(changeorderGetCmd)
	enableOptionalSpace(changeorderGetCmd)
	changeorderGetCmd.Flags().BoolVar(&changeorderGetContainerImages, "container-images", false,
		"Also list the container images the changeorder changed in each space it has reached: those at the revisions its end tag marks that differ from those at the revisions its start tag marks")
	changeorderCmd.AddCommand(changeorderGetCmd)
}

func changeorderGetCmdRun(cmd *cobra.Command, args []string) error {
	changeorderDetails, err := resolveChangeOrder(args[0], selectedSpaceID, selectFields)
	if err != nil {
		return err
	}
	if changeorderGetContainerImages {
		changeOrder := changeorderDetails.ChangeOrder
		changeOrder.ContainerImages, err = cubapi.GetChangeOrderContainerImages(ctx, cubClient, changeOrder.SpaceID, changeOrder.ChangeOrderID)
		if err != nil {
			return err
		}
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
		view.Append([]string{"Skipped Units", skippedUnitsSummary(changeorderDetails.SkippedUnits)})
	}
	// Where the change has got to, which the server derives when it reads the change order.
	// In-scope first: it is what the other two are measured against.
	view.Append([]string{"State", changeorderDetails.State})
	stage, completed := changeOrderRollout(changeorderDetails)
	view.Append([]string{"Stage", stage})
	view.Append([]string{"Completed", completed})
	// The server does not expand ChangeWorkflowID, so the slug is looked up by id. The ID is the
	// fallback, since the workflow may have been deleted or be unreadable by the caller.
	if changeorderDetails.ChangeWorkflowID != nil {
		changeWorkflow, err := resolveChangeWorkflow(changeorderDetails.ChangeWorkflowID.String(), "", "ChangeWorkflowID,Slug")
		if err == nil && changeWorkflow != nil && changeWorkflow.ChangeWorkflow != nil {
			view.Append([]string{"Change Workflow", changeWorkflow.ChangeWorkflow.Slug})
		} else {
			view.Append([]string{"Change Workflow ID", changeorderDetails.ChangeWorkflowID.String()})
		}
	}
	// The copy of the workflow taken when it was associated, which is what promotions are judged
	// against even if the ChangeWorkflow has since been edited.
	if changeWorkflow := changeorderDetails.ChangeWorkflow; changeWorkflow != nil {
		for i, stage := range changeWorkflow.Stages {
			view.Append([]string{fmt.Sprintf("Stage %d: %s", i+1, stage.Name), formatChangeWorkflowStage(stage)})
		}
		if final := changeWorkflowFinalPrerequisites(changeWorkflow.Final); len(final) > 0 {
			view.Append([]string{"Final Gates", strings.Join(final, ", ")})
		}
		for _, custom := range changeWorkflow.CustomPrerequisites {
			detail := custom.Expression
			if custom.Description != "" {
				detail = custom.Description + " (" + custom.Expression + ")"
			}
			view.Append([]string{"Prerequisite " + custom.Name, detail})
		}
		for _, required := range changeWorkflow.AttestationPrerequisites {
			view.Append([]string{"Prerequisite " + required.Name, formatAttestationPrerequisite(required)})
		}
	}
	// Who last moved the change order and where to, and why it has not moved, when a promotion of
	// it did not complete. The most recent of each only: -o json has the rest.
	if n := len(changeorderDetails.Promotions); n > 0 {
		view.Append([]string{"Last Promotion", changeorderPromotion(changeorderDetails.Promotions[n-1], n)})
	}
	if n := len(changeorderDetails.PromotionFailures); n > 0 {
		view.Append([]string{"Last Promotion Failure", changeorderPromotionFailure(changeorderDetails.PromotionFailures[n-1], n)})
	}
	// Only there when asked for with --container-images.
	if len(changeorderDetails.ContainerImages) > 0 {
		view.Append([]string{"Container Images", changeorderContainerImages(changeorderDetails.ContainerImages)})
	}
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
	view.Append([]string{"Released Spaces", changeorderReleasedSpaces(changeorderDetails)})
	// Where it has been taken back out again, which only a change order somebody has undone has
	// anything to say about.
	if len(changeorderDetails.RestoredSpaceIDs) > 0 {
		view.Append([]string{"Restored Spaces", changeorderSpaceSlugs(changeorderDetails.RestoredSpaceIDs)})
		view.Append([]string{"Released Restored Spaces", changeorderSpaceSlugs(changeorderDetails.ReleasedRestoredSpaceIDs)})
	}

	view.Render()
}

// changeorderPromotionWhen heads a recorded promotion or failure: when it ran, the stage it
// entered when a change workflow governs the change order, and how many more are recorded.
func changeorderPromotionWhen(at time.Time, stage string, recorded int) string {
	when := at.Format(time.RFC3339)
	if stage != "" {
		when += ", stage " + stage
	}
	if recorded > 1 {
		when += fmt.Sprintf(" (%d recorded; -o json lists them all)", recorded)
	}
	return when
}

// changeorderPromotion renders one recorded promotion: when, and the spaces it promoted into.
func changeorderPromotion(promotion goclientnew.ChangeOrderPromotion, recorded int) string {
	return changeorderPromotionWhen(promotion.PromotedAt, promotion.Stage, recorded) + "\n" +
		changeorderSpaceSlugs(promotion.SpaceIDs)
}

// changeorderPromotionFailure renders one recorded failure: when, and each Space, Unit and Link it
// failed in with its error, one per line.
func changeorderPromotionFailure(failure goclientnew.ChangeOrderPromotionFailure, recorded int) string {
	lines := []string{changeorderPromotionWhen(failure.FailedAt, failure.Stage, recorded)}
	for _, space := range failure.Spaces {
		switch {
		case space.Error != "":
			lines = append(lines, fmt.Sprintf("%s: %s", space.SpaceSlug, space.Error))
		case space.Reason != "":
			lines = append(lines, fmt.Sprintf("%s: %s: %s", space.SpaceSlug, space.Action, space.Reason))
		}
		for _, unit := range space.Units {
			lines = append(lines, fmt.Sprintf("%s/%s: %s", space.SpaceSlug, unit.Slug, unit.Error))
		}
		for _, link := range space.Links {
			lines = append(lines, fmt.Sprintf("%s: link %s: %s", space.SpaceSlug, link.Slug, link.Error))
		}
	}
	return strings.Join(lines, "\n")
}

// changeorderContainerImages renders the images a change order changed, one per line: the space
// and unit, the resource and path, and the image before and after. The server sorts them.
func changeorderContainerImages(spaces []goclientnew.ChangeOrderSpaceContainerImages) string {
	orNone := func(image string) string {
		if image == "" {
			return "(none)"
		}
		return image
	}
	var lines []string
	for _, space := range spaces {
		for _, image := range space.Images {
			lines = append(lines, fmt.Sprintf("%s/%s %s %s %s: %s -> %s", space.SpaceSlug, image.UnitSlug,
				image.ResourceType, image.ResourceName, image.Path, orNone(image.FromImage), orNone(image.ToImage)))
		}
	}
	return strings.Join(lines, "\n")
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

// skippedUnitsSummary renders the units a change order or a release left out, by unit slug and
// reason. The server stores the reason against the unit's id, since a slug can be renamed.
// The generated client keys a uuid-keyed map by string, since JSON object keys are strings.
func skippedUnitsSummary(skipped map[string]string) string {
	lines := make([]string, 0, len(skipped))
	for unitID, reason := range skipped {
		name := unitID
		// Looked up by id across the organization: a fan-out change order's skipped units are its
		// sources, outside its own space.
		if unit, err := resolveUnit(unitID, "", "UnitID,Slug"); err == nil && unit != nil {
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
// changeorderReleasedSpaces lists the Spaces the change has been released in, each with the
// Release that released it there when the server names one.
func changeorderReleasedSpaces(changeorder *goclientnew.ChangeOrder) string {
	releaseNums := make(map[uuid.UUID]int64, len(changeorder.Releases))
	for _, release := range changeorder.Releases {
		releaseNums[release.SpaceID] = release.ReleaseNum
	}
	entries := make([]string, 0, len(changeorder.ReleasedSpaceIDs))
	for _, spaceID := range changeorder.ReleasedSpaceIDs {
		entry := changeorderSpaceSlugs([]uuid.UUID{spaceID})
		if num, ok := releaseNums[spaceID]; ok {
			entry += fmt.Sprintf(" (release %d)", num)
		}
		entries = append(entries, entry)
	}
	sort.Strings(entries)
	return strings.Join(entries, ", ")
}

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
