// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"github.com/confighub/sdk/core/cubapi"
	goclientnew "github.com/confighub/sdk/core/openapi/goclient-new"
	"github.com/spf13/cobra"
)

var serviceAccountListCmd = &cobra.Command{
	Use:   "list",
	Short: "List service accounts",
	Args:  cobra.NoArgs,
	Long: getCommandHelp(`List the ServiceAccount entities in this organization.

Examples:
`+"```"+`
  # List all service accounts
  cub serviceaccount list

  # List just the service account names
  cub serviceaccount list -o name

  # List the service accounts that have an organization role
  cub serviceaccount list --where "OrgRole != 'none'"
`+"```"+`
`, ""),
	RunE: serviceAccountListCmdRun,
}

// Default columns to display when no custom columns are specified
var defaultServiceAccountColumns = []string{"ServiceAccount.Slug", "ServiceAccount.OrgRole", "ServiceAccount.UserID", "ServiceAccount.Labels"}

// serviceAccountBaseSelectFields are the fields always returned by service account list queries.
var serviceAccountBaseSelectFields = []string{"Slug", "ServiceAccountID", "OrganizationID"}

var serviceAccountAliases = map[string]string{
	"Name": "ServiceAccount.Slug",
	"ID":   "ServiceAccount.ServiceAccountID",
}

var serviceAccountCustomColumnDependencies = map[string][]string{}

func init() {
	enableWhereFlag(serviceAccountListCmd)
	enableFilterFlag(serviceAccountListCmd)
	enableContainsFlag(serviceAccountListCmd)
	enableListPagingFlags(serviceAccountListCmd)
	addStandardListDisplayFlags(serviceAccountListCmd)
	serviceAccountCmd.AddCommand(serviceAccountListCmd)
}

func serviceAccountListCmdRun(_ *cobra.Command, _ []string) error {
	filterID, err := parseFilterFlag(filter)
	if err != nil {
		return err
	}
	selectValue := handleSelectParameter(selectFields, selectFields, func() string {
		return buildSelectList("ServiceAccount", listColumnsFor("cub serviceaccount list"), "", defaultServiceAccountColumns, serviceAccountAliases, serviceAccountCustomColumnDependencies, serviceAccountBaseSelectFields)
	})
	serviceAccounts, err := cubapi.ListServiceAccounts(ctx, cubClient, cubapi.NewWhere(where), cubapi.ListOpts{
		Limit:         listLimit,
		OrderBy:       listOrderBy,
		Select:        cubapi.SelectFields(selectValue),
		Filter:        filterID,
		Contains:      contains,
		IncludeHidden: includeHidden,
	})
	if err != nil {
		return err
	}
	displayListResults(serviceAccounts, func(sa *goclientnew.ExtendedServiceAccount) string { return sa.ServiceAccount.Slug }, displayServiceAccountList)
	return nil
}

func displayServiceAccountList(serviceAccounts []*goclientnew.ExtendedServiceAccount) {
	if displayRequestedColumns(serviceAccounts, serviceAccountAliases, nil) {
		return
	}
	table := tableView()
	if !noheader {
		table.SetHeader([]string{"Name", "Org-Role", "User-ID", "Labels"})
	}
	for _, sa := range serviceAccounts {
		serviceAccount := sa.ServiceAccount
		table.Append([]string{
			serviceAccount.Slug,
			serviceAccount.OrgRole,
			serviceAccount.UserID.String(),
			labelsToString(serviceAccount.Labels),
		})
	}
	table.Render()
}
