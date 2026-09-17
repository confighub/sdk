// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"fmt"

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
var selectedSpaceID string
var selectedSpaceSlug string

func addSpaceFlags(cmd *cobra.Command) {
	cmd.PersistentFlags().StringVar(&spaceFlag, "space", "", "space to operate in, by slug or UUID. Omitted, a list or bulk operation spans the organization and a single entity is named as <space>/<slug>")
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
