// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

// Entity provisioning (admin.go) holds idempotent create helpers for ConfigHub
// control-plane objects — Spaces, Triggers, Filters, and stored Invocations —
// plus wiring a Space's Triggers to a shared Filter. Each Ensure* helper creates
// the entity with allow-exists semantics, so it is safe to run repeatedly (the
// "install once per org" pattern), and returns the created or existing entity.
//
// The helpers take the generated entity structs directly (goclientnew.Trigger,
// etc.) rather than re-describing them; fields the caller leaves unset (such as
// DisplayName) are defaulted by the server. Function arguments on Triggers and
// Invocations are built with [Arguments].
package cubapi

import (
	"context"

	goclientnew "github.com/confighub/sdk/core/openapi/goclient-new"
)

func allowExists() *string {
	s := "true"
	return &s
}

// EnsureSpace idempotently creates a Space and returns it (the existing Space if
// it already exists).
func EnsureSpace(ctx context.Context, c *Client, space goclientnew.Space) (*goclientnew.Space, error) {
	res, err := c.API.CreateSpaceWithResponse(ctx, &goclientnew.CreateSpaceParams{AllowExists: allowExists()}, space)
	if IsAPIError(err, res) {
		return nil, InterpretErrorGeneric(err, res)
	}
	return res.JSON200, nil
}

// EnsureTrigger idempotently creates a Trigger in trigger.SpaceID.
func EnsureTrigger(ctx context.Context, c *Client, trigger goclientnew.Trigger) (*goclientnew.Trigger, error) {
	res, err := c.API.CreateTriggerWithResponse(ctx, trigger.SpaceID, &goclientnew.CreateTriggerParams{AllowExists: allowExists()}, trigger)
	if IsAPIError(err, res) {
		return nil, InterpretErrorGeneric(err, res)
	}
	return res.JSON200, nil
}

// EnsureFilter idempotently creates a Filter in filter.SpaceID.
func EnsureFilter(ctx context.Context, c *Client, filter goclientnew.Filter) (*goclientnew.Filter, error) {
	res, err := c.API.CreateFilterWithResponse(ctx, filter.SpaceID, &goclientnew.CreateFilterParams{AllowExists: allowExists()}, filter)
	if IsAPIError(err, res) {
		return nil, InterpretErrorGeneric(err, res)
	}
	return res.JSON200, nil
}

// EnsureInvocation idempotently creates a stored Invocation in invocation.SpaceID.
func EnsureInvocation(ctx context.Context, c *Client, invocation goclientnew.Invocation) (*goclientnew.Invocation, error) {
	res, err := c.API.CreateInvocationWithResponse(ctx, invocation.SpaceID, &goclientnew.CreateInvocationParams{AllowExists: allowExists()}, invocation)
	if IsAPIError(err, res) {
		return nil, InterpretErrorGeneric(err, res)
	}
	return res.JSON200, nil
}

// SetSpaceTriggerFilter points a Space's Triggers at a shared Filter: it sets the
// Space's TriggerFilterID to filterID and clears its WhereTrigger. space should
// be a full Space record (e.g. from [ResolveSpace]).
func SetSpaceTriggerFilter(ctx context.Context, c *Client, space *goclientnew.Space, filterID goclientnew.UUID) error {
	space.WhereTrigger = ""
	space.TriggerFilterID = &filterID
	res, err := c.API.UpdateSpaceWithResponse(ctx, space.SpaceID, &goclientnew.UpdateSpaceParams{}, *space)
	if IsAPIError(err, res) {
		return InterpretErrorGeneric(err, res)
	}
	return nil
}
