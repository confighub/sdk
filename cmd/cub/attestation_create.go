// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"github.com/confighub/sdk/core/cubapi"
	"github.com/google/uuid"
	"github.com/spf13/cobra"
)

var attestationCreateArgs struct {
	statement   attestationStatementArgs
	revision    string
	changeOrder string
	dryRun      bool
}

var attestationCreateCmd = &cobra.Command{
	Use:   "create",
	Short: "Record an attestation about revisions in a space",
	Long: getCommandHelp(`Record an attestation about one revision of each unit in a space.

The units are those --where selects, every unit in the space by default. The revision
of each is named by --revision in the form the other unit commands take: a number,
HeadRevisionNum, LastReleasedRevisionNum, Tag:<tag>, ChangeSet:<changeset> or
ChangeOrder:<changeorder>, optionally prefixed with Before:. With --change-order and no
--revision, it is the revision the change order's end tag marks; otherwise it is the
head. A unit with no such revision is reported as skipped rather than covered at some
other revision, and when no unit has one, nothing is recorded.

To approve the units of one or more spaces, `+"`cub variant approve`"+` is shorter.

Examples:
`+"```"+`
  # Record that a security review of the head revisions passed
  cub attestation create --space payments-prod --type SecurityReview --note "reviewed network policy"

  # Record a change record from another system against what change order checkout-v42 brought
  cub attestation create --space payments-prod --type ChangeRecord \
    --change-order payments-base/checkout-v42 --claim servicenow.com/change=CHG0012345

  # Reject the revisions tagged v1.2.0
  cub attestation create --space payments-prod --revision Tag:v1.2.0 --reject --note "breaks the ingress"
`+"```"+`
`, ""),
	Args: cobra.NoArgs,
	RunE: attestationCreateCmdRun,
}

func init() {
	enableWhereFlag(attestationCreateCmd)
	addAttestationStatementFlags(attestationCreateCmd, &attestationCreateArgs.statement, true)
	attestationCreateCmd.Flags().StringVar(&attestationCreateArgs.revision, "revision", "", "the revision of each unit to attest to; defaults to the change order's, or the head")
	attestationCreateCmd.Flags().StringVar(&attestationCreateArgs.changeOrder, "change-order", "", "the change order the attestation is made in the context of")
	attestationCreateCmd.Flags().BoolVar(&attestationCreateArgs.dryRun, "dry-run", false, "report what would be covered, and record nothing")
	addStandardDisplayFlags(attestationCreateCmd)
	attestationCmd.AddCommand(attestationCreateCmd)
}

func attestationCreateCmdRun(cmd *cobra.Command, args []string) error {
	if err := validateSpaceFlag(false); err != nil {
		return err
	}
	var changeOrderID *uuid.UUID
	if attestationCreateArgs.changeOrder != "" {
		id, err := resolveChangeOrderID(attestationCreateArgs.changeOrder)
		if err != nil {
			return err
		}
		changeOrderID = &id
	}
	statement, err := attestationCreateArgs.statement.statement(changeOrderID)
	if err != nil {
		return err
	}
	revision, err := attestationRevisionParameter(attestationCreateArgs.revision)
	if err != nil {
		return err
	}
	request := statement.createRequest()
	request.WhereUnit = where
	request.Revision = revision

	result, err := cubapi.CreateAttestation(ctx, cubClient, uuid.MustParse(selectedSpaceID), request, attestationCreateArgs.dryRun)
	if err != nil {
		return err
	}
	displayAttestationCreateResult(selectedSpaceSlug, result, attestationCreateArgs.dryRun)
	renderPayload(result)
	return nil
}
