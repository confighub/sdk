// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"fmt"
	"sort"
	"strings"

	"github.com/confighub/sdk/core/cubapi"
	goclientnew "github.com/confighub/sdk/core/openapi/goclient-new"
	"github.com/google/uuid"
	"github.com/spf13/cobra"
)

// resourceFlag is --resource on the function verbs: the one resource to operate on, named
// the way a Kubernetes user names one, without the unit that holds it.
var resourceFlag string

// resourceFlagCommands are the commands addResourceFlag gave --resource to, for completion.
var resourceFlagCommands []*cobra.Command

func addResourceFlag(cmd *cobra.Command) {
	cmd.Flags().StringVar(&resourceFlag, "resource", "", "the one resource to operate on, as <kind>/<name> or <kind>/<namespace>/<name>, such as Deployment/backend. The kind is matched in any case and takes kubectl's .group suffix (Ingress.networking.k8s.io) when the kind alone is ambiguous. It has to match exactly one resource among the units --space, --variant and --where select, so a name held in several namespaces needs its namespace")
	resourceFlagCommands = append(resourceFlagCommands, cmd)
}

// resourceRef is a parsed --resource value.
type resourceRef struct {
	Kind      string // matched case-insensitively against the kind in the configuration
	Group     string // API group from a kubectl-style Kind.group; empty means any
	Namespace string // empty means any, which is an error only when it matters
	Name      string
}

// parseResourceRef reads <kind>/<name> or <kind>/<namespace>/<name>.
func parseResourceRef(ref string) (resourceRef, error) {
	parts := strings.Split(ref, "/")
	var r resourceRef
	switch len(parts) {
	case 2:
		r.Kind, r.Name = parts[0], parts[1]
	case 3:
		r.Kind, r.Namespace, r.Name = parts[0], parts[1], parts[2]
	default:
		return r, fmt.Errorf("--resource takes <kind>/<name> or <kind>/<namespace>/<name>, got %q", ref)
	}
	if kind, group, ok := strings.Cut(r.Kind, "."); ok {
		r.Kind, r.Group = kind, group
		if group == "" {
			return r, fmt.Errorf("--resource %q names no API group after the kind's dot", ref)
		}
	}
	if r.Kind == "" || r.Name == "" || (len(parts) == 3 && r.Namespace == "") {
		return r, fmt.Errorf("--resource %q has an empty part; it takes <kind>/<name> or <kind>/<namespace>/<name>", ref)
	}
	// The parts are sent in a filter, and the filter grammar has no way to express these.
	for _, part := range []string{r.Kind, r.Group, r.Namespace, r.Name} {
		if strings.ContainsAny(part, "'\\ ") {
			return r, fmt.Errorf("--resource %q: %q cannot appear in a resource name", ref, part)
		}
	}
	return r, nil
}

// String renders the reference as it was typed, for messages.
func (r resourceRef) String() string {
	kind := r.Kind
	if r.Group != "" {
		kind += "." + r.Group
	}
	if r.Namespace != "" {
		return kind + "/" + r.Namespace + "/" + r.Name
	}
	return kind + "/" + r.Name
}

// splitResourceName splits a ResourceName, <namespace>/<name>, into the two. A
// cluster-scoped resource is stored as /<name>, so its namespace is empty.
func splitResourceName(resourceName string) (namespace, name string) {
	namespace, name, found := strings.Cut(resourceName, "/")
	if !found {
		return "", resourceName
	}
	return namespace, name
}

// matches reports whether a stored resource is the one the reference names.
func (r resourceRef) matches(resourceType, resourceName string) bool {
	group, kind := splitResourceType(resourceType)
	if !strings.EqualFold(kind, r.Kind) {
		return false
	}
	if r.Group != "" && group != r.Group {
		return false
	}
	namespace, name := splitResourceName(resourceName)
	if name != r.Name {
		return false
	}
	return r.Namespace == "" || namespace == r.Namespace
}

// listWhere is the filter that fetches the candidates for the reference. It is a prefilter,
// since a regular expression over the stored strings cannot be anchored to exactly the parts
// matches compares (a dot in a group matches any character, for one); matches decides.
func (r resourceRef) listWhere() cubapi.Where {
	// A ResourceType always ends in /<Kind>, and a ResourceName in /<name>, so a slash
	// before the word anchors each to a whole part.
	var where cubapi.Where
	if r.Group != "" {
		where = cubapi.NewWhere("ResourceType ~* '^" + r.Group + "/.*/" + r.Kind + "$'")
	} else {
		where = cubapi.NewWhere("ResourceType ~* '/" + r.Kind + "$'")
	}
	if r.Namespace != "" {
		return where.Eq("ResourceName", r.Namespace+"/"+r.Name)
	}
	return where.And("ResourceName ~ '/" + r.Name + "$'")
}

// resolvedResource is the one resource --resource named, as the server stores it.
type resolvedResource struct {
	UnitID        goclientnew.UUID
	UnitSlug      string
	SpaceSlug     string
	ResourceType  string // <apiVersion>/<Kind>
	ResourceName  string // <namespace>/<name>, or /<name> when cluster-scoped
	ToolchainType string // of the unit holding it, which decides which functions can read it
}

func (r resolvedResource) String() string {
	return fmt.Sprintf("%s %s in unit %s/%s", r.ResourceType, r.ResourceName, r.SpaceSlug, r.UnitSlug)
}

// whereResource is the --where-resource clause that selects this resource inside its unit:
// the apiVersion and kind that make up its type, its name, and its namespace when it has one.
func (r resolvedResource) whereResource() string {
	// A ResourceType is <apiVersion>/<Kind>, so the apiVersion is what precedes the kind.
	_, kind := splitResourceType(r.ResourceType)
	apiVersion := strings.TrimSuffix(r.ResourceType, "/"+kind)
	namespace, name := splitResourceName(r.ResourceName)
	clause := fmt.Sprintf("apiVersion = '%s' AND kind = '%s' AND metadata.name = '%s'", apiVersion, kind, name)
	if namespace != "" {
		clause += fmt.Sprintf(" AND metadata.namespace = '%s'", namespace)
	}
	return clause
}

// resolveResourceRef finds the one resource the reference names among the units in scope:
// the selected space, or the organization, narrowed by unitWhere when there is one. Zero
// matches is an error rather than a function that ran on nothing, and more than one is
// reported with each match, since picking one would change a resource the user did not name.
func resolveResourceRef(ref resourceRef, unitWhere string) (resolvedResource, error) {
	where := ref.listWhere()
	scope := "the organization"
	if selectedSpaceID != "" && selectedSpaceID != "*" {
		where = where.SpaceID(goclientnew.UUID(uuid.MustParse(selectedSpaceID)))
		scope = "space " + selectedSpaceSlug
		if variantFlag != "" {
			scope = "variant " + variantFlag
		}
	}
	rows, err := cubapi.ListResources(ctx, cubClient, where, cubapi.ListOpts{
		Select:        "ResourceID,UnitID,SpaceID,SpaceSlug,UnitSlug,ResourceType,ResourceName,ToolchainType",
		IncludeHidden: includeHidden,
	})
	if err != nil {
		return resolvedResource{}, err
	}
	var matches []resolvedResource
	for _, row := range rows {
		if row == nil || row.Resource == nil || !ref.matches(row.Resource.ResourceType, row.Resource.ResourceName) {
			continue
		}
		matches = append(matches, resolvedResource{
			UnitID:        row.Resource.UnitID,
			UnitSlug:      row.Resource.UnitSlug,
			SpaceSlug:     row.Resource.SpaceSlug,
			ResourceType:  row.Resource.ResourceType,
			ResourceName:  row.Resource.ResourceName,
			ToolchainType: row.Resource.ToolchainType,
		})
	}
	if unitWhere != "" && len(matches) > 0 {
		matches, err = keepResourcesInUnits(matches, unitWhere)
		if err != nil {
			return resolvedResource{}, err
		}
	}
	switch len(matches) {
	case 1:
		return matches[0], nil
	case 0:
		return resolvedResource{}, fmt.Errorf("no resource %s in %s", ref, scope)
	}
	lines := make([]string, 0, len(matches))
	for _, match := range matches {
		lines = append(lines, match.String())
	}
	sort.Strings(lines)
	return resolvedResource{}, fmt.Errorf("%s matches %d resources in %s:\n  %s\nname the namespace as <kind>/<namespace>/<name>, or narrow the units with --space, --variant or --where",
		ref, len(matches), scope, strings.Join(lines, "\n  "))
}

// keepResourcesInUnits keeps the matches whose unit the where clause selects. A clause that
// would not fit in a filter, which takes hundreds of matches, is left out: the matches are
// then reported as ambiguous, which they are, rather than refused for their number.
func keepResourcesInUnits(matches []resolvedResource, unitWhere string) ([]resolvedResource, error) {
	ids := make([]goclientnew.UUID, 0, len(matches))
	for _, match := range matches {
		ids = append(ids, match.UnitID)
	}
	where := cubapi.NewWhere(unitWhere).In("UnitID", ids)
	if len(where.String()) > cubapi.MaxFilterLength {
		return matches, nil
	}
	units, err := cubapi.ListUnits(ctx, cubClient, where, cubapi.ListOpts{Select: "UnitID", IncludeHidden: includeHidden})
	if err != nil {
		return nil, err
	}
	selected := map[goclientnew.UUID]bool{}
	for _, unit := range units {
		if unit != nil && unit.Unit != nil {
			selected[unit.Unit.UnitID] = true
		}
	}
	kept := matches[:0]
	for _, match := range matches {
		if selected[match.UnitID] {
			kept = append(kept, match)
		}
	}
	return kept, nil
}

// applyResourceFlag turns --resource, when given, into the selection the invocation runs
// with: the where clause naming the one unit that holds the resource, and, through the
// resourceType and whereResource the request reads, that resource alone within it. Without
// --resource the unit where clause is returned as it came.
func applyResourceFlag(unitWhere string) (string, error) {
	if resourceFlag == "" {
		return unitWhere, nil
	}
	if len(unitIdentifiers) > 0 || revisionIdentifier != "" || filter != "" || resourceType != "" || whereData != "" || whereResource != "" {
		return "", fmt.Errorf("--resource selects the unit and the resource itself, so it cannot be combined with --unit, --revision, --filter, --resource-type, --where-data or --where-resource; --where narrows the units it is looked for in")
	}
	ref, err := parseResourceRef(resourceFlag)
	if err != nil {
		return "", err
	}
	resolved, err := resolveResourceRef(ref, unitWhere)
	if err != nil {
		return "", err
	}
	resourceType = resolved.ResourceType
	whereResource = resolved.whereResource()
	return fmt.Sprintf("UnitID = '%s'", resolved.UnitID.String()), nil
}
