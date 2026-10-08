// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"

	goclientnew "github.com/confighub/sdk/core/openapi/goclient-new"
	"github.com/google/uuid"
	"gopkg.in/yaml.v3"
)

// setPermissions applies --permission to permissions, creating them if the flag names any.
func setPermissions(permissions **goclientnew.Permissions) error {
	return applyPermissions(permissionFlag, permissions)
}

// applyPermissions applies permission strings, as parsePermissions reads them, to an entity's
// Permissions, creating them if there are any to apply: an entity read or built without
// Permissions has none to add to.
func applyPermissions(permissionStrs []string, permissions **goclientnew.Permissions) error {
	if len(permissionStrs) > 0 && *permissions == nil {
		*permissions = &goclientnew.Permissions{}
	}
	return parsePermissions(permissionStrs, *permissions)
}

// permissionGroupPrefix marks the subject of a permission string as a Group: Action:group:<slug>.
const permissionGroupPrefix = "group:"

// permissionServiceAccountPrefix marks the subject of a permission string as a ServiceAccount:
// Action:serviceaccount:<slug>. The grant is to the ServiceAccount's User, which is what
// Permissions name.
const permissionServiceAccountPrefix = "serviceaccount:"

// permissionFormat describes a permission string, for errors.
const permissionFormat = "Action:UserIDOrUsername, Action:group:GroupSlugOrID or Action:serviceaccount:ServiceAccountSlugOrID, prefixed with - to remove"

// The help of every --permission flag: permissionCreateHelp where the flag sets an entity's
// permissions, permissionUpdateHelp where it adds to or removes from them. A group is resolved
// among the groups the caller belongs to.
const (
	permissionCreateHelp = "permission in format Action:UserIDOrUsername, Action:group:GroupSlugOrID for a group you belong to, or Action:serviceaccount:ServiceAccountSlugOrID (e.g., Manage:user@example.com, View:group:platform, View:serviceaccount:deploy-bot, can be repeated)"
	permissionUpdateHelp = "permission in format Action:UserIDOrUsername, Action:group:GroupSlugOrID or Action:serviceaccount:ServiceAccountSlugOrID to add, or the same prefixed with - to remove (e.g., Manage:user@example.com, View:group:platform, View:serviceaccount:deploy-bot, -View:user@example.com, can be repeated)"
)

// permissionGrant is one parsed permission string: the action, the subject it names, and whether
// the subject is added or removed.
type permissionGrant struct {
	action    string
	field     string // "UserIDs" or "GroupIDs", the Subjects field the subject belongs in
	subjectID string
	remove    bool
}

// parsePermissionGrant parses one permission string. The subject is a user, by UUID or username,
// or with the group: prefix a Group, by slug or UUID. A UUID is never taken to be a Group without
// the prefix: it would otherwise be granted as a user that does not exist, which grants nothing.
func parsePermissionGrant(permStr string) (permissionGrant, error) {
	var grant permissionGrant
	if strings.HasPrefix(permStr, "-") {
		grant.remove = true
		permStr = permStr[1:]
	}

	action, subject, ok := strings.Cut(permStr, ":")
	if !ok || action == "" || subject == "" {
		return grant, fmt.Errorf("invalid permission format %q, expected %s", permStr, permissionFormat)
	}
	grant.action = action

	if groupRef, isGroup := strings.CutPrefix(subject, permissionGroupPrefix); isGroup {
		if groupRef == "" {
			return grant, fmt.Errorf("invalid permission format %q: no group after %q", permStr, permissionGroupPrefix)
		}
		group, err := resolveGroup(groupRef, "GroupID,Slug")
		if err != nil {
			return grant, fmt.Errorf("failed to find group %q: %w", groupRef, err)
		}
		grant.field = "GroupIDs"
		grant.subjectID = group.Group.GroupID.String()
		return grant, nil
	}

	if serviceAccountRef, isServiceAccount := strings.CutPrefix(subject, permissionServiceAccountPrefix); isServiceAccount {
		if serviceAccountRef == "" {
			return grant, fmt.Errorf("invalid permission format %q: no service account after %q", permStr, permissionServiceAccountPrefix)
		}
		serviceAccount, err := resolveServiceAccount(serviceAccountRef, "ServiceAccountID,Slug,UserID")
		if err != nil {
			return grant, fmt.Errorf("failed to find service account %q: %w", serviceAccountRef, err)
		}
		grant.field = "UserIDs"
		grant.subjectID = serviceAccount.ServiceAccount.UserID.String()
		return grant, nil
	}

	userID, err := uuid.Parse(subject)
	if err != nil {
		user, err := resolveUserCore(subject)
		if err != nil {
			return grant, fmt.Errorf("failed to find user %q: %w", subject, err)
		}
		userID = user.UserID
	}
	grant.field = "UserIDs"
	grant.subjectID = userID.String()
	return grant, nil
}

// parsePermissions parses permission strings (see parsePermissionGrant) and populates a Permissions
// object. A string prefixed with - removes its user or Group from the action's subjects.
func parsePermissions(permissionStrs []string, permissions *goclientnew.Permissions) error {
	if len(permissionStrs) == 0 {
		return nil
	}

	if *permissions == nil {
		*permissions = make(goclientnew.Permissions)
	}

	for _, permStr := range permissionStrs {
		grant, err := parsePermissionGrant(permStr)
		if err != nil {
			return err
		}

		subjects := (*permissions)[grant.action]
		ids := &subjects.UserIDs
		if grant.field == "GroupIDs" {
			ids = &subjects.GroupIDs
		}
		if *ids == nil {
			*ids = make(map[string]bool)
		}
		if grant.remove {
			delete(*ids, grant.subjectID)
		} else {
			(*ids)[grant.subjectID] = true
		}
		(*permissions)[grant.action] = subjects
	}

	return nil
}

// parsePermissionsIntoPatchMap parses permission strings (see parsePermissionGrant) and adds them
// to a generic map structure for use in JSON patches. A string prefixed with - removes its user or
// Group by setting it to null, as a JSON Merge Patch removes a key.
// This is used by the patch operations where we're building a generic map[string]interface{}.
func parsePermissionsIntoPatchMap(permissionStrs []string, permissionsMap map[string]interface{}) error {
	if len(permissionStrs) == 0 {
		return nil
	}

	for _, permStr := range permissionStrs {
		grant, err := parsePermissionGrant(permStr)
		if err != nil {
			return err
		}

		subjects, ok := permissionsMap[grant.action].(map[string]interface{})
		if !ok {
			subjects = make(map[string]interface{})
		}
		ids, ok := subjects[grant.field].(map[string]interface{})
		if !ok {
			ids = make(map[string]interface{})
		}

		if grant.remove {
			ids[grant.subjectID] = nil
		} else {
			ids[grant.subjectID] = true
		}

		subjects[grant.field] = ids
		permissionsMap[grant.action] = subjects
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
	// The generated entity types are named for their schemas.
	if err := checkEntityInput(reflect.Indirect(reflect.ValueOf(v)).Type().Name(), generic); err != nil {
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
