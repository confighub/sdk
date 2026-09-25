// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"fmt"
	"sort"
	"strings"

	"github.com/confighub/sdk/core/cubapi"
	goclientnew "github.com/confighub/sdk/core/openapi/goclient-new"
	"github.com/spf13/cobra"
)

// Well-known Space labels. A Space's component is the Component it names with
// ComponentID, not a label; labelComponent is the label a Stage's selector may
// not name, since the component is the change order's own (see stageWhereSpace).
const (
	labelComponent = "Component"
	labelVariant   = "Variant"
	labelOwner     = "Owner"
)

// labelNamespace is the Space label recording the Kubernetes namespace a variant's
// units are placed in: "cub variant create --namespace" sets it, and "cub variant
// promote" applies it to the units it later clones into the space.
const labelNamespace = "Namespace"

// componentCmd is inherently cross-space — a component spans one space per
// variant — so it does not use spacePreRunE. It only needs globalPreRun to
// initialize the API client.
var componentCmd = &cobra.Command{
	Use:               "component",
	Short:             "Component commands",
	Long:              getComponentCommandGroupHelp(),
	PersistentPreRunE: globalPreRun,
}

func getComponentCommandGroupHelp() string {
	baseHelp := `The component subcommands list components and open them in the web UI, and manage Component entities.

A component is an application or service tracked across its variants. 'list', 'create', 'get',
'update' and 'delete' manage the Component entity, which decides which ChangeWorkflows promotions
and releases of its variants may use, and whether one is required. Its variants are the spaces
naming it with ComponentID.

'open' shows the web UI's component view of a Component's variants, as created by 'cub variant
upload' and 'cub variant create'. It shows those variants as a deployment graph and is where config
changes are promoted downstream.`

	agentContext := `'list', 'create', 'get', 'update' and 'delete' operate on the Component entity by slug or UUID.
'open' shows a Component's variants: the spaces naming it with ComponentID, which 'cub variant
upload' sets and 'cub variant create' inherits from the upstream space.

The equivalent raw query is:
  COMPONENT_ID=$(cub component get my-app -o jq=.ComponentID)
  cub space list --where "ComponentID = '$COMPONENT_ID'"

'component open' is interactive — it launches a browser — so prefer 'component list' and the space
and unit commands for automation. Use 'component open --print-url' when you only need the URL.`

	return getCommandHelp(baseHelp, agentContext)
}

func init() {
	rootCmd.AddCommand(componentCmd)
}

// Component aggregates the spaces that are Variants of one Component, named by
// its slug.
type Component struct {
	Name string
	// Owner is the "Owner" label, taken from the component's spaces. Empty when
	// they disagree or none set it.
	Owner string
	// Variants are the "Variant" label values, sorted, one per space. A space
	// with no Variant label contributes an empty string.
	Variants []string
	// Spaces are the spaces naming this Component with ComponentID, sorted by slug.
	Spaces []*goclientnew.ExtendedSpace
	// UnitCount is the total number of units across those spaces.
	UnitCount int
}

// apiListComponents groups every space the caller can see by its ComponentID,
// naming each group by the Component's slug. Spaces in no Component are not
// part of any component and are dropped.
func apiListComponents(whereFilter string, filterParam string) ([]*Component, error) {
	extendedSpaces, err := apiListExtendedSpaces(whereFilter, "", filterParam, true)
	if err != nil {
		return nil, err
	}
	componentEntities, err := cubapi.ListComponents(ctx, cubClient, cubapi.NewWhere(""), cubapi.ListOpts{
		Select: "ComponentID,Slug,OrganizationID",
	})
	if err != nil {
		return nil, err
	}
	componentSlugs := make(map[goclientnew.UUID]string, len(componentEntities))
	for _, c := range componentEntities {
		if c.Component != nil {
			componentSlugs[c.Component.ComponentID] = c.Component.Slug
		}
	}

	byName := map[string]*Component{}
	for _, extendedSpace := range extendedSpaces {
		if extendedSpace.Space == nil || extendedSpace.Space.ComponentID == nil {
			continue
		}
		name := componentSlugs[*extendedSpace.Space.ComponentID]
		if name == "" {
			continue
		}
		component := byName[name]
		if component == nil {
			component = &Component{Name: name}
			byName[name] = component
		}
		component.Spaces = append(component.Spaces, extendedSpace)
		component.Variants = append(component.Variants, extendedSpace.Space.Labels[labelVariant])
		component.UnitCount += int(extendedSpace.TotalUnitCount)
	}

	components := make([]*Component, 0, len(byName))
	for _, component := range byName {
		sort.Slice(component.Spaces, func(i, j int) bool {
			return component.Spaces[i].Space.Slug < component.Spaces[j].Space.Slug
		})
		sort.Strings(component.Variants)
		component.Owner = commonOwnerLabel(component.Spaces)
		components = append(components, component)
	}
	sort.Slice(components, func(i, j int) bool { return components[i].Name < components[j].Name })
	return components, nil
}

// commonOwnerLabel returns the "Owner" label shared by every space, or "" if the
// spaces disagree or none set it. A component's owner is a property of the
// component, so a split answer is reported as no answer rather than picking one.
func commonOwnerLabel(spaces []*goclientnew.ExtendedSpace) string {
	owner := ""
	for i, extendedSpace := range spaces {
		value := extendedSpace.Space.Labels[labelOwner]
		if i == 0 {
			owner = value
			continue
		}
		if value != owner {
			return ""
		}
	}
	return owner
}

// apiGetComponentFromName resolves a component by its Component's slug.
func apiGetComponentFromName(name string) (*Component, error) {
	entity, err := resolveComponent(name, "")
	if err != nil {
		return nil, err
	}
	components, err := apiListComponents(fmt.Sprintf("ComponentID = '%s'", entity.Component.ComponentID), "")
	if err != nil {
		return nil, err
	}
	for _, component := range components {
		if component.Name == name {
			return component, nil
		}
	}
	return nil, fmt.Errorf("component %s not found. Run 'cub component list' to see available components", name)
}

// spaceIDForVariant returns the ID of the component's space with the given
// "Variant" label.
func (c *Component) spaceIDForVariant(variant string) (string, error) {
	for _, extendedSpace := range c.Spaces {
		if extendedSpace.Space.Labels[labelVariant] == variant {
			return extendedSpace.Space.SpaceID.String(), nil
		}
	}
	return "", fmt.Errorf("component %s has no %s variant. Available variants: %s",
		c.Name, variant, joinVariants(c.Variants))
}

// joinVariants renders a component's variants for display, naming the unlabeled
// case rather than showing a stray empty entry.
func joinVariants(variants []string) string {
	shown := make([]string, 0, len(variants))
	for _, variant := range variants {
		if variant == "" {
			variant = "(none)"
		}
		shown = append(shown, variant)
	}
	return strings.Join(shown, ", ")
}
