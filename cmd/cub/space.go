// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/confighub/sdk/core/cubapi"
	goclientnew "github.com/confighub/sdk/core/openapi/goclient-new"
	"github.com/spf13/cobra"
)

var spaceCmd = &cobra.Command{
	Use:   "space",
	Short: "Space commands",
	Long:  getSpaceCommandGroupHelp(),
}

func getSpaceCommandGroupHelp() string {
	baseHelp := `The space subcommands are used to manage spaces.

Spaces are explained at https://docs.confighub.com/background/entities/space/.
A guide for how to use spaces is at https://docs.confighub.com/guide/environments/.`
	agentContext := `Spaces are organizational boundaries within ConfigHub that contain units, define access control, and provide collaboration contexts.

Key concepts for agents:
- Spaces contain all units and their configurations
- Each space has independent access control and permissions
- Lists and bulk operations span every space unless --space narrows them
- A single entity is named as SPACE_SLUG/SLUG, or with --space SPACE_SLUG and its slug
- Creating an entity always takes --space

Setup workflow:
1. List available spaces ('space list') to discover accessible spaces
2. Use --space SPACE_SLUG, or SPACE_SLUG/SLUG references, on every command that needs one`

	return getCommandHelp(baseHelp, agentContext)
}

func init() {
	rootCmd.AddCommand(spaceCmd)
	addExplainCmd(spaceCmd, "Space")
}

var spaceFlag string
var variantFlag string
var componentFlag string
var selectedSpaceID string
var selectedSpaceSlug string

// spaceScopedCommands are the commands addSpaceFlags gave --space and --variant to. The
// completer for --variant is attached to these, and not found by flag name, because
// "cub space create --variant" and "cub variant upload --variant" take a label value.
var spaceScopedCommands []*cobra.Command

func addSpaceFlags(cmd *cobra.Command) {
	cmd.PersistentFlags().StringVar(&spaceFlag, "space", "", "space to operate in, by slug or UUID. Omitted, a list or bulk operation spans the organization and a single entity is named as <space>/<slug>; \"*\" means the same as omitting it")
	cmd.PersistentFlags().StringVar(&variantFlag, "variant", "", "variant to operate in, as <component>/<variant>, or as <variant> with --component: the space naming that Component whose Variant label is <variant>. The same scope as --space, named by what it is rather than by the space's slug; the two cannot be combined")
	cmd.PersistentFlags().StringVar(&componentFlag, "component", "", "the Component of --variant, when that names the variant alone; --variant <component>/<variant> says the same in one flag")
	spaceScopedCommands = append(spaceScopedCommands, cmd)
}

// qualifiedVariant settles which Component and variant a --variant value names, together
// with a --component given beside it. The value is <component>/<variant>, or <variant> alone
// with the component from --component; both given, they have to agree. A variant name
// cannot contain a slash, since the slash is what qualifies it.
func qualifiedVariant(component, value string) (string, string, error) {
	if value == "" {
		return "", "", fmt.Errorf("--variant must not be empty")
	}
	inline, variant, qualified := strings.Cut(value, "/")
	if !qualified {
		if component == "" {
			return "", "", fmt.Errorf("--variant %s names no component: pass --component <component> with it, or write --variant <component>/%s", value, value)
		}
		return component, value, nil
	}
	if inline == "" || variant == "" {
		return "", "", fmt.Errorf("--variant takes <component>/<variant>, got %q", value)
	}
	if strings.Contains(variant, "/") {
		return "", "", fmt.Errorf("--variant %q: a variant name cannot contain a slash", value)
	}
	if component != "" && component != inline {
		return "", "", fmt.Errorf("--component %s and --variant %s name different components", component, value)
	}
	return inline, variant, nil
}

// resolveVariantOrSpace resolves the argument of a command that takes a variant: as
// <component>/<variant> when it has a slash, which a space slug never has, and as a space
// slug or UUID otherwise.
func resolveVariantOrSpace(ref string, selectParam string) (*goclientnew.ExtendedSpace, error) {
	if strings.Contains(ref, "/") {
		return resolveVariantSpace(ref, selectParam)
	}
	return resolveSpace(ref, selectParam)
}

// parseVariantRef splits a <component>/<variant> reference given on its own.
func parseVariantRef(ref string) (component, variant string, err error) {
	return qualifiedVariant("", ref)
}

// listVariantSpaces lists the spaces naming the Component, those with the given Variant
// label when it is not empty.
func listVariantSpaces(ctx context.Context, componentID goclientnew.UUID, variant string) ([]*goclientnew.ExtendedSpace, error) {
	where := cubapi.NewWhere(fmt.Sprintf("ComponentID = '%s'", componentID.String()))
	if variant != "" {
		where = where.Eq("Labels.Variant", variant)
	}
	return cubapi.ListSpaces(ctx, cubClient, where, cubapi.ListOpts{
		Select: "SpaceID,Slug,ComponentID,Labels,OrganizationID",
	})
}

// resolveVariantSpace resolves a <component>/<variant> reference to the one space naming
// that Component with that Variant label, with the fields selectParam names. Variant is a
// label, so nothing stops two spaces of a Component from carrying the same one; that is
// reported rather than answered with either.
func resolveVariantSpace(ref string, selectParam string) (*goclientnew.ExtendedSpace, error) {
	componentRef, variant, err := parseVariantRef(ref)
	if err != nil {
		return nil, err
	}
	return resolveVariant(componentRef, variant, selectParam)
}

// resolveVariant resolves a Component and a variant name given separately.
func resolveVariant(componentRef, variant string, selectParam string) (*goclientnew.ExtendedSpace, error) {
	ref := componentRef + "/" + variant
	componentID, err := resolveComponentID(componentRef)
	if err != nil {
		return nil, err
	}
	spaces, err := listVariantSpaces(ctx, goclientnew.UUID(componentID), variant)
	if err != nil {
		return nil, err
	}
	// The listing selects what the ambiguity report needs; the one match is then read with
	// what the caller asked for, as any other reference is.
	if len(spaces) == 1 {
		return resolveSpace(spaces[0].Space.SpaceID.String(), selectParam)
	}
	if len(spaces) == 0 {
		return nil, fmt.Errorf("component %s has no variant %q: no space naming it carries the label Variant=%s", componentRef, variant, variant)
	}
	slugs := make([]string, 0, len(spaces))
	for _, space := range spaces {
		slugs = append(slugs, space.Space.Slug)
	}
	sort.Strings(slugs)
	return nil, fmt.Errorf("variant %s is ambiguous: %d spaces name component %s with Variant=%s (%s); scope with --space instead",
		ref, len(spaces), componentRef, variant, strings.Join(slugs, ", "))
}

// spaceOptionalAnnotation marks a command that names its operand by reference
// and can therefore run without a space.
const spaceOptionalAnnotation = "SpaceOptional"

// enableOptionalSpace marks a command as able to run without a space.
//
// It is for the commands that take an entity reference and resolve it: a UUID
// names an entity outright, and a "space/slug" carries its own space, so
// demanding a space up front rejects references that need none. What is left --
// a bare slug with no space in sight -- is searched for across the organization
// and refused only if it matches in more than one space, which is the point at
// which a space is genuinely required and the error can say so.
func enableOptionalSpace(cmd *cobra.Command) {
	if cmd.Annotations == nil {
		cmd.Annotations = map[string]string{}
	}
	cmd.Annotations[spaceOptionalAnnotation] = ""
}

// allowOmittedSpace reports whether a command may run with no space resolved.
func allowOmittedSpace(cmd *cobra.Command) bool {
	_, ok := cmd.Annotations[spaceOptionalAnnotation]
	return ok
}

// spacePreRunE settles the space a command runs in, for the commands that take
// one. --space names it. Without --space, a command that lists or operates in
// bulk (OrgLevel) spans the organization; a command that names its operand by
// reference (SpaceOptional) runs with no space and lets the reference decide;
// any other command is refused with the two ways to supply one. There is no
// default space: a context names a server, an organization and a user, and
// every command says which space it means.
func spacePreRunE(cmd *cobra.Command, args []string) error {
	if err := globalPreRun(cmd, args); err != nil {
		return err
	}
	if componentFlag != "" && variantFlag == "" {
		return fmt.Errorf("--component names the component of --variant: pass --variant <variant> with it, or --variant <component>/<variant>")
	}
	if variantFlag != "" {
		if spaceFlag != "" {
			return fmt.Errorf("--variant and --space name the same scope: pass one of them")
		}
		component, variant, err := qualifiedVariant(componentFlag, variantFlag)
		if err != nil {
			return err
		}
		space, err := resolveVariant(component, variant, "*")
		if err != nil {
			return err
		}
		selectedSpaceID = space.Space.SpaceID.String()
		selectedSpaceSlug = space.Space.Slug
		return nil
	}
	// "*" is the explicit spelling of "no space", kept so that scripts written
	// when it was needed keep working.
	if spaceFlag != "" && spaceFlag != "*" {
		space, err := resolveSpace(spaceFlag, "*") // get all fields for now
		if err != nil {
			return err
		}
		selectedSpaceID = space.Space.SpaceID.String()
		selectedSpaceSlug = space.Space.Slug
		return nil
	}
	if _, orgLevel := cmd.Annotations["OrgLevel"]; orgLevel {
		selectedSpaceID = "*"
		selectedSpaceSlug = "*"
		return nil
	}
	if allowOmittedSpace(cmd) {
		selectedSpaceID = ""
		selectedSpaceSlug = ""
		return nil
	}
	return fmt.Errorf("%s needs a space: pass --space <space>, or name the entity as <space>/<slug> (the context's default space is no longer used)", cmd.CommandPath())
}

func buildWhereClauseFromSpaces(spaceIds []string) (string, error) {
	return buildWhereClauseFromIdentifiers(spaceIds, "SpaceID", "Slug")
}

// addSpaceIDToWhereClause adds space constraint to where clause, for reuse across commands
func addSpaceIDToWhereClause(whereClause, spaceID string) string {
	if spaceID == "*" {
		return whereClause
	}
	spaceConstraint := fmt.Sprintf("SpaceID = '%s'", spaceID)
	if whereClause != "" {
		return fmt.Sprintf("%s AND %s", whereClause, spaceConstraint)
	}
	return spaceConstraint
}
