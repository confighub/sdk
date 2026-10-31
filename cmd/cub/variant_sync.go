// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"fmt"

	"github.com/cockroachdb/errors"
	"github.com/confighub/sdk/core/cubapi"
	"github.com/spf13/cobra"
)

var variantSyncArgs struct {
	changeDescription string
	protect           bool
	dryRun            bool
	includeConfigData bool
}

var variantSyncCmd = &cobra.Command{
	Use:         "sync <source-variant> <target-variant>",
	Short:       "Give one variant the configuration and links of another",
	Annotations: map[string]string{"OrgLevel": ""},
	Long: getCommandHelp(`Give the target variant's units the source variant's configuration, and the target the
source's links it does not have.

Promotion carries a change down from a variant's upstream. Two variants of the same base
are not each other's upstream, so promotion cannot carry one's configuration to the other;
this does. Each variant is named as <component>/<variant> or by its space slug.

Units are paired by slug. Each pair is merged as one squashed diff of the source unit from
its first revision to its head: everything that has happened to it since it was made. A unit
made empty and then given its content has an empty first revision, so that is the whole of
its configuration. For a clone, whose first revision is what its upstream gave it, it is the
source variant's own changes and the upgrades it took, and what the clone started with stays
as the target has it: compare with --dry-run --include-config-data. The merge is like any
other: a path the target protects keeps the target's value, and the change is reported as a
conflict.

A source unit the target does not have is cloned from the source unit's upstream first, as
promotion clones a unit its upstream added, and moved into the target's namespace when the
target space records one; then it is merged like the rest. One with no upstream is skipped.

Then each of the source's links that the target has no link of the same slug for is copied:
from the target's unit of the same slug, and to the target's unit of the same slug when the
link is within the source variant, or to the same unit when it leads out of it. A copy keeps
what the link resolves and its UpstreamLinkID, and starts with no merge history or delete
gates. UpgradeUnit links are not copied: each is its unit's upstream, which this does not
change. A link the target has a link of the same slug for is left alone, and the target's
links the source does not have are kept.

--protect protects the paths each merge wrote, so that a later upgrade from the target's
upstream does not write the upstream's values back over them. It is a step of its own after
each merge, since a merge is itself subject to protection and cannot protect what it writes.
A resource the merge added whole has no path of its own, and is protected as a whole; the
Protected column counts it as one.

The table says, for each unit, what was done and whether its configuration now says what the
source's does: the same documents with the same values, whatever the order of their keys or
the style of their lists, for YAML and JSON, and the same text for other formats. A path the
target protects keeps it from matching.

Examples:
`+"```"+`
  # What syncing staging-2 to staging would do
  cub variant sync app/staging app/staging-2 --dry-run

  # Sync it, protecting what the sync changes against the base's later upgrades
  cub variant sync app/staging app/staging-2 --protect --change-desc "Sync from staging"

  # Each unit's changes, path by path
  cub variant sync app/staging app/staging-2 --dry-run -o mutations
`+"```"+`
`, ""),
	Args: cobra.ExactArgs(2),
	RunE: variantSyncCmdRun,
}

func init() {
	variantSyncCmd.Flags().StringVar(&variantSyncArgs.changeDescription, "change-desc", "", "change description recorded on the units the sync writes")
	variantSyncCmd.Flags().BoolVar(&variantSyncArgs.protect, "protect", false, "protect the paths each merge writes, so a later upgrade from the target's upstream does not overwrite them")
	variantSyncCmd.Flags().BoolVar(&variantSyncArgs.dryRun, "dry-run", false, "report what the sync would do without changing anything")
	variantSyncCmd.Flags().BoolVar(&variantSyncArgs.includeConfigData, "include-config-data", false, "return the configuration each merge produces, or with --dry-run would produce, as ConfigData in structured output (-o json, -o yaml, -o jq=...)")
	enableWaitFlag(variantSyncCmd)
	addStandardDisplayFlags(variantSyncCmd)
	variantCmd.AddCommand(variantSyncCmd)
}

func variantSyncCmdRun(_ *cobra.Command, args []string) error {
	source, err := resolveVariantOrSpace(args[0], "*")
	if err != nil {
		return err
	}
	target, err := resolveVariantOrSpace(args[1], "*")
	if err != nil {
		return err
	}
	if source.Space.SpaceID == target.Space.SpaceID {
		return errors.New("the source and target are the same space")
	}
	changeDescription := variantSyncArgs.changeDescription
	if changeDescription == "" {
		changeDescription = "Sync from " + source.Space.Slug
	}

	result, err := cubapi.SyncVariant(ctx, cubClient, cubapi.VariantSyncSpec{
		Source:            source.Space.SpaceID,
		Target:            target.Space.SpaceID,
		Protect:           variantSyncArgs.protect,
		ChangeDescription: changeDescription,
		DryRun:            variantSyncArgs.dryRun,
		IncludeConfigData: variantSyncArgs.includeConfigData,
	})
	if err != nil {
		return err
	}

	// The copied links resolve on their own, and their resolves write to the target's units.
	if wait && !variantSyncArgs.dryRun {
		awaited := map[string]bool{}
		for _, unit := range result.Units {
			if unit.Unit == nil || awaited[unit.Slug] {
				continue
			}
			awaited[unit.Slug] = true
			if err := awaitTriggersRemoval(unit.Unit); err != nil {
				return err
			}
		}
	}

	if renderPayload(result) {
		return nil
	}
	if shouldDisplayMutations() {
		for _, unit := range result.Units {
			if unit.Diff != nil {
				displayUnitChanges(target.Space.Slug+"/"+unit.Slug, "sync from "+source.Space.Slug, unit.Diff)
			}
		}
		return nil
	}
	displayVariantSync(result, variantSyncArgs.dryRun)
	return nil
}

func displayVariantSync(result *cubapi.VariantSyncResult, dryRun bool) {
	if dryRun {
		tprintRaw("Dry run: nothing was changed.")
		tprintRaw("")
	}
	table := tableView()
	if !noheader {
		table.SetHeader([]string{"Unit", "Action", "Matches Source", "Conflicts", "Protected", "Details"})
	}
	failed := 0
	for _, unit := range result.Units {
		matches := "no"
		if unit.MatchesSource {
			matches = "yes"
		}
		if unit.Action == cubapi.UnitSyncSkipped || unit.Action == cubapi.UnitSyncFailed || unit.Reason != "" && unit.Diff == nil {
			matches = ""
		}
		if unit.Action == cubapi.UnitSyncFailed {
			failed++
		}
		details := unit.Reason
		protected := ""
		if unit.Protected > 0 {
			protected = fmt.Sprintf("%d", unit.Protected)
		}
		conflicts := ""
		if len(unit.Conflicts) > 0 {
			conflicts = fmt.Sprintf("%d", len(unit.Conflicts))
		}
		table.Append([]string{unit.Slug, unit.Action, matches, conflicts, protected, details})
	}
	table.Render()

	if len(result.Links) > 0 {
		tprintRaw("")
		links := tableView()
		if !noheader {
			links.SetHeader([]string{"Link", "From", "To", "Update Type", "Action", "Details"})
		}
		for _, link := range result.Links {
			action := link.Action
			if dryRun && action == cubapi.LinkSyncCreated {
				action = "would create"
			}
			if link.Action == cubapi.LinkSyncFailed {
				failed++
			}
			links.Append([]string{link.Slug, link.FromUnit, link.ToUnit, link.UpdateType, action, link.Reason})
		}
		links.Render()
	}
	if failed > 0 {
		tprintRaw("")
		tprintRaw(fmt.Sprintf("%d failed; the rest were synced.", failed))
	}
	if !noheader {
		tprintRaw("")
		tprintRaw(fmt.Sprintf("%s'cub unit conflicts <unit>' shows a unit's conflicts; -o mutations shows what each merge changed.%s", colorDim, colorReset))
	}
}
