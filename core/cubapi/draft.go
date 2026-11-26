// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package cubapi

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strings"

	goclientnew "github.com/confighub/sdk/core/openapi/goclient-new"
)

// A draft Space is a clone of some or all of a Space's Units, made to change them without
// touching the Space itself: the original keeps taking edits, upgrades and releases while the
// draft is open, several drafts can change the same Unit, and Triggers, validation and Link
// resolution run in the draft as in any Space. Each clone has a syncback Link, a MergeUnits Link
// in the original Space from the original Unit to the clone, which is how the draft lands: the
// Links are resolved into the original Units inside a ChangeSet of the original Space, so what
// landed is one named change there. The draft Space is then archived, which archives the
// syncback Links with it, and stays as the record of the change.
//
// A draft Space stays in its original's Component. It carries the Draft label, naming the
// draft, and the Stage label StageDraft, and its UpstreamSpaceID names the original.

const (
	// LabelDraft is the Space label naming a draft: a draft Space carries it, and its
	// UpstreamSpaceID names the Space it is a draft of.
	LabelDraft = "Draft"
	// LabelStage is the Space label naming the Stage a Space is in.
	LabelStage = "Stage"
	// StageDraft is the Stage label of a draft Space, so that ChangeWorkflow Stages, which
	// select Spaces by their labels, can tell drafts from the Spaces changes are promoted to.
	StageDraft = "Draft"
	// AnnotationDraftChangeSetID is the draft Space annotation naming the ChangeSet in the
	// original Space that the draft lands in, once ProposeDraft or LandDraft has created it.
	AnnotationDraftChangeSetID = "DraftChangeSetID"
)

// draftOutgoingLinksWhere selects the Links of the original Units, to Units the draft does not
// clone, that are copied onto the clones, so a clone reads from the same producers its original
// does. Left out are the original's own UpgradeUnit Link, since the clone's upgrade Link points
// at the original instead, and the syncback Links of other drafts, which point at their clones.
const draftOutgoingLinksWhere = "UpdateType != 'UpgradeUnit' AND Slug NOT LIKE 'syncback-%'"

var draftNamePattern = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]*[a-z0-9])?$`)

// ValidateDraftName refuses a draft name that cannot be part of a Space slug.
func ValidateDraftName(name string) error {
	if !draftNamePattern.MatchString(name) {
		return fmt.Errorf("draft name %q must be lowercase letters, digits and dashes, starting and ending with a letter or digit", name)
	}
	return nil
}

// DraftSpaceSlug is the slug of the Space holding the draft name of the Space originalSlug.
func DraftSpaceSlug(originalSlug, name string) string {
	return originalSlug + "-draft-" + name
}

// draftVariantLabel is the Variant label of a draft of a Space with Variant label variant. A
// draft is in its original's Component, where a Space is named by its Variant label, so a draft
// needs one of its own.
func draftVariantLabel(variant, name string) string {
	if variant == "" {
		return "draft-" + name
	}
	return variant + "-draft-" + name
}

// DraftSpec describes a draft to create.
type DraftSpec struct {
	// Original is the Space to draft a change to.
	Original goclientnew.UUID
	// Name names the draft among the original's drafts.
	Name string
	// UnitWhere selects the Units to draft, as a where expression over the original's Units.
	// Empty drafts every one.
	UnitWhere string
	// ReleaseTarget gives the draft a release Target of its own, for a preview deployment. A
	// draft has none otherwise, so that releasing it never publishes to the original's Target.
	ReleaseTarget *goclientnew.UUID
	// ChangeDescription is recorded on the clones' first Revision.
	ChangeDescription string
	// Diff asks each clone for what it changed; see VariantSpec.Diff.
	Diff bool
}

// CreateDraft clones the Units spec selects into a draft Space of spec.Original, each with a
// syncback Link back to its original Unit, and copies their Links. The Space's slug is
// DraftSpaceSlug of the original's.
func CreateDraft(ctx context.Context, c *Client, spec DraftSpec) (*VariantResult, error) {
	if err := ValidateDraftName(spec.Name); err != nil {
		return nil, err
	}
	originalRes, err := c.API.GetSpaceWithResponse(ctx, spec.Original, &goclientnew.GetSpaceParams{})
	if IsAPIError(err, originalRes) {
		return nil, InterpretErrorGeneric(err, originalRes)
	}
	original := originalRes.JSON200.Space
	if original.Labels[LabelDraft] != "" {
		return nil, fmt.Errorf("space %s is itself a draft; draft a change to the space it drafts instead", original.Slug)
	}
	existing, err := FindDraft(ctx, c, original.SpaceID, spec.Name)
	if err != nil {
		return nil, err
	}
	if existing != nil {
		return nil, fmt.Errorf("space %s already has a draft named %s, in space %s", original.Slug, spec.Name, existing.Slug)
	}
	// A draft that landed keeps its ChangeSet, which is the record of the change and what a
	// ChangeOrder names, so its name stays taken: the new draft could not create its own.
	csSlug := DraftChangeSetSlug(spec.Name)
	taken, err := ListChangeSets(ctx, c, NewWhere("").SpaceID(original.SpaceID).Eq("Slug", csSlug), ListOpts{Select: "ChangeSetID", Limit: 1})
	if err != nil {
		return nil, err
	}
	if len(taken) > 0 {
		return nil, fmt.Errorf("space %s already has a change set named %s, which a draft named %s lands in; choose another name", original.Slug, csSlug, spec.Name)
	}

	// The draft starts with no release Target and no TargetID annotation, so a release of it
	// cannot publish to where the original's go. Its delete gates are not copied by the clone.
	spacePatch, err := json.Marshal(map[string]any{
		"ReleaseTargetID": nil,
		"Annotations":     map[string]any{"TargetID": nil},
	})
	if err != nil {
		return nil, err
	}
	return CreateVariant(ctx, c, VariantSpec{
		Upstream: spec.Original,
		Name:     draftVariantLabel(original.Labels["Variant"], spec.Name),
		VariantLabels: []string{
			LabelStage + "=" + StageDraft,
			LabelDraft + "=" + spec.Name,
		},
		SpacePattern:              "template:" + DraftSpaceSlug(original.Slug, spec.Name),
		SpacePatch:                spacePatch,
		ReleaseTarget:             spec.ReleaseTarget,
		UnitWhere:                 spec.UnitWhere,
		ChangeDescription:         spec.ChangeDescription,
		Syncback:                  true,
		IncludeOutgoingLinksWhere: draftOutgoingLinksWhere,
		Diff:                      spec.Diff,
	})
}

const draftSpaceSelect = "SpaceID,Slug,Labels,Annotations,UpstreamSpaceID,ComponentID,OrganizationID,HiddenReason,ReleaseTargetID"

// FindDraft returns the open draft of the Space originalID named name, or nil when it has
// none. An archived draft, one that has landed or been abandoned, is not open.
func FindDraft(ctx context.Context, c *Client, originalID goclientnew.UUID, name string) (*goclientnew.Space, error) {
	where := NewWhere("").Eq("UpstreamSpaceID", originalID.String()).Eq("Labels."+LabelDraft, name)
	spaces, err := ListSpaces(ctx, c, where, ListOpts{Select: draftSpaceSelect})
	if err != nil {
		return nil, err
	}
	for _, s := range spaces {
		if s.Space != nil {
			return s.Space, nil
		}
	}
	return nil, nil
}

// ListDrafts returns the open drafts of the Space originalID, or of every Space when it is nil.
// includeHidden lists archived ones too, as ListOpts.IncludeHidden does.
func ListDrafts(ctx context.Context, c *Client, originalID *goclientnew.UUID, includeHidden string) ([]*goclientnew.ExtendedSpace, error) {
	where := NewWhere("Labels." + LabelDraft + " IS NOT NULL")
	if originalID != nil {
		where = where.Eq("UpstreamSpaceID", originalID.String())
	}
	return ListSpaces(ctx, c, where, ListOpts{Select: draftSpaceSelect, Include: "UpstreamSpaceID", IncludeHidden: includeHidden, OrderBy: "Slug"})
}

// DraftOriginal returns the Space draft is a draft of, refusing a Space that is not a draft.
func DraftOriginal(draft *goclientnew.Space) (goclientnew.UUID, error) {
	if draft.Labels[LabelDraft] == "" || draft.UpstreamSpaceID == nil {
		return goclientnew.UUID{}, fmt.Errorf("space %s is not a draft", draft.Slug)
	}
	return *draft.UpstreamSpaceID, nil
}

// DraftChangeSetSlug is the slug of the ChangeSet a draft named name lands in.
func DraftChangeSetSlug(name string) string {
	return "draft-" + name
}

// DraftChangeSet returns the ChangeSet the draft lands in, or nil when neither ProposeDraft nor
// LandDraft has created it yet.
func DraftChangeSet(ctx context.Context, c *Client, draft *goclientnew.Space) (*goclientnew.ChangeSet, error) {
	id := draft.Annotations[AnnotationDraftChangeSetID]
	if id == "" {
		return nil, nil
	}
	cs, err := ResolveChangeSet(ctx, c, ParseRef(id), ResolveOpts{})
	if err != nil {
		return nil, fmt.Errorf("the change set draft %s lands in: %w", draft.Slug, err)
	}
	return cs.ChangeSet, nil
}

// DraftChangeSetSpec says how to create the ChangeSet a draft lands in.
type DraftChangeSetSpec struct {
	// Description describes the change, for review.
	Description string
	// ChangeWorkflowID governs the ChangeSet: closing it, which landing does, is gated on the
	// workflow's Final prerequisites.
	ChangeWorkflowID *goclientnew.UUID
	// DryRun creates nothing.
	DryRun bool
}

// EnsureDraftChangeSet returns the ChangeSet the draft lands in, creating it in the original
// Space when it does not exist yet, and records it on the draft Space. It is opened on no Unit:
// the original's Units are locked only once the draft lands. The boolean reports whether it
// was created.
func EnsureDraftChangeSet(ctx context.Context, c *Client, draft *goclientnew.Space, spec DraftChangeSetSpec) (*goclientnew.ChangeSet, bool, error) {
	originalID, err := DraftOriginal(draft)
	if err != nil {
		return nil, false, err
	}
	existing, err := DraftChangeSet(ctx, c, draft)
	if err != nil || existing != nil {
		return existing, false, err
	}
	name := draft.Labels[LabelDraft]
	description := spec.Description
	if description == "" {
		description = fmt.Sprintf("Lands draft space %s", draft.Slug)
	}
	body := goclientnew.ChangeSet{
		Slug:             DraftChangeSetSlug(name),
		Description:      description,
		ChangeWorkflowID: spec.ChangeWorkflowID,
		Labels:           map[string]string{LabelDraft: name},
		Annotations:      map[string]string{"DraftSpaceID": draft.SpaceID.String()},
	}
	res, err := c.API.CreateChangeSetWithResponse(ctx, originalID, &goclientnew.CreateChangeSetParams{DryRun: dryRun(spec.DryRun)}, body)
	if IsAPIError(err, res) {
		return nil, false, fmt.Errorf("creating change set %s: %w", body.Slug, InterpretErrorGeneric(err, res))
	}
	created := res.JSON200
	if spec.DryRun {
		return created, true, nil
	}
	if err := patchSpace(ctx, c, draft.SpaceID, map[string]any{
		"Annotations": map[string]any{AnnotationDraftChangeSetID: created.ChangeSetID.String()},
	}, false); err != nil {
		return created, true, fmt.Errorf("recording change set %s on draft space %s: %w", created.Slug, draft.Slug, err)
	}
	if draft.Annotations == nil {
		draft.Annotations = map[string]string{}
	}
	draft.Annotations[AnnotationDraftChangeSetID] = created.ChangeSetID.String()
	return created, true, nil
}

// DraftUnit is one Unit of a draft, paired with the Unit it was cloned from.
type DraftUnit struct {
	// Draft is the clone, in the draft Space.
	Draft *goclientnew.Unit
	// Link is the syncback Link from the original Unit to the clone, or nil for a Unit created
	// in the draft, which has nothing to land in.
	Link *goclientnew.Link
	// Pending is whether the clone has Revisions its syncback Link has not merged: the change
	// the draft makes to this Unit, which landing brings home.
	Pending bool
}

// DraftUnits returns the draft's Units, each with its syncback Link, in slug order.
func DraftUnits(ctx context.Context, c *Client, draft *goclientnew.Space) ([]DraftUnit, error) {
	originalID, err := DraftOriginal(draft)
	if err != nil {
		return nil, err
	}
	units, err := ListUnits(ctx, c, NewWhere("").SpaceID(draft.SpaceID),
		ListOpts{Select: "UnitID,Slug,SpaceID,HeadRevisionNum,UpstreamUnitID,UpstreamRevisionNum,ChangeSetID", OrderBy: "Slug"})
	if err != nil {
		return nil, err
	}
	links, err := ListLinks(ctx, c, NewWhere("").SpaceID(originalID).Eq("ToSpaceID", draft.SpaceID.String()).Eq("UpdateType", "MergeUnits"), ListOpts{})
	if err != nil {
		return nil, err
	}
	byClone := map[goclientnew.UUID]*goclientnew.Link{}
	for _, l := range links {
		if l.Link != nil {
			byClone[l.Link.ToUnitID] = l.Link
		}
	}
	result := make([]DraftUnit, 0, len(units))
	for _, u := range units {
		if u.Unit == nil {
			continue
		}
		du := DraftUnit{Draft: u.Unit, Link: byClone[u.Unit.UnitID]}
		if du.Link != nil {
			du.Pending = linkHasPending(du.Link, u.Unit.HeadRevisionNum)
		}
		result = append(result, du)
	}
	return result, nil
}

// linkHasPending reports whether a Link has merged less than its source's head: some Revision
// above its watermark it has neither merged nor skipped.
func linkHasPending(link *goclientnew.Link, head int64) bool {
	if head <= link.UpstreamLastMergedRevisionNum {
		return false
	}
	taken := map[int64]bool{}
	for _, n := range link.UpstreamMergedAbove {
		taken[n] = true
	}
	for _, run := range link.UpstreamSkipped {
		for n := run.FromRevisionNum; n <= run.ToRevisionNum; n++ {
			taken[n] = true
		}
	}
	for n := link.UpstreamLastMergedRevisionNum + 1; n <= head; n++ {
		if !taken[n] {
			return true
		}
	}
	return false
}

// DraftLandSpec says how to land a draft.
type DraftLandSpec struct {
	// ChangeSet is used to create the ChangeSet the draft lands in, when it was not proposed.
	ChangeSet DraftChangeSetSpec
	// ForceReason closes the ChangeSet although its ChangeWorkflow's gates do not hold,
	// recording the reason on it.
	ForceReason string
	// DryRun reports what landing would do and changes nothing.
	DryRun bool
}

// DraftLandResult is what LandDraft did.
type DraftLandResult struct {
	// ChangeSet is the ChangeSet in the original Space the draft landed in.
	ChangeSet *goclientnew.ChangeSet
	// CreatedChangeSet is whether landing created it, because the draft was not proposed.
	CreatedChangeSet bool
	// Landed are the original Units' responses to resolving their syncback Links.
	Landed []goclientnew.UnitCreateOrUpdateResponse
	// Unlinked names the Units created in the draft, which have no original to land in.
	Unlinked []string
	// Close is what closing the ChangeSet was judged against, when it has gates.
	Close *goclientnew.ChangeSetCloseResult
	// Closed is whether the ChangeSet closed. When it did not, the change has landed in it and
	// it stays open on the original Units; landing again closes it once its gates hold.
	Closed bool
	// CloseError is why the ChangeSet did not close.
	CloseError string
	// FailedGates names each gate that refused the close, and why, as <space>/<gate>: <reason>.
	FailedGates []string `json:",omitempty"`
	// Archived is whether the draft Space was archived, which happens once the ChangeSet closed.
	Archived bool
}

// LandDraft brings the draft's changes home: it opens the draft's ChangeSet in the original
// Space on the original Units the draft changed, resolves their syncback Links in it -- which
// replays each draft Revision as a Revision of the original Unit, "Replayed revision N of
// <draft-space>/<unit>" -- and closes the ChangeSet. When the ChangeSet closes, the draft Space
// is archived, with its syncback Links, as the record of the change.
//
// A ChangeSet whose ChangeWorkflow's gates do not hold stays open on the original Units with
// the change in it, and the draft is kept; LandDraft called again resolves whatever the draft
// changed since and tries the close again.
//
// Landing is a merge, so a change the original made to the same paths since the draft was
// created can withhold part of the draft's: the withheld changes are recorded as conflicts on
// the original Units, as any merge records them, and returned in Landed.
func LandDraft(ctx context.Context, c *Client, draft *goclientnew.Space, spec DraftLandSpec) (*DraftLandResult, error) {
	if _, err := DraftOriginal(draft); err != nil {
		return nil, err
	}
	units, err := DraftUnits(ctx, c, draft)
	if err != nil {
		return nil, err
	}
	result := &DraftLandResult{}
	var originals []goclientnew.UUID
	for _, u := range units {
		switch {
		case u.Link == nil:
			result.Unlinked = append(result.Unlinked, u.Draft.Slug)
		case u.Pending:
			originals = append(originals, u.Link.FromUnitID)
		}
	}

	// A draft that changed nothing, and has landed nothing in its ChangeSet yet, has nothing to
	// land: a ChangeSet opened on no Unit cannot be closed, since closing takes Units out of it.
	if len(originals) == 0 {
		existing, err := DraftChangeSet(ctx, c, draft)
		if err != nil {
			return nil, err
		}
		holds := false
		if existing != nil {
			if holds, err = changeSetHoldsUnits(ctx, c, existing); err != nil {
				return nil, err
			}
		}
		if !holds {
			return nil, fmt.Errorf("draft %s has no changes to land; change its units first, or abandon it", draft.Slug)
		}
	}

	csSpec := spec.ChangeSet
	csSpec.DryRun = spec.DryRun
	cs, created, err := EnsureDraftChangeSet(ctx, c, draft, csSpec)
	if err != nil {
		return nil, err
	}
	result.ChangeSet, result.CreatedChangeSet = cs, created
	if spec.DryRun && created {
		// The ChangeSet a dry run creates is rolled back, so nothing can be opened on it.
		// Resolving as a dry run without it still reports what each Unit would take.
		cs = nil
	}

	if len(originals) > 0 {
		if cs != nil {
			if _, err := OpenChangeSetOnUnits(ctx, c, cs.ChangeSetID, originals, OpenChangeSetOpts{DryRun: spec.DryRun}); err != nil {
				return result, fmt.Errorf("change set %s was not opened on the units draft %s changes: %w", cs.Slug, draft.Slug, err)
			}
		}
		result.Landed, err = resolveSyncBackLinks(ctx, c, draft, originals, cs, spec.DryRun)
		if err != nil {
			return result, err
		}
		for _, r := range result.Landed {
			if r.Error != nil {
				return result, fmt.Errorf("draft %s did not land in %s: %s", draft.Slug, unitName(r), r.Error.Message)
			}
		}
	}
	if spec.DryRun || cs == nil {
		return result, nil
	}

	if err := closeDraftChangeSet(ctx, c, cs, spec.ForceReason, result); err != nil {
		return result, err
	}
	if !result.Closed {
		return result, nil
	}
	if err := ArchiveDraft(ctx, c, draft); err != nil {
		return result, fmt.Errorf("draft %s landed, but its space was not archived: %w", draft.Slug, err)
	}
	result.Archived = true
	return result, nil
}

func unitName(r goclientnew.UnitCreateOrUpdateResponse) string {
	if r.Unit != nil {
		return "unit " + r.Unit.Slug
	}
	return "a unit"
}

// resolveSyncBackLinks resolves, on each of the original Units named, its syncback Link to the
// draft, in the ChangeSet cs when it is not nil.
func resolveSyncBackLinks(ctx context.Context, c *Client, draft *goclientnew.Space, originals []goclientnew.UUID,
	cs *goclientnew.ChangeSet, dryRunRequest bool) ([]goclientnew.UnitCreateOrUpdateResponse, error) {
	resolve := fmt.Sprintf("Link:ToSpaceID = '%s'", draft.SpaceID)
	patch := map[string]any{}
	if cs != nil {
		patch["ChangeSetID"] = cs.ChangeSetID.String()
	}
	body, err := json.Marshal(patch)
	if err != nil {
		return nil, err
	}
	var all []goclientnew.UnitCreateOrUpdateResponse
	for start := 0; start < len(originals); start += changeSetChunk {
		selection := Where{}.In("UnitID", originals[start:min(start+changeSetChunk, len(originals))]).String()
		params := &goclientnew.BulkPatchUnitsParams{Where: &selection, Resolve: &resolve, DryRun: dryRun(dryRunRequest)}
		if cs != nil {
			params.ChangeSetId = &cs.ChangeSetID
		}
		res, err := c.API.BulkPatchUnitsWithBodyWithResponse(ctx, params, mergePatchContentType, bytes.NewReader(body))
		if IsAPIError(err, res) {
			return all, InterpretErrorGeneric(err, res)
		}
		responses := res.JSON200
		if responses == nil {
			responses = res.JSON207
		}
		if responses != nil {
			all = append(all, *responses...)
		}
	}
	return all, nil
}

// closeDraftChangeSet closes cs by taking every Unit out of it, recording on result whether it
// closed and the gates it was judged against. A ChangeSet open on no Unit has nothing to close.
func closeDraftChangeSet(ctx context.Context, c *Client, cs *goclientnew.ChangeSet, forceReason string, result *DraftLandResult) error {
	selection := NewWhere("").Eq("ChangeSetID", cs.ChangeSetID.String()).String()
	params := &goclientnew.BulkPatchUnitsParams{Where: &selection}
	if forceReason != "" {
		force := true
		params.ForceClose = &force
		params.ForceReason = &forceReason
	}
	res, err := c.API.BulkPatchUnitsWithBodyWithResponse(ctx, params, mergePatchContentType, strings.NewReader(`{"ChangeSetID":null}`))
	if err == nil && res != nil && res.StatusCode() == http.StatusUnprocessableEntity {
		// The gates refused the close, which takes no Unit out of it: the change stays in the
		// open ChangeSet. The error names each gate that does not hold.
		result.CloseError = InterpretErrorGeneric(err, res).Error()
		if res.JSON422 != nil && res.JSON422.Error != nil {
			refusal := res.JSON422.Error
			result.CloseError = refusal.Message
			if refusal.ErrorMetadata != nil {
				for _, item := range refusal.ErrorMetadata.Items {
					result.FailedGates = append(result.FailedGates, item.Item+": "+item.Description)
				}
			}
		}
		return nil
	}
	if IsAPIError(err, res) {
		return fmt.Errorf("closing change set %s: %w", cs.Slug, InterpretErrorGeneric(err, res))
	}
	responses := res.JSON200
	if responses == nil {
		responses = res.JSON207
	}
	result.Closed = true
	if responses == nil {
		return nil
	}
	for _, r := range *responses {
		if result.Close == nil && r.ChangeSetClose != nil {
			result.Close = r.ChangeSetClose
		}
		if r.Error != nil && result.Closed {
			result.Closed = false
			result.CloseError = r.Error.Message
		}
	}
	return nil
}

// ArchiveDraft archives the draft Space with everything in it. The syncback Links in the
// original Space point at the draft's Units, so they are archived with them, and come back if
// the draft is unarchived.
func ArchiveDraft(ctx context.Context, c *Client, draft *goclientnew.Space) error {
	recursive := "true"
	archive := true
	res, err := c.API.DeleteSpaceWithResponse(ctx, draft.SpaceID, &goclientnew.DeleteSpaceParams{Recursive: &recursive, Archive: &archive})
	if IsAPIError(err, res) {
		return InterpretErrorGeneric(err, res)
	}
	return nil
}

// AbandonDraft archives the draft Space without landing it, and archives the ChangeSet it would
// have landed in. It refuses when that ChangeSet is open on any Unit, since the draft has then
// landed in it already: closing the ChangeSet, or reverting it, is what is left to decide.
func AbandonDraft(ctx context.Context, c *Client, draft *goclientnew.Space) (*goclientnew.ChangeSet, error) {
	if _, err := DraftOriginal(draft); err != nil {
		return nil, err
	}
	cs, err := DraftChangeSet(ctx, c, draft)
	if err != nil {
		return nil, err
	}
	if cs != nil {
		holds, err := changeSetHoldsUnits(ctx, c, cs)
		if err != nil {
			return nil, err
		}
		if holds {
			return nil, fmt.Errorf("draft %s has landed in change set %s, which is still open on units of the original space; close it, or revert it with \"cub variant revert\", rather than abandoning the draft", draft.Slug, cs.Slug)
		}
	}
	if err := ArchiveDraft(ctx, c, draft); err != nil {
		return nil, err
	}
	if cs != nil {
		archive := true
		res, err := c.API.DeleteChangeSetWithResponse(ctx, cs.SpaceID, cs.ChangeSetID, &goclientnew.DeleteChangeSetParams{Archive: &archive})
		if IsAPIError(err, res) {
			return cs, fmt.Errorf("draft %s was archived, but change set %s was not: %w", draft.Slug, cs.Slug, InterpretErrorGeneric(err, res))
		}
	}
	return cs, nil
}

// changeSetHoldsUnits reports whether any Unit is in the ChangeSet.
func changeSetHoldsUnits(ctx context.Context, c *Client, cs *goclientnew.ChangeSet) (bool, error) {
	held, err := ListUnits(ctx, c, NewWhere("").Eq("ChangeSetID", cs.ChangeSetID.String()), ListOpts{Select: "UnitID", Limit: 1})
	return len(held) > 0, err
}
