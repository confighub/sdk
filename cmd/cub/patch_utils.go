// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"encoding/json"
	"fmt"
	"strings"
)

// BuildPatchData builds patch JSON bytes from stdin/file input, labels, and entity-specific fields.
// It handles reading from stdin or file, merging with existing data, processing labels, and applying
// entity-specific enhancements through the enhancer function.
func BuildPatchData(enhancer PatchEnhancer) ([]byte, error) {
	return BuildPatchDataWithPermissions(enhancer, nil)
}

// BuildPatchDataWithPermissions builds patch JSON bytes from stdin/file input, labels, permissions, and entity-specific fields.
// It handles reading from stdin or file, merging with existing data, processing labels, permissions, and applying
// entity-specific enhancements through the enhancer function.
func BuildPatchDataWithPermissions(enhancer PatchEnhancer, permissions []string) ([]byte, error) {
	// Get base patch data from stdin/file if provided
	var patchData []byte
	if flagPopulateModelFromStdin || flagFilename != "" {
		var err error
		patchData, err = getBytesFromFlags()
		if err != nil {
			return nil, err
		}
	}
	if patchData == nil {
		patchData = []byte("null")
	}

	// Enhance with labels, delete gates, permissions, and entity-specific fields
	return EnhancePatchData(patchData, annotation, label, deleteGate, permissions, enhancer)
}

// PatchEnhancer is a function that adds entity-specific fields to patch data.
// It receives the patch map and should modify it in place.
type PatchEnhancer func(patchMap map[string]interface{})

func patchKeyValues(existing map[string]interface{}, newKVs []string) error {
	for _, kvString := range newKVs {
		keyValue := strings.Split(kvString, "=")
		switch len(keyValue) {
		case 1:
			// Key without value sets empty string
			existing[keyValue[0]] = ""
		case 2:
			key := keyValue[0]
			value := keyValue[1]
			if value == "-" {
				// Mark for removal by setting to null in JSON Merge Patch
				existing[key] = nil
			} else {
				existing[key] = value
			}
		default:
			return fmt.Errorf("expected key=value or key=-: %s", kvString)
		}
	}

	return nil
}

// EnhancePatchData adds labels, delete gates, permissions, and entity-specific fields to existing patch data.
// This is used when patch data is already constructed and needs to be enhanced.
// It handles the special case of "-" value for label/delete gate removal in patch operations.
// The enhancer parameter is optional and can be used to add entity-specific fields.
func EnhancePatchData(
	patchData []byte,
	annotations []string,
	labels []string,
	deleteGates []string,
	permissions []string,
	enhancer PatchEnhancer,
) ([]byte, error) {
	// Check if we need to enhance the patch
	needsEnhancement := len(annotations) > 0 || len(labels) > 0 || len(deleteGates) > 0 || len(permissions) > 0 || enhancer != nil
	if !needsEnhancement {
		return patchData, nil
	}

	// Parse existing patch data
	var patchMap map[string]interface{}
	if len(patchData) > 0 && string(patchData) != "null" {
		if err := json.Unmarshal(patchData, &patchMap); err != nil {
			return nil, fmt.Errorf("failed to parse patch data: %w", err)
		}
	} else {
		patchMap = make(map[string]interface{})
	}

	// Apply entity-specific enhancements first
	if enhancer != nil {
		enhancer(patchMap)
	}

	// Add annotations if specified
	if len(annotations) > 0 {
		annotationMap := make(map[string]interface{})

		if existingAnnotations, ok := patchMap["Annotations"]; ok {
			if annotationMapInterface, ok := existingAnnotations.(map[string]interface{}); ok {
				for k, v := range annotationMapInterface {
					annotationMap[k] = v
				}
			}
		}

		err := patchKeyValues(annotationMap, annotations)
		if err != nil {
			return nil, fmt.Errorf("invalid annotation; %w", err)
		}

		patchMap["Annotations"] = annotationMap
	}

	// Add labels if specified
	if len(labels) > 0 {
		labelMap := make(map[string]interface{})
		// Preserve existing labels if any
		if existingLabels, ok := patchMap["Labels"]; ok {
			if labelMapInterface, ok := existingLabels.(map[string]interface{}); ok {
				for k, v := range labelMapInterface {
					labelMap[k] = v
				}
			}
		}

		// Process new labels from command line
		err := patchKeyValues(labelMap, labels)
		if err != nil {
			return nil, fmt.Errorf("invalid label; %w", err)
		}

		patchMap["Labels"] = labelMap
	}

	// Add delete gates if specified
	if len(deleteGates) > 0 {
		var deleteGateMap map[string]interface{}

		// Preserve existing delete gates from the patch if any
		if existingDeleteGates, ok := patchMap["DeleteGates"]; ok {
			deleteGateMap = existingDeleteGates.(map[string]interface{})
		} else {
			deleteGateMap = make(map[string]interface{})
		}

		// Process new delete gates from command line
		for _, deleteGateString := range deleteGates {
			keyValue := strings.Split(deleteGateString, "=")
			switch len(keyValue) {
			case 1:
				// Key without value sets true
				deleteGateMap[keyValue[0]] = true
			case 2:
				key := keyValue[0]
				value := keyValue[1]
				if value == "-" {
					// Mark for removal by setting to null in JSON Merge Patch
					deleteGateMap[key] = nil
				} else if value == "true" {
					deleteGateMap[key] = true
				} else {
					return nil, fmt.Errorf("invalid delete-gate value; only 'true' or '-' is allowed: %s", deleteGateString)
				}
			default:
				return nil, fmt.Errorf("invalid delete-gate; expected key, key=true, or key=-: %s", deleteGateString)
			}
		}

		patchMap["DeleteGates"] = deleteGateMap
	}

	// Add permissions if specified
	if len(permissions) > 0 {
		// Parse permissions into the expected structure
		// We need to convert the Permissions from parsePermissions format to the patch map format
		// The permissions will be merged into the existing permissions
		var permissionsMap map[string]interface{}

		// Preserve existing permissions from the patch if any
		if existingPermissions, ok := patchMap["Permissions"]; ok {
			if existingPermMap, ok := existingPermissions.(map[string]interface{}); ok {
				permissionsMap = existingPermMap
			} else {
				permissionsMap = make(map[string]interface{})
			}
		} else {
			permissionsMap = make(map[string]interface{})
		}

		// Parse the permissions from command line into the map
		// We need to call parsePermissionsIntoPatchMap to convert the string slice into the proper structure
		err := parsePermissionsIntoPatchMap(permissions, permissionsMap)
		if err != nil {
			return nil, err
		}

		patchMap["Permissions"] = permissionsMap
	}

	// Re-marshal the enhanced patch
	return json.Marshal(patchMap)
}

// ValidateLabelRemoval checks if label removal is being attempted without patch mode.
// Returns an error if --label key=- is used without --patch.
func ValidateLabelRemoval(labels []string, isPatch bool) error {
	if !isPatch {
		for _, labelString := range labels {
			keyValue := strings.Split(labelString, "=")
			if len(keyValue) == 2 && keyValue[1] == "-" {
				return fmt.Errorf("label removal (--label %s) requires --patch flag", labelString)
			}
		}
	}
	return nil
}

// ValidateDeleteGateRemoval checks if delete gate removal is being attempted without patch mode.
// Returns an error if --delete-gate key=- is used without --patch.
func ValidateDeleteGateRemoval(deleteGates []string, isPatch bool) error {
	if !isPatch {
		for _, deleteGateString := range deleteGates {
			keyValue := strings.Split(deleteGateString, "=")
			if len(keyValue) == 2 && keyValue[1] == "-" {
				return fmt.Errorf("delete gate removal (--delete-gate %s) requires --patch flag", deleteGateString)
			}
		}
	}
	return nil
}
