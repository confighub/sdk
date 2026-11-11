// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

// Entity provisioning (admin.go) reconciles ConfigHub control-plane objects —
// Spaces, Filters, Triggers, Invocations, Targets and Attributes — from
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
//
// # Backing Units
//
// Every Ensure except [EnsureSpace] gives its entity a backing Unit: a
// ConfigHub/YAML Unit in the entity's Space holding the entity's configuration,
// so the entity has a revision history and can be changed through the Unit. A
// create asks for one, and an update asks for one too, so an entity created
// before it had one gets it the next time it is ensured. A Space's backing Unit
// would have to live in another Space, which the description does not name, so
// EnsureSpace creates none.
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

func withBackingUnits() *bool {
	b := true
	return &b
}

// whereID selects the one entity whose idField is id, for a bulk write of that entity alone.
func whereID(idField string, id goclientnew.UUID) *string {
	where := fmt.Sprintf("%s = '%s'", idField, id)
	return &where
}

// onlyResult is the entity a bulk write of one entity returned, or the error it reported for it.
// An Ensure updates through the bulk patch because the single-entity PATCH takes no
// with_backing_units.
func onlyResult[Result any, Entity any](results200, results207 *[]Result, entityType string, id goclientnew.UUID,
	read func(Result) (*Entity, *goclientnew.ResponseError)) (*Entity, error) {
	results := results200
	if results == nil {
		results = results207
	}
	if results == nil || len(*results) == 0 {
		return nil, fmt.Errorf("cubapi: %s %s not found to update", entityType, id)
	}
	if len(*results) != 1 {
		return nil, fmt.Errorf("cubapi: update of %s %s wrote %d entities", entityType, id, len(*results))
	}
	entity, responseError := read((*results)[0])
	if responseError != nil {
		return nil, fmt.Errorf("cubapi: update %s %s: %s", entityType, id, errString(responseError))
	}
	if entity == nil {
		return nil, fmt.Errorf("cubapi: update of %s %s returned no %s", entityType, id, entityType)
	}
	return entity, nil
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
	existing, err := ResolveSpace(ctx, c, NewRef("", space.Slug), ResolveOpts{Select: "SpaceID,Slug"})
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
	res, err := c.API.PatchSpaceWithBodyWithResponse(ctx, existing.Space.SpaceID, &goclientnew.PatchSpaceParams{},
		mergePatchContentType, bytes.NewReader(patch))
	if IsAPIError(err, res) {
		return nil, InterpretErrorGeneric(err, res)
	}
	return res.JSON200, nil
}

// EnsureFilter creates a Filter in filter.SpaceID or patches an existing one.
func EnsureFilter(ctx context.Context, c *Client, filter goclientnew.Filter) (*goclientnew.Filter, error) {
	existing, err := ResolveFilter(ctx, c, NewRef("", filter.Slug), ResolveOpts{Space: filter.SpaceID, Select: "FilterID,Slug,SpaceID"})
	if err != nil {
		res, err := c.API.CreateFilterWithResponse(ctx, filter.SpaceID, &goclientnew.CreateFilterParams{AllowExists: allowExists(), WithBackingUnits: withBackingUnits()}, filter)
		if IsAPIError(err, res) {
			return nil, InterpretErrorGeneric(err, res)
		}
		return res.JSON200, nil
	}
	patch, err := mergePatch[goclientnew.PatchFilterApplicationMergePatchPlusJSONBody](filter)
	if err != nil {
		return nil, err
	}
	id := existing.Filter.FilterID
	res, err := c.API.BulkPatchFiltersWithBodyWithResponse(ctx, &goclientnew.BulkPatchFiltersParams{
		Where:            whereID("FilterID", id),
		WithBackingUnits: withBackingUnits(),
	}, mergePatchContentType, bytes.NewReader(patch))
	if IsAPIError(err, res) {
		return nil, InterpretErrorGeneric(err, res)
	}
	return onlyResult(res.JSON200, res.JSON207, "Filter", id,
		func(r goclientnew.FilterCreateOrUpdateResponse) (*goclientnew.Filter, *goclientnew.ResponseError) {
			return r.Filter, r.Error
		})
}

// EnsureTrigger creates a Trigger in trigger.SpaceID or patches an existing one.
func EnsureTrigger(ctx context.Context, c *Client, trigger goclientnew.Trigger) (*goclientnew.Trigger, error) {
	existing, err := ResolveTrigger(ctx, c, NewRef("", trigger.Slug), ResolveOpts{Space: trigger.SpaceID, Select: "TriggerID,Slug,SpaceID"})
	if err != nil {
		res, err := c.API.CreateTriggerWithResponse(ctx, trigger.SpaceID, &goclientnew.CreateTriggerParams{AllowExists: allowExists(), WithBackingUnits: withBackingUnits()}, trigger)
		if IsAPIError(err, res) {
			return nil, InterpretErrorGeneric(err, res)
		}
		return res.JSON200, nil
	}
	patch, err := triggerMergePatch(trigger)
	if err != nil {
		return nil, err
	}
	id := existing.Trigger.TriggerID
	res, err := c.API.BulkPatchTriggersWithBodyWithResponse(ctx, &goclientnew.BulkPatchTriggersParams{
		Where:            whereID("TriggerID", id),
		WithBackingUnits: withBackingUnits(),
	}, mergePatchContentType, bytes.NewReader(patch))
	if IsAPIError(err, res) {
		return nil, InterpretErrorGeneric(err, res)
	}
	return onlyResult(res.JSON200, res.JSON207, "Trigger", id,
		func(r goclientnew.TriggerCreateOrUpdateResponse) (*goclientnew.Trigger, *goclientnew.ResponseError) {
			return r.Trigger, r.Error
		})
}

// triggerMergePatch is the merge patch of a Trigger's description. A description that names a
// function states the function's whole invocation, so its Arguments go in the patch even when
// there are none: left out, a merge patch would keep the arguments of the function the Trigger
// ran before, which the new one may not take.
func triggerMergePatch(trigger goclientnew.Trigger) ([]byte, error) {
	patch, err := mergePatch[goclientnew.PatchTriggerApplicationMergePatchPlusJSONBody](trigger)
	if err != nil || trigger.FunctionName == "" || len(trigger.Arguments) > 0 {
		return patch, err
	}
	var m map[string]any
	if err := json.Unmarshal(patch, &m); err != nil {
		return nil, fmt.Errorf("cubapi: reread the patch of trigger %s: %w", trigger.Slug, err)
	}
	m["Arguments"] = []any{}
	return json.Marshal(m)
}

// EnsureInvocation creates a stored Invocation in invocation.SpaceID or patches
// an existing one.
func EnsureInvocation(ctx context.Context, c *Client, invocation goclientnew.Invocation) (*goclientnew.Invocation, error) {
	existing, err := ResolveInvocation(ctx, c, NewRef("", invocation.Slug), ResolveOpts{Space: invocation.SpaceID, Select: "InvocationID,Slug,SpaceID"})
	if err != nil {
		res, err := c.API.CreateInvocationWithResponse(ctx, invocation.SpaceID, &goclientnew.CreateInvocationParams{AllowExists: allowExists(), WithBackingUnits: withBackingUnits()}, invocation)
		if IsAPIError(err, res) {
			return nil, InterpretErrorGeneric(err, res)
		}
		return res.JSON200, nil
	}
	patch, err := mergePatch[goclientnew.PatchInvocationApplicationMergePatchPlusJSONBody](invocation)
	if err != nil {
		return nil, err
	}
	id := existing.Invocation.InvocationID
	res, err := c.API.BulkPatchInvocationsWithBodyWithResponse(ctx, &goclientnew.BulkPatchInvocationsParams{
		Where:            whereID("InvocationID", id),
		WithBackingUnits: withBackingUnits(),
	}, mergePatchContentType, bytes.NewReader(patch))
	if IsAPIError(err, res) {
		return nil, InterpretErrorGeneric(err, res)
	}
	return onlyResult(res.JSON200, res.JSON207, "Invocation", id,
		func(r goclientnew.InvocationCreateOrUpdateResponse) (*goclientnew.Invocation, *goclientnew.ResponseError) {
			return r.Invocation, r.Error
		})
}

// EnsureTarget creates a Target in target.SpaceID or patches an existing one.
func EnsureTarget(ctx context.Context, c *Client, target goclientnew.Target) (*goclientnew.Target, error) {
	existing, err := ResolveTarget(ctx, c, NewRef("", target.Slug), ResolveOpts{Space: target.SpaceID, Select: "TargetID,Slug,SpaceID"})
	if err != nil {
		res, err := c.API.CreateTargetWithResponse(ctx, target.SpaceID, &goclientnew.CreateTargetParams{AllowExists: allowExists(), WithBackingUnits: withBackingUnits()}, target)
		if IsAPIError(err, res) {
			return nil, InterpretErrorGeneric(err, res)
		}
		return res.JSON200, nil
	}
	patch, err := mergePatch[goclientnew.PatchTargetApplicationMergePatchPlusJSONBody](target)
	if err != nil {
		return nil, err
	}
	id := existing.Target.TargetID
	res, err := c.API.BulkPatchTargetsWithBodyWithResponse(ctx, &goclientnew.BulkPatchTargetsParams{
		Where:            whereID("TargetID", id),
		WithBackingUnits: withBackingUnits(),
	}, mergePatchContentType, bytes.NewReader(patch))
	if IsAPIError(err, res) {
		return nil, InterpretErrorGeneric(err, res)
	}
	return onlyResult(res.JSON200, res.JSON207, "Target", id,
		func(r goclientnew.TargetCreateOrUpdateResponse) (*goclientnew.Target, *goclientnew.ResponseError) {
			return r.Target, r.Error
		})
}

// EnsureAttribute creates an Attribute in attribute.SpaceID or patches an
// existing one.
func EnsureAttribute(ctx context.Context, c *Client, attribute goclientnew.Attribute) (*goclientnew.Attribute, error) {
	existing, err := ResolveAttribute(ctx, c, NewRef("", attribute.Slug), ResolveOpts{Space: attribute.SpaceID, Select: "AttributeID,Slug,SpaceID"})
	if err != nil {
		res, err := c.API.CreateAttributeWithResponse(ctx, attribute.SpaceID, &goclientnew.CreateAttributeParams{AllowExists: allowExists(), WithBackingUnits: withBackingUnits()}, attribute)
		if IsAPIError(err, res) {
			return nil, InterpretErrorGeneric(err, res)
		}
		return res.JSON200, nil
	}
	patch, err := mergePatch[goclientnew.PatchAttributeApplicationMergePatchPlusJSONBody](attribute)
	if err != nil {
		return nil, err
	}
	id := existing.Attribute.AttributeID
	res, err := c.API.BulkPatchAttributesWithBodyWithResponse(ctx, &goclientnew.BulkPatchAttributesParams{
		Where:            whereID("AttributeID", id),
		WithBackingUnits: withBackingUnits(),
	}, mergePatchContentType, bytes.NewReader(patch))
	if IsAPIError(err, res) {
		return nil, InterpretErrorGeneric(err, res)
	}
	return onlyResult(res.JSON200, res.JSON207, "Attribute", id,
		func(r goclientnew.AttributeCreateOrUpdateResponse) (*goclientnew.Attribute, *goclientnew.ResponseError) {
			return r.Attribute, r.Error
		})
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
