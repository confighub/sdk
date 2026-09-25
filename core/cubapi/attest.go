// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package cubapi

import (
	"context"
	"github.com/cockroachdb/errors"

	goclientnew "github.com/confighub/sdk/core/openapi/goclient-new"
	"github.com/google/uuid"
)

// Attest records Attestations across Spaces with POST /attest: one in each Space the request
// selects, covering the Revisions it selects there. With dryRun, it reports what would be covered
// and records nothing. The result carries each Space's outcome, including the Spaces that failed,
// so a partial failure is returned as a result rather than as an error.
func Attest(ctx context.Context, c *Client, req goclientnew.AttestRequest, dryRun bool) (*goclientnew.AttestResult, error) {
	params := &goclientnew.AttestParams{}
	if dryRun {
		params.DryRun = &dryRun
	}
	res, err := c.API.AttestWithResponse(ctx, params, req)
	if IsAPIError(err, res) {
		return nil, InterpretErrorGeneric(err, res)
	}
	// 200 when every Space succeeded, 207 when some failed; both carry a result.
	result := res.JSON200
	if result == nil {
		result = res.JSON207
	}
	if result == nil {
		return nil, errors.New("cubapi: attest returned no result")
	}
	return result, nil
}

// CreateAttestation records one Attestation in a Space, or revokes one when the request names
// RevokedAttestationID. With dryRun, it reports what would be covered and records nothing.
func CreateAttestation(ctx context.Context, c *Client, spaceID uuid.UUID, req goclientnew.AttestationCreateRequest,
	dryRun bool) (*goclientnew.AttestationCreateResponse, error) {
	params := &goclientnew.CreateAttestationParams{}
	if dryRun {
		params.DryRun = &dryRun
	}
	res, err := c.API.CreateAttestationWithResponse(ctx, spaceID, params, req)
	if IsAPIError(err, res) {
		return nil, InterpretErrorGeneric(err, res)
	}
	if res.JSON200 == nil {
		return nil, errors.New("cubapi: create attestation returned no result")
	}
	return res.JSON200, nil
}
