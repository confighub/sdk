// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"github.com/confighub/sdk/cubutil/blame"
	"github.com/spf13/cobra"
)

var unitPathArgs struct {
	resource    string
	history     bool
	noUpstream  bool
	upstreamMax int
}

var unitPathCmd = &cobra.Command{
	Use:   "path <unit> <ref|path>",
	Short: "Show everything recorded about one field of a unit",
	Long: getCommandHelp(`Show everything ConfigHub records about one field of a unit's configuration: its value,
whether it is protected, the guards and other annotations on it, the conflicts and link
findings about it, whether a link needs or provides it, the comments written beside it in the
configuration, the latest thread of review comments about it, and every change from this unit
out to where the value was set -- each with its whole mutation and revision rather than the one
line 'cub unit blame' has room for.

Name the field by the REF 'cub unit blame' printed for it, or any unambiguous prefix of at
least four characters of one, or by its path. A path that more than one resource has needs
--resource. A path that holds other fields, such as a container, lists the fields under it
with their REFs. A field the unit does not have, which blame lists with the A flag because a
conflict names it, is reported with the conflicts about it.

The review thread is the one about the field's resource and path that was commented in most
recently, with its replies; 'cub review-comment list' lists every comment on the unit, and
'cub review-comment create --reply-to' answers one.

For each change it shows:
  - the mutation: the function and its arguments, the link with its update type and
    upstream unit, the trigger or invocation, the merge it was and the upstream revisions it
    took, or the revision a restore went back to
  - the revision: the operation that made it, its description, who made it with what
    client and when, and the ChangeSet, ChangeOrders, Releases and Tags it belongs to

--history adds every change this unit has recorded to the field, oldest first, rather than
only the last. It reads the record of every revision of the unit.

-o json and -o yaml emit the whole report, with each change's Mutation and Revision as the
API returns them.

Examples:
`+"```"+`
  # A field by the REF 'cub unit blame' printed for it
  cub unit path --space prod-app ui 070f84b

  # The same field by its path
  cub unit path --space prod-app ui spec.template.spec.containers.?name=ui.image

  # A path two resources share
  cub unit path --space prod-app ui metadata.name --resource Deployment/ui

  # Every change recorded to the field
  cub unit path --space prod-app ui 070f84b --history
`+"```"+`
`, ""),
	Args: cobra.ExactArgs(2),
	RunE: runUnitPath,
}

func init() {
	addStandardDisplayFlags(unitPathCmd)
	unitPathCmd.Flags().StringVar(&unitPathArgs.resource, "resource", "",
		"the resource the path is in, as <type>/<name> or <name>, when more than one has it")
	unitPathCmd.Flags().BoolVar(&unitPathArgs.history, "history", false,
		"list every change this unit has recorded to the field, not only the last")
	unitPathCmd.Flags().BoolVar(&unitPathArgs.noUpstream, "no-upstream", false,
		"report this unit's own record without following a merge to where the value was set")
	unitPathCmd.Flags().IntVar(&unitPathArgs.upstreamMax, "upstream-depth", blame.MaxUpstreamDepth,
		"how many upstream units to follow before giving up")
	enableOptionalSpace(unitPathCmd)
	unitCmd.AddCommand(unitPathCmd)
}

func runUnitPath(cmd *cobra.Command, args []string) error {
	unit, err := resolveUnit(args[0], selectedSpaceID, "*")
	if err != nil {
		return err
	}
	report, err := blame.Path(ctx, cubClient, unit.Unit.SpaceID, unit.Unit.UnitID, args[1], unitPathArgs.resource,
		unitPathArgs.history, blame.Options{
			MaxUpstreamDepth: unitPathArgs.upstreamMax,
			NoUpstream:       unitPathArgs.noUpstream,
		})
	if err != nil {
		return err
	}

	if renderPayload(report) {
		return nil
	}
	displayUnitPath(report)
	return nil
}
