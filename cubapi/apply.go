// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package cubapi

import (
	"bytes"
	"context"
	"fmt"
	"strings"

	"github.com/confighub/sdk/configkit/cubkit"
	"github.com/confighub/sdk/configkit/yamlkit"
	"github.com/confighub/sdk/function/api"
	goclientnew "github.com/confighub/sdk/openapi/goclient-new"
	"github.com/confighub/sdk/third_party/gaby"
	jsonpatch "github.com/evanphx/json-patch"
	"github.com/google/uuid"
)

// Entity types
const (
	EntityTypeSpace        = "Space"
	EntityTypeFilter       = "Filter"
	EntityTypeView         = "View"
	EntityTypeInvocation   = "Invocation"
	EntityTypeTrigger      = "Trigger"
	EntityTypeTag          = "Tag"
	EntityTypeChangeSet    = "ChangeSet"
	EntityTypeTarget       = "Target"
	EntityTypeBridgeWorker = "BridgeWorker"
	EntityTypeUnit         = "Unit"
	EntityTypeLink         = "Link"
	EntityTypeSet          = "Set"
	// Pseudo-EntityType
	EntityTypeInventory = "Inventory"
)

// Apply processes the configuration data and returns the results and updated inventory.
func Apply(
	ctx context.Context,
	client *goclientnew.ClientWithResponses,
	data, lastAppliedData []byte,
	oldInventory *Inventory,
	defaultSpaceSlug string,
) ([]ApplyResult, *Inventory, error) {

	// Parse the YAML data
	container, err := gaby.ParseAll(data)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to parse YAML: %w", err)
	}

	// Parse last-applied data if it exists and build a map
	lastAppliedMap := make(map[string]*gaby.YamlDoc)
	if len(lastAppliedData) > 0 {
		lastAppliedContainer, err := gaby.ParseAll(lastAppliedData)
		if err == nil {
			// Build map of entity type and name to YamlDoc
			visitor := func(yamlDoc *gaby.YamlDoc, output any, index int, resourceInfo *api.ResourceInfo) (any, []error) {
				spaceSlug, resourceName := cubkit.ConfigHubResourceProvider.ParseResourceName(resourceInfo.ResourceName)
				key := fmt.Sprintf("%s:%s/%s", resourceInfo.ResourceType, spaceSlug, resourceName)
				lastAppliedMap[key] = yamlDoc
				return output, nil
			}
			yamlkit.VisitResources(lastAppliedContainer, nil, cubkit.ConfigHubResourceProvider, visitor)
		}
	}

	// Create resource provider
	resourceProvider := cubkit.ConfigHubResourceProvider

	// Track results and build new inventory
	var results []ApplyResult
	newInventory := &Inventory{
		EntityType: EntityTypeInventory,
		Resources:  []InventoryResource{},
	}

	// Create visitor function
	visitor := func(yamlDoc *gaby.YamlDoc, output any, index int, resourceInfo *api.ResourceInfo) (any, []error) {
		outputData := output.(map[string]interface{})
		resultsList := outputData["results"].([]ApplyResult)
		inventory := outputData["inventory"].(*Inventory)

		spaceSlug, resourceName := cubkit.ConfigHubResourceProvider.ParseResourceName(resourceInfo.ResourceName)
		result := ApplyResult{
			EntityType: string(resourceInfo.ResourceType),
			EntityName: resourceName,
		}

		// Get space slug for inventory tracking
		if resourceInfo.ResourceType != EntityTypeSpace {
			if spaceSlug == "" {
				spaceSlug = defaultSpaceSlug
			}
			result.SpaceSlug = spaceSlug
		}

		// Get last-applied YamlDoc if it exists
		// Try multiple key combinations since space might be inferred
		keys := []string{
			fmt.Sprintf("%s:%s/%s", resourceInfo.ResourceType, spaceSlug, resourceName),
			fmt.Sprintf("%s:/%s", resourceInfo.ResourceType, resourceName), // For entities without explicit space
		}
		var lastAppliedDoc *gaby.YamlDoc
		for _, key := range keys {
			if doc := lastAppliedMap[key]; doc != nil {
				lastAppliedDoc = doc
				break
			}
		}

		// Process the entity
		action, entity, entityID, err := processEntity(ctx, client, yamlDoc, lastAppliedDoc, resourceInfo, spaceSlug)
		if err != nil {
			result.Action = "failed"
			result.Error = err
		} else {
			result.Action = action
			result.Entity = entity
			result.EntityID = entityID

			// Add to new inventory on success (including unchanged)
			if action != "failed" {
				inventory.Resources = append(inventory.Resources, InventoryResource{
					ResourceType: string(resourceInfo.ResourceType),
					ResourceName: spaceSlug + "/" + resourceName, // ok even if spaceSlug is empty
				})
			}
		}

		resultsList = append(resultsList, result)
		outputData["results"] = resultsList
		return outputData, nil // Continue processing even if one entity fails
	}

	// Visit all resources
	initialOutput := map[string]interface{}{
		"results":   results,
		"inventory": newInventory,
	}
	output, err := yamlkit.VisitResources(container, initialOutput, resourceProvider, visitor)
	if err != nil {
		// Some entities failed, but we still want to report what we can
		if outputData, ok := output.(map[string]interface{}); ok {
			if resultsList, ok := outputData["results"].([]ApplyResult); ok {
				results = resultsList
			}
			if inventory, ok := outputData["inventory"].(*Inventory); ok {
				newInventory = inventory
			}
		}
	} else if outputData, ok := output.(map[string]interface{}); ok {
		if resultsList, ok := outputData["results"].([]ApplyResult); ok {
			results = resultsList
		}
		if inventory, ok := outputData["inventory"].(*Inventory); ok {
			newInventory = inventory
		}
	}

	// Prune resources that were in old inventory but not in new inventory
	pruneResults := pruneResources(ctx, client, oldInventory, newInventory, defaultSpaceSlug)
	results = append(results, pruneResults...)

	// Update inventory with failed deletions (keep resources that failed to delete)
	for _, result := range pruneResults {
		if result.Action == "failed" && result.Error != nil {
			// Add back to inventory if deletion failed for reasons other than not found
			newInventory.Resources = append(newInventory.Resources, InventoryResource{
				ResourceType: result.EntityType,
				ResourceName: result.SpaceSlug + "/" + result.EntityName, // ok even if SpaceSlug is empty
			})
		}
	}

	return results, newInventory, nil
}

type ApplyResult struct {
	EntityType string
	EntityName string
	EntityID   string
	SpaceSlug  string
	Action     string // "created", "updated", "deleted", "failed"
	Error      error
	Entity     interface{} // Store the created/updated entity for display
}

type InventoryResource struct {
	ResourceType string `yaml:"ResourceType"`
	ResourceName string `yaml:"ResourceName"`
}

type Inventory struct {
	EntityType string              `yaml:"EntityType"`
	Resources  []InventoryResource `yaml:"Resources"`
}

// getSpaceIDFromSlug fetches only the SpaceID of a space by its slug
// This is used when we only need the SpaceID and not the full space object
func getSpaceIDFromSlug(ctx context.Context, client *goclientnew.ClientWithResponses, slug string) (string, error) {
	whereFilter := "Slug = '" + slug + "'"
	selectParam := "SpaceID,Slug"
	params := &goclientnew.ListSpacesParams{
		Where:  &whereFilter,
		Select: &selectParam,
	}
	resp, err := client.ListSpacesWithResponse(ctx, params)
	if IsAPIError(err, resp) {
		return "", InterpretErrorGeneric(err, resp)
	}
	if resp.JSON200 == nil || len(*resp.JSON200) == 0 {
		return "", fmt.Errorf("space %s not found", slug)
	}
	return (*resp.JSON200)[0].Space.SpaceID.String(), nil
}

// getEntityBySlug fetches an entity by its slug, similar to the old apiGet* functions
// but using direct client library calls without global variable dependencies
func getEntityBySlug(ctx context.Context, client *goclientnew.ClientWithResponses, entityType, slug, spaceID string) (interface{}, error) {
	// For apply operations, we need all fields (no select parameter)
	// This is the default behavior when no Select parameter is provided

	// Build the where filter for slug matching
	whereFilter := "Slug = '" + slug + "'"

	switch entityType {
	case EntityTypeSpace:
		// Spaces don't reside in other spaces, so we list at organization level
		params := &goclientnew.ListSpacesParams{
			Where: &whereFilter,
		}
		resp, err := client.ListSpacesWithResponse(ctx, params)
		if IsAPIError(err, resp) {
			return nil, InterpretErrorGeneric(err, resp)
		}
		if resp.JSON200 == nil || len(*resp.JSON200) == 0 {
			return nil, fmt.Errorf("space %s not found", slug)
		}
		return (*resp.JSON200)[0].Space, nil

	case EntityTypeUnit:
		spaceUUID := uuid.MustParse(spaceID)
		params := &goclientnew.ListUnitsParams{
			Where: &whereFilter,
		}
		resp, err := client.ListUnitsWithResponse(ctx, spaceUUID, params)
		if IsAPIError(err, resp) {
			return nil, InterpretErrorGeneric(err, resp)
		}
		if resp.JSON200 == nil || len(*resp.JSON200) == 0 {
			return nil, fmt.Errorf("unit %s not found in space %s", slug, spaceID)
		}
		return (*resp.JSON200)[0].Unit, nil

	case EntityTypeLink:
		spaceUUID := uuid.MustParse(spaceID)
		params := &goclientnew.ListLinksParams{
			Where: &whereFilter,
		}
		resp, err := client.ListLinksWithResponse(ctx, spaceUUID, params)
		if IsAPIError(err, resp) {
			return nil, InterpretErrorGeneric(err, resp)
		}
		if resp.JSON200 == nil || len(*resp.JSON200) == 0 {
			return nil, fmt.Errorf("link %s not found in space %s", slug, spaceID)
		}
		return (*resp.JSON200)[0].Link, nil

	case EntityTypeView:
		spaceUUID := uuid.MustParse(spaceID)
		params := &goclientnew.ListViewsParams{
			Where: &whereFilter,
		}
		resp, err := client.ListViewsWithResponse(ctx, spaceUUID, params)
		if IsAPIError(err, resp) {
			return nil, InterpretErrorGeneric(err, resp)
		}
		if resp.JSON200 == nil || len(*resp.JSON200) == 0 {
			return nil, fmt.Errorf("view %s not found in space %s", slug, spaceID)
		}
		return (*resp.JSON200)[0].View, nil

	case EntityTypeFilter:
		spaceUUID := uuid.MustParse(spaceID)
		params := &goclientnew.ListFiltersParams{
			Where: &whereFilter,
		}
		resp, err := client.ListFiltersWithResponse(ctx, spaceUUID, params)
		if IsAPIError(err, resp) {
			return nil, InterpretErrorGeneric(err, resp)
		}
		if resp.JSON200 == nil || len(*resp.JSON200) == 0 {
			return nil, fmt.Errorf("filter %s not found in space %s", slug, spaceID)
		}
		return (*resp.JSON200)[0].Filter, nil

	case EntityTypeTrigger:
		spaceUUID := uuid.MustParse(spaceID)
		params := &goclientnew.ListTriggersParams{
			Where: &whereFilter,
		}
		resp, err := client.ListTriggersWithResponse(ctx, spaceUUID, params)
		if IsAPIError(err, resp) {
			return nil, InterpretErrorGeneric(err, resp)
		}
		if resp.JSON200 == nil || len(*resp.JSON200) == 0 {
			return nil, fmt.Errorf("trigger %s not found in space %s", slug, spaceID)
		}
		return (*resp.JSON200)[0].Trigger, nil

	case EntityTypeInvocation:
		spaceUUID := uuid.MustParse(spaceID)
		params := &goclientnew.ListInvocationsParams{
			Where: &whereFilter,
		}
		resp, err := client.ListInvocationsWithResponse(ctx, spaceUUID, params)
		if IsAPIError(err, resp) {
			return nil, InterpretErrorGeneric(err, resp)
		}
		if resp.JSON200 == nil || len(*resp.JSON200) == 0 {
			return nil, fmt.Errorf("invocation %s not found in space %s", slug, spaceID)
		}
		return (*resp.JSON200)[0].Invocation, nil

	case EntityTypeChangeSet:
		spaceUUID := uuid.MustParse(spaceID)
		params := &goclientnew.ListChangeSetsParams{
			Where: &whereFilter,
		}
		resp, err := client.ListChangeSetsWithResponse(ctx, spaceUUID, params)
		if IsAPIError(err, resp) {
			return nil, InterpretErrorGeneric(err, resp)
		}
		if resp.JSON200 == nil || len(*resp.JSON200) == 0 {
			return nil, fmt.Errorf("changeset %s not found in space %s", slug, spaceID)
		}
		return (*resp.JSON200)[0].ChangeSet, nil

	case EntityTypeTag:
		spaceUUID := uuid.MustParse(spaceID)
		params := &goclientnew.ListTagsParams{
			Where: &whereFilter,
		}
		resp, err := client.ListTagsWithResponse(ctx, spaceUUID, params)
		if IsAPIError(err, resp) {
			return nil, InterpretErrorGeneric(err, resp)
		}
		if resp.JSON200 == nil || len(*resp.JSON200) == 0 {
			return nil, fmt.Errorf("tag %s not found in space %s", slug, spaceID)
		}
		return (*resp.JSON200)[0].Tag, nil

	default:
		return nil, fmt.Errorf("unsupported entity type: %s", entityType)
	}
}

func processEntity(ctx context.Context, client *goclientnew.ClientWithResponses, yamlDoc *gaby.YamlDoc, lastAppliedDoc *gaby.YamlDoc, resourceInfo *api.ResourceInfo, spaceSlug string) (string, interface{}, string, error) {
	entityType := string(resourceInfo.ResourceType)
	entityName := string(resourceInfo.ResourceNameWithoutScope)

	// Marshal the YAML document to JSON
	jsonData, err := yamlDoc.MarshalJSON()
	if err != nil {
		return "", nil, "", fmt.Errorf("failed to marshal entity data: %w", err)
	}

	// Get last-applied JSON if available
	var lastAppliedJSON []byte
	if lastAppliedDoc != nil {
		lastAppliedJSON, _ = lastAppliedDoc.MarshalJSON()
	}

	// Handle Space entity specially (doesn't reside in a space)
	if entityType == EntityTypeSpace {
		action, entity, entityID, err := processSpace(ctx, client, jsonData, entityName, lastAppliedJSON)
		return action, entity, entityID, err
	}

	// Get the space ID for this entity
	spaceID, err := getSpaceIDFromSlug(ctx, client, spaceSlug)
	if err != nil {
		return "", nil, "", fmt.Errorf("failed to get space '%s' for %s: %w", spaceSlug, entityName, err)
	}

	// Check if entity exists using the new getEntityBySlug function
	existingEntity, err := getEntityBySlug(ctx, client, entityType, entityName, spaceID)
	entityExists := err == nil && existingEntity != nil

	// Create or update the entity
	if entityExists {
		entity, entityID, err := updateEntity(ctx, client, entityType, spaceID, spaceSlug, entityName, existingEntity, jsonData, lastAppliedJSON)
		if err != nil {
			return "", nil, "", err
		}
		// Check if entity was actually changed
		if entity == nil {
			// Return existing entity for unchanged status
			return "unchanged", existingEntity, entityID, nil
		}
		return "updated", entity, entityID, nil
	} else {
		entity, entityID, err := createEntity(ctx, client, entityType, spaceID, spaceSlug, entityName, jsonData)
		if err != nil {
			return "", nil, "", err
		}
		return "created", entity, entityID, nil
	}
}

// isPatchEmpty checks if a JSON merge patch is empty (no changes)
func isPatchEmpty(patch []byte) bool {
	if len(patch) == 0 {
		return true
	}
	patchStr := string(patch)
	return patchStr == "null" || patchStr == "{}" || patchStr == "{\n}"
}

func processSpace(ctx context.Context, client *goclientnew.ClientWithResponses, jsonData []byte, spaceName string, lastAppliedJSON []byte) (string, interface{}, string, error) {
	// Check if space exists
	existingEntity, err := getEntityBySlug(ctx, client, EntityTypeSpace, spaceName, "")
	existingSpace, _ := existingEntity.(*goclientnew.Space)
	spaceExists := err == nil && existingSpace != nil

	if spaceExists {
		// Update space using patch
		var patchData []byte
		if len(lastAppliedJSON) > 0 {
			patchData, err = jsonpatch.CreateMergePatch(lastAppliedJSON, jsonData)
			if err != nil {
				return "", nil, "", fmt.Errorf("failed to create merge patch: %w", err)
			}
		} else {
			patchData = jsonData
		}

		// Check if patch is empty
		if isPatchEmpty(patchData) {
			return "unchanged", existingSpace, existingSpace.SpaceID.String(), nil
		}

		resp, err := client.PatchSpaceWithBodyWithResponse(ctx, existingSpace.SpaceID, "application/merge-patch+json", bytes.NewReader(patchData))
		if IsAPIError(err, resp) {
			return "", nil, "", InterpretErrorGeneric(err, resp)
		}
		return "updated", resp.JSON200, resp.JSON200.SpaceID.String(), nil
	} else {
		// Create space
		allowExistsParam := "true"
		params := &goclientnew.CreateSpaceParams{
			AllowExists: &allowExistsParam,
		}

		resp, err := client.CreateSpaceWithBodyWithResponse(ctx, params, "application/json", bytes.NewReader(jsonData))
		if IsAPIError(err, resp) {
			return "", nil, "", InterpretErrorGeneric(err, resp)
		}
		return "created", resp.JSON200, resp.JSON200.SpaceID.String(), nil
	}
}

func createEntity(ctx context.Context, client *goclientnew.ClientWithResponses, entityType, spaceID, spaceSlug, entityName string, jsonData []byte) (interface{}, string, error) {
	spaceUUID, err := uuid.Parse(spaceID)
	if err != nil {
		return nil, "", fmt.Errorf("invalid space ID: %w", err)
	}

	allowExistsStr := "true"

	switch entityType {
	case EntityTypeUnit:
		params := &goclientnew.CreateUnitParams{AllowExists: &allowExistsStr}
		resp, err := client.CreateUnitWithBodyWithResponse(ctx, spaceUUID, params, "application/json", bytes.NewReader(jsonData))
		if IsAPIError(err, resp) {
			return nil, "", InterpretErrorGeneric(err, resp)
		}
		return resp.JSON200, resp.JSON200.UnitID.String(), nil

	case EntityTypeLink:
		params := &goclientnew.CreateLinkParams{AllowExists: &allowExistsStr}
		resp, err := client.CreateLinkWithBodyWithResponse(ctx, spaceUUID, params, "application/json", bytes.NewReader(jsonData))
		if IsAPIError(err, resp) {
			return nil, "", InterpretErrorGeneric(err, resp)
		}
		return resp.JSON200, resp.JSON200.LinkID.String(), nil

	case EntityTypeView:
		params := &goclientnew.CreateViewParams{AllowExists: &allowExistsStr}
		resp, err := client.CreateViewWithBodyWithResponse(ctx, spaceUUID, params, "application/json", bytes.NewReader(jsonData))
		if IsAPIError(err, resp) {
			return nil, "", InterpretErrorGeneric(err, resp)
		}
		return resp.JSON200, resp.JSON200.ViewID.String(), nil

	case EntityTypeFilter:
		params := &goclientnew.CreateFilterParams{AllowExists: &allowExistsStr}
		resp, err := client.CreateFilterWithBodyWithResponse(ctx, spaceUUID, params, "application/json", bytes.NewReader(jsonData))
		if IsAPIError(err, resp) {
			return nil, "", InterpretErrorGeneric(err, resp)
		}
		return resp.JSON200, resp.JSON200.FilterID.String(), nil

	case EntityTypeTrigger:
		params := &goclientnew.CreateTriggerParams{AllowExists: &allowExistsStr}
		resp, err := client.CreateTriggerWithBodyWithResponse(ctx, spaceUUID, params, "application/json", bytes.NewReader(jsonData))
		if IsAPIError(err, resp) {
			return nil, "", InterpretErrorGeneric(err, resp)
		}
		return resp.JSON200, resp.JSON200.TriggerID.String(), nil

	case EntityTypeInvocation:
		params := &goclientnew.CreateInvocationParams{AllowExists: &allowExistsStr}
		resp, err := client.CreateInvocationWithBodyWithResponse(ctx, spaceUUID, params, "application/json", bytes.NewReader(jsonData))
		if IsAPIError(err, resp) {
			return nil, "", InterpretErrorGeneric(err, resp)
		}
		return resp.JSON200, resp.JSON200.InvocationID.String(), nil

	case EntityTypeChangeSet:
		params := &goclientnew.CreateChangeSetParams{AllowExists: &allowExistsStr}
		resp, err := client.CreateChangeSetWithBodyWithResponse(ctx, spaceUUID, params, "application/json", bytes.NewReader(jsonData))
		if IsAPIError(err, resp) {
			return nil, "", InterpretErrorGeneric(err, resp)
		}
		return resp.JSON200, resp.JSON200.ChangeSetID.String(), nil

	case EntityTypeTag:
		params := &goclientnew.CreateTagParams{AllowExists: &allowExistsStr}
		resp, err := client.CreateTagWithBodyWithResponse(ctx, spaceUUID, params, "application/json", bytes.NewReader(jsonData))
		if IsAPIError(err, resp) {
			return nil, "", InterpretErrorGeneric(err, resp)
		}
		return resp.JSON200, resp.JSON200.TagID.String(), nil

	default:
		return nil, "", fmt.Errorf("unsupported entity type for create: %s", entityType)
	}
}

func updateEntity(ctx context.Context, client *goclientnew.ClientWithResponses, entityType, spaceID, spaceSlug, entityName string, existingEntity interface{}, newData []byte, lastAppliedJSON []byte) (interface{}, string, error) {
	spaceUUID, err := uuid.Parse(spaceID)
	if err != nil {
		return nil, "", fmt.Errorf("invalid space ID: %w", err)
	}

	// Create patch data
	var patchData []byte
	if len(lastAppliedJSON) > 0 {
		patchData, err = jsonpatch.CreateMergePatch(lastAppliedJSON, newData)
		if err != nil {
			return nil, "", fmt.Errorf("failed to create merge patch: %w", err)
		}
	} else {
		patchData = newData
	}

	// Check if patch is empty - if so, no update needed
	if isPatchEmpty(patchData) {
		// Return nil entity to indicate unchanged status
		switch entityType {
		case EntityTypeUnit:
			unit := existingEntity.(*goclientnew.Unit)
			return nil, unit.UnitID.String(), nil
		case EntityTypeLink:
			link := existingEntity.(*goclientnew.Link)
			return nil, link.LinkID.String(), nil
		case EntityTypeView:
			view := existingEntity.(*goclientnew.View)
			return nil, view.ViewID.String(), nil
		case EntityTypeFilter:
			filter := existingEntity.(*goclientnew.Filter)
			return nil, filter.FilterID.String(), nil
		case EntityTypeTrigger:
			trigger := existingEntity.(*goclientnew.Trigger)
			return nil, trigger.TriggerID.String(), nil
		case EntityTypeInvocation:
			invocation := existingEntity.(*goclientnew.Invocation)
			return nil, invocation.InvocationID.String(), nil
		case EntityTypeChangeSet:
			changeSet := existingEntity.(*goclientnew.ChangeSet)
			return nil, changeSet.ChangeSetID.String(), nil
		case EntityTypeTag:
			tag := existingEntity.(*goclientnew.Tag)
			return nil, tag.TagID.String(), nil
		}
	}

	switch entityType {
	case EntityTypeUnit:
		unit := existingEntity.(*goclientnew.Unit)
		params := &goclientnew.PatchUnitParams{}
		resp, err := client.PatchUnitWithBodyWithResponse(ctx, spaceUUID, unit.UnitID, params, "application/merge-patch+json", bytes.NewReader(patchData))
		if IsAPIError(err, resp) {
			return nil, "", InterpretErrorGeneric(err, resp)
		}
		return resp.JSON200, resp.JSON200.UnitID.String(), nil

	case EntityTypeLink:
		link := existingEntity.(*goclientnew.Link)
		resp, err := client.PatchLinkWithBodyWithResponse(ctx, spaceUUID, link.LinkID, "application/merge-patch+json", bytes.NewReader(patchData))
		if IsAPIError(err, resp) {
			return nil, "", InterpretErrorGeneric(err, resp)
		}
		return resp.JSON200, resp.JSON200.LinkID.String(), nil

	case EntityTypeView:
		view := existingEntity.(*goclientnew.View)
		resp, err := client.PatchViewWithBodyWithResponse(ctx, spaceUUID, view.ViewID, "application/merge-patch+json", bytes.NewReader(patchData))
		if IsAPIError(err, resp) {
			return nil, "", InterpretErrorGeneric(err, resp)
		}
		return resp.JSON200, resp.JSON200.ViewID.String(), nil

	case EntityTypeFilter:
		filter := existingEntity.(*goclientnew.Filter)
		resp, err := client.PatchFilterWithBodyWithResponse(ctx, spaceUUID, filter.FilterID, "application/merge-patch+json", bytes.NewReader(patchData))
		if IsAPIError(err, resp) {
			return nil, "", InterpretErrorGeneric(err, resp)
		}
		return resp.JSON200, resp.JSON200.FilterID.String(), nil

	case EntityTypeTrigger:
		trigger := existingEntity.(*goclientnew.Trigger)
		resp, err := client.PatchTriggerWithBodyWithResponse(ctx, spaceUUID, trigger.TriggerID, "application/merge-patch+json", bytes.NewReader(patchData))
		if IsAPIError(err, resp) {
			return nil, "", InterpretErrorGeneric(err, resp)
		}
		return resp.JSON200, resp.JSON200.TriggerID.String(), nil

	case EntityTypeInvocation:
		invocation := existingEntity.(*goclientnew.Invocation)
		resp, err := client.PatchInvocationWithBodyWithResponse(ctx, spaceUUID, invocation.InvocationID, "application/merge-patch+json", bytes.NewReader(patchData))
		if IsAPIError(err, resp) {
			return nil, "", InterpretErrorGeneric(err, resp)
		}
		return resp.JSON200, resp.JSON200.InvocationID.String(), nil

	case EntityTypeChangeSet:
		changeSet := existingEntity.(*goclientnew.ChangeSet)
		resp, err := client.PatchChangeSetWithBodyWithResponse(ctx, spaceUUID, changeSet.ChangeSetID, "application/merge-patch+json", bytes.NewReader(patchData))
		if IsAPIError(err, resp) {
			return nil, "", InterpretErrorGeneric(err, resp)
		}
		return resp.JSON200, resp.JSON200.ChangeSetID.String(), nil

	case EntityTypeTag:
		tag := existingEntity.(*goclientnew.Tag)
		resp, err := client.PatchTagWithBodyWithResponse(ctx, spaceUUID, tag.TagID, "application/merge-patch+json", bytes.NewReader(patchData))
		if IsAPIError(err, resp) {
			return nil, "", InterpretErrorGeneric(err, resp)
		}
		return resp.JSON200, resp.JSON200.TagID.String(), nil

	default:
		return nil, "", fmt.Errorf("unsupported entity type for update: %s", entityType)
	}
}

func pruneResources(ctx context.Context, client *goclientnew.ClientWithResponses, oldInventory, newInventory *Inventory, defaultSpaceSlug string) []ApplyResult {
	var results []ApplyResult

	// Create a map of new resources for quick lookup
	newResources := make(map[string]bool)
	for _, resource := range newInventory.Resources {
		key := fmt.Sprintf("%s:%s", resource.ResourceType, resource.ResourceName)
		newResources[key] = true
	}

	// Check each resource in old inventory
	for _, resource := range oldInventory.Resources {
		key := fmt.Sprintf("%s:%s", resource.ResourceType, resource.ResourceName)
		if !newResources[key] {
			// Resource was removed, delete it
			result := deleteResource(ctx, client, resource, defaultSpaceSlug)
			results = append(results, result)
		}
	}

	return results
}

func deleteResource(ctx context.Context, client *goclientnew.ClientWithResponses, resource InventoryResource, defaultSpaceSlug string) ApplyResult {
	spaceSlug, resourceName := cubkit.ConfigHubResourceProvider.ParseResourceName(api.ResourceName(resource.ResourceName))
	result := ApplyResult{
		EntityType: resource.ResourceType,
		EntityName: resourceName,
		SpaceSlug:  spaceSlug,
		Action:     "failed",
	}

	// Get space ID if not a Space entity
	var spaceID string
	if resource.ResourceType != EntityTypeSpace {
		if spaceSlug == "" {
			spaceSlug = defaultSpaceSlug
		}
		var err error
		spaceID, err = getSpaceIDFromSlug(ctx, client, spaceSlug)
		if err != nil {
			result.Error = fmt.Errorf("failed to get space '%s': %w", spaceSlug, err)
			return result
		}
	}

	// Get the entity to delete using the new getEntityBySlug function
	existingEntity, err := getEntityBySlug(ctx, client, resource.ResourceType, resourceName, spaceID)
	if err != nil {
		// If entity is not found, consider it already deleted (success)
		if strings.Contains(err.Error(), "not found") || strings.Contains(err.Error(), "404") {
			result.Action = "deleted"
			return result
		}
		result.Error = err
		return result
	}

	// Parse spaceID to UUID if not a Space entity
	var spaceUUID uuid.UUID
	if resource.ResourceType != EntityTypeSpace {
		spaceUUID, err = uuid.Parse(spaceID)
		if err != nil {
			result.Error = fmt.Errorf("invalid space ID: %w", err)
			return result
		}
	}

	// Delete the entity based on type and store response
	switch resource.ResourceType {
	case EntityTypeSpace:
		space := existingEntity.(*goclientnew.Space)
		resp, err := client.DeleteSpaceWithResponse(ctx, space.SpaceID, &goclientnew.DeleteSpaceParams{})
		if IsAPIError(err, resp) {
			result.Error = InterpretErrorGeneric(err, resp)
			return result
		}
		result.Action = "deleted"
		result.EntityID = space.SpaceID.String()
		result.Entity = resp

	case EntityTypeUnit:
		unit := existingEntity.(*goclientnew.Unit)
		resp, err := client.DeleteUnitWithResponse(ctx, spaceUUID, unit.UnitID)
		if IsAPIError(err, resp) {
			result.Error = InterpretErrorGeneric(err, resp)
			return result
		}
		result.Action = "deleted"
		result.EntityID = unit.UnitID.String()
		result.Entity = resp

	case EntityTypeLink:
		link := existingEntity.(*goclientnew.Link)
		resp, err := client.DeleteLinkWithResponse(ctx, spaceUUID, link.LinkID)
		if IsAPIError(err, resp) {
			result.Error = InterpretErrorGeneric(err, resp)
			return result
		}
		result.Action = "deleted"
		result.EntityID = link.LinkID.String()
		result.Entity = resp

	case EntityTypeView:
		view := existingEntity.(*goclientnew.View)
		resp, err := client.DeleteViewWithResponse(ctx, spaceUUID, view.ViewID)
		if IsAPIError(err, resp) {
			result.Error = InterpretErrorGeneric(err, resp)
			return result
		}
		result.Action = "deleted"
		result.EntityID = view.ViewID.String()
		result.Entity = resp

	case EntityTypeFilter:
		filter := existingEntity.(*goclientnew.Filter)
		resp, err := client.DeleteFilterWithResponse(ctx, spaceUUID, filter.FilterID)
		if IsAPIError(err, resp) {
			result.Error = InterpretErrorGeneric(err, resp)
			return result
		}
		result.Action = "deleted"
		result.EntityID = filter.FilterID.String()
		result.Entity = resp

	case EntityTypeTrigger:
		trigger := existingEntity.(*goclientnew.Trigger)
		resp, err := client.DeleteTriggerWithResponse(ctx, spaceUUID, trigger.TriggerID)
		if IsAPIError(err, resp) {
			result.Error = InterpretErrorGeneric(err, resp)
			return result
		}
		result.Action = "deleted"
		result.EntityID = trigger.TriggerID.String()
		result.Entity = resp

	case EntityTypeInvocation:
		invocation := existingEntity.(*goclientnew.Invocation)
		resp, err := client.DeleteInvocationWithResponse(ctx, spaceUUID, invocation.InvocationID)
		if IsAPIError(err, resp) {
			result.Error = InterpretErrorGeneric(err, resp)
			return result
		}
		result.Action = "deleted"
		result.EntityID = invocation.InvocationID.String()
		result.Entity = resp

	case EntityTypeChangeSet:
		changeSet := existingEntity.(*goclientnew.ChangeSet)
		resp, err := client.DeleteChangeSetWithResponse(ctx, spaceUUID, changeSet.ChangeSetID)
		if IsAPIError(err, resp) {
			result.Error = InterpretErrorGeneric(err, resp)
			return result
		}
		result.Action = "deleted"
		result.EntityID = changeSet.ChangeSetID.String()
		result.Entity = resp

	case EntityTypeTag:
		tag := existingEntity.(*goclientnew.Tag)
		resp, err := client.DeleteTagWithResponse(ctx, spaceUUID, tag.TagID)
		if IsAPIError(err, resp) {
			result.Error = InterpretErrorGeneric(err, resp)
			return result
		}
		result.Action = "deleted"
		result.EntityID = tag.TagID.String()
		result.Entity = resp
	}

	return result
}
