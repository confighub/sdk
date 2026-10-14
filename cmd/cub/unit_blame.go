// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"github.com/cockroachdb/errors"
	"github.com/confighub/sdk/cubutil/blame"
	"github.com/spf13/cobra"
)

var unitBlameArgs struct {
	path         string
	resource     string
	noUpstream   bool
	upstreamMax  int
	showInternal bool
}

var unitBlameCmd = &cobra.Command{
	Use:   "blame <unit>",
	Short: "Show what set each field of a unit, when, and why",
	Long: getCommandHelp(`Show, for every field of a unit's configuration, what set the value that is
there: the operation, the revision, who ran it, and the change description they gave it.

Where 'cub unit get -o mutations' shows the mutations grouped by resource and path, this
shows the fields, one per line, with the history joined onto each. It is the answer to
"who set this, and why" for a value you are looking at.

A value that arrived from upstream is followed to where it was actually set. A field a
variant took from its base reads as the base's change, and a field the base took from a
rendered chart reads as that chart -- so "did the chart set this, or did we?" is one
command rather than three.

A value a link resolved from another unit is followed into that unit the same way, to the
field the link read: a needs/provides binding's provided path, or the one upstream path of a
TransformPaths link. An Upsert link that copies the upstream's resources is followed as a
merge is. A TransformPaths link that reads several paths or uses a getter, and an Upsert or
Insert link that runs a transform invocation, computed the value from more than one field,
and the walk ends at the link. 'cub unit path' shows the link either way.

Pass --no-upstream to report only this unit's own record, which names the upstream unit
without reading it.

Columns:
  REF       the field's short reference: pass it to 'cub unit path' for everything
            recorded about the field. The same field has the same REF in every revision of
            the unit and in every variant of it
  FLAGS     one letter for each thing there is to know about the field, explained below
  PATH      the field, in the same path syntax functions and links use
  SET BY    the link, trigger, function, or external source that wrote it, or otherwise
            the operation that made the revision, such as UpdateUnit
  USER      who made that change; "automation" for one made by no user
  WHERE     the space whose change it was, after following upstream
  REV       that space's revision number
  WHEN      how long ago

-o wide adds VALUE, what is there now; FUNCTION, the function a link or trigger ran; MUT, the
mutation number; VIA, the change in this unit that brought a value set upstream, such as the
upgrade or the link that resolved it; and CHANGE ORDER, the ChangeOrder that made the change
in this unit.

Flags:
  P  protected: a local override, which a merge from upstream leaves alone
  G  guarded: reasons are recorded for the value it holds, and an operation has to be
     cleared for them before it may overwrite it
  X  conflict: a merge or resolve dropped an upstream change to the field
  F  finding: a link's resolve could not do what it names here
  E  validation error: a validating trigger failed a check that names the field, which
     blocks a release
  W  validation warning: a validating trigger with --warn failed a check that names the field
  U  an upgrade from upstream brought the value to this unit, through an UpgradeUnit link
  L  another link set the value in this unit, by resolving or merging
  T  a trigger set the value in this unit
  C  the change in this unit was made by a ChangeOrder
  N  not released: the value changed after the unit's last release, or the unit has never
     been released. Never shown in a space with no release target, which has nothing to
     release
  n  needed: the value is to come from an upstream unit, through a NeedsProvides link,
     whether or not a link has bound it yet
  p  provided: the value is offered to downstream units, through NeedsProvides links

U, L, T, C and N are about the change in this unit, which for a value set upstream is the
merge that brought it here rather than the change SET BY names. n and p are lowercase
because they are informational: they say what part the field plays in NeedsProvides links,
not that anything is wrong or pending. --verbose prints the guard reasons, conflicts,
findings, failed checks, and needed and provided attributes under each field.

Every toolchain is read: a Kubernetes manifest, and equally an AppConfig JSON, YAML,
TOML, INI, Properties, or Env unit, or a ConfigHub one. The unit's data is read in its
own format, and array elements are addressed the way that format addresses them -- by
merge key where it has them ("?name=server" for a Kubernetes container), by position
where it does not.

Use --path to ask about one field, --resource to restrict to one resource, and --verbose
to see each change description and the full upstream chain rather than its end. -o json
emits one object per field. 'cub variant blame' does the same for every unit of a variant.

Examples:
`+"```"+`
  # Every field of a unit, and what set it.
  cub unit blame --space apptique-dev deployment-frontend

  # One field: did the chart set this, our base, or this variant?
  cub unit blame --space apptique-dev deployment-frontend --path spec.replicas

  # Every container image in the unit, with the change descriptions.
  cub unit blame --space apptique-dev deployment-frontend --path image --verbose

  # This unit's own record, without reading upstream.
  cub unit blame --space apptique-dev deployment-frontend --no-upstream

  # With the values, functions, and ChangeOrders.
  cub unit blame --space apptique-dev deployment-frontend -o wide

  # Everything about one field, by the REF blame printed for it.
  cub unit path --space apptique-dev deployment-frontend 3f9a2c1
`+"```"+`
`, ""),
	Args: cobra.MaximumNArgs(1),
	RunE: runUnitBlame,
}

func init() {
	addStandardGetFlags(unitBlameCmd)
	unitBlameCmd.Flags().StringVar(&unitBlameArgs.path, "path", "",
		"only fields whose path contains this substring")
	unitBlameCmd.Flags().StringVar(&unitBlameArgs.resource, "resource", "",
		"only fields of this resource, as <type>/<name> or <name>")
	unitBlameCmd.Flags().BoolVar(&unitBlameArgs.noUpstream, "no-upstream", false,
		"report this unit's own record without following a merge to where the value was set")
	unitBlameCmd.Flags().IntVar(&unitBlameArgs.upstreamMax, "upstream-depth", blame.MaxUpstreamDepth,
		"how many upstream units to follow before giving up")
	unitBlameCmd.Flags().BoolVar(&unitBlameArgs.showInternal, "show-comments", false,
		"include the comment-carrying pseudo-fields ($comment$...) that hold YAML comments")
	enableOptionalSpace(unitBlameCmd)
	unitCmd.AddCommand(unitBlameCmd)
}

func runUnitBlame(cmd *cobra.Command, args []string) error {
	if len(args) == 0 {
		return errors.New("name a unit to blame")
	}
	unit, err := resolveUnit(args[0], selectedSpaceID, "*")
	if err != nil {
		return err
	}

	resolver := blame.NewResolver(cubClient, blame.Options{
		MaxUpstreamDepth: unitBlameArgs.upstreamMax,
		NoUpstream:       unitBlameArgs.noUpstream,
	})
	fields, err := resolver.Unit(ctx, unit.Unit)
	if err != nil {
		return err
	}
	fields = blame.Filter(fields, unitBlameArgs.path, unitBlameArgs.resource, unitBlameArgs.showInternal)

	if renderPayload(fields) {
		return nil
	}
	displayBlame(fields, false)
	return nil
}
