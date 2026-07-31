// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"encoding/base64"
	"encoding/json"
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
// Kubernetes resources inside Units by invoking the get-resources function
// across the selected Units, then filter and shape the result.
//
// Resource selection happens in the function's WhereResource clause rather than
// through the list API's --resource-type / --where-data parameters, because
// those run a second function (where-filter) over every Unit to decide which
// Units to visit. One get-resources pass with a WhereResource clause does the
// same work once, and Units whose resources all get filtered out simply produce
// no output.

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

// k8sUnitWhere builds the Unit-level filter: the user's --where, the resolved
// --target list, and a ToolchainType constraint, since these commands read
// Kubernetes configuration only.
func k8sUnitWhere() (string, error) {
	clauses := make([]string, 0, 3)
	if where != "" {
		clauses = append(clauses, where)
	}
	if len(k8sQueryTargets) > 0 {
		targetClause, err := k8sTargetWhereClause()
		if err != nil {
			return "", err
		}
		clauses = append(clauses, targetClause)
	}
	clauses = append(clauses, fmt.Sprintf("ToolchainType = '%s'", workerapi.ToolchainKubernetesYAML))
	return strings.Join(clauses, " AND "), nil
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

// k8sWhereResource composes the WhereResource clause sent to the function: the
// resource types, the namespace, the requested names, and any user-supplied
// --where-resource. All are ANDed, which is the only conjunction the
// WhereResource grammar supports.
func k8sWhereResource(types *resourceTypeFilter, names []string) string {
	var clauses []string
	if types != nil {
		if expr := types.whereResourceExpression(); expr != "" {
			clauses = append(clauses, expr)
		}
	}
	if k8sQueryNamespace != "" {
		clauses = append(clauses, "ConfigHub.ResourceName ~ '^"+k8sQueryNamespace+"/'")
	}
	if len(names) > 0 {
		// Accept a bare name or a namespace-qualified one: ResourceName is
		// "<namespace>/<name>", or "/<name>" for cluster-scoped resources.
		clauses = append(clauses, "ConfigHub.ResourceName ~ '(^|/)("+strings.Join(names, "|")+")$'")
	}
	if whereResource != "" {
		clauses = append(clauses, whereResource)
	}
	return strings.Join(clauses, " AND ")
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

// listK8sResources runs get-resources across the selected Units and returns the
// matching resources. withBody requests each resource's YAML; without it only
// names and types come back, which is far cheaper for wide sweeps.
func listK8sResources(types *resourceTypeFilter, names []string, withBody bool) ([]*k8sResource, error) {
	unitWhere, err := k8sUnitWhere()
	if err != nil {
		return nil, err
	}
	filterID, err := parseFilterFlag(filter)
	if err != nil {
		return nil, err
	}

	// "native" rather than "yaml": ConfigHub stores YAML comments as $comment$
	// map keys, and the native conversion turns them back into real comments.
	// Without it the bodies would carry keys that aren't part of the resource.
	bodyFormat := "none"
	if withBody {
		bodyFormat = "native"
	}
	req := newGetResourcesRequest(k8sWhereResource(types, names), bodyFormat)
	responses, err := invokeFunctionsOnUnits(&invokeArgs{
		Where:    unitWhere,
		FilterID: filterID,
		Body:     req,
	})
	if err != nil {
		return nil, err
	}
	// Non-nil even when empty, so the serialized formats emit an empty list
	// rather than null.
	resources := []*k8sResource{}
	if responses == nil {
		return resources, nil
	}

	for i := range *responses {
		response := &(*responses)[i]
		if !response.Success {
			// A Unit whose data can't be parsed shouldn't abort the sweep, but
			// silently dropping it would misreport the fleet.
			if !quiet {
				message := "unknown error"
				if response.Error != nil {
					message = response.Error.Message
				}
				tprintErr("Skipping unit %s: %s", unitDisplayName(response), message)
			}
			continue
		}
		list, err := decodeResourceList(response)
		if err != nil {
			return nil, err
		}
		for j := range list {
			resource, err := newK8sResource(response, &list[j])
			if err != nil {
				return nil, err
			}
			// The server-side WhereResource clause is deliberately broader than
			// the request in the cases it can't express exactly, so match again.
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
	}
	sortK8sResources(resources)
	return resources, nil
}

func newGetResourcesRequest(whereResourceExpr, bodyFormat string) *goclientnew.FunctionInvocationsRequest {
	req := &goclientnew.FunctionInvocationsRequest{
		ToolchainType: string(workerapi.ToolchainKubernetesYAML),
		WhereResource: whereResourceExpr,
		NumFilters:    0,
		StopOnError:   false,
	}
	invocation := initializeFunctionInvocation("get-resources", []string{bodyFormat})
	req.FunctionInvocations = &[]goclientnew.FunctionInvocation{*invocation}
	return req
}

// decodeResourceList pulls the ResourceList payload out of a function response.
// Units with no matching resource return a null payload rather than an entry.
func decodeResourceList(response *goclientnew.FunctionInvocationsResponse) (api.ResourceList, error) {
	encoded, found := response.Outputs[string(api.OutputTypeResourceList)]
	if !found || encoded == "" {
		return nil, nil
	}
	decoded, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return nil, fmt.Errorf("failed to decode output of unit %s: %w", unitDisplayName(response), err)
	}
	var list api.ResourceList
	if err := json.Unmarshal(decoded, &list); err != nil {
		return nil, fmt.Errorf("failed to parse resources of unit %s: %w", unitDisplayName(response), err)
	}
	return list, nil
}

func newK8sResource(response *goclientnew.FunctionInvocationsResponse, resource *api.Resource) (*k8sResource, error) {
	apiVersion, kind := apiVersionAndKind(string(resource.ResourceType))
	// Kubernetes ResourceNames are "<namespace>/<name>", or "/<name>" when
	// cluster-scoped; anything else has no scope to strip.
	name := k8skit.GetNameFromResourceName(resource.ResourceName)
	if name == "" {
		name = string(resource.ResourceName)
	}
	result := &k8sResource{
		Space:        response.SpaceSlug,
		Unit:         response.UnitSlug,
		Namespace:    k8skit.GetNamespaceFromResourceName(resource.ResourceName),
		Name:         name,
		Kind:         kind,
		APIVersion:   apiVersion,
		ResourceType: string(resource.ResourceType),
		ResourceName: string(resource.ResourceName),
		SpaceID:      uuid.UUID(response.SpaceID),
		UnitID:       uuid.UUID(response.UnitID),
		body:         resource.ResourceBody,
	}
	if resource.ResourceBody != "" {
		if err := yaml.Unmarshal([]byte(resource.ResourceBody), &result.Resource); err != nil {
			return nil, fmt.Errorf("failed to parse %s %s in unit %s: %w",
				resource.ResourceType, resource.ResourceName, unitDisplayName(response), err)
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

// loadK8sUnits fetches the Unit metadata for the same selection of Units, keyed
// by UnitID, so views that show more than the Space and Unit slug — the Target,
// revision numbers, labels — have it available. Kept separate from
// listK8sResources because the wide and detail views are the only ones that
// need it.
func loadK8sUnits() (map[uuid.UUID]*k8sUnit, error) {
	unitWhere, err := k8sUnitWhere()
	if err != nil {
		return nil, err
	}
	filterID, err := parseFilterFlag(filter)
	if err != nil {
		return nil, err
	}
	extendedUnits, err := apiListAllUnits(cubapi.NewWhere(unitWhere), "", "", "", "", false,
		k8sUnitSelectFields, filterID, "")
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
