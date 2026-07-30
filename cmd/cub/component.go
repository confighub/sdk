// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"fmt"
	"sort"
	"strings"

	goclientnew "github.com/confighub/sdk/core/openapi/goclient-new"
	"github.com/spf13/cobra"
)

// Well-known Space labels that define a component. A component is not a stored
// entity: it is the set of spaces sharing a "Component" label value, one space
// per variant. The web UI derives its component view from exactly these labels.
const (
	labelComponent = "Component"
	labelVariant   = "Variant"
	labelOwner     = "Owner"
)

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
	baseHelp := `The component subcommands list components and open them in the web UI.

A component is an application or service tracked across its variants. It is not a stored entity:
it is the set of spaces sharing a "Component" label value, one space per variant, as created by
'cub variant upload' and 'cub variant create'. The web UI's component view shows those variants as
a deployment graph and is where config changes are promoted downstream.`

	agentContext := `Components are a view over Space labels, not an API resource. There is no
component create/update/delete: a component comes into existence when a space is labeled
Component=<name>, and gains a variant when another space with the same Component label is created.

The equivalent raw query is:
  cub space list --where "Labels.Component = 'my-app'"

'component open' is interactive — it launches a browser — so prefer 'component list' and the space
and unit commands for automation. Use 'component open --print-url' when you only need the URL.`

	return getCommandHelp(baseHelp, agentContext)
}

func init() {
	rootCmd.AddCommand(componentCmd)
}

// Component aggregates the spaces that share one "Component" label value.
type Component struct {
	Name string
	// Owner is the "Owner" label, taken from the component's spaces. Empty when
	// they disagree or none set it.
	Owner string
	// Variants are the "Variant" label values, sorted, one per space. A space
	// with no Variant label contributes an empty string.
	Variants []string
	// Spaces are the spaces carrying this Component label, sorted by slug.
	Spaces []*goclientnew.ExtendedSpace
	// UnitCount is the total number of units across those spaces.
	UnitCount int
}

// apiListComponents groups every space the caller can see by its "Component"
// label. Spaces without that label are not part of any component and are
// dropped. This mirrors the web UI, which filters the same way client-side;
// there is no server-side component query to delegate to.
func apiListComponents(whereFilter string, filterParam string) ([]*Component, error) {
	extendedSpaces, err := apiListExtendedSpaces(whereFilter, "", filterParam, true)
	if err != nil {
		return nil, err
	}

	byName := map[string]*Component{}
	for _, extendedSpace := range extendedSpaces {
		if extendedSpace.Space == nil {
			continue
		}
		name := extendedSpace.Space.Labels[labelComponent]
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

// apiGetComponentFromName resolves a component by its "Component" label value.
func apiGetComponentFromName(name string) (*Component, error) {
	components, err := apiListComponents(fmt.Sprintf("Labels.%s = '%s'", labelComponent, name), "")
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
