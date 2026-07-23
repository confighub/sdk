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
- Operations are typically scoped to a specific space
- Use wildcard "*" for cross-space operations where supported

Setup workflow:
1. List available spaces ('space list') to discover accessible spaces
2. Set default space context ('context set --space SPACE_SLUG')
3. Verify current context ('context get')

Most unit and function commands require a space context either through --space flag or default context.`

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
	// TODO: Should we set space from context on the flag?
	cmd.PersistentFlags().StringVar(&spaceFlag, "space", "", "space ID to perform command on")
}

// allowWildcardSpace sets the selected space to the wildcard "*" for cross-space
// operations when the command is annotated as OrgLevel (i.e. it supports operating
// across all spaces). It reports whether the wildcard was permitted.
func allowWildcardSpace(cmd *cobra.Command) bool {
	if _, orgLevel := cmd.Annotations["OrgLevel"]; orgLevel {
		selectedSpaceID = "*"
		selectedSpaceSlug = "*"
		return true
	}
	return false
}

// to be used by sub-commands that requires space ID
func spacePreRunE(cmd *cobra.Command, args []string) error {
	if err := globalPreRun(cmd, args); err != nil {
		return err
	}

	if spaceFlag != "" {
		if spaceFlag == "*" {
			if allowWildcardSpace(cmd) {
				return nil
			}
			return fmt.Errorf("space wildcard * not permitted for command %s", cmd.Name())
		}
		space, err := apiGetSpaceFromSlug(spaceFlag, "*") // get all fields for now
		if err != nil {
			return err
		}
		selectedSpaceID = space.SpaceID.String()
		selectedSpaceSlug = space.Slug
		return nil
	}
	// Check if we have space information
	ctx := contextManager.ActiveContext()
	if ctx == nil {
		return fmt.Errorf("no context available")
	}
	// An empty or wildcard default space means cross-space (org-level) operations
	// by default. Honor it only for commands that support it (OrgLevel); otherwise
	// the user must supply a specific space via --space.
	if ctx.Settings.DefaultSpace == "" || ctx.Settings.DefaultSpace == "*" {
		if allowWildcardSpace(cmd) {
			return nil
		}
		return fmt.Errorf("space is required. Set with --space option or set in context with the context sub-command")
	}
	if selectedSpaceID == "" {
		// Any message output here messes up json and jq output, output only for functions, config data only, etc.
		// tprint("Space ID is not set. Fetching for space name %s", ctx.Settings.DefaultSpace)
		space, err := apiGetSpaceFromSlug(ctx.Settings.DefaultSpace, "")
		// Note: If the DefaultSpace has been deleted, this will fail.
		if err != nil {
			return fmt.Errorf("failed to resolve space %s: %w", ctx.Settings.DefaultSpace, err)
		}
		selectedSpaceID = space.SpaceID.String()
		selectedSpaceSlug = space.Slug
	}
	return nil
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
