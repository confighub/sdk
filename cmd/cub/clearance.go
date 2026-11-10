// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"github.com/spf13/cobra"

	"github.com/confighub/sdk/core/cubapi"
	"github.com/confighub/sdk/core/function/api"
	goclientnew "github.com/confighub/sdk/core/openapi/goclient-new"
)

// A clearance says which classes of guarded reason an operation is cleared for. On the command
// line each requirement is one of four forms, chosen to read like the set-based label selection
// they come from:
//
//	KEY              cleared for the key whatever its value      (Exists)
//	KEY=A[;B...]     cleared for those values                    (In)
//	KEY!=A[;B...]    cleared for any value but those             (NotIn)
//	!KEY             refuse any path carrying the key at all     (DoesNotExist)
//
// The last is a precondition rather than a clearance: it is how an operation says "I am cleared
// for owner guards, but never touch anything carrying a policy-exception".
//
// The values of one requirement are separated by semicolons, which is what separates the items
// of a list inside a flag value throughout cub. They have to be quoted in a shell.

// formatClearance renders a Clearance back in the same four forms the --clearance flag takes,
// so what is displayed is what would be typed to set it again.
func formatClearance(clearance *goclientnew.Clearance) string {
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
			specs = append(specs, requirement.Key+"="+strings.Join(requirement.Values, clearanceValueSeparator))
		case api.ClearanceOperatorNotIn:
			specs = append(specs, requirement.Key+"!="+strings.Join(requirement.Values, clearanceValueSeparator))
		default:
			specs = append(specs, fmt.Sprintf("%s %s %v", requirement.Key, requirement.Operator, requirement.Values))
		}
	}
	return strings.Join(specs, " ")
}

// parseClearanceSpecs turns the --clearance flag values into a Clearance; see
// cubapi.ParseClearance, which manager tools taking the same flag share.
func parseClearanceSpecs(specs []string) (goclientnew.Clearance, error) {
	return cubapi.ParseClearance(specs)
}

// clearanceValueSeparator separates the values of one requirement.
const clearanceValueSeparator = cubapi.ClearanceValueSeparator

// changeClearance holds the --clearance flag shared by the commands that change configuration
// data: a function invocation, a run, a unit update, a promotion, an upload. Declared once because
// the flag means the same thing on each of them, and an operator should not have to learn three
// spellings.
var changeClearance []string

// addClearanceFlag registers --clearance on a command that writes configuration data.
func addClearanceFlag(cmd *cobra.Command) {
	cmd.Flags().StringArrayVar(&changeClearance, "clearance", nil,
		"class of guarded reason this change is cleared for, as KEY, KEY=VALUE[;VALUE...], KEY!=VALUE[;VALUE...], or !KEY to refuse any path carrying KEY (repeatable). A guarded path this does not cover is not written, and the withheld change is reported as a conflict")
}

// clearanceJSON renders the --clearance flag for the wire, or "" when none was given. The
// clearance travels as a JSON query parameter rather than a repeated scalar because a
// requirement is a triple, and flattening it into a string grammar the server would have to
// re-parse is how two grammars drift apart.
//
// A malformed flag is an error rather than an empty result. Dropping it would send the change
// with no clearance, and a guarded path would be withheld with nothing said about why -- the
// operator stated the reason and was told, silently, that they had not.
func clearanceJSON() (string, error) {
	if len(changeClearance) == 0 {
		return "", nil
	}
	clearance, err := parseClearanceSpecs(changeClearance)
	if err != nil {
		return "", err
	}
	if len(clearance) == 0 {
		return "", nil
	}
	encoded, err := json.Marshal(clearance)
	if err != nil {
		return "", err
	}
	return string(encoded), nil
}

// addPersistentClearanceFlag is addClearanceFlag for a command whose subcommands all write
// configuration data.
func addPersistentClearanceFlag(cmd *cobra.Command) {
	cmd.PersistentFlags().StringArrayVar(&changeClearance, "clearance", nil,
		"class of guarded reason this change is cleared for, as KEY, KEY=VALUE[;VALUE...], KEY!=VALUE[;VALUE...], or !KEY to refuse any path carrying KEY (repeatable). A guarded path this does not cover is not written, and the withheld change is reported as a conflict")
}

// triggerClearance holds the --clearance flag on trigger create and update. Separate from
// changeClearance because a Trigger's clearance is stored on the Trigger and applies to every
// run of it, where the others describe one operation.
var triggerClearance []string

// invocationClearance holds the --clearance flag on invocation create and update, for the reason
// triggerClearance is separate.
var invocationClearance []string

// addInvocationClearanceFlag registers --clearance on an invocation command.
func addInvocationClearanceFlag(cmd *cobra.Command) {
	cmd.Flags().StringArrayVar(&invocationClearance, "clearance", nil,
		"class of guarded reason this invocation's functions are cleared for, as KEY, KEY=VALUE[;VALUE...], KEY!=VALUE[;VALUE...], or !KEY to refuse any path carrying KEY (repeatable). Combined with the clearance of the trigger or command that runs it. Part of the invocation's Hash")
}

// addTriggerClearanceFlag registers --clearance on a trigger command.
func addTriggerClearanceFlag(cmd *cobra.Command) {
	cmd.Flags().StringArrayVar(&triggerClearance, "clearance", nil,
		"class of guarded reason this trigger's function is cleared for, as KEY, KEY=VALUE[;VALUE...], KEY!=VALUE[;VALUE...], or !KEY to refuse any path carrying KEY (repeatable). Part of the trigger's Hash, unlike --protect: changing it changes what a re-run lands")
}

// clearanceErrorPhrase introduces the clearance a refusal names as the one that would cover what
// it refused. The server spells it as the request body does, as JSON.
const clearanceErrorPhrase = "a Clearance of "

// withClearanceFlags rewrites the clearance a server refusal names into the --clearance flags
// that state it, so the error says what to type rather than what to send. An error naming none
// is returned as it is.
func withClearanceFlags(err error) error {
	if err == nil {
		return nil
	}
	message := err.Error()
	start := strings.Index(message, clearanceErrorPhrase)
	if start < 0 {
		return err
	}
	rest := message[start+len(clearanceErrorPhrase):]
	decoder := json.NewDecoder(strings.NewReader(rest))
	var clearance goclientnew.Clearance
	if decodeErr := decoder.Decode(&clearance); decodeErr != nil || len(clearance) == 0 {
		return err
	}
	flags := make([]string, 0, len(clearance))
	for _, requirement := range clearance {
		flags = append(flags, fmt.Sprintf("--clearance %q", formatClearance(&goclientnew.Clearance{requirement})))
	}
	return fmt.Errorf("%s%s%s", message[:start], strings.Join(flags, " "), rest[decoder.InputOffset():])
}

// clearanceCoveringGuards is the narrowest clearance admitting the guards that withheld the
// given conflicts: per key, In the values the conflicts name, or Exists for a guard with no
// value. A guard the change forbade itself with DoesNotExist is left out, since no added
// requirement allows it.
func clearanceCoveringGuards(conflicts []goclientnew.MutationConflict) goclientnew.Clearance {
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
