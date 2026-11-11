// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package write

import (
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/confighub/sdk/cliutil"
	"github.com/confighub/sdk/core/cubapi"
	api "github.com/confighub/sdk/core/function/api"
	goclientnew "github.com/confighub/sdk/core/openapi/goclient-new"
)

// CommitFlags are the flags every mutating command of a manager tool takes: --commit and
// --change-desc, which make it write; the guards it records on what it writes and the
// clearance it writes with, as cub's own writes take them; and the ChangeSet to make it in.
//
// A value a tool writes into a Unit whose configuration is uploaded from somewhere else, such as
// a rendered chart, is lost the next time it is uploaded unless it is guarded, as in
// --guard Override=External; and a Unit that is already guarded is written only where
// --clearance covers its guards.
type CommitFlags struct {
	cliutil.CommitFlags
	Guards          []string
	InheritedGuards []string
	Clearance       []string
	// ChangeSet is the ChangeSet to make the change in, by space/slug or UUID.
	ChangeSet string
}

// Bind registers --commit, --change-desc, --guard, --inherited-guard, --changeset and --clearance
// on cmd.
func (c *CommitFlags) Bind(cmd *cobra.Command) {
	c.CommitFlags.Bind(cmd)
	f := cmd.Flags()
	f.StringArrayVar(&c.Guards, "guard", nil,
		"reason to record on the paths this change writes, as KEY=VALUE (repeatable), such as Override=External for a value the configuration the Unit is uploaded from does not have; the change is cleared for it too")
	f.StringArrayVar(&c.InheritedGuards, "inherited-guard", nil,
		"reason to record as --guard does, which clones of the Unit receive and merges carry to the Units downstream (repeatable)")
	f.StringVar(&c.ChangeSet, "changeset", "",
		"ChangeSet to make the change in, as space/slug or UUID, so it can be reviewed as one: a commit opens it on the Units the change selects, all of them or none, and leaves it open; its revisions cannot be released until it is closed, as with 'cub variant changeset close'")
	f.StringArrayVar(&c.Clearance, "clearance", nil,
		"guarded reason the change is cleared for (repeatable): KEY, KEY=VALUE[;VALUE...], KEY!=VALUE[;VALUE...], or !KEY. A path whose guards it does not cover is left as it is, and reported")
}

// Change validates the flags and returns the change they describe: a dry run unless --commit,
// asking for each Unit's diff either way, as [Change] does, with the guards to record and the
// clearance to write with. summary is suggested when --change-desc is missing.
//
// The clearance includes each guard being recorded, because a change has to be cleared for the
// guards on a path before overwriting it: without that, running a command a second time would
// withhold the very paths the first run guarded.
func (c CommitFlags) Change(summary string) (cubapi.Change, error) {
	changeDesc, dryRun, err := c.Validate(summary)
	if err != nil {
		return cubapi.Change{}, err
	}
	change := Change(changeDesc, dryRun)
	guards, err := cubapi.ParseGuards("guard", c.Guards)
	if err != nil {
		return cubapi.Change{}, err
	}
	inherited, err := cubapi.ParseGuards("inherited-guard", c.InheritedGuards)
	if err != nil {
		return cubapi.Change{}, err
	}
	clearance, err := cubapi.ParseClearance(c.Clearance)
	if err != nil {
		return cubapi.Change{}, err
	}
	clearance = append(clearance, clearanceFor(guards)...)
	clearance = append(clearance, clearanceFor(inherited)...)
	if len(guards) > 0 {
		change.Guards = guards
	}
	if len(inherited) > 0 {
		change.InheritedGuards = inherited
	}
	if len(clearance) > 0 {
		change.Clearance = clearance
	}
	change.ChangeSet = strings.TrimSpace(c.ChangeSet)
	return change, nil
}

// clearanceFor is a clearance for exactly the guards given, in key order so a request is the
// same from one run to the next.
func clearanceFor(guards goclientnew.GuardStamp) goclientnew.Clearance {
	keys := make([]string, 0, len(guards))
	for key := range guards {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	var clearance goclientnew.Clearance
	for _, key := range keys {
		clearance = append(clearance, goclientnew.ClearanceRequirement{
			Key: key, Operator: string(api.ClearanceOperatorIn), Values: []string{guards[key]},
		})
	}
	return clearance
}
