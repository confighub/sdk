// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"encoding/json"
	"fmt"
	"strings"

	goclientnew "github.com/confighub/sdk/core/openapi/goclient-new"
	"github.com/google/uuid"
	"gopkg.in/yaml.v3"
)

// parsePermissions parses permission strings in the format "Action:UserIDOrUsername" and populates a Permissions object.
// Use "-Action:UserIDOrUsername" to remove a user from a permission (the user is removed from the UserIDs map).
func parsePermissions(permissionStrs []string, permissions *goclientnew.Permissions) error {
	if len(permissionStrs) == 0 {
		return nil
	}

	if *permissions == nil {
		*permissions = make(goclientnew.Permissions)
	}

	for _, permStr := range permissionStrs {
		// Check for removal prefix
		isRemoval := strings.HasPrefix(permStr, "-")
		if isRemoval {
			permStr = permStr[1:] // Strip the "-" prefix
		}

		parts := strings.SplitN(permStr, ":", 2)
		if len(parts) != 2 {
			return fmt.Errorf("invalid permission format %q, expected Action:UserIDOrUsername or -Action:UserIDOrUsername", permStr)
		}

		action := parts[0]
		userIdentifier := parts[1]

		// Try to parse as UUID first
		userID, err := uuid.Parse(userIdentifier)
		if err != nil {
			// Not a UUID, try to look up by username
			user, err := apiGetUserFromUsername(userIdentifier)
			if err != nil {
				return fmt.Errorf("failed to find user %q: %w", userIdentifier, err)
			}
			userID = user.UserID
		}

		// Get or create the subjects for this action
		subjects, ok := (*permissions)[action]
		if !ok {
			subjects = goclientnew.Subjects{}
		}

		// Initialize the UserIDs map if needed
		if subjects.UserIDs == nil {
			subjects.UserIDs = make(map[string]bool)
		}

		if isRemoval {
			// Remove the user ID from the map
			delete(subjects.UserIDs, userID.String())
		} else {
			// Add the user ID to the map
			subjects.UserIDs[userID.String()] = true
		}

		(*permissions)[action] = subjects
	}

	return nil
}

// parsePermissionsIntoPatchMap parses permission strings in the format "Action:UserIDOrUsername"
// and adds them to a generic map structure for use in JSON patches.
// Use "-Action:UserIDOrUsername" to remove a user from a permission (sets null in the patch for JSON Merge Patch).
// This is used by the patch operations where we're building a generic map[string]interface{}.
func parsePermissionsIntoPatchMap(permissionStrs []string, permissionsMap map[string]interface{}) error {
	if len(permissionStrs) == 0 {
		return nil
	}

	for _, permStr := range permissionStrs {
		// Check for removal prefix
		isRemoval := strings.HasPrefix(permStr, "-")
		if isRemoval {
			permStr = permStr[1:] // Strip the "-" prefix
		}

		parts := strings.SplitN(permStr, ":", 2)
		if len(parts) != 2 {
			return fmt.Errorf("invalid permission format %q, expected Action:UserIDOrUsername or -Action:UserIDOrUsername", permStr)
		}

		action := parts[0]
		userIdentifier := parts[1]

		// Try to parse as UUID first
		userID, err := uuid.Parse(userIdentifier)
		if err != nil {
			// Not a UUID, try to look up by username
			user, err := apiGetUserFromUsername(userIdentifier)
			if err != nil {
				return fmt.Errorf("failed to find user %q: %w", userIdentifier, err)
			}
			userID = user.UserID
		}

		// Get or create the subjects for this action
		var subjects map[string]interface{}
		if existingSubjects, ok := permissionsMap[action]; ok {
			if subjectsMap, ok := existingSubjects.(map[string]interface{}); ok {
				subjects = subjectsMap
			} else {
				subjects = make(map[string]interface{})
			}
		} else {
			subjects = make(map[string]interface{})
		}

		// Get or create the UserIDs map
		var userIDs map[string]interface{}
		if existingUserIDs, ok := subjects["UserIDs"]; ok {
			if userIDsMap, ok := existingUserIDs.(map[string]interface{}); ok {
				userIDs = userIDsMap
			} else {
				userIDs = make(map[string]interface{})
			}
		} else {
			userIDs = make(map[string]interface{})
		}

		userIDStr := userID.String()
		if isRemoval {
			// Mark for removal by setting to null in JSON Merge Patch
			userIDs[userIDStr] = nil
		} else {
			// Add the user ID to the map
			userIDs[userIDStr] = true
		}

		subjects["UserIDs"] = userIDs
		permissionsMap[action] = subjects
	}

	return nil
}

// parseFilterFlag parses the filter flag and returns the filter ID
// Supports formats:
// - "filter-slug" (uses current space)
// - "space-slug/filter-slug" (uses specified space)
// - "filter-uuid" (direct UUID)
// - "space-uuid/filter-slug" (uses specified space)
func parseFilterFlag(filterValue string) (string, error) {
	if filterValue == "" {
		return "", nil
	}

	uuid, err := parseEntityIdentifierSingle[goclientnew.Filter](
		filterValue,
		EntityTypeFilter,
		apiGetFilterFromSlugInSpace,
		func(f *goclientnew.Filter) string { return f.FilterID.String() },
	)
	if err != nil {
		return "", err
	}
	return uuid.String(), nil
}

// Entity type constants for consistent naming across the codebase
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
	EntityTypeAttribute    = "Attribute"
)

// EntityInSpace type constraint for all entities that reside in spaces
type EntityInSpace interface {
	goclientnew.Filter | goclientnew.View | goclientnew.Invocation |
		goclientnew.Trigger | goclientnew.Tag | goclientnew.ChangeSet |
		goclientnew.Target | goclientnew.BridgeWorker | goclientnew.Unit |
		goclientnew.Link | goclientnew.Attribute
}

// apiGetEntityFromSlugInSpaceFunc is a function type for getting entities by slug in a space
type apiGetEntityFromSlugInSpaceFunc[T any] func(slug string, spaceID string, selectParam string) (*T, error)

// parseEntityIdentifiersAsEntities parses entity identifiers in various formats:
// - UUID (direct entity UUID)
// - Slug (using current space)
// - space/slug or space-uuid/slug
// Returns the full entities
func parseEntityIdentifiersAsEntities[T EntityInSpace](
	identifiers []string,
	entityType string,
	selectParam string,
	apiGetFunc apiGetEntityFromSlugInSpaceFunc[T],
	getEntityID func(*T) string,
) ([]T, error) {
	var entities []T

	// Default selectParam to entityType + "ID" if empty
	if selectParam == "" {
		selectParam = entityType + "ID"
	}

	for _, identifier := range identifiers {
		// Try to parse as UUID directly first
		if id, err := uuid.Parse(identifier); err == nil {
			// For UUIDs, we need to get the entity from the current space
			entity, err := apiGetFunc(id.String(), selectedSpaceID, selectParam)
			if err != nil {
				return nil, fmt.Errorf("%s with ID '%s' not found: %w", entityType, id.String(), err)
			}
			entities = append(entities, *entity)
			continue
		}

		// Split on "/" to check for space/entity format
		parts := strings.Split(identifier, "/")

		var spaceID string
		var entitySlug string

		if len(parts) == 1 {
			// Format: "entity-slug" - determine which space to use
			entitySlug = parts[0]

			// Priority: selectedSpaceID (from --space flag) > context default space
			if selectedSpaceID != "*" && selectedSpaceID != "" {
				// Use explicitly selected space (from --space flag)
				spaceID = selectedSpaceID
			} else {
				// Fall back to default space from context
				cubContext := contextManager.CurrentContext()
				if cubContext == nil || cubContext.Settings.DefaultSpace == "" {
					return nil, fmt.Errorf("no space for %s selected, specified, or in current context", entityType)
				}
				// Look up space (handles both UUIDs and slugs)
				space, err := apiGetSpaceFromSlug(cubContext.Settings.DefaultSpace, "SpaceID")
				if err != nil {
					return nil, fmt.Errorf("space '%s' not found: %w", cubContext.Settings.DefaultSpace, err)
				}
				spaceID = space.SpaceID.String()
			}
		} else if len(parts) == 2 {
			// Format: "space/entity-slug"
			// Look up space (handles both UUIDs and slugs)
			space, err := apiGetSpaceFromSlug(parts[0], "SpaceID")
			if err != nil {
				return nil, fmt.Errorf("space '%s' not found: %w", parts[0], err)
			}
			spaceID = space.SpaceID.String()
			entitySlug = parts[1]
		} else {
			return nil, fmt.Errorf("invalid %s identifier format: %s", entityType, identifier)
		}

		// Look up the entity by slug in the specified space
		entity, err := apiGetFunc(entitySlug, spaceID, selectParam)
		if err != nil {
			return nil, fmt.Errorf("%s '%s' not found in space %s: %w", entityType, entitySlug, spaceID, err)
		}

		entities = append(entities, *entity)
	}

	return entities, nil
}

// entityUUIDs extracts the UUID of each entity using the supplied ID getter.
// Returned slice parallels the input. Factored out so callers that already
// fetched entities via parseEntityIdentifiersAsEntities can convert to UUIDs
// without re-fetching.
func entityUUIDs[T any](entities []T, getEntityID func(*T) string) ([]uuid.UUID, error) {
	uuids := make([]uuid.UUID, len(entities))
	for i := range entities {
		entityUUID, err := uuid.Parse(getEntityID(&entities[i]))
		if err != nil {
			return nil, fmt.Errorf("invalid UUID from entity: %w", err)
		}
		uuids[i] = entityUUID
	}
	return uuids, nil
}

// parseEntityIdentifiers is a wrapper that returns UUIDs by extracting them from the entities
func parseEntityIdentifiers[T EntityInSpace](
	identifiers []string,
	entityType string,
	apiGetFunc apiGetEntityFromSlugInSpaceFunc[T],
	getEntityID func(*T) string,
) ([]uuid.UUID, error) {
	// Get entities with minimal select (just ID field)
	entities, err := parseEntityIdentifiersAsEntities(identifiers, entityType, "", apiGetFunc, getEntityID)
	if err != nil {
		return nil, err
	}
	return entityUUIDs(entities, getEntityID)
}

// parseEntityIdentifierSingle parses a single entity identifier using parseEntityIdentifiers
// Returns the UUID directly
func parseEntityIdentifierSingle[T EntityInSpace](
	identifier string,
	entityType string,
	apiGetFunc apiGetEntityFromSlugInSpaceFunc[T],
	getEntityID func(*T) string,
) (uuid.UUID, error) {
	if identifier == "" {
		return uuid.Nil, fmt.Errorf("%s value cannot be empty", entityType)
	}

	uuids, err := parseEntityIdentifiers([]string{identifier}, entityType, apiGetFunc, getEntityID)
	if err != nil {
		return uuid.Nil, err
	}

	if len(uuids) != 1 {
		return uuid.Nil, fmt.Errorf("unexpected number of UUIDs returned for %s", entityType)
	}

	return uuids[0], nil
}

// parseEntityIdentifierSingleAsEntity parses a single entity identifier and returns the full entity
func parseEntityIdentifierSingleAsEntity[T EntityInSpace](
	identifier string,
	entityType string,
	selectParam string,
	apiGetFunc apiGetEntityFromSlugInSpaceFunc[T],
	getEntityID func(*T) string,
) (*T, error) {
	if identifier == "" {
		return nil, fmt.Errorf("%s value cannot be empty", entityType)
	}

	entities, err := parseEntityIdentifiersAsEntities([]string{identifier}, entityType, selectParam, apiGetFunc, getEntityID)
	if err != nil {
		return nil, err
	}

	if len(entities) != 1 {
		return nil, fmt.Errorf("unexpected number of entities returned for %s", entityType)
	}

	return &entities[0], nil
}

func parseTagSlug(tagValue string) (uuid.UUID, error) {
	return parseEntityIdentifierSingle[goclientnew.Tag](
		tagValue,
		EntityTypeTag,
		apiGetTagFromSlugInSpace,
		func(t *goclientnew.Tag) string { return t.TagID.String() },
	)
}

func parseChangeSetSlug(changesetValue string) (uuid.UUID, error) {
	return parseEntityIdentifierSingle[goclientnew.ChangeSet](
		changesetValue,
		EntityTypeChangeSet,
		apiGetChangeSetFromSlugInSpace,
		func(c *goclientnew.ChangeSet) string { return c.ChangeSetID.String() },
	)
}

func parseInvocationSlug(invocationValue string) (uuid.UUID, error) {
	return parseEntityIdentifierSingle[goclientnew.Invocation](
		invocationValue,
		EntityTypeInvocation,
		apiGetInvocationFromSlugInSpace,
		func(i *goclientnew.Invocation) string { return i.InvocationID.String() },
	)
}

type Unmarshalable interface {
	UnmarshalBinary(data []byte) error
}

// Functionality for populating entities from stdin.

func populateNewModelFromStdin(v interface{}) error {
	jsonBytes, err := readStdin()
	if err != nil {
		return err
	}
	return mergeEntityWithData(v, jsonBytes)
}

func mergeEntityWithData(v any, data []byte) error {
	// Parse YAML/JSON input into a generic structure, then re-marshal as JSON
	// and unmarshal into the target. This ensures that generated types with
	// UnmarshalJSON (e.g., union types) are handled correctly even when the
	// input is YAML.
	var generic interface{}
	if err := yaml.Unmarshal(data, &generic); err != nil {
		return err
	}
	jsonData, err := json.Marshal(convertYAMLToJSON(generic))
	if err != nil {
		return err
	}
	return json.Unmarshal(jsonData, v)
}

// convertYAMLToJSON converts YAML-unmarshaled data to JSON-compatible types.
// The yaml.v3 library produces map[string]interface{} for mappings, which is
// already JSON-compatible. However, map keys from yaml.v3 can sometimes be
// non-string types in edge cases, so we normalize them here.
func convertYAMLToJSON(v interface{}) interface{} {
	switch val := v.(type) {
	case map[string]interface{}:
		result := make(map[string]interface{}, len(val))
		for k, v := range val {
			result[k] = convertYAMLToJSON(v)
		}
		return result
	case []interface{}:
		result := make([]interface{}, len(val))
		for i, v := range val {
			result[i] = convertYAMLToJSON(v)
		}
		return result
	default:
		return v
	}
}

func populateModelFromFile(v any, filename string) error {
	data, err := fetchContent(filename)
	if err != nil {
		return err
	}
	return mergeEntityWithData(v, data)
}

// getBytesFromFlags returns raw bytes from --from-stdin or --filename flags
func getBytesFromFlags() ([]byte, error) {
	if flagPopulateModelFromStdin {
		return readStdin()
	} else if flagFilename != "" {
		return fetchContent(flagFilename)
	}
	return nil, nil
}

// populateModelFromFlags handles both --from-stdin and --filename flags
func populateModelFromFlags(v any) error {
	data, err := getBytesFromFlags()
	if err != nil {
		return err
	}
	if data != nil {
		return mergeEntityWithData(v, data)
	}
	return nil
}
