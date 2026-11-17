// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package cubapi

import (
	"fmt"
	"slices"
	"strings"

	api "github.com/confighub/sdk/core/function/api"
	goclientnew "github.com/confighub/sdk/core/openapi/goclient-new"
)

// ParseClearance turns --clearance flag values into a Clearance. Each requirement is one of four
// forms, read like set-based label selection:
//
//	KEY              cleared for the key whatever its value      (Exists)
//	KEY=A[;B...]     cleared for those values                    (In)
//	KEY!=A[;B...]    cleared for any value but those             (NotIn)
//	!KEY             refuse any path carrying the key at all     (DoesNotExist)
//
// An empty list, or one whose only entry is empty, yields an empty clearance -- which clears nothing, and is how a
// clearance is removed.
func ParseClearance(specs []string) (goclientnew.Clearance, error) {
	clearance := goclientnew.Clearance{}
	for _, spec := range specs {
		spec = strings.TrimSpace(spec)
		if spec == "" {
			continue
		}
		requirement, err := ParseClearanceRequirement(spec)
		if err != nil {
			return nil, err
		}
		clearance = append(clearance, requirement)
	}
	return clearance, nil
}

// DefaultClearance is the clearance a client sends with a change that is not a merge when none is
// specified: cleared for Override guards, whatever their value. An Override guard keeps a merge --
// an upgrade, a promotion, an upload, a merge from a source -- from replacing a value someone set
// on purpose, and it is not meant to stand in the way of the next edit by hand. Each call returns a new Clearance, so a caller may extend it.
func DefaultClearance() goclientnew.Clearance {
	return goclientnew.Clearance{{
		Key:      api.GuardKeyOverride,
		Operator: string(api.ClearanceOperatorExists),
	}}
}

// OperationClearance is the clearance to send with one change, from its --clearance flag values:
// what they say when any are given, and otherwise DefaultClearance for a change that is not a
// merge and none for one that is. A clearance given replaces the default rather than adding to
// it, so a change given an empty clearance is cleared for nothing.
func OperationClearance(specs []string, merge bool) (goclientnew.Clearance, error) {
	if len(specs) > 0 {
		return ParseClearance(specs)
	}
	if merge {
		return nil, nil
	}
	return DefaultClearance(), nil
}

// MergeUpdateTypes are the UpdateTypes of a Link or ChangeOrder that merge: UpgradeUnit and
// MergeUnits take their change from Revisions of a source Unit, where the others resolve or run
// an Invocation. A change that resolves Links or promotes a ChangeOrder of one of them is a merge,
// for OperationClearance.
func MergeUpdateTypes() []string {
	return []string{"UpgradeUnit", "MergeUnits"}
}

// IsMergeUpdateType reports whether updateType is one of MergeUpdateTypes.
func IsMergeUpdateType(updateType string) bool {
	return slices.Contains(MergeUpdateTypes(), updateType)
}

// ClearanceToRetry is the clearance to suggest for running a change again so that it writes
// what the guards withheld from it: what covers those guards (ClearanceCoveringGuards), with the
// default restated where sent, the clearance the change was sent with, relied on it
// (RestateDefaultClearance). Empty when no guard withheld anything.
func ClearanceToRetry(conflicts []goclientnew.MutationConflict, sent goclientnew.Clearance) goclientnew.Clearance {
	return RestateDefaultClearance(ClearanceCoveringGuards(conflicts), sent)
}

// RestateDefaultClearance is clearance, a clearance to suggest adding to a change, after the
// requirements of DefaultClearance that sent, the clearance the change was sent with, has. A
// clearance given replaces the default, so a change that relied on it has to state it to be run
// again without withholding what it wrote the first time. An empty clearance is returned as it
// is: there is nothing to suggest.
func RestateDefaultClearance(clearance, sent goclientnew.Clearance) goclientnew.Clearance {
	if len(clearance) == 0 {
		return clearance
	}
	var restated goclientnew.Clearance
	for _, requirement := range DefaultClearance() {
		if slices.ContainsFunc(sent, func(r goclientnew.ClearanceRequirement) bool { return sameRequirement(r, requirement) }) {
			restated = append(restated, requirement)
		}
	}
	return MergeClearances(restated, clearance)
}

// MergeClearances is the requirements of first followed by those of second that first does not
// already state. Requirements of one key are alternatives, so the result clears what either does.
func MergeClearances(first, second goclientnew.Clearance) goclientnew.Clearance {
	merged := slices.Clone(first)
	for _, requirement := range second {
		if !slices.ContainsFunc(merged, func(r goclientnew.ClearanceRequirement) bool { return sameRequirement(r, requirement) }) {
			merged = append(merged, requirement)
		}
	}
	return merged
}

// sameRequirement reports whether two requirements say the same thing.
func sameRequirement(a, b goclientnew.ClearanceRequirement) bool {
	return a.Key == b.Key && a.Operator == b.Operator && slices.Equal(a.Values, b.Values)
}

// ParseClearanceRequirement parses one requirement of ParseClearance.
func ParseClearanceRequirement(spec string) (goclientnew.ClearanceRequirement, error) {
	malformed := func() (goclientnew.ClearanceRequirement, error) {
		return goclientnew.ClearanceRequirement{}, fmt.Errorf(
			"--clearance %q must be KEY, KEY=VALUE[;VALUE...], KEY!=VALUE[;VALUE...], or !KEY", spec)
	}

	if key, found := strings.CutPrefix(spec, "!"); found {
		if key == "" || strings.ContainsAny(key, "=,;") {
			return malformed()
		}
		return goclientnew.ClearanceRequirement{
			Key:      key,
			Operator: string(api.ClearanceOperatorDoesNotExist),
		}, nil
	}

	// != before =, or the != form would be read as a key ending in '!'.
	if key, values, found := strings.Cut(spec, "!="); found {
		if key == "" || values == "" {
			return malformed()
		}
		split, err := splitClearanceValues(spec, values)
		if err != nil {
			return goclientnew.ClearanceRequirement{}, err
		}
		return goclientnew.ClearanceRequirement{
			Key:      key,
			Operator: string(api.ClearanceOperatorNotIn),
			Values:   split,
		}, nil
	}

	if key, values, found := strings.Cut(spec, "="); found {
		if key == "" || values == "" {
			return malformed()
		}
		split, err := splitClearanceValues(spec, values)
		if err != nil {
			return goclientnew.ClearanceRequirement{}, err
		}
		return goclientnew.ClearanceRequirement{
			Key:      key,
			Operator: string(api.ClearanceOperatorIn),
			Values:   split,
		}, nil
	}

	return goclientnew.ClearanceRequirement{
		Key:      spec,
		Operator: string(api.ClearanceOperatorExists),
	}, nil
}

// ClearanceValueSeparator separates the values of one requirement.
const ClearanceValueSeparator = ";"

// splitClearanceValues splits the values of one requirement. A guard value may contain neither a
// semicolon nor a comma, so the split is unambiguous, and a comma can only be a list written with
// the wrong separator: it is refused here, where the message can say so, rather than sent as one
// value for the server to refuse as malformed.
func splitClearanceValues(spec, values string) ([]string, error) {
	if strings.Contains(values, ",") {
		return nil, fmt.Errorf("--clearance %q: separate values with %q, as in KEY=VALUE;VALUE", spec, ClearanceValueSeparator)
	}
	parts := strings.Split(values, ClearanceValueSeparator)
	trimmed := make([]string, 0, len(parts))
	for _, part := range parts {
		trimmed = append(trimmed, strings.TrimSpace(part))
	}
	return trimmed, nil
}

// ParseGuards turns --guard flag values, KEY=VALUE each, into the guards a change records on the
// paths it writes. flag names the flag in an error.
func ParseGuards(flag string, specs []string) (goclientnew.GuardStamp, error) {
	stamp := goclientnew.GuardStamp{}
	for _, spec := range specs {
		spec = strings.TrimSpace(spec)
		if spec == "" {
			continue
		}
		key, value, found := strings.Cut(spec, "=")
		if !found || key == "" {
			return nil, fmt.Errorf("--%s %q must be KEY=VALUE", flag, spec)
		}
		stamp[key] = value
	}
	return stamp, nil
}

// FormatClearance renders a Clearance back in the same four forms the --clearance flag takes,
// so what is displayed is what would be typed to set it again.
func FormatClearance(clearance *goclientnew.Clearance) string {
	if clearance == nil || len(*clearance) == 0 {
		return ""
	}
	specs := make([]string, 0, len(*clearance))
	for _, requirement := range *clearance {
		switch api.ClearanceOperator(requirement.Operator) {
		case api.ClearanceOperatorExists:
			specs = append(specs, requirement.Key)
		case api.ClearanceOperatorDoesNotExist:
			specs = append(specs, "!"+requirement.Key)
		case api.ClearanceOperatorIn:
			specs = append(specs, requirement.Key+"="+strings.Join(requirement.Values, ClearanceValueSeparator))
		case api.ClearanceOperatorNotIn:
			specs = append(specs, requirement.Key+"!="+strings.Join(requirement.Values, ClearanceValueSeparator))
		default:
			specs = append(specs, fmt.Sprintf("%s %s %v", requirement.Key, requirement.Operator, requirement.Values))
		}
	}
	return strings.Join(specs, " ")
}

// ClearanceCoveringGuards is the narrowest clearance admitting the guards that withheld the
// given conflicts: per key, In the values the conflicts name, or Exists for a guard with no
// value. A guard the change forbade itself with DoesNotExist is left out, since no added
// requirement allows it.
func ClearanceCoveringGuards(conflicts []goclientnew.MutationConflict) goclientnew.Clearance {
	values := map[string][]string{}
	var keys []string
	for _, conflict := range conflicts {
		guards := conflict.Guards
		if len(guards) == 0 && conflict.Guard != nil {
			guards = []goclientnew.WithheldGuard{*conflict.Guard}
		}
		for _, guard := range guards {
			if guard.Precondition || guard.Key == "" {
				continue
			}
			if _, seen := values[guard.Key]; !seen {
				keys = append(keys, guard.Key)
				values[guard.Key] = nil
			}
			if guard.Value != "" && !slices.Contains(values[guard.Key], guard.Value) {
				values[guard.Key] = append(values[guard.Key], guard.Value)
			}
		}
	}
	slices.Sort(keys)
	clearance := goclientnew.Clearance{}
	for _, key := range keys {
		if len(values[key]) == 0 {
			clearance = append(clearance, goclientnew.ClearanceRequirement{Key: key, Operator: string(api.ClearanceOperatorExists)})
			continue
		}
		slices.Sort(values[key])
		clearance = append(clearance, goclientnew.ClearanceRequirement{Key: key, Operator: string(api.ClearanceOperatorIn), Values: values[key]})
	}
	return clearance
}
