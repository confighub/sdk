// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"fmt"

	"github.com/confighub/sdk/core/cubapi"
	goclientnew "github.com/confighub/sdk/core/openapi/goclient-new"
	"github.com/google/uuid"

	"github.com/spf13/cobra"
)

var workerCmd = &cobra.Command{
	Use:   "worker",
	Short: "Manage workers",
	Long: getCommandHelp(`Manage workers.

Workers are explained at https://docs.confighub.com/background/entities/worker/.
A guide for how to use workers is at https://docs.confighub.com/guide/workers/.`, ""),
	PersistentPreRunE: spacePreRunE,
}

func init() {
	addSpaceFlags(workerCmd)
	rootCmd.AddCommand(workerCmd)
}

// apiGetBridgeWorkerFromSlug supports space/slug syntax through the generic parser
func apiGetBridgeWorkerFromSlug(slug string, selectParam string) (*goclientnew.BridgeWorker, error) {
	// Use the generic entity parser to get the full worker with proper selectParam
	return parseEntityIdentifierSingleAsEntity[goclientnew.BridgeWorker](
		slug,
		EntityTypeBridgeWorker,
		selectParam,
		apiGetBridgeWorkerFromSlugInSpace,
		func(w *goclientnew.BridgeWorker) string { return w.BridgeWorkerID.String() },
	)
}

func apiGetBridgeWorkerFromSlugInSpace(slug string, spaceID string, selectParam string) (*goclientnew.BridgeWorker, error) {
	id, err := uuid.Parse(slug)
	if err == nil {
		spaceUUID, err := uuid.Parse(spaceID)
		if err != nil {
			return nil, fmt.Errorf("invalid space ID: %w", err)
		}
		return apiGetBridgeWorker(spaceUUID, id, selectParam)
	}
	// The default for get is "*" rather than auto-selected list columns
	if selectParam == "" {
		selectParam = "*"
	}
	// Note: When getting a worker by slug for cub worker run, we need all fields including Secret
	list, err := apiListBridgeworkers(spaceID, "Slug = '"+slug+"'", selectParam, "")
	if err != nil {
		return nil, err
	}
	for _, entity := range list {
		if entity.BridgeWorker.Slug == slug {
			return entity.BridgeWorker, nil
		}
	}
	return nil, fmt.Errorf("bridgeworker %s not found in space %s", slug, spaceID)
}

func apiGetBridgeWorker(spaceID, workerID uuid.UUID, selectParam string) (*goclientnew.BridgeWorker, error) {
	extendedWorker, err := apiGetExtendedBridgeWorker(spaceID, workerID, selectParam)
	if err != nil {
		return nil, err
	}
	return extendedWorker.BridgeWorker, nil
}

func apiGetExtendedBridgeWorker(spaceID, workerID uuid.UUID, selectParam string) (*goclientnew.ExtendedBridgeWorker, error) {
	newParams := &goclientnew.GetBridgeWorkerParams{}
	include := "SpaceID"
	newParams.Include = &include
	selectValue := handleSelectParameter(selectParam, selectFields, nil)
	if selectValue != "" && selectValue != "*" {
		newParams.Select = &selectValue
	}
	workerRes, err := cubClientNew.GetBridgeWorkerWithResponse(ctx, spaceID, workerID, newParams)
	if cubapi.IsAPIError(err, workerRes) {
		return nil, cubapi.InterpretErrorGeneric(err, workerRes)
	}

	return workerRes.JSON200, nil
}

func apiGetExtendedBridgeWorkerFromSlug(slug string, selectParam string) (*goclientnew.ExtendedBridgeWorker, error) {
	// For now, use the simple approach until EntityInSpace constraint is updated
	return apiGetExtendedBridgeWorkerFromSlugInSpace(slug, selectedSpaceID, selectParam)
}

func apiGetExtendedBridgeWorkerFromSlugInSpace(slug string, spaceID string, selectParam string) (*goclientnew.ExtendedBridgeWorker, error) {
	id, err := uuid.Parse(slug)
	if err == nil {
		spaceUUID, err := uuid.Parse(spaceID)
		if err != nil {
			return nil, fmt.Errorf("invalid space ID: %w", err)
		}
		return apiGetExtendedBridgeWorker(spaceUUID, id, selectParam)
	}
	// The default for get is "*" rather than auto-selected list columns
	if selectParam == "" {
		selectParam = "*"
	}
	list, err := apiListBridgeworkers(spaceID, "Slug = '"+slug+"'", selectParam, "")
	if err != nil {
		return nil, err
	}
	for _, entity := range list {
		if entity.BridgeWorker.Slug == slug {
			return entity, nil
		}
	}
	return nil, fmt.Errorf("bridgeworker %s not found in space %s", slug, spaceID)
}
