// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"encoding/base64"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/olekukonko/tablewriter"

	"github.com/confighub/sdk/function/api"
	goclientnew "github.com/confighub/sdk/openapi/goclient-new"
	"github.com/spf13/cobra"
)

var unitGetCmd = &cobra.Command{
	Use:   "get <slug or id>",
	Short: "Get details about an unit",
	Args:  cobra.ExactArgs(1),
	Long:  getUnitGetHelp(),
	RunE:  unitGetCmdRun,
}

func getUnitGetHelp() string {
	baseHelp := `Get detailed information about a unit in a space including its configuration, status, and metadata.

Examples:
  # Get details about a namespace unit
  cub unit get --space my-space --json my-ns

  # Get details about a deployment unit
  cub unit get --space my-space --json my-deployment

  # Get details about a headlamp application unit
  cub unit get --space my-space --json headlamp

  # Get only the configuration data of a unit
  cub unit get --space my-space --data-only my-deployment

  # Get extended information about a unit
  cub unit get --space my-space --json --extended my-ns`

	agentContext := `Critical for inspecting unit configuration and state before making changes.

Agent inspection workflow:
1. Use 'unit get UNIT_SLUG' to understand unit structure and current state
2. Check revision numbers to understand change history
3. Use --data-only to get raw configuration for local processing

Key information provided:
- Unit metadata: ID, slug, display name, creation/update times
- Revision tracking: HeadRevisionNum vs LiveRevisionNum shows pending changes
- Approval state: ApprovedBy list and ApplyGates status
- Configuration data: Actual YAML/HCL content via --data-only

Important flags for agents:
- --data-only: Get just the configuration content, useful for:
  * Saving to local files for editing
  * Piping to other tools for processing
  * Understanding current configuration state
- --json: Get full metadata in structured format
- --extended: Include additional related entity information
- --quiet: Suppress table output, useful with --json

Common agent patterns:
  # Download unit for local editing
  cub unit get my-app --space prod --data-only > my-app.yaml
  
  # Check if unit has pending changes
  cub unit get my-app --space prod --json --jq '.HeadRevisionNum > .LiveRevisionNum'
  
  # Get approval status
  cub unit get my-app --space prod --json --jq '.ApprovedBy | length'

Use the slug or UUID to identify the unit. Slugs are more human-readable and typically preferred.`

	return getCommandHelp(baseHelp, agentContext)
}

func init() {
	addStandardGetFlags(unitGetCmd)
	enableVerboseFlag(unitGetCmd)
	unitGetCmd.Flags().BoolVar(&dataOnly, "data-only", false, "show config data without other response details")
	unitCmd.AddCommand(unitGetCmd)
}

func unitGetCmdRun(cmd *cobra.Command, args []string) error {
	unitDetails, err := apiGetUnitFromSlug(args[0])
	if err != nil {
		return err
	}

	// the previous call got the list resource. We want the "detail" resource just in case they're different
	unitDetails, err = apiGetUnit(unitDetails.UnitID.String())
	if err != nil {
		return err
	}
	displayGetResults(unitDetails, displayUnitDetails)
	return nil
}

func displayUnitExtendedDetails(view *tablewriter.Table, unitExtendedDetails *goclientnew.UnitExtended) {
	action := ""
	actionResult := ""
	if unitExtendedDetails.Action != nil {
		action = fmt.Sprintf("%s", *unitExtendedDetails.Action)
	}
	if unitExtendedDetails.ActionResult != nil {
		actionResult = fmt.Sprintf("%s", *unitExtendedDetails.ActionResult)
	}
	view.Append([]string{"Status", strings.TrimSpace(unitExtendedDetails.Status)})
	view.Append([]string{"Action", strings.TrimSpace(action)})
	view.Append([]string{"Action Result", strings.TrimSpace(actionResult)})
	view.Append([]string{"Action Started At", strings.TrimSpace(unitExtendedDetails.ActionStartedAt.String())})
	view.Append([]string{"Action Terminated At", strings.TrimSpace(unitExtendedDetails.ActionTerminatedAt.String())})
	view.Append([]string{"Drift", strings.TrimSpace(unitExtendedDetails.Drift)})

	if len(unitExtendedDetails.ApprovedByUsers) != 0 {
		approvers := ""
		for _, approver := range unitExtendedDetails.ApprovedByUsers {
			approvers += " " + approver
		}
		view.Append([]string{"Approved By", strings.TrimSpace(approvers)})
	}
	for _, link := range unitExtendedDetails.FromLinks {
		view.Append([]string{fmt.Sprintf("From Link %s To Unit ID", link.Slug), link.ToUnitID.String()})
		if link.ToSpaceID != unitExtendedDetails.Unit.SpaceID {
			view.Append([]string{fmt.Sprintf("From Link %s To Space ID", link.Slug), link.ToSpaceID.String()})
		}
	}
	for _, link := range unitExtendedDetails.ToLinks {
		view.Append([]string{fmt.Sprintf("To Link %s From Unit ID", link.Slug), link.FromUnitID.String()})
		if link.SpaceID != unitExtendedDetails.Unit.SpaceID {
			view.Append([]string{fmt.Sprintf("To Link %s From Space ID", link.Slug), link.SpaceID.String()})
		}
	}
}

func countResources(unitDetails *goclientnew.Unit) int {
	if len(*unitDetails.MutationSources) == 0 {
		return 0
	}
	count := 0
	for i := range *unitDetails.MutationSources {
		if *(*unitDetails.MutationSources)[i].ResourceMutationInfo.MutationType == goclientnew.MutationType(api.MutationTypeDelete) {
			continue
		}
		count++
	}
	return count
}

func displayUnitDetails(unitDetails *goclientnew.Unit) {
	if !dataOnly {
		view := tableView()
		view.Append([]string{"ID", unitDetails.UnitID.String()})
		view.Append([]string{"Slug", unitDetails.Slug})
		view.Append([]string{"Display Name", unitDetails.DisplayName})
		view.Append([]string{"Toolchain Type", unitDetails.ToolchainType})
		if unitDetails.SetID != nil && *unitDetails.SetID != uuid.Nil {
			view.Append([]string{"Set", unitDetails.SetID.String()})
		}
		if unitDetails.TargetID != nil && *unitDetails.TargetID != uuid.Nil {
			view.Append([]string{"Target", unitDetails.TargetID.String()})
		}
		view.Append([]string{"Created At", unitDetails.CreatedAt.String()})
		view.Append([]string{"Updated At", unitDetails.UpdatedAt.String()})
		view.Append([]string{"Labels", labelsToString(unitDetails.Labels)})
		view.Append([]string{"Annotations", annotationsToString(unitDetails.Annotations)})
		view.Append([]string{"Organization ID", unitDetails.OrganizationID.String()})
		view.Append([]string{"Last Change Description", unitDetails.LastChangeDescription})
		view.Append([]string{"Head Revision Num", fmt.Sprintf("%d", unitDetails.HeadRevisionNum)})
		view.Append([]string{"Last Applied Revision Num", fmt.Sprintf("%d", unitDetails.LastAppliedRevisionNum)})
		view.Append([]string{"Live Revision Num", fmt.Sprintf("%d", unitDetails.LiveRevisionNum)})
		view.Append([]string{"Previous Live Revision Num", fmt.Sprintf("%d", unitDetails.PreviousLiveRevisionNum)})
		if unitDetails.UpstreamUnitID != nil && *unitDetails.UpstreamUnitID != uuid.Nil {
			view.Append([]string{"Upstream Organization ID", unitDetails.UpstreamOrganizationID.String()})
			view.Append([]string{"Upstream Space ID", unitDetails.UpstreamSpaceID.String()})
			view.Append([]string{"Upstream Unit ID", unitDetails.UpstreamUnitID.String()})
			view.Append([]string{"Upstream Revision Num", fmt.Sprintf("%d", unitDetails.UpstreamRevisionNum)})
		}
		if len(unitDetails.ApplyGates) != 0 {
			gates := ""
			for gate, failed := range unitDetails.ApplyGates {
				if failed {
					gates += gate + " "
				}
			}
			view.Append([]string{"Apply Gates", strings.TrimSpace(gates)})
		}
		if len(unitDetails.ApprovedBy) != 0 {
			approverIDs := ""
			for _, approverID := range unitDetails.ApprovedBy {
				approverIDs += " " + approverID.String()
			}
			view.Append([]string{"Approved By", strings.TrimSpace(approverIDs)})
		}
		view.Append([]string{"Head Mutation Num", fmt.Sprintf("%d", unitDetails.HeadMutationNum)})
		view.Append([]string{"Number of Resources", fmt.Sprintf("%d", countResources(unitDetails))})

		if extended {
			unitExtended, err := apiGetUnitExtended(unitDetails.UnitID.String())
			if err != nil {
				failOnError(err)
			}
			displayUnitExtendedDetails(view, unitExtended)
		}

		view.Render()

		if len(*unitDetails.MutationSources) != 0 && verbose {
			tprintRaw("")
			tprintRaw("Mutation Sources:")
			tprintRaw("-----------------")
			// TODO: Make this prettier
			displayJSON(unitDetails.MutationSources)
		}

		if unitDetails.LiveState != "" && verbose {
			tprintRaw("")
			tprintRaw("Live State:")
			tprintRaw("-----------")
			livestate, err := base64.StdEncoding.DecodeString(unitDetails.LiveState)
			failOnError(err)
			tprintRaw(string(livestate))
		}
	}

	if dataOnly || verbose {
		if verbose {
			tprintRaw("")
			tprintRaw("Config Data:")
			tprintRaw("------------")
		}
		data, err := base64.StdEncoding.DecodeString(unitDetails.Data)
		failOnError(err)
		tprintRaw(string(data))
	}
}

func apiGetUnitExtended(unitID string) (*goclientnew.UnitExtended, error) {
	unitRes, err := cubClientNew.GetUnitExtendedWithResponse(ctx, uuid.MustParse(selectedSpaceID), uuid.MustParse(unitID))
	if IsAPIError(err, unitRes) {
		return nil, InterpretErrorGeneric(err, unitRes)
	}

	if unitRes.JSON200.Unit == nil || unitRes.JSON200.Unit.SpaceID.String() != selectedSpaceID {
		return nil, fmt.Errorf("SERVER DIDN'T CHECK: unit %s not found in space %s (%s)", unitID, selectedSpaceSlug, selectedSpaceID)
	}

	return unitRes.JSON200, nil
}

func apiGetUnit(unitID string) (*goclientnew.Unit, error) {
	extendedUnit, err := apiGetExtendedUnitInSpace(unitID, selectedSpaceID)
	if err != nil {
		return nil, err
	}
	if extendedUnit.Unit.SpaceID.String() != selectedSpaceID {
		return nil, fmt.Errorf("SERVER DIDN'T CHECK: unit %s not found in space %s (%s)", unitID, selectedSpaceSlug, selectedSpaceID)
	}

	return extendedUnit.Unit, nil
}

func apiGetUnitInSpace(unitID string, spaceID string) (*goclientnew.Unit, error) {
	extendedUnit, err := apiGetExtendedUnitInSpace(unitID, spaceID)
	if err != nil {
		return nil, err
	}
	return extendedUnit.Unit, nil
}

func apiGetExtendedUnitInSpace(unitID string, spaceID string) (*goclientnew.ExtendedUnit, error) {
	newParams := &goclientnew.GetUnitParams{}
	include := "UnitEventID,TargetID,UpstreamUnitID,SpaceID"
	newParams.Include = &include
	unitRes, err := cubClientNew.GetUnitWithResponse(ctx, uuid.MustParse(spaceID), uuid.MustParse(unitID), newParams)
	if IsAPIError(err, unitRes) {
		return nil, InterpretErrorGeneric(err, unitRes)
	}
	return unitRes.JSON200, nil
}

func apiGetUnitFromSlug(slug string) (*goclientnew.Unit, error) {
	return apiGetUnitFromSlugInSpace(slug, selectedSpaceID)
}

func apiGetUnitFromSlugInSpace(slug string, spaceID string) (*goclientnew.Unit, error) {
	id, err := uuid.Parse(slug)
	if err == nil {
		return apiGetUnit(id.String())
	}
	units, err := apiListUnits(spaceID, "Slug = '"+slug+"'")
	if err != nil {
		return nil, err
	}
	for _, unit := range units {
		if unit.Slug == slug {
			return unit, nil
		}
	}
	return nil, fmt.Errorf("unit %s not found in space %s", slug, spaceID)
}

func apiGetExtendedUnitFromSlugInSpace(slug string, spaceID string) (*goclientnew.ExtendedUnit, error) {
	_, err := uuid.Parse(slug)
	var where string
	if err == nil {
		where = "UnitID='" + slug + "'"
	} else {
		where = "SpaceID='" + spaceID + "' AND Slug='" + slug + "'"
	}
	units, err := apiSearchUnits(where, "", "")
	if err != nil {
		return nil, err
	}
	if len(units) == 1 {
		return units[0], nil
	}
	return nil, fmt.Errorf("unit %s not found in space %s", slug, spaceID)
}
