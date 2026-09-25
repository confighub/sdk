// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"github.com/confighub/sdk/core/cubapi"
	goclientnew "github.com/confighub/sdk/core/openapi/goclient-new"
	"github.com/spf13/cobra"
)

var attestationListCmd = &cobra.Command{
	Use:   "list",
	Short: "List attestations",
	Long: getCommandHelp(`List attestations in a space, or across the organization when no space is selected.

Examples:
`+"```"+`
  # List the attestations in a space
  cub attestation list --space payments-prod

  # List the rejections recorded anywhere
  cub attestation list --where "Result = 'Fail'"

  # List the attestations covering a revision
  cub revision list --space payments-prod web -o jq='.[].Revision.Attestations'
`+"```"+`
`, ""),
	Args:        cobra.NoArgs,
	RunE:        attestationListCmdRun,
	Annotations: map[string]string{"OrgLevel": ""},
}

var defaultAttestationColumns = []string{
	"Attestation.AttestationID", "Attestation.Type", "Attestation.Result", "Attestation.RevokedAttestationID",
	"Attestation.UserID", "Attestation.CreatedAt",
}

var attestationAliases = map[string]string{
	"ID": "AttestationID",
}

func init() {
	addStandardListFlags(attestationListCmd)
	attestationCmd.AddCommand(attestationListCmd)
}

func attestationListCmdRun(cmd *cobra.Command, args []string) error {
	filterID, err := parseFilterFlag(filter)
	if err != nil {
		return err
	}
	w := cubapi.NewWhere(where)
	if selectedSpaceID != "*" {
		w = w.Eq("SpaceID", selectedSpaceID)
	}
	attestations, err := cubapi.ListAttestations(ctx, cubClient, w, cubapi.ListOpts{
		Select:   attestationSelectValue(),
		Filter:   filterID,
		Contains: contains,
	})
	if err != nil {
		return err
	}
	displayListResults(attestations, getAttestationID, displayAttestationList)
	return nil
}

func getAttestationID(attestation *goclientnew.ExtendedAttestation) string {
	if attestation.Attestation != nil {
		return attestation.Attestation.AttestationID.String()
	}
	return ""
}

func displayAttestationList(attestations []*goclientnew.ExtendedAttestation) {
	if displayRequestedColumns(attestations, attestationAliases, nil) {
		return
	}
	table := tableView()
	if !noheader {
		table.SetHeader([]string{"Attestation-ID", "Type", "Result", "Revokes", "User-ID", "Created"})
	}
	for _, ea := range attestations {
		a := ea.Attestation
		if a == nil {
			continue
		}
		revokes := ""
		if a.RevokedAttestationID != nil {
			revokes = a.RevokedAttestationID.String()
		}
		table.Append([]string{a.AttestationID.String(), a.Type, string(a.Result), revokes, a.UserID.String(), a.CreatedAt.String()})
	}
	table.Render()
}

func attestationSelectValue() string {
	return cubapi.SelectFields(handleSelectParameter(selectFields, selectFields, func() string {
		baseFields := []string{"AttestationID", "SpaceID", "OrganizationID"}
		return buildSelectList("Attestation", listColumnsFor("cub attestation list"), "", defaultAttestationColumns, attestationAliases, map[string][]string{}, baseFields)
	}))
}
