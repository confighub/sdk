// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"github.com/confighub/sdk/core/cubapi"
	goclientnew "github.com/confighub/sdk/core/openapi/goclient-new"
	"github.com/google/uuid"
	"github.com/spf13/cobra"
)

var targetListCmd = &cobra.Command{
	Use:         "list",
	Short:       "List targets",
	Long:        getCommandHelp(`List targets you have access to in a space or across all spaces. Use --space "*" to list targets across all spaces.`, ""),
	Args:        cobra.ExactArgs(0),
	Annotations: map[string]string{"OrgLevel": ""},
	RunE:        targetListCmdRun,
}

// Default columns to display when no custom columns are specified
var defaultTargetColumns = []string{"Target.Slug", "BridgeWorker.Slug", "Target.ProviderType", "Target.Parameters", "Target.SpaceSlug"}

// targetListInclude is the Include parameter for target list queries (the related
// entities expanded into each ExtendedTarget).
const targetListInclude = "SpaceID,BridgeWorkerID,TriggerFilterID,TriggerIDs"

// targetBaseSelectFields are the fields always returned by target list queries,
// regardless of the requested columns.
var targetBaseSelectFields = []string{"Slug", "TargetID", "BridgeWorkerID", "SpaceID", "OrganizationID"}

// Target-specific aliases
var targetAliases = map[string]string{
	"Name": "Target.Slug",
	"ID":   "Target.TargetID",
}

// Target custom column dependencies
var targetCustomColumnDependencies = map[string][]string{}

func init() {
	addStandardListFlags(targetListCmd)
	enableWebFlag(targetListCmd)
	targetCmd.AddCommand(targetListCmd)
}

func targetListCmdRun(cmd *cobra.Command, args []string) error {
	if webFlag {
		ctx := contextManager.ActiveContext()
		url := cubapi.GetTargetListURL(ctx.Coordinate.ServerURL)
		return openWebUI(url)
	}

	filterID, err := parseFilterFlag(filter)
	if err != nil {
		return err
	}

	targets, err := apiListTargets(selectedSpaceID, where, selectFields, filterID)
	if err != nil {
		return err
	}
	displayListResults(targets, getTargetSlug, displayTargetList)
	return nil
}

func getTargetSlug(exTarget *goclientnew.ExtendedTarget) string {
	return prefixedSlug(exTarget.Target.SpaceSlug, exTarget.Target.Slug)
}

func displayTargetList(exTargets []*goclientnew.ExtendedTarget) {
	table := tableView()
	if !noheader {
		table.SetHeader([]string{"Name", "Worker", "ProviderType", "Parameters", "Space"})
	}
	for _, exTarget := range exTargets {
		workerSlug := ""
		if exTarget.BridgeWorker != nil {
			workerSlug = exTarget.BridgeWorker.Slug
		}
		table.Append([]string{
			exTarget.Target.Slug,
			workerSlug,
			exTarget.Target.ProviderType,
			exTarget.Target.Parameters,
			exTarget.Target.SpaceSlug,
		})
	}
	table.Render()
}

// apiListTargets lists targets via the org-level endpoint, scoped to a single
// space by a SpaceID clause unless spaceID is "*" (list across all spaces).
func apiListTargets(spaceID string, whereFilter string, selectParam string, filterParam string) ([]*goclientnew.ExtendedTarget, error) {
	where := cubapi.NewWhere(whereFilter)
	if spaceID != "*" {
		where = where.SpaceID(goclientnew.UUID(uuid.MustParse(spaceID)))
	}
	return apiListAllTargets(where, selectParam, filterParam)
}

func apiListAllTargets(where cubapi.Where, selectParam string, filterParam string) ([]*goclientnew.ExtendedTarget, error) {
	selectValue := handleSelectParameter(selectParam, selectFields, func() string {
		return buildSelectList("Target", nil, targetListInclude, defaultTargetColumns, targetAliases, targetCustomColumnDependencies, targetBaseSelectFields)
	})
	return cubapi.ListTargets(ctx, cubClient, where, cubapi.ListOpts{
		Select:   cubapi.SelectFields(selectValue),
		Include:  targetListInclude,
		Filter:   filterParam,
		Contains: contains,
	})
}
