// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

// Entity provisioning (admin.go) reconciles ConfigHub control-plane objects —
// Spaces, Filters, Triggers, Invocations, Targets, Workers and Attributes — from
// a description in code. Each Ensure* helper creates the entity if it is not
// there and patches it towards the description if it is, so a tool that declares
// its policy or its profile library once can re-run and have the server agree
// with the declaration.
//
// The helpers take the generated entity structs directly (goclientnew.Trigger,
// etc.) rather than re-describing them. Function arguments on Triggers and
// Invocations are built with [Arguments].
//
// # What a patch carries
//
// The description is rendered through the entity's generated merge-patch body,
// which admits the writable fields and nothing else, so the server's own fields
// -- the entity id, its SpaceID, the timestamps -- are never asserted.
//
// An optional field the caller left unset is dropped, and keeps whatever value
// it has on the server. A required field is always asserted, set or not: the
// same description has to serve the create, and an entity missing a required
// field would not have been creatable, so a description that cares about one
// sets it.
//
// The other side of that: an optional field cannot be cleared by leaving it out,
// because leaving it out is how a caller says "not mine". Clearing one is a job
// for the CLI or the API, not for a declaration in code. Nor does an Ensure
// preserve a hand edit -- a field the description names is set to what the
// description says.
package cubapi

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"

	goclientnew "github.com/confighub/sdk/core/openapi/goclient-new"
)

func allowExists() *string {
	s := "true"
	return &s
}

const mergePatchContentType = "application/merge-patch+json"

// mergePatch renders desired as a JSON merge patch, through the generated
// merge-patch body type Body.
//
// Two filters, and both are load-bearing. Round-tripping through Body drops
// every field that is not writable — an entity's own id, its SpaceID, the
// timestamps the server owns — which a patch built from the entity struct would
// otherwise carry, sending a zero UUID for a field the caller never touched.
// Then the nulls are stripped: a merge-patch body is all pointers, so a field
// the caller did not set marshals as null, and null in a merge patch means
// delete. Sent as-is it would erase every field the description does not name.
func mergePatch[Body any](desired any) ([]byte, error) {
	raw, err := json.Marshal(desired)
	if err != nil {
		return nil, fmt.Errorf("cubapi: marshal %T: %w", desired, err)
	}
	var body Body
	if err := json.Unmarshal(raw, &body); err != nil {
		return nil, fmt.Errorf("cubapi: %T is not describable as %T: %w", desired, body, err)
	}
	filtered, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("cubapi: marshal %T: %w", body, err)
	}
	var m map[string]any
	if err := json.Unmarshal(filtered, &m); err != nil {
		return nil, fmt.Errorf("cubapi: reread %T: %w", body, err)
	}
	return json.Marshal(stripNulls(m))
}

// stripNulls removes null values at every depth, including inside slices.
//
// Depth matters: a merge patch merges nested objects recursively, so a null left
// inside one deletes that key rather than the whole object, and a description
// that never mentioned the field would silently clear it.
func stripNulls(v any) any {
	switch t := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(t))
		for k, val := range t {
			if val == nil {
				continue
			}
			out[k] = stripNulls(val)
		}
		return out
	case []any:
		out := make([]any, 0, len(t))
		for _, val := range t {
			if val == nil {
				continue
			}
			out = append(out, stripNulls(val))
		}
		return out
	default:
		return v
	}
}

// EnsureSpace creates a Space or patches an existing one towards space.
func EnsureSpace(ctx context.Context, c *Client, space goclientnew.Space) (*goclientnew.Space, error) {
	existing, err := ResolveSpace(ctx, c, space.Slug)
	if err != nil {
		res, err := c.API.CreateSpaceWithResponse(ctx, &goclientnew.CreateSpaceParams{AllowExists: allowExists()}, space)
		if IsAPIError(err, res) {
			return nil, InterpretErrorGeneric(err, res)
		}
		return res.JSON200, nil
	}
	patch, err := mergePatch[goclientnew.PatchSpaceApplicationMergePatchPlusJSONBody](space)
	if err != nil {
		return nil, err
	}
	res, err := c.API.PatchSpaceWithBodyWithResponse(ctx, existing.SpaceID, &goclientnew.PatchSpaceParams{},
		mergePatchContentType, bytes.NewReader(patch))
	if IsAPIError(err, res) {
		return nil, InterpretErrorGeneric(err, res)
	}
	return res.JSON200, nil
}

// EnsureFilter creates a Filter in filter.SpaceID or patches an existing one.
func EnsureFilter(ctx context.Context, c *Client, filter goclientnew.Filter) (*goclientnew.Filter, error) {
	existing, err := ResolveFilter(ctx, c, filter.SpaceID, filter.Slug)
	if err != nil {
		res, err := c.API.CreateFilterWithResponse(ctx, filter.SpaceID, &goclientnew.CreateFilterParams{AllowExists: allowExists()}, filter)
		if IsAPIError(err, res) {
			return nil, InterpretErrorGeneric(err, res)
		}
		return res.JSON200, nil
	}
	patch, err := mergePatch[goclientnew.PatchFilterApplicationMergePatchPlusJSONBody](filter)
	if err != nil {
		return nil, err
	}
	res, err := c.API.PatchFilterWithBodyWithResponse(ctx, filter.SpaceID, existing.FilterID,
		mergePatchContentType, bytes.NewReader(patch))
	if IsAPIError(err, res) {
		return nil, InterpretErrorGeneric(err, res)
	}
	return res.JSON200, nil
}

// EnsureTrigger creates a Trigger in trigger.SpaceID or patches an existing one.
func EnsureTrigger(ctx context.Context, c *Client, trigger goclientnew.Trigger) (*goclientnew.Trigger, error) {
	existing, err := ResolveTrigger(ctx, c, trigger.SpaceID, trigger.Slug)
	if err != nil {
		res, err := c.API.CreateTriggerWithResponse(ctx, trigger.SpaceID, &goclientnew.CreateTriggerParams{AllowExists: allowExists()}, trigger)
		if IsAPIError(err, res) {
			return nil, InterpretErrorGeneric(err, res)
		}
		return res.JSON200, nil
	}
	patch, err := mergePatch[goclientnew.PatchTriggerApplicationMergePatchPlusJSONBody](trigger)
	if err != nil {
		return nil, err
	}
	res, err := c.API.PatchTriggerWithBodyWithResponse(ctx, trigger.SpaceID, existing.TriggerID,
		mergePatchContentType, bytes.NewReader(patch))
	if IsAPIError(err, res) {
		return nil, InterpretErrorGeneric(err, res)
	}
	return res.JSON200, nil
}

// EnsureInvocation creates a stored Invocation in invocation.SpaceID or patches
// an existing one.
func EnsureInvocation(ctx context.Context, c *Client, invocation goclientnew.Invocation) (*goclientnew.Invocation, error) {
	existing, err := ResolveInvocation(ctx, c, invocation.SpaceID, invocation.Slug)
	if err != nil {
		res, err := c.API.CreateInvocationWithResponse(ctx, invocation.SpaceID, &goclientnew.CreateInvocationParams{AllowExists: allowExists()}, invocation)
		if IsAPIError(err, res) {
			return nil, InterpretErrorGeneric(err, res)
		}
		return res.JSON200, nil
	}
	patch, err := mergePatch[goclientnew.PatchInvocationApplicationMergePatchPlusJSONBody](invocation)
	if err != nil {
		return nil, err
	}
	res, err := c.API.PatchInvocationWithBodyWithResponse(ctx, invocation.SpaceID, existing.InvocationID,
		mergePatchContentType, bytes.NewReader(patch))
	if IsAPIError(err, res) {
		return nil, InterpretErrorGeneric(err, res)
	}
	return res.JSON200, nil
}

// EnsureTarget creates a Target in target.SpaceID or patches an existing one.
func EnsureTarget(ctx context.Context, c *Client, target goclientnew.Target) (*goclientnew.Target, error) {
	existing, err := ResolveTarget(ctx, c, target.SpaceID, target.Slug)
	if err != nil {
		res, err := c.API.CreateTargetWithResponse(ctx, target.SpaceID, &goclientnew.CreateTargetParams{AllowExists: allowExists()}, target)
		if IsAPIError(err, res) {
			return nil, InterpretErrorGeneric(err, res)
		}
		return res.JSON200, nil
	}
	patch, err := mergePatch[goclientnew.PatchTargetApplicationMergePatchPlusJSONBody](target)
	if err != nil {
		return nil, err
	}
	res, err := c.API.PatchTargetWithBodyWithResponse(ctx, target.SpaceID, existing.TargetID, &goclientnew.PatchTargetParams{},
		mergePatchContentType, bytes.NewReader(patch))
	if IsAPIError(err, res) {
		return nil, InterpretErrorGeneric(err, res)
	}
	return res.JSON200, nil
}

// EnsureBridgeWorker creates a worker in worker.SpaceID or patches an existing one.
func EnsureBridgeWorker(ctx context.Context, c *Client, worker goclientnew.BridgeWorker) (*goclientnew.BridgeWorker, error) {
	existing, err := ResolveBridgeWorker(ctx, c, worker.SpaceID, worker.Slug)
	if err != nil {
		res, err := c.API.CreateBridgeWorkerWithResponse(ctx, worker.SpaceID, &goclientnew.CreateBridgeWorkerParams{AllowExists: allowExists()}, worker)
		if IsAPIError(err, res) {
			return nil, InterpretErrorGeneric(err, res)
		}
		return res.JSON200, nil
	}
	patch, err := mergePatch[goclientnew.PatchBridgeWorkerApplicationMergePatchPlusJSONBody](worker)
	if err != nil {
		return nil, err
	}
	res, err := c.API.PatchBridgeWorkerWithBodyWithResponse(ctx, worker.SpaceID, existing.BridgeWorkerID,
		mergePatchContentType, bytes.NewReader(patch))
	if IsAPIError(err, res) {
		return nil, InterpretErrorGeneric(err, res)
	}
	return res.JSON200, nil
}

// EnsureAttribute creates an Attribute in attribute.SpaceID or patches an
// existing one.
func EnsureAttribute(ctx context.Context, c *Client, attribute goclientnew.Attribute) (*goclientnew.Attribute, error) {
	existing, err := ResolveAttribute(ctx, c, attribute.SpaceID, attribute.Slug)
	if err != nil {
		res, err := c.API.CreateAttributeWithResponse(ctx, attribute.SpaceID, &goclientnew.CreateAttributeParams{AllowExists: allowExists()}, attribute)
		if IsAPIError(err, res) {
			return nil, InterpretErrorGeneric(err, res)
		}
		return res.JSON200, nil
	}
	patch, err := mergePatch[goclientnew.PatchAttributeApplicationMergePatchPlusJSONBody](attribute)
	if err != nil {
		return nil, err
	}
	res, err := c.API.PatchAttributeWithBodyWithResponse(ctx, attribute.SpaceID, existing.AttributeID,
		mergePatchContentType, bytes.NewReader(patch))
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
