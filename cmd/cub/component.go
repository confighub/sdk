// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"bytes"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/confighub/sdk/core/cubapi"
	goclientnew "github.com/confighub/sdk/core/openapi/goclient-new"
	"github.com/spf13/cobra"
)

// Well-known Space labels. A Space's component is the Component it names with
// ComponentID, not a label.
const (
	labelVariant = "Variant"
	labelOwner   = "Owner"
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
  COMPONENT_ID=$(cub component get my-app -o jq=.Component.ComponentID)
  cub space list --where "ComponentID = '$COMPONENT_ID'"

'component open' is interactive — it launches a browser — so prefer 'component list' and the space
and unit commands for automation. Use 'component open --print-url' when you only need the URL.`

	return getCommandHelp(baseHelp, agentContext)
}

func init() {
	rootCmd.AddCommand(componentCmd)
	addExplainCmd(componentCmd, "Component")
}

// Component aggregates the spaces that are Variants of one Component, named by
// its slug.
type Component struct {
	Name string
	// Owner is the Component's owner, as componentOwner reads it.
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
		Select: "ComponentID,Slug,Labels,OrganizationID",
	})
	if err != nil {
		return nil, err
	}
	componentSlugs := make(map[goclientnew.UUID]string, len(componentEntities))
	componentLabels := make(map[string]map[string]string, len(componentEntities))
	for _, c := range componentEntities {
		if c.Component != nil {
			componentSlugs[c.Component.ComponentID] = c.Component.Slug
			componentLabels[c.Component.Slug] = c.Component.Labels
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
		component.Owner = componentOwner(componentLabels[component.Name], component.Spaces)
		components = append(components, component)
	}
	sort.Slice(components, func(i, j int) bool { return components[i].Name < components[j].Name })
	return components, nil
}

// componentOwner is a Component's owner: its own Owner label when set, otherwise the Owner
// label every one of its Spaces carries with the same value. Spaces that disagree, a Space
// with no Owner label, or no Spaces at all give "": an owner is a property of the Component,
// so a split answer is reported as no answer rather than picking one.
func componentOwner(componentLabels map[string]string, spaces []*goclientnew.ExtendedSpace) string {
	if owner := componentLabels[labelOwner]; owner != "" {
		return owner
	}
	owner := ""
	for i, extendedSpace := range spaces {
		value := ""
		if extendedSpace.Space != nil {
			value = extendedSpace.Space.Labels[labelOwner]
		}
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

// ownerWrite decides what --owner does to a Component that already exists. It refuses to
// replace an owner the Component already has, since --owner on a variant command names the
// owner of one variant's Component and is not the place to reassign it; it reports set when
// the Component's own label has to be written, which it does not when the label already says
// the same thing.
func ownerWrite(componentSlug string, componentLabels map[string]string, spaces []*goclientnew.ExtendedSpace, requested string) (bool, error) {
	if requested == "" {
		return false, nil
	}
	current := componentOwner(componentLabels, spaces)
	switch current {
	case "":
		return true, nil
	case requested:
		return componentLabels[labelOwner] == "", nil
	default:
		return false, &ownerConflictError{componentSlug: componentSlug, current: current, requested: requested}
	}
}

// ownerConflictError is ownerWrite's refusal to replace an owner the Component already has. It
// carries the command that changes the owner, so a caller does not add that hint again.
type ownerConflictError struct {
	componentSlug string
	current       string
	requested     string
}

func (e *ownerConflictError) Error() string {
	return fmt.Sprintf("component %s already has owner %q, so --owner %q is refused; to change the owner, run: %s",
		e.componentSlug, e.current, e.requested, componentOwnerCommand(e.componentSlug, e.requested))
}

// ownerValueRegexp is the server's rule for a label value: letters, digits, and the printable
// ASCII punctuation other than quotes, backquote, backslash, "*" and "^", which would need
// escaping in a where expression, with inner spaces but no leading or trailing ones. It is checked here so that an owner the
// server would refuse stops a variant command before it writes anything.
var ownerValueRegexp = regexp.MustCompile(`^[\-_@/#$%&~+=!?.,:;(){}\[\]<>|A-Za-z0-9](([\-_@/#$%&~+=!?.,:;(){}\[\]<>|A-Za-z0-9]| )*[\-_@/#$%&~+=!?.,:;(){}\[\]<>|A-Za-z0-9])?$`)

// ownerValueMaxLength is the server's limit on the length of a label value.
const ownerValueMaxLength = 128

// validateOwnerValue refuses an --owner value that the server would refuse as a label value.
func validateOwnerValue(owner string) error {
	if len(owner) > ownerValueMaxLength {
		return fmt.Errorf("--owner %q is not a valid label value: it is longer than %d characters", owner, ownerValueMaxLength)
	}
	if !ownerValueRegexp.MatchString(owner) {
		return fmt.Errorf("--owner %q is not a valid label value: it may hold letters, digits, inner spaces and the characters %s, "+
			"and must not start or end with a space", owner, "-_@/#$%&~+=!?.,:;(){}[]<>|")
	}
	return nil
}

// componentOwnerCommand is the command that sets a Component's owner by hand.
func componentOwnerCommand(componentSlug, owner string) string {
	return fmt.Sprintf("cub component update --patch %s --label %s", componentSlug, shellQuoteArg(labelOwner+"="+owner))
}

// shellQuoteArg quotes s for a POSIX shell when it holds anything but plain characters, so a
// command printed for the user can be pasted as it is.
func shellQuoteArg(s string) string {
	plain := s != ""
	for _, r := range s {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || strings.ContainsRune("-_.=/:@+,", r)) {
			plain = false
			break
		}
	}
	if plain {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// componentSpaces lists the Spaces naming the Component, with the labels componentOwner reads.
func componentSpaces(componentID goclientnew.UUID) ([]*goclientnew.ExtendedSpace, error) {
	return cubapi.ListSpaces(ctx, cubClient, cubapi.NewWhere(fmt.Sprintf("ComponentID = '%s'", componentID)), cubapi.ListOpts{
		Select: "SpaceID,Slug,ComponentID,Labels,OrganizationID",
	})
}

// checkComponentOwner applies ownerWrite to the Component as it is now, and reports whether
// --owner has to write its label.
func checkComponentOwner(component *goclientnew.Component, requested string) (bool, error) {
	spaces, err := componentSpaces(component.ComponentID)
	if err != nil {
		return false, err
	}
	return ownerWrite(component.Slug, component.Labels, spaces, requested)
}

// applyComponentOwner sets the Component's Owner label with the merge patch "cub component
// update --patch --label" sends, so its other labels are left alone. With dryRun the server
// checks the write, including the caller's permission, without making it.
func applyComponentOwner(componentID goclientnew.UUID, owner string, dryRun bool) error {
	patchData, err := EnhancePatchData([]byte("null"), nil, []string{labelOwner + "=" + owner}, nil, nil, nil)
	if err != nil {
		return err
	}
	var dryRunValue *bool
	if dryRun {
		dryRunValue = &dryRun
	}
	componentRes, err := cubClientNew.PatchComponentWithBodyWithResponse(
		ctx,
		componentID,
		&goclientnew.PatchComponentParams{DryRun: dryRunValue},
		"application/merge-patch+json",
		bytes.NewReader(patchData),
	)
	if cubapi.IsAPIError(err, componentRes) {
		return cubapi.InterpretErrorGeneric(err, componentRes)
	}
	return nil
}

// dryRunCreateComponentOwner asks the server to check the create of a Component with the Owner
// label, the way a variant command creates a missing Component and then sets the label, without
// making it. It catches a value or a permission the server refuses before the variant is written.
func dryRunCreateComponentOwner(componentSlug, owner string) error {
	dryRun := true
	componentRes, err := cubClientNew.CreateComponentWithResponse(ctx, &goclientnew.CreateComponentParams{DryRun: &dryRun}, goclientnew.Component{
		Slug:        componentSlug,
		DisplayName: componentSlug,
		Labels:      map[string]string{labelOwner: owner},
	})
	if cubapi.IsAPIError(err, componentRes) {
		return cubapi.InterpretErrorGeneric(err, componentRes)
	}
	return nil
}

// writeComponentOwner writes --owner to the Component after the variant was written. It reads
// the Component and its Spaces again and applies ownerWrite to them first: another writer can
// set an owner after the check made before the variant was written, and that owner is kept and
// reported as a conflict, not overwritten. It reports whether it wrote the label.
func writeComponentOwner(componentRef, owner string) (bool, error) {
	entity, err := resolveComponent(componentRef, "ComponentID,Slug,Labels")
	if err != nil {
		return false, err
	}
	set, err := checkComponentOwner(entity.Component, owner)
	if err != nil || !set {
		return false, err
	}
	if err := applyComponentOwner(entity.Component.ComponentID, owner, false); err != nil {
		return false, err
	}
	return true, nil
}

// componentOwnerNotSet is the error for a Component write that failed after the variant it
// belongs to was written, so the user knows the variant is there and how to finish the job.
func componentOwnerNotSet(variant, componentSlug, owner string, err error) error {
	var conflict *ownerConflictError
	if errors.As(err, &conflict) {
		return fmt.Errorf("%s exists, but the Owner label of component %s was not set: %w", variant, componentSlug, err)
	}
	return fmt.Errorf("%s exists, but the Owner label of component %s was not set: %w; set it with: %s",
		variant, componentSlug, err, componentOwnerCommand(componentSlug, owner))
}

// componentOwnerSkipped adds to the error of a variant command that stopped after it wrote
// something but before it set the Owner label, so the user knows the label is missing too and how
// to set it.
func componentOwnerSkipped(err error, componentSlug, owner string) error {
	return fmt.Errorf("%w; the Owner label of component %s was not set either; set it with: %s",
		err, componentSlug, componentOwnerCommand(componentSlug, owner))
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
