// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package cubapi

import (
	"context"
	"net/http"

	"github.com/cockroachdb/errors"

	goclientnew "github.com/confighub/sdk/core/openapi/goclient-new"
)

// UnitData returns a Unit's configuration data.
//
// The data is read through its own endpoint, whose successful response body is
// the configuration itself rather than a JSON envelope, so [IsAPIError] -- which
// treats a missing JSON200 as a failure -- cannot judge it.
func UnitData(ctx context.Context, c *Client, spaceID, unitID goclientnew.UUID) (string, error) {
	res, err := c.API.DownloadUnitDataWithResponse(ctx, spaceID, unitID)
	if err != nil {
		return "", err
	}
	if res.StatusCode() != http.StatusOK {
		if apiErr := InterpretErrorGeneric(nil, res); apiErr != nil {
			return "", apiErr
		}
		return "", errors.Errorf("read unit data: %s", res.Status())
	}
	return string(res.Body), nil
}
