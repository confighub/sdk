// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"fmt"
	"strings"

	"github.com/confighub/sdk/core/function/api"
	goclientnew "github.com/confighub/sdk/core/openapi/goclient-new"
)

// A clearance says which classes of guarded reason an operation is cleared for. On the command
// line each requirement is one of four forms, chosen to read like the set-based label selection
// they come from:
//
//	KEY              cleared for the key whatever its value      (Exists)
//	KEY=A[,B...]     cleared for those values                    (In)
//	KEY!=A[,B...]    cleared for any value but those             (NotIn)
//	!KEY             refuse any path carrying the key at all     (DoesNotExist)
//
// The last is a precondition rather than a clearance: it is how an operation says "I am cleared
// for owner guards, but never touch anything carrying a policy-exception".

// parseClearanceSpecs turns the --clearance flag values into a Clearance. An empty list, or one
// whose only entry is empty, yields an empty clearance -- which clears nothing, and is how a
// clearance is removed.
func parseClearanceSpecs(specs []string) (goclientnew.Clearance, error) {
	clearance := goclientnew.Clearance{}
	for _, spec := range specs {
		spec = strings.TrimSpace(spec)
		if spec == "" {
			continue
		}
		requirement, err := parseClearanceSpec(spec)
		if err != nil {
			return nil, err
		}
		clearance = append(clearance, requirement)
	}
	return clearance, nil
}

func parseClearanceSpec(spec string) (goclientnew.ClearanceRequirement, error) {
	malformed := func() (goclientnew.ClearanceRequirement, error) {
		return goclientnew.ClearanceRequirement{}, fmt.Errorf(
			"--clearance %q must be KEY, KEY=VALUE[,VALUE...], KEY!=VALUE[,VALUE...], or !KEY", spec)
	}

	if key, found := strings.CutPrefix(spec, "!"); found {
		if key == "" || strings.ContainsAny(key, "=,") {
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
		return goclientnew.ClearanceRequirement{
			Key:      key,
			Operator: string(api.ClearanceOperatorNotIn),
			Values:   splitClearanceValues(values),
		}, nil
	}

	if key, values, found := strings.Cut(spec, "="); found {
		if key == "" || values == "" {
			return malformed()
		}
		return goclientnew.ClearanceRequirement{
			Key:      key,
			Operator: string(api.ClearanceOperatorIn),
			Values:   splitClearanceValues(values),
		}, nil
	}

	return goclientnew.ClearanceRequirement{
		Key:      spec,
		Operator: string(api.ClearanceOperatorExists),
	}, nil
}

// splitClearanceValues splits a comma-separated value list. A guard value may not contain a
// comma -- that is exactly why the character class excludes it -- so the split is unambiguous.
func splitClearanceValues(values string) []string {
	parts := strings.Split(values, ",")
	trimmed := make([]string, 0, len(parts))
	for _, part := range parts {
		trimmed = append(trimmed, strings.TrimSpace(part))
	}
	return trimmed
}
