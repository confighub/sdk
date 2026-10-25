// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package cubapi

import (
	"context"
	"strconv"

	goclientnew "github.com/confighub/sdk/core/openapi/goclient-new"
	"github.com/google/uuid"
)

// This file reads ConfigDiffs: the server's account of how two configurations differ, resource
// by resource and path by path. What a function invocation or a bulk patch changed, or on a dry
// run would change, comes back the same way when its [Change] asks for it with Diff.

// diffInclude is the include that asks a diff read for the unchanged paths around each change,
// or nil when context is not wanted.
func diffInclude(withContext bool) *string {
	if !withContext {
		return nil
	}
	include := "Context"
	return &include
}

// GetUnitDiff reads the difference between two Revisions of a Unit, each named by a Revision
// selector the server resolves, such as a Revision number, "LastApplied" or a Tag. An empty
// selector leaves that side to the server's default. withContext also returns the unchanged
// paths around each change.
func GetUnitDiff(ctx context.Context, c *Client, spaceID, unitID uuid.UUID, from, to string, withContext bool) (*goclientnew.UnitDiff, error) {
	params := &goclientnew.GetUnitDiffParams{Include: diffInclude(withContext)}
	if from != "" {
		params.From = &from
	}
	if to != "" {
		params.To = &to
	}
	res, err := c.API.GetUnitDiffWithResponse(ctx, spaceID, unitID, params)
	if IsAPIError(err, res) {
		return nil, InterpretErrorGeneric(err, res)
	}
	return res.JSON200, nil
}

// DiffConfigurations reads the difference between two configurations, each a Unit at a Revision
// ([UnitDiffSide]) or configuration given inline ([InlineDiffSide]).
func DiffConfigurations(ctx context.Context, c *Client, request goclientnew.DiffRequest, withContext bool) (*goclientnew.DiffResult, error) {
	res, err := c.API.DiffConfigurationsWithResponse(ctx,
		&goclientnew.DiffConfigurationsParams{Include: diffInclude(withContext)}, request)
	if IsAPIError(err, res) {
		return nil, InterpretErrorGeneric(err, res)
	}
	return res.JSON200, nil
}

// SearchUnitDiffs reads, in one request, the difference between two Revisions of every Unit
// matching where, each side named by a Revision selector the server resolves per Unit. A Unit
// that has neither Revision comes back with both Revision numbers zero.
func SearchUnitDiffs(ctx context.Context, c *Client, where, from, to string, withContext bool) ([]goclientnew.UnitDiff, error) {
	params := &goclientnew.SearchUnitDiffParams{Include: diffInclude(withContext)}
	if where != "" {
		params.Where = &where
	}
	if from != "" {
		params.From = &from
	}
	if to != "" {
		params.To = &to
	}
	res, err := c.API.SearchUnitDiffWithResponse(ctx, params)
	if IsAPIError(err, res) {
		return nil, InterpretErrorGeneric(err, res)
	}
	if res.JSON200 == nil {
		return nil, nil
	}
	return *res.JSON200, nil
}

// UnitDiffSide is one side of a DiffRequest: a Unit at a Revision number.
func UnitDiffSide(unitID uuid.UUID, revisionNum int64) *goclientnew.DiffSide {
	return &goclientnew.DiffSide{UnitID: unitID, Revision: strconv.FormatInt(revisionNum, 10)}
}

// InlineDiffSide is one side of a DiffRequest: configuration given inline.
func InlineDiffSide(data []byte, toolchainType string) *goclientnew.DiffSide {
	return &goclientnew.DiffSide{Data: string(data), ToolchainType: toolchainType}
}
