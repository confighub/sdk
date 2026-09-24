// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"github.com/confighub/sdk/core/cubapi"
	goclientnew "github.com/confighub/sdk/core/openapi/goclient-new"
)

// The move commands of the entity types other than unit; see addEntityMoveCommand.
func init() {
	addEntityMoveCommand(entityMove{
		parent:         filterCmd,
		entity:         "filter",
		plural:         "filters",
		identifierFlag: "filter-entity",
		about:          "Spaces, views and triggers that use a filter keep using it.",
		buildWhere:     buildWhereClauseFromFilters,
		move: func(params entityMoveParams, body goclientnew.MoveRequest) (int, *[]goclientnew.MoveResponse, *[]goclientnew.MoveResponse, error) {
			resp, err := cubClientNew.BulkMoveFiltersWithResponse(ctx, &goclientnew.BulkMoveFiltersParams{
				Where: &params.where, Filter: params.filter, DryRun: params.dryRun}, body)
			if cubapi.IsAPIError(err, resp) {
				return 0, nil, nil, cubapi.InterpretErrorGeneric(err, resp)
			}
			return resp.StatusCode(), resp.JSON200, resp.JSON207, nil
		},
	})
	addEntityMoveCommand(entityMove{
		parent:         viewCmd,
		entity:         "view",
		plural:         "views",
		identifierFlag: "view",
		about:          "",
		buildWhere:     buildWhereClauseFromViews,
		move: func(params entityMoveParams, body goclientnew.MoveRequest) (int, *[]goclientnew.MoveResponse, *[]goclientnew.MoveResponse, error) {
			resp, err := cubClientNew.BulkMoveViewsWithResponse(ctx, &goclientnew.BulkMoveViewsParams{
				Where: &params.where, Filter: params.filter, DryRun: params.dryRun}, body)
			if cubapi.IsAPIError(err, resp) {
				return 0, nil, nil, cubapi.InterpretErrorGeneric(err, resp)
			}
			return resp.StatusCode(), resp.JSON200, resp.JSON207, nil
		},
	})
	addEntityMoveCommand(entityMove{
		parent:         tagCmd,
		entity:         "tag",
		plural:         "tags",
		identifierFlag: "tag",
		about:          "The revisions a tag marks keep their marks. A tag that a changeset, changeorder or\nrelease made for itself stays with its owner; move a changeset to move its tags.",
		buildWhere:     buildWhereClauseFromTags,
		move: func(params entityMoveParams, body goclientnew.MoveRequest) (int, *[]goclientnew.MoveResponse, *[]goclientnew.MoveResponse, error) {
			resp, err := cubClientNew.BulkMoveTagsWithResponse(ctx, &goclientnew.BulkMoveTagsParams{
				Where: &params.where, Filter: params.filter, DryRun: params.dryRun}, body)
			if cubapi.IsAPIError(err, resp) {
				return 0, nil, nil, cubapi.InterpretErrorGeneric(err, resp)
			}
			return resp.StatusCode(), resp.JSON200, resp.JSON207, nil
		},
	})
	addEntityMoveCommand(entityMove{
		parent:         changesetCmd,
		entity:         "changeset",
		plural:         "changesets",
		identifierFlag: "changeset",
		about:          "A changeset's start and end tags move with it. Its revisions and units stay where they are.",
		buildWhere:     buildWhereClauseFromChangeSets,
		move: func(params entityMoveParams, body goclientnew.MoveRequest) (int, *[]goclientnew.MoveResponse, *[]goclientnew.MoveResponse, error) {
			resp, err := cubClientNew.BulkMoveChangeSetsWithResponse(ctx, &goclientnew.BulkMoveChangeSetsParams{
				Where: &params.where, Filter: params.filter, DryRun: params.dryRun}, body)
			if cubapi.IsAPIError(err, resp) {
				return 0, nil, nil, cubapi.InterpretErrorGeneric(err, resp)
			}
			return resp.StatusCode(), resp.JSON200, resp.JSON207, nil
		},
	})
	addEntityMoveCommand(entityMove{
		parent:         changeworkflowCmd,
		entity:         "changeworkflow",
		plural:         "changeworkflows",
		identifierFlag: "changeworkflow",
		about:          "",
		buildWhere:     buildWhereClauseFromChangeWorkflows,
		move: func(params entityMoveParams, body goclientnew.MoveRequest) (int, *[]goclientnew.MoveResponse, *[]goclientnew.MoveResponse, error) {
			resp, err := cubClientNew.BulkMoveChangeWorkflowsWithResponse(ctx, &goclientnew.BulkMoveChangeWorkflowsParams{
				Where: &params.where, Filter: params.filter, DryRun: params.dryRun}, body)
			if cubapi.IsAPIError(err, resp) {
				return 0, nil, nil, cubapi.InterpretErrorGeneric(err, resp)
			}
			return resp.StatusCode(), resp.JSON200, resp.JSON207, nil
		},
	})
	addEntityMoveCommand(entityMove{
		parent:         targetCmd,
		entity:         "target",
		plural:         "targets",
		identifierFlag: "target",
		about:          "Units and spaces that use a target keep using it, and releases published through it\nstay in the spaces that published them.",
		buildWhere:     buildWhereClauseFromTargets,
		move: func(params entityMoveParams, body goclientnew.MoveRequest) (int, *[]goclientnew.MoveResponse, *[]goclientnew.MoveResponse, error) {
			resp, err := cubClientNew.BulkMoveTargetsWithResponse(ctx, &goclientnew.BulkMoveTargetsParams{
				Where: &params.where, Filter: params.filter, DryRun: params.dryRun}, body)
			if cubapi.IsAPIError(err, resp) {
				return 0, nil, nil, cubapi.InterpretErrorGeneric(err, resp)
			}
			return resp.StatusCode(), resp.JSON200, resp.JSON207, nil
		},
	})
	addEntityMoveCommand(entityMove{
		parent:         invocationCmd,
		entity:         "invocation",
		plural:         "invocations",
		identifierFlag: "invocation",
		about:          "An invocation's built-in functions run on its space's executor, so a move to a space\nwith different attributes re-validates them there, and is refused while a trigger runs\nthe invocation.",
		buildWhere:     buildWhereClauseFromInvocations,
		move: func(params entityMoveParams, body goclientnew.MoveRequest) (int, *[]goclientnew.MoveResponse, *[]goclientnew.MoveResponse, error) {
			resp, err := cubClientNew.BulkMoveInvocationsWithResponse(ctx, &goclientnew.BulkMoveInvocationsParams{
				Where: &params.where, Filter: params.filter, DryRun: params.dryRun}, body)
			if cubapi.IsAPIError(err, resp) {
				return 0, nil, nil, cubapi.InterpretErrorGeneric(err, resp)
			}
			return resp.StatusCode(), resp.JSON200, resp.JSON207, nil
		},
	})
	addEntityMoveCommand(entityMove{
		parent:         attributeCmd,
		entity:         "attribute",
		plural:         "attributes",
		identifierFlag: "attribute",
		about:          "The space an attribute leaves and the one it arrives in rebuild their functions if they\nselected it before or select it after. Invocations and triggers in the space it leaves that\ncall its getter or setter stop finding them, as when it is deleted.",
		buildWhere:     buildWhereClauseFromAttributes,
		move: func(params entityMoveParams, body goclientnew.MoveRequest) (int, *[]goclientnew.MoveResponse, *[]goclientnew.MoveResponse, error) {
			resp, err := cubClientNew.BulkMoveAttributesWithResponse(ctx, &goclientnew.BulkMoveAttributesParams{
				Where: &params.where, Filter: params.filter, DryRun: params.dryRun}, body)
			if cubapi.IsAPIError(err, resp) {
				return 0, nil, nil, cubapi.InterpretErrorGeneric(err, resp)
			}
			return resp.StatusCode(), resp.JSON200, resp.JSON207, nil
		},
	})
}
