// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"fmt"
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

TargetID mirrors the containing unit's, so it selects resources by the Target they are released to.
Resources of units with no target have no TargetID.

By default the resources listed are those of each unit's head revision. --revision
LastReleasedRevisionNum lists those of the revision a release last published instead (for a
backing unit, the one its entity last took): what is released, rather than what will be. A unit
never released has none.

Attributes of the containing unit and space are addressed with a prefix -- Unit.Slug,
Space.ComponentID, Space.Labels.Environment -- and can be combined with resource and data
predicates. Only the fields a filter names are fetched for those entities, so filtering on the
unit's slug does not drag the unit's configuration data along.

--selected-by and --selecting match label selectors as Kubernetes does, in the named resource's
namespace (default when it has none) and on its cluster: among the resources released to its
Target, or in its Space when it has none. --selected-by names a NetworkPolicy, PodDisruptionBudget
or Service and lists the workloads whose pods its selector selects; --selecting names a workload
(such as a Deployment, StatefulSet, DaemonSet, Job or CronJob: any kind with a pod template) and
lists the NetworkPolicies, PodDisruptionBudgets and Services whose selectors select its pods.
Either takes a ResourceID, or [<space>/]<unit>:<type>/<name> as -o name prints it, which names a
resource of the revision --revision lists.

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
  cub resource list --where "ResourceType = 'apps/v1/Deployment'"

  # List everything headed for one target, across all spaces
  cub resource list --where "TargetID = '<target-uuid>'"

  # Find resources that are not bound to any target yet
  cub resource list --where "TargetID IS NULL"

  # Filter by the resource's own Kubernetes labels, or by its space's labels
  cub resource list --where "Data.metadata.labels.app = 'checkout'"
  cub resource list --where "Space.Labels.Environment = 'prod' AND ResourceType = 'v1/Service'"

  # Filter by its space's Component
  COMPONENT_ID=$(cub component get checkout -o jq=.Component.ComponentID)
  cub resource list --where "Space.ComponentID = '$COMPONENT_ID'"

  # Find Deployments running more than one replica
  cub resource list --where "ResourceType = 'apps/v1/Deployment' AND Data.spec.replicas > 1"

  # Find a named container's image, wherever it sits in the containers array
  cub resource list --where "Data.spec.template.spec.containers.?name=nginx.image LIKE 'nginx:1.%'"

  # Any container running an image from a given registry
  cub resource list --where "Data.spec.template.spec.containers.*.image LIKE 'ghcr.io/%'"

  # Find resources by name
  cub resource list --where "ResourceName LIKE '%/checkout%'"

  # Find resources whose data has a top-level key
  cub resource list --space my-space --where "Data ? 'spec'"

  # List the NetworkPolicies, PodDisruptionBudgets and Services whose selectors select a workload's pods,
  # named as -o name prints it, or by its ResourceID
  cub resource list --selecting my-space/web:apps/v1/Deployment/shop/web

  # List the workloads whose pods a NetworkPolicy selects
  cub resource list --selected-by my-space/netpols:networking.k8s.io/v1/NetworkPolicy/shop/web-ingress

  # List the Deployments released to a target, rather than those of the units' heads
  cub resource list --revision LastReleasedRevisionNum --where "TargetID = '<target-uuid>' AND ResourceType = 'apps/v1/Deployment'"

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

// resourceListRevision names the revision of each unit whose resources are listed.
var resourceListRevision string

// resourceListSelectedBy and resourceListSelecting name the resources a selector match is made
// against, and the IDs they resolve to.
var (
	resourceListSelectedBy, resourceListSelecting     string
	resourceListSelectedByID, resourceListSelectingID *uuid.UUID
)

func init() {
	addStandardListFlags(resourceListCmd)
	enableWherePermissionFlag(resourceListCmd)
	enableListPagingFlags(resourceListCmd)
	resourceListCmd.Flags().BoolVar(&resourceListRawData, "raw-data", false,
		"include each resource's configuration in its original form (YAML for Kubernetes), as RawData in --json output")
	resourceListCmd.Flags().StringVar(&resourceListRevision, "revision", "",
		"list the resources of each unit's HeadRevisionNum (the default) or LastReleasedRevisionNum")
	resourceListCmd.Flags().StringVar(&resourceListSelectedBy, "selected-by", "",
		"list only the workloads whose pods this NetworkPolicy, PodDisruptionBudget or Service selects: a ResourceID, or [<space>/]<unit>:<type>/<name>")
	resourceListCmd.Flags().StringVar(&resourceListSelecting, "selecting", "",
		"list only the NetworkPolicies, PodDisruptionBudgets and Services whose selectors select this workload's pods: a ResourceID, or [<space>/]<unit>:<type>/<name>")
	enableViewFlag(resourceListCmd)
	resourceCmd.AddCommand(resourceListCmd)
}

func resourceListCmdRun(cmd *cobra.Command, args []string) error {
	filterID, err := parseFilterFlag(filter)
	if err != nil {
		return err
	}
	for _, named := range []struct {
		ref string
		id  **uuid.UUID
	}{{resourceListSelectedBy, &resourceListSelectedByID}, {resourceListSelecting, &resourceListSelectingID}} {
		if named.ref == "" {
			continue
		}
		id, err := resolveResourceID(named.ref)
		if err != nil {
			return err
		}
		*named.id = &id
	}

	extendedResources, err := apiListResources(selectedSpaceID, where, selectFields, filterID)
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
	if displayRequestedColumns(resources, resourceAliases, nil) {
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

// apiListResources lists resources via the org-level endpoint, scoped to a single space by a
// SpaceID clause unless spaceID is "*" (list across all spaces).
func apiListResources(spaceID string, whereFilter string, selectParam string, filterParam string) ([]*goclientnew.ExtendedResource, error) {
	where := cubapi.NewWhere(whereFilter)
	if spaceID != "*" {
		where = where.SpaceID(goclientnew.UUID(uuid.MustParse(spaceID)))
	}
	selectValue := handleSelectParameter(selectParam, selectFields, func() string {
		return buildSelectList("Resource", listColumnsFor("cub resource list"), resourceListInclude, defaultResourceColumns, resourceAliases, resourceCustomColumnDependencies, resourceBaseSelectFields)
	})
	return cubapi.ListResources(ctx, cubClient, where, cubapi.ListOpts{
		Limit:           listLimit,
		OrderBy:         listOrderBy,
		Select:          cubapi.SelectFields(selectValue),
		Include:         resourceListInclude,
		Filter:          filterParam,
		Contains:        contains,
		WherePermission: wherePermission,
		IncludeHidden:   includeHidden,
		View:            listViewFor("cub resource list"),
	}, cubapi.WithResourceRevision(resourceListRevision), func(params *goclientnew.ListAllResourcesParams) {
		if resourceListRawData {
			params.RawData = &resourceListRawData
		}
		params.SelectedBy = resourceListSelectedByID
		params.Selecting = resourceListSelectingID
	})
}

// resolveResourceID resolves a ResourceID, or a resource named as getResourceSlug names it:
// [<space>/]<unit>:<type>/<name>, of the revision --revision lists. A type has a slash or two in it, and a name may too, so the two
// are matched together against the Unit's resources rather than split.
func resolveResourceID(ref string) (uuid.UUID, error) {
	if id, err := uuid.Parse(ref); err == nil {
		return id, nil
	}
	unitRef, typeAndName, ok := strings.Cut(ref, ":")
	if !ok || unitRef == "" || typeAndName == "" {
		return uuid.Nil, fmt.Errorf("%q names no resource: give a ResourceID, or [<space>/]<unit>:<type>/<name> as cub resource list -o name prints it", ref)
	}
	unitID, err := resolveUnitID(unitRef)
	if err != nil {
		return uuid.Nil, err
	}
	resources, err := cubapi.ListResources(ctx, cubClient, cubapi.NewWhere("").Eq("UnitID", unitID.String()),
		cubapi.ListOpts{Select: cubapi.SelectFields("ResourceID,ResourceType,ResourceName")},
		cubapi.WithResourceRevision(resourceListRevision))
	if err != nil {
		return uuid.Nil, err
	}
	for _, resource := range resources {
		if resource.Resource.ResourceType+"/"+resource.Resource.ResourceName == typeAndName {
			return resource.Resource.ResourceID, nil
		}
	}
	return uuid.Nil, fmt.Errorf("the Unit %s has no resource %s", unitRef, typeAndName)
}
