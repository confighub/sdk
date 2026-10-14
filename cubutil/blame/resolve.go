// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package blame

import (
	"context"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/confighub/sdk/core/cubapi"
	"github.com/confighub/sdk/core/function/api"
	goclientnew "github.com/confighub/sdk/core/openapi/goclient-new"
	"github.com/google/uuid"
)

// Resolver turns a MutationSources entry into the chain of changes that put a
// value where it is, following each merge into the unit it merged from.
//
// Everything it reads is per (unit, revision) rather than per field, and a unit has
// far fewer distinct revisions than fields, so the caches are what make blaming a
// hundred-field resource a handful of requests instead of a hundred.
type Resolver struct {
	client   *cubapi.Client
	maxDepth int
	// noUpstream stops every walk at this unit's own record.
	noUpstream bool

	// mutations maps a unit to its MutationNum -> Mutation. The whole set is read at
	// once: a resource's fields are credited to a handful of mutations, and asking
	// for them one at a time costs a request each.
	mutations map[uuid.UUID]map[int64]*goclientnew.ExtendedMutation
	// revisions maps a unit to its RevisionNum -> Revision, read the same way, with the
	// ChangeOrders, Releases and Tags each one belongs to.
	revisions map[uuid.UUID]map[int64]*goclientnew.ExtendedRevision
	// units caches the upstream units Links name, nil for one that cannot be read.
	units map[uuid.UUID]*goclientnew.Unit
	// targets caches the names TargetName reads.
	targets map[uuid.UUID]string
	// revisionSources maps a unit to every Revision's MutationSources, oldest first, read
	// only for a unit with an entry that names no Mutation.
	revisionSources map[uuid.UUID][]*goclientnew.RevisionMutationSources
	// sources maps an upstream (unit, revision) to that revision's MutationSources,
	// which is where the walk looks the path up next.
	sources map[string]goclientnew.ResourceMutationList
	// spaces caches what a row needs of a Space: its slug, and whether it has anything to
	// release.
	spaces map[uuid.UUID]spaceInfo
	// users caches username lookups, which are per revision author and repeat heavily.
	users map[string]string
}

// Options says how far a Resolver follows a value upstream.
type Options struct {
	// MaxUpstreamDepth is how many upstream units to follow before giving up; 0 means
	// MaxUpstreamDepth.
	MaxUpstreamDepth int
	// NoUpstream reports each unit's own record without following a merge or a Link to
	// where the value was set.
	NoUpstream bool
}

// NewResolver returns a Resolver reading through client. One Resolver can blame many units,
// and its caches are what make a variant cost little more than its base.
func NewResolver(client *cubapi.Client, opts Options) *Resolver {
	maxDepth := opts.MaxUpstreamDepth
	if maxDepth <= 0 {
		maxDepth = MaxUpstreamDepth
	}
	return &Resolver{
		client:     client,
		maxDepth:   maxDepth,
		noUpstream: opts.NoUpstream,
		mutations:  map[uuid.UUID]map[int64]*goclientnew.ExtendedMutation{},
		revisions:  map[uuid.UUID]map[int64]*goclientnew.ExtendedRevision{},
		sources:    map[string]goclientnew.ResourceMutationList{},

		revisionSources: map[uuid.UUID][]*goclientnew.RevisionMutationSources{},
		units:           map[uuid.UUID]*goclientnew.Unit{},
		targets:         map[uuid.UUID]string{},
		spaces:          map[uuid.UUID]spaceInfo{},
		users:           map[string]string{},
	}
}

// walk builds the chain for one field, starting from the mutation that wrote it in
// this unit and following each merge into the unit it merged from. It stops at a
// change that is not a merge -- the value was set there -- or when it runs out of
// depth, of upstream record, or of that path upstream.
//
// atRevision is the Revision of unit whose record info was read from: the head for the unit
// being blamed, and the Revision a merge took for an upstream one. It bounds the search for
// a change no Mutation records.
func (r *Resolver) walk(ctx context.Context, unit *goclientnew.Unit, info goclientnew.MutationInfo,
	resourceType, resourceName, resourceNameCore, path string, atRevision int64, depth int,
) ([]*Origin, error) {
	origin, err := r.describe(ctx, unit, info.Index)
	if err != nil {
		return nil, err
	}
	if origin == nil && info.Index == 0 {
		mutationNum, revisionNum, err := r.unrecordedCredit(ctx, unit, info, resourceType, resourceName, resourceNameCore, path, atRevision)
		if err != nil {
			return nil, err
		}
		switch {
		case mutationNum != 0:
			if origin, err = r.describe(ctx, unit, mutationNum); err != nil {
				return nil, err
			}
		case revisionNum != 0:
			origin = r.describeUnrecorded(ctx, unit, revisionNum)
		}
	}
	if origin == nil {
		return nil, nil
	}
	chain := []*Origin{origin}

	if r.noUpstream || depth+1 >= r.maxDepth || origin.upstreamUnit == nil {
		return chain, nil
	}

	// A merge took the same field from upstream; a Link's resolve read a field of its own.
	next := lookupTarget{resourceType, resourceName, resourceNameCore, path}
	if origin.upstreamTarget != nil {
		next = *origin.upstreamTarget
	}
	upstreamInfo, err := r.upstreamCredit(ctx, origin, next.resourceType, next.resourceName, next.resourceNameCore, next.path)
	if err != nil || upstreamInfo == nil {
		// An upstream we cannot read -- deleted, or not visible to this user -- is
		// reported as the merge or resolve it is rather than as a failure. The local
		// record is still true and still useful.
		return chain, nil //nolint:nilerr // an unreadable upstream ends the chain at the merge or resolve
	}
	rest, err := r.walk(ctx, origin.upstreamUnit, *upstreamInfo, next.resourceType, next.resourceName, next.resourceNameCore, next.path,
		origin.upstreamRevisionNum, depth+1)
	if err != nil {
		return chain, nil //nolint:nilerr // likewise for a unit further up
	}
	return append(chain, rest...), nil
}

// upstreamCredit reads the upstream unit's MutationSources as of the revision the
// merge took, and returns what wrote the same path there.
func (r *Resolver) upstreamCredit(ctx context.Context, origin *Origin, resourceType, resourceName, resourceNameCore, path string) (
	*goclientnew.MutationInfo, error,
) {
	upstream := origin.upstreamUnit
	extended, err := r.revision(ctx, upstream, origin.upstreamRevisionNum)
	if err != nil || extended == nil {
		return nil, err
	}
	revision := extended.Revision
	key := fmt.Sprintf("%s/%d", upstream.UnitID, revision.RevisionNum)
	sources, cached := r.sources[key]
	if !cached {
		fetched, err := r.revisionMutationSources(ctx, upstream.SpaceID, upstream.UnitID, revision.RevisionID)
		if err != nil {
			return nil, err
		}
		sources = fetched
		r.sources[key] = sources
	}
	identifier, _ := blameResourceProvider(upstream.ToolchainType)
	info, _ := creditPath(identifier, sources, resourceType, resourceName, resourceNameCore, path)
	return info, nil
}

// describe turns one MutationNum in a unit into a displayable hop, and says whether
// the walk continues past it.
func (r *Resolver) describe(ctx context.Context, unit *goclientnew.Unit, mutationNum int64) (*Origin, error) {
	mutations, err := r.unitMutations(ctx, unit)
	if err != nil {
		return nil, err
	}
	em, ok := mutations[mutationNum]
	if !ok || em == nil || em.Mutation == nil {
		return nil, nil
	}
	m := em.Mutation

	spaceSlug, err := r.spaceSlug(ctx, unit.SpaceID)
	if err != nil {
		return nil, err
	}
	origin := &Origin{
		SpaceSlug:   spaceSlug,
		UnitSlug:    unit.Slug,
		RevisionNum: m.RevisionNum,
		MutationNum: m.MutationNum,
		Function:    functionInvocationName(m.FunctionInvocation),
		mutation:    em,
	}
	if em.Link != nil {
		origin.Link = r.qualify(ctx, unit.SpaceID, em.Link.SpaceID, em.Link.Slug)
		origin.linkUpdateType = em.Link.UpdateType
	}
	if em.Trigger != nil {
		origin.Trigger = r.qualify(ctx, unit.SpaceID, em.Trigger.SpaceID, em.Trigger.Slug)
	}
	if em.Invocation != nil {
		origin.Invocation = r.qualify(ctx, unit.SpaceID, em.Invocation.SpaceID, em.Invocation.Slug)
	}
	mergeSource := ""
	if em.MergeSource != nil {
		mergeSource = r.qualify(ctx, unit.SpaceID, em.MergeSource.SpaceID, em.MergeSource.Slug)
	}

	r.describeRevision(ctx, unit, origin)
	origin.SetBy = blameSetBy(origin, mergeSource)

	// The walk continues only where the Mutation's expansion resolved the upstream
	// unit for us. A MergeSourceID with no expansion names a unit this caller cannot
	// read, and reporting the merge is the honest end of the chain.
	//
	// An Upsert is a merge of a render of its upstream, and records its source as one. It is
	// followed as a merge when it copied the upstream's resources as they are. One with a
	// TransformInvocation is not: the function computed what it wrote from the upstream
	// document as a whole, so a resource of the same type and name upstream, if there is one,
	// did not necessarily hold the value, and the walk ends at the Link.
	if m.MergeSourceID != nil && *m.MergeSourceID != uuid.Nil {
		if em.MergeSource != nil && !blameTransformedByLink(em) {
			origin.upstreamUnit = em.MergeSource
			origin.upstreamRevisionNum = m.MergeEndRevisionNum
		}
		return origin, nil
	}

	// A Link's resolve records the upstream Revision it read and no merge source: its
	// upstream unit is the Link's. The walk follows it into that unit as it follows a merge,
	// to the field the resolve read, where there is exactly one.
	if em.Link == nil {
		return origin, nil
	}
	upstream := r.unitByID(ctx, em.Link.ToSpaceID, em.Link.ToUnitID)
	if upstream == nil {
		return origin, nil
	}
	origin.LinkUpstream = r.qualify(ctx, unit.SpaceID, upstream.SpaceID, upstream.Slug)
	origin.LinkUpstreamRevisionNum = m.MergeEndRevisionNum
	if origin.LinkUpstreamRevisionNum == 0 {
		origin.LinkUpstreamRevisionNum = r.legacyResolveRevision(ctx, origin, upstream, m.CreatedAt)
	}
	target, follow := blameLinkTarget(em)
	if !follow || origin.LinkUpstreamRevisionNum == 0 {
		return origin, nil
	}
	if target != nil && target.resourceNameCore == "" {
		target.resourceNameCore = target.resourceName
		if identifier, err := blameResourceProvider(upstream.ToolchainType); err == nil {
			target.resourceNameCore = string(identifier.RemoveScopeFromResourceName(api.ResourceName(target.resourceName)))
		}
	}
	origin.upstreamUnit = upstream
	origin.upstreamRevisionNum = origin.LinkUpstreamRevisionNum
	origin.upstreamTarget = target
	return origin, nil
}

// blameResolveDescription matches the change description a Link's resolve writes, which names
// the upstream Revision it read: "Transformed from revision 73 of unit registry-facts/ui".
var blameResolveDescription = regexp.MustCompile(`(?:Transformed|Inserted|Upserted) from revision (\d+) of unit ([^/\s]+)/(\S+)`)

// legacyResolveRevision finds the upstream Revision a Link's resolve read, for a Mutation
// written before resolves recorded it in MergeEndRevisionNum. Two places say:
//
//   - The change description a TransformPaths, Insert or Upsert resolve writes names it, and is
//     the better answer where it is there: a promotion resolves against the upstream Revision
//     the ChangeOrder started from, not the head. It is taken only when it names the Link's
//     upstream unit, since a caller's own description replaces it.
//   - Otherwise a resolve read the upstream unit's head when it ran, which is the newest of
//     its Revisions created by the time the Mutation was. That covers needs/provides, which
//     writes no description.
//
// 0 when neither finds one.
func (r *Resolver) legacyResolveRevision(ctx context.Context, origin *Origin, upstream *goclientnew.Unit, mutationCreatedAt time.Time) int64 {
	if match := blameResolveDescription.FindStringSubmatch(origin.Description); match != nil &&
		match[2] == r.space(ctx, upstream.SpaceID).slug && match[3] == upstream.Slug {
		if revisionNum, err := strconv.ParseInt(match[1], 10, 64); err == nil {
			return revisionNum
		}
	}
	if mutationCreatedAt.IsZero() {
		return 0
	}
	revisions, err := r.unitRevisions(ctx, upstream)
	if err != nil {
		return 0
	}
	var revisionNum int64
	for num, extended := range revisions {
		if extended.Revision != nil && !extended.Revision.CreatedAt.After(mutationCreatedAt) && num > revisionNum {
			revisionNum = num
		}
	}
	return revisionNum
}

// blameLinkTarget says whether the walk can follow a Link's resolve into the Link's upstream
// unit, and to which field there: target, or the same field when target is nil.
//
// A needs/provides resolve records the resource and path it read on each Mutation, one
// Mutation per value. A TransformPaths Link names what it reads in its UpstreamPaths, and the
// value it wrote came from that field only when it reads exactly one: with several, or with
// an UpstreamGetter, the value was computed from more than one field, and the walk ends at
// the Link. The Link is read as it is now, so a Link whose UpstreamPaths were edited since is
// followed to the field it reads today. An Insert reads a whole document, and has no field
// to follow.
//
// An Upsert records a merge source, and describe follows it as a merge before this is asked.
// One written before Upserts were merges recorded no source, and is followed here instead:
// to the same field when it copied the upstream's resources as they are, and not at all when
// it ran a TransformInvocation, which reads the whole document.
func blameLinkTarget(em *goclientnew.ExtendedMutation) (target *lookupTarget, follow bool) {
	m := em.Mutation
	if m.ProvidedPath != "" && m.ProvidedResource != nil && m.ProvidedResource.ResourceName != "" {
		return &lookupTarget{
			resourceType: m.ProvidedResource.ResourceType,
			resourceName: m.ProvidedResource.ResourceName,
			path:         m.ProvidedPath,
		}, true
	}
	link := em.Link
	switch link.UpdateType {
	case blameUpsertLink:
		return nil, !blameTransformedByLink(em)
	case blameTransformPathsLink:
		if len(link.UpstreamPaths) != 1 || len(link.UpstreamGetters) != 0 {
			return nil, false
		}
		upstreamPath := link.UpstreamPaths[0]
		if upstreamPath.Resource == nil || upstreamPath.Path == "" {
			return nil, false
		}
		return &lookupTarget{
			resourceType:     upstreamPath.Resource.ResourceType,
			resourceName:     upstreamPath.Resource.ResourceName,
			resourceNameCore: upstreamPath.Resource.ResourceNameWithoutScope,
			path:             upstreamPath.Path,
		}, true
	}
	return nil, false
}

// blameTransformedByLink reports whether the Link behind a Mutation ran its upstream data
// through a TransformInvocation, as an Upsert or Insert Link may.
func blameTransformedByLink(em *goclientnew.ExtendedMutation) bool {
	return em.Link != nil && em.Link.TransformInvocationID != nil && *em.Link.TransformInvocationID != uuid.Nil
}

// The Link UpdateTypes the walk follows other than by a merge source: a TransformPaths Link
// names what it reads in its UpstreamPaths, and an Upsert from before Upserts recorded one
// may have copied the upstream's resources.
const (
	blameTransformPathsLink = "TransformPaths"
	blameUpsertLink         = "Upsert"
)

// unitByID reads a unit a Link names, or nil when it cannot be read.
func (r *Resolver) unitByID(ctx context.Context, spaceID, unitID uuid.UUID) *goclientnew.Unit {
	if unit, ok := r.units[unitID]; ok {
		return unit
	}
	var unit *goclientnew.Unit
	if extended, err := cubapi.ResolveUnit(ctx, r.client, cubapi.ParseRef(unitID.String()),
		cubapi.ResolveOpts{Space: spaceID, Select: "UnitID,SpaceID,Slug,SpaceSlug,ToolchainType"}); err == nil && extended != nil {
		unit = extended.Unit
	}
	r.units[unitID] = unit
	return unit
}

// describeRevision fills in what a hop's Revision says: the operation that made it, who
// made it and when, and what it belongs to.
func (r *Resolver) describeRevision(ctx context.Context, unit *goclientnew.Unit, origin *Origin) {
	extended, err := r.revision(ctx, unit, origin.RevisionNum)
	if err != nil || extended == nil {
		return
	}
	revision := extended.Revision
	origin.revision = extended
	origin.Source = revision.Source
	origin.Description = revision.Description
	origin.When = revision.CreatedAt
	origin.User = r.username(ctx, revision.UserID)
	for _, changeOrder := range extended.ChangeOrders {
		origin.ChangeOrders = append(origin.ChangeOrders, r.qualify(ctx, unit.SpaceID, changeOrder.SpaceID, changeOrder.Slug))
	}
	// A Release is in the Space of the units it bundles, so its number alone names it.
	for _, release := range extended.Releases {
		origin.Releases = append(origin.Releases, fmt.Sprintf("%d", release.ReleaseNum))
	}
	for _, tag := range extended.Tags {
		origin.Tags = append(origin.Tags, r.qualify(ctx, unit.SpaceID, tag.SpaceID, tag.Slug))
	}
	// The API returns a Revision's associations in no particular order; sorted, the same
	// Revision reads the same way every time.
	sort.Strings(origin.ChangeOrders)
	sort.Strings(origin.Tags)
	sort.Slice(origin.Releases, func(i, j int) bool {
		a, _ := strconv.ParseInt(origin.Releases[i], 10, 64)
		b, _ := strconv.ParseInt(origin.Releases[j], 10, 64)
		return a < b
	})
}

// describeUnrecorded is describe for a change that has a Revision and no Mutation: an
// upload to an existing unit before uploads recorded one wrote a MergeExternal Revision
// alone, and the paths it changed carry an entry naming Mutation 0. The Revision still
// says what made it, who, and when, and that is the answer. It has no merge source, so
// the walk ends here.
func (r *Resolver) describeUnrecorded(ctx context.Context, unit *goclientnew.Unit, revisionNum int64) *Origin {
	origin := &Origin{
		SpaceSlug:   r.space(ctx, unit.SpaceID).slug,
		UnitSlug:    unit.Slug,
		RevisionNum: revisionNum,
	}
	r.describeRevision(ctx, unit, origin)
	origin.SetBy = blameSetBy(origin, "")
	return origin
}

// unrecordedCredit finds what made a change whose entry names no Mutation, reading the
// unit's record Revision by Revision up to atRevision. Two things leave such an entry:
//
//   - A change that recorded no Mutation, as an upload to an existing unit did before
//     uploads recorded one. The value is new in the entry, and the Revision it first
//     appears in is the answer: revisionNum.
//   - A write of the record that dropped the Mutation of an entry it did not change. The
//     value is the one the entry already had, so the change that set it is still the
//     answer: the Mutation the entry named before, mutationNum, or the Revision that
//     entry was itself credited to.
//
// Both are 0 when nothing is found. Each Revision's record is read, which is the cost of an
// entry that does not say where it came from; it is paid once per unit, and only for a unit
// that has such an entry.
func (r *Resolver) unrecordedCredit(ctx context.Context, unit *goclientnew.Unit, info goclientnew.MutationInfo,
	resourceType, resourceName, resourceNameCore, path string, atRevision int64) (mutationNum, revisionNum int64, err error) {
	revisions, err := r.allRevisionSources(ctx, unit)
	if err != nil {
		return 0, 0, err
	}
	identifier, _ := blameResourceProvider(unit.ToolchainType)
	var previous *goclientnew.MutationInfo
	for _, revision := range revisions {
		if atRevision > 0 && revision.RevisionNum > atRevision {
			break
		}
		var entry *goclientnew.MutationInfo
		if revision.MutationSources != nil {
			entry, _ = creditPath(identifier, *revision.MutationSources, resourceType, resourceName, resourceNameCore, path)
		}
		switch {
		case entry == nil:
			mutationNum, revisionNum = 0, 0
		case entry.Index != 0:
			mutationNum, revisionNum = entry.Index, 0
		case !sameBlameEntryValue(previous, entry):
			mutationNum, revisionNum = 0, revision.RevisionNum
		}
		previous = entry
	}
	if previous == nil || previous.Index != 0 || !sameBlameEntryValue(previous, &info) {
		// The record at atRevision is not the entry being explained, so what was found is
		// the credit of something else.
		return 0, 0, nil
	}
	return mutationNum, revisionNum, nil
}

// sameBlameEntryValue reports whether two entries for a path record the same change of
// value: the same type and the same value.
func sameBlameEntryValue(a, b *goclientnew.MutationInfo) bool {
	return a != nil && b != nil && a.Value == b.Value && blameMutationType(a) == blameMutationType(b)
}

// allRevisionSources returns every Revision's record of a unit, oldest first.
func (r *Resolver) allRevisionSources(ctx context.Context, unit *goclientnew.Unit) ([]*goclientnew.RevisionMutationSources, error) {
	if revisions, ok := r.revisionSources[unit.UnitID]; ok {
		return revisions, nil
	}
	revisions, err := r.searchRevisionMutationSources(ctx, unit)
	if err != nil {
		return nil, err
	}
	sort.Slice(revisions, func(i, j int) bool { return revisions[i].RevisionNum < revisions[j].RevisionNum })
	r.revisionSources[unit.UnitID] = revisions
	return revisions, nil
}

func blameMutationType(info *goclientnew.MutationInfo) string {
	if info.MutationType == nil {
		return ""
	}
	return string(*info.MutationType)
}

// blameSetBy names what decided to make a change, most specific first. A Link or a
// Trigger is the answer when there is one: it chose the function it ran, so the function
// is how the change was made rather than why, and a TransformPaths Link that set an image
// reads as that Link, not as set-image-reference-by-uri. A function run directly is next;
// a merge with none is named by the external source the change description carries, which
// is how "MergeExternal; from helm template ..." becomes "helm template ...". What is left
// -- an update that wrote the data it was given, a restore -- is named by the operation
// that made the Revision, its Source.
//
// The names are the hop's own, already qualified with their Space where it is not the
// unit's; mergeSource is the unit a merge took from, named the same way.
func blameSetBy(origin *Origin, mergeSource string) string {
	if origin.Link != "" {
		return "link " + origin.Link
	}
	if origin.Trigger != "" {
		return "trigger " + origin.Trigger
	}
	if origin.Function != "" {
		return origin.Function
	}
	if source := externalSourceFromDescription(origin.Description); source != "" {
		return source
	}
	if origin.Invocation != "" {
		return "invocation " + origin.Invocation
	}
	if mergeSource != "" {
		return "merge from " + mergeSource
	}
	return origin.Source
}

// externalSourceFromDescription pulls the source out of the change description an
// external merge writes ("MergeExternal; from <source>"). That text is the only
// place the source is recorded, and it is what makes a chart nameable in a blame
// row -- so it is read back rather than reconstructed.
func externalSourceFromDescription(description string) string {
	const marker = "; from "
	if !strings.HasPrefix(description, "MergeExternal") {
		return ""
	}
	i := strings.Index(description, marker)
	if i < 0 {
		return ""
	}
	source := strings.TrimSpace(description[i+len(marker):])
	// The description may carry the caller's own text after a "|" separator.
	if j := strings.Index(source, "|"); j >= 0 {
		source = strings.TrimSpace(source[:j])
	}
	return source
}

func (r *Resolver) unitMutations(ctx context.Context, unit *goclientnew.Unit) (map[int64]*goclientnew.ExtendedMutation, error) {
	if cached, ok := r.mutations[unit.UnitID]; ok {
		return cached, nil
	}
	list, err := r.listMutations(ctx, unit)
	if err != nil {
		return nil, err
	}
	byNum := make(map[int64]*goclientnew.ExtendedMutation, len(list))
	for _, em := range list {
		if em != nil && em.Mutation != nil {
			byNum[em.Mutation.MutationNum] = em
		}
	}
	r.mutations[unit.UnitID] = byNum
	return byNum, nil
}

func (r *Resolver) revision(ctx context.Context, unit *goclientnew.Unit, revisionNum int64) (*goclientnew.ExtendedRevision, error) {
	if revisionNum == 0 {
		return nil, nil
	}
	byNum, err := r.unitRevisions(ctx, unit)
	if err != nil {
		return nil, err
	}
	return byNum[revisionNum], nil
}

// unitRevisions returns every Revision of a unit by number, read once.
func (r *Resolver) unitRevisions(ctx context.Context, unit *goclientnew.Unit) (map[int64]*goclientnew.ExtendedRevision, error) {
	if byNum, ok := r.revisions[unit.UnitID]; ok {
		return byNum, nil
	}
	list, err := r.listRevisions(ctx, unit)
	if err != nil {
		return nil, err
	}
	byNum := make(map[int64]*goclientnew.ExtendedRevision, len(list))
	for _, er := range list {
		if er != nil && er.Revision != nil {
			byNum[er.Revision.RevisionNum] = er
		}
	}
	r.revisions[unit.UnitID] = byNum
	return byNum, nil
}

// spaceInfo is what a blame row needs to know of a Space.
type spaceInfo struct {
	slug string
	// releasable is whether the Space has a release Target. One with none has nothing to
	// release, so none of its values is reported unreleased.
	releasable bool
}

func (r *Resolver) space(ctx context.Context, spaceID uuid.UUID) spaceInfo {
	if cached, ok := r.spaces[spaceID]; ok {
		return cached
	}
	info := spaceInfo{slug: spaceID.String()}
	space, err := cubapi.ResolveSpace(ctx, r.client, cubapi.ParseRef(spaceID.String()),
		cubapi.ResolveOpts{Select: "SpaceID,Slug,ReleaseTargetID"})
	if err == nil && space != nil && space.Space != nil {
		if space.Space.Slug != "" {
			info.slug = space.Space.Slug
		}
		info.releasable = space.Space.ReleaseTargetID != nil && *space.Space.ReleaseTargetID != uuid.Nil
	}
	r.spaces[spaceID] = info
	return info
}

func (r *Resolver) spaceSlug(ctx context.Context, spaceID uuid.UUID) (string, error) {
	return r.space(ctx, spaceID).slug, nil
}

// qualify names an entity the way the CLI takes it from the unit's Space: its slug alone when
// it is in the unit's Space, and space/slug when it is in another, as a ChangeOrder promoting
// a variant and the Tags it marks the Revision with usually are.
func (r *Resolver) qualify(ctx context.Context, unitSpaceID, entitySpaceID uuid.UUID, slug string) string {
	if entitySpaceID == uuid.Nil || entitySpaceID == unitSpaceID {
		return slug
	}
	return r.space(ctx, entitySpaceID).slug + "/" + slug
}

func (r *Resolver) username(ctx context.Context, userID goclientnew.UUID) string {
	key := userID.String()
	if key == uuid.Nil.String() {
		// Triggers, links, and resolve run as nobody. Saying so is more honest than
		// printing a zero UUID.
		return "automation"
	}
	if cached, ok := r.users[key]; ok {
		return cached
	}
	name := key
	if username, ok := r.usernames(ctx, []goclientnew.UUID{userID})[userID]; ok {
		name = username
	}
	r.users[key] = name
	return name
}
