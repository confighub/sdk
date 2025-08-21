// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	goclientnew "github.com/confighub/sdk/openapi/goclient-new"
	"github.com/google/uuid"
	"github.com/spf13/cobra"
)

var targetListCmd = &cobra.Command{
	Use:         "list",
	Short:       "List targets",
	Long:        `List targets you have access to in a space or across all spaces. Use --space "*" to list targets across all spaces.`,
	Args:        cobra.ExactArgs(0),
	Annotations: map[string]string{"OrgLevel": ""},
	RunE:        targetListCmdRun,
}

// Default columns to display when no custom columns are specified
var defaultTargetColumns = []string{"Target.Slug", "BridgeWorker.Slug", "Target.ProviderType", "Target.Parameters", "Space.Slug"}

// Target-specific aliases
var targetAliases = map[string]string{
	"Name": "Target.Slug",
	"ID":   "Target.TargetID",
}

// Target custom column dependencies
var targetCustomColumnDependencies = map[string][]string{}

func init() {
	addStandardListFlags(targetListCmd)
	targetCmd.AddCommand(targetListCmd)
}

func targetListCmdRun(cmd *cobra.Command, args []string) error {
	var targets []*goclientnew.ExtendedTarget
	var err error
	if selectedSpaceID == "*" {
		// Cross-space listing
		targets, err = apiListAllTargets(where, selectFields)
		if err != nil {
			return err
		}
	} else {
		// Single space listing
		targets, err = apiListTargets(selectedSpaceID, where, selectFields)
		if err != nil {
			return err
		}
	}
	displayListResults(targets, getTargetSlug, displayTargetList)
	return nil
}

func getTargetSlug(exTarget *goclientnew.ExtendedTarget) string {
	return exTarget.Target.Slug
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
		spaceSlug := ""
		if exTarget.Space != nil {
			spaceSlug = exTarget.Space.Slug
		}
		table.Append([]string{
			exTarget.Target.Slug,
			workerSlug,
			exTarget.Target.ProviderType,
			exTarget.Target.Parameters,
			spaceSlug,
		})
	}
	table.Render()
}

func apiListTargets(spaceID string, whereFilter string, selectParam string) ([]*goclientnew.ExtendedTarget, error) {
	newParams := &goclientnew.ListTargetsParams{}
	if whereFilter != "" {
		newParams.Where = &whereFilter
	}
	if contains != "" {
		newParams.Contains = &contains
	}
	include := "SpaceID,BridgeWorkerID"
	newParams.Include = &include
	// Handle select parameter
	selectValue := handleSelectParameter(selectParam, selectFields, func() string {
		baseFields := []string{"Slug", "TargetID", "BridgeWorkerID", "SpaceID", "OrganizationID"}
		return buildSelectList("Target", "", include, defaultTargetColumns, targetAliases, targetCustomColumnDependencies, baseFields)
	})
	if selectValue != "" && selectValue != "*" {
		newParams.Select = &selectValue
	}
	targetsRes, err := cubClientNew.ListTargetsWithResponse(ctx, uuid.MustParse(spaceID), newParams)
	if IsAPIError(err, targetsRes) {
		return nil, InterpretErrorGeneric(err, targetsRes)
	}

	targets := make([]*goclientnew.ExtendedTarget, 0, len(*targetsRes.JSON200))
	for _, target := range *targetsRes.JSON200 {
		targets = append(targets, &target)
	}
	return targets, nil
}

func apiListAllTargets(whereFilter string, selectParam string) ([]*goclientnew.ExtendedTarget, error) {
	newParams := &goclientnew.ListAllTargetsParams{}
	if whereFilter != "" {
		newParams.Where = &whereFilter
	}
	if contains != "" {
		newParams.Contains = &contains
	}
	include := "SpaceID,BridgeWorkerID"
	newParams.Include = &include
	// Handle select parameter
	selectValue := handleSelectParameter(selectParam, selectFields, func() string {
		baseFields := []string{"Slug", "TargetID", "SpaceID", "OrganizationID"}
		return buildSelectList("Target", "", include, defaultTargetColumns, targetAliases, targetCustomColumnDependencies, baseFields)
	})
	if selectValue != "" && selectValue != "*" {
		newParams.Select = &selectValue
	}
	targetsRes, err := cubClientNew.ListAllTargetsWithResponse(ctx, newParams)
	if IsAPIError(err, targetsRes) {
		return nil, InterpretErrorGeneric(err, targetsRes)
	}

	targets := make([]*goclientnew.ExtendedTarget, 0, len(*targetsRes.JSON200))
	for _, target := range *targetsRes.JSON200 {
		targets = append(targets, &target)
	}
	return targets, nil
}
