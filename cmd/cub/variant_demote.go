// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"bytes"
	"fmt"
	"sort"
	"strings"

	"github.com/cockroachdb/errors"
	"github.com/confighub/sdk/core/cubapi"
	goclientnew "github.com/confighub/sdk/core/openapi/goclient-new"
	"github.com/google/uuid"
	"github.com/spf13/cobra"
)

var variantDemoteArgs struct {
	changeDescription string
	changeorderSlug   string
	dryRun            bool
}

var variantDemoteCmd = &cobra.Command{
	Use:         "demote <space>",
	Short:       "Undo a change order in a space, restoring the revisions before it",
	Annotations: map[string]string{"OrgLevel": ""},
	Long: getCommandHelp(`Undo a change order in one space, restoring each unit it marked to the revision
that unit was at before the change.

Aborting a change order says the change is not coming to the spaces still waiting for it. It
changes nothing about the ones that already took it, which is what leaves a fleet part-way
through a change nobody is going to finish. Demote is what takes it back out, space by space.

The change order must have an AbortedReason. Setting it is the decision that the change is not
coming; undoing one nobody has said that about is a race with whoever is still promoting it.

Which units are restored is the change order's answer rather than a selection of your own: the
units of this space its start tag marks. A unit it covered but carried no changes for is marked
as restored without a revision being made -- there is nothing of it here to undo, and the
revisions after it, if any, are somebody else's change.

Undoing a change order in a unit happens once, as promoting it into one does: a unit already
carrying the restore tag is left alone, so a demote that landed partway is run again to reach the
units it did not, and a unit edited forward since it was undone keeps that work.

Each restore is a new revision holding the earlier content, not an unwinding of the ones between,
so the history of what happened stays intact. The change order's own tags stay where they are: the
space took the change, whatever has happened since, and the restore tag is what says it was taken
back out again. "cub changeorder get" reports where that has reached, as Restored Spaces, and the
change order's State reads Restored once every space that took it has been, and RestoreReleased
once every space that had released it has released the restored revisions.

The change order is named the way any entity in another space is: a bare slug resolves in the space
being demoted, which is where the change order resides only when that space is where the change was
made, and a <space>/<slug> or a UUID names one anywhere else -- which is what a variant's is, since
it lives upstream, possibly several hops up.

Demote does not promote the restored revisions onward, and does not restore the spaces downstream
of this one. Each is demoted on its own account, because what it goes back to has to be released
where the change was released. What demote does do is advance the merge pointers of the links that
follow this space's units onto the restored revisions, so that a later upgrade does not replay the
change that was just taken out.

Publishing is separate, as it is for promotion: "cub release publish" is what takes the restored
revisions to a cluster.

A unit whose head has moved past where the change order ended has changes the restore will drop,
and they are reported before anything is written. Nothing re-applies them: the restored revisions
are expected to be released first, and re-applying them is a forward change to make afterwards.

Examples:
`+"```"+`
  # Undo an aborted change order in the space it was made in
  cub variant demote web-base --change-order release-42

  # Report what would be restored, and what later changes it would drop
  cub variant demote web-base --change-order release-42 --dry-run

  # Undo it in a variant, naming the change order in the space it was made in
  cub variant demote web-prod --change-order web-base/release-42 --change-desc "back out 1.42"
`+"```"+`
`, ""),
	Args: cobra.ExactArgs(1),
	RunE: variantDemoteCmdRun,
}

func init() {
	variantDemoteCmd.Flags().StringVar(&variantDemoteArgs.changeorderSlug, "change-order", "", "change order to undo (required): a bare slug resolves in the space being demoted, which is where it resides only when that space is where the change was made; a variant's is <space>/<slug> or a UUID")
	variantDemoteCmd.Flags().StringVar(&variantDemoteArgs.changeDescription, "change-desc", "", "change description recorded on the restored revisions")
	variantDemoteCmd.Flags().BoolVar(&variantDemoteArgs.dryRun, "dry-run", false, "report the units that would be restored, and the later revisions the restore would drop, without changing anything")
	addStandardDisplayFlags(variantDemoteCmd)
	variantCmd.AddCommand(variantDemoteCmd)
}

func variantDemoteCmdRun(cmd *cobra.Command, args []string) error {
	if variantDemoteArgs.changeorderSlug == "" {
		return errors.New("demote needs --change-order: what it undoes is one named change, and which units that is comes from the change order")
	}
	space, err := apiGetSpaceFromSlug(args[0], "*")
	if err != nil {
		return err
	}

	// Demote names its space positionally rather than through --space, so the selected space is
	// whatever the context defaults to -- possibly nothing. Point it at the space being demoted:
	// the helpers that render mutations resolve units through it, and one of them parses it as a
	// UUID.
	selectedSpaceID = space.SpaceID.String()
	selectedSpaceSlug = space.Slug

	// A change order resides in the space the change was made in, which is this space only when
	// the source of the change is what is being undone. A variant's is upstream of it, and may be
	// several hops upstream, so it is named the way every other entity in another space is:
	// <space>/<slug>, or by UUID.
	changeOrder, err := resolveChangeOrder(variantDemoteArgs.changeorderSlug)
	if err != nil {
		return err
	}
	// Refused by the server too. Saying it here means the caller hears it before the units are
	// listed, and hears it once rather than once per unit.
	if changeOrder.AbortedReason == "" {
		return errors.Newf("change order '%s' has not been aborted; set its AbortedReason to say why the change is not coming, then demote:\n  cub changeorder update --space %s %s --aborted-reason \"<why>\"",
			changeOrder.Slug, changeOrder.SpaceID, changeOrder.Slug)
	}

	marked, alreadyUndone, err := demoteMarkedUnits(space.SpaceID, changeOrder)
	if err != nil {
		return err
	}
	if alreadyUndone > 0 && !jsonOutput && outputFormat == "" {
		tprint("Leaving %d unit(s) of %s alone: change order %s has already been taken back out of them",
			alreadyUndone, space.Slug, changeOrder.Slug)
	}
	if len(marked) == 0 {
		if !jsonOutput && outputFormat == "" {
			if alreadyUndone > 0 {
				tprint("Nothing left to restore in %s", space.Slug)
			} else {
				tprint("Change order %s marks no unit of %s, so there is nothing to restore there",
					changeOrder.Slug, space.Slug)
			}
		}
		return nil
	}
	demoteReportDroppedRevisions(marked)

	return demoteRestoreUnits(space.SpaceID, changeOrder, marked)
}

// demotedUnit is one unit of the space being demoted, and what the change order left on it.
type demotedUnit struct {
	unitID uuid.UUID
	slug   string
	// startRevisionNum is the revision the unit was at before the change, which the change order's
	// start tag marks and which restoring goes back to.
	startRevisionNum int64
	// endRevisionNum is the revision the change arrived at here, which the change order's end tag
	// marks. Revisions after it are later changes, and restoring drops them.
	endRevisionNum  int64
	headRevisionNum int64
}

// demoteMarkedUnits finds the units of a space this demote restores, and reports how many it leaves
// alone because they have already been restored.
//
// The marks are the answer rather than a where clause of ours. A change order covers the units the
// change is about, including the ones it carried no changes for, and which those are was decided
// when its scope was fixed; a selection made here would be a second description of that set, and
// the server refuses a unit the change order never marked rather than passing it over.
//
// A unit carrying the change order's restore tag has already had it taken back out. The server
// passes such a unit over, and leaving it out here is what keeps what demote reports -- the count,
// and which later revisions it says it would drop -- describing what it is going to do.
func demoteMarkedUnits(spaceID uuid.UUID, changeOrder *goclientnew.ChangeOrder) ([]*demotedUnit, int, error) {
	started, err := demoteRevisionsWithTag(spaceID, changeOrder.StartTagID)
	if err != nil {
		return nil, 0, err
	}
	if len(started) == 0 {
		return nil, 0, nil
	}
	ended, err := demoteRevisionsWithTag(spaceID, changeOrder.EndTagID)
	if err != nil {
		return nil, 0, err
	}
	// Empty until something has been restored, which is every change order that has only ever
	// moved forwards. An unset UUID reads back as the zero one rather than being absent.
	undone := map[uuid.UUID]int64{}
	if changeOrder.RestoreTagID != uuid.Nil {
		undone, err = demoteRevisionsWithTag(spaceID, changeOrder.RestoreTagID)
		if err != nil {
			return nil, 0, err
		}
	}

	units, err := apiListUnits(spaceID.String(), "", "UnitID,Slug,HeadRevisionNum")
	if err != nil {
		return nil, 0, err
	}
	marked := make([]*demotedUnit, 0, len(started))
	alreadyUndone := 0
	for _, unit := range units {
		if _, isMarked := started[unit.UnitID]; !isMarked {
			continue
		}
		if _, isUndone := undone[unit.UnitID]; isUndone {
			alreadyUndone++
			continue
		}
		marked = append(marked, &demotedUnit{
			unitID:           unit.UnitID,
			slug:             unit.Slug,
			startRevisionNum: started[unit.UnitID],
			endRevisionNum:   ended[unit.UnitID],
			headRevisionNum:  unit.HeadRevisionNum,
		})
	}
	sort.Slice(marked, func(i, j int) bool { return marked[i].slug < marked[j].slug })
	return marked, alreadyUndone, nil
}

// demoteRevisionsWithTag maps each unit of a space to the revision of it a tag marks. A tag marks
// at most one revision of a unit, which is what makes this a map rather than a list.
func demoteRevisionsWithTag(spaceID uuid.UUID, tagID uuid.UUID) (map[uuid.UUID]int64, error) {
	where := fmt.Sprintf("SpaceID = '%s' AND Tags ? '%s'", spaceID.String(), tagID.String())
	revisions, err := apiSearchListRevisions(where, "UnitID,RevisionNum,SpaceID,OrganizationID,RevisionID", "")
	if err != nil {
		return nil, err
	}
	marked := make(map[uuid.UUID]int64, len(revisions))
	for _, extended := range revisions {
		if extended.Revision == nil {
			continue
		}
		marked[extended.Revision.UnitID] = extended.Revision.RevisionNum
	}
	return marked, nil
}

// demoteReportDroppedRevisions says which later changes the restore takes out with the change
// order's.
//
// A unit whose head has moved past where the change order ended has revisions the restore does not
// keep: it goes back to the state before the change, and everything after it goes with it. Nothing
// re-applies them, because the restored revisions are expected to be released first -- re-applying
// what was dropped is a forward change to make after that, with whatever the release said in hand.
//
// A unit the change order carried nothing for is left where it is, so whatever came after its mark
// stays, and it is not reported here.
func demoteReportDroppedRevisions(marked []*demotedUnit) {
	var dropped []string
	for _, unit := range marked {
		if unit.endRevisionNum == 0 || unit.headRevisionNum <= unit.endRevisionNum {
			continue
		}
		// A unit the change order carried nothing for has its two tags on one revision and is
		// not restored at all, so the revisions after that one are not this undoing's to drop.
		if unit.startRevisionNum == unit.endRevisionNum {
			continue
		}
		if unit.headRevisionNum == unit.endRevisionNum+1 {
			dropped = append(dropped, fmt.Sprintf("  %s: revision %d", unit.slug, unit.headRevisionNum))
			continue
		}
		dropped = append(dropped, fmt.Sprintf("  %s: revisions %d-%d", unit.slug,
			unit.endRevisionNum+1, unit.headRevisionNum))
	}
	if len(dropped) == 0 {
		return
	}
	if !jsonOutput && outputFormat == "" {
		verb := "drops"
		if variantDemoteArgs.dryRun {
			verb = "would drop"
		}
		tprint("Warning: restoring %s changes made after the change order, which nothing re-applies:\n%s",
			verb, strings.Join(dropped, "\n"))
	}
}

// demoteRestoreUnits restores the marked units to the revision before the change order.
//
// One bulk request, naming the change order on it: that is what mints and places the restore tag,
// what advances the merge pointers of the links that follow each restored unit, and what makes a unit
// the change order never marked an error rather than a unit passed over.
func demoteRestoreUnits(spaceID uuid.UUID, changeOrder *goclientnew.ChangeOrder, marked []*demotedUnit) error {
	// Demote waits for triggers, except on a dry run: nothing is changed, so there is nothing to
	// wait for.
	wait = !variantDemoteArgs.dryRun

	if !jsonOutput && outputFormat == "" {
		verb := "Restoring"
		if variantDemoteArgs.dryRun {
			verb = "Would restore"
		}
		tprint("%s %d unit(s) to the revisions before change order %s...", verb, len(marked), changeOrder.Slug)
	}

	ids := make([]string, 0, len(marked))
	for _, unit := range marked {
		ids = append(ids, "'"+unit.unitID.String()+"'")
	}
	where := fmt.Sprintf("SpaceID = '%s' AND UnitID IN (%s)", spaceID.String(), strings.Join(ids, ", "))
	restore := "Before:ChangeOrder:" + changeOrder.ChangeOrderID.String()
	include := "UnitEventID,TargetID,UpstreamUnitID,SpaceID"
	params := &goclientnew.BulkPatchUnitsParams{Where: &where, Include: &include, Restore: &restore}
	params.ChangeOrder = &changeOrder.ChangeOrderID
	if variantDemoteArgs.dryRun {
		params.DryRun = &variantDemoteArgs.dryRun
	}

	// Naming what a revision is for is the whole of what a change description does, and "restored
	// the revision before the change order's start tag" says less than the change order's name.
	description := variantDemoteArgs.changeDescription
	if description == "" {
		description = fmt.Sprintf("Undo change order %s", changeOrder.Slug)
	}
	patchData, err := EnhancePatchData([]byte("null"), nil, nil, nil, nil, func(patchMap map[string]interface{}) {
		patchMap["LastChangeDescription"] = description
	})
	if err != nil {
		return err
	}

	// Snapshot prior unit state before the patch so the mutation display can tell what the
	// restore takes back out from what was already there.
	var priorUnits map[string]priorUnitInfo
	if shouldDisplayMutations() {
		priorUnits = savePriorUnitInfoInSpace(spaceID.String(), where, false)
		// A dry run stores nothing, so what it produced comes back on the response or not at
		// all. Appended to the expansions this request already asked for, because include is one
		// list: replacing it would trade them for the configuration.
		withWriteResult := include + "," + *includeWriteResult()
		params.Include = &withWriteResult
	}

	bulkRes, err := cubClientNew.BulkPatchUnitsWithBodyWithResponse(ctx, params, "application/merge-patch+json", bytes.NewReader(patchData))
	if cubapi.IsAPIError(err, bulkRes) {
		return cubapi.InterpretErrorGeneric(err, bulkRes)
	}
	responses, statusCode := bulkUnitResponses(bulkRes.JSON200, bulkRes.JSON207)
	if responses == nil {
		return fmt.Errorf("unexpected response from bulk patch API")
	}
	bulkErr := handleBulkCreateOrUpdateResponse(responses, statusCode, "restore", "")
	if shouldDisplayMutations() {
		displayMutationsForBulkUnitUpdate(responses, priorUnits, false, variantDemoteArgs.dryRun, "restore")
	}
	return bulkErr
}
