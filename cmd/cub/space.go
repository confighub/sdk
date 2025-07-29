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
	baseHelp := `The space subcommands are used to manage spaces`
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
}

var spaceFlag string
var selectedSpaceID string
var selectedSpaceSlug string

func addSpaceFlags(cmd *cobra.Command) {
	// TODO: Should we set space from context on the flag?
	cmd.PersistentFlags().StringVar(&spaceFlag, "space", "", "space ID to perform command on")
}

// to be used by sub-commands that requires space ID
func spacePreRunE(cmd *cobra.Command, args []string) error {
	globalPreRun(cmd, args)

	if spaceFlag != "" {
		if spaceFlag == "*" {
			_, orgLevel := cmd.Annotations["OrgLevel"]
			if orgLevel {
				selectedSpaceID = "*"
				selectedSpaceSlug = "*"
				return nil
			}
			return fmt.Errorf("space wildcard * not permitted for command %s", cmd.Name())
		}
		space, err := apiGetSpaceFromSlug(spaceFlag)
		if err != nil {
			return err
		}
		selectedSpaceID = space.SpaceID.String()
		selectedSpaceSlug = space.Slug
		return nil
	}
	if cubContext.SpaceID == "" {
		return fmt.Errorf("space is required. Set with --space option or set in context with the context sub-command")
	}
	selectedSpaceID = cubContext.SpaceID
	selectedSpaceSlug = cubContext.Space
	return nil
}
