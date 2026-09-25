// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"fmt"

	"github.com/confighub/sdk/core/cubapi"
	goclientnew "github.com/confighub/sdk/core/openapi/goclient-new"
	"github.com/google/uuid"
	"github.com/spf13/cobra"
)

var attestationRevokeNote string

var attestationRevokeCmd = &cobra.Command{
	Use:   "revoke <attestation-id>",
	Short: "Withdraw an attestation",
	Long: getCommandHelp(`Withdraw an attestation, so that it no longer satisfies requirements.

The attestation is not changed or deleted: a revocation is recorded beside it, naming
it, so the record of what was claimed and later withdrawn is kept. Only the user who
recorded an attestation, or a user who manages its space, can revoke it.

To change an approval into a rejection, revoke it and record the rejection.

Examples:
`+"```"+`
  cub attestation revoke 61f26b06-3c34-4363-8b9d-7d0a7c2b5f1c --note "approved the wrong change"
`+"```"+`
`, ""),
	Args: cobra.ExactArgs(1),
	RunE: attestationRevokeCmdRun,
}

func init() {
	attestationRevokeCmd.Flags().StringVar(&attestationRevokeNote, "note", "", "why the attestation is withdrawn")
	addStandardDisplayFlags(attestationRevokeCmd)
	enableOptionalSpace(attestationRevokeCmd)
	attestationCmd.AddCommand(attestationRevokeCmd)
}

func attestationRevokeCmdRun(cmd *cobra.Command, args []string) error {
	id, err := uuid.Parse(args[0])
	if err != nil {
		return fmt.Errorf("invalid attestation id %q: %w", args[0], err)
	}
	revoked, err := apiFindAttestation(id)
	if err != nil {
		return err
	}
	request := goclientnew.AttestationCreateRequest{
		Type:                 revoked.Attestation.Type,
		Note:                 attestationRevokeNote,
		RevokedAttestationID: &id,
	}
	result, err := cubapi.CreateAttestation(ctx, cubClient, revoked.Attestation.SpaceID, request, false)
	if err != nil {
		return err
	}
	spaceSlug := revoked.Attestation.SpaceID.String()
	if revoked.Space != nil {
		spaceSlug = revoked.Space.Slug
	}
	displayAttestationCreateResult(spaceSlug, result, false)
	renderPayload(result)
	return nil
}
