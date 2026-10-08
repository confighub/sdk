// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"bytes"

	goclientnew "github.com/confighub/sdk/core/openapi/goclient-new"
)

const mergePatchContentType = "application/merge-patch+json"

// The rename commands; see addEntityRenameCommand. An attribute has none: its getter, setter and
// paths are named for its slug, so the server refuses to rename one.
func init() {
	addEntityRenameCommand(entityRename{
		parent:   spaceCmd,
		entity:   "space",
		orgLevel: true,
		about: "Gate names in units' ValidationErrors and ValidationWarnings, and their Values keys, start\n" +
			"with the space's slug, and keep the old one until each unit is next resolved.",
		resolve: func(ref string) (*renamedEntity, error) {
			d, err := resolveSpace(ref, "SpaceID,Slug,DisplayName")
			if err != nil {
				return nil, err
			}
			return &renamedEntity{spaceID: d.Space.SpaceID, id: d.Space.SpaceID, slug: d.Space.Slug, displayName: d.Space.DisplayName}, nil
		},
		patch: func(e *renamedEntity, patch []byte) (any, error) {
			return patchSpace(e.id, patch)
		},
	})
	addEntityRenameCommand(entityRename{
		parent:   componentCmd,
		entity:   "component",
		orgLevel: true,
		about:    "An upload that names the component by its old name creates a new one.",
		resolve: func(ref string) (*renamedEntity, error) {
			d, err := resolveComponent(ref, "ComponentID,Slug,DisplayName")
			if err != nil {
				return nil, err
			}
			return &renamedEntity{id: d.Component.ComponentID, slug: d.Component.Slug, displayName: d.Component.DisplayName}, nil
		},
		patch: func(e *renamedEntity, patch []byte) (any, error) {
			res, err := cubClientNew.PatchComponentWithBodyWithResponse(ctx, e.id,
				&goclientnew.PatchComponentParams{DryRun: dryRunParam()}, mergePatchContentType, bytes.NewReader(patch))
			return patchedEntity(res, err, func(r *goclientnew.PatchComponentResponse) *goclientnew.Component { return r.JSON200 })
		},
	})
	addEntityRenameCommand(entityRename{
		parent: unitCmd,
		entity: "unit",
		about: "Configuration that functions stamped with the unit's slug, such as the\n" +
			"confighub.com/UnitSlug annotation, keeps the old one until the functions run again.",
		resolve: func(ref string) (*renamedEntity, error) {
			d, err := resolveUnit(ref, selectedSpaceID, "UnitID,Slug,DisplayName,SpaceID")
			if err != nil {
				return nil, err
			}
			return &renamedEntity{spaceID: d.Unit.SpaceID, id: d.Unit.UnitID, slug: d.Unit.Slug, displayName: d.Unit.DisplayName}, nil
		},
		patch: func(e *renamedEntity, patch []byte) (any, error) {
			res, err := patchUnit(e.spaceID, e.id, &goclientnew.UpdateUnitParams{DryRun: dryRunParam()}, patch)
			if err != nil {
				return nil, err
			}
			return res.Unit, nil
		},
	})
	addEntityRenameCommand(entityRename{
		parent: targetCmd,
		entity: "target",
		resolve: func(ref string) (*renamedEntity, error) {
			d, err := resolveTarget(ref, selectedSpaceID, "TargetID,Slug,DisplayName,SpaceID")
			if err != nil {
				return nil, err
			}
			return &renamedEntity{spaceID: d.Target.SpaceID, id: d.Target.TargetID, slug: d.Target.Slug, displayName: d.Target.DisplayName}, nil
		},
		patch: func(e *renamedEntity, patch []byte) (any, error) {
			res, err := cubClientNew.PatchTargetWithBodyWithResponse(ctx, e.spaceID, e.id,
				&goclientnew.PatchTargetParams{DryRun: dryRunParam()}, mergePatchContentType, bytes.NewReader(patch))
			return patchedEntity(res, err, func(r *goclientnew.PatchTargetResponse) *goclientnew.Target { return r.JSON200 })
		},
	})
	addEntityRenameCommand(entityRename{
		parent: triggerCmd,
		entity: "trigger",
		about: "Gates are found by the trigger's ID, so approving and clearing them are unaffected, but\n" +
			"gate names in units' ValidationErrors and ValidationWarnings, and their Values keys,\n" +
			"keep the old slug until each unit is next resolved.",
		forceReason: "units' gate names keep its old slug until they are next resolved",
		resolve: func(ref string) (*renamedEntity, error) {
			d, err := resolveTrigger(ref, selectedSpaceID, "TriggerID,Slug,DisplayName,SpaceID")
			if err != nil {
				return nil, err
			}
			return &renamedEntity{spaceID: d.Trigger.SpaceID, id: d.Trigger.TriggerID, slug: d.Trigger.Slug, displayName: d.Trigger.DisplayName}, nil
		},
		patch: func(e *renamedEntity, patch []byte) (any, error) {
			return patchTrigger(e.spaceID, e.id, patch)
		},
	})
	addEntityRenameCommand(entityRename{
		parent: invocationCmd,
		entity: "invocation",
		resolve: func(ref string) (*renamedEntity, error) {
			d, err := resolveInvocation(ref, selectedSpaceID, "InvocationID,Slug,DisplayName,SpaceID")
			if err != nil {
				return nil, err
			}
			return &renamedEntity{spaceID: d.Invocation.SpaceID, id: d.Invocation.InvocationID, slug: d.Invocation.Slug, displayName: d.Invocation.DisplayName}, nil
		},
		patch: func(e *renamedEntity, patch []byte) (any, error) {
			return patchInvocation(e.spaceID, e.id, patch)
		},
	})
	addEntityRenameCommand(entityRename{
		parent: linkCmd,
		entity: "link",
		resolve: func(ref string) (*renamedEntity, error) {
			d, err := resolveLink(ref, selectedSpaceID, "LinkID,Slug,DisplayName,SpaceID")
			if err != nil {
				return nil, err
			}
			return &renamedEntity{spaceID: d.Link.SpaceID, id: d.Link.LinkID, slug: d.Link.Slug, displayName: d.Link.DisplayName}, nil
		},
		patch: func(e *renamedEntity, patch []byte) (any, error) {
			res, err := cubClientNew.PatchLinkWithBodyWithResponse(ctx, e.spaceID, e.id,
				&goclientnew.PatchLinkParams{DryRun: dryRunParam()}, mergePatchContentType, bytes.NewReader(patch))
			return patchedEntity(res, err, func(r *goclientnew.PatchLinkResponse) *goclientnew.Link { return r.JSON200 })
		},
	})
	addEntityRenameCommand(entityRename{
		parent: filterCmd,
		entity: "filter",
		resolve: func(ref string) (*renamedEntity, error) {
			d, err := resolveFilter(ref, selectedSpaceID, "FilterID,Slug,DisplayName,SpaceID")
			if err != nil {
				return nil, err
			}
			return &renamedEntity{spaceID: d.Filter.SpaceID, id: d.Filter.FilterID, slug: d.Filter.Slug, displayName: d.Filter.DisplayName}, nil
		},
		patch: func(e *renamedEntity, patch []byte) (any, error) {
			return patchFilter(e.spaceID, e.id, patch)
		},
	})
	addEntityRenameCommand(entityRename{
		parent: viewCmd,
		entity: "view",
		resolve: func(ref string) (*renamedEntity, error) {
			d, err := resolveView(ref, selectedSpaceID, "ViewID,Slug,DisplayName,SpaceID")
			if err != nil {
				return nil, err
			}
			return &renamedEntity{spaceID: d.View.SpaceID, id: d.View.ViewID, slug: d.View.Slug, displayName: d.View.DisplayName}, nil
		},
		patch: func(e *renamedEntity, patch []byte) (any, error) {
			return patchView(e.spaceID, e.id, patch)
		},
	})
	addEntityRenameCommand(entityRename{
		parent: tagCmd,
		entity: "tag",
		about: "A tag that a changeset or changeorder made is named after it, and renaming the\n" +
			"changeset or changeorder renames the tag to match.",
		resolve: func(ref string) (*renamedEntity, error) {
			d, err := resolveTag(ref, selectedSpaceID, "TagID,Slug,DisplayName,SpaceID")
			if err != nil {
				return nil, err
			}
			return &renamedEntity{spaceID: d.Tag.SpaceID, id: d.Tag.TagID, slug: d.Tag.Slug, displayName: d.Tag.DisplayName}, nil
		},
		patch: func(e *renamedEntity, patch []byte) (any, error) {
			return patchTag(e.spaceID, e.id, patch)
		},
	})
	addEntityRenameCommand(entityRename{
		parent: changesetCmd,
		entity: "changeset",
		about:  "Its start and end tags, <slug>-start and <slug>-end, are renamed with it.",
		resolve: func(ref string) (*renamedEntity, error) {
			d, err := resolveChangeSet(ref, selectedSpaceID, "ChangeSetID,Slug,DisplayName,SpaceID")
			if err != nil {
				return nil, err
			}
			return &renamedEntity{spaceID: d.ChangeSet.SpaceID, id: d.ChangeSet.ChangeSetID, slug: d.ChangeSet.Slug, displayName: d.ChangeSet.DisplayName}, nil
		},
		patch: func(e *renamedEntity, patch []byte) (any, error) {
			return patchChangeSet(e.spaceID, e.id, patch)
		},
	})
	addEntityRenameCommand(entityRename{
		parent: changeorderCmd,
		entity: "changeorder",
		about: "The tags it made, <slug>-co-start, <slug>-co-end and <slug>-co-restore, are renamed with\n" +
			"it. An end tag it was created with, which another change made, keeps its name.",
		resolve: func(ref string) (*renamedEntity, error) {
			d, err := resolveChangeOrder(ref, selectedSpaceID, "ChangeOrderID,Slug,DisplayName,SpaceID")
			if err != nil {
				return nil, err
			}
			return &renamedEntity{spaceID: d.ChangeOrder.SpaceID, id: d.ChangeOrder.ChangeOrderID, slug: d.ChangeOrder.Slug, displayName: d.ChangeOrder.DisplayName}, nil
		},
		patch: func(e *renamedEntity, patch []byte) (any, error) {
			return patchChangeOrder(e.spaceID, e.id, patch)
		},
	})
	addEntityRenameCommand(entityRename{
		parent: changeworkflowCmd,
		entity: "changeworkflow",
		resolve: func(ref string) (*renamedEntity, error) {
			d, err := resolveChangeWorkflow(ref, selectedSpaceID, "ChangeWorkflowID,Slug,DisplayName,SpaceID")
			if err != nil {
				return nil, err
			}
			return &renamedEntity{spaceID: d.ChangeWorkflow.SpaceID, id: d.ChangeWorkflow.ChangeWorkflowID, slug: d.ChangeWorkflow.Slug, displayName: d.ChangeWorkflow.DisplayName}, nil
		},
		patch: func(e *renamedEntity, patch []byte) (any, error) {
			return patchChangeWorkflow(e.spaceID, e.id, patch)
		},
	})
	addEntityRenameCommand(entityRename{
		parent: workerCmd,
		entity: "worker",
		about:  "A running worker identifies itself by its ID, so it keeps working.",
		resolve: func(ref string) (*renamedEntity, error) {
			d, err := resolveWorker(ref, selectedSpaceID, "BridgeWorkerID,Slug,DisplayName,SpaceID")
			if err != nil {
				return nil, err
			}
			return &renamedEntity{spaceID: d.BridgeWorker.SpaceID, id: d.BridgeWorker.BridgeWorkerID, slug: d.BridgeWorker.Slug, displayName: d.BridgeWorker.DisplayName}, nil
		},
		patch: func(e *renamedEntity, patch []byte) (any, error) {
			res, err := cubClientNew.PatchBridgeWorkerWithBodyWithResponse(ctx, e.spaceID, e.id,
				&goclientnew.PatchBridgeWorkerParams{DryRun: dryRunParam()}, mergePatchContentType, bytes.NewReader(patch))
			return patchedEntity(res, err, func(r *goclientnew.PatchBridgeWorkerResponse) *goclientnew.BridgeWorker { return r.JSON200 })
		},
	})
}
