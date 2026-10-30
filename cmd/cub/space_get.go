// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"fmt"
	"strings"

	goclientnew "github.com/confighub/sdk/core/openapi/goclient-new"
	"github.com/olekukonko/tablewriter"
	"github.com/spf13/cobra"
)

var spaceGetCmd = &cobra.Command{
	Use:   "get <name or id>",
	Short: "Get details about a space",
	Args:  cobra.ExactArgs(1),
	Long: getCommandHelp(`Get detailed information about a space including its ID, name, and organization details.

The Release Target row is the target the space's units are released to, named as
<space>/<target> when the target is in another space.

For a space with a release target, the OCI Repository row is where its releases are published:
the oci:// URL to give Argo CD as an Application's repoURL, or Flux as an OCIRepository's url,
with the tag latest to follow the newest release or a release's manifest digest to pin it.

Examples:
`+"```"+`
  # Get space details in table format
  cub space get my-space

  # Get space details in JSON format
  cub space get -o json my-space

  # The OCI repository the space's releases are published to
  cub space get my-space -o jq=.Space.OCIRepositoryURL
`+"```"+`
`, ""),
	RunE: spaceGetCmdRun,
}

func init() {
	addStandardGetFlags(spaceGetCmd)
	spaceCmd.AddCommand(spaceGetCmd)
}

func spaceGetCmdRun(cmd *cobra.Command, args []string) error {
	extendedSpace, err := resolveSpaceSummary(args[0], selectFields)
	if err != nil {
		return err
	}

	displayGetResults(extendedSpace, displayExtendedSpaceDetails)
	return nil
}

func displaySpaceDetailsInView(spaceDetails *goclientnew.Space, expanded *goclientnew.ExtendedPermissions,
	component *goclientnew.Component, releaseTarget *goclientnew.Target, view *tablewriter.Table) {
	view.Append([]string{"ID", spaceDetails.SpaceID.String()})
	view.Append([]string{"Name", spaceDetails.Slug})
	if spaceDetails.ComponentID != nil {
		view.Append([]string{"Component", componentName(spaceDetails, component)})
	}
	view.Append([]string{"Created At", spaceDetails.CreatedAt.String()})
	view.Append([]string{"Updated At", spaceDetails.UpdatedAt.String()})
	view.Append([]string{"Labels", labelsToString(spaceDetails.Labels)})
	appendBackingUnitRow(view, spaceDetails.BackingUnitID)
	view.Append([]string{"Delete Gates", deleteGatesToString(spaceDetails.DeleteGates)})
	view.Append([]string{"Annotations", annotationsToString(spaceDetails.Annotations)})
	view.Append([]string{"Permissions", permissionsToString(spaceDetails.Permissions, expanded)})
	view.Append([]string{"Where Trigger", spaceDetails.WhereTrigger})
	view.Append([]string{"Release Target", releaseTargetName(spaceDetails, releaseTarget)})
	view.Append([]string{"OCI Repository", spaceDetails.OCIRepositoryURL})
	view.Append([]string{"Organization ID", spaceDetails.OrganizationID.String()})
}

// releaseTargetName names a space's release target as the CLI takes it: its slug, qualified with
// its space's slug when it is in another space, as release targets often are. A target that was
// not expanded, as when --select leaves it out, is named by its ID.
func releaseTargetName(space *goclientnew.Space, target *goclientnew.Target) string {
	if space.ReleaseTargetID == nil {
		return ""
	}
	if target == nil {
		return space.ReleaseTargetID.String()
	}
	if target.SpaceID != space.SpaceID && target.SpaceSlug != "" {
		return target.SpaceSlug + "/" + target.Slug
	}
	return target.Slug
}

// componentName names the Component a space is a Variant of by its slug, or by its ID when it
// was not expanded, as when --select leaves it out.
func componentName(space *goclientnew.Space, component *goclientnew.Component) string {
	if space.ComponentID == nil {
		return ""
	}
	if component == nil || component.Slug == "" {
		return space.ComponentID.String()
	}
	return component.Slug
}

func displaySpaceDetails(spaceDetails *goclientnew.Space) {
	// Create an ExtendedSpace wrapper with just the Space set
	extendedSpace := &goclientnew.ExtendedSpace{
		Space: spaceDetails,
		// All other fields (Organization, counts, etc.) will be nil/zero, causing Extended display to show basic info
	}
	displayExtendedSpaceDetails(extendedSpace)
}

func totalCountMap(counts map[string]int) int {
	if len(counts) == 0 {
		return 0
	}
	total := 0
	for _, count := range counts {
		total += count
	}
	return total
}

func triggerSliceSlugsToString(triggers []goclientnew.Trigger) string {
	if len(triggers) == 0 {
		return ""
	}
	slugs := make([]string, len(triggers))
	for i, trigger := range triggers {
		slugs[i] = trigger.Slug
	}
	return strings.Join(slugs, ", ")
}

func displayExtendedSpaceDetails(extendedSpace *goclientnew.ExtendedSpace) {
	view := tableView()
	displaySpaceDetailsInView(extendedSpace.Space, extendedSpace.Permissions, extendedSpace.Component,
		extendedSpace.ReleaseTarget, view)

	// Display TriggerFilter - use Slug if expanded, otherwise UUID
	if extendedSpace.TriggerFilter != nil {
		view.Append([]string{"Trigger Filter", extendedSpace.TriggerFilter.Slug})
	} else if extendedSpace.Space.TriggerFilterID != nil {
		view.Append([]string{"Trigger Filter", extendedSpace.Space.TriggerFilterID.String()})
	} else {
		view.Append([]string{"Trigger Filter", ""})
	}

	// Display Triggers - use Slugs if expanded, otherwise UUIDs
	if len(extendedSpace.Triggers) > 0 {
		view.Append([]string{"Triggers", triggerSliceSlugsToString(extendedSpace.Triggers)})
	} else if len(extendedSpace.Space.TriggerIDs) > 0 {
		view.Append([]string{"Triggers", uuidSliceToString(extendedSpace.Space.TriggerIDs)})
	} else {
		view.Append([]string{"Triggers", ""})
	}
	view.Append([]string{"Trigger Hash", extendedSpace.Space.TriggerHash})

	// TODO: TriggerCountByEventType
	view.Append([]string{"# Units", fmt.Sprintf("%d", extendedSpace.TotalUnitCount)})
	view.Append([]string{"# Unreleased Units", fmt.Sprintf("%d", extendedSpace.UnreleasedUnitCount)})
	view.Append([]string{"# Gated Units", fmt.Sprintf("%d", extendedSpace.GatedUnitCount)})
	view.Append([]string{"# Warned Units", fmt.Sprintf("%d", extendedSpace.WarnedUnitCount)})
	view.Append([]string{"# Upgradable Units", fmt.Sprintf("%d", extendedSpace.UpgradableUnitCount)})
	view.Append([]string{"# Unlinked Units", fmt.Sprintf("%d", extendedSpace.UnlinkedUnitCount)})
	view.Append([]string{"# Workers", fmt.Sprintf("%d", extendedSpace.TotalBridgeWorkerCount)})
	view.Append([]string{"# Filters", fmt.Sprintf("%d", extendedSpace.TotalFilterCount)})
	view.Append([]string{"# Views", fmt.Sprintf("%d", extendedSpace.TotalViewCount)})
	view.Append([]string{"# Tags", fmt.Sprintf("%d", extendedSpace.TotalTagCount)})
	view.Append([]string{"# ChangeSets", fmt.Sprintf("%d", extendedSpace.TotalChangeSetCount)})
	view.Append([]string{"# ChangeOrders", fmt.Sprintf("%d", extendedSpace.TotalChangeOrderCount)})
	view.Append([]string{"# ChangeWorkflows", fmt.Sprintf("%d", extendedSpace.TotalChangeWorkflowCount)})
	view.Append([]string{"# Invocations", fmt.Sprintf("%d", extendedSpace.TotalInvocationCount)})
	view.Append([]string{"# Targets", fmt.Sprintf("%d", extendedSpace.TotalTargetCount)})
	view.Append([]string{"# Triggers", fmt.Sprintf("%d", totalCountMap(extendedSpace.TriggerCountByEventType))})
	view.Append([]string{"# Attributes", fmt.Sprintf("%d", extendedSpace.TotalAttributeCount)})
	view.Append([]string{"# Channels", fmt.Sprintf("%d", extendedSpace.TotalChannelCount)})
	view.Render()
}
