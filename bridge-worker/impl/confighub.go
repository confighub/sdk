// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package impl

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/cockroachdb/errors"
	"github.com/confighub/sdk/bridge-worker/api"
	"github.com/confighub/sdk/bridge-worker/lib"
	"github.com/confighub/sdk/cubapi"
	goclientnew "github.com/confighub/sdk/openapi/goclient-new"
	"github.com/confighub/sdk/third_party/gaby"
	"github.com/confighub/sdk/workerapi"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/yaml"
)

type ConfigHubBridgeWorker struct {
	serverURL    string
	serverPort   string
	workerID     string
	workerSecret string
	client       *goclientnew.ClientWithResponses
	authSession  *cubapi.AuthSession
	mu           sync.RWMutex
	stopRefresh  chan struct{}
}

var _ api.BridgeWorker = (*ConfigHubBridgeWorker)(nil)

// NewConfigHubBridgeWorker creates a new ConfigHubBridgeWorker
func NewConfigHubBridgeWorker(serverURL, serverPort, workerID, workerSecret string) *ConfigHubBridgeWorker {
	worker := &ConfigHubBridgeWorker{
		serverURL:    serverURL,
		serverPort:   serverPort,
		workerID:     workerID,
		workerSecret: workerSecret,
		stopRefresh:  make(chan struct{}),
	}

	// Initialize the client with authentication
	if err := worker.refreshAuth(); err != nil {
		log.Log.Error(err, "Failed to initialize authentication for ConfigHub bridge worker")
	}

	// Start token refresh goroutine
	go worker.tokenRefreshLoop()

	return worker
}

// refreshAuth exchanges worker credentials for a JWT token and updates the client
func (w *ConfigHubBridgeWorker) refreshAuth() error {
	// Get JWT token using worker credentials
	url := w.serverURL
	if w.serverPort != "" {
		url += ":" + w.serverPort
	}

	log.Log.Info("Attempting ConfigHub bridge worker authentication",
		"url", url,
		"workerID", w.workerID,
		"hasSecret", w.workerSecret != "")

	authSession, err := cubapi.PerformWorkerAuth(url, w.workerID, w.workerSecret)
	if err != nil {
		wrappedErr := errors.WithStack(err)
		log.Log.Error(wrappedErr, "Worker authentication failed",
			"url", url,
			"workerID", w.workerID)
		return errors.Wrap(err, "failed to authenticate worker")
	}

	// Create a new client with the JWT token
	client, err := goclientnew.NewClientWithResponses(url+"/api", func(c *goclientnew.Client) error {
		c.RequestEditors = append(c.RequestEditors, func(ctx context.Context, req *http.Request) error {
			req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", authSession.AccessToken))
			return nil
		})
		return nil
	})
	if err != nil {
		return errors.Wrap(err, "failed to create authenticated client")
	}

	// Update the client and auth session under lock
	w.mu.Lock()
	w.client = client
	w.authSession = authSession
	w.mu.Unlock()

	log.Log.Info("Successfully refreshed ConfigHub bridge worker authentication")
	return nil
}

// tokenRefreshLoop refreshes the token every 23 hours
func (w *ConfigHubBridgeWorker) tokenRefreshLoop() {
	ticker := time.NewTicker(23 * time.Hour)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			if err := w.refreshAuth(); err != nil {
				log.Log.Error(err, "Failed to refresh authentication token")
			}
		case <-w.stopRefresh:
			return
		}
	}
}

// getClient returns the authenticated client (thread-safe)
func (w *ConfigHubBridgeWorker) getClient() *goclientnew.ClientWithResponses {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.client
}

// Info returns the bridge worker information
func (w *ConfigHubBridgeWorker) Info(opts api.InfoOptions) api.BridgeWorkerInfo {
	return api.BridgeWorkerInfo{
		SupportedConfigTypes: []*api.ConfigType{
			{
				ToolchainType: workerapi.ToolchainConfigHubYAML,
				ProviderType:  api.ProviderConfigHub,
				LiveStateType: workerapi.ToolchainConfigHubYAML,
				AvailableTargets: []api.Target{
					{
						Name:   api.GenerateTargetName(opts.WorkerSlug, api.ProviderConfigHub, workerapi.ToolchainConfigHubYAML, ""),
						Params: map[string]interface{}{},
					},
				},
			},
		},
	}
}

// Apply processes the configuration data
func (w *ConfigHubBridgeWorker) Apply(wctx api.BridgeWorkerContext, payload api.BridgeWorkerPayload) (retErr error) {
	// Add panic recovery with stack trace
	defer func() {
		if r := recover(); r != nil {
			retErr = errors.WithStack(errors.Newf("panic in ConfigHub Apply: %v", r))
			log.Log.Error(retErr, "Panic recovered in ConfigHub Apply",
				"unitSlug", payload.UnitSlug,
				"panic", r)
		}
	}()

	log.Log.Info("Starting ConfigHub Apply operation", "unitSlug", payload.UnitSlug)

	// Create a new action result helper
	newActionResult := func(status api.ActionStatusType, result api.ActionResultType, message string) *api.ActionResult {
		return &api.ActionResult{
			UnitID:            payload.UnitID,
			SpaceID:           payload.SpaceID,
			QueuedOperationID: payload.QueuedOperationID,
			ActionResultBaseMeta: api.ActionResultBaseMeta{
				RevisionNum: payload.RevisionNum,
				Action:      api.ActionApply,
				Result:      result,
				Status:      status,
				Message:     message,
				StartedAt:   time.Now(),
			},
		}
	}

	// Check if client is initialized
	client := w.getClient()
	if client == nil {
		// Try to refresh auth once more
		if err := w.refreshAuth(); err != nil {
			return lib.SafeSendStatus(wctx, newActionResult(
				api.ActionStatusFailed,
				api.ActionResultApplyFailed,
				fmt.Sprintf("ConfigHub bridge authentication failed: %v", err),
			), err)
		}
		client = w.getClient()
		if client == nil {
			err := errors.WithStack(errors.New("failed to initialize ConfigHub API client"))
			return lib.SafeSendStatus(wctx, newActionResult(
				api.ActionStatusFailed,
				api.ActionResultApplyFailed,
				err.Error(),
			), err)
		}
	}

	// Send progressing status
	if err := wctx.SendStatus(newActionResult(
		api.ActionStatusProgressing,
		api.ActionResultNone,
		"Starting to apply ConfigHub resources...",
	)); err != nil {
		return err
	}

	// Parse the existing inventory and lastAppliedData from LiveData if available
	var oldInventory *cubapi.Inventory
	var lastAppliedDataFromLiveData []byte

	if len(payload.LiveData) > 0 {
		// Parse the LiveData as multi-document YAML
		container, err := gaby.ParseAll(payload.LiveData)
		if err != nil {
			log.Log.Error(err, "Failed to parse LiveData as multi-document YAML")
		} else if len(container) > 0 {
			// First document should be the Inventory
			firstDoc := container[0]
			inventoryBytes, err := yaml.Marshal(firstDoc.Data())
			if err != nil {
				log.Log.Error(err, "Failed to marshal first document as YAML")
			} else {
				oldInventory = &cubapi.Inventory{}
				if err := yaml.Unmarshal(inventoryBytes, oldInventory); err != nil {
					log.Log.Error(err, "Failed to unmarshal first document as Inventory")
					oldInventory = nil
				}
			}

			// Remaining documents are the lastAppliedData
			if len(container) > 1 {
				var lastAppliedDocs []string
				for i := 1; i < len(container); i++ {
					lastAppliedDocs = append(lastAppliedDocs, container[i].String())
				}
				if len(lastAppliedDocs) > 0 {
					lastAppliedDataFromLiveData = []byte(strings.Join(lastAppliedDocs, "\n---\n"))
				}
			}
		}
	}

	// Get last-applied data from LiveRevisionNum if available, fallback to LiveData
	var lastAppliedData []byte
	if payload.LiveRevisionNum > 0 {
		revision, err := cubapi.GetRevisionByNum(wctx.Context(), client, payload.SpaceID.String(), payload.UnitID.String(), payload.LiveRevisionNum)
		if err != nil {
			log.Log.Info("Could not fetch live revision for lastAppliedData, falling back to LiveData",
				"liveRevisionNum", payload.LiveRevisionNum,
				"error", err)
			lastAppliedData = lastAppliedDataFromLiveData
		} else if revision != nil && revision.Revision != nil && revision.Revision.Data != "" {
			// Data is base64 encoded, need to decode it
			decodedData, err := base64.StdEncoding.DecodeString(revision.Revision.Data)
			if err != nil {
				log.Log.Error(err, "Failed to decode base64 data from revision, falling back to LiveData",
					"liveRevisionNum", payload.LiveRevisionNum)
				lastAppliedData = lastAppliedDataFromLiveData
			} else {
				lastAppliedData = decodedData
			}
		} else {
			lastAppliedData = lastAppliedDataFromLiveData
		}
	} else {
		// No LiveRevisionNum, use data from LiveData
		lastAppliedData = lastAppliedDataFromLiveData
	}

	// Call cubapi.Apply with the current unit's space slug as the default
	defaultSpaceSlug := "default"
	if payload.SpaceSlug != "" {
		defaultSpaceSlug = payload.SpaceSlug
	}
	results, newInventory, err := cubapi.Apply(
		wctx.Context(),
		client,
		payload.Data,
		lastAppliedData,
		oldInventory,
		defaultSpaceSlug,
	)

	if err != nil {
		wrappedErr := errors.WithStack(err)
		log.Log.Error(wrappedErr, "cubapi.Apply failed", "unitSlug", payload.UnitSlug)
		return lib.SafeSendStatus(wctx, newActionResult(
			api.ActionStatusFailed,
			api.ActionResultApplyFailed,
			fmt.Sprintf("Apply failed: %v", err),
		), wrappedErr)
	}

	// Check if any results failed and collect successful entities
	var failedCount int
	var successCount int
	type entityWithType struct {
		Entity     interface{}
		EntityType string
		SpaceSlug  string
	}
	var successfulEntities []entityWithType

	for _, result := range results {

		if result.Action == "failed" {
			failedCount++
		} else if result.Action == "created" || result.Action == "updated" || result.Action == "unchanged" || result.Action == "deleted" {
			successCount++
			// Include the entity in LiveData if it currently exists (created, updated, or unchanged)
			// Deleted entities should not be included in LiveData since they no longer exist
			if result.Entity != nil && (result.Action == "created" || result.Action == "updated" || result.Action == "unchanged") {
				successfulEntities = append(successfulEntities, entityWithType{
					Entity:     result.Entity,
					EntityType: result.EntityType,
					SpaceSlug:  result.SpaceSlug,
				})
			}
		}
	}

	// Build the new LiveData with Inventory first, followed by applied entities
	var liveDataData []byte
	if newInventory != nil {
		var liveDataDocs []string

		// First document: Inventory
		inventoryYAML, err := yaml.Marshal(newInventory)
		if err != nil {
			log.Log.Error(err, "Failed to marshal inventory to YAML")
		} else {
			liveDataDocs = append(liveDataDocs, strings.TrimSpace(string(inventoryYAML)))
		}

		// Subsequent documents: Successfully applied entities
		for _, item := range successfulEntities {
			// Use MarshalYAMLWithoutReadonlyFields to filter out readonly fields and add SpaceSlug
			entityYAML, err := cubapi.MarshalYAMLWithoutReadonlyFields(item.EntityType, item.Entity, item.SpaceSlug)
			if err != nil {
				log.Log.Error(err, "Failed to marshal entity to YAML", "entity", item.Entity, "entityType", item.EntityType, "spaceSlug", item.SpaceSlug)
				continue
			}
			liveDataDocs = append(liveDataDocs, strings.TrimSpace(string(entityYAML)))
		}

		// Join all documents with YAML document separator (matching gaby format)
		if len(liveDataDocs) > 0 {
			liveDataData = []byte(strings.Join(liveDataDocs, "\n---\n"))
		}
	}

	// Determine the final status
	if failedCount > 0 {
		message := fmt.Sprintf("Apply partially failed: %d succeeded, %d failed", successCount, failedCount)
		return lib.SafeSendStatus(wctx, newActionResult(
			api.ActionStatusFailed,
			api.ActionResultApplyFailed,
			message,
		), fmt.Errorf("%s", message))
	}

	// Success - send completed status with updated LiveData
	finalResult := newActionResult(
		api.ActionStatusCompleted,
		api.ActionResultApplyCompleted,
		fmt.Sprintf("Successfully applied %d resources", successCount),
	)
	finalResult.LiveData = liveDataData

	return wctx.SendStatus(finalResult)
}

// Destroy removes all resources tracked in the inventory
func (w *ConfigHubBridgeWorker) Destroy(wctx api.BridgeWorkerContext, payload api.BridgeWorkerPayload) error {
	log.Log.Info("Starting ConfigHub Destroy operation", "unitSlug", payload.UnitSlug)

	// Create a new action result helper
	newActionResult := func(status api.ActionStatusType, result api.ActionResultType, message string) *api.ActionResult {
		return &api.ActionResult{
			UnitID:            payload.UnitID,
			SpaceID:           payload.SpaceID,
			QueuedOperationID: payload.QueuedOperationID,
			ActionResultBaseMeta: api.ActionResultBaseMeta{
				RevisionNum: payload.RevisionNum,
				Action:      api.ActionDestroy,
				Result:      result,
				Status:      status,
				Message:     message,
				StartedAt:   time.Now(),
			},
		}
	}

	// Check if client is initialized
	client := w.getClient()
	if client == nil {
		// Try to refresh auth once more
		if err := w.refreshAuth(); err != nil {
			return lib.SafeSendStatus(wctx, newActionResult(
				api.ActionStatusFailed,
				api.ActionResultDestroyFailed,
				fmt.Sprintf("ConfigHub bridge authentication failed: %v", err),
			), err)
		}
		client = w.getClient()
		if client == nil {
			err := errors.WithStack(errors.New("failed to initialize ConfigHub API client"))
			return lib.SafeSendStatus(wctx, newActionResult(
				api.ActionStatusFailed,
				api.ActionResultDestroyFailed,
				err.Error(),
			), err)
		}
	}

	// Send progressing status
	if err := wctx.SendStatus(newActionResult(
		api.ActionStatusProgressing,
		api.ActionResultNone,
		"Starting to destroy ConfigHub resources...",
	)); err != nil {
		return err
	}

	// Parse the inventory from LiveData
	var inventory *cubapi.Inventory
	if len(payload.LiveData) > 0 {
		inventory = &cubapi.Inventory{}
		if err := yaml.Unmarshal(payload.LiveData, inventory); err != nil {
			return lib.SafeSendStatus(wctx, newActionResult(
				api.ActionStatusFailed,
				api.ActionResultDestroyFailed,
				fmt.Sprintf("Failed to parse inventory from LiveData: %v", err),
			), err)
		}
	}

	// If no inventory, nothing to destroy
	if inventory == nil || len(inventory.Resources) == 0 {
		return wctx.SendStatus(newActionResult(
			api.ActionStatusCompleted,
			api.ActionResultDestroyCompleted,
			"No resources to destroy",
		))
	}

	// Call cubapi.Destroy
	defaultSpaceSlug := "default"
	if payload.SpaceSlug != "" {
		defaultSpaceSlug = payload.SpaceSlug
	}
	results := cubapi.Destroy(
		wctx.Context(),
		client,
		inventory,
		defaultSpaceSlug,
	)

	// Check results
	var failedCount int
	var successCount int
	for _, result := range results {
		if result.Action == "failed" {
			failedCount++
		} else if result.Action == "deleted" {
			successCount++
		}
	}

	// Determine the final status
	if failedCount > 0 {
		message := fmt.Sprintf("Destroy partially failed: %d succeeded, %d failed", successCount, failedCount)
		return lib.SafeSendStatus(wctx, newActionResult(
			api.ActionStatusFailed,
			api.ActionResultDestroyFailed,
			message,
		), fmt.Errorf("%s", message))
	}

	// Success - send completed status with empty LiveData
	finalResult := newActionResult(
		api.ActionStatusCompleted,
		api.ActionResultDestroyCompleted,
		fmt.Sprintf("Successfully destroyed %d resources", successCount),
	)
	finalResult.LiveData = []byte{} // Clear the LiveData since everything is destroyed

	return wctx.SendStatus(finalResult)
}

// Refresh gets the current state of entities and updates the config data, inventory, and live state
func (w *ConfigHubBridgeWorker) Refresh(wctx api.BridgeWorkerContext, payload api.BridgeWorkerPayload) (retErr error) {
	// Add panic recovery with stack trace
	defer func() {
		if r := recover(); r != nil {
			retErr = errors.WithStack(errors.Newf("panic in ConfigHub Refresh: %v", r))
			log.Log.Error(retErr, "Panic recovered in ConfigHub Refresh",
				"unitSlug", payload.UnitSlug,
				"panic", r)
		}
	}()

	log.Log.Info("Starting ConfigHub Refresh operation", "unitSlug", payload.UnitSlug)

	// Create a new action result helper
	newActionResult := func(status api.ActionStatusType, result api.ActionResultType, message string) *api.ActionResult {
		return &api.ActionResult{
			UnitID:            payload.UnitID,
			SpaceID:           payload.SpaceID,
			QueuedOperationID: payload.QueuedOperationID,
			ActionResultBaseMeta: api.ActionResultBaseMeta{
				RevisionNum: payload.RevisionNum,
				Action:      api.ActionRefresh,
				Result:      result,
				Status:      status,
				Message:     message,
				StartedAt:   time.Now(),
			},
		}
	}

	// Check if client is initialized
	client := w.getClient()
	if client == nil {
		// Try to refresh auth once more
		if err := w.refreshAuth(); err != nil {
			return lib.SafeSendStatus(wctx, newActionResult(
				api.ActionStatusFailed,
				api.ActionResultRefreshFailed,
				fmt.Sprintf("ConfigHub bridge authentication failed: %v", err),
			), err)
		}
		client = w.getClient()
		if client == nil {
			err := errors.WithStack(errors.New("failed to initialize ConfigHub API client"))
			return lib.SafeSendStatus(wctx, newActionResult(
				api.ActionStatusFailed,
				api.ActionResultRefreshFailed,
				err.Error(),
			), err)
		}
	}

	// Send progressing status
	if err := wctx.SendStatus(newActionResult(
		api.ActionStatusProgressing,
		api.ActionResultNone,
		"Starting to refresh ConfigHub resources...",
	)); err != nil {
		return err
	}

	// Parse the existing inventory from LiveData if available
	var oldInventory *cubapi.Inventory
	if len(payload.LiveData) > 0 {
		// Parse the LiveData as multi-document YAML
		container, err := gaby.ParseAll(payload.LiveData)
		if err != nil {
			log.Log.Error(err, "Failed to parse LiveData as multi-document YAML")
		} else if len(container) > 0 {
			// First document should be the Inventory
			firstDoc := container[0]
			inventoryBytes, err := yaml.Marshal(firstDoc.Data())
			if err != nil {
				log.Log.Error(err, "Failed to marshal first document as YAML")
			} else {
				oldInventory = &cubapi.Inventory{}
				if err := yaml.Unmarshal(inventoryBytes, oldInventory); err != nil {
					log.Log.Error(err, "Failed to unmarshal first document as Inventory")
					oldInventory = nil
				}
			}
		}
	}

	// Call cubapi.Refresh with the current unit's space slug as the default
	defaultSpaceSlug := "default"
	if payload.SpaceSlug != "" {
		defaultSpaceSlug = payload.SpaceSlug
	}

	results, newInventory, updatedConfigData, err := cubapi.Refresh(
		wctx.Context(),
		client,
		payload.Data,
		oldInventory,
		defaultSpaceSlug,
		payload.LiveRevisionNum,
		payload.SpaceID.String(),
		payload.UnitID.String(),
	)

	if err != nil {
		wrappedErr := errors.WithStack(err)
		log.Log.Error(wrappedErr, "cubapi.Refresh failed", "unitSlug", payload.UnitSlug)
		return lib.SafeSendStatus(wctx, newActionResult(
			api.ActionStatusFailed,
			api.ActionResultRefreshFailed,
			fmt.Sprintf("Refresh failed: %v", err),
		), wrappedErr)
	}

	// Check if any results failed and collect successful entities
	var failedCount int
	var successCount int
	var deletedCount int
	type entityWithType struct {
		Entity     interface{}
		EntityType string
		SpaceSlug  string
	}
	var successfulEntities []entityWithType

	for _, result := range results {
		if result.Action == "failed" {
			failedCount++
		} else if result.Action == "refreshed" {
			successCount++
			// Include the entity in LiveData
			if result.Entity != nil {
				successfulEntities = append(successfulEntities, entityWithType{
					Entity:     result.Entity,
					EntityType: result.EntityType,
					SpaceSlug:  result.SpaceSlug,
				})
			}
		} else if result.Action == "deleted" {
			deletedCount++
			// Deleted entities are not included in LiveData since they no longer exist
		}
	}

	// Build the new LiveData with Inventory first, followed by refreshed entities
	var liveDataData []byte
	if newInventory != nil {
		var liveDataDocs []string

		// First document: Inventory
		inventoryYAML, err := yaml.Marshal(newInventory)
		if err != nil {
			log.Log.Error(err, "Failed to marshal inventory to YAML")
		} else {
			liveDataDocs = append(liveDataDocs, strings.TrimSpace(string(inventoryYAML)))
		}

		// Subsequent documents: Successfully refreshed entities
		for _, item := range successfulEntities {
			// Use MarshalYAMLWithoutReadonlyFields to filter out readonly fields and add SpaceSlug
			entityYAML, err := cubapi.MarshalYAMLWithoutReadonlyFields(item.EntityType, item.Entity, item.SpaceSlug)
			if err != nil {
				log.Log.Error(err, "Failed to marshal entity to YAML", "entity", item.Entity, "entityType", item.EntityType, "spaceSlug", item.SpaceSlug)
				continue
			}
			liveDataDocs = append(liveDataDocs, strings.TrimSpace(string(entityYAML)))
		}

		// Join all documents with YAML document separator (matching gaby format)
		if len(liveDataDocs) > 0 {
			liveDataData = []byte(strings.Join(liveDataDocs, "\n---\n"))
		}
	}

	// Determine the final status
	if failedCount > 0 {
		message := fmt.Sprintf("Refresh partially failed: %d refreshed, %d deleted, %d failed", successCount, deletedCount, failedCount)
		return lib.SafeSendStatus(wctx, newActionResult(
			api.ActionStatusFailed,
			api.ActionResultRefreshFailed,
			message,
		), fmt.Errorf("%s", message))
	}

	// Determine if drift was detected by comparing original data with updated data
	driftDetected := !bytes.Equal(payload.Data, updatedConfigData)

	var resultType api.ActionResultType
	var message string
	if driftDetected {
		resultType = api.ActionResultRefreshAndDrifted
		message = fmt.Sprintf("Drift detected during refresh: %d resources refreshed (%d deleted)", successCount, deletedCount)
	} else {
		resultType = api.ActionResultRefreshAndNoDrift
		message = fmt.Sprintf("No drift detected: %d resources refreshed (%d deleted)", successCount, deletedCount)
	}

	// Success - send completed status with updated LiveData and config data
	finalResult := newActionResult(
		api.ActionStatusCompleted,
		resultType,
		message,
	)
	finalResult.LiveData = liveDataData
	// TODO: Properly merge the config data instead of blowing away the current data.
	finalResult.Data = updatedConfigData

	return wctx.SendStatus(finalResult)
}

// Import imports entities from ConfigHub based on the provided ImportRequest
func (w *ConfigHubBridgeWorker) Import(wctx api.BridgeWorkerContext, payload api.BridgeWorkerPayload) (retErr error) {
	// Add panic recovery with stack trace
	defer func() {
		if r := recover(); r != nil {
			retErr = errors.WithStack(errors.Newf("panic in ConfigHub Import: %v", r))
			log.Log.Error(retErr, "Panic recovered in ConfigHub Import",
				"unitSlug", payload.UnitSlug,
				"panic", r)
		}
	}()

	startTime := time.Now()

	// Send initial status
	if err := wctx.SendStatus(&api.ActionResult{
		UnitID:            payload.UnitID,
		SpaceID:           payload.SpaceID,
		QueuedOperationID: payload.QueuedOperationID,
		ActionResultBaseMeta: api.ActionResultBaseMeta{
			RevisionNum: payload.RevisionNum,
			Action:      api.ActionImport,
			Result:      api.ActionResultNone,
			Status:      api.ActionStatusProgressing,
			Message:     "Starting ConfigHub import operation",
			StartedAt:   startTime,
		},
	}); err != nil {
		return err
	}

	// Parse ImportRequest from ExtraParams
	var importRequest *goclientnew.ImportRequest
	if len(payload.ExtraParams) > 0 {
		importRequest = new(goclientnew.ImportRequest)
		if err := json.Unmarshal(payload.ExtraParams, importRequest); err != nil {
			return lib.SafeSendStatus(wctx, &api.ActionResult{
				UnitID:            payload.UnitID,
				SpaceID:           payload.SpaceID,
				QueuedOperationID: payload.QueuedOperationID,
				ActionResultBaseMeta: api.ActionResultBaseMeta{
					RevisionNum: payload.RevisionNum,
					Action:      api.ActionImport,
					Result:      api.ActionResultImportFailed,
					Status:      api.ActionStatusFailed,
					Message:     fmt.Sprintf("Failed to parse import request: %v", err),
					StartedAt:   startTime,
				},
			}, err)
		}
	}

	// Handle ResourceInfoList import (similar to Kubernetes bridge)
	if importRequest != nil && importRequest.ResourceInfoList != nil && len(*importRequest.ResourceInfoList) > 0 {
		return w.importFromResourceInfoList(wctx, payload, *importRequest.ResourceInfoList, startTime)
	}

	// Handle filter-based import
	return w.importFromFilters(wctx, payload, importRequest, startTime)
}

// importFromResourceInfoList imports entities specified in a ResourceInfoList
func (w *ConfigHubBridgeWorker) importFromResourceInfoList(wctx api.BridgeWorkerContext, payload api.BridgeWorkerPayload, resourceInfoList goclientnew.ResourceInfoList, startTime time.Time) error {
	if err := wctx.SendStatus(&api.ActionResult{
		UnitID:            payload.UnitID,
		SpaceID:           payload.SpaceID,
		QueuedOperationID: payload.QueuedOperationID,
		ActionResultBaseMeta: api.ActionResultBaseMeta{
			RevisionNum: payload.RevisionNum,
			Action:      api.ActionImport,
			Result:      api.ActionResultNone,
			Status:      api.ActionStatusProgressing,
			Message:     fmt.Sprintf("Found %d resources to import", len(resourceInfoList)),
			StartedAt:   startTime,
		},
	}); err != nil {
		return err
	}

	// Build inventory by retrieving each resource from ConfigHub
	var entities []interface{}
	for _, resourceInfo := range resourceInfoList {
		entity, err := cubapi.GetEntityBySlug(context.Background(), w.client, resourceInfo.ResourceType, payload.SpaceID.String(), resourceInfo.ResourceName)
		if err != nil {
			log.Log.Error(err, "Failed to get entity", "type", resourceInfo.ResourceType, "slug", resourceInfo.ResourceName)
			// Continue with other entities rather than failing the entire import
			continue
		}
		entities = append(entities, entity)
	}

	if len(entities) == 0 {
		return lib.SafeSendStatus(wctx, &api.ActionResult{
			UnitID:            payload.UnitID,
			SpaceID:           payload.SpaceID,
			QueuedOperationID: payload.QueuedOperationID,
			ActionResultBaseMeta: api.ActionResultBaseMeta{
				RevisionNum: payload.RevisionNum,
				Action:      api.ActionImport,
				Result:      api.ActionResultImportFailed,
				Status:      api.ActionStatusFailed,
				Message:     "No valid entities found to import",
				StartedAt:   startTime,
			},
		}, errors.New("no valid entities found"))
	}

	return w.completeImportMixed(wctx, payload, entities, startTime)
}

// importFromFilters imports entities based on filter criteria
func (w *ConfigHubBridgeWorker) importFromFilters(wctx api.BridgeWorkerContext, payload api.BridgeWorkerPayload, importRequest *goclientnew.ImportRequest, startTime time.Time) error {
	if importRequest == nil || importRequest.Options == nil {
		return lib.SafeSendStatus(wctx, &api.ActionResult{
			UnitID:            payload.UnitID,
			SpaceID:           payload.SpaceID,
			QueuedOperationID: payload.QueuedOperationID,
			ActionResultBaseMeta: api.ActionResultBaseMeta{
				RevisionNum: payload.RevisionNum,
				Action:      api.ActionImport,
				Result:      api.ActionResultImportFailed,
				Status:      api.ActionStatusFailed,
				Message:     "import.EntityType must be specified in import options",
				StartedAt:   startTime,
			},
		}, errors.New("EntityType option required"))
	}

	// Extract entity type from options
	entityType := cubapi.ParseStringOption(importRequest.Options, "EntityType", "")
	if entityType == "" {
		return lib.SafeSendStatus(wctx, &api.ActionResult{
			UnitID:            payload.UnitID,
			SpaceID:           payload.SpaceID,
			QueuedOperationID: payload.QueuedOperationID,
			ActionResultBaseMeta: api.ActionResultBaseMeta{
				RevisionNum: payload.RevisionNum,
				Action:      api.ActionImport,
				Result:      api.ActionResultImportFailed,
				Status:      api.ActionStatusFailed,
				Message:     "import.EntityType must be specified in import options",
				StartedAt:   startTime,
			},
		}, errors.New("EntityType option required"))
	}

	if err := wctx.SendStatus(&api.ActionResult{
		UnitID:            payload.UnitID,
		SpaceID:           payload.SpaceID,
		QueuedOperationID: payload.QueuedOperationID,
		ActionResultBaseMeta: api.ActionResultBaseMeta{
			RevisionNum: payload.RevisionNum,
			Action:      api.ActionImport,
			Result:      api.ActionResultNone,
			Status:      api.ActionStatusProgressing,
			Message:     fmt.Sprintf("Fetching %s entities from ConfigHub", entityType),
			StartedAt:   startTime,
		},
	}); err != nil {
		return err
	}

	// Build where filter from ImportFilters
	whereFilter := ""
	if len(importRequest.Filters) > 0 {
		var err error
		whereFilter, err = cubapi.BuildWhereFilterFromImportFilters(importRequest.Filters)
		if err != nil {
			return lib.SafeSendStatus(wctx, &api.ActionResult{
				UnitID:            payload.UnitID,
				SpaceID:           payload.SpaceID,
				QueuedOperationID: payload.QueuedOperationID,
				ActionResultBaseMeta: api.ActionResultBaseMeta{
					RevisionNum: payload.RevisionNum,
					Action:      api.ActionImport,
					Result:      api.ActionResultImportFailed,
					Status:      api.ActionStatusFailed,
					Message:     fmt.Sprintf("Failed to build where filter: %v", err),
					StartedAt:   startTime,
				},
			}, err)
		}
	}

	// Fetch entities using the list function
	entities, err := cubapi.ListEntitiesForImport(context.Background(), w.client, payload.SpaceID.String(), entityType, whereFilter)
	if err != nil {
		return lib.SafeSendStatus(wctx, &api.ActionResult{
			UnitID:            payload.UnitID,
			SpaceID:           payload.SpaceID,
			QueuedOperationID: payload.QueuedOperationID,
			ActionResultBaseMeta: api.ActionResultBaseMeta{
				RevisionNum: payload.RevisionNum,
				Action:      api.ActionImport,
				Result:      api.ActionResultImportFailed,
				Status:      api.ActionStatusFailed,
				Message:     fmt.Sprintf("Failed to list entities: %v", err),
				StartedAt:   startTime,
			},
		}, err)
	}

	if len(entities) == 0 {
		return lib.SafeSendStatus(wctx, &api.ActionResult{
			UnitID:            payload.UnitID,
			SpaceID:           payload.SpaceID,
			QueuedOperationID: payload.QueuedOperationID,
			ActionResultBaseMeta: api.ActionResultBaseMeta{
				RevisionNum: payload.RevisionNum,
				Action:      api.ActionImport,
				Result:      api.ActionResultImportCompleted,
				Status:      api.ActionStatusCompleted,
				Message:     fmt.Sprintf("No %s entities found matching the filter criteria", entityType),
				StartedAt:   startTime,
			},
		}, nil)
	}

	return w.completeImport(wctx, payload, entities, entityType, startTime)
}

// completeImportMixed handles ResourceInfoList imports where entities may be of different types
func (w *ConfigHubBridgeWorker) completeImportMixed(wctx api.BridgeWorkerContext, payload api.BridgeWorkerPayload, entities []interface{}, startTime time.Time) error {
	// For mixed entity imports, we need to handle each entity type separately
	// Group entities by type for proper processing
	entityGroups := make(map[string][]interface{})

	for _, entity := range entities {
		entityType := ""

		// Determine entity type
		switch entity.(type) {
		case *goclientnew.Space:
			entityType = cubapi.EntityTypeSpace
		case *goclientnew.Unit:
			entityType = cubapi.EntityTypeUnit
		case *goclientnew.Link:
			entityType = cubapi.EntityTypeLink
		case *goclientnew.View:
			entityType = cubapi.EntityTypeView
		case *goclientnew.Filter:
			entityType = cubapi.EntityTypeFilter
		case *goclientnew.Trigger:
			entityType = cubapi.EntityTypeTrigger
		case *goclientnew.Invocation:
			entityType = cubapi.EntityTypeInvocation
		case *goclientnew.ChangeSet:
			entityType = cubapi.EntityTypeChangeSet
		case *goclientnew.Tag:
			entityType = cubapi.EntityTypeTag
		default:
			log.Log.Error(nil, "Unknown entity type in mixed import", "entity", entity)
			continue
		}

		entityGroups[entityType] = append(entityGroups[entityType], entity)
	}

	// Get space slug for space-resident entities
	defaultSpaceSlug := "default"
	if payload.SpaceSlug != "" {
		defaultSpaceSlug = payload.SpaceSlug
	}

	// Create inventory with all imported entities
	var inventoryResources []cubapi.InventoryResource
	var liveDataDocs []string
	var dataDocs []string

	// Process each entity type group
	for entityType, groupEntities := range entityGroups {
		for _, entity := range groupEntities {
			// Add to inventory
			slug := extractSlugFromEntity(entity)
			if slug != "" {
				inventoryResources = append(inventoryResources, cubapi.InventoryResource{
					ResourceType: entityType,
					ResourceName: slug,
				})
			}

			// Add to YAML documents
			entityYAML, err := cubapi.MarshalYAMLWithoutReadonlyFields(entityType, entity, defaultSpaceSlug)
			if err != nil {
				log.Log.Error(err, "Failed to marshal entity to YAML", "entity", entity, "entityType", entityType, "spaceSlug", defaultSpaceSlug)
				continue
			}
			liveDataDocs = append(liveDataDocs, strings.TrimSpace(string(entityYAML)))
			dataDocs = append(dataDocs, strings.TrimSpace(string(entityYAML)))
		}
	}

	inventory := &cubapi.Inventory{
		EntityType: cubapi.EntityTypeInventory,
		Resources:  inventoryResources,
	}

	// Build final YAML documents
	var finalLiveDataDocs []string

	// First document: Inventory
	inventoryYAML, err := yaml.Marshal(inventory)
	if err != nil {
		log.Log.Error(err, "Failed to marshal inventory to YAML")
	} else {
		finalLiveDataDocs = append(finalLiveDataDocs, strings.TrimSpace(string(inventoryYAML)))
	}

	// Add entity documents
	finalLiveDataDocs = append(finalLiveDataDocs, liveDataDocs...)

	// Join all documents
	var liveDataData []byte
	if len(finalLiveDataDocs) > 0 {
		liveDataData = []byte(strings.Join(finalLiveDataDocs, "\n---\n"))
	}

	var dataYAML []byte
	if len(dataDocs) > 0 {
		dataYAML = []byte(strings.Join(dataDocs, "\n---\n"))
	}

	// Send final success result
	finalResult := &api.ActionResult{
		UnitID:            payload.UnitID,
		SpaceID:           payload.SpaceID,
		QueuedOperationID: payload.QueuedOperationID,
		ActionResultBaseMeta: api.ActionResultBaseMeta{
			RevisionNum: payload.RevisionNum,
			Action:      api.ActionImport,
			Result:      api.ActionResultImportCompleted,
			Status:      api.ActionStatusCompleted,
			Message:     fmt.Sprintf("Imported %d entities successfully at %s", len(entities), time.Now().Format(time.RFC3339)),
			StartedAt:   startTime,
		},
		Data:     dataYAML,
		LiveData: liveDataData,
	}

	return wctx.SendStatus(finalResult)
}

// completeImport converts entities to YAML similar to how Refresh works and sends the final result
func (w *ConfigHubBridgeWorker) completeImport(wctx api.BridgeWorkerContext, payload api.BridgeWorkerPayload, entities []interface{}, entityType string, startTime time.Time) error {
	if err := wctx.SendStatus(&api.ActionResult{
		UnitID:            payload.UnitID,
		SpaceID:           payload.SpaceID,
		QueuedOperationID: payload.QueuedOperationID,
		ActionResultBaseMeta: api.ActionResultBaseMeta{
			RevisionNum: payload.RevisionNum,
			Action:      api.ActionImport,
			Result:      api.ActionResultNone,
			Status:      api.ActionStatusProgressing,
			Message:     "Converting entities to YAML format",
			StartedAt:   startTime,
		},
	}); err != nil {
		return err
	}

	// Get space slug for space-resident entities
	defaultSpaceSlug := "default"
	if payload.SpaceSlug != "" {
		defaultSpaceSlug = payload.SpaceSlug
	}

	// Create inventory with imported entities
	var inventoryResources []cubapi.InventoryResource
	for _, entity := range entities {
		// Extract the slug from each entity to build inventory
		if entityMap, ok := entity.(map[string]interface{}); ok {
			if slug, ok := entityMap["Slug"].(string); ok {
				inventoryResources = append(inventoryResources, cubapi.InventoryResource{
					ResourceType: entityType,
					ResourceName: slug,
				})
			}
		} else {
			// Handle typed entities by using reflection or type assertions
			slug := extractSlugFromEntity(entity)
			if slug != "" {
				inventoryResources = append(inventoryResources, cubapi.InventoryResource{
					ResourceType: entityType,
					ResourceName: slug,
				})
			}
		}
	}

	inventory := &cubapi.Inventory{
		EntityType: cubapi.EntityTypeInventory,
		Resources:  inventoryResources,
	}

	// Build LiveData with Inventory first, followed by entities (similar to Refresh)
	var liveDataDocs []string

	// First document: Inventory
	inventoryYAML, err := yaml.Marshal(inventory)
	if err != nil {
		log.Log.Error(err, "Failed to marshal inventory to YAML")
	} else {
		liveDataDocs = append(liveDataDocs, strings.TrimSpace(string(inventoryYAML)))
	}

	// Subsequent documents: Imported entities with cleaned fields
	for _, entity := range entities {
		// Use MarshalYAMLWithoutReadonlyFields to filter out readonly fields and add SpaceSlug
		entityYAML, err := cubapi.MarshalYAMLWithoutReadonlyFields(entityType, entity, defaultSpaceSlug)
		if err != nil {
			log.Log.Error(err, "Failed to marshal entity to YAML", "entity", entity, "entityType", entityType, "spaceSlug", defaultSpaceSlug)
			continue
		}
		liveDataDocs = append(liveDataDocs, strings.TrimSpace(string(entityYAML)))
	}

	// Join all documents with YAML document separator (matching gaby format)
	var liveDataData []byte
	if len(liveDataDocs) > 0 {
		liveDataData = []byte(strings.Join(liveDataDocs, "\n---\n"))
	}

	// Create separate documents for Data field as well
	var dataDocs []string
	for _, entity := range entities {
		entityYAML, err := cubapi.MarshalYAMLWithoutReadonlyFields(entityType, entity, defaultSpaceSlug)
		if err != nil {
			log.Log.Error(err, "Failed to marshal entity to YAML for data", "entity", entity, "entityType", entityType)
			continue
		}
		dataDocs = append(dataDocs, strings.TrimSpace(string(entityYAML)))
	}

	var dataYAML []byte
	if len(dataDocs) > 0 {
		dataYAML = []byte(strings.Join(dataDocs, "\n---\n"))
	}

	// Send final success result
	finalResult := &api.ActionResult{
		UnitID:            payload.UnitID,
		SpaceID:           payload.SpaceID,
		QueuedOperationID: payload.QueuedOperationID,
		ActionResultBaseMeta: api.ActionResultBaseMeta{
			RevisionNum: payload.RevisionNum,
			Action:      api.ActionImport,
			Result:      api.ActionResultImportCompleted,
			Status:      api.ActionStatusCompleted,
			Message:     fmt.Sprintf("Imported %d entities successfully at %s", len(entities), time.Now().Format(time.RFC3339)),
			StartedAt:   startTime,
		},
		Data:     dataYAML,
		LiveData: liveDataData,
	}

	return wctx.SendStatus(finalResult)
}

// extractSlugFromEntity extracts the Slug field from an entity using type assertions
func extractSlugFromEntity(entity interface{}) string {
	switch e := entity.(type) {
	case *goclientnew.Space:
		return e.Slug
	case *goclientnew.Unit:
		return e.Slug
	case *goclientnew.Link:
		return e.Slug
	case *goclientnew.View:
		return e.Slug
	case *goclientnew.Filter:
		return e.Slug
	case *goclientnew.Trigger:
		return e.Slug
	case *goclientnew.Invocation:
		return e.Slug
	case *goclientnew.ChangeSet:
		return e.Slug
	case *goclientnew.Tag:
		return e.Slug
	default:
		return ""
	}
}

// Finalize performs cleanup operations
func (w *ConfigHubBridgeWorker) Finalize(wctx api.BridgeWorkerContext, payload api.BridgeWorkerPayload) error {
	// Stop the token refresh goroutine
	close(w.stopRefresh)
	return nil
}
