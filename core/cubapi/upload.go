// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package cubapi

import (
	"context"

	"github.com/cockroachdb/errors"

	goclientnew "github.com/confighub/sdk/core/openapi/goclient-new"
)

// Upload sends a bundle of rendered configuration to the server, which splits it
// into resources and makes a Space's Units, Links, and Invocations match it.
//
// The client does not plan: it fetches the bundle, calls this, and reports what
// comes back. Deciding which Units to create, merge, or empty is the server's,
// so every client -- cub, the installer, the UI -- gets the same answer for the
// same bundle.
//
// With dryRun set nothing is written and the result describes what would happen.
// A partial success (some Unit or Link write failed, each carrying its own
// error) is returned as a result rather than an error, because the writes that
// did land are real and the caller has to be able to report them; check each
// UploadUnitResult's Error. Only a request that wrote nothing at all is an error.
func Upload(ctx context.Context, c *Client, req goclientnew.UploadRequest, dryRun bool) (*goclientnew.UploadResult, error) {
	params := &goclientnew.UploadParams{}
	if dryRun {
		params.DryRun = &dryRun
	}

	res, err := c.API.UploadWithResponse(ctx, params, req)
	if IsAPIError(err, res) {
		return nil, InterpretErrorGeneric(err, res)
	}

	// 200 when every write succeeded, 207 when some failed; both carry a result.
	result := res.JSON200
	if result == nil {
		result = res.JSON207
	}
	if result == nil {
		return nil, errors.New("cubapi: upload returned no result")
	}
	return result, nil
}
