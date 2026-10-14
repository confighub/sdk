// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package blame

import (
	"context"

	"github.com/confighub/sdk/core/cubapi"
	goclientnew "github.com/confighub/sdk/core/openapi/goclient-new"
	"github.com/google/uuid"
)

// UnitName names a unit as space/unit, or by its ID when it cannot be read. A report names
// the same upstream unit on every hop, so the lookup is cached with the Resolver's others.
func (r *Resolver) UnitName(ctx context.Context, spaceID, unitID uuid.UUID) string {
	if unitID == uuid.Nil {
		return ""
	}
	unit := r.unitByID(ctx, spaceID, unitID)
	if unit == nil {
		return unitID.String()
	}
	return r.space(ctx, unit.SpaceID).slug + "/" + unit.Slug
}

// TargetName names a Target by slug, space/slug when it is in another Space than spaceID, or
// by its ID when it cannot be read.
func (r *Resolver) TargetName(ctx context.Context, spaceID, targetID uuid.UUID) string {
	if name, ok := r.targets[targetID]; ok {
		return name
	}
	name := targetID.String()
	if target, err := cubapi.ResolveTarget(ctx, r.client, cubapi.ParseRef(targetID.String()),
		cubapi.ResolveOpts{Space: spaceID, Select: "TargetID,Slug,SpaceID"}); err == nil && target != nil && target.Target != nil {
		name = r.qualify(ctx, spaceID, target.Target.SpaceID, target.Target.Slug)
	}
	r.targets[targetID] = name
	return name
}

// functionInvocationName is the function a Mutation ran, if it ran one.
func functionInvocationName(invocation *goclientnew.FunctionInvocation) string {
	if invocation == nil {
		return ""
	}
	return invocation.FunctionName
}
