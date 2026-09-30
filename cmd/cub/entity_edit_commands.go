// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"bytes"
	"fmt"

	goclientnew "github.com/confighub/sdk/core/openapi/goclient-new"
)

// The edit commands of the entity types that can have backing Units; see addEntityEditCommand.
func init() {
	addEntityEditCommand(entityEdit{
		parent: triggerCmd,
		entity: "trigger",
		resolve: func(ref string) (*editedEntity, error) {
			d, err := resolveTrigger(ref, selectedSpaceID, "TriggerID,Slug,SpaceID,BackingUnitID")
			if err != nil {
				return nil, err
			}
			return &editedEntity{spaceID: d.Trigger.SpaceID, id: d.Trigger.TriggerID, slug: d.Trigger.Slug, backingUnitID: d.Trigger.BackingUnitID}, nil
		},
		getDocument: func(e *editedEntity) (*goclientnew.EntityDocument, error) {
			res, err := cubClientNew.GetTriggerDocumentWithResponse(ctx, e.spaceID, e.id)
			return readDocument(res, err, func(r *goclientnew.GetTriggerDocumentResponse) *goclientnew.EntityDocument { return r.JSON200 })
		},
		updateDocument: func(e *editedEntity, edit goclientnew.EntityDocumentEdit) (bool, error) {
			res, err := cubClientNew.UpdateTriggerDocumentWithResponse(ctx, e.spaceID, e.id, &goclientnew.UpdateTriggerDocumentParams{}, edit)
			return documentUpdated("trigger", res, err,
				func(r *goclientnew.UpdateTriggerDocumentResponse) *goclientnew.Trigger { return r.JSON200 },
				func(x *goclientnew.Trigger) (string, string) { return x.Slug, x.TriggerID.String() }, displayTriggerDetails)
		},
		applyFromBackingUnit: func(e *editedEntity) error {
			where := fmt.Sprintf("TriggerID = '%s'", e.id)
			res, err := cubClientNew.BulkPatchTriggersWithBodyWithResponse(ctx,
				&goclientnew.BulkPatchTriggersParams{Where: &where, FromBackingUnits: fromBackingUnitsTrue()},
				"application/merge-patch+json", bytes.NewReader([]byte("{}")))
			return appliedFromBackingUnit("trigger", where, res, err,
				func(r *goclientnew.BulkPatchTriggersResponse) (*[]goclientnew.TriggerCreateOrUpdateResponse, *[]goclientnew.TriggerCreateOrUpdateResponse) {
					return r.JSON200, r.JSON207
				},
				func(r *goclientnew.TriggerCreateOrUpdateResponse) *goclientnew.ResponseError { return r.Error },
				func(r *goclientnew.TriggerCreateOrUpdateResponse) string {
					if r.Trigger == nil {
						return ""
					}
					return fmt.Sprintf("%s (ID: %s)", r.Trigger.Slug, r.Trigger.TriggerID)
				})
		},
	})
	addEntityEditCommand(entityEdit{
		parent: filterCmd,
		entity: "filter",
		resolve: func(ref string) (*editedEntity, error) {
			d, err := resolveFilter(ref, selectedSpaceID, "FilterID,Slug,SpaceID,BackingUnitID")
			if err != nil {
				return nil, err
			}
			return &editedEntity{spaceID: d.Filter.SpaceID, id: d.Filter.FilterID, slug: d.Filter.Slug, backingUnitID: d.Filter.BackingUnitID}, nil
		},
		getDocument: func(e *editedEntity) (*goclientnew.EntityDocument, error) {
			res, err := cubClientNew.GetFilterDocumentWithResponse(ctx, e.spaceID, e.id)
			return readDocument(res, err, func(r *goclientnew.GetFilterDocumentResponse) *goclientnew.EntityDocument { return r.JSON200 })
		},
		updateDocument: func(e *editedEntity, edit goclientnew.EntityDocumentEdit) (bool, error) {
			res, err := cubClientNew.UpdateFilterDocumentWithResponse(ctx, e.spaceID, e.id, &goclientnew.UpdateFilterDocumentParams{}, edit)
			return documentUpdated("filter", res, err,
				func(r *goclientnew.UpdateFilterDocumentResponse) *goclientnew.Filter { return r.JSON200 },
				func(x *goclientnew.Filter) (string, string) { return x.Slug, x.FilterID.String() }, displayFilterDetails)
		},
		applyFromBackingUnit: func(e *editedEntity) error {
			where := fmt.Sprintf("FilterID = '%s'", e.id)
			res, err := cubClientNew.BulkPatchFiltersWithBodyWithResponse(ctx,
				&goclientnew.BulkPatchFiltersParams{Where: &where, FromBackingUnits: fromBackingUnitsTrue()},
				"application/merge-patch+json", bytes.NewReader([]byte("{}")))
			return appliedFromBackingUnit("filter", where, res, err,
				func(r *goclientnew.BulkPatchFiltersResponse) (*[]goclientnew.FilterCreateOrUpdateResponse, *[]goclientnew.FilterCreateOrUpdateResponse) {
					return r.JSON200, r.JSON207
				},
				func(r *goclientnew.FilterCreateOrUpdateResponse) *goclientnew.ResponseError { return r.Error },
				func(r *goclientnew.FilterCreateOrUpdateResponse) string {
					if r.Filter == nil {
						return ""
					}
					return fmt.Sprintf("%s (ID: %s)", r.Filter.Slug, r.Filter.FilterID)
				})
		},
	})
	addEntityEditCommand(entityEdit{
		parent: viewCmd,
		entity: "view",
		resolve: func(ref string) (*editedEntity, error) {
			d, err := resolveView(ref, selectedSpaceID, "ViewID,Slug,SpaceID,BackingUnitID")
			if err != nil {
				return nil, err
			}
			return &editedEntity{spaceID: d.View.SpaceID, id: d.View.ViewID, slug: d.View.Slug, backingUnitID: d.View.BackingUnitID}, nil
		},
		getDocument: func(e *editedEntity) (*goclientnew.EntityDocument, error) {
			res, err := cubClientNew.GetViewDocumentWithResponse(ctx, e.spaceID, e.id)
			return readDocument(res, err, func(r *goclientnew.GetViewDocumentResponse) *goclientnew.EntityDocument { return r.JSON200 })
		},
		updateDocument: func(e *editedEntity, edit goclientnew.EntityDocumentEdit) (bool, error) {
			res, err := cubClientNew.UpdateViewDocumentWithResponse(ctx, e.spaceID, e.id, &goclientnew.UpdateViewDocumentParams{}, edit)
			return documentUpdated("view", res, err,
				func(r *goclientnew.UpdateViewDocumentResponse) *goclientnew.View { return r.JSON200 },
				func(x *goclientnew.View) (string, string) { return x.Slug, x.ViewID.String() }, displayViewDetails)
		},
		applyFromBackingUnit: func(e *editedEntity) error {
			where := fmt.Sprintf("ViewID = '%s'", e.id)
			res, err := cubClientNew.BulkPatchViewsWithBodyWithResponse(ctx,
				&goclientnew.BulkPatchViewsParams{Where: &where, FromBackingUnits: fromBackingUnitsTrue()},
				"application/merge-patch+json", bytes.NewReader([]byte("{}")))
			return appliedFromBackingUnit("view", where, res, err,
				func(r *goclientnew.BulkPatchViewsResponse) (*[]goclientnew.ViewCreateOrUpdateResponse, *[]goclientnew.ViewCreateOrUpdateResponse) {
					return r.JSON200, r.JSON207
				},
				func(r *goclientnew.ViewCreateOrUpdateResponse) *goclientnew.ResponseError { return r.Error },
				func(r *goclientnew.ViewCreateOrUpdateResponse) string {
					if r.View == nil {
						return ""
					}
					return fmt.Sprintf("%s (ID: %s)", r.View.Slug, r.View.ViewID)
				})
		},
	})
	addEntityEditCommand(entityEdit{
		parent: invocationCmd,
		entity: "invocation",
		resolve: func(ref string) (*editedEntity, error) {
			d, err := resolveInvocation(ref, selectedSpaceID, "InvocationID,Slug,SpaceID,BackingUnitID")
			if err != nil {
				return nil, err
			}
			return &editedEntity{spaceID: d.Invocation.SpaceID, id: d.Invocation.InvocationID, slug: d.Invocation.Slug, backingUnitID: d.Invocation.BackingUnitID}, nil
		},
		getDocument: func(e *editedEntity) (*goclientnew.EntityDocument, error) {
			res, err := cubClientNew.GetInvocationDocumentWithResponse(ctx, e.spaceID, e.id)
			return readDocument(res, err, func(r *goclientnew.GetInvocationDocumentResponse) *goclientnew.EntityDocument { return r.JSON200 })
		},
		updateDocument: func(e *editedEntity, edit goclientnew.EntityDocumentEdit) (bool, error) {
			res, err := cubClientNew.UpdateInvocationDocumentWithResponse(ctx, e.spaceID, e.id, &goclientnew.UpdateInvocationDocumentParams{}, edit)
			return documentUpdated("invocation", res, err,
				func(r *goclientnew.UpdateInvocationDocumentResponse) *goclientnew.Invocation { return r.JSON200 },
				func(x *goclientnew.Invocation) (string, string) { return x.Slug, x.InvocationID.String() }, displayInvocationDetails)
		},
		applyFromBackingUnit: func(e *editedEntity) error {
			where := fmt.Sprintf("InvocationID = '%s'", e.id)
			res, err := cubClientNew.BulkPatchInvocationsWithBodyWithResponse(ctx,
				&goclientnew.BulkPatchInvocationsParams{Where: &where, FromBackingUnits: fromBackingUnitsTrue()},
				"application/merge-patch+json", bytes.NewReader([]byte("{}")))
			return appliedFromBackingUnit("invocation", where, res, err,
				func(r *goclientnew.BulkPatchInvocationsResponse) (*[]goclientnew.InvocationCreateOrUpdateResponse, *[]goclientnew.InvocationCreateOrUpdateResponse) {
					return r.JSON200, r.JSON207
				},
				func(r *goclientnew.InvocationCreateOrUpdateResponse) *goclientnew.ResponseError { return r.Error },
				func(r *goclientnew.InvocationCreateOrUpdateResponse) string {
					if r.Invocation == nil {
						return ""
					}
					return fmt.Sprintf("%s (ID: %s)", r.Invocation.Slug, r.Invocation.InvocationID)
				})
		},
	})
	addEntityEditCommand(entityEdit{
		parent: attributeCmd,
		entity: "attribute",
		resolve: func(ref string) (*editedEntity, error) {
			d, err := resolveAttribute(ref, selectedSpaceID, "AttributeID,Slug,SpaceID,BackingUnitID")
			if err != nil {
				return nil, err
			}
			return &editedEntity{spaceID: d.Attribute.SpaceID, id: d.Attribute.AttributeID, slug: d.Attribute.Slug, backingUnitID: d.Attribute.BackingUnitID}, nil
		},
		getDocument: func(e *editedEntity) (*goclientnew.EntityDocument, error) {
			res, err := cubClientNew.GetAttributeDocumentWithResponse(ctx, e.spaceID, e.id)
			return readDocument(res, err, func(r *goclientnew.GetAttributeDocumentResponse) *goclientnew.EntityDocument { return r.JSON200 })
		},
		updateDocument: func(e *editedEntity, edit goclientnew.EntityDocumentEdit) (bool, error) {
			res, err := cubClientNew.UpdateAttributeDocumentWithResponse(ctx, e.spaceID, e.id, &goclientnew.UpdateAttributeDocumentParams{}, edit)
			return documentUpdated("attribute", res, err,
				func(r *goclientnew.UpdateAttributeDocumentResponse) *goclientnew.Attribute { return r.JSON200 },
				func(x *goclientnew.Attribute) (string, string) { return x.Slug, x.AttributeID.String() }, displayAttributeDetails)
		},
		applyFromBackingUnit: func(e *editedEntity) error {
			where := fmt.Sprintf("AttributeID = '%s'", e.id)
			res, err := cubClientNew.BulkPatchAttributesWithBodyWithResponse(ctx,
				&goclientnew.BulkPatchAttributesParams{Where: &where, FromBackingUnits: fromBackingUnitsTrue()},
				"application/merge-patch+json", bytes.NewReader([]byte("{}")))
			return appliedFromBackingUnit("attribute", where, res, err,
				func(r *goclientnew.BulkPatchAttributesResponse) (*[]goclientnew.AttributeCreateOrUpdateResponse, *[]goclientnew.AttributeCreateOrUpdateResponse) {
					return r.JSON200, r.JSON207
				},
				func(r *goclientnew.AttributeCreateOrUpdateResponse) *goclientnew.ResponseError { return r.Error },
				func(r *goclientnew.AttributeCreateOrUpdateResponse) string {
					if r.Attribute == nil {
						return ""
					}
					return fmt.Sprintf("%s (ID: %s)", r.Attribute.Slug, r.Attribute.AttributeID)
				})
		},
	})
	addEntityEditCommand(entityEdit{
		parent: linkCmd,
		entity: "link",
		resolve: func(ref string) (*editedEntity, error) {
			d, err := resolveLink(ref, selectedSpaceID, "LinkID,Slug,SpaceID,BackingUnitID")
			if err != nil {
				return nil, err
			}
			return &editedEntity{spaceID: d.Link.SpaceID, id: d.Link.LinkID, slug: d.Link.Slug, backingUnitID: d.Link.BackingUnitID}, nil
		},
		getDocument: func(e *editedEntity) (*goclientnew.EntityDocument, error) {
			res, err := cubClientNew.GetLinkDocumentWithResponse(ctx, e.spaceID, e.id)
			return readDocument(res, err, func(r *goclientnew.GetLinkDocumentResponse) *goclientnew.EntityDocument { return r.JSON200 })
		},
		updateDocument: func(e *editedEntity, edit goclientnew.EntityDocumentEdit) (bool, error) {
			res, err := cubClientNew.UpdateLinkDocumentWithResponse(ctx, e.spaceID, e.id, &goclientnew.UpdateLinkDocumentParams{}, edit)
			return documentUpdated("link", res, err,
				func(r *goclientnew.UpdateLinkDocumentResponse) *goclientnew.Link { return r.JSON200 },
				func(x *goclientnew.Link) (string, string) { return x.Slug, x.LinkID.String() }, displayLinkDetails)
		},
		applyFromBackingUnit: func(e *editedEntity) error {
			where := fmt.Sprintf("LinkID = '%s'", e.id)
			res, err := cubClientNew.BulkPatchLinksWithBodyWithResponse(ctx,
				&goclientnew.BulkPatchLinksParams{Where: &where, FromBackingUnits: fromBackingUnitsTrue()},
				"application/merge-patch+json", bytes.NewReader([]byte("{}")))
			return appliedFromBackingUnit("link", where, res, err,
				func(r *goclientnew.BulkPatchLinksResponse) (*[]goclientnew.LinkCreateOrUpdateResponse, *[]goclientnew.LinkCreateOrUpdateResponse) {
					return r.JSON200, r.JSON207
				},
				func(r *goclientnew.LinkCreateOrUpdateResponse) *goclientnew.ResponseError { return r.Error },
				func(r *goclientnew.LinkCreateOrUpdateResponse) string {
					if r.Link == nil {
						return ""
					}
					return fmt.Sprintf("%s (ID: %s)", r.Link.Slug, r.Link.LinkID)
				})
		},
	})
	addEntityEditCommand(entityEdit{
		parent: changeworkflowCmd,
		entity: "change workflow",
		resolve: func(ref string) (*editedEntity, error) {
			d, err := resolveChangeWorkflow(ref, selectedSpaceID, "ChangeWorkflowID,Slug,SpaceID,BackingUnitID")
			if err != nil {
				return nil, err
			}
			return &editedEntity{spaceID: d.ChangeWorkflow.SpaceID, id: d.ChangeWorkflow.ChangeWorkflowID, slug: d.ChangeWorkflow.Slug, backingUnitID: d.ChangeWorkflow.BackingUnitID}, nil
		},
		getDocument: func(e *editedEntity) (*goclientnew.EntityDocument, error) {
			res, err := cubClientNew.GetChangeWorkflowDocumentWithResponse(ctx, e.spaceID, e.id)
			return readDocument(res, err, func(r *goclientnew.GetChangeWorkflowDocumentResponse) *goclientnew.EntityDocument { return r.JSON200 })
		},
		updateDocument: func(e *editedEntity, edit goclientnew.EntityDocumentEdit) (bool, error) {
			res, err := cubClientNew.UpdateChangeWorkflowDocumentWithResponse(ctx, e.spaceID, e.id, &goclientnew.UpdateChangeWorkflowDocumentParams{}, edit)
			return documentUpdated("change workflow", res, err,
				func(r *goclientnew.UpdateChangeWorkflowDocumentResponse) *goclientnew.ChangeWorkflow {
					return r.JSON200
				},
				func(x *goclientnew.ChangeWorkflow) (string, string) { return x.Slug, x.ChangeWorkflowID.String() }, displayChangeWorkflowDetails)
		},
		applyFromBackingUnit: func(e *editedEntity) error {
			where := fmt.Sprintf("ChangeWorkflowID = '%s'", e.id)
			res, err := cubClientNew.BulkPatchChangeWorkflowsWithBodyWithResponse(ctx,
				&goclientnew.BulkPatchChangeWorkflowsParams{Where: &where, FromBackingUnits: fromBackingUnitsTrue()},
				"application/merge-patch+json", bytes.NewReader([]byte("{}")))
			return appliedFromBackingUnit("change workflow", where, res, err,
				func(r *goclientnew.BulkPatchChangeWorkflowsResponse) (*[]goclientnew.ChangeWorkflowCreateOrUpdateResponse, *[]goclientnew.ChangeWorkflowCreateOrUpdateResponse) {
					return r.JSON200, r.JSON207
				},
				func(r *goclientnew.ChangeWorkflowCreateOrUpdateResponse) *goclientnew.ResponseError { return r.Error },
				func(r *goclientnew.ChangeWorkflowCreateOrUpdateResponse) string {
					if r.ChangeWorkflow == nil {
						return ""
					}
					return fmt.Sprintf("%s (ID: %s)", r.ChangeWorkflow.Slug, r.ChangeWorkflow.ChangeWorkflowID)
				})
		},
	})
	addEntityEditCommand(entityEdit{
		parent:   spaceCmd,
		entity:   "space",
		orgLevel: true,
		resolve: func(ref string) (*editedEntity, error) {
			d, err := resolveSpace(ref, "SpaceID,Slug,BackingUnitID")
			if err != nil {
				return nil, err
			}
			return &editedEntity{spaceID: d.Space.SpaceID, id: d.Space.SpaceID, slug: d.Space.Slug, backingUnitID: d.Space.BackingUnitID}, nil
		},
		getDocument: func(e *editedEntity) (*goclientnew.EntityDocument, error) {
			res, err := cubClientNew.GetSpaceDocumentWithResponse(ctx, e.id)
			return readDocument(res, err, func(r *goclientnew.GetSpaceDocumentResponse) *goclientnew.EntityDocument { return r.JSON200 })
		},
		updateDocument: func(e *editedEntity, edit goclientnew.EntityDocumentEdit) (bool, error) {
			res, err := cubClientNew.UpdateSpaceDocumentWithResponse(ctx, e.id, &goclientnew.UpdateSpaceDocumentParams{}, edit)
			return documentUpdated("space", res, err,
				func(r *goclientnew.UpdateSpaceDocumentResponse) *goclientnew.Space { return r.JSON200 },
				func(x *goclientnew.Space) (string, string) { return x.Slug, x.SpaceID.String() }, displaySpaceDetails)
		},
		applyFromBackingUnit: func(e *editedEntity) error {
			where := fmt.Sprintf("SpaceID = '%s'", e.id)
			res, err := cubClientNew.BulkPatchSpacesWithBodyWithResponse(ctx,
				&goclientnew.BulkPatchSpacesParams{Where: &where, FromBackingUnits: fromBackingUnitsTrue()},
				"application/merge-patch+json", bytes.NewReader([]byte("{}")))
			return appliedFromBackingUnit("space", where, res, err,
				func(r *goclientnew.BulkPatchSpacesResponse) (*[]goclientnew.SpaceCreateOrUpdateResponse, *[]goclientnew.SpaceCreateOrUpdateResponse) {
					return r.JSON200, r.JSON207
				},
				func(r *goclientnew.SpaceCreateOrUpdateResponse) *goclientnew.ResponseError { return r.Error },
				func(r *goclientnew.SpaceCreateOrUpdateResponse) string {
					if r.Space == nil {
						return ""
					}
					return fmt.Sprintf("%s (ID: %s)", r.Space.Slug, r.Space.SpaceID)
				})
		},
	})
	addEntityEditCommand(entityEdit{
		parent:   componentCmd,
		entity:   "component",
		orgLevel: true,
		resolve: func(ref string) (*editedEntity, error) {
			d, err := resolveComponent(ref, "ComponentID,Slug,BackingUnitID")
			if err != nil {
				return nil, err
			}
			return &editedEntity{id: d.Component.ComponentID, slug: d.Component.Slug, backingUnitID: d.Component.BackingUnitID}, nil
		},
		getDocument: func(e *editedEntity) (*goclientnew.EntityDocument, error) {
			res, err := cubClientNew.GetComponentDocumentWithResponse(ctx, e.id)
			return readDocument(res, err, func(r *goclientnew.GetComponentDocumentResponse) *goclientnew.EntityDocument { return r.JSON200 })
		},
		updateDocument: func(e *editedEntity, edit goclientnew.EntityDocumentEdit) (bool, error) {
			res, err := cubClientNew.UpdateComponentDocumentWithResponse(ctx, e.id, &goclientnew.UpdateComponentDocumentParams{}, edit)
			return documentUpdated("component", res, err,
				func(r *goclientnew.UpdateComponentDocumentResponse) *goclientnew.Component { return r.JSON200 },
				func(x *goclientnew.Component) (string, string) { return x.Slug, x.ComponentID.String() }, displayComponentEntityDetails)
		},
		applyFromBackingUnit: func(e *editedEntity) error {
			where := fmt.Sprintf("ComponentID = '%s'", e.id)
			res, err := cubClientNew.BulkPatchComponentsWithBodyWithResponse(ctx,
				&goclientnew.BulkPatchComponentsParams{Where: &where, FromBackingUnits: fromBackingUnitsTrue()},
				"application/merge-patch+json", bytes.NewReader([]byte("{}")))
			return appliedFromBackingUnit("component", where, res, err,
				func(r *goclientnew.BulkPatchComponentsResponse) (*[]goclientnew.ComponentCreateOrUpdateResponse, *[]goclientnew.ComponentCreateOrUpdateResponse) {
					return r.JSON200, r.JSON207
				},
				func(r *goclientnew.ComponentCreateOrUpdateResponse) *goclientnew.ResponseError { return r.Error },
				func(r *goclientnew.ComponentCreateOrUpdateResponse) string {
					if r.Component == nil {
						return ""
					}
					return fmt.Sprintf("%s (ID: %s)", r.Component.Slug, r.Component.ComponentID)
				})
		},
	})
}

// fromBackingUnitsTrue is from_backing_units=true, for an apply from a backing Unit.
func fromBackingUnitsTrue() *bool {
	fromBackingUnits := true
	return &fromBackingUnits
}
