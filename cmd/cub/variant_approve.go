// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"fmt"
	"strings"

	"github.com/cockroachdb/errors"
	"github.com/confighub/sdk/core/cubapi"
	goclientnew "github.com/confighub/sdk/core/openapi/goclient-new"
	"github.com/google/uuid"
	"github.com/spf13/cobra"
)

// variantApproveDefaultWhere selects the Units of a variant that can be released: the ones with
// a Target, which are what a release of the space bundles. A base has none, so approving one
// takes --all.
const variantApproveDefaultWhere = "TargetID IS NOT NULL"

var variantApproveArgs struct {
	statement   attestationStatementArgs
	revision    string
	all         bool
	changeOrder string
	stage       string
	whereSpace  string
	dryRun      bool
}

var variantApproveCmd = &cobra.Command{
	Use:         "approve [<space>]",
	Short:       "Approve the units of one or more variants",
	Annotations: map[string]string{"OrgLevel": ""},
	Long: getCommandHelp(`Approve one revision of each unit in one or more variant spaces.

An approval is an attestation of type Approval, recorded per space and covering specific
revisions: the ones approved, and later revisions of the same unit with identical
content. A new revision with different content is not approved until someone approves
it. See `+"`cub attestation`"+`.

The spaces are the one named as an argument, or those --where-space selects, or those
--change-order names -- its own space and the spaces it is headed for -- narrowed to one
stage of its change workflow with --stage. These combine as an intersection.

In each space, the units approved are those with a Target -- what a release of the space
would publish -- unless --change-order or --all is given, in which case they are every
unit. --where narrows the units further. The revision of each is the head, or with
--change-order the revision its end tag marks there, or --revision: a number,
LastReleasedRevisionNum, Tag:<tag>, ChangeSet:<changeset> or ChangeOrder:<changeorder>,
optionally prefixed with Before:. A unit with no such revision is reported and skipped.

--reject records a rejection instead, and --note says why.

Examples:
`+"```"+`
  # Approve the change that has reached this variant, then release it.
  cub variant approve apptique-dev
  cub release publish apptique-dev

  # Approve only the workloads.
  cub variant approve apptique-prod --where "Slug LIKE 'deployment-%'"

  # Approve the revision a release pinned, rather than each unit's head.
  cub variant approve apptique-prod --revision LastReleasedRevisionNum

  # Approve a base, which has no targets of its own.
  cub variant approve apptique-base --all

  # Approve change order checkout-v42 as it stands in every staging space.
  cub variant approve --change-order apptique-base/checkout-v42 --stage staging

  # Reject it in one of them.
  cub variant approve apptique-staging-eu --change-order apptique-base/checkout-v42 --reject --note "breaks the EU ingress"
`+"```"+`
`, ""),
	Args: cobra.MaximumNArgs(1),
	RunE: variantApproveCmdRun,
}

func init() {
	enableWhereFlag(variantApproveCmd)
	addAttestationStatementFlags(variantApproveCmd, &variantApproveArgs.statement, false)
	variantApproveCmd.Flags().StringVar(&variantApproveArgs.revision, "revision", "",
		"revision of each unit to approve; defaults to the change order's, or the head")
	variantApproveCmd.Flags().BoolVar(&variantApproveArgs.all, "all", false,
		"approve every unit in the space, not only the ones with a Target")
	variantApproveCmd.Flags().StringVar(&variantApproveArgs.changeOrder, "change-order", "",
		"approve this change order: the spaces it is headed for, and the revisions its end tag marks")
	variantApproveCmd.Flags().StringVar(&variantApproveArgs.stage, "stage", "",
		"with --change-order, approve in the spaces of this stage of its change workflow")
	variantApproveCmd.Flags().StringVar(&variantApproveArgs.whereSpace, "where-space", "",
		"select the spaces to approve in with a where expression over spaces")
	variantApproveCmd.Flags().BoolVar(&variantApproveArgs.dryRun, "dry-run", false,
		"report what would be approved, and record nothing")
	addStandardDisplayFlags(variantApproveCmd)
	variantCmd.AddCommand(variantApproveCmd)
}

// variantApproveWhere composes the selection: what --where asked for, narrowed to
// the releasable units unless --all. Both halves are ANDed, which is the only way
// clauses combine.
func variantApproveWhere(userWhere string, all bool) string {
	if all {
		return userWhere
	}
	if userWhere == "" {
		return variantApproveDefaultWhere
	}
	return fmt.Sprintf("%s AND %s", userWhere, variantApproveDefaultWhere)
}

func variantApproveCmdRun(cmd *cobra.Command, args []string) error {
	variantApproveArgs.statement.attestationType = "Approval"
	request := goclientnew.AttestRequest{}
	var spaceClauses []string
	if len(args) == 1 {
		space, err := resolveSpace(args[0], "SpaceID,Slug")
		if err != nil {
			return err
		}
		spaceClauses = append(spaceClauses, fmt.Sprintf("SpaceID = '%s'", space.Space.SpaceID))
	} else if selectedSpaceID != "" && selectedSpaceID != "*" {
		spaceClauses = append(spaceClauses, fmt.Sprintf("SpaceID = '%s'", selectedSpaceID))
	}
	if variantApproveArgs.whereSpace != "" {
		spaceClauses = append(spaceClauses, variantApproveArgs.whereSpace)
	}
	request.WhereSpace = strings.Join(spaceClauses, " AND ")

	var changeOrderID *uuid.UUID
	if variantApproveArgs.changeOrder != "" {
		id, err := resolveChangeOrderID(variantApproveArgs.changeOrder)
		if err != nil {
			return err
		}
		changeOrderID = &id
	}
	if variantApproveArgs.stage != "" && changeOrderID == nil {
		return errors.New("--stage names a stage of a change order's workflow, so it needs --change-order")
	}
	if request.WhereSpace == "" && changeOrderID == nil {
		return errors.New("approve needs the spaces to approve in: name a space, or pass --where-space or --change-order")
	}

	statement, err := variantApproveArgs.statement.statement(changeOrderID)
	if err != nil {
		return err
	}
	revision, err := attestationRevisionParameter(variantApproveArgs.revision)
	if err != nil {
		return err
	}
	selection := statement.attestRequest()
	selection.WhereSpace = request.WhereSpace
	selection.TargetStage = variantApproveArgs.stage
	selection.Revision = revision
	// A release publishes the Units with a Target, so that is what approving for a release
	// covers. A change is approved wherever it landed, Target or not.
	selection.WhereUnit = variantApproveWhere(where, variantApproveArgs.all || changeOrderID != nil)

	result, err := cubapi.Attest(ctx, cubClient, selection, variantApproveArgs.dryRun)
	if err != nil {
		return err
	}
	var failed []string
	for i := range result.Spaces {
		space := &result.Spaces[i]
		if space.Error != nil {
			failed = append(failed, space.SpaceSlug)
			if !quiet && !isAlternativeOutput() {
				tprint("Failed to approve in %s: %s", space.SpaceSlug, space.Error.Message)
			}
			continue
		}
		displayAttestationCreateResult(space.SpaceSlug, &goclientnew.AttestationCreateResponse{
			Attestation: space.Attestation, Subjects: space.Subjects, SkippedUnits: space.SkippedUnits,
		}, variantApproveArgs.dryRun)
	}
	renderPayload(result)
	if len(failed) > 0 {
		return errors.Newf("approval failed in %d space(s): %s", len(failed), strings.Join(failed, ", "))
	}
	return nil
}
