// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"fmt"
	"strings"

	"github.com/cockroachdb/errors"
	"github.com/confighub/sdk/core/cubapi"
	goclientnew "github.com/confighub/sdk/core/openapi/goclient-new"
	"github.com/spf13/cobra"
)

var variantDemoteArgs struct {
	changeDescription string
	changeorderSlug   string
	changesetSlug     string
	whereSpace        string
	filterSpace       string
	targetStage       string
	expectedPlan      string
	dryRun            bool
}

// What the server reports it did, or would do, to a Space and a Unit.
const (
	demoteSpaceActionSkipped = "Skipped"
	demoteSpaceActionFailed  = "Failed"

	demoteUnitActionRestore   = "Restore"
	demoteUnitActionMark      = "Mark"
	demoteUnitActionUnchanged = "Unchanged"
)

var variantDemoteCmd = &cobra.Command{
	Use:         "demote [<space>]",
	Short:       "Undo a change order, restoring the revisions before it",
	Annotations: map[string]string{"OrgLevel": ""},
	Long: getCommandHelp(`Undo a change order in the spaces it reached, restoring each unit it marked to the
revision that unit was at before the change.

Aborting a change order says the change is not coming to the spaces still waiting for it. It
changes nothing about the ones that already took it, which is what leaves a fleet part-way
through a change nobody is going to finish. Demote is what takes it back out.

The spaces are the named space, or those --where-space, --filter-space and --target-stage select
among the spaces the change order reached. With none of them, demote takes the change order out
of every space it reached, including the one it was made in. A selected space it never reached is
reported and left alone.

The change order must have an AbortedReason. Setting it is the decision that the change is not
coming; undoing one nobody has said that about is a race with whoever is still promoting it.

Which units are restored is the change order's answer rather than a selection of your own: the
units of each space its start tag marks. A unit it covered but carried no changes for is marked
as restored without a revision being made -- there is nothing of it here to undo, and the
revisions after it, if any, are somebody else's change.

Undoing a change order in a unit happens once, as promoting it into one does: a unit already
carrying the restore tag is left alone, so a demote that landed partway is run again to reach the
units it did not, and a unit edited forward since it was undone keeps that work.

Each restore is a new revision holding the earlier content, not an unwinding of the ones between,
so the history of what happened stays intact. The change order's own tags stay where they are: the
space took the change, whatever has happened since, and the restore tag is what says it was taken
back out again. "cub changeorder get" reports where that has reached, as Restored Spaces, and the
change order's State reads Restored once every space that took it has been, and RestoreReleased
once every space that had released it has released the restored revisions.

The change order is named the way any entity in another space is: a bare slug resolves in the space
being demoted, or in the selected space when no space is named, and a <space>/<slug> or a UUID names
one anywhere else -- which is what a variant's is, since it lives upstream, possibly several hops
up.

Demote does not promote the restored revisions onward. Each space is restored on its own account,
because what it goes back to has to be released where the change was released. What demote does do is advance the merge pointers of the links that
follow this space's units onto the restored revisions, so that a later upgrade does not replay the
change that was just taken out.

A change order with UpdateType Invoke is undone the same way, since what a restore reads is the
tags the change left and those are the same tags. The one difference is that nothing carried it:
the change was made in place rather than merged from an upstream, so no link records it and none
is advanced.

Publishing is separate, as it is for promotion: "cub release publish" is what takes the restored
revisions to a cluster.

A unit whose head has moved past where the change order ended has changes the restore will drop,
and they are reported, by --dry-run before anything is written. To apply exactly what a dry run
showed, pass its Plan (-o jq=.Plan) as --expected-plan: nothing is written if the demotion would
now do anything different. Nothing re-applies them: the restored revisions
are expected to be released first, and re-applying them is a forward change to make afterwards.

Examples:
`+"```"+`
  # Undo an aborted change order in the space it was made in
  cub variant demote web-base --change-order release-42

  # Report what would be restored, and what later changes it would drop
  cub variant demote web-base --change-order release-42 --dry-run

  # Undo it in a variant, naming the change order in the space it was made in
  cub variant demote web-prod --change-order web-base/release-42 --change-desc "back out 1.42"

  # Undo it everywhere it reached
  cub variant demote --change-order web-base/release-42

  # Undo it in the spaces of one stage of its change workflow
  cub variant demote --change-order web-base/release-42 --target-stage prod
`+"```"+`
`, ""),
	Args: cobra.MaximumNArgs(1),
	RunE: variantDemoteCmdRun,
}

func init() {
	variantDemoteCmd.Flags().StringVar(&variantDemoteArgs.changeorderSlug, "change-order", "", "change order to undo (required): a bare slug resolves in the space being demoted, which is where it resides only when that space is where the change was made; a variant's is <space>/<slug> or a UUID")
	variantDemoteCmd.Flags().StringVar(&variantDemoteArgs.changeDescription, "change-desc", "", "change description recorded on the restored revisions")
	variantDemoteCmd.Flags().StringVar(&variantDemoteArgs.changesetSlug, "changeset", "", "changeset to record the restored revisions in")
	variantDemoteCmd.Flags().StringVar(&variantDemoteArgs.whereSpace, "where-space", "", "where expression narrowing the spaces the change order reached, instead of naming one")
	variantDemoteCmd.Flags().StringVar(&variantDemoteArgs.filterSpace, "filter-space", "", "filter over spaces narrowing the spaces the change order reached")
	variantDemoteCmd.Flags().StringVar(&variantDemoteArgs.targetStage, "target-stage", "", "stage of the change order's ChangeWorkflow whose spaces to demote")
	variantDemoteCmd.Flags().StringVar(&variantDemoteArgs.expectedPlan, "expected-plan", "", "the Plan a --dry-run of the same demotion returned; nothing is written if the demotion would now do anything different")
	variantDemoteCmd.Flags().BoolVar(&variantDemoteArgs.dryRun, "dry-run", false, "report the units that would be restored, and the later revisions the restore would drop, without changing anything")
	addStandardDisplayFlags(variantDemoteCmd)
	variantCmd.AddCommand(variantDemoteCmd)
}

func variantDemoteCmdRun(cmd *cobra.Command, args []string) error {
	if variantDemoteArgs.changeorderSlug == "" {
		return errors.New("demote needs --change-order: what it undoes is one named change, and which units that is comes from the change order")
	}
	if len(args) > 0 && (variantDemoteArgs.whereSpace != "" || variantDemoteArgs.filterSpace != "") {
		return errors.New("name the space to demote or select spaces with --where-space and --filter-space, not both")
	}
	req := goclientnew.DemoteRequest{
		ChangeDescription: variantDemoteArgs.changeDescription,
		WhereSpace:        variantDemoteArgs.whereSpace,
		TargetStage:       variantDemoteArgs.targetStage,
		ExpectedPlan:      variantDemoteArgs.expectedPlan,
	}

	// A change order resides in the space the change was made in, which is the space being
	// demoted only when the source of the change is what is being undone. A variant's is upstream
	// of it, and may be several hops upstream, so it is named the way every other entity in
	// another space is: <space>/<slug>, or by UUID. A bare slug resolves in the space named, or
	// in the selected space.
	if len(args) > 0 {
		space, err := resolveSpace(args[0], "*")
		if err != nil {
			return err
		}
		req.WhereSpace = fmt.Sprintf("SpaceID = '%s'", space.Space.SpaceID)
		// The helpers that render mutations resolve units through the selected space.
		selectedSpaceID = space.Space.SpaceID.String()
		selectedSpaceSlug = space.Space.Slug
	}
	changeOrder, err := changeOrderByRef(variantDemoteArgs.changeorderSlug)
	if err != nil {
		return err
	}
	req.ChangeOrderID = changeOrder.ChangeOrderID
	if variantDemoteArgs.filterSpace != "" {
		filterID, err := resolveFilterID(variantDemoteArgs.filterSpace)
		if err != nil {
			return err
		}
		req.SpaceFilterID = &filterID
	}
	if variantDemoteArgs.changesetSlug != "" {
		changeSetID, err := resolveChangeSetID(variantDemoteArgs.changesetSlug)
		if err != nil {
			return errors.Wrap(err, "failed to get changeset")
		}
		req.ChangeSetID = &changeSetID
	}

	var with []func(*goclientnew.DemoteParams)
	if shouldDisplayMutations() {
		with = append(with, cubapi.WithDemoteMutations)
	}
	result, err := cubapi.Demote(ctx, cubClient, req, variantDemoteArgs.dryRun, with...)
	if err != nil {
		return err
	}
	if !renderPayload(result) {
		displayDemoteResult(result, changeOrder.Slug)
	}
	// Demote waits for the triggers of what it wrote, since publishing the restored revisions is
	// what comes next. A dry run changed nothing, so there is nothing to wait for.
	if !variantDemoteArgs.dryRun {
		if err := awaitDemotedUnits(result); err != nil {
			return err
		}
	}
	return demoteResultError(result, len(args) > 0)
}

// displayDemoteResult prints what the demotion did, or on a dry run would do, space by space.
func displayDemoteResult(result *goclientnew.DemoteResult, changeOrderSlug string) {
	showText := !isAlternativeOutput()
	dryRun := variantDemoteArgs.dryRun
	for i := range result.Spaces {
		space := &result.Spaces[i]
		if space.Action == demoteSpaceActionFailed {
			continue
		}
		if space.Action == demoteSpaceActionSkipped {
			if showText {
				tprint("Change order %s marks no unit of %s, so there is nothing to restore there", changeOrderSlug, space.SpaceSlug)
			}
			continue
		}
		if showText {
			if len(result.Spaces) > 1 {
				tprint("Demoting %s...", space.SpaceSlug)
			}
			displayDemoteSpaceSummary(space, changeOrderSlug, dryRun)
		}
		if shouldDisplayMutations() {
			displayDemoteSpaceMutations(space, dryRun)
		}
	}
}

// displayDemoteSpaceSummary prints one space's units by what was done to them, and the later
// changes the restores drop.
func displayDemoteSpaceSummary(space *goclientnew.DemoteSpaceResult, changeOrderSlug string, dryRun bool) {
	counts := map[string]int{}
	var dropped []string
	for _, unit := range space.Units {
		counts[unit.Action]++
		if unit.Error != nil {
			tprint("Failed to restore unit %s", unit.Slug)
			displayResponseError(unit.Error)
		}
		switch {
		case unit.DropsFromRevisionNum == 0:
		case unit.DropsFromRevisionNum == unit.DropsToRevisionNum:
			dropped = append(dropped, fmt.Sprintf("  %s: revision %d", unit.Slug, unit.DropsFromRevisionNum))
		default:
			dropped = append(dropped, fmt.Sprintf("  %s: revisions %d-%d", unit.Slug, unit.DropsFromRevisionNum, unit.DropsToRevisionNum))
		}
	}
	if n := counts[demoteUnitActionUnchanged]; n > 0 {
		tprint("Leaving %d unit(s) of %s alone: change order %s has already been taken back out of them",
			n, space.SpaceSlug, changeOrderSlug)
	}
	restored, marked := counts[demoteUnitActionRestore], counts[demoteUnitActionMark]
	if restored+marked == 0 {
		tprint("Nothing left to restore in %s", space.SpaceSlug)
		return
	}
	if len(dropped) > 0 {
		verb := "drops"
		if dryRun {
			verb = "would drop"
		}
		tprint("Warning: restoring %s changes made after the change order, which nothing re-applies:\n%s",
			verb, strings.Join(dropped, "\n"))
	}
	verb := "Restored"
	if dryRun {
		verb = "Would restore"
	}
	tprint("%s %d unit(s) to the revisions before change order %s", verb, restored, changeOrderSlug)
	if marked > 0 {
		verb = "Marked"
		if dryRun {
			verb = "Would mark"
		}
		tprint("%s %d unit(s) the change order carried nothing for as restored, without a new revision", verb, marked)
	}
}

// displayDemoteSpaceMutations prints the mutations each restore made, or would make.
func displayDemoteSpaceMutations(space *goclientnew.DemoteSpaceResult, dryRun bool) {
	// The helpers that fetch prior values resolve units through the selected space.
	selectedSpaceID = space.SpaceID.String()
	selectedSpaceSlug = space.SpaceSlug
	for i := range space.Units {
		unit := &space.Units[i]
		if unit.Error != nil || unit.Mutations == nil {
			continue
		}
		tprintRaw(fmt.Sprintf("Mutations for unit %s:", unit.Slug))
		lookupMutationsUnitID = unit.UnitID.String()
		lookupMutationsSpaceID = space.SpaceID.String()
		priorRevision := "dry-run"
		if !dryRun {
			priorRevision = fmt.Sprintf("%s/%d", unit.Slug, unit.PreviousHeadRevisionNum)
		}
		displayResourceMutationList(unit.Mutations, true, unit.PreviousHeadMutationNum, "restore", priorRevision)
	}
}

// awaitDemotedUnits waits for the triggers of every unit the demotion restored.
func awaitDemotedUnits(result *goclientnew.DemoteResult) error {
	for i := range result.Spaces {
		space := &result.Spaces[i]
		for _, unit := range space.Units {
			if unit.Error != nil || unit.Action != demoteUnitActionRestore {
				continue
			}
			unitDetails, err := resolveUnit(unit.UnitID.String(), space.SpaceID.String(), "*")
			if err != nil {
				return err
			}
			if err := awaitTriggersRemoval(unitDetails.Unit); err != nil {
				return err
			}
		}
	}
	return nil
}

// demoteResultError fails the command when anything the demotion tried did not land, naming what
// failed.
func demoteResultError(result *goclientnew.DemoteResult, namedSpace bool) error {
	var errs []error
	for i := range result.Spaces {
		space := &result.Spaces[i]
		if space.Error != nil {
			errs = append(errs, errors.Newf("failed to demote %s: %s", space.SpaceSlug, space.Error.Message))
		}
		for _, unit := range space.Units {
			if unit.Error != nil {
				errs = append(errs, errors.Newf("failed to restore unit %s of %s: %s", unit.Slug, space.SpaceSlug, unit.Error.Message))
			}
		}
	}
	if len(errs) == 0 {
		return nil
	}
	if namedSpace || len(result.Spaces) == 1 {
		return errors.Join(errs...)
	}
	return errors.Wrapf(errors.Join(errs...), "demoting %d space(s)", len(result.Spaces))
}
