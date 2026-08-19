// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"bytes"
	"encoding/json"
	"fmt"
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

	// Find the change this promotion carries, then refuse to promote it until the previous stage is
	// already running it. A ChangeOrder is itself the change and needs no looking up; everything
	// else has to be read off the Units the promotion would upgrade. The gate runs on a dry run
	// too: a preview that ignored it would describe a promotion that cannot happen.
	promoted := &promotedChange{changeOrder: changeOrder}
	if changeOrder == nil {
		promoted, err = newPromotedChange(downstreamSpaceID)
		if err != nil {
			return err
		}
	}
	if err := validatePreviousStageForPromotion(downstreamSpace, promoted); err != nil {
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

func validatePreviousStageForPromotion(space *goclientnew.Space, promoted *promotedChange) error {
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

		if err := validateDeployedChange(variant, liveStatus.Revision, promoted, stage, variantName); err != nil {
			return err
		}
	}

	return nil
}

// validateDeployedChange errors when the change being promoted is newer than what a Variant of the
// previous stage is running.
//
// liveRevision is the OCI digest live-status reports, which names the Release the Variant is
// actually running -- not necessarily its newest. The Revisions that Release bundles are what is
// deployed there, and each Revision's backlink to the Releases that bundled it is what identifies
// them: the Release's own TagID is unset on older Releases, and a Unit with no Revision carrying a
// Tag supplied at publish time is bundled at its head, so the Tag can be missing from a Revision
// the Release does contain.
func validateDeployedChange(variant *goclientnew.Space, liveRevision string,
	promoted *promotedChange, stage, variantName string) error {
	if liveRevision == "" {
		return errors.Newf("unable to promote to stage '%s', live-status for Variant '%s' reports no revision", stage, variantName)
	}

	// argobot reports the digest of the OCI manifest it pulled, which is the Release's
	// ManifestDigest; a reporter naming the stored bundle instead is matched by Digest.
	var release *goclientnew.Release
	for _, field := range []string{"ManifestDigest", "Digest"} {
		releases, err := apiListReleases(variant.SpaceID.String(),
			field+" = '"+liveRevision+"'",
			"ReleaseID,SpaceID,ReleaseNum,ManifestDigest,Digest,Published", "")
		if err != nil {
			return err
		}
		if len(releases) > 0 && releases[0].Release != nil {
			release = releases[0].Release
			break
		}
	}
	if release == nil {
		return errors.Newf("unable to promote to stage '%s', no Release of Variant '%s' matches the deployed revision %s", stage, variantName, liveRevision)
	}

	// The Revision of each Unit the deployed Release bundles.
	bundled, err := apiSearchListRevisions(
		fmt.Sprintf("SpaceID = '%s' AND Releases ? '%s'", variant.SpaceID, release.ReleaseID),
		"RevisionID,SpaceID,UnitID,RevisionNum,ChangeSetID", "")
	if err != nil {
		return err
	}
	deployed := make(map[uuid.UUID]int64, len(bundled))
	for _, extended := range bundled {
		if extended.Revision != nil && extended.Revision.RevisionNum > deployed[extended.Revision.UnitID] {
			deployed[extended.Revision.UnitID] = extended.Revision.RevisionNum
		}
	}

	// The Variant's own Units, by the upstream Unit each one tracks, so they line up with the
	// change being promoted.
	units, err := apiListUnits(variant.SpaceID.String(), "", "UnitID,Slug,HeadRevisionNum,UpstreamUnitID,UpstreamRevisionNum")
	if err != nil {
		return err
	}
	byUpstream := make(map[uuid.UUID]*goclientnew.Unit, len(units))
	byUnitID := make(map[uuid.UUID]*goclientnew.Unit, len(units))
	for _, unit := range units {
		byUnitID[unit.UnitID] = unit
		if unit.UpstreamUnitID != nil {
			byUpstream[*unit.UpstreamUnitID] = unit
		}
	}

	// A ChangeOrder is the change's own identity and crosses Spaces, so where it reached this
	// Variant is a query. Without one the change is a range of upstream Revisions, and the only
	// thing comparable here is what the Variant recorded taking.
	if promoted.changeOrder != nil {
		return validateDeployedChangeOrder(variant, release, deployed, byUpstream, byUnitID,
			promoted.changeOrder, stage, variantName)
	}

	for upstreamUnitID, revisionNum := range promoted.revisionNum {
		unit, ok := byUpstream[upstreamUnitID]
		if !ok {
			return errors.Newf("unable to promote to stage '%s', Variant '%s' does not have unit '%s'",
				stage, variantName, promoted.unitSlug[upstreamUnitID])
		}
		if unit.UpstreamRevisionNum < revisionNum {
			return errors.Newf("unable to promote to stage '%s', Variant '%s' is at upstream revision %d for unit '%s', behind the %d being promoted",
				stage, variantName, unit.UpstreamRevisionNum, unit.Slug, revisionNum)
		}
		// Without a ChangeOrder there is nothing to tell a Revision carrying the change from a
		// local edit made after it, so the whole of what the Variant has must be deployed.
		if deployed[unit.UnitID] < unit.HeadRevisionNum {
			return errors.Newf("unable to promote to stage '%s', Variant '%s' has unit '%s' at revision %d but Release %d deploys revision %d",
				stage, variantName, unit.Slug, unit.HeadRevisionNum, release.ReleaseNum, deployed[unit.UnitID])
		}
	}
	return nil
}

// validateDeployedChangeOrder errors when a Variant has not taken the change order being promoted,
// or has taken it in Revisions its deployed Release does not include.
//
// The Units it has to have taken it into are the ones the change order changed in its own Space,
// not the ones it happens to have landed on here -- driving off the latter would pass a change
// order that reached three of five Units. Units it covers but did not change are not among them:
// with no Revision to replay there is nothing for the Variant to have taken.
//
// The change order's range ends where it ends, so a Revision the Variant made afterwards need not
// be deployed: at or after the change order is the test, not equality with the Variant's head.
func validateDeployedChangeOrder(variant *goclientnew.Space, release *goclientnew.Release, deployed map[uuid.UUID]int64,
	byUpstream, byUnitID map[uuid.UUID]*goclientnew.Unit,
	changeOrder *goclientnew.ChangeOrder, stage, variantName string) error {
	changed, err := changeOrderChangedUnits(changeOrder)
	if err != nil {
		return err
	}

	landed, err := apiSearchListRevisions(
		fmt.Sprintf("SpaceID = '%s' AND ChangeOrders ? '%s'", variant.SpaceID, changeOrder.ChangeOrderID),
		"RevisionID,SpaceID,UnitID,RevisionNum,ChangeSetID", "")
	if err != nil {
		return err
	}
	// The last Revision the change order made in each Unit: all of them have to be deployed, not
	// just the first.
	needed := make(map[uuid.UUID]int64, len(landed))
	for _, extended := range landed {
		revision := extended.Revision
		if revision == nil {
			continue
		}
		if revision.RevisionNum > needed[revision.UnitID] {
			needed[revision.UnitID] = revision.RevisionNum
		}
	}

	for sourceUnitID, sourceSlug := range changed {
		// A Variant that is the change order's own Space tracks no upstream: its Units are the
		// ones the change order changed.
		unit, ok := byUpstream[sourceUnitID]
		if !ok {
			unit, ok = byUnitID[sourceUnitID]
		}
		if !ok {
			return errors.Newf("unable to promote to stage '%s', Variant '%s' does not have unit '%s', which change order '%s' changed",
				stage, variantName, sourceSlug, changeOrder.Slug)
		}
		revisionNum, ok := needed[unit.UnitID]
		if !ok {
			return errors.Newf("unable to promote to stage '%s', Variant '%s' has not taken change order '%s' into unit '%s'",
				stage, variantName, changeOrder.Slug, unit.Slug)
		}
		if deployed[unit.UnitID] < revisionNum {
			return errors.Newf("unable to promote to stage '%s', Variant '%s' took change order '%s' into unit '%s' at revision %d but Release %d deploys revision %d",
				stage, variantName, changeOrder.Slug, unit.Slug, revisionNum, release.ReleaseNum, deployed[unit.UnitID])
		}
	}
	return nil
}

// promotedChange is the change a promotion carries, named the way another Space can be asked
// whether it already has it.
type promotedChange struct {
	// changeOrder is the ChangeOrder being promoted, when --change-order named one. It is the one
	// identity a change keeps as it crosses Spaces -- promoting it stamps its ID onto every
	// Revision it makes, wherever it lands -- so the other fields are unset when it is present:
	// nothing has to be lined up by revision number.
	changeOrder *goclientnew.ChangeOrder

	// revisionNum is the upstream Revision the promotion takes each Unit to, keyed by upstream
	// UnitID. Another variant of the same upstream records what it last took the same way, in its
	// own UpstreamRevisionNum, which is what makes the two comparable.
	revisionNum map[uuid.UUID]int64

	// unitSlug names each of those upstream Units, for the diagnostics.
	unitSlug map[uuid.UUID]string
}

// newPromotedChange finds the change a promotion carries, reading it off the Space being promoted
// into: the Units behind their upstream are the ones it would upgrade, and each one's UpstreamUnit
// carries the Revision it is about to take. The upstream Space is neither needed nor consulted.
//
// This is for a promotion without --change-order. A ChangeOrder is itself the change, and
// promoteChangeOrder has already resolved it.
func newPromotedChange(downstreamSpaceID uuid.UUID) (*promotedChange, error) {
	behind, err := apiListExtendedUnits(downstreamSpaceID.String(),
		"UpstreamRevisionNum < UpstreamUnit.HeadRevisionNum", "", "", "", "", false,
		"UnitID,Slug,UpstreamUnitID,UpstreamRevisionNum,UpstreamUnit.HeadRevisionNum,UpstreamUnit.Slug",
		"", "")
	if err != nil {
		return nil, err
	}

	promoted := &promotedChange{
		revisionNum: make(map[uuid.UUID]int64, len(behind)),
		unitSlug:    make(map[uuid.UUID]string, len(behind)),
	}
	for _, extended := range behind {
		unit := extended.Unit
		if unit == nil || unit.UpstreamUnitID == nil || extended.UpstreamUnit == nil {
			continue
		}
		upstreamUnitID := *unit.UpstreamUnitID
		promoted.revisionNum[upstreamUnitID] = extended.UpstreamUnit.HeadRevisionNum
		promoted.unitSlug[upstreamUnitID] = extended.UpstreamUnit.Slug
	}
	return promoted, nil
}

// changeOrderChangedUnits names the Units a change order actually changed, in the Space it resides
// in -- the Space holding the changes to promote. Those are the Units every target has to have
// taken it into.
//
// A change order's scope is wider than this: it Tags every Unit in scope at creation, including
// ones the change never touched. Those have no Revision to replay anywhere, so requiring a target
// to carry the change order into them would block on a change that was never made.
func changeOrderChangedUnits(changeOrder *goclientnew.ChangeOrder) (map[uuid.UUID]string, error) {
	revisions, err := apiSearchListRevisions(
		fmt.Sprintf("SpaceID = '%s' AND ChangeOrders ? '%s'", changeOrder.SpaceID, changeOrder.ChangeOrderID),
		"RevisionID,SpaceID,UnitID,RevisionNum,ChangeSetID", "")
	if err != nil {
		return nil, err
	}
	changed := make(map[uuid.UUID]string, len(revisions))
	for _, extended := range revisions {
		if extended.Revision == nil {
			continue
		}
		slug := ""
		if extended.Unit != nil {
			slug = extended.Unit.Slug
		}
		changed[extended.Revision.UnitID] = slug
	}
	return changed, nil
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
