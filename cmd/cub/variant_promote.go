// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"bytes"
	"fmt"
	"strings"

	"github.com/confighub/sdk/core/cubapi"
	goclientnew "github.com/confighub/sdk/core/openapi/goclient-new"
	"github.com/google/uuid"
	"github.com/spf13/cobra"
)

var variantPromoteArgs struct {
	changeDescription string
	changesetSlug     string
	dryRun            bool
}

var variantPromoteCmd = &cobra.Command{
	Use:   "promote <space>",
	Short: "Promote a variant space to match its upstream space",
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
     when it points at another unit in the upstream space.

Promote waits for triggers to complete. Use --dry-run to preview: the units that would be
upgraded (add -o mutations to see the changes) and the units that would be added.

Examples:
`+"```"+`
  # Promote a variant to match its upstream
  cub variant promote web-prod

  # Preview the changes, including the mutations
  cub variant promote web-prod --dry-run -o mutations

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
	variantPromoteCmd.Flags().BoolVar(&variantPromoteArgs.dryRun, "dry-run", false, "preview the units that would be upgraded and added without changing anything")
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

	// Promote always waits for triggers, except on a dry run (nothing is changed,
	// so there is nothing to wait for).
	wait = !variantPromoteArgs.dryRun

	if err := promoteUpgradeUnits(downstreamSpaceID); err != nil {
		return err
	}
	return promoteAddNewUnits(downstreamSpaceID, upstreamSpaceID)
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

// promoteUpgradeUnits upgrades every downstream unit whose upstream has advanced.
func promoteUpgradeUnits(downstreamSpaceID uuid.UUID) error {
	if !jsonOutput && outputFormat == "" {
		tprint("Upgrading units behind their upstream...")
	}
	where := fmt.Sprintf("SpaceID = '%s' AND UpstreamRevisionNum < UpstreamUnit.HeadRevisionNum", downstreamSpaceID.String())
	include := "UnitEventID,TargetID,UpstreamUnitID,SpaceID"
	upgrade := true
	params := &goclientnew.BulkPatchUnitsParams{Where: &where, Include: &include, Upgrade: &upgrade}
	if variantPromoteArgs.dryRun {
		params.DryRun = &variantPromoteArgs.dryRun
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

// promoteAddNewUnits finds units added to the upstream space (not tracked by any
// downstream unit) and, on a real run, clones them into the downstream space and
// copies their non-UpgradeUnit links. On a dry run it just lists them.
func promoteAddNewUnits(downstreamSpaceID, upstreamSpaceID uuid.UUID) error {
	// Upstream unit IDs already tracked by the downstream space.
	downstreamUnits, err := apiListUnits(downstreamSpaceID.String(), "", "UnitID,UpstreamUnitID")
	if err != nil {
		return err
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
// unit's downstream copy; a link that points elsewhere keeps its target.
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
	crossWhere := fmt.Sprintf(
		"SpaceID = '%s' AND UpdateType != 'UpgradeUnit' AND ToSpaceID != '%s' AND FromUnitID IN (%s)",
		upstreamSpaceID.String(), upstreamSpaceID.String(), idList)
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
