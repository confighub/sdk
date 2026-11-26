// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/confighub/sdk/core/cubapi"
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

// formatClearance is cubapi.FormatClearance, named for its callers here.
func formatClearance(clearance *goclientnew.Clearance) string {
	return cubapi.FormatClearance(clearance)
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

// sentClearance is the clearance the command's change was sent with, cubapi.DefaultClearance
// where it stated none, for the suggestions of what to run it again with
// (cubapi.ClearanceToRetry).
var sentClearance goclientnew.Clearance

// clearanceFlagUsage describes --clearance on a command that writes configuration data.
const clearanceFlagUsage = "class of guarded reason this change is cleared for, as KEY, KEY=VALUE[;VALUE...], KEY!=VALUE[;VALUE...], or !KEY to refuse any path carrying KEY (repeatable). A guarded path this does not cover is not written, and the withheld change is reported as a conflict"

// clearanceDefaultUsage describes the clearance a change that is not a merge has without the flag.
const clearanceDefaultUsage = ". Without it the change is cleared for Override guards, whatever their value, as a merge is not; a clearance given replaces that, and --clearance '' clears nothing"

// addClearanceFlag registers --clearance on a command that writes configuration data and does
// not merge, which is cleared for cubapi.DefaultClearance when the flag is not given.
func addClearanceFlag(cmd *cobra.Command) {
	cmd.Flags().StringArrayVar(&changeClearance, "clearance", nil, clearanceFlagUsage+clearanceDefaultUsage)
}

// addMergeClearanceFlag registers --clearance on a command that merges, such as a promotion or
// an upload, which is cleared for nothing when the flag is not given.
func addMergeClearanceFlag(cmd *cobra.Command) {
	cmd.Flags().StringArrayVar(&changeClearance, "clearance", nil, clearanceFlagUsage)
}

// clearanceJSON renders the clearance of the change for the wire, or "" for none: the
// --clearance flag, or cubapi.DefaultClearance when it was not given and the change is not a
// merge (see cubapi.OperationClearance). The clearance travels as a JSON query parameter rather
// than a repeated scalar because a requirement is a triple, and flattening it into a string
// grammar the server would have to re-parse is how two grammars drift apart.
//
// A malformed flag is an error rather than an empty result. Dropping it would send the change
// with no clearance, and a guarded path would be withheld with nothing said about why -- the
// operator stated the reason and was told, silently, that they had not.
func clearanceJSON(merge bool) (string, error) {
	clearance, err := cubapi.OperationClearance(changeClearance, merge)
	if err != nil {
		return "", err
	}
	sentClearance = clearance
	if len(clearance) == 0 {
		return "", nil
	}
	encoded, err := json.Marshal(clearance)
	if err != nil {
		return "", err
	}
	return string(encoded), nil
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
		"class of guarded reason this trigger's function is cleared for, as KEY, KEY=VALUE[;VALUE...], KEY!=VALUE[;VALUE...], or !KEY to refuse any path carrying KEY (repeatable). Part of the trigger's Hash: changing it changes what a re-run lands")
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
	// The clearance named covers what was refused; a request that relied on the default has to
	// state it too, since a clearance given replaces it.
	clearance = cubapi.RestateDefaultClearance(clearance, sentClearance)
	return fmt.Errorf("%s%s%s", message[:start], clearanceFlags(clearance), rest[decoder.InputOffset():])
}

// clearanceToRetry is cubapi.ClearanceToRetry for the change this command sent.
func clearanceToRetry(conflicts []goclientnew.MutationConflict) goclientnew.Clearance {
	return cubapi.ClearanceToRetry(conflicts, sentClearance)
}
