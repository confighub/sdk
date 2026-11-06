// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/confighub/sdk/core/cubapi"
	"github.com/confighub/sdk/core/function/api"
	goclientnew "github.com/confighub/sdk/core/openapi/goclient-new"
	"github.com/spf13/cobra"
)

var unitConflictsCmd = &cobra.Command{
	Use:   "conflicts <unit-slug> [--apply | --dismiss [--permanent] | --undismiss] [--reason REASON] [--path PATH] [--resource NAME] [--resource-type TYPE] [--guard KEY[=VALUE]]...",
	Short: "Show, apply, or dismiss a unit's outstanding merge conflicts",
	Long: getCommandHelp(`Work with the parts of the last merge's patch that were not applied.

A merge reports a conflict for every change it could not make: a path the
downstream owns (Subtracted), a path the downstream protects (ProtectedPath),
a path that could not be located in this unit (UnresolvedPath), and each
downstream change displaced by an upstream deletion (DeleteShadowed). The
merged data is correct as it stands -- a conflict says what the source wanted
to change and couldn't.

Guarded is one of those, for a value the source wanted to write at a path
carrying a guard -- a recorded reason its value is what it is -- that the merge
was not cleared for. The conflict names the guard, so the report says which
reason stopped it rather than only that something did.

GuardWithheld is the other half, and is about a guard rather than a value: the
source changed the guards at a path and this merge was not cleared for the
guards this unit already has there, so the guard change did not propagate.
Reported because a base that adds a policy exception and a variant that
silently does not receive it is exactly the failure guards exist to prevent,
and the data merging cleanly would otherwise hide it. Applying one writes the
guard change rather than any configuration data.

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

With no flags this lists the outstanding conflicts, and the dismissals recorded
with --permanent. --apply replays the changes they withheld, with every path
eligible so nothing filters them out a second time. --dismiss drops them without
touching the configuration data. Both act on every outstanding conflict unless
--reason, --path, --resource, --resource-type, or --guard narrows the selection,
and both take --dry-run, which reports what the request would do and writes
nothing.

--resource names a resource by its whole name (prod/web, or /web for a
cluster-scoped resource) or a name it had before a rename. A name without a
namespace (web) is a filter instead, selecting the resource of that name in
every namespace; --permanent and --undismiss refuse one that matches more than
one resource. --resource-type tells apart resources that
share a name, as a Deployment and a Service named for their app do; it matches
the type in any API version. --path names an element of a list by its position
in this unit (spec.data.0.secretKey), by merge key
(spec.template.spec.containers.?name=main.image), or as the conflict shows it,
and * stands for any one list element or map key (spec.data.*.secretKey).
--guard, repeatable, names a guard as key or key=value, and selects a Guarded or
GuardWithheld conflict when it names every guard the conflict lists.

--dismiss --permanent also records the selection on the unit, so that a later
merge reporting a conflict it covers leaves that conflict dismissed rather than
outstanding: for a value this unit overrides on purpose, or a guard that is
meant to withhold what the upstream sends. Each recorded dismissal is about one
resource and one reason, so --permanent needs --resource and --reason, and
--guard for a Guarded or GuardWithheld conflict to dismiss only what those
guards withheld. Without --path it covers every path of the resource. The merge's
revision keeps the conflicts it reported, and a revision of its own records
their dismissal. --undismiss removes recorded dismissals of a --resource, for
--reason and --guard, or for every reason without them: without --path, the one
on the resource as a whole; with --path '*', every one; with a path, the one
recorded at that path, a pattern such as containers.*.image included.

Applying changes the configuration data, so the unit goes through the same
trigger pass as any other change and may pick up a ValidationError. --wait, on by
default, waits for that pass to finish before returning.

Examples:
`+"```"+`
  # What did the last merge fail to apply?
  cub unit conflicts my-unit

  # What would taking the upstream's value do to this unit?
  cub unit conflicts my-unit --apply --dry-run -o mutations

  # Apply the upstream changes that were withheld because this unit owns the path
  cub unit conflicts my-unit --apply --reason Subtracted

  # Accept the current state and stop reporting them
  cub unit conflicts my-unit --dismiss

  # Dismiss the conflict at one path of the Service, not of the Deployment of the same name
  cub unit conflicts my-unit --dismiss --resource prod/web --resource-type v1/Service \
    --path metadata.labels.tier

  # The same, naming the resource without its namespace and the element by position
  cub unit conflicts my-unit --dismiss --resource app --path spec.data.0.secretKey

  # Stop reporting the upstream's replica count: this unit sets its own
  cub unit conflicts my-unit --dismiss --permanent --resource web --reason Subtracted \
    --path spec.replicas

  # Stop reporting values withheld only by the platform team's guard, at any element
  cub unit conflicts my-unit --dismiss --permanent --resource web --reason Guarded \
    --guard owner=platform --path 'spec.template.spec.containers.*.image'

  # Report the replica count again
  cub unit conflicts my-unit --undismiss --resource web --path spec.replicas

  # Remove every dismissal recorded on the resource
  cub unit conflicts my-unit --undismiss --resource web --path '*'

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
	conflictsPermanent    bool
	conflictsUndismiss    bool
	conflictsReason       string
	conflictsPath         string
	conflictsResourceName string
	conflictsResourceType string
	conflictsGuards       []string
)

// conflictReasonsHelp lists the reasons a conflict can have, for flag help.
func conflictReasonsHelp() string {
	reasons := api.KnownConflictReasons()
	names := make([]string, len(reasons))
	for i, reason := range reasons {
		names[i] = string(reason)
	}
	return strings.Join(names[:len(names)-1], ", ") + ", or " + names[len(names)-1]
}

func init() {
	unitConflictsCmd.Flags().BoolVar(&conflictsApply, "apply", false,
		"apply the changes the selected conflicts withheld")
	unitConflictsCmd.Flags().BoolVar(&conflictsDismiss, "dismiss", false,
		"drop the selected conflicts without changing the configuration data")
	unitConflictsCmd.Flags().BoolVar(&conflictsPermanent, "permanent", false,
		"with --dismiss, also dismiss what the selection covers whenever a later merge reports it")
	unitConflictsCmd.Flags().BoolVar(&conflictsUndismiss, "undismiss", false,
		"remove dismissals recorded with --permanent, so the conflicts they covered are reported again")
	unitConflictsCmd.Flags().StringVar(&conflictsReason, "reason", "",
		"select conflicts with this reason: "+conflictReasonsHelp())
	unitConflictsCmd.Flags().StringVar(&conflictsPath, "path", "",
		"select conflicts at this path; a list element by position or merge key, * for any one element or key")
	unitConflictsCmd.Flags().StringVar(&conflictsResourceName, "resource", "",
		"select conflicts on this resource, by its whole name (namespace/name); a name without a namespace selects that name in every namespace")
	unitConflictsCmd.Flags().StringVar(&conflictsResourceType, "resource-type", "",
		"select conflicts on resources of this type, such as apps/v1/Deployment, in any API version")
	unitConflictsCmd.Flags().StringArrayVar(&conflictsGuards, "guard", nil,
		"select conflicts withheld only by these guards, as key or key=value (repeatable)")
	unitConflictsCmd.Flags().BoolVar(&dryRun, "dry-run", false,
		"dry run mode: report what --apply or --dismiss would do without writing anything")
	enableWaitFlag(unitConflictsCmd)
	addStandardDisplayFlags(unitConflictsCmd)
	enableOptionalSpace(unitConflictsCmd)
	unitCmd.AddCommand(unitConflictsCmd)
}

func unitConflictsCmdRun(_ *cobra.Command, args []string) error {
	actions := 0
	for _, set := range []bool{conflictsApply, conflictsDismiss, conflictsUndismiss} {
		if set {
			actions++
		}
	}
	if actions > 1 {
		return fmt.Errorf("--apply, --dismiss, and --undismiss are mutually exclusive")
	}
	if conflictsPermanent && !conflictsDismiss {
		return fmt.Errorf("--permanent records what --dismiss dismisses, so it goes with --dismiss")
	}
	selecting := conflictsReason != "" || conflictsPath != "" || conflictsResourceName != "" || conflictsResourceType != "" ||
		len(conflictsGuards) > 0
	if selecting && actions == 0 {
		return fmt.Errorf("--reason, --path, --resource, --resource-type, and --guard select which conflicts to --apply, --dismiss, or --undismiss")
	}
	if (conflictsPermanent || conflictsUndismiss) && conflictsResourceName == "" {
		return fmt.Errorf("a recorded dismissal is about one resource: name it with --resource")
	}
	if conflictsPermanent && conflictsReason == "" {
		return fmt.Errorf("a recorded dismissal is about one reason: name it with --reason")
	}

	if actions == 0 {
		return listUnitConflicts(args[0])
	}

	configUnit, err := resolveUnit(args[0], selectedSpaceID, "UnitID,SpaceID,HeadMutationNum,HeadRevisionNum")
	if err != nil {
		return err
	}
	body := goclientnew.UnitConflictsRequest{Action: "Dismiss", Permanent: conflictsPermanent}
	switch {
	case conflictsApply:
		body.Action = "Apply"
	case conflictsUndismiss:
		body.Action = "Undismiss"
	}
	body.DryRun = dryRun
	if selecting {
		selector := goclientnew.UnitConflictSelector{
			Reason:       conflictsReason,
			Path:         conflictsPath,
			ResourceType: conflictsResourceType,
		}
		if conflictsResourceName != "" {
			resource := specResource(conflictsResourceType, conflictsResourceName)
			selector.ResourceName, selector.ResourceNameWithoutScope = resource.ResourceName, resource.ResourceNameWithoutScope
		}
		if len(conflictsGuards) > 0 {
			selector.Guards = conflictsGuards
		}
		body.Select = []goclientnew.UnitConflictSelector{selector}
	}

	res, err := cubClientNew.ResolveUnitConflictsWithResponse(ctx, configUnit.Unit.SpaceID, configUnit.Unit.UnitID, body)
	if cubapi.IsAPIError(err, res) {
		return cubapi.InterpretErrorGeneric(err, res)
	}
	resp := res.JSON200
	if resp == nil {
		return nil
	}
	if !quiet {
		verb := "Applied"
		if dryRun {
			verb = "Would apply"
		}
		if resp.Applied > 0 {
			fmt.Printf("%s %d withheld merge %s on unit %s\n",
				verb, resp.Applied, plural("change", resp.Applied), args[0])
		}
		if resp.Dismissed > 0 {
			verb = "Dismissed"
			if dryRun {
				verb = "Would dismiss"
			}
			fmt.Printf("%s %d merge %s on unit %s\n",
				verb, resp.Dismissed, plural("conflict", resp.Dismissed), args[0])
		}
		if resp.Recorded > 0 {
			verb = "Recorded"
			if dryRun {
				verb = "Would record"
			}
			fmt.Printf("%s %d conflict %s on unit %s\n",
				verb, resp.Recorded, plural("dismissal", resp.Recorded), args[0])
		}
		if resp.Removed > 0 {
			verb = "Removed"
			if dryRun {
				verb = "Would remove"
			}
			fmt.Printf("%s %d conflict %s on unit %s\n",
				verb, resp.Removed, plural("dismissal", resp.Removed), args[0])
		}
		switch {
		case conflictsUndismiss && resp.Removed == 0:
			fmt.Printf("No matching recorded dismissals on unit %s\n", args[0])
		case !conflictsUndismiss && resp.Applied == 0 && resp.Dismissed == 0 && resp.Recorded == 0:
			fmt.Printf("No matching outstanding conflicts on unit %s\n", args[0])
		}
	}
	// What the request wrote, or would write.
	if conflictsApply && resp.Unit != nil && shouldDisplayMutations() {
		tprintRaw("")
		displayUnitChanges(args[0], "apply withheld merge changes", resp.Diff)
	}
	if !dryRun && wait && resp.Unit != nil && (resp.Applied > 0 || resp.Dismissed > 0 || resp.Recorded > 0 || resp.Removed > 0) {
		printAwaitingTriggers()
		if err := awaitTriggersRemoval(resp.Unit); err != nil {
			return err
		}
	}
	if resp.Conflicts != nil && len(*resp.Conflicts) > 0 {
		label := "Still outstanding:"
		if dryRun {
			label = "Would still be outstanding:"
		}
		fmt.Printf("\n%s\n", label)
		displayConflicts(*resp.Conflicts)
	}
	return nil
}

func listUnitConflicts(slug string) error {
	configUnit, err := resolveUnit(slug, selectedSpaceID, "UnitID,SpaceID,Conflicts,PathAnnotations")
	if err != nil {
		return err
	}
	var conflicts []goclientnew.MutationConflict
	if configUnit.Unit.Conflicts != nil {
		conflicts = *configUnit.Unit.Conflicts
	}
	if renderPayload(conflicts) {
		return nil
	}
	if len(conflicts) == 0 {
		if !quiet {
			fmt.Printf("No outstanding merge conflicts on unit %s\n", slug)
		}
	} else {
		displayConflicts(conflicts)
	}
	if configUnit.Unit.PathAnnotations != nil {
		if dismissals := conflictDismissalRows(*configUnit.Unit.PathAnnotations); len(dismissals) > 0 {
			fmt.Printf("\nDismissed whenever a merge reports them:\n")
			table := tableView()
			table.SetHeader([]string{"Resource", "Type", "Path", "Reason", "Guard"})
			for _, row := range dismissals {
				table.Append(row)
			}
			table.Render()
		}
	}
	return nil
}

// conflictDismissalRows lists the dismissals a unit has recorded, one row per reason, or per
// reason and guard, at each path, in resource and path order.
func conflictDismissalRows(annotations goclientnew.PathAnnotationList) [][]string {
	kind := string(api.AnnotationKindConflictDismissal)
	var rows [][]string
	appendRows := func(resource *goclientnew.ResourceInfo, path string, entries map[string]string) {
		resourceName, resourceType := "", ""
		if resource != nil {
			resourceName, resourceType = resource.ResourceName, resource.ResourceType
		}
		if path == "" {
			path = "(any)"
		}
		for _, key := range slices.Sorted(maps.Keys(entries)) {
			reason, guard := api.ParseConflictDismissalKey(key)
			if guard != "" && entries[key] != "" {
				guard += "=" + entries[key]
			}
			rows = append(rows, []string{resourceName, resourceType, path, string(reason), guard})
		}
	}
	for _, entry := range annotations {
		if entry.ResourceAnnotations != nil {
			appendRows(entry.Resource, "", (*entry.ResourceAnnotations)[kind])
		}
		if entry.PathAnnotationMap == nil {
			continue
		}
		for _, path := range slices.Sorted(maps.Keys(entry.PathAnnotationMap)) {
			appendRows(entry.Resource, path, entry.PathAnnotationMap[path][kind])
		}
	}
	return rows
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

// displayLinkFindings prints what the resolves of a unit's links could not do, one row each.
func displayLinkFindings(findings []goclientnew.LinkFinding) {
	table := tableView()
	table.SetHeader([]string{"Link", "Reason", "Entry", "Resource", "Path", "Details"})
	for _, finding := range findings {
		resourceName := ""
		if finding.Resource != nil {
			resourceName = finding.Resource.ResourceName
		}
		entry := finding.Entry
		if finding.Key != "" {
			entry += " " + finding.Key
		}
		table.Append([]string{finding.LinkID.String(), finding.Reason, entry, resourceName, finding.Path, finding.Details})
	}
	table.Render()
}

// truncateValue keeps a conflict's value to one readable line: newlines would break the row it
// sits in, whatever its length.
func truncateValue(value string) string {
	const maxValueLength = 48
	return truncateWithEllipsis(strings.ReplaceAll(value, "\n", " "), maxValueLength)
}

func plural(word string, count int) string {
	if count == 1 {
		return word
	}
	return word + "s"
}
