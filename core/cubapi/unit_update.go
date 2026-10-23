// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package cubapi

import (
	"context"
	"fmt"

	goclientnew "github.com/confighub/sdk/core/openapi/goclient-new"
)

// ResolveUnitLinks resolves the unit's links named by resolve, as `cub unit update --resolve`
// does: "Link:*" for every link that can resolve, "Link:<uuid>" for one, or "Link:<where>"
// to select among them. ch names the change and may not be a dry-run.
func ResolveUnitLinks(ctx context.Context, c *Client, spaceID, unitID goclientnew.UUID, resolve string, ch Change) (*goclientnew.UnitCreateOrUpdateResponse, error) {
	if ch.DryRun() {
		return nil, fmt.Errorf("cubapi: ResolveUnitLinks needs a change description")
	}
	current, err := c.API.GetUnitWithResponse(ctx, spaceID, unitID, &goclientnew.GetUnitParams{})
	if IsAPIError(err, current) {
		return nil, InterpretErrorGeneric(err, current)
	}
	unit := *current.JSON200.Unit
	unit.LastChangeDescription = ch.Description
	res, err := c.API.UpdateUnitWithResponse(ctx, spaceID, unitID, &goclientnew.UpdateUnitParams{Resolve: &resolve}, unit)
	if IsAPIError(err, res) {
		return nil, InterpretErrorGeneric(err, res)
	}
	if res.JSON200.Error != nil {
		return nil, fmt.Errorf("resolving links of %s: %s", unit.Slug, res.JSON200.Error.Message)
	}
	return res.JSON200, nil
}
