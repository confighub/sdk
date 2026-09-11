// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

// Entity resolution for the CLI: one function per entity type, each a thin
// adapter over the cubapi resolvers.
//
// The adapters exist because cub still carries its space as a string global
// that may be a UUID, "*", or empty; they translate that into the typed
// ResolveOpts the library takes. When the space becomes a resolved value on the
// command's run context, the adapters lose their spaceID parameter and most of
// them disappear.
//
// Every lookup accepts the spellings a person writes: a slug, a "space/slug",
// or a UUID. A UUID is never scoped by space -- see cubapi's resolve.go.
package main

import (
	"github.com/confighub/sdk/core/cubapi"
	goclientnew "github.com/confighub/sdk/core/openapi/goclient-new"
	"github.com/google/uuid"
)

// spaceUUID converts cub's string-valued space to the typed one the resolvers
// take. An empty or wildcard space becomes the zero UUID, which means "search
// the organization" rather than a parse failure -- commands that never ran
// spacePreRunE hold an empty selectedSpaceID, and a lookup from one of those
// should search rather than panic.
func spaceUUID(spaceID string) goclientnew.UUID {
	if spaceID == "" || spaceID == "*" {
		return goclientnew.UUID{}
	}
	parsed, err := uuid.Parse(spaceID)
	if err != nil {
		return goclientnew.UUID{}
	}
	return goclientnew.UUID(parsed)
}

// resolveOpts builds the lookup options from a space and a per-call select.
// An empty selectParam falls back to the --select flag, and an empty --select
// means every field, which is what a single-entity read wants.
func resolveOpts(spaceID string, selectParam string) cubapi.ResolveOpts {
	return cubapi.ResolveOpts{
		Space:  spaceUUID(spaceID),
		Select: cubapi.SelectFields(handleSelectParameter(selectParam, selectFields, nil)),
	}
}

// defaultSpaceID is the space an unqualified reference is looked up in: the one
// --space selected, else the context's default. It is empty when neither names
// one, which makes the lookup organization-wide.
func defaultSpaceID() string {
	if selectedSpaceID != "" && selectedSpaceID != "*" {
		return selectedSpaceID
	}
	if cubContext := contextManager.CurrentContext(); cubContext != nil {
		if space := cubContext.Settings.DefaultSpace; space != "" && space != "*" {
			if resolved, err := resolveSpace(space, ""); err == nil {
				return resolved.Space.SpaceID.String()
			}
		}
	}
	return ""
}

func resolveSpace(ref string, selectParam string) (*goclientnew.ExtendedSpace, error) {
	return cubapi.ResolveSpace(ctx, cubClient, cubapi.ParseRef(ref), resolveOpts("", selectParam))
}

// resolveSpaceSummary resolves a space and asks for its rollup counts, which
// only "cub space get" displays.
func resolveSpaceSummary(ref string, selectParam string) (*goclientnew.ExtendedSpace, error) {
	return cubapi.ResolveSpace(ctx, cubClient, cubapi.ParseRef(ref), resolveOpts("", selectParam),
		func(p *goclientnew.ListSpacesParams) {
			summary := true
			p.Summary = &summary
		})
}

func resolveUnit(ref string, spaceID string, selectParam string) (*goclientnew.ExtendedUnit, error) {
	return cubapi.ResolveUnit(ctx, cubClient, cubapi.ParseRef(ref), resolveOpts(spaceID, selectParam))
}

func resolveTarget(ref string, spaceID string, selectParam string) (*goclientnew.ExtendedTarget, error) {
	return cubapi.ResolveTarget(ctx, cubClient, cubapi.ParseRef(ref), resolveOpts(spaceID, selectParam))
}

func resolveTrigger(ref string, spaceID string, selectParam string) (*goclientnew.ExtendedTrigger, error) {
	return cubapi.ResolveTrigger(ctx, cubClient, cubapi.ParseRef(ref), resolveOpts(spaceID, selectParam))
}

func resolveFilter(ref string, spaceID string, selectParam string) (*goclientnew.ExtendedFilter, error) {
	return cubapi.ResolveFilter(ctx, cubClient, cubapi.ParseRef(ref), resolveOpts(spaceID, selectParam))
}

func resolveInvocation(ref string, spaceID string, selectParam string) (*goclientnew.ExtendedInvocation, error) {
	return cubapi.ResolveInvocation(ctx, cubClient, cubapi.ParseRef(ref), resolveOpts(spaceID, selectParam))
}

func resolveChangeSet(ref string, spaceID string, selectParam string) (*goclientnew.ExtendedChangeSet, error) {
	return cubapi.ResolveChangeSet(ctx, cubClient, cubapi.ParseRef(ref), resolveOpts(spaceID, selectParam))
}

func resolveChangeOrder(ref string, spaceID string, selectParam string) (*goclientnew.ExtendedChangeOrder, error) {
	return cubapi.ResolveChangeOrder(ctx, cubClient, cubapi.ParseRef(ref), resolveOpts(spaceID, selectParam))
}

func resolveChangeWorkflow(ref string, spaceID string, selectParam string) (*goclientnew.ExtendedChangeWorkflow, error) {
	return cubapi.ResolveChangeWorkflow(ctx, cubClient, cubapi.ParseRef(ref), resolveOpts(spaceID, selectParam))
}

func resolveTag(ref string, spaceID string, selectParam string) (*goclientnew.ExtendedTag, error) {
	return cubapi.ResolveTag(ctx, cubClient, cubapi.ParseRef(ref), resolveOpts(spaceID, selectParam))
}

func resolveView(ref string, spaceID string, selectParam string) (*goclientnew.ExtendedView, error) {
	return cubapi.ResolveView(ctx, cubClient, cubapi.ParseRef(ref), resolveOpts(spaceID, selectParam))
}

func resolveAttribute(ref string, spaceID string, selectParam string) (*goclientnew.ExtendedAttribute, error) {
	return cubapi.ResolveAttribute(ctx, cubClient, cubapi.ParseRef(ref), resolveOpts(spaceID, selectParam))
}

func resolveLink(ref string, spaceID string, selectParam string) (*goclientnew.ExtendedLink, error) {
	return cubapi.ResolveLink(ctx, cubClient, cubapi.ParseRef(ref), resolveOpts(spaceID, selectParam))
}

func resolveWorker(ref string, spaceID string, selectParam string) (*goclientnew.ExtendedBridgeWorker, error) {
	return cubapi.ResolveBridgeWorker(ctx, cubClient, cubapi.ParseRef(ref), resolveOpts(spaceID, selectParam))
}

// The resolve*ID helpers answer the common question -- "which entity does this
// name?" -- for a caller that needs only the id, and that looks the entity up
// in the space the command selected. They ask for the id and the slug alone,
// since nothing else is read.
//
// These replace the parseEntityIdentifier* generics, which took the entity type
// and a lookup function as parameters so that one function could serve every
// entity; naming the entity in the function name says the same thing in one
// place instead of four arguments.

func idSelect(field string) string { return field + ",Slug,SpaceID" }

func resolveTargetID(ref string) (uuid.UUID, error) {
	target, err := resolveTarget(ref, defaultSpaceID(), idSelect("TargetID"))
	if err != nil {
		return uuid.Nil, err
	}
	return target.Target.TargetID, nil
}

func resolveFilterID(ref string) (uuid.UUID, error) {
	filter, err := resolveFilter(ref, defaultSpaceID(), idSelect("FilterID"))
	if err != nil {
		return uuid.Nil, err
	}
	return filter.Filter.FilterID, nil
}

func resolveViewID(ref string) (uuid.UUID, error) {
	view, err := resolveView(ref, defaultSpaceID(), idSelect("ViewID"))
	if err != nil {
		return uuid.Nil, err
	}
	return view.View.ViewID, nil
}

func resolveInvocationID(ref string) (uuid.UUID, error) {
	invocation, err := resolveInvocation(ref, defaultSpaceID(), idSelect("InvocationID"))
	if err != nil {
		return uuid.Nil, err
	}
	return invocation.Invocation.InvocationID, nil
}

func resolveTriggerID(ref string) (uuid.UUID, error) {
	trigger, err := resolveTrigger(ref, defaultSpaceID(), idSelect("TriggerID"))
	if err != nil {
		return uuid.Nil, err
	}
	return trigger.Trigger.TriggerID, nil
}

func resolveTagID(ref string) (uuid.UUID, error) {
	tag, err := resolveTag(ref, defaultSpaceID(), idSelect("TagID"))
	if err != nil {
		return uuid.Nil, err
	}
	return tag.Tag.TagID, nil
}

func resolveChangeSetID(ref string) (uuid.UUID, error) {
	changeSet, err := resolveChangeSet(ref, defaultSpaceID(), idSelect("ChangeSetID"))
	if err != nil {
		return uuid.Nil, err
	}
	return changeSet.ChangeSet.ChangeSetID, nil
}

func resolveChangeOrderID(ref string) (uuid.UUID, error) {
	changeOrder, err := resolveChangeOrder(ref, defaultSpaceID(), idSelect("ChangeOrderID"))
	if err != nil {
		return uuid.Nil, err
	}
	return changeOrder.ChangeOrder.ChangeOrderID, nil
}

func resolveChangeWorkflowID(ref string) (uuid.UUID, error) {
	changeWorkflow, err := resolveChangeWorkflow(ref, defaultSpaceID(), idSelect("ChangeWorkflowID"))
	if err != nil {
		return uuid.Nil, err
	}
	return changeWorkflow.ChangeWorkflow.ChangeWorkflowID, nil
}

func resolveUnitID(ref string) (uuid.UUID, error) {
	unit, err := resolveUnit(ref, defaultSpaceID(), idSelect("UnitID"))
	if err != nil {
		return uuid.Nil, err
	}
	return unit.Unit.UnitID, nil
}

func resolveLinkID(ref string) (uuid.UUID, error) {
	link, err := resolveLink(ref, defaultSpaceID(), idSelect("LinkID"))
	if err != nil {
		return uuid.Nil, err
	}
	return link.Link.LinkID, nil
}

func resolveAttributeID(ref string) (uuid.UUID, error) {
	attribute, err := resolveAttribute(ref, defaultSpaceID(), idSelect("AttributeID"))
	if err != nil {
		return uuid.Nil, err
	}
	return attribute.Attribute.AttributeID, nil
}

func resolveWorkerID(ref string) (uuid.UUID, error) {
	worker, err := resolveWorker(ref, defaultSpaceID(), idSelect("BridgeWorkerID"))
	if err != nil {
		return uuid.Nil, err
	}
	return worker.BridgeWorker.BridgeWorkerID, nil
}

// The plural helpers resolve a list of references, which is what the flags that
// accept several of them (--trigger, --invocation, cub k8s --target) need.
// They return value slices because their callers index into them.

func resolveTriggersCore(refs []string, selectParam string) ([]goclientnew.Trigger, error) {
	out := make([]goclientnew.Trigger, 0, len(refs))
	for _, ref := range refs {
		trigger, err := resolveTrigger(ref, defaultSpaceID(), selectParam)
		if err != nil {
			return nil, err
		}
		out = append(out, *trigger.Trigger)
	}
	return out, nil
}

func resolveInvocationsCore(refs []string, selectParam string) ([]goclientnew.Invocation, error) {
	out := make([]goclientnew.Invocation, 0, len(refs))
	for _, ref := range refs {
		invocation, err := resolveInvocation(ref, defaultSpaceID(), selectParam)
		if err != nil {
			return nil, err
		}
		out = append(out, *invocation.Invocation)
	}
	return out, nil
}

func resolveTargetsCore(refs []string, selectParam string) ([]goclientnew.Target, error) {
	out := make([]goclientnew.Target, 0, len(refs))
	for _, ref := range refs {
		target, err := resolveTarget(ref, defaultSpaceID(), selectParam)
		if err != nil {
			return nil, err
		}
		out = append(out, *target.Target)
	}
	return out, nil
}

// resolveTargetCore returns just the Target, for the callers that hold a space
// and want the record rather than the envelope.
func resolveTargetCore(ref string, spaceID string, selectParam string) (*goclientnew.Target, error) {
	target, err := resolveTarget(ref, spaceID, selectParam)
	if err != nil {
		return nil, err
	}
	return target.Target, nil
}
