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
	"sort"
	"strings"
	"time"

	goclientnew "github.com/confighub/sdk/core/openapi/goclient-new"
	"sigs.k8s.io/yaml"
)

// VariantSyncSpec describes a sync from one variant to another, as `cub variant sync` does:
// the target's units take the source's configuration, and the target gets the links the
// source has and it does not. The two need not be related. Two variants of one base each
// descend from the base and not from each other, so promotion cannot carry one's
// configuration to the other; a sync can.
type VariantSyncSpec struct {
	// Source and Target are the two variants' Spaces.
	Source goclientnew.UUID
	Target goclientnew.UUID
	// Protect records the paths each merge writes as protected local overrides, so that a
	// later upgrade from the target's upstream does not write the upstream's values back over
	// what the sync brought. It is a step of its own after the merge: a merge is itself held
	// back by protection, so the server refuses to protect what one writes.
	Protect bool
	// ChangeDescription is recorded on every unit the sync writes.
	ChangeDescription string
	// DryRun sends every request as a dry run.
	DryRun bool
	// IncludeConfigData returns each target unit's configuration after its merge, or on a dry
	// run as the merge would leave it, as UnitSyncResult.ConfigData.
	IncludeConfigData bool
}

// Outcomes of syncing one unit.
const (
	UnitSyncMerged    = "merged"
	UnitSyncUnchanged = "unchanged"
	UnitSyncCloned    = "cloned"
	UnitSyncSkipped   = "skipped"
	UnitSyncFailed    = "failed"
)

// UnitSyncResult is what a sync did to one of the source's units in the target.
type UnitSyncResult struct {
	Slug string
	// Action is one of the UnitSync constants. A unit the target did not have is cloned and
	// then merged, and is reported as cloned.
	Action string
	// Reason says why a unit was skipped or failed.
	Reason string `json:",omitempty"`
	// Diff is what the merge changed, or on a dry run would change.
	Diff *goclientnew.ConfigDiff `json:",omitempty"`
	// Conflicts are the source's changes the merge did not make: those at a path the target
	// protects, above all.
	Conflicts []goclientnew.MutationConflict `json:",omitempty"`
	// MatchesSource reports whether the target's configuration is now the source's: the same
	// documents with the same values, whatever the order of their keys, for YAML and JSON, and
	// the same text for other formats. A path the target protects keeps the target's value,
	// and is one reason it is not.
	MatchesSource bool
	// ConfigData is the target unit's configuration after the merge, when asked for.
	ConfigData string `json:",omitempty"`
	// Protected is the number of paths the sync protected, counting a resource the merge added
	// whole, which it protected as a whole, as one.
	Protected int `json:",omitempty"`
	// Unit is the target unit after the sync, absent on a dry run of a unit it would clone.
	Unit *goclientnew.Unit `json:"-"`
}

// Outcomes of syncing one link.
const (
	LinkSyncCreated = "created"
	LinkSyncSkipped = "skipped"
	LinkSyncFailed  = "failed"
)

// LinkSyncResult is what a sync did with one of the source's links the target did not have.
type LinkSyncResult struct {
	Slug       string
	FromUnit   string
	ToUnit     string
	UpdateType string
	// Action is one of the LinkSync constants; on a dry run, created means it would be.
	Action string
	Reason string `json:",omitempty"`
}

// VariantSyncResult is what SyncVariant did, unit by unit and link by link.
type VariantSyncResult struct {
	Units []UnitSyncResult
	Links []LinkSyncResult
}

// syncWriteAttempts is how many times a unit's merge is tried when another write to the unit
// gets in first.
const syncWriteAttempts = 5

// syncUnitSelect are the unit fields the sync reads to pair the two variants' units.
const syncUnitSelect = "UnitID,Slug,SpaceID,HeadRevisionNum,UpstreamUnitID,ToolchainType"

// SyncVariant syncs spec.Target to spec.Source.
//
// Units are paired by slug. Each pair is merged as one squashed diff of the source unit from
// its first Revision to its head: everything that has happened to it since it was made. A unit
// made empty and then given its content has an empty first Revision, so the diff is the whole
// of its configuration; for a clone, whose first Revision is what its upstream gave it, it is
// the source variant's own changes and whatever upgrades it took, and what the clone started
// with stays as the target has it. The merge respects the target's protected paths, as any
// merge does: such a path keeps the target's value and is reported as a conflict. It is a
// merge from another unit rather than an external-source merge, which would leave the target
// recording the source as its external source and change what later pushes of a real one do.
// A source unit the target does not have is
// cloned from the source unit's upstream first, as promotion clones a unit the upstream added,
// so that it has an upstream like the target's other units; one with no upstream is skipped.
//
// Then each of the source's links that the target has no link of the same slug for is copied,
// from the target's unit of the same slug, to the target's unit of the same slug when the link
// is within the source, and to the same unit when it leads out of it. UpgradeUnit links are not
// copied: they are each unit's upstream, which the sync does not change.
//
// A failure of one unit or link is reported in the result, and the sync goes on to the next.
func SyncVariant(ctx context.Context, c *Client, spec VariantSyncSpec) (*VariantSyncResult, error) {
	if spec.Source == spec.Target {
		return nil, fmt.Errorf("cubapi: SyncVariant needs two different spaces")
	}
	targetSpace, err := c.API.GetSpaceWithResponse(ctx, spec.Target, &goclientnew.GetSpaceParams{})
	if IsAPIError(err, targetSpace) {
		return nil, InterpretErrorGeneric(err, targetSpace)
	}
	sourceUnits, err := syncUnitsBySlug(ctx, c, spec.Source)
	if err != nil {
		return nil, err
	}
	targetUnits, err := syncUnitsBySlug(ctx, c, spec.Target)
	if err != nil {
		return nil, err
	}

	slugs := make([]string, 0, len(sourceUnits))
	for slug := range sourceUnits {
		slugs = append(slugs, slug)
	}
	sort.Strings(slugs)

	result := &VariantSyncResult{}
	for _, slug := range slugs {
		source := sourceUnits[slug]
		target, ok := targetUnits[slug]
		action := ""
		if !ok {
			if source.UpstreamUnitID == nil {
				result.Units = append(result.Units, UnitSyncResult{Slug: slug, Action: UnitSyncSkipped,
					Reason: "the target has no unit of this slug, and the source's has no upstream to clone it from"})
				continue
			}
			target, err = cloneSyncUnit(ctx, c, spec, targetSpace.JSON200.Space, *source.UpstreamUnitID)
			if err != nil {
				result.Units = append(result.Units, UnitSyncResult{Slug: slug, Action: UnitSyncFailed, Reason: err.Error()})
				continue
			}
			if spec.DryRun {
				// The clone was not made, so there is nothing to merge into.
				result.Units = append(result.Units, UnitSyncResult{Slug: slug, Action: UnitSyncCloned,
					Reason: "cloned from the source unit's upstream; the merge is not previewed until the clone exists"})
				continue
			}
			targetUnits[slug] = target
			action = UnitSyncCloned
		}
		unitResult := mergeSyncUnit(ctx, c, spec, source, target)
		if action != "" && unitResult.Action != UnitSyncFailed {
			unitResult.Action = action
		}
		result.Units = append(result.Units, unitResult)
	}

	links, err := copySyncLinks(ctx, c, spec, sourceUnits, targetUnits)
	if err != nil {
		return result, err
	}
	result.Links = links
	return result, nil
}

func syncUnitsBySlug(ctx context.Context, c *Client, spaceID goclientnew.UUID) (map[string]*goclientnew.Unit, error) {
	units, err := ListUnits(ctx, c, NewWhere("").SpaceID(spaceID), ListOpts{Select: syncUnitSelect})
	if err != nil {
		return nil, err
	}
	bySlug := make(map[string]*goclientnew.Unit, len(units))
	for _, unit := range units {
		if unit.Unit != nil {
			bySlug[unit.Unit.Slug] = unit.Unit
		}
	}
	return bySlug, nil
}

// cloneSyncUnit clones the upstream unit into the target space, with an UpgradeUnit link back
// to it, and moves it into the target's namespace when the target records one.
func cloneSyncUnit(ctx context.Context, c *Client, spec VariantSyncSpec, targetSpace *goclientnew.Space,
	upstreamUnitID goclientnew.UUID) (*goclientnew.Unit, error) {
	where := NewWhere("").Eq("UnitID", upstreamUnitID.String()).String()
	whereSpace := NewWhere("").SpaceID(targetSpace.SpaceID).String()
	include := "UnitEventID,TargetID,UpstreamUnitID,SpaceID"
	params := &goclientnew.BulkCreateUnitsParams{Where: &where, WhereSpace: &whereSpace, Include: &include, DryRun: dryRun(spec.DryRun)}
	patch, err := layerPatch(nil, map[string]any{"LastChangeDescription": spec.ChangeDescription}, nil)
	if err != nil {
		return nil, err
	}
	res, err := c.API.BulkCreateUnitsWithBodyWithResponse(ctx, params, mergePatchContentType, bytes.NewReader(patch))
	if IsAPIError(err, res) {
		return nil, InterpretErrorGeneric(err, res)
	}
	responses := res.JSON200
	if responses == nil {
		responses = res.JSON207
	}
	if responses == nil || len(*responses) != 1 {
		return nil, fmt.Errorf("cloning the upstream unit %s: unexpected response", upstreamUnitID)
	}
	created := (*responses)[0]
	if created.Error != nil {
		return nil, fmt.Errorf("cloning the upstream unit %s: %s", upstreamUnitID, created.Error.Message)
	}
	if spec.DryRun {
		return created.Unit, nil
	}
	if namespace := targetSpace.Labels[LabelNamespace]; namespace != "" && created.Unit != nil {
		variantSpec := VariantSpec{Namespace: namespace, ChangeDescription: spec.ChangeDescription}
		unitWhere := NewWhere("").Eq("UnitID", created.Unit.UnitID.String()).String()
		if err := setVariantNamespace(ctx, c, variantSpec, targetSpace, "", unitWhere); err != nil {
			return nil, err
		}
	}
	return created.Unit, nil
}

// mergeSyncUnit merges the source unit's whole history into the target unit, as one squashed
// diff from its first Revision to its head, and protects what the merge wrote when asked.
func mergeSyncUnit(ctx context.Context, c *Client, spec VariantSyncSpec, source, target *goclientnew.Unit) UnitSyncResult {
	result := UnitSyncResult{Slug: target.Slug}
	fail := func(err error) UnitSyncResult {
		result.Action, result.Reason = UnitSyncFailed, err.Error()
		return result
	}

	sourceData, err := UnitData(ctx, c, source.SpaceID, source.UnitID)
	if err != nil {
		return fail(err)
	}

	mergeSource := source.UnitID.String()
	mergeBase := "1"
	mergeEnd := fmt.Sprintf("%d", source.HeadRevisionNum)
	squash := true
	include := "Diff,ConfigData,MutationSources"
	params := &goclientnew.UpdateUnitParams{
		MergeSource: &mergeSource,
		MergeBase:   &mergeBase,
		MergeEnd:    &mergeEnd,
		Squash:      &squash,
		DryRun:      dryRun(spec.DryRun),
		Include:     &include,
	}
	// The sync's own writes set off resolves of the target's links -- a ConfigMap's merge
	// re-hashes it into the Deployment that mounts it -- so the unit can change between being
	// read and being written. The write then fails with a conflict, and is retried on what the
	// unit holds now.
	var res *goclientnew.UpdateUnitResponse
	var before int64
	for attempt := 1; ; attempt++ {
		current, err := c.API.GetUnitWithResponse(ctx, target.SpaceID, target.UnitID, &goclientnew.GetUnitParams{})
		if IsAPIError(err, current) {
			return fail(InterpretErrorGeneric(err, current))
		}
		unit := *current.JSON200.Unit
		before = unit.HeadRevisionNum
		unit.LastChangeDescription = spec.ChangeDescription
		res, err = c.API.UpdateUnitWithResponse(ctx, target.SpaceID, target.UnitID, params, unit)
		if res != nil && res.StatusCode() == http.StatusConflict && attempt < syncWriteAttempts {
			time.Sleep(time.Duration(attempt) * 250 * time.Millisecond)
			continue
		}
		if IsAPIError(err, res) {
			return fail(InterpretErrorGeneric(err, res))
		}
		break
	}
	merged := res.JSON200
	if merged.Error != nil {
		return fail(fmt.Errorf("%s", merged.Error.Message))
	}
	result.Unit = merged.Unit
	result.Diff = merged.Diff
	if merged.Conflicts != nil {
		result.Conflicts = *merged.Conflicts
	}
	result.MatchesSource = sameConfiguration(merged.ConfigData, sourceData, target.ToolchainType)
	if spec.IncludeConfigData {
		result.ConfigData = merged.ConfigData
	}

	result.Action = UnitSyncUnchanged
	if spec.DryRun {
		if merged.Diff != nil && len(merged.Diff.Resources) > 0 {
			result.Action = UnitSyncMerged
		}
		return result
	}
	if merged.Unit == nil || merged.Unit.HeadRevisionNum == before {
		return result
	}
	result.Action = UnitSyncMerged

	if spec.Protect && merged.MutationSources != nil {
		protection := syncProtection(*merged.MutationSources, merged.Unit.HeadMutationNum)
		if len(protection) > 0 {
			protectRes, err := c.API.SetUnitProtectionWithResponse(ctx, target.SpaceID, target.UnitID,
				goclientnew.UnitProtectionRequest{ResourceProtection: protection})
			if IsAPIError(err, protectRes) {
				return fail(fmt.Errorf("the merge succeeded, but protecting what it wrote failed: %w", InterpretErrorGeneric(err, protectRes)))
			}
			for _, resource := range protection {
				result.Protected += len(resource.Protected)
			}
		}
	}
	return result
}

// sameConfiguration reports whether two configurations of a toolchain say the same thing. A
// merge keeps the target's order of keys and style of lists, so YAML and JSON are compared as
// documents -- parsed, comments dropped, in any order -- and anything else as text.
func sameConfiguration(a, b, toolchainType string) bool {
	if strings.TrimSpace(a) == strings.TrimSpace(b) {
		return true
	}
	if !strings.HasSuffix(toolchainType, "/YAML") && !strings.HasSuffix(toolchainType, "/JSON") {
		return false
	}
	docsA, errA := canonicalDocuments(a)
	docsB, errB := canonicalDocuments(b)
	if errA != nil || errB != nil || len(docsA) != len(docsB) {
		return false
	}
	for i := range docsA {
		if docsA[i] != docsB[i] {
			return false
		}
	}
	return true
}

// documentSeparator is a line that separates YAML documents.
var documentSeparator = regexp.MustCompile(`(?m)^---[ \t]*$`)

// canonicalDocuments returns each non-empty document of a YAML or JSON configuration as
// canonical JSON, sorted, so that two configurations with the same documents compare equal.
func canonicalDocuments(data string) ([]string, error) {
	var docs []string
	for _, part := range documentSeparator.Split(data, -1) {
		var doc any
		if err := yaml.Unmarshal([]byte(part), &doc); err != nil {
			return nil, err
		}
		if doc == nil {
			continue
		}
		canonical, err := json.Marshal(doc)
		if err != nil {
			return nil, err
		}
		docs = append(docs, string(canonical))
	}
	sort.Strings(docs)
	return docs, nil
}

// syncProtection returns, for each resource, the paths the Mutation numbered mutationNum
// wrote, to protect: the merge's own writes. A resource that Mutation added whole has no paths
// of its own, and is protected as a whole, by the empty path.
func syncProtection(mutationSources goclientnew.ResourceMutationList, mutationNum int64) []goclientnew.ResourceProtection {
	var protection []goclientnew.ResourceProtection
	for _, rm := range mutationSources {
		if rm.Resource == nil {
			continue
		}
		paths := map[string]bool{}
		if rm.PathMutationMap != nil {
			for path, info := range *rm.PathMutationMap {
				if info.Index == mutationNum && info.MutationType != nil && *info.MutationType != goclientnew.None {
					paths[path] = true
				}
			}
		}
		if len(paths) == 0 {
			if info := rm.ResourceMutationInfo; info != nil && info.Index == mutationNum && info.MutationType != nil &&
				*info.MutationType == goclientnew.Add {
				paths[""] = true
			}
		}
		if len(paths) > 0 {
			protection = append(protection, goclientnew.ResourceProtection{
				Resource:  &goclientnew.ResourceInfo{ResourceType: rm.Resource.ResourceType, ResourceName: rm.Resource.ResourceName},
				Protected: paths,
			})
		}
	}
	sort.Slice(protection, func(a, b int) bool {
		return protection[a].Resource.ResourceType+protection[a].Resource.ResourceName <
			protection[b].Resource.ResourceType+protection[b].Resource.ResourceName
	})
	return protection
}

// copySyncLinks copies each of the source's links that the target has no link of the same
// slug for. See SyncVariant.
func copySyncLinks(ctx context.Context, c *Client, spec VariantSyncSpec, sourceUnits, targetUnits map[string]*goclientnew.Unit) ([]LinkSyncResult, error) {
	sourceLinks, err := ListLinks(ctx, c, NewWhere("").SpaceID(spec.Source), ListOpts{Include: "FromUnitID,ToUnitID,ToSpaceID"})
	if err != nil {
		return nil, err
	}
	targetLinks, err := ListLinks(ctx, c, NewWhere("").SpaceID(spec.Target), ListOpts{Select: "LinkID,Slug"})
	if err != nil {
		return nil, err
	}
	have := map[string]bool{}
	for _, link := range targetLinks {
		if link.Link != nil {
			have[link.Link.Slug] = true
		}
	}
	sort.Slice(sourceLinks, func(a, b int) bool { return syncLinkSlug(sourceLinks[a]) < syncLinkSlug(sourceLinks[b]) })

	var results []LinkSyncResult
	for _, extended := range sourceLinks {
		link := extended.Link
		if link == nil || have[link.Slug] {
			continue
		}
		result := LinkSyncResult{Slug: link.Slug, UpdateType: link.UpdateType}
		fromSlug := ""
		if extended.FromUnit != nil {
			fromSlug = extended.FromUnit.Slug
		}
		result.FromUnit = fromSlug
		toName := link.ToUnitID.String()
		if extended.ToUnit != nil {
			toName = extended.ToUnit.Slug
			if extended.ToSpace != nil && link.ToSpaceID != spec.Source {
				toName = extended.ToSpace.Slug + "/" + toName
			}
		}
		result.ToUnit = toName

		copied, reason := syncLinkCopy(*link, spec, fromSlug, extended.ToUnit, targetUnits)
		if copied == nil {
			result.Action, result.Reason = LinkSyncSkipped, reason
			results = append(results, result)
			continue
		}
		res, err := c.API.CreateLinkWithResponse(ctx, spec.Target, &goclientnew.CreateLinkParams{DryRun: dryRun(spec.DryRun)}, *copied)
		if IsAPIError(err, res) {
			result.Action, result.Reason = LinkSyncFailed, InterpretErrorGeneric(err, res).Error()
		} else {
			result.Action = LinkSyncCreated
		}
		results = append(results, result)
	}
	return results, nil
}

func syncLinkSlug(link *goclientnew.ExtendedLink) string {
	if link.Link == nil {
		return ""
	}
	return link.Link.Slug
}

// syncLinkCopy is a source link as the target's: from the target's unit of the same slug, to
// the target's unit of the same slug when the link is within the source and to the same unit
// when it leads out of it. It keeps everything the link says about what it resolves, and its
// UpstreamLinkID, which names the link it was itself copied from. It starts with no merge
// history, no delete gates and nothing derived, as a link copied in bulk does. It returns nil
// and the reason when the link cannot be copied.
func syncLinkCopy(link goclientnew.Link, spec VariantSyncSpec, fromSlug string, toUnit *goclientnew.Unit,
	targetUnits map[string]*goclientnew.Unit) (*goclientnew.Link, string) {
	if link.UpdateType == "UpgradeUnit" {
		return nil, "an UpgradeUnit link is its unit's upstream, which a sync does not change"
	}
	from, ok := targetUnits[fromSlug]
	if !ok {
		return nil, fmt.Sprintf("the target has no unit %q to link from", fromSlug)
	}
	copied := link
	copied.LinkID = goclientnew.UUID{}
	copied.SpaceID = spec.Target
	copied.SpaceSlug = ""
	copied.FromUnitID = from.UnitID
	if link.ToSpaceID == spec.Source {
		if toUnit == nil {
			return nil, "its unit in the source could not be read"
		}
		to, ok := targetUnits[toUnit.Slug]
		if !ok {
			return nil, fmt.Sprintf("the target has no unit %q to link to", toUnit.Slug)
		}
		copied.ToUnitID = to.UnitID
		copied.ToSpaceID = spec.Target
	}
	copied.OrganizationID = goclientnew.UUID{}
	copied.CreatedAt, copied.UpdatedAt = time.Time{}, time.Time{}
	copied.Version = 0
	copied.Hash = ""
	copied.EntityType = ""
	copied.Stale = false
	copied.Bindings = nil
	copied.BackingUnitID = nil
	copied.DeleteGates = nil
	copied.UpstreamLastMergedRevisionNum = 0
	copied.DownstreamLastMergedRevisionNum = 0
	return &copied, ""
}
