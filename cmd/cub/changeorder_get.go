// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/confighub/sdk/core/changeworkflow"
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
`+"```"+`
`, ""),
	RunE: changeorderGetCmdRun,
}

func init() {
	addStandardGetFlags(changeorderGetCmd)
	changeorderCmd.AddCommand(changeorderGetCmd)
}

func changeorderGetCmdRun(cmd *cobra.Command, args []string) error {
	changeorderDetails, err := apiGetExtendedChangeOrderFromSlug(args[0], selectFields)
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

// changeOrderRollout names how far the change has got through the ChangeWorkflow
// governing it -- the last Stage it has reached -- and whether that workflow considers
// the rollout completed. Nothing stores either -- a ChangeOrder has no notion of stages
// -- so both are read off the workflow, the Stage the same way promotion decides where
// to go next.
//
// Both are empty when no workflow governs the ChangeOrder or the workflow cannot be
// read: how far a change has got and whether it has completed are the workflow's
// readings, and there is no workflow to read them from. The Stage is also empty while
// the change has not finished the first one.
func changeOrderRollout(changeOrder *goclientnew.ChangeOrder) (string, string) {
	changeWorkflow, err := getChangeWorkflowForChangeOrder(changeOrder)
	if err != nil || changeWorkflow == nil {
		return "", ""
	}
	nextStage, currentStage, err := getNextWorkflowStage(changeWorkflow, changeOrder)
	if err != nil {
		return "", ""
	}
	stage := ""
	switch {
	// No next Stage means every Stage has the change, so the workflow's last Stage is
	// where it got to.
	case nextStage == nil && len(changeWorkflow.Spec.Stages) > 0:
		stage = changeWorkflow.Spec.Stages[len(changeWorkflow.Spec.Stages)-1].Name
	case currentStage != nil:
		stage = currentStage.Name
	}
	return stage, strconv.FormatBool(changeOrderIsCompleted(changeWorkflow, changeOrder))
}

// changeOrderIsCompleted reports whether the ChangeWorkflow considers the rollout
// completed: the change has reached the workflow's last Stage -- always checked, since a
// change that has not landed there has not landed everywhere the workflow sends it --
// and that Stage satisfies final.prerequisites. Neither is a check promotion can make,
// there being no hop left to gate once the change is in the last Stage.
//
// Completion is a different reading from State: State never consults live status, and it
// reduces over every Space in scope rather than over the last Stage, so the two
// legitimately disagree in both directions.
func changeOrderIsCompleted(changeWorkflow *changeworkflow.ChangeWorkflow, changeOrder *goclientnew.ChangeOrder) bool {
	if len(changeWorkflow.Spec.Stages) == 0 {
		return false
	}
	lastStage := &changeWorkflow.Spec.Stages[len(changeWorkflow.Spec.Stages)-1]

	// A last Stage selecting nothing is not one the change has reached: the workflow
	// says Spaces belong there, the same reading getNextWorkflowStage takes. Which
	// Spaces those are is the Stage's selector within the ChangeOrder's component,
	// so a Stage naming the component itself holds the rollout open rather than
	// completing it -- there is nothing to refuse at this point.
	component, err := changeOrderComponent(changeOrder)
	if err != nil {
		return false
	}
	variants, err := stageSpaces(lastStage, component, changeOrder, "*")
	if err != nil || len(variants) == 0 {
		return false
	}

	for _, variant := range variants {
		variantName := variant.Labels["Variant"]
		if variantName == "" {
			variantName = variant.Slug
		}
		// A prerequisite nothing knows how to check is an error to promotion and
		// unsatisfied here: there is nothing to refuse at this point, so it holds the
		// rollout open rather than passing it as completed.
		if checkVariantPrerequisites(changeWorkflow.Spec.Final.Prerequisites, changeOrder, variant, lastStage.Name, variantName) != nil {
			return false
		}
	}

	return true
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

// changeorderSkippedUnits renders what the change order covers nothing of, by unit slug and
// reason. The server stores the reason against the unit's id, since a slug can be renamed.
// The generated client keys a uuid-keyed map by string, since JSON object keys are strings.
func changeorderSkippedUnits(skipped map[string]string, spaceID string) string {
	lines := make([]string, 0, len(skipped))
	for unitID, reason := range skipped {
		name := unitID
		// A skipped unit is always in the change order's own space -- they are the units the
		// derivation walked.
		if unit, err := apiGetUnitInSpace(unitID, spaceID, "UnitID,Slug"); err == nil && unit != nil {
			name = unit.Slug
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
		space, err := apiGetSpace(spaceID.String(), "SpaceID,Slug")
		if err != nil || space == nil {
			slugs = append(slugs, spaceID.String())
			continue
		}
		slugs = append(slugs, space.Slug)
	}
	// By name: the server answers in ID order, which is stable but says nothing.
	sort.Strings(slugs)
	return strings.Join(slugs, ", ")
}

func apiGetChangeOrder(changeorderID string, selectParam string) (*goclientnew.ChangeOrder, error) {
	extendedChangeOrder, err := apiGetExtendedChangeOrder(changeorderID, selectParam)
	if err != nil {
		return nil, err
	}
	return extendedChangeOrder.ChangeOrder, nil
}

func apiGetExtendedChangeOrder(changeorderID string, selectParam string) (*goclientnew.ExtendedChangeOrder, error) {
	newParams := &goclientnew.GetChangeOrderParams{}
	include := "SpaceID,StartTagID,EndTagID,RestoreTagID"
	newParams.Include = &include
	selectValue := handleSelectParameter(selectParam, selectFields, nil)
	if selectValue != "" && selectValue != "*" {
		newParams.Select = &selectValue
	}
	changeorderRes, err := cubClientNew.GetChangeOrderWithResponse(ctx, uuid.MustParse(selectedSpaceID), uuid.MustParse(changeorderID), newParams)
	if cubapi.IsAPIError(err, changeorderRes) {
		return nil, cubapi.InterpretErrorGeneric(err, changeorderRes)
	}
	return changeorderRes.JSON200, nil
}

func apiGetChangeOrderFromSlug(slug string, selectParam string) (*goclientnew.ChangeOrder, error) {
	return apiGetChangeOrderFromSlugInSpace(slug, selectedSpaceID, selectParam)
}

func apiGetChangeOrderFromSlugWithSpace(slug string, selectParam string, spaceID string) (*goclientnew.ChangeOrder, error) {
	return apiGetChangeOrderFromSlugInSpace(slug, spaceID, selectParam)
}

func apiGetChangeOrderFromSlugInSpace(slug string, spaceID string, selectParam string) (*goclientnew.ChangeOrder, error) {
	id, err := uuid.Parse(slug)
	if err == nil {
		return apiGetChangeOrder(id.String(), selectParam)
	}
	// The default for get is "*" rather than auto-selected list columns
	if selectParam == "" {
		selectParam = "*"
	}
	changeorders, err := apiListChangeOrders(spaceID, "Slug = '"+slug+"'", selectParam, "")
	if err != nil {
		return nil, err
	}
	// find changeorder by slug
	for _, changeorder := range changeorders {
		if changeorder.ChangeOrder != nil && changeorder.ChangeOrder.Slug == slug {
			return changeorder.ChangeOrder, nil
		}
	}
	return nil, fmt.Errorf("changeorder %s not found in space %s", slug, spaceID)
}

func apiGetExtendedChangeOrderFromSlug(slug string, selectParam string) (*goclientnew.ExtendedChangeOrder, error) {
	id, err := uuid.Parse(slug)
	if err == nil {
		return apiGetExtendedChangeOrder(id.String(), selectParam)
	}
	// The default for get is "*" rather than auto-selected list columns
	if selectParam == "" {
		selectParam = "*"
	}
	changeorders, err := apiListChangeOrders(selectedSpaceID, "Slug = '"+slug+"'", selectParam, "")
	if err != nil {
		return nil, err
	}
	// find changeorder by slug
	for _, changeorder := range changeorders {
		if changeorder.ChangeOrder != nil && changeorder.ChangeOrder.Slug == slug {
			return changeorder, nil
		}
	}
	return nil, fmt.Errorf("changeorder %s not found in space %s", slug, selectedSpaceID)
}
