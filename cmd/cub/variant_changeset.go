// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"github.com/cockroachdb/errors"
	"github.com/confighub/sdk/core/cubapi"
	goclientnew "github.com/confighub/sdk/core/openapi/goclient-new"
	"github.com/google/uuid"
	"github.com/spf13/cobra"
)

var variantChangeSetCmd = &cobra.Command{
	Use:   "changeset",
	Short: "Open and close changesets on the units of a variant",
	Long: getCommandHelp(`Open a changeset on the units of a variant, and close it.

A unit is in a changeset from when the changeset is opened on it until the changeset is
closed, and every revision made in between is recorded in it, so the change can be reviewed,
approved and restored as one. A changeset can be opened on the units of several variants.

A changeset created with --change-workflow is governed by that ChangeWorkflow: closing it is
gated on the workflow's Final prerequisites, evaluated over every space the changeset was
opened in. A changeset is released by a change order once it has closed.`, ""),
}

var variantChangeSetOpenArgs struct {
	dryRun bool
}

var variantChangeSetOpenCmd = &cobra.Command{
	Use:         "open <variant> <changeset>",
	Short:       "Open a changeset on the units of a variant",
	Annotations: map[string]string{"OrgLevel": ""},
	Long: getCommandHelp(`Open a changeset on the units of a variant: every unit of the variant's space, or those
--where selects among them.

The changeset is opened on all of the units or on none. A unit is in one changeset at a time,
so when another changeset holds any of them the command fails, naming the units and the
changeset holding each, and opens nothing; wait for that changeset to close, or select other
units with --where. A unit left in a changeset that has closed stays held until it is taken
out with "cub unit update --patch --changeset -". A unit the changeset is already open on
stays in it.

The changeset is named by slug, space/slug, or UUID; a bare slug is looked up in the variant's
space. When the variant is a Variant of a Component, the changeset's ChangeWorkflow has to be
one the Component allows, and the changeset needs one if the Component requires it.

Examples:
`+"```"+`
  # Open a changeset on every unit of apptique-dev.
  cub changeset create --space apptique-dev hotfix --change-workflow platform/changeset-review
  cub variant changeset open apptique-dev hotfix

  # Open it on the workloads only.
  cub variant changeset open apptique-dev hotfix --where "Slug LIKE 'deployment-%'"
`+"```"+``, ""),
	Args: cobra.ExactArgs(2),
	RunE: variantChangeSetOpenCmdRun,
}

var variantChangeSetCloseArgs struct {
	forceReason string
	dryRun      bool
}

var variantChangeSetCloseCmd = &cobra.Command{
	Use:         "close <variant> <changeset>",
	Short:       "Close a changeset",
	Annotations: map[string]string{"OrgLevel": ""},
	Long: getCommandHelp(`Close a changeset, by taking every unit it is open on out of it, in every space it was
opened in. The changeset is named by slug, space/slug, or UUID; a bare slug is looked up in the
variant's space.

A changeset its ChangeWorkflow governs closes only when every prerequisite of the workflow's
Final holds in every space it was opened in, and the gates are printed either way. --dry-run
reports them and closes nothing. --force-reason closes it although some do not hold, which
needs Edit permission on the changeset and is recorded on it.

Examples:
`+"```"+`
  # See whether the changeset may close.
  cub variant changeset close apptique-dev hotfix --dry-run

  # Close it.
  cub variant changeset close apptique-dev hotfix

  # Close it although its approvals are not all in.
  cub variant changeset close apptique-dev hotfix --force-reason "incident 4321: approved by phone"
`+"```"+``, ""),
	Args: cobra.ExactArgs(2),
	RunE: variantChangeSetCloseCmdRun,
}

func init() {
	enableWhereFlag(variantChangeSetOpenCmd)
	variantChangeSetOpenCmd.Flags().BoolVar(&variantChangeSetOpenArgs.dryRun, "dry-run", false,
		"report which units the changeset would be opened on, and change nothing")
	variantChangeSetCloseCmd.Flags().StringVar(&variantChangeSetCloseArgs.forceReason, "force-reason", "",
		"close the changeset although its ChangeWorkflow's gates do not hold, recording this reason on it")
	variantChangeSetCloseCmd.Flags().BoolVar(&variantChangeSetCloseArgs.dryRun, "dry-run", false,
		"evaluate the gates closing the changeset is judged against, and close nothing")
	variantChangeSetCmd.AddCommand(variantChangeSetOpenCmd)
	variantChangeSetCmd.AddCommand(variantChangeSetCloseCmd)
	variantCmd.AddCommand(variantChangeSetCmd)
}

// variantChangeSet resolves the variant and the changeset, looking a bare changeset slug up in
// the variant's space.
func variantChangeSet(variantRef, changeSetRef string) (*goclientnew.ExtendedSpace, uuid.UUID, error) {
	space, err := resolveVariantOrSpace(variantRef, "SpaceID,Slug")
	if err != nil {
		return nil, uuid.Nil, err
	}
	changeSetID, err := resolveChangeSetIDIn(space.Space.SpaceID.String(), changeSetRef)
	if err != nil {
		return nil, uuid.Nil, errors.Wrap(err, "failed to resolve the changeset")
	}
	return space, changeSetID, nil
}

func variantChangeSetOpenCmdRun(cmd *cobra.Command, args []string) error {
	space, changeSetID, err := variantChangeSet(args[0], args[1])
	if err != nil {
		return err
	}
	// The bulk patch is not one transaction, so what would make it partial is found first: a
	// unit another changeset holds is refused by the server one unit at a time, after the units
	// before it have been taken.
	units, err := apiListUnits(space.Space.SpaceID.String(), where, "UnitID,Slug,ChangeSetID")
	if err != nil {
		return err
	}
	already, held := unitsByChangeSet(units, changeSetID)
	if len(held) > 0 {
		return errors.Newf("changeset %s was not opened on any unit of %s, because another changeset holds %d of them: %s",
			args[1], space.Space.Slug, heldUnitCount(held), describeHeldUnits(held, changeSetNames(held, space.Space.SpaceID)))
	}
	// Every selected unit is patched, those already in the changeset included, which the
	// server takes as no change. A unit another changeset took since the listing is refused
	// by the server by name, where leaving it out of the selection would hide it.
	selection := fmt.Sprintf("SpaceID = '%s'", space.Space.SpaceID)
	if where != "" {
		selection = fmt.Sprintf("%s AND %s", selection, where)
	}
	responses, err := patchUnitsChangeSet(selection, map[string]any{"ChangeSetID": changeSetID.String()},
		&goclientnew.BulkPatchUnitsParams{ChangeSetId: &changeSetID}, variantChangeSetOpenArgs.dryRun)
	if err != nil {
		return err
	}
	patched, failed := 0, 0
	for _, response := range responses {
		if response.Error != nil {
			failed++
			displayBulkFailure("open the changeset on", "unit", response.Error)
			continue
		}
		patched++
	}
	verb := "Opened"
	if variantChangeSetOpenArgs.dryRun {
		verb = "Would open"
	}
	tprint("%s changeset %s on %d unit(s) of %s", verb, args[1], max(patched-already, 0), space.Space.Slug)
	if failed > 0 {
		return errors.Errorf("failed to open the changeset on %d unit(s)", failed)
	}
	return nil
}

// unitsByChangeSet counts the units already in changeSetID, and groups the slugs of those
// another changeset holds by that changeset.
func unitsByChangeSet(units []*goclientnew.Unit, changeSetID uuid.UUID) (int, map[uuid.UUID][]string) {
	already := 0
	held := map[uuid.UUID][]string{}
	for _, unit := range units {
		switch {
		case unit.ChangeSetID == nil || *unit.ChangeSetID == uuid.Nil:
		case *unit.ChangeSetID == changeSetID:
			already++
		default:
			held[*unit.ChangeSetID] = append(held[*unit.ChangeSetID], unit.Slug)
		}
	}
	return already, held
}

func heldUnitCount(held map[uuid.UUID][]string) int {
	count := 0
	for _, slugs := range held {
		count += len(slugs)
	}
	return count
}

// changeSetNames names each changeset holding units for a message, as its slug and state, with
// its space when that is not the variant's. One that cannot be read keeps its ID, which is what
// the caller is left to look up.
func changeSetNames(held map[uuid.UUID][]string, spaceID uuid.UUID) map[uuid.UUID]string {
	names := make(map[uuid.UUID]string, len(held))
	ids := make([]goclientnew.UUID, 0, len(held))
	for id := range held {
		names[id] = id.String()
		ids = append(ids, id)
	}
	changeSets, err := cubapi.ListChangeSets(ctx, cubClient, cubapi.Where{}.In("ChangeSetID", ids),
		cubapi.ListOpts{Include: "SpaceID"})
	if err != nil {
		return names
	}
	for _, extended := range changeSets {
		if extended.ChangeSet == nil {
			continue
		}
		name := extended.ChangeSet.Slug
		if extended.ChangeSet.SpaceID != spaceID && extended.Space != nil {
			name = prefixedSlug(extended.Space.Slug, name)
		}
		if extended.ChangeSet.State != "" {
			name = fmt.Sprintf("%s (%s)", name, extended.ChangeSet.State)
		}
		names[extended.ChangeSet.ChangeSetID] = name
	}
	return names
}

// describeHeldUnits lists the held units by the changeset holding them, in a stable order:
// "api, config are in capacity-1 (Open); web is in hotfix (Open)".
func describeHeldUnits(held map[uuid.UUID][]string, names map[uuid.UUID]string) string {
	parts := make([]string, 0, len(held))
	for id, slugs := range held {
		slices.Sort(slugs)
		verb := "is"
		if len(slugs) > 1 {
			verb = "are"
		}
		parts = append(parts, fmt.Sprintf("%s %s in %s", strings.Join(slugs, ", "), verb, names[id]))
	}
	slices.Sort(parts)
	return strings.Join(parts, "; ")
}

func variantChangeSetCloseCmdRun(cmd *cobra.Command, args []string) error {
	_, changeSetID, err := variantChangeSet(args[0], args[1])
	if err != nil {
		return err
	}
	params := &goclientnew.BulkPatchUnitsParams{}
	if variantChangeSetCloseArgs.forceReason != "" {
		force := true
		params.ForceClose = &force
		params.ForceReason = &variantChangeSetCloseArgs.forceReason
	}
	// Every Unit of the changeset, in every space: detaching the first closes the whole of it,
	// and a unit left in a closed changeset cannot be written until it is taken out.
	responses, err := patchUnitsChangeSet(fmt.Sprintf("ChangeSetID = '%s'", changeSetID),
		map[string]any{"ChangeSetID": nil}, params, variantChangeSetCloseArgs.dryRun)
	if err != nil {
		return err
	}
	var gates *goclientnew.ChangeSetCloseResult
	detached, failed := 0, 0
	var firstFailure *goclientnew.ResponseError
	for _, response := range responses {
		if gates == nil && response.ChangeSetClose != nil {
			gates = response.ChangeSetClose
		}
		if response.Error != nil {
			failed++
			if firstFailure == nil {
				firstFailure = response.Error
			}
			continue
		}
		detached++
	}
	if gates != nil {
		displayChangeSetCloseGates(gates)
	}
	if failed > 0 {
		// Every Unit of a ChangeSet whose gates refused it fails the same way, so one says it.
		displayBulkFailure("close the changeset on", "unit", firstFailure)
		return errors.Errorf("changeset %s was not closed: %d unit(s) could not be taken out of it", args[1], failed)
	}
	verb := "Closed"
	if variantChangeSetCloseArgs.dryRun {
		verb = "Would close"
	}
	forced := ""
	if gates != nil && gates.Forced {
		forced = ", forced past its gates"
	}
	tprint("%s changeset %s%s, taking %d unit(s) out of it", verb, args[1], forced, detached)
	return nil
}

// patchUnitsChangeSet bulk-patches the units the where clause selects with patch, returning each
// unit's response, failures included.
func patchUnitsChangeSet(selection string, patch map[string]any, params *goclientnew.BulkPatchUnitsParams,
	dryRunRequested bool) ([]goclientnew.UnitCreateOrUpdateResponse, error) {
	body, err := json.Marshal(patch)
	if err != nil {
		return nil, err
	}
	params.Where = &selection
	if dryRunRequested {
		params.DryRun = &dryRunRequested
	}
	res, err := cubClientNew.BulkPatchUnitsWithBodyWithResponse(ctx, params, "application/merge-patch+json", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	switch {
	case res.JSON200 != nil:
		return *res.JSON200, nil
	case res.JSON207 != nil:
		return *res.JSON207, nil
	}
	return nil, cubapi.InterpretErrorGeneric(err, res)
}

// displayChangeSetCloseGates prints each gate closing a changeset was judged against, one to a
// line per space.
func displayChangeSetCloseGates(result *goclientnew.ChangeSetCloseResult) {
	if len(result.Gates) == 0 {
		return
	}
	table := tableView()
	if !noheader {
		table.SetHeader([]string{"Space", "Prerequisite", "Holds", "Reason"})
	}
	for _, gate := range result.Gates {
		holds := "yes"
		if !gate.Satisfied {
			holds = "no"
		}
		table.Append([]string{gate.SpaceSlug, gate.Prerequisite, holds, gate.Message})
	}
	table.Render()
}
