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
	"strings"

	goclientnew "github.com/confighub/sdk/core/openapi/goclient-new"
	"github.com/spf13/cobra"
)

var packageLoadCmd = &cobra.Command{
	Use:   "load <dir-or-url>",
	Short: "load a package from a directory or URL",
	Long: getCommandHelp(`load a package by deserializing a directory structure or remote URL into ConfigHub spaces, units, links, etc.

Supports:
  - Local directories: ./my-package or /path/to/package
  - HTTP/HTTPS URLs: https://example.com/packages/my-package
  - GitHub repositories: https://github.com/user/repo/tree/main/packages/my-package`, ""),
	Args: cobra.ExactArgs(1),
	RunE: packageLoadCmdRun,
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
var createdLinks = map[string]goclientnew.Link{}
var createdViews = map[string]goclientnew.View{}
var createdFilters = map[string]goclientnew.Filter{}
var createdTags = map[string]goclientnew.Tag{}
var createdInvocations = map[string]goclientnew.Invocation{}
var createdTriggers = map[string]goclientnew.Trigger{}
var createdAttributes = map[string]goclientnew.Attribute{}

func packageLoadCmdRun(cmd *cobra.Command, args []string) error {
	source := args[0]
	prefix, err := cmd.Flags().GetString("prefix")
	if err != nil {
		return err
	}

	// Determine if source is remote or local
	if isRemoteURL(source) {
		return loadRemotePackage(source, prefix)
	}

	// Local directory loading
	if _, err := os.Stat(source); os.IsNotExist(err) {
		return fmt.Errorf("package directory does not exist: %s", source)
	}
	manifest, err := loadManifest(source)
	if err != nil {
		return err
	}
	for _, space := range manifest.Spaces {
		if prefix != "" {
			space.Slug = prefix + "-" + space.Slug
		}
		spaceDetails, err := loadSpaceDetails(source, space)
		if err != nil {
			return err
		}
		resp, err := cubClientNew.CreateSpaceWithResponse(ctx, nil, *spaceDetails)
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
		workerDetails, err := loadWorkerDetails(source, worker)
		if err != nil {
			return err
		}
		workerDetails.SpaceID = createdSpaces[worker.SpaceSlug].SpaceID
		workerParams := &goclientnew.CreateBridgeWorkerParams{}
		resp, err := cubClientNew.CreateBridgeWorkerWithResponse(ctx, createdSpaces[worker.SpaceSlug].SpaceID, workerParams, *workerDetails)
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
		targetDetails, err := loadTargetDetails(source, target)
		if err != nil {
			return err
		}
		targetDetails.SpaceID = createdSpaces[target.SpaceSlug].SpaceID
		worker, ok := createdWorkers[target.Worker]
		if !ok {
			return fmt.Errorf("worker %s not found for target %s", target.Worker, target.Slug)
		}
		targetDetails.BridgeWorkerID = worker.BridgeWorkerID
		resp, err := cubClientNew.CreateTargetWithResponse(ctx, createdSpaces[target.SpaceSlug].SpaceID, nil, *targetDetails)
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
		unitDetails, err := loadUnitDetails(source, unit)
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
		unitData, err := os.ReadFile(source + unit.UnitDataLoc)
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
		createdUnits[unit.SpaceSlug+"/"+unit.Slug] = *resp.JSON200
	}
	for _, link := range manifest.Links {
		if prefix != "" {
			link.SpaceSlug = prefix + "-" + link.SpaceSlug
			// FromUnit and ToUnit use slash notation, need to prefix the space part
			if link.FromUnit != "" {
				parts := strings.Split(link.FromUnit, "/")
				if len(parts) == 2 {
					link.FromUnit = prefix + "-" + parts[0] + "/" + parts[1]
				}
			}
			if link.ToUnit != "" {
				parts := strings.Split(link.ToUnit, "/")
				if len(parts) == 2 {
					link.ToUnit = prefix + "-" + parts[0] + "/" + parts[1]
				}
			}
		}
		linkDetails, err := loadLinkDetails(source, link)
		if err != nil {
			return err
		}
		linkDetails.SpaceID = createdSpaces[link.SpaceSlug].SpaceID

		// Find the from unit using the slash notation
		fromUnit, ok := createdUnits[link.FromUnit]
		if !ok {
			return fmt.Errorf("from unit %s not found for link %s", link.FromUnit, link.Slug)
		}
		linkDetails.FromUnitID = fromUnit.UnitID

		// Find the to unit using the slash notation
		toUnit, ok := createdUnits[link.ToUnit]
		if !ok {
			return fmt.Errorf("to unit %s not found for link %s", link.ToUnit, link.Slug)
		}
		linkDetails.ToUnitID = toUnit.UnitID

		// Extract ToSpaceSlug from ToUnit slash notation
		toParts := strings.Split(link.ToUnit, "/")
		if len(toParts) == 2 {
			toSpaceSlug := toParts[0]
			linkDetails.ToSpaceID = createdSpaces[toSpaceSlug].SpaceID
		}

		resp, err := cubClientNew.CreateLinkWithResponse(ctx, createdSpaces[link.SpaceSlug].SpaceID, nil, *linkDetails)
		if err != nil {
			return err
		}
		if resp.JSON200 == nil {
			return fmt.Errorf("failed to create link %s: %s", link.Slug, resp.Body)
		}
		tprint("Created link %s with id %s in space %s", link.Slug, resp.JSON200.LinkID.String(), link.SpaceSlug)
	}
	// Load Filters first (as Views may reference them)
	for _, filter := range manifest.Filters {
		if prefix != "" {
			filter.SpaceSlug = prefix + "-" + filter.SpaceSlug
			if filter.FromSpaceSlug != "" {
				filter.FromSpaceSlug = prefix + "-" + filter.FromSpaceSlug
			}
		}
		filterDetails, err := loadFilterDetails(source, filter)
		if err != nil {
			return err
		}
		if filter.SpaceSlug != "" {
			filterDetails.SpaceID = createdSpaces[filter.SpaceSlug].SpaceID
		}
		// Set FromSpaceID if FromSpaceSlug is specified
		if filter.FromSpaceSlug != "" {
			fromSpaceID := createdSpaces[filter.FromSpaceSlug].SpaceID
			filterDetails.FromSpaceID = &fromSpaceID
		}

		resp, err := cubClientNew.CreateFilterWithResponse(ctx, createdSpaces[filter.SpaceSlug].SpaceID, nil, *filterDetails)
		if err != nil {
			return err
		}
		if resp.JSON200 == nil {
			return fmt.Errorf("failed to create filter %s: %s", filter.Slug, resp.Body)
		}
		tprint("Created filter %s with id %s", filter.Slug, resp.JSON200.FilterID.String())
		createdFilters[filter.SpaceSlug+"/"+filter.Slug] = *resp.JSON200
	}
	// Load Views (after Filters since they may reference them)
	for _, view := range manifest.Views {
		if prefix != "" {
			view.SpaceSlug = prefix + "-" + view.SpaceSlug
			if view.FilterSlug != "" {
				// FilterSlug contains "space/filter" format, need to prefix the space part
				parts := strings.Split(view.FilterSlug, "/")
				if len(parts) == 2 {
					view.FilterSlug = prefix + "-" + parts[0] + "/" + parts[1]
				}
			}
		}
		viewDetails, err := loadViewDetails(source, view)
		if err != nil {
			return err
		}
		viewDetails.SpaceID = createdSpaces[view.SpaceSlug].SpaceID

		// Set FilterID if FilterSlug is specified
		if view.FilterSlug != "" {
			// FilterSlug already contains the full path (e.g., "space28643/headlamp")
			// Use it directly as the key
			filterKey := view.FilterSlug
			if filter, ok := createdFilters[filterKey]; ok {
				viewDetails.FilterID = filter.FilterID
			} else {
				tprint("Warning: Filter %s not found for view %s", filterKey, view.Slug)
			}
		}

		resp, err := cubClientNew.CreateViewWithResponse(ctx, createdSpaces[view.SpaceSlug].SpaceID, nil, *viewDetails)
		if err != nil {
			return err
		}
		if resp.JSON200 == nil {
			return fmt.Errorf("failed to create view %s: %s", view.Slug, resp.Body)
		}
		tprint("Created view %s with id %s in space %s", view.Slug, resp.JSON200.ViewID.String(), view.SpaceSlug)
		createdViews[view.SpaceSlug+"/"+view.Slug] = *resp.JSON200
	}
	// Load Tags
	for _, tag := range manifest.Tags {
		if prefix != "" {
			tag.SpaceSlug = prefix + "-" + tag.SpaceSlug
		}
		tagDetails, err := loadTagDetails(source, tag)
		if err != nil {
			return err
		}
		tagDetails.SpaceID = createdSpaces[tag.SpaceSlug].SpaceID

		resp, err := cubClientNew.CreateTagWithResponse(ctx, createdSpaces[tag.SpaceSlug].SpaceID, nil, *tagDetails)
		if err != nil {
			return err
		}
		if resp.JSON200 == nil {
			return fmt.Errorf("failed to create tag %s: %s", tag.Slug, resp.Body)
		}
		tprint("Created tag %s with id %s in space %s", tag.Slug, resp.JSON200.TagID.String(), tag.SpaceSlug)
		createdTags[tag.SpaceSlug+"/"+tag.Slug] = *resp.JSON200
	}
	// Load Invocations
	for _, invocation := range manifest.Invocations {
		if prefix != "" {
			invocation.SpaceSlug = prefix + "-" + invocation.SpaceSlug
			if invocation.WorkerSlug != "" {
				// WorkerSlug contains "space/worker" format, need to prefix the space part
				parts := strings.Split(invocation.WorkerSlug, "/")
				if len(parts) == 2 {
					invocation.WorkerSlug = prefix + "-" + parts[0] + "/" + parts[1]
				}
			}
		}
		invocationDetails, err := loadInvocationDetails(source, invocation)
		if err != nil {
			return err
		}
		invocationDetails.SpaceID = createdSpaces[invocation.SpaceSlug].SpaceID

		// Restore BridgeWorker reference if present
		if invocation.WorkerSlug != "" {
			// WorkerSlug already contains the full path (e.g., "space28643/worker-name")
			workerKey := invocation.WorkerSlug
			if worker, ok := createdWorkers[workerKey]; ok {
				invocationDetails.BridgeWorkerID = &worker.BridgeWorkerID
			} else {
				tprint("Warning: Worker %s not found for invocation %s", workerKey, invocation.Slug)
			}
		}

		resp, err := cubClientNew.CreateInvocationWithResponse(ctx, createdSpaces[invocation.SpaceSlug].SpaceID, nil, *invocationDetails)
		if err != nil {
			return err
		}
		if resp.JSON200 == nil {
			return fmt.Errorf("failed to create invocation %s: %s", invocation.Slug, resp.Body)
		}
		tprint("Created invocation %s with id %s in space %s", invocation.Slug, resp.JSON200.InvocationID.String(), invocation.SpaceSlug)
		createdInvocations[invocation.SpaceSlug+"/"+invocation.Slug] = *resp.JSON200
	}
	// Load Triggers
	for _, trigger := range manifest.Triggers {
		if prefix != "" {
			trigger.SpaceSlug = prefix + "-" + trigger.SpaceSlug
		}
		triggerDetails, err := loadTriggerDetails(source, trigger)
		if err != nil {
			return err
		}
		triggerDetails.SpaceID = createdSpaces[trigger.SpaceSlug].SpaceID

		resp, err := cubClientNew.CreateTriggerWithResponse(ctx, createdSpaces[trigger.SpaceSlug].SpaceID, nil, *triggerDetails)
		if err != nil {
			return err
		}
		if resp.JSON200 == nil {
			return fmt.Errorf("failed to create trigger %s: %s", trigger.Slug, resp.Body)
		}
		tprint("Created trigger %s with id %s in space %s", trigger.Slug, resp.JSON200.TriggerID.String(), trigger.SpaceSlug)
		createdTriggers[trigger.SpaceSlug+"/"+trigger.Slug] = *resp.JSON200
	}
	// Load Attributes
	for _, attribute := range manifest.Attributes {
		if prefix != "" {
			attribute.SpaceSlug = prefix + "-" + attribute.SpaceSlug
		}
		attributeDetails, err := loadAttributeDetails(source, attribute)
		if err != nil {
			return err
		}
		attributeDetails.SpaceID = createdSpaces[attribute.SpaceSlug].SpaceID

		resp, err := cubClientNew.CreateAttributeWithResponse(ctx, createdSpaces[attribute.SpaceSlug].SpaceID, nil, *attributeDetails)
		if err != nil {
			return err
		}
		if resp.JSON200 == nil {
			return fmt.Errorf("failed to create attribute %s: %s", attribute.Slug, resp.Body)
		}
		tprint("Created attribute %s with id %s in space %s", attribute.Slug, resp.JSON200.AttributeID.String(), attribute.SpaceSlug)
		createdAttributes[attribute.SpaceSlug+"/"+attribute.Slug] = *resp.JSON200
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

func loadLinkDetails(dir string, link LinkEntry) (*goclientnew.Link, error) {
	jsonBytes, err := os.ReadFile(dir + link.DetailsLoc)
	if err != nil {
		return nil, err
	}
	linkDetails := &goclientnew.Link{}
	err = json.Unmarshal(jsonBytes, linkDetails)
	if err != nil {
		return nil, err
	}
	linkDetails.Slug = link.Slug
	return linkDetails, nil
}

func loadViewDetails(dir string, view ViewEntry) (*goclientnew.View, error) {
	jsonBytes, err := os.ReadFile(dir + view.DetailsLoc)
	if err != nil {
		return nil, err
	}
	viewDetails := &goclientnew.View{}
	err = json.Unmarshal(jsonBytes, viewDetails)
	if err != nil {
		return nil, err
	}
	viewDetails.Slug = view.Slug
	return viewDetails, nil
}

func loadFilterDetails(dir string, filter FilterEntry) (*goclientnew.Filter, error) {
	jsonBytes, err := os.ReadFile(dir + filter.DetailsLoc)
	if err != nil {
		return nil, err
	}
	filterDetails := &goclientnew.Filter{}
	err = json.Unmarshal(jsonBytes, filterDetails)
	if err != nil {
		return nil, err
	}
	filterDetails.Slug = filter.Slug
	return filterDetails, nil
}

func loadTagDetails(dir string, tag TagEntry) (*goclientnew.Tag, error) {
	jsonBytes, err := os.ReadFile(dir + tag.DetailsLoc)
	if err != nil {
		return nil, err
	}
	tagDetails := &goclientnew.Tag{}
	err = json.Unmarshal(jsonBytes, tagDetails)
	if err != nil {
		return nil, err
	}
	tagDetails.Slug = tag.Slug
	return tagDetails, nil
}

func loadInvocationDetails(dir string, invocation InvocationEntry) (*goclientnew.Invocation, error) {
	jsonBytes, err := os.ReadFile(dir + invocation.DetailsLoc)
	if err != nil {
		return nil, err
	}
	invocationDetails := &goclientnew.Invocation{}
	err = json.Unmarshal(jsonBytes, invocationDetails)
	if err != nil {
		return nil, err
	}
	invocationDetails.Slug = invocation.Slug
	return invocationDetails, nil
}

func loadTriggerDetails(dir string, trigger TriggerEntry) (*goclientnew.Trigger, error) {
	jsonBytes, err := os.ReadFile(dir + trigger.DetailsLoc)
	if err != nil {
		return nil, err
	}
	triggerDetails := &goclientnew.Trigger{}
	err = json.Unmarshal(jsonBytes, triggerDetails)
	if err != nil {
		return nil, err
	}
	triggerDetails.Slug = trigger.Slug
	return triggerDetails, nil
}

func loadAttributeDetails(dir string, attribute AttributeEntry) (*goclientnew.Attribute, error) {
	jsonBytes, err := os.ReadFile(dir + attribute.DetailsLoc)
	if err != nil {
		return nil, err
	}
	attributeDetails := &goclientnew.Attribute{}
	err = json.Unmarshal(jsonBytes, attributeDetails)
	if err != nil {
		return nil, err
	}
	attributeDetails.Slug = attribute.Slug
	return attributeDetails, nil
}

func loadRemotePackage(sourceURL string, prefix string) error {
	tprint("Loading package from remote URL: %s", sourceURL)

	loader, err := NewRemotePackageLoader(sourceURL)
	if err != nil {
		return fmt.Errorf("failed to create remote loader: %w", err)
	}

	manifest, err := loader.LoadManifest()
	if err != nil {
		return fmt.Errorf("failed to load remote manifest: %w", err)
	}

	// Process spaces
	for _, space := range manifest.Spaces {
		if prefix != "" {
			space.Slug = prefix + "-" + space.Slug
		}
		spaceDetails, err := loader.LoadSpaceDetails(space)
		if err != nil {
			return fmt.Errorf("failed to load remote space %s: %w", space.Slug, err)
		}
		resp, err := cubClientNew.CreateSpaceWithResponse(ctx, nil, *spaceDetails)
		if err != nil {
			return err
		}
		if resp.JSON200 == nil {
			return fmt.Errorf("failed to create space %s: %s", space.Slug, resp.Body)
		}
		createdSpaces[space.Slug] = *resp.JSON200
	}

	// Process workers
	for _, worker := range manifest.Workers {
		if prefix != "" {
			worker.SpaceSlug = prefix + "-" + worker.SpaceSlug
		}
		workerDetails, err := loader.LoadWorkerDetails(worker)
		if err != nil {
			return fmt.Errorf("failed to load remote worker %s: %w", worker.Slug, err)
		}
		workerDetails.SpaceID = createdSpaces[worker.SpaceSlug].SpaceID
		workerParams := &goclientnew.CreateBridgeWorkerParams{}
		resp, err := cubClientNew.CreateBridgeWorkerWithResponse(ctx, createdSpaces[worker.SpaceSlug].SpaceID, workerParams, *workerDetails)
		if err != nil {
			return err
		}
		if resp.JSON200 == nil {
			return fmt.Errorf("failed to create worker %s: %s", worker.Slug, resp.Body)
		}
		tprint("Created worker %s with id %s and space slug %s", worker.Slug, resp.JSON200.BridgeWorkerID.String(), worker.SpaceSlug)
		createdWorkers[worker.SpaceSlug+"/"+worker.Slug] = *resp.JSON200
	}

	// Process targets
	for _, target := range manifest.Targets {
		if prefix != "" {
			target.SpaceSlug = prefix + "-" + target.SpaceSlug
			target.Worker = prefix + "-" + target.Worker
		}
		targetDetails, err := loader.LoadTargetDetails(target)
		if err != nil {
			return fmt.Errorf("failed to load remote target %s: %w", target.Slug, err)
		}
		targetDetails.SpaceID = createdSpaces[target.SpaceSlug].SpaceID
		worker, ok := createdWorkers[target.Worker]
		if !ok {
			return fmt.Errorf("worker %s not found for target %s", target.Worker, target.Slug)
		}
		targetDetails.BridgeWorkerID = worker.BridgeWorkerID
		resp, err := cubClientNew.CreateTargetWithResponse(ctx, createdSpaces[target.SpaceSlug].SpaceID, nil, *targetDetails)
		if err != nil {
			return err
		}
		if resp.JSON200 == nil {
			return fmt.Errorf("failed to create target %s: %s", target.Slug, resp.Body)
		}
		tprint("Created target %s with id %s and space slug %s", target.Slug, resp.JSON200.TargetID.String(), target.SpaceSlug)
		createdTargets[target.SpaceSlug+"/"+target.Slug] = *resp.JSON200
	}

	// Process units
	for _, unit := range manifest.Units {
		if prefix != "" {
			unit.SpaceSlug = prefix + "-" + unit.SpaceSlug
			if unit.Target != "" {
				unit.Target = prefix + "-" + unit.Target
			}
		}
		unitDetails, err := loader.LoadUnitDetails(unit)
		if err != nil {
			return fmt.Errorf("failed to load remote unit %s: %w", unit.Slug, err)
		}
		unitDetails.SpaceID = createdSpaces[unit.SpaceSlug].SpaceID
		if unit.Target != "" {
			target, ok := createdTargets[unit.Target]
			if ok {
				unitDetails.TargetID = &target.TargetID
			}
		}
		unitData, err := loader.LoadUnitData(unit)
		if err != nil {
			return fmt.Errorf("failed to load remote unit data for %s: %w", unit.Slug, err)
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
		createdUnits[unit.SpaceSlug+"/"+unit.Slug] = *resp.JSON200
	}

	// Process links
	for _, link := range manifest.Links {
		if prefix != "" {
			link.SpaceSlug = prefix + "-" + link.SpaceSlug
			// FromUnit and ToUnit use slash notation, need to prefix the space part
			if link.FromUnit != "" {
				parts := strings.Split(link.FromUnit, "/")
				if len(parts) == 2 {
					link.FromUnit = prefix + "-" + parts[0] + "/" + parts[1]
				}
			}
			if link.ToUnit != "" {
				parts := strings.Split(link.ToUnit, "/")
				if len(parts) == 2 {
					link.ToUnit = prefix + "-" + parts[0] + "/" + parts[1]
				}
			}
		}
		linkDetails, err := loader.LoadLinkDetails(link)
		if err != nil {
			return fmt.Errorf("failed to load remote link %s: %w", link.Slug, err)
		}
		linkDetails.SpaceID = createdSpaces[link.SpaceSlug].SpaceID

		// Find the from unit using the slash notation
		fromUnit, ok := createdUnits[link.FromUnit]
		if !ok {
			return fmt.Errorf("from unit %s not found for link %s", link.FromUnit, link.Slug)
		}
		linkDetails.FromUnitID = fromUnit.UnitID

		// Find the to unit using the slash notation
		toUnit, ok := createdUnits[link.ToUnit]
		if !ok {
			return fmt.Errorf("to unit %s not found for link %s", link.ToUnit, link.Slug)
		}
		linkDetails.ToUnitID = toUnit.UnitID

		// Extract ToSpaceSlug from ToUnit slash notation
		toParts := strings.Split(link.ToUnit, "/")
		if len(toParts) == 2 {
			toSpaceSlug := toParts[0]
			linkDetails.ToSpaceID = createdSpaces[toSpaceSlug].SpaceID
		}

		resp, err := cubClientNew.CreateLinkWithResponse(ctx, createdSpaces[link.SpaceSlug].SpaceID, nil, *linkDetails)
		if err != nil {
			return err
		}
		if resp.JSON200 == nil {
			return fmt.Errorf("failed to create link %s: %s", link.Slug, resp.Body)
		}
		tprint("Created link %s with id %s in space %s", link.Slug, resp.JSON200.LinkID.String(), link.SpaceSlug)
	}

	// Process filters first (as Views may reference them)
	for _, filter := range manifest.Filters {
		if prefix != "" {
			filter.SpaceSlug = prefix + "-" + filter.SpaceSlug
			if filter.FromSpaceSlug != "" {
				filter.FromSpaceSlug = prefix + "-" + filter.FromSpaceSlug
			}
		}
		filterDetails, err := loader.LoadFilterDetails(filter)
		if err != nil {
			return fmt.Errorf("failed to load remote filter %s: %w", filter.Slug, err)
		}
		if filter.SpaceSlug != "" {
			filterDetails.SpaceID = createdSpaces[filter.SpaceSlug].SpaceID
		}
		// Set FromSpaceID if FromSpaceSlug is specified
		if filter.FromSpaceSlug != "" {
			fromSpaceID := createdSpaces[filter.FromSpaceSlug].SpaceID
			filterDetails.FromSpaceID = &fromSpaceID
		}

		resp, err := cubClientNew.CreateFilterWithResponse(ctx, createdSpaces[filter.SpaceSlug].SpaceID, nil, *filterDetails)
		if err != nil {
			return err
		}
		if resp.JSON200 == nil {
			return fmt.Errorf("failed to create filter %s: %s", filter.Slug, resp.Body)
		}
		tprint("Created filter %s with id %s", filter.Slug, resp.JSON200.FilterID.String())
		createdFilters[filter.SpaceSlug+"/"+filter.Slug] = *resp.JSON200
	}

	// Process views (after filters since they may reference them)
	for _, view := range manifest.Views {
		if prefix != "" {
			view.SpaceSlug = prefix + "-" + view.SpaceSlug
		}
		viewDetails, err := loader.LoadViewDetails(view)
		if err != nil {
			return fmt.Errorf("failed to load remote view %s: %w", view.Slug, err)
		}
		viewDetails.SpaceID = createdSpaces[view.SpaceSlug].SpaceID

		// Set FilterID if FilterSlug is specified
		if view.FilterSlug != "" {
			// FilterSlug already contains the full path (e.g., "space28643/headlamp")
			// Use it directly as the key
			filterKey := view.FilterSlug
			if filter, ok := createdFilters[filterKey]; ok {
				viewDetails.FilterID = filter.FilterID
			} else {
				tprint("Warning: Filter %s not found for view %s", filterKey, view.Slug)
			}
		}

		resp, err := cubClientNew.CreateViewWithResponse(ctx, createdSpaces[view.SpaceSlug].SpaceID, nil, *viewDetails)
		if err != nil {
			return err
		}
		if resp.JSON200 == nil {
			return fmt.Errorf("failed to create view %s: %s", view.Slug, resp.Body)
		}
		tprint("Created view %s with id %s in space %s", view.Slug, resp.JSON200.ViewID.String(), view.SpaceSlug)
	}

	// Process tags
	for _, tag := range manifest.Tags {
		if prefix != "" {
			tag.SpaceSlug = prefix + "-" + tag.SpaceSlug
		}
		tagDetails, err := loader.LoadTagDetails(tag)
		if err != nil {
			return fmt.Errorf("failed to load remote tag %s: %w", tag.Slug, err)
		}
		tagDetails.SpaceID = createdSpaces[tag.SpaceSlug].SpaceID

		resp, err := cubClientNew.CreateTagWithResponse(ctx, createdSpaces[tag.SpaceSlug].SpaceID, nil, *tagDetails)
		if err != nil {
			return err
		}
		if resp.JSON200 == nil {
			return fmt.Errorf("failed to create tag %s: %s", tag.Slug, resp.Body)
		}
		tprint("Created tag %s with id %s in space %s", tag.Slug, resp.JSON200.TagID.String(), tag.SpaceSlug)
	}

	// Process invocations
	for _, invocation := range manifest.Invocations {
		if prefix != "" {
			invocation.SpaceSlug = prefix + "-" + invocation.SpaceSlug
			if invocation.WorkerSlug != "" {
				// WorkerSlug contains "space/worker" format, need to prefix the space part
				parts := strings.Split(invocation.WorkerSlug, "/")
				if len(parts) == 2 {
					invocation.WorkerSlug = prefix + "-" + parts[0] + "/" + parts[1]
				}
			}
		}
		invocationDetails, err := loader.LoadInvocationDetails(invocation)
		if err != nil {
			return fmt.Errorf("failed to load remote invocation %s: %w", invocation.Slug, err)
		}
		invocationDetails.SpaceID = createdSpaces[invocation.SpaceSlug].SpaceID

		// Restore BridgeWorker reference if present
		if invocation.WorkerSlug != "" {
			// WorkerSlug already contains the full path (e.g., "space28643/worker-name")
			workerKey := invocation.WorkerSlug
			if worker, ok := createdWorkers[workerKey]; ok {
				invocationDetails.BridgeWorkerID = &worker.BridgeWorkerID
			} else {
				tprint("Warning: Worker %s not found for invocation %s", workerKey, invocation.Slug)
			}
		}

		resp, err := cubClientNew.CreateInvocationWithResponse(ctx, createdSpaces[invocation.SpaceSlug].SpaceID, nil, *invocationDetails)
		if err != nil {
			return err
		}
		if resp.JSON200 == nil {
			return fmt.Errorf("failed to create invocation %s: %s", invocation.Slug, resp.Body)
		}
		tprint("Created invocation %s with id %s in space %s", invocation.Slug, resp.JSON200.InvocationID.String(), invocation.SpaceSlug)
	}

	// Process triggers
	for _, trigger := range manifest.Triggers {
		if prefix != "" {
			trigger.SpaceSlug = prefix + "-" + trigger.SpaceSlug
		}
		triggerDetails, err := loader.LoadTriggerDetails(trigger)
		if err != nil {
			return fmt.Errorf("failed to load remote trigger %s: %w", trigger.Slug, err)
		}
		triggerDetails.SpaceID = createdSpaces[trigger.SpaceSlug].SpaceID

		resp, err := cubClientNew.CreateTriggerWithResponse(ctx, createdSpaces[trigger.SpaceSlug].SpaceID, nil, *triggerDetails)
		if err != nil {
			return err
		}
		if resp.JSON200 == nil {
			return fmt.Errorf("failed to create trigger %s: %s", trigger.Slug, resp.Body)
		}
		tprint("Created trigger %s with id %s in space %s", trigger.Slug, resp.JSON200.TriggerID.String(), trigger.SpaceSlug)
	}

	// Process attributes
	for _, attribute := range manifest.Attributes {
		if prefix != "" {
			attribute.SpaceSlug = prefix + "-" + attribute.SpaceSlug
		}
		attributeDetails, err := loader.LoadAttributeDetails(attribute)
		if err != nil {
			return fmt.Errorf("failed to load remote attribute %s: %w", attribute.Slug, err)
		}
		attributeDetails.SpaceID = createdSpaces[attribute.SpaceSlug].SpaceID

		resp, err := cubClientNew.CreateAttributeWithResponse(ctx, createdSpaces[attribute.SpaceSlug].SpaceID, nil, *attributeDetails)
		if err != nil {
			return err
		}
		if resp.JSON200 == nil {
			return fmt.Errorf("failed to create attribute %s: %s", attribute.Slug, resp.Body)
		}
		tprint("Created attribute %s with id %s in space %s", attribute.Slug, resp.JSON200.AttributeID.String(), attribute.SpaceSlug)
	}

	tprint("Successfully loaded remote package from %s", sourceURL)
	return nil
}
