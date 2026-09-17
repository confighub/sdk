// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"fmt"

	"github.com/cockroachdb/errors"
	"github.com/confighub/sdk/core/cubapi"
	goclientnew "github.com/confighub/sdk/core/openapi/goclient-new"
	"github.com/spf13/cobra"
)

// What the server reports it did, or would do, to a Space, a Unit, and a Link.
const (
	promoteSpaceActionUnchanged = "Unchanged"
	promoteUnitActionUnchanged  = "Unchanged"
	promoteSpaceActionSkipped   = "Skipped"
	promoteSpaceActionBlocked   = "Blocked"
	promoteSpaceActionFailed    = "Failed"

	promoteUnitActionUpgrade = "Upgrade"
	promoteUnitActionMark    = "Mark"
	promoteUnitActionEmpty   = "Empty"
	promoteUnitActionRevive  = "Revive"
	promoteUnitActionClone   = "Clone"
	promoteUnitActionInvoke  = "Invoke"
	promoteUnitActionSkip    = "Skip"

	promoteLinkActionCreate = "Create"
)

// promoteChangeOrderSlug is the slug of the change order being promoted, for messages.
var promoteChangeOrderSlug string

var variantPromoteArgs struct {
	changeDescription string
	changesetSlug     string
	changeorderSlug   string
	targetStage       string
	whereSpace        string
	filterSpace       string
	dryRun            bool
	squash            bool
	force             bool
	forceReason       string
}

var variantPromoteCmd = &cobra.Command{
	Use:         "promote [<space>]",
	Short:       "Promote a variant space to match its upstream space",
	Annotations: map[string]string{"OrgLevel": ""},
	Long: getCommandHelp(`Promote variant spaces to match changes in their upstream spaces.

A space must have been created by "cub variant create", which stamps an "UpstreamSpaceID"
annotation recording the upstream space it was cloned from. The server reconciles each variant
with that upstream:

  1. Upgrade every unit whose upstream unit has advanced, merging the upstream changes. A unit
     whose upstream was emptied is emptied, and an empty unit whose upstream has content again is
     revived.
  2. Clone any units added to the upstream space since the variant was created or last promoted,
     linking each clone to its upstream unit. When the variant space has a Namespace label, which
     "cub variant create --namespace" sets, set-namespace places the clones, and revived units,
     in that namespace.
  3. Copy the new units' non-UpgradeUnit links, retargeting a link to its downstream copy when it
     points at another unit in the upstream space. A link that already points into this variant
     is left alone rather than copied into it.

Promoting again changes nothing. Use --dry-run to preview what would be upgraded and added, and
add -o mutations to see the changes.

The spaces to promote are the positional space, or those --where-space and --filter-space select,
or those a change order is headed for. Selectors combine: naming a stage and --where-space
promotes the spaces of that stage the expression selects. Spaces are promoted in upstream order,
so a variant is promoted after a selected variant it takes from.

An upgrade re-runs the upstream's recorded function invocations against each unit where it
can -- so a change lands where the unit's own structure puts it -- and records one revision per
upstream revision that has an effect, carrying that revision's change description. --squash gives
up both: the range arrives as one rebased diff in one revision.

--change-order promotes a named change rather than everything the upstream has reached. The
change order fixed its range when it was created, so the upgrade stops where it ends, a unit
it does not cover is passed over, and a unit that is not where it starts is an error rather
than a merge of a different range. A unit it covers but has no changes for is marked and not
changed: its start and end tags land on the same revision. That is what lets
"cub release publish --revision ChangeOrder:<slug>" pin every unit of the space. Three things
follow from what a change order is:

  - Units are cloned first, at the revision the change order starts from, and then upgraded
    through the change with everything else. A unit created upstream after the change order was
    fixed is outside it: those are listed rather than cloned, and promoting without
    --change-order adds them.
  - The promotion is undone in one step, however many revisions it made:
    "cub variant demote <space> --change-order <space>/<change-order>", once the change order
    has an AbortedReason saying the change is not coming.
  - Promoting is idempotent. A unit already carrying the change order's end tag has taken the
    change and is passed over, so running a promotion that landed partway again finishes it. An
    aborted change order is not promoted anywhere.

A change order with UpdateType Invoke propagates differently and reads the same. Its change is one
invocation, run in each space rather than merged from an upstream: nothing is cloned, and the
space need not be a variant of anything.

A change order created under a ChangeWorkflow is promoted through its stages, and the server
enforces each stage's entry gates -- evaluated over every space of the stage ahead of it -- for
every client. Without a space or selector, --change-order advances the change into the next
stage it has not reached; --target-stage names the stage instead. Naming a stage says where the
change is headed, not that it may skip what precedes it. --force promotes past gates that do not
hold, requires --force-reason, and is recorded on the change order.

A stage is promoted one variant at a time and can land partway. A variant that fails is reported
and the ones after it are still promoted. Running the stage again repairs it.

Examples:
`+"```"+`
  # Promote a variant to match its upstream
  cub variant promote web-prod

  # Preview the changes, including the mutations
  cub variant promote web-prod --dry-run -o mutations

  # Promote every variant of a component
  cub variant promote --where-space "Labels.Component = 'web' AND Labels.Variant != 'base'"

  # Promote, recording the whole range as one revision per unit
  cub variant promote web-prod --squash

  # Promote one named change, leaving later upstream changes behind
  cub variant promote web-prod --change-order release-42

  # Promote a change that is an invocation rather than an upstream merge
  cub variant promote web-prod --change-order platform/bump-api-image

  # Promote that change into every variant of a stage of its change workflow
  cub variant promote --change-order web-base/release-42 --target-stage staging

  # Preview what promoting the whole stage would do
  cub variant promote --change-order web-base/release-42 --target-stage staging --dry-run

  # Advance the change to the next stage it has not reached
  cub variant promote --change-order web-base/release-42

  # Promote past a stage's gates, recording why
  cub variant promote --change-order web-base/release-42 --target-stage prod --force --force-reason "incident 1234"

  # Promote within a changeset, with a change description
  cub variant promote web-prod --changeset release-2024-06 --change-desc "Promote to prod"
`+"```"+`
`, ""),
	Args: cobra.MaximumNArgs(1),
	RunE: variantPromoteCmdRun,
}

func init() {
	variantPromoteCmd.Flags().StringVar(&variantPromoteArgs.changeDescription, "change-desc", "", "change description recorded on the upgraded and cloned units")
	variantPromoteCmd.Flags().StringVar(&variantPromoteArgs.changesetSlug, "changeset", "", "changeset to associate the upgraded and cloned units with")
	variantPromoteCmd.Flags().StringVar(&variantPromoteArgs.changeorderSlug, "change-order", "", "change order to promote instead of everything the upstream has reached: it supplies the range, units it does not cover are passed over, and a unit that is not where it starts is an error. A bare slug resolves in the upstream space of the space being promoted, or in the selected space otherwise. Units the variant does not have yet are cloned at the change order's start and then upgraded through it; ones created upstream after the change order was fixed are outside it and are listed rather than cloned")
	variantPromoteCmd.Flags().StringVar(&variantPromoteArgs.targetStage, "target-stage", "", "stage of the change order's ChangeWorkflow to promote into, promoting every variant the stage selects: it requires --change-order, the gates of the stage ahead are checked once for the whole stage, and a variant that fails is reported without stopping the ones after it. A space or --where-space narrows the stage. Without it, --change-order alone advances the change into the first stage it has not reached")
	variantPromoteCmd.Flags().StringVar(&variantPromoteArgs.whereSpace, "where-space", "", "where expression selecting the spaces to promote, instead of naming one")
	variantPromoteCmd.Flags().StringVar(&variantPromoteArgs.filterSpace, "filter-space", "", "filter over spaces selecting the spaces to promote")
	variantPromoteCmd.Flags().BoolVar(&variantPromoteArgs.dryRun, "dry-run", false, "preview the units that would be upgraded and added without changing anything")
	variantPromoteCmd.Flags().BoolVar(&variantPromoteArgs.squash, "squash", false, "merge each unit's range as one rebased diff in one revision instead of walking it: by default a promotion re-runs the upstream's recorded function invocations against each unit where it can, and records one revision per upstream revision that has an effect there")
	variantPromoteCmd.Flags().BoolVar(&variantPromoteArgs.force, "force", false, "promote past ChangeWorkflow gates that do not hold; requires --force-reason, and is recorded on the change order")
	variantPromoteCmd.Flags().StringVar(&variantPromoteArgs.forceReason, "force-reason", "", "why the gates are overridden; required with --force")
	addStandardDisplayFlags(variantPromoteCmd)
	variantCmd.AddCommand(variantPromoteCmd)
}

func variantPromoteCmdRun(cmd *cobra.Command, args []string) error {
	haveSelector := variantPromoteArgs.whereSpace != "" || variantPromoteArgs.filterSpace != ""
	if len(args) > 0 && haveSelector {
		return errors.New("name the space to promote or select spaces with --where-space and --filter-space, not both")
	}
	if len(args) == 0 && !haveSelector && variantPromoteArgs.changeorderSlug == "" {
		return errors.New("promote needs the space to promote into, --where-space or --filter-space to select spaces, or --change-order to advance a change order through the stages of its ChangeWorkflow")
	}
	if variantPromoteArgs.force && variantPromoteArgs.forceReason == "" {
		return errors.New("--force requires --force-reason: an override of the gates is recorded with the reason for it")
	}

	req := goclientnew.PromoteRequest{
		ChangeDescription: variantPromoteArgs.changeDescription,
		TargetStage:       variantPromoteArgs.targetStage,
		Squash:            variantPromoteArgs.squash,
		Force:             variantPromoteArgs.force,
		ForceReason:       variantPromoteArgs.forceReason,
		WhereSpace:        variantPromoteArgs.whereSpace,
	}

	// A bare change order slug resolves where the change was authored: the upstream of the space
	// being promoted. Without a space, or for a space that is nobody's clone, it resolves in the
	// selected space.
	changeOrderSpaceID := defaultSpaceID()
	if len(args) > 0 {
		space, err := resolveSpace(args[0], "*")
		if err != nil {
			return err
		}
		req.WhereSpace = fmt.Sprintf("SpaceID = '%s'", space.Space.SpaceID)
		if upstream := space.Space.Annotations[AnnotationUpstreamSpaceID]; upstream != "" {
			changeOrderSpaceID = upstream
		}
		// The helpers that render mutations resolve units through the selected space.
		selectedSpaceID = space.Space.SpaceID.String()
		selectedSpaceSlug = space.Space.Slug
	}
	if variantPromoteArgs.changeorderSlug != "" {
		changeOrder, err := resolveChangeOrder(variantPromoteArgs.changeorderSlug, changeOrderSpaceID, "*")
		if err != nil {
			return errors.Wrap(err, "failed to get change order")
		}
		req.ChangeOrderID = &changeOrder.ChangeOrder.ChangeOrderID
		promoteChangeOrderSlug = changeOrder.ChangeOrder.Slug
	}
	if variantPromoteArgs.filterSpace != "" {
		filterID, err := resolveFilterID(variantPromoteArgs.filterSpace)
		if err != nil {
			return err
		}
		req.SpaceFilterID = &filterID
	}
	if variantPromoteArgs.changesetSlug != "" {
		changeSetID, err := resolveChangeSetID(variantPromoteArgs.changesetSlug)
		if err != nil {
			return errors.Wrap(err, "failed to get changeset")
		}
		req.ChangeSetID = &changeSetID
	}

	var with []func(*goclientnew.PromoteParams)
	if shouldDisplayMutations() {
		with = append(with, cubapi.WithPromoteMutations)
	}
	result, err := cubapi.Promote(ctx, cubClient, req, variantPromoteArgs.dryRun, with...)
	if err != nil {
		// A refusal carries which gates failed; a structured output shows all of them.
		var refused *cubapi.PromoteRefusedError
		if errors.As(err, &refused) {
			renderPayload(refused.Result)
		}
		return err
	}
	if !renderPayload(result) {
		displayPromoteResult(result)
	}
	// Promote waits for the triggers of what it wrote, since publishing a release is what
	// usually comes next and one issued while evaluation is pending fails on that gate.
	if !variantPromoteArgs.dryRun {
		if err := awaitPromotedUnits(result); err != nil {
			return err
		}
	}
	return promoteResultError(result, len(args) > 0)
}

// awaitPromotedUnits waits for the triggers of every Unit the promotion wrote.
func awaitPromotedUnits(result *goclientnew.PromoteResult) error {
	for i := range result.Spaces {
		space := &result.Spaces[i]
		for _, unit := range space.Units {
			if unit.Error != nil || unit.UnitID == nil {
				continue
			}
			switch unit.Action {
			case promoteUnitActionUnchanged, promoteUnitActionSkip:
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

// displayPromoteResult prints what the promotion did, or on a dry run would do: the stage it
// entered, and for each space the units it upgraded, added, and left out.
func displayPromoteResult(result *goclientnew.PromoteResult) {
	showText := !isAlternativeOutput()
	for _, stage := range result.Stages {
		if stage.Chosen && showText {
			tprint("Advancing change order %s to stage %s", promoteChangeOrderSlug, stage.Name)
		}
		if stage.Forced && showText {
			tprint("Forced past the gates of stage %s:", stage.Name)
			for _, gate := range stage.Gates {
				if !gate.Satisfied {
					tprint("  - %s", gate.Message)
				}
			}
		}
	}
	if result.Complete {
		if showText {
			tprint("Change order %s has reached every stage of its ChangeWorkflow; nothing to promote",
				promoteChangeOrderSlug)
		}
		return
	}

	dryRun := variantPromoteArgs.dryRun
	for i := range result.Spaces {
		space := &result.Spaces[i]
		switch space.Action {
		case promoteSpaceActionSkipped:
			if showText {
				tprint("Skipping %s, %s", space.SpaceSlug, space.Reason)
			}
			continue
		case promoteSpaceActionBlocked:
			if showText {
				tprint("Not promoting %s: %s", space.SpaceSlug, space.Reason)
			}
			continue
		case promoteSpaceActionFailed:
			continue
		}
		if showText {
			if space.Stage != "" {
				tprint("Promoting %s into stage %s...", space.SpaceSlug, space.Stage)
			} else if len(result.Spaces) > 1 {
				tprint("Promoting %s...", space.SpaceSlug)
			}
			displayPromoteSpaceSummary(space, result.ChangeOrderID != nil, dryRun)
		}
		if shouldDisplayMutations() {
			displayPromoteSpaceMutations(space, result.ChangeOrderID != nil, dryRun)
		}
	}
}

// displayPromoteSpaceSummary prints one space's units by what was done to them.
func displayPromoteSpaceSummary(space *goclientnew.PromoteSpaceResult, withChangeOrder, dryRun bool) {
	counts := map[string]int{}
	var clones, outside []string
	for _, unit := range space.Units {
		counts[unit.Action]++
		switch {
		case unit.Action == promoteUnitActionClone:
			clones = append(clones, unit.Slug)
		case unit.Action == promoteUnitActionSkip && unit.Reason == "CreatedAfterChangeOrder":
			outside = append(outside, unit.Slug)
		}
		if unit.Error != nil {
			tprint("Failed to promote unit %s", unit.Slug)
			displayResponseError(unit.Error)
		}
	}
	verb := "Upgraded"
	addVerb := "Adding"
	if dryRun {
		verb = "Would upgrade"
		addVerb = "Would add"
	}
	upgraded := counts[promoteUnitActionUpgrade] + counts[promoteUnitActionEmpty] +
		counts[promoteUnitActionRevive]
	// An invocation runs in place, with no upstream to be behind or to add units from.
	invoked := space.UpstreamSpaceID == nil
	if !invoked {
		tprint("%s %d unit(s) behind their upstream", verb, upgraded)
	}
	if n := counts[promoteUnitActionEmpty]; n > 0 {
		tprint("  %d of them emptied, as their upstream units were", n)
	}
	if n := counts[promoteUnitActionRevive]; n > 0 {
		tprint("  %d of them revived, as their upstream units have content again", n)
	}
	if n := counts[promoteUnitActionMark]; n > 0 {
		tprint("Marked %d unit(s) the change order covers and carries no changes for", n)
	}
	if n := counts[promoteUnitActionInvoke]; n > 0 {
		tprint("Ran change order %s on %d unit(s)", promoteChangeOrderSlug, n)
	}
	switch {
	case invoked:
	case withChangeOrder:
		tprint("%s %d unit(s) from upstream at the change order's start", addVerb, len(clones))
	default:
		tprint("%s %d unit(s) from upstream", addVerb, len(clones))
	}
	for _, slug := range clones {
		tprint("  + %s", slug)
	}
	if len(outside) > 0 {
		tprint("Leaving %d upstream unit(s) outside the change order uncloned; promote without --change-order to add them:", len(outside))
		for _, slug := range outside {
			tprint("  - %s", slug)
		}
	}
	links := 0
	for _, link := range space.Links {
		if link.Action == promoteLinkActionCreate {
			links++
		}
		if link.Error != nil {
			tprint("Failed to copy link %s", link.Slug)
			displayResponseError(link.Error)
		}
	}
	if links > 0 {
		if dryRun {
			tprint("Would copy %d link(s) from the added units", links)
		} else {
			tprint("Copied %d link(s) from the added units", links)
		}
	}
}

// displayPromoteSpaceMutations prints the mutations each unit's write made, or would make.
func displayPromoteSpaceMutations(space *goclientnew.PromoteSpaceResult, withChangeOrder, dryRun bool) {
	// The helpers that fetch prior values resolve units through the selected space.
	selectedSpaceID = space.SpaceID.String()
	selectedSpaceSlug = space.SpaceSlug
	first := true
	for i := range space.Units {
		unit := &space.Units[i]
		if unit.Error != nil || unit.UnitID == nil || unit.Mutations == nil {
			continue
		}
		if !first {
			tprintRaw("")
		}
		first = false
		tprintRaw(fmt.Sprintf("Mutations for unit %s:", unit.Slug))
		lookupMutationsUnitID = unit.UnitID.String()
		lookupMutationsSpaceID = space.SpaceID.String()
		priorRevision := "dry-run"
		if !dryRun {
			priorRevision = ""
			if unit.PreviousHeadRevisionNum > 0 {
				priorRevision = fmt.Sprintf("%s/%d", unit.Slug, unit.PreviousHeadRevisionNum)
			}
		}
		displayResourceMutationList(unit.Mutations, true, unit.PreviousHeadMutationNum, "upgrade", priorRevision)
	}
	if first {
		if withChangeOrder {
			tprintRaw("No units with an upstream")
		} else {
			tprintRaw("No units behind their upstream")
		}
	}
}

// promoteResultError fails the command when anything the promotion tried did not land, naming
// what failed. Naming one space is the narrow case: a space that was skipped or blocked there is
// an error, since nothing that was asked for happened.
func promoteResultError(result *goclientnew.PromoteResult, namedSpace bool) error {
	var errs []error
	promoted := 0
	considered := 0
	for i := range result.Spaces {
		space := &result.Spaces[i]
		failed := false
		switch space.Action {
		case promoteSpaceActionSkipped, promoteSpaceActionBlocked:
			if namedSpace {
				return errors.Newf("not promoting %s: %s", space.SpaceSlug, space.Reason)
			}
			if space.Action == promoteSpaceActionSkipped {
				continue
			}
			failed = true
			errs = append(errs, errors.Newf("not promoting %s: %s", space.SpaceSlug, space.Reason))
		case promoteSpaceActionFailed:
			failed = true
			if space.Error != nil {
				errs = append(errs, errors.Newf("failed to promote Variant '%s': %s", space.SpaceSlug, space.Error.Message))
			}
		}
		considered++
		if space.Error != nil && space.Action != promoteSpaceActionFailed {
			failed = true
			errs = append(errs, errors.Newf("failed to promote Variant '%s': %s", space.SpaceSlug, space.Error.Message))
		}
		for _, unit := range space.Units {
			if unit.Error != nil {
				failed = true
				errs = append(errs, errors.Newf("failed to promote unit %s of %s: %s", unit.Slug, space.SpaceSlug, unit.Error.Message))
			}
		}
		for _, link := range space.Links {
			if link.Error != nil {
				failed = true
				errs = append(errs, errors.Newf("failed to copy link %s into %s: %s", link.Slug, space.SpaceSlug, link.Error.Message))
			}
		}
		if !failed {
			promoted++
		}
	}
	if len(errs) == 0 {
		return nil
	}
	if namedSpace {
		return errors.Join(errs...)
	}
	stage := ""
	for _, s := range result.Stages {
		stage = s.Name
	}
	if stage != "" {
		return errors.Wrapf(errors.Join(errs...), "promoted %d of %d Variant(s) of stage '%s'", promoted, considered, stage)
	}
	return errors.Wrapf(errors.Join(errs...), "promoted %d of %d space(s)", promoted, considered)
}
