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
