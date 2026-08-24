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
	"github.com/confighub/sdk/core/livestatus"
	goclientnew "github.com/confighub/sdk/core/openapi/goclient-new"
	"github.com/google/uuid"
	"github.com/spf13/cobra"
)

var variantPromoteArgs struct {
	changeDescription string
	changesetSlug     string
	changeorderSlug   string
	dryRun            bool
	squash            bool
}

var variantPromoteCmd = &cobra.Command{
	Use:         "promote <space>",
	Short:       "Promote a variant space to match its upstream space",
	Annotations: map[string]string{"OrgLevel": ""},
	Long: getCommandHelp(`Promote a variant space to match changes in its upstream space.

The space must have been created by "cub variant create", which stamps an "UpstreamSpaceID"
annotation recording the upstream space it was cloned from. Promote reconciles the variant
with that upstream in three steps:

  1. Upgrade every unit whose upstream unit has advanced (the unit's UpstreamRevisionNum is
     behind the upstream unit's HeadRevisionNum), merging the upstream changes. Equivalent to
     "cub unit update --patch --upgrade --where 'UpstreamRevisionNum < UpstreamUnit.HeadRevisionNum'".
  2. Clone any units added to the upstream space since the variant was created or last
     promoted, linking each clone to its upstream unit.
  3. Copy the new units' non-UpgradeUnit links, retargeting a link to its downstream copy
     when it points at another unit in the upstream space. A link that already points into
     this variant is left alone rather than copied into it.

Promote waits for triggers to complete. Use --dry-run to preview: the units that would be
upgraded (add -o mutations to see the changes) and the units that would be added.

An upgrade re-runs the upstream's recorded function invocations against each unit where it
can -- so a change lands where the unit's own structure puts it -- and records one revision per
upstream revision that has an effect, carrying that revision's change description. The variant's
history then reads as the upstream's does rather than as a series of promotions. --squash gives
up both: the range arrives as one rebased diff in one revision.

--change-order promotes a named change rather than everything the upstream has reached. The
change order fixed its range when it was created, so the upgrade stops where it ends, a unit
it does not cover is passed over, and a unit that is not where it starts is an error rather
than a merge of a different range. Two things follow from what a change order is:

  - The steps run in the other order, clone first. A unit the variant does not have yet is
    cloned at the revision the change order starts from, and then upgraded through the change
    with everything else, so it ends up with the revisions the change made everywhere else
    rather than arriving whole with the change already in it. A unit created upstream after
    the change order was fixed is outside it: those are listed rather than cloned, and
    promoting without --change-order adds them.
  - The promotion is undone in one step, however many revisions it made:
    "cub unit update --patch --space <variant> --where \"Slug LIKE '%'\"
     --restore Before:ChangeOrder:<space>/<change-order>".

Examples:
`+"```"+`
  # Promote a variant to match its upstream
  cub variant promote web-prod

  # Preview the changes, including the mutations
  cub variant promote web-prod --dry-run -o mutations

  # Promote, recording the whole range as one revision per unit
  cub variant promote web-prod --squash

  # Promote one named change, leaving later upstream changes behind
  cub variant promote web-prod --change-order release-42

  # Promote within a changeset, with a change description
  cub variant promote web-prod --changeset release-2024-06 --change-desc "Promote to prod"
`+"```"+`
`, ""),
	Args: cobra.ExactArgs(1),
	RunE: variantPromoteCmdRun,
}

func init() {
	variantPromoteCmd.Flags().StringVar(&variantPromoteArgs.changeDescription, "change-desc", "", "change description recorded on the upgraded and cloned units")
	variantPromoteCmd.Flags().StringVar(&variantPromoteArgs.changesetSlug, "changeset", "", "changeset to associate the upgraded and cloned units with")
	variantPromoteCmd.Flags().StringVar(&variantPromoteArgs.changeorderSlug, "change-order", "", "change order to promote instead of everything the upstream has reached: it supplies the range, units it does not cover are passed over, and a unit that is not where it starts is an error. A bare slug resolves in the upstream space. Units the variant does not have yet are cloned at the change order's start and then upgraded through it; ones created upstream after the change order was fixed are outside it and are listed rather than cloned")
	variantPromoteCmd.Flags().BoolVar(&variantPromoteArgs.dryRun, "dry-run", false, "preview the units that would be upgraded and added without changing anything")
	variantPromoteCmd.Flags().BoolVar(&variantPromoteArgs.squash, "squash", false, "merge each unit's range as one rebased diff in one revision instead of walking it: by default a promotion re-runs the upstream's recorded function invocations against each unit where it can, and records one revision per upstream revision that has an effect there")
	addStandardDisplayFlags(variantPromoteCmd)
	variantCmd.AddCommand(variantPromoteCmd)
}

func variantPromoteCmdRun(cmd *cobra.Command, args []string) error {
	downstreamSpace, err := apiGetSpaceFromSlug(args[0], "*")
	if err != nil {
		return err
	}
	upstreamSpaceID, err := promoteUpstreamSpaceID(downstreamSpace)
	if err != nil {
		return err
	}
	downstreamSpaceID := downstreamSpace.SpaceID

	// Promote names its space positionally rather than through --space, so the selected
	// space is whatever the context defaults to -- possibly nothing. Point it at the space
	// being promoted: the helpers that render mutations resolve units through it, and one
	// of them parses it as a UUID.
	selectedSpaceID = downstreamSpaceID.String()
	selectedSpaceSlug = downstreamSpace.Slug

	changeOrder, err := promoteChangeOrder(upstreamSpaceID)
	if err != nil {
		return err
	}

	// Refuse to promote until the previous stage is already running the change. The gate runs on a
	// dry run too: a preview that ignored it would describe a promotion that cannot happen.
	if err := validatePreviousStageForPromotion(downstreamSpace, changeOrder); err != nil {
		return err
	}

	// Promote always waits for triggers, except on a dry run (nothing is changed,
	// so there is nothing to wait for).
	wait = !variantPromoteArgs.dryRun

	if changeOrder != nil {
		// Clone before upgrading, which is the other way round from a plain promote. A unit the
		// variant does not have yet is taken at the change order's start -- the state the change
		// begins from -- and then upgraded through the change with everything else, so it ends up
		// with the same revisions the change made everywhere else rather than arriving whole.
		if err := promoteAddNewUnitsForChangeOrder(downstreamSpaceID, upstreamSpaceID, changeOrder); err != nil {
			return err
		}
		return promoteUpgradeUnits(downstreamSpaceID, &changeOrder.ChangeOrderID)
	}
	if err := promoteUpgradeUnits(downstreamSpaceID, nil); err != nil {
		return err
	}
	return promoteAddNewUnits(downstreamSpaceID, upstreamSpaceID)
}

func validatePreviousStageForPromotion(space *goclientnew.Space, changeOrder *goclientnew.ChangeOrder) error {
	previousStage, ok := space.Labels["PreviousStage"]
	if !ok {
		return nil
	}

	component, ok := space.Labels["Component"]
	if !ok {
		return errors.New("Component not found in Space")
	}

	stage := space.Labels["Stage"]

	previousStageVariants, err := apiListSpaces("Labels.Stage = '"+previousStage+
		"' AND Labels.Component = '"+component+"'", "*")
	if err != nil {
		return err
	}
	if len(previousStageVariants) == 0 {
		return errors.Newf("unable to promote to stage '%s', no Space of Component '%s' has Stage '%s'", stage, component, previousStage)
	}

	for _, variant := range previousStageVariants {
		variantName := variant.Labels["Variant"]
		if variantName == "" {
			variantName = variant.Slug
		}

		// A Space with no release target releases nothing, ever, so nothing is deployed from it
		// and there is no live state to report: taking the change is the whole of what it can be
		// asked for.
		if variant.ReleaseTargetID != nil {
			liveStatusJSON, ok := variant.Annotations[livestatus.Annotation]
			if !ok {
				return errors.Newf("unable to promote to stage '%s', live-status not found for Variant '%s'", stage, variantName)
			}

			var liveStatus livestatus.Status
			if err := json.Unmarshal([]byte(liveStatusJSON), &liveStatus); err != nil {
				return err
			}

			if liveStatus.SyncStatus != "Synced" {
				return errors.Newf("unable to promote to stage '%s', Variant '%s' is not synced", stage, variantName)
			}
			if liveStatus.OperationPhase != "Succeeded" {
				return errors.Newf("unable to promote to stage '%s', Variant '%s' has not succeeded in deployment", stage, variantName)
			}
			if liveStatus.HealthStatus != "Healthy" {
				return errors.Newf("unable to promote to stage '%s', Variant '%s' is not healthy", stage, variantName)
			}
		}

		// A ChangeOrder is the change's own identity, and the server keeps track of where it has
		// got to, so this is a question rather than a reconstruction: the Release the Variant is
		// running does not have to be found and taken apart to answer it.
		if changeOrder != nil {
			if err := validateReleasedChangeOrder(variant, changeOrder, stage, variantName); err != nil {
				return err
			}
			continue
		}
	}

	return nil
}

// validateReleasedChangeOrder errors when a Variant of the previous stage has not taken the change
// order being promoted, or has taken it without releasing it.
//
// ResolvedSpaceIDs and ReleasedSpaceIDs are the server's own answer to both questions, derived from
// the Links the change order propagates over and the Revision each Unit is applied at. A Unit the
// change order covers but did not change is not counted in either, and neither is one outside the
// Space's release: what is being asked is whether this change has finished arriving there, not
// whether the Space is otherwise up to date.
func validateReleasedChangeOrder(variant *goclientnew.Space, changeOrder *goclientnew.ChangeOrder,
	stage, variantName string) error {
	if !slices.Contains(changeOrder.ResolvedSpaceIDs, variant.SpaceID) {
		return errors.Newf("unable to promote to stage '%s', Variant '%s' has not taken change order '%s'",
			stage, variantName, changeOrder.Slug)
	}
	// A Space with no release target releases nothing, ever -- a base, or any Space whose Units
	// are delivered from somewhere else -- so having taken the change is the whole of what it
	// can be asked for.
	if variant.ReleaseTargetID == nil {
		return nil
	}
	if !slices.Contains(changeOrder.ReleasedSpaceIDs, variant.SpaceID) {
		return errors.Newf("unable to promote to stage '%s', Variant '%s' has taken change order '%s' but has not released it",
			stage, variantName, changeOrder.Slug)
	}
	return nil
}

// promoteChangeOrder resolves --change-order, or nil when it was not given.
//
// A ChangeOrder resides in the Space holding the changes to promote, so both the slug and the
// entity behind it resolve there rather than in the variant being promoted, which is what the
// selected space names by the time this runs.
func promoteChangeOrder(upstreamSpaceID uuid.UUID) (*goclientnew.ChangeOrder, error) {
	if variantPromoteArgs.changeorderSlug == "" {
		return nil, nil
	}
	selectedForChangeOrder := selectedSpaceID
	selectedSpaceID = upstreamSpaceID.String()
	defer func() { selectedSpaceID = selectedForChangeOrder }()
	changeOrder, err := parseEntityIdentifierSingleAsEntity[goclientnew.ChangeOrder](
		variantPromoteArgs.changeorderSlug,
		EntityTypeChangeOrder,
		"*",
		apiGetChangeOrderFromSlugInSpace,
		func(c *goclientnew.ChangeOrder) string { return c.ChangeOrderID.String() },
	)
	if err != nil {
		return nil, fmt.Errorf("failed to get change order: %w", err)
	}
	return changeOrder, nil
}

// promoteUpstreamSpaceID reads the UpstreamSpaceID annotation stamped by
// "cub variant create".
func promoteUpstreamSpaceID(space *goclientnew.Space) (uuid.UUID, error) {
	idStr := ""
	if space.Annotations != nil {
		idStr = space.Annotations[AnnotationUpstreamSpaceID]
	}
	if idStr == "" {
		return uuid.Nil, fmt.Errorf("space %s has no %s annotation; only spaces created by 'cub variant create' can be promoted",
			space.Slug, AnnotationUpstreamSpaceID)
	}
	id, err := uuid.Parse(idStr)
	if err != nil {
		return uuid.Nil, fmt.Errorf("invalid %s annotation %q on space %s: %w", AnnotationUpstreamSpaceID, idStr, space.Slug, err)
	}
	return id, nil
}

// promotePatchEnhancer builds the patch enhancer applied to upgraded and cloned
// units, setting the change description and/or changeset when requested. The
// returned ChangeSetID (or nil) is also passed as a bulk-operation parameter.
func promotePatchEnhancer() (PatchEnhancer, *uuid.UUID, error) {
	var changesetID *uuid.UUID
	if variantPromoteArgs.changesetSlug != "" {
		id, err := parseChangeSetSlug(variantPromoteArgs.changesetSlug)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to get changeset: %w", err)
		}
		changesetID = &id
	}
	enhancer := func(patchMap map[string]interface{}) {
		if variantPromoteArgs.changeDescription != "" {
			patchMap["LastChangeDescription"] = variantPromoteArgs.changeDescription
		}
		if changesetID != nil {
			patchMap["ChangeSetID"] = *changesetID
		}
	}
	return enhancer, changesetID, nil
}

// promoteUpgradeUnits upgrades every downstream unit whose upstream has advanced. With a change
// order it upgrades those of them the change order covers, to where it ends rather than to the
// upstream's head.
func promoteUpgradeUnits(downstreamSpaceID uuid.UUID, changeOrderID *uuid.UUID) error {
	if !jsonOutput && outputFormat == "" {
		if changeOrderID != nil {
			tprint("Promoting change order %s into units behind their upstream...", variantPromoteArgs.changeorderSlug)
		} else {
			tprint("Upgrading units behind their upstream...")
		}
	}
	// The selection stays "behind its upstream" even with a change order: which of those units
	// the change is in is the change order's answer, given per unit by the server, and a unit
	// already level with its upstream cannot be behind the end of anything.
	where := fmt.Sprintf("SpaceID = '%s' AND UpstreamRevisionNum < UpstreamUnit.HeadRevisionNum", downstreamSpaceID.String())
	include := "UnitEventID,TargetID,UpstreamUnitID,SpaceID"
	upgrade := true
	params := &goclientnew.BulkPatchUnitsParams{Where: &where, Include: &include, Upgrade: &upgrade}
	params.ChangeOrder = changeOrderID
	if variantPromoteArgs.dryRun {
		params.DryRun = &variantPromoteArgs.dryRun
	}
	if variantPromoteArgs.squash {
		params.Squash = &variantPromoteArgs.squash
	}
	enhancer, changesetID, err := promotePatchEnhancer()
	if err != nil {
		return err
	}
	params.ChangeSetId = changesetID
	patchData, err := EnhancePatchData([]byte("null"), nil, nil, nil, nil, enhancer)
	if err != nil {
		return err
	}
	// Snapshot prior unit state before the patch so the mutation display can tell the
	// changes this promotion brings from the ones already there.
	var priorUnits map[string]priorUnitInfo
	if shouldDisplayMutations() {
		priorUnits = savePriorUnitInfoInSpace(downstreamSpaceID.String(), where, false)
		// A dry run stores nothing, so what it produced comes back on the response or not
		// at all.
		params.Include = includeWriteResult()
	}
	bulkRes, err := cubClientNew.BulkPatchUnitsWithBodyWithResponse(ctx, params, "application/merge-patch+json", bytes.NewReader(patchData))
	if cubapi.IsAPIError(err, bulkRes) {
		return cubapi.InterpretErrorGeneric(err, bulkRes)
	}
	responses, statusCode := bulkUnitResponses(bulkRes.JSON200, bulkRes.JSON207)
	if responses == nil {
		return fmt.Errorf("unexpected response from bulk patch API")
	}
	bulkErr := handleBulkCreateOrUpdateResponse(responses, statusCode, "upgrade", "")
	if shouldDisplayMutations() {
		// A unit already level with its upstream is not selected at all, so there are no
		// responses to render. Say so rather than printing nothing: -o mutations
		// suppresses the ordinary summary, and silence is indistinguishable from the
		// renderer not being wired up.
		if len(*responses) == 0 {
			tprintRaw("No units behind their upstream")
		}
		displayMutationsForBulkUnitUpdate(responses, priorUnits, false, variantPromoteArgs.dryRun, "upgrade")
	}
	return bulkErr
}

// promoteUntrackedUpstreamUnits finds the units of the upstream space that no downstream unit
// tracks -- the ones added since the variant was created or last promoted -- and the where clause
// that selects them.
func promoteUntrackedUpstreamUnits(downstreamSpaceID, upstreamSpaceID uuid.UUID) ([]*goclientnew.Unit, string, error) {
	// Upstream unit IDs already tracked by the downstream space.
	downstreamUnits, err := apiListUnits(downstreamSpaceID.String(), "", "UnitID,UpstreamUnitID")
	if err != nil {
		return nil, "", err
	}
	tracked := make([]string, 0, len(downstreamUnits))
	for _, u := range downstreamUnits {
		if u.UpstreamUnitID != nil {
			tracked = append(tracked, "'"+u.UpstreamUnitID.String()+"'")
		}
	}

	// Upstream units not tracked downstream are the ones to add.
	newWhere := ""
	if len(tracked) > 0 {
		newWhere = fmt.Sprintf("UnitID NOT IN (%s)", strings.Join(tracked, ", "))
	}
	newUnits, err := apiListUnits(upstreamSpaceID.String(), newWhere, "UnitID,Slug")
	if err != nil {
		return nil, "", err
	}
	return newUnits, newWhere, nil
}

// promoteAddNewUnitsForChangeOrder clones the units the variant does not have yet, at the revision
// the change order starts from rather than at the upstream's head.
//
// Which of them the change order covers is decided by its start Tag: a unit created upstream after
// the change order was fixed carries none of its Tags and is not part of the change, so it is named
// rather than cloned. The clone itself names its revision the way every other operation does --
// `Before:ChangeOrder:<id>`, resolved by the server against each source unit -- so the upgrade that
// follows replays the change into the clone.
func promoteAddNewUnitsForChangeOrder(downstreamSpaceID, upstreamSpaceID uuid.UUID, changeOrder *goclientnew.ChangeOrder) error {
	newUnits, newWhere, err := promoteUntrackedUpstreamUnits(downstreamSpaceID, upstreamSpaceID)
	if err != nil {
		return err
	}
	if len(newUnits) == 0 {
		return nil
	}

	startTagWhere := fmt.Sprintf("SpaceID = '%s' AND Tags ? '%s'", upstreamSpaceID.String(), changeOrder.StartTagID.String())
	startRevisions, err := apiSearchListRevisions(startTagWhere, "UnitID,RevisionNum,Tags", "")
	if err != nil {
		return fmt.Errorf("failed to find the revisions the change order starts at: %w", err)
	}
	inChangeOrder := map[uuid.UUID]bool{}
	for _, r := range startRevisions {
		if r.Revision != nil {
			inChangeOrder[r.Revision.UnitID] = true
		}
	}

	toClone := make([]*goclientnew.Unit, 0, len(newUnits))
	outside := make([]string, 0, len(newUnits))
	for _, u := range newUnits {
		if inChangeOrder[u.UnitID] {
			toClone = append(toClone, u)
		} else {
			outside = append(outside, u.Slug)
		}
	}

	if !jsonOutput && outputFormat == "" {
		verb := "Adding"
		if variantPromoteArgs.dryRun {
			verb = "Would add"
		}
		tprint("%s %d unit(s) from upstream at the change order's start", verb, len(toClone))
		if len(outside) > 0 {
			tprint("Leaving %d upstream unit(s) outside the change order uncloned; promote without --change-order to add them:", len(outside))
			for _, slug := range outside {
				tprint("  - %s", slug)
			}
		}
	}
	if len(toClone) == 0 {
		return nil
	}
	if variantPromoteArgs.dryRun {
		for _, u := range toClone {
			tprint("  + %s", u.Slug)
		}
		return nil
	}

	cloneWhere := fmt.Sprintf("SpaceID = '%s'", upstreamSpaceID.String())
	if newWhere != "" {
		cloneWhere += " AND " + newWhere
	}
	cloneWhere += fmt.Sprintf(" AND %s", unitIDInList(toClone))
	whereSpace := fmt.Sprintf("SpaceID = '%s'", downstreamSpaceID.String())
	include := "UnitEventID,TargetID,UpstreamUnitID,SpaceID"
	upstreamRevision := fmt.Sprintf("Before:ChangeOrder:%s", changeOrder.ChangeOrderID.String())
	cloneParams := &goclientnew.BulkCreateUnitsParams{
		Where:            &cloneWhere,
		WhereSpace:       &whereSpace,
		Include:          &include,
		UpstreamRevision: &upstreamRevision,
	}
	enhancer, _, err := promotePatchEnhancer()
	if err != nil {
		return err
	}
	patchData, err := EnhancePatchData([]byte("null"), nil, nil, nil, nil, enhancer)
	if err != nil {
		return err
	}
	responses, statusCode, err := bulkCreateUnits(cloneParams, patchData)
	if err != nil {
		return err
	}
	if err := handleBulkCreateOrUpdateResponse(responses, statusCode, "create", ""); err != nil {
		return err
	}

	return promoteCopyLinks(downstreamSpaceID, upstreamSpaceID, toClone)
}

// unitIDInList renders a UnitID IN (...) clause for a set of units.
func unitIDInList(units []*goclientnew.Unit) string {
	ids := make([]string, 0, len(units))
	for _, u := range units {
		ids = append(ids, "'"+u.UnitID.String()+"'")
	}
	return fmt.Sprintf("UnitID IN (%s)", strings.Join(ids, ", "))
}

// promoteAddNewUnits finds units added to the upstream space (not tracked by any
// downstream unit) and, on a real run, clones them into the downstream space and
// copies their non-UpgradeUnit links. On a dry run it just lists them.
func promoteAddNewUnits(downstreamSpaceID, upstreamSpaceID uuid.UUID) error {
	newUnits, newWhere, err := promoteUntrackedUpstreamUnits(downstreamSpaceID, upstreamSpaceID)
	if err != nil {
		return err
	}

	if !jsonOutput && outputFormat == "" {
		verb := "Adding"
		if variantPromoteArgs.dryRun {
			verb = "Would add"
		}
		tprint("%s %d unit(s) from upstream", verb, len(newUnits))
	}
	if len(newUnits) == 0 {
		return nil
	}
	if variantPromoteArgs.dryRun {
		for _, u := range newUnits {
			tprint("  + %s", u.Slug)
		}
		return nil
	}

	// Clone the new upstream units into the downstream space.
	cloneWhere := fmt.Sprintf("SpaceID = '%s'", upstreamSpaceID.String())
	if newWhere != "" {
		cloneWhere += " AND " + newWhere
	}
	whereSpace := fmt.Sprintf("SpaceID = '%s'", downstreamSpaceID.String())
	include := "UnitEventID,TargetID,UpstreamUnitID,SpaceID"
	cloneParams := &goclientnew.BulkCreateUnitsParams{Where: &cloneWhere, WhereSpace: &whereSpace, Include: &include}
	// The changeset is associated via the patch (ChangeSetID field) the enhancer
	// writes; BulkCreateUnits has no changeset parameter.
	enhancer, _, err := promotePatchEnhancer()
	if err != nil {
		return err
	}
	patchData, err := EnhancePatchData([]byte("null"), nil, nil, nil, nil, enhancer)
	if err != nil {
		return err
	}
	responses, statusCode, err := bulkCreateUnits(cloneParams, patchData)
	if err != nil {
		return err
	}
	if err := handleBulkCreateOrUpdateResponse(responses, statusCode, "create", ""); err != nil {
		return err
	}

	return promoteCopyLinks(downstreamSpaceID, upstreamSpaceID, newUnits)
}

// promoteCopyLinks copies the non-UpgradeUnit links of the newly-added units into
// the downstream space, retargeting their endpoints to the downstream copies. A
// link that points at another unit in the upstream space is retargeted to that
// unit's downstream copy; a link that points elsewhere keeps its target, and a link
// that already points into the downstream space is not copied at all.
func promoteCopyLinks(downstreamSpaceID, upstreamSpaceID uuid.UUID, newUnits []*goclientnew.Unit) error {
	if len(newUnits) == 0 {
		return nil
	}
	ids := make([]string, 0, len(newUnits))
	for _, u := range newUnits {
		ids = append(ids, "'"+u.UnitID.String()+"'")
	}
	idList := strings.Join(ids, ", ")
	// from_downstream_where finds the downstream copy of each source link's
	// FromUnit via the UpgradeUnit link the clone just created. Use only direct
	// Link fields (SpaceID, not Space.SpaceID) so the filter stays in SQL — the
	// in-memory filter path mishandles UUID fields like FromUnitID.
	fromDownstream := fmt.Sprintf("UpdateType = 'UpgradeUnit' AND SpaceID = '%s'", downstreamSpaceID.String())

	if !jsonOutput && outputFormat == "" {
		tprint("Copying links from the added units...")
	}

	// Intra-space links (to another unit in the upstream space): retarget both
	// endpoints to their downstream copies.
	intraWhere := fmt.Sprintf(
		"SpaceID = '%s' AND UpdateType != 'UpgradeUnit' AND ToSpaceID = '%s' AND FromUnitID IN (%s)",
		upstreamSpaceID.String(), upstreamSpaceID.String(), idList)
	if err := promoteBulkCopyLinks(upstreamSpaceID, intraWhere, fromDownstream, fromDownstream); err != nil {
		return err
	}

	// Cross-space links (to a unit outside the upstream space): retarget only the
	// FromUnit, keeping the original target.
	//
	// A link that already points into this variant is not one of those. Copying it would
	// retarget its FromUnit and leave the target alone, landing a link from the new unit to
	// a unit beside it -- a relationship that belongs to the variant, arriving as though the
	// upstream had asked for it. A syncback link (cub variant create --syncback) is what
	// points this way: it runs from an upstream unit to its clone, so a promotion that
	// copied it would give the new unit a syncback link nobody asked for.
	crossWhere := fmt.Sprintf(
		"SpaceID = '%s' AND UpdateType != 'UpgradeUnit' AND ToSpaceID != '%s' AND ToSpaceID != '%s' AND FromUnitID IN (%s)",
		upstreamSpaceID.String(), upstreamSpaceID.String(), downstreamSpaceID.String(), idList)
	return promoteBulkCopyLinks(upstreamSpaceID, crossWhere, fromDownstream, "")
}

func promoteBulkCopyLinks(upstreamSpaceID uuid.UUID, where, fromDownstreamWhere, toDownstreamWhere string) error {
	// The bulk-create endpoint returns 404 when its source selector matches no
	// links, so skip the call when there's nothing to copy (e.g. a newly-added
	// unit with only intra-space links has no cross-space links to copy).
	srcLinks, err := apiListLinks(upstreamSpaceID.String(), where, "", "")
	if err != nil {
		return err
	}
	if len(srcLinks) == 0 {
		return nil
	}
	bulkRes, err := callBulkCreateLinks(where, "", []byte("null"), false, fromDownstreamWhere, toDownstreamWhere, false)
	if err != nil {
		return err
	}
	return handleBulkLinkUpdateResponse(bulkRes.JSON200, bulkRes.JSON207, bulkRes.StatusCode(), "create", "")
}

// bulkUnitResponses normalizes a 200/207 bulk unit response pair.
func bulkUnitResponses(json200, json207 *[]goclientnew.UnitCreateOrUpdateResponse) (*[]goclientnew.UnitCreateOrUpdateResponse, int) {
	if json200 != nil {
		return json200, 200
	}
	if json207 != nil {
		return json207, 207
	}
	return nil, 0
}
