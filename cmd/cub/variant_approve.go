// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"fmt"
	"time"

	"github.com/cockroachdb/errors"
	goclientnew "github.com/confighub/sdk/core/openapi/goclient-new"
	"github.com/spf13/cobra"
)

// variantApproveDefaultWhere selects the Units of a variant that can be released.
// A base has no Target, so nothing in it is awaiting the approval that gates a
// release; approving there would clear gates nobody is waiting on and consume the
// approval of a revision the deployments have not taken yet.
const variantApproveDefaultWhere = "TargetID IS NOT NULL"

var variantApproveArgs struct {
	revision string
	all      bool
	noWait   bool
}

// variantApproveTriggerTimeout bounds the wait for trigger evaluation to finish.
// Generous because a Trigger whose function a worker hosts is a round trip per
// unit, and a whole variant is approved at once.
const variantApproveTriggerTimeout = 2 * time.Minute

var variantApproveCmd = &cobra.Command{
	Use:         "approve [<space>]",
	Short:       "Approve the deployable units of a variant",
	Annotations: map[string]string{"OrgLevel": ""},
	Long: getCommandHelp(`Approve every unit of a variant space that can be released.

Approval is per unit and per revision, so approving a variant that a change has just
reached otherwise means naming its units. This approves the whole variant in one step:
the review is of the change that arrived, and the change arrived across the space.

By default the units with a Target are approved -- what a release of this space would
publish. A base has no Target, so "`+variantApproveDefaultWhere+`" selects nothing
there; pass --all to approve regardless, which is what a base being reviewed before its
deployments take the change wants.

--where narrows the selection further, ANDed with the default. --revision approves a
revision other than each unit's head, which is what a review of an already-superseded
change needs; it takes the same forms "cub unit approve --revision" does.

A vet-approvedby Trigger is what makes approval load-bearing: it attaches an ApplyGate
to every revision with too few approvals, and "cub release publish" refuses while a gate
is on. Approving clears the gate for the revision approved and no other, so a later
change is gated again without anyone re-arming anything.

Approving waits for the space's Triggers to finish evaluating before returning, because
publishing is what usually comes next and a publish issued while evaluation is pending
fails on the transient "awaiting/triggers" gate rather than on anything real. Pass
--no-wait to return as soon as the approvals are recorded.

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
`+"```"+`
`, ""),
	Args: cobra.MaximumNArgs(1),
	RunE: variantApproveCmdRun,
}

func init() {
	enableWhereFlag(variantApproveCmd)
	variantApproveCmd.Flags().StringVar(&variantApproveArgs.revision, "revision", "",
		"revision to approve (defaults to each unit's head); a number, LastReleasedRevisionNum, Tag:slug, or ChangeSet:slug")
	variantApproveCmd.Flags().BoolVar(&variantApproveArgs.all, "all", false,
		"approve every unit in the space, not only the ones with a Target")
	variantApproveCmd.Flags().BoolVar(&variantApproveArgs.noWait, "no-wait", false,
		"return as soon as the approvals are recorded, without waiting for triggers to finish evaluating")
	addStandardDisplayFlags(variantApproveCmd)
	variantCmd.AddCommand(variantApproveCmd)
}

// reportRemainingApplyGates names the units whose gates outlived the wait. Approval
// clears the gate it answers and no other, so a unit listed here is failing something
// else -- a policy vet, a placeholder -- and the release will refuse it.
func reportRemainingApplyGates(units []*goclientnew.Unit) {
	if quiet || isAlternativeOutput() {
		return
	}
	for _, unit := range units {
		if unit == nil || len(unit.ApplyGates) == 0 {
			continue
		}
		tprint("Unit %s (%s) has apply gates: %s",
			unit.Slug, unit.UnitID.String(), applyGatesToString(unit.ApplyGates))
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
	spaceSlug := ""
	if len(args) == 1 {
		spaceSlug = args[0]
	} else if selectedSpaceSlug != "" && selectedSpaceSlug != "*" {
		spaceSlug = selectedSpaceSlug
	}
	if spaceSlug == "" {
		return errors.New("approve needs the variant space to approve, either as an argument or as the selected space")
	}

	space, err := apiGetSpaceFromSlug(spaceSlug, "SpaceID,Slug")
	if err != nil {
		return err
	}
	// As "variant promote" does: this command names its space positionally, so the
	// selected space may be unset or "*". Point it at the space being approved.
	selectedSpaceID = space.SpaceID.String()
	selectedSpaceSlug = space.Slug

	revisionParam, err := parseApproveRevisionParameter(variantApproveArgs.revision)
	if err != nil {
		return err
	}

	// The selection is kept without the space clause as well: the bulk API wants it
	// included, and apiListUnits adds its own.
	selectionWhere := variantApproveWhere(where, variantApproveArgs.all)
	effectiveWhere := addSpaceIDToWhereClause(selectionWhere, selectedSpaceID)

	if !quiet && !isAlternativeOutput() {
		tprint("Approving units in %s", space.Slug)
	}
	if err := bulkApproveUnits(effectiveWhere, "", revisionParam); err != nil {
		return err
	}
	if variantApproveArgs.noWait {
		return nil
	}
	return variantApproveWaitTriggers(selectionWhere)
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
		units, err := apiListUnits(selectedSpaceID, selectionWhere, "UnitID,Slug,ApplyGates")
		if err != nil {
			return err
		}
		pending := 0
		for _, unit := range units {
			if unit == nil {
				continue
			}
			if _, awaiting := unit.ApplyGates["awaiting/triggers"]; awaiting {
				pending++
			}
		}
		if pending == 0 {
			reportRemainingApplyGates(units)
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
