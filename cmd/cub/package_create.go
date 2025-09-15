// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

// The create command creates a package by serializing a set of spaces, units, links, etc. from ConfigHub
// into a directory structure.

package main

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/confighub/sdk/cubapi"
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
	enableFilterFlag(packageCreateCmd)

	packageCmdGroup.AddCommand(packageCreateCmd)
}

func packageCreateCmdRun(cmd *cobra.Command, args []string) error {
	dir := args[0]
	if err := createDirIfNotExists(dir); err != nil {
		return err
	}

	// Fetch units
	newParams := &goclientnew.ListAllUnitsParams{}
	if where != "" {
		newParams.Where = &where
	}

	include := "UnitEventID,TargetID,SpaceID"
	newParams.Include = &include
	// No select. Get all fields.
	res, err := cubClientNew.ListAllUnits(ctx, newParams)
	if err != nil {
		return err
	}
	unitsRes, err := goclientnew.ParseListAllUnitsResponse(res)
	if cubapi.IsAPIError(err, unitsRes) {
		return cubapi.InterpretErrorGeneric(err, unitsRes)
	}

	// Fetch links based on the units we got
	var links []*goclientnew.ExtendedLink
	if len(*unitsRes.JSON200) > 0 {
		// Collect all unit IDs from the fetched units
		unitIDs := make([]string, 0, len(*unitsRes.JSON200))
		for _, unit := range *unitsRes.JSON200 {
			unitIDs = append(unitIDs, fmt.Sprintf("'%s'", unit.Unit.UnitID.String()))
		}

		// Query for links where FromUnitID is in our unit list
		// This captures all outgoing links from our units
		if len(unitIDs) > 0 {
			linkWhere := fmt.Sprintf("FromUnitID IN (%s)", strings.Join(unitIDs, ","))
			links, err = apiSearchLinks(linkWhere, "*", "")
			if err != nil {
				tprint("Warning: Could not fetch links: %v", err)
				links = []*goclientnew.ExtendedLink{}
			}
		}
	}
	// TODO: We need to decide what to do with dangling outgoing links For now, we'll assume they don't exist.
	// In the future, we want to record them in the manifest, and perhaps not store them as json files
	// because during load, the user will have to decide what to point them to.
	serializeEntities(dir, *unitsRes.JSON200, links)
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

// Track which space each worker belongs to
var workerSpaceMap = map[string]string{} // workerID -> spaceSlug

func addWorkerIfNotDone(dir string, manifest *PackageManifest, spaceID uuid.UUID, spaceSlug string, workerID uuid.UUID) (*string, error) {
	if addedWorkersByID[workerID.String()] != nil {
		return &addedWorkersByID[workerID.String()].Slug, nil
	}
	worker, err := apiGetBridgeWorker(spaceID, workerID, "*") // get all fields for now
	if err != nil {
		return nil, err
	}
	addedWorkersByID[workerID.String()] = worker
	workerSpaceMap[workerID.String()] = spaceSlug
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

func serializeEntities(dir string, unitList []goclientnew.ExtendedUnit, links []*goclientnew.ExtendedLink) error {
	var err error
	manifest := &PackageManifest{
		Spaces:      make([]SpaceEntry, 0),
		Units:       make([]UnitEntry, 0),
		Targets:     make([]TargetEntry, 0),
		Workers:     make([]WorkerEntry, 0),
		Links:       make([]LinkEntry, 0),
		Views:       make([]ViewEntry, 0),
		Filters:     make([]FilterEntry, 0),
		Tags:        make([]TagEntry, 0),
		Invocations: make([]InvocationEntry, 0),
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

	// Collect unique space IDs for fetching space-specific entities
	spaceMap := make(map[string]string) // spaceID -> spaceSlug
	for _, unit := range unitList {
		// Skip units with nil/zero space IDs
		if unit.Space.SpaceID == uuid.Nil {
			continue
		}
		spaceMap[unit.Space.SpaceID.String()] = unit.Space.Slug
	}

	// Serialize links
	err = createDirIfNotExists(dir + "/links")
	if err != nil {
		return err
	}

	for _, extendedLink := range links {
		err = addLink(dir, manifest, extendedLink, extendedLink.Space.Slug)
		if err != nil {
			tprint("Warning: Could not add link %s: %v", extendedLink.Link.Slug, err)
			continue
		}
	}

	// Fetch and serialize Views, Filters, Tags, and Invocations for each space
	for spaceID, spaceSlug := range spaceMap {
		// Fetch and serialize Views
		err = fetchAndSerializeViews(dir, manifest, spaceID, spaceSlug, spaceMap)
		if err != nil {
			tprint("Warning: Could not fetch views for space %s: %v", spaceSlug, err)
		}

		// Fetch and serialize Filters
		err = fetchAndSerializeFilters(dir, manifest, spaceID, spaceSlug, spaceMap)
		if err != nil {
			tprint("Warning: Could not fetch filters for space %s: %v", spaceSlug, err)
		}

		// Fetch and serialize Tags
		err = fetchAndSerializeTags(dir, manifest, spaceID, spaceSlug, spaceMap)
		if err != nil {
			tprint("Warning: Could not fetch tags for space %s: %v", spaceSlug, err)
		}

		// Fetch and serialize Invocations
		err = fetchAndSerializeInvocations(dir, manifest, spaceID, spaceSlug, spaceMap)
		if err != nil {
			tprint("Warning: Could not fetch invocations for space %s: %v", spaceSlug, err)
		}
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

func addLink(dir string, manifest *PackageManifest, extendedLink *goclientnew.ExtendedLink, spaceSlug string) error {
	// Create directory for the space's links
	err := createDirIfNotExists(dir + "/links/" + spaceSlug)
	if err != nil {
		return err
	}

	// Prune the link (remove IDs, timestamps, etc.)
	pruneLink(extendedLink.Link)

	// Serialize the entire pruned Link object (preserving all remaining fields)
	fileName := "/links/" + spaceSlug + "/" + extendedLink.Link.Slug + ".json"
	jsonBytes, err := json.MarshalIndent(extendedLink.Link, "", "  ")
	if err != nil {
		return err
	}
	err = os.WriteFile(dir+fileName, jsonBytes, 0644)
	if err != nil {
		return err
	}

	// Add link entry to manifest with slash notation for unit references
	linkEntry := LinkEntry{
		Slug:       extendedLink.Link.Slug,
		SpaceSlug:  spaceSlug,
		FromUnit:   spaceSlug + "/" + extendedLink.FromUnit.Slug,               // FromUnit is always in the link's space
		ToUnit:     extendedLink.ToSpace.Slug + "/" + extendedLink.ToUnit.Slug, // ToUnit with its space
		DetailsLoc: fileName,
	}
	manifest.Links = append(manifest.Links, linkEntry)
	return nil
}

func pruneLink(link *goclientnew.Link) {
	// Only remove IDs and system-generated fields
	// Everything else (like DisplayName, Description, etc.) is preserved
	link.LinkID = uuid.Nil
	link.OrganizationID = uuid.Nil
	link.SpaceID = uuid.Nil
	link.FromUnitID = uuid.Nil
	link.ToUnitID = uuid.Nil
	link.ToSpaceID = uuid.Nil
	link.CursorID = 0
	link.CreatedAt = time.Time{}
	link.UpdatedAt = time.Time{}
}

func fetchAndSerializeViews(dir string, manifest *PackageManifest, spaceID string, spaceSlug string, spaceMap map[string]string) error {
	views, err := apiListViews(spaceID, "", "*", "")
	if err != nil {
		return err
	}

	if len(views) == 0 {
		return nil
	}

	err = createDirIfNotExists(dir + "/views/" + spaceSlug)
	if err != nil {
		return err
	}

	for _, extendedView := range views {
		pruneView(extendedView.View)
		fileName := "/views/" + spaceSlug + "/" + extendedView.View.Slug + ".json"
		jsonBytes, err := json.MarshalIndent(extendedView.View, "", "  ")
		if err != nil {
			return err
		}
		err = os.WriteFile(dir+fileName, jsonBytes, 0644)
		if err != nil {
			return err
		}

		viewEntry := ViewEntry{
			Slug:       extendedView.View.Slug,
			SpaceSlug:  spaceSlug,
			DetailsLoc: fileName,
		}

		// Record filter reference if present
		if extendedView.Filter != nil {
			// Use the filter's SpaceID to find its space slug
			filterSpaceSlug := spaceSlug // Default to same space
			if extendedView.Filter.SpaceID != uuid.Nil {
				// Look up the space slug from our spaceMap
				if slug, ok := spaceMap[extendedView.Filter.SpaceID.String()]; ok {
					filterSpaceSlug = slug
				}
			}
			viewEntry.FilterSlug = filterSpaceSlug + "/" + extendedView.Filter.Slug
		}

		manifest.Views = append(manifest.Views, viewEntry)
	}
	return nil
}

func fetchAndSerializeFilters(dir string, manifest *PackageManifest, spaceID string, spaceSlug string, spaceMap map[string]string) error {
	filters, err := apiListFilters(spaceID, "", "*")
	if err != nil {
		return err
	}

	if len(filters) == 0 {
		return nil
	}

	err = createDirIfNotExists(dir + "/filters/" + spaceSlug)
	if err != nil {
		return err
	}

	for _, extendedFilter := range filters {
		pruneFilter(extendedFilter.Filter)
		fileName := "/filters/" + spaceSlug + "/" + extendedFilter.Filter.Slug + ".json"
		jsonBytes, err := json.MarshalIndent(extendedFilter.Filter, "", "  ")
		if err != nil {
			return err
		}
		err = os.WriteFile(dir+fileName, jsonBytes, 0644)
		if err != nil {
			return err
		}

		filterEntry := FilterEntry{
			Slug:       extendedFilter.Filter.Slug,
			SpaceSlug:  spaceSlug,
			DetailsLoc: fileName,
		}

		// Record FromSpace reference if present
		if extendedFilter.FromSpace != nil {
			filterEntry.FromSpaceSlug = extendedFilter.FromSpace.Slug
		}

		manifest.Filters = append(manifest.Filters, filterEntry)
	}
	return nil
}

func fetchAndSerializeTags(dir string, manifest *PackageManifest, spaceID string, spaceSlug string, spaceMap map[string]string) error {
	tags, err := apiListTags(spaceID, "", "*", "")
	if err != nil {
		return err
	}

	if len(tags) == 0 {
		return nil
	}

	err = createDirIfNotExists(dir + "/tags/" + spaceSlug)
	if err != nil {
		return err
	}

	for _, extendedTag := range tags {
		pruneTag(extendedTag.Tag)
		fileName := "/tags/" + spaceSlug + "/" + extendedTag.Tag.Slug + ".json"
		jsonBytes, err := json.MarshalIndent(extendedTag.Tag, "", "  ")
		if err != nil {
			return err
		}
		err = os.WriteFile(dir+fileName, jsonBytes, 0644)
		if err != nil {
			return err
		}

		tagEntry := TagEntry{
			Slug:       extendedTag.Tag.Slug,
			SpaceSlug:  spaceSlug,
			DetailsLoc: fileName,
		}
		manifest.Tags = append(manifest.Tags, tagEntry)
	}
	return nil
}

func fetchAndSerializeInvocations(dir string, manifest *PackageManifest, spaceID string, spaceSlug string, spaceMap map[string]string) error {
	invocations, err := apiListInvocations(spaceID, "", "*", "")
	if err != nil {
		return err
	}

	if len(invocations) == 0 {
		return nil
	}

	err = createDirIfNotExists(dir + "/invocations/" + spaceSlug)
	if err != nil {
		return err
	}

	for _, extendedInvocation := range invocations {
		pruneInvocation(extendedInvocation.Invocation)
		fileName := "/invocations/" + spaceSlug + "/" + extendedInvocation.Invocation.Slug + ".json"
		jsonBytes, err := json.MarshalIndent(extendedInvocation.Invocation, "", "  ")
		if err != nil {
			return err
		}
		err = os.WriteFile(dir+fileName, jsonBytes, 0644)
		if err != nil {
			return err
		}

		invocationEntry := InvocationEntry{
			Slug:       extendedInvocation.Invocation.Slug,
			SpaceSlug:  spaceSlug,
			DetailsLoc: fileName,
		}

		// Track BridgeWorker reference if present
		if extendedInvocation.BridgeWorker != nil {
			// Use the worker's ID to find its space slug from our tracking map
			workerSpaceSlug := spaceSlug // Default to same space
			if extendedInvocation.BridgeWorker.BridgeWorkerID != uuid.Nil {
				// First check our tracking map
				if slug, ok := workerSpaceMap[extendedInvocation.BridgeWorker.BridgeWorkerID.String()]; ok {
					workerSpaceSlug = slug
				} else if extendedInvocation.BridgeWorker.SpaceID != uuid.Nil {
					// If not in tracking map, look up the space slug from spaceMap
					if slug, ok := spaceMap[extendedInvocation.BridgeWorker.SpaceID.String()]; ok {
						workerSpaceSlug = slug
					}
				}
			}
			invocationEntry.WorkerSlug = workerSpaceSlug + "/" + extendedInvocation.BridgeWorker.Slug
		}

		manifest.Invocations = append(manifest.Invocations, invocationEntry)
	}
	return nil
}

func pruneView(view *goclientnew.View) {
	view.ViewID = uuid.Nil
	view.OrganizationID = uuid.Nil
	view.SpaceID = uuid.Nil
	view.FilterID = uuid.Nil
	view.CursorID = 0
	view.CreatedAt = time.Time{}
	view.UpdatedAt = time.Time{}
}

func pruneFilter(filter *goclientnew.Filter) {
	filter.FilterID = uuid.Nil
	filter.OrganizationID = uuid.Nil
	filter.SpaceID = uuid.Nil
	// FromSpaceID is optional pointer
	filter.FromSpaceID = nil
	filter.CursorID = 0
	filter.CreatedAt = time.Time{}
	filter.UpdatedAt = time.Time{}
}

func pruneTag(tag *goclientnew.Tag) {
	tag.TagID = uuid.Nil
	tag.OrganizationID = uuid.Nil
	tag.SpaceID = uuid.Nil
	tag.CursorID = 0
	tag.CreatedAt = time.Time{}
	tag.UpdatedAt = time.Time{}
}

func pruneInvocation(invocation *goclientnew.Invocation) {
	invocation.InvocationID = uuid.Nil
	invocation.OrganizationID = uuid.Nil
	invocation.SpaceID = uuid.Nil
	invocation.CursorID = 0
	invocation.CreatedAt = time.Time{}
	invocation.UpdatedAt = time.Time{}
}

func writeManifest(dir string, manifest *PackageManifest) error {
	fileName := dir + "/manifest.json"
	jsonBytes, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(fileName, jsonBytes, 0644)
}
