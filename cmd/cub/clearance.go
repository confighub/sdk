// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

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

// changeClearance holds the --clearance flag shared by the commands that change configuration
// data: a function invocation, a run, a unit update. Declared once because the flag means the
// same thing on each of them, and an operator should not have to learn three spellings.
var changeClearance []string

// addClearanceFlag registers --clearance on a command that writes configuration data.
func addClearanceFlag(cmd *cobra.Command) {
	cmd.Flags().StringArrayVar(&changeClearance, "clearance", nil,
		"class of guarded reason this change is cleared for, as KEY, KEY=VALUE[,VALUE...], KEY!=VALUE[,VALUE...], or !KEY to refuse any path carrying KEY (repeatable). A guarded path this does not cover is not written, and the withheld change is reported as a conflict")
}

// clearanceJSON renders the --clearance flag for the wire, or "" when none was given. The
// clearance travels as a JSON query parameter rather than a repeated scalar because a
// requirement is a triple, and flattening it into a string grammar the server would have to
// re-parse is how two grammars drift apart.
func clearanceJSON() string {
	if len(changeClearance) == 0 {
		return ""
	}
	clearance, err := parseClearanceSpecs(changeClearance)
	if err != nil || len(clearance) == 0 {
		return ""
	}
	encoded, err := json.Marshal(clearance)
	if err != nil {
		return ""
	}
	return string(encoded)
}

// addPersistentClearanceFlag is addClearanceFlag for a command whose subcommands all write
// configuration data.
func addPersistentClearanceFlag(cmd *cobra.Command) {
	cmd.PersistentFlags().StringArrayVar(&changeClearance, "clearance", nil,
		"class of guarded reason this change is cleared for, as KEY, KEY=VALUE[,VALUE...], KEY!=VALUE[,VALUE...], or !KEY to refuse any path carrying KEY (repeatable). A guarded path this does not cover is not written, and the withheld change is reported as a conflict")
}

// triggerClearance holds the --clearance flag on trigger create and update. Separate from
// changeClearance because a Trigger's clearance is stored on the Trigger and applies to every
// run of it, where the others describe one operation.
var triggerClearance []string

// addTriggerClearanceFlag registers --clearance on a trigger command.
func addTriggerClearanceFlag(cmd *cobra.Command) {
	cmd.Flags().StringArrayVar(&triggerClearance, "clearance", nil,
		"class of guarded reason this trigger's function is cleared for, as KEY, KEY=VALUE[,VALUE...], KEY!=VALUE[,VALUE...], or !KEY to refuse any path carrying KEY (repeatable). Part of the trigger's Hash, unlike --protect: changing it changes what a re-run lands")
}
