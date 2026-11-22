// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package guardrails

import (
	"fmt"
	"maps"
	"regexp"
	"slices"
	"strings"

	goclientnew "github.com/confighub/sdk/core/openapi/goclient-new"
)

// OtherData is what a check reads besides the resources it checks: under Key, the resources a
// selection picks for the check's rule. A rule whose checks read other data is written with an
// OtherUnitSelection for each key, which each Space or Target selecting the rule evaluates for
// itself and records; the function then reads the resources as other[Key], with where each came
// from as otherOrigins[Key]. By default the selection picks the Units on the same cluster: for a
// rule applied to Spaces, those released to the Space's release Target, so a Space with no release
// Target is passed nothing under the key, and its function decides what that means; for a rule
// applied to Targets, those released to the Target.
type OtherData struct {
	// Key is the name the function reads the resources under: letters, digits, '-' and '_'.
	Key string
	// ResourceTypes are the types passed, [Group/]Version/Kind, such as
	// networking.k8s.io/v1/NetworkPolicy.
	ResourceTypes []string
	// WhereResource narrows the resources passed further, ANDed with ResourceTypes, such as to
	// skip those annotated as excepted.
	WhereResource string
	// Where selects the Units whose resources are passed, in the extended filter syntax with
	// the Space evaluating it named EvaluatingSpace, or the Target EvaluatingTarget. Empty is
	// OnSameTarget for a rule applied to Spaces and OnThisTarget for one applied to Targets.
	Where string
}

// OnSameTarget selects the Units released to the Target the evaluating Space releases to.
const OnSameTarget = "Space.ReleaseTargetID = EvaluatingSpace.ReleaseTargetID"

// OnThisTarget selects the Units released to the evaluating Target.
const OnThisTarget = "Space.ReleaseTargetID = EvaluatingTarget.TargetID"

var otherDataKeyPattern = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

// validateOtherData checks what a check or rule named owner reads besides its resources.
func validateOtherData(owner string, otherData []OtherData) error {
	keys := map[string]bool{}
	for _, data := range otherData {
		switch {
		case !otherDataKeyPattern.MatchString(data.Key):
			return fmt.Errorf("%s: other data key %q is not letters, digits, '-' and '_'", owner, data.Key)
		case keys[data.Key]:
			return fmt.Errorf("%s: other data key %s is declared twice", owner, data.Key)
		case len(data.ResourceTypes) == 0 && data.WhereResource == "":
			return fmt.Errorf("%s: other data %s needs ResourceTypes or a WhereResource, or it passes every resource on the cluster", owner, data.Key)
		}
		keys[data.Key] = true
	}
	return nil
}

// otherDataEqual says whether two checks applied to appliedTo read the same other data, in
// whatever order they declare it, so that they can run in one rule.
func otherDataEqual(appliedTo string, a, b []OtherData) bool {
	return maps.EqualFunc(otherDataByKey(a), otherDataByKey(b), func(x, y OtherData) bool {
		return x.WhereResource == y.WhereResource && x.where(appliedTo) == y.where(appliedTo) &&
			slices.Equal(sortedTypes(x.ResourceTypes), sortedTypes(y.ResourceTypes))
	})
}

func otherDataByKey(otherData []OtherData) map[string]OtherData {
	byKey := make(map[string]OtherData, len(otherData))
	for _, data := range otherData {
		byKey[data.Key] = data
	}
	return byKey
}

// where is the selection's Where for a rule applied to appliedTo.
func (data OtherData) where(appliedTo string) string {
	switch {
	case data.Where != "":
		return data.Where
	case appliedTo == AppliedToTarget:
		return OnThisTarget
	}
	return OnSameTarget
}

// selection is the OtherUnitSelection reading data for a rule applied to appliedTo.
func (data OtherData) selection(appliedTo string) goclientnew.OtherUnitSelection {
	clauses := []string{}
	if types := resourceTypesWhere(sortedTypes(data.ResourceTypes)); types != "" {
		clauses = append(clauses, types)
	}
	if data.WhereResource != "" {
		clauses = append(clauses, data.WhereResource)
	}
	return goclientnew.OtherUnitSelection{Where: data.where(appliedTo), WhereResource: strings.Join(clauses, " AND ")}
}

// otherUnitSelections are the rule's Trigger's OtherUnitSelections: exactly those its other data
// needs, none when it reads none, so that installing a rule that no longer reads some data
// removes its selection.
func (r Rule) otherUnitSelections() map[string]goclientnew.OtherUnitSelection {
	selections := make(map[string]goclientnew.OtherUnitSelection, len(r.OtherData))
	for _, data := range r.OtherData {
		selections[data.Key] = data.selection(r.AppliedTo)
	}
	return selections
}
