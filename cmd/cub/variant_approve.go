// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"fmt"
	"strings"
	"time"

	"github.com/cockroachdb/errors"
	"github.com/confighub/sdk/core/cubapi"
	goclientnew "github.com/confighub/sdk/core/openapi/goclient-new"
	"github.com/google/uuid"
	"github.com/spf13/cobra"
)

// variantApproveDefaultWhere selects the Units of a variant that can be released.
// A base has no Target, so nothing in it is awaiting the approval that gates a
// release; approving there would clear gates nobody is waiting on and consume the
// approval of a revision the deployments have not taken yet.
const variantApproveDefaultWhere = "TargetID IS NOT NULL"

var variantApproveArgs struct {
	statement   attestationStatementArgs
	revision    string
	all         bool
	noWait      bool
	changeOrder string
	stage       string
	whereSpace  string
	dryRun      bool
}

// variantApproveTriggerTimeout bounds the wait for trigger evaluation to finish.
// Generous because a Trigger whose function a worker hosts is a round trip per
// unit, and a whole variant is approved at once.
const variantApproveTriggerTimeout = 2 * time.Minute

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

While spaces still gate releases with a vet-approvedby Trigger, approving a unit's head
revision also clears that Trigger's gate, and this command waits for the space's
Triggers to finish evaluating before returning, since a publish issued while evaluation
is pending fails on the transient "awaiting/triggers" gate. Pass --no-wait to return as
soon as the approvals are recorded.

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
	variantApproveCmd.Flags().BoolVar(&variantApproveArgs.noWait, "no-wait", false,
		"return as soon as the approvals are recorded, without waiting for triggers to finish evaluating")
	addStandardDisplayFlags(variantApproveCmd)
	variantCmd.AddCommand(variantApproveCmd)
}

// reportRemainingValidationErrors names the units whose gates outlived the wait. Approval
// clears the gate it answers and no other, so a unit listed here is failing something
// else -- a policy vet, a placeholder -- and the release will refuse it.
func reportRemainingValidationErrors(units []*goclientnew.Unit) {
	if quiet || isAlternativeOutput() {
		return
	}
	for _, unit := range units {
		if unit == nil || len(unit.ValidationErrors) == 0 {
			continue
		}
		tprint("Unit %s (%s) has validation errors: %s",
			unit.Slug, unit.UnitID.String(), validationErrorsToString(unit.ValidationErrors))
	}
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
	if variantApproveArgs.noWait || variantApproveArgs.dryRun || variantApproveArgs.statement.reject {
		return nil
	}
	for i := range result.Spaces {
		space := &result.Spaces[i]
		if space.Attestation == nil || len(space.Subjects) == 0 {
			continue
		}
		selectedSpaceID = space.SpaceID.String()
		if err := variantApproveWaitTriggers(unitIDsWhere(space.Subjects)); err != nil {
			return err
		}
	}
	return nil
}

// unitIDsWhere selects the Units an attestation covered.
func unitIDsWhere(subjects []goclientnew.AttestationSubject) string {
	ids := make([]string, 0, len(subjects))
	for _, subject := range subjects {
		ids = append(ids, "'"+subject.UnitID.String()+"'")
	}
	return "UnitID IN (" + strings.Join(ids, ", ") + ")"
}

// variantApproveWaitTriggers waits for the approved units to finish trigger
// evaluation. Triggers run asynchronously on every Mutation, and while one is
// pending the unit carries an "awaiting/triggers" ApplyGate; "cub release publish"
// refuses to bundle a unit with any gate, so a publish issued straight after an
// approval fails on that transient gate rather than on a real verdict. Same reason
// "cub cluster up" waits after its own mutations.
//
// This polls the whole selection rather than calling awaitTriggersRemoval per unit,
// which is what the rest of the CLI does (including the bulk patch in unit_update).
// That helper reads one unit per request, so waiting on a variant would cost a fetch
// of every unit up front and a poll loop per unit that is still pending -- sequential,
// and proportional to the size of the variant. One list query selecting just the gates
// answers for all of them at once, which is the difference between a handful of
// requests and a few hundred on a 36-unit variant.
//
// What it keeps from that helper: a unit still carrying a gate when the wait ends is
// reported, because that gate is a real verdict rather than a transient one. This
// waits for evaluation to finish, not for it to pass -- enforcing the verdict is the
// server's job.
func variantApproveWaitTriggers(selectionWhere string) error {
	deadline := time.Now().Add(variantApproveTriggerTimeout)
	backoff := 500 * time.Millisecond
	for {
		units, err := apiListUnits(selectedSpaceID, selectionWhere, "UnitID,Slug,ValidationErrors")
		if err != nil {
			return err
		}
		pending := 0
		for _, unit := range units {
			if unit == nil {
				continue
			}
			if _, awaiting := unit.ValidationErrors["awaiting/triggers"]; awaiting {
				pending++
			}
		}
		if pending == 0 {
			reportRemainingValidationErrors(units)
			return nil
		}
		if time.Now().After(deadline) {
			return errors.Newf("%d unit(s) still awaiting trigger evaluation after %s",
				pending, variantApproveTriggerTimeout)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(backoff):
		}
		if backoff < 2*time.Second {
			backoff *= 2
		}
	}
}
