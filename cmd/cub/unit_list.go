// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"fmt"
	"strings"

	goclientnew "github.com/confighub/sdk/openapi/goclient-new"
	"github.com/google/uuid"
	"github.com/spf13/cobra"
)

var unitListCmd = &cobra.Command{
	Use:         "list",
	Short:       "List units",
	Long:        getUnitListHelp(),
	Args:        cobra.ExactArgs(0),
	Annotations: map[string]string{"OrgLevel": ""},
	RunE:        unitListCmdRun,
}

func getUnitListHelp() string {
	baseHelp := `List units you have access to in a space. The output includes slugs, data size, head revision, apply gates, and last change timestamp.

Examples:
  # List all units in a space
  cub unit list --space my-space

  # List units without headers for scripting
  cub unit list --space my-space --no-header

  # List only unit names
  cub unit list --space my-space --no-header --names

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
  cub unit list --space my-space --quiet --json --jq '.[].Slug'

  # List units with custom columns
  cub unit list --space my-space --columns Unit.Slug,Target.Slug

  # List units showing label and annotation values
  cub unit list --space my-space --columns Unit.Slug,Unit.Labels.Environment,Unit.Labels.Tier,Unit.Annotations.Owner

Available columns (prefixed with Unit.):
  - Basic: Slug (or Name), DataBytes, HeadRevisionNum, HeadMutationNum
  - Metadata: CreatedAt, UpdatedAt, SpaceID, OrganizationID, UnitID
  - Status: ApplyGates, LastChangeDescription, LiveRevisionNum, LiveState, ApprovedBy
  - Relationships: SetID, TargetID, ToolchainType
  - Revisions: LastAppliedRevisionNum, PreviousLiveRevisionNum
  - Dynamic: Labels.<key>, Annotations.<key>

Example extended available columns (not exhaustive):
  - Basic: Space.Slug, Target.Slug
  - Status: UnitStatus.Status`

	agentContext := `Essential for discovering and filtering units in ConfigHub.

Agent discovery workflow:
1. Start with 'unit list --space SPACE' to see all units
2. Use --where filters to find specific units of interest
3. Use --names for scripting and automation

Key filtering patterns for agents:

Configuration state:
- Find units with pending changes: --where 'HeadRevisionNum > LiveRevisionNum' 
- Find unapplied units: --where 'LiveRevisionNum = 0'
- Find units with placeholders: Use 'function do get-placeholders' instead

Approval workflow:
- Find units needing approval: --where 'LEN(ApprovedBy) = 0'
- Find approved units: --where 'LEN(ApprovedBy) > 0'
- Find units with apply gates: --where 'LEN(ApplyGates) > 0'

Content filtering:
- By resource type: --resource-type apps/v1/Deployment --where-data "spec.replicas > 1"
- By labels: --where "Labels.app = 'myapp'"
- By creation time: --where "CreatedAt > '2025-01-01T00:00:00'"

Output formats:
- --json + --jq: Extract specific fields for further processing
- --names: Get unit identifiers for use with other commands
- --quiet: Suppress table headers for clean output

The --where flag supports SQL-like expressions with AND conjunctions. All attribute names are PascalCase as in JSON output.`

	return getCommandHelp(baseHelp, agentContext)
}

var resourceType string
var whereData string
var columns string

// Default columns to display when --columns is not specified
// var defaultUnitColumns = []string{"Name", "Space", "Target", "Status", "LastAction", "DataBytes", "HeadRevisionNum", "HeadMutationNum", "ApplyGates", "LastChangeDescription"}
var defaultUnitColumns = []string{"Unit.Slug", "Space.Slug", "Target.Slug", "UnitStatus.Status", "UnitStatus.LastAction", "UpgradeNeeded", "UnappliedChanges", "Unit.ApplyGates", "Unit.LastChangeDescription"}

// Unit-specific aliases
var unitAliases = map[string]string{
	"Name": "Unit.Slug",
	"ID":   "Unit.UnitID",
}

// Unit-specific custom columns
var unitCustomColumns = map[string]func(interface{}) string{
	"DataBytes": func(obj interface{}) string {
		if unit, ok := obj.(*goclientnew.ExtendedUnit); ok {
			return fmt.Sprintf("%d", len(unit.Unit.Data))
		}
		if unit, ok := obj.(*goclientnew.Unit); ok {
			return fmt.Sprintf("%d", len(unit.Data))
		}
		return "0"
	},
	"UpgradeNeeded": func(obj interface{}) string {
		if extendedUnit, ok := obj.(*goclientnew.ExtendedUnit); ok {
			unit := extendedUnit.Unit
			if extendedUnit.UpstreamUnit != nil {
				if unit.UpstreamRevisionNum < extendedUnit.UpstreamUnit.HeadRevisionNum {
					return "Yes"
				}
				if unit.UpstreamRevisionNum > 0 {
					return "No"
				}
			}
		}
		return ""
	},
	"UnappliedChanges": func(obj interface{}) string {
		if extendedUnit, ok := obj.(*goclientnew.ExtendedUnit); ok {
			unit := extendedUnit.Unit
			if unit.HeadRevisionNum > unit.LiveRevisionNum {
				return "Yes"
			}
		}
		return ""
	},
}

// Fields required by custom columns
var unitCustomColumnDependencies = map[string][]string{
	"DataBytes":        {"Data"},
	"UpgradeNeeded":    {"UpstreamRevisionNum", "UpstreamUnit.HeadRevisionNum"},
	"UnappliedChanges": {"HeadRevisionNum", "LiveRevisionNum"},
}

func init() {
	addStandardListFlags(unitListCmd)
	unitListCmd.Flags().StringVar(&resourceType, "resource-type", "", "resource-type filter")
	unitListCmd.Flags().StringVar(&whereData, "where-data", "", "where data filter")
	unitListCmd.Flags().StringVar(&columns, "columns", "", "comma-separated list of columns to display (e.g., Name,TargetID,Labels.Environment,Annotations.Owner)")
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
	var extendedUnits []*goclientnew.ExtendedUnit
	if selectedSpaceID == "*" {
		extendedUnits, err = apiSearchUnits(where, resourceType, whereData, selectFields)
		if err != nil {
			return err
		}
	} else {
		extendedUnits, err = apiListExtendedUnits(selectedSpaceID, where, selectFields)
		if err != nil {
			return err
		}
	}
	displayListResults(extendedUnits, getExtendedUnitSlug, displayExtendedUnitList)
	return nil
}

func getExtendedUnitSlug(extendedUnit *goclientnew.ExtendedUnit) string {
	return extendedUnit.Unit.Slug
}

func getUnitExtendedSlug(unitExtended *goclientnew.UnitExtended) string {
	return unitExtended.Unit.Slug
}

func displayExtendedUnitList(units []*goclientnew.ExtendedUnit) {
	DisplayListGeneric(units, columns, defaultUnitColumns, unitAliases, unitCustomColumns)
}

func apiListUnits(spaceID string, whereFilter string, selectParam string) ([]*goclientnew.Unit, error) {
	extendedUnits, err := apiListExtendedUnits(spaceID, whereFilter, selectParam)
	if err != nil {
		return nil, err
	}

	units := make([]*goclientnew.Unit, 0, len(extendedUnits))
	for _, extendedUnit := range extendedUnits {
		units = append(units, extendedUnit.Unit)
	}
	return units, nil
}

func apiListExtendedUnits(spaceID string, whereFilter string, selectParam string) ([]*goclientnew.ExtendedUnit, error) {
	newParams := &goclientnew.ListUnitsParams{}
	if whereFilter != "" {
		newParams.Where = &whereFilter
	}
	if contains != "" {
		newParams.Contains = &contains
	}
	include := "UnitEventID,TargetID,UpstreamUnitID,SpaceID"
	newParams.Include = &include
	// Handle select parameter
	selectValue := handleSelectParameter(selectParam, selectFields, func() string {
		baseFields := []string{"Slug", "UnitID", "SpaceID", "OrganizationID"}
		// UnitEventID is not a real field. Remove it.
		selectInclude := strings.TrimPrefix(include, "UnitEventID,")
		return buildSelectList("Unit", columns, selectInclude, defaultUnitColumns, unitAliases, unitCustomColumnDependencies, baseFields)
	})
	if selectValue != "" && selectValue != "*" {
		newParams.Select = &selectValue
	}
	unitsRes, err := cubClientNew.ListUnitsWithResponse(ctx, uuid.MustParse(spaceID), newParams)
	if IsAPIError(err, unitsRes) {
		return nil, InterpretErrorGeneric(err, unitsRes)
	}

	extendedUnits := make([]*goclientnew.ExtendedUnit, 0, len(*unitsRes.JSON200))
	for _, extendedUnit := range *unitsRes.JSON200 {
		extendedUnits = append(extendedUnits, &extendedUnit)
	}
	return extendedUnits, nil
}

func apiSearchUnits(whereFilter string, resourceType string, whereData string, selectParam string) ([]*goclientnew.ExtendedUnit, error) {
	newParams := &goclientnew.ListAllUnitsParams{}
	if whereFilter != "" {
		newParams.Where = &whereFilter
	}
	if contains != "" {
		newParams.Contains = &contains
	}

	if resourceType != "" {
		newParams.ResourceType = &resourceType
	}
	if whereData != "" {
		newParams.WhereData = &whereData
	}
	include := "UnitEventID,TargetID,UpstreamUnitID,SpaceID"
	newParams.Include = &include
	
	selectValue := handleSelectParameter(selectParam, selectFields, func() string {
		baseFields := []string{"Slug", "UnitID", "SpaceID", "OrganizationID"}
		// UnitEventID is not a real field. Remove it.
		selectInclude := strings.TrimPrefix(include, "UnitEventID,")
		return buildSelectList("Unit", columns, selectInclude, defaultUnitColumns, unitAliases, unitCustomColumnDependencies, baseFields)
	})
	if selectValue != "" && selectValue != "*" {
		newParams.Select = &selectValue
	}
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
