// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package cubapi

import (
	"context"

	"github.com/cockroachdb/errors"

	goclientnew "github.com/confighub/sdk/core/openapi/goclient-new"
)

// Demote asks the server to take an aborted ChangeOrder back out of the Spaces it reached,
// restoring each Unit it marked to the Revision the Unit was at before the change. The server
// decides which Units that is from the ChangeOrder's Tags, and reports the Revisions made since
// the change that restoring drops.
//
// With dryRun set nothing is written and the result describes what would happen. A partial
// success (some Unit writes failed, each carrying its own error) is returned as a result rather
// than an error; check each result's Error.
func Demote(ctx context.Context, c *Client, req goclientnew.DemoteRequest, dryRun bool,
	with ...func(*goclientnew.DemoteParams)) (*goclientnew.DemoteResult, error) {
	params := &goclientnew.DemoteParams{}
	if dryRun {
		params.DryRun = &dryRun
	}
	for _, fn := range with {
		fn(params)
	}

	res, err := c.API.DemoteWithResponse(ctx, params, req)
	if IsAPIError(err, res) {
		return nil, InterpretErrorGeneric(err, res)
	}
	// 200 when every write succeeded, 207 when some failed; both carry a result.
	result := res.JSON200
	if result == nil {
		result = res.JSON207
	}
	if result == nil {
		return nil, errors.New("cubapi: demote returned no result")
	}
	return result, nil
}

// WithDemoteMutations asks a demotion for each restore's Mutations: what it changed, or on a dry
// run what it would change.
func WithDemoteMutations(params *goclientnew.DemoteParams) {
	include := "Mutations"
	params.Include = &include
}
