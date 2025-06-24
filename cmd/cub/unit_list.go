// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"fmt"
	"net/url"

	goclientnew "github.com/confighub/sdk/openapi/goclient-new"
	"github.com/google/uuid"
	"github.com/olekukonko/tablewriter"
	"github.com/spf13/cobra"
)

var unitListCmd = &cobra.Command{
	Use:   "list",
	Short: "List units",
	Long: `List units you have access to in a space. The output includes display names, slugs, unit IDs, data size, head revision, apply gates, and last change timestamp.

Examples:
  # List all units in a space
  cub unit list --space my-space

  # List units without headers for scripting
  cub unit list --space my-space --noheader

  # List only unit slugs
  cub unit list --space my-space --noheader --slugs-only

  # List units with specific labels
  cub unit list --space my-space --where "Labels.tier = 'Backend'"

  # List units with approval gates
  cub unit list --space my-space --where "ApplyGates.require-approval/is-approved = true"

  # List units with any apply gates
  cub unit list --space my-space --where "LEN(ApplyGates) > 0"

  # List units that have been approved
  cub unit list --space my-space --where "LEN(ApprovedBy) > 0"

  # List units approved by a specific user
  cub unit list --space my-space --where "ApprovedBy ? 'd1b98309-874c-44ab-b1f2-a505e53dd9e8'"

  # List units with upstream revisions
  cub unit list --space my-space --where "UpstreamRevisionNum > 0"

  # List units with JSON output and JQ filtering
  cub unit list --space my-space --quiet --json --jq '.[].UnitID'`,
	Args:        cobra.ExactArgs(0),
	Annotations: map[string]string{"OrgLevel": ""},
	RunE:        unitListCmdRun,
}

var resourceType string
var whereData string

func init() {
	addStandardListFlags(unitListCmd)
	unitListCmd.Flags().StringVar(&resourceType, "resource-type", "", "resource-type filter")
	unitListCmd.Flags().StringVar(&whereData, "where-data", "", "where data filter")
	unitCmd.AddCommand(unitListCmd)
}

func unitListCmdRun(cmd *cobra.Command, args []string) error {
	var err error
	if whereData != "" {
		if selectedSpaceID != "*" {
			slugQuery := "SpaceID='" + selectedSpaceID + "'"
			if where != "" {
				where += " AND " + slugQuery
			} else {
				where = slugQuery
			}
			selectedSpaceID = "*"
		}
	}
	if selectedSpaceID == "*" {
		var extendedUnits []*goclientnew.ExtendedUnit
		extendedUnits, err = apiSearchUnits(where, resourceType, whereData)
		if err != nil {
			return err
		}
		displayListResults(extendedUnits, getExtendedUnitSlug, displayExtendedUnitList)
	} else {
		var units []*goclientnew.Unit
		units, err = apiListUnits(selectedSpaceID, where)
		if err != nil {
			return err
		}
		displayListResults(units, getUnitSlug, displayUnitList)
	}
	return nil
}

func getUnitSlug(unit *goclientnew.Unit) string {
	return unit.Slug
}

func getExtendedUnitSlug(extendedUnit *goclientnew.ExtendedUnit) string {
	return extendedUnit.Unit.Slug
}

func getUnitExtendedSlug(unitExtended *goclientnew.UnitExtended) string {
	return unitExtended.Unit.Slug
}

func appendUnitDetails(unitDetails *goclientnew.Unit, table *tablewriter.Table) {
	applyGates := "None"
	if len(unitDetails.ApplyGates) != 0 {
		if len(unitDetails.ApplyGates) > 1 {
			applyGates = "Multiple"
		} else {
			for key := range unitDetails.ApplyGates {
				applyGates = key
			}
		}
	}
	table.Append([]string{
		unitDetails.DisplayName,
		unitDetails.Slug,
		unitDetails.UnitID.String(),
		fmt.Sprintf("%d", len(unitDetails.Data)),
		fmt.Sprintf("%d", unitDetails.HeadRevisionNum),
		fmt.Sprintf("%d", unitDetails.HeadMutationNum),
		applyGates,
		unitDetails.LastChangeDescription,
	})
}

func displayUnitList(units []*goclientnew.Unit) {
	table := tableView()
	if !noheader {
		table.SetHeader([]string{"Display-Name", "Slug", "ID", "Data-Bytes", "Head-Revision", "Head-Mutation", "Apply-Gates", "Last-Change"})
	}
	for _, unitDetails := range units {
		appendUnitDetails(unitDetails, table)
	}
	table.Render()
}

func displayExtendedUnitList(units []*goclientnew.ExtendedUnit) {
	table := tableView()
	if !noheader {
		table.SetHeader([]string{"Display-Name", "Slug", "ID", "Data-Bytes", "Head-Revision", "Head-Mutation", "Apply-Gates", "Last-Change"})
	}
	for _, extendedUnitDetails := range units {
		u := extendedUnitDetails.Unit
		appendUnitDetails(u, table)
	}
	table.Render()
}

func apiListUnits(spaceID string, whereFilter string) ([]*goclientnew.Unit, error) {
	newParams := &goclientnew.ListUnitsParams{}
	if whereFilter != "" {
		whereFilter = url.QueryEscape(whereFilter)
		newParams.Where = &whereFilter
	}
	unitsRes, err := cubClientNew.ListUnitsWithResponse(ctx, uuid.MustParse(spaceID), newParams)
	if IsAPIError(err, unitsRes) {
		return nil, InterpretErrorGeneric(err, unitsRes)
	}

	units := make([]*goclientnew.Unit, 0, len(*unitsRes.JSON200))
	for _, unit := range *unitsRes.JSON200 {
		units = append(units, &unit)
	}
	return units, nil
}

func apiSearchUnits(whereFilter string, resourceType string, whereData string) ([]*goclientnew.ExtendedUnit, error) {
	newParams := &goclientnew.ListAllUnitsParams{}
	if whereFilter != "" {
		whereFilter = url.QueryEscape(whereFilter)
		newParams.Where = &whereFilter
	}

	if resourceType != "" {
		resourceType = url.QueryEscape(resourceType)
		newParams.ResourceType = &resourceType
	}
	if whereData != "" {
		whereData = url.QueryEscape(whereData)
		newParams.WhereData = &whereData
	}
	include := url.QueryEscape("UnitEventID,TargetID")
	newParams.Include = &include
	res, err := cubClientNew.ListAllUnits(ctx, newParams)
	if err != nil {
		return nil, err
	}
	unitsRes, err := goclientnew.ParseListAllUnitsResponse(res)
	if IsAPIError(err, unitsRes) {
		return nil, InterpretErrorGeneric(err, unitsRes)
	}
	extendedUnits := make([]*goclientnew.ExtendedUnit, 0, len(*unitsRes.JSON200))
	for _, unit := range *unitsRes.JSON200 {
		extendedUnits = append(extendedUnits, &unit)
	}

	return extendedUnits, nil
}
