// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

// The create command creates a package by serializing a set of spaces, units, links, etc. from ConfigHub
// into a directory structure.

package main

import (
	"encoding/base64"
	"encoding/json"
	"os"
	"strings"
	"time"

	goclientnew "github.com/confighub/sdk/openapi/goclient-new"
	"github.com/google/uuid"
	"github.com/spf13/cobra"
)

var packageCreateCmd = &cobra.Command{
	Use:   "create <dir>",
	Short: "create a package in a directory",
	Long:  `create a package by serializing a set of spaces, units, links, etc. from ConfigHub into a directory structure.`,
	Args:  cobra.ExactArgs(1),
	RunE:  packageCreateCmdRun,
}

func init() {
	addSpaceFlags(packageCreateCmd)
	enableWhereFlag(packageCreateCmd)

	packageCmdGroup.AddCommand(packageCreateCmd)
}

func packageCreateCmdRun(cmd *cobra.Command, args []string) error {
	dir := args[0]
	if err := createDirIfNotExists(dir); err != nil {
		return err
	}

	newParams := &goclientnew.ListAllUnitsParams{}
	if where != "" {
		newParams.Where = &where
	}

	include := "UnitEventID,TargetID,SpaceID"
	newParams.Include = &include
	res, err := cubClientNew.ListAllUnits(ctx, newParams)
	if err != nil {
		return err
	}
	unitsRes, err := goclientnew.ParseListAllUnitsResponse(res)
	if IsAPIError(err, unitsRes) {
		return InterpretErrorGeneric(err, unitsRes)
	}
	serializeEntities(dir, *unitsRes.JSON200)
	return nil
}

func createDirIfNotExists(dir string) error {
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		return os.MkdirAll(dir, 0755)
	}
	return nil
}

func addSpaceIfNotDone(dir string, manifest *PackageManifest, space *goclientnew.Space) error {
	for _, spaceEntry := range manifest.Spaces {
		if spaceEntry.Slug == space.Slug {
			return nil
		}
	}
	pruneSpace(space)
	fileName := dir + "/spaces/" + space.Slug + ".json"
	jsonBytes, err := json.MarshalIndent(space, "", "  ")
	if err != nil {
		return err
	}
	err = os.WriteFile(fileName, jsonBytes, 0644)
	if err != nil {
		return err
	}
	spaceEntry := SpaceEntry{
		Slug:       space.Slug,
		DetailsLoc: "/spaces/" + space.Slug + ".json",
	}
	manifest.Spaces = append(manifest.Spaces, spaceEntry)
	return nil
}

// small optimization to avoid fetching same worker multiple times
var addedWorkersByID = map[string]*goclientnew.BridgeWorker{}

func addWorkerIfNotDone(dir string, manifest *PackageManifest, spaceID uuid.UUID, spaceSlug string, workerID uuid.UUID) (*string, error) {
	if addedWorkersByID[workerID.String()] != nil {
		return &addedWorkersByID[workerID.String()].Slug, nil
	}
	worker, err := apiGetBridgeWorker(spaceID, workerID)
	if err != nil {
		return nil, err
	}
	addedWorkersByID[workerID.String()] = worker
	err = createDirIfNotExists(dir + "/workers/" + spaceSlug)
	if err != nil {
		return nil, err
	}
	pruneWorker(worker)
	fileName := "/workers/" + spaceSlug + "/" + worker.Slug + ".json"
	jsonBytes, err := json.MarshalIndent(worker, "", "  ")
	if err != nil {
		return nil, err
	}
	err = os.WriteFile(dir+fileName, jsonBytes, 0644)
	if err != nil {
		return nil, err
	}
	workerEntry := WorkerEntry{
		Slug:       worker.Slug,
		SpaceSlug:  spaceSlug,
		DetailsLoc: fileName,
	}
	manifest.Workers = append(manifest.Workers, workerEntry)
	return &worker.Slug, nil
}

func addTargetAndWorkerIfNotDone(dir string, manifest *PackageManifest, spaceSlug string, target *goclientnew.Target) error {
	if target == nil {
		return nil
	}
	for _, targetEntry := range manifest.Targets {
		if targetEntry.Slug == target.Slug {
			return nil
		}
	}
	workerSlug, err := addWorkerIfNotDone(dir, manifest, target.SpaceID, spaceSlug, target.BridgeWorkerID)
	if err != nil {
		return err
	}
	pruneTarget(target)
	err = createDirIfNotExists(dir + "/targets/" + spaceSlug)
	if err != nil {
		return err
	}
	fileName := "/targets/" + spaceSlug + "/" + target.Slug + ".json"
	jsonBytes, err := json.MarshalIndent(target, "", "  ")
	if err != nil {
		return err
	}
	err = os.WriteFile(dir+fileName, jsonBytes, 0644)
	if err != nil {
		return err
	}
	targetEntry := TargetEntry{
		Slug:       target.Slug,
		SpaceSlug:  spaceSlug,
		Worker:     spaceSlug + "/" + *workerSlug,
		DetailsLoc: fileName,
	}
	manifest.Targets = append(manifest.Targets, targetEntry)
	return nil
}

func serializeEntities(dir string, unitList []goclientnew.ExtendedUnit) error {
	var err error
	manifest := &PackageManifest{
		Spaces:  make([]SpaceEntry, 0),
		Units:   make([]UnitEntry, 0),
		Targets: make([]TargetEntry, 0),
		Workers: make([]WorkerEntry, 0),
	}
	//collectSpacesAndTargets(unitList)
	//err = loadWorkersForTargets()
	if err != nil {
		return err
	}
	err = createDirIfNotExists(dir + "/spaces")
	if err != nil {
		return err
	}
	for _, unit := range unitList {
		err = addSpaceIfNotDone(dir, manifest, unit.Space)
		if err != nil {
			return err
		}
		err = addTargetAndWorkerIfNotDone(dir, manifest, unit.Space.Slug, unit.Target)
		if err != nil {
			return err
		}
		createDirIfNotExists(dir + "/units/" + unit.Space.Slug)
		fileName := "/units/" + unit.Space.Slug + "/" + unit.Unit.Slug + ".json"
		// base64 decode the data
		unitData, err := base64.StdEncoding.DecodeString(unit.Unit.Data)
		if err != nil {
			return err
		}
		toolchainParts := strings.Split(unit.Unit.ToolchainType, "/")
		suffix := ""
		if len(toolchainParts) > 1 {
			suffix = "." + strings.ToLower(toolchainParts[1])
		}
		dataFileName := "/units/" + unit.Space.Slug + "/" + unit.Unit.Slug + ".data" + suffix
		err = os.WriteFile(dir+dataFileName, unitData, 0644)
		if err != nil {
			return err
		}
		pruneUnit(unit.Unit)
		jsonBytes, err := json.MarshalIndent(unit.Unit, "", "  ")
		if err != nil {
			return err
		}
		err = os.WriteFile(dir+fileName, jsonBytes, 0644)
		if err != nil {
			return err
		}
		unitEntry := UnitEntry{
			Slug:        unit.Unit.Slug,
			SpaceSlug:   unit.Space.Slug,
			DetailsLoc:  fileName,
			UnitDataLoc: dataFileName,
		}
		if unit.Target != nil {
			unitEntry.Target = unit.Space.Slug + "/" + unit.Target.Slug
		}
		manifest.Units = append(manifest.Units, unitEntry)
	}
	err = writeManifest(dir, manifest)
	if err != nil {
		return err
	}
	return nil
}

func pruneUnit(unit *goclientnew.Unit) {
	unit.UnitID = uuid.Nil
	unit.OrganizationID = uuid.Nil
	unit.SpaceID = uuid.Nil
	unit.SetID = nil
	unit.UpstreamOrganizationID = nil
	unit.UpstreamSpaceID = nil
	unit.UpstreamUnitID = nil
	unit.LastChangeDescription = ""
	unit.Annotations = nil
	unit.ApplyGates = nil
	unit.ApprovedBy = nil
	unit.CursorID = 0
	unit.LiveState = ""
	unit.MutationSources = nil
	unit.LastAppliedRevisionNum = 0
	unit.HeadRevisionNum = 0
	unit.HeadMutationNum = 0
	unit.LastChangeDescription = ""
	unit.LiveRevisionNum = 0
	unit.CreatedAt = time.Time{}
	unit.Data = ""
	unit.TargetID = nil
}

func pruneSpace(space *goclientnew.Space) {
	space.SpaceID = uuid.Nil
	space.OrganizationID = uuid.Nil
	space.CursorID = 0
}

func pruneTarget(target *goclientnew.Target) {
	target.TargetID = uuid.Nil
	target.OrganizationID = uuid.Nil
	target.SpaceID = uuid.Nil
	target.CursorID = 0
	target.BridgeWorkerID = uuid.Nil
}

func pruneWorker(worker *goclientnew.BridgeWorker) {
	worker.BridgeWorkerID = uuid.Nil
	worker.OrganizationID = uuid.Nil
	worker.SpaceID = uuid.Nil
	worker.Secret = ""
	worker.CursorID = 0
	worker.LastSeenAt = time.Time{}
	worker.Condition = ""
}

func writeManifest(dir string, manifest *PackageManifest) error {
	fileName := dir + "/manifest.json"
	jsonBytes, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(fileName, jsonBytes, 0644)
}
