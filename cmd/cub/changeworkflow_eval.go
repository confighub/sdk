// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

// Reading a ChangeWorkflow on the client: which component a Stage selects within, and the
// prerequisites a Stage may declare. Promotion and how far a ChangeOrder has got through its
// workflow are the server's.

import (
	"fmt"
	"regexp"

	"github.com/cockroachdb/errors"
	goclientnew "github.com/confighub/sdk/core/openapi/goclient-new"
	"github.com/google/uuid"
)

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

// The prerequisites a Stage may declare. What each one checks is decided by the
// server's promotion; authoring refuses anything else before a definition is stored,
// so the two cannot come to name different sets.
const (
	prerequisiteValidated = "Validated"
	prerequisiteReleased  = "Released"
	prerequisiteHealthy   = "Healthy"
)

// knownPrerequisites is what a definition may name, in the order they are offered
// to someone writing one.
var knownPrerequisites = []string{prerequisiteValidated, prerequisiteReleased, prerequisiteHealthy}

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
