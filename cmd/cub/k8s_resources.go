// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"encoding/base64"
	"fmt"
	"sort"
	"strings"

	"github.com/confighub/sdk/configkit/k8skit"
	"github.com/confighub/sdk/core/cubapi"
	"github.com/confighub/sdk/core/function/api"
	goclientnew "github.com/confighub/sdk/core/openapi/goclient-new"
	"github.com/confighub/sdk/core/workerapi"
	"github.com/google/uuid"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

// Shared query machinery for `cub k8s get` and `cub k8s types`. Both read the
// Kubernetes resources inside Units from the Resource entity, which mirrors the
// configuration in each Unit's data and is queried in SQL.
//
// Every scope the commands document -- the resource type, the namespace, the
// names, --where-resource -- becomes a predicate the database evaluates, so a
// fleet-wide sweep no longer invokes a function over every Unit. The exception
// is --where, which selects Units rather than resources: those are resolved to
// their IDs first and the resource query narrowed to them.

var (
	k8sQueryTargets   []string
	k8sQueryNamespace string
)

// k8sResource is one Kubernetes resource found in a Unit, joined with the
// ConfigHub metadata that says where it lives.
type k8sResource struct {
	Space        string
	Unit         string
	Target       string `json:",omitempty"`
	Namespace    string `json:",omitempty"`
	Name         string
	Kind         string
	APIVersion   string
	ResourceType string
	ResourceName string
	SpaceID      uuid.UUID
	UnitID       uuid.UUID
	// Resource is the resource's configuration, present when the body was
	// requested (--show detail or --show data).
	Resource map[string]any `json:",omitempty"`
	// body is the resource's YAML as stored, preserving comments and field
	// order that a parse-and-reserialize round trip would lose.
	body string
}

// k8sUnit is the Unit metadata joined onto resources for the wider views.
type k8sUnit struct {
	unit   *goclientnew.Unit
	space  string
	target string
}

// addK8sQueryFlags registers the flags shared by the `cub k8s` read commands.
func addK8sQueryFlags(cmd *cobra.Command) {
	addSpaceFlags(cmd)
	enableWhereFlag(cmd)
	enableFilterFlag(cmd)
	cmd.Flags().StringSliceVar(&k8sQueryTargets, "target", nil,
		"only Units bound to these targets, as space-slug/target-slug (can be repeated or comma-separated); implies --space '*' unless --space is given")
	cmd.Flags().StringVarP(&k8sQueryNamespace, "namespace", "n", "",
		"only resources in this Kubernetes namespace")
	cmd.Flags().StringVar(&whereResource, "where-resource", "",
		"additional filter on individual resources, ANDed with the resource type; e.g. \"spec.replicas > 1\"")
	// Not addStandardListDisplayFlags: --columns and -o custom-columns select
	// among an entity's fields, and these commands list resources rather than
	// ConfigHub entities.
	enableNamesFlag(cmd)
	enableNoheaderFlag(cmd)
	addStandardDisplayFlags(cmd)
}

// checkK8sOutputFormat rejects the output formats that shape ConfigHub entities
// rather than resources, so that asking for one fails instead of silently
// falling back to the default table.
func checkK8sOutputFormat() error {
	switch effectiveOutput().Kind {
	case OutputCustomColumns:
		return fmt.Errorf("--output=custom-columns is not supported by cub k8s; use -o json with -o jq=<expr>")
	case OutputMutations:
		return fmt.Errorf("--output=mutations is not supported by cub k8s")
	}
	return nil
}

// k8sPreRunE resolves the space the same way other space-scoped commands do,
// except that naming a target searches every space by default: a target's Units
// usually live in Spaces other than the target's own.
func k8sPreRunE(cmd *cobra.Command, args []string) error {
	if len(k8sQueryTargets) > 0 && !cmd.Flags().Changed("space") {
		spaceFlag = "*"
	}
	return spacePreRunE(cmd, args)
}

// k8sTargetWhereClause resolves each "space-slug/target-slug" to its UUID and
// renders them as a TargetID IN clause.
func k8sTargetWhereClause() (string, error) {
	targets, err := parseEntityIdentifiersAsEntities(k8sQueryTargets, EntityTypeTarget, "TargetID",
		apiGetTargetFromSlugInSpaceCore,
		func(t *goclientnew.Target) string { return t.TargetID.String() },
	)
	if err != nil {
		return "", err
	}
	ids := make([]string, 0, len(targets))
	for i := range targets {
		ids = append(ids, "'"+targets[i].TargetID.String()+"'")
	}
	return "TargetID IN (" + strings.Join(ids, ", ") + ")", nil
}

// matchesNames reports whether a resource was asked for by name. Empty names
// match everything.
func matchesNames(names []string, resource *k8sResource) bool {
	if len(names) == 0 {
		return true
	}
	for _, name := range names {
		if name == resource.Name || name == resource.ResourceName {
			return true
		}
	}
	return false
}

// listK8sResources returns the Kubernetes resources matching the query.
//
// The resources come from the Resource entity, which mirrors the configuration inside Units and
// is queried in SQL. The previous implementation ran the get-resources function over every
// selected Unit and filtered the results client-side; the resource type, the namespace, the
// names, and --where-resource are now predicates the database evaluates, so a fleet-wide sweep
// no longer parses every Unit's data.
//
// withBody requests each resource's original configuration. It is off for the list view, where
// only names and types are shown, because the bodies are bulk.
func listK8sResources(types *resourceTypeFilter, names []string, withBody bool) ([]*k8sResource, error) {
	where, err := k8sResourceWhere(types, names)
	if err != nil {
		return nil, err
	}
	filterID, err := parseFilterFlag(filter)
	if err != nil {
		return nil, err
	}

	// No include: the Space and Unit slugs are columns on the row, populated by a join, so
	// expanding those entities would fetch a Unit's configuration data for nothing. The Target
	// slug is only needed by the wide and detail views, which load it with the Units.
	extended, err := cubapi.ListResources(ctx, cubClient, where, cubapi.ListOpts{
		Filter: filterID,
	}, func(params *goclientnew.ListAllResourcesParams) {
		if withBody {
			params.RawData = &withBody
		}
	})
	if err != nil {
		return nil, err
	}

	// Non-nil even when empty, so the serialized formats emit an empty list rather than null.
	resources := []*k8sResource{}
	for _, er := range extended {
		if er.Resource == nil {
			continue
		}
		resource, err := newK8sResourceFromEntity(er)
		if err != nil {
			return nil, err
		}
		// The SQL clauses are deliberately broader than the request where they cannot express
		// it exactly -- a Kind that resolves across any API group, most of all -- so match again.
		if types != nil && !types.matches(resource.ResourceType) {
			continue
		}
		if !matchesNames(names, resource) {
			continue
		}
		if k8sQueryNamespace != "" && resource.Namespace != k8sQueryNamespace {
			continue
		}
		resources = append(resources, resource)
	}
	sortK8sResources(resources)
	return resources, nil
}

// k8sResourceWhere builds the Resource-level filter. Every scope the command documents maps onto
// a column except --where, which selects Units and is resolved to their IDs first.
func k8sResourceWhere(types *resourceTypeFilter, names []string) (cubapi.Where, error) {
	clauses := cubapi.NewWhere(fmt.Sprintf("ToolchainType = '%s'", workerapi.ToolchainKubernetesYAML))

	if selectedSpaceID != "*" {
		spaceID, err := uuid.Parse(selectedSpaceID)
		if err != nil {
			return clauses, fmt.Errorf("invalid space: %w", err)
		}
		clauses = clauses.SpaceID(goclientnew.UUID(spaceID))
	}

	if len(k8sQueryTargets) > 0 {
		targetClause, err := k8sTargetWhereClause()
		if err != nil {
			return clauses, err
		}
		clauses = clauses.And(targetClause)
	}

	// --where goes to the server as written. Attributes of the containing Unit, Space, and
	// Target are addressable with a prefix -- Unit.Slug, Space.Labels.Environment -- because
	// ExtendedResource carries those entities and the filter expands them on demand.
	if where != "" {
		clauses = clauses.And(where)
	}

	if types != nil {
		if expr := types.resourceWhereExpression(); expr != "" {
			clauses = clauses.And(expr)
		}
	}
	if k8sQueryNamespace != "" {
		clauses = clauses.And("ResourceName LIKE '" + escapeSQLLiteral(k8sQueryNamespace) + "/%'")
	}
	if len(names) > 0 {
		// Accept a bare name or a namespace-qualified one: ResourceName is "<namespace>/<name>",
		// or "/<name>" for cluster-scoped resources.
		quoted := make([]string, 0, len(names))
		for _, name := range names {
			quoted = append(quoted, escapeSQLLiteral(name))
		}
		clauses = clauses.And("ResourceName ~ '(^|/)(" + strings.Join(quoted, "|") + ")$'")
	}
	if whereResource != "" {
		translated, err := whereResourceToResourceClause(whereResource)
		if err != nil {
			return clauses, err
		}
		clauses = clauses.And(translated)
	}
	return clauses, nil
}

// whereResourceToResourceClause rewrites a --where-resource expression against the Resource
// entity. A ConfigHub.<field> path names a column; anything else is a path into the resource's
// configuration and becomes a Data. path, which the entity filter translates to a SQL/JSON path.
func whereResourceToResourceClause(expression string) (string, error) {
	parsed, err := api.ParseAndValidateWhereResource(expression)
	if err != nil {
		return "", err
	}
	clauses := make([]string, 0, len(parsed))
	for _, expr := range parsed {
		path := expr.Path
		if expr.IsSplitPath {
			path = expr.VisitorPath + ".|" + expr.SubPath
		}
		if strings.HasPrefix(path, "ConfigHub.") {
			column := strings.TrimPrefix(path, "ConfigHub.")
			switch column {
			case "ResourceName", "ResourceType":
				path = column
			default:
				// ResourceCategory, ResourceNameWithoutScope and ResourceNameStableCore are
				// computed during the walk and are not stored, so they have no column to
				// filter on.
				return "", fmt.Errorf("--where-resource path %s is not supported by cub k8s get; "+
					"use ConfigHub.ResourceName or ConfigHub.ResourceType", path)
			}
		} else {
			path = "Data." + path
		}
		clauses = append(clauses, path+" "+expr.Operator+" "+expr.Literal)
	}
	return strings.Join(clauses, " AND "), nil
}

// escapeSQLLiteral doubles single quotes so a value can be embedded in the filter expression.
// The filter parser rejects a literal containing a quote outright, so this turns a name that
// would be refused into one that matches nothing, rather than a parse error.
func escapeSQLLiteral(value string) string {
	return strings.ReplaceAll(value, "'", "''")
}

// newK8sResourceFromEntity builds the command's view of a resource from a Resource row.
func newK8sResourceFromEntity(er *goclientnew.ExtendedResource) (*k8sResource, error) {
	resource := er.Resource
	// RawData is declared as a byte-format string in the spec, so the generated client hands it
	// back base64-encoded.
	rawBody := ""
	if er.RawData != "" {
		decoded, err := base64.StdEncoding.DecodeString(er.RawData)
		if err != nil {
			return nil, fmt.Errorf("failed to decode the configuration of %s %s: %w",
				resource.ResourceType, resource.ResourceName, err)
		}
		rawBody = string(decoded)
	}
	apiVersion, kind := apiVersionAndKind(resource.ResourceType)
	resourceName := api.ResourceName(resource.ResourceName)
	name := k8skit.GetNameFromResourceName(resourceName)
	if name == "" {
		name = resource.ResourceName
	}
	result := &k8sResource{
		Namespace:    k8skit.GetNamespaceFromResourceName(resourceName),
		Name:         name,
		Kind:         kind,
		APIVersion:   apiVersion,
		ResourceType: resource.ResourceType,
		ResourceName: resource.ResourceName,
		SpaceID:      uuid.UUID(resource.SpaceID),
		UnitID:       uuid.UUID(resource.UnitID),
		body:         rawBody,
	}
	result.Space = resource.SpaceSlug
	result.Unit = resource.UnitSlug
	if rawBody != "" {
		if err := yaml.Unmarshal([]byte(rawBody), &result.Resource); err != nil {
			return nil, fmt.Errorf("failed to parse %s %s in unit %s: %w",
				resource.ResourceType, resource.ResourceName, result.Unit, err)
		}
	}
	return result, nil
}

// apiVersionAndKind splits a ConfigHub ResourceType back into the apiVersion
// and kind a Kubernetes manifest carries.
func apiVersionAndKind(resourceType string) (apiVersion, kind string) {
	index := strings.LastIndex(resourceType, "/")
	if index < 0 {
		return "", resourceType
	}
	return resourceType[:index], resourceType[index+1:]
}

func sortK8sResources(resources []*k8sResource) {
	sort.SliceStable(resources, func(i, j int) bool {
		a, b := resources[i], resources[j]
		if a.Space != b.Space {
			return a.Space < b.Space
		}
		if a.Unit != b.Unit {
			return a.Unit < b.Unit
		}
		if a.ResourceType != b.ResourceType {
			return a.ResourceType < b.ResourceType
		}
		return a.ResourceName < b.ResourceName
	})
}

// loadK8sUnits fetches the Units behind the given resources, keyed by UnitID, so views that
// show more than the Space and Unit slug -- the Target, revision numbers, labels -- have it
// available. It selects by the UnitIDs already in hand rather than re-running the query's
// filter, which is expressed against resources and their related entities, not against Units.
func loadK8sUnits(resources []*k8sResource) (map[uuid.UUID]*k8sUnit, error) {
	unitIDs := make([]goclientnew.UUID, 0, len(resources))
	seen := make(map[uuid.UUID]bool, len(resources))
	for _, resource := range resources {
		if resource.UnitID != uuid.Nil && !seen[resource.UnitID] {
			seen[resource.UnitID] = true
			unitIDs = append(unitIDs, goclientnew.UUID(resource.UnitID))
		}
	}
	if len(unitIDs) == 0 {
		return map[uuid.UUID]*k8sUnit{}, nil
	}

	extendedUnits, err := apiListAllUnits(cubapi.Where{}.In("UnitID", unitIDs), "", "", "", "", false,
		k8sUnitSelectFields, "", "")
	if err != nil {
		return nil, err
	}
	// Targets are addressed as space-slug/target-slug, and a Target usually
	// lives in a Space other than the Units bound to it, so the Target's own
	// Space has to be resolved rather than assumed to be the Unit's.
	spaceSlugs, err := k8sSpaceSlugs()
	if err != nil {
		return nil, err
	}

	units := make(map[uuid.UUID]*k8sUnit, len(extendedUnits))
	for _, extendedUnit := range extendedUnits {
		entry := &k8sUnit{unit: extendedUnit.Unit}
		if extendedUnit.Space != nil {
			entry.space = extendedUnit.Space.Slug
		}
		if extendedUnit.Target != nil {
			entry.target = prefixedSlug(spaceSlugs[uuid.UUID(extendedUnit.Target.SpaceID)], extendedUnit.Target.Slug)
		}
		units[uuid.UUID(extendedUnit.Unit.UnitID)] = entry
	}
	return units, nil
}

// k8sSpaceSlugs maps Space IDs to slugs, for rendering the space-qualified
// names of entities that come back with only an ID.
func k8sSpaceSlugs() (map[uuid.UUID]string, error) {
	spaces, err := apiListSpaces("", "SpaceID,Slug")
	if err != nil {
		return nil, err
	}
	slugs := make(map[uuid.UUID]string, len(spaces))
	for _, space := range spaces {
		slugs[uuid.UUID(space.SpaceID)] = space.Slug
	}
	return slugs, nil
}

// k8sUnitSelectFields are the Unit fields the wide and detail views show.
const k8sUnitSelectFields = "UnitID,SpaceID,Slug,DisplayName,TargetID,Labels,ApplyGates," +
	"HeadRevisionNum,LastAppliedRevisionNum,LiveRevisionNum,UpstreamRevisionNum," +
	"LastChangeDescription,UpdatedAt"
