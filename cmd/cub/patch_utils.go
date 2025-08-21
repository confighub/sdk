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

	// Enhance with labels and entity-specific fields
	return EnhancePatchData(patchData, label, enhancer)
}

// PatchEnhancer is a function that adds entity-specific fields to patch data.
// It receives the patch map and should modify it in place.
type PatchEnhancer func(patchMap map[string]interface{})

// EnhancePatchData adds labels and entity-specific fields to existing patch data.
// This is used when patch data is already constructed and needs to be enhanced.
// It handles the special case of "-" value for label removal in patch operations.
// The enhancer parameter is optional and can be used to add entity-specific fields.
func EnhancePatchData(patchData []byte, labels []string, enhancer PatchEnhancer) ([]byte, error) {
	// Check if we need to enhance the patch
	needsEnhancement := len(labels) > 0 || enhancer != nil
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
		for _, labelString := range labels {
			keyValue := strings.Split(labelString, "=")
			switch len(keyValue) {
			case 1:
				// Key without value sets empty string
				labelMap[keyValue[0]] = ""
			case 2:
				key := keyValue[0]
				value := keyValue[1]
				if value == "-" {
					// Mark for removal by setting to null in JSON Merge Patch
					labelMap[key] = nil
				} else {
					labelMap[key] = value
				}
			default:
				return nil, fmt.Errorf("invalid label; expected key=value or key=-: %s", labelString)
			}
		}
		
		patchMap["Labels"] = labelMap
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