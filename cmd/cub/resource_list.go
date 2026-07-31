// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"strings"

	"github.com/google/uuid"
	"github.com/spf13/cobra"

	"github.com/confighub/sdk/core/cubapi"
	goclientnew "github.com/confighub/sdk/core/openapi/goclient-new"
)

var resourceListCmd = &cobra.Command{
	Use:   "list",
	Short: "List resources",
	Long: getCommandHelp(`List the resources contained in your units' configuration data, in a space or
across all spaces.

Resources can be filtered by their metadata (ResourceType, ResourceName, ToolchainType, UnitID,
SpaceID, TargetID) and by their configuration data. Data paths use dot notation, and a literal
dot within a key is escaped as ~1 (for example Data.metadata.labels.app~1kubernetes~1io/name).

TargetID mirrors the containing unit's, so it selects resources by where they will be applied.
Resources of units with no target have no TargetID.

Attributes of the containing unit and space are addressed with a prefix -- Unit.Labels.App,
Unit.Slug, Space.Labels.Environment -- and can be combined with resource and data predicates.
Only the fields a filter names are fetched for those entities, so filtering on a unit label does
not drag the unit's configuration data along.

Data paths use the same syntax as --where-data on units, including array indexes (containers.0),
wildcards (containers.*), associative matching (containers.?name=nginx), and split paths (.|).
A path selecting several values matches when any of them satisfies the comparison. Embedded
accessors (#accessor) and parameter bindings (@key:name) are the exceptions; use
"cub unit list --where-data" for those.

Examples:
`+"```"+`
  # List all resources in a space
  cub resource list --space my-space

  # List every Deployment in the organization
  cub resource list --space "*" --where "ResourceType = 'apps/v1/Deployment'"

  # List everything headed for one target, across all spaces
  cub resource list --space "*" --where "TargetID = '<target-uuid>'"

  # Find resources that are not bound to any target yet
  cub resource list --space "*" --where "TargetID IS NULL"

  # Filter by a label on the containing unit or its space
  cub resource list --space "*" --where "Unit.Labels.App = 'checkout'"
  cub resource list --space "*" --where "Space.Labels.Environment = 'prod' AND ResourceType = 'v1/Service'"

  # Find Deployments running more than one replica
  cub resource list --space "*" --where "ResourceType = 'apps/v1/Deployment' AND Data.spec.replicas > 1"

  # Find a named container's image, wherever it sits in the containers array
  cub resource list --space "*" --where "Data.spec.template.spec.containers.?name=nginx.image LIKE 'nginx:1.%'"

  # Any container running an image from a given registry
  cub resource list --space "*" --where "Data.spec.template.spec.containers.*.image LIKE 'ghcr.io/%'"

  # Find resources by name
  cub resource list --space "*" --where "ResourceName LIKE '%/checkout%'"

  # Find resources whose data has a top-level key
  cub resource list --space my-space --where "Data ? 'spec'"

  # List resources without headers for scripting
  cub resource list --space my-space --no-headers

  # Include the configuration data, which is omitted from the default output
  cub resource list --space my-space --select "ResourceType,ResourceName,Data" -o json
`+"```"+`
`, ""),
	Args:        cobra.ExactArgs(0),
	RunE:        resourceListCmdRun,
	Annotations: map[string]string{"OrgLevel": ""},
}

// Default columns to display when no custom columns are specified. Data is deliberately absent:
// a resource body is far too large for a table, and leaving it out of the default select list
// keeps it off the wire too. Ask for it with --select.
var defaultResourceColumns = []string{"Resource.SpaceSlug", "Resource.UnitSlug", "Target.Slug", "Resource.ResourceType", "Resource.ResourceName"}

// resourceListInclude is the Include parameter for resource list queries. The Space and Unit are
// deliberately absent: their slugs are columns on the row, populated by a join, so expanding
// those entities would fetch a Unit's configuration data only to read its name.
const resourceListInclude = "TargetID"

// resourceBaseSelectFields are the fields always returned by resource list queries.
var resourceBaseSelectFields = []string{"ResourceID", "UnitID", "SpaceID", "OrganizationID", "SpaceSlug", "UnitSlug"}

// Resource-specific aliases
var resourceAliases = map[string]string{
	"Name":   "Resource.ResourceName",
	"Space":  "Resource.SpaceSlug",
	"Unit":   "Resource.UnitSlug",
	"Type":   "Resource.ResourceType",
	"ID":     "Resource.ResourceID",
	"Target": "Target.Slug",
}

// Resource custom column dependencies
var resourceCustomColumnDependencies = map[string][]string{}

// resourceListRawData requests each resource's original configuration.
var resourceListRawData bool

// resourceViewSlug names a View whose columns shape the output.
var resourceViewSlug string

func init() {
	addStandardListFlags(resourceListCmd)
	resourceListCmd.Flags().BoolVar(&resourceListRawData, "raw-data", false,
		"include each resource's configuration in its original form (YAML for Kubernetes), as RawData in --json output")
	resourceListCmd.Flags().StringVar(&resourceViewSlug, "view", "",
		"view slug or UUID whose columns to display; DataPath columns are read from the stored configuration")
	resourceCmd.AddCommand(resourceListCmd)
}

func resourceListCmdRun(cmd *cobra.Command, args []string) error {
	// Resolve the view slug before any space promotion, since the view may live in a space that
	// the slug has to be resolved against.
	viewID := ""
	if resourceViewSlug != "" {
		viewUUID, viewErr := parseEntityIdentifierSingle(resourceViewSlug, EntityTypeView,
			apiGetViewFromSlugInSpace,
			func(v *goclientnew.View) string { return v.ViewID.String() },
		)
		if viewErr != nil {
			return viewErr
		}
		viewID = viewUUID.String()
	}

	filterID, err := parseFilterFlag(filter)
	if err != nil {
		return err
	}

	extendedResources, err := apiListResources(selectedSpaceID, where, selectFields, filterID, viewID)
	if err != nil {
		return err
	}

	displayListResults(extendedResources, getResourceSlug, displayResourceList)
	return nil
}

// getResourceSlug identifies a resource for `-o name`. Resources have no slug of their own, so
// they are named by the unit that contains them plus their type and name, which is exactly what
// makes one unique.
func getResourceSlug(r *goclientnew.ExtendedResource) string {
	unit := r.Resource.UnitSlug
	if unit == "" {
		unit = r.Resource.UnitID.String()
	}
	return prefixedSlug(r.Resource.SpaceSlug, unit) + ":" + r.Resource.ResourceType + "/" + r.Resource.ResourceName
}

func displayResourceList(resources []*goclientnew.ExtendedResource) {
	// With a view active, its columns replace the default ones.
	if resourceViewSlug != "" && len(resources) > 0 && resources[0].View != nil && len(resources[0].View.Columns) > 0 {
		displayResourceViewColumns(resources)
		return
	}

	table := tableView()
	if !noheader {
		table.SetHeader([]string{"Space", "Unit", "Target", "Type", "Name"})
	}
	for _, r := range resources {
		resource := r.Resource

		spaceSlug := resource.SpaceSlug
		if spaceSlug == "" {
			spaceSlug = resource.SpaceID.String()
		}
		unitSlug := resource.UnitSlug
		if unitSlug == "" {
			unitSlug = resource.UnitID.String()
		}

		// A unit need not have a target, so an empty cell here is normal, not missing data.
		targetSlug := ""
		if r.Target != nil {
			targetSlug = r.Target.Slug
		} else if resource.TargetID != nil {
			targetSlug = resource.TargetID.String()
		}

		table.Append([]string{
			spaceSlug,
			unitSlug,
			targetSlug,
			resource.ResourceType,
			resource.ResourceName,
		})
	}
	table.Render()
}

// displayResourceViewColumns renders the table from the View's column definitions. A path that
// selected several values produced several entries sharing a name, which are joined so that one
// resource stays one row.
func displayResourceViewColumns(resources []*goclientnew.ExtendedResource) {
	view := resources[0].View
	table := tableView()
	if !noheader {
		headers := make([]string, 0, len(view.Columns))
		for _, col := range view.Columns {
			headers = append(headers, col.Name)
		}
		table.SetHeader(headers)
	}
	for _, r := range resources {
		values := make(map[string][]string, len(view.Columns))
		for _, vc := range r.ViewColumns {
			if vc.Value != "" {
				values[vc.Name] = append(values[vc.Name], vc.Value)
			}
		}
		row := make([]string, 0, len(view.Columns))
		for _, col := range view.Columns {
			row = append(row, strings.Join(values[col.Name], ", "))
		}
		table.Append(row)
	}
	table.Render()
}

// apiListResources lists resources via the org-level endpoint, scoped to a single space by a
// SpaceID clause unless spaceID is "*" (list across all spaces).
func apiListResources(spaceID string, whereFilter string, selectParam string, filterParam string, viewID string) ([]*goclientnew.ExtendedResource, error) {
	where := cubapi.NewWhere(whereFilter)
	if spaceID != "*" {
		where = where.SpaceID(goclientnew.UUID(uuid.MustParse(spaceID)))
	}
	selectValue := handleSelectParameter(selectParam, selectFields, func() string {
		return buildSelectList("Resource", nil, resourceListInclude, defaultResourceColumns, resourceAliases, resourceCustomColumnDependencies, resourceBaseSelectFields)
	})
	return cubapi.ListResources(ctx, cubClient, where, cubapi.ListOpts{
		Select:   cubapi.SelectFields(selectValue),
		Include:  resourceListInclude,
		Filter:   filterParam,
		Contains: contains,
	}, func(params *goclientnew.ListAllResourcesParams) {
		if resourceListRawData {
			params.RawData = &resourceListRawData
		}
		if viewID != "" {
			params.View = &viewID
		}
	})
}
