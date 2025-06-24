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
	Long:  `The space subcommands are used to manage spaces`,
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
