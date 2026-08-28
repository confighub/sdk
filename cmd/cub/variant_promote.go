// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/cockroachdb/errors"
	"github.com/confighub/sdk/core/changeworkflow"
	"github.com/confighub/sdk/core/cubapi"
	"github.com/confighub/sdk/core/livestatus"
	goclientnew "github.com/confighub/sdk/core/openapi/goclient-new"
	"github.com/google/uuid"
	"github.com/spf13/cobra"
	"sigs.k8s.io/yaml"
)

var variantPromoteArgs struct {
	changeDescription string
	changesetSlug     string
	changeorderSlug   string
	targetStage       string
	dryRun            bool
	squash            bool
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
     promoted, linking each clone to its upstream unit.
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
    "cub unit update --patch --space <variant> --where \"Slug LIKE '%'\"
     --restore Before:ChangeOrder:<space>/<change-order>".
  - Promoting is idempotent. A unit already carrying the change order's end tag has taken the
    change, whatever has happened to it since, so it is passed over -- which is what makes
    repeating a promotion that landed partway finish it rather than refuse.

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
    landed. Promoting again is what repairs a partial stage: a variant that already took the
    change is no longer behind its upstream, so it is not selected again.

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
	changeOrder, err := resolveChangeOrder(variantPromoteArgs.changeorderSlug)
	if err != nil {
		return err
	}
	changeWorkflow, err := getChangeWorkflowForChangeOrder(changeOrder)
	if err != nil {
		return err
	}
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
			changeOrder.Slug, changeWorkflow.Name)
	}
	var currentStage, previousStage *changeworkflow.ChangeWorkflowStage
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
				changeOrder.Slug, changeWorkflow.Name)
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
	if err := validateStageEntryGates(currentStage, previousStage, changeOrder); err != nil {
		return err
	}

	variants, err := apiListSpaces(currentStage.WhereSpace, "*")
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
		// The Space the change order resides in is the Space the change was
		// authored in, so it already has the change: promoting it would ask it to
		// take what it originated. A first Stage whose selector covers the source
		// Space is how it turns up here.
		if variant.SpaceID == changeOrder.SpaceID {
			if !jsonOutput && outputFormat == "" {
				tprint("Skipping %s, the space the change order was created in", variant.Slug)
			}
			continue
		}
		upstreamSpaceID, err := promoteUpstreamSpaceID(variant)
		if err != nil {
			errs = append(errs, err)
			continue
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
	downstreamSpace, err := apiGetSpaceFromSlug(spaceSlug, "*")
	if err != nil {
		return err
	}
	upstreamSpaceID, err := promoteUpstreamSpaceID(downstreamSpace)
	if err != nil {
		return err
	}

	// Promote names its space positionally rather than through --space, so the selected
	// space is whatever the context defaults to -- possibly nothing. Point it at the space
	// being promoted: the helpers that render mutations resolve units through it, and one
	// of them parses it as a UUID.
	selectedSpaceID = downstreamSpace.SpaceID.String()
	selectedSpaceSlug = downstreamSpace.Slug

	changeOrder, err := promoteChangeOrder(upstreamSpaceID)
	if err != nil {
		return err
	}

	changeWorkflow, err := getChangeWorkflowForChangeOrder(changeOrder)
	if err != nil {
		return err
	}
	if changeWorkflow != nil {
		if changeOrderIsCompleted(changeWorkflow, changeOrder) {
			return errors.Newf("change order '%s' has completed ChangeWorkflow '%s', so there is nothing left to promote",
				changeOrder.Slug, changeWorkflow.Name)
		}
		// Refuse to promote until the previous stage is already running the change. The gate runs on a
		// dry run too: a preview that ignored it would describe a promotion that cannot happen.
		currentStage, previousStage, err := getCurrentAndPreviousWorkflowStages(downstreamSpace, changeWorkflow)
		if err != nil {
			return err
		}
		if err := validateStageEntryGates(currentStage, previousStage, changeOrder); err != nil {
			return err
		}
	}

	return promoteIntoSpace(downstreamSpace.SpaceID, upstreamSpaceID, changeOrder)
}

// getChangeWorkflowForChangeOrder returns the ChangeWorkflow governing the ChangeOrder, or
// nil when nothing governs the promotion: no ChangeOrder was named, or the one
// named was created without a workflow. A promotion of whatever the upstream has
// reached is not part of a rollout, so there is no Stage sequence to place it in.
func getChangeWorkflowForChangeOrder(changeOrder *goclientnew.ChangeOrder) (*changeworkflow.ChangeWorkflow, error) {
	if changeOrder == nil {
		return nil, nil
	}
	changeWorkflowUnitID, ok := changeOrder.Annotations[changeWorkflowUnitIDAnnotation]
	if !ok {
		return nil, nil
	}
	return getChangeWorkflowFromUnit(changeWorkflowUnitID,
		changeOrder.Annotations[changeWorkflowRevisionAnnotation])
}

// promoteIntoSpace is the promotion itself, once the Space to promote into has
// been settled and the gates ahead of it have passed.
func promoteIntoSpace(downstreamSpaceID, upstreamSpaceID uuid.UUID, changeOrder *goclientnew.ChangeOrder) error {
	// Promote always waits for triggers, except on a dry run (nothing is changed,
	// so there is nothing to wait for).
	wait = !variantPromoteArgs.dryRun

	if changeOrder != nil {
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

// getChangeWorkflowFromUnit parses the ChangeWorkflow definition the change
// workflow annotations record: the pinned Revision of the named Unit, rather
// than whatever that Unit holds now, so a workflow edited part way through a
// rollout does not change the rules a ChangeOrder already started under.
//
// The Unit is reached by a list rather than a get because the annotation carries
// only its ID, while every unit get is scoped to a Space, and the definition need
// not live in either Space taking part in the promotion.
func getChangeWorkflowFromUnit(changeWorkflowUnitID, changeWorkflowRevision string) (*changeworkflow.ChangeWorkflow, error) {
	unitID, err := uuid.Parse(changeWorkflowUnitID)
	if err != nil {
		return nil, errors.Wrapf(err, "invalid %s annotation %q", changeWorkflowUnitIDAnnotation, changeWorkflowUnitID)
	}

	revisionNum, err := strconv.ParseInt(changeWorkflowRevision, 10, 64)
	if err != nil {
		return nil, errors.Wrapf(err, "invalid %s annotation %q", changeWorkflowRevisionAnnotation, changeWorkflowRevision)
	}

	units, err := apiListAllUnits(cubapi.Where{}.In("UnitID", []goclientnew.UUID{goclientnew.UUID(unitID)}),
		"", "", "", "", false, "UnitID,SpaceID,Slug", "", "")
	if err != nil {
		return nil, errors.Wrapf(err, "failed to fetch ChangeWorkflow unit %s", unitID)
	}
	if len(units) == 0 || units[0].Unit == nil {
		return nil, errors.Errorf("ChangeWorkflow unit %s not found", unitID)
	}
	unit := units[0].Unit

	revision, err := apiGetRevisionFromNumberInSpace(revisionNum, unit.UnitID.String(), unit.SpaceID.String(),
		"RevisionID,RevisionNum")
	if err != nil {
		return nil, errors.Wrapf(err, "failed to fetch revision %d of ChangeWorkflow unit %s", revisionNum, unit.Slug)
	}

	// A Revision's configuration is read from its data endpoint, not off the entity.
	data, err := fetchRevisionData(unit.SpaceID, unit.UnitID, revision.RevisionID)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to fetch data of revision %d of unit %s", revisionNum, unit.Slug)
	}

	changeWorkflow := &changeworkflow.ChangeWorkflow{}
	if err := yaml.Unmarshal([]byte(data), changeWorkflow); err != nil {
		return nil, errors.Wrapf(err, "unit %s revision %d does not carry a ChangeWorkflow definition",
			unit.Slug, revisionNum)
	}

	return changeWorkflow, nil
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
	changeWorkflow *changeworkflow.ChangeWorkflow,
) (*changeworkflow.ChangeWorkflowStage, *changeworkflow.ChangeWorkflowStage, error) {
	var previous *changeworkflow.ChangeWorkflowStage

	for i := range changeWorkflow.Spec.Stages {
		current := &changeWorkflow.Spec.Stages[i]

		spaces, err := apiListSpaces(current.WhereSpace, "*")
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
		space.Slug, changeWorkflow.Name)
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
	changeWorkflow *changeworkflow.ChangeWorkflow,
	changeOrder *goclientnew.ChangeOrder,
) (*changeworkflow.ChangeWorkflowStage, *changeworkflow.ChangeWorkflowStage, error) {
	var previous *changeworkflow.ChangeWorkflowStage

	for i := range changeWorkflow.Spec.Stages {
		current := &changeWorkflow.Spec.Stages[i]

		spaces, err := apiListSpaces(current.WhereSpace, "SpaceID")
		if err != nil {
			return nil, nil, errors.Wrapf(err, "failed to resolve the Spaces of Stage '%s'", current.Name)
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
	changeWorkflow *changeworkflow.ChangeWorkflow,
	name string,
) (*changeworkflow.ChangeWorkflowStage, *changeworkflow.ChangeWorkflowStage, error) {
	var previous *changeworkflow.ChangeWorkflowStage

	for i := range changeWorkflow.Spec.Stages {
		current := &changeWorkflow.Spec.Stages[i]
		if current.Name == name {
			return current, previous, nil
		}
		previous = current
	}

	names := make([]string, 0, len(changeWorkflow.Spec.Stages))
	for _, stage := range changeWorkflow.Spec.Stages {
		names = append(names, stage.Name)
	}
	return nil, nil, errors.Newf("ChangeWorkflow '%s' has no stage '%s'; its stages are: %s",
		changeWorkflow.Name, name, strings.Join(names, ", "))
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
func checkVariantPrerequisites(
	prerequisites []string,
	changeOrder *goclientnew.ChangeOrder,
	variant *goclientnew.Space,
	stage, variantName string,
) error {
	if err := checkChangeOrderIsPromotedToVariant(changeOrder, variant, stage, variantName); err != nil {
		return err
	}

	for _, prerequisite := range prerequisites {
		switch prerequisite {
		case "healthy": // validate live-status reflects the intended change is healthy
			if err := checkVariantIsHealthy(variant, variantName); err != nil {
				return err
			}
		case "released": // validate the change has been released to this Variant
			if err := checkChangeOrderIsReleasedToVariant(changeOrder, variant, stage, variantName); err != nil {
				return err
			}
		default:
			return errors.Newf("unrecognized prerequisite for Stage '%s': '%s'", stage, prerequisite)
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
	currentStage *changeworkflow.ChangeWorkflowStage,
	previousStage *changeworkflow.ChangeWorkflowStage,
	changeOrder *goclientnew.ChangeOrder,
) error {
	// No previous Stage means this is the workflow's first, so the change is
	// promoting from the base Variant that source.space names -- where it was
	// authored and so already is. Nothing precedes it that could have taken or
	// released anything, so the promotion just goes ahead. This Stage's own
	// prerequisites are not its entry gates either: they gate the Stage after it.
	if previousStage == nil {
		return nil
	}

	previousStageVariants, err := apiListSpaces(previousStage.WhereSpace, "*")
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

		err = checkVariantPrerequisites(currentStage.Prerequisites, changeOrder, variant, currentStage.Name, variantName)
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
// selected space names by the time this runs.
func promoteChangeOrder(upstreamSpaceID uuid.UUID) (*goclientnew.ChangeOrder, error) {
	if variantPromoteArgs.changeorderSlug == "" {
		return nil, nil
	}
	selectedForChangeOrder := selectedSpaceID
	selectedSpaceID = upstreamSpaceID.String()
	defer func() { selectedSpaceID = selectedForChangeOrder }()
	return resolveChangeOrder(variantPromoteArgs.changeorderSlug)
}

// resolveChangeOrder resolves a change order identifier -- a bare slug, space/slug
// or UUID -- against whatever space is selected when it runs.
func resolveChangeOrder(identifier string) (*goclientnew.ChangeOrder, error) {
	changeOrder, err := parseEntityIdentifierSingleAsEntity[goclientnew.ChangeOrder](
		identifier,
		EntityTypeChangeOrder,
		"*",
		apiGetChangeOrderFromSlugInSpace,
		func(c *goclientnew.ChangeOrder) string { return c.ChangeOrderID.String() },
	)
	if err != nil {
		return nil, fmt.Errorf("failed to get change order: %w", err)
	}
	return changeOrder, nil
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
		id, err := parseChangeSetSlug(variantPromoteArgs.changesetSlug)
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

	return promoteCopyLinks(downstreamSpaceID, upstreamSpaceID, toClone)
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
