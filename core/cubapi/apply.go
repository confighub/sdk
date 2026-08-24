// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package cubapi

import (
	"bytes"
	"context"
	"fmt"
	"strings"

	"github.com/cockroachdb/errors"
	"github.com/confighub/sdk/core/configkit/cubkit"
	"github.com/confighub/sdk/core/configkit/yamlkit"
	"github.com/confighub/sdk/core/constants"
	"github.com/confighub/sdk/core/function/api"
	goclientnew "github.com/confighub/sdk/core/openapi/goclient-new"
	"github.com/confighub/sdk/core/third_party/gaby"
	jsonpatch "github.com/evanphx/json-patch"
	"github.com/google/uuid"
	"sigs.k8s.io/yaml"
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
	EntityTypeChangeOrder  = "ChangeOrder"
	EntityTypeTarget       = "Target"
	EntityTypeBridgeWorker = "BridgeWorker"
	EntityTypeUnit         = "Unit"
	EntityTypeLink         = "Link"
	EntityTypeSet          = "Set"
	EntityTypeAttribute    = "Attribute"
	// Pseudo-EntityType
	EntityTypeInventory = "Inventory"
)

// ErrEntityNotFound is used to mark entities that are not found during refresh
var ErrEntityNotFound = errors.New("entity not found")

// Apply processes the configuration data and returns the results and updated inventory.
// If unitSlug and spaceID are non-empty, each applied entity is annotated with the
// managing Unit's slug and SpaceID (confighub.com/UnitSlug, confighub.com/SpaceID),
// analogous to k8skit.EnsureConfigHubContextOnData for the Kubernetes bridge.
func Apply(
	ctx context.Context,
	client *goclientnew.ClientWithResponses,
	data, lastAppliedData []byte,
	oldInventory *Inventory,
	defaultSpaceSlug string,
	unitSlug, spaceID string,
) ([]ApplyResult, *Inventory, error) {

	// Parse the YAML data
	container, err := gaby.ParseAll(data)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to parse YAML: %w", err)
	}

	// Create resource provider
	resourceProvider := cubkit.NewConfigHubResourceProvider()

	// Parse last-applied data if it exists and build a map
	lastAppliedMap := make(map[string]*gaby.YamlDoc)
	if len(lastAppliedData) > 0 {
		lastAppliedContainer, err := gaby.ParseAll(lastAppliedData)
		if err == nil {
			// Build map of entity type and name to YamlDoc
			visitor := func(yamlDoc *gaby.YamlDoc, output any, index int, resourceInfo *api.ResourceInfo) (any, []error) {
				spaceSlug, resourceName := resourceProvider.ParseResourceName(resourceInfo.ResourceName)
				key := fmt.Sprintf("%s:%s/%s", resourceInfo.ResourceType, spaceSlug, resourceName)
				lastAppliedMap[key] = yamlDoc
				return output, nil
			}
			yamlkit.VisitResources(lastAppliedContainer, nil, resourceProvider, visitor)
		}
	}

	// Track results and build new inventory
	var results []ApplyResult
	newInventory := newInventory()

	// Create visitor function
	visitor := func(yamlDoc *gaby.YamlDoc, output any, index int, resourceInfo *api.ResourceInfo) (any, []error) {
		outputData := output.(map[string]interface{})
		resultsList := outputData["results"].([]ApplyResult)
		inventory := outputData["inventory"].(*Inventory)

		spaceSlug, resourceName := resourceProvider.ParseResourceName(resourceInfo.ResourceName)
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
		action, entity, entityID, err := processEntity(ctx, client, yamlDoc, lastAppliedDoc, resourceInfo, spaceSlug, resourceProvider, unitSlug, spaceID)
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

func newInventory() *Inventory {
	newInventory := &Inventory{
		EntityType: EntityTypeInventory,
		Resources:  []InventoryResource{},
	}
	return newInventory
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

// GetRevisionByNum fetches a revision by its RevisionNum for a specific unit
func GetRevisionByNum(ctx context.Context, client *goclientnew.ClientWithResponses, spaceID, unitID string, revisionNum int64) (*goclientnew.ExtendedRevision, error) {
	spaceUUID, err := uuid.Parse(spaceID)
	if err != nil {
		return nil, fmt.Errorf("invalid space ID: %w", err)
	}

	unitUUID, err := uuid.Parse(unitID)
	if err != nil {
		return nil, fmt.Errorf("invalid unit ID: %w", err)
	}

	whereFilter := fmt.Sprintf("RevisionNum = %d", revisionNum)
	params := &goclientnew.ListExtendedRevisionsParams{
		Where: &whereFilter,
	}

	resp, err := client.ListExtendedRevisionsWithResponse(ctx, spaceUUID, unitUUID, params)
	if IsAPIError(err, resp) {
		return nil, InterpretErrorGeneric(err, resp)
	}
	if resp.JSON200 == nil || len(*resp.JSON200) == 0 {
		return nil, fmt.Errorf("revision %d not found for unit %s in space %s", revisionNum, unitID, spaceID)
	}

	// Return the first match (there should only be one)
	extendedRevision := (*resp.JSON200)[0]
	return &extendedRevision, nil
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

// GetEntityBySlug fetches an entity by its slug, similar to the old apiGet* functions
// but using direct client library calls without global variable dependencies
func GetEntityBySlug(ctx context.Context, client *goclientnew.ClientWithResponses, entityType, slug, spaceID string) (interface{}, error) {
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
			return nil, errors.Mark(fmt.Errorf("space %s not found", slug), ErrEntityNotFound)
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
			return nil, errors.Mark(fmt.Errorf("unit %s not found in space %s", slug, spaceID), ErrEntityNotFound)
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
			return nil, errors.Mark(fmt.Errorf("link %s not found in space %s", slug, spaceID), ErrEntityNotFound)
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
			return nil, errors.Mark(fmt.Errorf("view %s not found in space %s", slug, spaceID), ErrEntityNotFound)
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
			return nil, errors.Mark(fmt.Errorf("filter %s not found in space %s", slug, spaceID), ErrEntityNotFound)
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
			return nil, errors.Mark(fmt.Errorf("trigger %s not found in space %s", slug, spaceID), ErrEntityNotFound)
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
			return nil, errors.Mark(fmt.Errorf("invocation %s not found in space %s", slug, spaceID), ErrEntityNotFound)
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
			return nil, errors.Mark(fmt.Errorf("changeset %s not found in space %s", slug, spaceID), ErrEntityNotFound)
		}
		return (*resp.JSON200)[0].ChangeSet, nil

	case EntityTypeChangeOrder:
		spaceUUID := uuid.MustParse(spaceID)
		params := &goclientnew.ListChangeOrdersParams{
			Where: &whereFilter,
		}
		resp, err := client.ListChangeOrdersWithResponse(ctx, spaceUUID, params)
		if IsAPIError(err, resp) {
			return nil, InterpretErrorGeneric(err, resp)
		}
		if resp.JSON200 == nil || len(*resp.JSON200) == 0 {
			return nil, errors.Mark(fmt.Errorf("changeorder %s not found in space %s", slug, spaceID), ErrEntityNotFound)
		}
		return (*resp.JSON200)[0].ChangeOrder, nil

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
			return nil, errors.Mark(fmt.Errorf("tag %s not found in space %s", slug, spaceID), ErrEntityNotFound)
		}
		return (*resp.JSON200)[0].Tag, nil

	case EntityTypeAttribute:
		spaceUUID := uuid.MustParse(spaceID)
		params := &goclientnew.ListAttributesParams{
			Where: &whereFilter,
		}
		resp, err := client.ListAttributesWithResponse(ctx, spaceUUID, params)
		if IsAPIError(err, resp) {
			return nil, InterpretErrorGeneric(err, resp)
		}
		if resp.JSON200 == nil || len(*resp.JSON200) == 0 {
			return nil, errors.Mark(fmt.Errorf("attribute %s not found in space %s", slug, spaceID), ErrEntityNotFound)
		}
		return (*resp.JSON200)[0].Attribute, nil

	default:
		return nil, fmt.Errorf("unsupported entity type: %s", entityType)
	}
}

func processEntity(ctx context.Context, client *goclientnew.ClientWithResponses, yamlDoc *gaby.YamlDoc, lastAppliedDoc *gaby.YamlDoc, resourceInfo *api.ResourceInfo, spaceSlug string, resourceProvider yamlkit.ResourceProvider, unitSlug, spaceID string) (string, interface{}, string, error) {
	entityType := string(resourceInfo.ResourceType)
	entityName := string(resourceInfo.ResourceNameWithoutScope)

	// Annotate the entity with the managing ConfigHub Unit's identity so applied
	// entities carry the same context the Kubernetes bridge stamps on managed
	// resources via k8skit.EnsureConfigHubContextOnData.
	if unitSlug != "" {
		if _, err := yamlDoc.SetP(unitSlug, resourceProvider.ContextPath(constants.UnitSlugKeySuffix)); err != nil {
			return "", nil, "", fmt.Errorf("failed to set UnitSlug annotation: %w", err)
		}
	}
	if spaceID != "" {
		if _, err := yamlDoc.SetP(spaceID, resourceProvider.ContextPath(constants.SpaceIDKeySuffix)); err != nil {
			return "", nil, "", fmt.Errorf("failed to set SpaceID annotation: %w", err)
		}
	}

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
	entitySpaceID, err := getSpaceIDFromSlug(ctx, client, spaceSlug)
	if err != nil {
		return "", nil, "", fmt.Errorf("failed to get space '%s' for %s: %w", spaceSlug, entityName, err)
	}

	// Check if entity exists using the new GetEntityBySlug function
	existingEntity, err := GetEntityBySlug(ctx, client, entityType, entityName, entitySpaceID)
	entityExists := err == nil && existingEntity != nil

	// Create or update the entity
	if entityExists {
		entity, entityID, err := updateEntity(ctx, client, entityType, entitySpaceID, spaceSlug, entityName, existingEntity, jsonData, lastAppliedJSON)
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
		entity, entityID, err := createEntity(ctx, client, entityType, entitySpaceID, spaceSlug, entityName, jsonData)
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
	existingEntity, err := GetEntityBySlug(ctx, client, EntityTypeSpace, spaceName, "")
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

		resp, err := client.PatchSpaceWithBodyWithResponse(ctx, existingSpace.SpaceID, nil, "application/merge-patch+json", bytes.NewReader(patchData))
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
		// A write to a Unit answers with the operation's result, which carries the Unit.
		if resp.JSON200.Unit == nil {
			return nil, "", errors.New("unit create returned no unit")
		}
		return resp.JSON200.Unit, resp.JSON200.Unit.UnitID.String(), nil

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

	case EntityTypeChangeOrder:
		params := &goclientnew.CreateChangeOrderParams{AllowExists: &allowExistsStr}
		resp, err := client.CreateChangeOrderWithBodyWithResponse(ctx, spaceUUID, params, "application/json", bytes.NewReader(jsonData))
		if IsAPIError(err, resp) {
			return nil, "", InterpretErrorGeneric(err, resp)
		}
		return resp.JSON200, resp.JSON200.ChangeOrderID.String(), nil

	case EntityTypeTag:
		params := &goclientnew.CreateTagParams{AllowExists: &allowExistsStr}
		resp, err := client.CreateTagWithBodyWithResponse(ctx, spaceUUID, params, "application/json", bytes.NewReader(jsonData))
		if IsAPIError(err, resp) {
			return nil, "", InterpretErrorGeneric(err, resp)
		}
		return resp.JSON200, resp.JSON200.TagID.String(), nil

	case EntityTypeAttribute:
		params := &goclientnew.CreateAttributeParams{AllowExists: &allowExistsStr}
		resp, err := client.CreateAttributeWithBodyWithResponse(ctx, spaceUUID, params, "application/json", bytes.NewReader(jsonData))
		if IsAPIError(err, resp) {
			return nil, "", InterpretErrorGeneric(err, resp)
		}
		return resp.JSON200, resp.JSON200.AttributeID.String(), nil

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
		case EntityTypeChangeOrder:
			changeOrder := existingEntity.(*goclientnew.ChangeOrder)
			return nil, changeOrder.ChangeOrderID.String(), nil
		case EntityTypeTag:
			tag := existingEntity.(*goclientnew.Tag)
			return nil, tag.TagID.String(), nil
		case EntityTypeAttribute:
			attribute := existingEntity.(*goclientnew.Attribute)
			return nil, attribute.AttributeID.String(), nil
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
		if resp.JSON200.Unit == nil {
			return nil, "", errors.New("unit patch returned no unit")
		}
		return resp.JSON200.Unit, resp.JSON200.Unit.UnitID.String(), nil

	case EntityTypeLink:
		link := existingEntity.(*goclientnew.Link)
		resp, err := client.PatchLinkWithBodyWithResponse(ctx, spaceUUID, link.LinkID, &goclientnew.PatchLinkParams{}, "application/merge-patch+json", bytes.NewReader(patchData))
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

	case EntityTypeAttribute:
		attribute := existingEntity.(*goclientnew.Attribute)
		resp, err := client.PatchAttributeWithBodyWithResponse(ctx, spaceUUID, attribute.AttributeID, "application/merge-patch+json", bytes.NewReader(patchData))
		if IsAPIError(err, resp) {
			return nil, "", InterpretErrorGeneric(err, resp)
		}
		return resp.JSON200, resp.JSON200.AttributeID.String(), nil

	default:
		return nil, "", fmt.Errorf("unsupported entity type for update: %s", entityType)
	}
}

func Destroy(ctx context.Context, client *goclientnew.ClientWithResponses, inventory *Inventory, defaultSpaceSlug string) []ApplyResult {
	return pruneResources(ctx, client, inventory, newInventory(), defaultSpaceSlug)
}

// Refresh gets the current state of entities in the inventory and updates the config data, inventory, and live state.
// It returns the same ApplyResult structure as Apply, along with updated inventory and refreshed config data.
func Refresh(
	ctx context.Context,
	client *goclientnew.ClientWithResponses,
	currentData []byte,
	oldInventory *Inventory,
	defaultSpaceSlug string,
	liveRevisionNum int64,
	spaceID, unitID string,
) ([]ApplyResult, *Inventory, []byte, error) {
	var results []ApplyResult
	newInventory := newInventory()

	// If there's no old inventory, nothing to refresh
	if oldInventory == nil || oldInventory.Resources == nil {
		return results, newInventory, currentData, nil
	}

	// Track successful entities for building LiveState
	type entityWithType struct {
		Entity     interface{}
		EntityType string
		SpaceSlug  string
	}
	var successfulEntities []entityWithType

	// Create resource provider
	resourceProvider := cubkit.NewConfigHubResourceProvider()

	// Process each resource in the inventory
	for _, resource := range oldInventory.Resources {
		spaceSlug, resourceName := resourceProvider.ParseResourceName(api.ResourceName(resource.ResourceName))
		result := ApplyResult{
			EntityType: resource.ResourceType,
			EntityName: resourceName,
			SpaceSlug:  spaceSlug,
		}

		// Get space ID if not a Space entity
		var entitySpaceID string
		if resource.ResourceType != EntityTypeSpace {
			if spaceSlug == "" {
				spaceSlug = defaultSpaceSlug
			}
			result.SpaceSlug = spaceSlug
			var err error
			entitySpaceID, err = getSpaceIDFromSlug(ctx, client, spaceSlug)
			if err != nil {
				result.Action = "failed"
				result.Error = fmt.Errorf("failed to get space '%s': %w", spaceSlug, err)
				results = append(results, result)
				continue
			}
		}

		// Get the current entity state
		entity, err := GetEntityBySlug(ctx, client, resource.ResourceType, resourceName, entitySpaceID)
		if err != nil {
			// If entity is not found, omit it from the new inventory, config data, and live state
			if errors.Is(err, ErrEntityNotFound) {
				result.Action = "deleted"
				result.Error = nil
				// Don't add to new inventory since entity no longer exists
			} else {
				result.Action = "failed"
				result.Error = err
			}
			results = append(results, result)
			continue
		}

		// Entity exists - include it in the refresh
		result.Action = "refreshed"
		result.Entity = entity

		// Extract entity ID based on type
		switch resource.ResourceType {
		case EntityTypeSpace:
			if space, ok := entity.(*goclientnew.Space); ok {
				result.EntityID = space.SpaceID.String()
			}
		case EntityTypeUnit:
			if unit, ok := entity.(*goclientnew.Unit); ok {
				result.EntityID = unit.UnitID.String()
			}
		case EntityTypeLink:
			if link, ok := entity.(*goclientnew.Link); ok {
				result.EntityID = link.LinkID.String()
			}
		case EntityTypeView:
			if view, ok := entity.(*goclientnew.View); ok {
				result.EntityID = view.ViewID.String()
			}
		case EntityTypeFilter:
			if filter, ok := entity.(*goclientnew.Filter); ok {
				result.EntityID = filter.FilterID.String()
			}
		case EntityTypeTrigger:
			if trigger, ok := entity.(*goclientnew.Trigger); ok {
				result.EntityID = trigger.TriggerID.String()
			}
		case EntityTypeInvocation:
			if invocation, ok := entity.(*goclientnew.Invocation); ok {
				result.EntityID = invocation.InvocationID.String()
			}
		case EntityTypeChangeSet:
			if changeSet, ok := entity.(*goclientnew.ChangeSet); ok {
				result.EntityID = changeSet.ChangeSetID.String()
			}
		case EntityTypeTag:
			if tag, ok := entity.(*goclientnew.Tag); ok {
				result.EntityID = tag.TagID.String()
			}
		case EntityTypeAttribute:
			if attribute, ok := entity.(*goclientnew.Attribute); ok {
				result.EntityID = attribute.AttributeID.String()
			}
		}

		results = append(results, result)

		// Add to new inventory and successful entities
		newInventory.Resources = append(newInventory.Resources, InventoryResource{
			ResourceType: resource.ResourceType,
			ResourceName: resource.ResourceName,
		})

		successfulEntities = append(successfulEntities, entityWithType{
			Entity:     entity,
			EntityType: resource.ResourceType,
			SpaceSlug:  spaceSlug,
		})
	}

	// Build the updated config data from refreshed entities
	// Refresh should reflect the current state in ConfigHub, so we rebuild from successful entities
	var updatedConfigData []byte
	if len(successfulEntities) > 0 {
		var configDataDocs []string
		for _, item := range successfulEntities {
			// Use MarshalYAMLWithoutReadonlyFields to filter out readonly fields and add SpaceSlug
			entityYAML, err := MarshalYAMLWithoutReadonlyFields(item.EntityType, item.Entity, item.SpaceSlug)
			if err != nil {
				return nil, nil, nil, fmt.Errorf("failed to marshal entity to YAML: %w", err)
			}
			configDataDocs = append(configDataDocs, strings.TrimSpace(string(entityYAML)))
		}
		// Join all documents with YAML document separator
		updatedConfigData = []byte(strings.Join(configDataDocs, "\n---\n"))
	} else {
		// No entities exist anymore
		updatedConfigData = []byte{}
	}

	return results, newInventory, updatedConfigData, nil
}

func pruneResources(ctx context.Context, client *goclientnew.ClientWithResponses, oldInventory, newInventory *Inventory, defaultSpaceSlug string) []ApplyResult {
	var results []ApplyResult

	// If there's no old inventory, nothing to prune
	if oldInventory == nil || oldInventory.Resources == nil {
		return results
	}

	// Create a map of new resources for quick lookup
	newResources := make(map[string]bool)
	if newInventory != nil && newInventory.Resources != nil {
		for _, resource := range newInventory.Resources {
			key := fmt.Sprintf("%s:%s", resource.ResourceType, resource.ResourceName)
			newResources[key] = true
		}
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
	// Create resource provider
	resourceProvider := cubkit.NewConfigHubResourceProvider()

	spaceSlug, resourceName := resourceProvider.ParseResourceName(api.ResourceName(resource.ResourceName))
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

	// Get the entity to delete using the new GetEntityBySlug function
	existingEntity, err := GetEntityBySlug(ctx, client, resource.ResourceType, resourceName, spaceID)
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

	case EntityTypeAttribute:
		attribute := existingEntity.(*goclientnew.Attribute)
		resp, err := client.DeleteAttributeWithResponse(ctx, spaceUUID, attribute.AttributeID)
		if IsAPIError(err, resp) {
			result.Error = InterpretErrorGeneric(err, resp)
			return result
		}
		result.Action = "deleted"
		result.EntityID = attribute.AttributeID.String()
		result.Entity = resp
	}

	return result
}

// MarshalYAMLWithoutReadonlyFields marshals an entity to YAML while excluding readonly fields.
// It uses the Patch types which contain only the mutable fields that should be included in LiveState.
// It also removes null fields, the Version field, and adds SpaceSlug for space-resident entities.
func MarshalYAMLWithoutReadonlyFields(entityType string, entity interface{}, spaceSlug string) ([]byte, error) {
	// Helper function to clean up the map representation
	cleanupMap := func(data map[string]interface{}, isSpaceResident bool, entType string) map[string]interface{} {
		cleaned := make(map[string]interface{})

		for key, value := range data {
			// Skip Version field
			if key == "Version" {
				continue
			}

			// Skip null values
			if value == nil {
				continue
			}

			// Handle nested maps (like Labels, Annotations, DeleteGates, etc.)
			if mapValue, ok := value.(map[string]interface{}); ok {
				cleanedMap := make(map[string]interface{})
				hasNonNullValue := false
				for k, v := range mapValue {
					if v != nil {
						cleanedMap[k] = v
						hasNonNullValue = true
					}
				}
				// Only include the map if it has non-null values
				if hasNonNullValue {
					cleaned[key] = cleanedMap
				}
			} else {
				cleaned[key] = value
			}
		}

		// Add SpaceSlug for space-resident entities (not for Space itself)
		if isSpaceResident && spaceSlug != "" && entType != EntityTypeSpace {
			cleaned["SpaceSlug"] = spaceSlug
		}

		return cleaned
	}

	// Determine if entity is space-resident
	isSpaceResident := entityType != EntityTypeSpace && entityType != EntityTypeInventory

	// First marshal the entity to get into a map form
	var entityMap map[string]interface{}
	yamlData, err := yaml.Marshal(entity)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal entity to YAML: %w", err)
	}
	if err := yaml.Unmarshal(yamlData, &entityMap); err != nil {
		return nil, fmt.Errorf("failed to unmarshal entity to map: %w", err)
	}

	// Based on the entity type, unmarshal into the appropriate Patch type, then to map for cleaning
	switch entityType {
	case EntityTypeUnit:
		var patchUnit goclientnew.PatchUnitApplicationMergePatchPlusJSONBody
		if err := yaml.Unmarshal(yamlData, &patchUnit); err != nil {
			return nil, fmt.Errorf("failed to unmarshal Unit to patch type: %w", err)
		}
		// Marshal to map for cleaning
		patchData, _ := yaml.Marshal(patchUnit)
		var patchMap map[string]interface{}
		yaml.Unmarshal(patchData, &patchMap)
		// We should only keep metadata fields, not Data
		delete(patchMap, "Data")
		return yaml.Marshal(cleanupMap(patchMap, isSpaceResident, entityType))

	case EntityTypeSpace:
		var patchSpace goclientnew.PatchSpaceApplicationMergePatchPlusJSONBody
		if err := yaml.Unmarshal(yamlData, &patchSpace); err != nil {
			return nil, fmt.Errorf("failed to unmarshal Space to patch type: %w", err)
		}
		patchData, _ := yaml.Marshal(patchSpace)
		var patchMap map[string]interface{}
		yaml.Unmarshal(patchData, &patchMap)
		return yaml.Marshal(cleanupMap(patchMap, isSpaceResident, entityType))

	case EntityTypeTarget:
		var patchTarget goclientnew.PatchTargetApplicationMergePatchPlusJSONBody
		if err := yaml.Unmarshal(yamlData, &patchTarget); err != nil {
			return nil, fmt.Errorf("failed to unmarshal Target to patch type: %w", err)
		}
		patchData, _ := yaml.Marshal(patchTarget)
		var patchMap map[string]interface{}
		yaml.Unmarshal(patchData, &patchMap)
		return yaml.Marshal(cleanupMap(patchMap, isSpaceResident, entityType))

	case EntityTypeBridgeWorker:
		var patchBridgeWorker goclientnew.PatchBridgeWorkerApplicationMergePatchPlusJSONBody
		if err := yaml.Unmarshal(yamlData, &patchBridgeWorker); err != nil {
			return nil, fmt.Errorf("failed to unmarshal BridgeWorker to patch type: %w", err)
		}
		patchData, _ := yaml.Marshal(patchBridgeWorker)
		var patchMap map[string]interface{}
		yaml.Unmarshal(patchData, &patchMap)
		return yaml.Marshal(cleanupMap(patchMap, isSpaceResident, entityType))

	case EntityTypeChangeSet:
		var patchChangeSet goclientnew.PatchChangeSetApplicationMergePatchPlusJSONBody
		if err := yaml.Unmarshal(yamlData, &patchChangeSet); err != nil {
			return nil, fmt.Errorf("failed to unmarshal ChangeSet to patch type: %w", err)
		}
		patchData, _ := yaml.Marshal(patchChangeSet)
		var patchMap map[string]interface{}
		yaml.Unmarshal(patchData, &patchMap)
		return yaml.Marshal(cleanupMap(patchMap, isSpaceResident, entityType))

	case EntityTypeFilter:
		var patchFilter goclientnew.PatchFilterApplicationMergePatchPlusJSONBody
		if err := yaml.Unmarshal(yamlData, &patchFilter); err != nil {
			return nil, fmt.Errorf("failed to unmarshal Filter to patch type: %w", err)
		}
		patchData, _ := yaml.Marshal(patchFilter)
		var patchMap map[string]interface{}
		yaml.Unmarshal(patchData, &patchMap)
		return yaml.Marshal(cleanupMap(patchMap, isSpaceResident, entityType))

	case EntityTypeInvocation:
		var patchInvocation goclientnew.PatchInvocationApplicationMergePatchPlusJSONBody
		if err := yaml.Unmarshal(yamlData, &patchInvocation); err != nil {
			return nil, fmt.Errorf("failed to unmarshal Invocation to patch type: %w", err)
		}
		patchData, _ := yaml.Marshal(patchInvocation)
		var patchMap map[string]interface{}
		yaml.Unmarshal(patchData, &patchMap)
		return yaml.Marshal(cleanupMap(patchMap, isSpaceResident, entityType))

	case EntityTypeLink:
		var patchLink goclientnew.PatchLinkApplicationMergePatchPlusJSONBody
		if err := yaml.Unmarshal(yamlData, &patchLink); err != nil {
			return nil, fmt.Errorf("failed to unmarshal Link to patch type: %w", err)
		}
		patchData, _ := yaml.Marshal(patchLink)
		var patchMap map[string]interface{}
		yaml.Unmarshal(patchData, &patchMap)
		return yaml.Marshal(cleanupMap(patchMap, isSpaceResident, entityType))

	case EntityTypeTag:
		var patchTag goclientnew.PatchTagApplicationMergePatchPlusJSONBody
		if err := yaml.Unmarshal(yamlData, &patchTag); err != nil {
			return nil, fmt.Errorf("failed to unmarshal Tag to patch type: %w", err)
		}
		patchData, _ := yaml.Marshal(patchTag)
		var patchMap map[string]interface{}
		yaml.Unmarshal(patchData, &patchMap)
		return yaml.Marshal(cleanupMap(patchMap, isSpaceResident, entityType))

	case EntityTypeTrigger:
		var patchTrigger goclientnew.PatchTriggerApplicationMergePatchPlusJSONBody
		if err := yaml.Unmarshal(yamlData, &patchTrigger); err != nil {
			return nil, fmt.Errorf("failed to unmarshal Trigger to patch type: %w", err)
		}
		patchData, _ := yaml.Marshal(patchTrigger)
		var patchMap map[string]interface{}
		yaml.Unmarshal(patchData, &patchMap)
		return yaml.Marshal(cleanupMap(patchMap, isSpaceResident, entityType))

	case EntityTypeView:
		var patchView goclientnew.PatchViewApplicationMergePatchPlusJSONBody
		if err := yaml.Unmarshal(yamlData, &patchView); err != nil {
			return nil, fmt.Errorf("failed to unmarshal View to patch type: %w", err)
		}
		patchData, _ := yaml.Marshal(patchView)
		var patchMap map[string]interface{}
		yaml.Unmarshal(patchData, &patchMap)
		return yaml.Marshal(cleanupMap(patchMap, isSpaceResident, entityType))

	case EntityTypeAttribute:
		var patchAttribute goclientnew.PatchAttributeApplicationMergePatchPlusJSONBody
		if err := yaml.Unmarshal(yamlData, &patchAttribute); err != nil {
			return nil, fmt.Errorf("failed to unmarshal Attribute to patch type: %w", err)
		}
		patchData, _ := yaml.Marshal(patchAttribute)
		var patchMap map[string]interface{}
		yaml.Unmarshal(patchData, &patchMap)
		return yaml.Marshal(cleanupMap(patchMap, isSpaceResident, entityType))

	case EntityTypeSet:
		// Set doesn't have a Patch type, but we still want to clean it up
		return yaml.Marshal(cleanupMap(entityMap, isSpaceResident, entityType))

	case EntityTypeInventory:
		// Inventory is a pseudo-entity that doesn't need filtering
		return yamlData, nil

	default:
		// For unknown entity types, still clean up the map
		return yaml.Marshal(cleanupMap(entityMap, isSpaceResident, entityType))
	}
}
