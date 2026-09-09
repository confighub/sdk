// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"bytes"
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

	uuid, err := resolveFilterID(filterValue)
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
	EntityTypeChangeOrder  = "ChangeOrder"
	EntityTypeTarget       = "Target"
	EntityTypeBridgeWorker = "BridgeWorker"
	EntityTypeUnit         = "Unit"
	EntityTypeLink         = "Link"
	EntityTypeSet          = "Set"
	EntityTypeAttribute    = "Attribute"
)

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

type Unmarshalable interface {
	UnmarshalBinary(data []byte) error
}

// Functionality for populating entities from stdin.

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
	dec := json.NewDecoder(bytes.NewReader(jsonData))
	dec.DisallowUnknownFields()
	return dec.Decode(v)
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
