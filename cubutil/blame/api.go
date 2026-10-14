// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package blame

import (
	"context"
	"fmt"
	"math"
	"net/http"

	"github.com/confighub/sdk/core/cubapi"
	goclientnew "github.com/confighub/sdk/core/openapi/goclient-new"
	"github.com/google/uuid"
)

// What a Resolver reads through the API. Each is one request, or one per page, for a whole
// unit or a whole selection, never one per field: the caches in Resolver are keyed on what
// these return.

// mutationInclude and revisionInclude expand what a hop names, so describing one costs no
// further request: the Link, Trigger, Invocation and merge source of a Mutation, and the
// ChangeSet, ChangeOrders, Releases and Tags of a Revision.
const (
	mutationInclude = "SpaceID,RevisionID,MergeSourceID,LinkID,TriggerID,InvocationID"
	revisionInclude = "UserID,UnitID,ChangeSetID,Tags,ChangeOrders,Releases"
)

func (r *Resolver) listMutations(ctx context.Context, unit *goclientnew.Unit) ([]*goclientnew.ExtendedMutation, error) {
	include := mutationInclude
	params := &goclientnew.ListExtendedMutationsParams{Include: &include}
	return cubapi.ReadPages(cubapi.ListOpts{}, func(limit *int, token *string) (*http.Response, *[]goclientnew.ExtendedMutation, error) {
		params.Limit, params.Continue = limit, token
		res, err := r.client.API.ListExtendedMutationsWithResponse(ctx, unit.SpaceID, unit.UnitID, params)
		if cubapi.IsAPIError(err, res) {
			return nil, nil, cubapi.InterpretErrorGeneric(err, res)
		}
		return res.HTTPResponse, res.JSON200, nil
	})
}

func (r *Resolver) listRevisions(ctx context.Context, unit *goclientnew.Unit) ([]*goclientnew.ExtendedRevision, error) {
	include := revisionInclude
	params := &goclientnew.ListExtendedRevisionsParams{Include: &include}
	return cubapi.ReadPages(cubapi.ListOpts{}, func(limit *int, token *string) (*http.Response, *[]goclientnew.ExtendedRevision, error) {
		params.Limit, params.Continue = limit, token
		res, err := r.client.API.ListExtendedRevisionsWithResponse(ctx, unit.SpaceID, unit.UnitID, params)
		if cubapi.IsAPIError(err, res) {
			return nil, nil, cubapi.InterpretErrorGeneric(err, res)
		}
		return res.HTTPResponse, res.JSON200, nil
	})
}

// unitMutationSources reads what set each value in a unit's configuration, which is not a
// field of the Unit and has its own endpoint.
func (r *Resolver) unitMutationSources(ctx context.Context, unit *goclientnew.Unit) (goclientnew.ResourceMutationList, error) {
	res, err := r.client.API.GetUnitMutationSourcesWithResponse(ctx, unit.SpaceID, unit.UnitID)
	if cubapi.IsAPIError(err, res) {
		return nil, cubapi.InterpretErrorGeneric(err, res)
	}
	if res.JSON200 == nil || res.JSON200.MutationSources == nil {
		return nil, nil
	}
	return *res.JSON200.MutationSources, nil
}

// revisionMutationSources is unitMutationSources as of one Revision.
func (r *Resolver) revisionMutationSources(ctx context.Context, spaceID, unitID, revisionID uuid.UUID) (goclientnew.ResourceMutationList, error) {
	res, err := r.client.API.GetRevisionMutationSourcesWithResponse(ctx, spaceID, unitID, revisionID)
	if cubapi.IsAPIError(err, res) {
		return nil, cubapi.InterpretErrorGeneric(err, res)
	}
	if res.JSON200 == nil || res.JSON200.MutationSources == nil {
		return nil, nil
	}
	return *res.JSON200.MutationSources, nil
}

// searchRevisionMutationSources reads the record of every Revision of a unit, in one request
// per thousand Revisions rather than one per Revision.
func (r *Resolver) searchRevisionMutationSources(ctx context.Context, unit *goclientnew.Unit) ([]*goclientnew.RevisionMutationSources, error) {
	whereClause := fmt.Sprintf("UnitID = '%s'", unit.UnitID)
	distinctOff := goclientnew.SearchRevisionMutationSourcesParamsDistinctOnOff
	params := &goclientnew.SearchRevisionMutationSourcesParams{Where: &whereClause, DistinctOn: &distinctOff}
	// Every Revision: the endpoint refuses DistinctOn Off without a limit, and ReadPages pages
	// up to this one a thousand at a time.
	return cubapi.ReadPages(cubapi.ListOpts{Limit: math.MaxInt32}, func(limit *int, token *string) (*http.Response, *[]goclientnew.RevisionMutationSources, error) {
		params.Limit, params.Continue = limit, token
		res, err := r.client.API.SearchRevisionMutationSourcesWithResponse(ctx, params)
		if cubapi.IsAPIError(err, res) {
			return nil, nil, cubapi.InterpretErrorGeneric(err, res)
		}
		return res.HTTPResponse, res.JSON200, nil
	})
}

// searchUnitMutationSources reads the record of every unit a where clause selects, keyed by
// unit ID, in one request.
func (r *Resolver) searchUnitMutationSources(ctx context.Context, whereClause string) (map[uuid.UUID]goclientnew.ResourceMutationList, error) {
	params := &goclientnew.SearchUnitMutationSourcesParams{Where: &whereClause}
	res, err := r.client.API.SearchUnitMutationSourcesWithResponse(ctx, params)
	if cubapi.IsAPIError(err, res) {
		return nil, cubapi.InterpretErrorGeneric(err, res)
	}
	byUnit := map[uuid.UUID]goclientnew.ResourceMutationList{}
	if res.JSON200 != nil {
		for _, row := range *res.JSON200 {
			if row.MutationSources != nil {
				byUnit[row.UnitID] = *row.MutationSources
			}
		}
	}
	return byUnit, nil
}

// searchUnitData reads the configuration of every unit a where clause selects, keyed by unit
// ID, in one request.
func (r *Resolver) searchUnitData(ctx context.Context, whereClause string) (map[uuid.UUID]string, error) {
	params := &goclientnew.SearchUnitDataParams{Where: &whereClause}
	res, err := r.client.API.SearchUnitDataWithResponse(ctx, params)
	if cubapi.IsAPIError(err, res) {
		return nil, cubapi.InterpretErrorGeneric(err, res)
	}
	byUnit := map[uuid.UUID]string{}
	if res.JSON200 != nil {
		for _, row := range *res.JSON200 {
			byUnit[row.UnitID] = row.Data
		}
	}
	return byUnit, nil
}

// usernames names users by ID, leaving the ID for one that cannot be read.
func (r *Resolver) usernames(ctx context.Context, userIDs []goclientnew.UUID) map[goclientnew.UUID]string {
	names := map[goclientnew.UUID]string{}
	if len(userIDs) == 0 {
		return names
	}
	users, err := cubapi.ListUsers(ctx, r.client, cubapi.Where{}.In("UserID", userIDs), cubapi.ListOpts{Select: "UserID,Username"})
	if err != nil {
		return names
	}
	for _, user := range users {
		if user.User != nil && user.User.Username != "" {
			names[user.User.UserID] = user.User.Username
		}
	}
	return names
}
