// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package cubapi

import (
	"context"

	"github.com/cockroachdb/errors"

	goclientnew "github.com/confighub/sdk/core/openapi/goclient-new"
)

// GetChangeOrderContainerImages reads one ChangeOrder with its ContainerImages: for each Space it
// has landed in, the container images it changed there. The server derives them only when asked,
// since doing so runs a function over two Revisions of every Unit the change moved.
func GetChangeOrderContainerImages(ctx context.Context, c *Client, spaceID, changeOrderID goclientnew.UUID) ([]goclientnew.ChangeOrderSpaceContainerImages, error) {
	containerImages := true
	res, err := c.API.GetChangeOrderWithResponse(ctx, spaceID, changeOrderID,
		&goclientnew.GetChangeOrderParams{ContainerImages: &containerImages})
	if IsAPIError(err, res) {
		return nil, InterpretErrorGeneric(err, res)
	}
	if res.JSON200 == nil || res.JSON200.ChangeOrder == nil {
		return nil, errors.New("cubapi: change order get returned no change order")
	}
	return res.JSON200.ChangeOrder.ContainerImages, nil
}
