// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package cubapi

import (
	"context"
	"strings"

	"github.com/cockroachdb/errors"

	goclientnew "github.com/confighub/sdk/core/openapi/goclient-new"
)

// PromoteRefusedError is returned when the entry gates of a Stage the promotion enters do not
// hold. Nothing was written. Result names every gate evaluated and whether it holds, so a caller
// can show all of what is holding the Stage rather than only the first.
type PromoteRefusedError struct {
	Result *goclientnew.PromoteResult
}

// FailedGates returns the messages of the gates that do not hold, in the order they were
// evaluated.
func (e *PromoteRefusedError) FailedGates() []string {
	var failed []string
	if e.Result == nil {
		return nil
	}
	for _, stage := range e.Result.Stages {
		for _, gate := range stage.Gates {
			if !gate.Satisfied {
				failed = append(failed, gate.Message)
			}
		}
	}
	return failed
}

func (e *PromoteRefusedError) Error() string {
	failed := e.FailedGates()
	if len(failed) == 0 {
		return "the promotion was refused by its gates"
	}
	return strings.Join(failed, "; ")
}

// Promote asks the server to promote changes into the downstream variants a request selects. The
// server selects the Spaces, evaluates any ChangeWorkflow gates, and decides what each Unit and
// Link needs, so every client promotes the same way.
//
// With dryRun set nothing is written and the result describes what would happen. A partial
// success (some Unit or Link write failed, each carrying its own error) is returned as a result
// rather than an error; check each result's Error. A refusal by the gates is a
// *PromoteRefusedError carrying the result.
func Promote(ctx context.Context, c *Client, req goclientnew.PromoteRequest, dryRun bool,
	with ...func(*goclientnew.PromoteParams)) (*goclientnew.PromoteResult, error) {
	params := &goclientnew.PromoteParams{}
	if dryRun {
		params.DryRun = &dryRun
	}
	for _, fn := range with {
		fn(params)
	}

	res, err := c.API.PromoteWithResponse(ctx, params, req)
	if err == nil && res != nil && res.JSON409 != nil {
		return nil, &PromoteRefusedError{Result: res.JSON409}
	}
	if IsAPIError(err, res) {
		return nil, InterpretErrorGeneric(err, res)
	}

	// 200 when every write succeeded, 207 when some failed; both carry a result.
	result := res.JSON200
	if result == nil {
		result = res.JSON207
	}
	if result == nil {
		return nil, errors.New("cubapi: promote returned no result")
	}
	return result, nil
}

// WithPromoteMutations asks a promotion for each Unit write's Mutations: what it changed, or on a
// dry run what it would change.
func WithPromoteMutations(params *goclientnew.PromoteParams) {
	include := "Mutations"
	params.Include = &include
}
