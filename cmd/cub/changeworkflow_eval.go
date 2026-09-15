// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

// Reading a ChangeWorkflow on the client: which Spaces a Stage selects, which Stage a change
// reaches next, and whether a Space satisfies a Stage's prerequisites. Promotion itself is the
// server's (POST /promote), which enforces the gates; these readings are what `cub changeorder
// get` and `cub changeorder create` display and derive.

import (
	"cmp"
	"encoding/json"
	"fmt"
	"regexp"
	"slices"

	"github.com/cockroachdb/errors"
	"github.com/confighub/sdk/core/changeworkflow"
	"github.com/confighub/sdk/core/livestatus"
	goclientnew "github.com/confighub/sdk/core/openapi/goclient-new"
	"github.com/google/uuid"
)

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

// The prerequisites a Stage may declare. What each one checks is decided by the
// server's promotion and by checkVariantPrerequisites below; authoring refuses anything else before a
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

// changeOrderByRef resolves a change order identifier -- a bare slug, space/slug
// or UUID -- against whatever space is selected when it runs.
func changeOrderByRef(identifier string) (*goclientnew.ChangeOrder, error) {
	changeOrder, err := resolveChangeOrder(identifier, defaultSpaceID(), "*")
	if err != nil {
		return nil, fmt.Errorf("failed to get change order: %w", err)
	}
	return changeOrder.ChangeOrder, nil
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
