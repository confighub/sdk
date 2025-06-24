// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"fmt"
	"net/url"
	"strconv"

	goclientnew "github.com/confighub/sdk/openapi/goclient-new"
	"github.com/google/uuid"
	"github.com/spf13/cobra"
)

var triggerListCmd = &cobra.Command{
	Use:   "list",
	Short: "List triggers",
	Long: `List triggers you have access to in a space. The output includes display names, slugs, trigger IDs, events, toolchain types, function names, and the number of arguments.

Examples:
  # List all triggers in a space with headers
  cub trigger list --space my-space

  # List triggers without headers for scripting
  cub trigger list --space my-space --noheader

  # List triggers in JSON format
  cub trigger list --space my-space --json

  # List only trigger slugs
  cub trigger list --space my-space --noheader --slugs-only

  # List triggers with a specific event type
  cub trigger list --space my-space --where "Event = 'Mutation'"

  # List triggers for a specific toolchain
  cub trigger list --space my-space --where "ToolchainType = 'Kubernetes/YAML'"

  # List triggers using a specific function
  cub trigger list --space my-space --where "FunctionName = 'cel-validate'"`,
	Args: cobra.ExactArgs(0),
	RunE: triggerListCmdRun,
}

func init() {
	addStandardListFlags(triggerListCmd)
	triggerCmd.AddCommand(triggerListCmd)
}

func triggerListCmdRun(cmd *cobra.Command, args []string) error {
	triggers, err := apiListTriggers(selectedSpaceID, where)
	if err != nil {
		return err
	}
	displayListResults(triggers, getTriggerSlug, displayTriggerList)
	return nil
}

func getTriggerSlug(trigger *goclientnew.ExtendedTrigger) string {
	return trigger.Trigger.Slug
}

func displayTriggerList(triggers []*goclientnew.ExtendedTrigger) {
	table := tableView()
	if !noheader {
		table.SetHeader([]string{"Display-Name", "Slug", "ID", "Worker-ID", "Event", "Validating", "Disabled", "Enforced", "Toolchain-Type", "Function-Name", "Num-Args"})
	}
	for _, t := range triggers {
		trigger := t.Trigger
		table.Append([]string{
			trigger.DisplayName,
			trigger.Slug,
			trigger.TriggerID.String(),
			uuidPtrToString(trigger.BridgeWorkerID),
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

func apiListTriggers(spaceID string, whereFilter string) ([]*goclientnew.ExtendedTrigger, error) {
	newParams := &goclientnew.ListTriggersParams{}
	include := "BridgeWorkerID"
	newParams.Include = &include
	if whereFilter != "" {
		whereFilter = url.QueryEscape(whereFilter)
		newParams.Where = &whereFilter
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
