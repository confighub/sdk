// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package cubapi

import (
	"context"

	goclientnew "github.com/confighub/sdk/core/openapi/goclient-new"
)

// DeleteSpace deletes a Space. With force, everything in it goes too, Releases included and
// whatever delete gates its entities carry: `cub space delete --recursive-force`.
func DeleteSpace(ctx context.Context, c *Client, spaceID goclientnew.UUID, force bool) error {
	params := &goclientnew.DeleteSpaceParams{}
	if force {
		value := "true"
		params.RecursiveForce = &value
	}
	res, err := c.API.DeleteSpaceWithResponse(ctx, spaceID, params)
	if IsAPIError(err, res) {
		return InterpretErrorGeneric(err, res)
	}
	return nil
}
