// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"fmt"
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
	baseHelp := `List units you have access to in a space, or across the organization without --space. The output includes slugs, data size, head revision, validation errors, and last change timestamp.

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

  # List units a particular Trigger gates
  cub unit list --space my-space --where "ValidationErrors.my-space/no-latest/vet-cel = true"

  # List units with any validation errors
  cub unit list --space my-space --where "LEN(ValidationErrors) > 0"

  # List units with upstream revisions
  cub unit list --space my-space --where "UpstreamRevisionNum > 0"

  # List units that need a value and have none of it bound to a link yet
  cub unit list --where "NeededPaths.*.AttributeName IS NOT NULL AND NeededPaths.*.Details.BoundLinkID IS NULL"

  # List units that provide a named attribute
  cub unit list --where "ProvidedPaths.*.AttributeName = 'image'"

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
  - Status: ValidationErrors, LastChangeDescription
  - Relationships: TargetID, ToolchainType
  - Revisions: LastReleasedRevisionNum
  - Dynamic: Labels.<key>, Annotations.<key>

Example extended available columns (not exhaustive):

  - Basic: Space.Slug, Target.Slug`

	agentContext := `Essential for discovering and filtering units in ConfigHub.

Agent discovery workflow:
1. Start with 'unit list --space SPACE' to see all units
2. Use --where filters to find specific units of interest
3. Use -o name for scripting and automation

Key filtering patterns for agents:

Configuration state:
- Find units with unreleased changes: --where 'HeadRevisionNum > LastReleasedRevisionNum AND TargetID IS NOT NULL'
- Find never-released units: --where 'LastReleasedRevisionNum = 0'
- Find units with placeholders: Use 'function get get-placeholders' instead

Validation:
- Find units with validation errors: --where 'LEN(ValidationErrors) > 0'

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

// Default columns to display when --columns is not specified
// var defaultUnitColumns = []string{"Name", "Space", "Target", "Status", "LastAction", "DataBytes", "HeadRevisionNum", "HeadMutationNum", "ValidationErrors", "LastChangeDescription"}
var defaultUnitColumns = []string{"Unit.Slug", "Space.Slug", "ChangeSet.Slug", "Target.Slug", "UpgradeNeeded", "UnreleasedChanges", "Unit.ValidationErrors", "Unit.LastChangeDescription"}

// unitListInclude is the Include parameter for unit list queries (the related
// entities expanded into each ExtendedUnit). UnitEventID is a pseudo-field used
// only by Include, not a selectable field.
const unitListInclude = "UnitEventID,TargetID,UpstreamUnitID,SpaceID,FromLinkID,ChangeSetID"

// unitBaseSelectFields are the fields always returned by unit list queries,
// regardless of the requested columns.
var unitBaseSelectFields = []string{"Slug", "UnitID", "SpaceID", "OrganizationID"}

// Unit-specific aliases
var unitAliases = map[string]string{
	"Name": "Unit.Slug",
	"ID":   "Unit.UnitID",
}

// Unit-specific custom columns
var unitCustomColumns = map[string]func(interface{}) string{
	"DataBytes": func(obj interface{}) string {
		if unit, ok := obj.(*goclientnew.ExtendedUnit); ok {
			return fmt.Sprintf("%d", unit.Unit.DataSize)
		}
		if unit, ok := obj.(*goclientnew.Unit); ok {
			return fmt.Sprintf("%d", unit.DataSize)
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
	"UnreleasedChanges": unreleasedChangesColumn,
	// UnappliedChanges is the pre-Release name for the same idea, kept so saved
	// Views and scripts that reference it keep working. It computes the new
	// semantics: unreleased, not unapplied. Custom columns are resolved before
	// aliases (see dynamic_columns.go), so this has to be a second entry rather
	// than an entry in unitAliases.
	"UnappliedChanges": unreleasedChangesColumn,
}

// unreleasedChangesColumn reports whether a Unit has changes that have not been
// published in a Release. LastReleasedRevisionNum is advanced by `release publish`, which is the
// apply point now. Only Units with a Target are released, so the others never report changes.
func unreleasedChangesColumn(obj interface{}) string {
	if extendedUnit, ok := obj.(*goclientnew.ExtendedUnit); ok {
		unit := extendedUnit.Unit
		if unit.HeadRevisionNum > unit.LastReleasedRevisionNum && (unit.TargetID != nil && *unit.TargetID != uuid.Nil) {
			return "Yes"
		}
	}
	return ""
}

// Fields required by custom columns
var unitCustomColumnDependencies = map[string][]string{
	"DataBytes":         {"Data"},
	"UpgradeNeeded":     {"UpstreamRevisionNum", "UpstreamUnit.HeadRevisionNum"},
	"UnreleasedChanges": {"HeadRevisionNum", "LastReleasedRevisionNum", "TargetID"},
	"UnappliedChanges":  {"HeadRevisionNum", "LastReleasedRevisionNum", "TargetID"},
}

func init() {
	addStandardListFlags(unitListCmd)
	enableListPagingFlags(unitListCmd)
	unitListCmd.Flags().StringVar(&resourceType, "resource-type", "", "resource-type filter")
	unitListCmd.Flags().StringVar(&whereData, "where-data", "", "where data filter")
	unitListCmd.Flags().StringVar(&whereTrigger, "where-trigger", "", "where expression to match triggers for validation filtering")
	unitListCmd.Flags().StringVar(&triggerFilter, "trigger-filter", "", "Filter UUID (with From=Trigger) for trigger validation filtering")
	unitListCmd.Flags().BoolVar(&triggersPassed, "triggers-passed", false, "return units passing trigger validation (default: return failing units)")
	enableViewFlag(unitListCmd)
	unitCmd.AddCommand(unitListCmd)
}

func unitListCmdRun(cmd *cobra.Command, args []string) error {
	var err error
	viewID := listViewFor("cub unit list")

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
		extendedUnits, err = apiListAllUnits(cubapi.NewWhere(where), resourceType, whereData, whereTrigger, triggerFilter, triggersPassed, selectFields, filterID, viewID)
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
	DisplayListGeneric(units, effectiveColumns(), defaultUnitColumns, unitAliases, unitCustomColumns)
}

func apiListUnits(spaceID string, whereFilter string, selectParam string) ([]*goclientnew.Unit, error) {
	// A spaceID of "*" means search across all spaces. apiListExtendedUnits
	// parses spaceID as a UUID, so route the org-wide case to apiListAllUnits
	// instead (mirroring unitListCmdRun's "*" handling) rather than panicking
	// in uuid.MustParse.
	var extendedUnits []*goclientnew.ExtendedUnit
	var err error
	if spaceID == "*" {
		extendedUnits, err = apiListAllUnits(cubapi.NewWhere(whereFilter), "", "", "", "", false, selectParam, "", "")
	} else {
		extendedUnits, err = apiListExtendedUnits(spaceID, whereFilter, "", "", "", "", false, selectParam, "", "")
	}
	if err != nil {
		return nil, err
	}

	units := make([]*goclientnew.Unit, 0, len(extendedUnits))
	for _, extendedUnit := range extendedUnits {
		units = append(units, extendedUnit.Unit)
	}
	return units, nil
}

// apiListExtendedUnits lists units in a single space by scoping the org-level
// query with a SpaceID clause and delegating to apiListAllUnits.
func apiListExtendedUnits(spaceID string, whereFilter string, resourceType string, whereData string, whereTrigger string, triggerFilter string, triggersPassed bool, selectParam string, filterParam string, viewParam string) ([]*goclientnew.ExtendedUnit, error) {
	where := cubapi.NewWhere(whereFilter).SpaceID(goclientnew.UUID(uuid.MustParse(spaceID)))
	return apiListAllUnits(where, resourceType, whereData, whereTrigger, triggerFilter, triggersPassed, selectParam, filterParam, viewParam)
}

func apiListAllUnits(where cubapi.Where, resourceType string, whereData string, whereTrigger string, triggerFilter string, triggersPassed bool, selectParam string, filterParam string, viewParam string) ([]*goclientnew.ExtendedUnit, error) {
	selectValue := handleSelectParameter(selectParam, selectFields, func() string {
		// UnitEventID is an Include-only pseudo-field, not selectable.
		selectInclude := strings.TrimPrefix(unitListInclude, "UnitEventID,")
		return buildSelectList("Unit", effectiveColumns(), selectInclude, defaultUnitColumns, unitAliases, unitCustomColumnDependencies, unitBaseSelectFields)
	})
	return cubapi.ListUnits(ctx, cubClient, where, cubapi.ListOpts{
		Limit:         listLimit,
		OrderBy:       listOrderBy,
		Select:        cubapi.SelectFields(selectValue),
		Include:       unitListInclude,
		Filter:        filterParam,
		Contains:      contains,
		IncludeHidden: includeHidden,
		View:          viewParam,
	}, func(p *goclientnew.ListAllUnitsParams) {
		if resourceType != "" {
			p.ResourceType = &resourceType
		}
		if whereData != "" {
			p.WhereData = &whereData
		}
		if whereTrigger != "" {
			p.WhereTrigger = &whereTrigger
		}
		if triggerFilter != "" {
			p.TriggerFilter = &triggerFilter
		}
		if triggersPassed {
			p.TriggersPassed = &triggersPassed
		}
	})
}
