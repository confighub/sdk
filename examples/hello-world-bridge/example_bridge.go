// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/confighub/sdk/bridge-worker/api"
	"github.com/confighub/sdk/workerapi"
	"github.com/gosimple/slug"
)

// ExampleBridge implements the Bridge interface
// This is a simple example bridge that demonstrates the basic structure
type ExampleBridge struct {
	// Add any fields you need for your bridge implementation
	name    string
	baseDir string
}

// NewExampleBridge creates a new ExampleBridge instance
func NewExampleBridge(name, baseDir string) (*ExampleBridge, error) {
	if baseDir == "" {
		baseDir = "/tmp/confighub-example-bridge"
	}
	err := os.MkdirAll(baseDir, 0755)
	if err != nil {
		return nil, err
	}

	return &ExampleBridge{
		name:    name,
		baseDir: baseDir,
	}, nil
}

// Info returns information about the bridge's capabilities
// For this particular bridge, it will offer a target for each subdirectory in the base directory.
func (eb *ExampleBridge) Info(opts api.InfoOptions) api.BridgeInfo {
	// Scan for subdirectories
	var targets []api.Target
	entries, err := os.ReadDir(eb.baseDir)
	if err != nil {
		fmt.Printf("Failed to read directory %s: %v\n", eb.baseDir, err)
		fmt.Println("Returning no available targets")
	} else {
		// create a target for each subdirectory
		for _, entry := range entries {
			if entry.IsDir() {
				targets = append(targets, api.Target{
					Name: slug.Make(entry.Name()),
					Params: map[string]interface{}{
						"description": fmt.Sprintf("Filesystem target for directory: %s", entry.Name()),
						"dir_name":    entry.Name(),
					},
				})
			}
		}
	}
	return api.BridgeInfo{
		// a single bridge can support multiple config types.
		// One config type is a tuple of toolchain type and provider type along with a list of already known targets.
		// ConfigHub users can create additional targets for a config type. Those targets will get routed to this bridge.
		// That means that if you assign a unit to the target and perform a bridge operation, the bridge will be called
		// with the unit and the target.
		SupportedConfigTypes: []*api.ConfigType{
			{
				ToolchainType:    workerapi.ToolchainKubernetesYAML,
				ProviderType:     api.ProviderType("Filesystem"),
				AvailableTargets: targets,
			},
		},
	}
}

// Apply handles the apply operation
// For this example, it will write the payload data to a file in the target directory.
func (eb *ExampleBridge) Apply(ctx api.BridgeContext, payload api.BridgePayload) error {
	startTime := time.Now()
	// Send initial status
	if err := ctx.SendStatus(&api.ActionResult{
		UnitID:            payload.UnitID,
		SpaceID:           payload.SpaceID,
		QueuedOperationID: payload.QueuedOperationID,
		ActionResultBaseMeta: api.ActionResultMeta{
			Action:    api.ActionApply,
			Result:    api.ActionResultNone,
			Status:    api.ActionStatusProgressing,
			Message:   fmt.Sprintf("Starting apply operation for %s", eb.name),
			StartedAt: startTime,
		},
	}); err != nil {
		return err
	}

	// Ensure base directory exists
	if err := os.MkdirAll(eb.baseDir, 0755); err != nil {
		return fmt.Errorf("failed to create base directory: %w", err)
	}

	// Get target name from payload parameters
	targetName, err := parseTargetParams(payload)
	if err != nil {
		return fmt.Errorf("failed to parse target parameters: %w", err)
	}

	// Create target subdirectory
	targetDir := filepath.Join(eb.baseDir, targetName)
	if err := os.MkdirAll(targetDir, 0755); err != nil {
		return fmt.Errorf("failed to create target directory %s: %w", targetDir, err)
	}

	// Create filename from unit slug
	filename := fmt.Sprintf("%s.yaml", payload.UnitSlug)
	filepath := filepath.Join(targetDir, filename)

	// Write payload data to file
	if err := os.WriteFile(filepath, payload.Data, 0644); err != nil {
		return fmt.Errorf("failed to write file %s: %w", filepath, err)
	}
	terminatedAt := time.Now()

	// Send completion status
	return ctx.SendStatus(&api.ActionResult{
		UnitID:            payload.UnitID,
		SpaceID:           payload.SpaceID,
		QueuedOperationID: payload.QueuedOperationID,
		ActionResultBaseMeta: api.ActionResultMeta{
			Action:       api.ActionApply,
			Result:       api.ActionResultApplyCompleted,
			Status:       api.ActionStatusCompleted,
			Message:      fmt.Sprintf("Successfully wrote configuration to %s at %s", filepath, time.Now().Format(time.RFC3339)),
			StartedAt:    startTime,
			TerminatedAt: &terminatedAt,
		},
		Data:      payload.Data,
		LiveState: payload.Data,
	})
}

// Refresh handles the refresh operation which will check if live and confighub have drifted.
// If there is drift, it will return the latest config from live.
func (eb *ExampleBridge) Refresh(ctx api.BridgeContext, payload api.BridgePayload) error {
	startedAt := time.Now()
	// Send initial status
	if err := ctx.SendStatus(&api.ActionResult{
		UnitID:            payload.UnitID,
		SpaceID:           payload.SpaceID,
		QueuedOperationID: payload.QueuedOperationID,
		ActionResultBaseMeta: api.ActionResultMeta{
			Action:    api.ActionRefresh,
			Result:    api.ActionResultNone,
			Status:    api.ActionStatusProgressing,
			Message:   fmt.Sprintf("Starting refresh operation for %s", eb.name),
			StartedAt: startedAt,
		},
	}); err != nil {
		return err
	}

	// Get target name from payload parameters
	targetName, err := parseTargetParams(payload)
	if err != nil {
		return fmt.Errorf("failed to parse target parameters: %w", err)
	}

	// Create filename from unit slug
	filename := fmt.Sprintf("%s.yaml", payload.UnitSlug)
	filepath := filepath.Join(eb.baseDir, targetName, filename)

	// Read file contents
	unitData, err := os.ReadFile(filepath)
	if err != nil {
		if os.IsNotExist(err) {
			// File doesn't exist, return empty state
			unitData = []byte{}
		} else {
			return fmt.Errorf("failed to read file %s: %w", filepath, err)
		}
	}

	// Compare file contents with payload data to detect drift
	oldUnitData := payload.Data
	var resultType api.ActionResultType
	var message string

	if len(unitData) == 0 && len(oldUnitData) == 0 {
		// Both empty - no drift
		resultType = api.ActionResultRefreshAndNoDrift
		message = fmt.Sprintf("Successfully refreshed configuration from %s at %s - no drift detected", filepath, time.Now().Format(time.RFC3339))
	} else if len(unitData) == 0 {
		// File is empty but payload has data - drift detected
		resultType = api.ActionResultRefreshAndDrifted
		message = fmt.Sprintf("Drift detected: file %s is empty but payload has data at %s", filepath, time.Now().Format(time.RFC3339))
	} else if len(oldUnitData) == 0 {
		// Payload is empty but file has data - drift detected
		resultType = api.ActionResultRefreshAndDrifted
		message = fmt.Sprintf("Drift detected: payload is empty but file %s has data at %s", filepath, time.Now().Format(time.RFC3339))
	} else if string(unitData) == string(oldUnitData) {
		// Contents match - no drift
		resultType = api.ActionResultRefreshAndNoDrift
		message = fmt.Sprintf("Successfully refreshed configuration from %s at %s - no drift detected", filepath, time.Now().Format(time.RFC3339))
	} else {
		// Contents differ - drift detected
		resultType = api.ActionResultRefreshAndDrifted
		message = fmt.Sprintf("Drift detected: file %s differs from payload at %s", filepath, time.Now().Format(time.RFC3339))
	}

	terminatedAt := time.Now()
	// Send completion status
	return ctx.SendStatus(&api.ActionResult{
		UnitID:            payload.UnitID,
		SpaceID:           payload.SpaceID,
		QueuedOperationID: payload.QueuedOperationID,
		ActionResultBaseMeta: api.ActionResultMeta{
			Action:       api.ActionRefresh,
			Result:       resultType,
			Status:       api.ActionStatusCompleted,
			Message:      message,
			StartedAt:    startedAt,
			TerminatedAt: &terminatedAt,
		},
		Data: unitData,
	})
}

// Import handles the import operation
// Not implemented for this example.
func (eb *ExampleBridge) Import(ctx api.BridgeContext, payload api.BridgePayload) error {
	startedAt := time.Now()
	// Send initial status
	if err := ctx.SendStatus(&api.ActionResult{
		UnitID:            payload.UnitID,
		SpaceID:           payload.SpaceID,
		QueuedOperationID: payload.QueuedOperationID,
		ActionResultBaseMeta: api.ActionResultMeta{
			Action:    api.ActionImport,
			Result:    api.ActionResultNone,
			Status:    api.ActionStatusProgressing,
			Message:   fmt.Sprintf("Starting import operation for %s", eb.name),
			StartedAt: startedAt,
		},
	}); err != nil {
		return err
	}

	// Get target name from payload parameters
	targetName, err := parseTargetParams(payload)
	if err != nil {
		return fmt.Errorf("failed to parse target parameters: %w", err)
	}

	// Create filename from unit slug
	filename := fmt.Sprintf("%s.yaml", payload.UnitSlug)
	filepath := filepath.Join(eb.baseDir, targetName, filename)

	// Read file contents for import
	importedData, err := os.ReadFile(filepath)
	if err != nil {
		if os.IsNotExist(err) {
			// File doesn't exist, return empty data
			importedData = []byte{}
		} else {
			return fmt.Errorf("failed to read file for import %s: %w", filepath, err)
		}
	}

	terminatedAt := time.Now()
	// Send completion status
	return ctx.SendStatus(&api.ActionResult{
		UnitID:            payload.UnitID,
		SpaceID:           payload.SpaceID,
		QueuedOperationID: payload.QueuedOperationID,
		ActionResultBaseMeta: api.ActionResultMeta{
			Action:       api.ActionImport,
			Result:       api.ActionResultImportCompleted,
			Status:       api.ActionStatusCompleted,
			Message:      fmt.Sprintf("Successfully imported configuration from %s at %s", filepath, time.Now().Format(time.RFC3339)),
			StartedAt:    startedAt,
			TerminatedAt: &terminatedAt,
		},
		Data:      importedData,
		LiveState: importedData,
	})
}

// Destroy handles the destroy operation
// For this example, it will delete the file in the target directory.
func (eb *ExampleBridge) Destroy(ctx api.BridgeContext, payload api.BridgePayload) error {
	startedAt := time.Now()
	// Send initial status
	if err := ctx.SendStatus(&api.ActionResult{
		UnitID:            payload.UnitID,
		SpaceID:           payload.SpaceID,
		QueuedOperationID: payload.QueuedOperationID,
		ActionResultBaseMeta: api.ActionResultMeta{
			Action:    api.ActionDestroy,
			Result:    api.ActionResultNone,
			Status:    api.ActionStatusProgressing,
			Message:   fmt.Sprintf("Starting destroy operation for %s", eb.name),
			StartedAt: startedAt,
		},
	}); err != nil {
		return err
	}

	// Get target name from payload parameters
	targetName, err := parseTargetParams(payload)
	if err != nil {
		return fmt.Errorf("failed to parse target parameters: %w", err)
	}

	// Create filename from unit slug
	filename := fmt.Sprintf("%s.yaml", payload.UnitSlug)
	filepath := filepath.Join(eb.baseDir, targetName, filename)

	// Delete the file
	if err := os.Remove(filepath); err != nil {
		if os.IsNotExist(err) {
			// File doesn't exist, which is fine for destroy
		} else {
			return fmt.Errorf("failed to delete file %s: %w", filepath, err)
		}
	}

	terminatedAt := time.Now()
	// Send completion status
	return ctx.SendStatus(&api.ActionResult{
		UnitID:            payload.UnitID,
		SpaceID:           payload.SpaceID,
		QueuedOperationID: payload.QueuedOperationID,
		ActionResultBaseMeta: api.ActionResultMeta{
			Action:       api.ActionDestroy,
			Result:       api.ActionResultDestroyCompleted,
			Status:       api.ActionStatusCompleted,
			Message:      fmt.Sprintf("Successfully deleted file %s at %s", filepath, time.Now().Format(time.RFC3339)),
			StartedAt:    startedAt,
			TerminatedAt: &terminatedAt,
		},
		LiveState: []byte{}, // Empty live state after destruction
	})
}

// Finalize handles the finalize operation
func (eb *ExampleBridge) Finalize(ctx api.BridgeContext, payload api.BridgePayload) error {
	startedAt := time.Now()
	// Finalize is typically used for cleanup operations
	// In this example, we'll just send a completion status
	return ctx.SendStatus(&api.ActionResult{
		UnitID:            payload.UnitID,
		SpaceID:           payload.SpaceID,
		QueuedOperationID: payload.QueuedOperationID,
		ActionResultBaseMeta: api.ActionResultMeta{
			Action:       api.ActionFinalize,
			Result:       api.ActionResultNone,
			Status:       api.ActionStatusCompleted,
			Message:      fmt.Sprintf("Finalized operations for %s at %s", eb.name, time.Now().Format(time.RFC3339)),
			StartedAt:    startedAt,
			TerminatedAt: &startedAt,
		},
	})
}

// parseTargetParams extracts the target name from the target parameters
func parseTargetParams(payload api.BridgeWorkerPayload) (string, error) {
	var params map[string]interface{}
	if len(payload.TargetParams) > 0 {
		if err := json.Unmarshal(payload.TargetParams, &params); err != nil {
			return "", fmt.Errorf("failed to parse target params: %v", err)
		}
	}

	// Get directory name from the parameter I set in Info()
	if dirName, ok := params["dir_name"].(string); ok && dirName != "" {
		return dirName, nil
	}

	// Default to "default" if no directory name found
	return "default", nil
}

// Ensure ExampleBridge implements the Bridge interface
var _ api.Bridge = (*ExampleBridge)(nil)
