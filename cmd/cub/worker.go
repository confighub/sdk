// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"fmt"
	"strings"

	goclientnew "github.com/confighub/sdk/openapi/goclient-new"
	"github.com/google/uuid"

	"github.com/spf13/cobra"
)

var workerCmd = &cobra.Command{
	Use:               "worker",
	Short:             "Manage workers",
	Long:              `Manage workers`,
	PersistentPreRunE: spacePreRunE,
}

func init() {
	addSpaceFlags(workerCmd)
	rootCmd.AddCommand(workerCmd)
}

func apiGetBridgeWorkerFromSlug(slug string) (*goclientnew.BridgeWorker, error) {
	id, err := uuid.Parse(slug)
	if err == nil {
		return apiGetBridgeWorker(uuid.MustParse(selectedSpaceID), id)
	}
	slugpath := strings.Split(slug, "/")
	space := selectedSpaceID
	slugParsed := slug
	// TODO: Support this syntax for other entities or remove it.
	if len(slugpath) == 2 {
		res, err := apiGetSpaceFromSlug(slugpath[0])
		if err != nil {
			return nil, err
		}
		space = res.SpaceID.String()
		slugParsed = slugpath[1]
	}
	// TODO: Take advantage of where filter.
	list, err := apiListBridgeworkers(space)
	if err != nil {
		return nil, err
	}
	for _, entity := range list {
		if entity.Slug == slugParsed {
			return entity, nil
		}
	}
	return nil, fmt.Errorf("bridgeworker %s not found in space %s", slug, space)
}

func apiGetBridgeWorker(spaceID, workerID uuid.UUID) (*goclientnew.BridgeWorker, error) {
	workerRes, err := cubClientNew.GetBridgeWorkerWithResponse(ctx, spaceID, workerID, nil)
	if IsAPIError(err, workerRes) {
		return nil, InterpretErrorGeneric(err, workerRes)
	}

	return workerRes.JSON200.BridgeWorker, nil
}
