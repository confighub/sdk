// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"fmt"
	"sort"
	"strings"

	"github.com/confighub/sdk/core/cubapi"
	goclientnew "github.com/confighub/sdk/core/openapi/goclient-new"
	"github.com/spf13/cobra"
)

var variantConflictsCmd = &cobra.Command{
	Use:         "conflicts <variant> [--apply [--clearance CLEARANCE]... | --dismiss [--permanent]] [--reason REASON] [--path PATH] [--resource <kind>/[<namespace>/]<name>] [--guard KEY[=VALUE]]...",
	Short:       "Show, apply, or dismiss a variant's outstanding merge conflicts",
	Annotations: map[string]string{"OrgLevel": ""},
	Long: getCommandHelp(`Work with the parts of the last promote's patch that were not applied, across a
whole variant.

A merge reports a conflict for every change it could not make: a path the variant
protects (ProtectedPath), a path the variant owns (Subtracted), a path that could not be
located (UnresolvedPath), a downstream change displaced by an upstream deletion
(DeleteShadowed), a value a guard stopped (Guarded), a guard change the merge was not
cleared for (GuardWithheld), and a change a mutually exclusive field blocked
(ExclusiveWithheld) or made room for (ExclusiveCleared). The merged data is correct as it
stands; a conflict says what the source wanted to change and couldn't. "cub unit
conflicts" explains each reason in full.

With no flags this lists the outstanding conflicts in every unit of the variant, by the
resource they are on. --apply replays the changes they withheld, and --dismiss drops them
without touching the configuration data. Both act on every outstanding conflict in the
variant unless --reason, --path, --resource, or --guard narrows the selection; --resource
names one resource the way the function verbs do, as <kind>/<name> or
<kind>/<namespace>/<name>. --path and --guard select as they do for "cub unit
conflicts": a list element by position or merge key, * for any one element or key, and
the guards a conflict was withheld by. Both take --dry-run, which reports what would be
done and writes nothing.

Applying a Guarded or GuardWithheld conflict needs a --clearance covering the guards at its
path, as "cub unit conflicts" explains; without one the apply is refused.

--dismiss --permanent also records the selection on the unit holding the resource, so a
later promote reporting a conflict it covers leaves it dismissed rather than outstanding.
It needs --resource and --reason. "cub unit conflicts --undismiss" removes one.

The variant is named as <component>/<variant>, or by its space slug.

Examples:
`+"```"+`
  # What did the last promote fail to apply?
  cub variant conflicts web/dev

  # What would taking the base's values do?
  cub variant conflicts web/dev --apply --dry-run -o mutations

  # Take them for one resource only
  cub variant conflicts web/dev --apply --resource Deployment/backend

  # Accept the variant as it is and stop reporting them
  cub variant conflicts web/dev --dismiss

  # This variant sets its own replica count: stop reporting the base's
  cub variant conflicts web/dev --dismiss --permanent --resource Deployment/backend \
    --reason Subtracted --path spec.replicas
`+"```"+`
`, ""),
	Args: cobra.ExactArgs(1),
	RunE: variantConflictsCmdRun,
}

var variantConflictsArgs struct {
	apply     bool
	dismiss   bool
	permanent bool
	reason    string
	path      string
	resource  string
	guards    []string
	clearance []string
	changeSet string
}

func init() {
	variantConflictsCmd.Flags().BoolVar(&variantConflictsArgs.apply, "apply", false,
		"apply the changes the selected conflicts withheld")
	variantConflictsCmd.Flags().BoolVar(&variantConflictsArgs.dismiss, "dismiss", false,
		"drop the selected conflicts without changing the configuration data")
	variantConflictsCmd.Flags().BoolVar(&variantConflictsArgs.permanent, "permanent", false,
		"with --dismiss, also dismiss what the selection covers whenever a later promote reports it")
	variantConflictsCmd.Flags().StringVar(&variantConflictsArgs.reason, "reason", "",
		"select conflicts with this reason: "+conflictReasonsHelp())
	variantConflictsCmd.Flags().StringVar(&variantConflictsArgs.path, "path", "",
		"select conflicts at this path; a list element by position or merge key, * for any one element or key")
	variantConflictsCmd.Flags().StringArrayVar(&variantConflictsArgs.guards, "guard", nil,
		"select conflicts withheld only by these guards, as key or key=value (repeatable)")
	variantConflictsCmd.Flags().StringVar(&variantConflictsArgs.resource, "resource", "",
		"select conflicts on one resource, as <kind>/<name> or <kind>/<namespace>/<name>")
	variantConflictsCmd.Flags().StringArrayVar(&variantConflictsArgs.clearance, "clearance", nil,
		"with --apply, class of guarded reason the applied changes are cleared for, as KEY, KEY=VALUE[;VALUE...], KEY!=VALUE[;VALUE...], or !KEY (repeatable). An apply writing a guarded path this does not cover is refused")
	variantConflictsCmd.Flags().StringVar(&variantConflictsArgs.changeSet, "changeset", "", changeSetFlagHelp)
	variantConflictsCmd.Flags().BoolVar(&dryRun, "dry-run", false,
		"dry run mode: report what --apply or --dismiss would do without writing anything")
	enableWaitFlag(variantConflictsCmd)
	addStandardDisplayFlags(variantConflictsCmd)
	// --resource completes like the function verbs' flag; the variant is a positional
	// here, so the completer offers the organization's resources.
	resourceFlagCommands = append(resourceFlagCommands, variantConflictsCmd)
	variantCmd.AddCommand(variantConflictsCmd)
}

// conflictedUnit is one unit of the variant with conflicts outstanding.
type conflictedUnit struct {
	unit      *goclientnew.Unit
	conflicts []goclientnew.MutationConflict
}

func variantConflictsCmdRun(_ *cobra.Command, args []string) error {
	a := variantConflictsArgs
	if a.apply && a.dismiss {
		return fmt.Errorf("--apply and --dismiss are mutually exclusive")
	}
	if len(a.clearance) > 0 && !a.apply {
		return fmt.Errorf("--clearance says what applied changes are cleared for, so it goes with --apply")
	}
	selecting := a.reason != "" || a.path != "" || a.resource != "" || len(a.guards) > 0
	if selecting && !a.apply && !a.dismiss {
		return fmt.Errorf("--reason, --path, --resource, and --guard select which conflicts to --apply or --dismiss")
	}
	if a.permanent && (!a.dismiss || a.resource == "" || a.reason == "") {
		return fmt.Errorf("--permanent records what --dismiss dismisses on the unit holding one resource: it needs --dismiss, --resource, and --reason")
	}

	space, err := resolveVariantOrSpace(args[0], "SpaceID,Slug")
	if err != nil {
		return err
	}
	units, err := cubapi.ListUnits(ctx, cubClient, cubapi.Where{}.SpaceID(space.Space.SpaceID), cubapi.ListOpts{
		Select:        "UnitID,SpaceID,Slug,Conflicts,HeadMutationNum,HeadRevisionNum",
		IncludeHidden: includeHidden,
	})
	if err != nil {
		return err
	}
	var conflicted []conflictedUnit
	for _, unit := range units {
		if unit == nil || unit.Unit == nil || unit.Unit.Conflicts == nil || len(*unit.Unit.Conflicts) == 0 {
			continue
		}
		conflicted = append(conflicted, conflictedUnit{unit: unit.Unit, conflicts: *unit.Unit.Conflicts})
	}
	sort.Slice(conflicted, func(i, j int) bool { return conflicted[i].unit.Slug < conflicted[j].unit.Slug })

	// --resource narrows to the unit holding that resource, and the selector to its name and type,
	// which keep apart another resource of the same name.
	var selector *goclientnew.UnitConflictSelector
	if selecting {
		selector = &goclientnew.UnitConflictSelector{Reason: a.reason, Path: a.path, Guards: a.guards}
	}
	if a.resource != "" {
		ref, err := parseResourceRef(a.resource)
		if err != nil {
			return err
		}
		selectedSpaceID, selectedSpaceSlug = space.Space.SpaceID.String(), space.Space.Slug
		resolved, err := resolveResourceRef(ref, "")
		if err != nil {
			return err
		}
		selector.ResourceName = resolved.ResourceName
		selector.ResourceType = resolved.ResourceType
		kept := conflicted[:0]
		for _, c := range conflicted {
			if c.unit.UnitID == resolved.UnitID {
				kept = append(kept, c)
			}
		}
		conflicted = kept
		// A dismissal can be recorded before the conflict it is about is reported again.
		if a.permanent && len(conflicted) == 0 {
			for _, unit := range units {
				if unit != nil && unit.Unit != nil && unit.Unit.UnitID == resolved.UnitID {
					conflicted = append(conflicted, conflictedUnit{unit: unit.Unit})
				}
			}
		}
	}

	if !a.apply && !a.dismiss {
		return listVariantConflicts(args[0], conflicted)
	}

	body := goclientnew.UnitConflictsRequest{Action: "Dismiss", DryRun: dryRun, Permanent: a.permanent}
	if body.ChangeSetID, err = resolveOptionalChangeSetID(a.changeSet, space.Space.SpaceID.String()); err != nil {
		return err
	}
	if a.apply {
		body.Action = "Apply"
		clearance, err := parseClearanceSpecs(a.clearance)
		if err != nil {
			return err
		}
		if len(clearance) > 0 {
			body.Clearance = &clearance
		}
	}
	if selector != nil {
		body.Select = []goclientnew.UnitConflictSelector{*selector}
	}
	applied, dismissed, recorded := 0, 0, 0
	var outstanding []goclientnew.MutationConflict
	var touched []*goclientnew.Unit
	for _, c := range conflicted {
		res, err := cubClientNew.ResolveUnitConflictsWithResponse(ctx, c.unit.SpaceID, c.unit.UnitID, body)
		if cubapi.IsAPIError(err, res) {
			return withClearanceFlags(cubapi.InterpretErrorGeneric(err, res))
		}
		resp := res.JSON200
		if resp == nil {
			continue
		}
		applied += resp.Applied
		dismissed += resp.Dismissed
		recorded += resp.Recorded
		if a.apply && resp.Unit != nil && shouldDisplayMutations() && resp.Applied > 0 {
			tprintRaw("")
			displayUnitChanges(space.Space.Slug+"/"+c.unit.Slug, "apply withheld merge changes", resp.Diff)
		}
		if resp.Unit != nil && (resp.Applied > 0 || resp.Dismissed > 0 || resp.Recorded > 0) {
			touched = append(touched, resp.Unit)
		}
		if resp.Conflicts != nil {
			outstanding = append(outstanding, *resp.Conflicts...)
		}
	}
	if !quiet {
		verb := "Applied"
		if dryRun {
			verb = "Would apply"
		}
		if applied > 0 {
			fmt.Printf("%s %d withheld merge %s in variant %s\n", verb, applied, plural("change", applied), args[0])
		}
		if dismissed > 0 {
			verb = "Dismissed"
			if dryRun {
				verb = "Would dismiss"
			}
			fmt.Printf("%s %d merge %s in variant %s\n", verb, dismissed, plural("conflict", dismissed), args[0])
		}
		if recorded > 0 {
			verb = "Recorded"
			if dryRun {
				verb = "Would record"
			}
			fmt.Printf("%s %d conflict %s in variant %s\n", verb, recorded, plural("dismissal", recorded), args[0])
		}
		if applied == 0 && dismissed == 0 && recorded == 0 {
			fmt.Printf("No matching outstanding conflicts in variant %s\n", args[0])
		}
	}
	if !dryRun && wait && len(touched) > 0 {
		printAwaitingTriggers()
		for _, unit := range touched {
			if err := awaitTriggersRemoval(unit); err != nil {
				return err
			}
		}
	}
	if len(outstanding) > 0 {
		label := "Still outstanding:"
		if dryRun {
			label = "Would still be outstanding:"
		}
		fmt.Printf("\n%s\n", label)
		displayResourceConflicts(outstanding)
	}
	return nil
}

func listVariantConflicts(variant string, conflicted []conflictedUnit) error {
	var all []goclientnew.MutationConflict
	for _, c := range conflicted {
		all = append(all, c.conflicts...)
	}
	if len(all) == 0 {
		if !quiet {
			fmt.Printf("No outstanding merge conflicts in variant %s\n", variant)
		}
		return nil
	}
	if renderPayload(all) {
		return nil
	}
	displayResourceConflicts(all)
	return nil
}

// displayResourceConflicts prints one line per conflict, naming the resource by its kind
// and name since the lines may come from several units.
func displayResourceConflicts(conflicts []goclientnew.MutationConflict) {
	table := tableView()
	table.SetHeader([]string{"Reason", "Resource", "Path", "Change"})
	for _, conflict := range conflicts {
		resource := ""
		if conflict.Resource != nil {
			_, kind := splitResourceType(conflict.Resource.ResourceType)
			resource = strings.TrimSpace(kind + " " + conflict.Resource.ResourceName)
		}
		change := ""
		if conflict.Source != nil && conflict.Source.MutationType != nil {
			change = string(*conflict.Source.MutationType)
			if value := strings.TrimSpace(conflict.Source.Value); value != "" {
				change += " " + truncateValue(value)
			}
		}
		table.Append([]string{conflict.Reason, resource, conflict.Path, change})
	}
	table.Render()
}
