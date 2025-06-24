// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"encoding/csv"
	"fmt"
	"os"
	"path/filepath"

	goclientnew "github.com/confighub/sdk/openapi/goclient-new"
	"github.com/go-openapi/strfmt"
	"github.com/spf13/cobra"
)

var confighubApi *ConfighubApi

var datasetLoadCmd = &cobra.Command{
	Use:   "load <dir>",
	Short: "Load an entire dataset of spaces, units, etc into an org",
	Long:  "Load an entire dataset of spaces, units, etc into an org",
	Args:  cobra.ExactArgs(1),
	RunE:  datasetLoadCmdRun,
}

func init() {
	datasetCmdGroup.AddCommand(datasetLoadCmd)
}

func datasetLoadCmdRun(cmd *cobra.Command, args []string) error {
	apiInfo := GetApiInfo()
	tprint("Using Client: %s", apiInfo.ClientID)
	if apiInfo.ClientID != "client_01J6JJSJ7C93DX3ZYVEKYFA6NY" {
		return fmt.Errorf("dataset loading is only supported for staging at the moment")
	}
	var err error
	confighubApi, err = NewConfighubApi()
	if err != nil {
		return err
	}
	dir := args[0]
	_, err = IsDir(dir)
	if err != nil {
		return fmt.Errorf("error checking if %s is a directory: %w", dir, err)
	}

	tprint("Creating spaces...")
	err = CreateSpaces(dir)
	if err != nil {
		return err
	}
	tprint("Creating workers...")
	err = CreateWorkers(dir)
	if err != nil {
		return err
	}
	tprint("Creating targets...")
	err = CreateTargets(dir)
	if err != nil {
		return err
	}
	tprint("Creating units...")
	err = CreateUnits(dir)
	if err != nil {
		return err
	}
	tprint("Creating links...")
	err = CreateLinks(dir)
	if err != nil {
		return err
	}

	return nil
}

func CreateSpaces(dir string) error {
	abspath := filepath.Join(dir, "spaces.csv")
	records, err := ReadCSV(abspath)
	if err != nil {
		return err
	}
	for _, record := range records {
		tprint("Creating space %s", record[0])
		confighubApi.CreateSpace(goclientnew.Space{
			Slug:        record[0],
			DisplayName: record[0],
		})
	}
	return nil
}

func CreateWorkers(dir string) error {
	abspath := filepath.Join(dir, "workers.csv")
	records, err := ReadCSV(abspath)
	if err != nil {
		return err
	}
	for i, record := range records {
		_, err = confighubApi.CreateWorker(record[0], record[1])
		if err != nil {
			tprint("Line %d: Error creating worker: %v", i+1, err)
		}
	}
	return nil
}

func CreateTargets(dir string) error {
	abspath := filepath.Join(dir, "targets.csv")
	records, err := ReadCSV(abspath)
	if err != nil {
		return err
	}
	// format:
	// slug, spaceSlug, workerSlug
	for i, record := range records {
		_, err = confighubApi.CreateTarget(record[0], record[1], record[2])
		if err != nil {
			tprint("Line %d: Error creating target: %v", i+1, err)
		}
	}
	return nil
}

func CreateUnits(dir string) error {
	abspath := filepath.Join(dir, "units.csv")
	records, err := ReadCSV(abspath)
	if err != nil {
		return err
	}
	// format:
	// slug, spaceSlug, targetSlug, upstream
	// targetSlug and upstream can be empty. If upstream is set, unit data will be ignored
	for i, record := range records {
		unit := &goclientnew.Unit{
			Slug:          record[0],
			DisplayName:   record[0],
			ToolchainType: "Kubernetes/YAML",
		}
		if record[3] == "" {
			// No upstream, so read in the config file
			dataFile := filepath.Join(dir, "unit-data", record[1], record[0]+".yaml")
			tprint("Reading unit data from %s", dataFile)
			if _, err := os.Stat(dataFile); err != nil {
				tprint("Line %d: Error reading unit data file from location %s: %v", i+1, dataFile, err)
			} else {
				var content strfmt.Base64
				content = readFile(dataFile)
				unit.Data = content.String()
			}
		}
		_, err = confighubApi.CreateUnit(record[0], record[1], record[2], record[3], unit)
		if err != nil {
			tprint("Line %d: Error creating unit: %v", i+1, err)
		}
	}
	return nil
}

func CreateLinks(dir string) error {
	abspath := filepath.Join(dir, "links.csv")
	records, err := ReadCSV(abspath)
	if err != nil {
		return err
	}
	// format:
	// slug, fromUnitSlug, fromUnitSpaceSlug, toUnitSlug, toUnitSpaceSlug
	for i, record := range records {
		_, err = confighubApi.CreateLink(record[0], record[1], record[2], record[3], record[4])
		if err != nil {
			tprint("Line %d: Error creating link: %v", i+1, err)
		}
	}
	return nil
}

func IsDir(path string) (bool, error) {
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil // does not exist
		}
		return false, err // other error
	}
	return info.IsDir(), nil
}

func ReadCSV(fileName string) ([][]string, error) {
	// Open the CSV file
	file, err := os.Open(fileName)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	// Create a new CSV reader
	reader := csv.NewReader(file)

	// Read all records
	records, err := reader.ReadAll()
	if err != nil {
		return nil, err
	}
	return records, nil
}

type ConfighubApi struct {
	spaces  map[string]*goclientnew.Space
	workers map[string]*goclientnew.BridgeWorker
	targets map[string]*goclientnew.Target
	units   map[string]*goclientnew.Unit
	links   map[string]*goclientnew.Link
}

func NewConfighubApi() (*ConfighubApi, error) {
	c := &ConfighubApi{
		spaces:  make(map[string]*goclientnew.Space),
		workers: make(map[string]*goclientnew.BridgeWorker),
		targets: make(map[string]*goclientnew.Target),
		units:   make(map[string]*goclientnew.Unit),
		links:   make(map[string]*goclientnew.Link),
	}

	spaces, err := apiListSpaces("")
	if err != nil {
		return nil, err
	}
	tprint("Fetched %d spaces", len(spaces))
	for _, space := range spaces {
		c.spaces[space.Slug] = space
		spaceid := space.SpaceID.String()
		workers, err := apiListBridgeworkers(spaceid)
		if err != nil {
			return nil, err
		}
		tprint("Fetched %d workers in space %s", len(workers), space.Slug)
		for _, worker := range workers {
			c.workers[space.Slug+"/"+worker.Slug] = worker
		}
		targets, err := apiListTargets(spaceid, "")
		if err != nil {
			return nil, err
		}
		tprint("Fetched %d targets in space %s", len(targets), space.Slug)
		for _, target := range targets {
			c.targets[space.Slug+"/"+target.Target.Slug] = target.Target
		}
		units, err := apiListUnits(spaceid, "")
		if err != nil {
			return nil, err
		}
		tprint("Fetched %d units in space %s", len(units), space.Slug)
		for _, unit := range units {
			c.units[space.Slug+"/"+unit.Slug] = unit
		}
		links, err := apiListLinks(spaceid, "")
		if err != nil {
			return nil, err
		}
		tprint("Fetched %d links in space %s", len(links), space.Slug)
		for _, link := range links {
			c.links[space.Slug+"/"+link.Slug] = link
		}
	}
	return c, nil
}

func (c *ConfighubApi) GetSpace(slug string) (*goclientnew.Space, bool) {
	space, ok := c.spaces[slug]
	return space, ok
}
func (c *ConfighubApi) GetWorker(spaceSlug, slug string) (*goclientnew.BridgeWorker, bool) {
	worker, ok := c.workers[spaceSlug+"/"+slug]
	return worker, ok
}
func (c *ConfighubApi) GetTarget(spaceSlug, slug string) (*goclientnew.Target, bool) {
	target, ok := c.targets[spaceSlug+"/"+slug]
	return target, ok
}
func (c *ConfighubApi) GetUnit(spaceSlug, slug string) (*goclientnew.Unit, bool) {
	unit, ok := c.units[spaceSlug+"/"+slug]
	return unit, ok
}
func (c *ConfighubApi) GetLink(spaceSlug, slug string) (*goclientnew.Link, bool) {
	link, ok := c.links[spaceSlug+"/"+slug]
	return link, ok
}

func (c *ConfighubApi) CreateSpace(spaceDetails goclientnew.Space) (*goclientnew.Space, error) {
	spaceRes, err := cubClientNew.CreateSpaceWithResponse(ctx, spaceDetails)
	if IsAPIError(err, spaceRes) {
		tprint("Error creating space: %v", err)
		return nil, err
	}
	c.spaces[spaceDetails.Slug] = spaceRes.JSON200
	return spaceRes.JSON200, nil
}

func (c *ConfighubApi) CreateWorker(slug, spaceSlug string) (*goclientnew.BridgeWorker, error) {
	space, ok := c.GetSpace(spaceSlug)
	if !ok {
		return nil, fmt.Errorf("space %s not found", spaceSlug)
	}
	workerDetails := goclientnew.BridgeWorker{
		Slug:        slug,
		DisplayName: slug,
		SpaceID:     space.SpaceID,
	}
	worker, err := apiCreateWorker(&workerDetails, space.SpaceID)
	if err != nil {
		return nil, err
	}
	c.workers[spaceSlug+"/"+workerDetails.Slug] = worker
	return worker, nil
}

func (c *ConfighubApi) CreateTarget(slug, spaceSlug, workerSlug string) (*goclientnew.Target, error) {
	space, ok := c.GetSpace(spaceSlug)
	if !ok {
		return nil, fmt.Errorf("space %s not found", spaceSlug)
	}
	worker, ok := c.GetWorker(spaceSlug, workerSlug)
	if !ok {
		return nil, fmt.Errorf("worker %s not found", workerSlug)
	}
	targetDetails := goclientnew.Target{
		Slug:           slug,
		DisplayName:    slug,
		SpaceID:        space.SpaceID,
		Parameters:     "{}",
		ToolchainType:  "Kubernetes/YAML",
		ProviderType:   "Kubernetes",
		BridgeWorkerID: worker.BridgeWorkerID,
	}
	targetRes, err := cubClientNew.CreateTargetWithResponse(ctx, space.SpaceID, targetDetails)
	if IsAPIError(err, targetRes) {
		return nil, InterpretErrorGeneric(err, targetRes)
	}
	c.targets[spaceSlug+"/"+targetDetails.Slug] = targetRes.JSON200
	return targetRes.JSON200, nil
}

func (c *ConfighubApi) CreateUnit(slug, spaceSlug, targetSlug, upstream string, unitDetails *goclientnew.Unit) (*goclientnew.Unit, error) {
	space, ok := c.GetSpace(spaceSlug)
	if !ok {
		return nil, fmt.Errorf("space %s not found", spaceSlug)
	}
	unitDetails.SpaceID = space.SpaceID
	if targetSlug != "" {
		target, ok := c.GetTarget(spaceSlug, targetSlug)
		if !ok {
			return nil, fmt.Errorf("target %s not found", targetSlug)
		}
		unitDetails.TargetID = &target.TargetID
	}
	unitParams := &goclientnew.CreateUnitParams{}
	if upstream != "" {
		upstreamUnit, ok := c.units[upstream]
		if !ok {
			tprint("Upstream unit %s not found", upstream)
		} else {
			unitParams.UpstreamSpaceId = &upstreamUnit.SpaceID
			unitParams.UpstreamUnitId = &upstreamUnit.UnitID
		}
	}
	unitRes, err := cubClientNew.CreateUnitWithResponse(ctx, space.SpaceID, unitParams, *unitDetails)
	if IsAPIError(err, unitRes) {
		return nil, InterpretErrorGeneric(err, unitRes)
	}
	c.units[spaceSlug+"/"+unitDetails.Slug] = unitRes.JSON200
	return unitRes.JSON200, nil
}

func (c *ConfighubApi) CreateLink(slug, fromSlug, fromSpaceSlug, toSlug, toSpaceSlug string) (*goclientnew.Link, error) {
	fromSpace, ok := c.GetSpace(fromSpaceSlug)
	if !ok {
		return nil, fmt.Errorf("space %s not found", fromSpaceSlug)
	}
	toSpace, ok := c.GetSpace(toSpaceSlug)
	if !ok {
		return nil, fmt.Errorf("space %s not found", toSpaceSlug)
	}
	fromUnit, ok := c.GetUnit(fromSpaceSlug, fromSlug)
	if !ok {
		return nil, fmt.Errorf("unit %s not found", fromSlug)
	}
	toUnit, ok := c.GetUnit(toSpaceSlug, toSlug)
	if !ok {
		return nil, fmt.Errorf("unit %s not found", toSlug)
	}
	linkDetails := goclientnew.Link{
		Slug:        slug,
		DisplayName: slug,
		FromUnitID:  fromUnit.UnitID,
		SpaceID:     fromSpace.SpaceID,
		ToUnitID:    toUnit.UnitID,
		ToSpaceID:   toSpace.SpaceID,
	}
	linkRes, err := cubClientNew.CreateLinkWithResponse(ctx, fromSpace.SpaceID, linkDetails)
	if IsAPIError(err, linkRes) {
		return nil, InterpretErrorGeneric(err, linkRes)
	}
	return linkRes.JSON200, nil
}
