// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"fmt"

	goclientnew "github.com/confighub/sdk/openapi/goclient-new"
	"github.com/spf13/cobra"
)

var organizationCmd = &cobra.Command{
	Use:               "organization",
	Short:             "Organization commands",
	Long:              `The organization subcommands are used to manage organizations`,
	PersistentPreRunE: organizationPreRunE,
}

func init() {
	rootCmd.AddCommand(organizationCmd)
}

var selectedOrganizationID string
var selectedOrganizationSlug string

// to be used by sub-commands that requires organization ID
func organizationPreRunE(cmd *cobra.Command, args []string) error {
	if err := globalPreRun(cmd, args); err != nil {
		return err
	}

	ctx := contextManager.ActiveContext()
	if ctx == nil {
		return fmt.Errorf("no context available")
	}

	selectedOrg := &goclientnew.Organization{}
	if ctx.Coordinate.OrganizationID == "" {
		return fmt.Errorf("organization is required. Set with --organization option or set in context with the context sub-command")
	}
	selectedOrg, err := apiGetOrganizationFromExternalID(ctx.Coordinate.OrganizationID)
	if err != nil {
		return err
	}
	selectedOrganizationID = selectedOrg.OrganizationID.String()
	selectedOrganizationSlug = selectedOrg.Slug
	return nil
}
