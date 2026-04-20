// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/confighub/sdk/core/cubapi"
	goclientnew "github.com/confighub/sdk/core/openapi/goclient-new"
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
` + "```" + `
  # List all units in a space
  cub unit list --space my-space

  # List units without headers for scripting
  cub unit list --space my-space --no-headers

  # List only unit names
  cub unit list --space my-space --no-headers -o name

  # List units with specific labels
  cub unit list --space my-space --where "Labels.tier = 'Backend'"

  # List units with approval gates
  cub unit list --space my-space --where "ApplyGates.require-approval/vet-approvedby = true"

  # List units with any apply gates
  cub unit list --space my-space --where "LEN(ApplyGates) > 0"

  # List units that have been approved
  cub unit list --space my-space --where "LEN(ApprovedBy) > 0"

  # List units approved by a specific user
  cub unit list --space my-space --where "ApprovedBy ? 'd1b98309-874c-44ab-b1f2-a505e53dd9e8'"

  # List units with upstream revisions
  cub unit list --space my-space --where "UpstreamRevisionNum > 0"

  # List units with jq filtering
  cub unit list --space my-space --quiet -o jq='.[].Unit.Slug'

  # List units with custom columns
  cub unit list --space my-space --columns Unit.Slug,Target.Slug

  # List units showing label and annotation values
  cub unit list --space my-space --columns Unit.Slug,Unit.Labels.Environment,Unit.Labels.Tier,Unit.Annotations.Owner
` + "```" + `

Available columns (prefixed with Unit.):

  - Basic: Slug (or Name), DataBytes, HeadRevisionNum, HeadMutationNum
  - Metadata: CreatedAt, UpdatedAt, SpaceID, OrganizationID, UnitID
  - Status: ApplyGates, LastChangeDescription, LiveRevisionNum, LiveState, ApprovedBy
  - Relationships: TargetID, ToolchainType
  - Revisions: LastAppliedRevisionNum, PreviousLiveRevisionNum
  - Dynamic: Labels.<key>, Annotations.<key>

Example extended available columns (not exhaustive):

  - Basic: Space.Slug, Target.Slug
  - Status: UnitStatus.Status`

	agentContext := `Essential for discovering and filtering units in ConfigHub.

Agent discovery workflow:
1. Start with 'unit list --space SPACE' to see all units
2. Use --where filters to find specific units of interest
3. Use -o name for scripting and automation

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
- By resource type: --resource-type apps/v1/Deployment --where-data "spec.replicas > 1" (--resource-type is optional; omitting it searches all resource types)
- By labels: --where "Labels.app = 'myapp'"
- By creation time: --where "CreatedAt > '2025-01-01T00:00:00'"

Output formats:
- -o jq=<expr>: Extract specific fields for further processing
- -o json: Full JSON payload
- -o name: Get unit identifiers (printed as <space-slug>/<slug>) for use with other commands
- --no-headers: Suppress table headers for clean output

The --where flag supports SQL-like expressions with AND conjunctions. All attribute names are PascalCase as in JSON output.`

	return getCommandHelp(baseHelp, agentContext)
}

var resourceType string
var whereData string
var columns []string
var whereTrigger string
var triggerFilter string
var triggersPassed bool
var viewSlug string

// Default columns to display when --columns is not specified
// var defaultUnitColumns = []string{"Name", "Space", "Target", "Status", "LastAction", "DataBytes", "HeadRevisionNum", "HeadMutationNum", "ApplyGates", "LastChangeDescription"}
var defaultUnitColumns = []string{"Unit.Slug", "Space.Slug", "ChangeSet.Slug", "Target.Slug", "UnitStatus.Status", "UnitStatus.LastAction", "ResourceStatus", "UpgradeNeeded", "UnappliedChanges", "Unit.ApplyGates", "Unit.LastChangeDescription"}

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
			if unit.HeadRevisionNum > unit.LiveRevisionNum && (unit.TargetID != nil && *unit.TargetID != uuid.Nil) {
				return "Yes"
			}
		}
		return ""
	},
	"ResourceStatus": func(obj interface{}) string {
		if extendedUnit, ok := obj.(*goclientnew.ExtendedUnit); ok {
			if extendedUnit.UnitStatus != nil && extendedUnit.UnitStatus.ResourceStatusSummary != nil {
				summary := extendedUnit.UnitStatus.ResourceStatusSummary
				if summary.Failed > 0 {
					return fmt.Sprintf("%d/%d Ready, %d Failed", summary.Ready, summary.Total, summary.Failed)
				}
				if summary.Total > 0 {
					return fmt.Sprintf("%d/%d Ready", summary.Ready, summary.Total)
				}
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
	"ResourceStatus":   {"UnitStatus.ResourceStatusSummary"},
}

func init() {
	addStandardListFlags(unitListCmd)
	enableWebFlag(unitListCmd)
	unitListCmd.Flags().StringVar(&resourceType, "resource-type", "", "resource-type filter")
	unitListCmd.Flags().StringVar(&whereData, "where-data", "", "where data filter")
	unitListCmd.Flags().StringVar(&whereTrigger, "where-trigger", "", "where expression to match triggers for validation filtering")
	unitListCmd.Flags().StringVar(&triggerFilter, "trigger-filter", "", "Filter UUID (with From=Trigger) for trigger validation filtering")
	unitListCmd.Flags().BoolVar(&triggersPassed, "triggers-passed", false, "return units passing trigger validation (default: return failing units)")
	unitListCmd.Flags().StringVar(&viewSlug, "view", "", "view slug or UUID to apply column definitions and optional filtering")
	unitCmd.AddCommand(unitListCmd)
}

func unitListCmdRun(cmd *cobra.Command, args []string) error {
	if webFlag {
		ctx := contextManager.ActiveContext()
		url := cubapi.GetUnitListURL(ctx.Coordinate.ServerURL)
		return openWebUI(url)
	}

	var err error

	// Resolve view slug/UUID to a UUID before space promotion, since the view
	// may reside in a specific space that we need to resolve the slug against.
	viewID := ""
	if viewSlug != "" {
		viewUUID, viewErr := parseEntityIdentifierSingle(viewSlug, EntityTypeView,
			apiGetViewFromSlugInSpace,
			func(v *goclientnew.View) string { return v.ViewID.String() },
		)
		if viewErr != nil {
			return viewErr
		}
		viewID = viewUUID.String()
	}

	// Promote to org-level search when data/trigger/view filters are used with a specific space
	if whereData != "" || whereTrigger != "" || triggerFilter != "" || viewID != "" {
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
	filterID, err := parseFilterFlag(filter)
	if err != nil {
		return err
	}

	var extendedUnits []*goclientnew.ExtendedUnit
	if selectedSpaceID == "*" {
		extendedUnits, err = apiSearchUnits(where, resourceType, whereData, whereTrigger, triggerFilter, triggersPassed, selectFields, filterID, viewID)
		if err != nil {
			return err
		}
	} else {
		extendedUnits, err = apiListExtendedUnits(selectedSpaceID, where, resourceType, whereData, whereTrigger, triggerFilter, triggersPassed, selectFields, filterID, viewID)
		if err != nil {
			return err
		}
	}
	displayListResults(extendedUnits, getExtendedUnitSlug, displayExtendedUnitList)
	return nil
}

func getExtendedUnitSlug(extendedUnit *goclientnew.ExtendedUnit) string {
	space := ""
	if extendedUnit.Space != nil {
		space = extendedUnit.Space.Slug
	}
	return prefixedSlug(space, extendedUnit.Unit.Slug)
}

func displayExtendedUnitList(units []*goclientnew.ExtendedUnit) {
	cols := effectiveColumns()
	// When a view is active and units have View metadata, display view columns
	if viewSlug != "" && len(cols) == 0 && len(units) > 0 && units[0].View != nil && len(units[0].View.Columns) > 0 {
		displayViewColumnList(units)
		return
	}
	DisplayListGeneric(units, cols, defaultUnitColumns, unitAliases, unitCustomColumns)
}

// displayViewColumnList displays units using the View's column definitions.
// It builds the table from ViewColumns data, sorts by GroupBy/OrderByDirection,
// and handles numeric sorting for int columns.
func displayViewColumnList(units []*goclientnew.ExtendedUnit) {
	view := units[0].View
	viewCols := view.Columns

	// Build column name list for headers
	colNames := make([]string, len(viewCols))
	for i, col := range viewCols {
		colNames[i] = col.Name
	}

	// Sort units by GroupBy/OrderByDirection columns
	sortViewUnits(units, viewCols)

	// Build table
	table := tableView()
	if !noheader {
		table.SetHeader(colNames)
	}

	for _, eu := range units {
		row := make([]string, len(viewCols))
		for i, col := range viewCols {
			row[i] = getViewColumnValue(eu, col.Name)
		}
		table.Append(row)
	}

	table.Render()
}

// getViewColumnValue finds the value of a named column from an ExtendedUnit's ViewColumns.
func getViewColumnValue(eu *goclientnew.ExtendedUnit, name string) string {
	for _, vc := range eu.ViewColumns {
		if vc.Name == name {
			return vc.Value
		}
	}
	return ""
}

// sortViewUnits sorts units by the View's ordering columns.
// Top-level GroupBy/OrderBy take priority, then column-level GroupBy/OrderByDirection in column order.
// GroupBy implies ascending order unless OrderByDirection is specified.
// Non-empty OrderByDirection implies ordering by that column.
// Int columns are sorted numerically.
func sortViewUnits(units []*goclientnew.ExtendedUnit, viewCols []goclientnew.Column) {
	view := units[0].View

	// Collect ordering columns in priority order
	type orderCol struct {
		name      string
		ascending bool
		isInt     bool
	}
	var orderCols []orderCol

	// Helper to find a column's DataType by name
	colDataType := func(name string) string {
		for _, c := range viewCols {
			if c.Name == name {
				return c.DataType
			}
		}
		return ""
	}

	// Top-level GroupBy takes first priority
	if view.GroupBy != "" {
		dir := view.OrderByDirection
		if dir == "" {
			dir = "ASC"
		}
		orderCols = append(orderCols, orderCol{
			name: view.GroupBy, ascending: dir != "DESC", isInt: colDataType(view.GroupBy) == "int",
		})
	}

	// Top-level OrderBy takes next priority (if different from GroupBy)
	if view.OrderBy != "" && view.OrderBy != view.GroupBy {
		dir := view.OrderByDirection
		if dir == "" {
			dir = "ASC"
		}
		orderCols = append(orderCols, orderCol{
			name: view.OrderBy, ascending: dir != "DESC", isInt: colDataType(view.OrderBy) == "int",
		})
	}

	// Column-level ordering follows, in column order
	for _, col := range viewCols {
		// Skip columns already handled by top-level
		if col.Name == view.GroupBy || col.Name == view.OrderBy {
			continue
		}
		dir := col.OrderByDirection
		if col.GroupBy && dir == "" {
			dir = "ASC"
		}
		if dir == "" {
			continue
		}
		orderCols = append(orderCols, orderCol{
			name:      col.Name,
			ascending: dir != "DESC",
			isInt:     col.DataType == "int",
		})
	}
	if len(orderCols) == 0 {
		return
	}

	sort.SliceStable(units, func(i, j int) bool {
		for _, oc := range orderCols {
			vi := getViewColumnValue(units[i], oc.name)
			vj := getViewColumnValue(units[j], oc.name)
			if vi == vj {
				continue
			}
			less := false
			if oc.isInt {
				ni, ei := strconv.ParseInt(vi, 10, 64)
				nj, ej := strconv.ParseInt(vj, 10, 64)
				if ei == nil && ej == nil {
					less = ni < nj
				} else {
					less = vi < vj
				}
			} else {
				less = vi < vj
			}
			if oc.ascending {
				return less
			}
			return !less
		}
		return false
	})
}

func apiListUnits(spaceID string, whereFilter string, selectParam string) ([]*goclientnew.Unit, error) {
	extendedUnits, err := apiListExtendedUnits(spaceID, whereFilter, "", "", "", "", false, selectParam, "", "")
	if err != nil {
		return nil, err
	}

	units := make([]*goclientnew.Unit, 0, len(extendedUnits))
	for _, extendedUnit := range extendedUnits {
		units = append(units, extendedUnit.Unit)
	}
	return units, nil
}

func apiListExtendedUnits(spaceID string, whereFilter string, resourceType string, whereData string, whereTrigger string, triggerFilter string, triggersPassed bool, selectParam string, filterParam string, viewParam string) ([]*goclientnew.ExtendedUnit, error) {
	newParams := &goclientnew.ListUnitsParams{}
	if whereFilter != "" {
		newParams.Where = &whereFilter
	}
	if filterParam != "" {
		newParams.Filter = &filterParam
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
	if whereTrigger != "" {
		newParams.WhereTrigger = &whereTrigger
	}
	if triggerFilter != "" {
		newParams.TriggerFilter = &triggerFilter
	}
	if triggersPassed {
		newParams.TriggersPassed = &triggersPassed
	}
	if viewParam != "" {
		newParams.View = &viewParam
	}
	include := "UnitEventID,TargetID,UpstreamUnitID,SpaceID,FromLinkID,BridgeWorkerID,ChangeSetID"
	newParams.Include = &include
	// Handle select parameter
	selectValue := handleSelectParameter(selectParam, selectFields, func() string {
		baseFields := []string{"Slug", "UnitID", "SpaceID", "OrganizationID"}
		// UnitEventID is not a real field. Remove it.
		selectInclude := strings.TrimPrefix(include, "UnitEventID,")
		return buildSelectList("Unit", effectiveColumns(), selectInclude, defaultUnitColumns, unitAliases, unitCustomColumnDependencies, baseFields)
	})
	if selectValue != "" && selectValue != "*" {
		newParams.Select = &selectValue
	}
	unitsRes, err := cubClientNew.ListUnitsWithResponse(ctx, uuid.MustParse(spaceID), newParams)
	if cubapi.IsAPIError(err, unitsRes) {
		return nil, cubapi.InterpretErrorGeneric(err, unitsRes)
	}

	extendedUnits := make([]*goclientnew.ExtendedUnit, 0, len(*unitsRes.JSON200))
	for _, extendedUnit := range *unitsRes.JSON200 {
		extendedUnits = append(extendedUnits, &extendedUnit)
	}
	return extendedUnits, nil
}

func apiSearchUnits(whereFilter string, resourceType string, whereData string, whereTrigger string, triggerFilter string, triggersPassed bool, selectParam string, filterParam string, viewParam string) ([]*goclientnew.ExtendedUnit, error) {
	newParams := &goclientnew.ListAllUnitsParams{}
	if whereFilter != "" {
		newParams.Where = &whereFilter
	}
	if contains != "" {
		newParams.Contains = &contains
	}
	if filterParam != "" {
		newParams.Filter = &filterParam
	}

	if resourceType != "" {
		newParams.ResourceType = &resourceType
	}
	if whereData != "" {
		newParams.WhereData = &whereData
	}
	if whereTrigger != "" {
		newParams.WhereTrigger = &whereTrigger
	}
	if triggerFilter != "" {
		newParams.TriggerFilter = &triggerFilter
	}
	if triggersPassed {
		newParams.TriggersPassed = &triggersPassed
	}
	if viewParam != "" {
		newParams.View = &viewParam
	}
	include := "UnitEventID,TargetID,UpstreamUnitID,SpaceID,FromLinkID,BridgeWorkerID,ChangeSetID"
	newParams.Include = &include

	selectValue := handleSelectParameter(selectParam, selectFields, func() string {
		baseFields := []string{"Slug", "UnitID", "SpaceID", "OrganizationID"}
		// UnitEventID is not a real field. Remove it.
		selectInclude := strings.TrimPrefix(include, "UnitEventID,")
		return buildSelectList("Unit", effectiveColumns(), selectInclude, defaultUnitColumns, unitAliases, unitCustomColumnDependencies, baseFields)
	})
	if selectValue != "" && selectValue != "*" {
		newParams.Select = &selectValue
	}
	res, err := cubClientNew.ListAllUnits(ctx, newParams)
	if err != nil {
		return nil, err
	}
	unitsRes, err := goclientnew.ParseListAllUnitsResponse(res)
	if cubapi.IsAPIError(err, unitsRes) {
		return nil, cubapi.InterpretErrorGeneric(err, unitsRes)
	}
	extendedUnits := make([]*goclientnew.ExtendedUnit, 0, len(*unitsRes.JSON200))
	for _, unit := range *unitsRes.JSON200 {
		extendedUnits = append(extendedUnits, &unit)
	}

	return extendedUnits, nil
}
