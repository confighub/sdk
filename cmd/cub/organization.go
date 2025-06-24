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
	globalPreRun(cmd, args)

	selectedOrg := &goclientnew.Organization{}
	if cubContext.OrganizationID == "" {
		return fmt.Errorf("organization is required. Set with --organization option or set in context with the context sub-command")
	}
	selectedOrg, err := apiGetOrganization(cubContext.OrganizationID)
	if err != nil {
		return err
	}
	selectedOrganizationID = selectedOrg.OrganizationID.String()
	selectedOrganizationSlug = selectedOrg.Slug
	return nil
}
