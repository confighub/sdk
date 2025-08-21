// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	goclientnew "github.com/confighub/sdk/openapi/goclient-new"
	"github.com/spf13/cobra"
)

var contextSetCmd = &cobra.Command{
	Use:   "set [options]",
	Short: "Sets context for the CLI",
	Long:  `Sets context for the CLI`,
	Args:  cobra.ExactArgs(0),
	RunE:  contextSetCmdRun,
}

func init() {
	contextSetCmd.PersistentFlags().StringVar(&spaceFlag, "space", "", "set space in context")
	contextCmd.AddCommand(contextSetCmd)
}

func contextSetCmdRun(_ *cobra.Command, _ []string) error {
	if spaceFlag != "" {
		space, err := apiGetSpaceFromSlug(spaceFlag, "") // default select should be fine
		if err != nil {
			return err
		}
		setCubContextFromSpace(space)
	}
	SaveCubContext(cubContext)
	return nil
}

func setCubContextFromSpace(space *goclientnew.Space) {
	cubContext.SpaceID = space.SpaceID.String()
	cubContext.Space = space.Slug
	cubContext.OrganizationID = space.OrganizationID.String()
}
