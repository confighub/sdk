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
	Use:         "conflicts <variant> [--apply | --dismiss] [--reason REASON] [--path PATH] [--resource <kind>/[<namespace>/]<name>]",
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
variant unless --reason, --path, or --resource narrows the selection; --resource names
one resource the way the function verbs do, as <kind>/<name> or
<kind>/<namespace>/<name>. Both take --dry-run, which reports what would be done and
writes nothing.

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
`+"```"+`
`, ""),
	Args: cobra.ExactArgs(1),
	RunE: variantConflictsCmdRun,
}

var variantConflictsArgs struct {
	apply    bool
	dismiss  bool
	reason   string
	path     string
	resource string
}

func init() {
	variantConflictsCmd.Flags().BoolVar(&variantConflictsArgs.apply, "apply", false,
		"apply the changes the selected conflicts withheld")
	variantConflictsCmd.Flags().BoolVar(&variantConflictsArgs.dismiss, "dismiss", false,
		"drop the selected conflicts without changing the configuration data")
	variantConflictsCmd.Flags().StringVar(&variantConflictsArgs.reason, "reason", "",
		"select conflicts with this reason: Subtracted, DeleteShadowed, ProtectedPath, UnresolvedPath, Guarded, GuardWithheld, ExclusiveWithheld, or ExclusiveCleared")
	variantConflictsCmd.Flags().StringVar(&variantConflictsArgs.path, "path", "",
		"select conflicts at this path")
	variantConflictsCmd.Flags().StringVar(&variantConflictsArgs.resource, "resource", "",
		"select conflicts on one resource, as <kind>/<name> or <kind>/<namespace>/<name>")
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
	selecting := a.reason != "" || a.path != "" || a.resource != ""
	if selecting && !a.apply && !a.dismiss {
		return fmt.Errorf("--reason, --path, and --resource select which conflicts to --apply or --dismiss")
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

	// --resource narrows to the unit holding that resource, and the selector to its name.
	var selector *goclientnew.UnitConflictSelector
	if selecting {
		selector = &goclientnew.UnitConflictSelector{Reason: a.reason, Path: a.path}
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
		kept := conflicted[:0]
		for _, c := range conflicted {
			if c.unit.UnitID == resolved.UnitID {
				kept = append(kept, c)
			}
		}
		conflicted = kept
	}

	if !a.apply && !a.dismiss {
		return listVariantConflicts(args[0], conflicted)
	}

	body := goclientnew.UnitConflictsRequest{Action: "Dismiss", DryRun: dryRun}
	if a.apply {
		body.Action = "Apply"
	}
	if selector != nil {
		body.Select = []goclientnew.UnitConflictSelector{*selector}
	}
	applied, dismissed := 0, 0
	var outstanding []goclientnew.MutationConflict
	var touched []*goclientnew.Unit
	for _, c := range conflicted {
		res, err := cubClientNew.ResolveUnitConflictsWithResponse(ctx, c.unit.SpaceID, c.unit.UnitID, body)
		if cubapi.IsAPIError(err, res) {
			return cubapi.InterpretErrorGeneric(err, res)
		}
		resp := res.JSON200
		if resp == nil {
			continue
		}
		applied += resp.Applied
		dismissed += resp.Dismissed
		if a.apply && resp.Unit != nil && shouldDisplayMutations() && resp.Applied > 0 {
			tprintRaw("")
			displayUnitChanges(space.Space.Slug+"/"+c.unit.Slug, "apply withheld merge changes", resp.Diff)
		}
		if resp.Unit != nil && (resp.Applied > 0 || resp.Dismissed > 0) {
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
		if applied == 0 && dismissed == 0 {
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
