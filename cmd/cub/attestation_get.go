// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"fmt"
	"sort"
	"strings"

	goclientnew "github.com/confighub/sdk/core/openapi/goclient-new"
	"github.com/google/uuid"
	"github.com/spf13/cobra"
)

var attestationGetCmd = &cobra.Command{
	Use:   "get <attestation-id>",
	Short: "Get details about an attestation",
	Long: getCommandHelp(`Get details about an attestation, from whichever space holds it.

Examples:
`+"```"+`
  cub attestation get 61f26b06-3c34-4363-8b9d-7d0a7c2b5f1c
  cub attestation get 61f26b06-3c34-4363-8b9d-7d0a7c2b5f1c -o json
`+"```"+`
`, ""),
	Args: cobra.ExactArgs(1),
	RunE: attestationGetCmdRun,
}

func init() {
	addStandardGetFlags(attestationGetCmd)
	enableOptionalSpace(attestationGetCmd)
	attestationCmd.AddCommand(attestationGetCmd)
}

func attestationGetCmdRun(cmd *cobra.Command, args []string) error {
	id, err := uuid.Parse(args[0])
	if err != nil {
		return fmt.Errorf("invalid attestation id %q: %w", args[0], err)
	}
	attestation, err := apiFindAttestation(id)
	if err != nil {
		return err
	}
	displayGetResults(attestation, displayAttestationDetails)
	return nil
}

func displayAttestationDetails(ea *goclientnew.ExtendedAttestation) {
	view := tableView()
	a := ea.Attestation
	if a != nil {
		view.Append([]string{"ID", a.AttestationID.String()})
		view.Append([]string{"Type", a.Type})
		view.Append([]string{"Result", string(a.Result)})
		if a.RevokedAttestationID != nil {
			view.Append([]string{"Revokes", a.RevokedAttestationID.String()})
		}
		if a.ChangeOrderID != nil {
			view.Append([]string{"Change Order ID", a.ChangeOrderID.String()})
		}
		if a.ReleaseID != nil {
			view.Append([]string{"Release ID", a.ReleaseID.String()})
		}
		if len(a.EvidenceAttestationIDs) > 0 {
			evidence := make([]string, 0, len(a.EvidenceAttestationIDs))
			for _, id := range a.EvidenceAttestationIDs {
				evidence = append(evidence, id.String())
			}
			view.Append([]string{"Evidence", strings.Join(evidence, ",")})
		}
		view.Append([]string{"Note", a.Note})
		if len(a.Claims) > 0 {
			keys := make([]string, 0, len(a.Claims))
			for key := range a.Claims {
				keys = append(keys, key)
			}
			sort.Strings(keys)
			claims := make([]string, 0, len(keys))
			for _, key := range keys {
				claims = append(claims, key+"="+a.Claims[key])
			}
			view.Append([]string{"Claims", strings.Join(claims, ",")})
		}
		if !a.ExpiresAt.IsZero() {
			view.Append([]string{"Expires At", a.ExpiresAt.String()})
		}
		view.Append([]string{"User ID", a.UserID.String()})
		view.Append([]string{"Created At", a.CreatedAt.String()})
	}
	if ea.Space != nil {
		view.Append([]string{"Space", ea.Space.Slug})
	} else if a != nil {
		view.Append([]string{"Space ID", a.SpaceID.String()})
	}
	view.Render()
}
