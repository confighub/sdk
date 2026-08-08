// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"fmt"
	"strings"

	"github.com/confighub/sdk/core/cubapi"
	goclientnew "github.com/confighub/sdk/core/openapi/goclient-new"
	"github.com/google/uuid"
	"github.com/spf13/cobra"
)

var unitConflictsCmd = &cobra.Command{
	Use:   "conflicts <unit-slug> [--apply | --dismiss] [--reason REASON] [--path PATH] [--resource NAME]",
	Short: "Show, apply, or dismiss a unit's outstanding merge conflicts",
	Long: getCommandHelp(`Work with the parts of the last merge's patch that were not applied.

A merge reports a conflict for every change it could not make: a path the
downstream owns (Subtracted), a path the downstream protects (ProtectedPath),
a path that could not be located in this unit (UnresolvedPath), and each
downstream change displaced by an upstream deletion (DeleteShadowed). The
merged data is correct as it stands -- a conflict says what the source wanted
to change and couldn't.

ExclusiveWithheld is one of those, for a change that could not be made because
this unit owns a field mutually exclusive with it -- the source switched a
volume to a secret and this unit had already chosen an emptyDir. Applying it
performs the switch. ExclusiveCleared reports the opposite: the source's change
applied and a value this unit had was removed to make room for it, because
keeping both is a resource that will not apply. It carries the removed value.

The conflicts stay on the unit until they are dealt with, so a merge that
dropped half its patch is still visible afterwards rather than only in the
response of the request that ran it. The next merge replaces them with its own,
and a merge that lands cleanly clears them.

With no flags this lists the outstanding conflicts. --apply replays the changes
they withheld, with every path eligible so nothing filters them out a second
time. --dismiss drops them without touching the configuration data. Both act on
every outstanding conflict unless --reason, --path, or --resource narrows the
selection.

Examples:
`+"```"+`
  # What did the last merge fail to apply?
  cub unit conflicts my-unit

  # Apply the upstream changes that were withheld because this unit owns the path
  cub unit conflicts my-unit --apply --reason Subtracted

  # Accept the current state and stop reporting them
  cub unit conflicts my-unit --dismiss

  # Which units in the space merged with paths the patch could not locate?
  cub unit list --where "Conflicts.*.Reason = 'UnresolvedPath'"
`+"```"+`
`, ""),
	Args: cobra.ExactArgs(1),
	RunE: unitConflictsCmdRun,
}

var (
	conflictsApply        bool
	conflictsDismiss      bool
	conflictsReason       string
	conflictsPath         string
	conflictsResourceName string
)

func init() {
	unitConflictsCmd.Flags().BoolVar(&conflictsApply, "apply", false,
		"apply the changes the selected conflicts withheld")
	unitConflictsCmd.Flags().BoolVar(&conflictsDismiss, "dismiss", false,
		"drop the selected conflicts without changing the configuration data")
	unitConflictsCmd.Flags().StringVar(&conflictsReason, "reason", "",
		"select conflicts with this reason: Subtracted, DeleteShadowed, ProtectedPath, UnresolvedPath, ExclusiveWithheld, or ExclusiveCleared")
	unitConflictsCmd.Flags().StringVar(&conflictsPath, "path", "",
		"select conflicts at this path")
	unitConflictsCmd.Flags().StringVar(&conflictsResourceName, "resource", "",
		"select conflicts on this resource, by name")
	addStandardDisplayFlags(unitConflictsCmd)
	unitCmd.AddCommand(unitConflictsCmd)
}

func unitConflictsCmdRun(_ *cobra.Command, args []string) error {
	if conflictsApply && conflictsDismiss {
		return fmt.Errorf("--apply and --dismiss are mutually exclusive")
	}
	selecting := conflictsReason != "" || conflictsPath != "" || conflictsResourceName != ""
	if selecting && !conflictsApply && !conflictsDismiss {
		return fmt.Errorf("--reason, --path, and --resource select which conflicts to --apply or --dismiss")
	}

	if !conflictsApply && !conflictsDismiss {
		return listUnitConflicts(args[0])
	}

	configUnit, err := apiGetUnitFromSlug(args[0], "UnitID,SpaceID")
	if err != nil {
		return err
	}

	body := goclientnew.UnitConflictsRequest{Action: "Dismiss"}
	if conflictsApply {
		body.Action = "Apply"
	}
	if selecting {
		body.Select = []goclientnew.UnitConflictSelector{{
			Reason:       conflictsReason,
			Path:         conflictsPath,
			ResourceName: conflictsResourceName,
		}}
	}

	res, err := cubClientNew.ResolveUnitConflictsWithResponse(ctx, uuid.MustParse(selectedSpaceID), configUnit.UnitID, body)
	if cubapi.IsAPIError(err, res) {
		return cubapi.InterpretErrorGeneric(err, res)
	}
	resp := res.JSON200
	if resp == nil {
		return nil
	}
	if !quiet {
		if resp.Applied > 0 {
			fmt.Printf("Applied %d withheld merge %s on unit %s\n",
				resp.Applied, plural("change", resp.Applied), args[0])
		}
		if resp.Dismissed > 0 {
			fmt.Printf("Dismissed %d merge %s on unit %s\n",
				resp.Dismissed, plural("conflict", resp.Dismissed), args[0])
		}
		if resp.Applied == 0 && resp.Dismissed == 0 {
			fmt.Printf("No matching outstanding conflicts on unit %s\n", args[0])
		}
	}
	if resp.Conflicts != nil && len(*resp.Conflicts) > 0 {
		fmt.Printf("\nStill outstanding:\n")
		displayConflicts(*resp.Conflicts)
	}
	return nil
}

func listUnitConflicts(slug string) error {
	configUnit, err := apiGetUnitFromSlug(slug, "UnitID,SpaceID,Conflicts")
	if err != nil {
		return err
	}
	if configUnit.Conflicts == nil || len(*configUnit.Conflicts) == 0 {
		if !quiet {
			fmt.Printf("No outstanding merge conflicts on unit %s\n", slug)
		}
		return nil
	}
	if jsonOutput {
		displayJSON(*configUnit.Conflicts)
		return nil
	}
	displayConflicts(*configUnit.Conflicts)
	return nil
}

// displayConflicts prints one line per conflict: why it was dropped, what it would have
// changed, and where.
func displayConflicts(conflicts []goclientnew.MutationConflict) {
	table := tableView()
	table.SetHeader([]string{"Reason", "Resource", "Path", "Change"})
	for _, conflict := range conflicts {
		resourceName := ""
		if conflict.Resource != nil {
			resourceName = conflict.Resource.ResourceName
		}
		change := ""
		if conflict.Source != nil && conflict.Source.MutationType != nil {
			change = string(*conflict.Source.MutationType)
			if value := strings.TrimSpace(conflict.Source.Value); value != "" {
				change += " " + truncateValue(value)
			}
		}
		table.Append([]string{conflict.Reason, resourceName, conflict.Path, change})
	}
	table.Render()
}

// truncateValue keeps a conflict's value to one readable line.
func truncateValue(value string) string {
	value = strings.ReplaceAll(value, "\n", " ")
	const maxValueLength = 48
	if len(value) > maxValueLength {
		return value[:maxValueLength] + "..."
	}
	return value
}

func plural(word string, count int) string {
	if count == 1 {
		return word
	}
	return word + "s"
}
