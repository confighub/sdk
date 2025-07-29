// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

// The load command loads a package by deserializing a directory structure into ConfigHub
// spaces, units, links, etc.

package main

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"

	goclientnew "github.com/confighub/sdk/openapi/goclient-new"
	"github.com/spf13/cobra"
)

var packageLoadCmd = &cobra.Command{
	Use:   "load <dir>",
	Short: "load a package from a directory",
	Long:  `load a package by deserializing a directory structure into ConfigHub spaces, units, links, etc.`,
	Args:  cobra.ExactArgs(1),
	RunE:  packageLoadCmdRun,
}

func init() {
	addSpaceFlags(packageLoadCmd)
	packageLoadCmd.Flags().String("prefix", "", "prefix to add to space slugs")
	packageCmdGroup.AddCommand(packageLoadCmd)
}

var createdSpaces = map[string]goclientnew.Space{}
var createdWorkers = map[string]goclientnew.BridgeWorker{}
var createdTargets = map[string]goclientnew.Target{}
var createdUnits = map[string]goclientnew.Unit{}

func packageLoadCmdRun(cmd *cobra.Command, args []string) error {
	dir := args[0]
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		return fmt.Errorf("package directory does not exist: %s", dir)
	}
	prefix, err := cmd.Flags().GetString("prefix")
	if err != nil {
		return err
	}
	manifest, err := loadManifest(dir)
	if err != nil {
		return err
	}
	for _, space := range manifest.Spaces {
		if prefix != "" {
			space.Slug = prefix + "-" + space.Slug
		}
		spaceDetails, err := loadSpaceDetails(dir, space)
		if err != nil {
			return err
		}
		resp, err := cubClientNew.CreateSpaceWithResponse(ctx, *spaceDetails)
		if err != nil {
			return err
		}
		if resp.JSON200 == nil {
			return fmt.Errorf("failed to create space %s: %s", space.Slug, resp.Body)
		}
		createdSpaces[space.Slug] = *resp.JSON200
	}
	for _, worker := range manifest.Workers {
		if prefix != "" {
			worker.SpaceSlug = prefix + "-" + worker.SpaceSlug
		}
		workerDetails, err := loadWorkerDetails(dir, worker)
		if err != nil {
			return err
		}
		workerDetails.SpaceID = createdSpaces[worker.SpaceSlug].SpaceID
		resp, err := cubClientNew.CreateBridgeWorkerWithResponse(ctx, createdSpaces[worker.SpaceSlug].SpaceID, *workerDetails)
		if err != nil {
			return err
		}
		if resp.JSON200 == nil {
			return fmt.Errorf("failed to create worker %s: %s", worker.Slug, resp.Body)
		}
		tprint("Created worker %s with id %s and space slug %s", worker.Slug, resp.JSON200.BridgeWorkerID.String(), worker.SpaceSlug)
		createdWorkers[worker.SpaceSlug+"/"+worker.Slug] = *resp.JSON200
	}
	for _, target := range manifest.Targets {
		if prefix != "" {
			target.SpaceSlug = prefix + "-" + target.SpaceSlug
			target.Worker = prefix + "-" + target.Worker
		}
		targetDetails, err := loadTargetDetails(dir, target)
		if err != nil {
			return err
		}
		targetDetails.SpaceID = createdSpaces[target.SpaceSlug].SpaceID
		worker, ok := createdWorkers[target.Worker]
		if !ok {
			return fmt.Errorf("worker %s not found for target %s", target.Worker, target.Slug)
		}
		targetDetails.BridgeWorkerID = worker.BridgeWorkerID
		resp, err := cubClientNew.CreateTargetWithResponse(ctx, createdSpaces[target.SpaceSlug].SpaceID, *targetDetails)
		if err != nil {
			return err
		}
		if resp.JSON200 == nil {
			return fmt.Errorf("failed to create target %s: %s", target.Slug, resp.Body)
		}
		tprint("Created target %s with id %s and space slug %s", target.Slug, resp.JSON200.TargetID.String(), target.SpaceSlug)
		createdTargets[target.SpaceSlug+"/"+target.Slug] = *resp.JSON200
	}
	for _, unit := range manifest.Units {
		if prefix != "" {
			unit.SpaceSlug = prefix + "-" + unit.SpaceSlug
			if unit.Target != "" {
				unit.Target = prefix + "-" + unit.Target
			}
		}
		unitDetails, err := loadUnitDetails(dir, unit)
		if err != nil {
			return err
		}
		unitDetails.SpaceID = createdSpaces[unit.SpaceSlug].SpaceID
		if unit.Target != "" {
			target, ok := createdTargets[unit.Target]
			if ok {
				unitDetails.TargetID = &target.TargetID
			}
		}
		unitData, err := os.ReadFile(dir + unit.UnitDataLoc)
		if err != nil {
			return err
		}
		unitDetails.Data = base64.StdEncoding.EncodeToString(unitData)
		resp, err := cubClientNew.CreateUnitWithResponse(ctx, createdSpaces[unit.SpaceSlug].SpaceID, &goclientnew.CreateUnitParams{}, *unitDetails)
		if err != nil {
			return err
		}
		if resp.JSON200 == nil {
			return fmt.Errorf("failed to create unit %s: %s", unit.Slug, resp.Body)
		}
		tprint("Created unit %s with id %s and space slug %s", unit.Slug, resp.JSON200.UnitID.String(), unit.SpaceSlug)
		awaitTriggersRemoval(resp.JSON200)
	}
	return nil
}

func loadManifest(dir string) (*PackageManifest, error) {
	manifest := &PackageManifest{}
	jsonBytes, err := os.ReadFile(dir + "/manifest.json")
	if err != nil {
		return nil, err
	}
	err = json.Unmarshal(jsonBytes, manifest)
	if err != nil {
		return nil, err
	}
	return manifest, nil
}

func loadSpaceDetails(dir string, space SpaceEntry) (*goclientnew.Space, error) {
	jsonBytes, err := os.ReadFile(dir + space.DetailsLoc)
	if err != nil {
		return nil, err
	}
	spaceDetails := &goclientnew.Space{}
	err = json.Unmarshal(jsonBytes, spaceDetails)
	if err != nil {
		return nil, err
	}
	spaceDetails.Slug = space.Slug
	return spaceDetails, nil
}

func loadWorkerDetails(dir string, worker WorkerEntry) (*goclientnew.BridgeWorker, error) {
	jsonBytes, err := os.ReadFile(dir + worker.DetailsLoc)
	if err != nil {
		return nil, err
	}
	workerDetails := &goclientnew.BridgeWorker{}
	err = json.Unmarshal(jsonBytes, workerDetails)
	if err != nil {
		return nil, err
	}
	workerDetails.Slug = worker.Slug
	return workerDetails, nil
}

func loadTargetDetails(dir string, target TargetEntry) (*goclientnew.Target, error) {
	jsonBytes, err := os.ReadFile(dir + target.DetailsLoc)
	if err != nil {
		return nil, err
	}
	targetDetails := &goclientnew.Target{}
	err = json.Unmarshal(jsonBytes, targetDetails)
	if err != nil {
		return nil, err
	}
	targetDetails.Slug = target.Slug
	return targetDetails, nil
}

func loadUnitDetails(dir string, unit UnitEntry) (*goclientnew.Unit, error) {
	jsonBytes, err := os.ReadFile(dir + unit.DetailsLoc)
	if err != nil {
		return nil, err
	}
	unitDetails := &goclientnew.Unit{}
	err = json.Unmarshal(jsonBytes, unitDetails)
	if err != nil {
		return nil, err
	}
	unitDetails.Slug = unit.Slug
	return unitDetails, nil
}
