// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"fmt"
	"strconv"

	goclientnew "github.com/confighub/sdk/openapi/goclient-new"
	"github.com/google/uuid"
	"github.com/spf13/cobra"
)

var triggerListCmd = &cobra.Command{
	Use:   "list",
	Short: "List triggers",
	Long: `List triggers you have access to in a space or across all spaces. The output includes slugs, worker slugs, events, validation status, disabled status, enforcement status, toolchain types, function names, and the number of arguments.

Examples:
  # List all triggers in a space with headers
  cub trigger list --space my-space

  # List triggers across all spaces (requires --space "*")
  cub trigger list --space "*" --where "Event = 'Mutation'"

  # List triggers without headers for scripting
  cub trigger list --space my-space --no-header

  # List triggers in JSON format
  cub trigger list --space my-space --json

  # List only trigger names
  cub trigger list --space my-space --no-header --names

  # List triggers with a specific event type
  cub trigger list --space my-space --where "Event = 'Mutation'"

  # List triggers for a specific toolchain
  cub trigger list --space my-space --where "ToolchainType = 'Kubernetes/YAML'"

  # List triggers using a specific function
  cub trigger list --space my-space --where "FunctionName = 'cel-validate'"
  
  # List disabled triggers across all spaces
  cub trigger list --space "*" --where "Disabled = true"`,
	Args:        cobra.ExactArgs(0),
	RunE:        triggerListCmdRun,
	Annotations: map[string]string{"OrgLevel": ""},
}

// Default columns to display when no custom columns are specified
var defaultTriggerColumns = []string{"Trigger.Slug", "Space.Slug", "BridgeWorker.Slug", "Trigger.Event", "Trigger.Validating", "Trigger.Disabled", "Trigger.Enforced", "Trigger.ToolchainType", "Trigger.FunctionName", "Trigger.Arguments"}

// Trigger-specific aliases
var triggerAliases = map[string]string{
	"Name": "Trigger.Slug",
	"ID":   "Trigger.TriggerID",
}

// Trigger custom column dependencies
var triggerCustomColumnDependencies = map[string][]string{}

func init() {
	addStandardListFlags(triggerListCmd)
	triggerCmd.AddCommand(triggerListCmd)
}

func triggerListCmdRun(cmd *cobra.Command, args []string) error {
	var extendedTriggers []*goclientnew.ExtendedTrigger
	var err error

	if selectedSpaceID == "*" {
		extendedTriggers, err = apiSearchTriggers(where, selectFields)
		if err != nil {
			return err
		}
	} else {
		extendedTriggers, err = apiListTriggers(selectedSpaceID, where, selectFields)
		if err != nil {
			return err
		}
	}

	displayListResults(extendedTriggers, getTriggerSlug, displayTriggerList)
	return nil
}

func getTriggerSlug(trigger *goclientnew.ExtendedTrigger) string {
	return trigger.Trigger.Slug
}

func displayTriggerList(triggers []*goclientnew.ExtendedTrigger) {
	table := tableView()
	if !noheader {
		table.SetHeader([]string{"Name", "Space", "Worker", "Event", "Validating", "Disabled", "Enforced", "Toolchain-Type", "Function-Name", "Num-Args"})
	}
	for _, t := range triggers {
		trigger := t.Trigger
		workerSlug := ""
		if t.BridgeWorker != nil {
			workerSlug = t.BridgeWorker.Slug
		}
		spaceSlug := t.Trigger.TriggerID.String()
		if t.Space != nil {
			spaceSlug = t.Space.Slug
		} else if selectedSpaceID != "*" {
			spaceSlug = selectedSpaceSlug
		}
		table.Append([]string{
			trigger.Slug,
			spaceSlug,
			workerSlug,
			trigger.Event,
			strconv.FormatBool(trigger.Validating),
			strconv.FormatBool(trigger.Disabled),
			strconv.FormatBool(trigger.Enforced),
			trigger.ToolchainType,
			trigger.FunctionName,
			fmt.Sprintf("%d", len(trigger.Arguments)),
		})
	}
	table.Render()
}

func apiListTriggers(spaceID string, whereFilter string, selectParam string) ([]*goclientnew.ExtendedTrigger, error) {
	newParams := &goclientnew.ListTriggersParams{}
	include := "SpaceID,BridgeWorkerID"
	newParams.Include = &include
	if whereFilter != "" {
		newParams.Where = &whereFilter
	}
	if contains != "" {
		newParams.Contains = &contains
	}
	selectValue := handleSelectParameter(selectParam, selectFields, func() string {
		baseFields := []string{"Slug", "TriggerID", "SpaceID", "OrganizationID"}
		return buildSelectList("Trigger", "", include, defaultTriggerColumns, triggerAliases, triggerCustomColumnDependencies, baseFields)
	})
	if selectValue != "" && selectValue != "*" {
		newParams.Select = &selectValue
	}
	triggersRes, err := cubClientNew.ListTriggersWithResponse(ctx, uuid.MustParse(spaceID), newParams)
	if IsAPIError(err, triggersRes) {
		return nil, InterpretErrorGeneric(err, triggersRes)
	}

	triggers := make([]*goclientnew.ExtendedTrigger, 0, len(*triggersRes.JSON200))
	for _, trigger := range *triggersRes.JSON200 {
		triggers = append(triggers, &trigger)
	}

	return triggers, nil
}

func apiSearchTriggers(whereFilter string, selectParam string) ([]*goclientnew.ExtendedTrigger, error) {
	newParams := &goclientnew.ListAllTriggersParams{}
	if whereFilter != "" {
		newParams.Where = &whereFilter
	}
	if contains != "" {
		newParams.Contains = &contains
	}

	include := "SpaceID,BridgeWorkerID"
	newParams.Include = &include

	selectValue := handleSelectParameter(selectParam, selectFields, func() string {
		baseFields := []string{"Slug", "TriggerID", "SpaceID", "OrganizationID"}
		return buildSelectList("Trigger", "", include, defaultTriggerColumns, triggerAliases, triggerCustomColumnDependencies, baseFields)
	})
	if selectValue != "" && selectValue != "*" {
		newParams.Select = &selectValue
	}

	res, err := cubClientNew.ListAllTriggers(ctx, newParams)
	if err != nil {
		return nil, err
	}
	triggersRes, err := goclientnew.ParseListAllTriggersResponse(res)
	if IsAPIError(err, triggersRes) {
		return nil, InterpretErrorGeneric(err, triggersRes)
	}

	extendedTriggers := make([]*goclientnew.ExtendedTrigger, 0, len(*triggersRes.JSON200))
	for _, trigger := range *triggersRes.JSON200 {
		extendedTriggers = append(extendedTriggers, &trigger)
	}

	return extendedTriggers, nil
}
