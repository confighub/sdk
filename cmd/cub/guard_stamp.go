// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	goclientnew "github.com/confighub/sdk/core/openapi/goclient-new"
)

// A change can state the reasons it has for what it writes, so that a later operation has to
// know about them before overwriting. This is --guard, the guard analogue of --protect: where
// --protect claims the paths the operation wrote, --guard says why they hold what they hold.
//
// On the command line each guard is one KEY=VALUE pair:
//
//	owner=transform-link            this field is maintained by a link
//	policy-exception=host-network   this value breaks a default policy on purpose
//
// It names no path, which is the point. The paths are the ones the operation wrote, exactly as
// they are for --protect. `cub unit set-guard --guard` spells the same flag with a resource and
// a path in front of it, because there it is guarding somewhere the command is not writing.
//
// It only ever adds and overwrites the keys it names. Retiring a guard is
// `cub unit set-guard --remove-guard`, which says so deliberately -- a removal is a decision
// about the Unit's policy rather than a by-product of writing a value.

const guardFlagUsage = "reason to record on the paths this change writes, as KEY=VALUE (repeatable). A later operation must be cleared for it before overwriting those paths. Adds and overwrites only; retiring a guard is cub unit set-guard --remove-guard"

// parseGuardStampSpecs turns --guard flag values into the key/value map the wire carries.
func parseGuardStampSpecs(specs []string) (goclientnew.GuardStamp, error) {
	stamp := goclientnew.GuardStamp{}
	for _, spec := range specs {
		spec = strings.TrimSpace(spec)
		if spec == "" {
			continue
		}
		key, value, found := strings.Cut(spec, "=")
		if !found || key == "" {
			return nil, fmt.Errorf("--guard %q must be KEY=VALUE", spec)
		}
		stamp[key] = value
	}
	if len(stamp) == 0 {
		return nil, nil
	}
	return stamp, nil
}

// formatGuardStamp renders guards back in the form the --guard flag takes, so what is displayed
// is what would be typed to set it again. Sorted, because a map has no order and a listing that
// reshuffles between runs is a listing nobody can diff.
func formatGuardStamp(stamp *goclientnew.GuardStamp) string {
	if stamp == nil || len(*stamp) == 0 {
		return ""
	}
	specs := make([]string, 0, len(*stamp))
	for key, value := range *stamp {
		specs = append(specs, key+"="+value)
	}
	sort.Strings(specs)
	return strings.Join(specs, " ")
}

// changeGuards holds the --guard flag shared by the commands that change configuration data: a
// function invocation, a run, a unit update. Declared once because the flag means the same thing
// on each of them, for the reason changeClearance is.
var changeGuards []string

// addGuardFlag registers --guard on a command that writes configuration data.
func addGuardFlag(cmd *cobra.Command) {
	cmd.Flags().StringArrayVar(&changeGuards, "guard", nil, guardFlagUsage)
}

// addPersistentGuardFlag is addGuardFlag for a command whose subcommands all write configuration
// data.
func addPersistentGuardFlag(cmd *cobra.Command) {
	cmd.PersistentFlags().StringArrayVar(&changeGuards, "guard", nil, guardFlagUsage)
}

// guardsJSON renders the --guard flag for the wire, or "" when none was given. The guards travel
// as a JSON query parameter rather than a repeated scalar for the reason the clearance does:
// one grammar, parsed where it is written, rather than two that drift apart.
//
// A malformed flag is an error rather than an empty result. Dropping it would send the change
// with no guards and report success, so the operator would be told the reason was recorded when
// nothing was -- the silent failure the whole design is built to avoid.
func guardsJSON() (string, error) {
	if len(changeGuards) == 0 {
		return "", nil
	}
	stamp, err := parseGuardStampSpecs(changeGuards)
	if err != nil {
		return "", err
	}
	if len(stamp) == 0 {
		return "", nil
	}
	encoded, err := json.Marshal(stamp)
	if err != nil {
		return "", err
	}
	return string(encoded), nil
}

// triggerGuards and linkGuards hold the --guard flag on the entities that carry a standing one.
// Separate from changeGuards because they are stored and apply to every run, where the others
// describe one operation.
var (
	triggerGuards []string
	linkGuards    []string
)

// addTriggerGuardFlag registers --guard on a trigger command.
func addTriggerGuardFlag(cmd *cobra.Command) {
	cmd.Flags().StringArrayVar(&triggerGuards, "guard", nil,
		"reason to record on the paths this trigger's function writes, as KEY=VALUE (repeatable). A later operation must be cleared for it before overwriting those paths. Part of the trigger's Hash, unlike --protect: changing it changes what a re-run leaves behind")
}
