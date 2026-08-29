// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"fmt"
	"strings"

	goclientnew "github.com/confighub/sdk/core/openapi/goclient-new"
	"github.com/google/uuid"
)

// blameResolver turns a MutationSources entry into the chain of changes that put a
// value where it is, following each merge into the unit it merged from.
//
// Everything it reads is per (unit, revision) rather than per field, and a unit has
// far fewer distinct revisions than fields, so the caches are what make blaming a
// hundred-field resource a handful of requests instead of a hundred.
type blameResolver struct {
	maxDepth int

	// mutations maps a unit to its MutationNum -> Mutation. The whole set is read at
	// once: a resource's fields are credited to a handful of mutations, and asking
	// for them one at a time costs a request each.
	mutations map[uuid.UUID]map[int64]*goclientnew.ExtendedMutation
	// revisions maps a unit to its RevisionNum -> Revision, read the same way.
	revisions map[uuid.UUID]map[int64]*goclientnew.Revision
	// sources maps an upstream (unit, revision) to that revision's MutationSources,
	// which is where the walk looks the path up next.
	sources map[string]goclientnew.ResourceMutationList
	// spaces caches the slug a row displays.
	spaces map[uuid.UUID]string
	// users caches username lookups, which are per revision author and repeat heavily.
	users map[string]string
}

func newBlameResolver(maxDepth int) *blameResolver {
	if maxDepth <= 0 {
		maxDepth = blameMaxUpstreamDepth
	}
	return &blameResolver{
		maxDepth:  maxDepth,
		mutations: map[uuid.UUID]map[int64]*goclientnew.ExtendedMutation{},
		revisions: map[uuid.UUID]map[int64]*goclientnew.Revision{},
		sources:   map[string]goclientnew.ResourceMutationList{},
		spaces:    map[uuid.UUID]string{},
		users:     map[string]string{},
	}
}

// walk builds the chain for one field, starting from the mutation that wrote it in
// this unit and following each merge into the unit it merged from. It stops at a
// change that is not a merge -- the value was set there -- or when it runs out of
// depth, of upstream record, or of that path upstream.
func (r *blameResolver) walk(unit *goclientnew.Unit, info goclientnew.MutationInfo,
	resourceType, resourceName, resourceNameCore, path string, depth int,
) ([]*blameOrigin, error) {
	origin, err := r.describe(unit, info.Index)
	if err != nil {
		return nil, err
	}
	if origin == nil {
		return nil, nil
	}
	chain := []*blameOrigin{origin}

	if unitBlameArgs.noUpstream || depth+1 >= r.maxDepth || origin.upstreamUnit == nil {
		return chain, nil
	}

	upstreamInfo, err := r.upstreamCredit(origin, resourceType, resourceName, resourceNameCore, path)
	if err != nil || upstreamInfo == nil {
		// An upstream we cannot read -- deleted, or not visible to this user -- is
		// reported as the merge it is rather than as a failure. The local record is
		// still true and still useful.
		return chain, nil
	}
	rest, err := r.walk(origin.upstreamUnit, *upstreamInfo, resourceType, resourceName, resourceNameCore, path, depth+1)
	if err != nil {
		return chain, nil
	}
	return append(chain, rest...), nil
}

// upstreamCredit reads the upstream unit's MutationSources as of the revision the
// merge took, and returns what wrote the same path there.
func (r *blameResolver) upstreamCredit(origin *blameOrigin, resourceType, resourceName, resourceNameCore, path string) (
	*goclientnew.MutationInfo, error,
) {
	upstream := origin.upstreamUnit
	revision, err := r.revision(upstream, origin.upstreamRevisionNum)
	if err != nil || revision == nil {
		return nil, err
	}
	key := fmt.Sprintf("%s/%d", upstream.UnitID, revision.RevisionNum)
	sources, cached := r.sources[key]
	if !cached {
		fetched, err := fetchRevisionMutationSources(upstream.SpaceID, upstream.UnitID, revision.RevisionID)
		if err != nil {
			return nil, err
		}
		if fetched != nil {
			sources = *fetched
		}
		r.sources[key] = sources
	}
	info, _ := creditPath(sources, resourceType, resourceName, resourceNameCore, path)
	return info, nil
}

// describe turns one MutationNum in a unit into a displayable hop, and says whether
// the walk continues past it.
func (r *blameResolver) describe(unit *goclientnew.Unit, mutationNum int64) (*blameOrigin, error) {
	mutations, err := r.unitMutations(unit)
	if err != nil {
		return nil, err
	}
	em, ok := mutations[mutationNum]
	if !ok || em == nil || em.Mutation == nil {
		return nil, nil
	}
	m := em.Mutation

	spaceSlug, err := r.spaceSlug(unit.SpaceID)
	if err != nil {
		return nil, err
	}
	origin := &blameOrigin{
		SpaceSlug:   spaceSlug,
		UnitSlug:    unit.Slug,
		RevisionNum: m.RevisionNum,
		MutationNum: m.MutationNum,
	}

	if revision, err := r.revision(unit, m.RevisionNum); err == nil && revision != nil {
		origin.Description = revision.Description
		origin.When = revision.CreatedAt
		origin.User = r.username(revision.UserID)
	}
	origin.SetBy = blameSetBy(em, origin.Description)

	// The walk continues only where the Mutation's expansion resolved the upstream
	// unit for us. A MergeSourceID with no expansion names a unit this caller cannot
	// read, and reporting the merge is the honest end of the chain.
	if m.MergeSourceID != nil && *m.MergeSourceID != uuid.Nil && em.MergeSource != nil {
		origin.upstreamUnit = em.MergeSource
		origin.upstreamRevisionNum = m.MergeEndRevisionNum
	}
	return origin, nil
}

// blameSetBy names the operation behind a mutation, most specific first. A function
// name is the best answer; a merge with none is named by the external source the
// change description carries, which is how "MergeExternal; from helm template ..."
// becomes "helm template ...".
func blameSetBy(em *goclientnew.ExtendedMutation, description string) string {
	m := em.Mutation
	if m.FunctionInvocation.FunctionName != "" {
		return m.FunctionInvocation.FunctionName
	}
	if source := externalSourceFromDescription(description); source != "" {
		return source
	}
	if em.Link != nil {
		return "link " + em.Link.Slug
	}
	if em.Trigger != nil {
		return "trigger " + em.Trigger.Slug
	}
	if em.Invocation != nil {
		return "invocation " + em.Invocation.Slug
	}
	if em.MergeSource != nil {
		return "merge from " + em.MergeSource.Slug
	}
	return ""
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

func (r *blameResolver) unitMutations(unit *goclientnew.Unit) (map[int64]*goclientnew.ExtendedMutation, error) {
	if cached, ok := r.mutations[unit.UnitID]; ok {
		return cached, nil
	}
	list, err := apiListMutations(unit.SpaceID.String(), unit.UnitID.String(), "", "*", "")
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

func (r *blameResolver) revision(unit *goclientnew.Unit, revisionNum int64) (*goclientnew.Revision, error) {
	if revisionNum == 0 {
		return nil, nil
	}
	byNum, ok := r.revisions[unit.UnitID]
	if !ok {
		list, err := apiListRevisions(unit.SpaceID.String(), unit.UnitID.String(), "", "*", "")
		if err != nil {
			return nil, err
		}
		byNum = make(map[int64]*goclientnew.Revision, len(list))
		for _, er := range list {
			if er != nil && er.Revision != nil {
				byNum[er.Revision.RevisionNum] = er.Revision
			}
		}
		r.revisions[unit.UnitID] = byNum
	}
	return byNum[revisionNum], nil
}

func (r *blameResolver) spaceSlug(spaceID uuid.UUID) (string, error) {
	if cached, ok := r.spaces[spaceID]; ok {
		return cached, nil
	}
	space, err := apiGetSpace(spaceID.String(), "SpaceID,Slug")
	slug := spaceID.String()
	if err == nil && space != nil && space.Slug != "" {
		slug = space.Slug
	}
	r.spaces[spaceID] = slug
	return slug, nil
}

func (r *blameResolver) username(userID goclientnew.UUID) string {
	key := userID.String()
	if key == uuid.Nil.String() {
		// Triggers, links, and resolve run as nobody. Saying so is more honest than
		// printing a zero UUID.
		return "automation"
	}
	if cached, ok := r.users[key]; ok {
		return cached
	}
	names := resolveUsernames([]goclientnew.UUID{userID})
	name := key
	if len(names) > 0 && names[0] != "" {
		name = names[0]
	}
	r.users[key] = name
	return name
}
