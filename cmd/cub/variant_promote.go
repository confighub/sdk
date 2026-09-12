// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"bytes"
	"cmp"
	"encoding/json"
	"fmt"
	"regexp"
	"slices"
	"strings"

	"github.com/cockroachdb/errors"
	"github.com/confighub/sdk/core/changeworkflow"
	"github.com/confighub/sdk/core/cubapi"
	"github.com/confighub/sdk/core/livestatus"
	goclientnew "github.com/confighub/sdk/core/openapi/goclient-new"
	"github.com/google/uuid"
	"github.com/spf13/cobra"
)

var variantPromoteArgs struct {
	changeDescription string
	changesetSlug     string
	changeorderSlug   string
	targetStage       string
	dryRun            bool
	squash            bool
	force             bool
}

var variantPromoteCmd = &cobra.Command{
	Use:         "promote [<space>]",
	Short:       "Promote a variant space to match its upstream space",
	Annotations: map[string]string{"OrgLevel": ""},
	Long: getCommandHelp(`Promote a variant space to match changes in its upstream space.

The space must have been created by "cub variant create", which stamps an "UpstreamSpaceID"
annotation recording the upstream space it was cloned from. Promote reconciles the variant
with that upstream in three steps:

  1. Upgrade every unit whose upstream unit has advanced (the unit's UpstreamRevisionNum is
     behind the upstream unit's HeadRevisionNum), merging the upstream changes. Equivalent to
     "cub unit update --patch --upgrade --where 'UpstreamRevisionNum < UpstreamUnit.HeadRevisionNum'".
     With --change-order the selection is every unit that has an upstream, since the change
     order also marks the units it carries no changes for.
  2. Clone any units added to the upstream space since the variant was created or last
     promoted, linking each clone to its upstream unit. When the variant space has a
     Namespace label, which "cub variant create --namespace" sets, set-namespace places
     the clones in that namespace.
  3. Copy the new units' non-UpgradeUnit links, retargeting a link to its downstream copy
     when it points at another unit in the upstream space. A link that already points into
     this variant is left alone rather than copied into it.

Promote waits for triggers to complete. Use --dry-run to preview: the units that would be
upgraded (add -o mutations to see the changes) and the units that would be added.

An upgrade re-runs the upstream's recorded function invocations against each unit where it
can -- so a change lands where the unit's own structure puts it -- and records one revision per
upstream revision that has an effect, carrying that revision's change description. The variant's
history then reads as the upstream's does rather than as a series of promotions. --squash gives
up both: the range arrives as one rebased diff in one revision.

--change-order promotes a named change rather than everything the upstream has reached. The
change order fixed its range when it was created, so the upgrade stops where it ends, a unit
it does not cover is passed over, and a unit that is not where it starts is an error rather
than a merge of a different range. A unit it covers but has no changes for is marked and not
changed: its start and end tags land on the same revision, saying where the change order
applied and that none of that unit's revisions belong to it. That is what lets
"cub release publish --revision ChangeOrder:<slug>" pin every unit of the space rather than
falling back to the head of each one the change did not touch. Three things follow from what a
change order is:

  - The steps run in the other order, clone first. A unit the variant does not have yet is
    cloned at the revision the change order starts from, and then upgraded through the change
    with everything else, so it ends up with the revisions the change made everywhere else
    rather than arriving whole with the change already in it. A unit created upstream after
    the change order was fixed is outside it: those are listed rather than cloned, and
    promoting without --change-order adds them.
  - The promotion is undone in one step, however many revisions it made:
    "cub variant demote <space> --change-order <space>/<change-order>", once the change order
    has an AbortedReason saying the change is not coming.
  - Promoting is idempotent. A change order reaches a unit once and only once: a unit already
    carrying its end tag has taken the change, whatever has happened to it since, so it is passed
    over -- which is what makes running a promotion that landed partway again finish it rather
    than refuse. An aborted change order is not promoted anywhere at all, since aborting is the
    record that the change is not coming; "cub variant demote" is what takes it back out of the
    spaces that did take it.

A change order with UpdateType Invoke propagates differently and reads the same. Its change is one
invocation, named on the change order and run here rather than merged from an upstream: nothing is
cloned, no revision range is followed, and the space need not be a variant of anything. What it
leaves behind is what an upgrade-borne promotion leaves behind -- the change order's tags on every
unit it reached, both on the head where the invocation changed nothing -- so restoring, releasing
and "where has this landed?" are the same afterwards. Its selection is its own WhereUnit and
UnitFilterID rather than a where clause here, which is what holds every space to the same change,
and it only runs in the spaces the change order is headed for.

--target-stage promotes a whole stage rather than one space, and takes no positional space. A
promotion is defined over a stage -- the spaces a change reaches together -- and naming one space
is the narrower case. The stage's membership is not a label search of its own: --change-order
supplies the change order, the change order records the ChangeWorkflow it was created under, and
that workflow's stage of this name carries the selector naming the spaces. So --target-stage
requires --change-order, whose bare slug resolves in the selected space here rather than in an
upstream, there being no variant yet to take an upstream from; a change order created without a
workflow is an error rather than a guess.

  - Naming a stage says where the change is headed, not that it may skip what precedes it. The
    entry gates are the stage's own, evaluated once over the whole membership of the stage ahead
    of it, so naming a later stage while an earlier one is unsatisfied is refused.
  - A stage is promoted one variant at a time and can land partway. A variant that fails is
    reported and the ones after it are still promoted, and what comes back says how many of them
    landed. Running the stage again is what repairs a partial one: a variant that already took
    the change is no longer behind its upstream, so it is not selected again.

Without --target-stage, --change-order alone advances the change one stage: into the first stage
it has not reached, which is where it is going next. Reaching a stage is having reached every
space that stage selects, so the stage the change was authored in is passed over -- the change
order lives there -- and a stage that has taken the change but not released it leaves the stage
after it as the next, whose gates then refuse the promotion naming what is missing. Once every
stage has the change, that is reported and nothing is changed.

Examples:
`+"```"+`
  # Promote a variant to match its upstream
  cub variant promote web-prod

  # Preview the changes, including the mutations
  cub variant promote web-prod --dry-run -o mutations

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
	variantPromoteCmd.Flags().StringVar(&variantPromoteArgs.changeorderSlug, "change-order", "", "change order to promote instead of everything the upstream has reached: it supplies the range, units it does not cover are passed over, and a unit that is not where it starts is an error. A bare slug resolves in the upstream space, or in the selected space with --target-stage. Units the variant does not have yet are cloned at the change order's start and then upgraded through it; ones created upstream after the change order was fixed are outside it and are listed rather than cloned")
	variantPromoteCmd.Flags().StringVar(&variantPromoteArgs.targetStage, "target-stage", "", "stage of the change order's ChangeWorkflow to promote into, promoting every variant the stage selects instead of one named space: it requires --change-order and takes no positional space, the gates of the stage ahead are checked once for the whole stage, and a variant that fails is reported without stopping the ones after it. Without it, --change-order alone advances the change into the first stage it has not reached")
	variantPromoteCmd.Flags().BoolVar(&variantPromoteArgs.dryRun, "dry-run", false, "preview the units that would be upgraded and added without changing anything")
	variantPromoteCmd.Flags().BoolVar(&variantPromoteArgs.squash, "squash", false, "merge each unit's range as one rebased diff in one revision instead of walking it: by default a promotion re-runs the upstream's recorded function invocations against each unit where it can, and records one revision per upstream revision that has an effect there")
	variantPromoteCmd.Flags().BoolVar(&variantPromoteArgs.force, "force", false, "ignore ChangeWorkflow prerequisite checks and force promotion to downstream Variant")
	addStandardDisplayFlags(variantPromoteCmd)
	variantCmd.AddCommand(variantPromoteCmd)
}

func variantPromoteCmdRun(cmd *cobra.Command, args []string) error {
	if variantPromoteArgs.targetStage != "" {
		if len(args) > 0 {
			return errors.Newf("--target-stage promotes every Variant of stage '%s', so it cannot also name the space '%s'",
				variantPromoteArgs.targetStage, args[0])
		}
		if variantPromoteArgs.changeorderSlug == "" {
			return errors.New("--target-stage requires --change-order: a Stage's membership comes from the ChangeWorkflow the change order was created under")
		}
		return variantPromoteStage()
	}
	if len(args) == 0 {
		if variantPromoteArgs.changeorderSlug == "" {
			return errors.New("promote needs the space to promote into, or --change-order to advance a change order through the stages of its ChangeWorkflow")
		}
		return variantPromoteStage()
	}
	return variantPromoteSpace(args[0])
}

// variantPromoteStage promotes every Variant of one Stage, rather than the
// single Space named on the command line: a promotion is defined over a Stage,
// and naming one Space is the narrower case. --target-stage names the Stage;
// without it the change advances to the Stage it has not reached yet.
//
// The Stage's membership is the ChangeWorkflow's answer rather than a label
// search of our own -- the ChangeOrder records the workflow it was created
// under, and that workflow's Stage carries the selector. So here the ChangeOrder
// is what produces the Spaces rather than the other way round, which is why it
// resolves in the selected Space (there is no downstream Space yet to read an
// upstream off of) and why one carrying no workflow is refused rather than
// guessed at.
func variantPromoteStage() error {
	changeOrder, err := changeOrderByRef(variantPromoteArgs.changeorderSlug)
	if err != nil {
		return err
	}
	if err := checkChangeOrderIsPromotable(changeOrder); err != nil {
		return err
	}
	changeWorkflow := getChangeWorkflowForChangeOrder(changeOrder)
	if changeWorkflow == nil {
		return errors.Newf("change order '%s' was created without a ChangeWorkflow, so it has no stages to promote through",
			changeOrder.Slug)
	}
	// A completed rollout has nothing left to advance. This is a refusal where having
	// reached every Stage is a report: a change can be in the last Stage with
	// final.prerequisites still holding the rollout open, and promoting then is a
	// legitimate repair of a Stage that landed partway.
	if changeOrderIsCompleted(changeWorkflow, changeOrder) {
		return errors.Newf("change order '%s' has completed ChangeWorkflow '%s', so there is nothing left to promote",
			changeOrder.Slug, changeOrderWorkflowName(changeOrder))
	}
	var currentStage, previousStage *goclientnew.ChangeWorkflowStage
	if variantPromoteArgs.targetStage != "" {
		currentStage, previousStage, err = getWorkflowStageForName(changeWorkflow, variantPromoteArgs.targetStage)
	} else {
		currentStage, previousStage, err = getNextWorkflowStage(changeWorkflow, changeOrder)
	}
	if err != nil {
		return err
	}
	// Every Stage already has the change, so there is nothing left to advance it
	// to. Reported rather than refused: the rollout is finished, which is not a
	// failure.
	if currentStage == nil {
		if !jsonOutput && outputFormat == "" {
			tprint("Change order %s has reached every stage of ChangeWorkflow %s; nothing to promote",
				changeOrder.Slug, changeOrderWorkflowName(changeOrder))
		}
		return nil
	}
	// Which Stage the change lands in was worked out rather than given, so say so
	// before promoting into it.
	if variantPromoteArgs.targetStage == "" && !jsonOutput && outputFormat == "" {
		tprint("Advancing change order %s to stage %s", changeOrder.Slug, currentStage.Name)
	}

	// The gates belong to the Stage rather than to any one Variant of it, so they
	// are evaluated once for the whole Stage. They run on a dry run too: a preview
	// that ignored them would describe a promotion that cannot happen.
	if err := validateStageEntryGates(currentStage, previousStage, changeWorkflow.CustomPrerequisites, changeOrder); err != nil {
		return err
	}

	component, err := changeOrderComponent(changeOrder)
	if err != nil {
		return err
	}
	variants, err := stageSpaces(currentStage, component, changeOrder, "*")
	if err != nil {
		return err
	}
	if len(variants) == 0 {
		return errors.Newf("unable to promote to stage '%s', it selects no Space", currentStage.Name)
	}

	// A Stage lands one Variant at a time and can land partway, so a Variant that
	// fails is reported and the ones after it are still promoted. Running the
	// promotion again is what repairs a partial Stage: a Variant that already took
	// the change is no longer behind its upstream, so it is not selected again.
	promoted := 0
	var errs []error
	for _, variant := range variants {
		// The Space a link-following change order resides in is the Space the change
		// was authored in, so it already has the change: promoting it would ask it to
		// take what it originated. A first Stage whose selector covers the source
		// Space is how it turns up here.
		//
		// An Invoke change order's own Space is not like that. Nothing has been made
		// anywhere until the invocation runs, so if the Space is in scope it is
		// invoked like every other -- and if it is not, it is not in the stage.
		if variant.SpaceID == changeOrder.SpaceID && changeOrder.UpdateType != updateTypeInvoke {
			if !jsonOutput && outputFormat == "" {
				tprint("Skipping %s, the space the change order was created in", variant.Slug)
			}
			continue
		}
		// An invocation is run in place, so the Space need not be a clone of anything.
		var upstreamSpaceID uuid.UUID
		if changeOrder.UpdateType != updateTypeInvoke {
			var err error
			upstreamSpaceID, err = promoteUpstreamSpaceID(variant)
			if err != nil {
				errs = append(errs, err)
				continue
			}
		}
		// Point the selected space at the Variant being promoted, as the
		// single-Space mode does: the helpers that render mutations resolve units
		// through it.
		selectedSpaceID = variant.SpaceID.String()
		selectedSpaceSlug = variant.Slug
		if !jsonOutput && outputFormat == "" {
			tprint("Promoting %s into stage %s...", variant.Slug, currentStage.Name)
		}
		if err := promoteIntoSpace(variant.SpaceID, upstreamSpaceID, changeOrder); err != nil {
			errs = append(errs, errors.Wrapf(err, "failed to promote Variant '%s'", variant.Slug))
			continue
		}
		promoted++
	}
	if len(errs) > 0 {
		return errors.Wrapf(errors.Join(errs...), "promoted %d of %d Variant(s) of stage '%s'",
			promoted, len(variants), currentStage.Name)
	}
	return nil
}

// variantPromoteSpace promotes the one Space named on the command line, working
// out for itself which Stage that Space is in when a ChangeWorkflow governs the
// change.
func variantPromoteSpace(spaceSlug string) error {
	downstreamSpace, err := resolveSpace(spaceSlug, "*")
	if err != nil {
		return err
	}
	// Not required yet. Everything a promotion over links does comes from the upstream space, but
	// an Invoke change order's change comes from its invocation, and the space it runs in need not
	// be a clone of anything. Which kind this is is the change order's answer, so the annotation is
	// insisted on below, once there is one to ask.
	upstreamSpaceID, upstreamErr := promoteUpstreamSpaceID(downstreamSpace.Space)

	// Promote names its space positionally rather than through --space, so the selected
	// space is whatever the context defaults to -- possibly nothing. Point it at the space
	// being promoted: the helpers that render mutations resolve units through it, and one
	// of them parses it as a UUID.
	selectedSpaceID = downstreamSpace.Space.SpaceID.String()
	selectedSpaceSlug = downstreamSpace.Space.Slug

	changeOrder, err := promoteChangeOrder(upstreamSpaceID, upstreamErr == nil)
	if err != nil {
		return err
	}
	if err := checkChangeOrderIsPromotable(changeOrder); err != nil {
		return err
	}
	if upstreamErr != nil && (changeOrder == nil || changeOrder.UpdateType != updateTypeInvoke) {
		return upstreamErr
	}

	changeWorkflow := getChangeWorkflowForChangeOrder(changeOrder)
	if changeWorkflow != nil {
		if changeOrderIsCompleted(changeWorkflow, changeOrder) {
			return errors.Newf("change order '%s' has completed ChangeWorkflow '%s', so there is nothing left to promote",
				changeOrder.Slug, changeOrderWorkflowName(changeOrder))
		}

		if !variantPromoteArgs.force {
			// Refuse to promote until the previous stage is already running the change. The gate runs on a
			// dry run too: a preview that ignored it would describe a promotion that cannot happen.
			currentStage, previousStage, err := getCurrentAndPreviousWorkflowStages(downstreamSpace.Space, changeWorkflow, changeOrder)
			if err != nil {
				return err
			}
			if err := validateStageEntryGates(currentStage, previousStage, changeWorkflow.CustomPrerequisites, changeOrder); err != nil {
				return err
			}
		}
	}

	return promoteIntoSpace(downstreamSpace.Space.SpaceID, upstreamSpaceID, changeOrder)
}

// changeOrderWorkflowName names the ChangeWorkflow a change order is promoted under, for an error
// message. The copy the change order carries has no name of its own -- it is the workflow's body,
// not the row -- so this is the id the copy was taken from.
func changeOrderWorkflowName(changeOrder *goclientnew.ChangeOrder) string {
	if changeOrder == nil || changeOrder.ChangeWorkflowID == nil {
		return "unknown"
	}
	return changeOrder.ChangeWorkflowID.String()
}

// getChangeWorkflowForChangeOrder returns the ChangeWorkflow governing the ChangeOrder, or
// nil when nothing governs the promotion: no ChangeOrder was named, or the one
// named was created without a workflow. A promotion of whatever the upstream has
// reached is not part of a rollout, so there is no Stage sequence to place it in.
//
// The ChangeOrder carries the workflow rather than naming it, so nothing is fetched and nothing
// can have moved since: the copy was taken when the workflow was associated, which is what holds
// every promotion of one rollout to one set of rules.
func getChangeWorkflowForChangeOrder(changeOrder *goclientnew.ChangeOrder) *goclientnew.ChangeWorkflowSpec {
	if changeOrder == nil {
		return nil
	}
	return changeOrder.ChangeWorkflow
}

// promoteIntoSpace is the promotion itself, once the Space to promote into has
// been settled and the gates ahead of it have passed.
func promoteIntoSpace(downstreamSpaceID, upstreamSpaceID uuid.UUID, changeOrder *goclientnew.ChangeOrder) error {
	// Promote always waits for triggers, except on a dry run (nothing is changed,
	// so there is nothing to wait for).
	wait = !variantPromoteArgs.dryRun

	if changeOrder != nil {
		// An Invoke change order carries no revisions to merge and clones nothing: the change is
		// its invocation, run here. What it leaves behind is what an upgrade-borne promotion
		// leaves behind -- the change order's tags on the units it reached -- so everything
		// after this point reads the same either way.
		if changeOrder.UpdateType == updateTypeInvoke {
			return promoteInvokeUnits(downstreamSpaceID, changeOrder)
		}
		// Clone before upgrading, which is the other way round from a plain promote. A unit the
		// variant does not have yet is taken at the change order's start -- the state the change
		// begins from -- and then upgraded through the change with everything else, so it ends up
		// with the same revisions the change made everywhere else rather than arriving whole.
		if err := promoteAddNewUnitsForChangeOrder(downstreamSpaceID, upstreamSpaceID, changeOrder); err != nil {
			return err
		}
		return promoteUpgradeUnits(downstreamSpaceID, &changeOrder.ChangeOrderID)
	}
	if err := promoteUpgradeUnits(downstreamSpaceID, nil); err != nil {
		return err
	}
	return promoteAddNewUnits(downstreamSpaceID, upstreamSpaceID)
}

// componentPredicate matches any mention of the component label in a Stage's
// selector, whatever the operator it is used with: what a Stage may not do is name
// the component at all, not name it with a particular comparison.
var componentPredicate = regexp.MustCompile(`(?i)\bLabels\.` + labelComponent + `\b`)

// spaceComponent is the component a Space belongs to: its "Component" label. A
// component is not an entity of its own -- it is the set of Spaces sharing that
// label value -- so the label is the whole of it.
//
// A Space with no Component label leaves a ChangeWorkflow's Stages with nothing to
// confine them to, which is an error rather than Stages selecting every component's
// Spaces at once.
func spaceComponent(spaceID uuid.UUID) (string, error) {
	space, err := resolveSpace(spaceID.String(), "*")
	if err != nil {
		return "", errors.Wrapf(err, "failed to fetch Space %s", spaceID)
	}
	component := space.Space.Labels[labelComponent]
	if component == "" {
		return "", errors.Newf("Space '%s' has no %s label, so there is no component for a ChangeWorkflow's stages to select within",
			space.Space.Slug, labelComponent)
	}
	return component, nil
}

// changeOrderComponent is the component the change belongs to: the component of the
// Space the ChangeOrder lives in, which is the base the change was authored in.
//
// This is the one place a promotion learns its component. A ChangeWorkflow does not
// name one and a Stage's selector may not either (stageWhereSpace), so the change
// itself is what says which component's Spaces the Stages mean.
func changeOrderComponent(changeOrder *goclientnew.ChangeOrder) (string, error) {
	component, err := spaceComponent(changeOrder.SpaceID)
	if err != nil {
		return "", errors.Wrapf(err, "failed to determine the component of change order '%s'", changeOrder.Slug)
	}
	return component, nil
}

// stageWhereSpace renders the clause selecting a Stage's Spaces: the Stage's own
// selector conjoined with the component the ChangeOrder is for. The component is
// therefore declared once, by the change being promoted, rather than restated by
// every Stage of every definition -- which is what lets one definition be cloned to
// give another component the same shape of rollout.
//
// A selector naming the component itself is refused rather than conjoined. Stating
// it again either agrees, and changes nothing, or disagrees -- and a Stage that then
// selects no Space at all is not obviously wrong: it promotes into nothing and
// reports nothing. Refusing keeps that failure loud, and it is not hypothetical, a
// definition written against the older format carrying the predicate and a clone of
// one into another component being exactly how a Stage goes silently empty.
func stageWhereSpace(stage *goclientnew.ChangeWorkflowStage, component string) (string, error) {
	if componentPredicate.MatchString(stage.WhereSpace) {
		return "", errors.Newf("stage '%s' names Labels.%s in its whereSpace %q: the component is the change order's own and is appended to every stage's selector, so remove the predicate",
			stage.Name, labelComponent, stage.WhereSpace)
	}
	componentWhere := fmt.Sprintf("Labels.%s = '%s'", labelComponent, component)
	if stage.WhereSpace == "" {
		return componentWhere, nil
	}
	return stage.WhereSpace + " AND " + componentWhere, nil
}

// stageSpaces is the Spaces a Stage covers for one ChangeOrder: the Stage's own
// selector, the ChangeOrder's component, and the ChangeOrder's InScopeSpaceIDs.
// Two of the three come from the change rather than the definition, so the same
// definition resolves differently under different ChangeOrders, and a Space a
// Stage selects but the change is not headed for is not part of that Stage.
//
// The in-scope list is applied to what came back rather than conjoined onto the
// clause. A change headed for a large fleet renders an IN list past the maximum
// filter size, and listing by the Stage's own expression is the clearer request
// besides.
//
// An empty list is no restriction: a ChangeOrder given none names a change
// without saying where it is headed, which is not the same as saying every Stage
// is empty.
func stageSpaces(stage *goclientnew.ChangeWorkflowStage, component string,
	changeOrder *goclientnew.ChangeOrder, selectFields string) ([]*goclientnew.Space, error) {
	whereSpace, err := stageWhereSpace(stage, component)
	if err != nil {
		return nil, err
	}
	spaces, err := apiListSpaces(whereSpace, selectFields)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to resolve the Spaces of Stage '%s'", stage.Name)
	}
	if len(changeOrder.InScopeSpaceIDs) == 0 {
		return spaces, nil
	}
	inScope := make([]*goclientnew.Space, 0, len(spaces))
	for _, space := range spaces {
		if space != nil && slices.Contains(changeOrder.InScopeSpaceIDs, space.SpaceID) {
			inScope = append(inScope, space)
		}
	}
	return inScope, nil
}

// getCurrentAndPreviousWorkflowStages finds the Stage the Space being promoted
// into belongs to, along with the Stage ahead of it whose gates have to pass
// first. Membership is by selector, so each Stage's selector is evaluated in
// turn and the Space looked for among what it returns.
//
// The previous Stage is nil for the workflow's first Stage, which has nothing
// ahead of it to gate on.
func getCurrentAndPreviousWorkflowStages(
	space *goclientnew.Space,
	changeWorkflow *goclientnew.ChangeWorkflowSpec,
	changeOrder *goclientnew.ChangeOrder,
) (*goclientnew.ChangeWorkflowStage, *goclientnew.ChangeWorkflowStage, error) {
	var previous *goclientnew.ChangeWorkflowStage

	component, err := changeOrderComponent(changeOrder)
	if err != nil {
		return nil, nil, err
	}

	for i := range changeWorkflow.Stages {
		current := &changeWorkflow.Stages[i]

		spaces, err := stageSpaces(current, component, changeOrder, "*")
		if err != nil {
			return nil, nil, err
		}

		// The Space in hand and the ones the selector returned come from
		// different calls, so they are matched by ID rather than by identity.
		if slices.ContainsFunc(spaces, func(s *goclientnew.Space) bool {
			return s != nil && s.SpaceID == space.SpaceID
		}) {
			return current, previous, nil
		}

		previous = current
	}

	return nil, nil, errors.Newf("Space '%s' is not in any Stage of ChangeWorkflow '%s'",
		space.Slug, changeOrderWorkflowName(changeOrder))
}

// getNextWorkflowStage finds the Stage to advance the change to: the first one
// it has not reached, along with the Stage ahead of it whose gates have to pass
// first. Both are nil once every Stage has it.
//
// Reaching a Stage is having reached every Space it selects, asked of
// ResolvedSpaceIDs -- the server's own answer, and the same one the gates read.
// The Stage the change was authored in is therefore passed over without being a
// special case: a ChangeOrder resides in that Space, which puts it in that set
// from the start.
//
// The Stage found this way is the next one to promote into, not necessarily one
// that can be entered yet. A Stage the change has reached but that has not
// released it leaves the Stage after it as the next, whose gates then refuse the
// promotion naming what is missing -- which is the answer to "what is holding
// this rollout up", rather than silently advancing past it.
func getNextWorkflowStage(
	changeWorkflow *goclientnew.ChangeWorkflowSpec,
	changeOrder *goclientnew.ChangeOrder,
) (*goclientnew.ChangeWorkflowStage, *goclientnew.ChangeWorkflowStage, error) {
	var previous *goclientnew.ChangeWorkflowStage

	component, err := changeOrderComponent(changeOrder)
	if err != nil {
		return nil, nil, err
	}

	for i := range changeWorkflow.Stages {
		current := &changeWorkflow.Stages[i]

		spaces, err := stageSpaces(current, component, changeOrder, "SpaceID")
		if err != nil {
			return nil, nil, err
		}

		// A Stage selecting nothing is not one the change has reached: the workflow
		// says Spaces belong there. Returning it rather than passing over it is what
		// makes the promotion report that, instead of advancing past a Stage that
		// was meant to receive the change.
		reached := len(spaces) > 0
		for _, space := range spaces {
			if space == nil || !slices.Contains(changeOrder.ResolvedSpaceIDs, space.SpaceID) {
				reached = false
				break
			}
		}
		if !reached {
			return current, previous, nil
		}

		previous = current
	}

	return nil, nil, nil
}

// getWorkflowStageForName finds the Stage named by --target-stage, along with the
// Stage ahead of it whose gates have to pass first.
//
// Naming a Stage says where the change is headed, not that it may skip what
// precedes it: the gates are still evaluated over the previous Stage, so naming
// a later Stage while an earlier one is unsatisfied is refused rather than
// obeyed.
func getWorkflowStageForName(
	changeWorkflow *goclientnew.ChangeWorkflowSpec,
	name string,
) (*goclientnew.ChangeWorkflowStage, *goclientnew.ChangeWorkflowStage, error) {
	var previous *goclientnew.ChangeWorkflowStage

	for i := range changeWorkflow.Stages {
		current := &changeWorkflow.Stages[i]
		if current.Name == name {
			return current, previous, nil
		}
		previous = current
	}

	names := make([]string, 0, len(changeWorkflow.Stages))
	for _, stage := range changeWorkflow.Stages {
		names = append(names, stage.Name)
	}
	return nil, nil, errors.Newf("the ChangeWorkflow has no stage '%s'; its stages are: %s",
		name, strings.Join(names, ", "))
}

// checkVariantIsHealthy errors unless the Variant's reported live state says the
// change is running there. The three fields are checked separately so the error
// names which one is not yet true.
//
// A Space with no release target releases nothing, ever, so it has no live state
// to report and can never satisfy a health prerequisite -- which was asked for, so
// that is an error rather than a Variant to pass over.
func checkVariantIsHealthy(variant *goclientnew.Space, variantName string) error {
	if variant.ReleaseTargetID == nil {
		return errors.Newf("Variant '%s' has no ReleaseTargetID, so its health cannot be determined", variantName)
	}

	liveStatusJSON, ok := variant.Annotations[livestatus.Annotation]
	if !ok {
		return errors.Newf("live-status not found for Variant '%s'", variantName)
	}

	var liveStatus livestatus.Status
	if err := json.Unmarshal([]byte(liveStatusJSON), &liveStatus); err != nil {
		return err
	}

	if liveStatus.SyncStatus != "Synced" {
		return errors.Newf("Variant '%s' is not synced", variantName)
	}
	if liveStatus.OperationPhase != "Succeeded" {
		return errors.Newf("Variant '%s' has not succeeded in deployment", variantName)
	}
	if liveStatus.HealthStatus != "Healthy" {
		return errors.Newf("Variant '%s' is not healthy", variantName)
	}

	return nil
}

// checkChangeOrderIsPromotedToVariant errors when a Variant of the previous
// Stage has not taken the change order being promoted.
//
// ResolvedSpaceIDs is the server's own answer, derived from the Links the change
// order propagates over and the Revision each Unit is applied at, so this is a
// question rather than a reconstruction. A Unit the change order covers but did
// not change is not counted: what is asked is whether this change has finished
// arriving there, not whether the Space is otherwise up to date.
func checkChangeOrderIsPromotedToVariant(changeOrder *goclientnew.ChangeOrder, variant *goclientnew.Space, stage, variantName string) error {
	if !slices.Contains(changeOrder.ResolvedSpaceIDs, variant.SpaceID) {
		return errors.Newf("unable to promote to stage '%s', Variant '%s' has not taken change order '%s'",
			stage, variantName, changeOrder.Slug)
	}

	return nil
}

// checkChangeOrderIsReleasedToVariant errors when a Variant of the previous
// Stage has taken the change order but not published a Release carrying it.
//
// ReleasedSpaceIDs answers this directly, so the Release the Variant is running
// does not have to be found and taken apart. A Space with no release target
// releases nothing, ever, and so can never satisfy a released gate.
func checkChangeOrderIsReleasedToVariant(changeOrder *goclientnew.ChangeOrder, variant *goclientnew.Space, stage, variantName string) error {
	if variant.ReleaseTargetID == nil {
		return errors.Newf("unable to promote to stage '%s', Variant '%s' cannot have any released changes, missing ReleaseTargetID",
			stage, variantName)
	}
	if !slices.Contains(changeOrder.ReleasedSpaceIDs, variant.SpaceID) {
		return errors.Newf("unable to promote to stage '%s', Variant '%s' has taken change order '%s' but has not released it",
			stage, variantName, changeOrder.Slug)
	}
	return nil
}

// revisionNumsForTag is the Revision each Unit of the Space sits at under this Tag.
func revisionNumsForTag(spaceID, tagID uuid.UUID) (map[uuid.UUID]int64, error) {
	revisions, err := apiSearchListRevisions(
		fmt.Sprintf("SpaceID = '%s' AND Tags ? '%s'", spaceID, tagID), "UnitID,RevisionNum", "")
	if err != nil {
		return nil, err
	}
	nums := make(map[uuid.UUID]int64, len(revisions))
	for _, extended := range revisions {
		if extended.Revision != nil {
			nums[extended.Revision.UnitID] = extended.Revision.RevisionNum
		}
	}
	return nums, nil
}

// releaseCarryingChangeOrder is the Release that first carried this ChangeOrder into
// the Variant, or nil when none has.
//
// A Release records the Tag its bundled Revisions were selected by, so what a Release
// holds is read through that Tag: it carries the change when every Unit the end Tag
// marks is bundled at or past the Revision it marks. Matching the end Tag itself would
// only find a Release published as "cub release publish --revision ChangeOrder:<slug>",
// but a Space that published the change inside a wider Release carries it just as
// truly. This is the tedious reading of that until a Release tracks its changes
// explicitly.
//
// The earliest such Release is the answer rather than the newest, because the newest is
// this change only by coincidence and stops being the answer as soon as anything else
// is published -- which would take back a hop an earlier Release had already earned.
func releaseCarryingChangeOrder(changeOrder *goclientnew.ChangeOrder,
	variant *goclientnew.Space) (*goclientnew.Release, error) {
	if changeOrder.EndTagID == uuid.Nil {
		return nil, nil
	}
	endRevisions, err := revisionNumsForTag(variant.SpaceID, changeOrder.EndTagID)
	if err != nil {
		return nil, err
	}
	// Having taken the change is checked before this runs, so an empty set means nothing
	// in the Space carries the end Tag at all -- the propagation gap for Units added
	// downstream. Saying so beats both alternatives: the comparison below quantifies over
	// these Units, so with none of them it holds vacuously and the Space's oldest Release
	// would answer as the change's, and a nil Release would surface as an expression
	// failing on a null rather than as the tag being missing.
	if len(endRevisions) == 0 {
		return nil, errors.Newf("Variant '%s' has taken change order '%s' but nothing in it carries the change order's end tag, so the Release carrying the change cannot be identified",
			variant.Slug, changeOrder.Slug)
	}

	releases, err := apiListReleases(variant.SpaceID.String(), "Published = true", "*", "")
	if err != nil {
		return nil, err
	}
	published := make([]*goclientnew.Release, 0, len(releases))
	for _, extended := range releases {
		if extended.Release != nil && extended.Release.TagID != nil {
			published = append(published, extended.Release)
		}
	}
	// Ascending, so the first Release found to hold the change is the earliest that did
	// and the ones after it need not be read at all.
	slices.SortFunc(published, func(a, b *goclientnew.Release) int {
		return cmp.Compare(a.ReleaseNum, b.ReleaseNum)
	})
	for _, release := range published {
		bundled, err := revisionNumsForTag(variant.SpaceID, *release.TagID)
		if err != nil {
			return nil, err
		}
		carries := true
		for unitID, endNum := range endRevisions {
			// A Unit the Release does not bundle at all has not taken the change.
			if num, ok := bundled[unitID]; !ok || num < endNum {
				carries = false
				break
			}
		}
		if carries {
			return release, nil
		}
	}
	return nil, nil
}

// checkVariantSatisfiesExpression errors unless the author's own predicate holds
// for this Variant. The Space, the ChangeOrder and the Release the Variant
// published of it are what it is given, so the state of all three -- and the
// Annotations anything outside ConfigHub has written on them -- is what a gate of
// this kind can read.
//
// A Variant that has published no Release of the change is given none, so an
// expression reading one fails rather than answering from a Release of some other
// change.
//
// An expression that cannot be evaluated fails the gate rather than passing it:
// the Variant has not been shown to satisfy the prerequisite, which is the same
// position as failing it, and promoting on an unanswered gate is the one outcome
// that cannot be walked back.
func checkVariantSatisfiesExpression(
	expression string,
	changeOrder *goclientnew.ChangeOrder,
	variant *goclientnew.Space,
	release *goclientnew.Release,
	stage, variantName string,
) error {
	satisfied, err := changeworkflow.EvaluateCELPrerequisite(expression, variant, changeOrder, release)
	if err != nil {
		return errors.Wrapf(err, "unable to promote to stage '%s', prerequisite for Variant '%s' could not be evaluated",
			stage, variantName)
	}
	if !satisfied {
		return errors.Newf("unable to promote to stage '%s', Variant '%s' does not satisfy prerequisite '%s%s'",
			stage, variantName, changeworkflow.CELPrerequisitePrefix, expression)
	}
	return nil
}

// checkVariantPrerequisites errors unless the Variant satisfies every declared
// prerequisite. A Stage's entry gates and final's are the same list of names, so
// what each name checks is decided here rather than by each caller.
//
// Having taken the change is checked whatever is declared: a Variant the change
// has not reached satisfies nothing, and the prerequisites are checks on top of
// that.
//
// A prerequisite nothing knows how to check cannot be satisfied, so it is an error
// rather than a name passed over.
// The prerequisites a Stage may declare. What each one checks is decided by the
// promotion that evaluates it, below; authoring refuses anything else before a
// definition is stored, so the two cannot come to name different sets.
const (
	prerequisiteReleased = "Released"
	prerequisiteHealthy  = "Healthy"
)

// knownPrerequisites is what a definition may name, in the order they are offered
// to someone writing one.
var knownPrerequisites = []string{prerequisiteReleased, prerequisiteHealthy}

func getPrerequisiteDefinition(
	name string,
	customPrerequisiteDefinitions []goclientnew.ChangeWorkflowPrerequisite,
) *goclientnew.ChangeWorkflowPrerequisite {
	for _, definition := range customPrerequisiteDefinitions {
		if definition.Name == name {
			return &definition
		}
	}

	return nil
}

func checkVariantPrerequisites(
	prerequisites []string,
	customPrerequisiteDefinitions []goclientnew.ChangeWorkflowPrerequisite,
	changeOrder *goclientnew.ChangeOrder,
	variant *goclientnew.Space,
	stage, variantName string,
) error {
	if err := checkChangeOrderIsPromotedToVariant(changeOrder, variant, stage, variantName); err != nil {
		return err
	}

	// Every expression in a Stage reads the same Release, so it is looked up once
	// here rather than per gate -- and not at all when no gate is an expression,
	// since a Stage that asks nothing of the Release should not have to have one.
	var release *goclientnew.Release
	if slices.ContainsFunc(prerequisites, func(prerequisite string) bool {
		return getPrerequisiteDefinition(prerequisite, customPrerequisiteDefinitions) != nil
	}) {
		var err error
		release, err = releaseCarryingChangeOrder(changeOrder, variant)
		if err != nil {
			return err
		}
	}

	for _, prerequisite := range prerequisites {
		switch prerequisite {
		case prerequisiteHealthy: // validate live-status reflects the intended change is healthy
			if err := checkVariantIsHealthy(variant, variantName); err != nil {
				return err
			}
		case prerequisiteReleased: // validate the change has been released to this Variant
			if err := checkChangeOrderIsReleasedToVariant(changeOrder, variant, stage, variantName); err != nil {
				return err
			}
		default:
			prereqDefinition := getPrerequisiteDefinition(prerequisite, customPrerequisiteDefinitions)
			if prereqDefinition == nil {
				return errors.Newf("unrecognized prerequisite for Stage '%s': '%s'", stage, prerequisite)
			}
			expression, isExpression := changeworkflow.CELPrerequisiteExpression(prereqDefinition.Expression)
			if !isExpression {
				return errors.Newf("malformed expression on custom prerequisite '%s'", prerequisite)
			}
			if err := checkVariantSatisfiesExpression(expression, changeOrder, variant, release, stage, variantName); err != nil {
				return err
			}
		}
	}
	return nil
}

// validateStageEntryGates refuses the promotion until every Space in the Stage
// ahead of the one being promoted into satisfies that Stage's entry gates. The
// gates quantify over the previous Stage's whole membership, so a Space added to
// it is gated without the workflow being edited -- and so they are a property of
// the Stage being entered rather than of the Space being promoted, whether one
// Space is being promoted or all of them.
//
// Having taken the change is checked for every Variant whatever the declared
// prerequisites: a Stage cannot be entered from a Stage the change has not
// reached. The prerequisites are checks on top of that.
func validateStageEntryGates(
	currentStage *goclientnew.ChangeWorkflowStage,
	previousStage *goclientnew.ChangeWorkflowStage,
	customPrerequisites []goclientnew.ChangeWorkflowPrerequisite,
	changeOrder *goclientnew.ChangeOrder,
) error {
	// No previous Stage means this is the workflow's first, so the change is
	// promoting from the base Variant the ChangeOrder lives in -- where it was
	// authored and so already is. Nothing precedes it that could have taken or
	// released anything, so the promotion just goes ahead. This Stage's own
	// prerequisites are not its entry gates either: they gate the Stage after it.
	if previousStage == nil {
		return nil
	}

	component, err := changeOrderComponent(changeOrder)
	if err != nil {
		return err
	}
	previousStageVariants, err := stageSpaces(previousStage, component, changeOrder, "*")
	if err != nil {
		return err
	}
	if len(previousStageVariants) == 0 {
		return errors.Newf("unable to promote to stage '%s', its previous stage '%s' selects no Space",
			currentStage.Name, previousStage.Name)
	}

	for _, variant := range previousStageVariants {
		variantName := variant.Labels["Variant"]
		if variantName == "" {
			variantName = variant.Slug
		}

		err = checkVariantPrerequisites(
			currentStage.Prerequisites,
			customPrerequisites,
			changeOrder,
			variant,
			currentStage.Name,
			variantName,
		)
		if err != nil {
			return err
		}
	}

	return nil
}

// promoteChangeOrder resolves --change-order, or nil when it was not given.
//
// A ChangeOrder resides in the Space holding the changes to promote, so both the slug and the
// entity behind it resolve there rather than in the variant being promoted, which is what the
// selected space names by the time this runs. haveUpstream says whether there is such a space:
// an Invoke ChangeOrder can be promoted into a Space that is nobody's clone.
func promoteChangeOrder(upstreamSpaceID uuid.UUID, haveUpstream bool) (*goclientnew.ChangeOrder, error) {
	if variantPromoteArgs.changeorderSlug == "" {
		return nil, nil
	}
	// A bare slug resolves in the upstream space, which is where a change to promote is
	// authored. A space with no upstream has no such space to look in, which happens only for an
	// Invoke change order -- the change is made in place rather than taken from anywhere -- so
	// the slug resolves in the space being promoted instead.
	if !haveUpstream {
		return changeOrderByRef(variantPromoteArgs.changeorderSlug)
	}
	selectedForChangeOrder := selectedSpaceID
	selectedSpaceID = upstreamSpaceID.String()
	defer func() { selectedSpaceID = selectedForChangeOrder }()
	return changeOrderByRef(variantPromoteArgs.changeorderSlug)
}

// changeOrderByRef resolves a change order identifier -- a bare slug, space/slug
// or UUID -- against whatever space is selected when it runs.
func changeOrderByRef(identifier string) (*goclientnew.ChangeOrder, error) {
	changeOrder, err := resolveChangeOrder(identifier, defaultSpaceID(), "*")
	if err != nil {
		return nil, fmt.Errorf("failed to get change order: %w", err)
	}
	return changeOrder.ChangeOrder, nil
}

// checkChangeOrderIsPromotable refuses a change order that was aborted.
//
// The server refuses it too, per unit. Saying it here means the caller hears it before anything is
// cloned or upgraded, and hears it once rather than once per unit -- and it is the whole of what a
// promotion of an aborted change order would do, since every unit of it would be refused.
func checkChangeOrderIsPromotable(changeOrder *goclientnew.ChangeOrder) error {
	if changeOrder == nil || changeOrder.AbortedReason == "" {
		return nil
	}
	return errors.Newf("change order '%s' was aborted (%s), so it is not being promoted anywhere else; clear its AbortedReason to put it back on its way, or create a change order for the change you mean to promote",
		changeOrder.Slug, changeOrder.AbortedReason)
}

// promoteUpstreamSpaceID reads the UpstreamSpaceID annotation stamped by
// "cub variant create".
func promoteUpstreamSpaceID(space *goclientnew.Space) (uuid.UUID, error) {
	idStr := ""
	if space.Annotations != nil {
		idStr = space.Annotations[AnnotationUpstreamSpaceID]
	}
	if idStr == "" {
		return uuid.Nil, fmt.Errorf("space %s has no %s annotation; only spaces created by 'cub variant create' can be promoted",
			space.Slug, AnnotationUpstreamSpaceID)
	}
	id, err := uuid.Parse(idStr)
	if err != nil {
		return uuid.Nil, fmt.Errorf("invalid %s annotation %q on space %s: %w", AnnotationUpstreamSpaceID, idStr, space.Slug, err)
	}
	return id, nil
}

// promotePatchEnhancer builds the patch enhancer applied to upgraded and cloned
// units, setting the change description and/or changeset when requested. The
// returned ChangeSetID (or nil) is also passed as a bulk-operation parameter.
func promotePatchEnhancer() (PatchEnhancer, *uuid.UUID, error) {
	var changesetID *uuid.UUID
	if variantPromoteArgs.changesetSlug != "" {
		id, err := resolveChangeSetID(variantPromoteArgs.changesetSlug)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to get changeset: %w", err)
		}
		changesetID = &id
	}
	enhancer := func(patchMap map[string]interface{}) {
		if variantPromoteArgs.changeDescription != "" {
			patchMap["LastChangeDescription"] = variantPromoteArgs.changeDescription
		}
		if changesetID != nil {
			patchMap["ChangeSetID"] = *changesetID
		}
	}
	return enhancer, changesetID, nil
}

// promoteUpgradeUnits upgrades every downstream unit whose upstream has advanced. With a change
// order it upgrades those of them the change order covers, to where it ends rather than to the
// upstream's head.
// updateTypeInvoke is the ChangeOrder UpdateType whose change is an invocation run in each space
// in scope, rather than a range of revisions followed downstream over links.
const updateTypeInvoke = "Invoke"

// promoteInvokeUnits promotes an Invoke change order into one space, by running its invocation
// there.
//
// The server takes what to run from the change order, so the request carries no functions of its
// own: that is what holds every space to the same change. It also decides which units the change
// order covers and which have already taken it, so the selection here is the whole space -- the
// same shape as an upgrade-borne promotion, where which units are covered is likewise the server's
// answer given per unit.
//
// A unit passed over -- outside the change order's selection, or already carrying its end tag --
// comes back in no response at all, so what is reported is what the invocation reached.
func promoteInvokeUnits(downstreamSpaceID uuid.UUID, changeOrder *goclientnew.ChangeOrder) error {
	// A space the change order is not headed for would have every one of its units passed over,
	// and the promotion would report reaching nothing without saying why.
	if !slices.Contains(changeOrder.InScopeSpaceIDs, goclientnew.UUID(downstreamSpaceID)) {
		return errors.Newf("change order '%s' is not headed for this space, so there is nothing to promote into it; add the space to its InScopeSpaceIDs first",
			changeOrder.Slug)
	}

	if !jsonOutput && outputFormat == "" {
		tprint("Promoting change order %s by invocation...", changeOrder.Slug)
	}

	params := &goclientnew.InvokeFunctionsParams{}
	changeOrderID := changeOrder.ChangeOrderID
	params.ChangeOrder = &changeOrderID
	if variantPromoteArgs.dryRun {
		dryRunStr := "true"
		params.DryRun = &dryRunStr
	}
	// The body is required but carries nothing: the change order supplies the invocation, and a
	// request that named functions as well would be refused.
	body := goclientnew.FunctionInvocationsRequest{}
	if variantPromoteArgs.changeDescription != "" {
		body.ChangeDescription = variantPromoteArgs.changeDescription
	}

	funcRes, err := cubClientNew.InvokeFunctionsWithResponse(ctx, downstreamSpaceID, params, body)
	if cubapi.IsAPIError(err, funcRes) {
		return errors.Wrapf(cubapi.InterpretErrorGeneric(err, funcRes),
			"failed to promote change order %s by invocation", changeOrder.Slug)
	}
	responses := funcRes.JSON200
	if responses == nil {
		responses = funcRes.JSON207
	}
	if responses == nil {
		return errors.New("unexpected response from the function invoke API")
	}
	return reportInvokePromotion(responses, changeOrder)
}

// reportInvokePromotion says which units the invocation reached and fails on the ones it could not.
//
// A partial promotion is reported rather than swallowed, and is not a reason to stop: running it
// again reaches the units it missed, since a unit that took the change carries the end tag and is
// passed over the second time.
func reportInvokePromotion(responses *[]goclientnew.FunctionInvocationsResponse,
	changeOrder *goclientnew.ChangeOrder) error {
	failed := 0
	// -o json and the other machine-readable formats render the responses themselves.
	showText := !jsonOutput && outputFormat == ""
	ran := "Ran"
	if variantPromoteArgs.dryRun {
		ran = "Would run"
	}
	for i := range *responses {
		response := &(*responses)[i]
		if !response.Success {
			failed++
			if showText {
				tprint("Failed to promote change order %s into unit %s", changeOrder.Slug, unitDisplayName(response))
				if response.Error != nil {
					displayResponseError(response.Error)
				}
			}
			continue
		}
		if showText {
			// Named one by one, so that a dry run says which units the change order would
			// reach here rather than only how many -- and so that a unit the invocation left
			// alone is told apart from one it changed, since both are marked either way.
			changed := "unchanged"
			if len(response.Mutators) > 0 {
				changed = "changed"
			}
			tprint("%s change order %s on unit %s (%s)", ran, changeOrder.Slug, unitDisplayName(response), changed)
		}
	}
	if showText {
		if len(*responses) == 0 {
			tprintRaw("No units the change order covers and has not already reached")
		} else {
			tprint("%s change order %s on %d unit(s)", ran, changeOrder.Slug, len(*responses)-failed)
		}
	}
	if failed > 0 {
		return errors.Newf("change order %s failed on %d of %d unit(s)", changeOrder.Slug, failed, len(*responses))
	}
	return nil
}

func promoteUpgradeUnits(downstreamSpaceID uuid.UUID, changeOrderID *uuid.UUID) error {
	if !jsonOutput && outputFormat == "" {
		if changeOrderID != nil {
			tprint("Promoting change order %s into units behind their upstream...", variantPromoteArgs.changeorderSlug)
		} else {
			tprint("Upgrading units behind their upstream...")
		}
	}
	// Without a change order the selection is "behind its upstream": there is nothing to upgrade
	// into a unit already level with it.
	//
	// With one it is every unit that has an upstream, level or not. A change order covers the
	// units the change is about whether or not it changed each of them, and promoting it into a
	// unit it carries nothing for is what marks that unit -- both of its tags on the head, no
	// revision made -- so that a release cut from the change order's end tag pins the whole
	// space. Which units those are is the server's answer, given per unit; a unit the change
	// order does not cover at all is still passed over there.
	where := fmt.Sprintf("SpaceID = '%s' AND UpstreamRevisionNum < UpstreamUnit.HeadRevisionNum", downstreamSpaceID.String())
	if changeOrderID != nil {
		where = fmt.Sprintf("SpaceID = '%s' AND UpstreamUnitID IS NOT NULL", downstreamSpaceID.String())
	}
	include := "UnitEventID,TargetID,UpstreamUnitID,SpaceID"
	upgrade := true
	params := &goclientnew.BulkPatchUnitsParams{Where: &where, Include: &include, Upgrade: &upgrade}
	params.ChangeOrder = changeOrderID
	if variantPromoteArgs.dryRun {
		params.DryRun = &variantPromoteArgs.dryRun
	}
	if variantPromoteArgs.squash {
		params.Squash = &variantPromoteArgs.squash
	}
	enhancer, changesetID, err := promotePatchEnhancer()
	if err != nil {
		return err
	}
	params.ChangeSetId = changesetID
	patchData, err := EnhancePatchData([]byte("null"), nil, nil, nil, nil, enhancer)
	if err != nil {
		return err
	}
	// Snapshot prior unit state before the patch so the mutation display can tell the
	// changes this promotion brings from the ones already there.
	var priorUnits map[string]priorUnitInfo
	if shouldDisplayMutations() {
		priorUnits = savePriorUnitInfoInSpace(downstreamSpaceID.String(), where, false)
		// A dry run stores nothing, so what it produced comes back on the response or not
		// at all. Appended to the expansions this request already asked for, because include
		// is one list: replacing it would trade them for the configuration.
		withWriteResult := include + "," + *includeWriteResult()
		params.Include = &withWriteResult
	}
	bulkRes, err := cubClientNew.BulkPatchUnitsWithBodyWithResponse(ctx, params, "application/merge-patch+json", bytes.NewReader(patchData))
	if cubapi.IsAPIError(err, bulkRes) {
		return cubapi.InterpretErrorGeneric(err, bulkRes)
	}
	responses, statusCode := bulkUnitResponses(bulkRes.JSON200, bulkRes.JSON207)
	if responses == nil {
		return fmt.Errorf("unexpected response from bulk patch API")
	}
	bulkErr := handleBulkCreateOrUpdateResponse(responses, statusCode, "upgrade", "")
	if shouldDisplayMutations() {
		// Nothing was selected, so there are no responses to render. Say so rather than
		// printing nothing: -o mutations suppresses the ordinary summary, and silence is
		// indistinguishable from the renderer not being wired up.
		if len(*responses) == 0 {
			if changeOrderID != nil {
				tprintRaw("No units with an upstream")
			} else {
				tprintRaw("No units behind their upstream")
			}
		}
		displayMutationsForBulkUnitUpdate(responses, priorUnits, false, variantPromoteArgs.dryRun, "upgrade")
	}
	return bulkErr
}

// promoteUntrackedUpstreamUnits finds the units of the upstream space that no downstream unit
// tracks -- the ones added since the variant was created or last promoted -- and the where clause
// that selects them.
func promoteUntrackedUpstreamUnits(downstreamSpaceID, upstreamSpaceID uuid.UUID) ([]*goclientnew.Unit, string, error) {
	// Upstream unit IDs already tracked by the downstream space.
	downstreamUnits, err := apiListUnits(downstreamSpaceID.String(), "", "UnitID,UpstreamUnitID")
	if err != nil {
		return nil, "", err
	}
	tracked := make([]string, 0, len(downstreamUnits))
	for _, u := range downstreamUnits {
		if u.UpstreamUnitID != nil {
			tracked = append(tracked, "'"+u.UpstreamUnitID.String()+"'")
		}
	}

	// Upstream units not tracked downstream are the ones to add.
	newWhere := ""
	if len(tracked) > 0 {
		newWhere = fmt.Sprintf("UnitID NOT IN (%s)", strings.Join(tracked, ", "))
	}
	newUnits, err := apiListUnits(upstreamSpaceID.String(), newWhere, "UnitID,Slug")
	if err != nil {
		return nil, "", err
	}
	return newUnits, newWhere, nil
}

// promoteAddNewUnitsForChangeOrder clones the units the variant does not have yet, at the revision
// the change order starts from rather than at the upstream's head.
//
// Which of them the change order covers is decided by its start Tag: a unit created upstream after
// the change order was fixed carries none of its Tags and is not part of the change, so it is named
// rather than cloned. The clone itself names its revision the way every other operation does --
// `Before:ChangeOrder:<id>`, resolved by the server against each source unit -- so the upgrade that
// follows replays the change into the clone.
func promoteAddNewUnitsForChangeOrder(downstreamSpaceID, upstreamSpaceID uuid.UUID, changeOrder *goclientnew.ChangeOrder) error {
	newUnits, newWhere, err := promoteUntrackedUpstreamUnits(downstreamSpaceID, upstreamSpaceID)
	if err != nil {
		return err
	}
	if len(newUnits) == 0 {
		return nil
	}

	startTagWhere := fmt.Sprintf("SpaceID = '%s' AND Tags ? '%s'", upstreamSpaceID.String(), changeOrder.StartTagID.String())
	startRevisions, err := apiSearchListRevisions(startTagWhere, "UnitID,RevisionNum,Tags", "")
	if err != nil {
		return fmt.Errorf("failed to find the revisions the change order starts at: %w", err)
	}
	inChangeOrder := map[uuid.UUID]bool{}
	for _, r := range startRevisions {
		if r.Revision != nil {
			inChangeOrder[r.Revision.UnitID] = true
		}
	}

	toClone := make([]*goclientnew.Unit, 0, len(newUnits))
	outside := make([]string, 0, len(newUnits))
	for _, u := range newUnits {
		if inChangeOrder[u.UnitID] {
			toClone = append(toClone, u)
		} else {
			outside = append(outside, u.Slug)
		}
	}

	if !jsonOutput && outputFormat == "" {
		verb := "Adding"
		if variantPromoteArgs.dryRun {
			verb = "Would add"
		}
		tprint("%s %d unit(s) from upstream at the change order's start", verb, len(toClone))
		if len(outside) > 0 {
			tprint("Leaving %d upstream unit(s) outside the change order uncloned; promote without --change-order to add them:", len(outside))
			for _, slug := range outside {
				tprint("  - %s", slug)
			}
		}
	}
	if len(toClone) == 0 {
		return nil
	}
	if variantPromoteArgs.dryRun {
		for _, u := range toClone {
			tprint("  + %s", u.Slug)
		}
		return nil
	}

	cloneWhere := fmt.Sprintf("SpaceID = '%s'", upstreamSpaceID.String())
	if newWhere != "" {
		cloneWhere += " AND " + newWhere
	}
	cloneWhere += fmt.Sprintf(" AND %s", unitIDInList(toClone))
	whereSpace := fmt.Sprintf("SpaceID = '%s'", downstreamSpaceID.String())
	include := "UnitEventID,TargetID,UpstreamUnitID,SpaceID"
	upstreamRevision := fmt.Sprintf("Before:ChangeOrder:%s", changeOrder.ChangeOrderID.String())
	cloneParams := &goclientnew.BulkCreateUnitsParams{
		Where:            &cloneWhere,
		WhereSpace:       &whereSpace,
		Include:          &include,
		UpstreamRevision: &upstreamRevision,
	}
	enhancer, _, err := promotePatchEnhancer()
	if err != nil {
		return err
	}
	patchData, err := EnhancePatchData([]byte("null"), nil, nil, nil, nil, enhancer)
	if err != nil {
		return err
	}
	responses, statusCode, err := bulkCreateUnits(cloneParams, patchData)
	if err != nil {
		return err
	}
	if err := handleBulkCreateOrUpdateResponse(responses, statusCode, "create", ""); err != nil {
		return err
	}
	if err := promoteSetNamespace(downstreamSpaceID, responses); err != nil {
		return err
	}

	return promoteCopyLinks(downstreamSpaceID, upstreamSpaceID, toClone)
}

// promoteSetNamespace places the units a promotion just cloned in the variant's namespace.
// "cub variant create --namespace" runs set-namespace over the space it creates and records
// the namespace as the space's Namespace label. A unit cloned later arrives with the
// upstream's namespace, typically the placeholder, so it gets the same function.
func promoteSetNamespace(downstreamSpaceID uuid.UUID, responses *[]goclientnew.UnitCreateOrUpdateResponse) error {
	if responses == nil {
		return nil
	}
	space, err := resolveSpace(downstreamSpaceID.String(), "*")
	if err != nil {
		return err
	}
	namespace := space.Space.Labels[labelNamespace]
	if namespace == "" {
		return nil
	}
	ids := make([]string, 0, len(*responses))
	for _, response := range *responses {
		if response.Error == nil && response.Unit != nil {
			ids = append(ids, "'"+response.Unit.UnitID.String()+"'")
		}
	}
	if len(ids) == 0 {
		return nil
	}
	args := []string{"function", "do", "--quiet", "--space", space.Space.Slug,
		"--where", fmt.Sprintf("UnitID IN (%s)", strings.Join(ids, ", "))}
	// The clones joined the promotion's changeset, and a change to a unit in an open
	// changeset has to name it.
	if variantPromoteArgs.changesetSlug != "" {
		args = append(args, "--changeset", variantPromoteArgs.changesetSlug)
	}
	args = append(args, "set-namespace", namespace)
	if err := runCub(args...); err != nil {
		return err
	}
	if !jsonOutput && outputFormat == "" {
		tprint("Set namespace %q on the %d added unit(s)", namespace, len(ids))
	}
	return nil
}

// unitIDInList renders a UnitID IN (...) clause for a set of units.
func unitIDInList(units []*goclientnew.Unit) string {
	ids := make([]string, 0, len(units))
	for _, u := range units {
		ids = append(ids, "'"+u.UnitID.String()+"'")
	}
	return fmt.Sprintf("UnitID IN (%s)", strings.Join(ids, ", "))
}

// promoteAddNewUnits finds units added to the upstream space (not tracked by any
// downstream unit) and, on a real run, clones them into the downstream space and
// copies their non-UpgradeUnit links. On a dry run it just lists them.
func promoteAddNewUnits(downstreamSpaceID, upstreamSpaceID uuid.UUID) error {
	newUnits, newWhere, err := promoteUntrackedUpstreamUnits(downstreamSpaceID, upstreamSpaceID)
	if err != nil {
		return err
	}

	if !jsonOutput && outputFormat == "" {
		verb := "Adding"
		if variantPromoteArgs.dryRun {
			verb = "Would add"
		}
		tprint("%s %d unit(s) from upstream", verb, len(newUnits))
	}
	if len(newUnits) == 0 {
		return nil
	}
	if variantPromoteArgs.dryRun {
		for _, u := range newUnits {
			tprint("  + %s", u.Slug)
		}
		return nil
	}

	// Clone the new upstream units into the downstream space.
	cloneWhere := fmt.Sprintf("SpaceID = '%s'", upstreamSpaceID.String())
	if newWhere != "" {
		cloneWhere += " AND " + newWhere
	}
	whereSpace := fmt.Sprintf("SpaceID = '%s'", downstreamSpaceID.String())
	include := "UnitEventID,TargetID,UpstreamUnitID,SpaceID"
	cloneParams := &goclientnew.BulkCreateUnitsParams{Where: &cloneWhere, WhereSpace: &whereSpace, Include: &include}
	// The changeset is associated via the patch (ChangeSetID field) the enhancer
	// writes; BulkCreateUnits has no changeset parameter.
	enhancer, _, err := promotePatchEnhancer()
	if err != nil {
		return err
	}
	patchData, err := EnhancePatchData([]byte("null"), nil, nil, nil, nil, enhancer)
	if err != nil {
		return err
	}
	responses, statusCode, err := bulkCreateUnits(cloneParams, patchData)
	if err != nil {
		return err
	}
	if err := handleBulkCreateOrUpdateResponse(responses, statusCode, "create", ""); err != nil {
		return err
	}
	if err := promoteSetNamespace(downstreamSpaceID, responses); err != nil {
		return err
	}

	return promoteCopyLinks(downstreamSpaceID, upstreamSpaceID, newUnits)
}

// promoteCopyLinks copies the non-UpgradeUnit links of the newly-added units into
// the downstream space, retargeting their endpoints to the downstream copies. A
// link that points at another unit in the upstream space is retargeted to that
// unit's downstream copy; a link that points elsewhere keeps its target, and a link
// that already points into the downstream space is not copied at all.
func promoteCopyLinks(downstreamSpaceID, upstreamSpaceID uuid.UUID, newUnits []*goclientnew.Unit) error {
	if len(newUnits) == 0 {
		return nil
	}
	ids := make([]string, 0, len(newUnits))
	for _, u := range newUnits {
		ids = append(ids, "'"+u.UnitID.String()+"'")
	}
	idList := strings.Join(ids, ", ")
	// from_downstream_where finds the downstream copy of each source link's
	// FromUnit via the UpgradeUnit link the clone just created. Use only direct
	// Link fields (SpaceID, not Space.SpaceID) so the filter stays in SQL — the
	// in-memory filter path mishandles UUID fields like FromUnitID.
	fromDownstream := fmt.Sprintf("UpdateType = 'UpgradeUnit' AND SpaceID = '%s'", downstreamSpaceID.String())

	if !jsonOutput && outputFormat == "" {
		tprint("Copying links from the added units...")
	}

	// Intra-space links (to another unit in the upstream space): retarget both
	// endpoints to their downstream copies.
	intraWhere := fmt.Sprintf(
		"SpaceID = '%s' AND UpdateType != 'UpgradeUnit' AND ToSpaceID = '%s' AND FromUnitID IN (%s)",
		upstreamSpaceID.String(), upstreamSpaceID.String(), idList)
	if err := promoteBulkCopyLinks(upstreamSpaceID, intraWhere, fromDownstream, fromDownstream); err != nil {
		return err
	}

	// Cross-space links (to a unit outside the upstream space): retarget only the
	// FromUnit, keeping the original target.
	//
	// A link that already points into this variant is not one of those. Copying it would
	// retarget its FromUnit and leave the target alone, landing a link from the new unit to
	// a unit beside it -- a relationship that belongs to the variant, arriving as though the
	// upstream had asked for it. A syncback link (cub variant create --syncback) is what
	// points this way: it runs from an upstream unit to its clone, so a promotion that
	// copied it would give the new unit a syncback link nobody asked for.
	crossWhere := fmt.Sprintf(
		"SpaceID = '%s' AND UpdateType != 'UpgradeUnit' AND ToSpaceID != '%s' AND ToSpaceID != '%s' AND FromUnitID IN (%s)",
		upstreamSpaceID.String(), upstreamSpaceID.String(), downstreamSpaceID.String(), idList)
	return promoteBulkCopyLinks(upstreamSpaceID, crossWhere, fromDownstream, "")
}

func promoteBulkCopyLinks(upstreamSpaceID uuid.UUID, where, fromDownstreamWhere, toDownstreamWhere string) error {
	// The bulk-create endpoint returns 404 when its source selector matches no
	// links, so skip the call when there's nothing to copy (e.g. a newly-added
	// unit with only intra-space links has no cross-space links to copy).
	srcLinks, err := apiListLinks(upstreamSpaceID.String(), where, "", "")
	if err != nil {
		return err
	}
	if len(srcLinks) == 0 {
		return nil
	}
	bulkRes, err := callBulkCreateLinks(where, "", []byte("null"), false, fromDownstreamWhere, toDownstreamWhere, false)
	if err != nil {
		return err
	}
	return handleBulkLinkUpdateResponse(bulkRes.JSON200, bulkRes.JSON207, bulkRes.StatusCode(), "create", "")
}

// bulkUnitResponses normalizes a 200/207 bulk unit response pair.
func bulkUnitResponses(json200, json207 *[]goclientnew.UnitCreateOrUpdateResponse) (*[]goclientnew.UnitCreateOrUpdateResponse, int) {
	if json200 != nil {
		return json200, 200
	}
	if json207 != nil {
		return json207, 207
	}
	return nil, 0
}
